package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AuthProviderRow represents a row from zeep_system.auth_providers.
type AuthProviderRow struct {
	Provider   string    `json:"provider"`
	Enabled    bool      `json:"enabled"`
	ConfigJSON string    `json:"-"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// GoogleProviderConfig is the typed config for the "google" provider.
type GoogleProviderConfig struct {
	ClientID       string          `json:"client_id"`
	ClientSecret   string          `json:"client_secret,omitempty"`
	ClientSecretSet bool           `json:"client_secret_set,omitempty"`
	RedirectURL    string          `json:"redirect_url"`
	AllowedDomains json.RawMessage `json:"allowed_domains,omitempty"`
}

// AuthProviderResponse is the API response for a provider.
type AuthProviderResponse struct {
	Provider    string          `json:"provider"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config,omitempty"`
	ConfigSet   bool            `json:"config_set"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// authProviderUpsertInput is the JSON body for upserting a provider.
type authProviderUpsertInput struct {
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config,omitempty"`
}

// If no row exists, returns empty provider with env var fallback for "google".
func GetAuthProvider(ctx context.Context, pool *db.Pool, provider string) (*AuthProviderResponse, error) {
	row := &AuthProviderRow{Provider: provider}
	err := pool.QueryRow(ctx,
		`SELECT provider, enabled, COALESCE(config_encrypted, ''), updated_at
		 FROM zeep_system.auth_providers WHERE provider = $1`,
		provider,
	).Scan(&row.Provider, &row.Enabled, &row.ConfigJSON, &row.UpdatedAt)
	if err != nil {
		if provider == "google" {
			return googleFallbackConfig(), nil
		}
		return &AuthProviderResponse{
			Provider:  provider,
			Enabled:   false,
			Config:    json.RawMessage("{}"),
			UpdatedAt: time.Now(),
		}, nil
	}

	return decryptProviderRow(row), nil
}

// ListAuthProviders returns all providers, optionally revealing secrets.
func ListAuthProviders(ctx context.Context, pool *db.Pool, reveal bool) ([]*AuthProviderResponse, error) {
	rows, err := pool.Query(ctx,
		`SELECT provider, enabled, COALESCE(config_encrypted, ''), updated_at
		 FROM zeep_system.auth_providers ORDER BY provider`,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list auth providers: %w", err)
	}
	defer rows.Close()

	var results []*AuthProviderResponse
	for rows.Next() {
		var r AuthProviderRow
		if err := rows.Scan(&r.Provider, &r.Enabled, &r.ConfigJSON, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: scan provider: %w", err)
		}
		resp := decryptProviderRow(&r)
		if !reveal {
			resp.Config = stripSecretFromConfig(r.Provider, resp.Config)
		}
		results = append(results, resp)
	}

	hasGoogle := false
	for _, r := range results {
		if r.Provider == "google" {
			hasGoogle = true
			break
		}
	}
	if !hasGoogle {
		fb := googleFallbackConfig()
		if fb.ConfigSet || fb.Enabled {
			if !reveal {
				fb.Config = stripSecretFromConfig("google", fb.Config)
			}
			results = append(results, fb)
		}
	}

	if results == nil {
		results = []*AuthProviderResponse{}
	}
	return results, nil
}

// Config JSON is encrypted before storing.
func UpsertAuthProvider(ctx context.Context, pool *db.Pool, provider string, input *authProviderUpsertInput) (*AuthProviderResponse, error) {
	encrypted := ""
	if len(input.Config) > 0 && string(input.Config) != "{}" {
		existing, _ := GetAuthProvider(ctx, pool, provider)
		fullConfig := mergeProviderConfig(provider, input.Config, existing)

		plaintext := string(fullConfig)
		var err error
		encrypted, err = crypto.Encrypt(plaintext)
		if err != nil {
			return nil, fmt.Errorf("dashboard: encrypt provider config: %w", err)
		}
	} else {
		// Preserve existing encrypted config
		var current string
		err := pool.QueryRow(ctx,
			`SELECT config_encrypted FROM zeep_system.auth_providers WHERE provider = $1`,
			provider,
		).Scan(&current)
		if err == nil {
			encrypted = current
		}
	}

	var row AuthProviderRow
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.auth_providers (provider, enabled, config_encrypted, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (provider) DO UPDATE SET
		   enabled = $2,
		   config_encrypted = CASE WHEN $3 = '' THEN auth_providers.config_encrypted ELSE $3 END,
		   updated_at = now()
		 RETURNING provider, enabled, COALESCE(config_encrypted, ''), updated_at`,
		provider, input.Enabled, encrypted,
	).Scan(&row.Provider, &row.Enabled, &row.ConfigJSON, &row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert provider: %w", err)
	}

	resp := decryptProviderRow(&row)
	return resp, nil
}

func decryptProviderRow(row *AuthProviderRow) *AuthProviderResponse {
	resp := &AuthProviderResponse{
		Provider:  row.Provider,
		Enabled:   row.Enabled,
		ConfigSet: row.ConfigJSON != "",
		UpdatedAt: row.UpdatedAt,
	}

	if row.ConfigJSON == "" {
		resp.Config = json.RawMessage("{}")
		return resp
	}

	decrypted, err := crypto.Decrypt(row.ConfigJSON)
	if err != nil {
		resp.Config = json.RawMessage("{}")
		return resp
	}

	resp.Config = json.RawMessage(decrypted)
	return resp
}

func googleFallbackConfig() *AuthProviderResponse {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	rawDomains := os.Getenv("GOOGLE_ALLOWED_DOMAINS")

	if clientID == "" {
		return &AuthProviderResponse{
			Provider:  "google",
			Enabled:   false,
			Config:    json.RawMessage("{}"),
			ConfigSet: false,
		}
	}

	var domains []string
	if rawDomains != "" {
		for _, d := range splitAndTrim(rawDomains, ",") {
			if d != "" {
				domains = append(domains, d)
			}
		}
	}
	configJSON, _ := json.Marshal(map[string]any{
		"client_id":        clientID,
		"client_secret":    clientSecret,
		"redirect_url":     redirectURL,
		"allowed_domains":  domains,
	})

	return &AuthProviderResponse{
		Provider:  "google",
		Enabled:   true,
		Config:    configJSON,
		ConfigSet: true,
	}
}

func mergeProviderConfig(provider string, input json.RawMessage, existing *AuthProviderResponse) json.RawMessage {
	if existing == nil || existing.Config == nil || string(existing.Config) == "{}" {
		return input
	}

	var inputMap map[string]any
	var existingMap map[string]any
	json.Unmarshal(input, &inputMap)
	json.Unmarshal(existing.Config, &existingMap)

	for k, v := range existingMap {
		if _, exists := inputMap[k]; !exists || inputMap[k] == nil || inputMap[k] == "" {
			inputMap[k] = v
		}
	}

	result, _ := json.Marshal(inputMap)
	return result
}

func stripSecretFromConfig(provider string, config json.RawMessage) json.RawMessage {
	if provider != "google" || config == nil {
		return config
	}
	var cfg map[string]any
	if err := json.Unmarshal(config, &cfg); err != nil {
		return config
	}
	delete(cfg, "client_secret")
	result, _ := json.Marshal(cfg)
	return result
}

func splitAndTrim(s, sep string) []string {
	var result []string
	for _, part := range strings.Split(s, sep) {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

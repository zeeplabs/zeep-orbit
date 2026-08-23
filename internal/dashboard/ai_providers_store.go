package dashboard

// ai_providers_store.go — CRUD for the single global AI provider row
// (zeep_system.ai_providers), one row per provider name ('openai' |
// 'gemini' | 'claude'). Mirrors auth_providers_store.go's merge-on-absent-
// key pattern, adapted for the {model, api_key} shape instead of a JSONB
// config blob. See .specs/features/ai-build-chat/design.md's Components
// section for the AIProviderResponse/aiProviderUpsertInput contract.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AIProviderResponse is the API-safe view of a provider row — never carries
// the API key in any form (AIBC-04).
type AIProviderResponse struct {
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Enabled   bool      `json:"enabled"`
	HasKey    bool      `json:"has_key"`
	UpdatedAt time.Time `json:"updated_at"`
}

// aiProviderUpsertInput is the JSON body for PUT /ai-providers/{provider}.
// APIKey is intentionally omitempty-friendly: an absent/empty key means
// "keep the previously stored key, update model/enabled only" (AIBC-03).
type aiProviderUpsertInput struct {
	Model   string `json:"model"`
	APIKey  string `json:"api_key,omitempty"`
	Enabled bool   `json:"enabled"`
}

// GetAIProvider returns the API-safe view of provider's config. If no row
// exists yet, returns a zero-value response (unconfigured), not an error.
func GetAIProvider(ctx context.Context, pool *db.Pool, provider string) (*AIProviderResponse, error) {
	var (
		model           string
		enabled         bool
		apiKeyEncrypted string
		updatedAt       time.Time
	)
	err := pool.QueryRow(ctx,
		`SELECT model, enabled, api_key_encrypted, updated_at
		 FROM zeep_system.ai_providers WHERE provider = $1`,
		provider,
	).Scan(&model, &enabled, &apiKeyEncrypted, &updatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &AIProviderResponse{Provider: provider}, nil
		}
		return nil, fmt.Errorf("dashboard: get ai provider: %w", err)
	}

	return &AIProviderResponse{
		Provider:  provider,
		Model:     model,
		Enabled:   enabled,
		HasKey:    apiKeyEncrypted != "",
		UpdatedAt: updatedAt,
	}, nil
}

// UpsertAIProvider creates or updates provider's row. When input.APIKey is
// non-empty, it is encrypted (crypto.EncryptAIProviderKey) and replaces any
// stored key. When input.APIKey is empty, the previously stored encrypted
// key (if any) is preserved untouched — merge-on-absent-key, same semantics
// as auth_providers_store.go's mergeProviderConfig (AIBC-03).
func UpsertAIProvider(ctx context.Context, pool *db.Pool, provider string, input *aiProviderUpsertInput) (*AIProviderResponse, error) {
	encrypted := ""
	if input.APIKey != "" {
		var err error
		encrypted, err = crypto.EncryptAIProviderKey(input.APIKey)
		if err != nil {
			return nil, fmt.Errorf("dashboard: encrypt ai provider key: %w", err)
		}
	}

	var (
		model           string
		enabled         bool
		apiKeyEncrypted string
		updatedAt       time.Time
	)
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.ai_providers (provider, model, api_key_encrypted, enabled, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (provider) DO UPDATE SET
		   model             = $2,
		   api_key_encrypted = CASE WHEN $3 = '' THEN ai_providers.api_key_encrypted ELSE $3 END,
		   enabled           = $4,
		   updated_at        = now()
		 RETURNING model, enabled, api_key_encrypted, updated_at`,
		provider, input.Model, encrypted, input.Enabled,
	).Scan(&model, &enabled, &apiKeyEncrypted, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert ai provider: %w", err)
	}

	return &AIProviderResponse{
		Provider:  provider,
		Model:     model,
		Enabled:   enabled,
		HasKey:    apiKeyEncrypted != "",
		UpdatedAt: updatedAt,
	}, nil
}

// resolveDecryptedAIProviderKey is the internal-only accessor used by the
// chat call path (never exposed over HTTP) to get the plaintext model+key
// needed to call the provider's API. A decrypt failure (e.g. the encryption
// key was rotated without migrating stored ciphertext) or a missing/
// disabled/empty-key row is returned as a plain error — the caller treats
// any error here as "provider unconfigured" (spec edge case), never a
// panic and never a partially-decrypted key.
func resolveDecryptedAIProviderKey(ctx context.Context, pool *db.Pool, provider string) (model string, key string, err error) {
	var (
		enabled         bool
		apiKeyEncrypted string
	)
	err = pool.QueryRow(ctx,
		`SELECT model, enabled, api_key_encrypted
		 FROM zeep_system.ai_providers WHERE provider = $1`,
		provider,
	).Scan(&model, &enabled, &apiKeyEncrypted)
	if err != nil {
		return "", "", fmt.Errorf("dashboard: resolve ai provider key: %w", err)
	}
	if !enabled {
		return "", "", fmt.Errorf("dashboard: ai provider %q is not enabled", provider)
	}
	if apiKeyEncrypted == "" {
		return "", "", fmt.Errorf("dashboard: ai provider %q has no key configured", provider)
	}

	plaintext, err := crypto.DecryptAIProviderKey(apiKeyEncrypted)
	if err != nil {
		return "", "", fmt.Errorf("dashboard: decrypt ai provider key: %w", err)
	}

	return model, plaintext, nil
}

package dashboard

// ai_providers_store.go — CRUD for the single global AI provider row
// (zeep_system.ai_providers), one row per provider name ('openai' |
// 'gemini' | 'claude'). Mirrors auth_providers_store.go's merge-on-absent-
// key pattern, adapted for the {model, api_key} shape instead of a JSONB
// config blob. See .specs/features/ai-build-chat/design.md's Components
// section for the AIProviderResponse/aiProviderUpsertInput contract.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AIProviderResponse is the API-safe view of a provider row — never carries
// the API key in any form (AIBC-04). BaseURL is the OpenAI-compatible
// endpoint ("" for the stock OpenAI API); it is not a secret (validation
// rejects URLs carrying credentials).
type AIProviderResponse struct {
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Enabled   bool      `json:"enabled"`
	HasKey    bool      `json:"has_key"`
	BaseURL   string    `json:"base_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// aiProviderUpsertInput is the JSON body for PUT /ai-providers/{provider}.
// APIKey is intentionally omitempty-friendly: an absent/empty key means
// "keep the previously stored key, update model/enabled only" (AIBC-03).
//
// BaseURL is a pointer to tell "absent" from "empty": nil keeps the stored
// base_url (merge-on-absent-key, same as the key), a non-nil value replaces it,
// and an explicit "" clears it. The handler validates and normalizes it before
// it reaches the store.
type aiProviderUpsertInput struct {
	Model   string  `json:"model"`
	APIKey  string  `json:"api_key,omitempty"`
	Enabled bool    `json:"enabled"`
	BaseURL *string `json:"base_url,omitempty"`
}

// GetAIProvider returns the API-safe view of provider's config. If no row
// exists yet, returns a zero-value response (unconfigured), not an error.
func GetAIProvider(ctx context.Context, pool *db.Pool, provider string) (*AIProviderResponse, error) {
	var (
		model           string
		enabled         bool
		apiKeyEncrypted string
		baseURL         string
		updatedAt       time.Time
	)
	err := pool.QueryRow(ctx,
		`SELECT model, enabled, api_key_encrypted, base_url, updated_at
		 FROM zeep_system.ai_providers WHERE provider = $1`,
		provider,
	).Scan(&model, &enabled, &apiKeyEncrypted, &baseURL, &updatedAt)
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
		BaseURL:   baseURL,
		UpdatedAt: updatedAt,
	}, nil
}

// UpsertAIProvider creates or updates provider's row. When input.APIKey is
// non-empty, it is encrypted (crypto.EncryptAIProviderKey) and replaces any
// stored key. When input.APIKey is empty, the previously stored encrypted
// key (if any) is preserved untouched — merge-on-absent-key, same semantics
// as auth_providers_store.go's mergeProviderConfig (AIBC-03). input.BaseURL
// follows the same rule: nil keeps the stored value (the caller must already
// have validated a non-nil one with normalizeAIProviderBaseURL).
func UpsertAIProvider(ctx context.Context, pool *db.Pool, provider string, input *aiProviderUpsertInput) (*AIProviderResponse, error) {
	encrypted := ""
	if input.APIKey != "" {
		var err error
		encrypted, err = crypto.EncryptAIProviderKey(input.APIKey)
		if err != nil {
			return nil, fmt.Errorf("dashboard: encrypt ai provider key: %w", err)
		}
	}

	baseURLSet, baseURL := false, ""
	if input.BaseURL != nil {
		baseURLSet, baseURL = true, *input.BaseURL
	}

	var (
		model           string
		enabled         bool
		apiKeyEncrypted string
		storedBaseURL   string
		updatedAt       time.Time
	)
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.ai_providers (provider, model, api_key_encrypted, enabled, base_url, updated_at)
		 VALUES ($1, $2, $3, $4, $6, now())
		 ON CONFLICT (provider) DO UPDATE SET
		   model             = $2,
		   api_key_encrypted = CASE WHEN $3 = '' THEN ai_providers.api_key_encrypted ELSE $3 END,
		   enabled           = $4,
		   base_url          = CASE WHEN $5::boolean THEN $6 ELSE ai_providers.base_url END,
		   updated_at        = now()
		 RETURNING model, enabled, api_key_encrypted, base_url, updated_at`,
		provider, input.Model, encrypted, input.Enabled, baseURLSet, baseURL,
	).Scan(&model, &enabled, &apiKeyEncrypted, &storedBaseURL, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert ai provider: %w", err)
	}

	return &AIProviderResponse{
		Provider:  provider,
		Model:     model,
		Enabled:   enabled,
		HasKey:    apiKeyEncrypted != "",
		BaseURL:   storedBaseURL,
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

// AIProviderRuntime is everything needed to build an LLM client from a
// provider row: the plaintext key included. It is internal-only (never
// serialized, no JSON tags) and must not be logged.
//
// To build the OpenAI-compatible adapter from it (internal/agent/provider):
//
//	rt, err := dashboard.ResolveAIProviderRuntime(ctx, pool, name)
//	p, err := provider.NewOpenAI(provider.OpenAIConfig{
//		Name: rt.Provider, BaseURL: rt.BaseURL, APIKey: rt.APIKey, Model: rt.Model,
//	})
//
// BaseURL is "" for the stock "openai" provider (the adapter then defaults to
// api.openai.com); APIKey may be "" for "openai_compatible" (e.g. Ollama).
type AIProviderRuntime struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

// ErrAIProviderUnavailable is returned by ResolveAIProviderRuntime when the
// provider row is missing, disabled, lacks a model, or lacks a required
// key/base URL, and when the stored key cannot be decrypted. Callers treat it
// as "provider unconfigured".
var ErrAIProviderUnavailable = errors.New("dashboard: ai provider is not available")

// ResolveAIProviderRuntime loads provider's row for server-side use. Unlike
// resolveDecryptedAIProviderKey (the legacy openai-only accessor used by the
// build/edit chat), it returns the base URL and accepts a key-less
// "openai_compatible" row. Rules:
//   - the row must exist, be enabled and have a model;
//   - "openai" requires a key; "openai_compatible" requires a base URL and
//     the key is optional;
//   - any other provider name is rejected (gemini/claude are not implemented).
//
// The underlying cause (decrypt error, missing row) is wrapped under
// ErrAIProviderUnavailable for logging, never for display.
func ResolveAIProviderRuntime(ctx context.Context, pool *db.Pool, provider string) (*AIProviderRuntime, error) {
	if provider != aiProviderOpenAI && provider != aiProviderOpenAICompatible {
		return nil, fmt.Errorf("%w: unsupported provider %q", ErrAIProviderUnavailable, provider)
	}
	var (
		model, apiKeyEncrypted, baseURL string
		enabled                         bool
	)
	err := pool.QueryRow(ctx,
		`SELECT model, enabled, api_key_encrypted, base_url
		 FROM zeep_system.ai_providers WHERE provider = $1`,
		provider,
	).Scan(&model, &enabled, &apiKeyEncrypted, &baseURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q is not configured", ErrAIProviderUnavailable, provider)
		}
		return nil, fmt.Errorf("dashboard: resolve ai provider runtime: %w", err)
	}
	if !enabled || model == "" {
		return nil, fmt.Errorf("%w: %q is disabled or has no model", ErrAIProviderUnavailable, provider)
	}
	if provider == aiProviderOpenAI && apiKeyEncrypted == "" {
		return nil, fmt.Errorf("%w: %q has no key configured", ErrAIProviderUnavailable, provider)
	}
	if provider == aiProviderOpenAICompatible && baseURL == "" {
		return nil, fmt.Errorf("%w: %q has no base URL configured", ErrAIProviderUnavailable, provider)
	}

	rt := &AIProviderRuntime{Provider: provider, Model: model, BaseURL: baseURL}
	if apiKeyEncrypted != "" {
		rt.APIKey, err = crypto.DecryptAIProviderKey(apiKeyEncrypted)
		if err != nil {
			return nil, fmt.Errorf("%w: decrypt key: %v", ErrAIProviderUnavailable, err)
		}
	}
	return rt, nil
}

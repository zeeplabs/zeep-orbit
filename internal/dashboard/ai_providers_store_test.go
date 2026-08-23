package dashboard

// ai_providers_store_test.go — coverage for the global AI provider store:
// encrypt-on-create, merge-on-absent-key preservation, never leaking the
// key via GetAIProvider, and the decrypt-failure fallback path used by the
// chat call path. Derived from spec.md's P1 acceptance criteria
// (AIBC-01/03/04/05) and tasks.md's T3 Done-when list — not from reading
// the implementation.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func aiProvidersTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.ai_providers`); err != nil {
		t.Fatalf("truncate ai_providers: %v", err)
	}
	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret-for-ai-provider-store")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_providers`)
	})
	return pool
}

// AIBC-01: a key present on upsert is encrypted (never stored as plaintext)
// and persisted such that GetAIProvider reports has_key: true.
func TestUpsertAIProvider_EncryptsKeyOnCreate(t *testing.T) {
	pool := aiProvidersTestPool(t)
	ctx := context.Background()

	const realKey = "sk-real-openai-key-abc123"
	_, err := UpsertAIProvider(ctx, pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o",
		APIKey:  realKey,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertAIProvider: %v", err)
	}

	var storedEncrypted string
	if err := pool.QueryRow(ctx,
		`SELECT api_key_encrypted FROM zeep_system.ai_providers WHERE provider = 'openai'`,
	).Scan(&storedEncrypted); err != nil {
		t.Fatalf("query stored key: %v", err)
	}
	if storedEncrypted == realKey {
		t.Fatal("expected the stored api_key_encrypted to differ from the plaintext key")
	}
	decrypted, err := crypto.DecryptAIProviderKey(storedEncrypted)
	if err != nil {
		t.Fatalf("DecryptAIProviderKey on stored value: %v", err)
	}
	if decrypted != realKey {
		t.Fatalf("expected decrypted stored key to equal the original, got %q", decrypted)
	}

	resp, err := GetAIProvider(ctx, pool, "openai")
	if err != nil {
		t.Fatalf("GetAIProvider: %v", err)
	}
	if !resp.HasKey {
		t.Error("expected has_key: true after a key-bearing upsert")
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("expected model %q, got %q", "gpt-4o", resp.Model)
	}
	if !resp.Enabled {
		t.Error("expected enabled: true")
	}
}

// AIBC-03: a model-only update (no api_key field) preserves the previously
// stored encrypted key rather than clearing it.
func TestUpsertAIProvider_ModelOnlyUpdatePreservesKey(t *testing.T) {
	pool := aiProvidersTestPool(t)
	ctx := context.Background()

	const realKey = "sk-preserve-me-456"
	if _, err := UpsertAIProvider(ctx, pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o",
		APIKey:  realKey,
		Enabled: true,
	}); err != nil {
		t.Fatalf("initial UpsertAIProvider: %v", err)
	}

	resp, err := UpsertAIProvider(ctx, pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o-mini",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("model-only UpsertAIProvider: %v", err)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("expected model updated to %q, got %q", "gpt-4o-mini", resp.Model)
	}
	if !resp.HasKey {
		t.Fatal("expected has_key: true — the previously stored key must be preserved on a model-only update")
	}

	// Prove the actual key material, not just the has_key flag, survived.
	model, key, err := resolveDecryptedAIProviderKey(ctx, pool, "openai")
	if err != nil {
		t.Fatalf("resolveDecryptedAIProviderKey: %v", err)
	}
	if key != realKey {
		t.Fatalf("expected preserved key %q, got %q", realKey, key)
	}
	if model != "gpt-4o-mini" {
		t.Errorf("expected resolved model %q, got %q", "gpt-4o-mini", model)
	}
}

// AIBC-04: GetAIProvider never returns the key in any form, cleartext or
// otherwise — only {has_key, model, enabled} fields exist on the response
// type, and this test proves no key-shaped value leaks into any of them.
func TestGetAIProvider_NeverLeaksKey(t *testing.T) {
	pool := aiProvidersTestPool(t)
	ctx := context.Background()

	const realKey = "sk-must-never-leak-789"
	if _, err := UpsertAIProvider(ctx, pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o",
		APIKey:  realKey,
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertAIProvider: %v", err)
	}

	resp, err := GetAIProvider(ctx, pool, "openai")
	if err != nil {
		t.Fatalf("GetAIProvider: %v", err)
	}
	if resp.Model == realKey || resp.Provider == realKey {
		t.Fatal("expected no field to carry the raw key value")
	}
	// AIProviderResponse's fields are fixed to Provider/Model/Enabled/
	// HasKey/UpdatedAt — there is no field to hold a key, so a successful
	// round-trip through the exported struct with HasKey=true and no other
	// field matching realKey is the strongest assertion available here.
	if !resp.HasKey {
		t.Error("expected has_key: true")
	}
}

// T3 Done-when: resolveDecryptedAIProviderKey returns a treatable error
// (not a panic) when the encryption key was rotated (or the stored
// ciphertext is otherwise undecryptable under the current key) — this is
// the fallback path spec.md's edge cases require: treat as "provider
// unconfigured" for chat purposes.
func TestResolveDecryptedAIProviderKey_DecryptFailureReturnsError(t *testing.T) {
	pool := aiProvidersTestPool(t)
	ctx := context.Background()

	if _, err := UpsertAIProvider(ctx, pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o",
		APIKey:  "sk-encrypted-under-old-key",
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertAIProvider: %v", err)
	}

	// Simulate a rotated encryption key: AI_PROVIDER_ENCRYPTION_KEY now
	// resolves to a different value than DASHBOARD_BOOTSTRAP_SECRET did at
	// encrypt time, so the stored ciphertext can no longer be decrypted.
	os.Setenv("AI_PROVIDER_ENCRYPTION_KEY", "a-completely-different-rotated-key")
	defer os.Unsetenv("AI_PROVIDER_ENCRYPTION_KEY")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("resolveDecryptedAIProviderKey panicked instead of returning an error: %v", r)
		}
	}()
	_, _, err := resolveDecryptedAIProviderKey(ctx, pool, "openai")
	if err == nil {
		t.Fatal("expected an error when the stored ciphertext can't be decrypted under the current key")
	}
}

// P1-AC6 (store-level half): gemini/claude have no functional persistence
// path yet, so GetAIProvider on either reports the same "unconfigured"
// shape (has_key: false, enabled: false) as any provider with no row —
// distinct from openai once configured.
func TestGetAIProvider_GeminiClaudeReportUnconfigured(t *testing.T) {
	pool := aiProvidersTestPool(t)
	ctx := context.Background()

	for _, provider := range []string{"gemini", "claude"} {
		resp, err := GetAIProvider(ctx, pool, provider)
		if err != nil {
			t.Fatalf("GetAIProvider(%q): %v", provider, err)
		}
		if resp.HasKey {
			t.Errorf("expected %s has_key: false, got true", provider)
		}
		if resp.Enabled {
			t.Errorf("expected %s enabled: false, got true", provider)
		}
	}
}

// Round-trip through the real Postgres pool (not a mock) end to end:
// upsert, then get, then resolve — covering the full store surface against
// an actual database connection, per this repo's integration-test
// convention (no DB mocking anywhere in the codebase).
func TestAIProviderStore_RoundTripThroughRealPool(t *testing.T) {
	pool := aiProvidersTestPool(t)
	ctx := context.Background()

	const realKey = "sk-full-round-trip-key"
	upserted, err := UpsertAIProvider(ctx, pool, "openai", &aiProviderUpsertInput{
		Model:   "gpt-4o",
		APIKey:  realKey,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertAIProvider: %v", err)
	}
	if upserted.Provider != "openai" {
		t.Errorf("expected provider %q, got %q", "openai", upserted.Provider)
	}

	got, err := GetAIProvider(ctx, pool, "openai")
	if err != nil {
		t.Fatalf("GetAIProvider: %v", err)
	}
	if got.Model != "gpt-4o" || !got.Enabled || !got.HasKey {
		t.Fatalf("expected {model: gpt-4o, enabled: true, has_key: true}, got %+v", got)
	}

	model, key, err := resolveDecryptedAIProviderKey(ctx, pool, "openai")
	if err != nil {
		t.Fatalf("resolveDecryptedAIProviderKey: %v", err)
	}
	if model != "gpt-4o" || key != realKey {
		t.Fatalf("expected {gpt-4o, %s}, got {%s, %s}", realKey, model, key)
	}
}

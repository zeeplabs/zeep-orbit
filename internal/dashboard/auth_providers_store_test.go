package dashboard

// auth_providers_store_test.go — coverage for the system-wide (superadmin,
// dashboard-login) auth providers store: mergeProviderConfig's merge-on-
// absent-key semantics and nil-map safety, and stripSecretFromConfig's
// allow-list redaction. This table is a separate subsystem from the
// per-app auth_providers JSONB column (apps_store.go covers that one) —
// see apps_store.go's redactAuthProviderSecrets/mergeAppAuthProviders for
// the per-app twin these mirror.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func authProvidersTestPool(t *testing.T) *db.Pool {
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
	t.Cleanup(pool.Close)
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.auth_providers`); err != nil {
		t.Fatalf("truncate auth_providers: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.auth_providers`)
	})
	return pool
}

// TestMergeProviderConfig_NonObjectInputDoesNotPanic covers a nil-map
// write-panic risk: a valid-but-non-object input (or a literal JSON null)
// unmarshals to a nil inputMap, and writing into a nil map panics.
func TestMergeProviderConfig_NonObjectInputDoesNotPanic(t *testing.T) {
	existing := &AuthProviderResponse{
		Config: json.RawMessage(`{"client_id":"existing-id","client_secret":"existing-secret"}`),
	}

	for _, input := range []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`123`),
		json.RawMessage(`"just a string"`),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("mergeProviderConfig(%s) panicked: %v", input, r)
				}
			}()
			result := mergeProviderConfig("google", input, existing)
			var decoded map[string]any
			if err := json.Unmarshal(result, &decoded); err != nil {
				t.Fatalf("mergeProviderConfig(%s) produced invalid JSON: %s", input, result)
			}
			// Existing fields should still come through the merge even
			// when the incoming side contributed nothing usable.
			if decoded["client_id"] != "existing-id" {
				t.Errorf("mergeProviderConfig(%s) = %s, expected existing client_id preserved", input, result)
			}
		}()
	}
}

// TestStripSecretFromConfig_AllowListDropsUnknownFields covers the
// allow-list behavior: only known display fields survive, client_secret
// becomes client_secret_set, and a field name outside the allow-list
// (simulating a secret stored under an unexpected key) is dropped too.
func TestStripSecretFromConfig_AllowListDropsUnknownFields(t *testing.T) {
	in := json.RawMessage(`{"enabled":true,"client_id":"abc","client_secret":"super-secret","redirect_url":"https://example.com/cb","allowed_domains":["example.com"],"some_future_credential_field":"also-secret"}`)
	out := stripSecretFromConfig("google", in)

	if strings.Contains(string(out), "super-secret") || strings.Contains(string(out), "also-secret") {
		t.Fatalf("expected all secret-shaped values stripped, got %s", out)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["client_id"] != "abc" {
		t.Errorf("expected client_id to survive, got %+v", decoded)
	}
	if _, hasUnknown := decoded["some_future_credential_field"]; hasUnknown {
		t.Errorf("expected unknown field dropped by the allow-list, got %+v", decoded)
	}
	if set, _ := decoded["client_secret_set"].(bool); !set {
		t.Errorf("expected client_secret_set=true, got %+v", decoded)
	}

	// Fails closed (not open) on malformed input.
	if got := string(stripSecretFromConfig("google", json.RawMessage(`not json`))); got != "{}" {
		t.Errorf("expected malformed input to fail closed to {}, got %s", got)
	}
}

// TestUpsertAuthProviderHandler_NeverEchoesRealSecret is the boundary-
// level regression test: UpsertAuthProvider (the PUT-config store
// function backing the HTTP handler) previously returned the full
// decrypted config — including client_secret in plaintext — with no
// reveal gate, unlike GetAuthProvider. Confirmed against the real
// encrypt/decrypt round trip, not a mock.
func TestUpsertAuthProviderHandler_NeverEchoesRealSecret(t *testing.T) {
	pool := authProvidersTestPool(t)
	ctx := context.Background()

	const realSecret = "the-real-system-wide-client-secret"
	resp, err := UpsertAuthProvider(ctx, pool, "google", &authProviderUpsertInput{
		Enabled: true,
		Config:  json.RawMessage(`{"client_id":"real-client-id","client_secret":"` + realSecret + `","redirect_url":"https://example.com/cb"}`),
	})
	if err != nil {
		t.Fatalf("UpsertAuthProvider: %v", err)
	}
	if !strings.Contains(string(resp.Config), realSecret) {
		t.Fatal("sanity check failed: UpsertAuthProvider's raw result should still carry the real secret before the handler strips it")
	}

	stripped := stripSecretFromConfig("google", resp.Config)
	if strings.Contains(string(stripped), realSecret) {
		t.Fatalf("expected the handler-applied stripSecretFromConfig to remove the real secret, got %s", stripped)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stripped, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["client_id"] != "real-client-id" {
		t.Errorf("expected client_id to survive, got %+v", decoded)
	}
}

package dashboard

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func githubTestPool(t *testing.T) *db.Pool {
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

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS zeep_system`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zeep_system.github_app_config (
			app_id           TEXT NOT NULL,
			client_id        TEXT NOT NULL,
			client_secret    TEXT NOT NULL,
			private_key      TEXT NOT NULL,
			webhook_secret   TEXT NOT NULL,
			org_login        TEXT NOT NULL DEFAULT '',
			installation_id  BIGINT,
			installed_at     TIMESTAMPTZ,
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_github_app_config_singleton
		ON zeep_system.github_app_config ((TRUE))`); err != nil {
		t.Fatalf("create singleton index: %v", err)
	}

	// Ensure a clean slate for each test — this table is a global singleton.
	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.github_app_config`); err != nil {
		t.Fatalf("truncate table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.github_app_config`)
	})

	return pool
}

func TestUpsertGitHubConfigInsertsFreshConfig(t *testing.T) {
	pool := githubTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	input := GitHubAppConfigInput{
		AppID:         "12345",
		ClientID:      "Iv1.abc123",
		ClientSecret:  "super-secret",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nMIIB...\n-----END RSA PRIVATE KEY-----",
		WebhookSecret: "webhook-secret",
	}

	if err := UpsertGitHubConfig(ctx, pool, input); err != nil {
		t.Fatalf("UpsertGitHubConfig: %v", err)
	}

	cfg, err := GetGitHubConfig(ctx, pool)
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.AppID != input.AppID {
		t.Errorf("AppID = %q, want %q", cfg.AppID, input.AppID)
	}
	if cfg.ClientID != input.ClientID {
		t.Errorf("ClientID = %q, want %q", cfg.ClientID, input.ClientID)
	}
	if cfg.ClientSecret != input.ClientSecret {
		t.Errorf("ClientSecret = %q, want %q", cfg.ClientSecret, input.ClientSecret)
	}
	if cfg.PrivateKey != input.PrivateKey {
		t.Errorf("PrivateKey = %q, want %q", cfg.PrivateKey, input.PrivateKey)
	}
	if cfg.WebhookSecret != input.WebhookSecret {
		t.Errorf("WebhookSecret = %q, want %q", cfg.WebhookSecret, input.WebhookSecret)
	}

	// Confirm the DB actually stores ciphertext, not cleartext.
	var rawSecret, rawKey, rawWebhook string
	err = pool.QueryRow(ctx,
		`SELECT client_secret, private_key, webhook_secret FROM zeep_system.github_app_config LIMIT 1`,
	).Scan(&rawSecret, &rawKey, &rawWebhook)
	if err != nil {
		t.Fatalf("scan raw row: %v", err)
	}
	if rawSecret == input.ClientSecret {
		t.Error("client_secret stored in cleartext, expected encrypted")
	}
	if rawKey == input.PrivateKey {
		t.Error("private_key stored in cleartext, expected encrypted")
	}
	if rawWebhook == input.WebhookSecret {
		t.Error("webhook_secret stored in cleartext, expected encrypted")
	}
}

func TestUpsertGitHubConfigPartialUpdatePreservesPrivateKey(t *testing.T) {
	pool := githubTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	original := GitHubAppConfigInput{
		AppID:         "12345",
		ClientID:      "Iv1.abc123",
		ClientSecret:  "original-secret",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nORIGINAL\n-----END RSA PRIVATE KEY-----",
		WebhookSecret: "original-webhook",
	}
	if err := UpsertGitHubConfig(ctx, pool, original); err != nil {
		t.Fatalf("initial UpsertGitHubConfig: %v", err)
	}

	partial := GitHubAppConfigInput{
		AppID:    "12345",
		ClientID: "Iv1.updated456",
		// ClientSecret, PrivateKey, WebhookSecret all empty -> keep existing
	}
	if err := UpsertGitHubConfig(ctx, pool, partial); err != nil {
		t.Fatalf("partial UpsertGitHubConfig: %v", err)
	}

	cfg, err := GetGitHubConfig(ctx, pool)
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.ClientID != partial.ClientID {
		t.Errorf("ClientID = %q, want updated %q", cfg.ClientID, partial.ClientID)
	}
	if cfg.PrivateKey != original.PrivateKey {
		t.Errorf("PrivateKey = %q, want preserved %q", cfg.PrivateKey, original.PrivateKey)
	}
	if cfg.ClientSecret != original.ClientSecret {
		t.Errorf("ClientSecret = %q, want preserved %q", cfg.ClientSecret, original.ClientSecret)
	}
	if cfg.WebhookSecret != original.WebhookSecret {
		t.Errorf("WebhookSecret = %q, want preserved %q", cfg.WebhookSecret, original.WebhookSecret)
	}
}

// TestUpsertGitHubConfigPartialUpdatePreservesExactCiphertext covers the fix
// for the Critical review finding: preservation of unchanged secrets must be
// enforced by the SQL CASE WHEN guard, not a Go-side pre-read that could
// silently wipe secrets to '' on a transient error. This test asserts the
// raw stored ciphertext is byte-identical across the partial update (proof
// the column was left untouched by the database, not merely that it decrypts
// to the same cleartext by coincidence).
func TestUpsertGitHubConfigPartialUpdatePreservesExactCiphertext(t *testing.T) {
	pool := githubTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	original := GitHubAppConfigInput{
		AppID:         "12345",
		ClientID:      "Iv1.abc123",
		ClientSecret:  "original-secret",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nORIGINAL\n-----END RSA PRIVATE KEY-----",
		WebhookSecret: "original-webhook",
	}
	if err := UpsertGitHubConfig(ctx, pool, original); err != nil {
		t.Fatalf("initial UpsertGitHubConfig: %v", err)
	}

	var rawSecretBefore, rawKeyBefore, rawWebhookBefore string
	if err := pool.QueryRow(ctx,
		`SELECT client_secret, private_key, webhook_secret FROM zeep_system.github_app_config LIMIT 1`,
	).Scan(&rawSecretBefore, &rawKeyBefore, &rawWebhookBefore); err != nil {
		t.Fatalf("scan raw row before update: %v", err)
	}

	// Two consecutive partial updates (only ClientID changes each time) to
	// confirm the SQL guard holds up across repeated calls, with no Go-side
	// pre-read involved at all.
	for i := 0; i < 2; i++ {
		partial := GitHubAppConfigInput{
			AppID:    "12345",
			ClientID: fmt.Sprintf("Iv1.updated-%d", i),
		}
		if err := UpsertGitHubConfig(ctx, pool, partial); err != nil {
			t.Fatalf("partial UpsertGitHubConfig #%d: %v", i, err)
		}
	}

	var rawSecretAfter, rawKeyAfter, rawWebhookAfter string
	if err := pool.QueryRow(ctx,
		`SELECT client_secret, private_key, webhook_secret FROM zeep_system.github_app_config LIMIT 1`,
	).Scan(&rawSecretAfter, &rawKeyAfter, &rawWebhookAfter); err != nil {
		t.Fatalf("scan raw row after update: %v", err)
	}

	if rawSecretAfter != rawSecretBefore {
		t.Errorf("client_secret ciphertext changed on partial update: before=%q after=%q", rawSecretBefore, rawSecretAfter)
	}
	if rawKeyAfter != rawKeyBefore {
		t.Errorf("private_key ciphertext changed on partial update: before=%q after=%q", rawKeyBefore, rawKeyAfter)
	}
	if rawWebhookAfter != rawWebhookBefore {
		t.Errorf("webhook_secret ciphertext changed on partial update: before=%q after=%q", rawWebhookBefore, rawWebhookAfter)
	}
}

// TestGetGitHubConfigPropagatesRealErrors covers the Important review finding:
// a genuine query error (as opposed to "no rows yet") must be surfaced to the
// caller, not folded into the same (nil, nil) "not configured" signal.
func TestGetGitHubConfigPropagatesRealErrors(t *testing.T) {
	pool := githubTestPool(t)
	ctx := context.Background()

	// Force a real query error distinct from "no rows": close the pool so
	// any subsequent query fails at the connection level.
	pool.Close()

	cfg, err := GetGitHubConfig(ctx, pool)
	if err == nil {
		t.Fatal("expected error from closed pool, got nil")
	}
	if cfg != nil {
		t.Fatalf("expected nil config alongside error, got %+v", cfg)
	}
}

func TestGetGitHubConfigReturnsNilWhenNotConfigured(t *testing.T) {
	pool := githubTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	cfg, err := GetGitHubConfig(ctx, pool)
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when unconfigured, got %+v", cfg)
	}
}

func TestGitHubConfigDecryptRoundTrip(t *testing.T) {
	pool := githubTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	input := GitHubAppConfigInput{
		AppID:         "99999",
		ClientID:      "Iv1.roundtrip",
		ClientSecret:  "round-trip-secret-ção",
		PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nROUNDTRIP\n-----END RSA PRIVATE KEY-----",
		WebhookSecret: "round-trip-webhook",
	}
	if err := UpsertGitHubConfig(ctx, pool, input); err != nil {
		t.Fatalf("UpsertGitHubConfig: %v", err)
	}

	cfg, err := GetGitHubConfig(ctx, pool)
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if cfg.ClientSecret != input.ClientSecret {
		t.Errorf("round-trip ClientSecret = %q, want %q", cfg.ClientSecret, input.ClientSecret)
	}
	if cfg.PrivateKey != input.PrivateKey {
		t.Errorf("round-trip PrivateKey = %q, want %q", cfg.PrivateKey, input.PrivateKey)
	}
	if cfg.WebhookSecret != input.WebhookSecret {
		t.Errorf("round-trip WebhookSecret = %q, want %q", cfg.WebhookSecret, input.WebhookSecret)
	}
}

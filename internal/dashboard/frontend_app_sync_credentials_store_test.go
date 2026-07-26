package dashboard

import (
	"context"
	"os"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func syncCredentialsTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	for _, stmt := range []string{
		`CREATE SCHEMA IF NOT EXISTS zeep_system`,
		`CREATE TABLE IF NOT EXISTS zeep_system.dashboard_users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL CHECK (role IN ('admin','superadmin')),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.github_templates (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			github_owner TEXT NOT NULL,
			github_repo TEXT NOT NULL,
			framework TEXT NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT true,
			created_by TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.frontend_apps (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			slug TEXT NOT NULL,
			template_id UUID NOT NULL,
			github_repo_url TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'ready',
			error_message TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			archived_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.frontend_app_sync_credentials (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			frontend_app_id UUID NOT NULL UNIQUE,
			github_key_id BIGINT,
			public_key TEXT NOT NULL DEFAULT '',
			private_key_encrypted TEXT NOT NULL DEFAULT '',
			sync_status TEXT NOT NULL DEFAULT 'pending',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.frontend_app_sync_credentials, zeep_system.frontend_apps, zeep_system.github_templates, zeep_system.dashboard_users`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return pool
}

func testFrontendApp(t *testing.T, pool *db.Pool, ctx context.Context, slug string) string {
	t.Helper()
	// Create user and template first.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.dashboard_users (id, email, password_hash, role)
		 VALUES ('b0000000-0000-0000-0000-000000000001', 'test@test.com', '', 'admin')`); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var tmplID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ('Test', '', 'zeeplabs', 'test-repo', 'React', 'test@test.com')
		 RETURNING id`,
	).Scan(&tmplID); err != nil {
		t.Fatalf("create template: %v", err)
	}

	var appID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (name, slug, template_id, status, created_by)
		 VALUES ($1, $2, $3, 'ready', 'test@test.com')
		 RETURNING id`, "Test App", slug, tmplID,
	).Scan(&appID); err != nil {
		t.Fatalf("create app: %v", err)
	}
	return appID
}

func TestCreateSyncCredential(t *testing.T) {
	pool := syncCredentialsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	appID := testFrontendApp(t, pool, ctx, "sync-test")

	if err := CreateSyncCredential(ctx, pool, appID); err != nil {
		t.Fatalf("CreateSyncCredential: %v", err)
	}

	sc, err := GetSyncCredential(ctx, pool, appID)
	if err != nil {
		t.Fatalf("GetSyncCredential: %v", err)
	}
	if sc.SyncStatus != "pending" {
		t.Errorf("SyncStatus = %q, want pending", sc.SyncStatus)
	}
}

func TestCreateSyncCredentialDuplicateFails(t *testing.T) {
	pool := syncCredentialsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	appID := testFrontendApp(t, pool, ctx, "sync-dup")

	if err := CreateSyncCredential(ctx, pool, appID); err != nil {
		t.Fatalf("CreateSyncCredential 1: %v", err)
	}

	if err := CreateSyncCredential(ctx, pool, appID); err == nil {
		t.Error("expected duplicate key error, got nil")
	}
}

func TestUpdateSyncCredentialSuccess(t *testing.T) {
	pool := syncCredentialsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	appID := testFrontendApp(t, pool, ctx, "sync-success")

	if err := CreateSyncCredential(ctx, pool, appID); err != nil {
		t.Fatalf("CreateSyncCredential: %v", err)
	}

	if err := UpdateSyncCredentialSuccess(ctx, pool, appID, 12345, "ssh-ed25519 AAAAtestkey", "encrypted-private-key"); err != nil {
		t.Fatalf("UpdateSyncCredentialSuccess: %v", err)
	}

	sc, err := GetSyncCredential(ctx, pool, appID)
	if err != nil {
		t.Fatalf("GetSyncCredential: %v", err)
	}

	if sc.SyncStatus != "ready" {
		t.Errorf("SyncStatus = %q, want ready", sc.SyncStatus)
	}
	if sc.PublicKey != "ssh-ed25519 AAAAtestkey" {
		t.Errorf("PublicKey = %q, want ssh-ed25519 AAAAtestkey", sc.PublicKey)
	}
	if sc.PrivateKeyEncrypted != "encrypted-private-key" {
		t.Errorf("PrivateKeyEncrypted mismatch")
	}
	if *sc.GithubKeyID != 12345 {
		t.Errorf("GithubKeyID = %d, want 12345", *sc.GithubKeyID)
	}
}

func TestUpdateSyncCredentialFailure(t *testing.T) {
	pool := syncCredentialsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	appID := testFrontendApp(t, pool, ctx, "sync-fail")

	if err := CreateSyncCredential(ctx, pool, appID); err != nil {
		t.Fatalf("CreateSyncCredential: %v", err)
	}

	if err := UpdateSyncCredentialFailure(ctx, pool, appID, "rate limit exceeded"); err != nil {
		t.Fatalf("UpdateSyncCredentialFailure: %v", err)
	}

	sc, err := GetSyncCredential(ctx, pool, appID)
	if err != nil {
		t.Fatalf("GetSyncCredential: %v", err)
	}

	if sc.SyncStatus != "failed" {
		t.Errorf("SyncStatus = %q, want failed", sc.SyncStatus)
	}
	if sc.ErrorMessage != "rate limit exceeded" {
		t.Errorf("ErrorMessage = %q, want rate limit exceeded", sc.ErrorMessage)
	}
}

func TestGetSyncCredentialNotFound(t *testing.T) {
	pool := syncCredentialsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := GetSyncCredential(ctx, pool, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

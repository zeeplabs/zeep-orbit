package dashboard

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ProvisionZeepSystem creates zeep_system schema and all required tables.
// Idempotent — safe to call on every startup.
func ProvisionZeepSystem(ctx context.Context, pool *db.Pool) error {
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE SCHEMA IF NOT EXISTS zeep_system`,
		`CREATE TABLE IF NOT EXISTS zeep_system.dashboard_users (
			id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			email        TEXT        UNIQUE NOT NULL,
			password_hash TEXT       NOT NULL,
			role         TEXT        NOT NULL CHECK (role IN ('admin','superadmin')),
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.sessions (
			token      TEXT        PRIMARY KEY,
			user_id    UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.apps (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			name               TEXT        UNIQUE NOT NULL,
			owner_id           UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id),
			jwt_secret         TEXT        NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
			auth_email_enabled BOOLEAN     NOT NULL DEFAULT true,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.app_tables (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			app_id     UUID        NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			name       TEXT        NOT NULL,
			rls        TEXT        NOT NULL DEFAULT '',
			columns    JSONB       NOT NULL DEFAULT '[]',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(app_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.app_ownership (
			user_id UUID NOT NULL REFERENCES zeep_system.dashboard_users(id) ON DELETE CASCADE,
			app_id  UUID NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, app_id)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("dashboard: provision: %w", err)
		}
	}
	return nil
}

package dashboard

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Idempotent — safe to call on every startup.
// Uses pg_advisory_xact_lock to serialize provisioning across concurrent pods.
func ProvisionZeepSystem(ctx context.Context, pool *db.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dashboard: provision begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('zeep-orbit-provision'))`); err != nil {
		return fmt.Errorf("dashboard: provision acquire lock: %w", err)
	}

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE SCHEMA IF NOT EXISTS zeep_system`,
		`CREATE TABLE IF NOT EXISTS zeep_system.dashboard_users (
			id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			email        TEXT        UNIQUE NOT NULL,
			password_hash TEXT       NOT NULL DEFAULT '',
			google_id    TEXT        UNIQUE,
			role         TEXT        NOT NULL CHECK (role IN ('admin','superadmin')),
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE zeep_system.dashboard_users ADD COLUMN IF NOT EXISTS google_id TEXT`,
		`ALTER TABLE zeep_system.dashboard_users ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE zeep_system.dashboard_users ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'en'`,
		`CREATE INDEX IF NOT EXISTS idx_dashboard_users_google_id
		 ON zeep_system.dashboard_users(google_id)`,
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
		`ALTER TABLE zeep_system.apps ADD COLUMN IF NOT EXISTS auth_providers JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE zeep_system.apps ADD COLUMN IF NOT EXISTS storage_config JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE zeep_system.apps ADD COLUMN IF NOT EXISTS rate_limit_config JSONB NOT NULL DEFAULT '{}'`,
		`CREATE TABLE IF NOT EXISTS zeep_system.app_tables (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			app_id     UUID        NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			name       TEXT        NOT NULL,
			rls        TEXT        NOT NULL DEFAULT '',
			columns    JSONB       NOT NULL DEFAULT '[]',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(app_id, name)
		)`,
		`ALTER TABLE zeep_system.app_tables ADD COLUMN IF NOT EXISTS indexes JSONB NOT NULL DEFAULT '[]'`,
		`CREATE TABLE IF NOT EXISTS zeep_system.app_ownership (
			user_id UUID NOT NULL REFERENCES zeep_system.dashboard_users(id) ON DELETE CASCADE,
			app_id  UUID NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, app_id)
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.brand_config (
			id           SERIAL      PRIMARY KEY,
			theme        TEXT        NOT NULL DEFAULT 'azure',
			company_name TEXT        NOT NULL DEFAULT 'Zeep Tecnologia',
			logo_url     TEXT        NOT NULL DEFAULT '',
			icon_url     TEXT        NOT NULL DEFAULT '',
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE zeep_system.brand_config ADD COLUMN IF NOT EXISTS icon_url TEXT NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_brand_config_singleton
		 ON zeep_system.brand_config ((TRUE))`,
		`CREATE TABLE IF NOT EXISTS zeep_system.auth_providers (
			provider         TEXT        PRIMARY KEY,
			enabled          BOOLEAN    NOT NULL DEFAULT false,
			config_encrypted TEXT       NOT NULL DEFAULT '',
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.audit_log (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id        UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id),
			user_email     TEXT        NOT NULL,
			action         TEXT        NOT NULL,
			resource_type  TEXT        NOT NULL,
			resource_id    TEXT,
			resource_name  TEXT,
			metadata       JSONB       NOT NULL DEFAULT '{}',
			ip_address     TEXT,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON zeep_system.audit_log(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_action ON zeep_system.audit_log(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_user_id ON zeep_system.audit_log(user_id)`,
		// user_id is nullable so events with no authenticated actor (e.g. the
		// GitHub App installation callback, which GitHub redirects the
		// browser to directly with no session cookie guaranteed) can still
		// be audit-logged, instead of the insert silently failing on an
		// empty-string UUID (see InsertAuditLog).
		`ALTER TABLE zeep_system.audit_log ALTER COLUMN user_id DROP NOT NULL`,
		`CREATE TABLE IF NOT EXISTS zeep_system.app_tokens (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			app_id       UUID NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			jti          TEXT NOT NULL UNIQUE,
			expires_at   TIMESTAMPTZ,
			revoked_at   TIMESTAMPTZ,
			last_used_at TIMESTAMPTZ,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_app_tokens_app_id ON zeep_system.app_tokens(app_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_tokens_jti ON zeep_system.app_tokens(jti)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.system_config (
			soft_delete_enabled BOOLEAN   NOT NULL DEFAULT false,
			storage_config      JSONB     NOT NULL DEFAULT '{}',
			updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS storage_config JSONB NOT NULL DEFAULT '{}'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_system_config_singleton
		 ON zeep_system.system_config ((TRUE))`,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled)
		 SELECT false WHERE NOT EXISTS (SELECT 1 FROM zeep_system.system_config)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.github_app_config (
			app_id           TEXT NOT NULL,
			client_id        TEXT NOT NULL,
			client_secret    TEXT NOT NULL,
			private_key      TEXT NOT NULL,
			webhook_secret   TEXT NOT NULL,
			org_login        TEXT NOT NULL DEFAULT '',
			installation_id  BIGINT,
			installed_at     TIMESTAMPTZ,
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE zeep_system.github_app_config ADD COLUMN IF NOT EXISTS app_slug TEXT NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_github_app_config_singleton
		 ON zeep_system.github_app_config ((TRUE))`,
		`CREATE TABLE IF NOT EXISTS zeep_system.github_templates (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name         TEXT NOT NULL,
			description  TEXT NOT NULL DEFAULT '',
			github_owner TEXT NOT NULL,
			github_repo  TEXT NOT NULL,
			framework    TEXT NOT NULL DEFAULT '',
			active       BOOLEAN NOT NULL DEFAULT true,
			created_by   TEXT NOT NULL,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.frontend_apps (
			id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name            TEXT NOT NULL,
			slug            TEXT NOT NULL,
			template_id     UUID NOT NULL REFERENCES zeep_system.github_templates(id),
			github_repo_url TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'ready',
			error_message   TEXT NOT NULL DEFAULT '',
			created_by      TEXT NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			archived_at     TIMESTAMPTZ
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_frontend_apps_slug
		 ON zeep_system.frontend_apps (slug) WHERE archived_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS zeep_system.frontend_app_sync_credentials (
			id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			frontend_app_id        UUID NOT NULL UNIQUE REFERENCES zeep_system.frontend_apps(id),
			github_key_id          BIGINT,
			public_key             TEXT NOT NULL DEFAULT '',
			private_key_encrypted  TEXT NOT NULL DEFAULT '',
			sync_status            TEXT NOT NULL DEFAULT 'pending',
			error_message          TEXT NOT NULL DEFAULT '',
			created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.deploy_provider_config (
			provider           TEXT NOT NULL DEFAULT 'render',
			api_key            TEXT NOT NULL,
			render_project_id  TEXT NOT NULL DEFAULT '',
			connected_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_deploy_provider_config_singleton
		 ON zeep_system.deploy_provider_config ((TRUE))`,
		`ALTER TABLE zeep_system.deploy_provider_config
		 ADD COLUMN IF NOT EXISTS render_project_id TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS base_domain       TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE zeep_system.github_templates
		 ADD COLUMN IF NOT EXISTS render_service_type TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS build_command       TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS publish_path        TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS start_command       TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE zeep_system.frontend_apps
		 ADD COLUMN IF NOT EXISTS backend_app_id        UUID REFERENCES zeep_system.apps(id),
		 ADD COLUMN IF NOT EXISTS deploy_service_id     TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS deploy_url            TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS deploy_status         TEXT NOT NULL DEFAULT 'pending',
		 ADD COLUMN IF NOT EXISTS deploy_error_message  TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS custom_domain         TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS owner_id              UUID REFERENCES zeep_system.dashboard_users(id)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.changelog_entries (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			version      TEXT NOT NULL,
			release_date DATE NOT NULL,
			title        TEXT NOT NULL DEFAULT '',
			summary      TEXT NOT NULL DEFAULT '',
			sections     JSONB NOT NULL DEFAULT '[]',
			published    BOOLEAN NOT NULL DEFAULT true,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("dashboard: provision: %w", err)
		}
	}
	return tx.Commit(ctx)
}

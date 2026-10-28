package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// provisionLockTimeout bounds how long each boot-provisioning ALTER waits
// for a table lock. Without it, a long-lived reader on a zeep_system table
// makes the ALTER wait forever, and every later query on that table queues
// behind it. Vars so tests can shorten them.
var (
	provisionLockTimeout = 5 * time.Second
	provisionBackoff     = []time.Duration{1 * time.Second, 2 * time.Second}
)

// Idempotent — safe to call on every startup.
// Uses pg_advisory_xact_lock to serialize provisioning across concurrent pods.
// A lock wait longer than provisionLockTimeout rolls the whole transaction
// back and retries, up to len(provisionBackoff)+1 attempts (~18s with the
// defaults, under the liveness probe budget), then returns the lock-timeout
// error so boot fails visibly instead of hanging.
func ProvisionZeepSystem(ctx context.Context, pool *db.Pool) error {
	attempts := len(provisionBackoff) + 1
	var err error
	for i := 0; i < attempts; i++ {
		if err = provisionZeepSystemOnce(ctx, pool); err == nil || !db.IsLockTimeout(err) {
			return err
		}
		if i < len(provisionBackoff) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(provisionBackoff[i]):
			}
		}
	}
	return fmt.Errorf("dashboard: provision gave up after %d attempts waiting for a table lock: %w", attempts, err)
}

func provisionZeepSystemOnce(ctx context.Context, pool *db.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dashboard: provision begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('zeep-orbit-provision'))`); err != nil {
		return fmt.Errorf("dashboard: provision acquire lock: %w", err)
	}
	// Set after the advisory lock: waiting for another pod's provisioning is
	// expected, and that pod's own ALTERs are bounded by this same limit.
	// provisionLockTimeout is a trusted package var — safe to format.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", provisionLockTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("dashboard: provision set lock_timeout: %w", err)
	}

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		// Dedicated Postgres role for end-user requests (no ownership, no
		// BYPASSRLS, cannot log in directly —
		// the server reaches it only via SET LOCAL ROLE inside a
		// transaction). This is what makes native RLS enforcement actually
		// bite: the principal/connecting role stays the tables' owner
		// (exempt from RLS by default), so without this second role every
		// policy would be invisible to every request. Idempotent: skips
		// CREATE ROLE if it already exists, and GRANT of a membership that
		// already holds is a no-op.
		`DO $do$
		 BEGIN
		   IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zeep_app_enduser') THEN
		     CREATE ROLE zeep_app_enduser NOSUPERUSER NOBYPASSRLS NOLOGIN;
		   END IF;
		 END
		 $do$`,
		`GRANT zeep_app_enduser TO CURRENT_USER`,
		`CREATE SCHEMA IF NOT EXISTS zeep_system`,
		`CREATE TABLE IF NOT EXISTS zeep_system.dashboard_users (
			id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			email        TEXT        UNIQUE NOT NULL,
			password_hash TEXT       NOT NULL DEFAULT '',
			google_id    TEXT        UNIQUE,
			role         TEXT        NOT NULL CHECK (role IN ('superadmin','admin','auditor','member')),
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// 2→4 role tiers. Existing 'admin' users (under the OLD 2-value
		// model) are reclassified as 'member' ('admin' is
		// now a platform-management role distinct from the per-app "owner"
		// pattern that 'member' replaces). 'superadmin' is untouched.
		//
		// This one-time demotion must run EXACTLY ONCE, not on every boot — once
		// 'admin' becomes a valid, assignable role again (via CreateUser /
		// UpdateUserRole), re-running the bare UPDATE on every ProvisionZeepSystem
		// call would silently demote every admin created after the migration on
		// the next restart/deploy. Guard: only run DROP/UPDATE/ADD while the OLD
		// 2-value constraint (no 'auditor' in its definition) is still in effect;
		// once the 4-value constraint is installed, this whole block is a no-op
		// forever after, regardless of how many admins exist.
		`DO $do$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM pg_constraint c
		     JOIN pg_class t ON t.oid = c.conrelid
		     JOIN pg_namespace n ON n.oid = t.relnamespace
		     WHERE n.nspname = 'zeep_system' AND t.relname = 'dashboard_users'
		       AND c.conname = 'dashboard_users_role_check'
		       AND pg_get_constraintdef(c.oid) NOT LIKE '%auditor%'
		   ) THEN
		     ALTER TABLE zeep_system.dashboard_users DROP CONSTRAINT dashboard_users_role_check;
		     UPDATE zeep_system.dashboard_users SET role = 'member' WHERE role = 'admin';
		     ALTER TABLE zeep_system.dashboard_users ADD CONSTRAINT dashboard_users_role_check
		       CHECK (role IN ('superadmin','admin','auditor','member'));
		   END IF;
		 END
		 $do$`,
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
		`ALTER TABLE zeep_system.apps ADD COLUMN IF NOT EXISTS enduser_roles_config JSONB NOT NULL DEFAULT '["member"]'::jsonb`,
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
		// Drop the pre-rbac `app_ownership` table. Its co-owners were
		// migrated to `app_members` as admin (idempotent ON CONFLICT DO
		// NOTHING), and authorization enforcement is 100% on
		// `ResolveAppRole` so the fallback is no longer needed.
		// New apps add the owner to `app_members` directly in CreateApp —
		// no path in the code touches `app_ownership` anymore.
		`DROP TABLE IF EXISTS zeep_system.app_ownership`,
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
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS max_csv_export_rows INT NOT NULL DEFAULT 10000`,
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS statement_timeout_ms INT NOT NULL DEFAULT 30000`,
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS require_rls_default BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE zeep_system.system_config ADD COLUMN IF NOT EXISTS retention_days INT NOT NULL DEFAULT 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_system_config_singleton
		 ON zeep_system.system_config ((TRUE))`,
		// An IF, not INSERT ... WHERE NOT EXISTS: the state_version trigger on
		// system_config is statement-level and fires on a zero-row INSERT
		// too, which made every pod boot reload the registry on every pod.
		`DO $do$
		 BEGIN
		   IF NOT EXISTS (SELECT 1 FROM zeep_system.system_config) THEN
		     INSERT INTO zeep_system.system_config (soft_delete_enabled) VALUES (false);
		   END IF;
		 END
		 $do$`,
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
		// Unified per-app membership. One row per (user, app) with role
		// admin/editor/viewer. This table is the single source of truth
		// for "can this user act on this app?" — every per-app auth check
		// routes through `ResolveAppRole`, which reads from here. The
		// pre-rbac `app_ownership` table (co-owners) was dropped once its
		// data was migrated here.
		//
		// Schema notes:
		//   - Exactly one of backend_app_id / frontend_app_id is set (CHECK).
		//   - UNIQUE is partial per axis (WHERE backend_app_id IS NOT NULL /
		//     WHERE frontend_app_id IS NOT NULL) so the same user can be admin
		//     of a backend app and viewer of a frontend app without conflict.
		//   - ON DELETE CASCADE on user_id cleans up membership when a
		//     dashboard user is deleted (spec edge case).
		//   - Created after both `apps` AND `frontend_apps` exist (the FK to
		//     frontend_apps would fail otherwise — see the move in the
		//     provisioner ordering).
		`CREATE TABLE IF NOT EXISTS zeep_system.app_members (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			backend_app_id  UUID        REFERENCES zeep_system.apps(id)          ON DELETE CASCADE,
			frontend_app_id UUID        REFERENCES zeep_system.frontend_apps(id) ON DELETE CASCADE,
			user_id         UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id) ON DELETE CASCADE,
			role            TEXT        NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			CHECK ((backend_app_id IS NOT NULL AND frontend_app_id IS NULL)
			    OR (backend_app_id IS NULL     AND frontend_app_id IS NOT NULL))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_members_backend_unique
		 ON zeep_system.app_members(backend_app_id, user_id) WHERE backend_app_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_members_frontend_unique
		 ON zeep_system.app_members(frontend_app_id, user_id) WHERE frontend_app_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_app_members_user
		 ON zeep_system.app_members(user_id)`,
		// Migrate existing ownership into app_members. The two
		// pre-existing sources of "this user is responsible for this app"
		// are collapsed into a single row with role='admin':
		//   1. apps.owner_id (backend apps, the current single owner)
		//   2. frontend_apps.created_by (frontend apps; resolved by email
		//      against dashboard_users — unresolved values leave the app
		//      without any membership, which is intentional: superadmin
		//      retains access and can add the first admin manually)
		//
		// The third source (pre-rbac `app_ownership` co-owners) was migrated
		// previously but is no longer present after the table was dropped;
		// the migration statement that read from it has been removed.
		//
		// Both use ON CONFLICT DO NOTHING against the partial UNIQUE
		// indexes on app_members, so re-running ProvisionZeepSystem is safe.
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role)
		 SELECT apps.id, apps.owner_id, 'admin' FROM zeep_system.apps
		 ON CONFLICT (backend_app_id, user_id) WHERE backend_app_id IS NOT NULL DO NOTHING`,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role)
		 SELECT fa.id, du.id, 'admin'
		 FROM zeep_system.frontend_apps fa
		 JOIN zeep_system.dashboard_users du ON du.email = fa.created_by
		 ON CONFLICT (frontend_app_id, user_id) WHERE frontend_app_id IS NOT NULL DO NOTHING`,
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
		 ADD COLUMN IF NOT EXISTS render_project_id     TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS base_domain           TEXT NOT NULL DEFAULT '',
		 ADD COLUMN IF NOT EXISTS render_environment_id TEXT NOT NULL DEFAULT ''`,
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
		// end-user-row-policies T-08: native RLS policy metadata. app_id has
		// ON DELETE CASCADE (whole-app deletion cleans these up automatically);
		// a single-table deletion (DeleteAppTable) has no DB-level FK to
		// app_tables here — table_name is a plain column, resolved logically
		// the same way app_tables.name already is elsewhere — so that cleanup
		// path is handled at the application level (see
		// deleteTablePoliciesForTable in table_policies_store.go).
		`CREATE TABLE IF NOT EXISTS zeep_system.table_policies (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			app_id         UUID        NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			table_name     TEXT        NOT NULL,
			action         TEXT        NOT NULL CHECK (action IN ('select','insert','update','delete')),
			roles          JSONB       NOT NULL,
			clauses        JSONB       NOT NULL,
			pg_policy_name TEXT        NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_by     UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id),
			UNIQUE (app_id, table_name, action, pg_policy_name)
		)`,
		`ALTER TABLE zeep_system.table_policies
		 ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ,
		 ADD COLUMN IF NOT EXISTS updated_by UUID REFERENCES zeep_system.dashboard_users(id)`,
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
		// inbound-webhooks T1: catalog tables for external providers pushing
		// events into an app table. webhook_subscriptions is the parent (one
		// URL/token per row); webhook_event_mappings holds the per-event-type
		// action/field configuration; webhook_deliveries is the append-only
		// call log. See .specs/features/inbound-webhooks/design.md Data Models.
		`CREATE TABLE IF NOT EXISTS zeep_system.webhook_subscriptions (
			id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			app_id           UUID        NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			name             TEXT        NOT NULL,
			method           TEXT        NOT NULL CHECK (method IN ('GET','POST','PUT','PATCH')),
			token_secret     TEXT        NOT NULL,
			event_type_path  TEXT        NOT NULL,
			event_id_path    TEXT,
			status           TEXT        NOT NULL DEFAULT 'capture' CHECK (status IN ('capture','active')),
			captured_sample  JSONB,
			deleted_at       TIMESTAMPTZ,
			created_by       UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id),
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// Renames token_hash -> token_secret for schemas provisioned before the
		// token switched from a one-way SHA-256 hash to reversible AES-256-GCM
		// encryption (the dashboard needs to display a webhook's full URL at
		// any time, not just once at creation/rotation) — a stored SHA-256
		// hash can never be decrypted, so any webhook created before this
		// migration must have its token rotated once to keep working.
		`DO $do$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM information_schema.columns
		     WHERE table_schema = 'zeep_system' AND table_name = 'webhook_subscriptions'
		       AND column_name = 'token_hash'
		   ) THEN
		     ALTER TABLE zeep_system.webhook_subscriptions RENAME COLUMN token_hash TO token_secret;
		   END IF;
		 END
		 $do$`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_app_id
		 ON zeep_system.webhook_subscriptions(app_id)`,
		// webhook_id has no ON DELETE CASCADE from webhook_deliveries (below)
		// since the parent is only ever soft-deleted in practice, but a
		// cascade here (mapping → subscription) is harmless and keeps the
		// mapping table from accumulating orphaned rows if a subscription
		// were ever hard-deleted.
		`CREATE TABLE IF NOT EXISTS zeep_system.webhook_event_mappings (
			id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			webhook_id        UUID        NOT NULL REFERENCES zeep_system.webhook_subscriptions(id) ON DELETE CASCADE,
			event_type_value  TEXT        NOT NULL,
			action            TEXT        NOT NULL CHECK (action IN ('insert','update','delete')),
			target_table      TEXT        NOT NULL,
			match_key_column  TEXT,
			field_mappings    JSONB       NOT NULL DEFAULT '[]',
			created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (webhook_id, event_type_value)
		)`,
		// No ON DELETE CASCADE on webhook_id: deliveries must outlive their
		// parent subscription's (soft-)deletion until their own independent
		// 30-day retention purge (design.md Data Models, WEBHOOK-22).
		`CREATE TABLE IF NOT EXISTS zeep_system.webhook_deliveries (
			id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			webhook_id        UUID        NOT NULL REFERENCES zeep_system.webhook_subscriptions(id),
			received_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			http_status       INT         NOT NULL,
			outcome           TEXT        NOT NULL CHECK (outcome IN (
			                    'captured','inserted','updated','deleted',
			                    'unmapped','duplicate_skipped','row_not_found',
			                    'invalid_token','malformed','write_error',
			                    'verification_challenge','ambiguous_match'
			                  )),
			event_type_value  TEXT,
			event_id          TEXT,
			raw_payload       JSONB       NOT NULL DEFAULT '{}',
			target_row_id     TEXT,
			error_detail      TEXT
		)`,
		// Widens the outcome CHECK for schemas already provisioned before
		// 'verification_challenge' existed — same guarded-DO pattern as the
		// dashboard_users_role_check widen above, so it's a no-op once applied.
		`DO $do$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM pg_constraint c
		     JOIN pg_class t ON t.oid = c.conrelid
		     JOIN pg_namespace n ON n.oid = t.relnamespace
		     WHERE n.nspname = 'zeep_system' AND t.relname = 'webhook_deliveries'
		       AND c.conname = 'webhook_deliveries_outcome_check'
		       AND pg_get_constraintdef(c.oid) NOT LIKE '%verification_challenge%'
		   ) THEN
		     ALTER TABLE zeep_system.webhook_deliveries DROP CONSTRAINT webhook_deliveries_outcome_check;
		     ALTER TABLE zeep_system.webhook_deliveries ADD CONSTRAINT webhook_deliveries_outcome_check
		       CHECK (outcome IN (
		         'captured','inserted','updated','deleted',
		         'unmapped','duplicate_skipped','row_not_found',
		         'invalid_token','malformed','write_error',
		         'verification_challenge'
		       ));
		   END IF;
		 END
		 $do$`,
		// Widens the outcome CHECK again for 'ambiguous_match' (match-key
		// column resolves to more than one row on update/delete) — same
		// guarded-DO pattern, no-op once applied.
		`DO $do$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM pg_constraint c
		     JOIN pg_class t ON t.oid = c.conrelid
		     JOIN pg_namespace n ON n.oid = t.relnamespace
		     WHERE n.nspname = 'zeep_system' AND t.relname = 'webhook_deliveries'
		       AND c.conname = 'webhook_deliveries_outcome_check'
		       AND pg_get_constraintdef(c.oid) NOT LIKE '%ambiguous_match%'
		   ) THEN
		     ALTER TABLE zeep_system.webhook_deliveries DROP CONSTRAINT webhook_deliveries_outcome_check;
		     ALTER TABLE zeep_system.webhook_deliveries ADD CONSTRAINT webhook_deliveries_outcome_check
		       CHECK (outcome IN (
		         'captured','inserted','updated','deleted',
		         'unmapped','duplicate_skipped','row_not_found',
		         'invalid_token','malformed','write_error',
		         'verification_challenge','ambiguous_match'
		       ));
		   END IF;
		 END
		 $do$`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id_received_at
		 ON zeep_system.webhook_deliveries(webhook_id, received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_dedup
		 ON zeep_system.webhook_deliveries(webhook_id, event_id) WHERE event_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_received_at
		 ON zeep_system.webhook_deliveries(received_at)`,
		// Personal access tokens for MCP-server / script access (mcp-server
		// spec, T1). oauth_client_id/refresh_token_hash have no FK yet — the
		// oauth_clients table they'll eventually reference is added later
		// (T17); a plain UUID column now avoids a second migration then, per
		// design.md's Data Models note for dashboard_pats.
		`CREATE TABLE IF NOT EXISTS zeep_system.dashboard_pats (
			id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id            UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id) ON DELETE CASCADE,
			name               TEXT        NOT NULL,
			token_hash         TEXT        NOT NULL UNIQUE,
			kind               TEXT        NOT NULL CHECK (kind IN ('manual','ephemeral','oauth')),
			oauth_client_id    UUID,
			refresh_token_hash TEXT,
			expires_at         TIMESTAMPTZ,
			revoked_at         TIMESTAMPTZ,
			last_used_at       TIMESTAMPTZ,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dashboard_pats_user_id ON zeep_system.dashboard_pats(user_id)`,
		// Dynamically-registered OAuth clients (mcp-server spec, T17) — id is
		// the client_id handed back at registration, generated via
		// generateToken (a hex string, not a UUID) per design.md's
		// OAuthClientStore component, so it's TEXT rather than this
		// codebase's usual UUID PRIMARY KEY.
		`CREATE TABLE IF NOT EXISTS zeep_system.oauth_clients (
			id            TEXT        PRIMARY KEY,
			name          TEXT        NOT NULL,
			redirect_uris JSONB       NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// dashboard_pats.oauth_client_id was added in T1 as a plain UUID
		// column (oauth_clients didn't exist yet, per that migration's own
		// comment) — retyped to TEXT now that oauth_clients.id is TEXT
		// (generateToken's hex output, not a UUID). Guarded so this is a
		// no-op once applied.
		`DO $do$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM information_schema.columns
		     WHERE table_schema = 'zeep_system' AND table_name = 'dashboard_pats'
		       AND column_name = 'oauth_client_id' AND data_type = 'uuid'
		   ) THEN
		     ALTER TABLE zeep_system.dashboard_pats ALTER COLUMN oauth_client_id TYPE TEXT;
		   END IF;
		 END
		 $do$`,
		// FK deferred until now since oauth_clients didn't exist at T1.
		// Guarded so this is a no-op once applied.
		`DO $do$
		 BEGIN
		   IF NOT EXISTS (
		     SELECT 1 FROM pg_constraint c
		     JOIN pg_class t ON t.oid = c.conrelid
		     JOIN pg_namespace n ON n.oid = t.relnamespace
		     WHERE n.nspname = 'zeep_system' AND t.relname = 'dashboard_pats'
		       AND c.conname = 'dashboard_pats_oauth_client_id_fkey'
		   ) THEN
		     ALTER TABLE zeep_system.dashboard_pats
		       ADD CONSTRAINT dashboard_pats_oauth_client_id_fkey
		       FOREIGN KEY (oauth_client_id) REFERENCES zeep_system.oauth_clients(id) ON DELETE CASCADE;
		   END IF;
		 END
		 $do$`,
		// Single-use, short-lived PKCE authorization codes (mcp-server spec,
		// T18) — hashed like every other token in this design (design.md
		// Tech Decisions), never an in-memory map, so a code issued by one
		// replica is exchangeable against a different replica handling the
		// token-exchange request (AGENTS.md's no-in-memory-cross-request-
		// state rule).
		`CREATE TABLE IF NOT EXISTS zeep_system.oauth_auth_codes (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			code_hash      TEXT        NOT NULL UNIQUE,
			client_id      TEXT        NOT NULL REFERENCES zeep_system.oauth_clients(id) ON DELETE CASCADE,
			user_id        UUID        NOT NULL REFERENCES zeep_system.dashboard_users(id) ON DELETE CASCADE,
			code_challenge TEXT        NOT NULL,
			redirect_uri   TEXT        NOT NULL,
			used_at        TIMESTAMPTZ,
			expires_at     TIMESTAMPTZ NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_auth_codes_expires_at ON zeep_system.oauth_auth_codes(expires_at)`,
		// family_id groups every dashboard_pats row descended from one OAuth
		// grant (the original authorization_code exchange, plus every
		// refresh rotation since) — mcp-server spec T20, so refresh-token-
		// reuse detection can revoke the whole lineage in one UPDATE
		// (design.md Tech Decisions: refresh token rotation + reuse
		// detection), not just the single reused row.
		`ALTER TABLE zeep_system.dashboard_pats ADD COLUMN IF NOT EXISTS family_id UUID`,
		`CREATE INDEX IF NOT EXISTS idx_dashboard_pats_family_id ON zeep_system.dashboard_pats(family_id) WHERE family_id IS NOT NULL`,
		// Global, single-row-per-provider AI provider config (ai-build-chat
		// spec, T2) — same shape as zeep_system.auth_providers, one row per
		// provider name ('openai' | 'gemini' | 'claude'). api_key_encrypted
		// is AES-256-GCM ciphertext (crypto.EncryptAIProviderKey), never the
		// plaintext key. See design.md Data Models.
		`CREATE TABLE IF NOT EXISTS zeep_system.ai_providers (
			provider          TEXT        PRIMARY KEY,
			model             TEXT        NOT NULL DEFAULT '',
			api_key_encrypted TEXT        NOT NULL DEFAULT '',
			enabled           BOOLEAN     NOT NULL DEFAULT false,
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// Per-user "Build with AI" chat session (ai-build-chat spec, T5).
		// One row per in-progress/completed/abandoned build; created_app_id
		// is nullable until confirm succeeds (AIBC-08/AIBC-22) and is set
		// immediately after CreateAppForUser, before the per-table loop, so
		// a partial confirm failure still leaves it pointing at what was
		// actually created. See design.md Data Models.
		`CREATE TABLE IF NOT EXISTS zeep_system.ai_build_sessions (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			owner_user_id  UUID        NOT NULL,
			status         TEXT        NOT NULL DEFAULT 'in_progress',
			created_app_id UUID,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS ai_build_sessions_owner_status_idx
		 ON zeep_system.ai_build_sessions (owner_user_id, status)`,
		// Chat turn history for a build session. plan_json is populated only
		// on the assistant message that carries a propose_app_plan result
		// (AIBC-14). ON DELETE CASCADE ties message lifetime to its session.
		`CREATE TABLE IF NOT EXISTS zeep_system.ai_build_messages (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			session_id UUID        NOT NULL REFERENCES zeep_system.ai_build_sessions(id) ON DELETE CASCADE,
			role       TEXT        NOT NULL,
			content    TEXT        NOT NULL DEFAULT '',
			plan_json  JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS ai_build_messages_session_idx
		 ON zeep_system.ai_build_messages (session_id, created_at)`,
		// mode/target_app_id (ai-edit-chat spec, T1) let one row on
		// ai_build_sessions represent either a "create" session (mode's
		// default, target_app_id stays NULL) or an "edit" session scoped to
		// an existing app (target_app_id populated at creation). Additive
		// only — created_app_id above is untouched and stays create-mode-only.
		`ALTER TABLE zeep_system.ai_build_sessions ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'create'`,
		`ALTER TABLE zeep_system.ai_build_sessions ADD COLUMN IF NOT EXISTS target_app_id UUID REFERENCES zeep_system.apps(id)`,
		// The FK above was created without ON DELETE CASCADE, so deleting an
		// app that ever had an "Edit with AI" session (even a message-less
		// one, since GetOrCreateInProgressEditSession creates the row
		// unconditionally) hit a 23503 and made the app permanently
		// undeletable. Every other REFERENCES zeep_system.apps(id) in this
		// file uses ON DELETE CASCADE; this swap brings target_app_id in
		// line. Idempotent: safe to re-run on installs that already have
		// the plain FK or the cascading one.
		`ALTER TABLE zeep_system.ai_build_sessions DROP CONSTRAINT IF EXISTS ai_build_sessions_target_app_id_fkey`,
		`ALTER TABLE zeep_system.ai_build_sessions
		 ADD CONSTRAINT ai_build_sessions_target_app_id_fkey
		 FOREIGN KEY (target_app_id) REFERENCES zeep_system.apps(id) ON DELETE CASCADE`,
		// One in_progress edit session per (owner_user_id, target_app_id) —
		// enforced at the database level, not just in application code, so a
		// race between two concurrent "Edit with AI" opens for the same
		// (user, app) can't create two in_progress rows (design.md Data
		// Models).
		`CREATE UNIQUE INDEX IF NOT EXISTS ai_build_sessions_edit_in_progress_uidx
		 ON zeep_system.ai_build_sessions (owner_user_id, target_app_id)
		 WHERE mode = 'edit' AND status = 'in_progress'`,
		// Cross-replica state version (multi-replica-consistency). Every pod
		// keeps apps, tables and system config in memory; each one polls this
		// single row and reloads when `version` moves. Bumped by triggers, not
		// Go call sites, so every writer (dashboard, MCP, AI chat, a manual
		// SQL fix) is covered. A row, not a sequence: nextval is not
		// transactional, so a pod could reload before the writer commits and
		// miss the change; the row update becomes visible with the commit.
		`CREATE TABLE IF NOT EXISTS zeep_system.state_version (
			id         SMALLINT    PRIMARY KEY CHECK (id = 1),
			version    BIGINT      NOT NULL DEFAULT 0,
			changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO zeep_system.state_version (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
		`CREATE OR REPLACE FUNCTION zeep_system.bump_state_version() RETURNS trigger
		 LANGUAGE plpgsql AS $fn$
		 BEGIN
		   UPDATE zeep_system.state_version SET version = version + 1, changed_at = now() WHERE id = 1;
		   RETURN NULL;
		 END
		 $fn$`,
		// Statement-level, so a bulk write bumps once. app_tokens only fires
		// on revocation (UPDATE OF revoked_at) and DELETE: last_used_at is
		// written on every API request and expires_at on login, and neither
		// changes what a pod caches. Guarded by pg_trigger because CREATE OR
		// REPLACE TRIGGER needs PG 14.
		`DO $do$
		 DECLARE
		   t RECORD;
		 BEGIN
		   FOR t IN SELECT * FROM (VALUES
		     ('apps',          'INSERT OR UPDATE OR DELETE'),
		     ('app_tables',    'INSERT OR UPDATE OR DELETE'),
		     ('system_config', 'INSERT OR UPDATE OR DELETE'),
		     ('app_tokens',    'UPDATE OF revoked_at OR DELETE')
		   ) AS v(tbl, events)
		   LOOP
		     IF NOT EXISTS (
		       SELECT 1 FROM pg_trigger
		       WHERE tgname = 'bump_state_version'
		         AND tgrelid = format('zeep_system.%I', t.tbl)::regclass
		     ) THEN
		       EXECUTE format(
		         'CREATE TRIGGER bump_state_version AFTER %s ON zeep_system.%I FOR EACH STATEMENT EXECUTE FUNCTION zeep_system.bump_state_version()',
		         t.events, t.tbl);
		     END IF;
		   END LOOP;
		 END
		 $do$`,
		// AI Frontend Builder (ai-frontend-builder spec, T11). Everything
		// below is appended after apps, frontend_apps, github_templates and
		// ai_providers because it references them. See design.md Data Models.
		`ALTER TABLE zeep_system.ai_providers ADD COLUMN IF NOT EXISTS base_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE zeep_system.github_templates ADD COLUMN IF NOT EXISTS builder_enabled BOOLEAN NOT NULL DEFAULT false`,
		// Singleton settings row (id = 1). A zero cap means "unset": sessions
		// stay blocked until a superadmin sets one (AFB-02, AFB-14).
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_settings (
			id                     SMALLINT    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			enabled                BOOLEAN     NOT NULL DEFAULT false,
			provider               TEXT        NOT NULL DEFAULT 'openai',
			context_window_tokens  INT         NOT NULL DEFAULT 128000,
			session_token_cap      BIGINT      NOT NULL DEFAULT 0,
			user_monthly_token_cap BIGINT      NOT NULL DEFAULT 0,
			max_steps_per_turn     INT         NOT NULL DEFAULT 40,
			max_fix_iterations     INT         NOT NULL DEFAULT 3,
			max_turn_seconds       INT         NOT NULL DEFAULT 600,
			updated_by             UUID,
			updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// Versioned persona + rules (AFB-25). The active version is the
		// newest row.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_prompt_versions (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			persona    TEXT        NOT NULL DEFAULT '',
			rules      TEXT        NOT NULL DEFAULT '',
			created_by UUID        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		// Custom builder skills (ai-builder-skills S06). One row per skill
		// lineage; a soft delete frees the name, and a later upload of the
		// same name starts a new lineage. Built-ins live in the binary, never
		// here: only their enabled override is stored.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_skills (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			name       TEXT        NOT NULL,
			enabled    BOOLEAN     NOT NULL DEFAULT true,
			created_by UUID        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at TIMESTAMPTZ,
			deleted_by UUID
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS builder_skills_name_live_idx
		 ON zeep_system.builder_skills (lower(name)) WHERE deleted_at IS NULL`,
		// Append-only versions; the active version is the highest one.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_skill_versions (
			id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			skill_id       UUID        NOT NULL REFERENCES zeep_system.builder_skills(id) ON DELETE CASCADE,
			version        INT         NOT NULL,
			description    TEXT        NOT NULL,
			license        TEXT        NOT NULL DEFAULT '',
			body           TEXT        NOT NULL,
			content_sha256 TEXT        NOT NULL,
			created_by     UUID        NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (skill_id, version)
		)`,
		// Enabled override of an embedded built-in. No row means enabled.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_skill_builtin_state (
			name       TEXT        PRIMARY KEY,
			enabled    BOOLEAN     NOT NULL,
			updated_by UUID        NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_sessions (
			id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			owner_user_id     UUID        NOT NULL,
			backend_app_id    UUID        NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
			title             TEXT        NOT NULL DEFAULT '',
			template_kind     TEXT        NOT NULL,
			template_id       UUID,
			template_version  TEXT        NOT NULL,
			prompt_version_id UUID        REFERENCES zeep_system.builder_prompt_versions(id),
			locale            TEXT        NOT NULL DEFAULT 'en',
			status            TEXT        NOT NULL DEFAULT 'idle',
			head_sha          TEXT        NOT NULL DEFAULT '',
			head_turn_seq     INT         NOT NULL DEFAULT 0,
			frontend_app_id   UUID        REFERENCES zeep_system.frontend_apps(id) ON DELETE SET NULL,
			published_sha     TEXT        NOT NULL DEFAULT '',
			tokens_in         BIGINT      NOT NULL DEFAULT 0,
			tokens_out        BIGINT      NOT NULL DEFAULT 0,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS builder_sessions_owner_idx
		 ON zeep_system.builder_sessions (owner_user_id, updated_at DESC)`,
		// AI Builder Backend (ai-builder-backend spec, B01). All additive and
		// defaulted: existing rows keep today's behavior.
		`ALTER TABLE zeep_system.builder_sessions
		   ADD COLUMN IF NOT EXISTS backend_origin TEXT NOT NULL DEFAULT 'existing'
		   CHECK (backend_origin IN ('existing', 'builder'))`,
		`ALTER TABLE zeep_system.builder_settings
		   ADD COLUMN IF NOT EXISTS backend_approval_mode TEXT NOT NULL DEFAULT 'ask'
		   CHECK (backend_approval_mode IN ('ask', 'auto'))`,
		`ALTER TABLE zeep_system.builder_settings
		   ADD COLUMN IF NOT EXISTS publish_rls_gate TEXT NOT NULL DEFAULT 'warn'
		   CHECK (publish_rls_gate IN ('warn', 'block'))`,
		// Turn queue. lease_owner/lease_until implement the Postgres row lease
		// claimed with FOR UPDATE SKIP LOCKED (AFB-18); the partial unique
		// index below allows at most one non-terminal turn per session.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_turns (
			id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			session_id       UUID        NOT NULL REFERENCES zeep_system.builder_sessions(id) ON DELETE CASCADE,
			seq              INT         NOT NULL,
			kind             TEXT        NOT NULL DEFAULT 'chat',
			status           TEXT        NOT NULL DEFAULT 'pending',
			pending_proposal JSONB,
			commit_sha       TEXT        NOT NULL DEFAULT '',
			lease_owner      TEXT,
			lease_until      TIMESTAMPTZ,
			error_code       TEXT,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
			finished_at      TIMESTAMPTZ,
			UNIQUE (session_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS builder_turns_claim_idx
		 ON zeep_system.builder_turns (status, created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS builder_turns_one_active_idx
		 ON zeep_system.builder_turns (session_id)
		 WHERE status IN ('pending','queued','running','awaiting_confirmation')`,
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_messages (
			id           BIGSERIAL   PRIMARY KEY,
			session_id   UUID        NOT NULL REFERENCES zeep_system.builder_sessions(id) ON DELETE CASCADE,
			turn_id      UUID        NOT NULL REFERENCES zeep_system.builder_turns(id) ON DELETE CASCADE,
			role         TEXT        NOT NULL,
			content      TEXT        NOT NULL DEFAULT '',
			tool_calls   JSONB,
			tool_call_id TEXT,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS builder_messages_session_idx
		 ON zeep_system.builder_messages (session_id, id)`,
		// builder_events.id is the polling cursor. payload never carries
		// secrets or raw provider errors.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_events (
			id         BIGSERIAL   PRIMARY KEY,
			session_id UUID        NOT NULL REFERENCES zeep_system.builder_sessions(id) ON DELETE CASCADE,
			turn_id    UUID        REFERENCES zeep_system.builder_turns(id) ON DELETE CASCADE,
			type       TEXT        NOT NULL,
			payload    JSONB       NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS builder_events_session_idx
		 ON zeep_system.builder_events (session_id, id)`,
		// One row per commit: a turn that commits after several tool batches
		// stores one incremental bundle per batch, numbered by part from 0 in
		// save order. Turn 0 / part 0 is the full initial tree.
		`CREATE TABLE IF NOT EXISTS zeep_system.builder_snapshots (
			session_id UUID        NOT NULL REFERENCES zeep_system.builder_sessions(id) ON DELETE CASCADE,
			turn_seq   INT         NOT NULL,
			part       INT         NOT NULL DEFAULT 0,
			commit_sha TEXT        NOT NULL,
			bundle     BYTEA       NOT NULL,
			size_bytes INT         NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (session_id, turn_seq, part)
		)`,
		// Upgrade for databases that ran the old one-row-per-turn shape. The
		// PK is only replaced when its columns differ from the new key, so a
		// second boot (or a fresh DB) is a no-op.
		`ALTER TABLE zeep_system.builder_snapshots ADD COLUMN IF NOT EXISTS part INT NOT NULL DEFAULT 0`,
		`DO $do$
		 DECLARE
		   pk_name TEXT;
		   pk_cols TEXT[];
		 BEGIN
		   SELECT c.conname,
		          array_agg(a.attname::text ORDER BY k.ord)
		     INTO pk_name, pk_cols
		     FROM pg_constraint c
		     CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
		     JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		    WHERE c.conrelid = 'zeep_system.builder_snapshots'::regclass
		      AND c.contype = 'p'
		    GROUP BY c.conname;
		   IF pk_cols IS DISTINCT FROM ARRAY['session_id', 'turn_seq', 'part'] THEN
		     IF pk_name IS NOT NULL THEN
		       EXECUTE format('ALTER TABLE zeep_system.builder_snapshots DROP CONSTRAINT %I', pk_name);
		     END IF;
		     ALTER TABLE zeep_system.builder_snapshots ADD PRIMARY KEY (session_id, turn_seq, part);
		   END IF;
		 END
		 $do$`,
		// Per-call token ledger. Month caps are computed from created_at
		// (AFB-14); no FK on session_id/turn_id so usage survives session
		// deletion.
		`CREATE TABLE IF NOT EXISTS zeep_system.ai_usage_ledger (
			id            BIGSERIAL   PRIMARY KEY,
			user_id       UUID        NOT NULL,
			feature       TEXT        NOT NULL,
			session_id    UUID,
			turn_id       UUID,
			provider      TEXT        NOT NULL,
			model         TEXT        NOT NULL,
			input_tokens  INT         NOT NULL,
			output_tokens INT         NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS ai_usage_ledger_user_month_idx
		 ON zeep_system.ai_usage_ledger (user_id, created_at)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("dashboard: provision: %w", err)
		}
	}
	return tx.Commit(ctx)
}

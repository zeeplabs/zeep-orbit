package provisioner

import (
	"context"
	"fmt"
)

// Idempotent — safe to call on every startup.
func (p *Provisioner) provisionAuthTables(ctx context.Context, schema string) ([]string, error) {
	var created []string

	usersDDL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q."_auth_users" (
		"id"                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
		"email"              TEXT        NOT NULL UNIQUE,
		"phone"              TEXT,
		"password_hash"      TEXT        NOT NULL,
		"name"               TEXT,
		"avatar_url"         TEXT,
		"email_confirmed_at" TIMESTAMPTZ,
		"last_sign_in_at"    TIMESTAMPTZ,
		"created_at"         TIMESTAMPTZ NOT NULL DEFAULT now(),
		"updated_at"         TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, schema)

	usersCreated, err := p.createAuthTable(ctx, schema, "_auth_users", usersDDL)
	if err != nil {
		return nil, err
	}
	if usersCreated {
		created = append(created, schema+"._auth_users")
	}

	if err := p.addMissingAuthUserColumns(ctx, schema); err != nil {
		return nil, err
	}

	sessionsDDL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q."_auth_sessions" (
		"id"            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
		"user_id"       UUID        NOT NULL REFERENCES %q."_auth_users"("id") ON DELETE CASCADE,
		"refresh_token" TEXT        NOT NULL UNIQUE,
		"expires_at"    TIMESTAMPTZ NOT NULL,
		"created_at"    TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, schema, schema)

	sessionsCreated, err := p.createAuthTable(ctx, schema, "_auth_sessions", sessionsDDL)
	if err != nil {
		return nil, err
	}
	if sessionsCreated {
		created = append(created, schema+"._auth_sessions")
	}

	return created, nil
}

// Safe to call on every request — only applies ALTER TABLE IF NOT EXISTS.
func (p *Provisioner) EnsureAuthUserColumns(ctx context.Context, schema string) error {
	return p.addMissingAuthUserColumns(ctx, schema)
}

func (p *Provisioner) addMissingAuthUserColumns(ctx context.Context, schema string) error {
	alters := []string{
		fmt.Sprintf(`ALTER TABLE %q."_auth_users" ADD COLUMN IF NOT EXISTS "phone"              TEXT`, schema),
		fmt.Sprintf(`ALTER TABLE %q."_auth_users" ADD COLUMN IF NOT EXISTS "email_confirmed_at" TIMESTAMPTZ`, schema),
		fmt.Sprintf(`ALTER TABLE %q."_auth_users" ADD COLUMN IF NOT EXISTS "last_sign_in_at"    TIMESTAMPTZ`, schema),
		fmt.Sprintf(`ALTER TABLE %q."_auth_users" ADD COLUMN IF NOT EXISTS "active"             BOOLEAN NOT NULL DEFAULT true`, schema),
		fmt.Sprintf(`ALTER TABLE %q."_auth_users" ADD COLUMN IF NOT EXISTS "provider"           TEXT NOT NULL DEFAULT 'email'`, schema),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %q ON %q."_auth_users" ("email")`, schema+"_auth_users_email_idx", schema),
	}
	for _, sql := range alters {
		if _, err := p.pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("auth migration %q: %w", sql, err)
		}
	}
	return nil
}

func (p *Provisioner) createAuthTable(ctx context.Context, schema, table, ddl string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
		schema, table,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("auth table: check existence %q.%q: %w", schema, table, err)
	}
	if exists {
		return false, nil
	}
	if _, err := p.pool.Exec(ctx, ddl); err != nil {
		return false, fmt.Errorf("auth table: create %q.%q: %w", schema, table, err)
	}
	return true, nil
}

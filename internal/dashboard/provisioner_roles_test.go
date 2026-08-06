package dashboard

import (
	"context"
	"os"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// rolesTestPool connects to the test DB and seeds a pre-migration state for
// zeep_system.dashboard_users: the OLD 2-value CHECK constraint and seeded
// 'admin'/'superadmin' users. ProvisionZeepSystem should migrate them to the
// 4-role model and replace the constraint.
func rolesTestPool(t *testing.T) *db.Pool {
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
		// Recreate the table with the OLD constraint to faithfully simulate a
		// pre-migration database. CASCADE drops anything that referenced it
		// (sessions, apps, audit_log, etc.) so the helper is self-contained.
		`DROP TABLE IF EXISTS zeep_system.dashboard_users CASCADE`,
		`CREATE TABLE zeep_system.dashboard_users (
			id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			email        TEXT        UNIQUE NOT NULL,
			password_hash TEXT       NOT NULL DEFAULT '',
			role         TEXT        NOT NULL CHECK (role IN ('admin','superadmin')),
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	return pool
}

func TestRoleMigration(t *testing.T) {
	pool := rolesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Seed: 1 admin (should become 'member') + 1 superadmin (must stay untouched).
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.dashboard_users (id, email, role) VALUES
		 ('a0000000-0000-0000-0000-000000000001', 'admin@example.com', 'admin'),
		 ('a0000000-0000-0000-0000-000000000002', 'super@example.com', 'superadmin')`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Run provisioning — the embedded migration must reclassify admin→member
	// and swap the CHECK constraint to the 4-value set.
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	// 1. 'admin' was reclassified to 'member'.
	var adminRole string
	if err := pool.QueryRow(ctx,
		`SELECT role FROM zeep_system.dashboard_users WHERE id = 'a0000000-0000-0000-0000-000000000001'`,
	).Scan(&adminRole); err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if adminRole != "member" {
		t.Errorf("admin role = %q, want member", adminRole)
	}

	// 2. 'superadmin' was not touched.
	var superRole string
	if err := pool.QueryRow(ctx,
		`SELECT role FROM zeep_system.dashboard_users WHERE id = 'a0000000-0000-0000-0000-000000000002'`,
	).Scan(&superRole); err != nil {
		t.Fatalf("query superadmin: %v", err)
	}
	if superRole != "superadmin" {
		t.Errorf("superadmin role = %q, want superadmin", superRole)
	}

	// 3. New constraint rejects values outside the 4-role set.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, role) VALUES ('bad@example.com', 'godmode')`,
	); err == nil {
		t.Error("expected CHECK violation for invalid role, got nil")
	}

	// 4. New constraint accepts every value in the 4-role set.
	for _, role := range []string{"superadmin", "admin", "auditor", "member"} {
		email := "new-" + role + "@example.com"
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.dashboard_users (email, role) VALUES ($1, $2)`,
			email, role,
		); err != nil {
			t.Errorf("insert role=%q should succeed, got: %v", role, err)
		}
	}

	// 5. Re-running provisioning is a no-op (idempotent).
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Errorf("second ProvisionZeepSystem should be idempotent, got: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT role FROM zeep_system.dashboard_users WHERE id = 'a0000000-0000-0000-0000-000000000001'`,
	).Scan(&adminRole); err != nil {
		t.Fatalf("re-query admin: %v", err)
	}
	if adminRole != "member" {
		t.Errorf("after re-run, admin role = %q, want member", adminRole)
	}
}

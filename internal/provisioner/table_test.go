package provisioner

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func ensureRLSTestPool(t *testing.T) *db.Pool {
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
	return pool
}

func rowSecurityEnabled(t *testing.T, pool *db.Pool, schema, table string) bool {
	t.Helper()
	var enabled bool
	err := pool.QueryRow(context.Background(),
		`SELECT relrowsecurity FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = $1 AND c.relname = $2`,
		schema, table,
	).Scan(&enabled)
	if err != nil {
		t.Fatalf("check relrowsecurity for %q.%q: %v", schema, table, err)
	}
	return enabled
}

// TestEnsureRowLevelSecurity_EnablesRLS covers T3's "Done when": the
// extracted helper actually runs ENABLE ROW LEVEL SECURITY against a real
// table (design.md's stated reuse target for createTable/UpdateAppTable/
// table_policies_store.go).
func TestEnsureRowLevelSecurity_EnablesRLS(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("ensure_rls_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.widgets (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if rowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS disabled before EnsureRowLevelSecurity runs")
	}

	if err := EnsureRowLevelSecurity(ctx, pool, schema, "widgets"); err != nil {
		t.Fatalf("EnsureRowLevelSecurity: %v", err)
	}
	if !rowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS enabled after EnsureRowLevelSecurity runs")
	}
}

// TestEnsureRowLevelSecurity_IsIdempotent covers T3's idempotency
// requirement: calling it a second time on an already-RLS-enabled table
// must not error (Postgres itself treats the ALTER as a no-op).
func TestEnsureRowLevelSecurity_IsIdempotent(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("ensure_rls_idempotent_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.widgets (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if err := EnsureRowLevelSecurity(ctx, pool, schema, "widgets"); err != nil {
		t.Fatalf("EnsureRowLevelSecurity (1st): %v", err)
	}
	if err := EnsureRowLevelSecurity(ctx, pool, schema, "widgets"); err != nil {
		t.Fatalf("EnsureRowLevelSecurity (2nd, must be idempotent): %v", err)
	}
	if !rowSecurityEnabled(t, pool, schema, "widgets") {
		t.Fatal("expected RLS still enabled after second call")
	}
}

// createAuthUsersTable creates the minimal "_auth_users" table createTable's
// owner_id FK needs, without going through the full auth provisioning flow
// (internal/provisioner/auth.go) — this test only needs the FK target to
// exist, not the rest of that table's columns.
func createAuthUsersTable(t *testing.T, pool *db.Pool, schema string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(
		`CREATE TABLE %q."_auth_users" (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema,
	)); err != nil {
		t.Fatalf("create _auth_users: %v", err)
	}
}

// TestCreateTable_PolicyModeEnablesRLSAtCreation covers T4 / RLSP-02: a
// table created with rls "policy" comes out of createTable with RLS already
// enabled, before any policy exists — the fail-closed guarantee proven end
// to end (zeep_app_enduser sees zero rows despite a row existing, with zero
// policies registered and no application-level filter).
func TestCreateTable_PolicyModeEnablesRLSAtCreation(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("createtable_policy_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	createAuthUsersTable(t, pool, schema)

	p := New(pool)
	created, err := p.createTable(ctx, schema, "posts", []config.ColumnConfig{
		{Name: "title", Type: "text", Required: true},
	}, "policy")
	if err != nil {
		t.Fatalf("createTable: %v", err)
	}
	if !created {
		t.Fatal("expected createTable to report the table as newly created")
	}

	if !rowSecurityEnabled(t, pool, schema, "posts") {
		t.Fatal("expected RLS enabled on a rls:policy table right after createTable, before any policy exists")
	}

	// Fail-closed proof: grant the enduser role full DML (as a real app
	// would via provisioner.grantEnduserAccess) — zero policies must still
	// deny every row natively, with no filter from the application.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`GRANT USAGE ON SCHEMA %q TO zeep_app_enduser`, schema)); err != nil {
		t.Fatalf("grant usage: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`GRANT SELECT ON ALL TABLES IN SCHEMA %q TO zeep_app_enduser`, schema)); err != nil {
		t.Fatalf("grant select: %v", err)
	}
	var ownerID string
	if err := pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %q."_auth_users" DEFAULT VALUES RETURNING id`, schema)).Scan(&ownerID); err != nil {
		t.Fatalf("seed auth user: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.posts (title, owner_id) VALUES ('hello', $1)`, schema), ownerID); err != nil {
		t.Fatalf("seed post row: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+db.EnduserRole); err != nil {
		t.Fatalf("set local role: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.posts`, schema)).Scan(&count); err != nil {
		t.Fatalf("count as enduser role: %v", err)
	}
	if count != 0 {
		t.Fatalf("zeep_app_enduser saw %d row(s), want 0 (fail-closed: RLS enabled, zero policies)", count)
	}
}

// TestCreateTable_PolicyModeCreatesOwnerColumn covers T4 / RLSP-02's DDL
// requirement: owner_id must exist and be NOT NULL for "policy", same as
// "owner"/"enabled" (spec: DDL is identical across the three modes).
func TestCreateTable_PolicyModeCreatesOwnerColumn(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("createtable_policy_col_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	createAuthUsersTable(t, pool, schema)

	p := New(pool)
	if _, err := p.createTable(ctx, schema, "posts", nil, "policy"); err != nil {
		t.Fatalf("createTable: %v", err)
	}

	var isNullable string
	err := pool.QueryRow(ctx,
		`SELECT is_nullable FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = 'owner_id'`,
		schema, "posts",
	).Scan(&isNullable)
	if err != nil {
		t.Fatalf("expected owner_id column to exist on a rls:policy table: %v", err)
	}
	if isNullable != "NO" {
		t.Fatalf("owner_id is_nullable = %q, want %q (NOT NULL)", isNullable, "NO")
	}
}

// TestAddMissingColumns_PolicyModeAddsOwnerColumn covers T4's addMissingColumns
// branch: a "policy" table missing owner_id (e.g. migrated from an older
// definition) gets it added, same as "owner"/"enabled".
func TestAddMissingColumns_PolicyModeAddsOwnerColumn(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("addcol_policy_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	createAuthUsersTable(t, pool, schema)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.posts (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create table: %v", err)
	}

	p := New(pool)
	added, err := p.addMissingColumns(ctx, schema, "posts", nil, "policy")
	if err != nil {
		t.Fatalf("addMissingColumns: %v", err)
	}

	want := schema + ".posts.owner_id"
	found := false
	for _, a := range added {
		if a == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("addMissingColumns returned %v, want it to include %q", added, want)
	}
}

// TestCreateTable_OwnerAndEnabledUnaffected is the RLSP-04 regression check
// for this file: "owner"/"enabled" table creation must not gain RLS at
// creation time (they still enable it lazily, on first CREATE POLICY) —
// only "policy" mode does.
func TestCreateTable_OwnerAndEnabledUnaffected(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	for _, rls := range []string{"owner", "enabled"} {
		t.Run(rls, func(t *testing.T) {
			schema := fmt.Sprintf("createtable_%s_test_%d", rls, time.Now().UnixNano())
			ctx := context.Background()
			if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
				t.Fatalf("create schema: %v", err)
			}
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
			})
			createAuthUsersTable(t, pool, schema)

			p := New(pool)
			if _, err := p.createTable(ctx, schema, "posts", nil, rls); err != nil {
				t.Fatalf("createTable: %v", err)
			}
			if rowSecurityEnabled(t, pool, schema, "posts") {
				t.Fatalf("rls=%q: expected RLS NOT enabled at creation (still lazy, on first policy)", rls)
			}
		})
	}
}

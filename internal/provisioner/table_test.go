package provisioner

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

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

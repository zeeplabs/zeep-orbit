package provisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	if _, err := pool.Exec(ctx, fmt.Sprintf(`GRANT SELECT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %q TO zeep_app_enduser`, schema)); err != nil {
		t.Fatalf("grant select/update/delete: %v", err)
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

	// Spec Edge Case: "IF a rls:policy table receives DELETE or UPDATE with
	// no matching policy THEN the operation is denied (0 rows affected)" —
	// same native deny-all must hold for writes, not just SELECT.
	updateTag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %q.posts SET title = 'hacked'`, schema))
	if err != nil {
		t.Fatalf("update as enduser role: %v", err)
	}
	if updateTag.RowsAffected() != 0 {
		t.Fatalf("zeep_app_enduser updated %d row(s), want 0 (fail-closed: RLS enabled, zero policies)", updateTag.RowsAffected())
	}
	deleteTag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %q.posts`, schema))
	if err != nil {
		t.Fatalf("delete as enduser role: %v", err)
	}
	if deleteTag.RowsAffected() != 0 {
		t.Fatalf("zeep_app_enduser deleted %d row(s), want 0 (fail-closed: RLS enabled, zero policies)", deleteTag.RowsAffected())
	}
}

// TestCreateTable_PolicyModeCreatesOwnerColumn covers T4 / RLSP-02's DDL
// requirement: owner_id must exist for "policy", same as "owner"/"enabled".
// Unlike those two, it's nullable — "policy" never auto-populates or filters
// by owner_id (config.AutoScopesByOwner is false for it), and a row with no
// end-user identity behind it (e.g. an inbound webhook delivery) has no
// value to put there.
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
	if isNullable != "YES" {
		t.Fatalf("owner_id is_nullable = %q, want %q (nullable)", isNullable, "YES")
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

// TestCheckForeignKeyColumnTypesMatch_Match covers T3 AC (CFK-05): when the
// existing column's real Postgres type matches the target column's real
// type, the check returns nil.
func TestCheckForeignKeyColumnTypesMatch_Match(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("fk_types_match_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}

	p := New(pool)
	if err := p.CheckForeignKeyColumnTypesMatch(ctx, schema, "orders", "customer_id", "customers", "id"); err != nil {
		t.Fatalf("expected nil for matching types, got: %v", err)
	}
}

// TestCheckForeignKeyColumnTypesMatch_Mismatch covers T3 AC (CFK-05): when
// the real types differ, the check returns a non-nil error naming both
// real types.
func TestCheckForeignKeyColumnTypesMatch_Mismatch(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("fk_types_mismatch_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id TEXT)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}

	p := New(pool)
	err := p.CheckForeignKeyColumnTypesMatch(ctx, schema, "orders", "customer_id", "customers", "id")
	if err == nil {
		t.Fatal("expected error for mismatched types, got nil")
	}
	if !strings.Contains(err.Error(), "text") || !strings.Contains(err.Error(), "uuid") {
		t.Fatalf("expected error to name both real types (text, uuid), got: %v", err)
	}
}

// TestCheckForeignKeyColumnTypesMatch_AuthUsersTarget covers T3's
// "_auth_users target" case: the check works against "_auth_users" the
// same way it works against any other real physical table in the schema.
func TestCheckForeignKeyColumnTypesMatch_AuthUsersTarget(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("fk_types_authusers_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	createAuthUsersTable(t, pool, schema)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.posts (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), author_id UUID)`, schema)); err != nil {
		t.Fatalf("create posts: %v", err)
	}

	p := New(pool)
	if err := p.CheckForeignKeyColumnTypesMatch(ctx, schema, "posts", "author_id", "_auth_users", "id"); err != nil {
		t.Fatalf("expected nil for matching types against _auth_users, got: %v", err)
	}
}

// hasFKConstraint reports whether tableName.columnName has any FOREIGN KEY
// constraint in schema, via information_schema (not naming-convention based).
func hasFKConstraint(t *testing.T, pool *db.Pool, schema, tableName, columnName string) bool {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*)
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY'
		   AND tc.table_schema = $1 AND tc.table_name = $2 AND kcu.column_name = $3`,
		schema, tableName, columnName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check FK constraint on %s.%s.%s: %v", schema, tableName, columnName, err)
	}
	return count > 0
}

// TestAddColumnForeignKey_Success covers T4 AC (CFK-01): a valid, non-
// orphaned FK add succeeds and the constraint is visible in Postgres.
func TestAddColumnForeignKey_Success(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("addfk_success_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}

	p := New(pool)
	if err := p.AddColumnForeignKey(ctx, schema, "orders", "customer_id", config.ReferenceConfig{Table: "customers", Column: "id"}); err != nil {
		t.Fatalf("AddColumnForeignKey: %v", err)
	}

	if !hasFKConstraint(t, pool, schema, "orders", "customer_id") {
		t.Fatal("expected a FOREIGN KEY constraint on orders.customer_id after AddColumnForeignKey")
	}
}

// TestAddColumnForeignKey_OrphanedRowsRejected covers T4 AC (CFK-06): an
// orphaned row (referencing a non-existent target key) makes the ADD
// FOREIGN KEY DDL fail with a *ForeignKeyViolationError carrying Postgres's
// Detail text.
func TestAddColumnForeignKey_OrphanedRowsRejected(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("addfk_orphan_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}
	orphanID := "11111111-1111-1111-1111-111111111111"
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.orders (customer_id) VALUES ($1)`, schema), orphanID); err != nil {
		t.Fatalf("seed orphan row: %v", err)
	}

	p := New(pool)
	err := p.AddColumnForeignKey(ctx, schema, "orders", "customer_id", config.ReferenceConfig{Table: "customers", Column: "id"})
	if err == nil {
		t.Fatal("expected error for orphaned row, got nil")
	}
	var fkErr *ForeignKeyViolationError
	if !errors.As(err, &fkErr) {
		t.Fatalf("expected *ForeignKeyViolationError, got: %T (%v)", err, err)
	}
	if fkErr.Column != "customer_id" {
		t.Errorf("expected Column %q, got %q", "customer_id", fkErr.Column)
	}
	if fkErr.Detail == "" {
		t.Error("expected Postgres Detail text to be preserved, got empty string")
	}
	if hasFKConstraint(t, pool, schema, "orders", "customer_id") {
		t.Fatal("expected no FK constraint to have been created after a rejected add")
	}
}

// TestAddColumnForeignKey_OnDeleteCascadeApplied covers T4's "on_delete
// clause applied correctly" AC: an on_delete:cascade FK actually cascades a
// delete of the target row to the referencing row.
func TestAddColumnForeignKey_OnDeleteCascadeApplied(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("addfk_ondelete_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}

	var customerID string
	if err := pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %q.customers DEFAULT VALUES RETURNING id`, schema)).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	var orderID string
	if err := pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %q.orders (customer_id) VALUES ($1) RETURNING id`, schema), customerID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	p := New(pool)
	if err := p.AddColumnForeignKey(ctx, schema, "orders", "customer_id", config.ReferenceConfig{Table: "customers", Column: "id", OnDelete: "cascade"}); err != nil {
		t.Fatalf("AddColumnForeignKey: %v", err)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %q.customers WHERE id = $1`, schema), customerID); err != nil {
		t.Fatalf("delete customer: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.orders WHERE id = $1`, schema), orderID).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the order to be cascade-deleted, but %d row(s) remain", count)
	}
}

// TestDropColumnForeignKey_DropsExistingConstraint covers T5 AC (CFK-09):
// an existing FK constraint on a column is dropped and no longer appears
// in the catalog.
func TestDropColumnForeignKey_DropsExistingConstraint(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("dropfk_success_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID REFERENCES %q.customers(id))`, schema, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}
	if !hasFKConstraint(t, pool, schema, "orders", "customer_id") {
		t.Fatal("test setup: expected orders.customer_id to have a FK constraint before drop")
	}

	p := New(pool)
	found, err := p.DropColumnForeignKey(ctx, schema, "orders", "customer_id")
	if err != nil {
		t.Fatalf("DropColumnForeignKey: %v", err)
	}
	if !found {
		t.Fatal("expected found=true when a constraint exists")
	}
	if hasFKConstraint(t, pool, schema, "orders", "customer_id") {
		t.Fatal("expected no FK constraint on orders.customer_id after drop")
	}
}

// TestDropColumnForeignKey_NoConstraintReturnsFoundFalse covers T5 AC
// (CFK-09/CFK-10's underlying primitive): a column with no FK constraint
// returns found=false, err=nil (not an error).
func TestDropColumnForeignKey_NoConstraintReturnsFoundFalse(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("dropfk_none_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}

	p := New(pool)
	found, err := p.DropColumnForeignKey(ctx, schema, "orders", "customer_id")
	if err != nil {
		t.Fatalf("expected nil error when no constraint exists, got: %v", err)
	}
	if found {
		t.Fatal("expected found=false when no constraint exists")
	}
}

// TestDropColumnForeignKey_FindsNonConventionallyNamedConstraint covers
// T5's "not named via the <table>_<column>_fkey convention" AC: the catalog
// lookup finds and drops a constraint created with an explicit custom name
// via raw SQL, proving DropColumnForeignKey never assumes a naming
// convention.
func TestDropColumnForeignKey_FindsNonConventionallyNamedConstraint(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("dropfk_customname_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`, schema)); err != nil {
		t.Fatalf("create customers: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %q.orders ADD CONSTRAINT my_totally_custom_fk_name FOREIGN KEY (customer_id) REFERENCES %q.customers(id)`,
		schema, schema,
	)); err != nil {
		t.Fatalf("create custom-named FK: %v", err)
	}

	p := New(pool)
	found, err := p.DropColumnForeignKey(ctx, schema, "orders", "customer_id")
	if err != nil {
		t.Fatalf("DropColumnForeignKey: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for a custom-named constraint (catalog lookup, not naming convention)")
	}
	if hasFKConstraint(t, pool, schema, "orders", "customer_id") {
		t.Fatal("expected no FK constraint on orders.customer_id after drop")
	}
}

// TestDropColumnForeignKey_GenuineQueryErrorPropagates covers the branch at
// table.go's constraint-lookup query that must distinguish "no rows" (a
// legitimate not-found, found=false/err=nil) from a genuine query failure,
// which must propagate as a non-nil error rather than being swallowed as a
// silent found=false/err=nil. A canceled context forces pgx to fail the
// QueryRow with context.Canceled (never pgx.ErrNoRows), so this asserts the
// error path independently of the "no constraint" path already covered by
// TestDropColumnForeignKey_NoConstraintReturnsFoundFalse.
func TestDropColumnForeignKey_GenuineQueryErrorPropagates(t *testing.T) {
	pool := ensureRLSTestPool(t)
	defer pool.Close()

	schema := fmt.Sprintf("dropfk_queryerr_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %q.orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), customer_id UUID)`, schema)); err != nil {
		t.Fatalf("create orders: %v", err)
	}

	p := New(pool)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before use so the lookup query fails immediately with context.Canceled, not ErrNoRows

	found, err := p.DropColumnForeignKey(canceledCtx, schema, "orders", "customer_id")
	if err == nil {
		t.Fatal("expected a non-nil error for a genuine query failure (canceled context), got nil")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected a genuine query error, not pgx.ErrNoRows: %v", err)
	}
	if found {
		t.Fatal("expected found=false when the lookup query itself fails")
	}
}

// --- enum column widen/narrow (CENUM-07 .. CENUM-12) ---

// enumTestTable creates a schema with a single table carrying an enum column
// provisioned through the real createTable path, so these tests exercise the
// same CHECK constraint the creation path emits (columnDDL).
func enumTestTable(t *testing.T, prefix string, allowed []string) (*db.Pool, string) {
	t.Helper()
	pool := ensureRLSTestPool(t)
	t.Cleanup(pool.Close)

	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	})

	if _, err := New(pool).createTable(ctx, schema, "assets", []config.ColumnConfig{
		{Name: "name", Type: "text"},
		{Name: "status", Type: "enum", AllowedValues: allowed},
	}, "disabled"); err != nil {
		t.Fatalf("createTable: %v", err)
	}
	return pool, schema
}

// checkConstraintDef returns the current CHECK constraint definition on
// assets.status, or "" when there is none.
func checkConstraintDef(t *testing.T, pool *db.Pool, schema, table, column string) string {
	t.Helper()
	var def string
	err := pool.QueryRow(context.Background(),
		`SELECT pg_get_constraintdef(c.oid)
		 FROM pg_constraint c
		 JOIN pg_class r ON r.oid = c.conrelid
		 JOIN pg_namespace n ON n.oid = r.relnamespace
		 JOIN pg_attribute a ON a.attrelid = r.oid AND a.attname = $3
		 WHERE c.contype = 'c' AND n.nspname = $1 AND r.relname = $2 AND c.conkey = ARRAY[a.attnum]`,
		schema, table, column,
	).Scan(&def)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read check constraint def: %v", err)
	}
	return def
}

// insertStatus attempts to write a status value, reporting whether Postgres
// accepted it. This is the real enforcement assertion: the constraint is
// only meaningful if out-of-set writes are rejected by the database.
func insertStatus(t *testing.T, pool *db.Pool, schema, status string) error {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		fmt.Sprintf(`INSERT INTO %q.assets (name, status) VALUES ('x', $1)`, schema), status)
	return err
}

// CENUM-07 / CENUM-08: widening to a superset replaces the constraint with
// the larger set, needs no data migration, and leaves existing rows
// untouched and valid.
func TestReplaceColumnEnumValues_WidenAddsValue(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_widen_test", []string{"pending", "active"})
	ctx := context.Background()
	p := New(pool)

	if err := insertStatus(t, pool, schema, "active"); err != nil {
		t.Fatalf("seed existing row: %v", err)
	}
	if err := insertStatus(t, pool, schema, "closed"); err == nil {
		t.Fatal("expected 'closed' to be rejected before widening")
	}

	if err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending", "active"}, []string{"pending", "active", "closed"}); err != nil {
		t.Fatalf("ReplaceColumnEnumValues (widen): %v", err)
	}

	// The previously rejected value is now accepted.
	if err := insertStatus(t, pool, schema, "closed"); err != nil {
		t.Fatalf("expected 'closed' to be accepted after widening, got: %v", err)
	}
	// An out-of-set value is still rejected: the constraint was replaced, not dropped.
	if err := insertStatus(t, pool, schema, "qualquer coisa"); err == nil {
		t.Fatal("expected an out-of-set value to still be rejected after widening")
	}
	// CENUM-08: the pre-existing row is unaffected and still valid.
	var count int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.assets WHERE status = 'active'`, schema)).Scan(&count); err != nil {
		t.Fatalf("count existing rows: %v", err)
	}
	if count != 1 {
		t.Errorf("existing row count = %d, want 1 (widening must not touch data)", count)
	}
}

// CENUM-09 / CENUM-11: narrowing when no row holds a removed value replaces
// the constraint with the narrowed set.
func TestReplaceColumnEnumValues_NarrowWithNoRowsUsingRemovedValue(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_narrow_ok_test", []string{"pending", "active", "closed"})
	ctx := context.Background()
	p := New(pool)

	if err := insertStatus(t, pool, schema, "active"); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending", "active", "closed"}, []string{"pending", "active"}); err != nil {
		t.Fatalf("ReplaceColumnEnumValues (narrow): %v", err)
	}

	if err := insertStatus(t, pool, schema, "closed"); err == nil {
		t.Fatal("expected the removed value 'closed' to be rejected after narrowing")
	}
	if err := insertStatus(t, pool, schema, "pending"); err != nil {
		t.Fatalf("expected a remaining value to still be accepted, got: %v", err)
	}
}

// CENUM-09 / CENUM-10: narrowing away a value still held by rows is rejected
// with a typed *EnumValueInUseError naming the value and its exact count.
// Rows holding values that are NOT being removed must not be reported (the
// pre-check is scoped to the removed values only).
func TestReplaceColumnEnumValues_NarrowRejectedForInUseValue(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_narrow_inuse_test", []string{"pending", "active", "closed"})
	ctx := context.Background()
	p := New(pool)

	if err := insertStatus(t, pool, schema, "closed"); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// Two rows on a value that stays: must not appear in the error.
	for i := 0; i < 2; i++ {
		if err := insertStatus(t, pool, schema, "active"); err != nil {
			t.Fatalf("seed kept-value row: %v", err)
		}
	}

	err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending", "active", "closed"}, []string{"pending", "active"})
	if err == nil {
		t.Fatal("expected an error when narrowing away an in-use value, got nil")
	}
	var inUse *EnumValueInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("expected *EnumValueInUseError, got %T (%v)", err, err)
	}
	if inUse.Column != "status" {
		t.Errorf("Column = %q, want %q", inUse.Column, "status")
	}
	if got, want := inUse.Counts["closed"], 1; got != want {
		t.Errorf(`Counts["closed"] = %d, want %d`, got, want)
	}
	if _, reported := inUse.Counts["active"]; reported {
		t.Errorf("Counts must only cover removed values, got %v", inUse.Counts)
	}
	if !strings.Contains(err.Error(), `"closed" is used by 1 row(s)`) {
		t.Errorf("error message must name the value and count, got: %q", err.Error())
	}
}

// CENUM-10: every offending value is reported with its own count, not just
// the first one found.
func TestReplaceColumnEnumValues_NarrowRejectionNamesEveryInUseValue(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_narrow_multi_test", []string{"pending", "active", "closed", "archived"})
	ctx := context.Background()
	p := New(pool)

	if err := insertStatus(t, pool, schema, "closed"); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := insertStatus(t, pool, schema, "archived"); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status",
		[]string{"pending", "active", "closed", "archived"}, []string{"pending", "active"})
	var inUse *EnumValueInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("expected *EnumValueInUseError, got %T (%v)", err, err)
	}
	if got, want := inUse.Counts["closed"], 1; got != want {
		t.Errorf(`Counts["closed"] = %d, want %d`, got, want)
	}
	if got, want := inUse.Counts["archived"], 3; got != want {
		t.Errorf(`Counts["archived"] = %d, want %d`, got, want)
	}
	if len(inUse.Counts) != 2 {
		t.Errorf("Counts = %v, want exactly the two in-use removed values", inUse.Counts)
	}
}

// CENUM-12: a rejected narrow never leaves the table partially migrated —
// the existing CHECK constraint is byte-for-byte unchanged and still
// enforcing the original set.
func TestReplaceColumnEnumValues_RejectedNarrowLeavesConstraintIntact(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_narrow_intact_test", []string{"pending", "active", "closed"})
	ctx := context.Background()
	p := New(pool)

	if err := insertStatus(t, pool, schema, "closed"); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	before := checkConstraintDef(t, pool, schema, "assets", "status")
	if before == "" {
		t.Fatal("expected a CHECK constraint on assets.status after createTable")
	}

	err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending", "active", "closed"}, []string{"pending", "active"})
	var inUse *EnumValueInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("expected *EnumValueInUseError, got %T (%v)", err, err)
	}

	if after := checkConstraintDef(t, pool, schema, "assets", "status"); after != before {
		t.Errorf("constraint changed after a rejected narrow:\n before = %q\n after  = %q", before, after)
	}
	// Still enforcing the original set: the value we tried to remove is
	// writable, an out-of-set value is not.
	if err := insertStatus(t, pool, schema, "closed"); err != nil {
		t.Errorf("expected 'closed' to still be writable after a rejected narrow, got: %v", err)
	}
	if err := insertStatus(t, pool, schema, "qualquer coisa"); err == nil {
		t.Error("expected an out-of-set value to still be rejected after a rejected narrow")
	}
}

// CENUM-07: the replaced constraint is itself re-locatable via catalog
// lookup, so a second widen/narrow works on the constraint the first one
// created (the reason the design mandates catalog lookup over an assumed
// name). Also covers a free-text value with a single quote surviving the
// replace path's escaping.
func TestReplaceColumnEnumValues_SecondReplaceFindsTheNewConstraint(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_replace_twice_test", []string{"pending"})
	ctx := context.Background()
	p := New(pool)

	if err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending"}, []string{"pending", "Em andamento"}); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	if err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending", "Em andamento"}, []string{"pending", "Em andamento", "O'Brien"}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	// Exactly one CHECK constraint on the column — the second replace must
	// have dropped the first one, not stacked a new one on top.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_constraint c
		 JOIN pg_class r ON r.oid = c.conrelid
		 JOIN pg_namespace nsp ON nsp.oid = r.relnamespace
		 JOIN pg_attribute a ON a.attrelid = r.oid AND a.attname = 'status'
		 WHERE c.contype = 'c' AND nsp.nspname = $1 AND r.relname = 'assets' AND c.conkey = ARRAY[a.attnum]`,
		schema,
	).Scan(&n); err != nil {
		t.Fatalf("count check constraints: %v", err)
	}
	if n != 1 {
		t.Errorf("check constraint count = %d, want 1", n)
	}

	for _, v := range []string{"pending", "Em andamento", "O'Brien"} {
		if err := insertStatus(t, pool, schema, v); err != nil {
			t.Errorf("expected %q to be accepted after two replaces, got: %v", v, err)
		}
	}
	if err := insertStatus(t, pool, schema, "qualquer coisa"); err == nil {
		t.Error("expected an out-of-set value to be rejected after two replaces")
	}
}

// TestReplaceColumnEnumValues_AmbiguousConstraintFailsClosed covers a gap
// found by the v1.6.0..HEAD release-readiness audit: findColumnCheckConstraint
// used to pick an arbitrary single-column CHECK via LIMIT 1 with no ORDER BY
// when a column had more than one (e.g. a hand-added constraint alongside
// the enum one), risking a DROP CONSTRAINT on the wrong one. It must now
// fail closed instead of guessing, and leave both constraints untouched.
func TestReplaceColumnEnumValues_AmbiguousConstraintFailsClosed(t *testing.T) {
	pool, schema := enumTestTable(t, "enum_ambiguous_test", []string{"pending", "active"})
	ctx := context.Background()
	p := New(pool)

	// A second, unrelated single-column CHECK on the same column — plausible
	// if an app owner (schema-per-app, so they have DDL access) added their
	// own length constraint alongside the enum one.
	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`ALTER TABLE %q.%q ADD CONSTRAINT status_length_check CHECK (length("status") < 100)`, schema, "assets"),
	); err != nil {
		t.Fatalf("add second check constraint: %v", err)
	}

	before := checkConstraintDef(t, pool, schema, "assets", "status")

	err := p.ReplaceColumnEnumValues(ctx, schema, "assets", "status", []string{"pending", "active"}, []string{"pending", "active", "closed"})
	if err == nil {
		t.Fatal("expected ReplaceColumnEnumValues to fail closed when the column has 2 single-column CHECK constraints")
	}
	if !strings.Contains(err.Error(), "cannot determine which one") {
		t.Errorf("expected an ambiguity error, got: %v", err)
	}

	// Neither constraint was touched by the failed attempt.
	after := checkConstraintDef(t, pool, schema, "assets", "status")
	if before != after {
		t.Errorf("enum constraint changed on an ambiguous/failed request:\n before = %q\n after  = %q", before, after)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_constraint c
		 JOIN pg_class r ON r.oid = c.conrelid
		 JOIN pg_namespace nsp ON nsp.oid = r.relnamespace
		 JOIN pg_attribute a ON a.attrelid = r.oid AND a.attname = 'status'
		 WHERE c.contype = 'c' AND nsp.nspname = $1 AND r.relname = 'assets' AND c.conkey = ARRAY[a.attnum]`,
		schema,
	).Scan(&n); err != nil {
		t.Fatalf("count check constraints: %v", err)
	}
	if n != 2 {
		t.Errorf("expected both check constraints to still exist, got %d", n)
	}
}

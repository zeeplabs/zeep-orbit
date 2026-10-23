package provisioner

// ddl_lock_test.go — multi-replica-consistency T10: every mutating
// provisioner method runs in one transaction under the per-app DDL lock,
// with lock waits bounded by db.DDLLockTimeout.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func shortDDLLockTimeout(t *testing.T) {
	t.Helper()
	prev := db.DDLLockTimeout
	db.DDLLockTimeout = 300 * time.Millisecond
	t.Cleanup(func() { db.DDLLockTimeout = prev })
}

func columnExists(t *testing.T, pool *db.Pool, schema, table, column string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
		schema, table, column,
	).Scan(&exists); err != nil {
		t.Fatalf("check column %s.%s.%s: %v", schema, table, column, err)
	}
	return exists
}

func tableExists(t *testing.T, pool *db.Pool, schema, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
		schema, table,
	).Scan(&exists); err != nil {
		t.Fatalf("check table %s.%s: %v", schema, table, err)
	}
	return exists
}

func assetsConfig(schema string, cols ...config.ColumnConfig) *config.Config {
	return &config.Config{Apps: []config.AppConfig{{
		Name:   schema,
		Tables: []config.TableConfig{{Name: "assets", Columns: cols}},
	}}}
}

// A long-running reader holds ACCESS SHARE on the table: the ALTER must give
// up after DDLLockTimeout instead of queueing (and blocking every later query
// on the table behind it), and nothing must be left half-applied.
func TestApply_TableLockedByReaderTimesOut(t *testing.T) {
	base := config.ColumnConfig{Name: "title", Type: "text"}
	pool, schema := constraintTestTable(t, "ddl_reader", []config.ColumnConfig{base})
	ctx := context.Background()
	shortDDLLockTimeout(t)

	reader, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("reader begin: %v", err)
	}
	defer reader.Rollback(ctx)
	if _, err := reader.Exec(ctx, fmt.Sprintf(`SELECT * FROM %q.assets`, schema)); err != nil {
		t.Fatalf("reader select: %v", err)
	}

	start := time.Now()
	_, err = New(pool).Apply(ctx, assetsConfig(schema, base, config.ColumnConfig{Name: "price", Type: "integer"}))
	if !errors.Is(err, ErrDDLLockTimeout) {
		t.Fatalf("expected ErrDDLLockTimeout, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Apply took %s, expected a bounded wait", elapsed)
	}
	if err := reader.Rollback(ctx); err != nil {
		t.Fatalf("reader rollback: %v", err)
	}
	if columnExists(t, pool, schema, "assets", "price") {
		t.Fatal("the column must not exist after a timed-out Apply")
	}
}

// Another request holds the same app's DDL lock: Apply times out.
func TestApply_SameAppSerialized(t *testing.T) {
	base := config.ColumnConfig{Name: "title", Type: "text"}
	pool, schema := constraintTestTable(t, "ddl_same", []config.ColumnConfig{base})
	ctx := context.Background()
	shortDDLLockTimeout(t)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if err := db.LockAppDDL(ctx, holder, schema); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	_, err = New(pool).Apply(ctx, assetsConfig(schema, base, config.ColumnConfig{Name: "price", Type: "integer"}))
	if !errors.Is(err, ErrDDLLockTimeout) {
		t.Fatalf("expected ErrDDLLockTimeout while the app lock is held, got: %v", err)
	}

	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}
	if _, err := New(pool).Apply(ctx, assetsConfig(schema, base, config.ColumnConfig{Name: "price", Type: "integer"})); err != nil {
		t.Fatalf("Apply after the lock is released: %v", err)
	}
	if !columnExists(t, pool, schema, "assets", "price") {
		t.Fatal("price should exist after the retry")
	}
}

// Two concurrent Applies on the same app: the second waits for the first
// (well inside the timeout) and both succeed, in order.
func TestApply_ConcurrentSameAppBothSucceed(t *testing.T) {
	base := config.ColumnConfig{Name: "title", Type: "text"}
	pool, schema := constraintTestTable(t, "ddl_conc_same", []config.ColumnConfig{base})
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, col := range []string{"a", "b"} {
		wg.Add(1)
		go func(i int, col string) {
			defer wg.Done()
			_, errs[i] = New(pool).Apply(ctx, assetsConfig(schema, base, config.ColumnConfig{Name: col, Type: "text"}))
		}(i, col)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Apply %d: %v", i, err)
		}
	}
}

// Different apps don't share a lock: one app's held lock doesn't block
// another app's Apply.
func TestApply_DifferentAppsDoNotBlock(t *testing.T) {
	base := config.ColumnConfig{Name: "title", Type: "text"}
	pool, schemaA := constraintTestTable(t, "ddl_app_a", []config.ColumnConfig{base})
	_, schemaB := constraintTestTable(t, "ddl_app_b", []config.ColumnConfig{base})
	ctx := context.Background()
	shortDDLLockTimeout(t)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if err := db.LockAppDDL(ctx, holder, schemaA); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	if _, err := New(pool).Apply(ctx, assetsConfig(schemaB, base, config.ColumnConfig{Name: "price", Type: "integer"})); err != nil {
		t.Fatalf("Apply on app B while app A is locked: %v", err)
	}
	if !columnExists(t, pool, schemaB, "assets", "price") {
		t.Fatal("app B's column should exist")
	}
}

// A failure on a later statement rolls back the earlier ones of the same
// Apply: the first table is not left behind.
func TestApply_FailureLeavesNoPartialDDL(t *testing.T) {
	pool := ensureRLSTestPool(t)
	ctx := context.Background()
	schema := fmt.Sprintf("ddl_partial_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
		pool.Close()
	})

	cfg := &config.Config{Apps: []config.AppConfig{{
		Name: schema,
		Tables: []config.TableConfig{
			{Name: "first", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}}},
			// An integer column with a non-numeric default fails CREATE TABLE.
			{Name: "second", Columns: []config.ColumnConfig{{Name: "n", Type: "integer", Default: "not-a-number"}}},
		},
	}}}
	_, err := New(pool).Apply(ctx, cfg)
	if err == nil {
		t.Fatal("Apply should fail on the invalid default")
	}
	// The failure must come from Postgres on the second table, after the
	// first one was created inside the transaction; a pre-DDL validation
	// error would make this test vacuous.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.Contains(err.Error(), `"second"`) {
		t.Fatalf("expected a Postgres error on table second, got: %v", err)
	}
	if tableExists(t, pool, schema, "first") {
		t.Fatal("the first table must be rolled back when a later statement fails")
	}
	var schemaExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema).Scan(&schemaExists); err != nil {
		t.Fatalf("check schema: %v", err)
	}
	if schemaExists {
		t.Fatal("the schema created by the failed Apply must be rolled back too")
	}
}

// ApplyColumnConstraintChanges opens its own transaction; inside a locked
// provisioner that Begin is a savepoint, and the nested call must not try
// to take the app lock again.
func TestApplyColumnConstraintChanges_NestedInAppLock(t *testing.T) {
	old := config.ColumnConfig{Name: "serial", Type: "text", Required: true}
	pool, schema := constraintTestTable(t, "ddl_nested", []config.ColumnConfig{old})
	ctx := context.Background()
	shortDDLLockTimeout(t)

	updated := old
	updated.Required = false
	err := New(pool).withAppLock(ctx, schema, func(tp *Provisioner) error {
		_, err := tp.ApplyColumnConstraintChanges(ctx, schema, "assets", []ColumnConstraintChange{{Old: old, New: updated}})
		return err
	})
	if err != nil {
		t.Fatalf("nested ApplyColumnConstraintChanges: %v", err)
	}
	if columnIsNotNull(t, pool, schema, "serial") {
		t.Fatal("serial is still NOT NULL after the nested change committed")
	}
}

// EnsureAuthUserColumns must not touch the table (no ACCESS EXCLUSIVE) when
// every column already exists: with a reader holding the table and the app
// lock held elsewhere, it still returns immediately.
func TestEnsureAuthUserColumns_NoDDLWhenPresent(t *testing.T) {
	pool := ensureRLSTestPool(t)
	ctx := context.Background()
	schema := fmt.Sprintf("ddl_auth_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
		pool.Close()
	})
	p := New(pool)
	if err := p.EnsureAuthTables(ctx, schema); err != nil {
		t.Fatalf("EnsureAuthTables: %v", err)
	}
	if err := p.EnsureAuthUserColumns(ctx, schema); err != nil {
		t.Fatalf("first EnsureAuthUserColumns: %v", err)
	}
	shortDDLLockTimeout(t)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if err := db.LockAppDDL(ctx, holder, schema); err != nil {
		t.Fatalf("holder lock: %v", err)
	}
	if _, err := holder.Exec(ctx, fmt.Sprintf(`SELECT * FROM %q."_auth_users"`, schema)); err != nil {
		t.Fatalf("holder select: %v", err)
	}

	if err := p.EnsureAuthUserColumns(ctx, schema); err != nil {
		t.Fatalf("EnsureAuthUserColumns with columns present should not need the lock: %v", err)
	}
}

package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func ddlLockTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func setDDLLockTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := db.DDLLockTimeout
	db.DDLLockTimeout = d
	t.Cleanup(func() { db.DDLLockTimeout = prev })
}

func TestIsLockTimeout(t *testing.T) {
	if !db.IsLockTimeout(&pgconn.PgError{Code: "55P03"}) {
		t.Fatal("55P03 (lock_not_available) should be detected as a lock timeout")
	}
	if !db.IsLockTimeout(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "55P03"})) {
		t.Fatal("a wrapped 55P03 should be detected as a lock timeout")
	}
	if db.IsLockTimeout(&pgconn.PgError{Code: "57014"}) {
		t.Fatal("57014 (statement timeout) must not be detected as a lock timeout")
	}
	if db.IsLockTimeout(errors.New("plain error")) {
		t.Fatal("a non-Postgres error must not be detected as a lock timeout")
	}
	if db.IsLockTimeout(nil) {
		t.Fatal("nil must not be detected as a lock timeout")
	}
}

// TestLockAppDDL_TimesOutOnSameSchema proves the design assumption that
// lock_timeout also bounds a pg_advisory_xact_lock wait, not just table
// locks. If this ever fails, LockAppDDL needs a pg_try_advisory_xact_lock
// loop with a deadline instead.
func TestLockAppDDL_TimesOutOnSameSchema(t *testing.T) {
	pool := ddlLockTestPool(t)
	ctx := context.Background()
	const schema = "ddl_lock_test_same"

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if err := db.LockAppDDL(ctx, holder, schema); err != nil {
		t.Fatalf("holder LockAppDDL: %v", err)
	}

	setDDLLockTimeout(t, 300*time.Millisecond)
	waiter, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("waiter begin: %v", err)
	}
	defer waiter.Rollback(ctx)

	start := time.Now()
	err = db.LockAppDDL(ctx, waiter, schema)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("LockAppDDL on a held schema lock should fail once lock_timeout expires")
	}
	if !db.IsLockTimeout(err) {
		t.Fatalf("expected a lock timeout (55P03), got: %v", err)
	}
	if elapsed < 250*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("expected the wait to be bounded by ~300ms, took %s", elapsed)
	}
}

func TestLockAppDDL_DifferentSchemaDoesNotWait(t *testing.T) {
	pool := ddlLockTestPool(t)
	ctx := context.Background()

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if err := db.LockAppDDL(ctx, holder, "ddl_lock_test_a"); err != nil {
		t.Fatalf("holder LockAppDDL: %v", err)
	}

	setDDLLockTimeout(t, 300*time.Millisecond)
	other, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("other begin: %v", err)
	}
	defer other.Rollback(ctx)

	start := time.Now()
	if err := db.LockAppDDL(ctx, other, "ddl_lock_test_b"); err != nil {
		t.Fatalf("LockAppDDL on a different schema should not wait, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("LockAppDDL on a different schema took %s, expected no wait", elapsed)
	}
}

func TestLockAppDDL_ReleasedAndNotLeakedAfterCommit(t *testing.T) {
	pool := ddlLockTestPool(t)
	ctx := context.Background()
	const schema = "ddl_lock_test_release"

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	var serverDefault string
	if err := conn.QueryRow(ctx, "SHOW lock_timeout").Scan(&serverDefault); err != nil {
		t.Fatalf("show lock_timeout before: %v", err)
	}

	setDDLLockTimeout(t, 1234*time.Millisecond)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := db.LockAppDDL(ctx, tx, schema); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("LockAppDDL: %v", err)
	}
	var inside string
	if err := tx.QueryRow(ctx, "SHOW lock_timeout").Scan(&inside); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("show lock_timeout inside: %v", err)
	}
	if inside == serverDefault {
		tx.Rollback(ctx)
		t.Fatalf("lock_timeout inside the transaction should differ from the default %q", serverDefault)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var after string
	if err := conn.QueryRow(ctx, "SHOW lock_timeout").Scan(&after); err != nil {
		t.Fatalf("show lock_timeout after: %v", err)
	}
	if after != serverDefault {
		t.Fatalf("lock_timeout leaked to the pooled connection: got %q, want %q", after, serverDefault)
	}

	// The advisory lock must be released at commit: another session gets it
	// immediately.
	setDDLLockTimeout(t, 300*time.Millisecond)
	next, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("next begin: %v", err)
	}
	defer next.Rollback(ctx)
	if err := db.LockAppDDL(ctx, next, schema); err != nil {
		t.Fatalf("advisory lock was not released at commit: %v", err)
	}
}

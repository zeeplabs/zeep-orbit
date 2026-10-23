package dashboard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func TestPurgeExpiredSoftDeletesOffByDefault(t *testing.T) {
	n, err := PurgeExpiredSoftDeletes(nil, nil, nil, 0)
	if err != nil || n != 0 {
		t.Fatalf("expected no-op when retentionDays<=0, got n=%d err=%v", n, err)
	}
}

func TestPurgeExpiredSoftDeletesNegativeIsOff(t *testing.T) {
	n, err := PurgeExpiredSoftDeletes(nil, nil, nil, -5)
	if err != nil || n != 0 {
		t.Fatalf("expected no-op when retentionDays<0, got n=%d err=%v", n, err)
	}
}

// purgeTestPool connects to the real test DB, provisions a fresh schema,
// and registers one app/table with the soft-delete deleted_at column — this
// exercises the actual DELETE path (identifier quoting, interval cast),
// which the two no-op tests above never touch.
func purgeTestPool(t *testing.T) (*db.Pool, *registry.Registry) {
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
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		t.Fatalf("drop zeep_system: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	const schema = "purge_test_app"
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		t.Fatalf("drop app schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create app schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE `+schema+`.items (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT NOT NULL,
		deleted_at TIMESTAMPTZ
	)`); err != nil {
		t.Fatalf("create items table: %v", err)
	}

	reg := registry.New()
	reg.Register(&registry.App{
		Config:     config.AppConfig{Name: "purge-test-app"},
		SchemaName: schema,
		Tables: map[string]*registry.Table{
			"items": {Name: "items"},
		},
	})
	return pool, reg
}

func TestPurgeExpiredSoftDeletesDeletesOnlyExpiredRows(t *testing.T) {
	pool, reg := purgeTestPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO purge_test_app.items (name, deleted_at) VALUES
		 ('old-deleted', now() - interval '10 days'),
		 ('recent-deleted', now() - interval '1 hour'),
		 ('not-deleted', NULL)`,
	); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	n, err := PurgeExpiredSoftDeletes(ctx, pool, reg, 5)
	if err != nil {
		t.Fatalf("PurgeExpiredSoftDeletes: %v", err)
	}
	if n != 1 {
		t.Errorf("rows deleted = %d, want 1 (only the 10-day-old soft-deleted row)", n)
	}

	var remaining int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purge_test_app.items`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Errorf("remaining rows = %d, want 2 (recent-deleted + not-deleted)", remaining)
	}

	var auditCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'system.purge.run'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for system.purge.run = %d, want 1", auditCount)
	}
}

func TestPurgeExpiredSoftDeletesZeroRowRunStillAudits(t *testing.T) {
	pool, reg := purgeTestPool(t)
	ctx := context.Background()

	n, err := PurgeExpiredSoftDeletes(ctx, pool, reg, 5)
	if err != nil {
		t.Fatalf("PurgeExpiredSoftDeletes: %v", err)
	}
	if n != 0 {
		t.Errorf("rows deleted = %d, want 0", n)
	}

	var auditCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'system.purge.run'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for zero-row run = %d, want 1", auditCount)
	}
}

func TestPurgeExpiredSoftDeletesSecondReplicaSkipsWhileLocked(t *testing.T) {
	pool, reg := purgeTestPool(t)
	ctx := context.Background()

	// Hold the advisory lock on a second connection to simulate a
	// concurrent replica already running this window's purge.
	dsn := os.Getenv("TEST_DATABASE_URL")
	other, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect second pool: %v", err)
	}
	defer other.Close()
	var gotLock bool
	if err := other.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, purgeAdvisoryLockKey).Scan(&gotLock); err != nil {
		t.Fatalf("acquire lock on other pool: %v", err)
	}
	if !gotLock {
		t.Fatal("expected to acquire the lock on the second pool")
	}
	defer other.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, purgeAdvisoryLockKey)

	if _, err := pool.Exec(ctx,
		`INSERT INTO purge_test_app.items (name, deleted_at) VALUES ('old-deleted', now() - interval '10 days')`,
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	n, err := PurgeExpiredSoftDeletes(ctx, pool, reg, 5)
	if err != nil {
		t.Fatalf("PurgeExpiredSoftDeletes should no-op, not error, when lock is held: %v", err)
	}
	if n != 0 {
		t.Errorf("rows deleted = %d, want 0 (lock held by another replica)", n)
	}

	var remaining int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purge_test_app.items`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining rows = %d, want 1 (purge should not have run)", remaining)
	}
}

// TestPurgeExpiredSoftDeletesUnaffectedByActiveDenyAllPolicy is a regression
// test for end-user-row-policies T9 (spec ROWPOL-15): PurgeExpiredSoftDeletes
// runs on the principal/owner pool and must never be filtered by a native
// RLS policy, even a deny-all one for an unrelated role — enabling RLS on a
// table must not silently break the retention job.
func TestPurgeExpiredSoftDeletesUnaffectedByActiveDenyAllPolicy(t *testing.T) {
	pool, reg := purgeTestPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `ALTER TABLE purge_test_app.items ENABLE ROW LEVEL SECURITY`); err != nil {
		t.Fatalf("enable row level security: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`CREATE POLICY deny_all ON purge_test_app.items FOR ALL TO PUBLIC USING (false)`,
	); err != nil {
		t.Fatalf("create deny-all policy: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO purge_test_app.items (name, deleted_at) VALUES
		 ('old-deleted', now() - interval '10 days'),
		 ('recent-deleted', now() - interval '1 hour'),
		 ('not-deleted', NULL)`,
	); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	n, err := PurgeExpiredSoftDeletes(ctx, pool, reg, 5)
	if err != nil {
		t.Fatalf("PurgeExpiredSoftDeletes: %v", err)
	}
	if n != 1 {
		t.Errorf("rows deleted = %d, want 1 (RLS must not filter the principal/owner role's purge query)", n)
	}

	var remaining int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purge_test_app.items`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Errorf("remaining rows = %d, want 2 (recent-deleted + not-deleted)", remaining)
	}
}

// TestPurgeExpiredSoftDeletesReleasesLock (multi-replica-consistency T15):
// the session lock is taken and released on the same connection, so after a
// run no connection keeps holding it and the next run (on any replica)
// gets it. Repeated runs exercise different pooled connections.
func TestPurgeExpiredSoftDeletesReleasesLock(t *testing.T) {
	pool, reg := purgeTestPool(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := PurgeExpiredSoftDeletes(ctx, pool, reg, 5); err != nil {
			t.Fatalf("purge run %d: %v", i, err)
		}
		var held int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_locks
			 WHERE locktype = 'advisory' AND granted AND objsubid = 1
			   AND objid = (hashtext($1)::bigint & 4294967295)::oid`,
			purgeAdvisoryLockKey,
		).Scan(&held); err != nil {
			t.Fatalf("count purge locks: %v", err)
		}
		if held != 0 {
			t.Fatalf("after run %d the purge advisory lock is still held by %d session(s)", i, held)
		}
	}

	// Another session (another replica) can take it right away.
	other, err := db.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect second pool: %v", err)
	}
	defer other.Close()
	conn, err := other.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, purgeAdvisoryLockKey).Scan(&got); err != nil {
		t.Fatalf("try lock: %v", err)
	}
	if !got {
		t.Fatal("a second replica could not take the purge lock after the run")
	}
	_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, purgeAdvisoryLockKey)
}

// TestPurgeExpiredSoftDeletesWithOneConnectionPool: the purge holds one
// connection for its advisory lock. With DB_MAX_CONNS=1, asking the pool for
// a second one (DELETEs, audit insert) waited forever.
func TestPurgeExpiredSoftDeletesWithOneConnectionPool(t *testing.T) {
	_, reg := purgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	small, err := db.NewWithMaxConns(ctx, os.Getenv("TEST_DATABASE_URL"), 1)
	if err != nil {
		t.Fatalf("NewWithMaxConns(1): %v", err)
	}
	defer small.Close()

	if _, err := small.Exec(ctx,
		`INSERT INTO purge_test_app.items (name, deleted_at) VALUES ('old-deleted', now() - interval '10 days')`,
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	n, err := PurgeExpiredSoftDeletes(ctx, small, reg, 5)
	if err != nil {
		t.Fatalf("purge on a one-connection pool: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows deleted = %d, want 1", n)
	}
}

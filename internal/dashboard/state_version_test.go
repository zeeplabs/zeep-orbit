package dashboard

// state_version_test.go — multi-replica-consistency T3: the
// zeep_system.state_version row and the triggers that bump it. Every pod
// polls this row to know when to reload its in-memory registry, so a write
// that skips the bump leaves other replicas stale, and a hot-path write that
// bumps it makes every pod reload on every request.
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func stateVersionTestPool(t *testing.T) *db.Pool {
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
	return pool
}

func readStateVersion(t *testing.T, pool *db.Pool) int64 {
	t.Helper()
	var v int64
	if err := pool.QueryRow(context.Background(),
		`SELECT version FROM zeep_system.state_version WHERE id = 1`).Scan(&v); err != nil {
		t.Fatalf("read state_version: %v", err)
	}
	return v
}

func TestProvisionZeepSystem_StateVersionIdempotent(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("second ProvisionZeepSystem: %v", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.state_version`).Scan(&rows); err != nil {
		t.Fatalf("count state_version: %v", err)
	}
	if rows != 1 {
		t.Fatalf("state_version should hold exactly one row, got %d", rows)
	}

	for _, tbl := range []string{"apps", "app_tables", "system_config", "app_tokens"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_trigger
			 WHERE tgname = 'bump_state_version' AND tgrelid = format('zeep_system.%I', $1::text)::regclass`,
			tbl).Scan(&n); err != nil {
			t.Fatalf("count triggers on %s: %v", tbl, err)
		}
		if n != 1 {
			t.Fatalf("expected exactly one bump_state_version trigger on %s, got %d", tbl, n)
		}
	}
}

func TestStateVersion_BumpedByRegistryWrites(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("statever-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	steps := []struct {
		name string
		sql  string
	}{
		{"insert apps", `INSERT INTO zeep_system.apps (name, owner_id) VALUES ('statever_app', '` + owner.ID + `')`},
		{"update apps", `UPDATE zeep_system.apps SET jwt_secret = 'rotated' WHERE name = 'statever_app'`},
		{"insert app_tables", `INSERT INTO zeep_system.app_tables (app_id, name) SELECT id, 'items' FROM zeep_system.apps WHERE name = 'statever_app'`},
		{"update app_tables", `UPDATE zeep_system.app_tables SET rls = 'owner' WHERE name = 'items'`},
		{"delete app_tables", `DELETE FROM zeep_system.app_tables WHERE name = 'items'`},
		{"update system_config", `UPDATE zeep_system.system_config SET soft_delete_enabled = NOT soft_delete_enabled`},
		{"insert app_tokens (setup, not asserted)", `INSERT INTO zeep_system.app_tokens (app_id, name, jti) SELECT id, 't', 'statever-jti' FROM zeep_system.apps WHERE name = 'statever_app'`},
		{"revoke app_tokens", `UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE jti = 'statever-jti'`},
		{"delete app_tokens", `DELETE FROM zeep_system.app_tokens WHERE jti = 'statever-jti'`},
		{"delete apps", `DELETE FROM zeep_system.apps WHERE name = 'statever_app'`},
	}
	for _, s := range steps {
		before := readStateVersion(t, pool)
		if _, err := pool.Exec(ctx, s.sql); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if s.name == "insert app_tokens (setup, not asserted)" {
			continue
		}
		if after := readStateVersion(t, pool); after <= before {
			t.Fatalf("%s should bump state_version (before %d, after %d)", s.name, before, after)
		}
	}
}

func TestStateVersion_NotBumpedByHotPathTokenWrites(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("statever-hot-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ('statever_hot', $1)`, owner.ID); err != nil {
		t.Fatalf("insert app: %v", err)
	}

	before := readStateVersion(t, pool)
	for _, sql := range []string{
		`INSERT INTO zeep_system.app_tokens (app_id, name, jti) SELECT id, 't', 'hot-jti' FROM zeep_system.apps WHERE name = 'statever_hot'`,
		`UPDATE zeep_system.app_tokens SET last_used_at = now() WHERE jti = 'hot-jti'`,
		`UPDATE zeep_system.app_tokens SET expires_at = now() + interval '1 hour' WHERE jti = 'hot-jti'`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	if after := readStateVersion(t, pool); after != before {
		t.Fatalf("token insert, last_used_at and expires_at writes must not bump state_version (before %d, after %d)", before, after)
	}
}

// TestProvisionZeepSystem_BoundedByLockTimeout covers T4: a session holding
// ACCESS EXCLUSIVE on a zeep_system table must make boot provisioning fail
// after its retries, not hang.
func TestProvisionZeepSystem_BoundedByLockTimeout(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	prevTimeout, prevBackoff := provisionLockTimeout, provisionBackoff
	provisionLockTimeout = 200 * time.Millisecond
	provisionBackoff = []time.Duration{50 * time.Millisecond, 50 * time.Millisecond}
	t.Cleanup(func() { provisionLockTimeout, provisionBackoff = prevTimeout, prevBackoff })

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `LOCK TABLE zeep_system.apps IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock apps: %v", err)
	}

	start := time.Now()
	err = ProvisionZeepSystem(ctx, pool)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ProvisionZeepSystem should fail while zeep_system.apps is locked")
	}
	if !db.IsLockTimeout(err) {
		t.Fatalf("expected a wrapped lock timeout, got: %v", err)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("expected 3 attempts, got: %v", err)
	}
	// 3 × 200ms waits + 2 × 50ms backoff ≈ 700ms; allow slack, but not a hang.
	if elapsed > 5*time.Second {
		t.Fatalf("ProvisionZeepSystem took %s, expected a bounded failure", elapsed)
	}

	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("ProvisionZeepSystem after the lock is released: %v", err)
	}
}

func TestStateVersion_RollbackLeavesVersionUnchanged(t *testing.T) {
	pool := stateVersionTestPool(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("statever-rb-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	before := readStateVersion(t, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ('statever_rb', $1)`, owner.ID); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("insert app: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if after := readStateVersion(t, pool); after != before {
		t.Fatalf("a rolled-back write must not move state_version (before %d, after %d)", before, after)
	}
}

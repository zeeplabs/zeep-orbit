package dashboard

// provisioner_snapshots_test.go: builder_snapshots moved from one row per turn
// (PK session_id, turn_seq) to one row per commit (PK session_id, turn_seq,
// part). Provisioning must reach the new shape on a fresh database and on one
// that already ran the old DDL, and stay a no-op on every later boot.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func snapshotsTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	// Make sure sessions/apps exist so the table can be (re)created with its FK.
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision: %v", err)
	}
	return pool
}

// snapshotsPK returns the primary key columns of builder_snapshots in key order.
func snapshotsPK(t *testing.T, pool *db.Pool) string {
	t.Helper()
	var cols string
	err := pool.QueryRow(context.Background(),
		`SELECT array_to_string(array_agg(a.attname::text ORDER BY k.ord), ',')
		   FROM pg_constraint c
		   CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
		   JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		  WHERE c.conrelid = 'zeep_system.builder_snapshots'::regclass AND c.contype = 'p'`,
	).Scan(&cols)
	if err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	return cols
}

func provisionTwice(t *testing.T, pool *db.Pool) {
	t.Helper()
	for i := 1; i <= 2; i++ {
		if err := ProvisionZeepSystem(context.Background(), pool); err != nil {
			t.Fatalf("ProvisionZeepSystem boot %d: %v", i, err)
		}
	}
}

func TestProvision_BuilderSnapshots_FreshDB(t *testing.T) {
	pool := snapshotsTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE zeep_system.builder_snapshots`); err != nil {
		t.Fatal(err)
	}

	provisionTwice(t, pool)

	if got := snapshotsPK(t, pool); got != "session_id,turn_seq,part" {
		t.Errorf("primary key = %q, want session_id,turn_seq,part", got)
	}
}

func TestProvision_BuilderSnapshots_UpgradesOldShape(t *testing.T) {
	pool := snapshotsTestPool(t)
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	u, err := CreateUser(ctx, pool, fmt.Sprintf("snap-upgrade-%d@example.com", suffix), "u", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	var appID, sessID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("snap-upgrade-%d", suffix), u.ID,
	).Scan(&appID); err != nil {
		t.Fatalf("create app: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM zeep_system.apps WHERE id = $1`, appID) // cascades the session and snapshots
		_, _ = pool.Exec(bg, `DELETE FROM zeep_system.dashboard_users WHERE id = $1`, u.ID)
	})
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_sessions (owner_user_id, backend_app_id, template_kind, template_version)
		 VALUES ($1, $2, 'default', 'v0') RETURNING id`, u.ID, appID,
	).Scan(&sessID); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// The table as the previous release created it.
	for _, stmt := range []string{
		`DROP TABLE zeep_system.builder_snapshots`,
		`CREATE TABLE zeep_system.builder_snapshots (
			session_id UUID        NOT NULL REFERENCES zeep_system.builder_sessions(id) ON DELETE CASCADE,
			turn_seq   INT         NOT NULL,
			commit_sha TEXT        NOT NULL,
			bundle     BYTEA       NOT NULL,
			size_bytes INT         NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (session_id, turn_seq)
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed old shape: %v", err)
		}
	}
	if got := snapshotsPK(t, pool); got != "session_id,turn_seq" {
		t.Fatalf("old-shape setup: primary key = %q", got)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.builder_snapshots (session_id, turn_seq, commit_sha, bundle, size_bytes)
		 VALUES ($1, 0, 'sha0', '\x01', 1)`, sessID); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	provisionTwice(t, pool)

	if got := snapshotsPK(t, pool); got != "session_id,turn_seq,part" {
		t.Errorf("primary key after upgrade = %q, want session_id,turn_seq,part", got)
	}
	// The existing row survives as part 0 and a second part of the same turn is
	// now allowed.
	var part int
	if err := pool.QueryRow(ctx,
		`SELECT part FROM zeep_system.builder_snapshots WHERE session_id = $1 AND turn_seq = 0`, sessID,
	).Scan(&part); err != nil || part != 0 {
		t.Errorf("old row part = %d (%v), want 0", part, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.builder_snapshots (session_id, turn_seq, part, commit_sha, bundle, size_bytes)
		 VALUES ($1, 0, 1, 'sha1', '\x02', 1)`, sessID); err != nil {
		t.Errorf("second part of the same turn rejected after upgrade: %v", err)
	}
	// The same (session, turn, part) is still rejected.
	_, err = pool.Exec(ctx,
		`INSERT INTO zeep_system.builder_snapshots (session_id, turn_seq, part, commit_sha, bundle, size_bytes)
		 VALUES ($1, 0, 1, 'dup', '\x03', 1)`, sessID)
	if err == nil || !strings.Contains(err.Error(), "builder_snapshots") {
		t.Errorf("duplicate (session, turn, part) must violate the primary key, got %v", err)
	}
}

package db_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func TestNew(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping() after New() failed: %v", err)
	}
}

func TestNewInvalidDSN(t *testing.T) {
	ctx := context.Background()

	_, err := db.New(ctx, "not-a-valid-dsn")
	if err == nil {
		t.Fatal("New() with invalid DSN should return an error, got nil")
	}
}

func TestNewBadHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dsn := "postgres://user:pass@localhost:19999/nonexistent_db_zeep_test"
	_, err := db.New(ctx, dsn)
	if err == nil {
		t.Fatal("New() with unreachable host should return an error, got nil")
	}
}

func TestIsStatementTimeout(t *testing.T) {
	if !db.IsStatementTimeout(&pgconn.PgError{Code: "57014"}) {
		t.Fatal("57014 (query_canceled) should be detected as a statement timeout")
	}
	if db.IsStatementTimeout(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique_violation must not be treated as a statement timeout")
	}
	if db.IsStatementTimeout(errors.New("some other error")) {
		t.Fatal("a non-pg error must not be treated as a statement timeout")
	}
	if db.IsStatementTimeout(nil) {
		t.Fatal("nil must not be treated as a statement timeout")
	}
}

// rlsTestPool connects to the test DB and bootstraps db.EnduserRole with
// membership granted to the connecting/principal role — the precondition
// WithRLSContext's SET LOCAL ROLE relies on. Mirrors the bootstrap that
// dashboard.ProvisionZeepSystem does in production (T3), kept local here to
// avoid an import of internal/dashboard (would be a package cycle risk and
// pulls in unrelated setup).
func rlsTestPool(t *testing.T) *db.Pool {
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

	setup := []string{
		`DO $do$
		 BEGIN
		   IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zeep_app_enduser') THEN
		     CREATE ROLE zeep_app_enduser NOSUPERUSER NOBYPASSRLS NOLOGIN;
		   END IF;
		 END
		 $do$`,
		`GRANT zeep_app_enduser TO CURRENT_USER`,
	}
	for _, stmt := range setup {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("bootstrap zeep_app_enduser: %v", err)
		}
	}

	return pool
}

// TestWithRLSContext_RoleSwitch covers ROWPOL-14: a query run inside
// WithRLSContext must execute as db.EnduserRole, not the pool's connecting
// role.
func TestWithRLSContext_RoleSwitch(t *testing.T) {
	pool := rlsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	var currentUser string
	err := pool.WithRLSContext(ctx, db.RLSClaims{Role: "approver", Sub: "user-1", Email: "a@test.com"}, 0, func(q db.Querier) error {
		return q.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser)
	})
	if err != nil {
		t.Fatalf("WithRLSContext: %v", err)
	}
	if currentUser != db.EnduserRole {
		t.Fatalf("expected current_user = %q inside WithRLSContext, got %q", db.EnduserRole, currentUser)
	}
}

// TestWithRLSContext_ClaimsReadableAndReverted covers ROWPOL-14: the session
// GUCs must be readable via current_setting() inside fn, and must not leak
// to a connection reused from the pool afterward.
func TestWithRLSContext_ClaimsReadableAndReverted(t *testing.T) {
	pool := rlsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	claims := db.RLSClaims{Role: "approver", Sub: "user-42", Email: "approver@test.com"}

	var role, sub, email string
	err := pool.WithRLSContext(ctx, claims, 0, func(q db.Querier) error {
		if err := q.QueryRow(ctx, `SELECT current_setting('app.jwt_role', true)`).Scan(&role); err != nil {
			return err
		}
		if err := q.QueryRow(ctx, `SELECT current_setting('app.jwt_sub', true)`).Scan(&sub); err != nil {
			return err
		}
		return q.QueryRow(ctx, `SELECT current_setting('app.jwt_email', true)`).Scan(&email)
	})
	if err != nil {
		t.Fatalf("WithRLSContext: %v", err)
	}
	if role != claims.Role {
		t.Fatalf("expected app.jwt_role=%q inside fn, got %q", claims.Role, role)
	}
	if sub != claims.Sub {
		t.Fatalf("expected app.jwt_sub=%q inside fn, got %q", claims.Sub, sub)
	}
	if email != claims.Email {
		t.Fatalf("expected app.jwt_email=%q inside fn, got %q", claims.Email, email)
	}

	// SET LOCAL / set_config(..., true) is transaction-scoped: after the
	// transaction commits, a connection acquired straight from the pool
	// (which may be the very same underlying connection) must observe no
	// leaked GUC and no leaked role — an empty string, not the old value.
	acquired, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer acquired.Release()

	var leakedRole string
	if err := acquired.QueryRow(ctx, `SELECT current_setting('app.jwt_role', true)`).Scan(&leakedRole); err != nil {
		t.Fatalf("select current_setting after commit: %v", err)
	}
	if leakedRole == claims.Role {
		t.Fatalf("app.jwt_role leaked to a pooled connection after WithRLSContext committed: got %q", leakedRole)
	}

	var currentUser string
	if err := acquired.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser); err != nil {
		t.Fatalf("select current_user after commit: %v", err)
	}
	if currentUser == db.EnduserRole {
		t.Fatalf("role switch leaked to a pooled connection after WithRLSContext committed")
	}
}

// TestWithRLSContext_StatementTimeoutPreserved covers ROWPOL-14's regression
// requirement: WithRLSContext must compose the existing statement_timeout
// behavior (as WithTimeout does), not silently drop it in favor of the role
// switch.
func TestWithRLSContext_StatementTimeoutPreserved(t *testing.T) {
	pool := rlsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	err := pool.WithRLSContext(ctx, db.RLSClaims{Role: "member"}, 50, func(q db.Querier) error {
		_, err := q.Exec(ctx, `SELECT pg_sleep(1)`)
		return err
	})
	if err == nil {
		t.Fatal("expected statement_timeout to abort a query slower than the configured timeout, got nil error")
	}
	if !db.IsStatementTimeout(err) {
		t.Fatalf("expected a statement-timeout error (57014), got: %v", err)
	}
}

// TestWithRLSContext_LacksMembershipFailsExplicitly covers ROWPOL-14's
// never-fail-open requirement: a connecting role that is NOT a
// superuser and has no membership in db.EnduserRole must fail SET LOCAL
// ROLE explicitly. Superusers bypass membership checks entirely (SET ROLE
// always succeeds for them), so this needs its own non-superuser login role
// and its own pool — the shared test DSN's role is a superuser.
func TestWithRLSContext_LacksMembershipFailsExplicitly(t *testing.T) {
	adminPool := rlsTestPool(t)
	ctx := context.Background()

	// Registered in reverse of desired teardown order: t.Cleanup runs LIFO
	// (last registered, first executed), and DROP ROLE must run against a
	// still-open adminPool, after the restricted connection has closed.
	// Using `defer pool.Close()` here would run before t.Cleanup fires (as
	// it did in an earlier version of this test, which silently left
	// zeep_test_no_membership behind for every run after the first).
	t.Cleanup(func() { adminPool.Close() })

	const restrictedRole = "zeep_test_no_membership"
	// Idempotent: a role leaked by an earlier interrupted run must not break this one.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %s`, restrictedRole)); err != nil {
		t.Fatalf("pre-clean restricted login role: %v", err)
	}
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		`CREATE ROLE %s NOSUPERUSER LOGIN PASSWORD 'test-pass-only'`, restrictedRole,
	)); err != nil {
		t.Fatalf("create restricted login role: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), fmt.Sprintf(`DROP ROLE %s`, restrictedRole)); err != nil {
			t.Logf("warn: cleanup drop role %q: %v", restrictedRole, err)
		}
	})

	baseDSN := os.Getenv("TEST_DATABASE_URL")
	restrictedDSN, err := dsnWithCredentials(baseDSN, restrictedRole, "test-pass-only")
	if err != nil {
		t.Fatalf("build restricted DSN: %v", err)
	}

	restrictedPool, err := db.New(ctx, restrictedDSN)
	if err != nil {
		t.Fatalf("connect as restricted role: %v", err)
	}
	t.Cleanup(func() { restrictedPool.Close() })

	err = restrictedPool.WithRLSContext(ctx, db.RLSClaims{Role: "member"}, 0, func(q db.Querier) error {
		t.Fatal("fn must not run when the connecting role lacks membership in EnduserRole")
		return nil
	})
	if err == nil {
		t.Fatal("expected an explicit error when the connecting role lacks EnduserRole membership, got nil")
	}
}

// dsnWithCredentials swaps the userinfo portion of a Postgres DSN, keeping
// host/port/db/query params intact.
func dsnWithCredentials(dsn, user, password string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}

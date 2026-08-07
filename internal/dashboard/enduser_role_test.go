package dashboard

import (
	"context"
	"os"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// enduserRoleTestPool connects to the test DB. The zeep_app_enduser role is
// cluster-global (not scoped to any schema), so unlike other provisioner
// tests this helper does not drop zeep_system first — that would defeat the
// idempotency assertion the test is making.
func enduserRoleTestPool(t *testing.T) *db.Pool {
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
	return pool
}

// TestBootstrapEnduserRole covers ROWPOL-13: ProvisionZeepSystem must create
// the zeep_app_enduser role (no superuser, no BYPASSRLS, cannot log in
// directly) exactly once, grant the connecting/principal role membership in
// it (a precondition for SET ROLE), and running it twice must not error or
// duplicate the role.
func TestBootstrapEnduserRole(t *testing.T) {
	pool := enduserRoleTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("first ProvisionZeepSystem: %v", err)
	}

	var rolsuper, rolbypassrls, rolcanlogin bool
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*), bool_and(rolsuper), bool_and(rolbypassrls), bool_and(rolcanlogin)
		 FROM pg_roles WHERE rolname = 'zeep_app_enduser'
		 GROUP BY rolsuper, rolbypassrls, rolcanlogin`,
	).Scan(&count, &rolsuper, &rolbypassrls, &rolcanlogin); err != nil {
		t.Fatalf("query pg_roles: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 zeep_app_enduser role, got %d", count)
	}
	if rolsuper {
		t.Error("zeep_app_enduser must not be a superuser")
	}
	if rolbypassrls {
		t.Error("zeep_app_enduser must not have BYPASSRLS")
	}
	if rolcanlogin {
		t.Error("zeep_app_enduser must not be able to log in directly")
	}

	// The connecting/principal role must be able to SET ROLE into it
	// (membership granted) — this is the precondition WithRLSContext (T5)
	// relies on. Acquire a single connection so SET ROLE / SELECT / RESET
	// ROLE all observe the same session (pool.Exec alone could round-robin
	// across pooled connections).
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SET ROLE zeep_app_enduser`); err != nil {
		t.Fatalf("connecting role could not SET ROLE zeep_app_enduser: %v", err)
	}
	var currentUser string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser); err != nil {
		t.Fatalf("select current_user: %v", err)
	}
	if currentUser != "zeep_app_enduser" {
		t.Fatalf("expected current_user = zeep_app_enduser after SET ROLE, got %q", currentUser)
	}
	if _, err := conn.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatalf("reset role: %v", err)
	}

	// Running provisioning again must not error and must not duplicate the role.
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("second ProvisionZeepSystem (idempotency): %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_roles WHERE rolname = 'zeep_app_enduser'`,
	).Scan(&count); err != nil {
		t.Fatalf("re-query pg_roles: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 zeep_app_enduser role after re-run, got %d", count)
	}
}

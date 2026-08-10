package server

// rls_policy_test.go — end-user-row-policies T11. End-to-end proof of the
// spec's motivating case (asset-manager-web: "a requester can't approve/
// update their own request"): a native Postgres policy — built with
// provisioner.BuildPolicySQL (T7) exactly as dashboard.CreateTablePolicy
// (T8) would — enforces the rule both through the Orbit REST API and
// through a raw pgx connection authenticated as zeep_app_enduser with the
// session GUCs set by hand, proving the enforcement lives in Postgres, not
// in the HTTP layer. Also proves the principal/owner pool (Data Browser's
// path) is unaffected throughout (T9's guarantee, re-confirmed here end to
// end).
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

const (
	rlsPolicySchema = "rls_policy_test_app"
	rlsPolicyApp    = "rls_policy_test_app"
	rlsPolicySecret = "rls-policy-jwt-secret"
)

// requestsPolicyColumns is the single source of truth for the fixture
// table's non-system columns — shared by provisioner.BuildPolicySQL (which
// wants config.ColumnConfig) and the registry.App built by hand below
// (which wants registry.Column).
func requestsPolicyColumns() []config.ColumnConfig {
	return []config.ColumnConfig{
		{Name: "requester_id", Type: "uuid", Required: true},
		{Name: "status", Type: "text"},
	}
}

func requestsPolicyRegistryColumns() []registry.Column {
	cols := requestsPolicyColumns()
	out := make([]registry.Column, len(cols))
	for i, c := range cols {
		out[i] = registry.Column{Name: c.Name, Type: c.Type, Required: c.Required}
	}
	return out
}

// setupRLSPolicyFixture creates a fresh physical schema+table, enables
// native RLS with a policy reproducing the spec's motivating case, registers
// the app in testReg (via Register — a single-app upsert, unlike Load, which
// would wipe out every other app TestMain already registered), and seeds two
// rows: rowA (requester_id = userA) and rowB (requester_id = userB).
func setupRLSPolicyFixture(t *testing.T) (rowAID, rowBID, userAID, userBID string) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()

	setup := []string{
		"DROP SCHEMA IF EXISTS " + rlsPolicySchema + " CASCADE",
		"CREATE SCHEMA " + rlsPolicySchema,
		`CREATE TABLE ` + rlsPolicySchema + `.requests (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			requester_id UUID NOT NULL,
			status       TEXT NOT NULL DEFAULT 'pending',
			created_at   TIMESTAMPTZ DEFAULT now(),
			updated_at   TIMESTAMPTZ DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + rlsPolicySchema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + rlsPolicySchema + ` TO zeep_app_enduser`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+rlsPolicySchema+" CASCADE")
	})

	updateDDL, err := provisioner.BuildPolicySQL(rlsPolicySchema, "requests", provisioner.PolicyDef{
		Name:   "approver_no_self_approve",
		Action: "update",
		Roles:  []string{"approver"},
		Clauses: []provisioner.PolicyClause{
			{Column: "requester_id", Operator: "!=", ValueSource: "claim", Value: "sub"},
		},
	}, requestsPolicyColumns())
	if err != nil {
		t.Fatalf("BuildPolicySQL (update): %v", err)
	}
	// A paired SELECT policy for the same role is what a real admin would
	// also configure in practice (Postgres requires the row to satisfy a
	// SELECT policy for `UPDATE ... RETURNING` to return anything at all,
	// even when the UPDATE itself succeeded) — without it, the REST API's
	// 200-vs-404 signal in the "allowed" case below would be masked by a
	// missing RETURNING row rather than actually proving the policy allowed
	// the write. This one is intentionally broad (always true) so it never
	// itself blocks anything — the UPDATE policy above is what's under test.
	selectDDL, err := provisioner.BuildPolicySQL(rlsPolicySchema, "requests", provisioner.PolicyDef{
		Name:   "approver_can_read_all_requests",
		Action: "select",
		Roles:  []string{"approver"},
		Clauses: []provisioner.PolicyClause{
			{Column: "requester_id", Operator: "IS NOT NULL"},
		},
	}, requestsPolicyColumns())
	if err != nil {
		t.Fatalf("BuildPolicySQL (select): %v", err)
	}
	if _, err := testPool.Exec(ctx, "ALTER TABLE "+rlsPolicySchema+".requests ENABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatalf("enable row level security: %v", err)
	}
	if _, err := testPool.Exec(ctx, updateDDL); err != nil {
		t.Fatalf("create update policy: %v", err)
	}
	if _, err := testPool.Exec(ctx, selectDDL); err != nil {
		t.Fatalf("create select policy: %v", err)
	}

	testReg.Register(&registry.App{
		Config: config.AppConfig{
			Name: rlsPolicyApp,
			Auth: config.AuthConfig{
				JWTSecret: rlsPolicySecret,
				Providers: config.AuthProviders{Email: true},
			},
		},
		SchemaName: rlsPolicySchema,
		Tables: map[string]*registry.Table{
			"requests": {
				Name:    "requests",
				RLS:     "",
				Columns: requestsPolicyRegistryColumns(),
			},
		},
	})
	t.Cleanup(func() { testReg.Unregister(rlsPolicyApp) })

	if err := testPool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&userAID); err != nil {
		t.Fatalf("gen userA id: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&userBID); err != nil {
		t.Fatalf("gen userB id: %v", err)
	}

	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+rlsPolicySchema+`.requests (requester_id, status) VALUES ($1, 'pending') RETURNING id`,
		userAID,
	).Scan(&rowAID); err != nil {
		t.Fatalf("seed rowA: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+rlsPolicySchema+`.requests (requester_id, status) VALUES ($1, 'pending') RETURNING id`,
		userBID,
	).Scan(&rowBID); err != nil {
		t.Fatalf("seed rowB: %v", err)
	}

	return rowAID, rowBID, userAID, userBID
}

// TestRLSPolicy_EndToEndMotivatingCase reproduces the spec's Independent
// Test for the "policy nativa" story end to end.
func TestRLSPolicy_EndToEndMotivatingCase(t *testing.T) {
	rowAID, rowBID, userAID, _ := setupRLSPolicyFixture(t)
	ctx := context.Background()

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)

	jwtA, err := auth.IssueJWT([]byte(rlsPolicySecret), userAID, "approver-a@test.com", rlsPolicyApp, "approver")
	if err != nil {
		t.Fatalf("IssueJWT userA: %v", err)
	}
	bearerA := "Bearer " + jwtA
	basePath := "/" + rlsPolicyApp + "/requests"

	t.Run("REST_UserCannotApproveOwnRequest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, basePath+"/"+rowAID+"/",
			jsonBody(map[string]any{"status": "approved"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearerA)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404 (linha invisível pela policy nativa), obtido %d: %s", rec.Code, rec.Body.String())
		}

		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM `+rlsPolicySchema+`.requests WHERE id = $1`, rowAID).Scan(&status); err != nil {
			t.Fatalf("check rowA status: %v", err)
		}
		if status != "pending" {
			t.Fatalf("rowA status = %q, want still %q (update must have been blocked)", status, "pending")
		}
	})

	t.Run("RawPgxConnectionAsEnduserRoleReproducesTheSameDenial", func(t *testing.T) {
		// Proves the enforcement is DB-level, not HTTP-level: a raw pgx
		// connection authenticated as zeep_app_enduser with the same GUCs
		// WithRLSContext would set gets denied identically, with zero Orbit
		// HTTP code in the path.
		tx, err := testPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+db.EnduserRole); err != nil {
			t.Fatalf("set local role: %v", err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.jwt_role', 'approver', true)`); err != nil {
			t.Fatalf("set app.jwt_role: %v", err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.jwt_sub', $1, true)`, userAID); err != nil {
			t.Fatalf("set app.jwt_sub: %v", err)
		}

		tag, err := tx.Exec(ctx, `UPDATE `+rlsPolicySchema+`.requests SET status = 'approved' WHERE id = $1`, rowAID)
		if err != nil {
			t.Fatalf("raw update: %v", err)
		}
		if tag.RowsAffected() != 0 {
			t.Fatalf("raw connection as %s updated %d row(s), want 0 (policy must deny self-approval)", db.EnduserRole, tag.RowsAffected())
		}
	})

	t.Run("REST_UserCanUpdateAnotherRequestersRow", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, basePath+"/"+rowBID+"/",
			jsonBody(map[string]any{"status": "approved"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearerA)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200 (linha de outro requester, policy permite), obtido %d: %s", rec.Code, rec.Body.String())
		}
		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if row["status"] != "approved" {
			t.Fatalf("status esperado 'approved', obtido %v", row["status"])
		}
	})

	t.Run("DataBrowserOwnerRoleSeesAndEditsEveryRowRegardlessOfPolicy", func(t *testing.T) {
		// Data Browser's path never calls WithRLSContext/SET ROLE — it runs
		// on the principal/owner pool directly, which Postgres exempts from
		// RLS by default. Confirm it still sees both rows (including rowA,
		// which the approver-role policy above denies) and can edit rowA
		// too, unaffected by the deny-all-for-approver policy.
		var count int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM `+rlsPolicySchema+`.requests`).Scan(&count); err != nil {
			t.Fatalf("count via owner pool: %v", err)
		}
		if count != 2 {
			t.Fatalf("owner pool sees %d row(s), want 2 (both rows, RLS must not filter the owner role)", count)
		}

		tag, err := testPool.Exec(ctx, `UPDATE `+rlsPolicySchema+`.requests SET status = 'admin_override' WHERE id = $1`, rowAID)
		if err != nil {
			t.Fatalf("owner pool update rowA: %v", err)
		}
		if tag.RowsAffected() != 1 {
			t.Fatalf("owner pool updated %d row(s), want 1 (owner role must edit rowA despite the approver-role policy)", tag.RowsAffected())
		}
	})
}

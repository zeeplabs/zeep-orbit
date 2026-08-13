package server

// rls_policy_mode_test.go — rls-policy-mode T11. End-to-end proof of the
// spec's motivating case (spec.md P1 Independent Test): a table with
// rls: "policy" gets native RLS enabled at creation with zero policies
// (fail-closed, RLSP-02), a SELECT policy without a row-restricting clause
// lets a role see rows created by other users (RLSP-01), an INSERT still
// populates owner_id even though no owner_id filter is ever applied to
// reads/writes (RLSP-03), and the Data Browser's owner pool keeps seeing
// every row regardless of policy (RLSP-04, re-confirmed here for the new
// mode specifically, same guarantee rls_policy_test.go already proved for
// end-user-row-policies).
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
	rlsPolicyModeSchema = "rls_policy_mode_test_app"
	rlsPolicyModeApp    = "rls_policy_mode_test_app"
	rlsPolicyModeSecret = "rls-policy-mode-jwt-secret"
)

func postsPolicyModeColumns() []config.ColumnConfig {
	return []config.ColumnConfig{
		{Name: "title", Type: "text", Required: true},
	}
}

func postsPolicyModeRegistryColumns() []registry.Column {
	cols := postsPolicyModeColumns()
	out := make([]registry.Column, len(cols))
	for i, c := range cols {
		out[i] = registry.Column{Name: c.Name, Type: c.Type, Required: c.Required}
	}
	return out
}

// setupRLSPolicyModeFixture creates a fresh physical schema+table mirroring
// exactly what provisioner.createTable produces for rls: "policy" (RLS
// enabled at creation, before any policy exists — RLSP-02), registers the
// app in testReg with the table's RLS set to "policy" (so
// resolveOwner/query.Build* take the "policy" branch: owner_id is still
// populated on INSERT but never used as a read/write filter — RLSP-01/03),
// and seeds two rows owned by two different users.
func setupRLSPolicyModeFixture(t *testing.T) (postAID, postBID, userAID, userBID string) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()

	setup := []string{
		"DROP SCHEMA IF EXISTS " + rlsPolicyModeSchema + " CASCADE",
		"CREATE SCHEMA " + rlsPolicyModeSchema,
		`CREATE TABLE ` + rlsPolicyModeSchema + `."_auth_users" (
			"id"                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"              TEXT        NOT NULL UNIQUE,
			"phone"              TEXT,
			"password_hash"      TEXT        NOT NULL,
			"name"               TEXT,
			"avatar_url"         TEXT,
			"email_confirmed_at" TIMESTAMPTZ,
			"last_sign_in_at"    TIMESTAMPTZ,
			"created_at"         TIMESTAMPTZ NOT NULL DEFAULT now(),
			"updated_at"         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE ` + rlsPolicyModeSchema + `.posts (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			owner_id   UUID NOT NULL REFERENCES ` + rlsPolicyModeSchema + `."_auth_users"("id"),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + rlsPolicyModeSchema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + rlsPolicyModeSchema + ` TO zeep_app_enduser`,
		// "policy" tables get RLS enabled at creation, before any policy
		// exists (provisioner.createTable, T4) — reproduced here by hand
		// since this fixture uses raw SQL instead of going through the
		// provisioner.
		`ALTER TABLE ` + rlsPolicyModeSchema + `.posts ENABLE ROW LEVEL SECURITY`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+rlsPolicyModeSchema+" CASCADE")
	})

	testReg.Register(&registry.App{
		Config: config.AppConfig{
			Name: rlsPolicyModeApp,
			Auth: config.AuthConfig{
				JWTSecret: rlsPolicyModeSecret,
				Providers: config.AuthProviders{Email: true},
			},
		},
		SchemaName: rlsPolicyModeSchema,
		Tables: map[string]*registry.Table{
			"posts": {
				Name:    "posts",
				RLS:     "policy",
				Columns: postsPolicyModeRegistryColumns(),
			},
		},
	})
	t.Cleanup(func() { testReg.Unregister(rlsPolicyModeApp) })

	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+rlsPolicyModeSchema+`."_auth_users" (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		"policy-mode-userA@test.com",
	).Scan(&userAID); err != nil {
		t.Fatalf("insert userA: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+rlsPolicyModeSchema+`."_auth_users" (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		"policy-mode-userB@test.com",
	).Scan(&userBID); err != nil {
		t.Fatalf("insert userB: %v", err)
	}

	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+rlsPolicyModeSchema+`.posts (title, owner_id) VALUES ('post from userA', $1) RETURNING id`,
		userAID,
	).Scan(&postAID); err != nil {
		t.Fatalf("seed postA: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+rlsPolicyModeSchema+`.posts (title, owner_id) VALUES ('post from userB', $1) RETURNING id`,
		userBID,
	).Scan(&postBID); err != nil {
		t.Fatalf("seed postB: %v", err)
	}

	return postAID, postBID, userAID, userBID
}

// TestRLSPolicyMode_EndToEnd reproduces spec.md's P1 Independent Test for
// rls: "policy" end to end against a real Postgres.
func TestRLSPolicyMode_EndToEnd(t *testing.T) {
	postAID, postBID, userAID, _ := setupRLSPolicyModeFixture(t)
	ctx := context.Background()

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/" + rlsPolicyModeApp + "/posts"

	// "no_policy_role" never gets any policy created for it in this test —
	// it exists purely to exercise the fail-closed case (spec AC P1-2).
	jwtNoPolicy, err := auth.IssueJWT([]byte(rlsPolicyModeSecret), userAID, "policy-mode-userA@test.com", rlsPolicyModeApp, "no_policy_role")
	if err != nil {
		t.Fatalf("IssueJWT (no_policy_role): %v", err)
	}
	bearerNoPolicy := "Bearer " + jwtNoPolicy

	t.Run("REST_NoSelectPolicyReturnsEmptyList", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", bearerNoPolicy)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		data, _ := resp["data"].([]any)
		if len(data) != 0 {
			t.Fatalf("esperado lista vazia (nenhuma select policy para no_policy_role), obtido %d item(s)", len(data))
		}
	})

	t.Run("REST_NoSelectPolicyReturnsNotFoundForGetByID", func(t *testing.T) {
		// spec AC P1-2 covers GET /{app}/{table} AND GET /{app}/{table}/{id}
		// — this proves the get-by-id half: fail-closed also denies a
		// direct lookup by id, not just the list form.
		req := httptest.NewRequest(http.MethodGet, basePath+"/"+postAID+"/", nil)
		req.Header.Set("Authorization", bearerNoPolicy)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404 (nenhuma select policy para no_policy_role), obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("RawPgxConnectionAsEnduserRoleReproducesTheSameDenial", func(t *testing.T) {
		// Proves the fail-closed guarantee is DB-level (RLS enabled + zero
		// policies), not something resolveOwner enforces in the app layer —
		// a raw pgx connection as zeep_app_enduser with the same GUCs
		// WithRLSContext would set sees zero rows too, even though two rows
		// physically exist in the table.
		tx, err := testPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+db.EnduserRole); err != nil {
			t.Fatalf("set local role: %v", err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.jwt_role', 'no_policy_role', true)`); err != nil {
			t.Fatalf("set app.jwt_role: %v", err)
		}

		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM `+rlsPolicyModeSchema+`.posts`).Scan(&count); err != nil {
			t.Fatalf("raw select: %v", err)
		}
		if count != 0 {
			t.Fatalf("raw connection as %s viu %d linha(s), want 0 (RLS deve negar tudo sem policy)", db.EnduserRole, count)
		}
	})

	t.Run("REST_SelectPolicyWithoutRowClauseShowsOtherUsersRows", func(t *testing.T) {
		// A select policy for "org_viewer" with no row-restricting clause —
		// spec AC P1-3: this role must see every row, including rows owned
		// by other users, something structurally impossible under
		// rls: "owner"/"enabled" (which always injects owner_id = $sub).
		ddl, err := provisioner.BuildPolicySQL(rlsPolicyModeSchema, "posts", provisioner.PolicyDef{
			Name:   "org_viewer_sees_all_posts",
			Action: "select",
			Roles:  []string{"org_viewer"},
			Clauses: []provisioner.PolicyClause{
				{Column: "owner_id", Operator: "IS NOT NULL"},
			},
		}, postsPolicyModeColumns())
		if err != nil {
			t.Fatalf("BuildPolicySQL: %v", err)
		}
		if _, err := testPool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create select policy: %v", err)
		}

		jwtOrgViewer, err := auth.IssueJWT([]byte(rlsPolicyModeSecret), userAID, "policy-mode-userA@test.com", rlsPolicyModeApp, "org_viewer")
		if err != nil {
			t.Fatalf("IssueJWT (org_viewer): %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", "Bearer "+jwtOrgViewer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		data, _ := resp["data"].([]any)
		if len(data) != 2 {
			t.Fatalf("esperado 2 linhas (org_viewer vê tudo, incluindo linha de outro usuário), obtido %d item(s): %+v", len(data), data)
		}
		seen := map[string]bool{}
		for _, item := range data {
			row, _ := item.(map[string]any)
			id, _ := row["id"].(string)
			seen[id] = true
		}
		if !seen[postAID] || !seen[postBID] {
			t.Fatalf("esperado ver postA (%s) e postB (%s), obtido %+v", postAID, postBID, seen)
		}
	})

	t.Run("REST_InsertStillPopulatesOwnerID", func(t *testing.T) {
		// spec AC P1-5: even though rls: "policy" never applies an
		// owner_id filter, the INSERT path must still populate owner_id
		// with the authenticated user's sub — otherwise it would violate
		// the column's NOT NULL constraint. "author" gets an INSERT policy
		// (WITH CHECK owner_id IS NOT NULL — always true once the app
		// populates owner_id before the write reaches Postgres) plus a
		// SELECT policy, since INSERT ... RETURNING also needs the new row
		// to satisfy a SELECT policy to be returned.
		insertDDL, err := provisioner.BuildPolicySQL(rlsPolicyModeSchema, "posts", provisioner.PolicyDef{
			Name:   "author_can_insert_own_post",
			Action: "insert",
			Roles:  []string{"author"},
			Clauses: []provisioner.PolicyClause{
				{Column: "owner_id", Operator: "IS NOT NULL"},
			},
		}, postsPolicyModeColumns())
		if err != nil {
			t.Fatalf("BuildPolicySQL (insert): %v", err)
		}
		if _, err := testPool.Exec(ctx, insertDDL); err != nil {
			t.Fatalf("create insert policy: %v", err)
		}
		selectDDL, err := provisioner.BuildPolicySQL(rlsPolicyModeSchema, "posts", provisioner.PolicyDef{
			Name:   "author_can_read_own_post",
			Action: "select",
			Roles:  []string{"author"},
			Clauses: []provisioner.PolicyClause{
				{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
			},
		}, postsPolicyModeColumns())
		if err != nil {
			t.Fatalf("BuildPolicySQL (select): %v", err)
		}
		if _, err := testPool.Exec(ctx, selectDDL); err != nil {
			t.Fatalf("create select policy: %v", err)
		}

		jwtAuthor, err := auth.IssueJWT([]byte(rlsPolicyModeSecret), userAID, "policy-mode-userA@test.com", rlsPolicyModeApp, "author")
		if err != nil {
			t.Fatalf("IssueJWT (author): %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{"title": "novo post"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtAuthor)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
		}
		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		ownerID, _ := row["owner_id"].(string)
		if ownerID != userAID {
			t.Fatalf("owner_id esperado %s, obtido %v", userAID, ownerID)
		}
	})

	t.Run("DataBrowserOwnerPoolSeesEveryRowRegardlessOfPolicy", func(t *testing.T) {
		// spec Edge Cases: the Data Browser (owner/principal pool) never
		// calls WithRLSContext — it runs outside RLS entirely, so it must
		// see every row (the 2 seeded + the 1 inserted by the subtest
		// above), unaffected by any policy created for any role above.
		var count int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM `+rlsPolicyModeSchema+`.posts`).Scan(&count); err != nil {
			t.Fatalf("count via owner pool: %v", err)
		}
		if count != 3 {
			t.Fatalf("owner pool viu %d linha(s), want 3 (2 seed + 1 inserido, RLS não deve filtrar o pool owner)", count)
		}
	})
}

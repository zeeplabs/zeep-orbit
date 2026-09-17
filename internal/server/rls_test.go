package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// buildRLSRouter creates a chi.Router with real JWTMiddleware (injects AuthUser).
func buildRLSRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Route("/{app}/{table}", func(r chi.Router) {
		r.Use(JWTMiddleware(testReg, nil))
		r.Get("/", h.HandleList)
		r.Post("/", h.HandleCreate)
	})
	r.Route("/{app}/{table}/{id}", func(r chi.Router) {
		r.Use(JWTMiddleware(testReg, nil))
		r.Get("/", h.HandleGetByID)
		r.Put("/", h.HandleUpdate)
		r.Patch("/", h.HandleUpdate)
		r.Delete("/", h.HandleDelete)
	})
	return r
}

// insertRLSUser inserts a user into _auth_users and returns the generated UUID.
func insertRLSUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := testPool.QueryRow(
		ctx,
		`INSERT INTO `+rlsSchema+`."_auth_users" (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertRLSUser %q: %v", email, err)
	}
	return id
}

// TestRLS cobre os acceptance criteria do ZC-21.
func TestRLS(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)

	user1ID := insertRLSUser(t, "rls-user1@test.com")
	user2ID := insertRLSUser(t, "rls-user2@test.com")

	jwt1, err := auth.IssueJWT([]byte(rlsSecret), user1ID, "rls-user1@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT user1: %v", err)
	}
	jwt2, err := auth.IssueJWT([]byte(rlsSecret), user2ID, "rls-user2@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT user2: %v", err)
	}

	bearer1 := "Bearer " + jwt1
	bearer2 := "Bearer " + jwt2
	basePath := "/" + rlsAppName + "/notes"

	var noteID string

	t.Run("AC1_PostPopulatesOwnerID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{"title": "nota do user1"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearer1)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
		}
		var row map[string]any
		json.NewDecoder(rec.Body).Decode(&row)
		id, _ := row["id"].(string)
		if id == "" {
			t.Fatal("id ausente na response")
		}
		ownerID, _ := row["owner_id"].(string)
		if ownerID != user1ID {
			t.Fatalf("owner_id esperado %s, obtido %v", user1ID, ownerID)
		}
		noteID = id
	})

	t.Run("AC2_User1GetListSeesOwnNote", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", bearer1)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		data, _ := resp["data"].([]any)
		if len(data) == 0 {
			t.Fatal("user1 deveria ver ao menos 1 nota")
		}
	})

	t.Run("AC3_User2GetListEmpty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		data, _ := resp["data"].([]any)
		if len(data) != 0 {
			t.Fatalf("user2 não deveria ver notas de user1, obtido %d item(s)", len(data))
		}
	})

	t.Run("AC4_User2GetByIDReturns404", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodGet, basePath+"/"+noteID+"/", nil)
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC5_User2PutReturns404", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodPut, basePath+"/"+noteID+"/", jsonBody(map[string]any{"title": "hack"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC6_User2DeleteReturns404", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodDelete, basePath+"/"+noteID+"/", nil)
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC7_User1GetByIDReturns200", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodGet, basePath+"/"+noteID+"/", nil)
		req.Header.Set("Authorization", bearer1)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC8_NoJWTOnRLSTableReturns401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("esperado 401, obtido %d", rec.Code)
		}
	})

	t.Run("AC9_TablesWithoutRLSUnaffected", func(t *testing.T) {
		app, ok := testReg.Get("testhandler")
		if !ok {
			t.Fatal("testhandler app não encontrado no registry")
		}
		tbl, ok := app.Tables[testTable]
		if !ok {
			t.Fatalf("tabela %q não encontrada", testTable)
		}
		if tbl.RLS != "" {
			t.Errorf("tabela %q não deveria ter RLS, obtido %q", testTable, tbl.RLS)
		}
	})
}

// TestHandlerCreateOnConflictIgnoreDoesNotLeakOtherOwnersRow is the
// regression test for the cross-tenant leak found in pre-release review:
// fetchRowByColumns (the SELECT that runs after an on_conflict:"ignore"
// insert short-circuits with zero rows) had no owner_id filter, unlike every
// other read path. On an "owner"-RLS table there is no native Postgres
// policy backing that filter — the app-level predicate is the only
// enforcement — so an attacker could read any other tenant's row by
// guessing/brute-forcing a value in a unique column and colliding on it via
// on_conflict/conflict_columns.
func TestHandlerCreateOnConflictIgnoreDoesNotLeakOtherOwnersRow(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/" + rlsAppName + "/notes"

	victimID := insertRLSUser(t, "victim-conflict@test.com")
	attackerID := insertRLSUser(t, "attacker-conflict@test.com")

	victimJWT, err := auth.IssueJWT([]byte(rlsSecret), victimID, "victim-conflict@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT victim: %v", err)
	}
	attackerJWT, err := auth.IssueJWT([]byte(rlsSecret), attackerID, "attacker-conflict@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT attacker: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, basePath+"/",
		jsonBody(map[string]any{"title": "victim's private note", "slug": "shared-slug-leak-test"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+victimJWT)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed da vítima: esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
	}

	attackReq := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{
		"title":            "attacker probing for victim's row",
		"slug":             "shared-slug-leak-test",
		"on_conflict":      "ignore",
		"conflict_columns": []string{"slug"},
	}))
	attackReq.Header.Set("Content-Type", "application/json")
	attackReq.Header.Set("Authorization", "Bearer "+attackerJWT)
	attackRec := httptest.NewRecorder()
	router.ServeHTTP(attackRec, attackReq)

	if attackRec.Code == http.StatusOK && strings.Contains(attackRec.Body.String(), "victim's private note") {
		t.Fatalf("VAZAMENTO CROSS-TENANT: attacker recebeu a linha da vítima: %d: %s", attackRec.Code, attackRec.Body.String())
	}
	if attackRec.Code != http.StatusConflict {
		t.Fatalf("esperado 409 (colisão pertence a outro owner, não é 'sua' linha), obtido %d: %s", attackRec.Code, attackRec.Body.String())
	}
}

// TestHandlerCreateOnConflictUpdateDoesNotOverwriteOtherOwnersRow is the
// regression test for the second cross-tenant leak found in the second
// pre-release review: excluding owner_id from the ON CONFLICT DO UPDATE SET
// clause only stops an upsert from reassigning ownership — it doesn't stop
// the upsert from overwriting (and RETURNING, disclosing) another tenant's
// row. Same root cause as the "ignore" leak above (no native Postgres
// policy backs "owner"/"enabled" RLS — the app-level filter is the only
// enforcement), but on the "update" path instead of "ignore".
func TestHandlerCreateOnConflictUpdateDoesNotOverwriteOtherOwnersRow(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/" + rlsAppName + "/notes"

	victimID := insertRLSUser(t, "victim-update-conflict@test.com")
	attackerID := insertRLSUser(t, "attacker-update-conflict@test.com")

	victimJWT, err := auth.IssueJWT([]byte(rlsSecret), victimID, "victim-update-conflict@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT victim: %v", err)
	}
	attackerJWT, err := auth.IssueJWT([]byte(rlsSecret), attackerID, "attacker-update-conflict@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT attacker: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, basePath+"/",
		jsonBody(map[string]any{"title": "victim's private title", "slug": "shared-slug-update-leak-test"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+victimJWT)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed da vítima: esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
	}
	var victimRow map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&victimRow); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}

	attackReq := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{
		"title":            "OVERWRITTEN BY ATTACKER",
		"slug":             "shared-slug-update-leak-test",
		"on_conflict":      "update",
		"conflict_columns": []string{"slug"},
	}))
	attackReq.Header.Set("Content-Type", "application/json")
	attackReq.Header.Set("Authorization", "Bearer "+attackerJWT)
	attackRec := httptest.NewRecorder()
	router.ServeHTTP(attackRec, attackReq)

	if attackRec.Code == http.StatusOK {
		t.Fatalf("VAZAMENTO/SOBRESCRITA CROSS-TENANT: attacker conseguiu upsert na linha da vítima: %d: %s", attackRec.Code, attackRec.Body.String())
	}
	if attackRec.Code != http.StatusConflict {
		t.Fatalf("esperado 409 (colisão pertence a outro owner, não é 'sua' linha), obtido %d: %s", attackRec.Code, attackRec.Body.String())
	}

	// The victim's row must be untouched — this is the part a plain 409
	// check wouldn't catch: the WHERE guard on DO UPDATE must have actually
	// stopped the write, not just made the response opaque.
	getReq := httptest.NewRequest(http.MethodGet, basePath+"/"+victimRow["id"].(string)+"/", nil)
	getReq.Header.Set("Authorization", "Bearer "+victimJWT)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("vítima deveria continuar vendo sua própria linha, obtido %d: %s", getRec.Code, getRec.Body.String())
	}
	var currentRow map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&currentRow); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if currentRow["title"] != "victim's private title" {
		t.Fatalf("linha da vítima foi sobrescrita pelo attacker: title = %v", currentRow["title"])
	}
}

// TestHandlerCreateOnConflictUpdateOwnerAndSoftDeleteComposition is the
// regression test the fourth pre-release review flagged as missing: no test
// exercised owner-scoping ("owner"/"enabled" RLS) and soft-delete exclusion
// together on the same on_conflict:"update" WHERE guard — every existing
// test proved one guard clause or the other, never both composed on the
// same table at once. The review verified the composed SQL and a live
// request by hand and found it correct (both clauses AND-composed, $N
// numbering right); this test pins that down so a future change can't
// silently regress the composition without a red test.
func TestHandlerCreateOnConflictUpdateOwnerAndSoftDeleteComposition(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	orig := testReg.SystemConfig()
	t.Cleanup(func() { testReg.SetSystemConfig(orig) })
	cfg := orig
	cfg.SoftDeleteEnabled = true
	testReg.SetSystemConfig(cfg)

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/" + rlsAppName + "/notes"

	ownerID := insertRLSUser(t, "composition-owner@test.com")
	otherID := insertRLSUser(t, "composition-other@test.com")
	ownerJWT, err := auth.IssueJWT([]byte(rlsSecret), ownerID, "composition-owner@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT owner: %v", err)
	}
	otherJWT, err := auth.IssueJWT([]byte(rlsSecret), otherID, "composition-other@test.com", rlsAppName, "member")
	if err != nil {
		t.Fatalf("IssueJWT other: %v", err)
	}

	t.Run("cross-tenant collision still 409s with soft delete on", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, basePath+"/",
			jsonBody(map[string]any{"title": "owner's row", "slug": "composition-cross-tenant-slug"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+ownerJWT)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed: esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
		}

		attackReq := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{
			"title": "attacker", "slug": "composition-cross-tenant-slug",
			"on_conflict": "update", "conflict_columns": []string{"slug"},
		}))
		attackReq.Header.Set("Content-Type", "application/json")
		attackReq.Header.Set("Authorization", "Bearer "+otherJWT)
		attackRec := httptest.NewRecorder()
		router.ServeHTTP(attackRec, attackReq)
		if attackRec.Code != http.StatusConflict {
			t.Fatalf("esperado 409 (owner guard deveria bloquear mesmo com soft delete ligado), obtido %d: %s", attackRec.Code, attackRec.Body.String())
		}
	})

	t.Run("own soft-deleted row still 409s with owner guard satisfied", func(t *testing.T) {
		createReq := httptest.NewRequest(http.MethodPost, basePath+"/",
			jsonBody(map[string]any{"title": "to be soft-deleted", "slug": "composition-soft-delete-slug"}))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("Authorization", "Bearer "+ownerJWT)
		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("seed: esperado 201, obtido %d: %s", createRec.Code, createRec.Body.String())
		}
		var created map[string]any
		if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}

		deleteReq := httptest.NewRequest(http.MethodDelete, basePath+"/"+created["id"].(string)+"/", nil)
		deleteReq.Header.Set("Authorization", "Bearer "+ownerJWT)
		deleteRec := httptest.NewRecorder()
		router.ServeHTTP(deleteRec, deleteReq)
		if deleteRec.Code != http.StatusNoContent {
			t.Fatalf("soft-delete: esperado 204, obtido %d: %s", deleteRec.Code, deleteRec.Body.String())
		}

		upsertReq := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{
			"title": "resurrection attempt", "slug": "composition-soft-delete-slug",
			"on_conflict": "update", "conflict_columns": []string{"slug"},
		}))
		upsertReq.Header.Set("Content-Type", "application/json")
		upsertReq.Header.Set("Authorization", "Bearer "+ownerJWT)
		upsertRec := httptest.NewRecorder()
		router.ServeHTTP(upsertRec, upsertReq)
		if upsertRec.Code != http.StatusConflict {
			t.Fatalf("esperado 409 (soft-delete guard deveria bloquear mesmo owner batendo), obtido %d: %s", upsertRec.Code, upsertRec.Body.String())
		}
	})

	t.Run("legitimate same-owner non-deleted collision still updates", func(t *testing.T) {
		createReq := httptest.NewRequest(http.MethodPost, basePath+"/",
			jsonBody(map[string]any{"title": "original", "slug": "composition-legit-update-slug"}))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("Authorization", "Bearer "+ownerJWT)
		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("seed: esperado 201, obtido %d: %s", createRec.Code, createRec.Body.String())
		}

		upsertReq := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{
			"title": "updated by its real owner", "slug": "composition-legit-update-slug",
			"on_conflict": "update", "conflict_columns": []string{"slug"},
		}))
		upsertReq.Header.Set("Content-Type", "application/json")
		upsertReq.Header.Set("Authorization", "Bearer "+ownerJWT)
		upsertRec := httptest.NewRecorder()
		router.ServeHTTP(upsertRec, upsertReq)
		if upsertRec.Code != http.StatusOK {
			t.Fatalf("esperado 200 (mesmo owner, linha viva — os dois guards devem deixar passar), obtido %d: %s", upsertRec.Code, upsertRec.Body.String())
		}
		var updated map[string]any
		if err := json.NewDecoder(upsertRec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		if updated["title"] != "updated by its real owner" {
			t.Fatalf("esperado update real, obtido title=%v", updated["title"])
		}
	})
}

// TestHandlerCreateOnConflictUpdatePolicyDenialReturns409 is the regression
// test for the third pre-release review's rls:"policy" finding:
// on_conflict:"update" colliding with a row a native Postgres UPDATE policy
// denies raises Postgres 42501 (insufficient_privilege /
// row_security_violation), which no branch classified — it fell through as
// a raw 500 "failed to insert row", the exact class of unclassified
// failure this feature was written to eliminate. on_conflict:"ignore"
// already answered 409 on the identical input (its own conflict
// rejection); "update" disagreeing was the bug.
func TestHandlerCreateOnConflictUpdatePolicyDenialReturns409(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()

	const schema = "rls_policy_conflict_test_app"
	const secret = "rls-policy-conflict-jwt-secret"

	setup := []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		`CREATE TABLE ` + schema + `."_auth_users" (
			"id"            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"         TEXT NOT NULL UNIQUE,
			"password_hash" TEXT NOT NULL
		)`,
		`CREATE TABLE ` + schema + `.items (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			slug       TEXT UNIQUE,
			owner_id   UUID NOT NULL REFERENCES ` + schema + `."_auth_users"("id"),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`ALTER TABLE ` + schema + `.items ENABLE ROW LEVEL SECURITY`,
		`CREATE POLICY select_all ON ` + schema + `.items FOR SELECT TO zeep_app_enduser USING (true)`,
		`CREATE POLICY insert_any ON ` + schema + `.items FOR INSERT TO zeep_app_enduser WITH CHECK (true)`,
		// Only the owning user can UPDATE their own row — this is what turns
		// an on_conflict:"update" collision on another user's row into a
		// Postgres-level denial (42501) instead of a normal row update.
		`CREATE POLICY update_own ON ` + schema + `.items FOR UPDATE TO zeep_app_enduser
			USING (owner_id = current_setting('app.jwt_sub', true)::UUID)`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	testReg.Register(&registry.App{
		Config: config.AppConfig{
			Name: schema,
			Auth: config.AuthConfig{JWTSecret: secret, Providers: config.AuthProviders{Email: true}},
		},
		SchemaName: schema,
		Tables: map[string]*registry.Table{
			"items": {
				Name: "items",
				RLS:  "policy",
				Columns: []registry.Column{
					{Name: "title", Type: "text", Required: true},
					{Name: "slug", Type: "text", Required: false, Unique: true},
				},
			},
		},
	})
	t.Cleanup(func() { testReg.Unregister(schema) })

	var victimID, attackerID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ('policy-victim@test.com', 'x') RETURNING id`,
	).Scan(&victimID); err != nil {
		t.Fatalf("insert victim: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ('policy-attacker@test.com', 'x') RETURNING id`,
	).Scan(&attackerID); err != nil {
		t.Fatalf("insert attacker: %v", err)
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/" + schema + "/items"

	victimJWT, err := auth.IssueJWT([]byte(secret), victimID, "policy-victim@test.com", schema, "member")
	if err != nil {
		t.Fatalf("IssueJWT victim: %v", err)
	}
	attackerJWT, err := auth.IssueJWT([]byte(secret), attackerID, "policy-attacker@test.com", schema, "member")
	if err != nil {
		t.Fatalf("IssueJWT attacker: %v", err)
	}

	seedReq := httptest.NewRequest(http.MethodPost, basePath+"/",
		jsonBody(map[string]any{"title": "victim's item", "slug": "policy-conflict-slug"}))
	seedReq.Header.Set("Content-Type", "application/json")
	seedReq.Header.Set("Authorization", "Bearer "+victimJWT)
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("seed da vítima: esperado 201, obtido %d: %s", seedRec.Code, seedRec.Body.String())
	}

	attackReq := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{
		"title":            "attacker's attempted update",
		"slug":             "policy-conflict-slug",
		"on_conflict":      "update",
		"conflict_columns": []string{"slug"},
	}))
	attackReq.Header.Set("Content-Type", "application/json")
	attackReq.Header.Set("Authorization", "Bearer "+attackerJWT)
	attackRec := httptest.NewRecorder()
	router.ServeHTTP(attackRec, attackReq)

	if attackRec.Code != http.StatusConflict {
		t.Fatalf("esperado 409 (colisão negada pela policy de UPDATE), obtido %d: %s", attackRec.Code, attackRec.Body.String())
	}
}

// TestHandlerCreatePlainInsertDeniedByPolicyStaysGeneric500 is the
// regression test for the fourth pre-release review's finding: Postgres
// raises the same 42501 (row_security_violation) SQLSTATE both for an
// on_conflict:"update" collision a policy denies (409 is correct there —
// see the test above) AND for a plain INSERT a table's WITH CHECK policy
// rejects outright, unrelated to any conflict. Gating the 42501->409
// branch on onConflict == "update" keeps this second, unrelated case on the
// pre-existing generic-500 path — answering 409 "row already exists" here
// would be a factual lie (the row was never created, and never existed).
func TestHandlerCreatePlainInsertDeniedByPolicyStaysGeneric500(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()

	const schema = "rls_policy_insert_denied_test_app"
	const secret = "rls-policy-insert-denied-jwt-secret"

	setup := []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		`CREATE TABLE ` + schema + `."_auth_users" (
			"id"            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"         TEXT NOT NULL UNIQUE,
			"password_hash" TEXT NOT NULL
		)`,
		`CREATE TABLE ` + schema + `.items (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			owner_id   UUID NOT NULL REFERENCES ` + schema + `."_auth_users"("id"),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`ALTER TABLE ` + schema + `.items ENABLE ROW LEVEL SECURITY`,
		// No conflict involved anywhere here — WITH CHECK (false) denies
		// every INSERT outright, reproducing the "unrelated to any
		// conflict" 42501 case.
		`CREATE POLICY deny_all_inserts ON ` + schema + `.items FOR INSERT TO zeep_app_enduser WITH CHECK (false)`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	testReg.Register(&registry.App{
		Config: config.AppConfig{
			Name: schema,
			Auth: config.AuthConfig{JWTSecret: secret, Providers: config.AuthProviders{Email: true}},
		},
		SchemaName: schema,
		Tables: map[string]*registry.Table{
			"items": {
				Name: "items",
				RLS:  "policy",
				Columns: []registry.Column{
					{Name: "title", Type: "text", Required: true},
				},
			},
		},
	})
	t.Cleanup(func() { testReg.Unregister(schema) })

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ('policy-insert-denied@test.com', 'x') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/" + schema + "/items"

	jwt, err := auth.IssueJWT([]byte(secret), userID, "policy-insert-denied@test.com", schema, "member")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}

	// Plain insert, no on_conflict field at all — nothing to conflict with.
	req := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{"title": "will be denied"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("esperado 500 genérico (negado por WITH CHECK, não é conflito), obtido %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if resp["error"] != "failed to insert row" {
		t.Fatalf("esperada mensagem genérica fixa, obtido %v", resp["error"])
	}
}

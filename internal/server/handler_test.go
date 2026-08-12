package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// ----------------------------------------------------------------------------

const testSchema = "testhandler"
const testTable = "items"

const (
	rlsSchema  = "rls_test_app"
	rlsAppName = "rls_test_app"
	rlsSecret  = "rls-jwt-secret"
)

var (
	testPool *db.Pool
	testReg  *registry.Registry
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		os.Exit(0)
	}

	ctx := context.Background()
	var err error
	testPool, err = db.New(ctx, dsn)
	if err != nil {
		panic("TestMain: falha ao conectar no banco: " + err.Error())
	}
	defer testPool.Close()

	// Bootstraps zeep_app_enduser (role + membership) — WithRLSContext
	// (end-user-row-policies T5/T6) needs it to SET LOCAL ROLE. Fixture
	// tables below are created with raw SQL (not through
	// provisioner.Apply), so they still need their own explicit GRANT —
	// ProvisionZeepSystem only bootstraps the role itself.
	if err := dashboard.ProvisionZeepSystem(ctx, testPool); err != nil {
		panic("TestMain: ProvisionZeepSystem falhou: " + err.Error())
	}

	setup := []string{
		"DROP SCHEMA IF EXISTS " + testSchema + " CASCADE",
		"CREATE SCHEMA " + testSchema,
		`CREATE TABLE ` + testSchema + `.` + testTable + ` (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name        TEXT NOT NULL,
			value       TEXT,
			-- executed_as has no application meaning: it exists only so
			-- TestHandlerRunsAsEnduserRole (end-user-row-policies T6) can
			-- prove, from outside the process, which Postgres role actually
			-- executed the INSERT the HTTP handler issued.
			executed_as TEXT NOT NULL DEFAULT current_user,
			created_at  TIMESTAMPTZ DEFAULT now(),
			updated_at  TIMESTAMPTZ DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + testSchema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + testSchema + ` TO zeep_app_enduser`,
	}
	for _, sql := range setup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			panic("TestMain: setup falhou: " + err.Error())
		}
	}

	rlsSetup := []string{
		"DROP SCHEMA IF EXISTS " + rlsSchema + " CASCADE",
		"CREATE SCHEMA " + rlsSchema,
		`CREATE TABLE ` + rlsSchema + `."_auth_users" (
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
		`CREATE TABLE ` + rlsSchema + `.notes (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			owner_id   UUID NOT NULL REFERENCES ` + rlsSchema + `."_auth_users"("id"),
			created_at TIMESTAMPTZ DEFAULT now(),
			updated_at TIMESTAMPTZ DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + rlsSchema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + rlsSchema + ` TO zeep_app_enduser`,
	}
	for _, sql := range rlsSetup {
		if _, err := testPool.Exec(ctx, sql); err != nil {
			panic("TestMain: rls setup falhou: " + err.Error())
		}
	}

	testReg = registry.New()
	_ = testReg.Load(&config.Config{
		Apps: []config.AppConfig{
			{
				Name: "testhandler",
				Auth: config.AuthConfig{JWTSecret: "test-secret"},
				Tables: []config.TableConfig{
					{
						Name: testTable,
						Columns: []config.ColumnConfig{
							{Name: "name", Type: "text", Required: true},
							{Name: "value", Type: "text", Required: false},
						},
					},
				},
			},
			{
				Name: rlsAppName,
				Auth: config.AuthConfig{
					JWTSecret: rlsSecret,
					Providers: config.AuthProviders{Email: true},
				},
				Tables: []config.TableConfig{
					{
						Name: "notes",
						RLS:  "owner",
						Columns: []config.ColumnConfig{
							{Name: "title", Type: "text", Required: true},
						},
					},
				},
			},
		},
	})

	code := m.Run()

	_, _ = testPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+testSchema+" CASCADE")
	_, _ = testPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+rlsSchema+" CASCADE")

	os.Exit(code)
}

// The route does not use JWTMiddleware: it injects the app directly into context.
func buildHandlerRouter(h *Handler) http.Handler {
	app, _ := testReg.Get("testhandler")

	injectApp := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), appContextKey, app)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	r := chi.NewRouter()
	r.Use(injectApp)
	r.Get("/health", h.HandleHealth)
	r.Get("/{table}", h.HandleList)
	r.Post("/{table}", h.HandleCreate)
	r.Get("/{table}/{id}", h.HandleGetByID)
	r.Patch("/{table}/{id}", h.HandleUpdate)
	r.Delete("/{table}/{id}", h.HandleDelete)
	return r
}

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// ----------------------------------------------------------------------------

func TestHandlerCRUD(t *testing.T) {
	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	var createdID string

	t.Run("CreateReturns201", func(t *testing.T) {
		body := map[string]any{"name": "foo", "value": "bar"}
		req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		id, ok := row["id"].(string)
		if !ok || id == "" {
			t.Fatal("row sem id")
		}
		if row["name"] != "foo" {
			t.Fatalf("name esperado 'foo', obtido %v", row["name"])
		}
		createdID = id
	})

	t.Run("ListReturnsData", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/"+testTable, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		data, ok := resp["data"].([]any)
		if !ok {
			t.Fatal("campo 'data' ausente ou tipo errado")
		}
		if len(data) == 0 {
			t.Fatal("esperado ao menos 1 item em data")
		}
		if _, ok := resp["count"]; !ok {
			t.Fatal("campo 'count' ausente")
		}
	})

	t.Run("GetByIDFound", func(t *testing.T) {
		if createdID == "" {
			t.Skip("CreateReturns201 não gerou ID")
		}
		req := httptest.NewRequest(http.MethodGet, "/"+testTable+"/"+createdID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		if row["id"] != createdID {
			t.Fatalf("id esperado %s, obtido %v", createdID, row["id"])
		}
	})

	t.Run("GetByIDNotFound404", func(t *testing.T) {
		fakeID := "00000000-0000-0000-0000-000000000000"
		req := httptest.NewRequest(http.MethodGet, "/"+testTable+"/"+fakeID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("UpdatePartial", func(t *testing.T) {
		if createdID == "" {
			t.Skip("CreateReturns201 não gerou ID")
		}
		body := map[string]any{"value": "updated"}
		req := httptest.NewRequest(http.MethodPatch, "/"+testTable+"/"+createdID, jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}

		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode falhou: %v", err)
		}
		if row["value"] != "updated" {
			t.Fatalf("value esperado 'updated', obtido %v", row["value"])
		}
		if row["name"] != "foo" {
			t.Fatalf("name não deveria mudar, obtido %v", row["name"])
		}
	})

	t.Run("DeleteReturns204", func(t *testing.T) {
		if createdID == "" {
			t.Skip("CreateReturns201 não gerou ID")
		}
		req := httptest.NewRequest(http.MethodDelete, "/"+testTable+"/"+createdID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("esperado 204, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DeleteNotFound404", func(t *testing.T) {
		fakeID := "00000000-0000-0000-0000-000000000000"
		req := httptest.NewRequest(http.MethodDelete, "/"+testTable+"/"+fakeID, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// ----------------------------------------------------------------------------

// TestHandlerRunsAsEnduserRole covers ROWPOL-14/15: an end-user request
// through HandleCreate must execute its INSERT as db.EnduserRole
// (zeep_app_enduser), not the pool's connecting/owner role — proving the
// WithTimeout → WithRLSContext swap actually changed which Postgres role
// runs the query, not just that the old tests still pass. executed_as is a
// column whose DEFAULT is current_user, captured server-side at INSERT time
// (see TestMain's table DDL) — a value this test process cannot fake.
func TestHandlerRunsAsEnduserRole(t *testing.T) {
	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"name": "role-probe"}
	req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
	}

	var row map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if row["executed_as"] != "zeep_app_enduser" {
		t.Fatalf("expected the INSERT to run as zeep_app_enduser, got executed_as=%v", row["executed_as"])
	}
}

// ----------------------------------------------------------------------------

func TestHandlerHealth(t *testing.T) {
	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("status esperado 'ok', obtido %v", resp["status"])
	}
	if _, ok := resp["apps"]; !ok {
		t.Fatal("campo 'apps' ausente")
	}
}

// ----------------------------------------------------------------------------

func TestHandlerCreateInvalidBody(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/"+testTable, bytes.NewBufferString("not-json{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateUnknownField(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"name": "x", "nonexistent_field": "y"}
	req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 por campo desconhecido, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerListUnknownTable(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent_table", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// ----------------------------------------------------------------------------
// rls-policy-mode T2: resolveOwner/filterOwner decouple "owner_id value to
// write" from "owner_id filter to apply" (spec RLSP-01/03/04).

func TestResolveOwner(t *testing.T) {
	userCtx := auth.WithUser(context.Background(), &auth.AuthUser{ID: "user-123"})
	anonCtx := context.Background()

	cases := []struct {
		name      string
		rls       string
		ctx       context.Context
		wantOwner string
		wantOK    bool
	}{
		{"no rls + user → no owner_id needed", "", userCtx, "", true},
		{"no rls + no user → no owner_id needed", "", anonCtx, "", true},
		{"owner + user → real owner_id", "owner", userCtx, "user-123", true},
		{"owner + no user → unauthorized", "owner", anonCtx, "", false},
		{"enabled + user → real owner_id", "enabled", userCtx, "user-123", true},
		{"enabled + no user → unauthorized", "enabled", anonCtx, "", false},
		{"policy + user → real owner_id (still populated for INSERT)", "policy", userCtx, "user-123", true},
		{"policy + no user → unauthorized", "policy", anonCtx, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			table := &registry.Table{RLS: c.rls}
			gotOwner, gotOK := resolveOwner(c.ctx, table)
			if gotOwner != c.wantOwner || gotOK != c.wantOK {
				t.Fatalf("resolveOwner(rls=%q) = (%q, %v), want (%q, %v)", c.rls, gotOwner, gotOK, c.wantOwner, c.wantOK)
			}
		})
	}
}

func TestFilterOwner(t *testing.T) {
	cases := []struct {
		name string
		rls  string
		want string
	}{
		{"no rls → never a filter", "", ""},
		{"owner → filters by owner_id", "owner", "user-123"},
		{"enabled → filters by owner_id", "enabled", "user-123"},
		{"policy → never a filter, native policies decide visibility", "policy", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			table := &registry.Table{RLS: c.rls}
			got := filterOwner("user-123", table)
			if got != c.want {
				t.Fatalf("filterOwner(%q, rls=%q) = %q, want %q", "user-123", c.rls, got, c.want)
			}
		})
	}
}

// setupPolicyModeFixture registers a fresh rls:"policy" table (no native
// policy created — that is provisioner/dashboard's job in later tasks; this
// task only proves the HTTP layer stops applying the automatic owner_id
// filter and still populates owner_id on INSERT) and seeds one row owned by
// a different user, so a filter regression (list/get scoped to $sub) would
// be visible as a 0-row/404 result instead of the row actually being there.
func setupPolicyModeFixture(t *testing.T) (otherUserRowID, otherUserID string) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()
	const schema = "rls_policy_mode_test_app"

	setup := []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		`CREATE TABLE ` + schema + `."_auth_users" (
			"id"            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			"email"         TEXT NOT NULL UNIQUE,
			"password_hash" TEXT NOT NULL
		)`,
		`CREATE TABLE ` + schema + `.posts (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			owner_id   UUID NOT NULL REFERENCES ` + schema + `."_auth_users"("id"),
			created_at TIMESTAMPTZ DEFAULT now(),
			updated_at TIMESTAMPTZ DEFAULT now()
		)`,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO zeep_app_enduser`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO zeep_app_enduser`,
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
			Name: "rls_policy_mode_test_app",
			Auth: config.AuthConfig{
				JWTSecret: "rls-policy-mode-secret",
				Providers: config.AuthProviders{Email: true},
			},
		},
		SchemaName: schema,
		Tables: map[string]*registry.Table{
			"posts": {
				Name: "posts",
				RLS:  "policy",
				Columns: []registry.Column{
					{Name: "title", Type: "text", Required: true},
				},
			},
		},
	})
	t.Cleanup(func() { testReg.Unregister("rls_policy_mode_test_app") })

	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ('other-user@test.com', 'x') RETURNING id`,
	).Scan(&otherUserID); err != nil {
		t.Fatalf("insert other user: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`.posts (title, owner_id) VALUES ('other user post', $1) RETURNING id`,
		otherUserID,
	).Scan(&otherUserRowID); err != nil {
		t.Fatalf("seed other user's row: %v", err)
	}
	return otherUserRowID, otherUserID
}

// TestPolicyMode_ListAndGetSeeOtherUsersRow proves RLSP-01/04: a "policy"
// table's list/get no longer apply the automatic owner_id = $sub filter —
// the calling user sees a row owned by someone else (visibility is left to
// native Postgres policies, none of which exist in this fixture, so nothing
// in the app layer itself restricts the result).
func TestPolicyMode_ListAndGetSeeOtherUsersRow(t *testing.T) {
	otherUserRowID, _ := setupPolicyModeFixture(t)
	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/rls_policy_mode_test_app/posts"

	jwt, err := auth.IssueJWT([]byte("rls-policy-mode-secret"), "calling-user-id", "caller@test.com", "rls_policy_mode_test_app", "member")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}
	bearer := "Bearer " + jwt

	t.Run("List", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", bearer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		data, _ := resp["data"].([]any)
		if len(data) != 1 {
			t.Fatalf("esperado ver a linha de outro usuário (nenhum filtro owner_id em rls:policy), obtido %d item(s)", len(data))
		}
	})

	t.Run("GetByID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/"+otherUserRowID+"/", nil)
		req.Header.Set("Authorization", bearer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200 (linha de outro usuário visível sem filtro owner_id), obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Update and Delete prove AC P1-4's other half: the system SHALL NOT
	// apply WHERE owner_id = $sub to UPDATE/DELETE on a "policy" table
	// either. Under the pre-fix wiring (filterOwner replaced by ownerID at
	// the query.BuildUpdate/BuildDelete call sites) both would 404/0-affect,
	// since "calling-user-id" never owns otherUserRowID.
	t.Run("Update", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, basePath+"/"+otherUserRowID+"/",
			jsonBody(map[string]any{"title": "editado por outro usuário"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200 (UPDATE em linha de outro usuário sem filtro owner_id em rls:policy), obtido %d: %s", rec.Code, rec.Body.String())
		}
		var row map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if row["title"] != "editado por outro usuário" {
			t.Fatalf("title esperado atualizado, obtido %v (UPDATE não pode ter sido bloqueado por owner_id)", row["title"])
		}
	})

	t.Run("Delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, basePath+"/"+otherUserRowID+"/", nil)
		req.Header.Set("Authorization", bearer)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("esperado 204 (DELETE em linha de outro usuário sem filtro owner_id em rls:policy), obtido %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestPolicyMode_CreatePopulatesOwnerID proves RLSP-03: INSERT on a
// "policy" table still fills owner_id with the authenticated user's sub,
// exactly like "enabled" — the removal of the auto-filter on reads must not
// also break the NOT NULL owner_id write path.
func TestPolicyMode_CreatePopulatesOwnerID(t *testing.T) {
	setupPolicyModeFixture(t)
	ctx := context.Background()

	var creatingUserID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO rls_policy_mode_test_app."_auth_users" (email, password_hash) VALUES ('creator@test.com', 'x') RETURNING id`,
	).Scan(&creatingUserID); err != nil {
		t.Fatalf("insert creating user: %v", err)
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/rls_policy_mode_test_app/posts"

	jwt, err := auth.IssueJWT([]byte("rls-policy-mode-secret"), creatingUserID, "creator@test.com", "rls_policy_mode_test_app", "member")
	if err != nil {
		t.Fatalf("IssueJWT: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{"title": "novo post"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
	}
	var row map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ownerID, _ := row["owner_id"].(string)
	if ownerID != creatingUserID {
		t.Fatalf("owner_id = %q, want %q (o sub do usuário autenticado)", ownerID, creatingUserID)
	}
}

package server

import (
	"bytes"
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
			-- status is an enum column (column-enum-type T9): exists only so
			-- TestHandlerCreate/UpdateEnumViolation can exercise the 23514
			-- check_violation -> 400 mapping against a real CHECK constraint.
			status      TEXT CHECK (status IN ('pending', 'active', 'closed')),
			-- executed_as has no application meaning: it exists only so
			-- TestHandlerRunsAsEnduserRole (end-user-row-policies T6) can
			-- prove, from outside the process, which Postgres role actually
			-- executed the INSERT the HTTP handler issued.
			executed_as TEXT NOT NULL DEFAULT current_user,
			created_at  TIMESTAMPTZ DEFAULT now(),
			updated_at  TIMESTAMPTZ DEFAULT now()
		)`,
		// insert_diag exists only for insert-error-diagnostics tests: opened_at
		// (required timestamptz) exercises the "required column still 400s"
		// case, closed_at (nullable timestamptz) exercises "" -> NULL
		// normalization, external_id (unique) exercises 409/on_conflict.
		`CREATE TABLE ` + testSchema + `.insert_diag (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			label       TEXT NOT NULL,
			opened_at   TIMESTAMPTZ NOT NULL,
			closed_at   TIMESTAMPTZ,
			external_id TEXT UNIQUE,
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
		// slug exists only for TestHandlerCreateOnConflictIgnoreDoesNotLeakOtherOwnersRow
		// (insert-error-diagnostics cross-tenant fix): it's the unique column
		// two different owners can collide on via on_conflict/conflict_columns.
		`CREATE TABLE ` + rlsSchema + `.notes (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title      TEXT NOT NULL,
			slug       TEXT UNIQUE,
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
	testReg.Register(&registry.App{
		Config:     config.AppConfig{Name: "testhandler", Auth: config.AuthConfig{JWTSecret: "test-secret"}},
		SchemaName: "testhandler",
		Tables: map[string]*registry.Table{
			testTable: {
				Name: testTable,
				Columns: []registry.Column{
					{Name: "name", Type: "text", Required: true},
					{Name: "value", Type: "text", Required: false},
					{Name: "status", Type: "enum", Required: false, AllowedValues: []string{"pending", "active", "closed"}},
				},
			},
			"insert_diag": {
				Name: "insert_diag",
				Columns: []registry.Column{
					{Name: "label", Type: "text", Required: true},
					{Name: "opened_at", Type: "timestamptz", Required: true},
					{Name: "closed_at", Type: "timestamptz", Required: false},
					{Name: "external_id", Type: "text", Required: false, Unique: true},
				},
			},
		},
	})
	testReg.Register(&registry.App{
		Config: config.AppConfig{
			Name: rlsAppName,
			Auth: config.AuthConfig{
				JWTSecret: rlsSecret,
				Providers: config.AuthProviders{Email: true},
			},
		},
		SchemaName: rlsAppName,
		Tables: map[string]*registry.Table{
			"notes": {
				Name: "notes",
				RLS:  "owner",
				Columns: []registry.Column{
					{Name: "title", Type: "text", Required: true},
					{Name: "slug", Type: "text", Required: false, Unique: true},
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
func setupPolicyModeFixture(t *testing.T) (otherUserRowID, otherUserID, callingUserID string) {
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

	// A real UUID sub, distinct from otherUserID, is required so the
	// filterOwner->ownerID mutation dies as a genuine 0-row/404 (the calling
	// user's owner_id legitimately doesn't match) instead of a 500 from an
	// invalid ::uuid cast on a non-UUID sub — a weaker, accidental kill.
	if err := testPool.QueryRow(ctx,
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ('calling-user@test.com', 'x') RETURNING id`,
	).Scan(&callingUserID); err != nil {
		t.Fatalf("insert calling user: %v", err)
	}
	return otherUserRowID, otherUserID, callingUserID
}

// TestPolicyMode_ListAndGetSeeOtherUsersRow proves RLSP-01/04: a "policy"
// table's list/get no longer apply the automatic owner_id = $sub filter —
// the calling user sees a row owned by someone else (visibility is left to
// native Postgres policies, none of which exist in this fixture, so nothing
// in the app layer itself restricts the result).
func TestPolicyMode_ListAndGetSeeOtherUsersRow(t *testing.T) {
	otherUserRowID, _, callingUserID := setupPolicyModeFixture(t)
	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)
	basePath := "/rls_policy_mode_test_app/posts"

	jwt, err := auth.IssueJWT([]byte("rls-policy-mode-secret"), callingUserID, "calling-user@test.com", "rls_policy_mode_test_app", "member")
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

// ----------------------------------------------------------------------------
// column-enum-type T9: map Postgres 23514 (check_violation) on the app-table
// write path to a safe 400, instead of the previous generic 500.

func TestHandlerCreateEnumViolation(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"name": "enum-create-reject", "status": "qualquer coisa"}
	req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para valor fora do enum, obtido %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	msg, _ := resp["error"].(string)
	if msg == "" {
		t.Fatal("esperada mensagem de erro não vazia")
	}
	if strings.Contains(msg, "qualquer coisa") {
		t.Fatalf("mensagem de erro não deve ecoar o valor tentado (raw Postgres detail leak): %q", msg)
	}
}

func TestHandlerCreateEnumValid(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"name": "enum-create-happy", "status": "pending"}
	req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201 para valor válido do enum, obtido %d: %s", rec.Code, rec.Body.String())
	}
	var row map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&row); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if row["status"] != "pending" {
		t.Fatalf("esperado status=pending, obtido %v", row["status"])
	}
}

func TestHandlerUpdateEnumViolation(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	// Create a valid row first.
	createBody := map[string]any{"name": "enum-update-reject", "status": "pending"}
	createReq := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup: esperado 201, obtido %d: %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	id, _ := created["id"].(string)

	updateBody := map[string]any{"status": "qualquer coisa"}
	updateReq := httptest.NewRequest(http.MethodPatch, "/"+testTable+"/"+id, jsonBody(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	router.ServeHTTP(updateRec, updateReq)

	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para valor fora do enum, obtido %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(updateRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	msg, _ := resp["error"].(string)
	if msg == "" {
		t.Fatal("esperada mensagem de erro não vazia")
	}
	if strings.Contains(msg, "qualquer coisa") {
		t.Fatalf("mensagem de erro não deve ecoar o valor tentado (raw Postgres detail leak): %q", msg)
	}
}

// TestHandlerCreateOtherErrorStillGeneric500 proves the 23514 branch is
// narrowly scoped: a different Postgres-level write failure reaching the
// same code path — here a NUL byte embedded in a text value, which
// Postgres rejects with SQLSTATE 22021 (invalid_byte_sequence), not 23514
// — must NOT be caught by the new check_violation branch and must still
// fall through to the existing generic 500 path.
func TestHandlerCreateOtherErrorStillGeneric500(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"name": "enum-other-error", "value": "a\x00b"}
	req := httptest.NewRequest(http.MethodPost, "/"+testTable, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("esperado 500 para erro não relacionado a enum (22021), obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerCreateEmptyTimestampNormalizesToNull proves "" sent for a
// nullable timestamptz column succeeds and stores NULL, end-to-end through
// the HTTP handler (INSERTERR-06).
func TestHandlerCreateEmptyTimestampNormalizesToNull(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "normalize-empty", "opened_at": "2026-01-01T00:00:00Z", "closed_at": ""}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
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
	if row["closed_at"] != nil {
		t.Errorf("esperado closed_at nulo, obtido %v", row["closed_at"])
	}
}

// TestHandlerCreateEmptyTimestampOnRequiredColumnFails proves "" sent for a
// required timestamptz column still 400s, naming the column, instead of
// being silently normalized (INSERTERR-07).
func TestHandlerCreateEmptyTimestampOnRequiredColumnFails(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "empty-required", "opened_at": ""}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "opened_at") {
		t.Fatalf("esperada mensagem citando opened_at, obtido %q", msg)
	}
}

// TestHandlerCreateOnConflictInvalidValue proves an unrecognized on_conflict
// value 400s instead of silently falling back (INSERTERR-15).
func TestHandlerCreateOnConflictInvalidValue(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "x", "opened_at": "2026-01-01T00:00:00Z", "on_conflict": "bogus"}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para on_conflict inválido, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerCreateOnConflictUpdateRequiresConflictColumns proves
// on_conflict:"update" without conflict_columns 400s (INSERTERR-13).
func TestHandlerCreateOnConflictUpdateRequiresConflictColumns(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "x", "opened_at": "2026-01-01T00:00:00Z", "on_conflict": "update"}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para on_conflict update sem conflict_columns, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerCreateOnConflictUnknownConflictColumn proves an unknown column
// name in conflict_columns 400s naming it (INSERTERR-17).
func TestHandlerCreateOnConflictUnknownConflictColumn(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{
		"label": "x", "opened_at": "2026-01-01T00:00:00Z",
		"on_conflict": "update", "conflict_columns": []string{"does_not_exist"},
	}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para conflict_columns com coluna inexistente, obtido %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "does_not_exist") {
		t.Fatalf("esperada mensagem citando does_not_exist, obtido %q", msg)
	}
}

// TestHandlerCreateOnConflictDuplicateConflictColumn proves parseOnConflict
// rejects a repeated column name in conflict_columns as 400, instead of
// letting it reach Postgres as ON CONFLICT (col, col), which raises 42P10
// (invalid_column_reference) and would otherwise fall through to a raw 500.
func TestHandlerCreateOnConflictDuplicateConflictColumn(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{
		"label": "x", "opened_at": "2026-01-01T00:00:00Z",
		"on_conflict": "update", "conflict_columns": []string{"external_id", "external_id"},
	}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para conflict_columns duplicada, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerCreateOnConflictColumnWithoutUniqueConstraint proves a
// conflict_columns target that passes schema validation (a real column) but
// has no unique/exclusion constraint backing it in Postgres is classified as
// 400, not left to fall through as a raw 500 from an unclassified 42P10.
func TestHandlerCreateOnConflictColumnWithoutUniqueConstraint(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{
		"label": "x", "opened_at": "2026-01-01T00:00:00Z",
		"on_conflict": "update", "conflict_columns": []string{"label"},
	}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 para conflict_columns sem constraint unique, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerCreateOnConflictAbsentBehavesAsError proves omitting
// on_conflict entirely doesn't change today's create behavior
// (INSERTERR-16).
func TestHandlerCreateOnConflictAbsentBehavesAsError(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "no-on-conflict", "opened_at": "2026-01-01T00:00:00Z"}
	req := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlerCreateOnConflictIgnoreWithTargetReturnsExistingRow proves a
// retried insert with on_conflict:"ignore" and conflict_columns returns 200
// with the pre-existing row on the second attempt (INSERTERR-10, INSERTERR-12).
func TestHandlerCreateOnConflictIgnoreWithTargetReturnsExistingRow(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{
		"label": "first", "opened_at": "2026-01-01T00:00:00Z",
		"external_id": "ignore-target-id", "on_conflict": "ignore", "conflict_columns": []string{"external_id"},
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup: esperado 201, obtido %d: %s", firstRec.Code, firstRec.Body.String())
	}
	var firstRow map[string]any
	if err := json.NewDecoder(firstRec.Body).Decode(&firstRow); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}

	secondBody := map[string]any{
		"label": "second-ignored", "opened_at": "2026-02-02T00:00:00Z",
		"external_id": "ignore-target-id", "on_conflict": "ignore", "conflict_columns": []string{"external_id"},
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(secondBody))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusOK {
		t.Fatalf("esperado 200 na segunda tentativa (ignore com conflito), obtido %d: %s", secondRec.Code, secondRec.Body.String())
	}
	var secondRow map[string]any
	if err := json.NewDecoder(secondRec.Body).Decode(&secondRow); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if secondRow["id"] != firstRow["id"] {
		t.Fatalf("esperado a mesma linha existente (id=%v), obtido id=%v", firstRow["id"], secondRow["id"])
	}
	if secondRow["label"] != "first" {
		t.Fatalf("esperado label da linha original ('first'), obtido %v - segundo insert não deveria ter escrito nada", secondRow["label"])
	}
}

// TestHandlerCreateOnConflictIgnoreWithoutTargetReturns204 proves a retried
// insert with on_conflict:"ignore" and no conflict_columns returns 204 with
// an empty body on the second attempt (INSERTERR-11).
func TestHandlerCreateOnConflictIgnoreWithoutTargetReturns204(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{
		"label": "first", "opened_at": "2026-01-01T00:00:00Z",
		"external_id": "ignore-no-target-id", "on_conflict": "ignore",
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup: esperado 201, obtido %d: %s", firstRec.Code, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusNoContent {
		t.Fatalf("esperado 204 na segunda tentativa (ignore sem alvo), obtido %d: %s", secondRec.Code, secondRec.Body.String())
	}
	if secondRec.Body.Len() != 0 {
		t.Fatalf("esperado corpo vazio, obtido: %s", secondRec.Body.String())
	}
}

// TestHandlerCreateOnConflictUpdateOverwritesRow proves a retried insert
// with on_conflict:"update" returns 200 with the updated row and a newer
// updated_at (INSERTERR-14 observable behavior, end-to-end).
func TestHandlerCreateOnConflictUpdateOverwritesRow(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{
		"label": "before-update", "opened_at": "2026-01-01T00:00:00Z",
		"external_id": "update-target-id",
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup: esperado 201, obtido %d: %s", firstRec.Code, firstRec.Body.String())
	}
	var firstRow map[string]any
	if err := json.NewDecoder(firstRec.Body).Decode(&firstRow); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}

	updateBody := map[string]any{
		"label": "after-update", "opened_at": "2026-01-01T00:00:00Z",
		"external_id": "update-target-id", "on_conflict": "update", "conflict_columns": []string{"external_id"},
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	router.ServeHTTP(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("esperado 200 no upsert de update, obtido %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var updatedRow map[string]any
	if err := json.NewDecoder(updateRec.Body).Decode(&updatedRow); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if updatedRow["id"] != firstRow["id"] {
		t.Fatalf("esperado atualizar a mesma linha (id=%v), obtido id=%v", firstRow["id"], updatedRow["id"])
	}
	if updatedRow["label"] != "after-update" {
		t.Fatalf("esperado label atualizado para 'after-update', obtido %v", updatedRow["label"])
	}
	if updatedRow["updated_at"] == firstRow["updated_at"] {
		t.Fatalf("esperado updated_at mais recente após update, obtido igual ao original: %v", updatedRow["updated_at"])
	}
}

// TestHandlerCreateOnConflictErrorStillConflicts proves a plain retry (no
// on_conflict field) against the same unique value still 409s, unaffected
// by the on_conflict machinery (regression check for INSERTERR-16's "error"
// default).
func TestHandlerCreateOnConflictErrorStillConflicts(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "first", "opened_at": "2026-01-01T00:00:00Z", "external_id": "plain-conflict-id"}
	firstReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup: esperado 201, obtido %d: %s", firstRec.Code, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusConflict {
		t.Fatalf("esperado 409 sem on_conflict, obtido %d: %s", secondRec.Code, secondRec.Body.String())
	}
}

// TestHandlerCreateUniqueViolation proves a duplicate insert against a
// unique column is classified 409 naming the constraint, not 500
// (INSERTERR-02, INSERTERR-04, INSERTERR-05).
func TestHandlerCreateUniqueViolation(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildHandlerRouter(h)

	body := map[string]any{"label": "first", "opened_at": "2026-01-01T00:00:00Z", "external_id": "dup-ext-id"}
	createReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(body))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup: esperado 201, obtido %d: %s", createRec.Code, createRec.Body.String())
	}

	dupBody := map[string]any{"label": "second", "opened_at": "2026-01-01T00:00:00Z", "external_id": "dup-ext-id"}
	dupReq := httptest.NewRequest(http.MethodPost, "/insert_diag", jsonBody(dupBody))
	dupReq.Header.Set("Content-Type", "application/json")
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dupReq)

	if dupRec.Code != http.StatusConflict {
		t.Fatalf("esperado 409 para violação de unique constraint, obtido %d: %s", dupRec.Code, dupRec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(dupRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "insert_diag_external_id_key") {
		t.Fatalf("esperada mensagem citando a constraint, obtido %q", msg)
	}
	if strings.Contains(msg, "dup-ext-id") {
		t.Fatalf("mensagem de erro não deve ecoar o valor tentado (raw Postgres detail leak): %q", msg)
	}
}

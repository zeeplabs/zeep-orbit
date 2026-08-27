package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// appsHandlerTestPool provisions the full zeep_system schema, seeds 8 actors
// (superadmin, admin global, auditor global, loner/owner, app admin/editor/
// viewer, outsider) and 1 test app owned by loner. Returns the pool, a Handler
// wired to it, the actors map (keyed by short name), the test appID, and the
// test tableID (for table CRUD tests).
//
// Skips if TEST_DATABASE_URL is not set.
func appsHandlerTestPool(t *testing.T) (*db.Pool, *Handler, map[string]*DashboardUser, string, string) {
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

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("drop schema: %v", err)
	}

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	// Seed 8 actors. Each gets a unique email so the UNIQUE constraint doesn't
	// trip when the test runs multiple times.
	actors := map[string]*DashboardUser{}
	actorDefs := []struct {
		key, email, role string
	}{
		{"super", fmt.Sprintf("super-%d@example.com", time.Now().UnixNano()), "superadmin"},
		{"admin", fmt.Sprintf("admin-%d@example.com", time.Now().UnixNano()), "admin"},
		{"auditor", fmt.Sprintf("auditor-%d@example.com", time.Now().UnixNano()), "auditor"},
		{"loner", fmt.Sprintf("loner-%d@example.com", time.Now().UnixNano()), "member"},
		{"appadmin", fmt.Sprintf("appadmin-%d@example.com", time.Now().UnixNano()), "member"},
		{"appeditor", fmt.Sprintf("appeditor-%d@example.com", time.Now().UnixNano()), "member"},
		{"appviewer", fmt.Sprintf("appviewer-%d@example.com", time.Now().UnixNano()), "member"},
		{"outsider", fmt.Sprintf("outsider-%d@example.com", time.Now().UnixNano()), "member"},
	}
	for _, ad := range actorDefs {
		u, err := CreateUser(ctx, pool, ad.email, ad.key, "hash", ad.role)
		if err != nil {
			pool.Close()
			t.Fatalf("create user %s: %v", ad.email, err)
		}
		actors[ad.key] = u
	}

	// Create test app owned by loner. The migration in ProvisionZeepSystem
	// already inserted loner as admin in app_members (from apps.owner_id).
	var appID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"test-app", actors["loner"].ID,
	).Scan(&appID); err != nil {
		pool.Close()
		t.Fatalf("create test app: %v", err)
	}

	// Belt-and-suspenders: ensure loner has an admin row in app_members.
	// (The migration handles this for ProvisionZeepSystem, but if a future
	// change drops the migration insert, this keeps the test valid.)
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
		appID, actors["loner"].ID,
	); err != nil {
		pool.Close()
		t.Fatalf("seed loner as admin: %v", err)
	}

	// Seed per-app roles for the explicit membership tests.
	for _, pr := range []struct {
		userKey, role string
	}{
		{"appadmin", "admin"},
		{"appeditor", "editor"},
		{"appviewer", "viewer"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, $3)`,
			appID, actors[pr.userKey].ID, pr.role,
		); err != nil {
			pool.Close()
			t.Fatalf("seed %s as %s: %v", pr.userKey, pr.role, err)
		}
	}

	// Create a test table for the table CRUD tests. This bypasses the
	// provisioner (which would try to create a physical Postgres table) — we
	// just need a row in app_tables for the handler to find.
	var tableID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.app_tables (app_id, name, rls, columns, indexes)
		 VALUES ($1, 'test_table', '', '[]'::jsonb, '[]'::jsonb) RETURNING id`,
		appID,
	).Scan(&tableID); err != nil {
		pool.Close()
		t.Fatalf("create test table: %v", err)
	}

	h := NewHandler(pool, registry.New(), zap.NewNop())

	return pool, h, actors, appID, tableID
}

// appsRBACCase describes one cell of the RBAC matrix: which actor hits which
// endpoint with which method/path/body, and what HTTP status is expected.
//
// For endpoints that go through the provisioner (CreateAppTable, UpdateAppTable,
// DeleteAppTable), the exact 2xx status depends on the provisioner's success.
// We use wantStatusMin/wantStatusMax to express a range: "status is NOT 4xx
// (auth check passed)" or "status is exactly 403 (auth check blocked)".
type appsRBACCase struct {
	name             string
	actor            string // key in actors map
	method           string
	pathFmt          string // format with appID/tableID as %s placeholders
	body             string
	wantExact        int // 0 = don't check exact status
	wantMin, wantMax int
}

// TestAppsRBACMatrix exercises the full per-app RBAC matrix: 8 actors × 7
// endpoints. The matrix is defined in the spec for rbac-per-app T-04.
func TestAppsRBACMatrix(t *testing.T) {
	pool, h, actors, appID, tableID := appsHandlerTestPool(t)
	defer pool.Close()

	// Helper: build a request with the actor's user in context and the given
	// path/body, then dispatch to the named handler method.
	call := func(t *testing.T, c appsRBACCase) int {
		t.Helper()
		actor, ok := actors[c.actor]
		if !ok {
			t.Fatalf("unknown actor %q", c.actor)
		}
		// pathFmt has 0, 1, or 2 %s placeholders depending on the case — only
		// pass as many args as it actually takes, or fmt.Sprintf appends a
		// "%!(EXTRA ...)" suffix that corrupts the URL (and, in turn, the
		// synthesized HTTP request line).
		var path string
		switch strings.Count(c.pathFmt, "%s") {
		case 0:
			path = c.pathFmt
		case 1:
			path = fmt.Sprintf(c.pathFmt, appID)
		default:
			path = fmt.Sprintf(c.pathFmt, appID, tableID)
		}
		var body *bytes.Reader
		if c.body != "" {
			body = bytes.NewReader([]byte(c.body))
		} else {
			body = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(c.method, path, body)
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req = withUser(req, actor)
		// Attach chi URL params (the handler reads chi.URLParam).
		rctx := chi.NewRouteContext()
		for _, seg := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
			rctx.URLParams.Add(seg, seg) // dummy; real params set below
		}
		// Reset and set the right params for this path.
		rctx = chi.NewRouteContext()
		switch {
		case strings.Contains(path, "/tables/"):
			rctx.URLParams.Add("id", appID)
			rctx.URLParams.Add("tableId", tableID)
		case strings.Contains(path, "/secret"):
			rctx.URLParams.Add("id", appID)
		default:
			rctx.URLParams.Add("id", appID)
		}
		req = req.WithContext(req.Context())
		req = req.WithContext(withCtx(req, rctx))
		w := httptest.NewRecorder()

		// Dispatch to the right handler method.
		switch {
		case c.method == http.MethodGet && strings.HasSuffix(path, "/apps"):
			h.ListApps(w, req)
		case c.method == http.MethodGet && strings.HasSuffix(path, "/secret"):
			h.GetAppSecret(w, req)
		case c.method == http.MethodGet && strings.Contains(path, "/apps/") && !strings.Contains(path, "/tables"):
			h.GetApp(w, req)
		case c.method == http.MethodPut && !strings.Contains(path, "/tables"):
			h.UpdateApp(w, req)
		case c.method == http.MethodDelete && !strings.Contains(path, "/tables"):
			h.DeleteApp(w, req)
		case c.method == http.MethodPost && strings.Contains(path, "/tables"):
			h.CreateAppTable(w, req)
		case c.method == http.MethodPut && strings.Contains(path, "/tables"):
			h.UpdateAppTable(w, req)
		case c.method == http.MethodDelete && strings.Contains(path, "/tables"):
			h.DeleteAppTable(w, req)
		default:
			t.Fatalf("unhandled case: %s %s", c.method, path)
		}

		return w.Code
	}

	checkStatus := func(t *testing.T, got int, c appsRBACCase) {
		t.Helper()
		if c.wantExact != 0 {
			if got != c.wantExact {
				t.Errorf("status = %d, want %d", got, c.wantExact)
			}
			return
		}
		if c.wantMin != 0 && got < c.wantMin {
			t.Errorf("status = %d, want >= %d", got, c.wantMin)
		}
		if c.wantMax != 0 && got > c.wantMax {
			t.Errorf("status = %d, want <= %d", got, c.wantMax)
		}
	}

	// The matrix. For each (actor, endpoint) cell, the expected status:
	//   - superadmin/loner/appadmin: auth passes → 2xx (provisioner-dependent
	//     endpoints use wantMin=200, wantMax=299 to tolerate provisioner variance)
	//   - admin/auditor global: read access via CanReadAnyApp, write blocked → 200
	//     for ListApps/GetApp, 403 for the rest
	//   - appeditor: CanWrite but not CanManage → 200 for ListApps/GetApp/table
	//     CRUD, 403 for config endpoints (UpdateApp, DeleteApp, GetAppSecret)
	//   - appviewer: read only → 200 for ListApps/GetApp, 403 for the rest
	//   - outsider: no membership → 200 with empty list for ListApps, 404 for the rest
	cases := []appsRBACCase{
		// --- ListApps (GET /apps) ---
		// Everyone can list; the response is filtered by membership.
		{"super_list_apps", "super", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"admin_global_list_apps", "admin", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"auditor_global_list_apps", "auditor", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"loner_list_apps", "loner", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"appadmin_list_apps", "appadmin", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"appeditor_list_apps", "appeditor", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"appviewer_list_apps", "appviewer", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},
		{"outsider_list_apps", "outsider", http.MethodGet, "/dashboard/api/apps", "", 200, 0, 0},

		// --- GetApp (GET /apps/{id}) ---
		// Members + global readers get 200; non-members get 404.
		{"super_get_app", "super", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"admin_global_get_app", "admin", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"auditor_global_get_app", "auditor", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"loner_get_app", "loner", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"appadmin_get_app", "appadmin", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"appeditor_get_app", "appeditor", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"appviewer_get_app", "appviewer", http.MethodGet, "/dashboard/api/apps/%s", "", 200, 0, 0},
		{"outsider_get_app", "outsider", http.MethodGet, "/dashboard/api/apps/%s", "", 0, 404, 404},

		// --- UpdateApp (PUT /apps/{id}) — CanManage required ---
		// Admin (super/loner/appadmin) passes; editor/viewer/global get 403;
		// outsider gets 404 (no access at all, "doesn't exist for you").
		{"super_update_app", "super", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 200, 0, 0},
		{"admin_global_update_app", "admin", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 0, 403, 403},
		{"auditor_global_update_app", "auditor", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 0, 403, 403},
		{"loner_update_app", "loner", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 200, 0, 0},
		{"appadmin_update_app", "appadmin", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 200, 0, 0},
		{"appeditor_update_app", "appeditor", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 0, 403, 403},
		{"appviewer_update_app", "appviewer", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 0, 403, 403},
		{"outsider_update_app", "outsider", http.MethodPut, "/dashboard/api/apps/%s", `{"name":"test-app","auth_email_enabled":true}`, 0, 404, 404},

		// --- DeleteApp (DELETE /apps/{id}) — CanManage required ---
		// Note: super/loner/appadmin will actually delete the app, which means
		// later sub-tests in the same matrix would fail. We test delete LAST
		// (or use a fresh app per actor). For simplicity, we only test the
		// "should be blocked" cases here and verify the "should succeed" case
		// with a dedicated app.
		{"admin_global_delete_app", "admin", http.MethodDelete, "/dashboard/api/apps/%s", "", 0, 403, 403},
		{"auditor_global_delete_app", "auditor", http.MethodDelete, "/dashboard/api/apps/%s", "", 0, 403, 403},
		{"appeditor_delete_app", "appeditor", http.MethodDelete, "/dashboard/api/apps/%s", "", 0, 403, 403},
		{"appviewer_delete_app", "appviewer", http.MethodDelete, "/dashboard/api/apps/%s", "", 0, 403, 403},
		{"outsider_delete_app", "outsider", http.MethodDelete, "/dashboard/api/apps/%s", "", 0, 404, 404},

		// --- CreateAppTable (POST /apps/{id}/tables) — CanWrite required ---
		// Admin/editor/loner/appadmin/super pass auth; viewer/global/outsider
		// are blocked. Status is provisioner-dependent for pass cases.
		// Table names must be unique per success case: the cases run
		// sequentially against the same app, so a second CREATE with an
		// already-taken name is rejected by validateTableInput with 400
		// (a name collision, not an auth failure).
		{"super_create_table", "super", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_super","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"admin_global_create_table", "admin", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_admin","columns":[{"name":"x","type":"text"}]}`, 0, 403, 403},
		{"auditor_global_create_table", "auditor", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_auditor","columns":[{"name":"x","type":"text"}]}`, 0, 403, 403},
		{"loner_create_table", "loner", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_loner","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"appadmin_create_table", "appadmin", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_appadmin","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"appeditor_create_table", "appeditor", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_appeditor","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"appviewer_create_table", "appviewer", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_appviewer","columns":[{"name":"x","type":"text"}]}`, 0, 403, 403},
		{"outsider_create_table", "outsider", http.MethodPost, "/dashboard/api/apps/%s/tables", `{"name":"t_outsider","columns":[{"name":"x","type":"text"}]}`, 0, 404, 404},

		// --- UpdateAppTable (PUT /apps/{id}/tables/{tableId}) — CanWrite required ---
		{"super_update_table", "super", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"admin_global_update_table", "admin", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 403, 403},
		{"auditor_global_update_table", "auditor", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 403, 403},
		{"loner_update_table", "loner", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"appadmin_update_table", "appadmin", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"appeditor_update_table", "appeditor", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 200, 299},
		{"appviewer_update_table", "appviewer", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 403, 403},
		{"outsider_update_table", "outsider", http.MethodPut, "/dashboard/api/apps/%s/tables/%s", `{"rls":"","columns":[{"name":"x","type":"text"}]}`, 0, 404, 404},

		// --- DeleteAppTable (DELETE /apps/{id}/tables/{tableId}) — CanWrite required ---
		{"admin_global_delete_table", "admin", http.MethodDelete, "/dashboard/api/apps/%s/tables/%s", "", 0, 403, 403},
		{"auditor_global_delete_table", "auditor", http.MethodDelete, "/dashboard/api/apps/%s/tables/%s", "", 0, 403, 403},
		// Table deletion is a CanWrite operation (same as create/update), so
		// an app editor is allowed through — this case belongs to the
		// "should succeed" set, not the blocked set.
		{"appeditor_delete_table", "appeditor", http.MethodDelete, "/dashboard/api/apps/%s/tables/%s", "", 0, 200, 299},
		{"appviewer_delete_table", "appviewer", http.MethodDelete, "/dashboard/api/apps/%s/tables/%s", "", 0, 403, 403},
		{"outsider_delete_table", "outsider", http.MethodDelete, "/dashboard/api/apps/%s/tables/%s", "", 0, 404, 404},

		// --- GetAppSecret (GET /apps/{id}/secret) — CanManage required ---
		{"super_get_secret", "super", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 200, 0, 0},
		{"admin_global_get_secret", "admin", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 0, 403, 403},
		{"auditor_global_get_secret", "auditor", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 0, 403, 403},
		{"loner_get_secret", "loner", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 200, 0, 0},
		{"appadmin_get_secret", "appadmin", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 200, 0, 0},
		{"appeditor_get_secret", "appeditor", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 0, 403, 403},
		{"appviewer_get_secret", "appviewer", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 0, 403, 403},
		{"outsider_get_secret", "outsider", http.MethodGet, "/dashboard/api/apps/%s/secret", "", 0, 404, 404},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := call(t, c)
			checkStatus(t, got, c)
		})
	}
}

// withCtx returns a new context with the chi RouteContext attached, so the
// handler can read URL params via chi.URLParam.
func withCtx(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}

// callUpdateAppTable issues a PUT /apps/{appID}/tables/{tableID} request
// directly against h.UpdateAppTable, mirroring TestAppsRBACMatrix's request
// construction for this same route.
func callUpdateAppTable(t *testing.T, h *Handler, actor *DashboardUser, appID, tableID string, reqBody TableRequestBody) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal TableRequestBody: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/dashboard/api/apps/%s/tables/%s", appID, tableID), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actor)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	rctx.URLParams.Add("tableId", tableID)
	req = req.WithContext(withCtx(req, rctx))

	w := httptest.NewRecorder()
	h.UpdateAppTable(w, req)
	return w
}

// TestUpdateAppTable_RejectsReferencesChangeOnExistingColumn covers T8 /
// spec CFK-19: a full-replace request that changes References on a column
// that already existed before the request is rejected with 400, and
// nothing is persisted.
func TestUpdateAppTable_RejectsReferencesChangeOnExistingColumn(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "put-fk-reject")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "categories",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text", Unique: true}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser categories: %v", err)
	}
	items, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "category_id", Type: "uuid"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, items.ID, TableRequestBody{
		RLS: items.RLS,
		Columns: []config.ColumnConfig{{
			Name: "category_id", Type: "uuid",
			References: &config.ReferenceConfig{Table: "categories", Column: "id"},
		}},
		Indexes: items.Indexes,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a References change on a pre-existing column, got %d (body: %s)", w.Code, w.Body.String())
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, "items")
	if refreshedTable == nil {
		t.Fatal("items table disappeared")
	}
	for _, c := range refreshedTable.Columns {
		if c.Name == "category_id" && c.References != nil {
			t.Fatalf("expected stored schema untouched (no References), got %+v", c.References)
		}
	}
	if hasFKConstraintOnColumn(t, pool, schemaNameForDB(app.Name), "items", "category_id") {
		t.Fatal("expected no FK constraint in Postgres — DDL must not have run for a rejected request")
	}
}

// TestUpdateAppTable_AllowsReferencesOnBrandNewColumn covers T8 / spec
// CFK-21: setting References on a column that is brand-new in this same
// request still succeeds exactly as before.
func TestUpdateAppTable_AllowsReferencesOnBrandNewColumn(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "put-fk-newcol")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "categories",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text", Unique: true}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser categories: %v", err)
	}
	items, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, items.ID, TableRequestBody{
		RLS: items.RLS,
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text"},
			{Name: "category_id", Type: "uuid", References: &config.ReferenceConfig{Table: "categories", Column: "id"}},
		},
		Indexes: items.Indexes,
	})
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("expected 2xx for References on a brand-new column, got %d (body: %s)", w.Code, w.Body.String())
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, "items")
	if refreshedTable == nil {
		t.Fatal("items table disappeared")
	}
	found := false
	for _, c := range refreshedTable.Columns {
		if c.Name == "category_id" {
			found = true
			if c.References == nil || c.References.Table != "categories" {
				t.Fatalf("expected persisted reference to categories, got %+v", c.References)
			}
		}
	}
	if !found {
		t.Fatalf("category_id column not found in %+v", refreshedTable.Columns)
	}
}

// TestUpdateAppTable_AllowsNonReferenceChangeWithReferencesUnchanged covers
// T8 / spec CFK-19 (negative control): changing a non-References field on
// an existing column, with References on shared columns left
// byte-identical, still succeeds exactly as before.
func TestUpdateAppTable_AllowsNonReferenceChangeWithReferencesUnchanged(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "put-fk-unchanged")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "categories",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text", Unique: true}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser categories: %v", err)
	}
	items, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "items",
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text"},
			{Name: "category_id", Type: "uuid", References: &config.ReferenceConfig{Table: "categories", Column: "id"}},
		},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	// Change title's Unique flag (non-References field) while leaving
	// category_id's References byte-identical.
	w := callUpdateAppTable(t, h, actors["loner"], app.ID, items.ID, TableRequestBody{
		RLS: items.RLS,
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text", Unique: true},
			{Name: "category_id", Type: "uuid", References: &config.ReferenceConfig{Table: "categories", Column: "id"}},
		},
		Indexes: items.Indexes,
	})
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("expected 2xx for a non-References change with References unchanged, got %d (body: %s)", w.Code, w.Body.String())
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, "items")
	if refreshedTable == nil {
		t.Fatal("items table disappeared")
	}
	for _, c := range refreshedTable.Columns {
		if c.Name == "title" && !c.Unique {
			t.Fatalf("expected title.Unique to be persisted true, got %+v", c)
		}
		if c.Name == "category_id" && (c.References == nil || c.References.Table != "categories") {
			t.Fatalf("expected category_id's reference to remain intact, got %+v", c.References)
		}
	}
}

// --- enum column type end-to-end (CENUM-01, CENUM-02, CENUM-04, CENUM-05, CENUM-06) ---

// checkConstraintDefForColumn returns the CHECK constraint definition on a
// column, or "" when there is none. Read from the catalog rather than
// assuming a constraint name.
func checkConstraintDefForColumn(t *testing.T, pool *db.Pool, schema, tableName, columnName string) string {
	t.Helper()
	var def string
	err := pool.QueryRow(context.Background(),
		`SELECT pg_get_constraintdef(c.oid)
		 FROM pg_constraint c
		 JOIN pg_class r ON r.oid = c.conrelid
		 JOIN pg_namespace n ON n.oid = r.relnamespace
		 JOIN pg_attribute a ON a.attrelid = r.oid AND a.attname = $3
		 WHERE c.contype = 'c' AND n.nspname = $1 AND r.relname = $2 AND c.conkey = ARRAY[a.attnum]`,
		schema, tableName, columnName,
	).Scan(&def)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return ""
		}
		t.Fatalf("read check constraint on %s.%s.%s: %v", schema, tableName, columnName, err)
	}
	return def
}

// physicalColumnType returns a column's physical Postgres type.
func physicalColumnType(t *testing.T, pool *db.Pool, schema, tableName, columnName string) string {
	t.Helper()
	var udt string
	if err := pool.QueryRow(context.Background(),
		`SELECT udt_name FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
		schema, tableName, columnName,
	).Scan(&udt); err != nil {
		t.Fatalf("read column type on %s.%s.%s: %v", schema, tableName, columnName, err)
	}
	return udt
}

// CENUM-01: creating a table with an enum column provisions it as text with
// a CHECK constraint restricting it to exactly the declared values, and the
// database itself rejects an out-of-set write.
func TestCreateAppTableForUser_EnumColumnProvisionsCheckConstraint(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "enum-create-app")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "assets",
		Columns: []config.ColumnConfig{
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active", "Em andamento"}},
		},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	// The declared config round-trips.
	if got := table.Columns[0].AllowedValues; len(got) != 3 || got[0] != "pending" || got[2] != "Em andamento" {
		t.Errorf("persisted AllowedValues = %v, want [pending active \"Em andamento\"]", got)
	}

	schema := schemaNameForDB(app.Name)
	if got := physicalColumnType(t, pool, schema, "assets", "status"); got != "text" {
		t.Errorf("physical column type = %q, want %q", got, "text")
	}
	if def := checkConstraintDefForColumn(t, pool, schema, "assets", "status"); def == "" {
		t.Fatal("expected a CHECK constraint on assets.status")
	} else if !strings.Contains(def, "pending") || !strings.Contains(def, "Em andamento") {
		t.Errorf("constraint def = %q, want it to restrict to the declared values", def)
	}

	// The real enforcement: in-set write accepted, out-of-set rejected.
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('pending')"); err != nil {
		t.Errorf("expected an in-set write to succeed, got: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('qualquer coisa')"); err == nil {
		t.Error("expected the database to reject an out-of-set write")
	}
}

// CENUM-04: an enum column with an empty allowed-value list is rejected at
// request-validation time, before any table is created.
func TestCreateAppTableForUser_EnumWithoutAllowedValuesRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "enum-empty-app")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}

	_, err = h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "status", Type: "enum"}},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for an enum column without allowed_values, got %v (%T)", err, err)
	}

	// Nothing was provisioned: the table does not exist in Postgres.
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'assets')`,
		schemaNameForDB(app.Name),
	).Scan(&exists); err != nil {
		t.Fatalf("check table existence: %v", err)
	}
	if exists {
		t.Error("expected no physical table after a rejected enum column definition")
	}
}

// CENUM-05: a default that is not a member of allowed_values is rejected at
// request-validation time.
func TestCreateAppTableForUser_EnumDefaultOutsideAllowedValuesRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "enum-baddef-app")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}

	_, err = h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "assets",
		Columns: []config.ColumnConfig{{
			Name: "status", Type: "enum",
			AllowedValues: []string{"pending", "active"},
			Default:       "closed",
		}},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for a default outside allowed_values, got %v (%T)", err, err)
	}
}

// CENUM-02: adding a new enum column to an existing table attaches the same
// CHECK constraint the creation path emits.
func TestAddTableColumnForUser_EnumColumnProvisionsCheckConstraint(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "enum-addcol-app")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	updated, err := h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "status", Type: "enum", AllowedValues: []string{"pending", "active"},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("AddTableColumnForUser: %v", err)
	}
	if got := updated.Columns[1].AllowedValues; len(got) != 2 || got[0] != "pending" {
		t.Errorf("persisted AllowedValues = %v, want [pending active]", got)
	}

	schema := schemaNameForDB(app.Name)
	if def := checkConstraintDefForColumn(t, pool, schema, "assets", "status"); def == "" {
		t.Fatal("expected a CHECK constraint on the added assets.status column")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('active')"); err != nil {
		t.Errorf("expected an in-set write to succeed, got: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('qualquer coisa')"); err == nil {
		t.Error("expected the database to reject an out-of-set write on the added column")
	}
}

// CENUM-04: an invalid enum column is rejected on the add-column path too,
// not just at table creation.
func TestAddTableColumnForUser_EnumWithoutAllowedValuesRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "enum-addcol-bad-app")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	_, err = h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "status", Type: "enum", AllowedValues: []string{},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for an enum column without allowed_values, got %v (%T)", err, err)
	}

	// The column was not added physically.
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		  WHERE table_schema = $1 AND table_name = 'assets' AND column_name = 'status')`,
		schemaNameForDB(app.Name),
	).Scan(&exists); err != nil {
		t.Fatalf("check column existence: %v", err)
	}
	if exists {
		t.Error("expected no physical column after a rejected enum column definition")
	}
}

// --- PUT guard against an AllowedValues change on an existing enum column ---

// createAppWithEnumTable is shared setup for the PUT-guard tests: an app with
// an assets table whose status column is an enum over pending/active.
func createAppWithEnumTable(t *testing.T, ctx context.Context, h *Handler, actors map[string]*DashboardUser, appName string) (*AppRow, *AppTableRow) {
	t.Helper()
	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: appName}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "assets",
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "text"},
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active"}},
		},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	return app, table
}

// A full-replace PUT that changes an existing enum column's AllowedValues is
// rejected with 400 pointing at the dedicated endpoint, and nothing is
// persisted or migrated — the same silent-no-op class the References guard
// above closes (addMissingColumns never re-emits an existing column's DDL).
func TestUpdateAppTable_RejectsAllowedValuesChangeOnExistingColumn(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithEnumTable(t, ctx, h, actors, uniqueAppName(t, "put-enum-reject"))
	schema := schemaNameForDB(app.Name)
	constraintBefore := checkConstraintDefForColumn(t, pool, schema, "assets", "status")

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, table.ID, TableRequestBody{
		RLS: table.RLS,
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "text"},
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active", "closed"}},
		},
		Indexes: table.Indexes,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an AllowedValues change on a pre-existing column, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "enum-values endpoint") {
		t.Errorf("expected the error to point at the dedicated endpoint, got: %s", w.Body.String())
	}

	// Stored schema untouched.
	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, "assets")
	if refreshedTable == nil {
		t.Fatal("assets table disappeared")
	}
	for _, c := range refreshedTable.Columns {
		if c.Name == "status" && len(c.AllowedValues) != 2 {
			t.Errorf("expected stored AllowedValues untouched (2 values), got %v", c.AllowedValues)
		}
	}
	// Physical constraint untouched: no DDL ran for a rejected request.
	if after := checkConstraintDefForColumn(t, pool, schema, "assets", "status"); after != constraintBefore {
		t.Errorf("constraint changed on a rejected request:\n before = %q\n after  = %q", constraintBefore, after)
	}
}

// A full-replace PUT that changes an existing enum column's Type away from
// "enum" (or a text column's Type to "enum") is rejected with a distinct
// message naming the real constraint — there is no endpoint for this at all,
// unlike widen/narrow, so pointing the caller at the enum-values endpoint
// (which itself requires the column to already be enum) would be a dead end.
func TestUpdateAppTable_RejectsTypeChangeIntoOrOutOfEnum(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithEnumTable(t, ctx, h, actors, uniqueAppName(t, "put-enum-typechange"))
	schema := schemaNameForDB(app.Name)
	constraintBefore := checkConstraintDefForColumn(t, pool, schema, "assets", "status")

	// enum -> text
	w := callUpdateAppTable(t, h, actors["loner"], app.ID, table.ID, TableRequestBody{
		RLS: table.RLS,
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "text"},
			{Name: "status", Type: "text"},
		},
		Indexes: table.Indexes,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for enum -> text on a pre-existing column, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "to or from the enum type is not supported") {
		t.Errorf("expected the error to name the real constraint (no such endpoint), got: %s", w.Body.String())
	}

	// text -> enum, on the pre-existing "name" column.
	w = callUpdateAppTable(t, h, actors["loner"], app.ID, table.ID, TableRequestBody{
		RLS: table.RLS,
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "enum", AllowedValues: []string{"a", "b"}},
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active"}},
		},
		Indexes: table.Indexes,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for text -> enum on a pre-existing column, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "to or from the enum type is not supported") {
		t.Errorf("expected the error to name the real constraint (no such endpoint), got: %s", w.Body.String())
	}

	// Physical constraint untouched: no DDL ran for either rejected request.
	if after := checkConstraintDefForColumn(t, pool, schema, "assets", "status"); after != constraintBefore {
		t.Errorf("constraint changed on a rejected request:\n before = %q\n after  = %q", constraintBefore, after)
	}
}

// Negative control: a PUT that leaves an existing enum column's
// AllowedValues unchanged (order may differ — the comparison is set-based)
// still succeeds, so the guard produces no false positives.
func TestUpdateAppTable_AllowsUnchangedAllowedValuesOnExistingColumn(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithEnumTable(t, ctx, h, actors, uniqueAppName(t, "put-enum-same"))

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, table.ID, TableRequestBody{
		RLS: table.RLS,
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "text"},
			// Same set, reversed order.
			{Name: "status", Type: "enum", AllowedValues: []string{"active", "pending"}},
			// A genuine, permitted change in the same request.
			{Name: "note", Type: "text"},
		},
		Indexes: table.Indexes,
	})
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("expected 2xx when AllowedValues is unchanged, got %d (body: %s)", w.Code, w.Body.String())
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, "assets")
	if refreshedTable == nil {
		t.Fatal("assets table disappeared")
	}
	if len(refreshedTable.Columns) != 3 {
		t.Errorf("expected the permitted change to be applied (3 columns), got %d: %+v", len(refreshedTable.Columns), refreshedTable.Columns)
	}
}

// A brand-new enum column in the same PUT request has no "before" state, so
// the guard must not fire — it is provisioned inline exactly as before.
func TestUpdateAppTable_AllowsAllowedValuesOnBrandNewColumn(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "put-enum-newcol")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, table.ID, TableRequestBody{
		RLS: table.RLS,
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "text"},
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active"}},
		},
		Indexes: table.Indexes,
	})
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("expected 2xx for an enum column that is brand-new in this request, got %d (body: %s)", w.Code, w.Body.String())
	}

	if def := checkConstraintDefForColumn(t, pool, schemaNameForDB(app.Name), "assets", "status"); def == "" {
		t.Error("expected a CHECK constraint on the newly added assets.status column")
	}
}

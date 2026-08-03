package dashboard

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// frontendHandlerTestPool provisions the full zeep_system schema, seeds 8
// actors (superadmin, admin global, auditor global, loner/owner, app admin/
// editor/viewer, outsider), 1 github_templates row, 1 frontend_apps row owned
// by loner, per-app roles in app_members, and a frontend_app_sync_credentials
// row. Returns the pool, a FrontendAppsHandler wired to it, the actors map,
// the test frontendAppID, and the sync credential ID.
//
// Skips if TEST_DATABASE_URL is not set.
func frontendHandlerTestPool(t *testing.T) (*Handler, *FrontendAppsHandler, map[string]*DashboardUser, string, string) {
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

	// Seed 8 actors with unique emails (UNIQUE constraint).
	actors := map[string]*DashboardUser{}
	actorDefs := []struct{ key, email, role string }{
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

	// Seed a github_templates row (frontend_apps.template_id is NOT NULL FK).
	var templateID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates
		 (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ('tpl1', '', 'owner', 'repo', 'vite', 'test@example.com') RETURNING id`,
	).Scan(&templateID); err != nil {
		pool.Close()
		t.Fatalf("seed github_templates: %v", err)
	}

	// Create the test frontend app owned by loner. The T-02 migration inserts
	// loner as admin in app_members based on apps.created_by.
	var faID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps
		 (name, slug, template_id, created_by, owner_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"test-frontend", "test-frontend", templateID,
		actors["loner"].Email, actors["loner"].ID,
	).Scan(&faID); err != nil {
		pool.Close()
		t.Fatalf("seed frontend_apps: %v", err)
	}

	// Belt-and-suspenders: ensure loner has an admin row in app_members.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
		faID, actors["loner"].ID,
	); err != nil {
		pool.Close()
		t.Fatalf("seed loner as admin: %v", err)
	}

	// Seed per-app roles for the explicit membership tests.
	for _, pr := range []struct{ userKey, role string }{
		{"appadmin", "admin"},
		{"appeditor", "editor"},
		{"appviewer", "viewer"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, $3)`,
			faID, actors[pr.userKey].ID, pr.role,
		); err != nil {
			pool.Close()
			t.Fatalf("seed %s as %s: %v", pr.userKey, pr.role, err)
		}
	}

	// Seed a sync credential row (needed for SyncStatus/RevealKey/SyncRetry).
	var scID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_app_sync_credentials (frontend_app_id) VALUES ($1) RETURNING id`,
		faID,
	).Scan(&scID); err != nil {
		pool.Close()
		t.Fatalf("seed sync credential: %v", err)
	}

	// Use a real (default) http.Client so the GitHub call paths don't nil-deref
	// on handlers that touch the API. The calls will fail (no real GitHub
	// config), but the auth check happens first, so "should be blocked" cases
	// work fine. "Should pass" cases will get a 400/500 from the downstream
	// GitHub step — we assert status < 400 for those.
	_ = scID

	_ = NewHandler // keep the main Handler import alive for other tests
	fh := &FrontendAppsHandler{pool: pool, httpClient: http.DefaultClient}

	return nil, fh, actors, faID, scID
}

// faRBACCase describes one cell of the RBAC matrix for frontend_apps handlers.
// For "should pass" cases that go through GitHub/deploy, we assert the status
// is NOT 4xx (auth check passed, downstream may fail).
type faRBACCase struct {
	name             string
	actor            string
	method           string
	path             string
	body             string
	handlerFn        func(http.ResponseWriter, *http.Request)
	wantExact        int
	wantMin, wantMax int
}

func TestFrontendAppsRBACMatrix(t *testing.T) {
	_, fh, actors, faID, _ := frontendHandlerTestPool(t)

	// Use a defer to close the pool from one of the actors' fields is not
	// available; the pool is shared via fh.pool. We can't easily close it here
	// without changing the helper signature, so leave it open (test process
	// will close it).
	_ = actors

	// Helper: build a request with the actor's user in context and the given
	// path/body, then dispatch to the named handler method.
	call := func(t *testing.T, c faRBACCase) int {
		t.Helper()
		actor, ok := actors[c.actor]
		if !ok {
			t.Fatalf("unknown actor %q", c.actor)
		}
		path := c.path
		// Substitute {id} placeholder with faID.
		path = strings.ReplaceAll(path, "{id}", faID)

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

		// Attach chi URL param so chi.URLParam(r, "id") works.
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", faID)
		req = req.WithContext(withCtxValue(req, rctx))
		w := httptest.NewRecorder()

		c.handlerFn(w, req)
		return w.Code
	}

	checkStatus := func(t *testing.T, got int, c faRBACCase) {
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

	cases := []faRBACCase{
		// --- List (GET /frontend-apps) — everyone gets 200, filtered by membership ---
		{"super_list", "super", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"admin_global_list", "admin", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"auditor_global_list", "auditor", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"loner_list", "loner", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"appadmin_list", "appadmin", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"appeditor_list", "appeditor", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"appviewer_list", "appviewer", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},
		{"outsider_list", "outsider", http.MethodGet, "/dashboard/api/frontend-apps", "", fh.List, 200, 0, 0},

		// --- Get (GET /frontend-apps/{id}) — members/global get 200, non-member 404 ---
		{"super_get", "super", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"admin_global_get", "admin", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"auditor_global_get", "auditor", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"loner_get", "loner", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"appadmin_get", "appadmin", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"appeditor_get", "appeditor", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"appviewer_get", "appviewer", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 200, 0, 0},
		{"outsider_get", "outsider", http.MethodGet, "/dashboard/api/frontend-apps/{id}", "", fh.Get, 0, 404, 404},

		// --- SyncStatus (GET /frontend-apps/{id}/sync) — read access ---
		// For "should pass" cases, the handler returns 200 (no GitHub needed).
		{"super_sync_status", "super", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"admin_global_sync_status", "admin", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"auditor_global_sync_status", "auditor", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"loner_sync_status", "loner", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"appadmin_sync_status", "appadmin", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"appeditor_sync_status", "appeditor", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"appviewer_sync_status", "appviewer", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 200, 0, 0},
		{"outsider_sync_status", "outsider", http.MethodGet, "/dashboard/api/frontend-apps/{id}/sync", "", fh.SyncStatus, 0, 404, 404},

		// --- Retry (POST /frontend-apps/{id}/retry) — CanManage required ---
		// For "should pass" cases, the handler will fail at the GitHub config
		// step (return 400 "github not connected"). We assert status < 400
		// (no 403/404 from auth).
		{"super_retry", "super", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 200, 399},
		{"admin_global_retry", "admin", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 403, 403},
		{"auditor_global_retry", "auditor", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 403, 403},
		{"loner_retry", "loner", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 200, 399},
		{"appadmin_retry", "appadmin", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 200, 399},
		{"appeditor_retry", "appeditor", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 403, 403},
		{"appviewer_retry", "appviewer", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 403, 403},
		{"outsider_retry", "outsider", http.MethodPost, "/dashboard/api/frontend-apps/{id}/retry", "", fh.Retry, 0, 404, 404},

		// --- Delete (DELETE /frontend-apps/{id}) — CanManage required ---
		// For "should pass" cases, the handler will fail at the GitHub archival
		// step. We assert status < 400 (no 403/404 from auth).
		{"super_delete", "super", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 200, 399},
		{"admin_global_delete", "admin", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 403, 403},
		{"auditor_global_delete", "auditor", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 403, 403},
		{"loner_delete", "loner", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 200, 399},
		{"appadmin_delete", "appadmin", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 200, 399},
		{"appeditor_delete", "appeditor", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 403, 403},
		{"appviewer_delete", "appviewer", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 403, 403},
		{"outsider_delete", "outsider", http.MethodDelete, "/dashboard/api/frontend-apps/{id}", "", fh.Delete, 0, 404, 404},

		// --- DeployRetry (POST /frontend-apps/{id}/deploy/retry) — CanManage ---
		{"admin_global_deploy_retry", "admin", http.MethodPost, "/dashboard/api/frontend-apps/{id}/deploy/retry", "", fh.DeployRetry, 0, 403, 403},
		{"appeditor_deploy_retry", "appeditor", http.MethodPost, "/dashboard/api/frontend-apps/{id}/deploy/retry", "", fh.DeployRetry, 0, 403, 403},
		{"appviewer_deploy_retry", "appviewer", http.MethodPost, "/dashboard/api/frontend-apps/{id}/deploy/retry", "", fh.DeployRetry, 0, 403, 403},
		{"outsider_deploy_retry", "outsider", http.MethodPost, "/dashboard/api/frontend-apps/{id}/deploy/retry", "", fh.DeployRetry, 0, 404, 404},

		// --- SetCustomDomain (POST /frontend-apps/{id}/domain) — CanManage ---
		{"admin_global_set_domain", "admin", http.MethodPost, "/dashboard/api/frontend-apps/{id}/domain", `{"subdomain":"app"}`, fh.SetCustomDomain, 0, 403, 403},
		{"appeditor_set_domain", "appeditor", http.MethodPost, "/dashboard/api/frontend-apps/{id}/domain", `{"subdomain":"app"}`, fh.SetCustomDomain, 0, 403, 403},
		{"appviewer_set_domain", "appviewer", http.MethodPost, "/dashboard/api/frontend-apps/{id}/domain", `{"subdomain":"app"}`, fh.SetCustomDomain, 0, 403, 403},
		{"outsider_set_domain", "outsider", http.MethodPost, "/dashboard/api/frontend-apps/{id}/domain", `{"subdomain":"app"}`, fh.SetCustomDomain, 0, 404, 404},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := call(t, c)
			checkStatus(t, got, c)
		})
	}
}

// withCtxValue returns a new context with the chi RouteContext attached, so
// the handler can read URL params via chi.URLParam.
func withCtxValue(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}

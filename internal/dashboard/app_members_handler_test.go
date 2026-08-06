package dashboard

// app_members_handler_test.go — T-06 of rbac-per-app. Exercises the
// 4-endpoint member management API (GET/POST/PATCH/DELETE on
// /dashboard/api/{apps|frontend-apps}/{id}/members) across the full
// RBAC matrix (8 actors × 4 endpoints × 2 axes = 64 cells) plus the 7
// "Independent Test" scenarios from the spec (line 46) and edge cases
// (UNIQUE violation, last-admin rejection, PATCH/DELETE on non-member,
// invalid input).
//
// Skips if TEST_DATABASE_URL is not set.

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
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// appMembersHandlerTestPool provisions the full zeep_system schema,
// seeds 8 actors (superadmin, admin global, auditor global, loner/owner,
// app admin/editor/viewer, outsider) and 2 test apps (1 backend, 1
// frontend) with per-app roles. Returns the pool, a Handler wired to it,
// the actors map (keyed by short name), the backend appID, and the
// frontend appID.
//
// Skips if TEST_DATABASE_URL is not set.
func appMembersHandlerTestPool(t *testing.T) (*db.Pool, *Handler, map[string]*DashboardUser, string, string) {
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

	// Backend app owned by loner (loner becomes admin via migration).
	var backendAppID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"backend-test", actors["loner"].ID,
	).Scan(&backendAppID); err != nil {
		pool.Close()
		t.Fatalf("create backend app: %v", err)
	}
	// Belt-and-suspenders: ensure loner has an admin row.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
		backendAppID, actors["loner"].ID,
	); err != nil {
		pool.Close()
		t.Fatalf("seed loner as admin on backend: %v", err)
	}
	for _, pr := range []struct{ userKey, role string }{
		{"appadmin", "admin"},
		{"appeditor", "editor"},
		{"appviewer", "viewer"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, $3)`,
			backendAppID, actors[pr.userKey].ID, pr.role,
		); err != nil {
			pool.Close()
			t.Fatalf("seed %s as %s on backend: %v", pr.userKey, pr.role, err)
		}
	}

	// Frontend app: seed a github_templates row first (FK is NOT NULL).
	var templateID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates (name, github_owner, github_repo, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		"tpl-members", "owner", "repo", actors["super"].Email,
	).Scan(&templateID); err != nil {
		pool.Close()
		t.Fatalf("create github template: %v", err)
	}
	var frontendAppID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (slug, name, template_id, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		"frontend-test", "Frontend Test", templateID, actors["loner"].Email,
	).Scan(&frontendAppID); err != nil {
		pool.Close()
		t.Fatalf("create frontend app: %v", err)
	}
	// Belt-and-suspenders: ensure loner has an admin row.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
		frontendAppID, actors["loner"].ID,
	); err != nil {
		pool.Close()
		t.Fatalf("seed loner as admin on frontend: %v", err)
	}
	for _, pr := range []struct{ userKey, role string }{
		{"appadmin", "admin"},
		{"appeditor", "editor"},
		{"appviewer", "viewer"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, $3)`,
			frontendAppID, actors[pr.userKey].ID, pr.role,
		); err != nil {
			pool.Close()
			t.Fatalf("seed %s as %s on frontend: %v", pr.userKey, pr.role, err)
		}
	}

	h := NewHandler(pool, registry.New(), zap.NewNop())

	return pool, h, actors, backendAppID, frontendAppID
}

// appMemberCase describes one cell of the RBAC matrix for the member
// management API: which actor hits which endpoint with which
// method/path/body, and what HTTP status is expected.
type appMemberCase struct {
	name      string
	actor     string // key in actors map
	method    string
	pathFmt   string // format with appID and userID as %s placeholders
	body      string
	userParam string // value substituted into userID %s (default: actor's own id)
	wantExact int    // 0 = don't check exact status
	wantMin   int
	wantMax   int
}

// callAppMember dispatches one appMemberCase. It sets up the chi URL
// params, attaches the actor, and routes to the correct handler method
// (the handlers are called directly to bypass the router — same
// pattern as apps_handler_test.go).
func callAppMember(t *testing.T, h *Handler, actors map[string]*DashboardUser, backendAppID, frontendAppID string, c appMemberCase) int {
	t.Helper()
	actor, ok := actors[c.actor]
	if !ok {
		t.Fatalf("unknown actor %q", c.actor)
	}
	// Pick the right app id based on the path prefix.
	var appID string
	switch {
	case strings.Contains(c.pathFmt, "/apps/"):
		appID = backendAppID
	case strings.Contains(c.pathFmt, "/frontend-apps/"):
		appID = frontendAppID
	default:
		t.Fatalf("cannot determine axis from pathFmt %q", c.pathFmt)
	}
	// pathFmt has either 1 %s (appID only — list/add) or 2 (appID + userID —
	// update/remove). hasUserID drives both the URL substitution and the
	// chi route param below; a literal "{userId}" substring never appears
	// in pathFmt, so checking for %s count is the only reliable signal.
	hasUserID := strings.Count(c.pathFmt, "%s") == 2

	// userID is either the actor's own id (default for "actor tries to
	// do something to themselves") or whatever the case provides.
	var userIDArg string
	if c.userParam != "" {
		userIDArg = c.userParam
	} else if hasUserID {
		userIDArg = actor.ID
	}
	var path string
	if hasUserID {
		path = fmt.Sprintf(c.pathFmt, appID, userIDArg)
	} else {
		path = fmt.Sprintf(c.pathFmt, appID)
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
	rctx := chi.NewRouteContext()
	switch {
	case hasUserID:
		rctx.URLParams.Add("id", appID)
		rctx.URLParams.Add("userId", userIDArg)
	default:
		rctx.URLParams.Add("id", appID)
	}
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()

	// Route to the right handler method.
	switch {
	case c.method == http.MethodGet && strings.HasSuffix(path, "/members"):
		h.ListAppMembers(w, req)
	case c.method == http.MethodPost && strings.HasSuffix(path, "/members"):
		h.AddAppMember(w, req)
	case c.method == http.MethodPatch:
		h.UpdateAppMember(w, req)
	case c.method == http.MethodDelete:
		h.RemoveAppMember(w, req)
	default:
		t.Fatalf("unhandled case: %s %s", c.method, path)
	}
	return w.Code
}

// checkStatus asserts the response code matches the case's expectation.
func checkAppMemberStatus(t *testing.T, got int, c appMemberCase) {
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

// TestAppMembersRBACMatrix exercises the full per-app RBAC matrix for
// the 4 member-management endpoints, on both axes (backend and
// frontend apps). The matrix covers the spec's AC-1 (admin can add),
// AC-3 (admin can update), AC-4 (admin can delete), and AC-6
// (editor/viewer/non-member get 403 on any endpoint).
func TestAppMembersRBACMatrix(t *testing.T) {
	pool, h, actors, backendAppID, frontendAppID := appMembersHandlerTestPool(t)
	defer pool.Close()

	// Each case uses a unique target user (via timestamp) so the UNIQUE
	// constraint doesn't trip between sub-tests. The "self" target is
	// the actor's own id — used for "actor tries to demote/remove self".
	ts := time.Now().UnixNano()
	uniqueUser := func(suffix string) string {
		u, err := CreateUser(context.Background(), pool,
			fmt.Sprintf("unique-%s-%d@example.com", suffix, ts), suffix, "hash", "member")
		if err != nil {
			t.Fatalf("create unique user %s: %v", suffix, err)
		}
		return u.ID
	}

	// seedMember creates a fresh user and pre-seeds it as a 'viewer'
	// member of the given app. Every Remove case that expects 204 needs
	// its OWN pre-seeded target: the cases run sequentially against a
	// shared app, so a target consumed by the first successful DELETE is
	// gone (404) by the time the next success case reaches it.
	seedMember := func(suffix, column, appID string) string {
		id := uniqueUser(suffix)
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf(`INSERT INTO zeep_system.app_members (%s, user_id, role) VALUES ($1, $2, 'viewer')`, column),
			appID, id,
		); err != nil {
			t.Fatalf("seed member %s: %v", suffix, err)
		}
		return id
	}
	beMember := func(suffix string) string { return seedMember(suffix, "backend_app_id", backendAppID) }
	feMember := func(suffix string) string { return seedMember(suffix, "frontend_app_id", frontendAppID) }

	// Backend axis: List/Add/Update/Remove.
	beTarget := uniqueUser("be") // not a member — used for Add success cases
	beOther := uniqueUser("be2") // not a member — used for Update/Delete success cases
	beThird := uniqueUser("be3") // not a member — a distinct target for the second Add success case (appadmin), since beOther is already consumed by the loner Add success case above it
	// One pre-seeded member per Remove-expects-204 case.
	beRemove1 := beMember("berm1") // consumed by be_super_remove
	beRemove2 := beMember("berm2") // consumed by be_loner_remove
	beRemove3 := beMember("berm3") // consumed by be_appadmin_remove

	// Frontend axis: List/Add/Update/Remove (mirror of backend).
	feTarget := uniqueUser("fe")
	feOther := uniqueUser("fe2")
	feThird := uniqueUser("fe3")   // distinct target for the second Add success case (appadmin) — feOther is consumed by fe_loner_add
	feRemove1 := feMember("ferm1") // consumed by fe_super_remove
	feRemove2 := feMember("ferm2") // consumed by fe_appadmin_remove

	cases := []appMemberCase{
		// ===== Backend app — List (GET) — CanManage required =====
		{"be_super_list", "super", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 200, 0, 0},
		{"be_admin_global_list", "admin", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 0, 403, 403},
		{"be_auditor_global_list", "auditor", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 0, 403, 403},
		{"be_loner_list", "loner", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 200, 0, 0},
		{"be_appadmin_list", "appadmin", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 200, 0, 0},
		{"be_appeditor_list", "appeditor", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 0, 403, 403},
		{"be_appviewer_list", "appviewer", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 0, 403, 403},
		{"be_outsider_list", "outsider", http.MethodGet, "/dashboard/api/apps/%s/members", "", "", 0, 403, 403},

		// ===== Backend app — Add (POST) — CanManage required =====
		// Auth-passing actors get 201 (adding a fresh non-member).
		// Auth-blocking actors get 403.
		{"be_super_add", "super", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beTarget + `","role":"editor"}`, "", 201, 0, 0},
		{"be_admin_global_add", "admin", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beOther + `","role":"editor"}`, "", 0, 403, 403},
		{"be_auditor_global_add", "auditor", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beOther + `","role":"editor"}`, "", 0, 403, 403},
		{"be_loner_add", "loner", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beOther + `","role":"editor"}`, "", 201, 0, 0},
		{"be_appadmin_add", "appadmin", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beThird + `","role":"editor"}`, "", 201, 0, 0},
		{"be_appeditor_add", "appeditor", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beOther + `","role":"editor"}`, "", 0, 403, 403},
		{"be_appviewer_add", "appviewer", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beOther + `","role":"editor"}`, "", 0, 403, 403},
		{"be_outsider_add", "outsider", http.MethodPost, "/dashboard/api/apps/%s/members",
			`{"user_id":"` + beOther + `","role":"editor"}`, "", 0, 403, 403},

		// ===== Backend app — Update (PATCH) — CanManage required =====
		// Actor updates beTarget (added by super_admin above) to viewer.
		{"be_super_update", "super", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 200, 0, 0},
		{"be_admin_global_update", "admin", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 0, 403, 403},
		{"be_auditor_global_update", "auditor", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 0, 403, 403},
		{"be_loner_update", "loner", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 200, 0, 0},
		{"be_appadmin_update", "appadmin", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 200, 0, 0},
		{"be_appeditor_update", "appeditor", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 0, 403, 403},
		{"be_appviewer_update", "appviewer", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 0, 403, 403},
		{"be_outsider_update", "outsider", http.MethodPatch, "/dashboard/api/apps/%s/members/%s",
			`{"role":"viewer"}`, beTarget, 0, 403, 403},

		// ===== Backend app — Remove (DELETE) — CanManage required =====
		// Each success case removes its own pre-seeded member; the 403
		// cases share beTarget since they never reach the store.
		{"be_super_remove", "super", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beRemove1, 204, 0, 0},
		{"be_admin_global_remove", "admin", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beTarget, 0, 403, 403},
		{"be_auditor_global_remove", "auditor", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beTarget, 0, 403, 403},
		{"be_loner_remove", "loner", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beRemove2, 204, 0, 0},
		{"be_appadmin_remove", "appadmin", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beRemove3, 204, 0, 0},
		{"be_appeditor_remove", "appeditor", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beTarget, 0, 403, 403},
		{"be_appviewer_remove", "appviewer", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beTarget, 0, 403, 403},
		{"be_outsider_remove", "outsider", http.MethodDelete, "/dashboard/api/apps/%s/members/%s", "", beTarget, 0, 403, 403},

		// ===== Frontend app — same matrix, abbreviated =====
		// We test the same 4 endpoints on the frontend axis to confirm
		// the axis switch works. Only the "should pass" and "should be
		// blocked" cases for superadmin/appeditor/outsider — enough to
		// confirm the router and handler axis resolution.
		{"fe_super_list", "super", http.MethodGet, "/dashboard/api/frontend-apps/%s/members", "", "", 200, 0, 0},
		{"fe_appadmin_list", "appadmin", http.MethodGet, "/dashboard/api/frontend-apps/%s/members", "", "", 200, 0, 0},
		{"fe_appeditor_list", "appeditor", http.MethodGet, "/dashboard/api/frontend-apps/%s/members", "", "", 0, 403, 403},
		{"fe_outsider_list", "outsider", http.MethodGet, "/dashboard/api/frontend-apps/%s/members", "", "", 0, 403, 403},

		{"fe_super_add", "super", http.MethodPost, "/dashboard/api/frontend-apps/%s/members",
			`{"user_id":"` + feTarget + `","role":"editor"}`, "", 201, 0, 0},
		{"fe_loner_add", "loner", http.MethodPost, "/dashboard/api/frontend-apps/%s/members",
			`{"user_id":"` + feOther + `","role":"editor"}`, "", 201, 0, 0},
		{"fe_appadmin_add", "appadmin", http.MethodPost, "/dashboard/api/frontend-apps/%s/members",
			`{"user_id":"` + feThird + `","role":"editor"}`, "", 201, 0, 0},
		{"fe_appeditor_add", "appeditor", http.MethodPost, "/dashboard/api/frontend-apps/%s/members",
			`{"user_id":"` + feOther + `","role":"editor"}`, "", 0, 403, 403},
		{"fe_outsider_add", "outsider", http.MethodPost, "/dashboard/api/frontend-apps/%s/members",
			`{"user_id":"` + feOther + `","role":"editor"}`, "", 0, 403, 403},

		{"fe_super_update", "super", http.MethodPatch, "/dashboard/api/frontend-apps/%s/members/%s",
			`{"role":"viewer"}`, feTarget, 200, 0, 0},
		{"fe_appadmin_update", "appadmin", http.MethodPatch, "/dashboard/api/frontend-apps/%s/members/%s",
			`{"role":"viewer"}`, feTarget, 200, 0, 0},
		{"fe_appeditor_update", "appeditor", http.MethodPatch, "/dashboard/api/frontend-apps/%s/members/%s",
			`{"role":"viewer"}`, feTarget, 0, 403, 403},
		{"fe_outsider_update", "outsider", http.MethodPatch, "/dashboard/api/frontend-apps/%s/members/%s",
			`{"role":"viewer"}`, feTarget, 0, 403, 403},

		{"fe_super_remove", "super", http.MethodDelete, "/dashboard/api/frontend-apps/%s/members/%s", "", feRemove1, 204, 0, 0},
		{"fe_appadmin_remove", "appadmin", http.MethodDelete, "/dashboard/api/frontend-apps/%s/members/%s", "", feRemove2, 204, 0, 0},
		{"fe_appeditor_remove", "appeditor", http.MethodDelete, "/dashboard/api/frontend-apps/%s/members/%s", "", feTarget, 0, 403, 403},
		{"fe_outsider_remove", "outsider", http.MethodDelete, "/dashboard/api/frontend-apps/%s/members/%s", "", feTarget, 0, 403, 403},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := callAppMember(t, h, actors, backendAppID, frontendAppID, c)
			checkAppMemberStatus(t, got, c)
		})
	}
}

// TestAppMembersIndependentTest exercises the 7 "Independent Test"
// scenarios from the spec (line 46), in sequence on a single app.
//
// Sequence:
//  1. User A (admin) adds user B as editor → 201
//  2. User B (now editor) can GET members but gets 403 on admin actions
//  3. User A tries to remove themselves as only admin → 400
//  4. User A adds user C as admin, then removes themselves → 204
//  5. Duplicate add → 400 (ErrAlreadyMember)
//  6. Non-member tries to manage → 403
//  7. Editor/viewer tries to manage → 403
//
// To make the "only admin" case meaningful, the test app starts with
// only one admin (appadmin). loner is the app owner but gets removed
// from app_members at the start of the test, leaving appadmin as the
// sole admin. The test then adds a fresh "userC" actor to the pool
// for the role-change scenario.
func TestAppMembersIndependentTest(t *testing.T) {
	pool, h, actors, backendAppID, _ := appMembersHandlerTestPool(t)
	defer pool.Close()

	// Add a "userC" actor for the role-change scenario.
	ctx := context.Background()
	userC, err := CreateUser(ctx, pool, fmt.Sprintf("userc-%d@example.com", time.Now().UnixNano()), "userC", "hash", "member")
	if err != nil {
		t.Fatalf("create userC: %v", err)
	}

	// Remove loner from app_members so appadmin is the sole admin.
	if _, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.app_members WHERE backend_app_id = $1 AND user_id = $2`,
		backendAppID, actors["loner"].ID,
	); err != nil {
		t.Fatalf("remove loner from app_members: %v", err)
	}

	// Helper to dispatch a request.
	do := func(t *testing.T, actor *DashboardUser, method, pathFmt, body, userIDArg string) (int, *httptest.ResponseRecorder) {
		t.Helper()
		// pathFmt has either 1 %s (appID only) or 2 (appID + userID) — a
		// literal "{userId}" substring never appears in it, so the %s count
		// is the only reliable signal for both the URL substitution and the
		// chi route param below.
		hasUserID := strings.Count(pathFmt, "%s") == 2
		var uid string
		if userIDArg != "" {
			uid = userIDArg
		} else if hasUserID {
			uid = actor.ID
		}
		var path string
		if hasUserID {
			path = fmt.Sprintf(pathFmt, backendAppID, uid)
		} else {
			path = fmt.Sprintf(pathFmt, backendAppID)
		}
		var r *bytes.Reader
		if body != "" {
			r = bytes.NewReader([]byte(body))
		} else {
			r = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, r)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req = withUser(req, actor)
		rctx := chi.NewRouteContext()
		if hasUserID {
			rctx.URLParams.Add("id", backendAppID)
			rctx.URLParams.Add("userId", uid)
		} else {
			rctx.URLParams.Add("id", backendAppID)
		}
		req = req.WithContext(withCtx(req, rctx))
		w := httptest.NewRecorder()
		switch {
		case method == http.MethodGet && strings.HasSuffix(path, "/members"):
			h.ListAppMembers(w, req)
		case method == http.MethodPost && strings.HasSuffix(path, "/members"):
			h.AddAppMember(w, req)
		case method == http.MethodPatch:
			h.UpdateAppMember(w, req)
		case method == http.MethodDelete:
			h.RemoveAppMember(w, req)
		}
		return w.Code, w
	}

	// --- Scenario 1: appadmin (A) adds appeditor (B) as editor ---
	// appeditor is already a member (seeded in the pool), so this
	// would be a duplicate. To test the "add fresh member" path, we
	// add outsider instead, which is NOT a member yet.
	t.Run("sc1_admin_adds_editor", func(t *testing.T) {
		// appadmin is the sole admin now (loner removed).
		// Add outsider (not a member) as viewer — fresh add.
		code, _ := do(t, actors["appadmin"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+actors["outsider"].ID+`","role":"viewer"}`, "")
		if code != http.StatusCreated {
			t.Errorf("sc1: add outsider as viewer: status = %d, want 201", code)
		}
	})

	// --- Scenario 2: newly-added user (outsider, now viewer) can GET but gets 403 on admin actions ---
	t.Run("sc2_viewer_gets_but_cannot_manage", func(t *testing.T) {
		// outsider (now viewer) GETs — should get 403 per AC-6
		// (editor/viewer get 403 on any /members endpoint, including GET).
		code, _ := do(t, actors["outsider"], http.MethodGet,
			"/dashboard/api/apps/%s/members", "", "")
		if code != http.StatusForbidden {
			t.Errorf("sc2: viewer GET: status = %d, want 403", code)
		}
		// viewer tries to add — 403
		code, _ = do(t, actors["outsider"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+userC.ID+`","role":"viewer"}`, "")
		if code != http.StatusForbidden {
			t.Errorf("sc2: viewer POST: status = %d, want 403", code)
		}
	})

	// --- Scenario 3: appadmin (only admin) tries to remove themselves → 400 ---
	t.Run("sc3_last_admin_cannot_remove_self", func(t *testing.T) {
		// appadmin tries to remove themselves. Since appadmin is the
		// ONLY admin, this would leave zero admins → ErrLastAppAdmin → 400.
		code, _ := do(t, actors["appadmin"], http.MethodDelete,
			"/dashboard/api/apps/%s/members/%s", "", "")
		if code != http.StatusBadRequest {
			t.Errorf("sc3: last admin remove self: status = %d, want 400", code)
		}
	})

	// --- Scenario 4: appadmin adds userC as admin, then removes themselves → 204 ---
	t.Run("sc4_admin_adds_second_admin_then_removes_self", func(t *testing.T) {
		// Add userC as admin.
		code, _ := do(t, actors["appadmin"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+userC.ID+`","role":"admin"}`, "")
		if code != http.StatusCreated {
			t.Errorf("sc4: add userC as admin: status = %d, want 201", code)
		}
		// Now appadmin removes themselves (userC is also admin, so OK).
		code, _ = do(t, actors["appadmin"], http.MethodDelete,
			"/dashboard/api/apps/%s/members/%s", "", "")
		if code != http.StatusNoContent {
			t.Errorf("sc4: appadmin remove self with second admin: status = %d, want 204", code)
		}
	})

	// --- Scenario 5: duplicate add → 400 ---
	t.Run("sc5_duplicate_add", func(t *testing.T) {
		// userC is already a member (added in sc4). userC (now admin) tries to add userC again.
		code, _ := do(t, userC, http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+userC.ID+`","role":"admin"}`, "")
		if code != http.StatusBadRequest {
			t.Errorf("sc5: duplicate add: status = %d, want 400", code)
		}
	})

	// --- Scenario 6: non-member (a fresh user, not in actors) tries to manage → 403 ---
	t.Run("sc6_non_member_blocked", func(t *testing.T) {
		// Create a user that is not a member of any app.
		fresh, err := CreateUser(ctx, pool, fmt.Sprintf("fresh-%d@example.com", time.Now().UnixNano()), "fresh", "hash", "member")
		if err != nil {
			t.Fatalf("create fresh user: %v", err)
		}
		code, _ := do(t, fresh, http.MethodGet,
			"/dashboard/api/apps/%s/members", "", "")
		if code != http.StatusForbidden {
			t.Errorf("sc6: non-member GET: status = %d, want 403", code)
		}
		code, _ = do(t, fresh, http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+actors["appadmin"].ID+`","role":"viewer"}`, "")
		if code != http.StatusForbidden {
			t.Errorf("sc6: non-member POST: status = %d, want 403", code)
		}
	})

	// --- Scenario 7: appviewer (member, viewer role) tries to manage → 403 ---
	t.Run("sc7_viewer_blocked_from_manage", func(t *testing.T) {
		code, _ := do(t, actors["appviewer"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+actors["outsider"].ID+`","role":"viewer"}`, "")
		if code != http.StatusForbidden {
			t.Errorf("sc7: viewer POST: status = %d, want 403", code)
		}
		code, _ = do(t, actors["appviewer"], http.MethodPatch,
			"/dashboard/api/apps/%s/members/%s", `{"role":"editor"}`, "")
		if code != http.StatusForbidden {
			t.Errorf("sc7: viewer PATCH: status = %d, want 403", code)
		}
		code, _ = do(t, actors["appviewer"], http.MethodDelete,
			"/dashboard/api/apps/%s/members/%s", "", "")
		if code != http.StatusForbidden {
			t.Errorf("sc7: viewer DELETE: status = %d, want 403", code)
		}
	})
}

// TestAppMembersEdgeCases covers the non-7-scenarios edge cases: UNIQUE
// violation, PATCH/DELETE on non-member, invalid input.
func TestAppMembersEdgeCases(t *testing.T) {
	pool, h, actors, backendAppID, _ := appMembersHandlerTestPool(t)
	defer pool.Close()

	// Helper.
	do := func(t *testing.T, actor *DashboardUser, method, pathFmt, body, userIDArg string) int {
		t.Helper()
		// pathFmt has either 1 %s (appID only) or 2 (appID + userID) — a
		// literal "{userId}" substring never appears in it, so the %s count
		// is the only reliable signal for both the URL substitution and the
		// chi route param below.
		hasUserID := strings.Count(pathFmt, "%s") == 2
		var uid string
		if userIDArg != "" {
			uid = userIDArg
		} else if hasUserID {
			uid = actor.ID
		}
		var path string
		if hasUserID {
			path = fmt.Sprintf(pathFmt, backendAppID, uid)
		} else {
			path = fmt.Sprintf(pathFmt, backendAppID)
		}
		var r *bytes.Reader
		if body != "" {
			r = bytes.NewReader([]byte(body))
		} else {
			r = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, r)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req = withUser(req, actor)
		rctx := chi.NewRouteContext()
		if hasUserID {
			rctx.URLParams.Add("id", backendAppID)
			rctx.URLParams.Add("userId", uid)
		} else {
			rctx.URLParams.Add("id", backendAppID)
		}
		req = req.WithContext(withCtx(req, rctx))
		w := httptest.NewRecorder()
		switch {
		case method == http.MethodGet && strings.HasSuffix(path, "/members"):
			h.ListAppMembers(w, req)
		case method == http.MethodPost && strings.HasSuffix(path, "/members"):
			h.AddAppMember(w, req)
		case method == http.MethodPatch:
			h.UpdateAppMember(w, req)
		case method == http.MethodDelete:
			h.RemoveAppMember(w, req)
		}
		return w.Code
	}

	// --- PATCH on non-member → 404 ---
	t.Run("patch_non_member_404", func(t *testing.T) {
		// loner is admin; tries to PATCH outsider (not a member) → 404.
		code := do(t, actors["loner"], http.MethodPatch,
			"/dashboard/api/apps/%s/members/%s", `{"role":"viewer"}`, actors["outsider"].ID)
		if code != http.StatusNotFound {
			t.Errorf("PATCH non-member: status = %d, want 404", code)
		}
	})

	// --- DELETE on non-member → 404 ---
	t.Run("delete_non_member_404", func(t *testing.T) {
		code := do(t, actors["loner"], http.MethodDelete,
			"/dashboard/api/apps/%s/members/%s", "", actors["outsider"].ID)
		if code != http.StatusNotFound {
			t.Errorf("DELETE non-member: status = %d, want 404", code)
		}
	})

	// --- Invalid role in body → 400 ---
	t.Run("add_invalid_role_400", func(t *testing.T) {
		code := do(t, actors["loner"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+actors["outsider"].ID+`","role":"superuser"}`, "")
		if code != http.StatusBadRequest {
			t.Errorf("ADD invalid role: status = %d, want 400", code)
		}
	})

	// --- Missing user_id in body → 400 ---
	t.Run("add_missing_user_id_400", func(t *testing.T) {
		code := do(t, actors["loner"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"role":"viewer"}`, "")
		if code != http.StatusBadRequest {
			t.Errorf("ADD missing user_id: status = %d, want 400", code)
		}
	})

	// --- UNIQUE violation: add same user twice → second is 400 ---
	t.Run("add_duplicate_400", func(t *testing.T) {
		// outsider is not a member yet. First add → 201.
		code := do(t, actors["loner"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+actors["outsider"].ID+`","role":"viewer"}`, "")
		if code != http.StatusCreated {
			t.Errorf("first ADD: status = %d, want 201", code)
		}
		// Second add → 400 (ErrAlreadyMember from UNIQUE partial index).
		code = do(t, actors["loner"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+actors["outsider"].ID+`","role":"editor"}`, "")
		if code != http.StatusBadRequest {
			t.Errorf("second ADD (duplicate): status = %d, want 400", code)
		}
	})

	// --- PATCH demoting the last admin → 400 ---
	t.Run("patch_last_admin_demote_400", func(t *testing.T) {
		// appadmin is admin. appeditor tries to demote appadmin to viewer
		// — but appeditor is not CanManage, so 403. Use loner (admin)
		// as the actor. loner demotes appadmin to viewer. But loner is
		// also admin, so the count after demotion is 1 (loner) → OK,
		// should succeed. We need a scenario where the demotion would
		// leave zero admins. So: remove loner first, then appadmin
		// (now sole admin) tries to demote themselves via a PATCH.
		ctx := context.Background()
		// Make appadmin the sole admin.
		if _, err := pool.Exec(ctx,
			`UPDATE zeep_system.app_members SET role = 'viewer' WHERE backend_app_id = $1 AND user_id = $2`,
			backendAppID, actors["loner"].ID,
		); err != nil {
			t.Fatalf("demote loner: %v", err)
		}
		// appadmin demotes themselves to viewer. Should fail (400).
		code := do(t, actors["appadmin"], http.MethodPatch,
			"/dashboard/api/apps/%s/members/%s", `{"role":"viewer"}`, "")
		if code != http.StatusBadRequest {
			t.Errorf("PATCH last admin demote: status = %d, want 400", code)
		}
		// Restore loner for cleanup.
		if _, err := pool.Exec(ctx,
			`UPDATE zeep_system.app_members SET role = 'admin' WHERE backend_app_id = $1 AND user_id = $2`,
			backendAppID, actors["loner"].ID,
		); err != nil {
			t.Fatalf("restore loner: %v", err)
		}
	})

	// --- Audit log written on success ---
	t.Run("audit_log_on_add", func(t *testing.T) {
		ctx := context.Background()
		// Use a fresh user to avoid UNIQUE violation.
		fresh, err := CreateUser(ctx, pool, fmt.Sprintf("audit-%d@example.com", time.Now().UnixNano()), "audit", "hash", "member")
		if err != nil {
			t.Fatalf("create audit user: %v", err)
		}
		before := countAuditLog(t, pool, "app_member.added")
		code := do(t, actors["loner"], http.MethodPost,
			"/dashboard/api/apps/%s/members",
			`{"user_id":"`+fresh.ID+`","role":"viewer"}`, "")
		if code != http.StatusCreated {
			t.Errorf("ADD for audit: status = %d, want 201", code)
		}
		after := countAuditLog(t, pool, "app_member.added")
		if after != before+1 {
			t.Errorf("audit log: app_member.added count = %d, want %d", after, before+1)
		}
	})
}

// countAuditLog returns the number of audit_log rows with the given action.
func countAuditLog(t *testing.T, pool *db.Pool, action string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = $1`, action,
	).Scan(&n); err != nil {
		t.Fatalf("count audit log: %v", err)
	}
	return n
}

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// callUpdateEnduserRoles dispatches a PUT .../roles request straight to the
// handler (bypassing the router, same style as TestAppsRBACMatrix's `call`
// closure), with the chi URL param wired so chi.URLParam(r, "id") resolves.
func callUpdateEnduserRoles(h *Handler, actor *DashboardUser, appID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/dashboard/api/apps/"+appID+"/roles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actor)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()
	h.UpdateAppEnduserRoles(w, req)
	return w
}

// TestUpdateAppEnduserRolesHandler_InvalidFormat covers ROLECFG-04: a role
// that doesn't match identRe is rejected with 400 and the exact message
// already used by UpdateAppUserRole.
func TestUpdateAppEnduserRolesHandler_InvalidFormat(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	w := callUpdateEnduserRoles(h, actors["loner"], appID, `{"roles":["Admin"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "role must match ^[a-z][a-z0-9_]{0,62}$" {
		t.Fatalf("error = %q, want the identRe format message", resp["error"])
	}
}

// TestUpdateAppEnduserRolesHandler_Duplicate covers ROLECFG-03: submitting
// the same role twice in one request is rejected with 400, without
// persisting anything.
func TestUpdateAppEnduserRolesHandler_Duplicate(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	w := callUpdateEnduserRoles(h, actors["loner"], appID, `{"roles":["member","member"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "role already exists" {
		t.Fatalf("error = %q, want %q", resp["error"], "role already exists")
	}

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(app.EnduserRolesConfig) != 1 || app.EnduserRolesConfig[0] != "member" {
		t.Fatalf("expected the default list untouched, got %+v", app.EnduserRolesConfig)
	}
}

// TestUpdateAppEnduserRolesHandler_Success covers ROLECFG-02/ROLECFG-06: an
// update that only adds a role, and one that removes a role nobody uses,
// both persist and echo the new list back as 200.
func TestUpdateAppEnduserRolesHandler_Success(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	w := callUpdateEnduserRoles(h, actors["loner"], appID, `{"roles":["member","auditor"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Roles) != 2 || resp.Roles[0] != "member" || resp.Roles[1] != "auditor" {
		t.Fatalf("roles = %+v, want [member auditor]", resp.Roles)
	}

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(app.EnduserRolesConfig) != 2 || app.EnduserRolesConfig[1] != "auditor" {
		t.Fatalf("persisted roles = %+v, want [member auditor]", app.EnduserRolesConfig)
	}

	// Now remove "auditor" — nobody uses it, so this must succeed.
	w = callUpdateEnduserRoles(h, actors["loner"], appID, `{"roles":["member"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 removing an unused role; body=%s", w.Code, w.Body.String())
	}
	app, _, err = GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp after removal: %v", err)
	}
	if len(app.EnduserRolesConfig) != 1 || app.EnduserRolesConfig[0] != "member" {
		t.Fatalf("persisted roles after removal = %+v, want [member]", app.EnduserRolesConfig)
	}
}

// TestUpdateAppEnduserRolesHandler_BlockedByEndUser covers ROLECFG-05: a role
// still assigned to at least one _auth_users row cannot be removed — 409
// with the end-user count.
func TestUpdateAppEnduserRolesHandler_BlockedByEndUser(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, _, err := GetApp(ctx, pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := schemaNameForDB(app.Name)
	if err := UpdateAppEnduserRoles(ctx, pool, appID, []string{"member", "editor"}); err != nil {
		t.Fatalf("seed enduser_roles_config: %v", err)
	}
	// Drop defensively before AND after: a schema outside zeep_system isn't
	// touched by appsHandlerTestPool's own reset, and t.Cleanup would run
	// after this test's `defer pool.Close()` closes the connection — so the
	// drop has to happen synchronously here, on both ends, while pool is
	// still open.
	dropSchema := func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
	}
	dropSchema()
	defer dropSchema()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schema)); err != nil {
		t.Fatalf("create app schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q."_auth_users" (
		id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		email         TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL DEFAULT '',
		role          TEXT NOT NULL DEFAULT 'member'
	)`, schema)); err != nil {
		t.Fatalf("create _auth_users: %v", err)
	}
	if _, err := pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %q."_auth_users" (email, password_hash, role) VALUES ($1, '', 'editor')`, schema),
		"blocked-by-user@example.com",
	); err != nil {
		t.Fatalf("seed end-user with role editor: %v", err)
	}

	w := callUpdateEnduserRoles(h, actors["loner"], appID, `{"roles":["member"]}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error        string `json:"error"`
		Role         string `json:"role"`
		EndUserCount int    `json:"endUserCount"`
		PolicyCount  int    `json:"policyCount"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "role in use" || resp.Role != "editor" || resp.EndUserCount != 1 || resp.PolicyCount != 0 {
		t.Fatalf("resp = %+v, want error=role in use role=editor endUserCount=1 policyCount=0", resp)
	}

	after, _, err := GetApp(ctx, pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp after blocked removal: %v", err)
	}
	if len(after.EnduserRolesConfig) != 2 || after.EnduserRolesConfig[1] != "editor" {
		t.Fatalf("expected the blocked removal to leave the list untouched, got %+v", after.EnduserRolesConfig)
	}
}

// TestUpdateAppEnduserRolesHandler_BlockedByPolicy covers ROLECFG-05: a role
// still referenced by at least one table_policies row cannot be removed —
// 409 with the policy count.
func TestUpdateAppEnduserRolesHandler_BlockedByPolicy(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	if err := UpdateAppEnduserRoles(ctx, pool, appID, []string{"member", "viewer"}); err != nil {
		t.Fatalf("seed enduser_roles_config: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.table_policies (app_id, table_name, action, roles, clauses, pg_policy_name, created_by)
		 VALUES ($1, 'test_table', 'select', '["viewer"]'::jsonb, '[]'::jsonb, 'viewer_read', $2)`,
		appID, actors["loner"].ID,
	); err != nil {
		t.Fatalf("seed table_policies row referencing viewer: %v", err)
	}

	w := callUpdateEnduserRoles(h, actors["loner"], appID, `{"roles":["member"]}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error        string `json:"error"`
		Role         string `json:"role"`
		EndUserCount int    `json:"endUserCount"`
		PolicyCount  int    `json:"policyCount"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "role in use" || resp.Role != "viewer" || resp.EndUserCount != 0 || resp.PolicyCount != 1 {
		t.Fatalf("resp = %+v, want error=role in use role=viewer endUserCount=0 policyCount=1", resp)
	}

	after, _, err := GetApp(ctx, pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp after blocked removal: %v", err)
	}
	if len(after.EnduserRolesConfig) != 2 || after.EnduserRolesConfig[1] != "viewer" {
		t.Fatalf("expected the blocked removal to leave the list untouched, got %+v", after.EnduserRolesConfig)
	}
}

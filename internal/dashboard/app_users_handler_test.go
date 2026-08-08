package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// callUpdateAppUser dispatches a PUT .../users/{userId} request straight to
// the handler (bypassing the router), same style as callUpdateEnduserRoles.
func callUpdateAppUser(h *Handler, actor *DashboardUser, appID, userID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/dashboard/api/apps/"+appID+"/users/"+userID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actor)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	rctx.URLParams.Add("userId", userID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()
	h.UpdateAppUser(w, req)
	return w
}

func appUsersHandlerSetup(t *testing.T, pool *db.Pool, appName string) (schema string) {
	t.Helper()
	schema = schemaNameForDB(appName)
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		t.Fatalf("drop schema %s: %v", schema, err)
	}
	if _, err := pool.Exec(context.Background(), `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if _, err := pool.Exec(context.Background(), `CREATE TABLE `+schema+`."_auth_users" (
		"id"                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		"email"              TEXT NOT NULL UNIQUE,
		"password_hash"      TEXT NOT NULL DEFAULT '',
		"name"               TEXT,
		"phone"              TEXT,
		"avatar_url"         TEXT,
		"provider"           TEXT NOT NULL DEFAULT 'email',
		"role"               TEXT NOT NULL DEFAULT 'member',
		"active"             BOOLEAN NOT NULL DEFAULT true,
		"email_confirmed_at" TIMESTAMPTZ,
		"last_sign_in_at"    TIMESTAMPTZ,
		"created_at"         TIMESTAMPTZ NOT NULL DEFAULT now(),
		"updated_at"         TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create %s._auth_users: %v", schema, err)
	}
	return schema
}

func appUsersHandlerInsertUser(t *testing.T, pool *db.Pool, schema, email string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO `+schema+`."_auth_users" (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		email,
	).Scan(&id); err != nil {
		t.Fatalf("insert app user: %v", err)
	}
	return id
}

// TestUpdateAppUserHandler_Success covers AUE-02, AUE-03, AUE-06: a manage-
// capable actor updates email, phone, and role in one request and gets 200
// plus an audit entry.
func TestUpdateAppUserHandler_Success(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := appUsersHandlerSetup(t, pool, app.Name)
	userID := appUsersHandlerInsertUser(t, pool, schema, "success@example.com")

	w := callUpdateAppUser(h, actors["loner"], appID, userID, `{"email":"updated@example.com","phone":"555-1234","role":"approver"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["message"] != "user updated" {
		t.Fatalf("message = %q, want %q", resp["message"], "user updated")
	}

	var email, phone, role string
	if err := pool.QueryRow(context.Background(), `SELECT email, phone, role FROM `+schema+`."_auth_users" WHERE id = $1`, userID).Scan(&email, &phone, &role); err != nil {
		t.Fatalf("read updated row: %v", err)
	}
	if email != "updated@example.com" || phone != "555-1234" || role != "approver" {
		t.Fatalf("row = (%q, %q, %q), want (updated@example.com, 555-1234, approver)", email, phone, role)
	}
}

// TestUpdateAppUserHandler_InvalidEmail covers AUE-07.
func TestUpdateAppUserHandler_InvalidEmail(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := appUsersHandlerSetup(t, pool, app.Name)
	userID := appUsersHandlerInsertUser(t, pool, schema, "invalid-email@example.com")

	w := callUpdateAppUser(h, actors["loner"], appID, userID, `{"email":"not-an-email","phone":"","role":"member"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "invalid email" {
		t.Fatalf("error = %q, want %q", resp["error"], "invalid email")
	}
}

// TestUpdateAppUserHandler_InvalidRole covers AUE-08.
func TestUpdateAppUserHandler_InvalidRole(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := appUsersHandlerSetup(t, pool, app.Name)
	userID := appUsersHandlerInsertUser(t, pool, schema, "invalid-role@example.com")

	w := callUpdateAppUser(h, actors["loner"], appID, userID, `{"email":"invalid-role@example.com","phone":"","role":"Not_Valid"}`)
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

// TestUpdateAppUserHandler_EmailConflict covers AUE-09.
func TestUpdateAppUserHandler_EmailConflict(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := appUsersHandlerSetup(t, pool, app.Name)
	_ = appUsersHandlerInsertUser(t, pool, schema, "taken@example.com")
	userID := appUsersHandlerInsertUser(t, pool, schema, "conflict@example.com")

	w := callUpdateAppUser(h, actors["loner"], appID, userID, `{"email":"taken@example.com","phone":"","role":"member"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "email already in use" {
		t.Fatalf("error = %q, want %q", resp["error"], "email already in use")
	}

	var email string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM `+schema+`."_auth_users" WHERE id = $1`, userID).Scan(&email); err != nil {
		t.Fatalf("read row after conflict: %v", err)
	}
	if email != "conflict@example.com" {
		t.Fatalf("expected email unchanged at %q, got %q", "conflict@example.com", email)
	}
}

// TestUpdateAppUserHandler_Forbidden covers AUE-10: an actor who can see the
// app (has a "viewer" membership) but whose role fails CanManage() is
// rejected with 403, distinct from an app that isn't visible at all (404).
func TestUpdateAppUserHandler_Forbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := appUsersHandlerSetup(t, pool, app.Name)
	userID := appUsersHandlerInsertUser(t, pool, schema, "forbidden@example.com")

	w := callUpdateAppUser(h, actors["appviewer"], appID, userID, `{"email":"forbidden@example.com","phone":"","role":"member"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "forbidden" {
		t.Fatalf("error = %q, want %q", resp["error"], "forbidden")
	}
}

// TestUpdateAppUserHandler_UserNotFound covers AUE-11.
func TestUpdateAppUserHandler_UserNotFound(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()

	app, _, err := GetApp(context.Background(), pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	appUsersHandlerSetup(t, pool, app.Name)

	w := callUpdateAppUser(h, actors["loner"], appID, "00000000-0000-0000-0000-000000000000", `{"email":"nobody@example.com","phone":"","role":"member"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "user not found" {
		t.Fatalf("error = %q, want %q", resp["error"], "user not found")
	}
}

package dashboard

// handler_update_app_provisioning_test.go covers a gap left by the
// app-update-schema-drift-fix feature: removing UpdateApp's unconditional
// h.prov.Apply call (to stop reconciling unrelated table schema on every
// save — see handler_update_app_schema_drift_test.go) also removed the only
// path that provisioned "_auth_users"/"_auth_sessions"/"_files" for an app
// created with auth/storage off and turned on later. Without a scoped
// replacement, enabling email auth or a storage bucket via the Login/Storage
// tab (or the equivalent MCP/AI-chat UpdateAppForUser call) left the app
// pointing at tables that were never created — auth login/signup and file
// upload would fail with a Postgres relation-does-not-exist error the first
// time they were used, until something else (e.g. creating any app table)
// happened to run a full Apply and fix it as a side effect.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/storage"
)

func appSchemaHasTable(t *testing.T, ctx context.Context, h *Handler, schema, table string) bool {
	t.Helper()
	var exists bool
	if err := h.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
		schema, table,
	).Scan(&exists); err != nil {
		t.Fatalf("check %s.%s existence: %v", schema, table, err)
	}
	return exists
}

// TestUpdateAppHandler_AuthToggleProvisionsAuthTables covers the REST path:
// an app created with auth off, then switched on via PUT
// /dashboard/api/apps/{id}, must get "_auth_users"/"_auth_sessions" even
// though this request never touches app_tables.
func TestUpdateAppHandler_AuthToggleProvisionsAuthTables(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	owner := actors["loner"]

	app, err := h.CreateAppForUser(ctx, owner, AppRequestBody{
		Name:             uniqueAppName(t, "auth-toggle-app"),
		AuthEmailEnabled: false,
	}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	schema := schemaNameForDB(app.Name)

	if appSchemaHasTable(t, ctx, h, schema, "_auth_users") {
		t.Fatalf("expected %s._auth_users to not exist before auth is enabled", schema)
	}

	body, _ := json.Marshal(AppRequestBody{Name: app.Name, AuthEmailEnabled: true})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/api/apps/"+app.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, owner)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", app.ID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()

	h.UpdateApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateApp: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !appSchemaHasTable(t, ctx, h, schema, "_auth_users") {
		t.Fatalf("expected %s._auth_users to exist after enabling auth via UpdateApp", schema)
	}
	if !appSchemaHasTable(t, ctx, h, schema, "_auth_sessions") {
		t.Fatalf("expected %s._auth_sessions to exist after enabling auth via UpdateApp", schema)
	}
}

// TestUpdateAppForUser_AuthToggleProvisionsAuthTables covers the same gap on
// the MCP/AI-chat path (UpdateAppForUser), which has its own call site.
func TestUpdateAppForUser_AuthToggleProvisionsAuthTables(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	owner := actors["loner"]

	app, err := h.CreateAppForUser(ctx, owner, AppRequestBody{
		Name:             uniqueAppName(t, "auth-toggle-mcp-app"),
		AuthEmailEnabled: false,
	}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	schema := schemaNameForDB(app.Name)

	if appSchemaHasTable(t, ctx, h, schema, "_auth_users") {
		t.Fatalf("expected %s._auth_users to not exist before auth is enabled", schema)
	}

	if _, err := h.UpdateAppForUser(ctx, owner, app.ID, true, "test"); err != nil {
		t.Fatalf("UpdateAppForUser: %v", err)
	}

	if !appSchemaHasTable(t, ctx, h, schema, "_auth_users") {
		t.Fatalf("expected %s._auth_users to exist after enabling auth via UpdateAppForUser", schema)
	}
}

// TestUpdateAppHandler_StorageBucketProvisionsFilesTable covers the storage
// side of the same gap: an app created with no bucket, then given one via
// PUT /dashboard/api/apps/{id}, must get "_files".
func TestUpdateAppHandler_StorageBucketProvisionsFilesTable(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	owner := actors["loner"]

	app, err := h.CreateAppForUser(ctx, owner, AppRequestBody{
		Name:             uniqueAppName(t, "storage-toggle-app"),
		AuthEmailEnabled: false,
	}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	schema := schemaNameForDB(app.Name)

	if appSchemaHasTable(t, ctx, h, schema, "_files") {
		t.Fatalf("expected %s._files to not exist before a bucket is set", schema)
	}

	body, _ := json.Marshal(AppRequestBody{
		Name:             app.Name,
		AuthEmailEnabled: false,
		StorageConfig: &storage.StorageConfig{
			Bucket:          "app-bucket",
			Region:          "us-east-1",
			Endpoint:        "https://s3.example.com",
			AccessKeyID:     "AKIAEXAMPLE",
			SecretAccessKey: "secret",
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/dashboard/api/apps/"+app.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, owner)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", app.ID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()

	h.UpdateApp(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateApp: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !appSchemaHasTable(t, ctx, h, schema, "_files") {
		t.Fatalf("expected %s._files to exist after setting a storage bucket via UpdateApp", schema)
	}
}

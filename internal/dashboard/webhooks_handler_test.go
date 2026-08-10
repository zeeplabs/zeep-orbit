package dashboard

// webhooks_handler_test.go — T9 (subscription CRUD), T10 (mapping CRUD +
// activation) and T11 (delivery listing) of inbound-webhooks. Exercises the
// dashboard-session-authenticated endpoints via the real HTTP handlers, same
// depth/shape as table_policies_handler_test.go: RBAC gate, validation-error
// mapping, and the audit_log side effect on every mutation.
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// webhooksHandlerTestPool provisions zeep_system, seeds an app admin + a
// non-admin member, one test app registered in the registry with an
// "employees" table (so SaveEventMapping's registry.GetTable validation has
// something real to check against), and returns everything a test needs.
func webhooksHandlerTestPool(t *testing.T) (*db.Pool, *Handler, map[string]*DashboardUser, string, string) {
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
		t.Fatalf("drop zeep_system: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	const appName = "webhookhandlertest"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, appName))
	})

	actors := map[string]*DashboardUser{}
	for _, ad := range []struct{ key, role string }{
		{"appadmin", "member"},
		{"appeditor", "member"},
	} {
		email := fmt.Sprintf("wh-%s-%d@example.com", ad.key, time.Now().UnixNano())
		u, err := CreateUser(ctx, pool, email, ad.key, "hash", ad.role)
		if err != nil {
			pool.Close()
			t.Fatalf("create user %s: %v", email, err)
		}
		actors[ad.key] = u
	}

	var appID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		appName, actors["appadmin"].ID,
	).Scan(&appID); err != nil {
		pool.Close()
		t.Fatalf("create test app: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		appID, actors["appadmin"].ID,
	); err != nil {
		pool.Close()
		t.Fatalf("seed appadmin membership: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'editor')`,
		appID, actors["appeditor"].ID,
	); err != nil {
		pool.Close()
		t.Fatalf("seed appeditor membership: %v", err)
	}

	reg := registry.New()
	reg.Register(&registry.App{
		Config:     config.AppConfig{Name: appName},
		SchemaName: appName,
		Tables: map[string]*registry.Table{
			"employees": {
				Name: "employees",
				Columns: []registry.Column{
					{Name: "external_id", Type: "text"},
					{Name: "full_name", Type: "text"},
					{Name: "status", Type: "text"},
				},
			},
		},
	})

	h := NewHandler(pool, reg, zap.NewNop())
	return pool, h, actors, appID, appName
}

func createTestWebhook(t *testing.T, h *Handler, appID string, admin *DashboardUser) createWebhookResponse {
	t.Helper()
	body := `{"name":"employees sync","method":"POST","event_type_path":"eventType"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/dashboard/api/apps/%s/webhooks", appID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, admin)
	req = withChiParams(req, map[string]string{"id": appID})
	w := httptest.NewRecorder()
	h.CreateWebhook(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create webhook: status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
	var resp createWebhookResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return resp
}

// TestCreateWebhookHandler_HappyPathReturnsTokenOnceAndAudits: T9 Done-when
// "Create returns the plaintext token exactly once ... never again on
// subsequent Get/List" and "produce one audit_log entry".
func TestCreateWebhookHandler_HappyPathReturnsTokenOnceAndAudits(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	if created.Token == "" {
		t.Fatal("expected a non-empty plaintext token on create")
	}
	if created.Status != "capture" {
		t.Errorf("status = %q, want %q (new webhook starts in capture mode)", created.Status, "capture")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'webhook.create'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for webhook.create = %d, want 1", auditCount)
	}

	// Get never echoes the token again.
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s", appID, created.ID), nil)
	getReq = withUser(getReq, actors["appadmin"])
	getReq = withChiParams(getReq, map[string]string{"id": appID, "webhookId": created.ID})
	wGet := httptest.NewRecorder()
	h.GetWebhook(wGet, getReq)
	if wGet.Code != http.StatusOK {
		t.Fatalf("get webhook: status = %d, want 200, body=%s", wGet.Code, wGet.Body.String())
	}
	if bytes.Contains(wGet.Body.Bytes(), []byte(created.Token)) {
		t.Error("GET response leaked the plaintext token")
	}
	if bytes.Contains(wGet.Body.Bytes(), []byte("token_hash")) {
		t.Error("GET response leaked token_hash field")
	}
}

// TestCreateWebhookHandler_NonAdminForbidden mirrors the table_policies RBAC
// test: an app member below CanManage (editor) resolves the app but is
// rejected by role.CanManage() → 403.
func TestCreateWebhookHandler_NonAdminForbidden(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	body := `{"name":"x","method":"POST","event_type_path":"eventType"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/dashboard/api/apps/%s/webhooks", appID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appeditor"])
	req = withChiParams(req, map[string]string{"id": appID})
	w := httptest.NewRecorder()
	h.CreateWebhook(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("appeditor create: status = %d, want 403", w.Code)
	}
}

// TestListWebhooksHandler: T9 list happy path.
func TestListWebhooksHandler(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	createTestWebhook(t, h, appID, actors["appadmin"])

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dashboard/api/apps/%s/webhooks", appID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID})
	w := httptest.NewRecorder()
	h.ListWebhooks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var rows []webhookResponse
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(rows))
	}
}

// TestRotateWebhookTokenHandler_InvalidatesOldTokenAndAudits: T9 Done-when
// "Rotate ... produce one audit_log entry with the correct action name" and
// spec P3 AC1 "invalidate the old token immediately".
func TestRotateWebhookTokenHandler_InvalidatesOldTokenAndAudits(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/rotate-token", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.RotateWebhookToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if resp["token"] == "" || resp["token"] == created.Token {
		t.Fatalf("expected a new, different token, got %q (old %q)", resp["token"], created.Token)
	}
	if !VerifyWebhookToken(hashWebhookToken(resp["token"]), resp["token"]) {
		t.Fatal("new token does not verify against its own hash")
	}
	// Old token must no longer verify against the stored (now rotated) hash.
	wh, err := GetWebhookByID(context.Background(), pool, appID, created.ID)
	if err != nil {
		t.Fatalf("get webhook after rotate: %v", err)
	}
	if VerifyWebhookToken(wh.TokenHash, created.Token) {
		t.Fatal("old token still verifies after rotation — expected immediate invalidation")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'webhook.rotate_token'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for webhook.rotate_token = %d, want 1", auditCount)
	}
}

// TestDeleteWebhookHandler_SoftDeletesAndAudits: T9 Done-when "Delete is a
// soft-delete visible in the store but the webhook's URL now 404s" (verified
// here via GetWebhookByID directly, since GetWebhookByID is exactly what the
// public route's lookup — T6 — uses) and "produce one audit_log entry".
func TestDeleteWebhookHandler_SoftDeletesAndAudits(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.DeleteWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	if _, err := GetWebhookByID(context.Background(), pool, appID, created.ID); err != ErrWebhookNotFound {
		t.Fatalf("expected ErrWebhookNotFound (soft-deleted, no longer resolvable), got %v", err)
	}

	var deletedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT deleted_at FROM zeep_system.webhook_subscriptions WHERE id = $1`, created.ID,
	).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("expected deleted_at to be set (soft-delete), row was hard-deleted or never touched")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'webhook.delete'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for webhook.delete = %d, want 1", auditCount)
	}
}

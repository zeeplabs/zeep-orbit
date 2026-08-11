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

func createTestWebhook(t *testing.T, h *Handler, appID string, admin *DashboardUser) webhookResponse {
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
	var resp webhookResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return resp
}

// TestCreateWebhookHandler_HappyPathAlwaysReturnsTokenAndAudits: the token
// is encrypted (recoverable), not hashed, precisely so the dashboard can
// always show the full callback URL — Create, Get, and List must all return
// the same plaintext token, not just Create once.
func TestCreateWebhookHandler_HappyPathAlwaysReturnsTokenAndAudits(t *testing.T) {
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

	// Get echoes the same token again — this is the whole point of storing
	// it encrypted instead of hashed.
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s", appID, created.ID), nil)
	getReq = withUser(getReq, actors["appadmin"])
	getReq = withChiParams(getReq, map[string]string{"id": appID, "webhookId": created.ID})
	wGet := httptest.NewRecorder()
	h.GetWebhook(wGet, getReq)
	if wGet.Code != http.StatusOK {
		t.Fatalf("get webhook: status = %d, want 200, body=%s", wGet.Code, wGet.Body.String())
	}
	var getResp webhookResponse
	if err := json.Unmarshal(wGet.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.Token != created.Token {
		t.Errorf("GET token = %q, want it to match the token from Create (%q)", getResp.Token, created.Token)
	}
	if bytes.Contains(wGet.Body.Bytes(), []byte("token_secret")) {
		t.Error("GET response leaked the raw token_secret field")
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
	var resp webhookResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if resp.Token == "" || resp.Token == created.Token {
		t.Fatalf("expected a new, different token, got %q (old %q)", resp.Token, created.Token)
	}
	// Old token must no longer verify against the stored (now rotated) secret.
	wh, err := GetWebhookByID(context.Background(), pool, appID, created.ID)
	if err != nil {
		t.Fatalf("get webhook after rotate: %v", err)
	}
	if !VerifyWebhookToken(wh.TokenSecret, resp.Token) {
		t.Fatal("new token does not verify against the rotated secret")
	}
	if VerifyWebhookToken(wh.TokenSecret, created.Token) {
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

// ----------------------------------------------------------------------------
// T10: mapping CRUD + activation
// ----------------------------------------------------------------------------

func saveTestMapping(t *testing.T, h *Handler, appID, webhookID string, admin *DashboardUser, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/mappings", appID, webhookID), bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, admin)
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": webhookID})
	w := httptest.NewRecorder()
	h.SaveEventMapping(w, req)
	return w
}

// TestSaveEventMappingHandler_HappyPathAudits: T10 Done-when "Every mutation
// produces an audit_log entry".
func TestSaveEventMappingHandler_HappyPathAudits(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	body := `{"event_type_value":"employee.created","action":"insert","target_table":"employees","field_mappings":[{"source_path":"id","column":"external_id"},{"source_path":"name","column":"full_name"}]}`
	w := saveTestMapping(t, h, appID, created.ID, actors["appadmin"], body)
	if w.Code != http.StatusCreated {
		t.Fatalf("save mapping: status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
	var row mappingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatalf("decode save mapping response: %v", err)
	}
	// mappingResponse's json tags are the wire contract the frontend
	// actually decodes (snake_case) — EventMappingRow has none, so
	// decoding into it here would silently mask a casing regression the
	// same way the pre-fix webhook_deliveries response did (see T17).
	if row.EventTypeValue != "employee.created" {
		t.Errorf("EventTypeValue = %q, want %q", row.EventTypeValue, "employee.created")
	}
	if row.Action != "insert" || row.TargetTable != "employees" {
		t.Errorf("saved mapping = %+v, want action=insert target_table=employees", row)
	}
	if len(row.FieldMappings) != 2 || row.FieldMappings[0].Column != "external_id" {
		t.Errorf("FieldMappings = %+v, want 2 entries starting with column=external_id", row.FieldMappings)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'webhook.mapping.save'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for webhook.mapping.save = %d, want 1", auditCount)
	}
}

// TestSaveEventMappingHandler_UnknownTableReturns400: T10 Done-when "Save
// mapping surfaces the store's validation errors (unknown table/column ...)
// as 400, not 500".
func TestSaveEventMappingHandler_UnknownTableReturns400(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	body := `{"event_type_value":"employee.created","action":"insert","target_table":"does_not_exist","field_mappings":[{"source_path":"id","column":"external_id"}]}`
	w := saveTestMapping(t, h, appID, created.ID, actors["appadmin"], body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] == "" || resp["error"] == "internal error" {
		t.Errorf("expected a descriptive validation error, got %q", resp["error"])
	}
}

// TestActivateWebhookHandler_ZeroMappingsReturns400: T10 Done-when "Activate
// on a webhook with zero mappings returns 400 with a clear message, not a
// silent no-op" (spec Edge Cases).
func TestActivateWebhookHandler_ZeroMappingsReturns400(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/activate", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.ActivateWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected a clear validation error message")
	}

	wh, err := GetWebhookByID(context.Background(), pool, appID, created.ID)
	if err != nil {
		t.Fatalf("get webhook: %v", err)
	}
	if wh.Status != "capture" {
		t.Errorf("status = %q, want %q (rejected activation must not change state)", wh.Status, "capture")
	}
}

// TestActivateWebhookHandler_SuccessAudits: T10 activate-success case,
// spec P2 AC2 "webhook that has at least one saved mapping" flips to active.
func TestActivateWebhookHandler_SuccessAudits(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	mapBody := `{"event_type_value":"employee.created","action":"insert","target_table":"employees","field_mappings":[{"source_path":"id","column":"external_id"}]}`
	if w := saveTestMapping(t, h, appID, created.ID, actors["appadmin"], mapBody); w.Code != http.StatusCreated {
		t.Fatalf("save mapping precondition: status = %d, body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/activate", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.ActivateWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("activate: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	wh, err := GetWebhookByID(context.Background(), pool, appID, created.ID)
	if err != nil {
		t.Fatalf("get webhook: %v", err)
	}
	if wh.Status != "active" {
		t.Errorf("status = %q, want %q", wh.Status, "active")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'webhook.activate'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for webhook.activate = %d, want 1", auditCount)
	}
}

// TestListEventMappingsHandler_ReturnsSnakeCaseFields: guards the exact bug
// class T17 already found once for webhook_deliveries — EventMappingRow has
// no json tags, so writing it (or a slice of it) directly to the response
// marshals PascalCase keys the frontend's snake_case-decoding never reads,
// silently reaching the UI as blank/undefined fields.
func TestListEventMappingsHandler_ReturnsSnakeCaseFields(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	mapBody := `{"event_type_value":"employee.created","action":"insert","target_table":"employees","field_mappings":[{"source_path":"id","column":"external_id"}]}`
	if w := saveTestMapping(t, h, appID, created.ID, actors["appadmin"], mapBody); w.Code != http.StatusCreated {
		t.Fatalf("save mapping precondition: status = %d, body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/mappings", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.ListEventMappings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list mappings: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	if !bytes.Contains(w.Body.Bytes(), []byte(`"event_type_value"`)) {
		t.Errorf("response is missing the snake_case key \"event_type_value\" the frontend decodes -- body=%s", w.Body.String())
	}
	// field_mappings is a nested slice — decoding it into mappingResponse
	// below would self-mask the exact bug this test exists to catch (a
	// PascalCase FieldMappings/SourcePath/Column would just leave the
	// decoded struct's fields zero-valued, not fail to unmarshal), so assert
	// the raw wire bytes directly, same as the event_type_value check above.
	for _, key := range []string{`"field_mappings"`, `"source_path"`, `"column"`} {
		if !bytes.Contains(w.Body.Bytes(), []byte(key)) {
			t.Errorf("response is missing the snake_case key %s the frontend decodes -- body=%s", key, w.Body.String())
		}
	}

	var rows []mappingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list mappings response: %v", err)
	}
	if len(rows) != 1 || rows[0].EventTypeValue != "employee.created" || rows[0].Action != "insert" || rows[0].TargetTable != "employees" {
		t.Fatalf("mappings = %+v, want 1 entry with event_type_value=employee.created action=insert target_table=employees", rows)
	}
	if len(rows[0].FieldMappings) != 1 || rows[0].FieldMappings[0].SourcePath != "id" || rows[0].FieldMappings[0].Column != "external_id" {
		t.Errorf("FieldMappings = %+v, want [{source_path:id column:external_id}]", rows[0].FieldMappings)
	}
}

// TestDeleteEventMappingHandler: T10 delete mapping case.
func TestDeleteEventMappingHandler(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	mapBody := `{"event_type_value":"employee.created","action":"insert","target_table":"employees","field_mappings":[{"source_path":"id","column":"external_id"}]}`
	wCreate := saveTestMapping(t, h, appID, created.ID, actors["appadmin"], mapBody)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("save mapping precondition: status = %d, body=%s", wCreate.Code, wCreate.Body.String())
	}
	var mapping mappingResponse
	if err := json.Unmarshal(wCreate.Body.Bytes(), &mapping); err != nil {
		t.Fatalf("decode mapping: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/mappings/%s", appID, created.ID, mapping.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID, "mappingId": mapping.ID})
	w := httptest.NewRecorder()
	h.DeleteEventMapping(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete mapping: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	mappings, err := ListEventMappings(context.Background(), pool, created.ID)
	if err != nil {
		t.Fatalf("list mappings after delete: %v", err)
	}
	if len(mappings) != 0 {
		t.Errorf("expected 0 mappings after delete, got %d", len(mappings))
	}
}

// ----------------------------------------------------------------------------
// T11: delivery log listing
// ----------------------------------------------------------------------------

// TestListWebhookDeliveriesHandler_NewestFirstWithPayloadAndError: T11
// Done-when "Endpoint returns deliveries newest-first with raw payload and
// error detail included per entry" (spec P2 dashboard-delivery-log AC1/AC2).
func TestListWebhookDeliveriesHandler_NewestFirstWithPayloadAndError(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	if err := InsertDelivery(context.Background(), pool, DeliveryEntry{
		WebhookID: created.ID, HTTPStatus: 200, Outcome: "captured", RawPayload: []byte(`{"a":1}`),
	}); err != nil {
		t.Fatalf("insert delivery 1: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := InsertDelivery(context.Background(), pool, DeliveryEntry{
		WebhookID: created.ID, HTTPStatus: 500, Outcome: "write_error", RawPayload: []byte(`{"b":2}`), ErrorDetail: "boom",
	}); err != nil {
		t.Fatalf("insert delivery 2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/deliveries", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.ListWebhookDeliveries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list deliveries: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	// Decoded into deliveryResponse (the actual wire DTO, snake_case json
	// tags), not the internal DeliveryRow store type -- DeliveryRow has no
	// json tags at all, so decoding the handler's real response into it
	// would silently pass via Go's case-insensitive-only field matching
	// while masking a real wire-format bug (PascalCase keys reaching a
	// frontend that reads snake_case; see toDeliveryResponse).
	var rows []deliveryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode deliveries response: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(rows))
	}
	if rows[0].Outcome != "write_error" {
		t.Errorf("rows[0].Outcome = %q, want %q (newest first)", rows[0].Outcome, "write_error")
	}
	if rows[0].ErrorDetail == nil || *rows[0].ErrorDetail != "boom" {
		t.Errorf("rows[0].ErrorDetail = %v, want %q", rows[0].ErrorDetail, "boom")
	}
	if rows[0].RawPayload["b"] != float64(2) {
		t.Errorf("rows[0].RawPayload = %v, want raw payload preserved", rows[0].RawPayload)
	}
	if rows[1].Outcome != "captured" {
		t.Errorf("rows[1].Outcome = %q, want %q", rows[1].Outcome, "captured")
	}
}

// TestListWebhookDeliveriesHandler_EmptyForFreshWebhook: T11 Done-when
// "empty list for a fresh webhook".
func TestListWebhookDeliveriesHandler_EmptyForFreshWebhook(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dashboard/api/apps/%s/webhooks/%s/deliveries", appID, created.ID), nil)
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "webhookId": created.ID})
	w := httptest.NewRecorder()
	h.ListWebhookDeliveries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list deliveries: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var rows []deliveryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode deliveries response: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 deliveries for a fresh webhook, got %d", len(rows))
	}
}

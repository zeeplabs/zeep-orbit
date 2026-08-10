package dashboard

// table_policies_handler_test.go — T10 of end-user-row-policies. Exercises
// the 3 policy endpoints (GET/POST list+create, DELETE) via the real HTTP
// handlers: the RBAC gate (CanManage — admin/superadmin only), the 400/409
// error mapping from policy.Builder/store errors, and the audit_log side
// effect on success.
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

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// tablePoliciesHandlerTestPool provisions zeep_system, seeds 4 actors
// (superadmin, app-admin, app-editor, outsider/non-member) plus one test app
// with a real physical schema+table (policy.Builder issues real DDL against
// it, so a fake app_tables-only row is not enough here, unlike some other
// handler tests). Returns the pool, a Handler wired to it, the actors map,
// the appID, and the physical table name.
func tablePoliciesHandlerTestPool(t *testing.T) (*db.Pool, *Handler, map[string]*DashboardUser, string, string) {
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

	const appName = "policyhandlertest"
	if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, appName)); err != nil {
		pool.Close()
		t.Fatalf("drop app schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, appName)); err != nil {
		pool.Close()
		t.Fatalf("create app schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %q.requests (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			requester_id UUID NOT NULL,
			status       TEXT NOT NULL DEFAULT 'pending'
		)`, appName,
	)); err != nil {
		pool.Close()
		t.Fatalf("create physical table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, appName))
	})

	actors := map[string]*DashboardUser{}
	for _, ad := range []struct{ key, email, role string }{
		{"super", fmt.Sprintf("tp-super-%d@example.com", time.Now().UnixNano()), "superadmin"},
		{"appadmin", fmt.Sprintf("tp-appadmin-%d@example.com", time.Now().UnixNano()), "member"},
		{"appeditor", fmt.Sprintf("tp-appeditor-%d@example.com", time.Now().UnixNano()), "member"},
		{"outsider", fmt.Sprintf("tp-outsider-%d@example.com", time.Now().UnixNano()), "member"},
	} {
		u, err := CreateUser(ctx, pool, ad.email, ad.key, "hash", ad.role)
		if err != nil {
			pool.Close()
			t.Fatalf("create user %s: %v", ad.email, err)
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
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
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

	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_tables (app_id, name, rls, columns, indexes)
		 VALUES ($1, 'requests', '', $2::jsonb, '[]'::jsonb)`,
		appID,
		`[{"name":"requester_id","type":"uuid"},{"name":"status","type":"text"}]`,
	); err != nil {
		pool.Close()
		t.Fatalf("seed app_tables row: %v", err)
	}

	h := NewHandler(pool, registry.New(), zap.NewNop())
	return pool, h, actors, appID, "requests"
}

// withChiParams attaches chi URL params to a request context so
// chi.URLParam works inside the handler under test. Reuses withCtx (defined
// in apps_handler_test.go) for the actual context wiring.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(withCtx(r, rctx))
}

// TestCreateTablePolicyHandler_NonAdminForbidden: an app member with a role
// below CanManage (editor) is a resolvable app member so GetApp succeeds,
// and is rejected at the CanManage() gate → 403. An outsider (no app_members
// row at all) never resolves an effective role, so GetApp itself returns
// ErrNotFound before the CanManage() check ever runs → 404, same as every
// other per-app handler in this codebase (CreateAppTable, UpdateAppTable,
// etc.) — this is existing, intentional behavior, not specific to policies.
func TestCreateTablePolicyHandler_NonAdminForbidden(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	body := `{"name":"p1","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`

	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appeditor"])
	req = withChiParams(req, map[string]string{"id": appID, "table": table})
	w := httptest.NewRecorder()
	h.CreateTablePolicy(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("appeditor: status = %d, want 403", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(body)))
	req2.Header.Set("Content-Type", "application/json")
	req2 = withUser(req2, actors["outsider"])
	req2 = withChiParams(req2, map[string]string{"id": appID, "table": table})
	w2 := httptest.NewRecorder()
	h.CreateTablePolicy(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("outsider: status = %d, want 404 (non-member can't resolve the app at all)", w2.Code)
	}
}

func TestCreateTablePolicyHandler_AdminHappyPath(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	body := `{"name":"select_active","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"=","value_source":"literal","value":"active"}]}`
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "table": table})

	w := httptest.NewRecorder()
	h.CreateTablePolicy(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
	}

	var row TablePolicyRow
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if row.PgPolicyName != "select_active" {
		t.Errorf("pg_policy_name = %q, want %q", row.PgPolicyName, "select_active")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'app.table_policy.create'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for app.table_policy.create = %d, want 1", auditCount)
	}
}

func TestCreateTablePolicyHandler_InvalidClauseReturns400WithBuilderMessage(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	body := `{"name":"bad","action":"select","roles":["member"],"clauses":[{"column":"does_not_exist","operator":"=","value_source":"literal","value":"x"}]}`
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "table": table})

	w := httptest.NewRecorder()
	h.CreateTablePolicy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] == "" || resp["error"] == "internal error" {
		t.Errorf("expected a descriptive builder error message, got %q", resp["error"])
	}
}

func TestCreateTablePolicyHandler_DuplicateReturns409(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	body := `{"name":"dup","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	create := func() int {
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req = withUser(req, actors["appadmin"])
		req = withChiParams(req, map[string]string{"id": appID, "table": table})
		w := httptest.NewRecorder()
		h.CreateTablePolicy(w, req)
		return w.Code
	}
	if code := create(); code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201", code)
	}
	if code := create(); code != http.StatusConflict {
		t.Fatalf("second create (duplicate): status = %d, want 409", code)
	}
}

func TestListTablePoliciesHandler(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	createBody := `{"name":"list_me","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	createReq := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq = withUser(createReq, actors["appadmin"])
	createReq = withChiParams(createReq, map[string]string{"id": appID, "table": table})
	wCreate := httptest.NewRecorder()
	h.CreateTablePolicy(wCreate, createReq)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201, body=%s", wCreate.Code, wCreate.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table), nil)
	listReq = withUser(listReq, actors["appadmin"])
	listReq = withChiParams(listReq, map[string]string{"id": appID, "table": table})
	wList := httptest.NewRecorder()
	h.ListTablePolicies(wList, listReq)
	if wList.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200, body=%s", wList.Code, wList.Body.String())
	}
	var rows []TablePolicyRow
	if err := json.Unmarshal(wList.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(rows) != 1 || rows[0].PgPolicyName != "list_me" {
		t.Fatalf("expected 1 policy named %q, got %+v", "list_me", rows)
	}

	// Non-admin member still forbidden on list — spec groups list under
	// CanManage. (An outsider with no app_members row would 404 instead,
	// same as every other per-app handler — see
	// TestCreateTablePolicyHandler_NonAdminForbidden.)
	forbiddenReq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table), nil)
	forbiddenReq = withUser(forbiddenReq, actors["appeditor"])
	forbiddenReq = withChiParams(forbiddenReq, map[string]string{"id": appID, "table": table})
	wForbidden := httptest.NewRecorder()
	h.ListTablePolicies(wForbidden, forbiddenReq)
	if wForbidden.Code != http.StatusForbidden {
		t.Errorf("appeditor list: status = %d, want 403", wForbidden.Code)
	}
}

func TestUpdateTablePolicyHandler_AdminHappyPath(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	createBody := `{"name":"editable","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	createReq := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq = withUser(createReq, actors["appadmin"])
	createReq = withChiParams(createReq, map[string]string{"id": appID, "table": table})
	wCreate := httptest.NewRecorder()
	h.CreateTablePolicy(wCreate, createReq)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201, body=%s", wCreate.Code, wCreate.Body.String())
	}
	var created TablePolicyRow
	if err := json.Unmarshal(wCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	updateBody := `{"name":"editable","action":"select","roles":["member","admin"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	updateReq := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, created.ID),
		bytes.NewReader([]byte(updateBody)))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq = withUser(updateReq, actors["appadmin"])
	updateReq = withChiParams(updateReq, map[string]string{"id": appID, "table": table, "policyId": created.ID})
	wUpdate := httptest.NewRecorder()
	h.UpdateTablePolicy(wUpdate, updateReq)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200, body=%s", wUpdate.Code, wUpdate.Body.String())
	}

	var updated TablePolicyRow
	if err := json.Unmarshal(wUpdate.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if len(updated.Roles) != 2 || updated.Roles[1] != "admin" {
		t.Errorf("roles = %v, want [member admin]", updated.Roles)
	}
	if updated.UpdatedAt == nil {
		t.Error("expected updated_at to be set in the response")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'app.table_policy.update'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for app.table_policy.update = %d, want 1", auditCount)
	}
}

func TestUpdateTablePolicyHandler_InvalidClauseReturns400(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	createBody := `{"name":"editable2","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	createReq := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq = withUser(createReq, actors["appadmin"])
	createReq = withChiParams(createReq, map[string]string{"id": appID, "table": table})
	wCreate := httptest.NewRecorder()
	h.CreateTablePolicy(wCreate, createReq)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201, body=%s", wCreate.Code, wCreate.Body.String())
	}
	var created TablePolicyRow
	if err := json.Unmarshal(wCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	badBody := `{"name":"editable2","action":"select","roles":["member"],"clauses":[{"column":"does_not_exist","operator":"=","value_source":"literal","value":"x"}]}`
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, created.ID),
		bytes.NewReader([]byte(badBody)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "table": table, "policyId": created.ID})
	w := httptest.NewRecorder()
	h.UpdateTablePolicy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] == "" || resp["error"] == "internal error" {
		t.Errorf("expected a descriptive builder error message, got %q", resp["error"])
	}
}

func TestUpdateTablePolicyHandler_UnknownPolicyReturns404(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	body := `{"name":"whatever","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, "00000000-0000-0000-0000-000000000000"),
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "table": table, "policyId": "00000000-0000-0000-0000-000000000000"})
	w := httptest.NewRecorder()
	h.UpdateTablePolicy(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateTablePolicyHandler_ConflictReturns409(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	createOne := func(name string) TablePolicyRow {
		body := fmt.Sprintf(`{"name":%q,"action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`, name)
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req = withUser(req, actors["appadmin"])
		req = withChiParams(req, map[string]string{"id": appID, "table": table})
		w := httptest.NewRecorder()
		h.CreateTablePolicy(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: status = %d, want 201, body=%s", name, w.Code, w.Body.String())
		}
		var row TablePolicyRow
		if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		return row
	}

	createOne("taken")
	movable := createOne("movable")

	updateBody := `{"name":"taken","action":"select","roles":["admin"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, movable.ID),
		bytes.NewReader([]byte(updateBody)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actors["appadmin"])
	req = withChiParams(req, map[string]string{"id": appID, "table": table, "policyId": movable.ID})
	w := httptest.NewRecorder()
	h.UpdateTablePolicy(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteTablePolicyHandler(t *testing.T) {
	pool, h, actors, appID, table := tablePoliciesHandlerTestPool(t)
	defer pool.Close()

	createBody := `{"name":"to_delete","action":"select","roles":["member"],"clauses":[{"column":"status","operator":"IS NOT NULL"}]}`
	createReq := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies", appID, table),
		bytes.NewReader([]byte(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq = withUser(createReq, actors["appadmin"])
	createReq = withChiParams(createReq, map[string]string{"id": appID, "table": table})
	wCreate := httptest.NewRecorder()
	h.CreateTablePolicy(wCreate, createReq)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201, body=%s", wCreate.Code, wCreate.Body.String())
	}
	var created TablePolicyRow
	if err := json.Unmarshal(wCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// Non-admin forbidden.
	forbiddenReq := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, created.ID), nil)
	forbiddenReq = withUser(forbiddenReq, actors["appeditor"])
	forbiddenReq = withChiParams(forbiddenReq, map[string]string{"id": appID, "table": table, "policyId": created.ID})
	wForbidden := httptest.NewRecorder()
	h.DeleteTablePolicy(wForbidden, forbiddenReq)
	if wForbidden.Code != http.StatusForbidden {
		t.Fatalf("editor delete: status = %d, want 403", wForbidden.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, created.ID), nil)
	deleteReq = withUser(deleteReq, actors["appadmin"])
	deleteReq = withChiParams(deleteReq, map[string]string{"id": appID, "table": table, "policyId": created.ID})
	wDelete := httptest.NewRecorder()
	h.DeleteTablePolicy(wDelete, deleteReq)
	if wDelete.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want 200, body=%s", wDelete.Code, wDelete.Body.String())
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'app.table_policy.delete'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for app.table_policy.delete = %d, want 1", auditCount)
	}

	// Deleting again → 404.
	notFoundReq := httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/policies/%s", appID, table, created.ID), nil)
	notFoundReq = withUser(notFoundReq, actors["appadmin"])
	notFoundReq = withChiParams(notFoundReq, map[string]string{"id": appID, "table": table, "policyId": created.ID})
	wNotFound := httptest.NewRecorder()
	h.DeleteTablePolicy(wNotFound, notFoundReq)
	if wNotFound.Code != http.StatusNotFound {
		t.Fatalf("second delete: status = %d, want 404", wNotFound.Code)
	}
}

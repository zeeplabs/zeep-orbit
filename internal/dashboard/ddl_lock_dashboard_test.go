package dashboard

// ddl_lock_dashboard_test.go — multi-replica-consistency T11: DDL that the
// dashboard runs itself (RLS mode switch, table policies) goes through the
// per-app DDL lock with a bounded lock wait, and rolls its metadata back
// when the wait times out.
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func shortDashboardDDLLockTimeout(t *testing.T) {
	t.Helper()
	prev := db.DDLLockTimeout
	db.DDLLockTimeout = 300 * time.Millisecond
	t.Cleanup(func() { db.DDLLockTimeout = prev })
}

func TestCreateTablePolicy_LockedTableTimesOutWithoutMetadata(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, userID := tablePoliciesTestApp(t, pool)
	ctx := context.Background()
	shortDashboardDDLLockTimeout(t)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `LOCK TABLE tp_test.requests IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock table: %v", err)
	}

	def := PolicyDef{
		Name:   "select_active_only",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []PolicyClause{
			{Column: "status", Operator: "=", ValueSource: "literal", Value: "active"},
		},
	}
	start := time.Now()
	_, err = CreateTablePolicy(ctx, pool, appID, "tp_test", "requests", requestsColumns(), def, userID)
	if !errors.Is(err, provisioner.ErrDDLLockTimeout) {
		t.Fatalf("expected ErrDDLLockTimeout, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("CreateTablePolicy took %s, expected a bounded wait", elapsed)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}

	policies, err := ListTablePolicies(ctx, pool, appID, "requests")
	if err != nil {
		t.Fatalf("ListTablePolicies: %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("a timed-out policy create must leave no metadata row, got %d", len(policies))
	}
}

// tp_test.requests has no owner_id column: switching it to "policy" mode
// must still succeed (RelaxOwnerColumn's missing-column path), now inside a
// transaction where a failed ALTER would abort everything.
func TestUpdateAppTable_PolicyModeWithoutOwnerColumn(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, _ := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	tbl, err := InsertAppTable(ctx, pool, appID, AppTableRow{Name: "requests", Columns: requestsColumns()})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}
	row, err := UpdateAppTable(ctx, pool, appID, tbl.ID, "tp_test", "policy", requestsColumns(), nil)
	if err != nil {
		t.Fatalf("UpdateAppTable to policy mode: %v", err)
	}
	if row.RLS != "policy" {
		t.Fatalf("rls = %q, want policy", row.RLS)
	}
	if !relRowSecurityEnabled(t, pool, "tp_test", "requests") {
		t.Fatal("RLS should be enabled after switching to policy mode")
	}
}

// A lock timeout on the RLS DDL rolls the metadata change back too, so the
// table is never marked "policy" without RLS enabled.
func TestUpdateAppTable_LockTimeoutRollsBackMetadata(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, _ := tablePoliciesTestApp(t, pool)
	ctx := context.Background()
	shortDashboardDDLLockTimeout(t)

	tbl, err := InsertAppTable(ctx, pool, appID, AppTableRow{Name: "requests", Columns: requestsColumns()})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT * FROM tp_test.requests`); err != nil {
		t.Fatalf("holder select: %v", err)
	}

	_, err = UpdateAppTable(ctx, pool, appID, tbl.ID, "tp_test", "policy", requestsColumns(), nil)
	if !errors.Is(err, provisioner.ErrDDLLockTimeout) {
		t.Fatalf("expected ErrDDLLockTimeout, got: %v", err)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}

	var rls string
	if err := pool.QueryRow(ctx, `SELECT rls FROM zeep_system.app_tables WHERE id = $1`, tbl.ID).Scan(&rls); err != nil {
		t.Fatalf("read rls: %v", err)
	}
	if rls == "policy" {
		t.Fatal("metadata was switched to policy even though the RLS DDL timed out")
	}
}

// TestUpdateAppTableHandler_LockTimeoutReturns409 (T12): PUT on a table held
// by a long-running reader answers 409 with the busy message, leaves the
// stored columns untouched, and logs the lock holders without query text,
// role name or client address.
func TestUpdateAppTableHandler_LockTimeoutReturns409(t *testing.T) {
	pool, _, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	core, observed := observer.New(zap.WarnLevel)
	h := NewHandler(pool, registry.New(), zap.New(core))

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "lock409")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	tbl, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "items", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	shortDashboardDDLLockTimeout(t)

	reader, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("reader begin: %v", err)
	}
	defer reader.Rollback(ctx)
	if _, err := reader.Exec(ctx, fmt.Sprintf(`SELECT * FROM %q.items`, schemaNameForDB(app.Name))); err != nil {
		t.Fatalf("reader select: %v", err)
	}

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, tbl.ID, TableRequestBody{
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}, {Name: "price", Type: "integer"}},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != provisioner.ErrDDLLockTimeout.Error() {
		t.Fatalf("error = %q, want %q", body["error"], provisioner.ErrDDLLockTimeout.Error())
	}
	if err := reader.Rollback(ctx); err != nil {
		t.Fatalf("reader rollback: %v", err)
	}

	var cols []config.ColumnConfig
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT columns FROM zeep_system.app_tables WHERE id = $1`, tbl.ID).Scan(&raw); err != nil {
		t.Fatalf("read columns: %v", err)
	}
	if err := json.Unmarshal(raw, &cols); err != nil {
		t.Fatalf("decode columns: %v", err)
	}
	if len(cols) != 1 {
		t.Fatalf("stored columns changed on a 409: %+v", cols)
	}

	entries := observed.FilterMessage("ddl lock timeout: lock held by other sessions").All()
	if len(entries) != 1 {
		t.Fatalf("expected one holders log entry, got %d (all: %v)", len(entries), observed.All())
	}
	logged, err := json.Marshal(entries[0].ContextMap())
	if err != nil {
		t.Fatalf("marshal log context: %v", err)
	}
	for _, want := range []string{`"pid"`, `"state"`, `"mode"`} {
		if !strings.Contains(string(logged), want) {
			t.Fatalf("holders log is missing %s: %s", want, logged)
		}
	}
	for _, forbidden := range []string{"query", "usename", "client_addr", "SELECT"} {
		if strings.Contains(string(logged), forbidden) {
			t.Fatalf("holders log must not contain %q: %s", forbidden, logged)
		}
	}
}

func callDeleteAppTable(t *testing.T, h *Handler, actor *DashboardUser, appID, tableID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/dashboard/api/apps/%s/tables/%s", appID, tableID), nil)
	req = withUser(req, actor)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	rctx.URLParams.Add("tableId", tableID)
	req = req.WithContext(withCtx(req, rctx))
	w := httptest.NewRecorder()
	h.DeleteAppTable(w, req)
	return w
}

// TestDeleteAppTableHandler_LockTimeoutKeepsTable: a DELETE whose DROP times
// out behind a long reader answers 409 and leaves both the metadata row and
// the physical table in place, so the retry (after the reader is gone)
// succeeds. Before the fix the metadata was committed away first, the retry
// got 404 and the physical table was orphaned.
func TestDeleteAppTableHandler_LockTimeoutKeepsTable(t *testing.T) {
	pool, _, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	h := NewHandler(pool, registry.New(), zap.NewNop())

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "del409")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	tbl, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "items", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	schema := schemaNameForDB(app.Name)
	shortDashboardDDLLockTimeout(t)

	reader, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("reader begin: %v", err)
	}
	defer reader.Rollback(ctx)
	if _, err := reader.Exec(ctx, fmt.Sprintf(`SELECT * FROM %q.items`, schema)); err != nil {
		t.Fatalf("reader select: %v", err)
	}

	if w := callDeleteAppTable(t, h, actors["loner"], app.ID, tbl.ID); w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", w.Code, w.Body.String())
	}
	if err := reader.Rollback(ctx); err != nil {
		t.Fatalf("reader rollback: %v", err)
	}

	var metaRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.app_tables WHERE id = $1`, tbl.ID).Scan(&metaRows); err != nil {
		t.Fatalf("count metadata: %v", err)
	}
	if metaRows != 1 {
		t.Fatalf("metadata row was deleted although the DROP timed out")
	}
	if !physicalTableExists(t, pool, schema, "items") {
		t.Fatal("physical table should still exist after a 409")
	}

	if w := callDeleteAppTable(t, h, actors["loner"], app.ID, tbl.ID); w.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if physicalTableExists(t, pool, schema, "items") {
		t.Fatal("physical table should be gone after the retry")
	}
}

func physicalTableExists(t *testing.T, pool *db.Pool, schema, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass(format('%I.%I', $1::text, $2::text)) IS NOT NULL`, schema, table,
	).Scan(&exists); err != nil {
		t.Fatalf("check table: %v", err)
	}
	return exists
}

// TestUpdateTableRLSModeForUser_LockTimeoutKeepsSentinel: the *ForUser layer
// wraps store errors with errAppTableInternalFailed. The lock timeout must
// stay in the chain, or REST answers 500 and MCP a generic error instead of
// the busy message.
func TestUpdateTableRLSModeForUser_LockTimeoutKeepsSentinel(t *testing.T) {
	pool, _, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	h := NewHandler(pool, registry.New(), zap.NewNop())

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "rls409"), AuthEmailEnabled: true}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "items", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	shortDashboardDDLLockTimeout(t)

	// Hold the state_version row: Apply (DDL only) goes through, and the
	// metadata UPDATE in UpdateAppTable times out in its trigger, past the
	// point where the *ForUser layer wraps the error.
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `UPDATE zeep_system.state_version SET changed_at = now() WHERE id = 1`); err != nil {
		t.Fatalf("holder update: %v", err)
	}

	_, err = h.UpdateTableRLSModeForUser(ctx, actors["loner"], app.ID, "items", "policy", "127.0.0.1")
	if !errors.Is(err, provisioner.ErrDDLLockTimeout) {
		t.Fatalf("expected ErrDDLLockTimeout in the chain, got: %v", err)
	}
	if !errors.Is(err, errAppTableInternalFailed) {
		t.Fatalf("the internal-failure sentinel should still match, got: %v", err)
	}
}

// TestUpdateAppTable_PolicyTableWithRLSOnSkipsLock: once RLS is on, a
// metadata edit on a "policy" table runs no ALTER, so it doesn't wait behind
// an open reader on that table.
func TestUpdateAppTable_PolicyTableWithRLSOnSkipsLock(t *testing.T) {
	pool := tablePoliciesTestPool(t)
	appID, _ := tablePoliciesTestApp(t, pool)
	ctx := context.Background()

	tbl, err := InsertAppTable(ctx, pool, appID, AppTableRow{Name: "requests", Columns: requestsColumns()})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}
	if _, err := UpdateAppTable(ctx, pool, appID, tbl.ID, "tp_test", "policy", requestsColumns(), nil); err != nil {
		t.Fatalf("first UpdateAppTable: %v", err)
	}
	shortDashboardDDLLockTimeout(t)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT * FROM tp_test.requests`); err != nil {
		t.Fatalf("holder select: %v", err)
	}

	if _, err := UpdateAppTable(ctx, pool, appID, tbl.ID, "tp_test", "policy", requestsColumns(), nil); err != nil {
		t.Fatalf("UpdateAppTable on a policy table with RLS already on should not wait for a table lock: %v", err)
	}
}

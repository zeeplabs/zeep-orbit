package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// createAppWithStatusEnum is shared setup: an app with an assets table whose
// status column is an enum over pending/active/closed.
func createAppWithStatusEnum(t *testing.T, ctx context.Context, h *Handler, actors map[string]*DashboardUser, appName string) (*AppRow, *AppTableRow) {
	t.Helper()
	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: appName}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "assets",
		Columns: []config.ColumnConfig{
			{Name: "name", Type: "text"},
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active", "closed"}},
		},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	return app, table
}

// CENUM-07 / CENUM-08: widening succeeds, existing rows are unaffected, the
// new value becomes writable, and the new set is persisted to the stored
// column config.
func TestUpdateColumnEnumValuesForUser_WidenSucceeds(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "enumvals-widen"))
	schema := schemaNameForDB(app.Name)
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('active')"); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	updated, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "status",
		[]string{"pending", "active", "closed", "archived"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("UpdateColumnEnumValuesForUser: %v", err)
	}

	// Persisted to the stored config.
	var stored []string
	for _, c := range updated.Columns {
		if c.Name == "status" {
			stored = c.AllowedValues
		}
	}
	if len(stored) != 4 || stored[3] != "archived" {
		t.Errorf("persisted AllowedValues = %v, want the 4-value widened set", stored)
	}

	// Enforced in Postgres: the new value is writable, an out-of-set one is not.
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('archived')"); err != nil {
		t.Errorf("expected the newly allowed value to be writable, got: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('qualquer coisa')"); err == nil {
		t.Error("expected an out-of-set value to still be rejected after widening")
	}
	// Existing row untouched.
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+schema+".assets WHERE status = 'active'").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("existing row count = %d, want 1 (widening must not touch data)", count)
	}
}

// CENUM-09 / CENUM-11: narrowing with no rows on the removed value succeeds
// and the removed value becomes unwritable.
func TestUpdateColumnEnumValuesForUser_NarrowWithNoRowsSucceeds(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "enumvals-narrow-ok"))
	schema := schemaNameForDB(app.Name)
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('active')"); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	updated, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "status",
		[]string{"pending", "active"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("UpdateColumnEnumValuesForUser: %v", err)
	}

	for _, c := range updated.Columns {
		if c.Name == "status" && len(c.AllowedValues) != 2 {
			t.Errorf("persisted AllowedValues = %v, want the 2-value narrowed set", c.AllowedValues)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('closed')"); err == nil {
		t.Error("expected the removed value to be rejected after narrowing")
	}
}

// CENUM-09 / CENUM-10 / CENUM-12: narrowing away an in-use value is rejected
// with the typed error naming the value and its count; nothing is persisted
// and the physical constraint is untouched.
func TestUpdateColumnEnumValuesForUser_NarrowRejectedForInUseValue(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "ev-inuse"))
	schema := schemaNameForDB(app.Name)
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('closed')"); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	constraintBefore := checkConstraintDefForColumn(t, pool, schema, "assets", "status")

	_, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "status",
		[]string{"pending", "active"}, "127.0.0.1")
	var inUse *provisioner.EnumValueInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("expected *provisioner.EnumValueInUseError, got %v (%T)", err, err)
	}
	if got, want := inUse.Counts["closed"], 1; got != want {
		t.Errorf(`Counts["closed"] = %d, want %d`, got, want)
	}
	if !strings.Contains(err.Error(), `"closed" is used by 1 row(s)`) {
		t.Errorf("error must name the value and count, got: %q", err.Error())
	}
	// No raw Postgres internals leak into the public message (AGENTS.md §4).
	if strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "check constraint") {
		t.Errorf("error leaks Postgres internals: %q", err.Error())
	}

	// Stored config untouched.
	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, "assets")
	for _, c := range refreshedTable.Columns {
		if c.Name == "status" && len(c.AllowedValues) != 3 {
			t.Errorf("expected stored AllowedValues untouched (3 values), got %v", c.AllowedValues)
		}
	}
	// Physical constraint untouched.
	if after := checkConstraintDefForColumn(t, pool, schema, "assets", "status"); after != constraintBefore {
		t.Errorf("constraint changed on a rejected narrow:\n before = %q\n after  = %q", constraintBefore, after)
	}
}

// A non-enum column is rejected: allowed values are only meaningful for
// type "enum", and this feature does not convert a column to enum.
func TestUpdateColumnEnumValuesForUser_NonEnumColumnRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "enumvals-nonenum"))

	_, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "name",
		[]string{"a", "b"}, "127.0.0.1")
	if !errors.Is(err, ErrColumnIsNotEnum) {
		t.Fatalf("expected ErrColumnIsNotEnum for a text column, got %v", err)
	}
}

// An unknown table or column is not found.
func TestUpdateColumnEnumValuesForUser_UnknownTableOrColumnNotFound(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "enumvals-404"))

	if _, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, "does_not_exist", "status",
		[]string{"pending"}, "127.0.0.1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for an unknown table, got %v", err)
	}
	if _, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "does_not_exist",
		[]string{"pending"}, "127.0.0.1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for an unknown column, got %v", err)
	}
}

// A viewer cannot mutate the schema: CanWrite() gate, no change made.
func TestUpdateColumnEnumValuesForUser_ViewerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.UpdateColumnEnumValuesForUser(ctx, actors["appviewer"], appID, "test_table", "status",
		[]string{"pending"}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a viewer (CanWrite()==false), got %v", err)
	}
}

// CENUM-04: the same caps the creation path enforces apply here — a widen or
// narrow cannot produce an empty or duplicate-containing set.
func TestUpdateColumnEnumValuesForUser_InvalidValuesRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "enumvals-invalid"))

	for _, tc := range []struct {
		name   string
		values []string
	}{
		{"empty list", []string{}},
		{"duplicate value", []string{"pending", "pending"}},
		{"empty entry", []string{"pending", ""}},
	} {
		_, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "status", tc.values, "127.0.0.1")
		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Errorf("%s: expected *ValidationError, got %v (%T)", tc.name, err, err)
		}
	}
}

// CENUM-05, applied to the widen/narrow path: a narrowing that drops the
// column's own default is rejected before any DDL runs.
func TestUpdateColumnEnumValuesForUser_NarrowDroppingDefaultRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "enumvals-default")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "assets",
		Columns: []config.ColumnConfig{{
			Name: "status", Type: "enum",
			AllowedValues: []string{"pending", "active"},
			Default:       "pending",
		}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	_, err = h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "status",
		[]string{"active"}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError when the new set drops the column default, got %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), "pending") {
		t.Errorf("error should name the offending default, got: %q", err.Error())
	}
}

// A successful call writes an audit log entry.
func TestUpdateColumnEnumValuesForUser_RecordsAuditLog(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "enumvals-audit"))

	if _, err := h.UpdateColumnEnumValuesForUser(ctx, actors["loner"], app.ID, table.Name, "status",
		[]string{"pending", "active", "closed", "archived"}, "127.0.0.1"); err != nil {
		t.Fatalf("UpdateColumnEnumValuesForUser: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'app.table.column.enum_values.update' AND resource_id = $1`,
		table.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 audit log entry for app.table.column.enum_values.update, got %d", count)
	}
}

// callUpdateColumnEnumValues issues a PATCH
// /apps/{appID}/tables/{tableID}/columns/{columnName}/enum-values request
// directly against h.UpdateColumnEnumValues, mirroring callUpdateAppTable's
// request construction for the sibling route.
func callUpdateColumnEnumValues(t *testing.T, h *Handler, actor *DashboardUser, appID, tableID, columnName string, values []string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(map[string][]string{"allowed_values": values})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/dashboard/api/apps/%s/tables/%s/columns/%s/enum-values", appID, tableID, columnName),
		bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, actor)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	rctx.URLParams.Add("tableId", tableID)
	rctx.URLParams.Add("columnName", columnName)
	req = req.WithContext(withCtx(req, rctx))

	w := httptest.NewRecorder()
	h.UpdateColumnEnumValues(w, req)
	return w
}

// The route's HTTP contract: 200 on a successful widen, 400 for a narrowing
// rejection (with the safe message, not a raw Postgres error), 400 for a
// non-enum column, 404 for an unknown table, 403 for a viewer.
func TestUpdateColumnEnumValues_RouteStatusCodes(t *testing.T) {
	pool, h, actors, seedAppID, seedTableID := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, table := createAppWithStatusEnum(t, ctx, h, actors, uniqueAppName(t, "ev-route"))
	schema := schemaNameForDB(app.Name)

	// Happy path: widen.
	w := callUpdateColumnEnumValues(t, h, actors["loner"], app.ID, table.ID, "status",
		[]string{"pending", "active", "closed", "archived"})
	if w.Code != http.StatusOK {
		t.Fatalf("widen: expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Narrowing rejection: 400 with the safe, value-naming message.
	if _, err := pool.Exec(ctx, "INSERT INTO "+schema+".assets (status) VALUES ('archived')"); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	w = callUpdateColumnEnumValues(t, h, actors["loner"], app.ID, table.ID, "status",
		[]string{"pending", "active", "closed"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("in-use narrow: expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `used by 1 row(s)`) {
		t.Errorf("in-use narrow: expected the value/count in the response, got: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SQLSTATE") {
		t.Errorf("in-use narrow: response leaks Postgres internals: %s", w.Body.String())
	}

	// Non-enum column: 400.
	w = callUpdateColumnEnumValues(t, h, actors["loner"], app.ID, table.ID, "name", []string{"a", "b"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("non-enum column: expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Unknown table: 404.
	w = callUpdateColumnEnumValues(t, h, actors["loner"], app.ID, "00000000-0000-0000-0000-000000000000", "status", []string{"pending"})
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown table: expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Unknown column: 404.
	w = callUpdateColumnEnumValues(t, h, actors["loner"], app.ID, table.ID, "does_not_exist", []string{"pending"})
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown column: expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Viewer: 403. Uses the seeded app, where appviewer actually holds the
	// viewer role — on an app they are not a member of at all the correct
	// answer is 404 (existence is not disclosed), which is a different case.
	w = callUpdateColumnEnumValues(t, h, actors["appviewer"], seedAppID, seedTableID, "status", []string{"pending"})
	if w.Code != http.StatusForbidden {
		t.Errorf("viewer: expected 403, got %d (body: %s)", w.Code, w.Body.String())
	}

	// A non-member gets 404, not 403 — the app's existence is not disclosed.
	w = callUpdateColumnEnumValues(t, h, actors["outsider"], app.ID, table.ID, "status", []string{"pending"})
	if w.Code != http.StatusNotFound {
		t.Errorf("outsider: expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}
}

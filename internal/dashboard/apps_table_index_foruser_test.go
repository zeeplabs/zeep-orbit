package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// TestAddTableIndexForUser_AddsIndexLeavesColumnsAndOtherIndexesUnchanged is
// the concrete regression test for the orphaning risk, applied to indexes.
func TestAddTableIndexForUser_AddsIndexLeavesColumnsAndOtherIndexesUnchanged(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "idx-add-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name: "items",
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text"},
			{Name: "sku", Type: "text"},
		},
		Indexes: []config.IndexConfig{{Name: "items_title_idx", Columns: []string{"title"}}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	updated, err := h.AddTableIndexForUser(ctx, actors["loner"], app.ID, table.Name, config.IndexConfig{
		Name: "items_sku_idx", Columns: []string{"sku"}, Unique: true,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("AddTableIndexForUser: %v", err)
	}
	if len(updated.Indexes) != 2 {
		t.Fatalf("expected 2 indexes after add, got %d: %+v", len(updated.Indexes), updated.Indexes)
	}
	if len(updated.Columns) != 2 {
		t.Fatalf("expected columns unchanged (2), got %d: %+v", len(updated.Columns), updated.Columns)
	}
	if updated.Indexes[0].Name != "items_title_idx" {
		t.Fatalf("original index mutated: %+v", updated.Indexes[0])
	}
	if updated.Indexes[1].Name != "items_sku_idx" || !updated.Indexes[1].Unique {
		t.Fatalf("new index not persisted correctly: %+v", updated.Indexes[1])
	}
}

// TestAddTableIndexForUser_DuplicateNameRejected covers spec P2 AC2.
func TestAddTableIndexForUser_DuplicateNameRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "idx-dup-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
		Indexes: []config.IndexConfig{{Name: "items_title_idx", Columns: []string{"title"}}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	_, err = h.AddTableIndexForUser(ctx, actors["loner"], app.ID, table.Name, config.IndexConfig{
		Name: "items_title_idx", Columns: []string{"title"},
	}, "127.0.0.1")
	if !errors.Is(err, ErrIndexAlreadyExists) {
		t.Fatalf("expected ErrIndexAlreadyExists, got %v", err)
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, table.Name)
	if refreshedTable == nil || len(refreshedTable.Indexes) != 1 {
		t.Fatalf("expected table untouched with 1 index, got %+v", refreshedTable)
	}
}

// TestAddTableIndexForUser_UnknownColumnRejected covers spec P2 AC3.
func TestAddTableIndexForUser_UnknownColumnRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "idx-badcol-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	_, err = h.AddTableIndexForUser(ctx, actors["loner"], app.ID, table.Name, config.IndexConfig{
		Name: "items_bad_idx", Columns: []string{"no_such_column"},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for an index on a nonexistent column, got %v (%T)", err, err)
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, table.Name)
	if refreshedTable == nil || len(refreshedTable.Indexes) != 0 {
		t.Fatalf("expected table untouched with 0 indexes, got %+v", refreshedTable)
	}
}

// TestAddTableIndexForUser_SuperadminBypassesMembership covers the spec's
// Edge Cases line: a superadmin with no app_members row still succeeds
// (ResolveAppRole's superadmin bypass grants AppRoleAdmin, satisfying
// CanWrite()) — no additional restriction from the MCP layer.
func TestAddTableIndexForUser_SuperadminBypassesMembership(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "idx-super-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	updated, err := h.AddTableIndexForUser(ctx, actors["super"], app.ID, table.Name, config.IndexConfig{
		Name: "items_title_idx", Columns: []string{"title"},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("AddTableIndexForUser as superadmin with no app_members row: %v", err)
	}
	if len(updated.Indexes) != 1 {
		t.Fatalf("expected 1 index after superadmin add, got %d: %+v", len(updated.Indexes), updated.Indexes)
	}
}

// TestAddTableIndexForUser_ViewerForbidden covers spec P2 AC4.
func TestAddTableIndexForUser_ViewerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.AddTableIndexForUser(ctx, actors["appviewer"], appID, "test_table", config.IndexConfig{
		Name: "new_idx", Columns: []string{"id"},
	}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a viewer (CanWrite()==false), got %v", err)
	}
}

// TestAddTableIndexForUser_UnknownTableNotFound covers spec P2 AC4's
// not-found branch.
func TestAddTableIndexForUser_UnknownTableNotFound(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.AddTableIndexForUser(ctx, actors["loner"], appID, "no-such-table", config.IndexConfig{
		Name: "new_idx", Columns: []string{"id"},
	}, "127.0.0.1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAddTableIndexForUser_RecordsAudit covers spec P2 AC5.
func TestAddTableIndexForUser_RecordsAudit(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "idx-audit-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	if _, err := h.AddTableIndexForUser(ctx, actors["loner"], app.ID, table.Name, config.IndexConfig{
		Name: "items_title_idx", Columns: []string{"title"},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("AddTableIndexForUser: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'app.table_index.create' AND resource_id = $1`,
		table.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit_log row for app.table_index.create, got %d", count)
	}
}

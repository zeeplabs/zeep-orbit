package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// TestAddTableColumnForUser_AddsColumnLeavesOthersUnchanged is the concrete
// regression test for the orphaned-column risk that motivated
// mcp-safe-mutation-tools (spec.md Success Criteria): adding a second column
// must leave the first one, plus indexes/RLS, byte-for-byte unchanged.
func TestAddTableColumnForUser_AddsColumnLeavesOthersUnchanged(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "col-add-app"}, "127.0.0.1")
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

	updated, err := h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "price", Type: "integer",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("AddTableColumnForUser: %v", err)
	}
	if len(updated.Columns) != 2 {
		t.Fatalf("expected 2 columns after add, got %d: %+v", len(updated.Columns), updated.Columns)
	}
	if updated.Columns[0].Name != "title" || updated.Columns[0].Type != "text" {
		t.Fatalf("original column mutated: %+v", updated.Columns[0])
	}
	if updated.Columns[1].Name != "price" || updated.Columns[1].Type != "integer" {
		t.Fatalf("new column not persisted correctly: %+v", updated.Columns[1])
	}

	// Add a second new column and confirm both prior columns survive — the
	// exact two-calls-in-a-row regression the spec's Success Criteria names.
	twice, err := h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "sku", Type: "text",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("second AddTableColumnForUser: %v", err)
	}
	if len(twice.Columns) != 3 {
		t.Fatalf("expected 3 columns after 2 adds, got %d: %+v", len(twice.Columns), twice.Columns)
	}
}

// TestAddTableColumnForUser_WithValidReference covers adding a column with a
// foreign-key reference to another existing table (spec P1 AC2).
func TestAddTableColumnForUser_WithValidReference(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "col-fk-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "categories",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text", Unique: true}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser categories: %v", err)
	}
	items, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	updated, err := h.AddTableColumnForUser(ctx, actors["loner"], app.ID, items.Name, config.ColumnConfig{
		Name: "category_id", Type: "uuid",
		References: &config.ReferenceConfig{Table: "categories", Column: "id"},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("AddTableColumnForUser with reference: %v", err)
	}
	found := false
	for _, c := range updated.Columns {
		if c.Name == "category_id" {
			found = true
			if c.References == nil || c.References.Table != "categories" {
				t.Fatalf("expected reference to categories, got %+v", c.References)
			}
		}
	}
	if !found {
		t.Fatalf("category_id column not found in %+v", updated.Columns)
	}
}

// TestAddTableColumnForUser_DuplicateNameRejected covers spec P1 AC3.
func TestAddTableColumnForUser_DuplicateNameRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "col-dup-app"}, "127.0.0.1")
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

	_, err = h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "title", Type: "integer",
	}, "127.0.0.1")
	if !errors.Is(err, ErrColumnAlreadyExists) {
		t.Fatalf("expected ErrColumnAlreadyExists, got %v", err)
	}

	// Table must be left untouched.
	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, table.Name)
	if refreshedTable == nil || len(refreshedTable.Columns) != 1 {
		t.Fatalf("expected table untouched with 1 column, got %+v", refreshedTable)
	}
}

// TestAddTableColumnForUser_BadReferenceRejected covers spec P1 AC4.
func TestAddTableColumnForUser_BadReferenceRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "col-badfk-app"}, "127.0.0.1")
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

	_, err = h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "category_id", Type: "uuid",
		References: &config.ReferenceConfig{Table: "no_such_table", Column: "id"},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for a nonexistent reference target, got %v (%T)", err, err)
	}

	refreshed, _, err := GetApp(ctx, pool, app.ID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	refreshedTable := findAppTableByName(refreshed, table.Name)
	if refreshedTable == nil || len(refreshedTable.Columns) != 1 {
		t.Fatalf("expected table untouched with 1 column, got %+v", refreshedTable)
	}
}

// TestAddTableColumnForUser_ViewerForbidden covers spec P1 AC5 — CanWrite()
// gate, tested with an explicit viewer role (not just "no membership").
func TestAddTableColumnForUser_ViewerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.AddTableColumnForUser(ctx, actors["appviewer"], appID, "test_table", config.ColumnConfig{
		Name: "new_col", Type: "text",
	}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a viewer (CanWrite()==false), got %v", err)
	}
}

// TestAddTableColumnForUser_UnknownTableNotFound covers spec P1 AC5's
// not-found branch.
func TestAddTableColumnForUser_UnknownTableNotFound(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.AddTableColumnForUser(ctx, actors["loner"], appID, "no-such-table", config.ColumnConfig{
		Name: "new_col", Type: "text",
	}, "127.0.0.1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAddTableColumnForUser_RecordsAudit covers spec P1 AC6.
func TestAddTableColumnForUser_RecordsAudit(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "col-audit-app"}, "127.0.0.1")
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

	if _, err := h.AddTableColumnForUser(ctx, actors["loner"], app.ID, table.Name, config.ColumnConfig{
		Name: "price", Type: "integer",
	}, "127.0.0.1"); err != nil {
		t.Fatalf("AddTableColumnForUser: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'app.table_column.create' AND resource_id = $1`,
		table.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit_log row for app.table_column.create, got %d", count)
	}
}

package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestAddColumnForeignKeyForUser_Success covers spec P1 "Add FK" AC1: a
// valid add on an existing column runs the DDL and persists References
// only after it succeeds.
func TestAddColumnForeignKeyForUser_Success(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "addfk-app"}, "127.0.0.1")
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
		Columns: []config.ColumnConfig{{Name: "category_id", Type: "uuid"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	updated, err := h.AddColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id",
		config.ReferenceConfig{Table: "categories", Column: "id"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("AddColumnForeignKeyForUser: %v", err)
	}

	found := false
	for _, c := range updated.Columns {
		if c.Name == "category_id" {
			found = true
			if c.References == nil || c.References.Table != "categories" || c.References.Column != "id" {
				t.Fatalf("expected persisted reference to categories.id, got %+v", c.References)
			}
		}
	}
	if !found {
		t.Fatalf("category_id column not found in %+v", updated.Columns)
	}
}

// TestAddColumnForeignKeyForUser_AlreadyHasReferenceRejected covers spec P1
// "Add FK" AC2.
func TestAddColumnForeignKeyForUser_AlreadyHasReferenceRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "addfk-dup-app"}, "127.0.0.1")
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
		Name: "items",
		Columns: []config.ColumnConfig{{
			Name: "category_id", Type: "uuid",
			References: &config.ReferenceConfig{Table: "categories", Column: "id"},
		}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	_, err = h.AddColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id",
		config.ReferenceConfig{Table: "categories", Column: "id"}, "127.0.0.1")
	if !errors.Is(err, ErrColumnAlreadyHasReference) {
		t.Fatalf("expected ErrColumnAlreadyHasReference, got %v", err)
	}
}

// TestAddColumnForeignKeyForUser_InvalidTargetRejected covers spec P1 "Add
// FK" AC3 — reuses config.ValidateTables' existing behavior for an
// unknown target table.
func TestAddColumnForeignKeyForUser_InvalidTargetRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "addfk-badtarget-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	items, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "category_id", Type: "uuid"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	_, err = h.AddColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id",
		config.ReferenceConfig{Table: "does_not_exist", Column: "id"}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for an unknown target table, got %v (%T)", err, err)
	}
}

// TestAddColumnForeignKeyForUser_TypeMismatchRejected covers spec P1 "Add
// FK" AC5 — the physical-type check (T3) rejects a real type mismatch with
// a *ValidationError before any DDL runs.
func TestAddColumnForeignKeyForUser_TypeMismatchRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "addfk-typemismatch-app"}, "127.0.0.1")
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
		Columns: []config.ColumnConfig{{Name: "category_ref", Type: "text", Unique: true}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	_, err = h.AddColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_ref",
		config.ReferenceConfig{Table: "categories", Column: "id"}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for a physical type mismatch (text vs uuid), got %v (%T)", err, err)
	}
}

// TestAddColumnForeignKeyForUser_OrphanedRowsRejected covers spec P1 "Add
// FK" AC6 — a *provisioner.ForeignKeyViolationError propagates when
// existing rows violate the new constraint.
func TestAddColumnForeignKeyForUser_OrphanedRowsRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "addfk-orphan-app"}, "127.0.0.1")
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
		Columns: []config.ColumnConfig{{Name: "category_id", Type: "uuid"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	orphanID := "11111111-1111-1111-1111-111111111111"
	if _, err := pool.Exec(ctx,
		"INSERT INTO "+schemaNameForDB(app.Name)+".items (category_id) VALUES ($1)", orphanID,
	); err != nil {
		t.Fatalf("seed orphan row: %v", err)
	}

	_, err = h.AddColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id",
		config.ReferenceConfig{Table: "categories", Column: "id"}, "127.0.0.1")
	var fkErr *provisioner.ForeignKeyViolationError
	if !errors.As(err, &fkErr) {
		t.Fatalf("expected *provisioner.ForeignKeyViolationError, got %v (%T)", err, err)
	}
}

// TestAddColumnForeignKeyForUser_ViewerForbidden covers spec P1 "Add FK"
// AC7 — CanWrite() gate, making no schema change.
func TestAddColumnForeignKeyForUser_ViewerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.AddColumnForeignKeyForUser(ctx, actors["appviewer"], appID, "test_table", "some_col",
		config.ReferenceConfig{Table: "other_table", Column: "id"}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a viewer (CanWrite()==false), got %v", err)
	}
}

// TestAddColumnForeignKeyForUser_RecordsAuditLog covers spec P1 "Add FK"
// AC8.
func TestAddColumnForeignKeyForUser_RecordsAuditLog(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: "addfk-audit-app"}, "127.0.0.1")
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
		Columns: []config.ColumnConfig{{Name: "category_id", Type: "uuid"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}

	if _, err := h.AddColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id",
		config.ReferenceConfig{Table: "categories", Column: "id"}, "127.0.0.1"); err != nil {
		t.Fatalf("AddColumnForeignKeyForUser: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'app.table_column.add_foreign_key' AND resource_id = $1`,
		items.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 audit log entry for app.table_column.add_foreign_key, got %d", count)
	}
}

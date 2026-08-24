package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestAddColumnForeignKeyForUser_Success covers spec P1 "Add FK" AC1: a
// valid add on an existing column runs the DDL and persists References
// only after it succeeds.
func TestAddColumnForeignKeyForUser_Success(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-app")}, "127.0.0.1")
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

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-dup-app")}, "127.0.0.1")
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

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-badtgt-app")}, "127.0.0.1")
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

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-typemis-app")}, "127.0.0.1")
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

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-orphan-app")}, "127.0.0.1")
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

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "addfk-audit-app")}, "127.0.0.1")
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

// hasFKConstraintOnColumn is the dashboard-package counterpart of
// provisioner's hasFKConstraint test helper — checks information_schema
// directly rather than assuming a naming convention.
func hasFKConstraintOnColumn(t *testing.T, pool *db.Pool, schema, tableName, columnName string) bool {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*)
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY'
		   AND tc.table_schema = $1 AND tc.table_name = $2 AND kcu.column_name = $3`,
		schema, tableName, columnName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check FK constraint on %s.%s.%s: %v", schema, tableName, columnName, err)
	}
	return count > 0
}

// createItemsWithFK is shared setup for the Remove-FK tests: an app with a
// categories table and an items table whose category_id column already has
// a foreign key to categories.id.
func createItemsWithFK(t *testing.T, ctx context.Context, h *Handler, actors map[string]*DashboardUser, appName string) (*AppRow, *AppTableRow) {
	t.Helper()
	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: appName}, "127.0.0.1")
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
	return app, items
}

// TestRemoveColumnForeignKeyForUser_Success covers spec P1 "Remove FK" AC1:
// the constraint no longer exists in Postgres and References is cleared in
// the stored schema.
func TestRemoveColumnForeignKeyForUser_Success(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, items := createItemsWithFK(t, ctx, h, actors, uniqueAppName(t, "removefk-app"))

	updated, err := h.RemoveColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id", "127.0.0.1")
	if err != nil {
		t.Fatalf("RemoveColumnForeignKeyForUser: %v", err)
	}

	for _, c := range updated.Columns {
		if c.Name == "category_id" && c.References != nil {
			t.Fatalf("expected References cleared, got %+v", c.References)
		}
	}
	if hasFKConstraintOnColumn(t, pool, schemaNameForDB(app.Name), "items", "category_id") {
		t.Fatal("expected no FK constraint on items.category_id after remove")
	}
}

// TestRemoveColumnForeignKeyForUser_NoReferenceRejected covers spec P1
// "Remove FK" AC2.
func TestRemoveColumnForeignKeyForUser_NoReferenceRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "removefk-noref-app")}, "127.0.0.1")
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

	_, err = h.RemoveColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id", "127.0.0.1")
	if !errors.Is(err, ErrColumnHasNoReference) {
		t.Fatalf("expected ErrColumnHasNoReference, got %v", err)
	}
}

// TestRemoveColumnForeignKeyForUser_ViewerForbidden covers spec P1 "Remove
// FK" AC3 — CanWrite() gate, making no schema change.
func TestRemoveColumnForeignKeyForUser_ViewerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.RemoveColumnForeignKeyForUser(ctx, actors["appviewer"], appID, "test_table", "some_col", "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a viewer (CanWrite()==false), got %v", err)
	}
}

// TestRemoveColumnForeignKeyForUser_RecordsAuditLog covers spec P1 "Remove
// FK" AC4.
func TestRemoveColumnForeignKeyForUser_RecordsAuditLog(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, items := createItemsWithFK(t, ctx, h, actors, uniqueAppName(t, "removefk-audit-app"))

	if _, err := h.RemoveColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id", "127.0.0.1"); err != nil {
		t.Fatalf("RemoveColumnForeignKeyForUser: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'app.table_column.remove_foreign_key' AND resource_id = $1`,
		items.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 audit log entry for app.table_column.remove_foreign_key, got %d", count)
	}
}

// TestRemoveColumnForeignKeyForUser_StaleSchemaConverges covers the Edge
// Case: the stored schema says References is set, but the real Postgres
// constraint was already dropped outside the platform (DropColumnForeignKey
// returns found=false). The handler still succeeds and clears the stale
// stored References instead of erroring.
func TestRemoveColumnForeignKeyForUser_StaleSchemaConverges(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, items := createItemsWithFK(t, ctx, h, actors, uniqueAppName(t, "removefk-stale-app"))
	schemaName := schemaNameForDB(app.Name)

	// Simulate drift: drop the real constraint directly, bypassing the
	// platform, while the stored schema still lists References.
	var constraintName string
	if err := pool.QueryRow(ctx,
		`SELECT tc.constraint_name
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = $1 AND tc.table_name = 'items' AND kcu.column_name = 'category_id'`,
		schemaName,
	).Scan(&constraintName); err != nil {
		t.Fatalf("find constraint to simulate drift: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE `+schemaName+`.items DROP CONSTRAINT `+constraintName); err != nil {
		t.Fatalf("drop constraint to simulate drift: %v", err)
	}

	updated, err := h.RemoveColumnForeignKeyForUser(ctx, actors["loner"], app.ID, items.Name, "category_id", "127.0.0.1")
	if err != nil {
		t.Fatalf("expected stale-schema convergence to succeed, got error: %v", err)
	}
	for _, c := range updated.Columns {
		if c.Name == "category_id" && c.References != nil {
			t.Fatalf("expected stale References to be cleared, got %+v", c.References)
		}
	}
}

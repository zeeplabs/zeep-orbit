package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// TestCreateAppTableForUser_HappyPath covers T5's Done-when: calling
// CreateAppTableForUser directly (no HTTP layer) creates a table with
// columns exactly as specified, produces the pre-extraction audit_log row
// shape (mcp-server spec MCP-07/MCP-10).
func TestCreateAppTableForUser_HappyPath(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	row, err := h.CreateAppTableForUser(ctx, actors["loner"], appID, TableRequestBody{
		Name:    "widgets",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}, {Name: "price", Type: "integer"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	if row.Name != "widgets" {
		t.Fatalf("expected table name %q, got %q", "widgets", row.Name)
	}
	if len(row.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d: %+v", len(row.Columns), row.Columns)
	}

	var action, resourceType string
	if err := pool.QueryRow(ctx,
		`SELECT action, resource_type FROM zeep_system.audit_log WHERE resource_type = 'app_table' AND resource_id = $1`,
		row.ID,
	).Scan(&action, &resourceType); err != nil {
		t.Fatalf("expected an audit_log row for the created table: %v", err)
	}
	if action != "app.table.create" {
		t.Fatalf("expected audit action %q, got %q", "app.table.create", action)
	}
}

// TestCreateAppTableForUser_DuplicateNameRejected covers T5's Done-when
// validation-failure case: duplicate table name against the app's existing
// tables (validateTableInput).
func TestCreateAppTableForUser_DuplicateNameRejected(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// appsHandlerTestPool seeds a table named "test_table" already.
	_, err := h.CreateAppTableForUser(ctx, actors["loner"], appID, TableRequestBody{
		Name:    "test_table",
		Columns: []config.ColumnConfig{{Name: "x", Type: "text"}},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *ValidationError for a duplicate table name, got %v (%T)", err, err)
	}
}

// TestCreateAppTableForUser_DuplicateColumnNameRejected covers T5's
// Done-when validation-failure case: duplicate column name within the
// table (validateTableInput).
func TestCreateAppTableForUser_DuplicateColumnNameRejected(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.CreateAppTableForUser(ctx, actors["loner"], appID, TableRequestBody{
		Name:    "dupcol_table",
		Columns: []config.ColumnConfig{{Name: "x", Type: "text"}, {Name: "x", Type: "int"}},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *ValidationError for a duplicate column name, got %v (%T)", err, err)
	}
}

// TestCreateAppTableForUser_UnsupportedColumnTypeRejected covers T5's
// Done-when validation-failure case: an unsupported column type
// (validateTableInput).
func TestCreateAppTableForUser_UnsupportedColumnTypeRejected(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.CreateAppTableForUser(ctx, actors["loner"], appID, TableRequestBody{
		Name:    "badtype_table",
		Columns: []config.ColumnConfig{{Name: "x", Type: "not-a-real-type"}},
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *ValidationError for an unsupported column type, got %v (%T)", err, err)
	}
}

// TestCreateAppTableForUser_NonWriterForbidden covers the authorization
// half of the extraction: a caller without CanWrite (appviewer) is rejected
// with ErrForbidden before any table is created — the same check the
// pre-extraction handler ran.
func TestCreateAppTableForUser_NonWriterForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.CreateAppTableForUser(ctx, actors["appviewer"], appID, TableRequestBody{
		Name:    "viewer_table",
		Columns: []config.ColumnConfig{{Name: "x", Type: "text"}},
	}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

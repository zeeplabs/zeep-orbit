package mcpserver

// tools_add_column_foreign_key_test.go — coverage for orbit_add_column_foreign_key
// (column-foreign-key spec T9), mirroring tools_add_table_column_test.go's
// structure: happy path, forbidden, and the two new sentinel error mappings
// (ErrColumnAlreadyHasReference, *provisioner.ForeignKeyViolationError).

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// seedAppWithFKTables creates a real app with a "categories" table (unique
// name column) and an "items" table with an uncommitted uuid "category_id"
// column, giving orbit_add_column_foreign_key a real existing column to
// target.
func seedAppWithFKTables(t *testing.T, h *dashboard.Handler, ownerID string) (appID, itemsTable string) {
	t.Helper()
	ctx := context.Background()
	owner := &dashboard.DashboardUser{ID: ownerID, Role: "member"}
	app, err := h.CreateAppForUser(ctx, owner, dashboard.AppRequestBody{Name: fmt.Sprintf("toolsfkapp-%d", time.Now().UnixNano())}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := h.CreateAppTableForUser(ctx, owner, app.ID, dashboard.TableRequestBody{
		Name:    "categories",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text", Unique: true}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateAppTableForUser categories: %v", err)
	}
	items, err := h.CreateAppTableForUser(ctx, owner, app.ID, dashboard.TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "category_id", Type: "uuid"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}
	return app.ID, items.Name
}

// TestOrbitAddColumnForeignKey_Success covers the happy path: matches T6's
// happy path through the MCP layer.
func TestOrbitAddColumnForeignKey_Success(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addfk-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, itemsTable := seedAppWithFKTables(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
			"references":  map[string]any{"table": "categories", "column": "id"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_column_foreign_key: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res.Content)
	}

	var out struct {
		Columns []struct {
			Name       string `json:"name"`
			References *struct {
				Table string `json:"table"`
			} `json:"references"`
		} `json:"columns"`
	}
	decodeToolResult(t, res, &out)
	var found bool
	for _, c := range out.Columns {
		if c.Name == "category_id" {
			found = true
			if c.References == nil || c.References.Table != "categories" {
				t.Fatalf("expected category_id to reference categories, got %+v", c.References)
			}
		}
	}
	if !found {
		t.Fatalf("category_id column not found in %+v", out.Columns)
	}
}

// TestOrbitAddColumnForeignKey_ViewerForbidden covers the CanWrite() tier
// this tool enforces, matching the REST-equivalent rejection class.
func TestOrbitAddColumnForeignKey_ViewerForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addfk-owner2@example.com")
	viewer := authTestUser(t, pool, "tools-addfk-viewer@example.com")
	appID, itemsTable := seedAppWithFKTables(t, h, owner.ID)
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: appID}, viewer.ID, dashboard.AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember (viewer): %v", err)
	}
	viewerToken, _, err := dashboard.CreatePAT(context.Background(), pool, viewer.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, viewerToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
			"references":  map[string]any{"table": "categories", "column": "id"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_column_foreign_key (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a forbidden tool error for a viewer (CanWrite()==false)")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "forbidden" {
		t.Fatalf("expected \"forbidden\", got %q", text.Text)
	}
}

// TestOrbitAddColumnForeignKey_AlreadyHasReferenceReturnsDistinctError
// covers the ErrColumnAlreadyHasReference sentinel mapping — a specific,
// safe-to-expose message, not a generic internal error.
func TestOrbitAddColumnForeignKey_AlreadyHasReferenceReturnsDistinctError(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addfk-dup-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, itemsTable := seedAppWithFKTables(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)
	ctx := context.Background()

	// First add succeeds, second must be rejected with the specific sentinel.
	if res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
			"references":  map[string]any{"table": "categories", "column": "id"},
		},
	}); err != nil || res.IsError {
		t.Fatalf("expected success on 1st add, err=%v res=%+v", err, res)
	}

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
			"references":  map[string]any{"table": "categories", "column": "id"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_column_foreign_key (2nd): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for a column that already has a foreign key")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != dashboard.ErrColumnAlreadyHasReference.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrColumnAlreadyHasReference.Error(), text.Text)
	}
}

// TestOrbitAddColumnForeignKey_OrphanedRowsReturnsForeignKeyViolationError
// covers the *provisioner.ForeignKeyViolationError mapping — the Postgres
// 23503 violation detail surfaces as a specific tool error, not a generic
// internal error.
func TestOrbitAddColumnForeignKey_OrphanedRowsReturnsForeignKeyViolationError(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addfk-orphan-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, itemsTable := seedAppWithFKTables(t, h, owner.ID)

	app, _, err := dashboard.GetApp(context.Background(), pool, appID, owner)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	orphanID := "11111111-1111-1111-1111-111111111111"
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO "+strings.ReplaceAll(app.Name, "-", "_")+".items (category_id) VALUES ($1)", orphanID,
	); err != nil {
		t.Fatalf("seed orphan row: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
			"references":  map[string]any{"table": "categories", "column": "id"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_column_foreign_key: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for orphaned rows violating the new constraint")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text == "internal error" {
		t.Fatalf("expected the specific foreign-key-violation message, got the generic internal error")
	}
}

package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// seedAppWithTableForColumnTests creates a real app + physical table (via
// the real *dashboard.Handler/provisioner, no manual pool.Exec DDL) so
// orbit_add_table_column's ALTER TABLE ADD COLUMN path has a real table to
// operate against.
func seedAppWithTableForColumnTests(t *testing.T, h *dashboard.Handler, ownerID string) (appID, tableName string) {
	t.Helper()
	ctx := context.Background()
	owner := &dashboard.DashboardUser{ID: ownerID, Role: "member"}
	app, err := h.CreateAppForUser(ctx, owner, dashboard.AppRequestBody{Name: "toolscolapp"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, owner, app.ID, dashboard.TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	return app.ID, table.Name
}

// TestOrbitAddTableColumn_TwiceInARowKeepsBothColumns is the concrete
// regression test for the orphaned-column risk (spec.md Success Criteria),
// driven through a real MCP client roundtrip.
func TestOrbitAddTableColumn_TwiceInARowKeepsBothColumns(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addcol-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)
	ctx := context.Background()

	res1, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_table_column",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"column":     map[string]any{"name": "price", "type": "integer", "required": false, "default": "", "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_column (1st): %v", err)
	}
	if res1.IsError {
		t.Fatalf("expected success on 1st add, got error: %+v", res1.Content)
	}

	res2, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_table_column",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"column":     map[string]any{"name": "sku", "type": "text", "required": false, "default": "", "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_column (2nd): %v", err)
	}
	if res2.IsError {
		t.Fatalf("expected success on 2nd add, got error: %+v", res2.Content)
	}

	var out struct {
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
	}
	decodeToolResult(t, res2, &out)
	if len(out.Columns) != 3 {
		t.Fatalf("expected 3 columns (title, price, sku) after 2 adds, got %d: %+v", len(out.Columns), out.Columns)
	}
}

// TestOrbitAddTableColumn_DuplicateNameReturnsDistinctError covers the
// "column already exists" tool error, not a generic internal error.
func TestOrbitAddTableColumn_DuplicateNameReturnsDistinctError(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addcol-dup-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_table_column",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"column":     map[string]any{"name": "title", "type": "integer", "required": false, "default": "", "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_column: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for a duplicate column name")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != dashboard.ErrColumnAlreadyExists.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrColumnAlreadyExists.Error(), text.Text)
	}
}

// TestOrbitAddTableColumn_ViewerForbidden covers the CanWrite() tier this
// tool actually enforces, tested with an explicit viewer member.
func TestOrbitAddTableColumn_ViewerForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addcol-owner2@example.com")
	viewer := authTestUser(t, pool, "tools-addcol-viewer@example.com")
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: appID}, viewer.ID, dashboard.AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember (viewer): %v", err)
	}
	viewerToken, _, err := dashboard.CreatePAT(context.Background(), pool, viewer.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, viewerToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_table_column",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"column":     map[string]any{"name": "new_col", "type": "text", "required": false, "default": "", "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_column (protocol-level): %v", err)
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

// TestOrbitAddTableColumn_UnknownAppReturnsNotFound covers the not-found
// branch for an invisible/nonexistent app.
func TestOrbitAddTableColumn_UnknownAppReturnsNotFound(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addcol-nf@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_table_column",
		Arguments: map[string]any{
			"app_id":     "00000000-0000-0000-0000-000000000000",
			"table_name": "items",
			"column":     map[string]any{"name": "new_col", "type": "text", "required": false, "default": "", "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_column: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a not-found tool error for a nonexistent app")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "not found" {
		t.Fatalf("expected \"not found\", got %q", text.Text)
	}
}

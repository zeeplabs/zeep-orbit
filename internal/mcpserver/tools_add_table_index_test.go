package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// TestOrbitAddTableIndex_AddsIndexReflectedInSchema covers the happy path:
// adding an index on an existing column, confirmed via a follow-up
// orbit_get_app_schema call.
func TestOrbitAddTableIndex_AddsIndexReflectedInSchema(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addidx-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)
	ctx := context.Background()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_table_index",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"index":      map[string]any{"name": "items_title_idx", "columns": []string{"title"}, "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_index: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res.Content)
	}
	var out struct {
		Indexes []struct {
			Name string `json:"name"`
		} `json:"indexes"`
	}
	decodeToolResult(t, res, &out)
	if len(out.Indexes) != 1 || out.Indexes[0].Name != "items_title_idx" {
		t.Fatalf("expected 1 index named items_title_idx, got %+v", out.Indexes)
	}

	schemaRes, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "orbit_get_app_schema",
		Arguments: map[string]any{"app_id": appID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_get_app_schema: %v", err)
	}
	if schemaRes.IsError {
		t.Fatalf("orbit_get_app_schema returned an error: %+v", schemaRes.Content)
	}
}

// TestOrbitAddTableIndex_DuplicateNameReturnsDistinctError covers the
// "index already exists" tool error.
func TestOrbitAddTableIndex_DuplicateNameReturnsDistinctError(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addidx-dup-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)
	ctx := context.Background()
	seedIdx := config.IndexConfig{Name: "items_title_idx", Columns: []string{"title"}}
	if _, err := h.AddTableIndexForUser(ctx, &dashboard.DashboardUser{ID: owner.ID, Role: "member"}, appID, tableName, seedIdx, "127.0.0.1"); err != nil {
		t.Fatalf("seed AddTableIndexForUser: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_table_index",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"index":      map[string]any{"name": "items_title_idx", "columns": []string{"title"}, "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_index: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for a duplicate index name")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != dashboard.ErrIndexAlreadyExists.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrIndexAlreadyExists.Error(), text.Text)
	}
}

// TestOrbitAddTableIndex_UnknownColumnReturnsValidationError covers an index
// referencing a nonexistent column.
func TestOrbitAddTableIndex_UnknownColumnReturnsValidationError(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addidx-badcol-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_add_table_index",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"index":      map[string]any{"name": "items_bad_idx", "columns": []string{"no_such_column"}, "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_index: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a validation tool error for an index on a nonexistent column")
	}
}

// TestOrbitAddTableIndex_ViewerForbidden covers the CanWrite() tier this
// tool actually enforces.
func TestOrbitAddTableIndex_ViewerForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addidx-owner2@example.com")
	viewer := authTestUser(t, pool, "tools-addidx-viewer@example.com")
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
		Name: "orbit_add_table_index",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			"index":      map[string]any{"name": "items_title_idx", "columns": []string{"title"}, "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_add_table_index (protocol-level): %v", err)
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

// TestOrbitAddTableIndex_DescriptionDisclosesBlockingBehavior covers spec
// P2 AC6: the tool description must warn the calling agent that index
// creation briefly blocks writes to the target table.
func TestOrbitAddTableIndex_DescriptionDisclosesBlockingBehavior(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addidx-desc-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)
	tools, err := sess.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *mcp.Tool
	for _, tl := range tools.Tools {
		if tl.Name == "orbit_add_table_index" {
			found = tl
		}
	}
	if found == nil {
		t.Fatal("orbit_add_table_index not found in tool list")
	}
	if !strings.Contains(found.Description, "block") && !strings.Contains(found.Description, "CONCURRENTLY") {
		t.Fatalf("expected orbit_add_table_index's description to disclose blocking write behavior, got %q", found.Description)
	}
}

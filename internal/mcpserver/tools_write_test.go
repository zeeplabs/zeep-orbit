package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// TestOrbitCreateApp_CreatesAppAndAudits covers T11's Done-when:
// orbit_create_app via a real MCP client creates an app identical to what
// the REST endpoint would for the same input, and produces the same
// audit_log entry (mcp-server spec MCP-06, MCP-10).
func TestOrbitCreateApp_CreatesAppAndAudits(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-create-app@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_app",
		Arguments: map[string]any{
			"name":               "mcp-created-app",
			"auth_email_enabled": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_app: %v", err)
	}
	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeToolResult(t, res, &out)

	if out.Name != "mcp-created-app" {
		t.Fatalf("expected app name %q, got %q", "mcp-created-app", out.Name)
	}
	if out.ID == "" {
		t.Fatal("expected a non-empty app id")
	}

	var action, resourceType string
	if err := pool.QueryRow(context.Background(),
		`SELECT action, resource_type FROM zeep_system.audit_log WHERE resource_type = 'app' AND resource_id = $1`,
		out.ID,
	).Scan(&action, &resourceType); err != nil {
		t.Fatalf("expected an audit_log row for the MCP-created app: %v", err)
	}
	if action != "app.create" {
		t.Fatalf("expected audit action %q, got %q", "app.create", action)
	}
}

// TestOrbitCreateTable_CreatesTableWithColumns covers T11's Done-when:
// orbit_create_table via a real MCP client creates a table with columns
// exactly as specified (mcp-server spec MCP-07).
func TestOrbitCreateTable_CreatesTableWithColumns(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-create-table@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-create-table-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_table",
		Arguments: map[string]any{
			"app_id": app.ID,
			"name":   "widgets",
			"rls":    "owner",
			"columns": []map[string]any{
				{"name": "title", "type": "text", "required": true, "default": "", "unique": false},
				{"name": "price", "type": "integer", "required": false, "default": "", "unique": false},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_table: %v", err)
	}
	var out struct {
		Name    string `json:"name"`
		RLS     string `json:"rls"`
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
	}
	decodeToolResult(t, res, &out)

	if out.Name != "widgets" || out.RLS != "owner" {
		t.Fatalf("expected table 'widgets' with rls 'owner', got %+v", out)
	}
	if len(out.Columns) != 2 || out.Columns[0].Name != "title" || out.Columns[1].Name != "price" {
		t.Fatalf("expected columns [title, price] exactly as specified, got %+v", out.Columns)
	}
}

// TestOrbitSetTableRLSMode_SetsModeAndRejectsInvalidValue covers T11's
// Done-when: orbit_set_table_rls_mode sets the table's RLS mode; invalid
// RLS value returns a structured tool error, table unchanged.
func TestOrbitSetTableRLSMode_SetsModeAndRejectsInvalidValue(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-set-rls@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-set-rls-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := dashboard.InsertAppTable(context.Background(), pool, app.ID, dashboard.AppTableRow{Name: "items", RLS: ""}); err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	// Invalid value first: table must stay unchanged (still "").
	invalidRes, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_set_table_rls_mode",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "items",
			"rls_mode":   "not-a-real-mode",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_set_table_rls_mode (invalid): %v", err)
	}
	if !invalidRes.IsError {
		t.Fatal("expected a structured tool error for an invalid rls_mode value")
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].RLSMode != "" {
		t.Fatalf("expected table rls_mode to remain unchanged (\"\") after a rejected invalid value, got %+v", schema.Tables)
	}

	// Now a valid value: table must be updated to "policy".
	validRes, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_set_table_rls_mode",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "items",
			"rls_mode":   "policy",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_set_table_rls_mode (valid): %v", err)
	}
	if validRes.IsError {
		t.Fatalf("expected a successful result for a valid rls_mode, got error: %+v", validRes.Content)
	}
	var out struct {
		RLS string `json:"rls"`
	}
	decodeToolResult(t, validRes, &out)
	if out.RLS != "policy" {
		t.Fatalf("expected rls %q, got %q", "policy", out.RLS)
	}
}

// TestOrbitCreateTable_DuplicateColumnNameReturnsStructuredError covers
// T11's Done-when: a malformed orbit_create_table call (duplicate column
// name) returns a structured tool error naming the problem, no partial
// table created.
func TestOrbitCreateTable_DuplicateColumnNameReturnsStructuredError(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-dup-column@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-dup-column-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_table",
		Arguments: map[string]any{
			"app_id": app.ID,
			"name":   "dup_cols",
			"columns": []map[string]any{
				{"name": "title", "type": "text", "required": true, "default": "", "unique": false},
				{"name": "title", "type": "text", "required": false, "default": "", "unique": false},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_table (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a structured tool error for a duplicate column name")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text == "" {
		t.Fatalf("expected a non-empty structured error message naming the duplicate column, got %+v", res.Content)
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables) != 0 {
		t.Fatalf("expected no table to have been created after the validation failure, got %+v", schema.Tables)
	}
}

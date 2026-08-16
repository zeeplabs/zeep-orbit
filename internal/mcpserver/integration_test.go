package mcpserver

// integration_test.go — T15 of mcp-server: end-to-end MCP tool-calling
// integration test, using the MCP SDK's real client against a running test
// server + real Postgres, driving the entire P1+P2 story in one sequence
// (mcp-server spec tasks.md T15, closing MCP-01 through MCP-14): connect
// with a PAT -> orbit_create_app -> orbit_create_table ->
// orbit_set_table_rls_mode ("policy") -> orbit_create_policy_from_template
// (owner_only) -> orbit_get_app_schema to verify final state -> revoke the
// PAT -> confirm the same client is now rejected.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

func TestMCPEndToEnd_CreateAppTableRLSPolicy_ThenRevokeRejects(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "e2e-mcp@example.com")
	token, patRow, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSession(t, pool, token)
	ctx := context.Background()

	// 1. orbit_create_app
	appName := fmt.Sprintf("e2e-mcp-app-%d", time.Now().UnixNano())
	createAppRes, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_create_app",
		Arguments: map[string]any{
			"name":               appName,
			"auth_email_enabled": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_app: %v", err)
	}
	var app struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeToolResult(t, createAppRes, &app)
	if app.Name != appName || app.ID == "" {
		t.Fatalf("expected created app %q with a non-empty id, got %+v", appName, app)
	}

	// 2. orbit_create_table
	createTableRes, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_create_table",
		Arguments: map[string]any{
			"app_id": app.ID,
			"name":   "notes",
			"columns": []map[string]any{
				{"name": "title", "type": "text", "required": true, "default": "", "unique": false},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_table: %v", err)
	}
	if createTableRes.IsError {
		t.Fatalf("orbit_create_table returned an error result: %+v", createTableRes.Content)
	}

	// 3. orbit_set_table_rls_mode -> "policy"
	rlsRes, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_set_table_rls_mode",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "notes",
			"rls_mode":   "policy",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_set_table_rls_mode: %v", err)
	}
	if rlsRes.IsError {
		t.Fatalf("orbit_set_table_rls_mode returned an error result: %+v", rlsRes.Content)
	}

	// 4. orbit_create_policy_from_template ("owner_only")
	policyRes, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_create_policy_from_template",
		Arguments: map[string]any{
			"app_id":      app.ID,
			"table_name":  "notes",
			"template_id": "owner_only",
			"actions":     []string{"select"},
			"roles":       []string{"member"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_from_template: %v", err)
	}
	var policyOut struct {
		Created []struct {
			Name   string   `json:"pg_policy_name"`
			Action string   `json:"action"`
			Roles  []string `json:"roles"`
		} `json:"created"`
		FailedPolicy string `json:"failed_policy"`
	}
	decodeToolResult(t, policyRes, &policyOut)
	if len(policyOut.Created) != 1 || policyOut.FailedPolicy != "" {
		t.Fatalf("expected exactly 1 created policy and no failure, got %+v", policyOut)
	}
	if policyOut.Created[0].Name != "tpl_owner_only_select" || policyOut.Created[0].Action != "select" {
		t.Fatalf("expected policy tpl_owner_only_select/select, got %+v", policyOut.Created[0])
	}

	// 5. orbit_get_app_schema -- verify final state matches exactly what was
	// created across the sequence.
	schemaRes, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "orbit_get_app_schema",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_get_app_schema: %v", err)
	}
	var schema dashboard.AppSchema
	decodeToolResult(t, schemaRes, &schema)

	if schema.AppID != app.ID || schema.AppName != appName {
		t.Fatalf("expected schema for app %q (%s), got %+v", appName, app.ID, schema)
	}
	if len(schema.Tables) != 1 {
		t.Fatalf("expected exactly 1 table, got %+v", schema.Tables)
	}
	table := schema.Tables[0]
	if table.Name != "notes" {
		t.Fatalf("expected table name %q, got %q", "notes", table.Name)
	}
	if table.RLSMode != "policy" {
		t.Fatalf("expected rls_mode %q, got %q", "policy", table.RLSMode)
	}
	foundTitleColumn := false
	for _, c := range table.Columns {
		if c.Name == "title" {
			foundTitleColumn = true
		}
	}
	if !foundTitleColumn {
		t.Fatalf("expected column %q to be present, got %+v", "title", table.Columns)
	}
	if len(table.Policies) != 1 {
		t.Fatalf("expected exactly 1 policy, got %+v", table.Policies)
	}
	if table.Policies[0].Name != "tpl_owner_only_select" || table.Policies[0].Action != "select" {
		t.Fatalf("expected policy tpl_owner_only_select/select, got %+v", table.Policies[0])
	}
	if len(table.Policies[0].Roles) != 1 || table.Policies[0].Roles[0] != "member" {
		t.Fatalf("expected policy roles [member], got %+v", table.Policies[0].Roles)
	}

	// 6. Revoke the PAT.
	if err := dashboard.RevokePAT(ctx, pool, owner.ID, patRow.ID); err != nil {
		t.Fatalf("RevokePAT: %v", err)
	}

	// 7. The same client's token must now be rejected -- re-validated on
	// every call (RequirePAT, per design.md: no caching, hits the DB every
	// time), so no propagation delay is tolerated.
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "orbit_list_apps"}); err == nil {
		t.Fatal("expected a tool call with the revoked PAT's session to fail, got no error")
	}
}

package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// TestOrbitCreatePolicyAdvanced_ChainedClauses covers spec MAPT-01: a
// caller with CanManage() can create a policy with a clause shape no
// template covers (two clauses joined by AND), and the result is visible
// via orbit_list_table_policies afterward.
func TestOrbitCreatePolicyAdvanced_ChainedClauses(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-adv-chained@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "advch", "notes")

	sess := startMCPSessionWithHandler(t, pool, dashH, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_policy_advanced",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "notes",
			"name":       "adv_chained_select",
			"action":     "select",
			"roles":      []string{"member"},
			"clauses": []map[string]any{
				{"column": "owner_id", "operator": "=", "value_source": "claim", "value": "sub"},
				{"column": "owner_id", "operator": "IS NOT NULL", "logic": "OR"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_advanced: %v", err)
	}
	if res.IsError {
		if text, ok := res.Content[0].(*mcp.TextContent); ok {
			t.Fatalf("expected success, got error: %s", text.Text)
		}
		t.Fatalf("expected success, got error: %+v", res.Content)
	}
	var out struct {
		Name    string                   `json:"pg_policy_name"`
		Action  string                   `json:"action"`
		Roles   []string                 `json:"roles"`
		Clauses []dashboard.PolicyClause `json:"clauses"`
	}
	decodeToolResult(t, res, &out)
	if out.Name != "adv_chained_select" || out.Action != "select" {
		t.Fatalf("expected policy adv_chained_select/select, got %+v", out)
	}
	if len(out.Clauses) != 2 {
		t.Fatalf("expected 2 clauses persisted, got %+v", out.Clauses)
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables[0].Policies) != 1 {
		t.Fatalf("expected exactly 1 policy visible after creation, got %+v", schema.Tables[0].Policies)
	}
}

// TestOrbitCreatePolicyAdvanced_ForbiddenWithoutManageRole covers spec
// MAPT-03: a caller without CanManage() on the app is rejected before any
// policy row or DDL is created.
func TestOrbitCreatePolicyAdvanced_ForbiddenWithoutManageRole(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-adv-forbidden-owner@example.com")
	outsider := authTestUser(t, pool, "tools-adv-forbidden-outsider@example.com")
	outsiderToken, _, err := dashboard.CreatePAT(context.Background(), pool, outsider.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "advfb", "notes")

	sess := startMCPSessionWithHandler(t, pool, dashH, outsiderToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_policy_advanced",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "notes",
			"name":       "adv_forbidden_select",
			"action":     "select",
			"roles":      []string{"member"},
			"clauses": []map[string]any{
				{"column": "owner_id", "operator": "=", "value_source": "claim", "value": "sub"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_advanced: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a forbidden error for a caller with no role on the app")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "forbidden" {
		t.Fatalf("expected \"forbidden\", got %+v", res.Content)
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables[0].Policies) != 0 {
		t.Fatalf("expected zero policies created after a forbidden rejection, got %+v", schema.Tables[0].Policies)
	}
}

// TestOrbitCreatePolicyAdvanced_UnknownColumnReturnsSpecificError covers
// spec MAPT-04 and the mapWriteError fix this feature depends on: a clause
// referencing a nonexistent column returns the real provisioner validation
// message, not the generic internal error, and creates nothing.
func TestOrbitCreatePolicyAdvanced_UnknownColumnReturnsSpecificError(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-adv-badcolumn@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "advbc", "notes")

	sess := startMCPSessionWithHandler(t, pool, dashH, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_policy_advanced",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "notes",
			"name":       "adv_badcolumn_select",
			"action":     "select",
			"roles":      []string{"member"},
			"clauses": []map[string]any{
				{"column": "does_not_exist", "operator": "=", "value_source": "literal", "value": "x"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_advanced: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for a clause referencing an unknown column")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text == "" {
		t.Fatalf("expected a non-empty error message, got %+v", res.Content)
	}
	if text.Text == errInternal.Error() {
		t.Fatalf("expected the specific validation message, got the generic internal error %q", text.Text)
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables[0].Policies) != 0 {
		t.Fatalf("expected zero policies created after a validation rejection, got %+v", schema.Tables[0].Policies)
	}
}

// TestOrbitCreatePolicyAdvanced_DuplicateNameReturnsAlreadyExists covers
// spec MAPT-05: a policy name colliding with an existing policy on the same
// table+action returns ErrPolicyAlreadyExists, not a duplicate row.
func TestOrbitCreatePolicyAdvanced_DuplicateNameReturnsAlreadyExists(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-adv-dupe@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "advdp", "notes")

	if _, err := dashH.CreateTablePolicyForUser(context.Background(), owner, app.ID, "notes", dashboard.PolicyDef{
		Name:   "adv_dupe_select",
		Action: "select",
		Roles:  []string{"member"},
		Clauses: []dashboard.PolicyClause{
			{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
		},
	}, "test"); err != nil {
		t.Fatalf("pre-create colliding policy: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, dashH, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_policy_advanced",
		Arguments: map[string]any{
			"app_id":     app.ID,
			"table_name": "notes",
			"name":       "adv_dupe_select",
			"action":     "select",
			"roles":      []string{"member"},
			"clauses": []map[string]any{
				{"column": "owner_id", "operator": "=", "value_source": "claim", "value": "sub"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_advanced: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an already-exists error for a colliding policy name")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text != dashboard.ErrPolicyAlreadyExists.Error() {
		t.Fatalf("expected %q, got %+v", dashboard.ErrPolicyAlreadyExists.Error(), res.Content)
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables[0].Policies) != 1 {
		t.Fatalf("expected exactly the 1 pre-existing policy, got %+v", schema.Tables[0].Policies)
	}
}

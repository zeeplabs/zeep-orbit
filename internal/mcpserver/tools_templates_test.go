package mcpserver

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// provisionPolicyTestTable creates a real app + physical table (rls:
// "policy", so the provisioner adds a physical owner_id column) via the
// same *ForUser methods a REST call or orbit_create_app/orbit_create_table
// tool call would use — CreateTablePolicyForUser (T7) issues real
// CREATE POLICY DDL, so a catalog-only fixture (dashboard.CreateApp +
// InsertAppTable, used by the read/write-tool tests) is not enough here.
// appName gets a unique suffix: unlike authTestPool's TRUNCATE cleanup
// (dashboard_pats/dashboard_users only), physical app schemas created by
// the real provisioner here aren't dropped between runs, so a fixed name
// would collide (including its policy names) across repeated test runs.
func provisionPolicyTestTable(t *testing.T, dashH *dashboard.Handler, owner *dashboard.DashboardUser, appName, tableName string) *dashboard.AppRow {
	t.Helper()
	app, err := dashH.CreateAppForUser(context.Background(), owner, dashboard.AppRequestBody{
		Name:             fmt.Sprintf("%s-%d", appName, time.Now().UnixNano()),
		AuthEmailEnabled: true,
	}, "test")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := dashH.CreateAppTableForUser(context.Background(), owner, app.ID, dashboard.TableRequestBody{
		Name: tableName,
		RLS:  "policy",
	}, "test"); err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	return app
}

// TestOrbitListPolicyTemplates_ReturnsSixTemplatesWithStructure covers T12's
// Done-when: orbit_list_policy_templates returns the same 6 templates
// policytemplates.List() does, with enough structure (id, description,
// required inputs) for an LLM to pick one without free-form clause syntax.
func TestOrbitListPolicyTemplates_ReturnsSixTemplatesWithStructure(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-list-templates@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "orbit_list_policy_templates"})
	if err != nil {
		t.Fatalf("CallTool orbit_list_policy_templates: %v", err)
	}
	var out struct {
		Templates []struct {
			ID             string   `json:"id"`
			Description    string   `json:"description"`
			RequiredInputs []string `json:"required_inputs"`
		} `json:"templates"`
	}
	decodeToolResult(t, res, &out)

	if len(out.Templates) != 6 {
		t.Fatalf("expected 6 templates, got %d: %+v", len(out.Templates), out.Templates)
	}
	wantIDs := []string{"owner_only", "open_read", "read_only", "value_match", "open_read_owner_write", "blocked_by_default"}
	for i, want := range wantIDs {
		if out.Templates[i].ID != want {
			t.Fatalf("expected template %d id %q, got %q", i, want, out.Templates[i].ID)
		}
		if out.Templates[i].Description == "" {
			t.Fatalf("expected a non-empty description for template %q", want)
		}
	}
	if len(out.Templates[0].RequiredInputs) == 0 {
		t.Fatalf("expected owner_only to declare required inputs, got none")
	}
}

// TestOrbitCreatePolicyFromTemplate_SingleActionTemplate covers T12's
// Done-when: orbit_create_policy_from_template for a single-action template
// (owner_only) creates the expected policy via CreateTablePolicyForUser.
func TestOrbitCreatePolicyFromTemplate_SingleActionTemplate(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-tpl-owner-only@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "tplo", "notes")

	rl := dashboard.NewRateLimiter(1000, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, dashH, rl))
	defer srv.Close()
	sess, err := connectClient(context.Background(), srv.URL, token)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
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
	var out struct {
		Created []struct {
			Name   string   `json:"pg_policy_name"`
			Action string   `json:"action"`
			Roles  []string `json:"roles"`
		} `json:"created"`
		FailedPolicy  string `json:"failed_policy"`
		FailureReason string `json:"failure_reason"`
	}
	decodeToolResult(t, res, &out)

	if len(out.Created) != 1 {
		t.Fatalf("expected exactly 1 created policy, got %+v", out.Created)
	}
	if out.Created[0].Name != "tpl_owner_only_select" || out.Created[0].Action != "select" {
		t.Fatalf("expected policy tpl_owner_only_select/select, got %+v", out.Created[0])
	}
}

// TestOrbitCreatePolicyFromTemplate_CompositeTemplatePartialFailure covers
// T12's Done-when: orbit_create_policy_from_template for the composite
// template (open_read_owner_write) creates all 3 policies in sequence;
// forcing the 2nd to fail (pre-existing colliding policy name) stops before
// the 3rd and reports created/failed/pending per policy.
func TestOrbitCreatePolicyFromTemplate_CompositeTemplatePartialFailure(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-tpl-composite@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "tplc", "posts")

	// open_read_owner_write produces, in order: select
	// (tpl_open_read_owner_write_select), update
	// (tpl_open_read_owner_write_update), delete
	// (tpl_open_read_owner_write_delete). Pre-create the 2nd policy's name
	// directly so CreateTablePolicyForUser's UNIQUE/duplicate-DDL check
	// forces it to fail, without needing a bad clause/role input.
	if _, err := dashH.CreateTablePolicyForUser(context.Background(), owner, app.ID, "posts", dashboard.PolicyDef{
		Name:   "tpl_open_read_owner_write_update",
		Action: "update",
		Roles:  []string{"member"},
		Clauses: []dashboard.PolicyClause{
			{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
		},
	}, "test"); err != nil {
		t.Fatalf("pre-create colliding policy: %v", err)
	}

	rl := dashboard.NewRateLimiter(1000, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, dashH, rl))
	defer srv.Close()
	sess, err := connectClient(context.Background(), srv.URL, token)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_policy_from_template",
		Arguments: map[string]any{
			"app_id":      app.ID,
			"table_name":  "posts",
			"template_id": "open_read_owner_write",
			"roles":       []string{"member"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_from_template: %v", err)
	}
	var out struct {
		Created []struct {
			Name string `json:"pg_policy_name"`
		} `json:"created"`
		FailedPolicy  string   `json:"failed_policy"`
		FailureReason string   `json:"failure_reason"`
		Pending       []string `json:"pending_policies"`
	}
	decodeToolResult(t, res, &out)

	if len(out.Created) != 1 || out.Created[0].Name != "tpl_open_read_owner_write_select" {
		t.Fatalf("expected exactly the 1st policy (select) created before the failure, got %+v", out.Created)
	}
	if out.FailedPolicy != "tpl_open_read_owner_write_update" {
		t.Fatalf("expected failed_policy %q, got %q", "tpl_open_read_owner_write_update", out.FailedPolicy)
	}
	if out.FailureReason == "" {
		t.Fatal("expected a non-empty failure_reason")
	}
	if len(out.Pending) != 1 || out.Pending[0] != "tpl_open_read_owner_write_delete" {
		t.Fatalf("expected pending_policies [tpl_open_read_owner_write_delete], got %+v", out.Pending)
	}
}

// TestOrbitCreatePolicyFromTemplate_MissingInputReturnsStructuredError
// covers T12's Done-when: missing/invalid required input (e.g. no roles for
// owner_only) returns a structured tool error naming the missing input,
// zero policies created.
func TestOrbitCreatePolicyFromTemplate_MissingInputReturnsStructuredError(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-tpl-missing-input@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	dashH := newTestDashboardHandler(pool)
	app := provisionPolicyTestTable(t, dashH, owner, "tplm", "secrets")

	rl := dashboard.NewRateLimiter(1000, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, dashH, rl))
	defer srv.Close()
	sess, err := connectClient(context.Background(), srv.URL, token)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer sess.Close()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_policy_from_template",
		Arguments: map[string]any{
			"app_id":      app.ID,
			"table_name":  "secrets",
			"template_id": "owner_only",
			"actions":     []string{"select"},
			// roles intentionally omitted.
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_policy_from_template: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a structured tool error for a missing required input (roles)")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || text.Text == "" {
		t.Fatalf("expected a non-empty error naming the missing input, got %+v", res.Content)
	}

	schema, err := dashboard.GetAppSchemaForUser(context.Background(), pool, owner, app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if len(schema.Tables[0].Policies) != 0 {
		t.Fatalf("expected zero policies created after a missing-input rejection, got %+v", schema.Tables[0].Policies)
	}
}

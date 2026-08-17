package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// TestOrbitCreateWebhook_CreatesWebhookForManager covers the happy path: a
// caller who can manage the app creates a webhook, returned config matches
// what the Dashboard's Webhooks page would show (no raw token leaked).
func TestOrbitCreateWebhook_CreatesWebhookForManager(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-createwh-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := h.CreateAppForUser(context.Background(), &dashboard.DashboardUser{ID: owner.ID, Role: "member"}, dashboard.AppRequestBody{Name: "createwh-app"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_webhook",
		Arguments: map[string]any{
			"app_id":          app.ID,
			"name":            "orders webhook",
			"method":          "POST",
			"event_type_path": "eventType",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_webhook: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res.Content)
	}
	var out map[string]any
	decodeToolResult(t, res, &out)
	if out["name"] != "orders webhook" {
		t.Fatalf("expected name %q, got %+v", "orders webhook", out)
	}
	if _, leaked := out["token_secret"]; leaked {
		t.Fatalf("orbit_create_webhook response leaked a raw token_secret field: %+v", out)
	}
}

// TestOrbitCreateWebhook_EditorForbidden covers the CanManage() tier this
// tool actually enforces, distinct from the table tools' CanWrite() tier.
func TestOrbitCreateWebhook_EditorForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-createwh-owner2@example.com")
	editor := authTestUser(t, pool, "tools-createwh-editor@example.com")
	app, err := h.CreateAppForUser(context.Background(), &dashboard.DashboardUser{ID: owner.ID, Role: "member"}, dashboard.AppRequestBody{Name: "createwh-app2"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: app.ID}, editor.ID, dashboard.AppRoleEditor); err != nil {
		t.Fatalf("AddAppMember (editor): %v", err)
	}
	editorToken, _, err := dashboard.CreatePAT(context.Background(), pool, editor.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, editorToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_create_webhook",
		Arguments: map[string]any{
			"app_id":          app.ID,
			"name":            "orders webhook",
			"method":          "POST",
			"event_type_path": "eventType",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_create_webhook (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a forbidden tool error for an editor (CanManage()==false)")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "forbidden" {
		t.Fatalf("expected \"forbidden\", got %q", text.Text)
	}
}

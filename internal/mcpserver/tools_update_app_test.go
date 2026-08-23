package mcpserver

// tools_update_app_test.go — coverage for orbit_update_app (ai-edit-chat
// spec T4, AIEC-13), driven through a real MCP client roundtrip. Derived
// from tasks.md's T4 Done-when list, not from reading the implementation:
// happy path toggles auth_email_enabled; RBAC-denied returns the same
// "forbidden" tool error sibling write tools already return for a viewer.

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// TestOrbitUpdateApp_TogglesAuthEmailEnabled covers the happy path: calling
// orbit_update_app flips auth_email_enabled on the target app, matching the
// exact value sent in the request.
func TestOrbitUpdateApp_TogglesAuthEmailEnabled(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-updateapp-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	ctx := context.Background()
	app, err := h.CreateAppForUser(ctx, &dashboard.DashboardUser{ID: owner.ID, Role: "member"}, dashboard.AppRequestBody{
		Name:             "toolsupdateapp",
		AuthEmailEnabled: true,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_update_app",
		Arguments: map[string]any{
			"app_id":             app.ID,
			"auth_email_enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_update_app: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res.Content)
	}

	var out struct {
		AuthEmailEnabled bool `json:"auth_email_enabled"`
	}
	decodeToolResult(t, res, &out)
	if out.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled=false after orbit_update_app, got true")
	}
}

// TestOrbitUpdateApp_ViewerForbidden covers the CanWrite() tier this tool
// enforces, matching orbit_add_table_column/orbit_add_table_index's
// existing RBAC test depth for a viewer member.
func TestOrbitUpdateApp_ViewerForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-updateapp-owner2@example.com")
	viewer := authTestUser(t, pool, "tools-updateapp-viewer@example.com")

	ctx := context.Background()
	app, err := h.CreateAppForUser(ctx, &dashboard.DashboardUser{ID: owner.ID, Role: "member"}, dashboard.AppRequestBody{
		Name:             "toolsupdateappviewer",
		AuthEmailEnabled: true,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if _, err := dashboard.AddAppMember(ctx, pool, dashboard.AppRef{BackendAppID: app.ID}, viewer.ID, dashboard.AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember (viewer): %v", err)
	}
	viewerToken, _, err := dashboard.CreatePAT(ctx, pool, viewer.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, viewerToken)

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_update_app",
		Arguments: map[string]any{
			"app_id":             app.ID,
			"auth_email_enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_update_app (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a forbidden tool error for a viewer (CanWrite()==false)")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "forbidden" {
		t.Fatalf("expected %q, got %q", "forbidden", text.Text)
	}

	// The app's auth_email_enabled must be unchanged — a denied request
	// must never reach the store.
	fresh, _, err := dashboard.GetApp(ctx, pool, app.ID, &dashboard.DashboardUser{ID: owner.ID, Role: "member"})
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if !fresh.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled to remain true after a denied update, got false")
	}
}

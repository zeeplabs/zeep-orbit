package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"go.uber.org/zap"
)

// seedAppWebhookForMappingTests creates a real app (with a registered
// "employees" table in the registry, matching the target table
// orbit_save_webhook_event_mapping tests write to) plus one webhook on it.
func seedAppWebhookForMappingTests(t *testing.T, h *dashboard.Handler, reg *registry.Registry, ownerID string) (appID, webhookID string) {
	t.Helper()
	ctx := context.Background()
	owner := &dashboard.DashboardUser{ID: ownerID, Role: "member"}
	app, err := h.CreateAppForUser(ctx, owner, dashboard.AppRequestBody{Name: "mappingapp"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	reg.Register(&registry.App{
		Config:     config.AppConfig{Name: app.Name},
		SchemaName: app.Name,
		Tables: map[string]*registry.Table{
			"employees": {
				Name: "employees",
				Columns: []registry.Column{
					{Name: "external_id", Type: "text"},
					{Name: "full_name", Type: "text"},
				},
			},
		},
	})
	wh, err := h.CreateWebhookForUser(ctx, owner, app.ID, dashboard.CreateWebhookInput{
		Name: "employees sync", Method: "POST", EventTypePath: "eventType",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebhookForUser: %v", err)
	}
	return app.ID, wh.ID
}

// TestOrbitSaveWebhookEventMapping_RegistersMapping covers the happy path.
func TestOrbitSaveWebhookEventMapping_RegistersMapping(t *testing.T) {
	pool := authTestPool(t)
	reg := registry.New()
	h := dashboard.NewHandler(pool, reg, zap.NewNop())
	owner := authTestUser(t, pool, "tools-savemap-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, webhookID := seedAppWebhookForMappingTests(t, h, reg, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_save_webhook_event_mapping",
		Arguments: map[string]any{
			"app_id":           appID,
			"webhook_id":       webhookID,
			"event_type_value": "employee.created",
			"action":           "insert",
			"target_table":     "employees",
			"field_mappings": []map[string]any{
				{"source_path": "id", "column": "external_id"},
				{"source_path": "name", "column": "full_name"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_save_webhook_event_mapping: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %+v", res.Content)
	}
	var out struct {
		EventTypeValue string `json:"event_type_value"`
		TargetTable    string `json:"target_table"`
	}
	decodeToolResult(t, res, &out)
	if out.EventTypeValue != "employee.created" || out.TargetTable != "employees" {
		t.Fatalf("unexpected mapping: %+v", out)
	}
}

// TestOrbitSaveWebhookEventMapping_UnknownTargetTableReturnsDistinctError
// covers the unknown-target-table tool error.
func TestOrbitSaveWebhookEventMapping_UnknownTargetTableReturnsDistinctError(t *testing.T) {
	pool := authTestPool(t)
	reg := registry.New()
	h := dashboard.NewHandler(pool, reg, zap.NewNop())
	owner := authTestUser(t, pool, "tools-savemap-badtable-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, webhookID := seedAppWebhookForMappingTests(t, h, reg, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_save_webhook_event_mapping",
		Arguments: map[string]any{
			"app_id":           appID,
			"webhook_id":       webhookID,
			"event_type_value": "employee.created",
			"action":           "insert",
			"target_table":     "does_not_exist",
			"field_mappings": []map[string]any{
				{"source_path": "id", "column": "external_id"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_save_webhook_event_mapping: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for an unknown target table")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != dashboard.ErrUnknownTargetTable.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrUnknownTargetTable.Error(), text.Text)
	}
}

// TestOrbitSaveWebhookEventMapping_ConflictReturnsDistinctError covers the
// mapping-conflict tool error, leaving the first mapping intact.
func TestOrbitSaveWebhookEventMapping_ConflictReturnsDistinctError(t *testing.T) {
	pool := authTestPool(t)
	reg := registry.New()
	h := dashboard.NewHandler(pool, reg, zap.NewNop())
	owner := authTestUser(t, pool, "tools-savemap-conflict-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, webhookID := seedAppWebhookForMappingTests(t, h, reg, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)
	ctx := context.Background()
	args := map[string]any{
		"app_id":           appID,
		"webhook_id":       webhookID,
		"event_type_value": "employee.created",
		"action":           "insert",
		"target_table":     "employees",
		"field_mappings": []map[string]any{
			{"source_path": "id", "column": "external_id"},
		},
	}

	first, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "orbit_save_webhook_event_mapping", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool (1st): %v", err)
	}
	if first.IsError {
		t.Fatalf("expected success on 1st save, got error: %+v", first.Content)
	}

	second, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "orbit_save_webhook_event_mapping", Arguments: args})
	if err != nil {
		t.Fatalf("CallTool (2nd): %v", err)
	}
	if !second.IsError {
		t.Fatal("expected a conflict error on the 2nd save with the same event_type_value")
	}
	text, ok := second.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", second.Content[0])
	}
	if text.Text != dashboard.ErrMappingConflict.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrMappingConflict.Error(), text.Text)
	}
}

// TestOrbitSaveWebhookEventMapping_CrossAppWebhookReturnsNotFound covers the
// cross-app scoping edge case.
func TestOrbitSaveWebhookEventMapping_CrossAppWebhookReturnsNotFound(t *testing.T) {
	pool := authTestPool(t)
	reg := registry.New()
	h := dashboard.NewHandler(pool, reg, zap.NewNop())
	owner := authTestUser(t, pool, "tools-savemap-crossapp-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	_, webhookID := seedAppWebhookForMappingTests(t, h, reg, owner.ID)
	otherApp, err := h.CreateAppForUser(context.Background(), &dashboard.DashboardUser{ID: owner.ID, Role: "member"}, dashboard.AppRequestBody{Name: "otherapp"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser (other): %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_save_webhook_event_mapping",
		Arguments: map[string]any{
			"app_id":           otherApp.ID,
			"webhook_id":       webhookID,
			"event_type_value": "employee.created",
			"action":           "insert",
			"target_table":     "employees",
			"field_mappings": []map[string]any{
				{"source_path": "id", "column": "external_id"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_save_webhook_event_mapping: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a not-found tool error for a webhook belonging to a different app")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "webhook not found" {
		t.Fatalf("expected \"webhook not found\", got %q", text.Text)
	}
}

// TestOrbitSaveWebhookEventMapping_EditorForbidden covers the CanManage()
// tier this tool enforces.
func TestOrbitSaveWebhookEventMapping_EditorForbidden(t *testing.T) {
	pool := authTestPool(t)
	reg := registry.New()
	h := dashboard.NewHandler(pool, reg, zap.NewNop())
	owner := authTestUser(t, pool, "tools-savemap-editor-owner@example.com")
	editor := authTestUser(t, pool, "tools-savemap-editor@example.com")
	appID, webhookID := seedAppWebhookForMappingTests(t, h, reg, owner.ID)
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: appID}, editor.ID, dashboard.AppRoleEditor); err != nil {
		t.Fatalf("AddAppMember (editor): %v", err)
	}
	editorToken, _, err := dashboard.CreatePAT(context.Background(), pool, editor.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, editorToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_save_webhook_event_mapping",
		Arguments: map[string]any{
			"app_id":           appID,
			"webhook_id":       webhookID,
			"event_type_value": "employee.created",
			"action":           "insert",
			"target_table":     "employees",
			"field_mappings": []map[string]any{
				{"source_path": "id", "column": "external_id"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_save_webhook_event_mapping (protocol-level): %v", err)
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

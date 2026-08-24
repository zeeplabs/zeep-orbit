package mcpserver

// tools_remove_column_foreign_key_test.go — coverage for
// orbit_remove_column_foreign_key (column-foreign-key spec T10), mirroring
// tools_add_column_foreign_key_test.go's structure: happy path, forbidden,
// and the new ErrColumnHasNoReference sentinel mapping.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// seedAppWithExistingFK creates a real app with a "categories" table and an
// "items" table whose "category_id" column already has a foreign key to
// categories.id, giving orbit_remove_column_foreign_key a real constraint to
// remove.
func seedAppWithExistingFK(t *testing.T, h *dashboard.Handler, ownerID string) (appID, itemsTable string) {
	t.Helper()
	ctx := context.Background()
	owner := &dashboard.DashboardUser{ID: ownerID, Role: "member"}
	app, err := h.CreateAppForUser(ctx, owner, dashboard.AppRequestBody{Name: fmt.Sprintf("toolsrmfkapp-%d", time.Now().UnixNano())}, "127.0.0.1")
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
		Name: "items",
		Columns: []config.ColumnConfig{{
			Name: "category_id", Type: "uuid",
			References: &config.ReferenceConfig{Table: "categories", Column: "id"},
		}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser items: %v", err)
	}
	return app.ID, items.Name
}

// TestOrbitRemoveColumnForeignKey_Success covers the happy path: matches
// T7's happy path through the MCP layer.
func TestOrbitRemoveColumnForeignKey_Success(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-rmfk-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, itemsTable := seedAppWithExistingFK(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_remove_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_remove_column_foreign_key: %v", err)
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
	for _, c := range out.Columns {
		if c.Name == "category_id" && c.References != nil {
			t.Fatalf("expected category_id's reference cleared, got %+v", c.References)
		}
	}
}

// TestOrbitRemoveColumnForeignKey_ViewerForbidden covers the CanWrite() tier
// this tool enforces, matching the REST-equivalent rejection class.
func TestOrbitRemoveColumnForeignKey_ViewerForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-rmfk-owner2@example.com")
	viewer := authTestUser(t, pool, "tools-rmfk-viewer@example.com")
	appID, itemsTable := seedAppWithExistingFK(t, h, owner.ID)
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: appID}, viewer.ID, dashboard.AppRoleViewer); err != nil {
		t.Fatalf("AddAppMember (viewer): %v", err)
	}
	viewerToken, _, err := dashboard.CreatePAT(context.Background(), pool, viewer.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, viewerToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_remove_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_remove_column_foreign_key (protocol-level): %v", err)
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

// TestOrbitRemoveColumnForeignKey_NoReferenceReturnsDistinctError covers the
// ErrColumnHasNoReference sentinel mapping — a specific, safe-to-expose
// message, not a generic internal error.
func TestOrbitRemoveColumnForeignKey_NoReferenceReturnsDistinctError(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-rmfk-noref-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, itemsTable := seedAppWithFKTables(t, h, owner.ID) // no reference set on category_id

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_remove_column_foreign_key",
		Arguments: map[string]any{
			"app_id":      appID,
			"table_name":  itemsTable,
			"column_name": "category_id",
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_remove_column_foreign_key: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for a column with no foreign key")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != dashboard.ErrColumnHasNoReference.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrColumnHasNoReference.Error(), text.Text)
	}
}

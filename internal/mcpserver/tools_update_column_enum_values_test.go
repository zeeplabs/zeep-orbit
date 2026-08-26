package mcpserver

// tools_update_column_enum_values_test.go — coverage for
// orbit_update_column_enum_values (column-enum-type T11), mirroring
// tools_add_column_foreign_key_test.go's structure: happy path (widen),
// the narrow-in-use rejection, and the non-enum-column rejection.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// seedAppWithEnumTable creates a real app with an "items" table that has a
// "status" enum column (allowed_values: pending, active, closed) plus a
// plain "title" text column, giving orbit_update_column_enum_values a real
// existing enum column (and a real non-enum column) to target.
func seedAppWithEnumTable(t *testing.T, h *dashboard.Handler, ownerID string) (appID, tableName string) {
	t.Helper()
	ctx := context.Background()
	owner := &dashboard.DashboardUser{ID: ownerID, Role: "member"}
	app, err := h.CreateAppForUser(ctx, owner, dashboard.AppRequestBody{Name: fmt.Sprintf("toolsenumapp-%d", time.Now().UnixNano())}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, owner, app.ID, dashboard.TableRequestBody{
		Name: "items",
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text"},
			{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active", "closed"}},
		},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	return app.ID, table.Name
}

// TestOrbitUpdateColumnEnumValues_WidenSucceeds covers T11's Done-when:
// widening (only additions) succeeds and the new set is persisted.
func TestOrbitUpdateColumnEnumValues_WidenSucceeds(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-updenum-widen-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithEnumTable(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_update_column_enum_values",
		Arguments: map[string]any{
			"app_id":         appID,
			"table_name":     tableName,
			"column_name":    "status",
			"allowed_values": []string{"pending", "active", "closed", "archived"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_update_column_enum_values: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success for a pure widen, got error: %+v", res.Content)
	}

	var out struct {
		Columns []struct {
			Name          string   `json:"name"`
			AllowedValues []string `json:"allowed_values"`
		} `json:"columns"`
	}
	decodeToolResult(t, res, &out)
	var found bool
	for _, c := range out.Columns {
		if c.Name == "status" {
			found = true
			want := []string{"pending", "active", "closed", "archived"}
			if len(c.AllowedValues) != len(want) {
				t.Fatalf("expected allowed_values %v, got %v", want, c.AllowedValues)
			}
			for i, v := range want {
				if c.AllowedValues[i] != v {
					t.Fatalf("expected allowed_values %v, got %v", want, c.AllowedValues)
				}
			}
		}
	}
	if !found {
		t.Fatalf("status column not found in %+v", out.Columns)
	}
}

// TestOrbitUpdateColumnEnumValues_NarrowInUseRejected covers T11's
// Done-when: narrow-rejects with the safe error message when values are
// still in use, instead of a generic internal error.
func TestOrbitUpdateColumnEnumValues_NarrowInUseRejected(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-updenum-narrow-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithEnumTable(t, h, owner.ID)

	app, _, err := dashboard.GetApp(context.Background(), pool, appID, owner)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	schema := strings.ReplaceAll(app.Name, "-", "_")
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO "+schema+"."+tableName+" (title, status) VALUES ('t1', 'closed')",
	); err != nil {
		t.Fatalf("seed row using the value to be removed: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_update_column_enum_values",
		Arguments: map[string]any{
			"app_id":         appID,
			"table_name":     tableName,
			"column_name":    "status",
			"allowed_values": []string{"pending", "active"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_update_column_enum_values: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for narrowing a value still in use")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text == "internal error" {
		t.Fatal("expected the specific enum-value-in-use message, got the generic internal error")
	}
	if !strings.Contains(text.Text, "closed") || !strings.Contains(text.Text, "1") {
		t.Fatalf("expected the error to name the offending value and its row count, got %q", text.Text)
	}
}

// TestOrbitUpdateColumnEnumValues_NonEnumColumnRejected covers T11's
// Done-when: calling the tool on a non-enum column is rejected with the
// dedicated sentinel message, not a generic internal error.
func TestOrbitUpdateColumnEnumValues_NonEnumColumnRejected(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-updenum-notenum-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithEnumTable(t, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "orbit_update_column_enum_values",
		Arguments: map[string]any{
			"app_id":         appID,
			"table_name":     tableName,
			"column_name":    "title",
			"allowed_values": []string{"a", "b"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_update_column_enum_values (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for a non-enum column")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != dashboard.ErrColumnIsNotEnum.Error() {
		t.Fatalf("expected %q, got %q", dashboard.ErrColumnIsNotEnum.Error(), text.Text)
	}
}

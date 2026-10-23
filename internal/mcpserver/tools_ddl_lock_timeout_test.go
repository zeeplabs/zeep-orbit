package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// TestOrbitAddTableColumn_LockTimeoutReturnsBusyMessage (multi-replica-
// consistency T12): a table held by a long-running reader makes the tool
// fail with the same English message REST returns with its 409, not the
// generic internal error.
func TestOrbitAddTableColumn_LockTimeoutReturnsBusyMessage(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-addcol-lock-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableForColumnTests(t, h, owner.ID)
	sess := startMCPSessionWithHandler(t, pool, h, token)
	ctx := context.Background()

	prev := db.DDLLockTimeout
	db.DDLLockTimeout = 300 * time.Millisecond
	t.Cleanup(func() { db.DDLLockTimeout = prev })

	reader, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("reader begin: %v", err)
	}
	defer reader.Rollback(ctx)
	if _, err := reader.Exec(ctx, `SELECT * FROM "toolscolapp"."items"`); err != nil {
		t.Fatalf("reader select: %v", err)
	}

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "orbit_add_table_column",
		Arguments: map[string]any{
			"app_id":     appID,
			"table_name": tableName,
			// A name no other test uses: the physical "toolscolapp" schema
			// survives between tests in this package, and a column that
			// already exists physically needs no ALTER (and no lock).
			"column": map[string]any{"name": "lock_probe", "type": "integer", "required": false, "default": "", "unique": false},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result while the table is locked")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != provisioner.ErrDDLLockTimeout.Error() {
		t.Fatalf("expected %q, got %q", provisioner.ErrDDLLockTimeout.Error(), text.Text)
	}
}

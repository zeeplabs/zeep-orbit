package dashboard

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// uniqueAppName avoids colliding with a leftover physical Postgres schema
// from a previous run of this test file against a persistent dev database
// (appsHandlerTestPool drops zeep_system's catalog rows between tests but
// does not drop the physical app schemas h.prov.Apply provisions).
func uniqueAppName(t *testing.T, base string) string {
	t.Helper()
	return base + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// TestGetAppSchemaForUser_TwoTablesWithAndWithoutPolicy covers T8's
// Done-when: an app with 2 tables (one rls="policy" with 1 policy, one
// rls="" with none) returns both tables with correct rls_mode, columns, and
// policies (empty array, not null, for the second table) — mcp-server spec
// MCP-09.
func TestGetAppSchemaForUser_TwoTablesWithAndWithoutPolicy(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{
		Name:             uniqueAppName(t, "schema-app"),
		AuthEmailEnabled: true,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}

	openTable, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "open_table",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text", Required: true}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser(open_table): %v", err)
	}

	policyTable, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "secured_table",
		RLS:     "policy",
		Columns: []config.ColumnConfig{{Name: "owner_id", Type: "uuid"}, {Name: "note", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser(secured_table): %v", err)
	}

	if _, err := h.CreateTablePolicyForUser(ctx, actors["loner"], app.ID, policyTable.Name, PolicyDef{
		Name:    "owner_reads",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateTablePolicyForUser: %v", err)
	}

	schema, err := GetAppSchemaForUser(ctx, pool, actors["loner"], app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if schema.AppID != app.ID || schema.AppName != app.Name {
		t.Fatalf("expected app_id=%q app_name=%q, got app_id=%q app_name=%q", app.ID, app.Name, schema.AppID, schema.AppName)
	}
	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(schema.Tables))
	}

	byName := map[string]AppSchemaTable{}
	for _, tbl := range schema.Tables {
		byName[tbl.Name] = tbl
	}

	open, ok := byName[openTable.Name]
	if !ok {
		t.Fatalf("expected table %q in schema", openTable.Name)
	}
	if open.RLSMode != "" {
		t.Fatalf("expected rls_mode \"\" for %q, got %q", openTable.Name, open.RLSMode)
	}
	if open.Policies == nil || len(open.Policies) != 0 {
		t.Fatalf("expected an empty (non-nil) policies slice for %q, got %v", openTable.Name, open.Policies)
	}
	if len(open.Columns) != 1 || open.Columns[0].Name != "title" || open.Columns[0].Type != "text" || open.Columns[0].Nullable {
		t.Fatalf("unexpected columns for %q: %+v", openTable.Name, open.Columns)
	}

	secured, ok := byName[policyTable.Name]
	if !ok {
		t.Fatalf("expected table %q in schema", policyTable.Name)
	}
	if secured.RLSMode != "policy" {
		t.Fatalf("expected rls_mode %q for %q, got %q", "policy", policyTable.Name, secured.RLSMode)
	}
	if len(secured.Policies) != 1 {
		t.Fatalf("expected 1 policy for %q, got %d", policyTable.Name, len(secured.Policies))
	}
	if secured.Policies[0].Name != "owner_reads" || secured.Policies[0].Action != "select" {
		t.Fatalf("unexpected policy for %q: %+v", policyTable.Name, secured.Policies[0])
	}
}

// TestGetAppSchemaForUser_EmptyAppReturnsEmptyTablesArray covers T8's
// Done-when: an app with zero tables returns tables: [], not an error.
func TestGetAppSchemaForUser_EmptyAppReturnsEmptyTablesArray(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "empty-app")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}

	schema, err := GetAppSchemaForUser(ctx, pool, actors["loner"], app.ID)
	if err != nil {
		t.Fatalf("GetAppSchemaForUser: %v", err)
	}
	if schema.Tables == nil {
		t.Fatal("expected a non-nil (empty) tables slice")
	}
	if len(schema.Tables) != 0 {
		t.Fatalf("expected 0 tables, got %d", len(schema.Tables))
	}
}

// TestGetAppSchemaForUser_NoAccessReturnsSameErrorAsGetApp covers T8's
// Done-when: an app the user has no access to returns the same
// authorization error GetApp already returns for that case.
func TestGetAppSchemaForUser_NoAccessReturnsSameErrorAsGetApp(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	_ = h

	_, err := GetAppSchemaForUser(ctx, pool, actors["outsider"], appID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound (same as GetApp for a no-access app), got %v", err)
	}

	// Cross-check directly against GetApp's own error for the identical
	// case, confirming "same authorization error" rather than merely "some
	// error".
	_, _, getAppErr := GetApp(ctx, pool, appID, actors["outsider"])
	if !errors.Is(getAppErr, ErrNotFound) {
		t.Fatalf("expected GetApp itself to also return ErrNotFound, got %v", getAppErr)
	}
}

// TestListAppsForUser_MatchesListApps covers T8's Done-when: ListAppsForUser
// output matches the existing ListApps handler's/store's output for the
// same user (extraction, no behavior change).
func TestListAppsForUser_MatchesListApps(t *testing.T) {
	pool, _, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	viaExtraction, err := ListAppsForUser(ctx, pool, actors["loner"])
	if err != nil {
		t.Fatalf("ListAppsForUser: %v", err)
	}
	viaOriginal, err := ListApps(ctx, pool, actors["loner"])
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(viaExtraction) != len(viaOriginal) {
		t.Fatalf("expected matching lengths, got %d vs %d", len(viaExtraction), len(viaOriginal))
	}
	for i := range viaOriginal {
		if viaExtraction[i].ID != viaOriginal[i].ID {
			t.Fatalf("expected matching app ids at index %d, got %q vs %q", i, viaExtraction[i].ID, viaOriginal[i].ID)
		}
	}
}

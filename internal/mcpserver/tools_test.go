package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/storage"
)

// startMCPSession spins up a NewHandler-backed httptest.Server and returns a
// connected MCP client session authenticated with token — used by every
// tool-registry test (T10-T12) to drive a real MCP client roundtrip, per
// the Test Coverage Matrix's "integration, real MCP client roundtrip"
// requirement for internal/mcpserver/tools.go.
func startMCPSession(t *testing.T, pool *db.Pool, token string) *mcp.ClientSession {
	t.Helper()
	return startMCPSessionWithHandler(t, pool, newTestDashboardHandler(pool), token)
}

// startMCPSessionWithHandler is startMCPSession's variant for tests that
// need to seed data through a specific *dashboard.Handler instance first
// (e.g. via CreateTablePolicyForUser) before spinning up the MCP server
// that wraps it — the seeding and the tool-call path must share the same
// Handler (and therefore the same *dashboard.Handler-scoped state) for the
// seeded data to be visible to the tool call.
func startMCPSessionWithHandler(t *testing.T, pool *db.Pool, h *dashboard.Handler, token string) *mcp.ClientSession {
	t.Helper()
	rl := dashboard.NewRateLimiter(1000, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, h, rl))
	sess, err := connectClient(context.Background(), srv.URL, token)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	t.Cleanup(func() {
		sess.Close()
		srv.Close()
	})
	return sess
}

// decodeToolResult unmarshals a successful CallToolResult's structured
// content into out.
func decodeToolResult(t *testing.T, res *mcp.CallToolResult, out interface{}) {
	t.Helper()
	if res.IsError {
		t.Fatalf("expected a successful tool result, got an error result: %+v", res.Content)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal StructuredContent into %T: %v", out, err)
	}
}

// TestOrbitListApps_ReturnsCallersOwnApps covers T10's Done-when: calling
// orbit_list_apps via a real MCP client returns the same apps
// ListAppsForUser would for that PAT's owning user (mcp-server spec
// MCP-09-adjacent tool-level coverage).
func TestOrbitListApps_ReturnsCallersOwnApps(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-list-apps@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-list-apps-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "orbit_list_apps"})
	if err != nil {
		t.Fatalf("CallTool orbit_list_apps: %v", err)
	}
	var out struct {
		Apps []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"apps"`
	}
	decodeToolResult(t, res, &out)

	if len(out.Apps) != 1 || out.Apps[0].ID != app.ID || out.Apps[0].Name != app.Name {
		t.Fatalf("expected exactly the caller's own app %s/%s, got %+v", app.ID, app.Name, out.Apps)
	}
}

// TestOrbitGetAppSchema_ReturnsTablesColumnsAndRLS covers T10's Done-when:
// calling orbit_get_app_schema for a known app id returns the same shape
// T8's tests already verified (tables/columns/rls_mode).
func TestOrbitGetAppSchema_ReturnsTablesColumnsAndRLS(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-get-schema@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-get-schema-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := dashboard.InsertAppTable(context.Background(), pool, app.ID, dashboard.AppTableRow{
		Name: "widgets",
		RLS:  "owner",
		Columns: []config.ColumnConfig{
			{Name: "title", Type: "text", Required: true},
		},
	}); err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_get_app_schema",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_get_app_schema: %v", err)
	}
	var out struct {
		AppID  string `json:"app_id"`
		Tables []struct {
			Name    string `json:"name"`
			RLSMode string `json:"rls_mode"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"tables"`
	}
	decodeToolResult(t, res, &out)

	if out.AppID != app.ID {
		t.Fatalf("expected app_id %s, got %s", app.ID, out.AppID)
	}
	if len(out.Tables) != 1 || out.Tables[0].Name != "widgets" || out.Tables[0].RLSMode != "owner" {
		t.Fatalf("expected one table 'widgets' with rls_mode 'owner', got %+v", out.Tables)
	}
	if len(out.Tables[0].Columns) != 1 || out.Tables[0].Columns[0].Name != "title" {
		t.Fatalf("expected one column 'title', got %+v", out.Tables[0].Columns)
	}
}

// TestOrbitGetAppSchema_NoAccessReturnsStructuredToolError covers T10's
// Done-when: calling orbit_get_app_schema for an app the caller can't
// access returns a structured tool error, not a raw Go error string.
func TestOrbitGetAppSchema_NoAccessReturnsStructuredToolError(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-schema-owner@example.com")
	// "member" (not authTestUser's default "admin") — CanReadAnyApp is true
	// for admin/auditor/superadmin, which would let this user read any app
	// regardless of membership, masking the no-access case this test needs.
	outsider, err := dashboard.CreateUser(context.Background(), pool, "tools-schema-outsider@example.com", "outsider", "hash", "member")
	if err != nil {
		t.Fatalf("create outsider user: %v", err)
	}
	token, _, err := dashboard.CreatePAT(context.Background(), pool, outsider.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-schema-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_get_app_schema",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_get_app_schema (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a structured tool error for an app the caller has no access to")
	}
	if len(res.Content) == 0 {
		t.Fatal("expected non-empty error content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text == "" {
		t.Fatal("expected a non-empty structured error message")
	}
}

// TestOrbitGetApp_ReturnsRedactedAppForAuthorizedCaller covers
// mcp-read-only-tools T1's Done-when: a caller with access to the app gets
// back the AppRow with RedactSecrets() already applied — no client_secret,
// secret_access_key, or jwt_secret value anywhere in the response (spec
// AC1/AC5).
func TestOrbitGetApp_ReturnsRedactedAppForAuthorizedCaller(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-get-app-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-get-app-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	const fakeSecretKey = "AKIAFAKESECRETACCESSKEYVALUE"
	const fakeClientSecret = "fake-google-oauth-client-secret"
	if err := dashboard.UpdateAppStorageConfig(context.Background(), pool, app.ID, &storage.StorageConfig{
		Bucket:          "starbem-apps",
		Region:          "us-east-1",
		Endpoint:        "https://s3.amazonaws.com",
		AccessKeyID:     "AKIAFAKEACCESSKEYID",
		SecretAccessKey: fakeSecretKey,
	}); err != nil {
		t.Fatalf("UpdateAppStorageConfig: %v", err)
	}
	authProviders := json.RawMessage(fmt.Sprintf(
		`{"google":{"enabled":true,"client_id":"fake-client-id.apps.googleusercontent.com","client_secret":%q,"redirect_url":"https://example.com/cb"}}`,
		fakeClientSecret,
	))
	if err := dashboard.UpdateAppAuthProvidersRaw(context.Background(), pool, app.ID, authProviders); err != nil {
		t.Fatalf("UpdateAppAuthProvidersRaw: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_get_app",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_get_app: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected a successful tool result, got an error result: %+v", res.Content)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	body := string(data)
	if strings.Contains(body, fakeSecretKey) {
		t.Fatalf("orbit_get_app leaked the S3 secret key: %s", body)
	}
	if strings.Contains(body, fakeClientSecret) {
		t.Fatalf("orbit_get_app leaked the OAuth client_secret: %s", body)
	}
	if strings.Contains(body, `"jwt_secret"`) {
		t.Fatalf("orbit_get_app leaked jwt_secret field: %s", body)
	}

	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal StructuredContent: %v", err)
	}
	if out.ID != app.ID || out.Name != app.Name {
		t.Fatalf("expected app %s/%s, got %+v", app.ID, app.Name, out)
	}
}

// TestOrbitGetApp_NoAccessReturnsStructuredToolError covers mcp-read-only-tools
// T1's Done-when: an app the caller has no membership/role on (and isn't
// superadmin/CanReadAnyApp) returns the same not-found tool error GetApp
// already returns (spec AC3), not the app data.
func TestOrbitGetApp_NoAccessReturnsStructuredToolError(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-get-app-owner2@example.com")
	outsider, err := dashboard.CreateUser(context.Background(), pool, "tools-get-app-outsider@example.com", "outsider", "hash", "member")
	if err != nil {
		t.Fatalf("create outsider user: %v", err)
	}
	token, _, err := dashboard.CreatePAT(context.Background(), pool, outsider.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-get-app-app2", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_get_app",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_get_app (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a structured tool error for an app the caller has no access to")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "not found" {
		t.Fatalf("expected the same not-found wording GetApp already returns, got %q", text.Text)
	}
}

// TestOrbitListAppAuthProviders_ReturnsRedactedShapeForAuthorizedCaller
// covers mcp-read-only-tools T2's Done-when: the same redacted shape
// GetAppAuthProviders' REST handler already returns (spec P2 AC3),
// client_secret replaced with client_secret_set, and the real secret
// value never present anywhere in the response.
func TestOrbitListAppAuthProviders_ReturnsRedactedShapeForAuthorizedCaller(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-list-providers-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-list-providers-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	const fakeClientSecret = "fake-google-oauth-client-secret-t2"
	authProviders := json.RawMessage(fmt.Sprintf(
		`{"google":{"enabled":true,"client_id":"fake-client-id.apps.googleusercontent.com","client_secret":%q,"redirect_url":"https://example.com/cb"}}`,
		fakeClientSecret,
	))
	if err := dashboard.UpdateAppAuthProvidersRaw(context.Background(), pool, app.ID, authProviders); err != nil {
		t.Fatalf("UpdateAppAuthProvidersRaw: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_app_auth_providers",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_app_auth_providers: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected a successful tool result, got an error result: %+v", res.Content)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	body := string(data)
	if strings.Contains(body, fakeClientSecret) {
		t.Fatalf("orbit_list_app_auth_providers leaked the OAuth client_secret: %s", body)
	}

	var out map[string]map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal StructuredContent: %v", err)
	}
	google, ok := out["google"]
	if !ok {
		t.Fatalf("expected a google provider entry, got %+v", out)
	}
	if _, hasSecret := google["client_secret"]; hasSecret {
		t.Fatalf("expected client_secret field to be absent, got %+v", google)
	}
	if set, _ := google["client_secret_set"].(bool); !set {
		t.Fatalf("expected client_secret_set=true, got %+v", google)
	}
	if google["client_id"] != "fake-client-id.apps.googleusercontent.com" {
		t.Fatalf("expected client_id to be preserved (non-secret field), got %+v", google)
	}
}

// TestOrbitListAppAuthProviders_NoAccessReturnsStructuredToolError covers
// mcp-read-only-tools T2's Done-when: an invisible/nonexistent app returns
// the same not-found tool error GetAppAuthProviders (via GetApp) already
// returns.
func TestOrbitListAppAuthProviders_NoAccessReturnsStructuredToolError(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-list-providers-owner2@example.com")
	outsider, err := dashboard.CreateUser(context.Background(), pool, "tools-list-providers-outsider@example.com", "outsider", "hash", "member")
	if err != nil {
		t.Fatalf("create outsider user: %v", err)
	}
	token, _, err := dashboard.CreatePAT(context.Background(), pool, outsider.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-list-providers-app2", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_app_auth_providers",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_app_auth_providers (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a structured tool error for an app the caller has no access to")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "not found" {
		t.Fatalf("expected the same not-found wording GetApp already returns, got %q", text.Text)
	}
}

// TestOrbitListMyPats_ReturnsOnlyCallersOwnPATs covers mcp-read-only-tools
// T3's Done-when: only PATs owned by the calling identity are returned —
// seeded with two distinct users' PATs, confirming no cross-user leakage
// (spec P2 AC4) — and that no raw token/JTI value is present, only
// metadata (id, name, kind, expiry, revoked/last-used timestamps).
func TestOrbitListMyPats_ReturnsOnlyCallersOwnPATs(t *testing.T) {
	pool := authTestPool(t)
	caller := authTestUser(t, pool, "tools-list-pats-caller@example.com")
	other := authTestUser(t, pool, "tools-list-pats-other@example.com")

	callerToken, _, err := dashboard.CreatePAT(context.Background(), pool, caller.ID, "caller-cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT (caller): %v", err)
	}
	otherToken, _, err := dashboard.CreatePAT(context.Background(), pool, other.ID, "other-cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT (other): %v", err)
	}

	sess := startMCPSession(t, pool, callerToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "orbit_list_my_pats"})
	if err != nil {
		t.Fatalf("CallTool orbit_list_my_pats: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected a successful tool result, got an error result: %+v", res.Content)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	body := string(data)
	if strings.Contains(body, callerToken) || strings.Contains(body, otherToken) {
		t.Fatalf("orbit_list_my_pats leaked a raw PAT token value: %s", body)
	}

	var out struct {
		PATs []struct {
			ID     string `json:"id"`
			UserID string `json:"user_id"`
			Name   string `json:"name"`
		} `json:"pats"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal StructuredContent: %v", err)
	}
	if len(out.PATs) != 1 {
		t.Fatalf("expected exactly 1 PAT (caller's own), got %d: %+v", len(out.PATs), out.PATs)
	}
	if out.PATs[0].UserID != caller.ID || out.PATs[0].Name != "caller-cli" {
		t.Fatalf("expected caller's own PAT (user_id=%s, name=caller-cli), got %+v", caller.ID, out.PATs[0])
	}
}

// TestOrbitListMyPats_EmptyForUserWithNoPATsReturnsEmptyArray covers
// mcp-read-only-tools T3's Done-when: a zero-PAT owner sees `[]`, never
// `null` (spec's "Empty-result shape" assumption). Calling orbit_list_my_pats
// itself always requires a PAT to authenticate (RequirePAT), so the calling
// identity can never have literally zero PATs at the moment of the call —
// this exercises the same dashboard.ListPATs call the tool makes directly,
// for a user who owns no PATs, confirming the tool's underlying data source
// returns []PATRow{}, not nil, for that case; the tool serializes whatever
// ListPATs returns unchanged (registerAccessReadTools does no filtering).
func TestOrbitListMyPats_EmptyForUserWithNoPATsReturnsEmptyArray(t *testing.T) {
	pool := authTestPool(t)
	noPatsUser := authTestUser(t, pool, "tools-list-pats-none@example.com")

	pats, err := dashboard.ListPATs(context.Background(), pool, noPatsUser.ID)
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if pats == nil {
		t.Fatal("expected ListPATs to return an empty slice, got nil")
	}
	if len(pats) != 0 {
		t.Fatalf("expected zero PATs for a fresh user, got %+v", pats)
	}

	out := orbitListMyPatsOutput{PATs: pats}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal orbitListMyPatsOutput: %v", err)
	}
	if !strings.Contains(string(data), `"pats":[]`) {
		t.Fatalf("expected serialized output to contain \"pats\":[], got %s", data)
	}
}

// seedAppWithTableAndPolicy provisions a real app schema + physical table
// (CreateTablePolicyForUser runs actual DDL, so a metadata-only app_tables
// row isn't enough) with one row policy already created, and adds member
// (userID, role) to the app. Returns the app id and the physical table's
// name. Used by orbit_list_table_policies tests (mcp-read-only-tools T5).
func seedAppWithTableAndPolicy(t *testing.T, pool *db.Pool, h *dashboard.Handler, ownerID string) (appID, tableName string) {
	t.Helper()
	ctx := context.Background()
	app, err := dashboard.CreateApp(ctx, pool, fmt.Sprintf("toolstpapp%d", time.Now().UnixNano()), ownerID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	schemaName := app.Name // no hyphens in the generated name above, so schemaNameForDB(app.Name) == app.Name
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schemaName)); err != nil {
		t.Fatalf("create app schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schemaName))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %q.requests (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), status TEXT NOT NULL DEFAULT 'pending')`,
		schemaName,
	)); err != nil {
		t.Fatalf("create physical table: %v", err)
	}
	if _, err := dashboard.InsertAppTable(ctx, pool, app.ID, dashboard.AppTableRow{
		Name:    "requests",
		RLS:     "",
		Columns: []config.ColumnConfig{{Name: "status", Type: "text"}},
	}); err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}
	owner := &dashboard.DashboardUser{ID: ownerID, Role: "member"}
	if _, err := h.CreateTablePolicyForUser(ctx, owner, app.ID, "requests", dashboard.PolicyDef{
		Name:    "seed_policy",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []dashboard.PolicyClause{{Column: "status", Operator: "IS NOT NULL"}},
	}, "127.0.0.1"); err != nil {
		t.Fatalf("CreateTablePolicyForUser (seed): %v", err)
	}
	return app.ID, "requests"
}

// TestOrbitListTablePolicies_ReturnsPoliciesForManager covers
// mcp-read-only-tools T5's Done-when: a caller who can manage the app
// (admin, the app owner) gets back the table's policies.
func TestOrbitListTablePolicies_ReturnsPoliciesForManager(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-tp-owner@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, tableName := seedAppWithTableAndPolicy(t, pool, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_table_policies",
		Arguments: map[string]any{"app_id": appID, "table_name": tableName},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_table_policies: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected a successful tool result, got an error result: %+v", res.Content)
	}
	var out struct {
		Policies []struct {
			PgPolicyName string `json:"pg_policy_name"`
		} `json:"policies"`
	}
	decodeToolResult(t, res, &out)
	if len(out.Policies) != 1 || out.Policies[0].PgPolicyName != "seed_policy" {
		t.Fatalf("expected 1 policy named %q, got %+v", "seed_policy", out.Policies)
	}
}

// TestOrbitListTablePolicies_EditorForbidden covers mcp-read-only-tools T5's
// Done-when: a caller whose role fails CanManage() on the app (a real
// editor member, not just "no membership") gets a forbidden tool error —
// the explicit CanManage() tier test the design's Authorization Matrix
// requires, distinct from orbit_get_app's plain-visibility tier.
func TestOrbitListTablePolicies_EditorForbidden(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-tp-owner2@example.com")
	editor := authTestUser(t, pool, "tools-tp-editor@example.com")
	appID, tableName := seedAppWithTableAndPolicy(t, pool, h, owner.ID)
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: appID}, editor.ID, dashboard.AppRoleEditor); err != nil {
		t.Fatalf("AddAppMember (editor): %v", err)
	}
	editorToken, _, err := dashboard.CreatePAT(context.Background(), pool, editor.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSessionWithHandler(t, pool, h, editorToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_table_policies",
		Arguments: map[string]any{"app_id": appID, "table_name": tableName},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_table_policies (protocol-level): %v", err)
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

// TestOrbitListTablePolicies_UnknownTableReturnsNotFound covers
// mcp-read-only-tools T5's Done-when: a nonexistent table name on a
// visible/manageable app returns a not-found tool error, never `[]` (spec
// AC4: an empty list means "table exists, zero policies").
func TestOrbitListTablePolicies_UnknownTableReturnsNotFound(t *testing.T) {
	pool := authTestPool(t)
	h := newTestDashboardHandler(pool)
	owner := authTestUser(t, pool, "tools-tp-owner3@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	appID, _ := seedAppWithTableAndPolicy(t, pool, h, owner.ID)

	sess := startMCPSessionWithHandler(t, pool, h, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_table_policies",
		Arguments: map[string]any{"app_id": appID, "table_name": "no-such-table"},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_table_policies (protocol-level): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a not-found tool error for a nonexistent table name")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent error, got %T", res.Content[0])
	}
	if text.Text != "table not found" {
		t.Fatalf("expected \"table not found\", got %q", text.Text)
	}
}

// TestOrbitListAppMembers_ReturnsMembersForManager covers mcp-read-only-tools
// T7's Done-when: a caller who can manage the app (admin, the app owner)
// gets back member rows (user_id, role, created_at) for every member,
// including a seeded editor.
func TestOrbitListAppMembers_ReturnsMembersForManager(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-members-owner@example.com")
	editor := authTestUser(t, pool, "tools-members-editor@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-members-app", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: app.ID}, editor.ID, dashboard.AppRoleEditor); err != nil {
		t.Fatalf("AddAppMember (editor): %v", err)
	}

	sess := startMCPSession(t, pool, token)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_app_members",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_app_members: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected a successful tool result, got an error result: %+v", res.Content)
	}
	var out struct {
		Members []struct {
			UserID    string `json:"user_id"`
			Role      string `json:"role"`
			CreatedAt string `json:"created_at"`
		} `json:"members"`
	}
	decodeToolResult(t, res, &out)
	if len(out.Members) != 2 {
		t.Fatalf("expected 2 members (owner admin + editor), got %d: %+v", len(out.Members), out.Members)
	}
	var sawAdmin, sawEditor bool
	for _, m := range out.Members {
		if m.CreatedAt == "" {
			t.Fatalf("expected a non-empty created_at, got %+v", m)
		}
		switch m.UserID {
		case owner.ID:
			sawAdmin = m.Role == "admin"
		case editor.ID:
			sawEditor = m.Role == "editor"
		}
	}
	if !sawAdmin || !sawEditor {
		t.Fatalf("expected owner=admin and editor=editor in the member list, got %+v", out.Members)
	}
}

// TestOrbitListAppMembers_EditorForbidden covers mcp-read-only-tools T7's
// Done-when: a caller whose role fails CanManage() on the app (a real
// editor member, not just "no membership") gets a forbidden tool error.
func TestOrbitListAppMembers_EditorForbidden(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "tools-members-owner2@example.com")
	editor := authTestUser(t, pool, "tools-members-editor2@example.com")
	app, err := dashboard.CreateApp(context.Background(), pool, "tools-members-app2", owner.ID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := dashboard.AddAppMember(context.Background(), pool, dashboard.AppRef{BackendAppID: app.ID}, editor.ID, dashboard.AppRoleEditor); err != nil {
		t.Fatalf("AddAppMember (editor): %v", err)
	}
	editorToken, _, err := dashboard.CreatePAT(context.Background(), pool, editor.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	sess := startMCPSession(t, pool, editorToken)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "orbit_list_app_members",
		Arguments: map[string]any{"app_id": app.ID},
	})
	if err != nil {
		t.Fatalf("CallTool orbit_list_app_members (protocol-level): %v", err)
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

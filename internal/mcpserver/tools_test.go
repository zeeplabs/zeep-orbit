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
	rl := dashboard.NewRateLimiter(1000, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, newTestDashboardHandler(pool), rl))
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

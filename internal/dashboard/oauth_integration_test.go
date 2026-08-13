// package dashboard_test (external test package, not dashboard) so this
// single end-to-end test can drive both the OAuth front door
// (internal/dashboard) and the MCP tool registry (internal/mcpserver) it
// hands a token to — a dashboard-internal _test.go file can't import
// mcpserver without an import cycle (mcpserver already imports dashboard).
package dashboard_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/mcpserver"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// oauthIntegrationTestPool provisions a clean zeep_system, mirroring the
// per-test-file helpers used throughout this package (e.g.
// oauth_client_store_test.go's oauthClientTestPool), duplicated here since
// this file's external package can't reach that unexported helper.
func oauthIntegrationTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := dashboard.ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.oauth_clients, zeep_system.dashboard_pats, zeep_system.dashboard_users CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	return pool
}

// bearerRoundTripper injects a static Authorization: Bearer <token> header
// — the MCP client's transport for driving /dashboard/mcp with the
// OAuth-issued access token.
type bearerRoundTripper struct {
	token string
}

func (t *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// callOrbitListApps drives a real MCP client through initialize + a single
// orbit_list_apps tool call against mcpURL, using accessToken as the
// bearer credential — returns an error if the handshake itself is
// rejected (e.g. a revoked token), matching how an external MCP client
// would observe the failure.
func callOrbitListApps(mcpURL, accessToken string) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "oauth-e2e-test-client", Version: "v0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   mcpURL,
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{token: accessToken}},
	}
	sess, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		return err
	}
	defer sess.Close()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "orbit_list_apps"})
	if err != nil {
		return err
	}
	if res.IsError {
		return &toolCallError{content: res.Content}
	}
	return nil
}

type toolCallError struct{ content []mcp.Content }

func (e *toolCallError) Error() string { return "tool call returned an error result" }

// pkcePair returns a matching (verifier, S256 challenge) pair.
func pkcePair() (verifier, challenge string) {
	verifier = "e2e-test-verifier-0123456789012345678901234"
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// TestOAuthEndToEnd_DiscoveryThroughToolCallRefreshAndReuseRevocation is
// T21's single end-to-end test, covering the full OAuth story spec.md's P1
// (OAuth) story describes as its Independent Test: discovery -> dynamic
// registration -> authorize (login+consent, simulated via a real session
// cookie + the Decide grant branch) -> code exchange -> a real MCP tool
// call against /dashboard/mcp with the resulting access token -> refresh
// rotation -> reuse of the superseded refresh token, confirming the
// resulting family-wide revocation blocks a further tool call
// (mcp-server spec MCP-19 through MCP-24).
func TestOAuthEndToEnd_DiscoveryThroughToolCallRefreshAndReuseRevocation(t *testing.T) {
	pool := oauthIntegrationTestPool(t)
	ctx := context.Background()

	oauthH := dashboard.NewOAuthHandler(pool)
	dashH := dashboard.NewHandler(pool, registry.New(), zap.NewNop())
	mcpRL := dashboard.NewRateLimiter(1000, time.Minute)
	mcpSrv := httptest.NewServer(mcpserver.NewHandler(pool, dashH, mcpRL))
	defer mcpSrv.Close()

	// 1. Discovery.
	metaReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	metaRR := httptest.NewRecorder()
	oauthH.GetMetadata(metaRR, metaReq)
	if metaRR.Code != http.StatusOK {
		t.Fatalf("metadata discovery: expected 200, got %d", metaRR.Code)
	}

	// 2. Dynamic client registration.
	regBody, _ := json.Marshal(map[string]any{
		"client_name":   "e2e-test-client",
		"redirect_uris": []string{"https://client.example.com/callback"},
	})
	regReq := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/register", strings.NewReader(string(regBody)))
	regRR := httptest.NewRecorder()
	oauthH.RegisterClient(regRR, regReq)
	if regRR.Code != http.StatusCreated {
		t.Fatalf("register client: expected 201, got %d, body=%s", regRR.Code, regRR.Body.String())
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(regRR.Body.Bytes(), &reg); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}

	// Admin already has an active Dashboard session (login itself is
	// exercised by other tests — GetSessionUser/CreateSession, not
	// duplicated here).
	admin, err := dashboard.CreateUser(ctx, pool, "oauth-e2e-admin@example.com", "e2e admin", "hash", "admin")
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	sessionToken := "e2e-session-token"
	if err := dashboard.CreateSession(ctx, pool, sessionToken, admin.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	verifier, challenge := pkcePair()
	authorizeQuery := "response_type=code&client_id=" + reg.ClientID +
		"&redirect_uri=" + url.QueryEscape("https://client.example.com/callback") +
		"&code_challenge=" + challenge + "&code_challenge_method=S256&state=e2e-state"

	// 3. Authorize (GET): active session hands off to consent.
	authzReq := httptest.NewRequest(http.MethodGet, "/dashboard/oauth/authorize?"+authorizeQuery, nil)
	authzReq.AddCookie(&http.Cookie{Name: "zeep_session", Value: sessionToken})
	authzRR := httptest.NewRecorder()
	oauthH.Authorize(authzRR, authzReq)
	if authzRR.Code != http.StatusFound {
		t.Fatalf("authorize: expected 302 handoff to consent, got %d", authzRR.Code)
	}
	if loc := authzRR.Header().Get("Location"); !strings.HasPrefix(loc, "/dashboard/oauth/consent?") {
		t.Fatalf("authorize: expected a consent handoff, got Location %q", loc)
	}

	// 4. Consent (Decide, grant): the admin's browser session authenticates
	// this request in production (RequireAuth middleware) — injected
	// directly here since this test calls the handler function, not the
	// full chi-routed server.
	decideBody, _ := json.Marshal(map[string]any{
		"client_id":             reg.ClientID,
		"redirect_uri":          "https://client.example.com/callback",
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"state":                 "e2e-state",
		"decision":              "grant",
	})
	decideReq := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/authorize", strings.NewReader(string(decideBody)))
	decideReq = decideReq.WithContext(dashboard.ContextWithUser(decideReq.Context(), admin))
	decideRR := httptest.NewRecorder()
	oauthH.Decide(decideRR, decideReq)
	if decideRR.Code != http.StatusOK {
		t.Fatalf("decide (grant): expected 200, got %d, body=%s", decideRR.Code, decideRR.Body.String())
	}
	var decideResp struct {
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.Unmarshal(decideRR.Body.Bytes(), &decideResp); err != nil {
		t.Fatalf("unmarshal decide response: %v", err)
	}
	callbackURL, err := url.Parse(decideResp.RedirectURL)
	if err != nil {
		t.Fatalf("parse redirect_url: %v", err)
	}
	code := callbackURL.Query().Get("code")
	if code == "" {
		t.Fatalf("expected a code param in the grant redirect, got %q", decideResp.RedirectURL)
	}

	// 5. Token exchange (authorization_code).
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {"https://client.example.com/callback"},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRR := httptest.NewRecorder()
	oauthH.Token(tokenRR, tokenReq)
	if tokenRR.Code != http.StatusOK {
		t.Fatalf("token exchange: expected 200, got %d, body=%s", tokenRR.Code, tokenRR.Body.String())
	}
	var firstTokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(tokenRR.Body.Bytes(), &firstTokens); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}

	// 6. Real MCP tool call against /dashboard/mcp with the OAuth-issued
	// access token — no PAT was ever manually created in the Dashboard UI
	// for this flow (spec.md P1-OAuth Independent Test).
	if err := callOrbitListApps(mcpSrv.URL, firstTokens.AccessToken); err != nil {
		t.Fatalf("orbit_list_apps with the OAuth-issued access token: %v", err)
	}

	// 7. Refresh rotation.
	refreshForm := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {firstTokens.RefreshToken}}
	refreshReq := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(refreshForm.Encode()))
	refreshReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	refreshRR := httptest.NewRecorder()
	oauthH.Token(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d, body=%s", refreshRR.Code, refreshRR.Body.String())
	}
	var secondTokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(refreshRR.Body.Bytes(), &secondTokens); err != nil {
		t.Fatalf("unmarshal refresh response: %v", err)
	}
	if secondTokens.AccessToken == firstTokens.AccessToken || secondTokens.RefreshToken == firstTokens.RefreshToken {
		t.Fatal("expected refresh rotation to mint a brand-new access+refresh pair")
	}
	if err := callOrbitListApps(mcpSrv.URL, secondTokens.AccessToken); err != nil {
		t.Fatalf("orbit_list_apps with the rotated access token: %v", err)
	}

	// 8. Reuse of the superseded (original) refresh token: rejected, and
	// the whole token family — including the current access token from
	// step 7 — is revoked.
	reuseForm := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {firstTokens.RefreshToken}}
	reuseReq := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(reuseForm.Encode()))
	reuseReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reuseRR := httptest.NewRecorder()
	oauthH.Token(reuseRR, reuseReq)
	if reuseRR.Code != http.StatusBadRequest {
		t.Fatalf("reuse of superseded refresh token: expected 400, got %d", reuseRR.Code)
	}

	if err := callOrbitListApps(mcpSrv.URL, secondTokens.AccessToken); err == nil {
		t.Fatal("expected the rotated access token to be rejected after reuse-triggered family revocation, but the tool call succeeded")
	}
}

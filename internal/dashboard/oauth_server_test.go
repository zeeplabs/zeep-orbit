package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestGetMetadata_ReturnsDiscoveryDocumentWithAllThreeEndpoints covers T17's
// Done-when: GET /.well-known/oauth-authorization-server returns a valid
// discovery document with all 3 endpoint URLs (spec MCP-19).
func TestGetMetadata_ReturnsDiscoveryDocumentWithAllThreeEndpoints(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)

	req := httptest.NewRequest(http.MethodGet, "https://orbit.example.com/.well-known/oauth-authorization-server", nil)
	req.Host = "orbit.example.com"
	rr := httptest.NewRecorder()

	h.GetMetadata(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body oauthMetadataResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal metadata response: %v", err)
	}
	if body.AuthorizationEndpoint == "" || body.TokenEndpoint == "" || body.RegistrationEndpoint == "" {
		t.Fatalf("expected all 3 endpoint URLs to be present, got %+v", body)
	}
	// httptest.NewRequest simulates a TLS connection (sets req.TLS) when
	// the target URL's scheme is https, so requestBaseURL reports https
	// here — matches how a real TLS-terminating request would look.
	if body.AuthorizationEndpoint != "https://orbit.example.com/dashboard/oauth/authorize" {
		t.Fatalf("expected authorization_endpoint to be absolute and correct, got %q", body.AuthorizationEndpoint)
	}
	if body.TokenEndpoint != "https://orbit.example.com/dashboard/oauth/token" {
		t.Fatalf("expected token_endpoint to be absolute and correct, got %q", body.TokenEndpoint)
	}
	if body.RegistrationEndpoint != "https://orbit.example.com/dashboard/oauth/register" {
		t.Fatalf("expected registration_endpoint to be absolute and correct, got %q", body.RegistrationEndpoint)
	}
}

// TestRegisterClientHandler_ReturnsClientIDWithoutPriorSetup covers T17's
// Done-when: POST /dashboard/oauth/register with a name and redirect URI
// returns a client_id, no prior manual setup required (spec MCP-20).
func TestRegisterClientHandler_ReturnsClientIDWithoutPriorSetup(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)

	body, _ := json.Marshal(registerClientRequest{
		ClientName:   "Claude Desktop",
		RedirectURIs: []string{"https://claude.ai/api/mcp/oauth/callback"},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/register", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.RegisterClient(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp registerClientResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	if resp.ClientID == "" {
		t.Fatal("expected a non-empty client_id")
	}
}

// TestRegisterClientHandler_RateLimitedPerIP covers T17's Done-when:
// registration endpoint rejects a request once its per-IP rate limit is
// exceeded — same RateLimiter.Middleware mechanism the real route
// (internal/server/server.go) wraps RegisterClient with.
func TestRegisterClientHandler_RateLimitedPerIP(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)
	rl := NewRateLimiter(2, time.Minute)
	limited := rl.Middleware(http.HandlerFunc(h.RegisterClient))

	doRequest := func() int {
		body, _ := json.Marshal(registerClientRequest{
			ClientName:   "client",
			RedirectURIs: []string{"https://example.com/cb"},
		})
		req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/register", bytes.NewReader(body))
		req.RemoteAddr = "203.0.113.5:12345"
		rr := httptest.NewRecorder()
		limited.ServeHTTP(rr, req)
		return rr.Code
	}

	for i := 0; i < 2; i++ {
		if code := doRequest(); code == http.StatusTooManyRequests {
			t.Fatalf("request %d was unexpectedly rate-limited", i+1)
		}
	}
	if code := doRequest(); code != http.StatusTooManyRequests {
		t.Fatalf("expected the 3rd request within the window to be rate-limited (429), got %d", code)
	}
}

// authorizeTestQuery builds a valid /dashboard/oauth/authorize query string
// for clientID/redirectURI, used as the baseline every T18 Authorize test
// tweaks one param of.
func authorizeTestQuery(clientID, redirectURI string) string {
	return "response_type=code&client_id=" + clientID +
		"&redirect_uri=" + redirectURI +
		"&code_challenge=abc123&code_challenge_method=S256&state=xyz"
}

// TestAuthorize_UnknownClientRejectedWithoutRedirect covers T18's Done-when:
// /authorize with an unknown client_id returns 400, no redirect (design.md
// Error Handling Strategy: invalid_client, redirecting to an unregistered
// URI would be an open-redirect risk).
func TestAuthorize_UnknownClientRejectedWithoutRedirect(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/oauth/authorize?"+authorizeTestQuery("not-a-real-client", "https://example.com/cb"), nil)
	rr := httptest.NewRecorder()

	h.Authorize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no redirect for an unknown client, got Location: %q", loc)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["error"] != "invalid_client" {
		t.Fatalf("expected error=invalid_client, got %q", body["error"])
	}
}

// TestAuthorize_MismatchedRedirectURIRejectedWithoutRedirect covers T18's
// Done-when: /authorize with a redirect_uri not matching the registered
// client returns 400, no redirect.
func TestAuthorize_MismatchedRedirectURIRejectedWithoutRedirect(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)
	client, err := RegisterClient(context.Background(), pool, RegisterClientInput{
		Name:         "cli",
		RedirectURIs: []string{"https://example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/oauth/authorize?"+authorizeTestQuery(client.ID, "https://attacker.example.com/cb"), nil)
	rr := httptest.NewRecorder()

	h.Authorize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no redirect for a mismatched redirect_uri, got Location: %q", loc)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["error"] != "invalid_request" {
		t.Fatalf("expected error=invalid_request, got %q", body["error"])
	}
}

// TestAuthorize_NoSessionRedirectsToLoginPreservingParams covers T18's
// Done-when: /authorize with no active session redirects to login,
// preserving OAuth params for after login completes (spec P1-OAuth AC3).
func TestAuthorize_NoSessionRedirectsToLoginPreservingParams(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)
	client, err := RegisterClient(context.Background(), pool, RegisterClientInput{
		Name:         "cli",
		RedirectURIs: []string{"https://example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	target := "/dashboard/oauth/authorize?" + authorizeTestQuery(client.ID, "https://example.com/cb")
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rr := httptest.NewRecorder()

	h.Authorize(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/dashboard/login?return_to=") {
		t.Fatalf("expected a redirect to the login page with return_to, got %q", loc)
	}
	decoded, err := url.QueryUnescape(strings.TrimPrefix(loc, "/dashboard/login?return_to="))
	if err != nil {
		t.Fatalf("unescape return_to: %v", err)
	}
	if decoded != target {
		t.Fatalf("expected return_to to preserve the original OAuth request exactly, got %q, want %q", decoded, target)
	}
}

// TestAuthorize_ActiveSessionHandsOffToConsent covers T18's Done-when:
// /authorize with an active session reaches the consent step (hands off to
// T19; this task's own test asserts the handoff happens, not the consent
// UI itself).
func TestAuthorize_ActiveSessionHandsOffToConsent(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)
	client, err := RegisterClient(context.Background(), pool, RegisterClientInput{
		Name:         "cli",
		RedirectURIs: []string{"https://example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	userID := testUser(t, pool, "authorize-consent@example.com", "admin")
	sessionToken := "session-token-for-authorize-consent-test"
	if err := CreateSession(context.Background(), pool, sessionToken, userID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	query := authorizeTestQuery(client.ID, "https://example.com/cb")
	req := httptest.NewRequest(http.MethodGet, "/dashboard/oauth/authorize?"+query, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionToken})
	rr := httptest.NewRecorder()

	h.Authorize(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/dashboard/oauth/consent?") {
		t.Fatalf("expected a redirect handing off to the consent screen, got %q", loc)
	}
	locURL, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if locURL.Query().Get("client_id") != client.ID {
		t.Fatalf("expected client_id to be preserved in the consent handoff, got %q", locURL.Query().Get("client_id"))
	}
	if locURL.Query().Get("code_challenge") != "abc123" {
		t.Fatalf("expected code_challenge to be preserved in the consent handoff, got %q", locURL.Query().Get("code_challenge"))
	}
	// P1-OAuth AC3 / design.md Risks & Concerns: the consent screen must
	// name the requesting client, not just show the redirect target — the
	// handoff carries the registered client's name for OAuthConsent.tsx to
	// render.
	if locURL.Query().Get("client_name") != client.Name {
		t.Fatalf("expected client_name to be preserved in the consent handoff, got %q, want %q", locURL.Query().Get("client_name"), client.Name)
	}
}

// TestDecide_DenyRedirectsWithAccessDeniedAndNoCodeIssued covers T19's
// Done-when: denying redirects to redirect_uri with error=access_denied,
// confirmed no code row was created (spec.md P1-OAuth edge case: admin
// denies consent).
func TestDecide_DenyRedirectsWithAccessDeniedAndNoCodeIssued(t *testing.T) {
	pool := oauthClientTestPool(t)
	h := NewOAuthHandler(pool)
	client, err := RegisterClient(context.Background(), pool, RegisterClientInput{
		Name:         "cli",
		RedirectURIs: []string{"https://example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	admin, err := CreateUser(context.Background(), pool, "decide-deny@example.com", "e2e admin", "hash", "admin")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	decideBody, _ := json.Marshal(map[string]any{
		"client_id":             client.ID,
		"redirect_uri":          "https://example.com/cb",
		"code_challenge":        "abc123",
		"code_challenge_method": "S256",
		"state":                 "xyz",
		"decision":              "deny",
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/authorize", bytes.NewReader(decideBody))
	req = req.WithContext(ContextWithUser(req.Context(), admin))
	rr := httptest.NewRecorder()

	h.Decide(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("decide (deny): expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp decideResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal decide response: %v", err)
	}
	redirectURL, err := url.Parse(resp.RedirectURL)
	if err != nil {
		t.Fatalf("parse redirect_url: %v", err)
	}
	if redirectURL.Query().Get("error") != "access_denied" {
		t.Fatalf("expected error=access_denied in the deny redirect, got %q", resp.RedirectURL)
	}
	if redirectURL.Query().Get("code") != "" {
		t.Fatalf("expected no code param on a denied consent, got %q", resp.RedirectURL)
	}
	if redirectURL.Query().Get("state") != "xyz" {
		t.Fatalf("expected state to be preserved on the deny redirect, got %q", resp.RedirectURL)
	}

	var codeCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.oauth_auth_codes WHERE client_id = $1`, client.ID).Scan(&codeCount); err != nil {
		t.Fatalf("count oauth_auth_codes: %v", err)
	}
	if codeCount != 0 {
		t.Fatalf("expected zero auth code rows after a deny, got %d", codeCount)
	}
}

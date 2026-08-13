package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

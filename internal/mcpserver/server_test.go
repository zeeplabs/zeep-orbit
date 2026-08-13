package mcpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// bearerTransport injects a static Authorization: Bearer <token> header on
// every outbound request — used so tests can drive a real
// mcp.StreamableClientTransport against NewHandler with a PAT, the way an
// external MCP client authenticates per design.md.
type bearerTransport struct {
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func connectClient(ctx context.Context, url, token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: &bearerTransport{token: token}},
	}
	return client.Connect(ctx, transport, nil)
}

// TestNewHandler_ValidPAT_InitializeHandshakeSucceeds covers T9's Done-when:
// "/dashboard/mcp responds to an MCP client's initialize handshake with a
// valid PAT, using the SDK's own streamable-HTTP client". T9's original
// "zero tools registered" assertion was scoped to that task only (its own
// Done-when says "this task only proves transport+auth") — superseded now
// that T10 registers the first tools; tool-registry coverage moved to
// tools_test.go.
func TestNewHandler_ValidPAT_InitializeHandshakeSucceeds(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "mcp-handshake@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	rl := dashboard.NewRateLimiter(100, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, rl))
	defer srv.Close()

	sess, err := connectClient(context.Background(), srv.URL, token)
	if err != nil {
		t.Fatalf("expected a successful initialize handshake with a valid PAT, got error: %v", err)
	}
	defer sess.Close()

	if _, err := sess.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
}

// TestNewHandler_InvalidPAT_RejectedBeforeMCPLayer covers T9's Done-when:
// "the same handshake without a valid PAT is rejected before reaching the
// MCP protocol layer (401, per T2)".
func TestNewHandler_InvalidPAT_RejectedBeforeMCPLayer(t *testing.T) {
	pool := authTestPool(t)
	rl := dashboard.NewRateLimiter(100, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, rl))
	defer srv.Close()

	if _, err := connectClient(context.Background(), srv.URL, "not-a-real-token"); err == nil {
		t.Fatal("expected the initialize handshake to fail without a valid PAT")
	}
}

// TestNewHandler_RateLimitExceeded_RejectsNthCallKeyedByPATID covers T9's
// Done-when: "A PAT that exceeds its rate-limit window is rejected on the
// Nth call within the window, keyed by PAT id (two different PATs each get
// their own budget)". Driven with raw HTTP requests rather than a full MCP
// client, since the rate limiter and RequirePAT run ahead of the MCP
// protocol layer — the assertion only needs the request to reach (or be
// rejected by) that middleware chain, not a valid JSON-RPC body.
func TestNewHandler_RateLimitExceeded_RejectsNthCallKeyedByPATID(t *testing.T) {
	pool := authTestPool(t)
	ownerA := authTestUser(t, pool, "mcp-rl-a@example.com")
	ownerB := authTestUser(t, pool, "mcp-rl-b@example.com")
	tokenA, _, err := dashboard.CreatePAT(context.Background(), pool, ownerA.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT A: %v", err)
	}
	tokenB, _, err := dashboard.CreatePAT(context.Background(), pool, ownerB.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT B: %v", err)
	}

	rl := dashboard.NewRateLimiter(2, time.Minute)
	srv := httptest.NewServer(NewHandler(pool, rl))
	defer srv.Close()

	doRequest := func(t *testing.T, token string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	for i := 0; i < 2; i++ {
		if code := doRequest(t, tokenA); code == http.StatusTooManyRequests {
			t.Fatalf("request %d for PAT A was unexpectedly rate-limited", i+1)
		}
	}
	if code := doRequest(t, tokenA); code != http.StatusTooManyRequests {
		t.Fatalf("expected the 3rd request within the window for PAT A to be rate-limited (429), got %d", code)
	}

	if code := doRequest(t, tokenB); code == http.StatusTooManyRequests {
		t.Fatal("expected PAT B to have its own rate-limit budget, unaffected by PAT A's, got 429")
	}
}

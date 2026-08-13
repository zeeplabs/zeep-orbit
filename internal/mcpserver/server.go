package mcpserver

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// NewHandler builds the /dashboard/mcp transport: an MCP server — zero
// tools registered yet, RegisterTools starts wiring them in T10 — wrapped
// first by RequirePAT (bearer-token auth, resolving to the same
// DashboardUser shape dashboard.RequireAuth produces) and then by the
// caller-supplied RateLimiter, keyed by the resolved PAT's id rather than
// remote IP. This mirrors the inbound-webhook route's per-webhook-id keying
// (internal/server/server.go): one noisy PAT can't exhaust every other
// token's budget behind the same non-sticky load balancer.
//
// The underlying StreamableHTTPHandler runs in Stateless mode: this service
// runs multiple stateless replicas behind a non-sticky load balancer
// (AGENTS.md), and the transport's normal stateful mode ties a session id
// to whichever replica's in-memory Server handled the initialize handshake
// — a later call for that session landing on a different replica would
// find no matching session there. Stateless mode re-derives a session per
// request instead, consistent with ResolvePAT's own no-caching, hit-the-DB-
// every-time design.
func NewHandler(pool *db.Pool, rl *dashboard.RateLimiter) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "zeep-orbit", Version: "1.0.0"}, nil)
	RegisterTools(server, ToolDeps{Pool: pool})

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	var handler http.Handler = streamable
	handler = rl.MiddlewareKeyedBy(patIDKey)(handler)
	handler = RequirePAT(pool)(handler)
	return handler
}

// patIDKey is the RateLimiter key function for the /dashboard/mcp route —
// keys by the resolved PAT's id, set in context by RequirePAT which always
// runs before this middleware in NewHandler's wrapping order.
func patIDKey(r *http.Request) string {
	id, _ := PATIDFromContext(r.Context())
	return id
}

// Package mcpserver hosts the MCP-layer HTTP transport for Zeep Orbit —
// the bearer-token (PAT) equivalent of the dashboard package's
// cookie-based RequireAuth, plus (in later tasks) the MCP tool registry
// and server bootstrap. See .specs/features/mcp-server/design.md.
package mcpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type mcpCtxKey int

const patIDCtxKey mcpCtxKey = 0

// PATIDFromContext retrieves the resolved PAT's id from context, set by
// RequirePAT alongside the DashboardUser — used by the rate limiter (T9) to
// key per-PAT instead of per-IP.
func PATIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(patIDCtxKey).(string)
	return id, ok
}

// writeUnauthorized mirrors dashboard's writeUnauthorized response shape
// (401, {"error":"unauthorized"}) — RequirePAT is a sibling of RequireAuth,
// not a different error contract.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
}

// RequirePAT is the MCP-layer equivalent of dashboard.RequireAuth: it
// resolves a `Authorization: Bearer <token>` header to a DashboardUser via
// dashboard.ResolvePAT instead of the zeep_session cookie, then injects that
// user into context using the exact same key dashboard.UserFromContext
// reads (dashboard.ContextWithUser) — so every extracted *ForUser operation
// function works identically regardless of which auth path produced the
// user. A successful resolution also fires TouchLastUsed in a background
// goroutine (never blocking the response on it — best-effort bookkeeping,
// not part of the auth decision).
func RequirePAT(pool *db.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(authz, prefix) {
				writeUnauthorized(w)
				return
			}
			token := strings.TrimPrefix(authz, prefix)
			if token == "" {
				writeUnauthorized(w)
				return
			}

			user, patID, err := dashboard.ResolvePATWithID(r.Context(), pool, token)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			go func(id string) {
				_ = dashboard.TouchLastUsed(context.Background(), pool, id) //nolint:errcheck
			}(patID)

			ctx := dashboard.ContextWithUser(r.Context(), user)
			ctx = context.WithValue(ctx, patIDCtxKey, patID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

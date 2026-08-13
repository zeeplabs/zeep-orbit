package dashboard

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type dashCtxKey int

const userCtxKey dashCtxKey = 0
const poolCtxKey dashCtxKey = 1

const cookieName = "zeep_session"

// UserFromContext retrieves the authenticated DashboardUser from context.
func UserFromContext(ctx context.Context) (*DashboardUser, bool) {
	u, ok := ctx.Value(userCtxKey).(*DashboardUser)
	return u, ok
}

// Returns 401 JSON if missing, invalid, or expired.
func RequireAuth(pool *db.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			user, err := GetSessionUser(r.Context(), pool, cookie.Value)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
}

// SecurityHeaders adds minimal security response headers to all dashboard responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is a simple per-IP sliding-window rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rlEntry
	max     int
	window  time.Duration
}

type rlEntry struct {
	count       int
	windowStart time.Time
}

// NewRateLimiter creates a limiter allowing max requests per window per IP.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		entries: make(map[string]*rlEntry),
		max:     max,
		window:  window,
	}
}

// rlSweepThreshold bounds how large entries can grow before a stale-entry
// sweep runs. Without this, a limiter keyed by anything an unauthenticated
// caller controls (e.g. a made-up webhook id — see MiddlewareKeyedBy) grows
// one entry per distinct key ever seen and never shrinks, since allow only
// ever touches the one key of the current request.
const rlSweepThreshold = 10_000

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if len(rl.entries) >= rlSweepThreshold {
		rl.sweep(now)
	}
	e, ok := rl.entries[key]
	if !ok || now.Sub(e.windowStart) > rl.window {
		rl.entries[key] = &rlEntry{count: 1, windowStart: now}
		return true
	}
	e.count++
	return e.count <= rl.max
}

// sweep drops every entry whose window has already expired. Caller holds rl.mu.
func (rl *RateLimiter) sweep(now time.Time) {
	for key, e := range rl.entries {
		if now.Sub(e.windowStart) > rl.window {
			delete(rl.entries, key)
		}
	}
}

// Middleware returns an http.Handler middleware that enforces the rate
// limit per source IP (remoteIP). Behind a non-sticky load balancer that
// terminates client connections, remoteIP is the LB's address, not the
// client's — fine for the dashboard's own login/API routes, which sit
// behind auth and see one LB per deployment, but wrong for anything
// unauthenticated that should be limited per logical caller instead; see
// MiddlewareKeyedBy.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return rl.MiddlewareKeyedBy(remoteIP)(next)
}

// MiddlewareKeyedBy returns a middleware that rate-limits by whatever keyFn
// returns for the request, instead of remoteIP. Used by the public webhook
// route (server.go): keying by webhookId scopes the budget to one webhook
// subscription, so one noisy or misbehaving provider can only exhaust its
// own budget, never every tenant's webhooks sharing an IP behind the LB —
// and unlike remoteIP, it doesn't depend on trusting a client-supplied
// X-Forwarded-For header.
func (rl *RateLimiter) MiddlewareKeyedBy(keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(keyFn(r)) {
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

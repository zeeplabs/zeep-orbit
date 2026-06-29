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

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	e, ok := rl.entries[ip]
	if !ok || now.Sub(e.windowStart) > rl.window {
		rl.entries[ip] = &rlEntry{count: 1, windowStart: now}
		return true
	}
	e.count++
	return e.count <= rl.max
}

// Middleware returns an http.Handler middleware that enforces the rate limit.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(remoteIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

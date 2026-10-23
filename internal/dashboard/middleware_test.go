package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// TestRateLimiter_AllowsUpToMaxThenBlocks: F3 (Opus pre-release review,
// independent Verifier addendum) — RateLimiter had zero direct tests, so a
// mutant removing the webhook route's rate limiter entirely still passed the
// full suite. Exercises the limiter itself, one IP, within a single window.
func TestRateLimiter_AllowsUpToMaxThenBlocks(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Middleware(next)

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/hooks/x/y", nil)
		req.RemoteAddr = "203.0.113.5:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (within limit)", i, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/hooks/x/y", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request: status = %d, want 429 (over limit)", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected a Retry-After header on the 429 response")
	}
}

// TestRateLimiter_SeparateIPsHaveIndependentBudgets: a different remote
// address must not be blocked by another IP's exhausted budget.
func TestRateLimiter_SeparateIPsHaveIndependentBudgets(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Middleware(next)

	req1 := httptest.NewRequest(http.MethodGet, "/hooks/x/y", nil)
	req1.RemoteAddr = "203.0.113.5:1"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("IP1 first request: status = %d, want 200", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/hooks/x/y", nil)
	req2.RemoteAddr = "198.51.100.9:1"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("IP2 first request: status = %d, want 200 (independent budget from IP1)", w2.Code)
	}
}

// TestRequireAuth_DatabaseErrorIsNotUnauthorized: the dashboard sends the user to the
// login screen on {"error":"unauthorized"}, so a failing session lookup (database down)
// must not answer that body. Uses a pool pointed at a closed port: no database needed.
func TestRequireAuth_DatabaseErrorIsNotUnauthorized(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://orbit@127.0.0.1:1/orbit?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	called := false
	handler := RequireAuth(&db.Pool{Pool: p})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/apps", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "some-session-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Fatal("next handler ran although the session could not be checked")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if strings.Contains(w.Body.String(), "unauthorized") {
		t.Fatalf("body = %s, must not be the expired-session body", w.Body.String())
	}
}

// TestRequireAuth_NoCookieIsUnauthorized: without a session cookie the answer stays the
// 401 body the dashboard treats as an expired session.
func TestRequireAuth_NoCookieIsUnauthorized(t *testing.T) {
	handler := RequireAuth(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler ran without a session")
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/api/apps", nil))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"unauthorized"`) {
		t.Fatalf("got %d %s, want 401 unauthorized", w.Code, w.Body.String())
	}
}

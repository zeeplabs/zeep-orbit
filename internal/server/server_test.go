package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// newTestServer creates a Server with empty registry and nil pool, without Start().
func newTestServer(t *testing.T) *Server {
	t.Helper()
	reg := registry.New()
	s, err := New(reg, nil, 0)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return s
}

func TestServerHealth(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

// TestServerEnduserRolesRouteRegistered covers ROLECFG-02: PUT
// /dashboard/api/apps/{id}/roles must be reachable through the real chi
// router (not just callable by invoking the handler directly). With no
// session cookie, RequireAuth rejects the request with 401 before ever
// touching the (nil) pool — a 404 here would mean chi never matched the
// route at all, so 401 is the proof the route is wired up.
func TestServerEnduserRolesRouteRegistered(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/dashboard/api/apps/some-app-id/roles", strings.NewReader(`{"roles":["member"]}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT .../roles error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (route registered, rejected by auth)", resp.StatusCode)
	}
}

// TestServerUpdateTablePolicyRouteRegistered covers table-policy-edit T4: PUT
// /dashboard/api/apps/{id}/tables/{table}/policies/{policyId} must be
// reachable through the real chi router (not just callable by invoking the
// handler directly). With no session cookie, RequireAuth rejects the
// request with 401 before ever touching the (nil) pool — a 404 here would
// mean chi never matched the route at all, so 401 is the proof the route is
// wired up (same pattern as TestServerEnduserRolesRouteRegistered above).
func TestServerUpdateTablePolicyRouteRegistered(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/dashboard/api/apps/some-app-id/tables/some-table/policies/some-policy-id", strings.NewReader(`{"name":"p","action":"select","roles":["member"],"clauses":[]}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT .../policies/{policyId} error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (route registered, rejected by auth)", resp.StatusCode)
	}
}

// TestServerUpdateWebhookRouteRegistered covers the M3 fix (Opus pre-release
// review): PATCH /dashboard/api/apps/{id}/webhooks/{webhookId} must be
// reachable through the real chi router. Same 401-proves-registered pattern
// as TestServerUpdateTablePolicyRouteRegistered — the Opus Verifier's
// mutation sensor found this route's wiring had zero coverage (renaming the
// route in server.go still passed the full suite).
func TestServerUpdateWebhookRouteRegistered(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/dashboard/api/apps/some-app-id/webhooks/some-webhook-id", strings.NewReader(`{"name":"x","method":"POST","event_type_path":"eventType"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH .../webhooks/{webhookId} error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (route registered, rejected by auth)", resp.StatusCode)
	}
}

// TestIsWebhookPath / TestRedactWebhookToken / TestIsDashboardWebhookTokenPath:
// direct unit coverage for the path predicates logMiddleware relies on to
// keep webhook tokens out of application logs (B1). The independent Verifier
// found these had zero direct tests — only exercised indirectly through
// production code, so a mutant flipping either predicate's logic still
// passed the full suite.
func TestIsWebhookPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/hooks/abc/def", true},
		{"/hooks/", true},
		{"/dashboard/api/apps/x/webhooks", false},
		{"/health", false},
	}
	for _, c := range cases {
		if got := isWebhookPath(c.path); got != c.want {
			t.Errorf("isWebhookPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRedactWebhookToken(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/hooks/abc123/secret-token-value", "/hooks/abc123/***"},
		{"/hooks/abc123", "/hooks/abc123"}, // too few segments to have a token
		{"/dashboard/api/apps/x/webhooks", "/dashboard/api/apps/x/webhooks"},
	}
	for _, c := range cases {
		if got := redactWebhookToken(c.path); got != c.want {
			t.Errorf("redactWebhookToken(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestIsDashboardWebhookTokenPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/dashboard/api/apps/app1/webhooks", true},
		{"/dashboard/api/apps/app1/webhooks/wh1", true},
		{"/dashboard/api/apps/app1/webhooks/wh1/rotate-token", true},
		{"/dashboard/api/apps/app1/webhooks/wh1/mappings", false},
		{"/dashboard/api/apps/app1/webhooks/wh1/mappings/m1", false},
		{"/dashboard/api/apps/app1/webhooks/wh1/deliveries", false},
		{"/dashboard/api/apps/app1/webhooks/wh1/activate", false},
		{"/dashboard/api/apps/app1/tables/foo/policies", false},
		{"/hooks/wh1/token1", false},
	}
	for _, c := range cases {
		if got := isDashboardWebhookTokenPath(c.path); got != c.want {
			t.Errorf("isDashboardWebhookTokenPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture: F1/B1 —
// direct proof that logMiddleware never stores request/response bodies for
// either the public /hooks/ route or the dashboard's webhook-token-bearing
// endpoints, while still capturing bodies for an unrelated JSON endpoint.
func TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture(t *testing.T) {
	buf := dashboard.NewRingBuffer(10)
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token":"super-secret-plaintext-token"}`))
	})
	handler := logMiddleware(zap.NewNop(), buf)(echo)

	cases := []struct {
		name           string
		path           string
		wantBodyLogged bool
	}{
		{"public hooks route", "/hooks/wh1/plaintext-token-in-url", false},
		{"dashboard webhook list/create", "/dashboard/api/apps/app1/webhooks", false},
		{"dashboard webhook get/update/delete", "/dashboard/api/apps/app1/webhooks/wh1", false},
		{"dashboard webhook rotate-token", "/dashboard/api/apps/app1/webhooks/wh1/rotate-token", false},
		{"dashboard webhook mappings (no token in response)", "/dashboard/api/apps/app1/webhooks/wh1/mappings", true},
		{"unrelated dashboard endpoint", "/dashboard/api/apps/app1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			entries := buf.Recent(1, "", nil)
			if len(entries) != 1 {
				t.Fatalf("expected 1 recent log entry, got %d", len(entries))
			}
			last := entries[0]
			gotBodyLogged := last.ResBody != ""
			if gotBodyLogged != c.wantBodyLogged {
				t.Errorf("path %q: response body logged = %v, want %v (entry: %+v)", c.path, gotBodyLogged, c.wantBodyLogged, last)
			}
		})
	}
}

func TestServerMetrics(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got %q", ct)
	}
}

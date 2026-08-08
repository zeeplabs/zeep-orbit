package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

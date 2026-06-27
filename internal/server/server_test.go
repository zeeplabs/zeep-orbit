package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-core/internal/registry"
)

// newTestServer cria um Server com registry vazio e pool nil, sem Start().
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

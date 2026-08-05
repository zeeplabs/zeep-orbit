package render

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		apiKey:     "test-key",
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}
}

func TestListDeploys_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/services/srv-123/deploys" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "3" {
			t.Fatalf("expected limit=3, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[
			{"deploy": {"id": "dep-1", "status": "live", "createdAt": "2026-08-05T10:00:00Z", "finishedAt": "2026-08-05T10:02:00Z"}},
			{"deploy": {"id": "dep-2", "status": "build_failed", "createdAt": "2026-08-04T10:00:00Z"}}
		]`))
	})

	deploys, err := client.ListDeploys(context.Background(), "srv-123", 3, []string{"live", "build_failed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deploys) != 2 {
		t.Fatalf("expected 2 deploys, got %d", len(deploys))
	}
	if deploys[0].ID != "dep-1" || deploys[0].Status != "live" {
		t.Fatalf("unexpected first deploy: %+v", deploys[0])
	}
	if deploys[0].FinishedAt == nil {
		t.Fatalf("expected FinishedAt to be set for dep-1")
	}
	if deploys[1].FinishedAt != nil {
		t.Fatalf("expected FinishedAt to be nil for dep-2, got %v", deploys[1].FinishedAt)
	}
	wantCreated := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	if !deploys[0].CreatedAt.Equal(wantCreated) {
		t.Fatalf("unexpected CreatedAt: %v", deploys[0].CreatedAt)
	}
}

func TestListDeploys_NotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "service not found"}`))
	})

	_, err := client.ListDeploys(context.Background(), "srv-gone", 3, []string{"live"})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

func TestListDeploys_RateLimited(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message": "rate limited"}`))
	})

	_, err := client.ListDeploys(context.Background(), "srv-123", 3, []string{"live"})
	if err == nil {
		t.Fatal("expected error for 429 response, got nil")
	}
}

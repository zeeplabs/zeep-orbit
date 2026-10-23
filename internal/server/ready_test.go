package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// TestHandleReady covers the readiness probe (multi-replica-consistency
// T13): 200 while Postgres answers, 503 within the 2s ping timeout when it
// doesn't.
func TestHandleReady(t *testing.T) {
	if testPool == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}

	t.Run("healthy pool returns 200", func(t *testing.T) {
		h := NewHandler(testPool, testReg)
		rec := httptest.NewRecorder()
		h.HandleReady(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ready"`) {
			t.Fatalf("got %d %s, want 200 ready", rec.Code, rec.Body.String())
		}
	})

	t.Run("closed pool returns 503", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool, err := db.New(ctx, os.Getenv("TEST_DATABASE_URL"))
		if err != nil {
			t.Fatalf("db.New: %v", err)
		}
		pool.Close()

		h := NewHandler(pool, testReg)
		rec := httptest.NewRecorder()
		start := time.Now()
		h.HandleReady(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"unavailable"`) {
			t.Fatalf("got %d %s, want 503 unavailable", rec.Code, rec.Body.String())
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("readiness took %s, want under the 2s timeout plus margin", elapsed)
		}
	})
}

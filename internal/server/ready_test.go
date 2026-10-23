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

// TestHandleReady_SaturatedMainPool: with every main-pool connection busy,
// /ready still answers 200 through its own pool, instead of timing out while
// waiting for a main-pool connection and pulling a healthy pod out.
func TestHandleReady_SaturatedMainPool(t *testing.T) {
	if testPool == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	main, err := db.NewWithMaxConns(ctx, os.Getenv("TEST_DATABASE_URL"), 1)
	if err != nil {
		t.Fatalf("NewWithMaxConns: %v", err)
	}
	defer main.Close()
	busy, err := main.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer busy.Release()

	readyPool, err := main.NewLockPool(ctx, 1)
	if err != nil {
		t.Fatalf("NewLockPool: %v", err)
	}
	defer readyPool.Close()

	h := NewHandler(main, testReg)
	h.SetReadyPool(readyPool)
	rec := httptest.NewRecorder()
	h.HandleReady(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200 with a saturated main pool", rec.Code, rec.Body.String())
	}
}

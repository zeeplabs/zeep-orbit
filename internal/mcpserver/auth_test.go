package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func authTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := dashboard.ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.dashboard_pats, zeep_system.dashboard_users CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	return pool
}

func authTestUser(t *testing.T, pool *db.Pool, email string) *dashboard.DashboardUser {
	t.Helper()
	user, err := dashboard.CreateUser(context.Background(), pool, email, "test user", "hash", "admin")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user
}

// TestRequirePAT_ValidTokenReachesHandlerWithUser covers T2's Done-when:
// a valid PAT reaches the wrapped handler with dashboard.UserFromContext(ctx)
// returning the correct user (spec MCP-02).
func TestRequirePAT_ValidTokenReachesHandlerWithUser(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "requirepat-valid@example.com")
	token, _, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	var gotUserID string
	var handlerCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		u, ok := dashboard.UserFromContext(r.Context())
		if !ok {
			t.Fatal("expected dashboard.UserFromContext to return a user")
		}
		gotUserID = u.ID
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/dashboard/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	RequirePAT(pool)(next).ServeHTTP(rr, req)

	if !handlerCalled {
		t.Fatal("expected the wrapped handler to be called for a valid PAT")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotUserID != owner.ID {
		t.Fatalf("expected resolved user id %s, got %s", owner.ID, gotUserID)
	}
}

// TestRequirePAT_RejectsBeforeHandlerRuns covers T2's Done-when: missing
// header, non-Bearer scheme, and an unresolvable token all return 401 before
// the wrapped handler runs (spec MCP-03) — asserted via a spy handler that
// must never be called.
func TestRequirePAT_RejectsBeforeHandlerRuns(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "requirepat-reject@example.com")
	_, patRow, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	if err := dashboard.RevokePAT(context.Background(), pool, owner.ID, patRow.ID); err != nil {
		t.Fatalf("RevokePAT: %v", err)
	}

	cases := []struct {
		name      string
		authValue string // "" means no Authorization header at all
	}{
		{"missing header", ""},
		{"non-bearer scheme", "Basic dXNlcjpwYXNz"},
		{"unresolvable token", "Bearer not-a-real-token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spyCalled := false
			spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				spyCalled = true
			})

			req := httptest.NewRequest(http.MethodPost, "/dashboard/mcp", nil)
			if tc.authValue != "" {
				req.Header.Set("Authorization", tc.authValue)
			}
			rr := httptest.NewRecorder()

			RequirePAT(pool)(spy).ServeHTTP(rr, req)

			if spyCalled {
				t.Fatal("expected the wrapped handler to never be called")
			}
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rr.Code)
			}
		})
	}
}

// TestRequirePAT_TouchLastUsedFiresWithoutBlockingResponse covers T2's
// Done-when: TouchLastUsed runs without changing/blocking the response, and
// its effect (last_used_at set) is eventually observable — proving it fires
// rather than silently being skipped.
func TestRequirePAT_TouchLastUsedFiresWithoutBlockingResponse(t *testing.T) {
	pool := authTestPool(t)
	owner := authTestUser(t, pool, "requirepat-touch@example.com")
	token, patRow, err := dashboard.CreatePAT(context.Background(), pool, owner.ID, "cli", dashboard.PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/dashboard/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	RequirePAT(pool)(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (response must not be affected by TouchLastUsed), got %d", rr.Code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		list, err := dashboard.ListPATs(context.Background(), pool, owner.ID)
		if err != nil {
			t.Fatalf("ListPATs: %v", err)
		}
		if len(list) == 1 && list[0].ID == patRow.ID && list[0].LastUsedAt != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected last_used_at to be set by the async TouchLastUsed call within 2s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

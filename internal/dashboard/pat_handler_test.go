package dashboard

// pat_handler_test.go — T13 of mcp-server: exercises the session-authenticated
// /dashboard/api/me/pats handlers (CreatePAT, ListPATs, RevokePAT) via the
// real HTTP handlers, same depth as webhooks_handler_test.go: happy path +
// validation error + ownership-scoping (IDOR) + audit_log side effect on
// every mutation.
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// patHandlerTestPool provisions zeep_system and seeds two dashboard users
// ("owner" and "other") so ownership-scoping (RevokePAT) has a real second
// account to test against.
func patHandlerTestPool(t *testing.T) (*db.Pool, *Handler, *DashboardUser, *DashboardUser) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("drop zeep_system: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	owner, err := CreateUser(ctx, pool, fmt.Sprintf("pat-owner-%d@example.com", time.Now().UnixNano()), "Owner", "hash", "admin")
	if err != nil {
		pool.Close()
		t.Fatalf("create owner: %v", err)
	}
	other, err := CreateUser(ctx, pool, fmt.Sprintf("pat-other-%d@example.com", time.Now().UnixNano()), "Other", "hash", "admin")
	if err != nil {
		pool.Close()
		t.Fatalf("create other: %v", err)
	}

	h := NewHandler(pool, registry.New(), zap.NewNop())
	return pool, h, owner, other
}

// TestCreatePATHandler_HappyPath_ReturnsTokenOnceAndAudits covers T13's
// Done-when: "POST /dashboard/api/me/pats with a name creates a PAT,
// response includes the plaintext token exactly once" and the pat.create
// audit_log entry (mcp-server spec MCP-01, MCP-05).
func TestCreatePATHandler_HappyPath_ReturnsTokenOnceAndAudits(t *testing.T) {
	pool, h, owner, _ := patHandlerTestPool(t)
	defer pool.Close()

	req := httptest.NewRequest(http.MethodPost, "/dashboard/api/me/pats", bytes.NewReader([]byte(`{"name":"laptop"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, owner)
	w := httptest.NewRecorder()
	h.CreatePAT(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
	var resp createPATResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a non-empty plaintext token in the create response")
	}
	if resp.Name != "laptop" {
		t.Errorf("name = %q, want %q", resp.Name, "laptop")
	}
	if bytes.Contains(w.Body.Bytes(), []byte("token_hash")) {
		t.Error("create response leaked the token_hash field")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'pat.create' AND resource_id = $1`,
		resp.ID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for pat.create = %d, want 1", auditCount)
	}
}

// TestCreatePATHandler_MissingNameReturns400 covers the name-required
// validation guard.
func TestCreatePATHandler_MissingNameReturns400(t *testing.T) {
	pool, h, owner, _ := patHandlerTestPool(t)
	defer pool.Close()

	req := httptest.NewRequest(http.MethodPost, "/dashboard/api/me/pats", bytes.NewReader([]byte(`{"name":""}`)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, owner)
	w := httptest.NewRecorder()
	h.CreatePAT(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// TestListPATsHandler_ListsCallersTokensWithoutValue covers T13's
// Done-when: "GET /dashboard/api/me/pats lists the caller's tokens, no
// token value in the response".
func TestListPATsHandler_ListsCallersTokensWithoutValue(t *testing.T) {
	pool, h, owner, other := patHandlerTestPool(t)
	defer pool.Close()

	if _, _, err := CreatePAT(context.Background(), pool, owner.ID, "owner-token", PATKindManual, nil); err != nil {
		t.Fatalf("seed owner PAT: %v", err)
	}
	if _, _, err := CreatePAT(context.Background(), pool, other.ID, "other-token", PATKindManual, nil); err != nil {
		t.Fatalf("seed other PAT: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/me/pats", nil)
	req = withUser(req, owner)
	w := httptest.NewRecorder()
	h.ListPATs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var rows []PATRow
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "owner-token" {
		t.Fatalf("expected exactly the caller's own 1 PAT, got %+v", rows)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("token_hash")) {
		t.Error("list response leaked the token_hash field")
	}
}

// TestRevokePATHandler_AnotherUsersPATReturns404 covers T13's Done-when:
// "DELETE /dashboard/api/me/pats/{id} for another user's PAT id returns
// 404/403, not success" (ownership-scoping / IDOR test).
func TestRevokePATHandler_AnotherUsersPATReturns404(t *testing.T) {
	pool, h, owner, other := patHandlerTestPool(t)
	defer pool.Close()

	_, otherRow, err := CreatePAT(context.Background(), pool, other.ID, "other-token", PATKindManual, nil)
	if err != nil {
		t.Fatalf("seed other's PAT: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/api/me/pats/"+otherRow.ID, nil)
	req = withUser(req, owner)
	req = withChiParams(req, map[string]string{"patId": otherRow.ID})
	w := httptest.NewRecorder()
	h.RevokePAT(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}

	// Confirm it genuinely was not revoked.
	rows, err := ListPATs(context.Background(), pool, other.ID)
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(rows) != 1 || rows[0].RevokedAt != nil {
		t.Fatalf("expected other's PAT to remain unrevoked, got %+v", rows)
	}
}

// TestRevokePATHandler_OwnPAT_RevokesAndAudits covers the RevokePAT happy
// path and its pat.revoke audit_log entry (mcp-server spec MCP-05).
func TestRevokePATHandler_OwnPAT_RevokesAndAudits(t *testing.T) {
	pool, h, owner, _ := patHandlerTestPool(t)
	defer pool.Close()

	_, row, err := CreatePAT(context.Background(), pool, owner.ID, "owner-token", PATKindManual, nil)
	if err != nil {
		t.Fatalf("seed owner's PAT: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/api/me/pats/"+row.ID, nil)
	req = withUser(req, owner)
	req = withChiParams(req, map[string]string{"patId": row.ID})
	w := httptest.NewRecorder()
	h.RevokePAT(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	rows, err := ListPATs(context.Background(), pool, owner.ID)
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(rows) != 1 || rows[0].RevokedAt == nil {
		t.Fatalf("expected owner's PAT to be revoked, got %+v", rows)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'pat.revoke' AND resource_id = $1`,
		row.ID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for pat.revoke = %d, want 1", auditCount)
	}
}

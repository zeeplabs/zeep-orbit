package dashboard

// oauth_client_handler_test.go — exercises the session-authenticated
// /dashboard/api/oauth-clients handlers (ListOAuthClients, DeleteOAuthClient)
// via the real HTTP handlers: superadmin-only gate, happy path, and the
// oauth_client.delete audit_log side effect. Same depth/pattern as
// pat_handler_test.go.
//
// Skips if TEST_DATABASE_URL is not set.

import (
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

// oauthClientHandlerTestPool provisions zeep_system and seeds a superadmin
// and a regular admin, so the role gate has a real non-superadmin account
// to test against.
func oauthClientHandlerTestPool(t *testing.T) (*db.Pool, *Handler, *DashboardUser, *DashboardUser) {
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

	super, err := CreateUser(ctx, pool, fmt.Sprintf("oauth-clients-super-%d@example.com", time.Now().UnixNano()), "Super", "hash", "superadmin")
	if err != nil {
		pool.Close()
		t.Fatalf("create superadmin: %v", err)
	}
	admin, err := CreateUser(ctx, pool, fmt.Sprintf("oauth-clients-admin-%d@example.com", time.Now().UnixNano()), "Admin", "hash", "admin")
	if err != nil {
		pool.Close()
		t.Fatalf("create admin: %v", err)
	}

	h := NewHandler(pool, registry.New(), zap.NewNop())
	return pool, h, super, admin
}

// TestListOAuthClientsHandler_NonSuperadminForbidden covers the role gate:
// a regular admin (not superadmin) gets 403, same instance-wide-config
// scoping as deploy_provider_config.go/github_config.go.
func TestListOAuthClientsHandler_NonSuperadminForbidden(t *testing.T) {
	pool, h, _, admin := oauthClientHandlerTestPool(t)
	defer pool.Close()

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/oauth-clients", nil)
	req = withUser(req, admin)
	w := httptest.NewRecorder()
	h.ListOAuthClients(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
}

// TestListOAuthClientsHandler_SuperadminListsRegisteredClients covers the
// happy path: a superadmin sees every self-registered client.
func TestListOAuthClientsHandler_SuperadminListsRegisteredClients(t *testing.T) {
	pool, h, super, _ := oauthClientHandlerTestPool(t)
	defer pool.Close()

	if _, err := RegisterClient(context.Background(), pool, RegisterClientInput{
		Name:         "Claude Desktop",
		RedirectURIs: []string{"https://claude.ai/api/mcp/oauth/callback"},
	}); err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/oauth-clients", nil)
	req = withUser(req, super)
	w := httptest.NewRecorder()
	h.ListOAuthClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var rows []OAuthClient
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Claude Desktop" {
		t.Fatalf("expected exactly the registered client, got %+v", rows)
	}
}

// TestDeleteOAuthClientHandler_NonSuperadminForbidden covers the role gate
// on the delete path too.
func TestDeleteOAuthClientHandler_NonSuperadminForbidden(t *testing.T) {
	pool, h, _, admin := oauthClientHandlerTestPool(t)
	defer pool.Close()

	client, err := RegisterClient(context.Background(), pool, RegisterClientInput{
		Name:         "cli",
		RedirectURIs: []string{"https://example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/api/oauth-clients/"+client.ID, nil)
	req = withUser(req, admin)
	req = withChiParams(req, map[string]string{"clientId": client.ID})
	w := httptest.NewRecorder()
	h.DeleteOAuthClient(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
	if _, err := GetClient(context.Background(), pool, client.ID); err != nil {
		t.Fatalf("expected client to still exist after forbidden delete attempt, GetClient: %v", err)
	}
}

// TestDeleteOAuthClientHandler_SuperadminDeletesAndCascades covers the
// happy path plus the ON DELETE CASCADE lifecycle guarantee
// (oauth_client_store.go's DeleteOAuthClient doc comment): deleting a
// client also deletes every outstanding auth code and revokes every
// access/refresh token pair it ever issued, and records an audit_log entry.
func TestDeleteOAuthClientHandler_SuperadminDeletesAndCascades(t *testing.T) {
	pool, h, super, _ := oauthClientHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	client, err := RegisterClient(ctx, pool, RegisterClientInput{
		Name:         "cli",
		RedirectURIs: []string{"https://example.com/cb"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	userID := testUser(t, pool, fmt.Sprintf("oauth-cascade-%d@example.com", time.Now().UnixNano()), "admin")
	if _, _, err := CreateAuthCode(ctx, pool, client.ID, userID, "challenge", "https://example.com/cb"); err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	_, patRow, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/api/oauth-clients/"+client.ID, nil)
	req = withUser(req, super)
	req = withChiParams(req, map[string]string{"clientId": client.ID})
	w := httptest.NewRecorder()
	h.DeleteOAuthClient(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if _, err := GetClient(ctx, pool, client.ID); err == nil {
		t.Fatal("expected the client to be gone after delete")
	}

	var codeCount, patCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.oauth_auth_codes WHERE client_id = $1`, client.ID).Scan(&codeCount); err != nil {
		t.Fatalf("count auth codes: %v", err)
	}
	if codeCount != 0 {
		t.Errorf("expected the client's auth codes to cascade-delete, found %d", codeCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.dashboard_pats WHERE id = $1`, patRow.ID).Scan(&patCount); err != nil {
		t.Fatalf("count dashboard_pats: %v", err)
	}
	if patCount != 0 {
		t.Errorf("expected the client's oauth PAT row to cascade-delete, found %d", patCount)
	}

	var auditCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'oauth_client.delete' AND resource_id = $1`,
		client.ID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit_log rows for oauth_client.delete = %d, want 1", auditCount)
	}
}

// TestDeleteOAuthClientHandler_UnknownClientReturns404 covers the not-found
// path.
func TestDeleteOAuthClientHandler_UnknownClientReturns404(t *testing.T) {
	pool, h, super, _ := oauthClientHandlerTestPool(t)
	defer pool.Close()

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/api/oauth-clients/does-not-exist", nil)
	req = withUser(req, super)
	req = withChiParams(req, map[string]string{"clientId": "does-not-exist"})
	w := httptest.NewRecorder()
	h.DeleteOAuthClient(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

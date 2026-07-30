package dashboard

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// githubHandlerTestPool provisions the full zeep_system schema (via
// ProvisionZeepSystem, the same idempotent migration path used in
// production) so tests can exercise dashboard_users, audit_log, and
// github_app_config together. Tables relevant to this test file are
// truncated before every test for isolation.
func githubHandlerTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision schema: %v", err)
	}

	truncate := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.audit_log`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.github_app_config`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.github_templates CASCADE`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.dashboard_users CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)

	return pool
}

// roundTripFunc adapts a function to http.RoundTripper, letting tests mock
// GitHub API responses without any real network dependency.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mockGitHubClient(handler func(req *http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: roundTripFunc(handler)}
}

func mockJSONResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func newGitHubTestHandler(pool *db.Pool, httpClient *http.Client) *GitHubConfigHandler {
	return &GitHubConfigHandler{
		pool:       pool,
		httpClient: httpClient,
	}
}

func mustCreateSuperadmin(t *testing.T, pool *db.Pool) *DashboardUser {
	t.Helper()
	u, err := CreateUser(context.Background(), pool, fmt.Sprintf("admin-%d@example.com", time.Now().UnixNano()), "Admin", "hash", "superadmin")
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	return u
}

// mustCreateRegularUser creates a non-superadmin dashboard user. The role
// check constraint only allows 'admin' or 'superadmin' — "admin" is the
// non-superadmin role used to test the forbidden path.
func mustCreateRegularUser(t *testing.T, pool *db.Pool) *DashboardUser {
	t.Helper()
	u, err := CreateUser(context.Background(), pool, fmt.Sprintf("user-%d@example.com", time.Now().UnixNano()), "User", "hash", "admin")
	if err != nil {
		t.Fatalf("create regular user: %v", err)
	}
	return u
}

func withUser(r *http.Request, u *DashboardUser) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
}

// genTestKeyPEM generates a fresh RSA key pair, PKCS1-PEM encoded, for use as
// a fake GitHub App private key in tests. VerifyAppCredentials/GetInstallation
// call internal/github.generateAppJWT under the hood, which requires a
// well-formed, parseable RSA private key (the mock GitHub server never
// actually validates the JWT signature — only this local signing step does).
func genTestKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test rsa key: %v", err)
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

func TestUpsertConfig_ValidCredentialsPersist(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()

	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://api.github.com/app" {
			t.Errorf("unexpected request to %s", req.URL.String())
		}
		return mockJSONResp(http.StatusOK, `{"id":1,"slug":"my-app"}`), nil
	})
	h := newGitHubTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"app_id":         "12345",
		"app_slug":       "my-app",
		"client_id":      "Iv1.abc",
		"client_secret":  "secret",
		"private_key":    keyPEM,
		"webhook_secret": "whsecret",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/config", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()

	h.UpsertConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, secretField := range []string{"client_secret", "private_key", "webhook_secret"} {
		if _, present := resp[secretField]; present {
			t.Errorf("response leaked secret field %q", secretField)
		}
	}

	cfg, err := GetGitHubConfig(context.Background(), pool)
	if err != nil || cfg == nil {
		t.Fatalf("GetGitHubConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.AppSlug != "my-app" {
		t.Errorf("AppSlug = %q, want my-app", cfg.AppSlug)
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", total)
	}
	if entries[0].Action != "github.config.update" {
		t.Errorf("audit action = %q, want github.config.update", entries[0].Action)
	}
}

func TestUpsertConfig_InvalidCredentialsRejectedNotPersisted(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()

	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResp(http.StatusUnauthorized, `{"message":"Bad credentials"}`), nil
	})
	h := newGitHubTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"app_id":      "12345",
		"app_slug":    "my-app",
		"client_id":   "Iv1.abc",
		"private_key": keyPEM,
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/config", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()

	h.UpsertConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	cfg, err := GetGitHubConfig(context.Background(), pool)
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected no config persisted after rejected credentials, got %+v", cfg)
	}
}

func TestUpsertConfig_EmptyPrivateKeyValidatesAgainstExisting(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()

	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	var calls int
	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		calls++
		return mockJSONResp(http.StatusOK, `{"id":1,"slug":"my-app"}`), nil
	})
	h := newGitHubTestHandler(pool, client)

	initialBody, _ := json.Marshal(map[string]string{
		"app_id":      "12345",
		"app_slug":    "my-app",
		"client_id":   "Iv1.abc",
		"private_key": keyPEM,
	})
	req1 := withUser(httptest.NewRequest(http.MethodPost, "/api/github/config", strings.NewReader(string(initialBody))), admin)
	w1 := httptest.NewRecorder()
	h.UpsertConfig(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("initial config: status = %d, body = %s", w1.Code, w1.Body.String())
	}

	// Second update: only app_slug changes, private_key is empty ("keep
	// existing"). Validation must still run, against the STORED key, not
	// skip validation and not fail on an empty key.
	partialBody, _ := json.Marshal(map[string]string{
		"app_id":   "12345",
		"app_slug": "my-app-renamed",
	})
	req2 := withUser(httptest.NewRequest(http.MethodPost, "/api/github/config", strings.NewReader(string(partialBody))), admin)
	w2 := httptest.NewRecorder()
	h.UpsertConfig(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("partial update: status = %d, want 200; body = %s", w2.Code, w2.Body.String())
	}
	if calls < 2 {
		t.Errorf("expected VerifyAppCredentials to be called on the partial update too, calls = %d", calls)
	}

	cfg, err := GetGitHubConfig(context.Background(), pool)
	if err != nil || cfg == nil {
		t.Fatalf("GetGitHubConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.AppSlug != "my-app-renamed" {
		t.Errorf("AppSlug = %q, want my-app-renamed", cfg.AppSlug)
	}
	if cfg.PrivateKey != keyPEM {
		t.Error("PrivateKey was not preserved across the partial update")
	}
}

func TestUpsertConfig_NonSuperadminForbidden(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()

	user := mustCreateRegularUser(t, pool)
	h := newGitHubTestHandler(pool, nil)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/config", strings.NewReader(`{}`)), user)
	w := httptest.NewRecorder()

	h.UpsertConfig(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestStatus_NotConfigured(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	h := newGitHubTestHandler(pool, nil)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/status", nil), admin)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp["connected"] != false || resp["configured"] != false {
		t.Errorf("resp = %+v, want connected=false configured=false", resp)
	}
}

func TestStatus_ConfiguredNotInstalled(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	h := newGitHubTestHandler(pool, nil)
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/status", nil), admin)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp["connected"] != false || resp["configured"] != true {
		t.Errorf("resp = %+v, want connected=false configured=true", resp)
	}
}

func TestStatus_RevokedInstallationReportsDisconnected(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := UpdateGitHubInstallation(context.Background(), pool, 999, "acme-org", time.Now()); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	// Simulate a revoked installation: the installation token exchange fails.
	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResp(http.StatusUnauthorized, `{"message":"Bad credentials"}`), nil
	})
	h := newGitHubTestHandler(pool, client)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/status", nil), admin)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp["connected"] != false {
		t.Errorf("connected = %v, want false for revoked installation", resp["connected"])
	}
	if resp["configured"] != true {
		t.Errorf("configured = %v, want true", resp["configured"])
	}
}

func TestStatus_Connected(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := UpdateGitHubInstallation(context.Background(), pool, 999, "acme-org", time.Now()); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResp(http.StatusCreated, fmt.Sprintf(`{"token":"tok","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))), nil
	})
	h := newGitHubTestHandler(pool, client)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/status", nil), admin)
	w := httptest.NewRecorder()
	h.Status(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp["connected"] != true {
		t.Errorf("connected = %v, want true", resp["connected"])
	}
	if resp["org_login"] != "acme-org" {
		t.Errorf("org_login = %v, want acme-org", resp["org_login"])
	}
}

func TestInstallStart_RequiresConfiguredAppSlug(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	h := newGitHubTestHandler(pool, nil)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/install/start", nil), admin)
	w := httptest.NewRecorder()
	h.InstallStart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when not configured", w.Code)
	}
}

func TestInstallStart_ReturnsInstallURL(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	h := newGitHubTestHandler(pool, nil)
	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/install/start", nil), admin)
	w := httptest.NewRecorder()
	h.InstallStart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if !strings.HasPrefix(resp["install_url"], "https://github.com/apps/my-app/installations/new?state=") {
		t.Errorf("install_url = %q, unexpected format", resp["install_url"])
	}
}

func TestInstallCallback_PersistsInstallationAndAudits(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://api.github.com/app/installations/999" {
			t.Errorf("unexpected url %s", req.URL.String())
		}
		return mockJSONResp(http.StatusOK, `{"id":999,"account":{"login":"acme-org"}}`), nil
	})
	h := newGitHubTestHandler(pool, client)

	state, err := signGitHubState([]byte(keyPEM))
	if err != nil {
		t.Fatalf("signGitHubState: %v", err)
	}

	q := url.Values{}
	q.Set("installation_id", "999")
	q.Set("setup_action", "install")
	q.Set("state", state)
	req := httptest.NewRequest(http.MethodGet, "/api/github/install/callback?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	h.InstallCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect; body = %s", w.Code, w.Body.String())
	}

	cfg, err := GetGitHubConfig(context.Background(), pool)
	if err != nil || cfg == nil {
		t.Fatalf("GetGitHubConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.InstallationID == nil || *cfg.InstallationID != 999 {
		t.Errorf("InstallationID = %v, want 999", cfg.InstallationID)
	}
	if cfg.OrgLogin != "acme-org" {
		t.Errorf("OrgLogin = %q, want acme-org", cfg.OrgLogin)
	}
	if cfg.InstalledAt == nil {
		t.Error("InstalledAt not set")
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 audit entry, got %d", total)
	}
	if entries[0].Action != "github.install" {
		t.Errorf("audit action = %q, want github.install", entries[0].Action)
	}
	if entries[0].UserID != "" {
		t.Errorf("audit UserID = %q, want empty (no session on this flow)", entries[0].UserID)
	}
}

// The install-flow state is now a stateless signed token (verified against
// the App's private key, time-limited only) instead of a single-use
// in-memory map entry — same trade-off already accepted for the Google OAuth
// login flow (google.go), required so the callback validates correctly
// regardless of which replica handled the earlier /install/start request
// behind a non-sticky, multi-replica load balancer. This means a state token
// is no longer rejected on reuse within its validity window; only expiry (see
// TestInstallCallback_RejectsExpiredState) and signature mismatch (see
// TestInstallCallback_RejectsInvalidState) are checked.
func TestInstallCallback_AllowsStateReuseWithinExpiryWindow(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResp(http.StatusOK, `{"id":999,"account":{"login":"acme-org"}}`), nil
	})
	h := newGitHubTestHandler(pool, client)

	state, err := signGitHubState([]byte(keyPEM))
	if err != nil {
		t.Fatalf("signGitHubState: %v", err)
	}
	q := url.Values{}
	q.Set("installation_id", "999")
	q.Set("state", state)
	path := "/api/github/install/callback?" + q.Encode()

	w1 := httptest.NewRecorder()
	h.InstallCallback(w1, httptest.NewRequest(http.MethodGet, path, nil))
	if w1.Code != http.StatusFound {
		t.Fatalf("first callback: status = %d, want 302; body = %s", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	h.InstallCallback(w2, httptest.NewRequest(http.MethodGet, path, nil))
	if w2.Code != http.StatusFound {
		t.Fatalf("replayed callback: status = %d, want 302 (stateless token has no single-use tracking)", w2.Code)
	}
}

func TestInstallCallback_RejectsExpiredState(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	h := newGitHubTestHandler(pool, nil)

	payload, err := json.Marshal(githubStateClaims{ExpiresAt: time.Now().Add(-1 * time.Minute).Unix()})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(keyPEM))
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	expiredState := encoded + "." + sig

	q := url.Values{}
	q.Set("installation_id", "999")
	q.Set("state", expiredState)
	req := httptest.NewRequest(http.MethodGet, "/api/github/install/callback?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	h.InstallCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (expired state must be rejected)", w.Code)
	}
}

func TestInstallCallback_RejectsInvalidState(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	h := newGitHubTestHandler(pool, nil)

	q := url.Values{}
	q.Set("installation_id", "999")
	q.Set("state", "not-a-real-state-token")
	req := httptest.NewRequest(http.MethodGet, "/api/github/install/callback?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	h.InstallCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestDeleteConfig_RemovesRowAndAudits(t *testing.T) {
	pool := githubHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	h := newGitHubTestHandler(pool, nil)
	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/github/config", nil), admin)
	w := httptest.NewRecorder()
	h.DeleteConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	cfg, err := GetGitHubConfig(context.Background(), pool)
	if err != nil {
		t.Fatalf("GetGitHubConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected config removed, got %+v", cfg)
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 1 || entries[0].Action != "github.config.delete" {
		t.Fatalf("expected 1 github.config.delete audit entry, got total=%d entries=%+v", total, entries)
	}
	if entries[0].UserID != admin.ID {
		t.Errorf("audit UserID = %q, want %q", entries[0].UserID, admin.ID)
	}
}

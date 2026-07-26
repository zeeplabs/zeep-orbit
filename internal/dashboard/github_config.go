// This file implements the GitHub App configuration and installation
// endpoints: saving/validating App credentials, reporting connectivity
// status, running the GitHub App installation flow (start + callback), and
// disconnecting. It mirrors the structure of GoogleOAuthHandler in
// google.go: a small handler struct holding the DB pool and an in-memory
// CSRF state store for the installation redirect flow.
package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/github"
)

// githubInstallState is a single-use, time-limited CSRF token issued right
// before redirecting the superadmin to GitHub's "install this App" page, and
// consumed by the installation callback. Same shape as googleState in
// google.go, kept separate since it belongs to an unrelated flow.
type githubInstallState struct {
	token     string
	expiresAt time.Time
}

// GitHubConfigHandler serves the superadmin-only GitHub App configuration
// and installation endpoints under /dashboard/api/github/*.
type GitHubConfigHandler struct {
	pool     *db.Pool
	states   map[string]*githubInstallState
	statesMu sync.Mutex

	// httpClient overrides the HTTP client used by internal/github.Client
	// instances built by this handler. nil in production (github.NewClient
	// falls back to its own default timeout client); tests set this to a
	// client wired to a mock transport so no real network call reaches
	// api.github.com.
	httpClient *http.Client
}

// NewGitHubConfigHandler builds a GitHubConfigHandler backed by pool.
func NewGitHubConfigHandler(pool *db.Pool) *GitHubConfigHandler {
	return &GitHubConfigHandler{
		pool:   pool,
		states: make(map[string]*githubInstallState),
	}
}

type githubConfigRequest struct {
	AppID         string `json:"app_id"`
	AppSlug       string `json:"app_slug"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	PrivateKey    string `json:"private_key"`
	WebhookSecret string `json:"webhook_secret"`
}

// UpsertConfig handles POST /dashboard/api/github/config. It validates the
// submitted App credentials against GitHub (GET /app, authenticated as the
// App itself) before persisting anything, so a typo'd App ID or private key
// never silently lands in the database.
func (h *GitHubConfigHandler) UpsertConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var body githubConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.AppID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app_id required"})
		return
	}

	existing, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// An empty PrivateKey in the request means "keep existing" (per
	// GitHubAppConfigInput's established partial-update semantics). To still
	// validate credentials in that case, verify against the existing stored
	// key rather than skipping validation or failing on an empty key.
	privateKeyToVerify := body.PrivateKey
	if privateKeyToVerify == "" {
		if existing == nil || existing.PrivateKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "private key required"})
			return
		}
		privateKeyToVerify = existing.PrivateKey
	}

	client := github.NewClient(github.AppConfig{
		AppID:         body.AppID,
		PrivateKeyPEM: []byte(privateKeyToVerify),
		HTTPClient:    h.httpClient,
	})
	if err := client.VerifyAppCredentials(r.Context()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid GitHub App credentials"})
		return
	}

	input := GitHubAppConfigInput{
		AppID:         body.AppID,
		AppSlug:       body.AppSlug,
		ClientID:      body.ClientID,
		ClientSecret:  body.ClientSecret,
		PrivateKey:    body.PrivateKey,
		WebhookSecret: body.WebhookSecret,
	}
	if err := UpsertGitHubConfig(r.Context(), h.pool, input); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	saved, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil || saved == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"app_id": body.AppID, "app_slug": body.AppSlug})
	h.audit(r.Context(), user.ID, user.Email, "github.config.update", "github_app_config", "", "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, redactGitHubConfig(saved))
}

// redactGitHubConfig strips every secret field (client_secret, private_key,
// webhook_secret) before a config is echoed back over HTTP.
func redactGitHubConfig(cfg *GitHubAppConfig) map[string]any {
	return map[string]any{
		"configured":      true,
		"app_id":          cfg.AppID,
		"app_slug":        cfg.AppSlug,
		"client_id":       cfg.ClientID,
		"org_login":       cfg.OrgLogin,
		"installation_id": cfg.InstallationID,
		"installed_at":    cfg.InstalledAt,
		"updated_at":      cfg.UpdatedAt,
	}
}

// Status handles GET /dashboard/api/github/status.
func (h *GitHubConfigHandler) Status(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	cfg, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "configured": false})
		return
	}
	if cfg.InstallationID == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "configured": true, "org_login": ""})
		return
	}

	client := github.NewClient(github.AppConfig{
		AppID:          cfg.AppID,
		InstallationID: strconv.FormatInt(*cfg.InstallationID, 10),
		PrivateKeyPEM:  []byte(cfg.PrivateKey),
		HTTPClient:     h.httpClient,
	})
	result, err := client.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"connected":  result.Connected,
		"configured": true,
		"org_login":  cfg.OrgLogin,
	})
}

// InstallStart handles GET /dashboard/api/github/install/start. It requires
// App credentials (and an app_slug) to already be configured, since the
// installation URL is keyed off the App's public slug.
func (h *GitHubConfigHandler) InstallStart(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	cfg, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if cfg == nil || cfg.AppSlug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "configure GitHub App credentials first"})
		return
	}

	state, err := h.generateState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	installURL := fmt.Sprintf(
		"https://github.com/apps/%s/installations/new?state=%s",
		url.PathEscape(cfg.AppSlug), url.QueryEscape(state),
	)
	writeJSON(w, http.StatusOK, map[string]string{"install_url": installURL})
}

// InstallCallback handles GET /dashboard/api/github/install/callback. GitHub
// redirects the superadmin's browser here directly after installation, with
// no session cookie guaranteed, so this endpoint is intentionally
// unauthenticated and relies entirely on the state token for CSRF
// protection.
func (h *GitHubConfigHandler) InstallCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	installationIDStr := q.Get("installation_id")

	if !h.validateState(state) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired state"})
		return
	}

	if installationIDStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "installation_id required"})
		return
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid installation_id"})
		return
	}

	cfg, err := GetGitHubConfig(r.Context(), h.pool)
	if err != nil || cfg == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub App not configured"})
		return
	}

	client := github.NewClient(github.AppConfig{
		AppID:         cfg.AppID,
		PrivateKeyPEM: []byte(cfg.PrivateKey),
		HTTPClient:    h.httpClient,
	})
	orgLogin, err := client.GetInstallation(r.Context(), installationIDStr)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch installation details from GitHub"})
		return
	}

	installedAt := time.Now()
	if err := UpdateGitHubInstallation(r.Context(), h.pool, installationID, orgLogin, installedAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// No authenticated user in this flow (no session cookie is guaranteed on
	// a GitHub-initiated redirect), so userID/userEmail are empty, but the
	// event itself — including which installation/org — is still recorded.
	meta, _ := json.Marshal(map[string]any{
		"installation_id": installationID,
		"org_login":       orgLogin,
		"setup_action":    q.Get("setup_action"),
	})
	h.audit(r.Context(), "", "", "github.install", "github_app_config", "", orgLogin, meta, r.RemoteAddr)

	http.Redirect(w, r, "/dashboard/integrations/github?installed=true", http.StatusFound)
}

// DeleteConfig handles DELETE /dashboard/api/github/config. It removes the
// github_app_config row entirely; github_templates rows are a separate
// table and are left untouched, so future repo-generation calls fail with
// "GitHub not connected" once there's no config to build a client from.
func (h *GitHubConfigHandler) DeleteConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if err := DeleteGitHubConfig(r.Context(), h.pool); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	h.audit(r.Context(), user.ID, user.Email, "github.config.delete", "github_app_config", "", "", nil, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// audit records an audit_log entry, swallowing the error the same way
// Handler.audit does elsewhere in this package: audit logging failures must
// never block the primary operation the caller already committed to.
func (h *GitHubConfigHandler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	_ = InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)
}

func (h *GitHubConfigHandler) generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("github: generate state: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	h.statesMu.Lock()
	h.states[token] = &githubInstallState{
		token:     token,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
	h.statesMu.Unlock()

	return token, nil
}

func (h *GitHubConfigHandler) validateState(token string) bool {
	if token == "" {
		return false
	}

	h.statesMu.Lock()
	defer h.statesMu.Unlock()

	s, ok := h.states[token]
	if !ok {
		return false
	}
	delete(h.states, token) // single-use: consumed whether or not it's still valid

	if time.Now().After(s.expiresAt) {
		return false
	}
	return true
}

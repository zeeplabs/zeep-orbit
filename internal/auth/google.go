package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

type appGoogleState struct {
	token     string
	expiresAt time.Time
	redirect  string
}

// AppGoogleHandler handles per-app Google OAuth sign-in.
type AppGoogleHandler struct {
	pool       *db.Pool
	reg        *registry.Registry
	states     map[string]*appGoogleState
	statesMu   sync.Mutex
	httpClient *http.Client
}

// NewAppGoogleHandler creates a new AppGoogleHandler.
func NewAppGoogleHandler(pool *db.Pool, reg *registry.Registry) *AppGoogleHandler {
	return &AppGoogleHandler{
		pool:       pool,
		reg:        reg,
		states:     make(map[string]*appGoogleState),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Returns the list of enabled auth providers for this app (without secrets).
func (h *AppGoogleHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	appName := chi.URLParam(r, "app")
	app, ok := h.reg.Get(appName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	providers := make(map[string]any)
	if app.Config.Auth.Providers.Email {
		providers["email"] = map[string]bool{"enabled": true}
	}
	if app.AuthProviders != nil {
		if googleCfg, ok := app.AuthProviders["google"].(map[string]any); ok {
			enabled, _ := googleCfg["enabled"].(bool)
			if enabled {
				safe := make(map[string]any)
				safe["enabled"] = true
				if v, ok := googleCfg["client_id"].(string); ok {
					safe["client_id"] = v
				}
				providers["google"] = safe
			}
		}
	}

	writeJSON(w, http.StatusOK, providers)
}

// getGoogleConfig extracts Google provider config from the app's auth_providers.
func (h *AppGoogleHandler) getGoogleConfig(app *registry.App) (clientID, clientSecret, redirectURL string, ok bool) {
	if app.AuthProviders == nil {
		return "", "", "", false
	}
	raw, hasProvider := app.AuthProviders["google"]
	if !hasProvider {
		return "", "", "", false
	}
	cfg, isMap := raw.(map[string]any)
	if !isMap {
		return "", "", "", false
	}
	enabled, _ := cfg["enabled"].(bool)
	if !enabled {
		return "", "", "", false
	}
	clientID, _ = cfg["client_id"].(string)
	clientSecret, _ = cfg["client_secret"].(string)
	redirectURL, _ = cfg["redirect_url"].(string)
	if clientID == "" {
		return "", "", "", false
	}
	return clientID, clientSecret, redirectURL, true
}

// Login handles GET /{app}/auth/google/login
func (h *AppGoogleHandler) Login(w http.ResponseWriter, r *http.Request) {
	appName := chi.URLParam(r, "app")
	app, ok := h.reg.Get(appName)
	if !ok {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	clientID, clientSecret, redirectURL, ok := h.getGoogleConfig(app)
	if !ok {
		http.Error(w, "Google OAuth not configured for this app", http.StatusServiceUnavailable)
		return
	}
	_ = clientSecret

	redirect := r.URL.Query().Get("redirect")

	state, err := h.generateState(redirect)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("redirect_uri", redirectURL)
	v.Set("response_type", "code")
	v.Set("scope", "openid email profile")
	v.Set("state", state)
	v.Set("access_type", "online")
	v.Set("prompt", "select_account")

	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+v.Encode(), http.StatusFound)
}

// Callback handles GET /{app}/auth/google/callback
func (h *AppGoogleHandler) Callback(w http.ResponseWriter, r *http.Request) {
	appName := chi.URLParam(r, "app")
	app, ok := h.reg.Get(appName)
	if !ok {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	clientID, clientSecret, redirectURL, ok := h.getGoogleConfig(app)
	if !ok {
		h.redirectOrError(w, r, "", "Google OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	frontendRedirect := ""
	if state != "" {
		if !h.validateState(state) {
			h.redirectOrError(w, r, "", "Login expired, please try again", http.StatusBadRequest)
			return
		}
		frontendRedirect = h.getStateRedirect(state)
	}

	if errorParam != "" {
		h.redirectOrError(w, r, frontendRedirect, "Authorization denied", http.StatusBadRequest)
		return
	}
	if code == "" {
		h.redirectOrError(w, r, frontendRedirect, "Invalid request", http.StatusBadRequest)
		return
	}

	token, err := h.exchangeCode(r.Context(), clientID, clientSecret, redirectURL, code)
	if err != nil {
		h.redirectOrError(w, r, frontendRedirect, "Google token exchange failed (redirect_url="+redirectURL+"): "+err.Error(), http.StatusInternalServerError)
		return
	}

	email, googleID := extractAppGoogleInfo(token)
	if email == "" || googleID == "" {
		h.redirectOrError(w, r, frontendRedirect, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	if !h.checkAllowedDomain(email, app) {
		h.redirectOrError(w, r, frontendRedirect, "Email domain not allowed", http.StatusForbidden)
		return
	}

	userID, err := h.findOrCreateAppUser(r.Context(), app.SchemaName, email, googleID)
	if err != nil {
		h.redirectOrError(w, r, frontendRedirect, "Failed to create user", http.StatusInternalServerError)
		return
	}

	jwtToken, err := IssueJWT([]byte(app.Config.Auth.JWTSecret), userID, email, appName)
	if err != nil {
		h.redirectOrError(w, r, frontendRedirect, "Failed to issue token", http.StatusInternalServerError)
		return
	}

	if frontendRedirect != "" {
		http.Redirect(w, r, frontendRedirect+"#token="+jwtToken, http.StatusFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token": jwtToken,
	})
}

func (h *AppGoogleHandler) redirectOrError(w http.ResponseWriter, r *http.Request, frontendRedirect, msg string, status int) {
	if frontendRedirect != "" {
		http.Redirect(w, r, frontendRedirect+"#error="+url.QueryEscape(msg), http.StatusFound)
		return
	}
	http.Error(w, msg, status)
}

type appGoogleTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

func (h *AppGoogleHandler) generateState(redirect string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("app-google: generate state: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	h.statesMu.Lock()
	h.states[token] = &appGoogleState{token: token, expiresAt: time.Now().Add(10 * time.Minute), redirect: redirect}
	h.statesMu.Unlock()
	return token, nil
}

func (h *AppGoogleHandler) validateState(token string) bool {
	h.statesMu.Lock()
	defer h.statesMu.Unlock()
	s, ok := h.states[token]
	if !ok {
		return false
	}
	return !time.Now().After(s.expiresAt)
}

func (h *AppGoogleHandler) getStateRedirect(token string) string {
	h.statesMu.Lock()
	defer h.statesMu.Unlock()
	s, ok := h.states[token]
	if !ok {
		return ""
	}
	delete(h.states, token)
	return s.redirect
}

func (h *AppGoogleHandler) exchangeCode(ctx context.Context, clientID, clientSecret, redirectURL, code string) (*appGoogleTokenResponse, error) {
	v := url.Values{}
	v.Set("code", code)
	v.Set("client_id", clientID)
	v.Set("client_secret", clientSecret)
	v.Set("redirect_uri", redirectURL)
	v.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(v.Encode()))
	if err != nil {
		return nil, fmt.Errorf("app-google: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("app-google: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("app-google: read response: %w", err)
	}

	var tr appGoogleTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("app-google: parse response (status=%d): %w", resp.StatusCode, err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("app-google: token error (status=%d): %s — %s", resp.StatusCode, tr.Error, tr.ErrorDesc)
	}
	return &tr, nil
}

func extractAppGoogleInfo(tr *appGoogleTokenResponse) (email, googleID string) {
	if tr.IDToken == "" {
		return "", ""
	}
	parts := strings.Split(tr.IDToken, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var info struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &info); err != nil {
		return "", ""
	}
	return info.Email, info.Sub
}

// If the list is empty, all domains are allowed.
func (h *AppGoogleHandler) checkAllowedDomain(email string, app *registry.App) bool {
	if app.AuthProviders == nil {
		return true
	}
	googleRaw, ok := app.AuthProviders["google"]
	if !ok {
		return true
	}
	googleCfg, ok := googleRaw.(map[string]any)
	if !ok {
		return true
	}
	domainsRaw, ok := googleCfg["allowed_domains"]
	if !ok {
		return true
	}
	domainsList, ok := domainsRaw.([]any)
	if !ok || len(domainsList) == 0 {
		return true
	}

	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, d := range domainsList {
		if allowed, ok := d.(string); ok && strings.EqualFold(domain, allowed) {
			return true
		}
	}
	return false
}

func (h *AppGoogleHandler) findOrCreateAppUser(ctx context.Context, schema, email, googleID string) (string, error) {
	// Check if user already exists by google_id
	var userID string
	err := h.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT id FROM %q."_auth_users" WHERE google_id = $1`, schema),
		googleID,
	).Scan(&userID)
	if err == nil {
		h.pool.Exec(ctx,
			fmt.Sprintf(`UPDATE %q."_auth_users" SET last_sign_in_at = now() WHERE id = $1`, schema),
			userID,
		)
		return userID, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	err = h.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT id FROM %q."_auth_users" WHERE email = $1`, schema),
		email,
	).Scan(&userID)
	if err == nil {
		_, err = h.pool.Exec(ctx,
			fmt.Sprintf(`UPDATE %q."_auth_users" SET google_id = $1, last_sign_in_at = now() WHERE id = $2`, schema),
			googleID, userID,
		)
		return userID, err
	}

	err = h.pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %q."_auth_users" (email, password_hash, provider, google_id) VALUES ($1, '', 'google', $2) RETURNING id`, schema),
		email, googleID,
	).Scan(&userID)
	return userID, err
}

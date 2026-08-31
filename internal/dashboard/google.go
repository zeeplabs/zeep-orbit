package dashboard

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Config is loaded lazily from DB (with env var fallback) on first request.
type GoogleOAuthHandler struct {
	pool       *db.Pool
	httpClient *http.Client
}

// cfg can be nil — config is loaded lazily from DB.
func NewGoogleOAuthHandler(pool *db.Pool, cfg *config.GoogleOAuthConfig) *GoogleOAuthHandler {
	return &GoogleOAuthHandler{
		pool:       pool,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// loadConfig returns the active Google OAuth config from DB (or env vars).
func (h *GoogleOAuthHandler) loadConfig(ctx context.Context) *config.GoogleOAuthConfig {
	resp, err := GetAuthProvider(ctx, h.pool, "google")
	if err != nil || !resp.Enabled || resp.Config == nil || string(resp.Config) == "{}" {
		return config.LoadGoogleOAuthConfig()
	}

	var cfg struct {
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		RedirectURL    string   `json:"redirect_url"`
		AllowedDomains []string `json:"allowed_domains"`
	}
	if err := json.Unmarshal(resp.Config, &cfg); err != nil {
		return config.LoadGoogleOAuthConfig()
	}

	if cfg.ClientID == "" {
		return config.LoadGoogleOAuthConfig()
	}

	return &config.GoogleOAuthConfig{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		RedirectURL:    cfg.RedirectURL,
		AllowedDomains: cfg.AllowedDomains,
	}
}

// isEnabled returns true if Google OAuth is configured.
func (h *GoogleOAuthHandler) isEnabled(ctx context.Context) bool {
	cfg := h.loadConfig(ctx)
	return cfg.ClientID != ""
}

// Login redirects the user to Google's OAuth consent screen.
func (h *GoogleOAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	cfg := h.loadConfig(r.Context())
	if cfg.ClientID == "" {
		http.Error(w, "Google OAuth is not configured", http.StatusServiceUnavailable)
		return
	}

	returnTo, _ := normalizeDashboardReturnPath(r.URL.Query().Get("return_to"))
	state, err := signGoogleState([]byte(cfg.ClientSecret), returnTo)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	v := url.Values{}
	v.Set("client_id", cfg.ClientID)
	v.Set("redirect_uri", cfg.RedirectURL)
	v.Set("response_type", "code")
	v.Set("scope", "openid email profile")
	v.Set("state", state)
	v.Set("access_type", "online")
	v.Set("prompt", "select_account")

	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+v.Encode(), http.StatusFound)
}

// Callback handles the OAuth redirect from Google.
func (h *GoogleOAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	cfg := h.loadConfig(r.Context())
	if cfg.ClientID == "" {
		h.callbackErrorPage(w, "Google OAuth is not configured")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		h.callbackErrorPage(w, "Authorization was denied. Please try again.")
		return
	}

	if code == "" || state == "" {
		h.callbackErrorPage(w, "Invalid request. Please try again.")
		return
	}

	returnTo, ok := verifyGoogleState([]byte(cfg.ClientSecret), state)
	if !ok {
		h.callbackErrorPage(w, "Session expired or invalid. Please try again.")
		return
	}

	token, err := h.exchangeCode(r.Context(), code, cfg)
	if err != nil {
		h.callbackErrorPage(w, "Google authentication failed. Please try again.")
		return
	}

	email, googleID := extractGoogleInfo(token)
	if email == "" || googleID == "" {
		h.callbackErrorPage(w, "Could not retrieve your Google account details. Please try again.")
		return
	}

	if !h.verifyDomain(email, cfg.AllowedDomains) {
		h.callbackErrorPage(w, "Your email does not belong to an authorized domain. Contact your administrator.")
		return
	}

	user, err := h.findOrCreateUser(r.Context(), email, googleID)
	if err != nil {
		h.callbackErrorPage(w, "Error creating your account. Contact your administrator.")
		return
	}

	sessionToken, err := generateToken()
	if err != nil {
		h.callbackErrorPage(w, "Internal error. Please try again.")
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	if err := CreateSession(r.Context(), h.pool, sessionToken, user.ID, expiresAt); err != nil {
		h.callbackErrorPage(w, "Error creating your session. Please try again.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionToken,
		Path:     "/dashboard",
		HttpOnly: true,
		Secure:   os.Getenv("ZEEP_INSECURE_COOKIES") != "1",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		DeleteExpiredSessions(ctx, h.pool)
	}()

	needsSetup := user.Name == "" && user.PasswordHash == ""
	if needsSetup {
		setupURL := "/dashboard/google-setup"
		if returnTo != "" {
			setupURL += "?return_to=" + url.QueryEscape(returnTo)
		}
		http.Redirect(w, r, setupURL, http.StatusFound)
		return
	}

	dest := "/dashboard"
	if returnTo != "" {
		dest += returnTo
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// googleStateClaims is the payload embedded in the OAuth "state" param,
// signed with the configured Google client secret. Stateless (no in-memory
// map) so the callback validates correctly regardless of which replica
// handled the earlier /login request — required behind a non-sticky load
// balancer running multiple replicas.
type googleStateClaims struct {
	ExpiresAt int64  `json:"exp"`
	ReturnTo  string `json:"return_to,omitempty"`
}

// normalizeDashboardReturnPath guards and normalizes a return_to value the
// same way the frontend's safeReturnTo (src/lib/returnTo.ts) does: only a
// same-origin path relative to the SPA's /dashboard basename is ever
// honored (never an absolute URL or a scheme-relative "//host/..." /
// "/\host/..." — browsers normalize the latter to the former), and any
// accidental "/dashboard" prefix is stripped so every caller can uniformly
// do "/dashboard"+path for a full-page navigation without re-deriving which
// shape it got. This is the server-side twin of that TS function — Login
// (below) receives return_to as an untrusted query param on a link the
// frontend built, so it's re-validated here rather than trusted just
// because the frontend already validated it once, and Callback trusts only
// what verifyGoogleState hands back after re-checking this same guard.
func normalizeDashboardReturnPath(path string) (normalized string, ok bool) {
	if path == "" {
		return "", false
	}
	isSafe := func(p string) bool {
		return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.HasPrefix(p, `/\`)
	}
	if !isSafe(path) {
		return "", false
	}
	if path == "/dashboard" || strings.HasPrefix(path, "/dashboard/") || strings.HasPrefix(path, "/dashboard?") {
		path = strings.TrimPrefix(path, "/dashboard")
		if path == "" {
			path = "/"
		}
	}
	if !isSafe(path) {
		return "", false
	}
	return path, true
}

func signGoogleState(secret []byte, returnTo string) (string, error) {
	payload, err := json.Marshal(googleStateClaims{
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
		ReturnTo:  returnTo,
	})
	if err != nil {
		return "", fmt.Errorf("google: marshal state: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + sig, nil
}

// verifyGoogleState validates token and, on success, returns the ReturnTo
// path it carried, normalized (already re-validated by
// normalizeDashboardReturnPath at Login time, but checked again here in
// case the signing key or claims shape ever changes — cheap, and this is
// the boundary Callback trusts).
func verifyGoogleState(secret []byte, token string) (returnTo string, ok bool) {
	encoded, sig, found := strings.Cut(token, ".")
	if !found || encoded == "" || sig == "" {
		return "", false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", false
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	var claims googleStateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return "", false
	}
	if claims.ReturnTo == "" {
		return "", true
	}
	normalized, _ := normalizeDashboardReturnPath(claims.ReturnTo)
	return normalized, true
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

type googleUserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *GoogleOAuthHandler) exchangeCode(ctx context.Context, code string, cfg *config.GoogleOAuthConfig) (*googleTokenResponse, error) {
	v := url.Values{}
	v.Set("code", code)
	v.Set("client_id", cfg.ClientID)
	v.Set("client_secret", cfg.ClientSecret)
	v.Set("redirect_uri", cfg.RedirectURL)
	v.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(v.Encode()))
	if err != nil {
		return nil, fmt.Errorf("google: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google: read token response: %w", err)
	}

	var tr googleTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("google: parse token response: %w", err)
	}

	if tr.Error != "" {
		return nil, fmt.Errorf("google: token error: %s", tr.Error)
	}

	return &tr, nil
}

func extractGoogleInfo(tr *googleTokenResponse) (email, googleID string) {
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

	var info googleUserInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		return "", ""
	}

	return info.Email, info.Sub
}

func (h *GoogleOAuthHandler) verifyDomain(email string, allowedDomains []string) bool {
	if len(allowedDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, allowed := range allowedDomains {
		if strings.EqualFold(domain, allowed) {
			return true
		}
	}
	return false
}

func (h *GoogleOAuthHandler) findOrCreateUser(ctx context.Context, email, googleID string) (*DashboardUser, error) {
	user, err := GetUserByGoogleID(ctx, h.pool, googleID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	user, err = GetUserByEmail(ctx, h.pool, email)
	if err == nil {
		if user.GoogleID == "" {
			if err := LinkGoogleID(ctx, h.pool, user.ID, googleID); err != nil {
				return nil, err
			}
			user.GoogleID = googleID
		}
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	return CreateGoogleUser(ctx, h.pool, email, googleID)
}

func (h *GoogleOAuthHandler) callbackErrorPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Login</title>
<style>
body{margin:0;display:flex;align-items:center;justify-content:center;min-height:100vh;
background:#0D0D14;font-family:-apple-system,BlinkMacSystemFont,sans-serif;color:#F8FAFC}
.card{max-width:420px;margin:16px;padding:32px;border:1px solid rgba(255,255,255,0.10);
border-radius:16px;background:rgba(13,13,20,0.6);backdrop-filter:blur(20px);text-align:center}
h2{margin:0 0 12px;font-size:18px;font-weight:700}
p{margin:0 0 24px;font-size:14px;color:#94A3B8;line-height:1.5}
a{display:inline-block;padding:10px 24px;border-radius:8px;font-size:14px;font-weight:600;
color:#fff;text-decoration:none;
background:linear-gradient(to bottom right,var(--brand-primary,#3B82F6),var(--brand-secondary,#8B5CF6))}
</style></head>
<body><div class="card"><h2>Login</h2><p>%s</p><a href="/dashboard">Try again</a></div></body>
</html>`, msg)
}

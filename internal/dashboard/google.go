package dashboard

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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type googleState struct {
	token     string
	expiresAt time.Time
}

// GoogleOAuthHandler handles Google Sign-In for the dashboard.
type GoogleOAuthHandler struct {
	pool       *db.Pool
	cfg        *config.GoogleOAuthConfig
	states     map[string]*googleState
	statesMu   sync.Mutex
	httpClient *http.Client
}

// NewGoogleOAuthHandler creates a new GoogleOAuthHandler.
func NewGoogleOAuthHandler(pool *db.Pool, cfg *config.GoogleOAuthConfig) *GoogleOAuthHandler {
	return &GoogleOAuthHandler{
		pool:       pool,
		cfg:        cfg,
		states:     make(map[string]*googleState),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Login redirects the user to Google's OAuth consent screen.
func (h *GoogleOAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := h.generateState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	v := url.Values{}
	v.Set("client_id", h.cfg.ClientID)
	v.Set("redirect_uri", h.cfg.RedirectURL)
	v.Set("response_type", "code")
	v.Set("scope", "openid email profile")
	v.Set("state", state)
	v.Set("access_type", "online")
	v.Set("prompt", "select_account")

	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+v.Encode(), http.StatusFound)
}

// Callback handles the OAuth redirect from Google.
func (h *GoogleOAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		h.callbackErrorPage(w, "A autorização foi recusada. Tente novamente.")
		return
	}

	if code == "" || state == "" {
		h.callbackErrorPage(w, "Requisição inválida. Tente novamente.")
		return
	}

	if !h.validateState(state) {
		h.callbackErrorPage(w, "Sessão expirada ou inválida. Tente novamente.")
		return
	}

	token, err := h.exchangeCode(r.Context(), code)
	if err != nil {
		h.callbackErrorPage(w, "Falha na autenticação com Google. Tente novamente.")
		return
	}

	email, googleID := extractGoogleInfo(token)
	if email == "" || googleID == "" {
		h.callbackErrorPage(w, "Não foi possível obter seus dados do Google. Tente novamente.")
		return
	}

	if !h.verifyDomain(email) {
		h.callbackErrorPage(w, "Seu email não pertence a um domínio autorizado. Contacte o administrador.")
		return
	}

	user, err := h.findOrCreateUser(r.Context(), email, googleID)
	if err != nil {
		h.callbackErrorPage(w, "Erro ao criar sua conta. Contacte o administrador.")
		return
	}

	sessionToken, err := generateToken()
	if err != nil {
		h.callbackErrorPage(w, "Erro interno. Tente novamente.")
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	if err := CreateSession(r.Context(), h.pool, sessionToken, user.ID, expiresAt); err != nil {
		h.callbackErrorPage(w, "Erro ao criar sessão. Tente novamente.")
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

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *GoogleOAuthHandler) generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("google: generate state: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	h.statesMu.Lock()
	h.states[token] = &googleState{
		token:     token,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
	h.statesMu.Unlock()

	return token, nil
}

func (h *GoogleOAuthHandler) validateState(token string) bool {
	h.statesMu.Lock()
	defer h.statesMu.Unlock()

	s, ok := h.states[token]
	if !ok {
		return false
	}
	delete(h.states, token)

	if time.Now().After(s.expiresAt) {
		return false
	}
	return true
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

func (h *GoogleOAuthHandler) exchangeCode(ctx context.Context, code string) (*googleTokenResponse, error) {
	v := url.Values{}
	v.Set("code", code)
	v.Set("client_id", h.cfg.ClientID)
	v.Set("client_secret", h.cfg.ClientSecret)
	v.Set("redirect_uri", h.cfg.RedirectURL)
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

func (h *GoogleOAuthHandler) verifyDomain(email string) bool {
	if len(h.cfg.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, allowed := range h.cfg.AllowedDomains {
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
<html lang="pt-BR">
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
<body><div class="card"><h2>Login</h2><p>%s</p><a href="/dashboard">Tentar novamente</a></div></body>
</html>`, msg)
}

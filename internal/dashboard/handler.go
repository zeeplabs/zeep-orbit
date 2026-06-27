package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"github.com/zeeplabs/zeep-core/internal/db"
)

// Handler holds dependencies for dashboard HTTP handlers.
type Handler struct {
	pool *db.Pool
}

// NewHandler creates a new Handler.
func NewHandler(pool *db.Pool) *Handler {
	return &Handler{pool: pool}
}

// Bootstrap handles POST /dashboard/api/bootstrap
// Creates the first superadmin. Requires DASHBOARD_BOOTSTRAP_SECRET env var.
func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("DASHBOARD_BOOTSTRAP_SECRET")
	if secret == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bootstrap not configured"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Secret   string `json:"secret"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.Secret != secret {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid secret"})
		return
	}

	if len(body.Password) < 12 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 12 characters"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	created, err := BootstrapFirstSuperadmin(r.Context(), h.pool, body.Email, string(hash))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if !created {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already bootstrapped"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"message": "superadmin created", "email": body.Email})
}

// Login handles POST /dashboard/api/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	user, err := GetUserByEmail(r.Context(), h.pool, body.Email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := generateToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	if err := CreateSession(r.Context(), h.pool, token, user.ID, expiresAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/dashboard",
		HttpOnly: true,
		Secure:   os.Getenv("ZEEP_INSECURE_COOKIES") != "1",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	// Lazy cleanup: purge expired sessions in the background on each login.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		DeleteExpiredSessions(ctx, h.pool) //nolint:errcheck
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]string{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

// Logout handles POST /dashboard/api/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err == nil {
		_ = DeleteSession(r.Context(), h.pool, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/dashboard",
		HttpOnly: true,
		Secure:   os.Getenv("ZEEP_INSECURE_COOKIES") != "1",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// Me handles GET /dashboard/api/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":    user.ID,
		"email": user.Email,
		"role":  user.Role,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-core/internal/db"
	"github.com/zeeplabs/zeep-core/internal/registry"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 12
	refreshTTL = 30 * 24 * time.Hour
)

// Handler serves native email/password auth endpoints per app.
type Handler struct {
	pool *db.Pool
	reg  *registry.Registry
	rl   *rateLimiter
}

// New creates an auth Handler.
func New(pool *db.Pool, reg *registry.Registry) *Handler {
	return &Handler{
		pool: pool,
		reg:  reg,
		rl:   newRateLimiter(10, time.Minute),
	}
}

// RateLimit returns the rate-limit middleware for sensitive endpoints.
func (h *Handler) RateLimit(next http.Handler) http.Handler {
	return h.rl.middleware(next)
}

// Register handles POST /{app}/auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithEmail(w, r)
	if !ok {
		return
	}

	var body struct {
		Email    string  `json:"email"`
		Password string  `json:"password"`
		Name     string  `json:"name"`
		Phone    *string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcryptCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	schema := app.SchemaName
	var userID string
	err = h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`INSERT INTO %q."_auth_users" (email, phone, password_hash, name) VALUES ($1, $2, $3, $4) RETURNING id`, schema),
		body.Email, body.Phone, string(hash), body.Name,
	).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	token, err := IssueJWT([]byte(app.Config.Auth.JWTSecret), userID, body.Email, app.Config.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"token": token})
}

// Login handles POST /{app}/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithEmail(w, r)
	if !ok {
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	schema := app.SchemaName
	var userID, passwordHash string
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT id, password_hash FROM %q."_auth_users" WHERE email = $1`, schema),
		body.Email,
	).Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query user")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if _, err := h.pool.Exec(r.Context(),
		fmt.Sprintf(`UPDATE %q."_auth_users" SET last_sign_in_at = now() WHERE id = $1`, schema),
		userID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update sign-in timestamp")
		return
	}

	token, err := IssueJWT([]byte(app.Config.Auth.JWTSecret), userID, body.Email, app.Config.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	refreshToken, err := generateRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate refresh token")
		return
	}

	_, err = h.pool.Exec(r.Context(),
		fmt.Sprintf(`INSERT INTO %q."_auth_sessions" (user_id, refresh_token, expires_at) VALUES ($1, $2, $3)`, schema),
		userID, refreshToken, time.Now().Add(refreshTTL),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":         token,
		"refresh_token": refreshToken,
	})
}

// Refresh handles POST /{app}/auth/refresh
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithEmail(w, r)
	if !ok {
		return
	}

	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token required")
		return
	}

	schema := app.SchemaName
	var sessionID, userID string
	var expiresAt time.Time
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT id, user_id, expires_at FROM %q."_auth_sessions" WHERE refresh_token = $1`, schema),
		body.RefreshToken,
	).Scan(&sessionID, &userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid refresh token")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query session")
		return
	}

	if time.Now().After(expiresAt) {
		writeError(w, http.StatusUnauthorized, "refresh token expired")
		return
	}

	var email string
	if err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT email FROM %q."_auth_users" WHERE id = $1`, schema),
		userID,
	).Scan(&email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query user")
		return
	}

	newRefresh, err := generateRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate refresh token")
		return
	}

	_, err = h.pool.Exec(r.Context(),
		fmt.Sprintf(`UPDATE %q."_auth_sessions" SET refresh_token = $1, expires_at = $2 WHERE id = $3`, schema),
		newRefresh, time.Now().Add(refreshTTL), sessionID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate session")
		return
	}

	token, err := IssueJWT([]byte(app.Config.Auth.JWTSecret), userID, email, app.Config.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token":         token,
		"refresh_token": newRefresh,
	})
}

// Logout handles POST /{app}/auth/logout (requires JWT via AuthJWTMiddleware)
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithEmail(w, r)
	if !ok {
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, err := h.pool.Exec(r.Context(),
		fmt.Sprintf(`DELETE FROM %q."_auth_sessions" WHERE user_id = $1`, app.SchemaName),
		user.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sessions")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /{app}/auth/me (requires JWT via AuthJWTMiddleware)
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithEmail(w, r)
	if !ok {
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var id, email string
	var phone, name, avatarURL *string
	var emailConfirmedAt, lastSignInAt *time.Time
	var createdAt, updatedAt time.Time
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`SELECT id, email, phone, name, avatar_url, email_confirmed_at, last_sign_in_at, created_at, updated_at FROM %q."_auth_users" WHERE id = $1`, app.SchemaName),
		user.ID,
	).Scan(&id, &email, &phone, &name, &avatarURL, &emailConfirmedAt, &lastSignInAt, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                  id,
		"email":               email,
		"phone":               phone,
		"name":                name,
		"avatar_url":          avatarURL,
		"email_confirmed_at":  emailConfirmedAt,
		"last_sign_in_at":     lastSignInAt,
		"created_at":          createdAt,
		"updated_at":          updatedAt,
	})
}

// UpdateMe handles PUT /{app}/auth/me (requires JWT via AuthJWTMiddleware)
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	app, ok := h.appWithEmail(w, r)
	if !ok {
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Name      *string `json:"name"`
		Phone     *string `json:"phone"`
		AvatarURL *string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var id, email string
	var phone, name, avatarURL *string
	var emailConfirmedAt, lastSignInAt *time.Time
	var createdAt, updatedAt time.Time
	err := h.pool.QueryRow(r.Context(),
		fmt.Sprintf(`UPDATE %q."_auth_users"
			SET name = COALESCE($1, name), phone = COALESCE($2, phone), avatar_url = COALESCE($3, avatar_url), updated_at = now()
			WHERE id = $4
			RETURNING id, email, phone, name, avatar_url, email_confirmed_at, last_sign_in_at, created_at, updated_at`, app.SchemaName),
		body.Name, body.Phone, body.AvatarURL, user.ID,
	).Scan(&id, &email, &phone, &name, &avatarURL, &emailConfirmedAt, &lastSignInAt, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                 id,
		"email":              email,
		"phone":              phone,
		"name":               name,
		"avatar_url":         avatarURL,
		"email_confirmed_at": emailConfirmedAt,
		"last_sign_in_at":    lastSignInAt,
		"created_at":         createdAt,
		"updated_at":         updatedAt,
	})
}

// appWithEmail retrieves the app and asserts email provider is enabled.
func (h *Handler) appWithEmail(w http.ResponseWriter, r *http.Request) (*registry.App, bool) {
	appName := chi.URLParam(r, "app")
	app, ok := h.reg.Get(appName)
	if !ok || !app.Config.Auth.Providers.Email {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	return app, true
}

func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

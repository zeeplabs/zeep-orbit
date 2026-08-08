package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
	"github.com/zeeplabs/zeep-orbit/internal/query"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"github.com/zeeplabs/zeep-orbit/internal/storage"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// Handler holds dependencies for dashboard HTTP handlers.
type Handler struct {
	pool   *db.Pool
	reg    *registry.Registry
	prov   *provisioner.Provisioner
	Logs   *RingBuffer
	logger *zap.Logger
}

// NewHandler creates a new Handler. logger receives detailed error context for
// every failed request so infra can diagnose issues from container logs alone;
// pass zap.NewNop() if no logger is available.
func NewHandler(pool *db.Pool, reg *registry.Registry, logger *zap.Logger) *Handler {
	bufSize := 2000
	if v := os.Getenv("DASHBOARD_LOG_BUFFER_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			bufSize = n
		}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{
		pool:   pool,
		reg:    reg,
		prov:   provisioner.New(pool),
		Logs:   NewRingBuffer(bufSize),
		logger: logger,
	}
}

func isValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeName title-cases each word: "JULIO augusto" -> "Julio Augusto".
func normalizeName(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

var (
	identRe      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	appNameRe    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	phoneE164Re  = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)
	allowedTypes = map[string]bool{
		"text": true, "integer": true, "bigint": true, "boolean": true,
		"uuid": true, "timestamptz": true, "numeric": true, "jsonb": true,
	}
)

// validateAppInput checks the app name is a safe SQL schema identifier.
func validateAppInput(name string) error {
	if !appNameRe.MatchString(name) {
		return errors.New("app name must be lowercase letters, digits, hyphens, or underscores (max 32), starting with a letter")
	}
	return nil
}

// resolveTableRLS applies the global "require RLS by default" setting: when a
// create request omits the access level and the setting is on AND the app has
// email auth (required for owner-scoped RLS), the table defaults to Restricted
// ("enabled"). An explicit access level from the client is always respected,
// and without email auth an omitted level stays Public (empty) — forcing
// Restricted there would fail provisioning (owner_id FK needs _auth_users).
func resolveTableRLS(requested string, requireRLSDefault, authEmailEnabled bool) string {
	if requested == "" && requireRLSDefault && authEmailEnabled {
		return "enabled"
	}
	return requested
}

// validateTableInput checks a single table before it reaches the provisioner
// DDL: safe identifiers, known column types, no duplicate name against the
// app's other tables (otherTables — exclude the table being updated, if any),
// no duplicate column name within the table, and the RLS×auth rule.
func validateTableInput(t AppTableRow, authEmailEnabled bool, otherTables []AppTableRow) error {
	if !identRe.MatchString(t.Name) {
		return errors.New("table name must be lowercase letters, digits, or underscores (max 63), starting with a letter")
	}
	for _, other := range otherTables {
		if other.Name == t.Name {
			return errors.New("duplicate table name: " + t.Name)
		}
	}

	// owner_id FK points at "_auth_users", which the provisioner only
	// creates when email auth is on — restricted access without it is a
	// guaranteed provisioning failure, not a soft misconfiguration.
	if (t.RLS == "enabled" || t.RLS == "owner") && !authEmailEnabled {
		return errors.New("table " + t.Name + " uses restricted access (RLS), which requires 'Autenticação por e-mail' to be enabled for this app")
	}

	seenColumns := make(map[string]bool, len(t.Columns))
	for _, c := range t.Columns {
		if !identRe.MatchString(c.Name) {
			return errors.New("column name must be lowercase letters, digits, or underscores (max 63), starting with a letter")
		}
		if !allowedTypes[c.Type] {
			return errors.New("unsupported column type: " + c.Type)
		}
		if seenColumns[c.Name] {
			return errors.New("duplicate column name in table " + t.Name + ": " + c.Name)
		}
		seenColumns[c.Name] = true
	}

	// References/indexes can only be validated against the app's full table
	// set (a reference may point at another table, a cycle spans tables).
	allTables := make([]config.TableConfig, 0, len(otherTables)+1)
	allTables = append(allTables, config.TableConfig{Name: t.Name, Columns: t.Columns, Indexes: t.Indexes})
	for _, other := range otherTables {
		allTables = append(allTables, config.TableConfig{Name: other.Name, Columns: other.Columns, Indexes: other.Indexes})
	}
	if err := config.ValidateTables(allTables); err != nil {
		return err
	}

	return nil
}

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
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	if body.Secret != secret {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid secret"})
		return
	}

	body.Email = normalizeEmail(body.Email)
	body.Name = normalizeName(body.Name)

	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}
	if !isValidEmail(body.Email) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email address"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	created, err := BootstrapFirstSuperadmin(r.Context(), h.pool, body.Email, body.Name, string(hash))
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !created {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already bootstrapped"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"message": "superadmin created", "email": body.Email})
	h.audit(r.Context(), "", body.Email, "bootstrap.complete", "user", "", body.Email, nil, r.RemoteAddr)
}

// BootstrapStatus handles GET /dashboard/api/bootstrap/status
func (h *Handler) BootstrapStatus(w http.ResponseWriter, r *http.Request) {
	ok, err := IsBootstrapped(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"bootstrapped": ok})
}

// Reads from zeep_system.brand_config, falling back to environment defaults.
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	cfg, err := GetBrandConfig(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	theme := os.Getenv("BRAND_THEME")
	if theme == "" {
		theme = "azure"
	}
	company := os.Getenv("BRAND_COMPANY_NAME")
	if company == "" {
		company = "Zeep Tecnologia"
	}

	if cfg != nil {
		theme = cfg.Theme
		company = cfg.CompanyName
	}

	googleProv, _ := GetAuthProvider(r.Context(), h.pool, "google")
	sysCfg, _ := GetSystemConfig(r.Context(), h.pool)

	writeJSON(w, http.StatusOK, map[string]any{
		"theme":                theme,
		"company_name":         company,
		"google_oauth_enabled": googleProv.Enabled,
		"storage_configured":   sysCfg != nil && sysCfg.StorageConfig != nil,
	})
}

// Updates the brand_config singleton row. Requires superadmin.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var body struct {
		Theme       string `json:"theme"`
		CompanyName string `json:"company_name"`
		LogoURL     string `json:"logo_url"`
		IconURL     string `json:"icon_url"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	validThemes := map[string]bool{"azure": true, "emerald": true, "ruby": true, "amber": true, "orange": true}
	if body.Theme != "" && !validThemes[body.Theme] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid theme"})
		return
	}

	cfg, err := UpsertBrandConfig(r.Context(), h.pool, body.Theme, body.CompanyName, body.LogoURL, body.IconURL)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, cfg)
	meta, _ := json.Marshal(body)
	h.audit(r.Context(), user.ID, user.Email, "config.update", "config", "", "", meta, r.RemoteAddr)
}

// Lists all configured auth providers. Requires superadmin.
func (h *Handler) ListAuthProviders(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	reveal := r.URL.Query().Get("reveal") == "true"
	providers, err := ListAuthProviders(r.Context(), h.pool, reveal)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, providers)
}

// Returns a single provider's config. Requires superadmin.
func (h *Handler) GetAuthProvider(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	provider := chi.URLParam(r, "provider")
	reveal := r.URL.Query().Get("reveal") == "true"

	resp, err := GetAuthProvider(r.Context(), h.pool, provider)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if !reveal {
		resp.Config = stripSecretFromConfig(provider, resp.Config)
	}

	writeJSON(w, http.StatusOK, resp)
}

// Creates or updates a provider's config. Requires superadmin. Encrypts config JSON.
func (h *Handler) UpsertAuthProvider(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	provider := chi.URLParam(r, "provider")

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var body authProviderUpsertInput
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	result, err := UpsertAuthProvider(r.Context(), h.pool, provider, &body)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update provider", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
	h.audit(r.Context(), user.ID, user.Email, "auth.provider.update", "auth_provider", provider, provider, nil, r.RemoteAddr)
}

func (h *Handler) GetSystemConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	cfg, err := GetSystemConfig(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if cfg.StorageConfig != nil {
		cfg.StorageConfig.SecretAccessKey = ""
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) UpdateSystemConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var patch systemConfigPatch
	if !h.decodeJSONBody(w, r, &patch) {
		return
	}

	current, err := GetSystemConfig(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load system config", err)
		return
	}
	merged := mergeSystemConfig(*current, patch)

	cfg, err := UpsertSystemConfig(r.Context(), h.pool, &merged)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update system config", err)
		return
	}

	if cfg.StorageConfig != nil {
		cfg.StorageConfig.SecretAccessKey = ""
	}

	h.reg.SetSystemConfig(registry.SystemConfig{
		SoftDeleteEnabled:  cfg.SoftDeleteEnabled,
		StatementTimeoutMs: cfg.StatementTimeoutMs,
	})

	writeJSON(w, http.StatusOK, cfg)
	h.audit(r.Context(), user.ID, user.Email, "config.system.update", "config", "system", "", nil, r.RemoteAddr)
}

// Login handles POST /dashboard/api/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	user, err := GetUserByEmail(r.Context(), h.pool, body.Email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)); err != nil {
		if user.PasswordHash == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "use Google to sign in"})
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := generateToken()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	if err := CreateSession(r.Context(), h.pool, token, user.ID, expiresAt); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
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

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		DeleteExpiredSessions(ctx, h.pool) //nolint:errcheck
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]string{
			"id":       user.ID,
			"email":    user.Email,
			"name":     user.Name,
			"role":     user.Role,
			"language": user.Language,
		},
	})
	h.audit(r.Context(), user.ID, user.Email, "user.login", "session", "", user.Email, nil, r.RemoteAddr)
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

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          user.ID,
		"email":       user.Email,
		"name":        user.Name,
		"role":        user.Role,
		"language":    user.Language,
		"needs_setup": user.Name == "" && user.PasswordHash == "",
	})
}

// Authenticated user changes own password (requires current password).
func (h *Handler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	if body.CurrentPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current_password is required"})
		return
	}
	if body.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new_password is required"})
		return
	}
	if len(body.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must be at least 8 characters"})
		return
	}
	if body.NewPassword != body.ConfirmPassword {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password and confirmation do not match"})
		return
	}

	fullUser, err := GetUserByEmail(r.Context(), h.pool, user.Email)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if fullUser.PasswordHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot change password for Google-only accounts. Use Google sign-in."})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(fullUser.PasswordHash), []byte(body.CurrentPassword)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), 12)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if err := UpdatePassword(r.Context(), h.pool, user.ID, string(hash)); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update password", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
	h.audit(r.Context(), user.ID, user.Email, "user.password.change", "user", user.ID, user.Email, nil, r.RemoteAddr)
}

// Superadmin changes any user's password (no current password required).
func (h *Handler) ChangeUserPassword(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id is required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	if body.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new_password is required"})
		return
	}
	if len(body.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must be at least 8 characters"})
		return
	}
	if body.NewPassword != body.ConfirmPassword {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password and confirmation do not match"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), 12)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if err := UpdatePassword(r.Context(), h.pool, targetID, string(hash)); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to update password", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
	h.audit(r.Context(), user.ID, user.Email, "user.password.change", "user", targetID, "", nil, r.RemoteAddr)
}

// ListUsers handles GET /dashboard/api/users
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	users, err := ListUsers(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if users == nil {
		users = []*DashboardUser{}
	}
	writeJSON(w, http.StatusOK, users)
}

// CreateUser handles POST /dashboard/api/users
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// admin can also manage users; the per-role target restriction (only
	// superadmin can create superadmin) is enforced separately below by
	// CanCreateUserWithRole.
	if !HasPlatformPermission(user.Role, ActionManageUsers) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	body.Email = normalizeEmail(body.Email)
	body.Name = normalizeName(body.Name)

	if body.Email == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}
	if !isValidEmail(body.Email) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email address"})
		return
	}
	switch body.Role {
	case "superadmin", "admin", "auditor", "member":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be one of: superadmin, admin, auditor, member"})
		return
	}
	// Only a superadmin can create another superadmin. Other restrictions on
	// the target role live in HasPlatformPermission; this is the only
	// function that decides "can actor X create role=Y?".
	if !CanCreateUserWithRole(user.Role, body.Role) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a superadmin can create a superadmin"})
		return
	}
	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	newUser, err := CreateUser(r.Context(), h.pool, body.Email, body.Name, string(hash), body.Role)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    newUser.ID,
		"email": newUser.Email,
		"name":  newUser.Name,
		"role":  newUser.Role,
	})
	h.audit(r.Context(), user.ID, user.Email, "user.create", "user", newUser.ID, newUser.Email, nil, r.RemoteAddr)
}

// DeleteUser handles DELETE /dashboard/api/users/{id}
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if currentUser.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	targetID := chi.URLParam(r, "id")
	if targetID == currentUser.ID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete yourself"})
		return
	}

	// ≥1 superadmin invariant. Before deleting, look up the target's role —
	// if it's a superadmin, verify at least one other superadmin exists.
	// Same helper as UpdateUserRole.
	target, err := GetUser(r.Context(), h.pool, targetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if target.Role == "superadmin" {
		if err := assertAtLeastOneSuperadminRemains(r.Context(), h.pool, targetID); err != nil {
			if errors.Is(err, errLastSuperadmin) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "platform must have at least one superadmin"})
				return
			}
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
			return
		}
	}

	if err := DeleteUser(r.Context(), h.pool, targetID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	h.audit(r.Context(), currentUser.ID, currentUser.Email, "user.delete", "user", targetID, "", nil, r.RemoteAddr)
}

// ListApps handles GET /dashboard/api/apps
func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	apps, err := ListApps(r.Context(), h.pool, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if apps == nil {
		apps = []*AppRow{}
	}
	writeJSON(w, http.StatusOK, apps)
}

// appRequestBody is the JSON body for create/update app requests. Tables are
// managed one at a time via the /apps/{id}/tables endpoints, not here.
type appRequestBody struct {
	Name             string                  `json:"name"`
	AuthEmailEnabled bool                    `json:"auth_email_enabled"`
	AuthProviders    json.RawMessage         `json:"auth_providers,omitempty"`
	StorageConfig    *storage.StorageConfig  `json:"storage_config,omitempty"`
	RateLimit        *config.RateLimitConfig `json:"rate_limit,omitempty"`
}

// tableRequestBody is the JSON body for create/update table requests.
type tableRequestBody struct {
	Name    string                `json:"name"`
	RLS     string                `json:"rls"`
	Columns []config.ColumnConfig `json:"columns"`
	Indexes []config.IndexConfig  `json:"indexes"`
}

// CreateApp handles POST /dashboard/api/apps
func (h *Handler) CreateApp(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body appRequestBody
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if err := validateAppInput(body.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	app, err := CreateApp(r.Context(), h.pool, body.Name, user.ID, body.AuthEmailEnabled)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if len(body.AuthProviders) > 0 {
		if err := UpdateAppAuthProvidersRaw(r.Context(), h.pool, app.ID, body.AuthProviders); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to save auth providers", err)
			return
		}
		app.AuthProviders = body.AuthProviders
	}

	if body.StorageConfig != nil && body.StorageConfig.Bucket != "" {
		sc, err := resolveAppStorage(r.Context(), h.pool, body.StorageConfig)
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to resolve storage config", err)
			return
		}
		if err := UpdateAppStorageConfig(r.Context(), h.pool, app.ID, sc); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to save storage config", err)
			return
		}
		app.StorageConfig = sc
	}

	cfg := buildAppConfig(app)
	if _, err := h.prov.Apply(r.Context(), &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		var typeErr *provisioner.TypeChangeError
		if errors.As(err, &typeErr) {
			h.writeError(w, r, http.StatusBadRequest, typeErr.Error(), err)
		} else {
			h.writeError(w, r, http.StatusInternalServerError, "provisioning failed — check server logs for details", err)
		}
		return
	}

	h.reg.Register(appRowToRegistryApp(app))

	app.JWTSecret = ""
	writeJSON(w, http.StatusCreated, app)
	h.audit(r.Context(), user.ID, user.Email, "app.create", "app", app.ID, app.Name, nil, r.RemoteAddr)
}

// GetApp handles GET /dashboard/api/apps/{id}
func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	app, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	app.JWTSecret = ""
	writeJSON(w, http.StatusOK, app)
}

// UpdateApp handles PUT /dashboard/api/apps/{id}
func (h *Handler) UpdateApp(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body appRequestBody
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	if err := validateAppInput(body.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// App config (auth_providers / storage_config / rate_limit_config) is
	// management-level — only admin can change it. Editor has CanWrite but
	// not CanManage, so they're blocked here even though they can edit tables.
	_, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	app, err := UpdateApp(r.Context(), h.pool, appID, user, body.AuthEmailEnabled)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if len(body.AuthProviders) > 0 {
		if err := UpdateAppAuthProvidersRaw(r.Context(), h.pool, app.ID, body.AuthProviders); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to save auth providers", err)
			return
		}
		app.AuthProviders = body.AuthProviders
	}

	if body.StorageConfig != nil && body.StorageConfig.Bucket != "" {
		sc, err := resolveAppStorage(r.Context(), h.pool, body.StorageConfig)
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to resolve storage config", err)
			return
		}
		if err := UpdateAppStorageConfig(r.Context(), h.pool, app.ID, sc); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to save storage config", err)
			return
		}
		app.StorageConfig = sc
	}

	if body.RateLimit != nil {
		if err := UpdateAppRateLimitConfig(r.Context(), h.pool, app.ID, body.RateLimit); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to save rate limit config", err)
			return
		}
		app.RateLimit = body.RateLimit
	}

	cfg := buildAppConfig(app)
	if _, err := h.prov.Apply(r.Context(), &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		var typeErr *provisioner.TypeChangeError
		if errors.As(err, &typeErr) {
			h.writeError(w, r, http.StatusBadRequest, typeErr.Error(), err)
		} else {
			h.writeError(w, r, http.StatusInternalServerError, "provisioning failed — check server logs for details", err)
		}
		return
	}

	h.reg.Register(appRowToRegistryApp(app))

	app.JWTSecret = ""
	writeJSON(w, http.StatusOK, app)
	h.audit(r.Context(), user.ID, user.Email, "app.update", "app", app.ID, app.Name, nil, r.RemoteAddr)
}

// DeleteApp handles DELETE /dashboard/api/apps/{id}
func (h *Handler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	existing, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	// Deleting an app is admin-only. Check here (same as UpdateApp) instead
	// of relying solely on the store: DeleteApp returns ErrForbidden, which
	// has to be mapped explicitly or it falls through to a 500.
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if err := DeleteApp(r.Context(), h.pool, appID, user); err != nil {
		if errors.Is(err, ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	h.reg.Unregister(existing.Name)

	w.WriteHeader(http.StatusNoContent)
	h.audit(r.Context(), user.ID, user.Email, "app.delete", "app", appID, existing.Name, nil, r.RemoteAddr)
}

// CreateAppTable handles POST /dashboard/api/apps/{id}/tables. Tables are
// created one at a time — the request only ever describes a single table,
// applied to the provisioner without touching the app's other tables.
func (h *Handler) CreateAppTable(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanWrite() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body tableRequestBody
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	sysCfg, err := GetSystemConfig(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load system config", err)
		return
	}
	body.RLS = resolveTableRLS(body.RLS, sysCfg.RequireRLSDefault, app.AuthEmailEnabled)

	table := AppTableRow{Name: body.Name, RLS: body.RLS, Columns: body.Columns, Indexes: body.Indexes}
	if err := validateTableInput(table, app.AuthEmailEnabled, app.Tables); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Provision the physical table before persisting its metadata row — if
	// Apply fails, nothing should be left behind for the dashboard to think
	// exists (a metadata row with no matching physical table blocks retrying
	// with the same name and shows a table that errors on every operation).
	cfg := buildAppConfig(app)
	cfg.Tables = []config.TableConfig{{Name: table.Name, RLS: table.RLS, Columns: table.Columns, Indexes: table.Indexes}}
	if _, err := h.prov.Apply(r.Context(), &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		var typeErr *provisioner.TypeChangeError
		if errors.As(err, &typeErr) {
			h.writeError(w, r, http.StatusBadRequest, typeErr.Error(), err)
		} else {
			h.writeError(w, r, http.StatusInternalServerError, "provisioning failed — check server logs for details", err)
		}
		return
	}

	row, err := InsertAppTable(r.Context(), h.pool, appID, table)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	updated, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	h.reg.Register(appRowToRegistryApp(updated))

	writeJSON(w, http.StatusCreated, row)
	h.audit(r.Context(), user.ID, user.Email, "app.table.create", "app_table", row.ID, app.Name+"/"+row.Name, nil, r.RemoteAddr)
}

// UpdateAppTable handles PUT /dashboard/api/apps/{id}/tables/{tableId}. Table
// name is immutable once created; only rls/columns can change here.
func (h *Handler) UpdateAppTable(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	tableID := chi.URLParam(r, "tableId")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanWrite() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	var existingTable *AppTableRow
	var otherTables []AppTableRow
	for _, t := range app.Tables {
		if t.ID == tableID {
			tCopy := t
			existingTable = &tCopy
		} else {
			otherTables = append(otherTables, t)
		}
	}
	if existingTable == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body tableRequestBody
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	table := AppTableRow{Name: existingTable.Name, RLS: body.RLS, Columns: body.Columns, Indexes: body.Indexes}
	if err := validateTableInput(table, app.AuthEmailEnabled, otherTables); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Provision the physical change before persisting metadata — if Apply
	// fails, the stored columns/indexes must still match what's actually in
	// Postgres, not a shape the physical table was never migrated to.
	cfg := buildAppConfig(app)
	cfg.Tables = []config.TableConfig{{Name: table.Name, RLS: table.RLS, Columns: table.Columns, Indexes: table.Indexes}}
	if _, err := h.prov.Apply(r.Context(), &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		var typeErr *provisioner.TypeChangeError
		if errors.As(err, &typeErr) {
			h.writeError(w, r, http.StatusBadRequest, typeErr.Error(), err)
		} else {
			h.writeError(w, r, http.StatusInternalServerError, "provisioning failed — check server logs for details", err)
		}
		return
	}

	row, err := UpdateAppTable(r.Context(), h.pool, appID, tableID, body.RLS, body.Columns, body.Indexes)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	updated, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	h.reg.Register(appRowToRegistryApp(updated))

	writeJSON(w, http.StatusOK, row)
	h.audit(r.Context(), user.ID, user.Email, "app.table.update", "app_table", row.ID, app.Name+"/"+row.Name, nil, r.RemoteAddr)
}

// DeleteAppTable handles DELETE /dashboard/api/apps/{id}/tables/{tableId}.
// Removes the metadata row and drops the physical table — unlike the old
// bulk UpdateApp flow, a removed table is never left behind in the database.
func (h *Handler) DeleteAppTable(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	tableID := chi.URLParam(r, "tableId")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanWrite() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	tableName, err := DeleteAppTable(r.Context(), h.pool, appID, tableID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if err := h.prov.DropTable(r.Context(), schemaNameForDB(app.Name), tableName); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to drop table: "+err.Error(), err)
		return
	}

	updated, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	h.reg.Register(appRowToRegistryApp(updated))

	writeJSON(w, http.StatusOK, map[string]string{"message": "table deleted"})
	h.audit(r.Context(), user.ID, user.Email, "app.table.delete", "app_table", tableID, app.Name+"/"+tableName, nil, r.RemoteAddr)
}

// findAppTableByName looks up a table by name in an already-loaded AppRow.
// Table policy routes address the table by name (not tableId, per spec's
// API shape), so every policy handler resolves the real column list this way
// before it can call into policy.Builder via CreateTablePolicy.
func findAppTableByName(app *AppRow, tableName string) *AppTableRow {
	for i := range app.Tables {
		if app.Tables[i].Name == tableName {
			return &app.Tables[i]
		}
	}
	return nil
}

// ListTablePolicies handles GET /dashboard/api/apps/{id}/tables/{table}/policies.
// Gated the same as create/delete (CanManage — admin/superadmin only), per
// spec: policy definitions are part of an app's access-control surface, not
// general read-only data any member can see.
func (h *Handler) ListTablePolicies(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	tableName := chi.URLParam(r, "table")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if findAppTableByName(app, tableName) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	policies, err := ListTablePolicies(r.Context(), h.pool, appID, tableName)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, policies)
}

// CreateTablePolicy handles POST /dashboard/api/apps/{id}/tables/{table}/policies.
func (h *Handler) CreateTablePolicy(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	tableName := chi.URLParam(r, "table")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	table := findAppTableByName(app, tableName)
	if table == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body PolicyDef
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	schemaName := schemaNameForDB(app.Name)
	row, err := CreateTablePolicy(r.Context(), h.pool, appID, schemaName, tableName, table.Columns, body, user.ID)
	if err != nil {
		var valErr *provisioner.ValidationError
		switch {
		case errors.As(err, &valErr):
			// Safe to expose: describes which clause/field failed, never
			// internal detail (AGENTS.md §4 — provisioner.ValidationError is
			// the explicit exception to "never leak err.Error() on 500s",
			// same pattern as provisioner.TypeChangeError).
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": valErr.Error()})
		case errors.Is(err, ErrPolicyAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a policy with this name already exists on this table"})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}

	writeJSON(w, http.StatusCreated, row)
	h.audit(r.Context(), user.ID, user.Email, "app.table_policy.create", "table_policy", row.ID, app.Name+"/"+tableName+"/"+row.PgPolicyName, nil, r.RemoteAddr)
}

// DeleteTablePolicy handles DELETE /dashboard/api/apps/{id}/tables/{table}/policies/{policyId}.
func (h *Handler) DeleteTablePolicy(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	tableName := chi.URLParam(r, "table")
	policyID := chi.URLParam(r, "policyId")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if findAppTableByName(app, tableName) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	schemaName := schemaNameForDB(app.Name)
	if err := DeleteTablePolicy(r.Context(), h.pool, appID, schemaName, policyID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "policy not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "policy deleted"})
	h.audit(r.Context(), user.ID, user.Email, "app.table_policy.delete", "table_policy", policyID, app.Name+"/"+tableName, nil, r.RemoteAddr)
}

// buildAppConfig converts an AppRow into a config.AppConfig for the provisioner.
func buildAppConfig(app *AppRow) config.AppConfig {
	tables := make([]config.TableConfig, 0, len(app.Tables))
	for _, t := range app.Tables {
		tables = append(tables, config.TableConfig{
			Name:    t.Name,
			RLS:     t.RLS,
			Columns: t.Columns,
		})
	}
	ac := config.AppConfig{
		Name: app.Name,
		Auth: config.AuthConfig{
			JWTSecret: app.JWTSecret,
			Providers: config.AuthProviders{Email: app.AuthEmailEnabled},
		},
		Tables: tables,
	}
	if app.StorageConfig != nil {
		ac.Storage = &config.StorageConfig{
			Bucket:          app.StorageConfig.Bucket,
			Region:          app.StorageConfig.Region,
			Endpoint:        app.StorageConfig.Endpoint,
			AccessKeyID:     app.StorageConfig.AccessKeyID,
			SecretAccessKey: app.StorageConfig.SecretAccessKey,
		}
	}
	if app.RateLimit != nil {
		ac.RateLimit = &config.RateLimitConfig{
			Enabled:           app.RateLimit.Enabled,
			RequestsPerMinute: app.RateLimit.RequestsPerMinute,
		}
	}
	return ac
}

// appRowToRegistryApp converts an AppRow into a *registry.App.
func appRowToRegistryApp(app *AppRow) *registry.App {
	tables := make(map[string]*registry.Table, len(app.Tables))
	for _, t := range app.Tables {
		cols := make([]registry.Column, 0, len(t.Columns))
		for _, c := range t.Columns {
			cols = append(cols, registry.Column{
				Name:     c.Name,
				Type:     c.Type,
				Required: c.Required,
				Default:  c.Default,
				Unique:   c.Unique,
			})
		}
		tables[t.Name] = &registry.Table{
			Name:    t.Name,
			RLS:     t.RLS,
			Columns: cols,
		}
	}

	var authProviders map[string]any
	if len(app.AuthProviders) > 0 {
		json.Unmarshal(app.AuthProviders, &authProviders)
	}

	var sc *config.StorageConfig
	if app.StorageConfig != nil {
		sc = &config.StorageConfig{
			Bucket:          app.StorageConfig.Bucket,
			Region:          app.StorageConfig.Region,
			Endpoint:        app.StorageConfig.Endpoint,
			AccessKeyID:     app.StorageConfig.AccessKeyID,
			SecretAccessKey: app.StorageConfig.SecretAccessKey,
		}
	}

	var rl *config.RateLimitConfig
	if app.RateLimit != nil {
		rl = &config.RateLimitConfig{
			Enabled:           app.RateLimit.Enabled,
			RequestsPerMinute: app.RateLimit.RequestsPerMinute,
		}
	}

	return &registry.App{
		Config:        buildAppConfig(app),
		SchemaName:    schemaNameForDB(app.Name),
		Tables:        tables,
		AuthProviders: authProviders,
		StorageConfig: sc,
		RateLimit:     rl,
	}
}

// If the request has only bucket_name and global S3 is configured, merge the
// global endpoint/region/keys into the app's storage config. The app's bucket
// name becomes a folder prefix within the global bucket.
func resolveAppStorage(ctx context.Context, pool *db.Pool, input *storage.StorageConfig) (*storage.StorageConfig, error) {
	if input == nil || input.Bucket == "" {
		return input, nil
	}

	// If all fields are provided, use as-is
	if input.Endpoint != "" && input.AccessKeyID != "" {
		return input, nil
	}

	// Try to merge with global S3 config
	cfg, err := GetSystemConfig(ctx, pool)
	if err != nil || cfg.StorageConfig == nil {
		return input, nil
	}
	global := cfg.StorageConfig
	if global.Bucket == "" || global.Region == "" || global.Endpoint == "" {
		return input, nil
	}

	appFolder := input.Bucket
	merged := &storage.StorageConfig{
		Bucket:          global.Bucket,
		Region:          global.Region,
		Endpoint:        global.Endpoint,
		AccessKeyID:     global.AccessKeyID,
		SecretAccessKey: global.SecretAccessKey,
		Folder:          appFolder,
	}

	return merged, nil
}

// Hyphens are not valid in PostgreSQL identifiers, so convert them to underscores.
func schemaNameForDB(appName string) string {
	return strings.ReplaceAll(appName, "-", "_")
}

func (h *Handler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	if err := InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip); err != nil {
	}
}

// CompleteGoogleSetup handles setting a password for a Google-provisioned
// account that doesn't have one yet, completing its dashboard login setup.
func (h *Handler) CompleteGoogleSetup(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	fullUser, err := GetUserByEmail(r.Context(), h.pool, user.Email)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if fullUser.PasswordHash != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account already has a password"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Name            string `json:"name"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password is required"})
		return
	}
	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}
	if body.Password != body.ConfirmPassword {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "passwords do not match"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if err := UpdateUserName(r.Context(), h.pool, fullUser.ID, body.Name); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if err := UpdatePassword(r.Context(), h.pool, fullUser.ID, string(hash)); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) SetLanguage(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 128)
	var body struct {
		Language string `json:"language"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Language != "pt-BR" && body.Language != "en" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "language must be 'pt-BR' or 'en'"})
		return
	}

	if err := SetUserLanguage(r.Context(), h.pool, user.ID, body.Language); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"language": body.Language})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError logs the real err (with request context) at Error level, then
// sends only publicMsg to the client so internals never leak over the wire.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, publicMsg string, err error) {
	h.logger.Error("dashboard request failed",
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.Int("status", status),
		zap.Error(err),
	)
	writeJSON(w, status, map[string]string{"error": publicMsg})
}

// decodeJSONBody decodes r.Body into v, logging and responding on failure.
// It distinguishes an oversized body (http.MaxBytesReader) from a genuinely
// malformed payload, since both previously surfaced as the same opaque
// "invalid request body" message. Returns false when the caller should return.
func (h *Handler) decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		h.writeError(w, r, http.StatusRequestEntityTooLarge, "request payload too large", err)
		return false
	}
	h.writeError(w, r, http.StatusBadRequest, "invalid request body", err)
	return false
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ListLogs handles GET /dashboard/api/logs?app=&limit=
func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	appFilter := r.URL.Query().Get("app")
	if appFilter != "" && allowedApps != nil && !allowedApps[appFilter] {
		appFilter = ""
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := parseInt(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	entries := h.Logs.Recent(limit, appFilter, allowedApps)
	if entries == nil {
		entries = []LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// LogsMetrics handles GET /dashboard/api/logs/metrics
func (h *Handler) LogsMetrics(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, h.Logs.Metrics(allowedApps))
}

// DataBrowserTableColumn represents a column in the data browser tree.
type DataBrowserTableColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DataBrowserTable represents a table in the data browser tree.
type DataBrowserTable struct {
	Name    string                   `json:"name"`
	Columns []DataBrowserTableColumn `json:"columns"`
	Count   int                      `json:"count"`
}

// DataBrowserApp represents an app in the data browser tree.
type DataBrowserApp struct {
	Name   string             `json:"name"`
	Tables []DataBrowserTable `json:"tables"`
}

// Returns apps with their tables from the registry, filtered by ownership.
func (h *Handler) ListDataBrowserApps(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	apps := h.reg.Apps()
	resp := make([]DataBrowserApp, 0, len(apps))
	for _, app := range apps {
		if allowedApps != nil && !allowedApps[app.Config.Name] {
			continue
		}
		tables := make([]DataBrowserTable, 0, len(app.Tables))
		for _, t := range app.Tables {
			cols := make([]DataBrowserTableColumn, 0, len(t.Columns)+4)
			cols = append(cols, DataBrowserTableColumn{Name: "id", Type: "uuid"})
			for _, c := range t.Columns {
				cols = append(cols, DataBrowserTableColumn{Name: c.Name, Type: c.Type})
			}
			cols = append(cols, DataBrowserTableColumn{Name: "created_at", Type: "timestamptz"})
			cols = append(cols, DataBrowserTableColumn{Name: "updated_at", Type: "timestamptz"})
			if t.RLS == "owner" {
				cols = append(cols, DataBrowserTableColumn{Name: "owner_id", Type: "uuid"})
			}

			var count int
			if q, err := query.BuildList(app.SchemaName, t.Name, t, nil, "", h.reg.SystemConfig().SoftDeleteEnabled); err == nil {
				filterArgs := q.Args[:len(q.Args)-2]
				_ = h.pool.QueryRow(r.Context(), q.CountSQL, filterArgs...).Scan(&count)
			}

			tables = append(tables, DataBrowserTable{Name: t.Name, Columns: cols, Count: count})
		}
		resp = append(resp, DataBrowserApp{Name: app.Config.Name, Tables: tables})
	}

	if resp == nil {
		resp = []DataBrowserApp{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// Executes a paginated SELECT using the existing query builder.
func (h *Handler) DataBrowserQuery(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appName := r.URL.Query().Get("app")
	tableName := r.URL.Query().Get("table")
	if appName == "" || tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app and table are required"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if allowedApps != nil && !allowedApps[appName] {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	app, ok := h.reg.Get(appName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	table, ok := app.Tables[tableName]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	params := make(map[string]string)
	for k, vals := range r.URL.Query() {
		if k == "app" || k == "table" {
			continue
		}
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	q, err := query.BuildList(app.SchemaName, tableName, table, params, "", h.reg.SystemConfig().SoftDeleteEnabled)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// COUNT
	var count int
	filterArgs := q.Args[:len(q.Args)-2]
	if err := h.pool.QueryRow(ctx, q.CountSQL, filterArgs...).Scan(&count); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to count rows", err)
		return
	}

	rows, err := h.pool.Query(ctx, q.SQL, q.Args...)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to query rows", err)
		return
	}
	data, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to collect rows", err)
		return
	}
	if data == nil {
		data = []map[string]any{}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   sanitizeData(data),
		"count":  count,
		"limit":  limit,
		"offset": offset,
	})
}

// Exports table data as CSV (max 10 000 rows). Respects the same filters as DataBrowserQuery.
func (h *Handler) DataBrowserExport(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appName := r.URL.Query().Get("app")
	tableName := r.URL.Query().Get("table")
	if appName == "" || tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app and table are required"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if allowedApps != nil && !allowedApps[appName] {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	app, ok := h.reg.Get(appName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	table, ok := app.Tables[tableName]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	exportLimit := 10000
	if sysCfg, cfgErr := GetSystemConfig(r.Context(), h.pool); cfgErr == nil && sysCfg.MaxCSVExportRows > 0 {
		exportLimit = sysCfg.MaxCSVExportRows
	}
	params := make(map[string]string)
	params["limit"] = strconv.Itoa(exportLimit)
	params["offset"] = "0"
	for k, vals := range r.URL.Query() {
		if k == "app" || k == "table" {
			continue
		}
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	q, err := query.BuildList(app.SchemaName, tableName, table, params, "", h.reg.SystemConfig().SoftDeleteEnabled)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rows, err := h.pool.Query(r.Context(), q.SQL, q.Args...)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to query rows", err)
		return
	}
	data, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to collect rows", err)
		return
	}

	sanitized := sanitizeData(data)

	colNames := make([]string, 0, len(table.Columns))
	for _, col := range table.Columns {
		colNames = append(colNames, col.Name)
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s_%s.csv"`, appName, tableName))
	if len(sanitized) == exportLimit {
		w.Header().Set("X-Truncated", "true")
	}

	cw := csv.NewWriter(w)
	_ = cw.Write(colNames)
	for _, row := range sanitized {
		record := make([]string, len(colNames))
		for i, col := range colNames {
			v := row[col]
			if v == nil {
				record[i] = ""
			} else {
				record[i] = csvSafeCell(fmt.Sprintf("%v", v))
			}
		}
		_ = cw.Write(record)
	}
	cw.Flush()
}

// with characters interpreted by spreadsheets as formulas (=, +, -, @, tab, CR).
func csvSafeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// sanitizeData converte [16]byte (UUID do pgx v5) em string UUID.
func sanitizeData(rows []map[string]any) []map[string]any {
	for i, row := range rows {
		for k, v := range row {
			if b, ok := v.([16]byte); ok {
				row[k] = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
					b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
			}
		}
		rows[i] = row
	}
	return rows
}

// sanitizeRow converts [16]byte to UUID string for a single row.
func sanitizeRow(row map[string]any) map[string]any {
	return sanitizeData([]map[string]any{row})[0]
}

type dataBrowserMutationRequest struct {
	App   string         `json:"app"`
	Table string         `json:"table"`
	ID    string         `json:"id,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

func (h *Handler) DataBrowserCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req dataBrowserMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid JSON body", err)
		return
	}
	if req.App == "" || req.Table == "" || req.Data == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app, table, and data are required"})
		return
	}

	role, err := ResolveAppRoleByName(r.Context(), h.pool, user, req.App)
	if err != nil && !errors.Is(err, ErrNotFound) {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanWrite() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	app, ok := h.reg.Get(req.App)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	table, ok := app.Tables[req.Table]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	q, err := query.BuildInsert(app.SchemaName, req.Table, table, req.Data, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rows, err := h.pool.Query(r.Context(), q.SQL, q.Args...)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to insert row: "+err.Error(), err)
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to read inserted row: "+err.Error(), err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"data": sanitizeRow(row),
	})
	h.audit(r.Context(), user.ID, user.Email, "data.create", "data", "", req.App+"/"+req.Table, nil, r.RemoteAddr)
}

func (h *Handler) DataBrowserUpdate(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req dataBrowserMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid JSON body", err)
		return
	}
	if req.App == "" || req.Table == "" || req.ID == "" || req.Data == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app, table, id, and data are required"})
		return
	}

	role, err := ResolveAppRoleByName(r.Context(), h.pool, user, req.App)
	if err != nil && !errors.Is(err, ErrNotFound) {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanWrite() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	app, ok := h.reg.Get(req.App)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	table, ok := app.Tables[req.Table]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	q, err := query.BuildUpdate(app.SchemaName, req.Table, table, req.ID, req.Data, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rows, err := h.pool.Query(r.Context(), q.SQL, q.Args...)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update row: "+err.Error(), err)
		return
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to read updated row: "+err.Error(), err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": sanitizeRow(row),
	})
	h.audit(r.Context(), user.ID, user.Email, "data.update", "data", req.ID, req.App+"/"+req.Table, nil, r.RemoteAddr)
}

func (h *Handler) DataBrowserDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appName := r.URL.Query().Get("app")
	tableName := r.URL.Query().Get("table")
	id := r.URL.Query().Get("id")
	if appName == "" || tableName == "" || id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app, table, and id are required"})
		return
	}

	role, err := ResolveAppRoleByName(r.Context(), h.pool, user, appName)
	if err != nil && !errors.Is(err, ErrNotFound) {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanWrite() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	app, ok := h.reg.Get(appName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	_, ok = app.Tables[tableName]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}

	q := query.BuildDelete(app.SchemaName, tableName, id, "", h.reg.SystemConfig().SoftDeleteEnabled)
	tag, err := h.pool.Exec(r.Context(), q.SQL, q.Args...)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to delete row: "+err.Error(), err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "row not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
	h.audit(r.Context(), user.ID, user.Email, "data.delete", "data", id, appName+"/"+tableName, nil, r.RemoteAddr)
}

// appUserListParams holds the query params for listing an app's users.
type appUserListParams struct {
	Search string `json:"search"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// Lists users registered in an app's _auth_users table.
func (h *Handler) ListAppUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	schema := schemaNameForDB(app.Name)

	if err := h.prov.EnsureAuthUserColumns(r.Context(), schema); err != nil {
	}

	search := r.URL.Query().Get("search")
	limit := 50
	offset := 0
	if l, err := parseInt(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	if o, err := parseInt(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	users, total, err := ListAppUsers(r.Context(), h.pool, schema, search, limit, offset)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to list users", err)
		return
	}

	counts, err := CountAppUsersByProvider(r.Context(), h.pool, schema)
	if err != nil {
		counts = []*AppUserProviderCount{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":           users,
		"total":          total,
		"limit":          limit,
		"offset":         offset,
		"providerCounts": counts,
	})
}

// DeactivateAppUser handles PUT /dashboard/api/apps/{id}/users/{userId}/deactivate
func (h *Handler) DeactivateAppUser(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	userID := chi.URLParam(r, "userId")
	schema := schemaNameForDB(app.Name)
	h.prov.EnsureAuthUserColumns(r.Context(), schema)
	if err := DeactivateAppUser(r.Context(), h.pool, schema, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to deactivate user", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "user deactivated"})
	h.audit(r.Context(), user.ID, user.Email, "app.user.deactivate", "app_user", appID, app.Name+"/"+userID, nil, r.RemoteAddr)
}

// ActivateAppUser handles PUT /dashboard/api/apps/{id}/users/{userId}/activate
func (h *Handler) ActivateAppUser(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	userID := chi.URLParam(r, "userId")
	schema := schemaNameForDB(app.Name)
	h.prov.EnsureAuthUserColumns(r.Context(), schema)
	if err := ActivateAppUser(r.Context(), h.pool, schema, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to activate user", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "user activated"})
	h.audit(r.Context(), user.ID, user.Email, "app.user.activate", "app_user", appID, app.Name+"/"+userID, nil, r.RemoteAddr)
}

// UpdateAppUser handles PUT /dashboard/api/apps/{id}/users/{userId}. Updates
// email, phone, and role together; email/phone are otherwise uneditable
// fields not covered by /activate, /deactivate, or /reset-sessions.
func (h *Handler) UpdateAppUser(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var body struct {
		Email string `json:"email"`
		Phone string `json:"phone"`
		Role  string `json:"role"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	email := normalizeEmail(body.Email)
	if !isValidEmail(email) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email"})
		return
	}
	if !identRe.MatchString(body.Role) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must match ^[a-z][a-z0-9_]{0,62}$"})
		return
	}
	if body.Phone != "" && !phoneE164Re.MatchString(body.Phone) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid phone number"})
		return
	}

	userID := chi.URLParam(r, "userId")
	schema := schemaNameForDB(app.Name)
	h.prov.EnsureAuthUserColumns(r.Context(), schema)
	emailChanged, err := UpdateAppUser(r.Context(), h.pool, schema, userID, email, body.Phone, body.Role)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		if errors.Is(err, ErrEmailConflict) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "email already in use"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to update user", err)
		return
	}
	if emailChanged {
		if err := ResetAppUserSessions(r.Context(), h.pool, schema, userID); err != nil {
			h.logger.Warn("failed to reset app user sessions after email change", zap.Error(err))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "user updated"})
	h.audit(r.Context(), user.ID, user.Email, "app.user.update", "app_user", appID, app.Name+"/"+userID, nil, r.RemoteAddr)
}

// UpdateAppEnduserRoles handles PUT /dashboard/api/apps/{id}/roles. Replaces
// the app's full enduser_roles_config list (total replace, same semantics as
// storage_config/rate_limit_config — not a merge). Removing a role that is
// still assigned to at least one end user or referenced by at least one row
// policy is blocked with 409 (spec.md "role órfã" decision: values already
// persisted elsewhere keep working even after their role drops off this
// list, but the list itself can't drop a role still in active use).
func (h *Handler) UpdateAppEnduserRoles(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, appRole, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	if !appRole.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body struct {
		Roles []string `json:"roles"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Roles == nil {
		body.Roles = []string{}
	}

	newSet := make(map[string]bool, len(body.Roles))
	for _, roleName := range body.Roles {
		if !identRe.MatchString(roleName) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must match ^[a-z][a-z0-9_]{0,62}$"})
			return
		}
		if newSet[roleName] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role already exists"})
			return
		}
		newSet[roleName] = true
	}

	schema := schemaNameForDB(app.Name)
	h.prov.EnsureAuthUserColumns(r.Context(), schema)
	for _, oldRole := range app.EnduserRolesConfig {
		if newSet[oldRole] {
			continue
		}
		endUserCount, err := CountAppUsersByRole(r.Context(), h.pool, schema, oldRole)
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to check role usage", err)
			return
		}
		policyCount, err := CountTablePoliciesByRole(r.Context(), h.pool, app.ID, oldRole)
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to check role usage", err)
			return
		}
		if endUserCount+policyCount > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":        "role in use",
				"role":         oldRole,
				"endUserCount": endUserCount,
				"policyCount":  policyCount,
			})
			return
		}
	}

	if err := UpdateAppEnduserRoles(r.Context(), h.pool, app.ID, body.Roles); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to update enduser roles", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"roles": body.Roles})
	h.audit(r.Context(), user.ID, user.Email, "app.roles.update", "app", appID, app.Name, nil, r.RemoteAddr)
}

// ResetAppUserSessions handles POST /dashboard/api/apps/{id}/users/{userId}/reset-sessions
func (h *Handler) ResetAppUserSessions(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	userID := chi.URLParam(r, "userId")
	schema := schemaNameForDB(app.Name)
	h.prov.EnsureAuthUserColumns(r.Context(), schema)
	if err := ResetAppUserSessions(r.Context(), h.pool, schema, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session found for user"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to reset sessions", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "sessions reset"})
	h.audit(r.Context(), user.ID, user.Email, "app.user.sessions.reset", "app_user", appID, app.Name+"/"+userID, nil, r.RemoteAddr)
}

func (h *Handler) ListAuditLog(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	limit := 50
	if l, err := parseInt(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, err := parseInt(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	entries, total, err := ListAuditLog(r.Context(), h.pool, AuditLogFilter{
		Action:    r.URL.Query().Get("action"),
		Category:  r.URL.Query().Get("category"),
		UserEmail: r.URL.Query().Get("user_email"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   entries,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) GetPublicBrandConfig(w http.ResponseWriter, r *http.Request) {
	bc, err := GetBrandConfig(r.Context(), h.pool)
	if err != nil || bc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logo_url": "", "icon_url": "", "company_name": "Zeep Orbit"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logo_url":     bc.LogoURL,
		"icon_url":     bc.IconURL,
		"company_name": bc.CompanyName,
	})
}

var tokenExpirationOptions = map[string]*time.Duration{
	"7d":    durationPtr(7 * 24 * time.Hour),
	"30d":   durationPtr(30 * 24 * time.Hour),
	"365d":  durationPtr(365 * 24 * time.Hour),
	"never": nil,
}

func durationPtr(d time.Duration) *time.Duration { return &d }

func (h *Handler) ListAppTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if app.AuthEmailEnabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app tokens only available for apps without email auth"})
		return
	}

	tokens, err := ListAppTokens(r.Context(), h.pool, appID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to list tokens", err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (h *Handler) CreateAppToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if app.AuthEmailEnabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app tokens only available for apps without email auth"})
		return
	}

	var body struct {
		Name       string `json:"name"`
		Expiration string `json:"expiration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if len(body.Name) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be at most 128 characters"})
		return
	}
	dur, ok := tokenExpirationOptions[body.Expiration]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiration"})
		return
	}

	var expiresAt *time.Time
	if dur != nil {
		t := time.Now().Add(*dur)
		expiresAt = &t
	}

	tokenRow, err := CreateAppToken(r.Context(), h.pool, CreateAppTokenInput{
		AppID: appID, Name: body.Name, ExpiresAt: expiresAt,
	})
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to create token", err)
		return
	}

	jwtStr, err := auth.IssueAppTokenJWT(
		[]byte(app.JWTSecret),
		tokenRow.JTI, app.Name, expiresAt,
	)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to issue jwt", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"token": jwtStr,
		"row":   tokenRow,
	})
	h.audit(r.Context(), user.ID, user.Email, "app.token.create", "app", app.ID, app.Name, nil, r.RemoteAddr)
}

func (h *Handler) RevokeAppToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if app.AuthEmailEnabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app tokens only available for apps without email auth"})
		return
	}

	tokenID := chi.URLParam(r, "tokenId")

	if err := RevokeAppToken(r.Context(), h.pool, tokenID, appID); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to revoke token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "token revoked"})
	h.audit(r.Context(), user.ID, user.Email, "app.token.revoke", "app", appID, "", nil, r.RemoteAddr)
}

func (h *Handler) RegenerateAppSecret(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	if _, role, err := GetApp(r.Context(), h.pool, appID, user); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	} else if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirmation required"})
		return
	}

	var newSecret string
	err := h.pool.QueryRow(r.Context(),
		`UPDATE zeep_system.apps SET jwt_secret = encode(gen_random_bytes(32), 'hex') WHERE id = $1 RETURNING jwt_secret`,
		appID,
	).Scan(&newSecret)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to regenerate secret", err)
		return
	}

	if err := RevokeAllAppTokens(r.Context(), h.pool, appID); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to revoke tokens", err)
		return
	}

	appRow, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err == nil {
		h.reg.Register(appRowToRegistryApp(appRow))
	}

	writeJSON(w, http.StatusOK, map[string]string{"jwt_secret": newSecret})
	h.audit(r.Context(), user.ID, user.Email, "app.secret.regenerate", "app", appID, "", nil, r.RemoteAddr)
}

func (h *Handler) GetAppSecret(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"jwt_secret": app.JWTSecret})
	h.audit(r.Context(), user.ID, user.Email, "app.secret.view", "app", appID, app.Name, nil, r.RemoteAddr)
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

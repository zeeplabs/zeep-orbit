package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
	"github.com/zeeplabs/zeep-orbit/internal/query"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// Handler holds dependencies for dashboard HTTP handlers.
type Handler struct {
	pool  *db.Pool
	reg   *registry.Registry
	prov  *provisioner.Provisioner
	Logs  *RingBuffer
}

// NewHandler creates a new Handler.
func NewHandler(pool *db.Pool, reg *registry.Registry) *Handler {
	return &Handler{
		pool: pool,
		reg:  reg,
		prov: provisioner.New(pool),
		Logs: NewRingBuffer(2000),
	}
}

var (
	identRe      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	appNameRe    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	allowedTypes = map[string]bool{
		"text": true, "integer": true, "bigint": true, "boolean": true,
		"uuid": true, "timestamptz": true, "numeric": true, "jsonb": true,
	}
)

// validateAppInput checks that app name, table names, column names, and column types
// are safe SQL identifiers / known types before they reach the provisioner DDL.
func validateAppInput(name string, tables []AppTableRow) error {
	if !appNameRe.MatchString(name) {
		return errors.New("app name must be lowercase letters, digits, or underscores (max 32), starting with a letter")
	}
	for _, t := range tables {
		if !identRe.MatchString(t.Name) {
			return errors.New("table name must be lowercase letters, digits, or underscores (max 63), starting with a letter")
		}
		for _, c := range t.Columns {
			if !identRe.MatchString(c.Name) {
				return errors.New("column name must be lowercase letters, digits, or underscores (max 63), starting with a letter")
			}
			if !allowedTypes[c.Type] {
				return errors.New("unsupported column type: " + c.Type)
			}
		}
	}
	return nil
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

	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
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

// BootstrapStatus handles GET /dashboard/api/bootstrap/status
func (h *Handler) BootstrapStatus(w http.ResponseWriter, r *http.Request) {
	ok, err := IsBootstrapped(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"bootstrapped": ok})
}

// Config handles GET /dashboard/api/config
// Reads from zeep_system.brand_config, falling back to environment defaults.
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	cfg, err := GetBrandConfig(r.Context(), h.pool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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

	writeJSON(w, http.StatusOK, map[string]string{
		"theme":        theme,
		"company_name": company,
	})
}

// UpdateConfig handles PUT /dashboard/api/config
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

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Theme       string `json:"theme"`
		CompanyName string `json:"company_name"`
		LogoURL     string `json:"logo_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	validThemes := map[string]bool{"azure": true, "emerald": true, "ruby": true, "amber": true, "orange": true}
	if body.Theme != "" && !validThemes[body.Theme] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid theme"})
		return
	}

	cfg, err := UpsertBrandConfig(r.Context(), h.pool, body.Theme, body.CompanyName, body.LogoURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, cfg)
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.Email == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}
	if body.Role != "admin" && body.Role != "superadmin" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be 'admin' or 'superadmin'"})
		return
	}
	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	newUser, err := CreateUser(r.Context(), h.pool, body.Email, string(hash), body.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    newUser.ID,
		"email": newUser.Email,
		"role":  newUser.Role,
	})
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

	if err := DeleteUser(r.Context(), h.pool, targetID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListApps handles GET /dashboard/api/apps
func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	apps, err := ListApps(r.Context(), h.pool, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if apps == nil {
		apps = []*AppRow{}
	}
	writeJSON(w, http.StatusOK, apps)
}

// appRequestBody is the JSON body for create/update app requests.
type appRequestBody struct {
	Name             string        `json:"name"`
	AuthEmailEnabled bool          `json:"auth_email_enabled"`
	Tables           []AppTableRow `json:"tables"`
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := validateAppInput(body.Name, body.Tables); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	app, err := CreateApp(r.Context(), h.pool, body.Name, user.ID, body.AuthEmailEnabled, body.Tables)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	cfg := buildAppConfig(app)
	if _, err := h.prov.Apply(r.Context(), &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "provisioning failed: " + err.Error()})
		return
	}

	h.reg.Register(appRowToRegistryApp(app))

	writeJSON(w, http.StatusCreated, app)
}

// GetApp handles GET /dashboard/api/apps/{id}
func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	app, err := GetApp(r.Context(), h.pool, appID, user.ID, user.Role)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := validateAppInput(body.Name, body.Tables); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	app, err := UpdateApp(r.Context(), h.pool, appID, user.ID, user.Role, body.AuthEmailEnabled, body.Tables)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	cfg := buildAppConfig(app)
	if _, err := h.prov.Apply(r.Context(), &config.Config{Apps: []config.AppConfig{cfg}}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "provisioning failed: " + err.Error()})
		return
	}

	h.reg.Register(appRowToRegistryApp(app))

	app.JWTSecret = "" // not returned on update; fetch via GET /apps/{id} if needed
	writeJSON(w, http.StatusOK, app)
}

// DeleteApp handles DELETE /dashboard/api/apps/{id}
func (h *Handler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	// Fetch name before deletion for registry unregister.
	existing, err := GetApp(r.Context(), h.pool, appID, user.ID, user.Role)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if err := DeleteApp(r.Context(), h.pool, appID, user.ID, user.Role); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	h.reg.Unregister(existing.Name)

	w.WriteHeader(http.StatusNoContent)
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
	return config.AppConfig{
		Name: app.Name,
		Auth: config.AuthConfig{
			JWTSecret: app.JWTSecret,
			Providers: config.AuthProviders{Email: app.AuthEmailEnabled},
		},
		Tables: tables,
	}
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
	return &registry.App{
		Config:     buildAppConfig(app),
		SchemaName: app.Name,
		Tables:     tables,
	}
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

// ListLogs handles GET /dashboard/api/logs?app=&limit=
func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	appFilter := r.URL.Query().Get("app")
	if appFilter != "" && allowedApps != nil && !allowedApps[appFilter] {
		appFilter = "" // user doesn't own this app, ignore filter
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

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
	Name    string                 `json:"name"`
	Columns []DataBrowserTableColumn `json:"columns"`
}

// DataBrowserApp represents an app in the data browser tree.
type DataBrowserApp struct {
	Name   string             `json:"name"`
	Tables []DataBrowserTable `json:"tables"`
}

// ListDataBrowserApps handles GET /dashboard/api/data-browser/apps
// Returns apps with their tables from the registry, filtered by ownership.
func (h *Handler) ListDataBrowserApps(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
			tables = append(tables, DataBrowserTable{Name: t.Name, Columns: cols})
		}
		resp = append(resp, DataBrowserApp{Name: app.Config.Name, Tables: tables})
	}

	if resp == nil {
		resp = []DataBrowserApp{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// DataBrowserQuery handles GET /dashboard/api/data-browser/query
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

	// Ownership check
	allowedApps, err := ListOwnedAppNames(r.Context(), h.pool, user.ID, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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

	// Parse query params
	params := make(map[string]string)
	for k, vals := range r.URL.Query() {
		if k == "app" || k == "table" {
			continue
		}
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	q, err := query.BuildList(app.SchemaName, tableName, table, params, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// COUNT
	var count int
	filterArgs := q.Args[:len(q.Args)-2]
	if err := h.pool.QueryRow(ctx, q.CountSQL, filterArgs...).Scan(&count); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to count rows"})
		return
	}

	// DATA
	rows, err := h.pool.Query(ctx, q.SQL, q.Args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query rows"})
		return
	}
	data, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to collect rows"})
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

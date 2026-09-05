package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AppProviderConfig is the per-provider config stored in an app's auth_providers JSONB.
type AppProviderConfig struct {
	Enabled     bool   `json:"enabled"`
	ClientID    string `json:"client_id,omitempty"`
	RedirectURL string `json:"redirect_url,omitempty"`
}

// GetAppAuthProviders returns the auth providers configuration for an app,
// with each provider's client_secret masked to a client_secret_set boolean
// (AppRow.RedactSecrets' redactAuthProviderSecrets) — this is a read
// endpoint for the dashboard UI, not the internal path that actually
// performs an OAuth exchange (internal/auth/google.go reads the real,
// unmasked value straight from the DB).
func GetAppAuthProviders(ctx context.Context, pool *db.Pool, appID string, user *DashboardUser) (json.RawMessage, error) {
	app, _, err := GetApp(ctx, pool, appID, user)
	if err != nil {
		return nil, err
	}
	if app.AuthProviders == nil || string(app.AuthProviders) == "{}" || string(app.AuthProviders) == "" {
		return json.RawMessage(`{}`), nil
	}
	return redactAuthProviderSecrets(app.AuthProviders), nil
}

// UpdateAppAuthProviders updates the auth_providers JSONB for an app.
// Requires CanManage() (admin only) — auth providers are app-level config.
// Merges onto the existing stored config (mergeAppAuthProviders) rather
// than overwriting the column outright, so a field this request doesn't
// mention — client_secret above all, since GetAppAuthProviders never
// returns its plaintext for a caller to resend — survives instead of
// being silently wiped.
func UpdateAppAuthProviders(ctx context.Context, pool *db.Pool, appID string, user *DashboardUser, providers json.RawMessage) error {
	app, role, err := GetApp(ctx, pool, appID, user)
	if err != nil {
		return err
	}
	if !role.CanManage() {
		return ErrForbidden
	}
	merged, err := mergeAppAuthProviders(app.AuthProviders, providers)
	if err != nil {
		return &ValidationError{msg: err.Error()}
	}
	return updateAppProvidersRaw(ctx, pool, appID, merged)
}

// UpdateAppAuthProvidersRaw updates auth_providers without access check (for use during app creation).
func UpdateAppAuthProvidersRaw(ctx context.Context, pool *db.Pool, appID string, providers json.RawMessage) error {
	return updateAppProvidersRaw(ctx, pool, appID, providers)
}

// anyProviderEnabled reports whether the auth_providers JSONB has at least
// one provider (google, etc.) with "enabled": true. _auth_users/_auth_sessions
// are shared by every provider, not just email/password, so any caller that
// gates EnsureAuthTables on AuthEmailEnabled alone must also check this —
// otherwise an app with only Google enabled never gets those tables
// provisioned and its first OAuth login fails with "relation does not exist".
func anyProviderEnabled(providers json.RawMessage) bool {
	if len(providers) == 0 {
		return false
	}
	var parsed map[string]map[string]any
	if err := json.Unmarshal(providers, &parsed); err != nil {
		return false
	}
	for _, cfg := range parsed {
		if enabled, _ := cfg["enabled"].(bool); enabled {
			return true
		}
	}
	return false
}

func updateAppProvidersRaw(ctx context.Context, pool *db.Pool, appID string, providers json.RawMessage) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.apps SET auth_providers = $1 WHERE id = $2`,
		providers, appID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update app providers: %w", err)
	}
	return nil
}

// ListAppProviders handles GET /dashboard/api/apps/{id}/auth/providers
func (h *Handler) ListAppProviders(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	providers, err := GetAppAuthProviders(r.Context(), h.pool, appID, user)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	writeJSON(w, http.StatusOK, providers)
}

// UpdateAppProviders handles PUT /dashboard/api/apps/{id}/auth/providers
func (h *Handler) UpdateAppProviders(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var providers json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&providers); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if err := UpdateAppAuthProviders(r.Context(), h.pool, appID, user, providers); err != nil {
		var valErr *ValidationError
		switch {
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.As(err, &valErr):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": valErr.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update providers"})
		}
		return
	}

	app, _, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	if anyProviderEnabled(app.AuthProviders) {
		if err := h.prov.EnsureAuthTables(r.Context(), schemaNameForDB(app.Name)); err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "failed to provision auth tables", err)
			return
		}
	}

	h.reg.Register(appRowToRegistryApp(app))

	// Echo back the merged, redacted config — never the caller's raw
	// payload (which may still carry a plaintext client_secret the admin
	// just typed; a response body is a worse place for that to sit than
	// the request that legitimately needed it, e.g. proxy/HAR logs).
	writeJSON(w, http.StatusOK, redactAuthProviderSecrets(app.AuthProviders))
}

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AppProviderConfig is the per-provider config stored in an app's auth_providers JSONB.
type AppProviderConfig struct {
	Enabled    bool     `json:"enabled"`
	ClientID   string   `json:"client_id,omitempty"`
	RedirectURL string  `json:"redirect_url,omitempty"`
}

// GetAppAuthProviders returns the auth providers configuration for an app.
func GetAppAuthProviders(ctx context.Context, pool *db.Pool, appID, userID, role string) (json.RawMessage, error) {
	app, err := GetApp(ctx, pool, appID, userID, role)
	if err != nil {
		return nil, err
	}
	if app.AuthProviders == nil || string(app.AuthProviders) == "{}" || string(app.AuthProviders) == "" {
		return json.RawMessage(`{}`), nil
	}
	return app.AuthProviders, nil
}

// UpdateAppAuthProviders updates the auth_providers JSONB for an app.
func UpdateAppAuthProviders(ctx context.Context, pool *db.Pool, appID, userID, role string, providers json.RawMessage) error {
	if _, err := GetApp(ctx, pool, appID, userID, role); err != nil {
		return err
	}

	return updateAppProvidersRaw(ctx, pool, appID, providers)
}

// UpdateAppAuthProvidersRaw updates auth_providers without access check (for use during app creation).
func UpdateAppAuthProvidersRaw(ctx context.Context, pool *db.Pool, appID string, providers json.RawMessage) error {
	return updateAppProvidersRaw(ctx, pool, appID, providers)
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
	providers, err := GetAppAuthProviders(r.Context(), h.pool, appID, user.ID, user.Role)
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

	if err := UpdateAppAuthProviders(r.Context(), h.pool, appID, user.ID, user.Role, providers); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update providers"})
		return
	}

	app, err := GetApp(r.Context(), h.pool, appID, user.ID, user.Role)
	if err == nil {
		h.reg.Register(appRowToRegistryApp(app))
	}

	writeJSON(w, http.StatusOK, providers)
}

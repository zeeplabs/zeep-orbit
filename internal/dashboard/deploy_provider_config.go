package dashboard

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/deploy/render"
)

type DeployProviderConfigHandler struct {
	pool *db.Pool
}

func NewDeployProviderConfigHandler(pool *db.Pool) *DeployProviderConfigHandler {
	return &DeployProviderConfigHandler{pool: pool}
}

type deployProviderConfigBody struct {
	APIKey string `json:"api_key"`
}

func (h *DeployProviderConfigHandler) Status(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	cfg, err := GetDeployProviderConfig(r.Context(), h.pool)
	if err != nil || cfg == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"connected": false, "provider": "render"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connected": true,
		"provider":  cfg.Provider,
	})
}

func (h *DeployProviderConfigHandler) UpsertConfig(w http.ResponseWriter, r *http.Request) {
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
	var body deployProviderConfigBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key required"})
		return
	}

	if err := render.ValidateAPIKey(r.Context(), body.APIKey); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid api key: " + err.Error()})
		return
	}

	encrypted, err := crypto.Encrypt(body.APIKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if err := UpsertDeployProviderConfig(r.Context(), h.pool, "render", encrypted); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	meta, _ := json.Marshal(map[string]string{"provider": "render"})
	h.audit(r.Context(), user.ID, user.Email, "deploy_provider.config.update", "deploy_provider_config", "", "", meta, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (h *DeployProviderConfigHandler) audit(ctx context.Context, userID, userEmail, action, resourceType, resourceID, resourceName string, metadata json.RawMessage, ip string) {
	_ = InsertAuditLog(ctx, h.pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)
}

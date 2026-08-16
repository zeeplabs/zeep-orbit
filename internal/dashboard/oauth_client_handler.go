package dashboard

// oauth_client_handler.go — dashboard-session-authenticated visibility and
// lifecycle for dynamically self-registered OAuth clients (RegisterClient,
// oauth_client_store.go). Registration itself is unauthenticated by design
// (RFC 7591 dynamic client registration precedes any credential), which
// left admins with no way to see what had self-registered against their
// instance, judge a client's self-declared name, or revoke one — this
// closes that gap. Superadmin-scoped, same as the other instance-wide
// (not per-app) config surfaces: deploy_provider_config.go, github_config.go.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ListOAuthClients handles GET /dashboard/api/oauth-clients.
func (h *Handler) ListOAuthClients(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	clients, err := ListOAuthClients(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

// DeleteOAuthClient handles DELETE /dashboard/api/oauth-clients/{clientId} —
// deletes the registered client and, via ON DELETE CASCADE, every
// authorization code and access/refresh token pair ever issued to it
// (oauth_client_store.go's DeleteOAuthClient doc comment).
func (h *Handler) DeleteOAuthClient(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.Role != "superadmin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	clientID := chi.URLParam(r, "clientId")

	if err := DeleteOAuthClient(r.Context(), h.pool, clientID); err != nil {
		if errors.Is(err, ErrOAuthClientNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "oauth client not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "oauth client deleted"})
	h.audit(r.Context(), user.ID, user.Email, "oauth_client.delete", "oauth_client", clientID, "", nil, r.RemoteAddr)
}

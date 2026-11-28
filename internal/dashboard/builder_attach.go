package dashboard

// builder_attach.go — POST /api/builder/sessions/{id}/backend (NB-08, T11e).
//
// Gives a session created with no backend one, later. The route only decides and
// records: it creates a turn of kind `attach_backend` (no model call) and the
// worker does the rest (agent.runAttach). A new backend app is not created
// here; the worker's callback creates it after the overlay is committed, so a
// rejected or failed request leaves nothing to clean up. See
// .specs/features/builder-no-backend/design.md ("T11b").

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// builderAttachBodyLimit caps the request body of the attach route.
const builderAttachBodyLimit = 1 << 10

const builderBackendAlreadyAttachedMsg = "session already has a backend"

type attachBuilderBackendRequest struct {
	// Mode is "new" or "existing".
	Mode string `json:"mode"`
	// AppID is the existing backend app; required for "existing", refused for "new".
	AppID string `json:"app_id"`
}

// AttachBackend handles POST /dashboard/api/builder/sessions/{id}/backend
// {"mode":"new"} or {"mode":"existing","app_id":"..."}: 202 with the attach
// turn; 400 for a bad body or a session that cannot take a backend; 404 for a
// missing session or an app the caller cannot see; 409 when the session already
// has a backend or a turn is in progress. A pending plan or question card is
// ended by the same transaction that creates the turn.
func (h *BuilderHandler) AttachBackend(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	var req attachBuilderBackendRequest
	if !h.decodeBuilderBody(w, r, &req, builderAttachBodyLimit) {
		return
	}
	mode := strings.TrimSpace(req.Mode)
	appID := strings.TrimSpace(req.AppID)
	switch mode {
	case store.AttachModeNew:
		if appID != "" {
			h.writeError(w, http.StatusBadRequest, `app_id cannot be combined with mode "new"`)
			return
		}
	case store.AttachModeExisting:
		if !looksLikeUUID(appID) {
			h.writeError(w, http.StatusBadRequest, "app_id must be a valid app id")
			return
		}
	default:
		h.writeError(w, http.StatusBadRequest, `mode must be "new" or "existing"`)
		return
	}
	if sess.BackendOrigin != store.BackendOriginNone || sess.BackendAppID != "" {
		h.writeError(w, http.StatusConflict, builderBackendAlreadyAttachedMsg)
		return
	}
	if sess.TemplateKind != store.TemplateDefault {
		// The overlay is written against the built-in template; a company template
		// has its own structure.
		h.writeError(w, http.StatusBadRequest, "a backend can only be added to a project made from the built-in template")
		return
	}

	payload := store.AttachPayload{Mode: mode}
	var backendName string
	if mode == store.AttachModeExisting {
		app, _, err := GetApp(r.Context(), h.pool, appID, user)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				h.writeError(w, http.StatusNotFound, "app not found")
				return
			}
			h.serverError(w, r, err)
			return
		}
		payload.AppID = app.ID
		backendName = app.Name
	} else {
		if h.dashboard == nil {
			h.writeError(w, http.StatusServiceUnavailable, builderNotConfiguredMsg)
			return
		}
		backendName = deriveBuilderAppName(sess.Title, randomSuffix())
		payload.AppName = backendName
	}
	if _, ok := h.requireAvailable(w, r); !ok {
		return
	}

	apiURL, ok := h.apiBaseURL(r)
	if !ok {
		h.serverError(w, r, errors.New("cannot determine the public API base URL for the generated project"))
		return
	}
	payload.Config = string(renderOrbitConfig(apiURL, backendName))

	turn, err := h.store.CreateAttachTurn(r.Context(), user.ID, sess.ID, payload)
	if errors.Is(err, store.ErrBackendAlreadyAttached) {
		h.writeError(w, http.StatusConflict, builderBackendAlreadyAttachedMsg)
		return
	}
	if !h.turnCreated(w, r, turn, err) {
		return
	}
	meta := map[string]any{"mode": mode, "turn_seq": turn.Seq}
	if mode == store.AttachModeExisting {
		meta["app_id"] = payload.AppID
	}
	h.audit(r.Context(), user, "builder.session.backend.attach", sess.ID, sess.Title, meta, r.RemoteAddr)
}

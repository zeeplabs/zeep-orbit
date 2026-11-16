package dashboard

// builder_revert.go — POST /api/builder/sessions/{id}/revert (T28, AFB-10).
//
// A revert is a turn of kind `revert`: the worker restores the workspace to
// the state a previous turn left it in (as a NEW commit, history is never
// rewritten), rebuilds the preview and records a `revert` event. It makes no
// model call, so it spends no budget. See agent.runRevert.

import (
	"errors"
	"net/http"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// builderRevertBodyLimit caps the request body of the revert route.
const builderRevertBodyLimit = 1 << 10

type revertBuilderRequest struct {
	// TurnSeq is the turn whose resulting state to restore; 0 is the initial
	// template. A pointer so a missing field is not read as turn 0.
	TurnSeq *int `json:"turn_seq"`
}

// RevertSession handles POST /dashboard/api/builder/sessions/{id}/revert
// {"turn_seq": N}: 202 with the new revert turn, 400 for a turn_seq the
// session does not have, 409 when a turn is already in progress.
func (h *BuilderHandler) RevertSession(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	var req revertBuilderRequest
	if !h.decodeBuilderBody(w, r, &req, builderRevertBodyLimit) {
		return
	}
	if req.TurnSeq == nil || *req.TurnSeq < 0 {
		h.writeError(w, http.StatusBadRequest, "turn_seq is required")
		return
	}
	if _, ok := h.requireAvailable(w, r); !ok {
		return
	}

	turn, err := h.store.CreateRevertTurn(r.Context(), user.ID, sess.ID, *req.TurnSeq)
	if errors.Is(err, store.ErrNoRevertTarget) {
		h.writeError(w, http.StatusBadRequest, "unknown turn_seq")
		return
	}
	if !h.turnCreated(w, r, turn, err) {
		return
	}
	h.audit(r.Context(), user, "builder.session.revert", sess.ID, sess.Title,
		map[string]any{"to_turn_seq": *req.TurnSeq, "turn_seq": turn.Seq}, r.RemoteAddr)
}

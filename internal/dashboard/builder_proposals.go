package dashboard

// builder_proposals.go — confirm / reject a schema proposal (ai-frontend-builder
// T27, AFB-12). A turn that called propose_backend_change is paused in
// 'awaiting_confirmation' (internal/agent loop); these routes are how the owner
// resolves it. Methods of *BuilderHandler, mounted by the router under
// /dashboard behind RequireAuth (and the AI chat limiter, they mutate):
//
//	POST /api/builder/sessions/{id}/turns/{turn}/confirm   ConfirmProposal
//	POST /api/builder/sessions/{id}/turns/{turn}/reject    RejectProposal
//
// {turn} is the turn id (uuid) or its seq number.
//
// Confirm needs write access (editor or admin) on the BACKEND app, checked on
// the live role, not on who owned the session when it was created: a viewer, or
// someone who lost access, gets 403/404 and the turn stays paused. The change is
// applied through the same path as the edit chat (applyEditOperation) with the
// audit origin "ai_builder", then the model's tool call is answered and the turn
// goes back to 'pending' for a worker. Reject answers it with "rejected by
// user" and applies nothing.
//
// Both are idempotent: a repeated confirm (reject) of a proposal that was
// already confirmed (rejected) answers 200 without doing anything, so a
// double-click can never run the DDL twice. A concurrent duplicate blocks on the
// turn row (store.ResolveProposal) and then takes the same no-op path.
//
// A proposal the app refuses (validation, duplicate name, ...) is not a server
// failure: the safe message goes to the user (400) AND to the model as an error
// tool result, and the turn resumes so the agent can adapt.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
)

// builderAuditOrigin is the audit ip_address (origin marker) of schema changes
// applied from a confirmed builder proposal.
const builderAuditOrigin = "ai_builder"

// Public messages of the proposal routes.
const (
	builderTurnNotFoundMsg     = "turn not found"
	builderProposalNotPending  = "proposal is not awaiting confirmation"
	builderProposalBusyMsg     = "proposal is being resolved, try again in a moment"
	builderForbiddenMsg        = "forbidden"
	builderBackendNotFoundMsg  = "backend app not found"
	builderProposalRejectedMsg = "The user rejected this proposal (rejected by user). It was NOT applied. Do not retry the same change; ask the user what they want instead, or continue without it."
)

const (
	// A duplicate request that finds the proposal being resolved by another
	// request waits for it in short steps (never holding a connection), so a
	// double click ends as "already resolved" instead of an error.
	proposalBusyRetries = 30
	proposalBusyWait    = 100 * time.Millisecond
)

// resolveProposal is store.ResolveProposal with a bounded retry while another
// request holds the proposal (store.ErrProposalBusy).
func (h *BuilderHandler) resolveProposal(ctx context.Context, userID, sessionID, turnID string,
	decide func(context.Context, store.PendingProposal) (*store.ProposalResolution, error)) (*store.Turn, error) {
	for attempt := 0; ; attempt++ {
		turn, err := h.store.ResolveProposal(ctx, userID, sessionID, turnID, decide)
		if !errors.Is(err, store.ErrProposalBusy) || attempt >= proposalBusyRetries {
			return turn, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(proposalBusyWait):
		}
	}
}

// BuilderProposalView is the API shape of a pending proposal.
type BuilderProposalView struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	// Operation is the model's propose_backend_change arguments: {"operation":
	// {"kind": ..., "<kind>": {...}}}. Schema names only; no row data.
	Operation json.RawMessage `json:"operation"`
}

// newBuilderProposalView returns the pending proposal of t, or nil unless t is
// awaiting confirmation with a well-formed proposal.
func newBuilderProposalView(t *store.Turn) *BuilderProposalView {
	if t == nil || t.Status != store.TurnAwaitingConfirmation {
		return nil
	}
	p, ok := store.ParsePendingProposal(t.PendingProposal)
	if !ok {
		return nil
	}
	return &BuilderProposalView{ToolCallID: p.ToolCallID, Name: p.Name, Operation: p.Arguments}
}

// builderProposalResult is the body of the confirm and reject routes.
type builderProposalResult struct {
	// Applied is true when the change was applied (or had been before).
	Applied bool `json:"applied"`
	// Rejected is true for a rejected proposal.
	Rejected bool   `json:"rejected,omitempty"`
	Kind     string `json:"kind,omitempty"`
	// AlreadyResolved marks a repeated request that did nothing.
	AlreadyResolved bool             `json:"already_resolved,omitempty"`
	Turn            *BuilderTurnView `json:"turn,omitempty"`
}

// resolveTurnParam maps the {turn} URL parameter (turn id or seq) to a turn of
// the session. ok is false when the session has no such turn.
func (h *BuilderHandler) resolveTurnParam(ctx context.Context, userID, sessionID, param string) (*store.Turn, bool, error) {
	turns, err := h.store.ListTurns(ctx, userID, sessionID)
	if err != nil {
		return nil, false, err
	}
	byID := looksLikeUUID(param)
	seq, seqErr := strconv.Atoi(param)
	for i := range turns {
		if (byID && turns[i].ID == param) || (!byID && seqErr == nil && turns[i].Seq == seq) {
			return &turns[i], true, nil
		}
	}
	return nil, false, nil
}

// proposalRoute is the common prologue of confirm and reject: it resolves the
// caller, session and turn. ok=false means the response was written.
func (h *BuilderHandler) proposalRoute(w http.ResponseWriter, r *http.Request) (*DashboardUser, *store.Session, *store.Turn, bool) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return nil, nil, nil, false
	}
	turn, found, err := h.resolveTurnParam(r.Context(), user.ID, sess.ID, chi.URLParam(r, "turn"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
			return nil, nil, nil, false
		}
		h.serverError(w, r, err)
		return nil, nil, nil, false
	}
	if !found {
		h.writeError(w, http.StatusNotFound, builderTurnNotFoundMsg)
		return nil, nil, nil, false
	}
	return user, sess, turn, true
}

// ConfirmProposal handles POST .../turns/{turn}/confirm.
func (h *BuilderHandler) ConfirmProposal(w http.ResponseWriter, r *http.Request) {
	user, sess, turn, ok := h.proposalRoute(w, r)
	if !ok {
		return
	}
	if h.dashboard == nil {
		h.writeError(w, http.StatusServiceUnavailable, builderNotConfiguredMsg)
		return
	}

	// Live RBAC on the backend app, before anything is locked or changed. No
	// access (or app gone) is a 404, read-only is a 403; both leave the turn
	// paused so the user can still reject it.
	role, err := ResolveAppRole(r.Context(), h.pool, user, AppRef{BackendAppID: sess.BackendAppID})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	switch {
	case !role.Effective():
		h.writeError(w, http.StatusNotFound, builderBackendNotFoundMsg)
		return
	case !role.CanWrite():
		h.writeError(w, http.StatusForbidden, builderForbiddenMsg)
		return
	}

	var (
		applied  bool
		kind     string
		userMsg  string // set when the app refused the change (400)
		applyErr error  // unexpected failure (500, turn stays paused)
	)
	resolved, err := h.resolveProposal(r.Context(), user.ID, sess.ID, turn.ID,
		func(ctx context.Context, p store.PendingProposal) (*store.ProposalResolution, error) {
			res := &store.ProposalResolution{EventType: store.EventProposalConfirmed}
			payload := map[string]any{"tool_call_id": p.ToolCallID}
			res.EventPayload = payload

			if p.Name != ToolProposeBackendChange {
				userMsg = "the proposal is not a backend change."
				res.IsError, res.Content = true, "The proposal could not be applied: it is not a backend change."
				payload["applied"] = false
				return res, nil
			}
			// Re-parse the stored arguments with the validator the proposal tool
			// used, so what is applied is exactly what was validated.
			op, perr := ParseBuilderProposal(p.Arguments)
			if perr != nil {
				userMsg = "the proposal is malformed."
				res.IsError, res.Content = true, "The proposal could not be applied: it is malformed."
				payload["applied"] = false
				return res, nil
			}
			kind = op.Kind
			payload["kind"] = op.Kind

			_, aerr := h.dashboard.applyEditOperation(ctx, user, sess.BackendAppID, op, builderAuditOrigin)
			switch {
			case aerr == nil:
				applied = true
				payload["applied"] = true
				res.Content = fmt.Sprintf("The user confirmed the proposal and it was applied (%s). The backend schema changed: call orbit_get_app_schema to see it before writing code that uses it.", op.Kind)
				return res, nil
			case errors.Is(aerr, ErrForbidden):
				// Lost write access between the check above and now.
				return nil, aerr
			default:
				msg, safe := builderApplyErrorMessage(aerr)
				if !safe {
					applyErr = aerr
					return nil, aerr
				}
				userMsg = msg
				payload["applied"] = false
				res.IsError = true
				res.Content = "The proposal was NOT applied: " + msg + " Nothing was changed. Propose a corrected change, or ask the user."
				return res, nil
			}
		})

	switch {
	case err == nil:
	case errors.Is(err, store.ErrProposalNotPending):
		h.alreadyResolved(w, r, turn, store.EventProposalConfirmed)
		return
	case errors.Is(err, store.ErrProposalBusy):
		h.writeError(w, http.StatusConflict, builderProposalBusyMsg)
		return
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrSessionArchived):
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
		return
	case errors.Is(err, ErrForbidden):
		h.writeError(w, http.StatusForbidden, builderForbiddenMsg)
		return
	default:
		if applyErr != nil {
			err = applyErr
		}
		h.serverError(w, r, err)
		return
	}

	if h.poke != nil {
		h.poke()
	}
	h.audit(r.Context(), user, "builder.proposal.confirm", sess.ID, sess.Title,
		map[string]any{"turn_seq": turn.Seq, "kind": kind, "applied": applied}, r.RemoteAddr)

	if userMsg != "" {
		h.writeError(w, http.StatusBadRequest, userMsg)
		return
	}
	view := newBuilderTurnView(resolved)
	writeJSON(w, http.StatusOK, builderProposalResult{Applied: true, Kind: kind, Turn: &view})
}

// RejectProposal handles POST .../turns/{turn}/reject.
func (h *BuilderHandler) RejectProposal(w http.ResponseWriter, r *http.Request) {
	user, sess, turn, ok := h.proposalRoute(w, r)
	if !ok {
		return
	}
	resolved, err := h.resolveProposal(r.Context(), user.ID, sess.ID, turn.ID,
		func(_ context.Context, p store.PendingProposal) (*store.ProposalResolution, error) {
			return &store.ProposalResolution{
				EventType:    store.EventProposalRejected,
				EventPayload: map[string]any{"tool_call_id": p.ToolCallID},
				IsError:      true,
				Content:      builderProposalRejectedMsg,
			}, nil
		})
	switch {
	case err == nil:
	case errors.Is(err, store.ErrProposalNotPending):
		h.alreadyResolved(w, r, turn, store.EventProposalRejected)
		return
	case errors.Is(err, store.ErrProposalBusy):
		h.writeError(w, http.StatusConflict, builderProposalBusyMsg)
		return
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrSessionArchived):
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
		return
	default:
		h.serverError(w, r, err)
		return
	}
	if h.poke != nil {
		h.poke()
	}
	h.audit(r.Context(), user, "builder.proposal.reject", sess.ID, sess.Title,
		map[string]any{"turn_seq": turn.Seq}, r.RemoteAddr)

	view := newBuilderTurnView(resolved)
	writeJSON(w, http.StatusOK, builderProposalResult{Rejected: true, Turn: &view})
}

// alreadyResolved answers a confirm/reject of a turn that is not awaiting
// confirmation: 200 (a no-op) when the same decision was already recorded,
// 409 otherwise (the other decision, or no proposal at all).
func (h *BuilderHandler) alreadyResolved(w http.ResponseWriter, r *http.Request, turn *store.Turn, want string) {
	eventType, payload, err := h.store.ProposalDecision(r.Context(), turn.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if eventType == want {
		res := builderProposalResult{AlreadyResolved: true}
		switch want {
		case store.EventProposalConfirmed:
			var p struct {
				Applied bool   `json:"applied"`
				Kind    string `json:"kind"`
			}
			_ = json.Unmarshal(payload, &p)
			if !p.Applied {
				// The confirm was refused by the app: nothing to repeat.
				h.writeError(w, http.StatusConflict, builderProposalNotPending)
				return
			}
			res.Applied, res.Kind = true, p.Kind
		case store.EventProposalRejected:
			res.Rejected = true
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	h.writeError(w, http.StatusConflict, builderProposalNotPending)
}

// builderApplyErrorMessage maps an error of the schema write paths to a message
// that is safe to show the user and the model. safe=false means an unexpected
// (infrastructure) failure: log it, answer a generic 500, never expose it. The
// split mirrors respondEditChatConfirmError.
func builderApplyErrorMessage(err error) (msg string, safe bool) {
	var valErr *ValidationError
	var typeErr *provisioner.TypeChangeError
	var colErr *provisioner.ColumnConstraintError
	var fkErr *provisioner.ForeignKeyViolationError
	switch {
	case errors.Is(err, ErrNotFound):
		return "the table or column the change refers to does not exist.", true
	case errors.Is(err, ErrColumnAlreadyHasReference):
		return "that column already has a foreign key; remove it first before adding a new one.", true
	case errors.Is(err, ErrColumnHasNoReference):
		return "that column has no foreign key to remove.", true
	case errors.Is(err, ErrColumnAlreadyExists):
		return "that column already exists; adding a foreign key to (or recreating) an existing column is not supported. Use a differently named column.", true
	case errors.Is(err, ErrIndexAlreadyExists):
		return "an index with that name already exists on this table; use a different index name.", true
	case errors.As(err, &valErr):
		return valErr.Error(), true
	case errors.As(err, &typeErr):
		return typeErr.Error(), true
	case errors.As(err, &colErr):
		return colErr.Error(), true
	case errors.As(err, &fkErr):
		return fkErr.Error(), true
	}
	return "", false
}

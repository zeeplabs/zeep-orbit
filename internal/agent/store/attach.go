package store

// attach.go — the attach_backend turn (builder-no-backend NB-08, T11c): give a
// session created with no backend one, later. The turn runs no model; the
// worker writes the overlay, commits, and then calls AttachBackend. See
// .specs/features/builder-no-backend/design.md ("T11b").

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// KindAttachBackend is the kind of a turn that attaches a backend.
const KindAttachBackend = "attach_backend"

// EventPlanDismissed is appended when a pending plan or question card is ended
// because a backend was attached instead.
const EventPlanDismissed = "plan_dismissed"

var (
	// ErrBackendAlreadyAttached: the session already has a backend (or another
	// attach won the race).
	ErrBackendAlreadyAttached = errors.New("session already has a backend")
	// ErrRevertAcrossBackend: a revert to before an attach would restore a
	// workspace without the SDK files while the session says it has a backend.
	ErrRevertAcrossBackend = errors.New("cannot revert across a backend change")
)

// Attach modes of AttachPayload.Mode.
const (
	AttachModeNew      = "new"
	AttachModeExisting = "existing"
)

// AttachPayload is what the handler decided and the worker needs. It is kept in
// builder_turns.pending_proposal (never awaiting confirmation, as for revert).
type AttachPayload struct {
	Mode string `json:"mode"`
	// AppID is the existing backend app; empty for a new one, which does not
	// exist yet (the worker's callback creates it after the overlay is committed).
	AppID string `json:"app_id,omitempty"`
	// AppName is the name of the new backend app.
	AppName string `json:"app_name,omitempty"`
	// Config is the rendered src/orbit.config.ts (public API URL and app name:
	// no secret).
	Config string `json:"config"`
}

type attachEnvelope struct {
	Attach AttachPayload `json:"attach"`
}

// AttachPayloadOf returns the payload of an attach turn. ok is false for any
// other kind of turn or a row without a usable payload.
func AttachPayloadOf(t *Turn) (AttachPayload, bool) {
	if t == nil || t.Kind != KindAttachBackend || len(t.PendingProposal) == 0 {
		return AttachPayload{}, false
	}
	var e attachEnvelope
	if err := json.Unmarshal(t.PendingProposal, &e); err != nil || validAttach(e.Attach) != nil {
		return AttachPayload{}, false
	}
	return e.Attach, true
}

func validAttach(p AttachPayload) error {
	if p.Config == "" {
		return fmt.Errorf("%w: attach config is required", ErrInvalidArgument)
	}
	switch p.Mode {
	case AttachModeNew:
		if p.AppName == "" || p.AppID != "" {
			return fmt.Errorf("%w: a new backend needs a name and no app id", ErrInvalidArgument)
		}
	case AttachModeExisting:
		if p.AppID == "" {
			return fmt.Errorf("%w: an existing backend needs an app id", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: attach mode must be %q or %q", ErrInvalidArgument, AttachModeNew, AttachModeExisting)
	}
	return nil
}

// dismissedPlanResult is the tool result written for the card that was ended, so
// the conversation never keeps a tool call without an answer.
const dismissedPlanResult = "The user added a backend to this project instead of answering. " +
	"This card is closed; a new planning round for the data part follows."

// CreateAttachTurn enqueues a pending attach turn on the owner's session. When
// the active turn is paused on a plan or a question card, that turn is ended in
// the same transaction (completed, nothing built, event plan_dismissed, a tool
// result for the card) because a paused turn counts as active and would block
// the attach.
//
// Errors: ErrNotFound, ErrSessionArchived, ErrInvalidArgument (payload),
// ErrBackendAlreadyAttached, ErrTurnActive (any other active turn).
func (s *Store) CreateAttachTurn(ctx context.Context, ownerUserID, sessionID string, p AttachPayload) (*Turn, error) {
	if err := validAttach(p); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(attachEnvelope{Attach: p})
	if err != nil {
		return nil, fmt.Errorf("builder store: create attach turn: marshal: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("builder store: create attach turn: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status, origin string
	var hasApp bool
	err = tx.QueryRow(ctx,
		`SELECT status, backend_origin, backend_app_id IS NOT NULL
		 FROM zeep_system.builder_sessions WHERE id = $1 AND owner_user_id = $2 FOR UPDATE`,
		sessionID, ownerUserID,
	).Scan(&status, &origin, &hasApp)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: create attach turn: lock session: %w", err)
	}
	if status == SessionArchived {
		return nil, ErrSessionArchived
	}
	if origin != BackendOriginNone || hasApp {
		return nil, ErrBackendAlreadyAttached
	}

	var activeID, activeStatus string
	var proposal []byte
	err = tx.QueryRow(ctx,
		`SELECT id, status, pending_proposal FROM zeep_system.builder_turns
		 WHERE session_id = $1 AND status IN ('pending','queued','running','awaiting_confirmation')`,
		sessionID,
	).Scan(&activeID, &activeStatus, &proposal)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("builder store: create attach turn: find active turn: %w", err)
	default:
		pp, ok := ParsePendingProposal(proposal)
		if activeStatus != TurnAwaitingConfirmation || !ok ||
			(pp.Name != PlanningToolAskUser && pp.Name != PlanningToolProposeBuildPlan) {
			return nil, ErrTurnActive
		}
		if err := dismissPlanCard(ctx, tx, sessionID, activeID, pp.ToolCallID); err != nil {
			return nil, err
		}
	}

	turn, err := scanTurn(tx.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_turns (session_id, seq, kind, pending_proposal)
		 VALUES ($1, (SELECT COALESCE(MAX(seq), 0) + 1 FROM zeep_system.builder_turns WHERE session_id = $1), $2, $3)
		 RETURNING `+turnColumns,
		sessionID, KindAttachBackend, payload,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == oneActiveTurnIndex {
			return nil, ErrTurnActive
		}
		return nil, fmt.Errorf("builder store: create attach turn: insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("builder store: create attach turn: commit: %w", err)
	}
	return turn, nil
}

// dismissPlanCard ends a turn paused on a plan or question card.
func dismissPlanCard(ctx context.Context, tx pgx.Tx, sessionID, turnID, toolCallID string) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_messages (session_id, turn_id, role, content, tool_calls, tool_call_id)
		 VALUES ($1, $2, 'tool', $3, '{"dismissed":true,"is_error":false}'::jsonb, $4)`,
		sessionID, turnID, dismissedPlanResult, toolCallID,
	); err != nil {
		return fmt.Errorf("builder store: dismiss plan card: tool result: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type, payload)
		 VALUES ($1, $2, $3, jsonb_build_object('tool_call_id', $4::text, 'reason', 'backend_added'))`,
		sessionID, turnID, EventPlanDismissed, toolCallID,
	); err != nil {
		return fmt.Errorf("builder store: dismiss plan card: event: %w", err)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_turns
		 SET status = 'completed', pending_proposal = NULL, lease_owner = NULL, lease_until = NULL, finished_at = now()
		 WHERE id = $1 AND status = 'awaiting_confirmation'`, turnID)
	if err != nil {
		return fmt.Errorf("builder store: dismiss plan card: finish turn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTurnActive
	}
	if _, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_sessions SET status = 'idle', updated_at = now()
		 WHERE id = $1 AND status <> 'archived'`, sessionID); err != nil {
		return fmt.Errorf("builder store: dismiss plan card: session: %w", err)
	}
	return nil
}

// AttachBackend records the backend on a session that has none: one conditional
// UPDATE (so two attaches cannot both win), plus the link of the session's
// published frontend app when it has one and no link yet. origin is
// BackendOriginBuilder for a backend the builder created or BackendOriginExisting.
//
// Errors: ErrInvalidArgument, ErrNotFound, ErrBackendAlreadyAttached.
func (s *Store) AttachBackend(ctx context.Context, sessionID, appID, origin string) error {
	if appID == "" || (origin != BackendOriginBuilder && origin != BackendOriginExisting) {
		return fmt.Errorf("%w: attach needs an app id and an origin of %q or %q", ErrInvalidArgument, BackendOriginBuilder, BackendOriginExisting)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("builder store: attach backend: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_sessions
		 SET backend_app_id = $2::uuid, backend_origin = $3, updated_at = now()
		 WHERE id = $1 AND backend_origin = 'none' AND backend_app_id IS NULL AND status <> 'archived'`,
		sessionID, appID, origin)
	if err != nil {
		return fmt.Errorf("builder store: attach backend: update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM zeep_system.builder_sessions WHERE id = $1)`, sessionID).Scan(&exists); err != nil {
			return fmt.Errorf("builder store: attach backend: find session: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		return ErrBackendAlreadyAttached
	}
	if _, err := tx.Exec(ctx,
		`UPDATE zeep_system.frontend_apps SET backend_app_id = $2::uuid
		 WHERE id = (SELECT frontend_app_id FROM zeep_system.builder_sessions WHERE id = $1)
		   AND backend_app_id IS NULL`,
		sessionID, appID); err != nil {
		return fmt.Errorf("builder store: attach backend: link frontend app: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("builder store: attach backend: commit: %w", err)
	}
	return nil
}

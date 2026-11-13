package store

// proposals.go — resolving a paused turn (ai-frontend-builder T27, AFB-12).
//
// A turn that called propose_backend_change ends in 'awaiting_confirmation':
// FinishTurn released the lease and parked the proposal in
// builder_turns.pending_proposal. The user's decision arrives through the HTTP
// API, outside any lease, so the lease-checked writers (AppendMessage,
// AppendEvent) cannot be used. ResolveProposal is the one place that writes the
// decision: the tool result the model will read, the event, and the move back
// to 'pending' so a worker resumes the turn.
//
// The turn row stays locked (FOR UPDATE NOWAIT) while the caller's decide
// function runs. That is what makes a confirm idempotent: a second, concurrent
// confirm cannot get the lock (ErrProposalBusy, retry shortly) and, once the
// first one committed, finds the turn no longer awaiting, so the schema change
// is applied at most once. NOWAIT, not a blocking lock, because a waiter would
// hold a pool connection while the winner needs another one for its DDL: a
// burst of duplicates could starve the pool. If decide fails, or the process
// dies, the transaction rolls back and the turn is still awaiting
// confirmation, so the user can try again.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrProposalNotPending: the turn is not awaiting confirmation (it was already
// resolved, or never proposed anything).
var ErrProposalNotPending = errors.New("turn is not awaiting confirmation")

// ErrProposalBusy: another request is resolving this proposal right now. Retry
// after a moment: it will then be resolved (ErrProposalNotPending) or, if that
// request failed, still awaiting.
var ErrProposalBusy = errors.New("proposal is being resolved")

// pgLockNotAvailable is SQLSTATE lock_not_available (FOR UPDATE NOWAIT).
const pgLockNotAvailable = "55P03"

// Event types written by ResolveProposal.
const (
	EventProposalConfirmed = "proposal_confirmed"
	EventProposalRejected  = "proposal_rejected"
)

// PendingProposal is the parked proposal of an awaiting turn, as the loop wrote
// it: the model's tool call id and name plus its raw arguments.
type PendingProposal struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

// ParsePendingProposal decodes builder_turns.pending_proposal. ok is false when
// the value is absent or has no tool call id.
func ParsePendingProposal(raw json.RawMessage) (PendingProposal, bool) {
	var p PendingProposal
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil || p.ToolCallID == "" {
		return PendingProposal{}, false
	}
	return p, true
}

// ProposalResolution is what decide returns: the outcome to record.
type ProposalResolution struct {
	// Content is the tool result text the model reads when the turn resumes.
	Content string
	// IsError marks the tool result as a failure (the proposal was not applied).
	IsError bool
	// EventType is EventProposalConfirmed or EventProposalRejected.
	EventType string
	// EventPayload is the event payload (no secrets, no file contents).
	EventPayload any
}

// ResolveProposal resolves the proposal of turnID, a turn of the owner's
// session sessionID. Under a lock on the turn row it checks the turn is
// awaiting confirmation, calls decide, and, when decide returns a resolution,
// atomically appends the tool result message, appends the event, clears the
// proposal, moves the turn back to 'pending' (a worker then resumes it) and the
// session to 'idle' (as for a freshly accepted message).
//
// decide runs inside the transaction: keep it short, and do not touch
// builder_turns from it. An error from decide rolls everything back and is
// returned unchanged, so the caller can tell its own failures apart from the
// typed errors below. A nil resolution with a nil error is an invalid use.
//
// A turn paused on a planning tool (a question round or a build plan) is not
// resolved here: it answers ErrProposalKind (see planning.go).
//
// Errors: ErrNotFound (turn or session is not the owner's), ErrSessionArchived,
// ErrProposalNotPending, ErrProposalBusy, ErrProposalKind.
func (s *Store) ResolveProposal(
	ctx context.Context, ownerUserID, sessionID, turnID string,
	decide func(ctx context.Context, p PendingProposal) (*ProposalResolution, error),
) (*Turn, error) {
	return s.resolve(ctx, ownerUserID, sessionID, turnID,
		func(name string) bool { return !isPlanningTool(name) },
		func(ctx context.Context, _ pgx.Tx, p PendingProposal) (*resolution, error) {
			res, err := decide(ctx, p)
			if err != nil || res == nil {
				return nil, err
			}
			return &resolution{ProposalResolution: *res}, nil
		})
}

// resolution is what the internal decide returns: the public outcome plus the
// planning markers stored in the tool message metadata.
type resolution struct {
	ProposalResolution
	questionsClosed   bool
	questionsReopened bool
}

// resolve is the shared body of every proposal resolution. accept says whether
// the parked tool is one this caller resolves; decide runs inside the
// transaction and receives it, so a caller can change other rows atomically
// with the resolution (it must leave the turn's status alone).
func (s *Store) resolve(
	ctx context.Context, ownerUserID, sessionID, turnID string,
	accept func(toolName string) bool,
	decide func(ctx context.Context, tx pgx.Tx, p PendingProposal) (*resolution, error),
) (*Turn, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("builder store: resolve proposal: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		status     string
		sessStatus string
		raw        []byte
	)
	err = tx.QueryRow(ctx,
		`SELECT t.status, s.status, t.pending_proposal
		 FROM zeep_system.builder_turns t
		 JOIN zeep_system.builder_sessions s ON s.id = t.session_id
		 WHERE t.id = $1 AND t.session_id = $2 AND s.owner_user_id = $3
		 FOR UPDATE OF t NOWAIT`,
		turnID, sessionID, ownerUserID,
	).Scan(&status, &sessStatus, &raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgLockNotAvailable {
			return nil, ErrProposalBusy
		}
		return nil, fmt.Errorf("builder store: resolve proposal: lock turn: %w", err)
	}
	if sessStatus == SessionArchived {
		return nil, ErrSessionArchived
	}
	if status != TurnAwaitingConfirmation {
		return nil, ErrProposalNotPending
	}
	proposal, ok := ParsePendingProposal(raw)
	if !ok {
		return nil, fmt.Errorf("builder store: resolve proposal: turn %s has no valid pending proposal", turnID)
	}

	if !accept(proposal.Name) {
		return nil, ErrProposalKind
	}

	res, err := decide(ctx, tx, proposal)
	if err != nil {
		return nil, err
	}
	if res == nil || res.EventType == "" {
		return nil, fmt.Errorf("%w: a resolution with an event type is required", ErrInvalidArgument)
	}
	payload, err := marshalPayload(res.EventPayload)
	if err != nil {
		return nil, err
	}
	metaFields := map[string]bool{"build_failed": false, "is_error": res.IsError}
	if res.questionsClosed {
		metaFields["questions_closed"] = true
	}
	if res.questionsReopened {
		metaFields["questions_reopened"] = true
	}
	meta, err := json.Marshal(metaFields)
	if err != nil {
		return nil, fmt.Errorf("builder store: resolve proposal: marshal tool metadata: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_messages (session_id, turn_id, role, content, tool_calls, tool_call_id)
		 VALUES ($1, $2, 'tool', $3, $4::jsonb, $5)`,
		sessionID, turnID, res.Content, meta, proposal.ToolCallID,
	); err != nil {
		return nil, fmt.Errorf("builder store: resolve proposal: tool result: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type, payload) VALUES ($1, $2, $3, $4::jsonb)`,
		sessionID, turnID, res.EventType, payload,
	); err != nil {
		return nil, fmt.Errorf("builder store: resolve proposal: event: %w", err)
	}
	turn, err := scanTurn(tx.QueryRow(ctx,
		`UPDATE zeep_system.builder_turns
		 SET status = 'pending', pending_proposal = NULL, lease_owner = NULL, lease_until = NULL
		 WHERE id = $1 AND status = 'awaiting_confirmation'
		 RETURNING `+turnColumns,
		turnID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProposalNotPending
		}
		return nil, fmt.Errorf("builder store: resolve proposal: resume turn: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_sessions SET status = 'idle', updated_at = now()
		 WHERE id = $1 AND status <> 'archived'`, sessionID,
	); err != nil {
		return nil, fmt.Errorf("builder store: resolve proposal: session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("builder store: resolve proposal: commit: %w", err)
	}
	return turn, nil
}

// ProposalDecision returns how the proposal of turnID was resolved: the event
// type (EventProposalConfirmed / EventProposalRejected) and payload of the
// latest decision event, or "" when there is none. Internal (not owner-scoped):
// call it only for a turn of a session the caller already authorized.
func (s *Store) ProposalDecision(ctx context.Context, turnID string) (eventType string, payload json.RawMessage, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT type, payload FROM zeep_system.builder_events
		 WHERE turn_id = $1 AND type IN ('proposal_confirmed', 'proposal_rejected')
		 ORDER BY id DESC LIMIT 1`, turnID,
	).Scan(&eventType, &payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("builder store: proposal decision: %w", err)
	}
	return eventType, payload, nil
}

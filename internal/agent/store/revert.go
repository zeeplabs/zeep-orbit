package store

// revert.go — revert turns (ai-frontend-builder T28).
//
// A revert turn restores the workspace to the state a previous turn left it
// in. The target is resolved ONCE, when the turn is created, and persisted in
// the turn row so the worker needs no lookup (and no LLM): see RevertTarget.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNoRevertTarget: the requested turn does not exist in the session, or the
// session holds no snapshot to restore it from. Callers map it to 400.
var ErrNoRevertTarget = errors.New("no such turn to revert to")

// RevertTarget is where a revert turn takes the workspace: the state after
// turn TurnSeq (0 is the initial template), i.e. commit SHA.
type RevertTarget struct {
	TurnSeq int    `json:"turn_seq"`
	SHA     string `json:"sha"`
}

// revertPayload is the JSON kept in builder_turns.pending_proposal of a revert
// turn. A revert turn never awaits confirmation, so the column is free;
// FinishTurn clears it when the turn ends.
type revertPayload struct {
	Revert RevertTarget `json:"revert"`
}

// RevertTargetOf returns the target of a revert turn. ok is false for any
// other kind of turn or a row without a usable target.
func RevertTargetOf(t *Turn) (RevertTarget, bool) {
	if t == nil || t.Kind != KindRevert || len(t.PendingProposal) == 0 {
		return RevertTarget{}, false
	}
	var p revertPayload
	if err := json.Unmarshal(t.PendingProposal, &p); err != nil {
		return RevertTarget{}, false
	}
	if p.Revert.SHA == "" || p.Revert.TurnSeq < 0 {
		return RevertTarget{}, false
	}
	return p.Revert, true
}

// CreateRevertTurn enqueues a pending revert turn on the owner's session that
// restores the state after turn turnSeq. The target is the commit of the last
// snapshot taken at or before that turn, so a turn that changed nothing (or
// failed before committing) resolves to the state it started from; turn 0 is
// the initial template.
//
// Errors: ErrNotFound (not the owner's session), ErrSessionArchived,
// ErrNoRevertTarget (unknown turn_seq or nothing to restore from) and
// ErrTurnActive (the session already has a non-terminal turn).
func (s *Store) CreateRevertTurn(ctx context.Context, ownerUserID, sessionID string, turnSeq int) (*Turn, error) {
	if turnSeq < 0 {
		return nil, ErrNoRevertTarget
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("builder store: create revert turn: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM zeep_system.builder_sessions WHERE id = $1 AND owner_user_id = $2 FOR UPDATE`,
		sessionID, ownerUserID,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: create revert turn: lock session: %w", err)
	}
	if status == SessionArchived {
		return nil, ErrSessionArchived
	}

	if turnSeq > 0 {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM zeep_system.builder_turns WHERE session_id = $1 AND seq = $2)`,
			sessionID, turnSeq,
		).Scan(&exists); err != nil {
			return nil, fmt.Errorf("builder store: create revert turn: find turn: %w", err)
		}
		if !exists {
			return nil, ErrNoRevertTarget
		}
	}
	var sha string
	err = tx.QueryRow(ctx,
		`SELECT commit_sha FROM zeep_system.builder_snapshots
		 WHERE session_id = $1 AND turn_seq <= $2
		 ORDER BY turn_seq DESC, part DESC LIMIT 1`,
		sessionID, turnSeq,
	).Scan(&sha)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoRevertTarget
		}
		return nil, fmt.Errorf("builder store: create revert turn: resolve target: %w", err)
	}
	payload, err := json.Marshal(revertPayload{Revert: RevertTarget{TurnSeq: turnSeq, SHA: sha}})
	if err != nil {
		return nil, fmt.Errorf("builder store: create revert turn: marshal target: %w", err)
	}

	turn, err := scanTurn(tx.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_turns (session_id, seq, kind, pending_proposal)
		 VALUES ($1, (SELECT COALESCE(MAX(seq), 0) + 1 FROM zeep_system.builder_turns WHERE session_id = $1), $2, $3)
		 RETURNING `+turnColumns,
		sessionID, KindRevert, payload,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == oneActiveTurnIndex {
			return nil, ErrTurnActive
		}
		return nil, fmt.Errorf("builder store: create revert turn: insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("builder store: create revert turn: commit: %w", err)
	}
	return turn, nil
}

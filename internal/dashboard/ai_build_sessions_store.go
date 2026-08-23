package dashboard

// ai_build_sessions_store.go — session/message persistence and lifecycle
// transitions (in_progress -> completed/abandoned) for the "Build with AI"
// chat. Every session and message is scoped to owner_user_id (AIBC-11); no
// query here ever reads or writes another user's session. See
// .specs/features/ai-build-chat/design.md's Components section.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AIBuildSession is a row from zeep_system.ai_build_sessions.
type AIBuildSession struct {
	ID           string    `json:"id"`
	OwnerUserID  string    `json:"owner_user_id"`
	Status       string    `json:"status"`
	CreatedAppID *string   `json:"created_app_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AIBuildMessage is a row from zeep_system.ai_build_messages.
type AIBuildMessage struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Plan      json.RawMessage `json:"plan,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// GetOrCreateInProgressSession resumes userID's existing in_progress
// session (with its full message history, oldest first) if one exists
// (AIBC-07), or creates a fresh one scoped to userID (AIBC-08).
func GetOrCreateInProgressSession(ctx context.Context, pool *db.Pool, userID string) (*AIBuildSession, []AIBuildMessage, error) {
	session, err := findInProgressSession(ctx, pool, userID)
	if err != nil {
		return nil, nil, err
	}
	if session != nil {
		messages, err := listMessages(ctx, pool, session.ID)
		if err != nil {
			return nil, nil, err
		}
		return session, messages, nil
	}

	created, err := createSession(ctx, pool, userID)
	if err != nil {
		return nil, nil, err
	}
	return created, []AIBuildMessage{}, nil
}

// AppendMessage persists one chat turn (user or assistant) on sessionID.
// plan is nil except on the assistant message carrying a propose_app_plan
// result (AIBC-14).
func AppendMessage(ctx context.Context, pool *db.Pool, sessionID string, role string, content string, plan json.RawMessage) error {
	var planParam any
	if len(plan) > 0 {
		planParam = plan
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.ai_build_messages (session_id, role, content, plan_json)
		 VALUES ($1, $2, $3, $4)`,
		sessionID, role, content, planParam,
	)
	if err != nil {
		return fmt.Errorf("dashboard: append ai build message: %w", err)
	}
	return nil
}

// AbandonAndRestartSession marks userID's current in_progress session (if
// any) as abandoned — preserving its messages — and creates a fresh
// in_progress session (AIBC-09). If userID has no in_progress session,
// this simply creates a new one.
func AbandonAndRestartSession(ctx context.Context, pool *db.Pool, userID string) (*AIBuildSession, error) {
	if _, err := pool.Exec(ctx,
		`UPDATE zeep_system.ai_build_sessions
		 SET status = 'abandoned', updated_at = now()
		 WHERE owner_user_id = $1 AND status = 'in_progress'`,
		userID,
	); err != nil {
		return nil, fmt.Errorf("dashboard: abandon ai build session: %w", err)
	}

	return createSession(ctx, pool, userID)
}

// CompleteSession marks sessionID completed and records the created app's
// ID. Called on full confirm success (AIBC-10).
func CompleteSession(ctx context.Context, pool *db.Pool, sessionID string, appID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.ai_build_sessions
		 SET status = 'completed', created_app_id = $2, updated_at = now()
		 WHERE id = $1`,
		sessionID, appID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: complete ai build session: %w", err)
	}
	return nil
}

// SetSessionCreatedApp records appID on sessionID without changing status.
// Called right after CreateAppForUser succeeds, before the per-table loop,
// so a partial confirm failure still leaves created_app_id pointing at
// what was actually created (AIBC-22) — independent of CompleteSession.
func SetSessionCreatedApp(ctx context.Context, pool *db.Pool, sessionID string, appID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.ai_build_sessions
		 SET created_app_id = $2, updated_at = now()
		 WHERE id = $1`,
		sessionID, appID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: set ai build session created app: %w", err)
	}
	return nil
}

func findInProgressSession(ctx context.Context, pool *db.Pool, userID string) (*AIBuildSession, error) {
	var s AIBuildSession
	err := pool.QueryRow(ctx,
		`SELECT id, owner_user_id, status, created_app_id, created_at, updated_at
		 FROM zeep_system.ai_build_sessions
		 WHERE owner_user_id = $1 AND status = 'in_progress'
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userID,
	).Scan(&s.ID, &s.OwnerUserID, &s.Status, &s.CreatedAppID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: find in-progress ai build session: %w", err)
	}
	return &s, nil
}

func createSession(ctx context.Context, pool *db.Pool, userID string) (*AIBuildSession, error) {
	var s AIBuildSession
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.ai_build_sessions (owner_user_id)
		 VALUES ($1)
		 RETURNING id, owner_user_id, status, created_app_id, created_at, updated_at`,
		userID,
	).Scan(&s.ID, &s.OwnerUserID, &s.Status, &s.CreatedAppID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create ai build session: %w", err)
	}
	return &s, nil
}

func listMessages(ctx context.Context, pool *db.Pool, sessionID string) ([]AIBuildMessage, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, session_id, role, content, plan_json, created_at
		 FROM zeep_system.ai_build_messages
		 WHERE session_id = $1
		 ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list ai build messages: %w", err)
	}
	defer rows.Close()

	messages := []AIBuildMessage{}
	for rows.Next() {
		var m AIBuildMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Plan, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: scan ai build message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, nil
}

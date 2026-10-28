// Package store is the Postgres persistence layer for the AI Frontend
// Builder (ai-frontend-builder spec, T12): sessions, turns, messages, the
// event log, snapshots and the turn lease.
//
// Concurrency model: nothing is held in process memory. Turns are a queue in
// zeep_system.builder_turns; any Orbit replica claims one with
// FOR UPDATE SKIP LOCKED and owns it through a row lease (lease_owner /
// lease_until). Every write the agent loop performs for a running turn goes
// through a Lease and is rejected with ErrLeaseLost once another replica has
// taken the turn over. User-facing reads and writes are scoped to the session
// owner; a session that belongs to someone else is reported as ErrNotFound so
// its existence is not disclosed.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Typed errors. Messages are English and carry no internal detail, so they are
// safe to surface; callers map them to HTTP statuses.
var (
	// ErrNotFound: the row does not exist, or it belongs to another user.
	ErrNotFound = errors.New("not found")
	// ErrTurnActive: the session already has a non-terminal turn.
	ErrTurnActive = errors.New("session already has an active turn")
	// ErrLeaseLost: the caller no longer owns the turn (lease taken over,
	// turn finished or session removed). The caller must stop without any
	// further side effects.
	ErrLeaseLost = errors.New("turn lease lost")
	// ErrNoTurn: ClaimTurn found nothing claimable.
	ErrNoTurn = errors.New("no claimable turn")
	// ErrSessionArchived: the session is archived and accepts no new turns.
	ErrSessionArchived = errors.New("session is archived")
	// ErrInvalidArgument: a caller-supplied value is not allowed.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Turn statuses (builder_turns.status).
const (
	TurnPending              = "pending"
	TurnQueued               = "queued"
	TurnRunning              = "running"
	TurnAwaitingConfirmation = "awaiting_confirmation"
	TurnCompleted            = "completed"
	TurnFailed               = "failed"
	TurnLimitReached         = "limit_reached"
	TurnBudgetExceeded       = "budget_exceeded"
)

// Turn kinds (builder_turns.kind).
const (
	KindChat   = "chat"
	KindRevert = "revert"
)

// Session statuses (builder_sessions.status).
const (
	SessionIdle                 = "idle"
	SessionRunning              = "running"
	SessionQueued               = "queued"
	SessionAwaitingConfirmation = "awaiting_confirmation"
	SessionFailed               = "failed"
	SessionArchived             = "archived"
)

// Message roles (builder_messages.role).
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleSummary   = "summary"
)

// Backend origins (builder_sessions.backend_origin): whether the session's
// backend app already existed or was created by the builder for this session.
const (
	BackendOriginExisting = "existing"
	BackendOriginBuilder  = "builder"
)

// Template kinds (builder_sessions.template_kind).
const (
	TemplateDefault = "default"
	TemplateGitHub  = "github"
)

const (
	// DefaultEventsLimit and MaxEventsLimit bound one ListEvents page.
	DefaultEventsLimit = 200
	MaxEventsLimit     = 500

	// Constraint name of the partial unique index that allows one
	// non-terminal turn per session (see provisioner.go).
	oneActiveTurnIndex = "builder_turns_one_active_idx"
	// SQLSTATE unique_violation.
	pgUniqueViolation = "23505"
)

// Store is the builder persistence layer. Safe for concurrent use.
type Store struct {
	pool *db.Pool
}

// New returns a Store on pool.
func New(pool *db.Pool) *Store {
	return &Store{pool: pool}
}

// Session is a row of zeep_system.builder_sessions.
type Session struct {
	ID              string
	OwnerUserID     string
	BackendAppID    string
	Title           string
	TemplateKind    string
	TemplateID      *string
	TemplateVersion string
	PromptVersionID *string
	Locale          string
	Status          string
	HeadSHA         string
	HeadTurnSeq     int
	FrontendAppID   *string
	PublishedSHA    string
	TokensIn        int64
	TokensOut       int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// BackendOrigin is BackendOriginExisting or BackendOriginBuilder.
	BackendOrigin string
}

// Turn is a row of zeep_system.builder_turns.
type Turn struct {
	ID              string
	SessionID       string
	Seq             int
	Kind            string
	Status          string
	PendingProposal json.RawMessage
	CommitSHA       string
	LeaseOwner      *string
	LeaseUntil      *time.Time
	ErrorCode       *string
	// UserMessage is the user's message that opened the turn. Set only by
	// ListTurns; empty for revert turns and everywhere else.
	UserMessage string
	CreatedAt   time.Time
	FinishedAt  *time.Time
}

// Message is a row of zeep_system.builder_messages.
type Message struct {
	ID         int64
	SessionID  string
	TurnID     string
	Role       string
	Content    string
	ToolCalls  json.RawMessage
	ToolCallID string
	CreatedAt  time.Time
}

// Event is a row of zeep_system.builder_events. ID is the polling cursor.
type Event struct {
	ID        int64
	SessionID string
	TurnID    *string
	Type      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// Snapshot is a row of zeep_system.builder_snapshots: the incremental git
// bundle that takes a workspace from the previous snapshot to CommitSHA
// (turn 0, part 0 is the full initial tree). A turn that commits several times
// has one row per commit, numbered by Part from 0 in save order.
type Snapshot struct {
	SessionID string
	TurnSeq   int
	Part      int
	CommitSHA string
	Bundle    []byte
	SizeBytes int
	CreatedAt time.Time
}

// Lease identifies the claimant of a running turn. Every loop write takes one.
type Lease struct {
	TurnID string
	Owner  string
}

// NewMessage is a message to append.
type NewMessage struct {
	Role       string
	Content    string
	ToolCalls  json.RawMessage // optional JSON array (assistant messages)
	ToolCallID string          // set on tool results
}

// CreateSessionParams are the inputs of CreateSession.
type CreateSessionParams struct {
	OwnerUserID     string
	BackendAppID    string
	Title           string
	TemplateKind    string
	TemplateID      *string
	TemplateVersion string
	PromptVersionID *string
	Locale          string
	// BackendOrigin defaults to BackendOriginExisting when empty.
	BackendOrigin string
}

const sessionColumns = `id, owner_user_id, backend_app_id, title, template_kind, template_id,
	template_version, prompt_version_id, locale, status, head_sha, head_turn_seq,
	frontend_app_id, published_sha, tokens_in, tokens_out, created_at, updated_at, backend_origin`

const turnColumns = `id, session_id, seq, kind, status, pending_proposal, commit_sha,
	lease_owner, lease_until, error_code, created_at, finished_at`

func scanSession(row pgx.Row) (*Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.OwnerUserID, &s.BackendAppID, &s.Title, &s.TemplateKind, &s.TemplateID,
		&s.TemplateVersion, &s.PromptVersionID, &s.Locale, &s.Status, &s.HeadSHA, &s.HeadTurnSeq,
		&s.FrontendAppID, &s.PublishedSHA, &s.TokensIn, &s.TokensOut, &s.CreatedAt, &s.UpdatedAt, &s.BackendOrigin)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func scanTurn(row pgx.Row) (*Turn, error) {
	var t Turn
	err := row.Scan(&t.ID, &t.SessionID, &t.Seq, &t.Kind, &t.Status, &t.PendingProposal, &t.CommitSHA,
		&t.LeaseOwner, &t.LeaseUntil, &t.ErrorCode, &t.CreatedAt, &t.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ---- sessions ----

// CreateSession inserts a session owned by p.OwnerUserID. Authorization on the
// backend app (>= viewer) is the caller's job.
func (s *Store) CreateSession(ctx context.Context, p CreateSessionParams) (*Session, error) {
	if p.OwnerUserID == "" || p.BackendAppID == "" || p.TemplateVersion == "" {
		return nil, fmt.Errorf("%w: owner, backend app and template version are required", ErrInvalidArgument)
	}
	if p.TemplateKind != TemplateDefault && p.TemplateKind != TemplateGitHub {
		return nil, fmt.Errorf("%w: template kind must be %q or %q", ErrInvalidArgument, TemplateDefault, TemplateGitHub)
	}
	locale := p.Locale
	if locale == "" {
		locale = "en"
	}
	origin := p.BackendOrigin
	if origin == "" {
		origin = BackendOriginExisting
	}
	if origin != BackendOriginExisting && origin != BackendOriginBuilder {
		return nil, fmt.Errorf("%w: backend origin must be %q or %q", ErrInvalidArgument, BackendOriginExisting, BackendOriginBuilder)
	}
	sess, err := scanSession(s.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_sessions
		   (owner_user_id, backend_app_id, title, template_kind, template_id, template_version, prompt_version_id, locale, backend_origin)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+sessionColumns,
		p.OwnerUserID, p.BackendAppID, p.Title, p.TemplateKind, p.TemplateID, p.TemplateVersion, p.PromptVersionID, locale, origin,
	))
	if err != nil {
		return nil, fmt.Errorf("builder store: create session: %w", err)
	}
	return sess, nil
}

// GetSession returns the session when it belongs to ownerUserID, else ErrNotFound.
func (s *Store) GetSession(ctx context.Context, ownerUserID, sessionID string) (*Session, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM zeep_system.builder_sessions WHERE id = $1 AND owner_user_id = $2`,
		sessionID, ownerUserID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: get session: %w", err)
	}
	return sess, nil
}

// GetSessionByID is the internal, non-owner-scoped lookup for the worker and
// loop, which act on behalf of the system. Never expose its result to a user
// without an ownership check.
func (s *Store) GetSessionByID(ctx context.Context, sessionID string) (*Session, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM zeep_system.builder_sessions WHERE id = $1`, sessionID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: get session: %w", err)
	}
	return sess, nil
}

// ListSessions returns ownerUserID's non-archived sessions, most recently
// updated first.
func (s *Store) ListSessions(ctx context.Context, ownerUserID string) ([]Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM zeep_system.builder_sessions
		 WHERE owner_user_id = $1 AND status <> 'archived'
		 ORDER BY updated_at DESC, id`,
		ownerUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("builder store: list sessions: %w", err)
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("builder store: scan session: %w", err)
		}
		out = append(out, *sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list sessions: %w", err)
	}
	return out, nil
}

// ArchiveSession marks the owner's session archived. Archived sessions are
// never claimed by the worker and accept no new turns. Idempotent.
func (s *Store) ArchiveSession(ctx context.Context, ownerUserID, sessionID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE zeep_system.builder_sessions SET status = 'archived', updated_at = now()
		 WHERE id = $1 AND owner_user_id = $2`,
		sessionID, ownerUserID,
	)
	if err != nil {
		return fmt.Errorf("builder store: archive session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- turns ----

// CreateTurn enqueues a new pending turn on the owner's session and, when
// userMessage is not empty, records it as the user message of that turn. The
// session row is locked to serialize seq allocation; the partial unique index
// builder_turns_one_active_idx is what actually enforces "one active turn" and
// is reported as ErrTurnActive.
func (s *Store) CreateTurn(ctx context.Context, ownerUserID, sessionID, kind, userMessage string) (*Turn, error) {
	if kind != KindChat && kind != KindRevert {
		return nil, fmt.Errorf("%w: unknown turn kind", ErrInvalidArgument)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("builder store: create turn: begin: %w", err)
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
		return nil, fmt.Errorf("builder store: create turn: lock session: %w", err)
	}
	if status == SessionArchived {
		return nil, ErrSessionArchived
	}

	turn, err := scanTurn(tx.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_turns (session_id, seq, kind)
		 VALUES ($1, (SELECT COALESCE(MAX(seq), 0) + 1 FROM zeep_system.builder_turns WHERE session_id = $1), $2)
		 RETURNING `+turnColumns,
		sessionID, kind,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == oneActiveTurnIndex {
			return nil, ErrTurnActive
		}
		return nil, fmt.Errorf("builder store: create turn: insert: %w", err)
	}
	if userMessage != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO zeep_system.builder_messages (session_id, turn_id, role, content) VALUES ($1, $2, 'user', $3)`,
			sessionID, turn.ID, userMessage,
		); err != nil {
			return nil, fmt.Errorf("builder store: create turn: insert message: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("builder store: create turn: commit: %w", err)
	}
	return turn, nil
}

// GetTurn returns a turn by id (internal, not owner-scoped).
func (s *Store) GetTurn(ctx context.Context, turnID string) (*Turn, error) {
	t, err := scanTurn(s.pool.QueryRow(ctx,
		`SELECT `+turnColumns+` FROM zeep_system.builder_turns WHERE id = $1`, turnID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: get turn: %w", err)
	}
	return t, nil
}

// ListTurns returns the turns of the owner's session in seq order.
func (s *Store) ListTurns(ctx context.Context, ownerUserID, sessionID string) ([]Turn, error) {
	if _, err := s.GetSession(ctx, ownerUserID, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+turnColumns+` FROM zeep_system.builder_turns WHERE session_id = $1 ORDER BY seq`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("builder store: list turns: %w", err)
	}
	defer rows.Close()
	out := []Turn{}
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, fmt.Errorf("builder store: scan turn: %w", err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list turns: %w", err)
	}
	rows.Close()

	// Attach each turn's user message so a reloaded page can rebuild the chat.
	mrows, err := s.pool.Query(ctx,
		`SELECT turn_id, content FROM zeep_system.builder_messages
		 WHERE session_id = $1 AND role = 'user' AND turn_id IS NOT NULL
		 ORDER BY id`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("builder store: list turns: messages: %w", err)
	}
	defer mrows.Close()
	byTurn := map[string]string{}
	for mrows.Next() {
		var turnID, content string
		if err := mrows.Scan(&turnID, &content); err != nil {
			return nil, fmt.Errorf("builder store: list turns: scan message: %w", err)
		}
		if _, seen := byTurn[turnID]; !seen {
			byTurn[turnID] = content
		}
	}
	if err := mrows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list turns: messages: %w", err)
	}
	for i := range out {
		out[i].UserMessage = byTurn[out[i].ID]
	}
	return out, nil
}

// ClaimTurn atomically takes the oldest claimable turn for owner and gives it a
// lease of leaseTTL: a pending or queued turn, or a running turn whose lease
// has expired (its worker died). Concurrent callers on any replica never get
// the same turn (FOR UPDATE SKIP LOCKED). Turns of archived sessions are
// skipped. Returns ErrNoTurn when nothing is claimable. The session moves to
// 'running'.
func (s *Store) ClaimTurn(ctx context.Context, owner string, leaseTTL time.Duration) (*Turn, error) {
	if owner == "" || leaseTTL <= 0 {
		return nil, fmt.Errorf("%w: owner and a positive lease TTL are required", ErrInvalidArgument)
	}
	t, err := scanTurn(s.pool.QueryRow(ctx,
		`WITH claimed AS (
		   UPDATE zeep_system.builder_turns
		   SET lease_owner = $1, lease_until = now() + make_interval(secs => $2), status = 'running'
		   WHERE id = (
		     SELECT t.id FROM zeep_system.builder_turns t
		     JOIN zeep_system.builder_sessions s ON s.id = t.session_id
		     WHERE s.status <> 'archived'
		       AND (t.status IN ('pending','queued') OR (t.status = 'running' AND t.lease_until < now()))
		     ORDER BY t.created_at, t.id
		     LIMIT 1
		     FOR UPDATE OF t SKIP LOCKED)
		   RETURNING `+turnColumns+`
		 ), sess AS (
		   UPDATE zeep_system.builder_sessions SET status = 'running', updated_at = now()
		   WHERE id = (SELECT session_id FROM claimed)
		 )
		 SELECT `+turnColumns+` FROM claimed`,
		owner, leaseTTL.Seconds(),
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoTurn
		}
		return nil, fmt.Errorf("builder store: claim turn: %w", err)
	}
	return t, nil
}

// PeekClaimable returns the turn ClaimTurn would take next (oldest pending or
// queued, or running with an expired lease) without locking or changing it, so
// the worker can check builder capacity before claiming. It is advisory: a
// concurrent worker may claim the turn first. ErrNoTurn when nothing is
// claimable.
func (s *Store) PeekClaimable(ctx context.Context) (*Turn, error) {
	t, err := scanTurn(s.pool.QueryRow(ctx,
		`SELECT t.id, t.session_id, t.seq, t.kind, t.status, t.pending_proposal, t.commit_sha,
		        t.lease_owner, t.lease_until, t.error_code, t.created_at, t.finished_at
		 FROM zeep_system.builder_turns t
		 JOIN zeep_system.builder_sessions s ON s.id = t.session_id
		 WHERE s.status <> 'archived'
		   AND (t.status IN ('pending','queued') OR (t.status = 'running' AND t.lease_until < now()))
		 ORDER BY t.created_at, t.id
		 LIMIT 1`,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoTurn
		}
		return nil, fmt.Errorf("builder store: peek claimable: %w", err)
	}
	return t, nil
}

// MarkQueued moves a pending turn to 'queued' (builder at capacity) and appends
// a single 'queued' event, atomically. It reports whether the turn changed, so
// the caller emits the event once even if it polls repeatedly.
func (s *Store) MarkQueued(ctx context.Context, turnID string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("builder store: mark queued: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var sessionID string
	err = tx.QueryRow(ctx,
		`UPDATE zeep_system.builder_turns SET status = 'queued' WHERE id = $1 AND status = 'pending' RETURNING session_id`,
		turnID,
	).Scan(&sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("builder store: mark queued: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_sessions SET status = 'queued', updated_at = now()
		 WHERE id = $1 AND status <> 'archived'`, sessionID,
	); err != nil {
		return false, fmt.Errorf("builder store: mark queued: session: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type) VALUES ($1, $2, 'queued')`,
		sessionID, turnID,
	); err != nil {
		return false, fmt.Errorf("builder store: mark queued: event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("builder store: mark queued: commit: %w", err)
	}
	return true, nil
}

// RenewLease extends the lease by ttl. ErrLeaseLost when l no longer owns a
// running turn.
func (s *Store) RenewLease(ctx context.Context, l Lease, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("%w: ttl must be positive", ErrInvalidArgument)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE zeep_system.builder_turns SET lease_until = now() + make_interval(secs => $3)
		 WHERE id = $1 AND lease_owner = $2 AND status = 'running'`,
		l.TurnID, l.Owner, ttl.Seconds(),
	)
	if err != nil {
		return fmt.Errorf("builder store: renew lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// FinishParams describes how a leased turn ends.
type FinishParams struct {
	// Status is one of TurnCompleted, TurnFailed, TurnLimitReached,
	// TurnBudgetExceeded (terminal) or TurnAwaitingConfirmation (pause).
	Status string
	// CommitSHA is the turn's commit; empty when nothing was committed. A
	// non-empty value also advances the session head.
	CommitSHA string
	// ErrorCode is a generic code (never a raw provider error) for failed turns.
	ErrorCode string
	// PendingProposal is required for TurnAwaitingConfirmation.
	PendingProposal json.RawMessage
}

// FinishTurn ends or pauses a leased turn and updates the session status
// (completed/limit_reached/budget_exceeded -> idle, failed -> failed,
// awaiting_confirmation -> awaiting_confirmation; an archived session stays
// archived). The lease is released, so a terminal turn frees the session for
// the next one. ErrLeaseLost when l no longer owns the turn.
func (s *Store) FinishTurn(ctx context.Context, l Lease, p FinishParams) error {
	var sessionStatus string
	terminal := true
	switch p.Status {
	case TurnCompleted, TurnLimitReached, TurnBudgetExceeded:
		sessionStatus = SessionIdle
	case TurnFailed:
		sessionStatus = SessionFailed
	case TurnAwaitingConfirmation:
		sessionStatus = SessionAwaitingConfirmation
		terminal = false
		if len(p.PendingProposal) == 0 {
			return fmt.Errorf("%w: a pending proposal is required to await confirmation", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: status %q cannot end a turn", ErrInvalidArgument, p.Status)
	}

	var proposal any
	if len(p.PendingProposal) > 0 {
		proposal = []byte(p.PendingProposal)
	}
	var errorCode any
	if p.ErrorCode != "" {
		errorCode = p.ErrorCode
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("builder store: finish turn: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var sessionID string
	var seq int
	err = tx.QueryRow(ctx,
		`UPDATE zeep_system.builder_turns
		 SET status = $3, commit_sha = $4, error_code = $5, pending_proposal = $6,
		     lease_owner = NULL, lease_until = NULL,
		     finished_at = CASE WHEN $7 THEN now() ELSE NULL END
		 WHERE id = $1 AND lease_owner = $2 AND status = 'running'
		 RETURNING session_id, seq`,
		l.TurnID, l.Owner, p.Status, p.CommitSHA, errorCode, proposal, terminal,
	).Scan(&sessionID, &seq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("builder store: finish turn: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_sessions
		 SET status = $2,
		     head_sha = CASE WHEN $3 <> '' THEN $3 ELSE head_sha END,
		     head_turn_seq = CASE WHEN $3 <> '' THEN $4 ELSE head_turn_seq END,
		     updated_at = now()
		 WHERE id = $1 AND status <> 'archived'`,
		sessionID, sessionStatus, p.CommitSHA, seq,
	); err != nil {
		return fmt.Errorf("builder store: finish turn: session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("builder store: finish turn: commit: %w", err)
	}
	return nil
}

// ---- lease-checked writes ----

// AppendMessage persists a message on the leased turn's session. ErrLeaseLost
// when l no longer owns a running turn.
func (s *Store) AppendMessage(ctx context.Context, l Lease, m NewMessage) (*Message, error) {
	switch m.Role {
	case RoleUser, RoleAssistant, RoleTool, RoleSummary:
	default:
		return nil, fmt.Errorf("%w: unknown message role", ErrInvalidArgument)
	}
	var toolCalls any
	if len(m.ToolCalls) > 0 {
		toolCalls = []byte(m.ToolCalls)
	}
	var toolCallID any
	if m.ToolCallID != "" {
		toolCallID = m.ToolCallID
	}
	var out Message
	var tcID *string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_messages (session_id, turn_id, role, content, tool_calls, tool_call_id)
		 SELECT t.session_id, t.id, $3::text, $4::text, $5::jsonb, $6::text
		 FROM zeep_system.builder_turns t
		 WHERE t.id = $1 AND t.lease_owner = $2 AND t.status = 'running'
		 RETURNING id, session_id, turn_id, role, content, tool_calls, tool_call_id, created_at`,
		l.TurnID, l.Owner, m.Role, m.Content, toolCalls, toolCallID,
	).Scan(&out.ID, &out.SessionID, &out.TurnID, &out.Role, &out.Content, &out.ToolCalls, &tcID, &out.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLeaseLost
		}
		return nil, fmt.Errorf("builder store: append message: %w", err)
	}
	if tcID != nil {
		out.ToolCallID = *tcID
	}
	return &out, nil
}

// AppendEvent appends an event on the leased turn's session and returns its id
// (the polling cursor). payload is marshaled to JSON; nil becomes {}. Callers
// must never put secrets, file contents or raw provider errors in it.
func (s *Store) AppendEvent(ctx context.Context, l Lease, eventType string, payload any) (int64, error) {
	if eventType == "" {
		return 0, fmt.Errorf("%w: event type is required", ErrInvalidArgument)
	}
	raw, err := marshalPayload(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type, payload)
		 SELECT t.session_id, t.id, $3::text, $4::jsonb
		 FROM zeep_system.builder_turns t
		 WHERE t.id = $1 AND t.lease_owner = $2 AND t.status = 'running'
		 RETURNING id`,
		l.TurnID, l.Owner, eventType, raw,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrLeaseLost
		}
		return 0, fmt.Errorf("builder store: append event: %w", err)
	}
	return id, nil
}

// AppendSessionEvent appends an event outside a turn lease (publish events,
// proposal confirmation, handler-side notices). turnID may be empty. The
// caller is responsible for having authorized access to sessionID.
func (s *Store) AppendSessionEvent(ctx context.Context, sessionID, turnID, eventType string, payload any) (int64, error) {
	if eventType == "" {
		return 0, fmt.Errorf("%w: event type is required", ErrInvalidArgument)
	}
	raw, err := marshalPayload(payload)
	if err != nil {
		return 0, err
	}
	var turn any
	if turnID != "" {
		turn = turnID
	}
	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type, payload)
		 SELECT s.id, $2::uuid, $3::text, $4::jsonb FROM zeep_system.builder_sessions s WHERE s.id = $1
		 RETURNING id`,
		sessionID, turn, eventType, raw,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("builder store: append session event: %w", err)
	}
	return id, nil
}

func marshalPayload(payload any) ([]byte, error) {
	if payload == nil {
		return []byte("{}"), nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: event payload is not JSON-serializable", ErrInvalidArgument)
	}
	return raw, nil
}

// ---- reads for the loop and the polling API ----

// ListEvents returns the owner's session events with id > afterID, oldest
// first, at most limit (default DefaultEventsLimit, capped at MaxEventsLimit).
// ErrNotFound when the session is not the owner's.
func (s *Store) ListEvents(ctx context.Context, ownerUserID, sessionID string, afterID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultEventsLimit
	}
	if limit > MaxEventsLimit {
		limit = MaxEventsLimit
	}
	rows, err := s.pool.Query(ctx,
		`SELECT e.id, e.session_id, e.turn_id, e.type, e.payload, e.created_at
		 FROM zeep_system.builder_events e
		 JOIN zeep_system.builder_sessions s ON s.id = e.session_id
		 WHERE e.session_id = $1 AND s.owner_user_id = $2 AND e.id > $3
		 ORDER BY e.id
		 LIMIT $4`,
		sessionID, ownerUserID, afterID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("builder store: list events: %w", err)
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.SessionID, &e.TurnID, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("builder store: scan event: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list events: %w", err)
	}
	if len(out) == 0 {
		// Distinguish "nothing new" from "not your session".
		if _, err := s.GetSession(ctx, ownerUserID, sessionID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListMessages returns every message of a session in insertion order. Internal
// (the loop rebuilds the conversation from it); not owner-scoped.
func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, session_id, turn_id, role, content, tool_calls, tool_call_id, created_at
		 FROM zeep_system.builder_messages WHERE session_id = $1 ORDER BY id`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("builder store: list messages: %w", err)
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var tcID *string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.TurnID, &m.Role, &m.Content, &m.ToolCalls, &tcID, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("builder store: scan message: %w", err)
		}
		if tcID != nil {
			m.ToolCallID = *tcID
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list messages: %w", err)
	}
	return out, nil
}

// ---- snapshots ----

// SaveSnapshot appends the git bundle of one commit of the leased turn as the
// next part (0, 1, ...) of that turn's snapshots. Not idempotent: every call
// adds a row, so callers save each commit once. The part is assigned inside a
// transaction that first locks the turn row, which both checks the lease and
// serializes concurrent saves of the same turn (without the lock two saves
// could read the same max(part) and one would hit the primary key).
// ErrLeaseLost when l no longer owns a running turn.
func (s *Store) SaveSnapshot(ctx context.Context, l Lease, commitSHA string, bundle []byte) error {
	if commitSHA == "" || len(bundle) == 0 {
		return fmt.Errorf("%w: commit sha and bundle are required", ErrInvalidArgument)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("builder store: save snapshot: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var sessionID string
	var turnSeq int
	err = tx.QueryRow(ctx,
		`SELECT session_id, seq FROM zeep_system.builder_turns
		 WHERE id = $1 AND lease_owner = $2 AND status = 'running'
		 FOR NO KEY UPDATE`,
		l.TurnID, l.Owner,
	).Scan(&sessionID, &turnSeq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("builder store: save snapshot: lease: %w", err)
	}
	// A separate statement from the lock above: under READ COMMITTED it takes
	// a fresh snapshot, so it sees parts committed by a save that held the lock
	// before us.
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_snapshots (session_id, turn_seq, part, commit_sha, bundle, size_bytes)
		 SELECT $1::uuid, $2::int, COALESCE(MAX(part) + 1, 0), $3::text, $4::bytea, $5::int
		 FROM zeep_system.builder_snapshots WHERE session_id = $1::uuid AND turn_seq = $2::int`,
		sessionID, turnSeq, commitSHA, bundle, len(bundle),
	); err != nil {
		return fmt.Errorf("builder store: save snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("builder store: save snapshot: commit: %w", err)
	}
	return nil
}

// SaveInitialSnapshot stores the turn-0, part-0 snapshot (the full initial tree) at
// session creation, before any turn exists. Owner-scoped; also records the
// session head. Idempotent.
func (s *Store) SaveInitialSnapshot(ctx context.Context, ownerUserID, sessionID, commitSHA string, bundle []byte) error {
	if commitSHA == "" || len(bundle) == 0 {
		return fmt.Errorf("%w: commit sha and bundle are required", ErrInvalidArgument)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("builder store: save initial snapshot: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE zeep_system.builder_sessions SET head_sha = $3, head_turn_seq = 0, updated_at = now()
		 WHERE id = $1 AND owner_user_id = $2`,
		sessionID, ownerUserID, commitSHA,
	)
	if err != nil {
		return fmt.Errorf("builder store: save initial snapshot: session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.builder_snapshots (session_id, turn_seq, part, commit_sha, bundle, size_bytes)
		 VALUES ($1, 0, 0, $2, $3, $4)
		 ON CONFLICT (session_id, turn_seq, part)
		 DO UPDATE SET commit_sha = EXCLUDED.commit_sha, bundle = EXCLUDED.bundle,
		               size_bytes = EXCLUDED.size_bytes, created_at = now()`,
		sessionID, commitSHA, bundle, len(bundle),
	); err != nil {
		return fmt.Errorf("builder store: save initial snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("builder store: save initial snapshot: commit: %w", err)
	}
	return nil
}

// ListSnapshots returns every snapshot of a session in (turn_seq, part) order,
// bundles included (the builder replays them in order to restore a workspace).
// Internal; not owner-scoped.
func (s *Store) ListSnapshots(ctx context.Context, sessionID string) ([]Snapshot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT session_id, turn_seq, part, commit_sha, bundle, size_bytes, created_at
		 FROM zeep_system.builder_snapshots WHERE session_id = $1 ORDER BY turn_seq, part`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("builder store: list snapshots: %w", err)
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		var sn Snapshot
		if err := rows.Scan(&sn.SessionID, &sn.TurnSeq, &sn.Part, &sn.CommitSHA, &sn.Bundle, &sn.SizeBytes, &sn.CreatedAt); err != nil {
			return nil, fmt.Errorf("builder store: scan snapshot: %w", err)
		}
		out = append(out, sn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("builder store: list snapshots: %w", err)
	}
	return out, nil
}

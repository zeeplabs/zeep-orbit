package store

// usage.go — the per-call token ledger and the budget check the agent loop
// runs before every LLM call (ai-frontend-builder spec, T13, AFB-02/AFB-14).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// FeatureBuilder is the ai_usage_ledger.feature value of the AI Frontend Builder.
const FeatureBuilder = "builder"

// Budget errors. All of them wrap ErrBudgetExceeded, so the loop can end the
// turn with a single errors.Is(err, ErrBudgetExceeded) and still tell the
// reason apart.
var (
	// ErrBudgetExceeded is the umbrella: the next LLM call must not be made.
	ErrBudgetExceeded = errors.New("ai budget exceeded")
	// ErrBudgetNotConfigured: a cap is 0 (unset), so sessions are blocked until
	// a superadmin sets it (design: "0 = unset → sessions blocked").
	ErrBudgetNotConfigured = fmt.Errorf("%w: token caps are not configured", ErrBudgetExceeded)
	// ErrSessionCapReached: the session's tokens in+out reached the session cap.
	ErrSessionCapReached = fmt.Errorf("%w: session token cap reached", ErrBudgetExceeded)
	// ErrMonthlyCapReached: the user's tokens in+out this calendar month (UTC)
	// reached the monthly cap.
	ErrMonthlyCapReached = fmt.Errorf("%w: monthly token cap reached", ErrBudgetExceeded)
)

// UsageRecord is one LLM call's token usage.
type UsageRecord struct {
	UserID       string
	Feature      string // FeatureBuilder
	SessionID    string // optional; when set the session's token counters are updated too
	TurnID       string // optional
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
}

// RecordUsage appends one ledger row and, when SessionID is set, adds the
// tokens to builder_sessions.tokens_in/out in the same transaction, so the
// ledger and the session counter cannot diverge. ErrNotFound when SessionID
// does not exist (the transaction is rolled back).
func (s *Store) RecordUsage(ctx context.Context, r UsageRecord) error {
	if r.UserID == "" || r.Feature == "" || r.Provider == "" || r.Model == "" {
		return fmt.Errorf("%w: user, feature, provider and model are required", ErrInvalidArgument)
	}
	if r.InputTokens < 0 || r.OutputTokens < 0 {
		return fmt.Errorf("%w: token counts must not be negative", ErrInvalidArgument)
	}
	var sessionID, turnID any
	if r.SessionID != "" {
		sessionID = r.SessionID
	}
	if r.TurnID != "" {
		turnID = r.TurnID
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("builder store: record usage: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if r.SessionID != "" {
		tag, err := tx.Exec(ctx,
			`UPDATE zeep_system.builder_sessions
			 SET tokens_in = tokens_in + $2, tokens_out = tokens_out + $3
			 WHERE id = $1`,
			r.SessionID, r.InputTokens, r.OutputTokens,
		)
		if err != nil {
			return fmt.Errorf("builder store: record usage: session: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.ai_usage_ledger
		   (user_id, feature, session_id, turn_id, provider, model, input_tokens, output_tokens)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.UserID, r.Feature, sessionID, turnID, r.Provider, r.Model, r.InputTokens, r.OutputTokens,
	); err != nil {
		return fmt.Errorf("builder store: record usage: ledger: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("builder store: record usage: commit: %w", err)
	}
	return nil
}

// BudgetChecker enforces the session and per-user monthly token caps from
// zeep_system.builder_settings.
type BudgetChecker struct {
	pool *db.Pool
	now  func() time.Time
}

// BudgetOption customizes a BudgetChecker.
type BudgetOption func(*BudgetChecker)

// WithClock replaces the clock used to find the current calendar month. For tests.
func WithClock(now func() time.Time) BudgetOption {
	return func(b *BudgetChecker) { b.now = now }
}

// NewBudgetChecker returns a BudgetChecker on pool.
func NewBudgetChecker(pool *db.Pool, opts ...BudgetOption) *BudgetChecker {
	b := &BudgetChecker{pool: pool, now: time.Now}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Check reports whether another LLM call is allowed for userID on sessionID.
// It returns nil when allowed, ErrBudgetNotConfigured when either cap is 0 or
// the settings row is missing, ErrSessionCapReached when the session's tokens
// (in + out) have reached the session cap, and ErrMonthlyCapReached when the
// user's builder tokens this UTC calendar month have reached the monthly cap.
// The cap is checked before the call, against what was already recorded: the
// call that crosses the cap is the last one. ErrNotFound when the session does
// not exist.
func (b *BudgetChecker) Check(ctx context.Context, userID, sessionID string) error {
	var sessionCap, monthlyCap int64
	err := b.pool.QueryRow(ctx,
		`SELECT session_token_cap, user_monthly_token_cap FROM zeep_system.builder_settings WHERE id = 1`,
	).Scan(&sessionCap, &monthlyCap)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBudgetNotConfigured
		}
		return fmt.Errorf("builder store: budget check: settings: %w", err)
	}
	if sessionCap <= 0 || monthlyCap <= 0 {
		return ErrBudgetNotConfigured
	}

	var sessionUsed int64
	err = b.pool.QueryRow(ctx,
		`SELECT tokens_in + tokens_out FROM zeep_system.builder_sessions WHERE id = $1`, sessionID,
	).Scan(&sessionUsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("builder store: budget check: session usage: %w", err)
	}
	if sessionUsed >= sessionCap {
		return ErrSessionCapReached
	}

	now := b.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var monthlyUsed int64
	err = b.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens::bigint + output_tokens::bigint), 0)
		 FROM zeep_system.ai_usage_ledger
		 WHERE user_id = $1 AND feature = $2 AND created_at >= $3 AND created_at < $4`,
		userID, FeatureBuilder, monthStart, monthStart.AddDate(0, 1, 0),
	).Scan(&monthlyUsed)
	if err != nil {
		return fmt.Errorf("builder store: budget check: monthly usage: %w", err)
	}
	if monthlyUsed >= monthlyCap {
		return ErrMonthlyCapReached
	}
	return nil
}

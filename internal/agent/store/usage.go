package store

// usage.go — the per-call token ledger and the budget check the agent loop
// runs before every LLM call (ai-frontend-builder spec, T13, AFB-02/AFB-14).

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/agent/catalog"
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
	// ErrSessionCostCapReached: the session's recorded cost reached the
	// session cost cap.
	ErrSessionCostCapReached = fmt.Errorf("%w: session cost cap reached", ErrBudgetExceeded)
	// ErrMonthlyCostCapReached: the user's recorded cost this calendar month
	// (UTC) reached the monthly cost cap.
	ErrMonthlyCostCapReached = fmt.Errorf("%w: monthly cost cap reached", ErrBudgetExceeded)
)

// The four caps, as they appear in the budget_warning and budget_exceeded
// event payloads.
const (
	BudgetCapSessionTokens = "session_tokens"
	BudgetCapMonthTokens   = "month_tokens"
	BudgetCapSessionCost   = "session_cost"
	BudgetCapMonthCost     = "month_cost"
)

// BudgetReasonNotConfigured is the BudgetReason of ErrBudgetNotConfigured.
const BudgetReasonNotConfigured = "not_configured"

// EventBudgetWarning is the event appended when usage crosses the warning
// threshold of a cap.
const EventBudgetWarning = "budget_warning"

// BudgetWarning says a cap is at or above the warning threshold (80%) but not
// reached yet. Used and Limit are tokens for the token caps and micros for
// the cost caps.
type BudgetWarning struct {
	Cap   string `json:"cap"`
	Used  int64  `json:"used"`
	Limit int64  `json:"limit"`
}

// BudgetReason names the cap behind a budget error: one of the BudgetCap*
// values, BudgetReasonNotConfigured, or "" for an error it does not know.
func BudgetReason(err error) string {
	switch {
	case errors.Is(err, ErrBudgetNotConfigured):
		return BudgetReasonNotConfigured
	case errors.Is(err, ErrSessionCapReached):
		return BudgetCapSessionTokens
	case errors.Is(err, ErrMonthlyCapReached):
		return BudgetCapMonthTokens
	case errors.Is(err, ErrSessionCostCapReached):
		return BudgetCapSessionCost
	case errors.Is(err, ErrMonthlyCostCapReached):
		return BudgetCapMonthCost
	}
	return ""
}

// Purposes of an LLM call (ai_usage_ledger.purpose). The column has no CHECK
// constraint, so a new purpose needs no DDL, only a constant here.
const (
	PurposeChat       = "chat"
	PurposeCompaction = "compaction"
	// PurposePlanning: a model call of a plan-mode turn (ai-builder-planning).
	PurposePlanning = "planning"
)

func validPurpose(p string) bool {
	return p == PurposeChat || p == PurposeCompaction || p == PurposePlanning
}

// UsageRecord is one LLM call's token usage. The three input classes do not
// overlap: InputTokens is only what was billed at the full input rate.
type UsageRecord struct {
	UserID            string
	Feature           string // FeatureBuilder
	SessionID         string // optional; when set the session's token counters are updated too
	TurnID            string // optional
	Provider          string
	Model             string
	Purpose           string // PurposeChat when empty
	InputTokens       int    // billed at the full input rate
	CachedInputTokens int    // read from the provider's prompt cache
	CacheWriteTokens  int    // written to the provider's prompt cache
	OutputTokens      int
	Reported          bool // false when the provider sent no usage object
}

// RecordUsage appends one ledger row and, when SessionID is set, adds the
// tokens to builder_sessions.tokens_in/out in the same transaction, so the
// ledger and the session counter cannot diverge. tokens_in counts every input
// class, cached or not, because the token caps are about volume, not price.
//
// The cost is computed here, from the price of (provider, model) as it is at
// this moment, and stored on the row: a later price change never rewrites
// history. A model with no price row gets cost_micros NULL ("no price set"),
// which is different from a price of zero (cost 0).
//
// ErrNotFound when SessionID does not exist (the transaction is rolled back).
func (s *Store) RecordUsage(ctx context.Context, r UsageRecord) error {
	if r.UserID == "" || r.Feature == "" || r.Provider == "" || r.Model == "" {
		return fmt.Errorf("%w: user, feature, provider and model are required", ErrInvalidArgument)
	}
	if r.InputTokens < 0 || r.CachedInputTokens < 0 || r.CacheWriteTokens < 0 || r.OutputTokens < 0 {
		return fmt.Errorf("%w: token counts must not be negative", ErrInvalidArgument)
	}
	if r.Purpose == "" {
		r.Purpose = PurposeChat
	}
	if !validPurpose(r.Purpose) {
		return fmt.Errorf("%w: unknown purpose", ErrInvalidArgument)
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
			r.SessionID, r.InputTokens+r.CachedInputTokens+r.CacheWriteTokens, r.OutputTokens,
		)
		if err != nil {
			return fmt.Errorf("builder store: record usage: session: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	cost, err := callCost(ctx, tx, r)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.ai_usage_ledger
		   (user_id, feature, session_id, turn_id, provider, model, input_tokens, output_tokens,
		    cached_input_tokens, cache_write_tokens, purpose, cost_micros, usage_reported)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		r.UserID, r.Feature, sessionID, turnID, r.Provider, r.Model, r.InputTokens, r.OutputTokens,
		r.CachedInputTokens, r.CacheWriteTokens, r.Purpose, cost, r.Reported,
	); err != nil {
		return fmt.Errorf("builder store: record usage: ledger: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("builder store: record usage: commit: %w", err)
	}
	return nil
}

// callCost prices one call inside the RecordUsage transaction. The providers
// of the model catalog (openai, anthropic) are priced by the catalog only; the
// others by their ai_model_prices row. It returns nil when the model has no
// price, and also when the cost does not fit the column (an absurd price must
// not fail a call whose tokens are already spent; the row then reads as "no
// price set").
func callCost(ctx context.Context, tx pgx.Tx, r UsageRecord) (*int64, error) {
	if catalog.Covers(r.Provider) {
		return catalogCost(r, time.Now().UTC()), nil
	}
	var p Price
	err := tx.QueryRow(ctx,
		`SELECT (input_per_mtok * 1000000)::bigint, (cached_input_per_mtok * 1000000)::bigint,
		        (cache_write_per_mtok * 1000000)::bigint, (output_per_mtok * 1000000)::bigint
		 FROM zeep_system.ai_model_prices WHERE provider = $1 AND model = $2`,
		r.Provider, r.Model,
	).Scan(&p.Input, &p.CachedInput, &p.CacheWrite, &p.Output)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("builder store: record usage: price: %w", err)
	}
	cost, err := CostMicros(p, r.InputTokens, r.CachedInputTokens, r.CacheWriteTokens, r.OutputTokens)
	if err != nil {
		return nil, nil
	}
	return &cost, nil
}

// catalogCost is the cost of a call of a catalog provider at time at: the
// price version in force, at the tier of the call's prompt size (every input
// class counts, as the providers bill it). nil when the model is not in the
// catalog or the cost cannot be computed.
func catalogCost(r UsageRecord, at time.Time) *int64 {
	m, ok := catalog.Lookup(r.Provider, r.Model)
	if !ok {
		return nil
	}
	v, ok := m.PriceAt(at)
	if !ok {
		return nil
	}
	p, err := tierPrice(v.TierFor(r.InputTokens + r.CachedInputTokens + r.CacheWriteTokens))
	if err != nil {
		return nil
	}
	cost, err := CostMicros(p, r.InputTokens, r.CachedInputTokens, r.CacheWriteTokens, r.OutputTokens)
	if err != nil {
		return nil
	}
	return &cost
}

// tierPrice converts a catalog tier into micros. The catalog test rejects
// malformed prices, so an error here means a broken release.
func tierPrice(t catalog.Tier) (Price, error) {
	var (
		p   Price
		err error
	)
	if p.Input, err = ParsePrice(t.Input); err != nil {
		return Price{}, err
	}
	if p.Output, err = ParsePrice(t.Output); err != nil {
		return Price{}, err
	}
	for _, opt := range []struct {
		value string
		dst   **int64
	}{{t.CachedInput, &p.CachedInput}, {t.CacheWrite, &p.CacheWrite}} {
		if opt.value == "" {
			continue
		}
		v, err := ParsePrice(opt.value)
		if err != nil {
			return Price{}, err
		}
		*opt.dst = &v
	}
	return p, nil
}

// BudgetChecker enforces the session and per-user monthly caps from
// zeep_system.builder_settings: the token caps, which are mandatory, and the
// cost caps, which are optional.
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
//
// Token caps: ErrBudgetNotConfigured when either is 0 or the settings row is
// missing, ErrSessionCapReached when the session's tokens (in + out) have
// reached the session cap, ErrMonthlyCapReached when the user's builder
// tokens this UTC calendar month have reached the monthly cap.
//
// Cost caps: 0 means no cap. ErrSessionCostCapReached and
// ErrMonthlyCostCapReached compare the recorded cost with the caps. Calls
// with no price have no cost and do not count, so a cost cap only protects
// models that have a price.
//
// Every cap is checked before the call, against what was already recorded:
// the call that crosses a cap is the last one. ErrNotFound when the session
// does not exist.
//
// When the call is allowed, the caps at or above 80% are returned as
// warnings, in a fixed order.
func (b *BudgetChecker) Check(ctx context.Context, userID, sessionID string) ([]BudgetWarning, error) {
	var sessionCap, monthlyCap, sessionCostCap, monthlyCostCap int64
	err := b.pool.QueryRow(ctx,
		`SELECT session_token_cap, user_monthly_token_cap,
		        COALESCE(session_cost_cap_micros, 0), COALESCE(user_monthly_cost_cap_micros, 0)
		 FROM zeep_system.builder_settings WHERE id = 1`,
	).Scan(&sessionCap, &monthlyCap, &sessionCostCap, &monthlyCostCap)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBudgetNotConfigured
		}
		return nil, fmt.Errorf("builder store: budget check: settings: %w", err)
	}
	if sessionCap <= 0 || monthlyCap <= 0 {
		return nil, ErrBudgetNotConfigured
	}

	var sessionUsed int64
	err = b.pool.QueryRow(ctx,
		`SELECT tokens_in + tokens_out FROM zeep_system.builder_sessions WHERE id = $1`, sessionID,
	).Scan(&sessionUsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("builder store: budget check: session usage: %w", err)
	}
	if sessionUsed >= sessionCap {
		return nil, ErrSessionCapReached
	}

	now := b.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var monthlyUsed, monthlyCost int64
	err = b.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens::bigint + cached_input_tokens::bigint
		                     + cache_write_tokens::bigint + output_tokens::bigint), 0),
		        COALESCE(SUM(cost_micros), 0)::bigint
		 FROM zeep_system.ai_usage_ledger
		 WHERE user_id = $1 AND feature = $2 AND created_at >= $3 AND created_at < $4`,
		userID, FeatureBuilder, monthStart, monthStart.AddDate(0, 1, 0),
	).Scan(&monthlyUsed, &monthlyCost)
	if err != nil {
		return nil, fmt.Errorf("builder store: budget check: monthly usage: %w", err)
	}
	if monthlyUsed >= monthlyCap {
		return nil, ErrMonthlyCapReached
	}

	var sessionCost int64
	if sessionCostCap > 0 {
		err = b.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(cost_micros), 0)::bigint FROM zeep_system.ai_usage_ledger WHERE session_id = $1`,
			sessionID,
		).Scan(&sessionCost)
		if err != nil {
			return nil, fmt.Errorf("builder store: budget check: session cost: %w", err)
		}
		if sessionCost >= sessionCostCap {
			return nil, ErrSessionCostCapReached
		}
	}
	if monthlyCostCap > 0 && monthlyCost >= monthlyCostCap {
		return nil, ErrMonthlyCostCapReached
	}

	var warnings []BudgetWarning
	for _, c := range []BudgetWarning{
		{BudgetCapSessionTokens, sessionUsed, sessionCap},
		{BudgetCapMonthTokens, monthlyUsed, monthlyCap},
		{BudgetCapSessionCost, sessionCost, sessionCostCap},
		{BudgetCapMonthCost, monthlyCost, monthlyCostCap},
	} {
		// 80%, in integers: used/limit >= 4/5.
		if c.Limit > 0 && c.Used*5 >= c.Limit*4 {
			warnings = append(warnings, c)
		}
	}
	return warnings, nil
}

// AppendBudgetWarning appends the budget_warning event of w on the leased
// turn's session, unless the session already has one for the same cap: a
// session is warned once per cap, whatever the number of turns and resumes.
// It reports whether the event was written. A lost lease also reports false;
// the caller's next leased write returns ErrLeaseLost.
func (s *Store) AppendBudgetWarning(ctx context.Context, l Lease, w BudgetWarning) (bool, error) {
	raw, err := marshalPayload(w)
	if err != nil {
		return false, err
	}
	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO zeep_system.builder_events (session_id, turn_id, type, payload)
		 SELECT t.session_id, t.id, $3::text, $4::jsonb
		 FROM zeep_system.builder_turns t
		 WHERE t.id = $1 AND t.lease_owner = $2 AND t.status = 'running'
		   AND NOT EXISTS (
		     SELECT 1 FROM zeep_system.builder_events e
		     WHERE e.session_id = t.session_id AND e.type = $3::text AND e.payload->>'cap' = $5)
		 RETURNING id`,
		l.TurnID, l.Owner, EventBudgetWarning, raw, w.Cap,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("builder store: append budget warning: %w", err)
	}
	return true, nil
}

// Sample bounds of the build estimate (ai-builder-planning design §9).
const (
	// EstimateWindow is how far back the estimate looks.
	EstimateWindow = 90 * 24 * time.Hour
	// EstimateMinSample is the fewest turns a range is computed from: fewer
	// completed turns give no estimate, fewer fully priced turns give tokens
	// only.
	EstimateMinSample = 10
)

// EstimateQuery selects the turns a build estimate is computed from.
type EstimateQuery struct {
	// Provider and Model are the provider kind and model configured now; a
	// turn counts only when every one of its build calls used them.
	Provider string
	Model    string
	// Since is the start of the window (now - EstimateWindow).
	Since time.Time
	// PricedSince excludes from the cost range the turns that started before
	// it (the installation currency changed then, so older costs are in
	// another currency). Nil when the currency never changed.
	PricedSince *time.Time
}

// BuildEstimate is the spread of what one build turn spent on this
// installation. The ranges are the 25th and 75th percentiles of the per-turn
// totals. TokensLow/High are 0 and the cost range nil when the sample is under
// EstimateMinSample; the cost range is also nil when the priced sample is.
type BuildEstimate struct {
	SampleTurns    int
	PricedTurns    int
	TokensLow      int64
	TokensHigh     int64
	CostMicrosLow  *int64
	CostMicrosHigh *int64
}

// EstimateBuildTurn computes the build estimate from the ledger. The sample is
// the completed build-mode turns of the builder created in the window, of every
// user. Per turn it adds the calls that are not planning calls (a turn whose
// plan was approved spent its first calls planning, which is not building), so
// the range describes what building one milestone costs. A turn is left out
// when any of its build calls used another provider or model, or did not report
// usage (its tokens would read as zero). A turn is priced when every build call
// has a cost and it started on or after q.PricedSince.
func (s *Store) EstimateBuildTurn(ctx context.Context, q EstimateQuery) (*BuildEstimate, error) {
	if q.Provider == "" || q.Model == "" {
		return nil, fmt.Errorf("%w: provider and model are required", ErrInvalidArgument)
	}
	var (
		out                                BuildEstimate
		tokLow, tokHigh, costLow, costHigh *float64
	)
	err := s.pool.QueryRow(ctx,
		`WITH per_turn AS (
		   SELECT sum(l.input_tokens::bigint + l.cached_input_tokens + l.cache_write_tokens + l.output_tokens) AS tokens,
		          sum(l.cost_micros) AS cost,
		          bool_and(l.cost_micros IS NOT NULL)
		            AND ($5::timestamptz IS NULL OR min(l.created_at) >= $5::timestamptz) AS priced
		   FROM zeep_system.builder_turns t
		   JOIN zeep_system.ai_usage_ledger l ON l.turn_id = t.id
		   WHERE t.created_at >= $1 AND t.mode = $6 AND t.status = $7
		     AND l.created_at >= $1 AND l.feature = $2 AND l.purpose <> $8
		   GROUP BY t.id
		   HAVING bool_and(l.provider = $3 AND l.model = $4 AND l.usage_reported)
		 )
		 SELECT count(*),
		        count(*) FILTER (WHERE priced),
		        percentile_cont(0.25) WITHIN GROUP (ORDER BY tokens),
		        percentile_cont(0.75) WITHIN GROUP (ORDER BY tokens),
		        percentile_cont(0.25) WITHIN GROUP (ORDER BY cost) FILTER (WHERE priced),
		        percentile_cont(0.75) WITHIN GROUP (ORDER BY cost) FILTER (WHERE priced)
		 FROM per_turn`,
		q.Since, FeatureBuilder, q.Provider, q.Model, q.PricedSince, ModeBuild, TurnCompleted, PurposePlanning,
	).Scan(&out.SampleTurns, &out.PricedTurns, &tokLow, &tokHigh, &costLow, &costHigh)
	if err != nil {
		return nil, fmt.Errorf("builder store: estimate build turn: %w", err)
	}
	if out.SampleTurns < EstimateMinSample || tokLow == nil || tokHigh == nil {
		return &out, nil
	}
	out.TokensLow, out.TokensHigh = int64(math.Round(*tokLow)), int64(math.Round(*tokHigh))
	if out.PricedTurns >= EstimateMinSample && costLow != nil && costHigh != nil {
		lo, hi := int64(math.Round(*costLow)), int64(math.Round(*costHigh))
		out.CostMicrosLow, out.CostMicrosHigh = &lo, &hi
	}
	return &out, nil
}

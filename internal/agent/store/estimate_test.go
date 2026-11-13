package store_test

// estimate_test.go — ai-builder-planning P07: the sample and the percentiles of
// store.EstimateBuildTurn (design §9). Turns and ledger rows are inserted with
// SQL so each test controls the mode, status, dates and costs exactly.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

const (
	estProvider = "openai_compatible"
	estModel    = "estimate-model"
)

// estSeed inserts turns and calls on one session.
type estSeed struct {
	e    *env
	sess *store.Session
	seq  int
}

func (e *env) estimateSeed(t *testing.T) *estSeed {
	t.Helper()
	return &estSeed{e: e, sess: e.session(t, e.userA, e.appA)}
}

// turn inserts a turn with the given mode and status, created at.
func (s *estSeed) turn(t *testing.T, mode, status string, at time.Time) string {
	t.Helper()
	s.seq++
	var id string
	if err := s.e.pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.builder_turns (session_id, seq, status, mode, created_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $5) RETURNING id`,
		s.sess.ID, s.seq, status, mode, at).Scan(&id); err != nil {
		t.Fatalf("insert turn: %v", err)
	}
	return id
}

type estCall struct {
	provider, model, purpose, feature string
	tokens                            int
	cost                              *int64
	unreported                        bool
}

// call inserts a ledger row on turnID at the given time. Its tokens are split
// over the four classes, which the estimate adds up.
func (s *estSeed) call(t *testing.T, turnID string, at time.Time, c estCall) {
	t.Helper()
	if c.provider == "" {
		c.provider = estProvider
	}
	if c.model == "" {
		c.model = estModel
	}
	if c.purpose == "" {
		c.purpose = store.PurposeChat
	}
	if c.feature == "" {
		c.feature = store.FeatureBuilder
	}
	q := c.tokens / 4
	if _, err := s.e.pool.Exec(context.Background(),
		`INSERT INTO zeep_system.ai_usage_ledger
		   (user_id, feature, session_id, turn_id, provider, model, input_tokens, cached_input_tokens,
		    cache_write_tokens, output_tokens, purpose, cost_micros, usage_reported, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		s.e.userA, c.feature, s.sess.ID, turnID, c.provider, c.model, c.tokens-3*q, q, q, q,
		c.purpose, c.cost, !c.unreported, at); err != nil {
		t.Fatalf("insert call: %v", err)
	}
}

func micros(v int64) *int64 { return &v }

// buildTurns inserts n completed build turns at the given time; turn i
// (1-based) spends i*100 tokens over two calls and costs i*1000 micros.
func (s *estSeed) buildTurns(t *testing.T, n int, at time.Time) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		id := s.turn(t, store.ModeBuild, store.TurnCompleted, at)
		s.call(t, id, at, estCall{tokens: i * 60, cost: micros(int64(i) * 600)})
		s.call(t, id, at.Add(time.Second), estCall{tokens: i * 40, cost: micros(int64(i) * 400)})
		ids = append(ids, id)
	}
	return ids
}

func estimate(t *testing.T, e *env, now time.Time, pricedSince *time.Time) *store.BuildEstimate {
	t.Helper()
	est, err := e.st.EstimateBuildTurn(context.Background(), store.EstimateQuery{
		Provider: estProvider, Model: estModel, Since: now.Add(-store.EstimateWindow), PricedSince: pricedSince,
	})
	if err != nil {
		t.Fatalf("EstimateBuildTurn: %v", err)
	}
	return est
}

// The percentiles of tokens 100..1000 are 325 and 775 (linear interpolation),
// and of costs 1000..10000 micros 3250 and 7750.
func checkSeededRange(t *testing.T, est *store.BuildEstimate, wantSample int) {
	t.Helper()
	if est.SampleTurns != wantSample || est.TokensLow != 325 || est.TokensHigh != 775 {
		t.Fatalf("estimate = %+v, want sample %d and tokens 325..775", est, wantSample)
	}
	if est.CostMicrosLow == nil || est.CostMicrosHigh == nil || *est.CostMicrosLow != 3250 || *est.CostMicrosHigh != 7750 {
		t.Fatalf("cost range = %v..%v, want 3250..7750", est.CostMicrosLow, est.CostMicrosHigh)
	}
}

func TestEstimate_FewerThanTenTurnsHasNoRange(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	e.estimateSeed(t).buildTurns(t, store.EstimateMinSample-1, now.Add(-time.Hour))

	est := estimate(t, e, now, nil)
	if est.SampleTurns != 9 || est.TokensLow != 0 || est.TokensHigh != 0 || est.CostMicrosLow != nil || est.CostMicrosHigh != nil {
		t.Fatalf("estimate = %+v, want a sample of 9 and no range", est)
	}
}

func TestEstimate_TenPricedTurnsGiveThePercentiles(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	e.estimateSeed(t).buildTurns(t, 10, now.Add(-time.Hour))

	est := estimate(t, e, now, nil)
	checkSeededRange(t, est, 10)
	if est.PricedTurns != 10 {
		t.Fatalf("priced = %d, want 10", est.PricedTurns)
	}
}

func TestEstimate_UnpricedCallGivesTokensOnly(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	s := e.estimateSeed(t)
	ids := s.buildTurns(t, 10, now.Add(-time.Hour))
	// A call with no price set makes its turn unpriced: 9 priced turns.
	s.call(t, ids[0], now.Add(-time.Hour), estCall{tokens: 0})

	est := estimate(t, e, now, nil)
	if est.SampleTurns != 10 || est.PricedTurns != 9 || est.TokensLow != 325 || est.TokensHigh != 775 {
		t.Fatalf("estimate = %+v, want 10 turns, 9 priced, tokens 325..775", est)
	}
	if est.CostMicrosLow != nil || est.CostMicrosHigh != nil {
		t.Fatalf("cost range = %v..%v, want none under 10 priced turns", est.CostMicrosLow, est.CostMicrosHigh)
	}
}

func TestEstimate_TurnsBeforeACurrencyChangeAreNotPriced(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	s := e.estimateSeed(t)
	changed := now.Add(-2 * time.Hour)
	// Three turns before the change, with costs in the old currency far above
	// the rest: they count for tokens only.
	for i := 0; i < 3; i++ {
		id := s.turn(t, store.ModeBuild, store.TurnCompleted, changed.Add(-time.Hour))
		s.call(t, id, changed.Add(-time.Hour), estCall{tokens: 550, cost: micros(1_000_000_000)})
	}
	s.buildTurns(t, 10, now.Add(-time.Hour))

	est := estimate(t, e, now, &changed)
	if est.SampleTurns != 13 || est.PricedTurns != 10 {
		t.Fatalf("estimate = %+v, want 13 turns, 10 priced", est)
	}
	if est.CostMicrosLow == nil || *est.CostMicrosLow != 3250 || *est.CostMicrosHigh != 7750 {
		t.Fatalf("cost range = %v..%v, want 3250..7750 from the turns after the change", est.CostMicrosLow, est.CostMicrosHigh)
	}
}

func TestEstimate_SampleExcludesWhatIsNotABuildOfThisModel(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	s := e.estimateSeed(t)
	at := now.Add(-time.Hour)
	ids := s.buildTurns(t, 10, at)
	huge := estCall{tokens: 1_000_000, cost: micros(1_000_000_000)}

	// Planning calls of a turn whose plan was approved are not building.
	planningCall := huge
	planningCall.purpose = store.PurposePlanning
	s.call(t, ids[4], at, planningCall)

	excluded := func(mode, status string, at time.Time, c estCall) {
		s.call(t, s.turn(t, mode, status, at), at, c)
	}
	excluded(store.ModePlan, store.TurnCompleted, at, huge)                         // plan-mode turn
	excluded(store.ModeBuild, store.TurnFailed, at, huge)                           // not completed
	excluded(store.ModeBuild, store.TurnCompleted, now.Add(-91*24*time.Hour), huge) // outside the window
	other := huge
	other.model = "another-model"
	excluded(store.ModeBuild, store.TurnCompleted, at, other) // another model
	other = huge
	other.provider = "anthropic"
	excluded(store.ModeBuild, store.TurnCompleted, at, other) // another provider
	other = huge
	other.feature = "other-feature"
	excluded(store.ModeBuild, store.TurnCompleted, at, other) // not the builder
	other = huge
	other.unreported = true
	excluded(store.ModeBuild, store.TurnCompleted, at, other) // usage not reported
	// A turn with one call of this model and one of another model.
	mixed := s.turn(t, store.ModeBuild, store.TurnCompleted, at)
	s.call(t, mixed, at, huge)
	other = huge
	other.model = "another-model"
	s.call(t, mixed, at, other)

	checkSeededRange(t, estimate(t, e, now, nil), 10)
}

func TestEstimate_RequiresProviderAndModel(t *testing.T) {
	e := newEnv(t)
	_, err := e.st.EstimateBuildTurn(context.Background(), store.EstimateQuery{Provider: estProvider, Since: time.Now()})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}

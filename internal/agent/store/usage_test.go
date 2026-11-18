package store_test

// usage_test.go — integration tests for the usage ledger and BudgetChecker,
// derived from ai-frontend-builder T13 "Done when" (AFB-02, AFB-14): under cap
// ok; session cap hit; monthly cap hit; previous month not counted; cap 0
// blocks.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

func (e *env) setCaps(t *testing.T, sessionCap, monthlyCap int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO zeep_system.builder_settings (id, session_token_cap, user_monthly_token_cap)
		 VALUES (1, $1, $2)
		 ON CONFLICT (id) DO UPDATE SET session_token_cap = EXCLUDED.session_token_cap,
		                                user_monthly_token_cap = EXCLUDED.user_monthly_token_cap`,
		sessionCap, monthlyCap); err != nil {
		t.Fatalf("set caps: %v", err)
	}
}

func (e *env) record(t *testing.T, userID, sessionID string, in, out int) {
	t.Helper()
	if err := e.st.RecordUsage(context.Background(), store.UsageRecord{
		UserID: userID, Feature: store.FeatureBuilder, SessionID: sessionID,
		Provider: "openai", Model: "gpt-test", InputTokens: in, OutputTokens: out,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
}

// Under both caps the call is allowed, and usage accumulates per call in the
// ledger and on the session (AFB-14: tokens in/out per call, per session).
func TestBudget_UnderCapAllowedAndUsageRecorded(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1000, 5000)
	sess := e.session(t, e.userA, e.appA)
	bc := store.NewBudgetChecker(e.pool)

	if _, err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Fatalf("fresh session must be allowed: %v", err)
	}
	e.record(t, e.userA, sess.ID, 300, 100)
	e.record(t, e.userA, sess.ID, 200, 50)
	if _, err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Fatalf("650/1000 must be allowed: %v", err)
	}

	got, err := e.st.GetSession(ctx, e.userA, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TokensIn != 500 || got.TokensOut != 150 {
		t.Errorf("session counters want 500/150, got %d/%d", got.TokensIn, got.TokensOut)
	}
	var rows, in, out int
	if err := e.pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(sum(input_tokens),0), COALESCE(sum(output_tokens),0)
		 FROM zeep_system.ai_usage_ledger WHERE session_id = $1`, sess.ID).Scan(&rows, &in, &out); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || in != 500 || out != 150 {
		t.Errorf("ledger want 2 rows 500/150, got %d rows %d/%d", rows, in, out)
	}
}

// The session cap blocks the next call once in+out reaches it (the call that
// crosses the cap is the last one), without touching other sessions.
func TestBudget_SessionCapReached(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1000, 1_000_000)
	s1 := e.session(t, e.userA, e.appA)
	s2 := e.session(t, e.userA, e.appA)
	bc := store.NewBudgetChecker(e.pool)

	e.record(t, e.userA, s1.ID, 999, 0)
	if _, err := bc.Check(ctx, e.userA, s1.ID); err != nil {
		t.Fatalf("999/1000 must still be allowed: %v", err)
	}
	e.record(t, e.userA, s1.ID, 1, 0) // reaches the cap exactly
	_, err := bc.Check(ctx, e.userA, s1.ID)
	if !errors.Is(err, store.ErrSessionCapReached) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Fatalf("want ErrSessionCapReached (wrapping ErrBudgetExceeded), got %v", err)
	}
	if _, err := bc.Check(ctx, e.userA, s2.ID); err != nil {
		t.Errorf("another session must be unaffected: %v", err)
	}
}

// The monthly cap is per user, summed across all that user's sessions.
func TestBudget_MonthlyCapReached(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1_000_000, 1000)
	s1 := e.session(t, e.userA, e.appA)
	s2 := e.session(t, e.userA, e.appA)
	sB := e.session(t, e.userB, e.appB)
	bc := store.NewBudgetChecker(e.pool)

	e.record(t, e.userA, s1.ID, 400, 100)
	if _, err := bc.Check(ctx, e.userA, s2.ID); err != nil {
		t.Fatalf("500/1000 must be allowed: %v", err)
	}
	e.record(t, e.userA, s2.ID, 300, 200) // total 1000 across two sessions
	_, err := bc.Check(ctx, e.userA, s2.ID)
	if !errors.Is(err, store.ErrMonthlyCapReached) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Fatalf("want ErrMonthlyCapReached, got %v", err)
	}
	if _, err := bc.Check(ctx, e.userB, sB.ID); err != nil {
		t.Errorf("another user must be unaffected: %v", err)
	}
}

// The accepted bound of the monthly caps under concurrency: Check reads what
// was recorded, and a call is recorded only after the provider answers, so N
// sessions of one user that check at the same time all pass and the month can
// end over the cap by up to N calls (the same rule as "the call that crosses a
// cap is the last one", once per concurrent session). Once those calls are
// recorded, every session of the user is blocked. A reservation would remove
// the overshoot but needs a reserve/settle/expire cycle for calls that crash
// mid-flight; with at most BUILDER_WORKER_CONCURRENCY turns per replica the
// bound is small, so it is accepted.
func TestBudget_ConcurrentSessionsOfOneUserOvershootByAtMostOneCallEach(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1_000_000, 1000)
	const n = 4
	bc := store.NewBudgetChecker(e.pool)
	sessions := make([]*store.Session, n)
	for i := range sessions {
		sessions[i] = e.session(t, e.userA, e.appA)
	}
	e.record(t, e.userA, sessions[0].ID, 900, 0) // 100 tokens left this month

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range sessions {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = bc.Check(ctx, e.userA, sessions[i].ID)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("session %d: concurrent checks under the cap all pass, got %v", i, err)
		}
	}
	// Each in-flight call is recorded after its answer: 4 x 300 tokens.
	for _, s := range sessions {
		e.record(t, e.userA, s.ID, 200, 100)
	}
	for i, s := range sessions {
		if _, err := bc.Check(ctx, e.userA, s.ID); !errors.Is(err, store.ErrMonthlyCapReached) {
			t.Errorf("session %d after the overshoot: want ErrMonthlyCapReached, got %v", i, err)
		}
	}
}

// Usage from a previous calendar month (UTC) does not count; the month's
// first instant does; next month's does not.
func TestBudget_PreviousMonthNotCounted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1_000_000, 1000)
	sess := e.session(t, e.userA, e.appA)
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	bc := store.NewBudgetChecker(e.pool, store.WithClock(func() time.Time { return now }))

	insertAt := func(at time.Time, tokens int) {
		t.Helper()
		if _, err := e.pool.Exec(ctx,
			`INSERT INTO zeep_system.ai_usage_ledger (user_id, feature, session_id, provider, model, input_tokens, output_tokens, created_at)
			 VALUES ($1, 'builder', $2, 'openai', 'gpt-test', $3, 0, $4)`,
			e.userA, sess.ID, tokens, at); err != nil {
			t.Fatalf("insert ledger row: %v", err)
		}
	}
	insertAt(time.Date(2026, time.February, 28, 23, 59, 59, 0, time.UTC), 5000) // previous month
	insertAt(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), 5000)        // next month
	if _, err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Fatalf("usage outside the current month must not count: %v", err)
	}

	insertAt(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), 1000) // first instant of the month
	if _, err := bc.Check(ctx, e.userA, sess.ID); !errors.Is(err, store.ErrMonthlyCapReached) {
		t.Fatalf("usage at the month's first instant must count: got %v", err)
	}
}

// Cap 0 (unset) blocks sessions until a superadmin sets it, as does a missing
// settings row; any non-zero value un-blocks.
func TestBudget_CapZeroBlocks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	bc := store.NewBudgetChecker(e.pool)

	// No settings row yet.
	if _, err := bc.Check(ctx, e.userA, sess.ID); !errors.Is(err, store.ErrBudgetNotConfigured) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Errorf("missing settings: want ErrBudgetNotConfigured, got %v", err)
	}
	for _, caps := range [][2]int64{{0, 0}, {0, 1000}, {1000, 0}} {
		e.setCaps(t, caps[0], caps[1])
		if _, err := bc.Check(ctx, e.userA, sess.ID); !errors.Is(err, store.ErrBudgetNotConfigured) {
			t.Errorf("caps %v: want ErrBudgetNotConfigured, got %v", caps, err)
		}
	}
	e.setCaps(t, 1000, 1000)
	if _, err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Errorf("caps set: want allowed, got %v", err)
	}
}

// Ledger rows of other features are not charged against the builder's cap.
func TestBudget_OnlyBuilderFeatureCounts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1_000_000, 1000)
	sess := e.session(t, e.userA, e.appA)
	if err := e.st.RecordUsage(ctx, store.UsageRecord{
		UserID: e.userA, Feature: "other_feature", Provider: "openai", Model: "m", InputTokens: 9999,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewBudgetChecker(e.pool).Check(ctx, e.userA, sess.ID); err != nil {
		t.Errorf("other features must not count toward the builder cap: %v", err)
	}
}

// RecordUsage validates input, and an unknown session rolls back the ledger
// row so ledger and session counters cannot diverge.
func TestRecordUsage_ValidationAndAtomicity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	valid := store.UsageRecord{UserID: e.userA, Feature: store.FeatureBuilder, Provider: "openai", Model: "m", InputTokens: 1, OutputTokens: 1}

	neg := valid
	neg.InputTokens = -1
	if err := e.st.RecordUsage(ctx, neg); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("negative tokens: want ErrInvalidArgument, got %v", err)
	}
	missing := valid
	missing.Model = ""
	if err := e.st.RecordUsage(ctx, missing); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("missing model: want ErrInvalidArgument, got %v", err)
	}

	e.setCaps(t, 1000, 1000)
	ghost := valid
	ghost.SessionID = "00000000-0000-0000-0000-000000000000"
	if err := e.st.RecordUsage(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown session: want ErrNotFound, got %v", err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.ai_usage_ledger`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("failed RecordUsage must leave no ledger row, found %d", n)
	}
	if _, err := store.NewBudgetChecker(e.pool).Check(ctx, e.userA, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Errorf("Check on unknown session must error")
	}
}

// --- ai-model-catalog M02: catalog providers are priced by the catalog ---

// A call of a catalog model gets the catalog price with no price row, and an
// ai_model_prices row for a catalog provider is ignored.
func TestRecordUsage_CatalogProviderUsesCatalogPrice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 10_000_000, 10_000_000)
	sess := e.session(t, e.userA, e.appA)
	// A stale operator price for a catalog model must not be used.
	e.setPrice(t, "openai", "gpt-4.1", "100", nil, nil, "100")

	rec := store.UsageRecord{
		UserID: e.userA, Feature: store.FeatureBuilder, SessionID: sess.ID,
		Provider: "openai", Model: "gpt-4.1", InputTokens: 1000, CachedInputTokens: 10_000, OutputTokens: 500, Reported: true,
	}
	if err := e.st.RecordUsage(ctx, rec); err != nil {
		t.Fatal(err)
	}
	notInCatalog := rec
	notInCatalog.Model = "o3-mini"
	if err := e.st.RecordUsage(ctx, notInCatalog); err != nil {
		t.Fatal(err)
	}

	got := e.ledger(t, sess.ID)
	if len(got) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(got))
	}
	// 1000*2 + 10000*0.5 + 500*8 = 11000 micros (catalog), not the 100/100 row.
	if got[0].cost == nil || *got[0].cost != 11_000 {
		t.Errorf("catalog cost = %v, want 11000", got[0].cost)
	}
	if got[1].cost != nil {
		t.Errorf("a model outside the catalog must read as no price, got %d", *got[1].cost)
	}
}

// --- ai-builder-costs C04: cost on every recorded call (ABC-02..04) ---

func (e *env) setPrice(t *testing.T, provider, model, input string, cached, write *string, output string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO zeep_system.ai_model_prices
		   (provider, model, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok, output_per_mtok)
		 VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6::numeric)
		 ON CONFLICT (provider, model) DO UPDATE SET
		   input_per_mtok = EXCLUDED.input_per_mtok, cached_input_per_mtok = EXCLUDED.cached_input_per_mtok,
		   cache_write_per_mtok = EXCLUDED.cache_write_per_mtok, output_per_mtok = EXCLUDED.output_per_mtok`,
		provider, model, input, cached, write, output); err != nil {
		t.Fatalf("set price: %v", err)
	}
	t.Cleanup(func() {
		e.pool.Exec(context.Background(),
			`DELETE FROM zeep_system.ai_model_prices WHERE provider = $1 AND model = $2`, provider, model)
	})
}

type ledgerRow struct {
	in, cached, write, out int
	purpose                string
	cost                   *int64
	reported               bool
}

func (e *env) ledger(t *testing.T, sessionID string) []ledgerRow {
	t.Helper()
	rows, err := e.pool.Query(context.Background(),
		`SELECT input_tokens, cached_input_tokens, cache_write_tokens, output_tokens, purpose, cost_micros, usage_reported
		 FROM zeep_system.ai_usage_ledger WHERE session_id = $1 ORDER BY id`, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []ledgerRow
	for rows.Next() {
		var r ledgerRow
		if err := rows.Scan(&r.in, &r.cached, &r.write, &r.out, &r.purpose, &r.cost, &r.reported); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A priced call stores its token classes and its cost; the cost is a
// snapshot, so editing the price afterwards leaves the row alone and only
// changes later calls. The session total is the sum of the ledger.
func TestRecordUsage_PricedCallStoresCostSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 10_000_000, 10_000_000)
	sess := e.session(t, e.userA, e.appA)
	cached, write := "0.30", "3.75"
	e.setPrice(t, "openai_compatible", "priced-model", "3", &cached, &write, "15")

	rec := store.UsageRecord{
		UserID: e.userA, Feature: store.FeatureBuilder, SessionID: sess.ID,
		Provider: "openai_compatible", Model: "priced-model", Purpose: store.PurposeCompaction,
		InputTokens: 1000, CachedInputTokens: 10_000, CacheWriteTokens: 2000, OutputTokens: 500, Reported: true,
	}
	if err := e.st.RecordUsage(ctx, rec); err != nil {
		t.Fatal(err)
	}
	// 1000*3 + 10000*0.30 + 2000*3.75 + 500*15 = 21000 micros.
	const first = int64(21_000)

	e.setPrice(t, "openai_compatible", "priced-model", "6", nil, nil, "30")
	rec.Purpose = ""
	if err := e.st.RecordUsage(ctx, rec); err != nil {
		t.Fatal(err)
	}
	// No cache prices now: all 13000 input tokens at 6, plus 500*30 = 93000.
	const second = int64(93_000)

	got := e.ledger(t, sess.ID)
	if len(got) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(got))
	}
	r := got[0]
	if r.in != 1000 || r.cached != 10_000 || r.write != 2000 || r.out != 500 || r.purpose != store.PurposeCompaction || !r.reported {
		t.Errorf("first row = %+v, want classes 1000/10000/2000/500, compaction, reported", r)
	}
	if r.cost == nil || *r.cost != first {
		t.Errorf("first cost = %v, want %d (unchanged by the later price edit)", r.cost, first)
	}
	if got[1].cost == nil || *got[1].cost != second || got[1].purpose != store.PurposeChat {
		t.Errorf("second row = %+v, want cost %d and the default purpose", got[1], second)
	}

	var total int64
	if err := e.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_micros), 0) FROM zeep_system.ai_usage_ledger WHERE session_id = $1`, sess.ID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != first+second {
		t.Errorf("session total = %d, want %d", total, first+second)
	}
	s, err := e.st.GetSession(ctx, e.userA, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.TokensIn != 26_000 || s.TokensOut != 1000 {
		t.Errorf("session counters = %d/%d, want 26000/1000 (every input class counts)", s.TokensIn, s.TokensOut)
	}
}

// No price row is "no price set" (NULL), not a cost of zero; a price of zero
// is a real price and gives a cost of 0.
func TestRecordUsage_UnpricedIsNullAndZeroPriceIsZero(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 1_000_000, 1_000_000)
	sess := e.session(t, e.userA, e.appA)
	e.setPrice(t, "openai_compatible", "local-free", "0", nil, nil, "0")

	base := store.UsageRecord{UserID: e.userA, Feature: store.FeatureBuilder, SessionID: sess.ID, InputTokens: 800, OutputTokens: 200}
	unpriced := base
	unpriced.Provider, unpriced.Model = "openai", "no-price-model"
	free := base
	free.Provider, free.Model, free.Reported = "openai_compatible", "local-free", true
	// The price key is (provider, model): the same model name under another
	// provider kind has no price.
	otherKind := base
	otherKind.Provider, otherKind.Model = "openai", "local-free"
	for _, r := range []store.UsageRecord{unpriced, free, otherKind} {
		if err := e.st.RecordUsage(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got := e.ledger(t, sess.ID)
	if len(got) != 3 {
		t.Fatalf("ledger rows = %d, want 3", len(got))
	}
	if got[0].cost != nil || got[0].reported {
		t.Errorf("unpriced row = %+v, want NULL cost and usage_reported false", got[0])
	}
	if got[1].cost == nil || *got[1].cost != 0 {
		t.Errorf("zero-price row cost = %v, want 0", got[1].cost)
	}
	if got[2].cost != nil {
		t.Errorf("same model under another provider kind: cost = %v, want NULL", *got[2].cost)
	}
}

// Cached and cache-write tokens count toward both token caps, as they did
// when the loop summed them into the input.
func TestBudget_EveryInputClassCountsTowardTheCaps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bc := store.NewBudgetChecker(e.pool)
	rec := func(sessionID string) {
		t.Helper()
		if err := e.st.RecordUsage(ctx, store.UsageRecord{
			UserID: e.userA, Feature: store.FeatureBuilder, SessionID: sessionID, Provider: "openai", Model: "gpt-test",
			InputTokens: 100, CachedInputTokens: 700, CacheWriteTokens: 150, OutputTokens: 50, Reported: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	e.setCaps(t, 1000, 1_000_000)
	a := e.session(t, e.userA, e.appA)
	rec(a.ID)
	if _, err := bc.Check(ctx, e.userA, a.ID); !errors.Is(err, store.ErrSessionCapReached) {
		t.Errorf("1000/1000 with cached tokens: want ErrSessionCapReached, got %v", err)
	}

	e.setCaps(t, 1_000_000, 1000)
	b := e.session(t, e.userA, e.appA)
	if _, err := bc.Check(ctx, e.userA, b.ID); !errors.Is(err, store.ErrMonthlyCapReached) {
		t.Errorf("monthly 1000/1000 with cached tokens: want ErrMonthlyCapReached, got %v", err)
	}
}

func TestRecordUsage_RejectsUnknownPurposeAndNegativeClasses(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	valid := store.UsageRecord{UserID: e.userA, Feature: store.FeatureBuilder, Provider: "openai", Model: "m", InputTokens: 1, OutputTokens: 1}
	bad := valid
	bad.Purpose = "billing"
	if err := e.st.RecordUsage(ctx, bad); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("unknown purpose: want ErrInvalidArgument, got %v", err)
	}
	for name, mutate := range map[string]func(*store.UsageRecord){
		"cached":      func(r *store.UsageRecord) { r.CachedInputTokens = -1 },
		"cache write": func(r *store.UsageRecord) { r.CacheWriteTokens = -1 },
	} {
		r := valid
		mutate(&r)
		if err := e.st.RecordUsage(ctx, r); !errors.Is(err, store.ErrInvalidArgument) {
			t.Errorf("negative %s tokens: want ErrInvalidArgument, got %v", name, err)
		}
	}
}

// --- ai-builder-costs C05: money caps and the 80% warning (ABC-19, ABC-20) ---

func (e *env) setCostCaps(t *testing.T, sessionMicros, monthlyMicros int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_settings
		 SET session_cost_cap_micros = $1, user_monthly_cost_cap_micros = $2 WHERE id = 1`,
		sessionMicros, monthlyMicros); err != nil {
		t.Fatalf("set cost caps: %v", err)
	}
	t.Cleanup(func() {
		e.pool.Exec(context.Background(),
			`UPDATE zeep_system.builder_settings SET session_cost_cap_micros = 0, user_monthly_cost_cap_micros = 0 WHERE id = 1`)
	})
}

// spend records one call of an openai_compatible model; with "cost-model" (1
// unit per million tokens in and out) micros spent == tokens.
func (e *env) spend(t *testing.T, sessionID, model string, tokens int) {
	t.Helper()
	if err := e.st.RecordUsage(context.Background(), store.UsageRecord{
		UserID: e.userA, Feature: store.FeatureBuilder, SessionID: sessionID,
		Provider: "openai_compatible", Model: model, InputTokens: tokens, Reported: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func warningCaps(ws []store.BudgetWarning) []string {
	out := []string{}
	for _, w := range ws {
		out = append(out, w.Cap)
	}
	return out
}

// A money cap of 0 is no cap; a cap reached refuses the next call with its
// own error; calls with no price never count toward a money cap.
func TestBudget_CostCaps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setCaps(t, 100_000_000, 100_000_000)
	e.setPrice(t, "openai_compatible", "cost-model", "1", nil, nil, "1")
	bc := store.NewBudgetChecker(e.pool)
	sess := e.session(t, e.userA, e.appA)

	e.spend(t, sess.ID, "cost-model", 5000)
	if ws, err := bc.Check(ctx, e.userA, sess.ID); err != nil || len(ws) != 0 {
		t.Fatalf("no money cap set: want allowed without warnings, got %v / %v", ws, err)
	}

	e.setCostCaps(t, 5000, 0)
	_, err := bc.Check(ctx, e.userA, sess.ID)
	if !errors.Is(err, store.ErrSessionCostCapReached) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Errorf("5000/5000 micros: want ErrSessionCostCapReached wrapping ErrBudgetExceeded, got %v", err)
	}
	if got := store.BudgetReason(err); got != store.BudgetCapSessionCost {
		t.Errorf("reason = %q, want %q", got, store.BudgetCapSessionCost)
	}
	other := e.session(t, e.userA, e.appA)
	if _, err := bc.Check(ctx, e.userA, other.ID); err != nil {
		t.Errorf("another session of the same user must not be blocked by a session cost cap: %v", err)
	}

	e.setCostCaps(t, 0, 5000)
	_, err = bc.Check(ctx, e.userA, other.ID)
	if !errors.Is(err, store.ErrMonthlyCostCapReached) || store.BudgetReason(err) != store.BudgetCapMonthCost {
		t.Errorf("month 5000/5000 micros: want ErrMonthlyCostCapReached, got %v", err)
	}
	sB := e.session(t, e.userB, e.appA)
	if _, err := bc.Check(ctx, e.userB, sB.ID); err != nil {
		t.Errorf("the monthly cost cap is per user: %v", err)
	}

	// Unpriced calls have no cost: a huge unpriced call does not move a money cap.
	e.setCostCaps(t, 6000, 6000)
	e.spend(t, sess.ID, "model-without-price", 1_000_000)
	if _, err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Errorf("unpriced usage must not count toward money caps: %v", err)
	}
}

func TestBudgetReason(t *testing.T) {
	for want, err := range map[string]error{
		store.BudgetReasonNotConfigured: store.ErrBudgetNotConfigured,
		store.BudgetCapSessionTokens:    store.ErrSessionCapReached,
		store.BudgetCapMonthTokens:      store.ErrMonthlyCapReached,
		store.BudgetCapSessionCost:      store.ErrSessionCostCapReached,
		store.BudgetCapMonthCost:        store.ErrMonthlyCostCapReached,
		"":                              store.ErrBudgetExceeded,
	} {
		if got := store.BudgetReason(err); got != want {
			t.Errorf("BudgetReason(%v) = %q, want %q", err, got, want)
		}
	}
}

// Each cap warns from 80% up to (not including) 100%, with the numbers.
func TestBudget_WarningsAtEightyPercent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setPrice(t, "openai_compatible", "cost-model", "1", nil, nil, "1")
	bc := store.NewBudgetChecker(e.pool)

	e.setCaps(t, 1000, 1_000_000)
	sess := e.session(t, e.userA, e.appA)
	e.spend(t, sess.ID, "cost-model", 799)
	if ws, err := bc.Check(ctx, e.userA, sess.ID); err != nil || len(ws) != 0 {
		t.Fatalf("799/1000: want no warning, got %v / %v", ws, err)
	}
	e.spend(t, sess.ID, "cost-model", 1)
	ws, err := bc.Check(ctx, e.userA, sess.ID)
	if err != nil || len(ws) != 1 || ws[0] != (store.BudgetWarning{Cap: store.BudgetCapSessionTokens, Used: 800, Limit: 1000}) {
		t.Fatalf("800/1000: want one session_tokens warning with 800/1000, got %v / %v", ws, err)
	}

	// All four at once: 800 tokens and 800 micros against caps of 1000.
	e.setCaps(t, 1000, 1000)
	e.setCostCaps(t, 1000, 1000)
	ws, err = bc.Check(ctx, e.userA, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{store.BudgetCapSessionTokens, store.BudgetCapMonthTokens, store.BudgetCapSessionCost, store.BudgetCapMonthCost}
	if got := warningCaps(ws); !slices.Equal(got, want) {
		t.Errorf("warning caps = %v, want %v", got, want)
	}
}

// One budget_warning event per session and cap, across turns; another cap
// and another session still get theirs. A writer without the lease writes
// nothing.
func TestAppendBudgetWarning_OncePerSessionAndCap(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.session(t, e.userA, e.appA)
	w := store.BudgetWarning{Cap: store.BudgetCapSessionCost, Used: 800, Limit: 1000}

	count := func(sessionID string) (n int) {
		t.Helper()
		if err := e.pool.QueryRow(ctx,
			`SELECT count(*) FROM zeep_system.builder_events WHERE session_id = $1 AND type = $2`,
			sessionID, store.EventBudgetWarning).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	_, l1 := e.claimed(t, sess, "w")
	if ok, err := e.st.AppendBudgetWarning(ctx, l1, w); err != nil || !ok {
		t.Fatalf("first warning: ok=%v err=%v, want written", ok, err)
	}
	if ok, err := e.st.AppendBudgetWarning(ctx, l1, w); err != nil || ok {
		t.Errorf("same cap, same turn: ok=%v err=%v, want not written", ok, err)
	}
	if err := e.st.FinishTurn(ctx, l1, store.FinishParams{Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}

	_, l2 := e.claimed(t, sess, "w")
	if ok, err := e.st.AppendBudgetWarning(ctx, l2, store.BudgetWarning{Cap: w.Cap, Used: 900, Limit: 1000}); err != nil || ok {
		t.Errorf("same cap, later turn: ok=%v err=%v, want not written", ok, err)
	}
	if ok, err := e.st.AppendBudgetWarning(ctx, l2, store.BudgetWarning{Cap: store.BudgetCapMonthTokens, Used: 8, Limit: 10}); err != nil || !ok {
		t.Errorf("another cap: ok=%v err=%v, want written", ok, err)
	}
	stranger := store.Lease{TurnID: l2.TurnID, Owner: "someone-else"}
	if ok, err := e.st.AppendBudgetWarning(ctx, stranger, store.BudgetWarning{Cap: store.BudgetCapMonthCost, Used: 8, Limit: 10}); err != nil || ok {
		t.Errorf("without the lease: ok=%v err=%v, want not written", ok, err)
	}
	if n := count(sess.ID); n != 2 {
		t.Errorf("budget_warning events = %d, want 2", n)
	}

	var payload string
	if err := e.pool.QueryRow(ctx,
		`SELECT payload::text FROM zeep_system.builder_events WHERE session_id = $1 AND type = $2 ORDER BY id LIMIT 1`,
		sess.ID, store.EventBudgetWarning).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"cap": "session_cost", "used": 800, "limit": 1000}` {
		t.Errorf("payload = %s, want only cap, used and limit", payload)
	}

	other := e.session(t, e.userA, e.appA)
	_, l3 := e.claimed(t, other, "w")
	if ok, err := e.st.AppendBudgetWarning(ctx, l3, w); err != nil || !ok {
		t.Errorf("another session: ok=%v err=%v, want written", ok, err)
	}
}

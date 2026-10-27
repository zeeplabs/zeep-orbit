package store_test

// usage_test.go — integration tests for the usage ledger and BudgetChecker,
// derived from ai-frontend-builder T13 "Done when" (AFB-02, AFB-14): under cap
// ok; session cap hit; monthly cap hit; previous month not counted; cap 0
// blocks.

import (
	"context"
	"errors"
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

	if err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Fatalf("fresh session must be allowed: %v", err)
	}
	e.record(t, e.userA, sess.ID, 300, 100)
	e.record(t, e.userA, sess.ID, 200, 50)
	if err := bc.Check(ctx, e.userA, sess.ID); err != nil {
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
	if err := bc.Check(ctx, e.userA, s1.ID); err != nil {
		t.Fatalf("999/1000 must still be allowed: %v", err)
	}
	e.record(t, e.userA, s1.ID, 1, 0) // reaches the cap exactly
	err := bc.Check(ctx, e.userA, s1.ID)
	if !errors.Is(err, store.ErrSessionCapReached) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Fatalf("want ErrSessionCapReached (wrapping ErrBudgetExceeded), got %v", err)
	}
	if err := bc.Check(ctx, e.userA, s2.ID); err != nil {
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
	if err := bc.Check(ctx, e.userA, s2.ID); err != nil {
		t.Fatalf("500/1000 must be allowed: %v", err)
	}
	e.record(t, e.userA, s2.ID, 300, 200) // total 1000 across two sessions
	err := bc.Check(ctx, e.userA, s2.ID)
	if !errors.Is(err, store.ErrMonthlyCapReached) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Fatalf("want ErrMonthlyCapReached, got %v", err)
	}
	if err := bc.Check(ctx, e.userB, sB.ID); err != nil {
		t.Errorf("another user must be unaffected: %v", err)
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
	if err := bc.Check(ctx, e.userA, sess.ID); err != nil {
		t.Fatalf("usage outside the current month must not count: %v", err)
	}

	insertAt(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), 1000) // first instant of the month
	if err := bc.Check(ctx, e.userA, sess.ID); !errors.Is(err, store.ErrMonthlyCapReached) {
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
	if err := bc.Check(ctx, e.userA, sess.ID); !errors.Is(err, store.ErrBudgetNotConfigured) || !errors.Is(err, store.ErrBudgetExceeded) {
		t.Errorf("missing settings: want ErrBudgetNotConfigured, got %v", err)
	}
	for _, caps := range [][2]int64{{0, 0}, {0, 1000}, {1000, 0}} {
		e.setCaps(t, caps[0], caps[1])
		if err := bc.Check(ctx, e.userA, sess.ID); !errors.Is(err, store.ErrBudgetNotConfigured) {
			t.Errorf("caps %v: want ErrBudgetNotConfigured, got %v", caps, err)
		}
	}
	e.setCaps(t, 1000, 1000)
	if err := bc.Check(ctx, e.userA, sess.ID); err != nil {
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
	if err := store.NewBudgetChecker(e.pool).Check(ctx, e.userA, sess.ID); err != nil {
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
	if err := store.NewBudgetChecker(e.pool).Check(ctx, e.userA, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Errorf("Check on unknown session must error")
	}
}

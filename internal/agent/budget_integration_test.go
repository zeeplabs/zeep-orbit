package agent_test

// budget_integration_test.go: the budget caps against a real Postgres
// (TEST_DATABASE_URL) with the real store.BudgetChecker.

import (
	"context"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// silentUsageProvider answers like an openai_compatible server that omits the
// usage object: the adapter returns Usage{} with Reported=false.
type silentUsageProvider struct{ calls int }

func (p *silentUsageProvider) Name() string  { return "openai_compatible" }
func (p *silentUsageProvider) Model() string { return "local-model" }
func (p *silentUsageProvider) Chat(context.Context, provider.ChatRequest) (provider.ChatResponse, error) {
	p.calls++
	return provider.ChatResponse{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "done"},
		FinishReason: provider.FinishStop,
	}, nil
}

func (e *env) setTokenCaps(t *testing.T, sessionCap, monthlyCap int64) {
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

// runNextTurn claims the oldest claimable turn and runs it to its end.
func (e *env) runNextTurn(t *testing.T, d agent.Deps) *store.Turn {
	t.Helper()
	ctx := context.Background()
	claimed, err := e.st.ClaimTurn(ctx, "test-owner", 30*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := agent.RunTurn(ctx, d, store.Lease{TurnID: claimed.ID, Owner: "test-owner"}); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	return e.turnStatus(t, claimed.ID)
}

// B1: a provider that never reports usage must still hit the session cap.
// Before the estimate, every call was recorded as 0 tokens and the session
// could run turns forever.
func TestBudget_ProviderWithoutUsageStillReachesTheSessionCap(t *testing.T) {
	e := newEnv(t)
	e.setTokenCaps(t, 5000, 1_000_000_000)
	p := &silentUsageProvider{}
	d := e.deps(p, &fakeBuilder{})
	d.Budget = store.NewBudgetChecker(e.pool)

	first := e.newTurn(t)
	var last *store.Turn
	for i := 0; i < 10; i++ {
		var turn *store.Turn
		if i == 0 {
			turn = first
		} else {
			var err error
			turn, err = e.st.CreateTurn(context.Background(), e.user, first.SessionID, store.KindChat, "more")
			if err != nil {
				t.Fatalf("create turn %d: %v", i+1, err)
			}
		}
		last = e.runNextTurn(t, d)
		if last.ID != turn.ID {
			t.Fatalf("ran turn %s, want %s", last.ID, turn.ID)
		}
		if last.Status == store.TurnBudgetExceeded {
			break
		}
	}
	if last.Status != store.TurnBudgetExceeded {
		t.Fatalf("after %d calls with no reported usage the last turn is %q, want budget_exceeded", p.calls, last.Status)
	}

	var reported bool
	var tokens int64
	if err := e.pool.QueryRow(context.Background(),
		`SELECT bool_or(usage_reported), COALESCE(SUM(input_tokens + output_tokens), 0)
		 FROM zeep_system.ai_usage_ledger WHERE session_id = $1`, first.SessionID,
	).Scan(&reported, &tokens); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if reported || tokens < 5000 {
		t.Fatalf("ledger: reported=%v tokens=%d, want unreported estimates reaching the cap", reported, tokens)
	}
}

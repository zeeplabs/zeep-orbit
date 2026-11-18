package agent_test

// worker_retry_test.go: a turn that keeps failing must not be retried forever
// (real Postgres, TEST_DATABASE_URL).

import (
	"context"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

type panickingProvider struct{ calls int64 }

func (p *panickingProvider) Name() string  { return "fake" }
func (p *panickingProvider) Model() string { return "fake-1" }
func (p *panickingProvider) Chat(context.Context, provider.ChatRequest) (provider.ChatResponse, error) {
	p.calls++
	panic("provider adapter bug")
}

func (e *env) expireLease(t *testing.T, turnID string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_turns SET lease_until = now() - interval '1 second' WHERE id = $1`, turnID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

// A panic in the loop fails the turn on the replica that hit it, and the
// session accepts the next message.
func TestWorker_PanicFailsTheTurnAndFreesTheSession(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	stop := start(t, e.worker(t, e.deps(&panickingProvider{}, &fakeBuilder{}), agent.WorkerConfig{}))
	defer stop()

	waitFor(t, 10*time.Second, "turn failed", func() bool {
		return e.turnStatus(t, turn.ID).Status == store.TurnFailed
	})
	if _, err := e.st.CreateTurn(context.Background(), e.user, turn.SessionID, store.KindChat, "again"); err != nil {
		t.Fatalf("the session must accept a new message after the failed turn: %v", err)
	}
}

// A replica that dies mid-turn without finishing it (OOM kill, node loss)
// leaves the turn running until the lease expires. Each reclaim is an
// attempt; past the cap the turn fails without another (paid) model call.
func TestWorker_TurnReclaimedPastTheAttemptCapFails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	turn := e.newTurn(t)
	for i := 0; i < agent.DefaultMaxTurnAttempts; i++ {
		if _, err := e.st.ClaimTurn(ctx, "doomed-replica", 30*time.Second); err != nil {
			t.Fatalf("claim %d: %v", i+1, err)
		}
		e.expireLease(t, turn.ID) // the replica died
	}

	p := &gateProvider{}
	stop := start(t, e.worker(t, e.deps(p, &fakeBuilder{}), agent.WorkerConfig{}))
	defer stop()

	waitFor(t, 10*time.Second, "turn failed", func() bool {
		return e.turnStatus(t, turn.ID).Status == store.TurnFailed
	})
	got := e.turnStatus(t, turn.ID)
	if got.ErrorCode == nil || *got.ErrorCode != agent.CodeInternal {
		t.Errorf("error code = %v, want %s", got.ErrorCode, agent.CodeInternal)
	}
	if n := p.calls.Load(); n != 0 {
		t.Errorf("provider calls = %d, want none past the attempt cap", n)
	}
}

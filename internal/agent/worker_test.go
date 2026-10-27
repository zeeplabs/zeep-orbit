package agent_test

// worker_test.go: integration tests for the turn worker (ai-frontend-builder
// T20) against a real Postgres (TEST_DATABASE_URL, no DB mocking). External
// test package: it needs dashboard.ProvisionZeepSystem, and keeping it out of
// package agent avoids an import cycle once dashboard imports agent.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ---- environment ----

type env struct {
	pool *db.Pool
	st   *store.Store
	user string
	app  string
}

func resetBuilderTables(pool *db.Pool) {
	_, _ = pool.Exec(context.Background(),
		`TRUNCATE zeep_system.builder_sessions, zeep_system.builder_settings, zeep_system.ai_usage_ledger CASCADE`)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := dashboard.ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}
	resetBuilderTables(pool) // the claim queue is global

	suffix := time.Now().UnixNano()
	u, err := dashboard.CreateUser(ctx, pool, fmt.Sprintf("builder-worker-%d@example.com", suffix), "w", "hash", "member")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	var appID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("builder-worker-%d", suffix), u.ID,
	).Scan(&appID); err != nil {
		t.Fatalf("create app: %v", err)
	}
	e := &env{pool: pool, st: store.New(pool), user: u.ID, app: appID}
	t.Cleanup(func() {
		resetBuilderTables(pool)
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM zeep_system.apps WHERE id = $1`, appID)
		_, _ = pool.Exec(bg, `DELETE FROM zeep_system.dashboard_users WHERE id = $1`, u.ID)
	})
	return e
}

// newTurn creates a session with one pending chat turn.
func (e *env) newTurn(t *testing.T) *store.Turn {
	t.Helper()
	ctx := context.Background()
	s, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.user, BackendAppID: e.app, TemplateKind: store.TemplateDefault, TemplateVersion: "v1",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turn, err := e.st.CreateTurn(ctx, e.user, s.ID, store.KindChat, "build a tasks page")
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	return turn
}

func (e *env) turnStatus(t *testing.T, id string) *store.Turn {
	t.Helper()
	turn, err := e.st.GetTurn(context.Background(), id)
	if err != nil {
		t.Fatalf("get turn: %v", err)
	}
	return turn
}

func (e *env) countEvents(t *testing.T, turnID, typ string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.builder_events WHERE turn_id = $1 AND type = $2`, turnID, typ,
	).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// ---- fakes ----

// gateProvider answers every call with a short final reply. When gate is
// non-nil a call blocks until the gate is closed (or its context ends), which
// is how the tests keep a turn "running".
type gateProvider struct {
	calls   atomic.Int64
	running atomic.Int64
	maxRun  atomic.Int64
	delay   time.Duration
	gate    chan struct{}
	started chan struct{} // receives once per call when non-nil
}

func (p *gateProvider) Name() string  { return "fake" }
func (p *gateProvider) Model() string { return "fake-1" }

func (p *gateProvider) Chat(ctx context.Context, _ provider.ChatRequest) (provider.ChatResponse, error) {
	p.calls.Add(1)
	n := p.running.Add(1)
	defer p.running.Add(-1)
	for {
		m := p.maxRun.Load()
		if n <= m || p.maxRun.CompareAndSwap(m, n) {
			break
		}
	}
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	if p.gate != nil {
		select {
		case <-p.gate:
		case <-ctx.Done():
			return provider.ChatResponse{}, &provider.ErrProvider{Err: ctx.Err()}
		}
	}
	return provider.ChatResponse{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "done"},
		Usage:        provider.Usage{InputTokens: 1, OutputTokens: 1},
		FinishReason: provider.FinishStop,
	}, nil
}

// fakeBuilder implements only what the worker and an empty turn use.
type fakeBuilder struct {
	sandbox.Sandbox
	capacity atomic.Int64 // 0 means free; otherwise Active >= Capacity
	failing  atomic.Bool
}

func (b *fakeBuilder) Health(context.Context) (sandbox.Health, error) {
	if b.failing.Load() {
		return sandbox.Health{}, fmt.Errorf("builder unreachable")
	}
	if b.capacity.Load() > 0 {
		return sandbox.Health{Active: 1, Capacity: 1}, nil
	}
	return sandbox.Health{Active: 0, Capacity: 1}, nil
}

func (b *fakeBuilder) Commit(context.Context, string, int, string) (sandbox.CommitResult, error) {
	return sandbox.CommitResult{SHA: "sha0", Prev: "sha0"}, nil
}

type allowBudget struct{}

func (allowBudget) Check(context.Context, string, string) error { return nil }

func (e *env) deps(p provider.Provider, b sandbox.Sandbox) agent.Deps {
	return agent.Deps{
		Store: e.st, Provider: p, Tools: tools.NewRegistry(), Builder: b, Budget: allowBudget{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (e *env) worker(t *testing.T, d agent.Deps, cfg agent.WorkerConfig) *agent.Worker {
	t.Helper()
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 40 * time.Millisecond
	}
	w, err := agent.NewWorker(d, e.st, cfg)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	return w
}

// start runs w until the returned stop function is called; stop waits for Run.
func start(t *testing.T, w *agent.Worker) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("worker did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// ---- tests ----

func TestWorker_RunsPendingTurn(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	w := e.worker(t, e.deps(&gateProvider{}, &fakeBuilder{}), agent.WorkerConfig{Owner: "w-a"})
	w.Poke()
	start(t, w)

	waitFor(t, 10*time.Second, "turn completed", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnCompleted })
	got := e.turnStatus(t, turn.ID)
	if got.LeaseOwner != nil || got.FinishedAt == nil {
		t.Errorf("finished turn still holds a lease or has no finish time: %+v", got)
	}
	if e.countEvents(t, turn.ID, agent.EventTurnStarted) != 1 || e.countEvents(t, turn.ID, agent.EventTurnFinished) != 1 {
		t.Error("expected exactly one turn_started and one turn_finished event")
	}
}

func TestWorker_BuilderFullQueuesOnceThenRunsWhenCapacityFrees(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	b := &fakeBuilder{}
	b.capacity.Store(1) // builder full
	prov := &gateProvider{}
	w := e.worker(t, e.deps(prov, b), agent.WorkerConfig{Owner: "w-a"})
	start(t, w)

	waitFor(t, 5*time.Second, "turn queued", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnQueued })
	time.Sleep(300 * time.Millisecond) // many polls while full
	if n := e.countEvents(t, turn.ID, "queued"); n != 1 {
		t.Errorf("queued events = %d, want exactly 1 however many times the worker polls", n)
	}
	if prov.calls.Load() != 0 {
		t.Fatal("the turn ran while the builder was full")
	}

	b.capacity.Store(0) // capacity frees up
	waitFor(t, 10*time.Second, "queued turn completed", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnCompleted })
	if n := e.countEvents(t, turn.ID, "queued"); n != 1 {
		t.Errorf("queued events after running = %d, want 1", n)
	}
}

func TestWorker_UnreachableBuilderLeavesTurnQueued(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	b := &fakeBuilder{}
	b.failing.Store(true)
	w := e.worker(t, e.deps(&gateProvider{}, b), agent.WorkerConfig{Owner: "w-a"})
	start(t, w)

	waitFor(t, 5*time.Second, "turn queued", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnQueued })
	b.failing.Store(false)
	waitFor(t, 10*time.Second, "turn completed once the builder is back", func() bool {
		return e.turnStatus(t, turn.ID).Status == store.TurnCompleted
	})
}

func TestWorker_TwoWorkersNeverRunTheSameTurn(t *testing.T) {
	e := newEnv(t)
	const n = 12
	ids := make([]string, n)
	for i := range ids {
		ids[i] = e.newTurn(t).ID
	}
	prov := &gateProvider{delay: 30 * time.Millisecond} // widen the race window
	b := &fakeBuilder{}
	wa := e.worker(t, e.deps(prov, b), agent.WorkerConfig{Owner: "w-a", Concurrency: 3})
	wb := e.worker(t, e.deps(prov, b), agent.WorkerConfig{Owner: "w-b", Concurrency: 3})
	start(t, wa)
	start(t, wb)

	waitFor(t, 20*time.Second, "all turns completed", func() bool {
		for _, id := range ids {
			if e.turnStatus(t, id).Status != store.TurnCompleted {
				return false
			}
		}
		return true
	})
	if got := prov.calls.Load(); got != n {
		t.Errorf("model calls = %d, want %d: a turn ran more than once", got, n)
	}
	for _, id := range ids {
		if c := e.countEvents(t, id, agent.EventTurnStarted); c != 1 {
			t.Errorf("turn %s started %d times, want 1", id, c)
		}
	}
}

func TestWorker_ResumesTurnOfADeadWorkerAfterLeaseExpiry(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	// A worker claimed the turn and then died: it holds a 1s lease nobody renews.
	claimed, err := e.st.ClaimTurn(context.Background(), "dead-worker", time.Second)
	if err != nil || claimed.ID != turn.ID {
		t.Fatalf("claim as the dead worker: %v, %+v", err, claimed)
	}
	claimedAt := time.Now()

	prov := &gateProvider{}
	w := e.worker(t, e.deps(prov, &fakeBuilder{}), agent.WorkerConfig{Owner: "w-live"})
	start(t, w)

	time.Sleep(500 * time.Millisecond)
	if prov.calls.Load() != 0 || e.turnStatus(t, turn.ID).Status != store.TurnRunning {
		t.Fatal("the live worker took the turn before its lease expired")
	}
	waitFor(t, 10*time.Second, "turn resumed and completed", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnCompleted })
	if since := time.Since(claimedAt); since < time.Second {
		t.Errorf("turn completed %s after the claim, before the 1s lease could expire", since)
	}
	if prov.calls.Load() != 1 {
		t.Errorf("model calls = %d, want 1", prov.calls.Load())
	}
}

func TestWorker_RespectsConcurrencyPerReplica(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.newTurn(t)
	}
	prov := &gateProvider{gate: make(chan struct{})}
	w := e.worker(t, e.deps(prov, &fakeBuilder{}), agent.WorkerConfig{Owner: "w-a", Concurrency: 2})
	start(t, w)

	waitFor(t, 5*time.Second, "two turns running", func() bool { return prov.running.Load() == 2 })
	time.Sleep(300 * time.Millisecond)
	if got := prov.maxRun.Load(); got != 2 {
		t.Fatalf("max concurrent turns = %d, want BUILDER_WORKER_CONCURRENCY = 2", got)
	}
	close(prov.gate)
	waitFor(t, 15*time.Second, "all five turns completed", func() bool { return prov.calls.Load() == 5 && prov.running.Load() == 0 })
	if got := prov.maxRun.Load(); got != 2 {
		t.Errorf("max concurrent turns = %d after draining, want 2", got)
	}
}

func TestWorker_RenewsLeaseWhileATurnRuns(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	prov := &gateProvider{gate: make(chan struct{}), started: make(chan struct{}, 4)}
	cfg := agent.WorkerConfig{LeaseTTL: time.Second, RenewEvery: 250 * time.Millisecond}
	cfg.Owner = "w-a"
	wa := e.worker(t, e.deps(prov, &fakeBuilder{}), cfg)
	start(t, wa)
	<-prov.started

	// A second replica polls fast the whole time. Without renewal the 1s lease
	// would expire and it would take the turn over.
	cfg.Owner = "w-b"
	wb := e.worker(t, e.deps(prov, &fakeBuilder{}), cfg)
	start(t, wb)
	time.Sleep(2500 * time.Millisecond)
	if got := prov.calls.Load(); got != 1 {
		t.Fatalf("model calls = %d after 2.5s of a 1s lease, want 1: the lease was not renewed", got)
	}
	if owner := e.turnStatus(t, turn.ID).LeaseOwner; owner == nil || *owner != "w-a" {
		t.Errorf("lease owner = %v, want w-a", owner)
	}
	close(prov.gate)
	waitFor(t, 10*time.Second, "turn completed", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnCompleted })
}

func TestWorker_GracefulStopWaitsForInFlightTurn(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	prov := &gateProvider{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	w := e.worker(t, e.deps(prov, &fakeBuilder{}), agent.WorkerConfig{Owner: "w-a", ShutdownGrace: 10 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	<-prov.started

	cancel() // shutdown requested while the turn is mid-flight
	select {
	case <-done:
		t.Fatal("Run returned while a turn was still running; it must wait for in-flight turns")
	case <-time.After(400 * time.Millisecond):
	}
	close(prov.gate) // the turn can finish now
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the in-flight turn finished")
	}
	if got := e.turnStatus(t, turn.ID).Status; got != store.TurnCompleted {
		t.Errorf("turn status = %q, want completed (not abandoned by the shutdown)", got)
	}
}

func TestWorker_ShutdownPastGraceReleasesTheLeaseForAnotherReplica(t *testing.T) {
	e := newEnv(t)
	turn := e.newTurn(t)
	prov := &gateProvider{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	w := e.worker(t, e.deps(prov, &fakeBuilder{}), agent.WorkerConfig{Owner: "w-a", ShutdownGrace: 200 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	<-prov.started
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the grace period")
	}

	got := e.turnStatus(t, turn.ID)
	if got.Status != store.TurnRunning || got.LeaseUntil == nil || got.LeaseUntil.After(time.Now().Add(time.Second)) {
		t.Fatalf("after a forced stop the turn must stay running with an (almost) expired lease, got %+v", got)
	}

	// Another replica picks it up right away instead of waiting out the TTL.
	close(prov.gate)
	w2 := e.worker(t, e.deps(prov, &fakeBuilder{}), agent.WorkerConfig{Owner: "w-b"})
	start(t, w2)
	waitFor(t, 10*time.Second, "turn resumed by the other replica", func() bool { return e.turnStatus(t, turn.ID).Status == store.TurnCompleted })
}

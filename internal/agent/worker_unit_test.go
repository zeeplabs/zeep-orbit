package agent

// worker_unit_test.go: the worker's own logic (capacity gate, queued-once,
// lease renewal, graceful stop) against an in-memory queue. Claim exclusivity
// and lease expiry are Postgres semantics and are covered by the integration
// tests in worker_test.go.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// fakeQueue is a one-turn WorkerStore on top of fakeStore.
type fakeQueue struct {
	*fakeStore
	qmu         sync.Mutex
	queuedMarks int
	claims      int
	renewals    []time.Duration
	renewErr    error
}

func newFakeQueue() *fakeQueue {
	fs := newFakeStore()
	fs.turn.Status, fs.turn.LeaseOwner = store.TurnPending, nil
	fs.seed("turn-1", store.RoleUser, "build me a tasks page")
	return &fakeQueue{fakeStore: fs}
}

func (q *fakeQueue) PeekClaimable(context.Context) (*store.Turn, error) {
	q.fakeStore.mu.Lock()
	defer q.fakeStore.mu.Unlock()
	if s := q.turn.Status; s == store.TurnPending || s == store.TurnQueued {
		t := *q.turn
		return &t, nil
	}
	return nil, store.ErrNoTurn
}

func (q *fakeQueue) ClaimTurn(_ context.Context, owner string, _ time.Duration) (*store.Turn, error) {
	q.fakeStore.mu.Lock()
	defer q.fakeStore.mu.Unlock()
	if s := q.turn.Status; s != store.TurnPending && s != store.TurnQueued {
		return nil, store.ErrNoTurn
	}
	q.turn.Status, q.turn.LeaseOwner = store.TurnRunning, &owner
	q.qmu.Lock()
	q.claims++
	q.qmu.Unlock()
	t := *q.turn
	return &t, nil
}

func (q *fakeQueue) RenewLease(_ context.Context, l store.Lease, ttl time.Duration) error {
	q.qmu.Lock()
	err := q.renewErr
	if err == nil {
		q.renewals = append(q.renewals, ttl)
	}
	q.qmu.Unlock()
	if err != nil {
		return err
	}
	q.fakeStore.mu.Lock()
	defer q.fakeStore.mu.Unlock()
	return q.checkLease(l)
}

func (q *fakeQueue) MarkQueued(_ context.Context, id string) (bool, error) {
	q.fakeStore.mu.Lock()
	defer q.fakeStore.mu.Unlock()
	if q.turn.ID != id || q.turn.Status != store.TurnPending {
		return false, nil
	}
	q.turn.Status = store.TurnQueued
	q.qmu.Lock()
	q.queuedMarks++
	q.qmu.Unlock()
	return true, nil
}

func (q *fakeQueue) snapshot() (queued, claims, renewals int) {
	q.qmu.Lock()
	defer q.qmu.Unlock()
	return q.queuedMarks, q.claims, len(q.renewals)
}

func (q *fakeQueue) status() string {
	q.fakeStore.mu.Lock()
	defer q.fakeStore.mu.Unlock()
	return q.turn.Status
}

// gateProv blocks each call until gate closes or its context ends.
type gateProv struct {
	gate    chan struct{}
	started chan struct{}
	mu      sync.Mutex
	n       int
}

func (p *gateProv) Name() string  { return "fake" }
func (p *gateProv) Model() string { return "fake-1" }
func (p *gateProv) Chat(ctx context.Context, _ provider.ChatRequest) (provider.ChatResponse, error) {
	p.mu.Lock()
	p.n++
	p.mu.Unlock()
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.gate:
		return textResp("done"), nil
	case <-ctx.Done():
		return provider.ChatResponse{}, &provider.ErrProvider{Err: ctx.Err()}
	}
}

func (p *gateProv) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func unitWorker(t *testing.T, q *fakeQueue, p provider.Provider, sb sandbox.Sandbox, cfg WorkerConfig) *Worker {
	t.Helper()
	h := newHarness(t)
	deps := h.deps
	deps.Store, deps.Provider, deps.Builder = q, p, sb
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 15 * time.Millisecond
	}
	if cfg.Owner == "" {
		cfg.Owner = "w-test"
	}
	w, err := NewWorker(deps, q, cfg)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func runWorker(w *Worker) (cancel func(), done chan struct{}) {
	ctx, c := context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	return c, done
}

func TestWorkerUnit_BuilderFullQueuesOnceThenRunsWhenCapacityFrees(t *testing.T) {
	q := newFakeQueue()
	sb := newFakeSandbox()
	sb.health = sandbox.Health{Active: 2, Capacity: 2}
	prov := &fakeProvider{}
	w := unitWorker(t, q, prov, sb, WorkerConfig{})
	cancel, done := runWorker(w)
	defer func() { cancel(); <-done }()

	eventually(t, "turn queued", func() bool { return q.status() == store.TurnQueued })
	time.Sleep(150 * time.Millisecond) // ~10 polls while full
	if queued, claims, _ := q.snapshot(); queued != 1 || claims != 0 {
		t.Fatalf("queued marks = %d, claims = %d; want 1 and 0 while the builder is full", queued, claims)
	}
	if prov.calls() != 0 {
		t.Fatal("the model was called while the builder was full")
	}

	sb.mu.Lock()
	sb.health = sandbox.Health{Active: 0, Capacity: 2}
	sb.mu.Unlock()
	eventually(t, "queued turn completed", func() bool { return q.status() == store.TurnCompleted })
	if queued, _, _ := q.snapshot(); queued != 1 {
		t.Errorf("queued marks = %d after running, want 1", queued)
	}
}

func TestWorkerUnit_UnreachableBuilderKeepsTurnQueued(t *testing.T) {
	q := newFakeQueue()
	sb := newFakeSandbox()
	sb.healthErr = context.DeadlineExceeded
	w := unitWorker(t, q, &fakeProvider{}, sb, WorkerConfig{})
	cancel, done := runWorker(w)
	defer func() { cancel(); <-done }()

	eventually(t, "turn queued", func() bool { return q.status() == store.TurnQueued })
	time.Sleep(80 * time.Millisecond)
	if _, claims, _ := q.snapshot(); claims != 0 {
		t.Errorf("claims = %d with the builder unreachable, want 0", claims)
	}
}

func TestWorkerUnit_RenewsLeaseAndStopsTheTurnWhenItIsLost(t *testing.T) {
	q := newFakeQueue()
	prov := &gateProv{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	w := unitWorker(t, q, prov, newFakeSandbox(), WorkerConfig{LeaseTTL: 300 * time.Millisecond, RenewEvery: 20 * time.Millisecond})
	cancel, done := runWorker(w)
	defer func() { cancel(); <-done }()

	<-prov.started
	eventually(t, "at least two lease renewals", func() bool { _, _, r := q.snapshot(); return r >= 2 })

	// Another replica takes the turn: the next renewal reports ErrLeaseLost.
	q.qmu.Lock()
	q.renewErr = store.ErrLeaseLost
	q.qmu.Unlock()
	eventually(t, "the running turn to be cancelled", func() bool {
		q.fakeStore.mu.Lock()
		defer q.fakeStore.mu.Unlock()
		return len(q.msgs) == 1 && q.finish == nil && prov.calls() == 1
	})
	// The worker must not finish a turn it no longer owns.
	time.Sleep(60 * time.Millisecond)
	if q.finish != nil {
		t.Errorf("turn finished after the lease was lost: %+v", q.finish)
	}
}

func TestWorkerUnit_GracefulStopWaitsForInFlightTurn(t *testing.T) {
	q := newFakeQueue()
	prov := &gateProv{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	w := unitWorker(t, q, prov, newFakeSandbox(), WorkerConfig{ShutdownGrace: 5 * time.Second})
	cancel, done := runWorker(w)
	<-prov.started
	cancel()
	select {
	case <-done:
		t.Fatal("Run returned with a turn still running")
	case <-time.After(150 * time.Millisecond):
	}
	close(prov.gate)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the turn finished")
	}
	if got := q.status(); got != store.TurnCompleted {
		t.Errorf("turn status = %q, want completed", got)
	}
}

func TestWorkerUnit_ShutdownPastGraceReleasesTheLease(t *testing.T) {
	q := newFakeQueue()
	prov := &gateProv{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	w := unitWorker(t, q, prov, newFakeSandbox(), WorkerConfig{ShutdownGrace: 100 * time.Millisecond, RenewEvery: time.Hour, LeaseTTL: 2 * time.Hour})
	cancel, done := runWorker(w)
	<-prov.started
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the grace period")
	}
	q.qmu.Lock()
	defer q.qmu.Unlock()
	if n := len(q.renewals); n == 0 || q.renewals[n-1] > time.Second {
		t.Errorf("lease renewals = %v, want a final near-zero TTL renewal that releases the lease", q.renewals)
	}
	if q.finish != nil {
		t.Error("a released turn must not be finished")
	}
}

func TestWorkerConcurrencyFromEnv(t *testing.T) {
	cases := []struct {
		val  string
		want int
	}{{"", DefaultWorkerConcurrency}, {"5", 5}, {" 3 ", 3}, {"0", DefaultWorkerConcurrency}, {"-2", DefaultWorkerConcurrency}, {"many", DefaultWorkerConcurrency}}
	for _, c := range cases {
		t.Setenv(EnvWorkerConcurrency, c.val)
		if got := WorkerConcurrencyFromEnv(); got != c.want {
			t.Errorf("BUILDER_WORKER_CONCURRENCY=%q -> %d, want %d", c.val, got, c.want)
		}
	}
}

func TestWorkerPoke_NeverBlocks(t *testing.T) {
	var nilWorker *Worker
	nilWorker.Poke() // safe on nil

	w := unitWorker(t, newFakeQueue(), &fakeProvider{}, newFakeSandbox(), WorkerConfig{})
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			w.Poke()
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Poke blocked with nobody receiving")
	}
}

// PrepareDeps runs once per claimed turn and the Deps it returns are the ones
// the turn runs with: a runtime-resolved provider reaches the loop.
func TestWorkerUnit_PrepareDepsProviderIsUsedPerTurn(t *testing.T) {
	q := newFakeQueue()
	base := &fakeProvider{}
	resolved := &fakeProvider{}
	var mu sync.Mutex
	prepared := 0
	w := unitWorker(t, q, base, newFakeSandbox(), WorkerConfig{
		PrepareDeps: func(_ context.Context, d Deps) Deps {
			mu.Lock()
			prepared++
			mu.Unlock()
			d.Provider = resolved
			return d
		},
	})
	cancel, done := runWorker(w)
	defer func() { cancel(); <-done }()

	eventually(t, "turn completed", func() bool { return q.status() == store.TurnCompleted })
	mu.Lock()
	n := prepared
	mu.Unlock()
	if n != 1 {
		t.Errorf("PrepareDeps calls = %d, want 1 (one claimed turn)", n)
	}
	if base.calls() != 0 || resolved.calls() == 0 {
		t.Errorf("provider calls: base=%d resolved=%d; the turn must use the prepared provider", base.calls(), resolved.calls())
	}
}

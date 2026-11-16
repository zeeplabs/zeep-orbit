package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// Worker defaults (design, section 8).
const (
	DefaultWorkerConcurrency = 2
	DefaultPollInterval      = time.Second
	DefaultLeaseTTL          = 30 * time.Second
	DefaultLeaseRenewEvery   = 10 * time.Second
	DefaultShutdownGrace     = 30 * time.Second

	// EnvWorkerConcurrency is the environment variable that sets the maximum
	// number of concurrent turns per replica.
	EnvWorkerConcurrency = "BUILDER_WORKER_CONCURRENCY"

	// releaseTTL is the lease a shutting-down worker leaves on a turn it could
	// not finish: it expires right away, so another replica resumes the turn.
	releaseTTL = time.Millisecond
)

// WorkerConcurrencyFromEnv reads BUILDER_WORKER_CONCURRENCY. Unset, empty,
// non-numeric or non-positive values yield [DefaultWorkerConcurrency].
func WorkerConcurrencyFromEnv() int {
	v := strings.TrimSpace(os.Getenv(EnvWorkerConcurrency))
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return DefaultWorkerConcurrency
	}
	return n
}

// WorkerStore is the queue side of the store. *store.Store satisfies it.
type WorkerStore interface {
	PeekClaimable(ctx context.Context) (*store.Turn, error)
	ClaimTurn(ctx context.Context, owner string, leaseTTL time.Duration) (*store.Turn, error)
	RenewLease(ctx context.Context, l store.Lease, ttl time.Duration) error
	MarkQueued(ctx context.Context, turnID string) (bool, error)
}

// WorkerConfig tunes a [Worker]; zero values use the defaults above.
type WorkerConfig struct {
	// Owner identifies this replica in lease_owner. Default: a random id.
	Owner string
	// Concurrency is the maximum number of turns this replica runs at once.
	Concurrency   int
	PollInterval  time.Duration
	LeaseTTL      time.Duration
	RenewEvery    time.Duration
	ShutdownGrace time.Duration
	// PrepareDeps, when set, runs at the start of every claimed turn and
	// returns the Deps that turn runs with. It lets a deployment resolve what
	// can change at runtime (the provider row, the limits from the settings
	// table) per turn instead of once at start-up. It must not fail: report a
	// problem through the Deps it returns (a Provider whose Chat errors ends
	// the turn with provider_unavailable). nil runs every turn with the Deps
	// given to NewWorker.
	PrepareDeps func(ctx context.Context, base Deps) Deps
}

// Worker is the per-replica turn executor: a Postgres-backed queue consumer.
// Any replica may run a worker; turns are claimed with FOR UPDATE SKIP LOCKED
// and owned through a lease, so no replica depends on another's memory.
type Worker struct {
	deps  Deps
	qs    WorkerStore
	cfg   WorkerConfig
	log   *slog.Logger
	poke  chan struct{}
	slots chan struct{}
	wg    sync.WaitGroup
}

// NewWorker returns a Worker that runs turns with deps, claiming from qs.
func NewWorker(deps Deps, qs WorkerStore, cfg WorkerConfig) (*Worker, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if qs == nil {
		return nil, errors.New("agent: worker store is required")
	}
	if cfg.Owner == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, fmt.Errorf("agent: generate worker id: %w", err)
		}
		host, _ := os.Hostname()
		cfg.Owner = host + "-" + hex.EncodeToString(b[:])
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = DefaultWorkerConcurrency
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = DefaultLeaseTTL
	}
	if cfg.RenewEvery <= 0 {
		cfg.RenewEvery = DefaultLeaseRenewEvery
	}
	if cfg.RenewEvery >= cfg.LeaseTTL {
		cfg.RenewEvery = cfg.LeaseTTL / 3
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Worker{
		deps: deps, qs: qs, cfg: cfg, log: log,
		poke:  make(chan struct{}, 1),
		slots: make(chan struct{}, cfg.Concurrency),
	}, nil
}

// Poke wakes the worker so a just-accepted message starts without waiting for
// the next poll. It never blocks and is safe on a nil Worker. It only helps the
// replica that holds the worker: other replicas find the turn by polling.
func (w *Worker) Poke() {
	if w == nil {
		return
	}
	select {
	case w.poke <- struct{}{}:
	default:
	}
}

// Run claims and runs turns until ctx is cancelled, then stops gracefully: it
// claims nothing more, waits up to ShutdownGrace for in-flight turns to finish
// and, for those still running after that, cancels them and releases their
// leases so another replica resumes them. Run returns once every turn
// goroutine is done.
func (w *Worker) Run(ctx context.Context) error {
	// Turns run on their own context so cancelling ctx lets them finish.
	runCtx, cancelRuns := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRuns()

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		w.fill(ctx, runCtx)
		select {
		case <-ctx.Done():
		case <-w.poke:
		case <-ticker.C:
		}
	}

	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(w.cfg.ShutdownGrace):
		w.log.Warn("agent worker: grace period over, releasing running turns", "owner", w.cfg.Owner)
		cancelRuns()
		<-done
	}
	return nil
}

// fill claims turns while there are free slots and claimable work.
func (w *Worker) fill(ctx, runCtx context.Context) {
	for ctx.Err() == nil {
		select {
		case w.slots <- struct{}{}:
		default:
			return // at BUILDER_WORKER_CONCURRENCY
		}
		turn := w.claimOne(ctx)
		if turn == nil {
			<-w.slots
			return
		}
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			defer func() { <-w.slots }()
			w.runClaimed(runCtx, turn)
			w.Poke() // a slot (and maybe builder capacity) just freed up
		}()
	}
}

// claimOne checks that there is work, then that the builder has capacity,
// then claims. A builder at capacity (or unreachable) leaves the turn
// 'queued', with a single `queued` event, and it runs on a later poll once
// capacity frees.
func (w *Worker) claimOne(ctx context.Context) *store.Turn {
	next, err := w.qs.PeekClaimable(ctx)
	if err != nil {
		if !errors.Is(err, store.ErrNoTurn) && ctx.Err() == nil {
			w.log.Error("agent worker: peek failed", "err", err)
		}
		return nil
	}

	h, herr := w.deps.Builder.Health(ctx)
	if herr != nil || (h.Capacity > 0 && h.Active >= h.Capacity) {
		if herr != nil && ctx.Err() == nil {
			w.log.Warn("agent worker: builder health check failed", "err", herr)
		}
		if next.Status == store.TurnPending {
			if _, err := w.qs.MarkQueued(ctx, next.ID); err != nil && ctx.Err() == nil {
				w.log.Error("agent worker: mark queued failed", "turn", next.ID, "err", err)
			}
		}
		return nil
	}

	turn, err := w.qs.ClaimTurn(ctx, w.cfg.Owner, w.cfg.LeaseTTL)
	if err != nil {
		if !errors.Is(err, store.ErrNoTurn) && ctx.Err() == nil {
			w.log.Error("agent worker: claim failed", "err", err)
		}
		return nil
	}
	return turn
}

// runClaimed runs one claimed turn with a lease renewal goroutine beside it.
func (w *Worker) runClaimed(runCtx context.Context, turn *store.Turn) {
	lease := store.Lease{TurnID: turn.ID, Owner: w.cfg.Owner}
	tctx, cancel := context.WithCancel(runCtx)
	defer cancel()

	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		w.renewLoop(tctx, cancel, lease)
	}()

	err := w.safeRun(tctx, lease)
	cancel()
	<-renewDone

	switch {
	case err == nil:
	case errors.Is(err, store.ErrLeaseLost):
		w.log.Info("agent worker: lease lost, turn left to its new owner", "turn", turn.ID)
	case runCtx.Err() != nil:
		// Shutdown: expire the lease now so another replica resumes the turn.
		rctx, rcancel := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
		defer rcancel()
		if rerr := w.qs.RenewLease(rctx, lease, releaseTTL); rerr != nil && !errors.Is(rerr, store.ErrLeaseLost) {
			w.log.Error("agent worker: release lease failed", "turn", turn.ID, "err", rerr)
		}
	case errors.Is(err, context.Canceled):
		// The renewal loop cancelled the turn after losing its lease.
		w.log.Info("agent worker: turn cancelled", "turn", turn.ID)
	default:
		w.log.Error("agent worker: turn stopped with an error", "turn", turn.ID, "err", err)
	}
}

func (w *Worker) safeRun(ctx context.Context, lease store.Lease) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			w.log.Error("agent worker: turn panicked", "turn", lease.TurnID, "panic", fmt.Sprint(rec))
			err = fmt.Errorf("turn panicked")
		}
	}()
	deps := w.deps
	if w.cfg.PrepareDeps != nil {
		deps = w.cfg.PrepareDeps(ctx, deps)
	}
	return RunTurn(ctx, deps, lease)
}

// renewLoop extends the lease every RenewEvery while the turn runs. Losing the
// lease cancels the turn: another replica owns it now.
func (w *Worker) renewLoop(ctx context.Context, cancelTurn context.CancelFunc, lease store.Lease) {
	t := time.NewTicker(w.cfg.RenewEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			err := w.qs.RenewLease(ctx, lease, w.cfg.LeaseTTL)
			switch {
			case err == nil:
			case errors.Is(err, store.ErrLeaseLost):
				cancelTurn()
				return
			case ctx.Err() == nil:
				// Transient DB error: keep trying until the lease actually expires.
				w.log.Warn("agent worker: lease renewal failed", "turn", lease.TurnID, "err", err)
			}
		}
	}
}

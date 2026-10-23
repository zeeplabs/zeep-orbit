package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// SyncerConfig wires a Syncer to its data source. Version and Load are
// injected so the registry package stays free of database code.
type SyncerConfig struct {
	// Version returns the current zeep_system.state_version. It must be
	// cheap (one row): it runs on every tick.
	Version func(ctx context.Context) (int64, error)
	// Load returns the full registry state. It only runs when Version moved.
	Load func(ctx context.Context) (Snapshot, error)
	// Interval between polls. Default 2s.
	Interval time.Duration
	// MissMinInterval is the shortest gap between two miss-triggered syncs
	// on one pod. Default 1s.
	MissMinInterval time.Duration
	// MissTimeout bounds a miss-triggered sync, which runs on the request
	// path. Default 2s.
	MissTimeout time.Duration
	// OnChange hooks run after every successful swap (e.g. clear a cache
	// derived from the registry).
	OnChange []func()
	// Logf receives "debug" and "warn" lines. nil discards them.
	Logf func(level, format string, args ...any)
}

// Syncer keeps one pod's Registry converged with the database. Every pod
// runs one; together they replace the boot-only load that left replicas
// stale after a change served by another pod.
type Syncer struct {
	reg *Registry
	cfg SyncerConfig

	group singleflight.Group

	mu          sync.Mutex
	lastVersion int64 // -1: unknown, the next sync reloads unconditionally
	lastMiss    time.Time
}

// NewSyncer returns a Syncer for reg. Zero durations get their defaults.
func NewSyncer(reg *Registry, cfg SyncerConfig) *Syncer {
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}
	if cfg.MissMinInterval <= 0 {
		cfg.MissMinInterval = time.Second
	}
	if cfg.MissTimeout <= 0 {
		cfg.MissTimeout = 2 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, string, ...any) {}
	}
	return &Syncer{reg: reg, cfg: cfg, lastVersion: -1}
}

// SyncNow reloads the registry if the state version moved since the last
// successful load. Concurrent calls share one in-flight sync. On error the
// registry keeps its last good state and the next call reloads regardless
// of the version.
func (s *Syncer) SyncNow(ctx context.Context) error {
	_, err, _ := s.group.Do("sync", func() (any, error) {
		return nil, s.sync(ctx)
	})
	return err
}

func (s *Syncer) sync(ctx context.Context) error {
	// Read the version before the load: a change committed during the load
	// bumps the version again, so the next tick reloads. At worst a change
	// is loaded twice, never missed.
	version, err := s.cfg.Version(ctx)
	if err != nil {
		s.markUnknown()
		s.cfg.Logf("warn", "registry sync: read state version: %v", err)
		return fmt.Errorf("registry sync: read state version: %w", err)
	}

	s.mu.Lock()
	unchanged := version == s.lastVersion
	s.mu.Unlock()
	if unchanged {
		return nil
	}

	gen := s.reg.Generation()
	snap, err := s.cfg.Load(ctx)
	if err != nil {
		s.markUnknown()
		s.cfg.Logf("warn", "registry sync: load snapshot (keeping the last good state): %v", err)
		return fmt.Errorf("registry sync: load snapshot: %w", err)
	}

	if !s.reg.ReplaceIfGeneration(gen, snap.Apps, snap.System) {
		// This pod registered a change during the load; the snapshot may
		// predate it. Keep the newer local state and reload next time.
		s.markUnknown()
		s.cfg.Logf("debug", "registry sync: local write during load, retrying on the next sync")
		return nil
	}
	s.mu.Lock()
	s.lastVersion = version
	s.mu.Unlock()
	for _, fn := range s.cfg.OnChange {
		fn()
	}
	s.cfg.Logf("debug", "registry sync: reloaded %d apps at state version %d", len(snap.Apps), version)
	return nil
}

func (s *Syncer) markUnknown() {
	s.mu.Lock()
	s.lastVersion = -1
	s.mu.Unlock()
}

// Run polls until ctx is done. Errors are logged by SyncNow and retried on
// the next tick.
func (s *Syncer) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.SyncNow(ctx)
		}
	}
}

// OnMiss is the registry miss hook: a lookup for an unknown app or table
// triggers a sync, at most once per MissMinInterval per pod, so a flood of
// requests for a name that really doesn't exist costs one version read per
// second, not one per request. Misses inside the window return immediately.
func (s *Syncer) OnMiss() {
	now := time.Now()
	s.mu.Lock()
	if !s.lastMiss.IsZero() && now.Sub(s.lastMiss) < s.cfg.MissMinInterval {
		s.mu.Unlock()
		return
	}
	s.lastMiss = now
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.MissTimeout)
	defer cancel()
	_ = s.SyncNow(ctx)
}

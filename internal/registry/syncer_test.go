package registry

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource is an in-memory stand-in for zeep_system: a version counter
// plus the snapshot Load returns.
type fakeSource struct {
	mu         sync.Mutex
	version    int64
	versionErr error
	snap       Snapshot
	loadErr    error
	loads      atomic.Int32
	loadDelay  time.Duration
}

func (f *fakeSource) Version(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, f.versionErr
}

func (f *fakeSource) Load(context.Context) (Snapshot, error) {
	f.loads.Add(1)
	if f.loadDelay > 0 {
		time.Sleep(f.loadDelay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return Snapshot{}, f.loadErr
	}
	apps := make(map[string]*App, len(f.snap.Apps))
	for k, v := range f.snap.Apps {
		apps[k] = v
	}
	return Snapshot{Apps: apps, System: f.snap.System}, nil
}

func (f *fakeSource) set(version int64, names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version = version
	f.snap.Apps = map[string]*App{}
	for _, n := range names {
		f.snap.Apps[n] = newTestApp(n)
	}
}

func newTestSyncer(reg *Registry, src *fakeSource, onChange ...func()) *Syncer {
	return NewSyncer(reg, SyncerConfig{
		Version:  src.Version,
		Load:     src.Load,
		OnChange: onChange,
	})
}

func TestSyncNow_LoadsOnlyWhenVersionChanges(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1, "a")
	changes := 0
	s := newTestSyncer(reg, src, func() { changes++ })
	ctx := context.Background()

	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if _, ok := reg.Get("a"); !ok {
		t.Fatal("app \"a\" should be loaded after the first sync")
	}
	if got := src.loads.Load(); got != 1 || changes != 1 {
		t.Fatalf("first sync: loads=%d changes=%d, want 1/1", got, changes)
	}

	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("unchanged sync: %v", err)
	}
	if got := src.loads.Load(); got != 1 || changes != 1 {
		t.Fatalf("unchanged version must not reload: loads=%d changes=%d", got, changes)
	}

	src.set(2, "b")
	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("changed sync: %v", err)
	}
	if got := src.loads.Load(); got != 2 || changes != 2 {
		t.Fatalf("changed version: loads=%d changes=%d, want 2/2", got, changes)
	}
	if _, ok := reg.Get("a"); ok {
		t.Fatal("app \"a\" should be gone after the reload")
	}
	if _, ok := reg.Get("b"); !ok {
		t.Fatal("app \"b\" should be loaded after the reload")
	}
}

func TestSyncNow_LoadErrorKeepsLastGoodState(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1, "good")
	changes := 0
	s := newTestSyncer(reg, src, func() { changes++ })
	ctx := context.Background()

	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.mu.Lock()
	src.version = 2
	src.loadErr = errors.New("db down")
	src.mu.Unlock()
	if err := s.SyncNow(ctx); err == nil {
		t.Fatal("sync should return the load error")
	}
	if _, ok := reg.Get("good"); !ok {
		t.Fatal("a failed load must keep the last good registry")
	}
	if changes != 1 {
		t.Fatalf("OnChange must not run on a failed load, ran %d times", changes)
	}

	// Same version as the failed attempt: must still reload.
	src.mu.Lock()
	src.loadErr = nil
	src.mu.Unlock()
	src.set(2, "recovered")
	loadsBefore := src.loads.Load()
	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("recovery sync: %v", err)
	}
	if src.loads.Load() != loadsBefore+1 {
		t.Fatal("after a failed load, the next sync must reload even with the same version")
	}
	if _, ok := reg.Get("recovered"); !ok {
		t.Fatal("recovery sync should load the new state")
	}
}

func TestSyncNow_VersionErrorKeepsLastGoodState(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1, "good")
	s := newTestSyncer(reg, src)
	ctx := context.Background()

	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	src.mu.Lock()
	src.versionErr = errors.New("db down")
	src.mu.Unlock()
	if err := s.SyncNow(ctx); err == nil {
		t.Fatal("sync should return the version error")
	}
	if _, ok := reg.Get("good"); !ok {
		t.Fatal("a failed version read must keep the last good registry")
	}

	src.mu.Lock()
	src.versionErr = nil
	src.mu.Unlock()
	loadsBefore := src.loads.Load()
	if err := s.SyncNow(ctx); err != nil {
		t.Fatalf("recovery sync: %v", err)
	}
	if src.loads.Load() != loadsBefore+1 {
		t.Fatal("after a failed version read, the next sync must reload even with the same version")
	}
}

func TestSyncNow_ConcurrentCallsShareOneLoad(t *testing.T) {
	reg := New()
	src := &fakeSource{loadDelay: 50 * time.Millisecond}
	src.set(1, "a")
	s := newTestSyncer(reg, src)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = s.SyncNow(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	if got := src.loads.Load(); got != 1 {
		t.Fatalf("50 concurrent SyncNow calls performed %d loads, want 1", got)
	}
}

func TestOnMiss_RateLimited(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1, "a")
	var versionReads atomic.Int32
	s := NewSyncer(reg, SyncerConfig{
		Version: func(ctx context.Context) (int64, error) {
			versionReads.Add(1)
			return src.Version(ctx)
		},
		Load:            src.Load,
		MissMinInterval: time.Second,
	})

	for i := 0; i < 100; i++ {
		s.OnMiss()
	}
	if got := versionReads.Load(); got != 1 {
		t.Fatalf("100 misses within 1s triggered %d syncs, want 1", got)
	}
}

func TestOnMiss_FindsAppCreatedElsewhere(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1)
	s := newTestSyncer(reg, src)
	if err := s.SyncNow(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	reg.SetMissHook(s.OnMiss)

	src.set(2, "created-on-other-pod")
	if _, ok := reg.Get("created-on-other-pod"); !ok {
		t.Fatal("a miss should sync and find the app created on another pod")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1, "a")
	s := NewSyncer(reg, SyncerConfig{Version: src.Version, Load: src.Load, Interval: 10 * time.Millisecond})

	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := reg.Get("a"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run did not sync within 1s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	// Give the runtime a moment to reap the goroutine before counting.
	deadline = time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines leaked: before=%d after=%d", before, after)
	}
}

func TestSyncNow_LogsWarnOnFailureAndDebugOnReload(t *testing.T) {
	reg := New()
	src := &fakeSource{}
	src.set(1, "a")
	var mu sync.Mutex
	levels := map[string]int{}
	s := NewSyncer(reg, SyncerConfig{
		Version: src.Version,
		Load:    src.Load,
		Logf: func(level, _ string, _ ...any) {
			mu.Lock()
			levels[level]++
			mu.Unlock()
		},
	})
	_ = s.SyncNow(context.Background())
	src.mu.Lock()
	src.versionErr = errors.New("db down")
	src.mu.Unlock()
	_ = s.SyncNow(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if levels["debug"] != 1 || levels["warn"] != 1 {
		t.Fatalf("log levels = %v, want one debug and one warn", levels)
	}
}

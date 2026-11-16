package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// flakySandbox answers Op with scripted errors and records Restore calls.
type flakySandbox struct {
	Sandbox // unimplemented methods panic on nil, on purpose

	opErrs      []error // consumed one per Op call; nil entry = success
	opCalls     int
	restoreErr  error
	restores    int
	restoredFor []Bundle
}

func (f *flakySandbox) Op(_ context.Context, _, _ string, _ FileOp) (OpResult, error) {
	var err error
	if f.opCalls < len(f.opErrs) {
		err = f.opErrs[f.opCalls]
	}
	f.opCalls++
	if err != nil {
		return OpResult{}, err
	}
	return OpResult{OK: true, Output: "done"}, nil
}

func (f *flakySandbox) Restore(_ context.Context, _ string, b []Bundle) error {
	f.restores++
	f.restoredFor = b
	return f.restoreErr
}

func snaps(n int) (SnapshotProvider, *int) {
	calls := 0
	return func(context.Context, string) ([]Bundle, error) {
		calls++
		out := make([]Bundle, n)
		for i := range out {
			out[i] = Bundle{Seq: i, Data: []byte("b")}
		}
		return out, nil
	}, &calls
}

func TestWithRestore_UnknownSessionRestoresThenSucceeds(t *testing.T) {
	f := &flakySandbox{opErrs: []error{ErrUnknownSession, nil}}
	provider, calls := snaps(2)
	sb := NewRestoring(f, provider)

	res, err := sb.Op(context.Background(), "s", "id", FileOp{Kind: OpListDir})
	if err != nil || res.Output != "done" {
		t.Fatalf("got %+v, %v", res, err)
	}
	if f.opCalls != 2 || f.restores != 1 || *calls != 1 || len(f.restoredFor) != 2 {
		t.Fatalf("op calls %d, restores %d, snapshot loads %d, bundles %d", f.opCalls, f.restores, *calls, len(f.restoredFor))
	}
}

func TestWithRestore_SecondUnknownIsWorkspaceLost(t *testing.T) {
	f := &flakySandbox{opErrs: []error{ErrUnknownSession, ErrUnknownSession, nil}}
	provider, _ := snaps(1)

	_, err := WithRestore(context.Background(), f, "s", provider, func(ctx context.Context) (OpResult, error) {
		return f.Op(ctx, "s", "id", FileOp{})
	})
	if !errors.Is(err, ErrWorkspaceLost) {
		t.Fatalf("want ErrWorkspaceLost, got %v", err)
	}
	if errors.Is(err, ErrUnknownSession) {
		t.Fatal("a lost workspace must not look retryable to an outer WithRestore")
	}
	if f.opCalls != 2 || f.restores != 1 {
		t.Fatalf("must retry exactly once: op calls %d, restores %d", f.opCalls, f.restores)
	}
}

func TestWithRestore_OtherErrorsAreNotRestored(t *testing.T) {
	boom := errors.New("boom")
	f := &flakySandbox{opErrs: []error{boom}}
	provider, calls := snaps(1)
	_, err := NewRestoring(f, provider).Op(context.Background(), "s", "id", FileOp{})
	if !errors.Is(err, boom) || f.restores != 0 || *calls != 0 || f.opCalls != 1 {
		t.Fatalf("err %v restores %d loads %d calls %d", err, f.restores, *calls, f.opCalls)
	}
}

func TestWithRestore_RestoreFailureModes(t *testing.T) {
	ctx := context.Background()
	call := func(f *flakySandbox, p SnapshotProvider) error {
		_, err := NewRestoring(f, p).Op(ctx, "s", "id", FileOp{})
		return err
	}

	// No snapshot provider, or no snapshots stored.
	if err := call(&flakySandbox{opErrs: []error{ErrUnknownSession}}, nil); !errors.Is(err, ErrWorkspaceLost) {
		t.Fatalf("nil provider: %v", err)
	}
	empty, _ := snaps(0)
	if err := call(&flakySandbox{opErrs: []error{ErrUnknownSession}}, empty); !errors.Is(err, ErrWorkspaceLost) {
		t.Fatalf("no snapshots: %v", err)
	}

	// Provider failure and restore failure keep their cause for logs.
	cause := errors.New("db down")
	failing := func(context.Context, string) ([]Bundle, error) { return nil, cause }
	if err := call(&flakySandbox{opErrs: []error{ErrUnknownSession}}, failing); !errors.Is(err, ErrWorkspaceLost) || !errors.Is(err, cause) {
		t.Fatalf("provider error: %v", err)
	}
	p, _ := snaps(1)
	rf := &flakySandbox{opErrs: []error{ErrUnknownSession}, restoreErr: &Error{Status: 422, Code: "bundle_verify_failed", Message: "bad"}}
	if err := call(rf, p); !errors.Is(err, ErrWorkspaceLost) || rf.opCalls != 1 {
		t.Fatalf("restore error: %v (op calls %d)", err, rf.opCalls)
	}

	// Someone else restored first: session_exists counts as success.
	ex := &flakySandbox{opErrs: []error{ErrUnknownSession, nil}, restoreErr: &Error{Status: 409, Code: "session_exists", Message: "exists"}}
	if err := call(ex, p); err != nil || ex.opCalls != 2 {
		t.Fatalf("session_exists: %v (op calls %d)", err, ex.opCalls)
	}

	// Cancellation is reported as such, not as a lost workspace.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	cf := &flakySandbox{opErrs: []error{ErrUnknownSession}, restoreErr: context.Canceled}
	_, err := NewRestoring(cf, p).Op(cctx, "s", "id", FileOp{})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWorkspaceLost) {
		t.Fatalf("cancel: %v", err)
	}
}

// TestWithRestore_RealBuilderVolumeLoss is the AFB-19 path end to end: commit,
// lose the working copy, and the next op restores from the stored bundles.
func TestWithRestore_RealBuilderVolumeLoss(t *testing.T) {
	raw, _ := newClient(t)
	ctx := context.Background()
	const sid = "lost-1"

	init, err := raw.Init(ctx, sid, TemplateSource{Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Op(ctx, sid, "w1", FileOp{Kind: OpWriteFile, Path: "src/kept.ts", Content: "export const kept = 1\n"}); err != nil {
		t.Fatal(err)
	}
	c, err := raw.Commit(ctx, sid, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	stored := []Bundle{{Seq: 0, SHA: init.SHA, Data: init.Bundle}, {Seq: 1, SHA: c.SHA, Data: c.Bundle}}

	// Simulate volume loss: the builder forgets the session.
	if err := raw.Destroy(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Op(ctx, sid, "", FileOp{Kind: OpListDir}); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("precondition: %v", err)
	}

	loads := 0
	sb := NewRestoring(raw, func(context.Context, string) ([]Bundle, error) { loads++; return stored, nil })
	res, err := sb.Op(ctx, sid, "", FileOp{Kind: OpReadFile, Path: "src/kept.ts"})
	if err != nil || !strings.Contains(res.Output, "kept = 1") {
		t.Fatalf("op after restore: %+v %v", res, err)
	}
	if loads != 1 {
		t.Fatalf("snapshots loaded %d times", loads)
	}
}

func TestError_ToolLevelClassification(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{{400, true}, {403, true}, {404, true}, {413, true}, {422, true},
		{401, false}, {429, false}, {500, false}, {504, false}, {0, false}}
	for _, c := range cases {
		if got := (&Error{Status: c.status}).ToolLevel(); got != c.want {
			t.Errorf("status %d: ToolLevel = %v, want %v", c.status, got, c.want)
		}
	}
	if _, ok := ToolMessage(errors.New("plain")); ok {
		t.Error("a plain error is never model-visible")
	}
	long := sanitizeMessage(strings.Repeat("é", 600) + "\x00")
	if len(long) > maxMessageLen || strings.ContainsRune(long, 0) {
		t.Errorf("sanitizeMessage kept %d bytes / NUL", len(long))
	}
}

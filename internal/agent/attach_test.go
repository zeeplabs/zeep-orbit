package agent

// attach_test.go — the attach_backend turn of the loop (builder-no-backend
// NB-08, T11d-2): overlay -> commit -> snapshot -> AttachBackend -> install and
// build, with the compensation of the failure matrix in the T11b design.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// attachSandbox adds the overlay, the revert and failure switches to the fake.
type attachSandbox struct {
	*fakeSandbox
	amu         sync.Mutex
	order       []string
	overlayErr  error
	overlayDirt bool // the overlay failed half way: files are left uncommitted
	overlayRes  sandbox.OverlayResult
	overlayOpID string
	overlayFile map[string]string
	commitErr   error
	revertErr   error
	reverts     []revertCall
	steps       []sandbox.BuildStep
	failStep    sandbox.BuildStep // exit 1 on this step
	buildErr    error
}

func (s *attachSandbox) note(what string) {
	s.amu.Lock()
	s.order = append(s.order, what)
	s.amu.Unlock()
}

func (s *attachSandbox) ApplyBackendOverlay(_ context.Context, _, opID string, files map[string]string) (sandbox.OverlayResult, error) {
	s.note("overlay")
	s.amu.Lock()
	s.overlayOpID, s.overlayFile = opID, files
	err, dirt, res := s.overlayErr, s.overlayDirt, s.overlayRes
	s.amu.Unlock()
	if dirt {
		s.fakeSandbox.mu.Lock()
		s.fakeSandbox.dirty = true
		s.fakeSandbox.mu.Unlock()
	}
	if err != nil {
		return sandbox.OverlayResult{}, err
	}
	s.fakeSandbox.mu.Lock()
	s.fakeSandbox.dirty = true
	s.fakeSandbox.mu.Unlock()
	return res, nil
}

func (s *attachSandbox) Commit(ctx context.Context, sid string, seq int, msg string) (sandbox.CommitResult, error) {
	s.note("commit")
	if s.commitErr != nil {
		return sandbox.CommitResult{}, s.commitErr
	}
	return s.fakeSandbox.Commit(ctx, sid, seq, msg)
}

func (s *attachSandbox) Revert(_ context.Context, _ string, toSHA string, seq int) (sandbox.CommitResult, error) {
	s.note("revert")
	s.amu.Lock()
	s.reverts = append(s.reverts, revertCall{toSHA, seq})
	err := s.revertErr
	s.amu.Unlock()
	if err != nil {
		return sandbox.CommitResult{}, err
	}
	s.fakeSandbox.mu.Lock()
	defer s.fakeSandbox.mu.Unlock()
	s.fakeSandbox.dirty = false
	prev := s.fakeSandbox.head
	s.fakeSandbox.head = "sha-cleanup"
	return sandbox.CommitResult{SHA: "sha-cleanup", Prev: prev, Changed: true, Bundle: []byte("bundle-cleanup")}, nil
}

func (s *attachSandbox) Build(_ context.Context, _, _ string, step sandbox.BuildStep, _ string) (sandbox.BuildResult, error) {
	s.note("build:" + string(step))
	s.amu.Lock()
	s.steps = append(s.steps, step)
	fail, err := s.failStep, s.buildErr
	s.amu.Unlock()
	if err != nil {
		return sandbox.BuildResult{}, err
	}
	if step == fail {
		return sandbox.BuildResult{ExitCode: 1, OutputTail: "boom"}, nil
	}
	return sandbox.BuildResult{OutputTail: "ok"}, nil
}

var attachNew = store.AttachPayload{Mode: store.AttachModeNew, AppName: "shop-abc123", Config: "export default {}\n"}

type attachCall struct {
	mode  string
	appID string
}

func newAttachHarness(t *testing.T, p store.AttachPayload) (*harness, *attachSandbox, *[]attachCall) {
	t.Helper()
	h := newHarness(t)
	as := &attachSandbox{fakeSandbox: h.sb}
	h.deps.Builder = as
	h.st.turn.Kind = store.KindAttachBackend
	h.st.turn.Seq = 4
	h.st.sess.BackendAppID = ""
	h.st.sess.BackendOrigin = store.BackendOriginNone
	raw, err := json.Marshal(map[string]any{"attach": p})
	if err != nil {
		t.Fatal(err)
	}
	h.st.turn.PendingProposal = raw
	var calls []attachCall
	h.deps.AttachBackend = func(_ context.Context, _ *store.Session, p store.AttachPayload) (string, error) {
		as.note("attach")
		calls = append(calls, attachCall{p.Mode, p.AppID})
		return "app-new", nil
	}
	return h, as, &calls
}

func (s *attachSandbox) orderString() string {
	s.amu.Lock()
	defer s.amu.Unlock()
	return strings.Join(s.order, ",")
}

func TestRunTurn_Attach_HappyPath(t *testing.T) {
	h, as, calls := newAttachHarness(t, attachNew)
	as.overlayRes = sandbox.OverlayResult{Replaced: []string{"ORBIT_AGENT.md"}}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnCompleted || fin.CommitSHA != "sha1" || fin.ErrorCode != "" {
		t.Fatalf("finish = %+v, want completed with the overlay commit", fin)
	}
	if h.prov.calls() != 0 || h.bud.calls != 0 {
		t.Errorf("an attach makes no model call (provider %d, budget %d)", h.prov.calls(), h.bud.calls)
	}
	if got := as.orderString(); got != "overlay,commit,attach,build:install,build:build" {
		t.Errorf("order = %s", got)
	}
	if as.overlayOpID != "attach-turn-1" || as.overlayFile["src/orbit.config.ts"] != attachNew.Config {
		t.Errorf("overlay call = %q %v", as.overlayOpID, as.overlayFile)
	}
	if len(*calls) != 1 || (*calls)[0].mode != store.AttachModeNew {
		t.Errorf("attach calls = %+v", *calls)
	}
	// The overlay is safe before the backend is attached.
	if last := h.st.snaps[len(h.st.snaps)-1]; last.TurnSeq != 4 || last.CommitSHA != "sha1" {
		t.Errorf("snapshot = %+v", last)
	}
	if got := strings.Join(h.st.eventTypes(), ","); got != "turn_started,build_started,build_succeeded,build_started,build_succeeded,backend_attached,turn_finished" {
		t.Errorf("events = %s", got)
	}
	p := eventPayload(t, h, EventBackendAttached)
	if p["mode"] != "new" || p["app_id"] != "app-new" || p["build"] != RevertBuildSucceeded {
		t.Errorf("event = %v", p)
	}
	if rep, _ := p["replaced"].([]any); len(rep) != 1 || rep[0] != "ORBIT_AGENT.md" {
		t.Errorf("replaced = %v", p["replaced"])
	}
}

func TestRunTurn_Attach_ExistingModePassesTheApp(t *testing.T) {
	h, _, calls := newAttachHarness(t, store.AttachPayload{Mode: store.AttachModeExisting, AppID: "app-9", Config: "x"})
	h.mustRun()
	if h.finished().Status != store.TurnCompleted {
		t.Fatalf("finish = %+v", h.finished())
	}
	if len(*calls) != 1 || (*calls)[0] != (attachCall{store.AttachModeExisting, "app-9"}) {
		t.Errorf("attach calls = %+v", *calls)
	}
}

func TestRunTurn_Attach_OverlayConflict_WritesNothingAndCleansNothing(t *testing.T) {
	h, as, calls := newAttachHarness(t, attachNew)
	as.overlayErr = &sandbox.Error{Status: 409, Code: sandbox.CodeOverlayConflict, Message: "src/lib/orbit.ts"}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeOverlayConflict || fin.CommitSHA != "" {
		t.Fatalf("finish = %+v", fin)
	}
	if got := as.orderString(); got != "overlay" {
		t.Errorf("order = %s, want only the overlay call (the builder wrote nothing)", got)
	}
	if len(*calls) != 0 {
		t.Errorf("no backend may be attached: %+v", *calls)
	}
}

func TestRunTurn_Attach_OverlayFailsHalfWay_DiscardsTheUncommittedTree(t *testing.T) {
	h, as, calls := newAttachHarness(t, attachNew)
	as.overlayErr = &sandbox.Error{Status: 500, Message: "disk full"}
	as.overlayDirt = true
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeAttachFailed {
		t.Fatalf("finish = %+v", fin)
	}
	if len(as.reverts) != 1 || as.reverts[0] != (revertCall{"sha0", 4}) {
		t.Errorf("reverts = %+v, want one back to the head the session had", as.reverts)
	}
	if as.fakeSandbox.dirty {
		t.Error("the working tree must be clean")
	}
	if len(*calls) != 0 {
		t.Errorf("attach calls = %+v", *calls)
	}
}

func TestRunTurn_Attach_WorkspaceLostOnOverlay(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	as.overlayErr = sandbox.ErrWorkspaceLost
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnFailed || fin.ErrorCode != CodeWorkspaceLost {
		t.Fatalf("finish = %+v", fin)
	}
}

func TestRunTurn_Attach_CommitFails_NoAppAndTheTreeIsPutBack(t *testing.T) {
	h, as, calls := newAttachHarness(t, attachNew)
	as.commitErr = &sandbox.Error{Status: 500, Message: "git"}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeCommitFailed {
		t.Fatalf("finish = %+v", fin)
	}
	if len(*calls) != 0 {
		t.Errorf("no app may be created when the overlay is not committed: %+v", *calls)
	}
	if len(as.reverts) != 1 || as.reverts[0].sha != "sha0" {
		t.Errorf("reverts = %+v", as.reverts)
	}
}

func TestRunTurn_Attach_AttachFails_RevertsAsANewCommitAndSnapshotsIt(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	h.deps.AttachBackend = func(context.Context, *store.Session, store.AttachPayload) (string, error) {
		as.note("attach")
		return "", store.ErrBackendAlreadyAttached
	}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeAttachFailed || fin.CommitSHA != "" {
		t.Fatalf("finish = %+v, want failed attach_failed with no head moved", fin)
	}
	if got := as.orderString(); got != "overlay,commit,attach,revert" {
		t.Errorf("order = %s, want no build after a failed attach", got)
	}
	if len(as.reverts) != 1 || as.reverts[0] != (revertCall{"sha0", 4}) {
		t.Errorf("reverts = %+v, want one back to sha0", as.reverts)
	}
	if last := h.st.snaps[len(h.st.snaps)-1]; last.CommitSHA != "sha-cleanup" {
		t.Errorf("the revert commit must be snapshotted, last snapshot = %+v", last)
	}
	if got := strings.Join(h.st.eventTypes(), ","); got != "turn_started,error,turn_finished" {
		t.Errorf("events = %s", got)
	}
}

func TestRunTurn_Attach_AttachFailsAndRevertFails_StillFailsWithItsOwnCode(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	as.revertErr = errors.New("git exploded")
	h.deps.AttachBackend = func(context.Context, *store.Session, store.AttachPayload) (string, error) {
		return "", errors.New("create app: boom")
	}
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnFailed || fin.ErrorCode != CodeAttachFailed {
		t.Fatalf("finish = %+v", fin)
	}
}

func TestRunTurn_Attach_BuildFailure_DoesNotFailTheTurn(t *testing.T) {
	for _, step := range []sandbox.BuildStep{sandbox.StepInstall, sandbox.StepBuild} {
		t.Run(string(step), func(t *testing.T) {
			h, as, _ := newAttachHarness(t, attachNew)
			as.failStep = step
			h.mustRun()
			if fin := h.finished(); fin.Status != store.TurnCompleted {
				t.Fatalf("finish = %+v, the backend is attached: the turn completes", fin)
			}
			if p := eventPayload(t, h, EventBackendAttached); p["build"] != RevertBuildFailed {
				t.Errorf("event = %v", p)
			}
			if step == sandbox.StepInstall && len(as.steps) != 1 {
				t.Errorf("steps = %v, the build must not run after a failed install", as.steps)
			}
		})
	}
}

func TestRunTurn_Attach_BuilderUnavailableDuringBuild(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	as.buildErr = errors.New("connection refused")
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("finish = %+v", fin)
	}
	if p := eventPayload(t, h, EventBackendAttached); p["build"] != RevertBuildError {
		t.Errorf("event = %v", p)
	}
}

func TestRunTurn_Attach_WithoutCallbackFailsCleanly(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	h.deps.AttachBackend = nil
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnFailed || fin.ErrorCode != CodeAttachUnavailable {
		t.Fatalf("finish = %+v", fin)
	}
	if got := as.orderString(); got != "" {
		t.Errorf("order = %s, nothing may be written without a callback", got)
	}
}

func TestRunTurn_Attach_WithoutPayloadFails(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	h.st.turn.PendingProposal = nil
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnFailed || fin.ErrorCode != CodeInternal {
		t.Fatalf("finish = %+v", fin)
	}
	if got := as.orderString(); got != "" {
		t.Errorf("order = %s", got)
	}
}

func TestRunTurn_Attach_CancelledWhileAttachingLeavesTheTurnForTheNextAttempt(t *testing.T) {
	h, as, _ := newAttachHarness(t, attachNew)
	ctx, cancel := context.WithCancel(context.Background())
	h.deps.AttachBackend = func(context.Context, *store.Session, store.AttachPayload) (string, error) {
		cancel()
		return "", context.Canceled
	}
	if err := h.run(ctx); err == nil {
		t.Fatal("a cancelled run must return its error so the turn is resumed")
	}
	if len(as.reverts) != 0 {
		t.Errorf("a cancelled attach is retried, not compensated: %+v", as.reverts)
	}
}

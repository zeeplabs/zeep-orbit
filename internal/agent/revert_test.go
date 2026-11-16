package agent

// revert_test.go — the revert turn of the loop (ai-frontend-builder T28):
// no model call, Sandbox.Revert -> snapshot -> build -> `revert` event.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// revertSandbox adds Revert to the fake sandbox and records what the revert
// turn asked of the builder.
type revertSandbox struct {
	*fakeSandbox
	rmu         sync.Mutex
	revertErr   error
	revertRes   sandbox.CommitResult
	buildErr    error
	reverts     []revertCall
	previewBase []string
	buildOpIDs  []string
	order       []string
	onRevert    func()
}

type revertCall struct {
	sha string
	seq int
}

func (s *revertSandbox) Revert(_ context.Context, _ string, toSHA string, turnSeq int) (sandbox.CommitResult, error) {
	if s.onRevert != nil {
		s.onRevert()
	}
	s.rmu.Lock()
	defer s.rmu.Unlock()
	s.order = append(s.order, "revert")
	s.reverts = append(s.reverts, revertCall{toSHA, turnSeq})
	if s.revertErr != nil {
		return sandbox.CommitResult{}, s.revertErr
	}
	return s.revertRes, nil
}

func (s *revertSandbox) Build(ctx context.Context, sid, opID string, step sandbox.BuildStep, base string) (sandbox.BuildResult, error) {
	s.rmu.Lock()
	s.order = append(s.order, "build")
	s.previewBase = append(s.previewBase, base)
	s.buildOpIDs = append(s.buildOpIDs, opID)
	err := s.buildErr
	s.rmu.Unlock()
	if err != nil {
		return sandbox.BuildResult{}, err
	}
	return s.fakeSandbox.Build(ctx, sid, opID, step, base)
}

func newRevertHarness(t *testing.T, target *store.RevertTarget) (*harness, *revertSandbox) {
	t.Helper()
	h := newHarness(t)
	rs := &revertSandbox{
		fakeSandbox: h.sb,
		revertRes:   sandbox.CommitResult{SHA: "sha-revert", Prev: "sha0", Changed: true, Bundle: []byte("bundle-revert")},
	}
	h.deps.Builder = rs
	h.st.turn.Kind = store.KindRevert
	h.st.turn.Seq = 3
	if target != nil {
		raw, err := json.Marshal(map[string]any{"revert": target})
		if err != nil {
			t.Fatal(err)
		}
		h.st.turn.PendingProposal = raw
	}
	return h, rs
}

func eventPayload(t *testing.T, h *harness, typ string) map[string]any {
	t.Helper()
	evs := h.st.eventsOf(typ)
	if len(evs) != 1 {
		t.Fatalf("%s events = %d, want 1 (all: %v)", typ, len(evs), h.st.eventTypes())
	}
	var m map[string]any
	if err := json.Unmarshal(evs[0].Payload, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRunTurn_Revert_RestoresSnapshotsAndRebuildsWithoutTheModel(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnCompleted || fin.CommitSHA != "sha-revert" || fin.ErrorCode != "" {
		t.Fatalf("finish = %+v, want completed with the revert commit as the turn commit", fin)
	}
	if h.prov.calls() != 0 || h.bud.calls != 0 {
		t.Errorf("a revert makes no model call and needs no budget (provider calls %d, budget checks %d)", h.prov.calls(), h.bud.calls)
	}
	if len(rs.reverts) != 1 || rs.reverts[0] != (revertCall{"sha1", 3}) {
		t.Fatalf("Revert calls = %+v, want one to sha1 as turn 3", rs.reverts)
	}
	// Snapshot first (durability), then the build.
	if strings.Join(rs.order, ",") != "revert,build" {
		t.Errorf("builder call order = %v", rs.order)
	}
	last := h.st.snaps[len(h.st.snaps)-1]
	if last.TurnSeq != 3 || last.Part != 0 || last.CommitSHA != "sha-revert" || string(last.Bundle) != "bundle-revert" {
		t.Errorf("snapshot = %+v, want the revert commit saved as turn 3 part 0", last)
	}
	if got := strings.Join(h.st.eventTypes(), ","); got != "turn_started,build_started,build_succeeded,revert,turn_finished" {
		t.Errorf("events = %s", got)
	}
	p := eventPayload(t, h, EventRevert)
	if p["to_turn_seq"] != float64(1) || p["sha"] != "sha-revert" || p["changed"] != true || p["build"] != RevertBuildSucceeded {
		t.Errorf("revert event = %v", p)
	}
	if want := "./"; rs.previewBase[0] != want {
		t.Errorf("preview base = %q, want %q", rs.previewBase[0], want)
	}
	if rs.buildOpIDs[0] != "revert-turn-1-build" {
		t.Errorf("build op id = %q, want a stable id per turn", rs.buildOpIDs[0])
	}
}

func TestRunTurn_Revert_UsesTheConfiguredPreviewBase(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
	h.deps.PreviewBase = func(id string) string { return "/p/" + id + "/" }
	h.mustRun()
	if rs.previewBase[0] != "/p/sess-1/" {
		t.Errorf("preview base = %q", rs.previewBase[0])
	}
}

func TestRunTurn_Revert_FailedBuildStillLeavesTheTreeRevertedAndReportsIt(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
	rs.fakeSandbox.builds = []sandbox.BuildResult{{ExitCode: 1, OutputTail: "error TS2304: cannot find name"}}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnCompleted || fin.CommitSHA != "sha-revert" {
		t.Fatalf("finish = %+v, want completed: the tree is reverted even if the build is not green", fin)
	}
	if last := h.st.snaps[len(h.st.snaps)-1]; last.CommitSHA != "sha-revert" {
		t.Errorf("the revert commit must be snapshotted even when the build fails, got %+v", last)
	}
	if got := strings.Join(h.st.eventTypes(), ","); got != "turn_started,build_started,build_failed,revert,turn_finished" {
		t.Errorf("events = %s", got)
	}
	if p := eventPayload(t, h, EventRevert); p["build"] != RevertBuildFailed {
		t.Errorf("revert event = %v, want build=failed", p)
	}
	if p := eventPayload(t, h, EventBuildFailed); !strings.Contains(fmt.Sprint(p["tail"]), "TS2304") || p["step"] != "build" {
		t.Errorf("build_failed event = %v", p)
	}
}

func TestRunTurn_Revert_BuilderCannotBuildIsReportedNotFatal(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
	rs.buildErr = &sandbox.Error{Status: 502, Code: "bad_gateway", Message: "builder unavailable", Err: errors.New("dial secret-host.internal: refused")}
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	p := eventPayload(t, h, EventRevert)
	if p["build"] != RevertBuildError {
		t.Errorf("revert event = %v, want build=%s", p, RevertBuildError)
	}
	for _, e := range h.st.events {
		if strings.Contains(string(e.Payload), "secret-host") {
			t.Errorf("raw builder error leaked into an event: %s", e.Payload)
		}
	}
}

func TestRunTurn_Revert_UnchangedTreeAddsNoCommit(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha0"})
	rs.revertRes = sandbox.CommitResult{SHA: "sha0", Prev: "sha0"} // HEAD already has that tree
	before := len(h.st.snaps)
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnCompleted || fin.CommitSHA != "" {
		t.Fatalf("finish = %+v, want completed without a commit", fin)
	}
	if len(h.st.snaps) != before {
		t.Error("no snapshot may be saved when nothing changed")
	}
	if p := eventPayload(t, h, EventRevert); p["changed"] != false || p["sha"] != "" {
		t.Errorf("revert event = %v", p)
	}
}

func TestRunTurn_Revert_FailureCodesAreGenericAndNothingIsSnapshotted(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"builder error", &sandbox.Error{Status: 500, Code: "git_failed", Message: "x", Err: errors.New("fatal: /srv/secret/path")}, CodeRevertFailed},
		{"workspace lost", sandbox.ErrWorkspaceLost, CodeWorkspaceLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
			rs.revertErr = tc.err
			before := len(h.st.snaps)
			h.mustRun()

			fin := h.finished()
			if fin.Status != store.TurnFailed || fin.ErrorCode != tc.code {
				t.Fatalf("finish = %+v, want failed/%s", fin, tc.code)
			}
			if len(h.st.snaps) != before || len(rs.previewBase) != 0 {
				t.Error("a failed revert must not snapshot or build")
			}
			p := eventPayload(t, h, EventError)
			if p["code"] != tc.code || len(p) != 1 {
				t.Errorf("error event = %v, want only the generic code", p)
			}
			for _, e := range h.st.events {
				if strings.Contains(string(e.Payload), "secret") {
					t.Errorf("raw error leaked into an event: %s", e.Payload)
				}
			}
		})
	}
}

func TestRunTurn_Revert_WithoutATargetFailsWithoutTouchingTheBuilder(t *testing.T) {
	h, rs := newRevertHarness(t, nil)
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnFailed || fin.ErrorCode != CodeInternal {
		t.Fatalf("finish = %+v, want failed/%s", fin, CodeInternal)
	}
	if len(rs.reverts) != 0 {
		t.Error("Revert must not be called without a target")
	}
}

func TestRunTurn_Revert_ShutdownDuringRevertLeavesTheTurnUnfinished(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
	ctx, cancel := context.WithCancel(context.Background())
	rs.onRevert = cancel
	rs.revertErr = context.Canceled
	if err := h.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the context error", err)
	}
	if h.st.finish != nil {
		t.Error("a cancelled revert must not finish the turn; another replica resumes it")
	}
}

func TestRunTurn_Revert_LostLeaseStopsBeforeTheRevert(t *testing.T) {
	h, rs := newRevertHarness(t, &store.RevertTarget{TurnSeq: 1, SHA: "sha1"})
	other := "w2"
	h.st.turn.LeaseOwner = &other
	if err := h.run(context.Background()); !errors.Is(err, store.ErrLeaseLost) {
		t.Fatalf("error = %v, want ErrLeaseLost", err)
	}
	if len(rs.reverts) != 0 {
		t.Error("a replica that lost the lease must not touch the workspace")
	}
}

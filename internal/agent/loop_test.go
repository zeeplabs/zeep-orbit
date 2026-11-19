package agent

// loop_test.go: RunTurn against a fake provider, fake sandbox and in-memory
// store. Cases come from the "Done when" list of ai-frontend-builder T18 and
// design sections 1 and 2.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

func TestRunTurn_HappyPathEndsWithCommit(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("c1", "src/App.tsx"))},
		scriptStep{resp: toolResp(buildCall("c2", "build"))},
		scriptStep{resp: textResp("Your tasks page is ready.")},
	)
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	if fin.CommitSHA != "sha1" {
		t.Errorf("commit sha = %q, want sha1", fin.CommitSHA)
	}
	if got := h.prov.calls(); got != 3 {
		t.Errorf("provider calls = %d, want 3", got)
	}
	if len(h.st.usage) != 3 {
		t.Errorf("usage records = %d, want one per call (3)", len(h.st.usage))
	}
	if u := h.st.usage[0]; u.UserID != "user-1" || u.SessionID != "sess-1" || u.TurnID != "turn-1" || u.Feature != store.FeatureBuilder {
		t.Errorf("usage record not attributed to the session owner/turn: %+v", u)
	}
	ev := h.st.eventTypes()
	for _, want := range []string{EventTurnStarted, EventToolCall, EventFileChanged, EventBuildStarted, EventBuildSucceeded, EventAssistantMessage, EventTurnFinished} {
		if indexOf(ev, want) < 0 {
			t.Errorf("missing event %q in %v", want, ev)
		}
	}
	if ev[len(ev)-1] != EventTurnFinished {
		t.Errorf("last event = %q, want turn_finished", ev[len(ev)-1])
	}
	if len(h.st.snaps) < 2 {
		t.Error("no snapshot saved for the turn")
	}
}

// Some OpenAI-compatible servers omit tool call ids, and the adapter then
// numbers them per response ("call_0" every time). The builder remembers
// op_ids for the whole session, so a repeated id would make the second write
// a replay of the first: the file would silently keep its old content.
func TestRunTurn_RepeatedToolCallIDsGetDistinctOpIDs(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("call_0", "src/a.ts"))},
		scriptStep{resp: toolResp(writeCall("call_0", "src/b.ts"))},
		scriptStep{resp: toolResp(writeCall("call_0", "src/c.ts"), writeCall("call_0", "src/d.ts"))},
		scriptStep{resp: textResp("done")},
	)
	h.mustRun()

	ops := h.sb.opIDs()
	seen := map[string]bool{}
	for _, id := range ops {
		if seen[id] {
			t.Fatalf("op id %q reused in %v: the builder would replay instead of executing", id, ops)
		}
		seen[id] = true
	}
	if len(ops) != 4 {
		t.Fatalf("builder ops = %v, want 4", ops)
	}
	// Every result must still pair with exactly one call.
	results := map[string]int{}
	for _, m := range h.st.messagesOf(store.RoleTool) {
		results[m.ToolCallID]++
	}
	if len(results) != 4 {
		t.Errorf("tool result ids = %v, want 4 distinct ids", results)
	}
}

func TestRunTurn_ParallelToolCallsAllExecutedInOrder(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("a", "src/a.ts"), writeCall("b", "src/b.ts"), listCall("c"))},
		scriptStep{resp: textResp("done")},
	)
	h.mustRun()

	if got := h.sb.opIDs(); strings.Join(got, ",") != "turn-1:a,turn-1:b," {
		t.Errorf("op ids in builder order = %v, want [turn-1:a turn-1:b \"\"] (turn-scoped; read-only ops carry no op_id)", got)
	}
	var ids []string
	for _, m := range h.st.messagesOf(store.RoleTool) {
		ids = append(ids, m.ToolCallID)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Errorf("persisted tool results = %v, want a,b,c in call order", ids)
	}
	// The next model call saw all three results.
	req := h.prov.reqs[1]
	n := 0
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			n++
		}
	}
	if n != 3 {
		t.Errorf("second request carries %d tool results, want 3", n)
	}
	if h.sb.commits != 1 {
		t.Errorf("commits = %d, want 1 (one checkpoint for the batch, nothing left for the final commit)", h.sb.commits)
	}
}

func TestRunTurn_FixIterationLimitEndsLimitReachedWithCommit(t *testing.T) {
	failing := sandbox.BuildResult{ExitCode: 1, OutputTail: "error TS2304: Cannot find name 'x'"}
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("w", "src/a.ts"))},
		scriptStep{resp: toolResp(buildCall("b1", "typecheck"))},
		scriptStep{resp: toolResp(buildCall("b2", "typecheck"))},
		scriptStep{resp: toolResp(buildCall("b3", "typecheck"))},
	)
	h.sb.builds = []sandbox.BuildResult{failing}
	h.deps.Limits = Limits{MaxFixIterations: 2}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if got := h.prov.calls(); got != 3 {
		t.Errorf("provider calls = %d, want 3 (write, build fail, build fail, then stop)", got)
	}
	lim := h.st.eventsOf(EventLimitReached)
	if len(lim) != 1 || !strings.Contains(string(lim[0].Payload), LimitFixes) {
		t.Errorf("limit_reached event = %v, want limit %q", lim, LimitFixes)
	}
	if fin.CommitSHA == "" {
		t.Error("limit_reached must still commit the partial work")
	}
	if len(h.st.eventsOf(EventBuildFailed)) != 2 {
		t.Errorf("build_failed events = %d, want 2", len(h.st.eventsOf(EventBuildFailed)))
	}
}

func TestRunTurn_StepLimitEndsLimitReachedWithCommit(t *testing.T) {
	var script []scriptStep
	for i := 0; i < 10; i++ {
		script = append(script, scriptStep{resp: toolResp(writeCall(fmt.Sprintf("w%d", i), "src/a.ts"))})
	}
	h := newHarness(t, script...)
	h.deps.Limits = Limits{MaxSteps: 3}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if got := h.prov.calls(); got != 4 {
		t.Errorf("provider calls = %d, want MaxSteps (3) + 1 closing call without tools", got)
	}
	if fin.CommitSHA == "" {
		t.Error("step limit must commit the partial work")
	}
	if lim := h.st.eventsOf(EventLimitReached); len(lim) != 1 || !strings.Contains(string(lim[0].Payload), LimitSteps) {
		t.Errorf("limit_reached event = %v, want limit %q", lim, LimitSteps)
	}
}

func TestRunTurn_TurnDurationLimit(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := newHarness(t, scriptStep{resp: toolResp(writeCall("w", "src/a.ts"))})
	h.deps.Limits = Limits{MaxTurnDuration: time.Minute}
	// RunTurn reads the clock once at start, then once per loop iteration.
	h.deps.Clock = scriptedClock(t0, t0, t0.Add(2*time.Minute))
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if lim := h.st.eventsOf(EventLimitReached); len(lim) != 1 || !strings.Contains(string(lim[0].Payload), LimitDuration) {
		t.Errorf("limit_reached event = %v, want limit %q", lim, LimitDuration)
	}
}

// A resumed attempt (the previous worker died) does not get a fresh
// MaxTurnDuration: the duration runs from the first claim of the run.
func TestRunTurn_ResumedTurnKeepsTheRunStart(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	h := newHarness(t, scriptStep{resp: textResp("done")})
	started := t0.Add(-11 * time.Minute)
	h.st.turn.RunStartedAt = &started
	h.st.turn.Attempts = 2
	h.deps.Limits = Limits{MaxTurnDuration: 10 * time.Minute}
	h.deps.Clock = func() time.Time { return t0 }
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if h.prov.calls() != 0 {
		t.Errorf("provider calls = %d, want none past the turn's duration", h.prov.calls())
	}
}

// A turn claimed more than MaxTurnAttempts times keeps crashing whatever
// replica runs it: it fails instead of looping, and frees the session.
func TestRunTurn_TooManyAttemptsFailsWithoutACall(t *testing.T) {
	h := newHarness(t, scriptStep{resp: textResp("done")})
	h.st.turn.Attempts = DefaultMaxTurnAttempts + 1
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeInternal {
		t.Fatalf("finish = %+v, want failed/%s", fin, CodeInternal)
	}
	if h.prov.calls() != 0 {
		t.Errorf("provider calls = %d, want none", h.prov.calls())
	}
}

// At the cap the turn still runs: the last allowed attempt.
func TestRunTurn_LastAllowedAttemptRuns(t *testing.T) {
	h := newHarness(t, scriptStep{resp: textResp("done")})
	h.st.turn.Attempts = DefaultMaxTurnAttempts
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
}

func TestRunTurn_BudgetExceededBeforeCallMakesNoProviderCall(t *testing.T) {
	h := newHarness(t)
	h.bud.fn = func(int) error { return fmt.Errorf("%w: session token cap reached", store.ErrBudgetExceeded) }
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnBudgetExceeded {
		t.Fatalf("status = %q, want budget_exceeded", fin.Status)
	}
	if got := h.prov.calls(); got != 0 {
		t.Errorf("provider was called %d times with the budget exhausted", got)
	}
	if len(h.st.eventsOf(EventBudgetExceeded)) != 1 {
		t.Errorf("events = %v, want one budget_exceeded", h.st.eventTypes())
	}
}

func TestRunTurn_BudgetExceededMidTurnKeepsPartialWork(t *testing.T) {
	h := newHarness(t, scriptStep{resp: toolResp(writeCall("w", "src/a.ts"))})
	h.bud.fn = func(n int) error {
		if n >= 2 {
			return store.ErrMonthlyCapReached
		}
		return nil
	}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnBudgetExceeded {
		t.Fatalf("status = %q, want budget_exceeded", fin.Status)
	}
	if h.prov.calls() != 1 {
		t.Errorf("provider calls = %d, want 1 (the second was blocked)", h.prov.calls())
	}
	if fin.CommitSHA == "" {
		t.Error("budget_exceeded must keep (commit) the work done so far")
	}
}

// The budget_exceeded event says which cap ended the turn.
func TestRunTurn_BudgetExceededCarriesTheReason(t *testing.T) {
	h := newHarness(t)
	h.bud.fn = func(int) error { return store.ErrSessionCostCapReached }
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnBudgetExceeded {
		t.Fatalf("status = %q, want budget_exceeded", fin.Status)
	}
	ev := h.st.eventsOf(EventBudgetExceeded)
	if len(ev) != 1 || string(ev[0].Payload) != `{"reason":"session_cost"}` {
		t.Errorf("budget_exceeded events = %v, want one with reason session_cost", ev)
	}
}

// A cap close to its limit is reported on every budget check of the turn;
// the loop asks the store once per cap, and the event is written once.
func TestRunTurn_BudgetWarningOncePerCap(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("w1", "src/a.ts"))},
		scriptStep{resp: toolResp(writeCall("w2", "src/b.ts"))},
		scriptStep{resp: textResp("done")},
	)
	h.bud.warn = func(n int) []store.BudgetWarning {
		ws := []store.BudgetWarning{{Cap: store.BudgetCapSessionTokens, Used: 800 + int64(n), Limit: 1000}}
		if n >= 2 {
			ws = append(ws, store.BudgetWarning{Cap: store.BudgetCapMonthCost, Used: 90, Limit: 100})
		}
		return ws
	}
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed (a warning never stops the turn)", fin.Status)
	}
	ev := h.st.eventsOf(EventBudgetWarning)
	if len(ev) != 2 {
		t.Fatalf("budget_warning events = %v, want 2 (one per cap)", ev)
	}
	if string(ev[0].Payload) != `{"cap":"session_tokens","used":801,"limit":1000}` {
		t.Errorf("first warning = %s, want the numbers of the first check", ev[0].Payload)
	}
	if h.st.warnAsks != 2 {
		t.Errorf("store asked %d times, want 2 (once per cap in the turn)", h.st.warnAsks)
	}
}

// A session warned in an earlier turn is not warned again (resume or new
// turn): the store refuses the duplicate and the turn goes on.
func TestRunTurn_BudgetWarningNotRepeatedInLaterTurn(t *testing.T) {
	h := newHarness(t, scriptStep{resp: textResp("done")})
	h.st.warnedBefore = map[string]bool{store.BudgetCapSessionTokens: true}
	h.bud.warn = func(int) []store.BudgetWarning {
		return []store.BudgetWarning{{Cap: store.BudgetCapSessionTokens, Used: 900, Limit: 1000}}
	}
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	if ev := h.st.eventsOf(EventBudgetWarning); len(ev) != 0 {
		t.Errorf("budget_warning events = %v, want none", ev)
	}
}

func TestRunTurn_ProposalPausesWithoutCommit(t *testing.T) {
	h := newHarness(t, scriptStep{resp: toolResp(
		writeCall("w", "src/a.ts"),
		proposeCall("p1", `{"op":"add_column"}`),
		writeCall("w2", "src/b.ts"),
	)})
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("status = %q, want awaiting_confirmation", fin.Status)
	}
	if h.sb.commits != 0 {
		t.Errorf("builder commits = %d, a proposal pause must not commit", h.sb.commits)
	}
	var p struct {
		ToolCallID string          `json:"tool_call_id"`
		Name       string          `json:"name"`
		Arguments  json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(fin.PendingProposal, &p); err != nil || p.ToolCallID != "p1" || p.Name != "propose_backend_change" {
		t.Errorf("pending proposal = %s (%v)", fin.PendingProposal, err)
	}
	if len(h.st.eventsOf(EventProposal)) != 1 {
		t.Errorf("events = %v, want a proposal event", h.st.eventTypes())
	}
	if len(h.st.eventsOf(EventTurnFinished)) != 0 {
		t.Error("a paused turn must not emit turn_finished")
	}
	// Calls before the proposal ran; the proposal has no result yet and the
	// call after it has not run (it runs on resume).
	var ids []string
	for _, m := range h.st.messagesOf(store.RoleTool) {
		ids = append(ids, m.ToolCallID)
	}
	if strings.Join(ids, ",") != "w" {
		t.Errorf("tool results = %v, want only [w]", ids)
	}
}

func TestRunTurn_MalformedProposalIsAToolErrorNotAPause(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(proposeCall("p1", `{"bad":true}`))},
		scriptStep{resp: textResp("ok, skipping the backend change")},
	)
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	tm := h.st.messagesOf(store.RoleTool)
	if len(tm) != 1 || !strings.Contains(tm[0].Content, "malformed proposal") {
		t.Errorf("tool results = %+v, want the validation error", tm)
	}
}

func TestRunTurn_ResumeAfterCrashResendsSameOpID(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("call-7", "src/a.ts"))},
		scriptStep{resp: textResp("done")},
	)
	// Crash: the replica dies (context cancelled) after the builder applied the
	// write but before the result was persisted.
	ctx, cancel := context.WithCancel(context.Background())
	var crashed bool
	h.sb.onOp = func(string) {
		// The builder applies the write (the call below returns its result),
		// then the worker is killed before the result can be persisted.
		if !crashed {
			crashed = true
			cancel()
		}
	}
	if err := h.run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("first run error = %v, want context.Canceled", err)
	}
	if h.st.finish != nil {
		t.Fatal("a crashed run must not finish the turn")
	}
	if got := len(h.st.messagesOf(store.RoleTool)); got != 0 {
		t.Fatalf("tool results after crash = %d, want 0", got)
	}

	// Another replica resumes the same turn.
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	ids := h.sb.opIDs()
	if len(ids) != 2 || ids[0] != "turn-1:call-7" || ids[1] != "turn-1:call-7" {
		t.Errorf("builder saw op ids %v, want turn-1:call-7 twice (same op_id on resume)", ids)
	}
	if h.prov.calls() != 2 {
		t.Errorf("provider calls = %d, want 2: the persisted tool call must not be re-requested from the model", h.prov.calls())
	}
	tm := h.st.messagesOf(store.RoleTool)
	if len(tm) != 1 || tm[0].ToolCallID != "call-7" {
		t.Errorf("tool results = %+v, want one for call-7", tm)
	}
	if started := h.st.eventsOf(EventTurnStarted); len(started) != 2 || !strings.Contains(string(started[1].Payload), `"resumed":true`) {
		t.Errorf("turn_started events = %v, want a resumed second one", started)
	}
}

func TestRunTurn_ProviderErrorFailsWithGenericCode(t *testing.T) {
	h := newHarness(t, scriptStep{err: errProviderSecret()})
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeProviderUnavailable {
		t.Fatalf("finish = %+v, want failed/%s", fin, CodeProviderUnavailable)
	}
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	for _, e := range h.st.events {
		if strings.Contains(string(e.Payload), "secret") {
			t.Errorf("event %s leaks provider error text: %s", e.Type, e.Payload)
		}
	}
	errEv := 0
	for _, e := range h.st.events {
		if e.Type == EventError {
			errEv++
			if string(e.Payload) != `{"code":"provider_unavailable"}` {
				t.Errorf("error event payload = %s", e.Payload)
			}
		}
	}
	if errEv != 1 {
		t.Errorf("error events = %d, want 1", errEv)
	}
}

func TestRunTurn_ToolGoErrorIsNotShownToTheModel(t *testing.T) {
	h := newHarness(t, scriptStep{resp: toolResp(writeCall("w", "src/a.ts"))})
	h.sb.opErr = errors.New("dial tcp 10.0.0.7:8080: connection refused")
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeToolFailed {
		t.Fatalf("finish = %+v, want failed/%s", fin, CodeToolFailed)
	}
	if got := len(h.st.messagesOf(store.RoleTool)); got != 0 {
		t.Errorf("a tool Go error produced %d tool messages; it must not reach the model", got)
	}
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	for _, e := range h.st.events {
		if strings.Contains(string(e.Payload), "10.0.0.7") {
			t.Errorf("event %s leaks the internal error: %s", e.Type, e.Payload)
		}
	}
}

func TestRunTurn_WorkspaceLostFailsWithoutCommit(t *testing.T) {
	h := newHarness(t, scriptStep{resp: toolResp(writeCall("w", "src/a.ts"))})
	h.sb.opErr = fmt.Errorf("%w: no snapshots to restore from", sandbox.ErrWorkspaceLost)
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeWorkspaceLost {
		t.Fatalf("finish = %+v, want failed/%s", fin, CodeWorkspaceLost)
	}
	if h.sb.commits != 0 {
		t.Error("a lost workspace has nothing to commit")
	}
}

func TestRunTurn_CommitsAndSnapshotsAfterEachWritingBatch(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("w1", "src/a.ts"))},
		scriptStep{resp: toolResp(listCall("l1"))}, // read-only batch: no checkpoint
		scriptStep{resp: toolResp(writeCall("w2", "src/b.ts"))},
		scriptStep{resp: textResp("done")},
	)
	h.mustRun()

	if h.sb.commits != 2 {
		t.Fatalf("builder commits = %d, want 2 (one per writing batch)", h.sb.commits)
	}
	fin := h.finished()
	if fin.CommitSHA != "sha2" {
		t.Errorf("turn commit = %q, want sha2 (the head after the last batch)", fin.CommitSHA)
	}
	// Each batch is its own (turn, part) row; the restore chain keeps them all.
	snaps, _ := h.st.ListSnapshots(context.Background(), "sess-1")
	var rows []string
	for _, sn := range snaps {
		rows = append(rows, fmt.Sprintf("%d/%d/%s", sn.TurnSeq, sn.Part, sn.CommitSHA))
	}
	if strings.Join(rows, ",") != "0/0/sha0,1/0/sha1,1/1/sha2" {
		t.Errorf("snapshot rows (turn/part/sha) = %v, want 0/0/sha0,1/0/sha1,1/1/sha2", rows)
	}
	var shas []string
	for _, b := range BundlesFromSnapshots(snaps) {
		shas = append(shas, b.SHA)
	}
	if strings.Join(shas, ",") != "sha0,sha1,sha2" {
		t.Errorf("restore chain = %v, want sha0,sha1,sha2 in order", shas)
	}
}

func TestRunTurn_LeaseLostStopsWithoutFurtherWrites(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("w1", "src/a.ts"))},
		scriptStep{resp: textResp("done")},
	)
	h.st.afterUsage = func(n int) {
		if n == 1 {
			// Another replica took the turn over.
			other := "w2"
			h.st.mu.Lock()
			h.st.turn.LeaseOwner = &other
			h.st.mu.Unlock()
		}
	}
	err := h.run(context.Background())
	if !errors.Is(err, store.ErrLeaseLost) {
		t.Fatalf("error = %v, want ErrLeaseLost", err)
	}
	if h.st.finish != nil {
		t.Error("a replica that lost the lease must not finish the turn")
	}
	if len(h.sb.opIDs()) != 0 {
		t.Error("no tool may run after the lease was lost")
	}
	if len(h.st.messagesOf(store.RoleAssistant)) != 0 {
		t.Error("no message may be written after the lease was lost")
	}
}

func TestRunTurn_ResumeAfterProposalConfirmationContinues(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(proposeCall("p1", `{"op":"add_column"}`))},
		scriptStep{resp: textResp("column added, building the page")},
	)
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("status = %q, want awaiting_confirmation", fin.Status)
	}

	// T27 (not part of this task) appends the decision as the tool result and
	// puts the turn back in the queue; a worker claims it again.
	h.st.mu.Lock()
	h.st.addLocked("turn-1", store.RoleTool, "applied by the user", nil, "p1")
	owner := "w1"
	h.st.turn.Status, h.st.turn.LeaseOwner, h.st.finish = store.TurnRunning, &owner, nil
	h.st.mu.Unlock()
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	last := h.prov.reqs[len(h.prov.reqs)-1]
	var sawResult bool
	for _, m := range last.Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == "p1" && m.Content == "applied by the user" {
			sawResult = true
		}
	}
	if !sawResult {
		t.Error("the model did not receive the confirmation result")
	}
	if h.sb.commits != 0 {
		t.Errorf("commits = %d, nothing was written", h.sb.commits)
	}
}

func TestToProviderMessages_RepairsUnansweredCallsOfEarlierTurns(t *testing.T) {
	calls, _ := json.Marshal([]provider.ToolCall{
		{ID: "x1", Name: "write_file", Arguments: json.RawMessage(`{}`)},
		{ID: "x2", Name: "write_file", Arguments: json.RawMessage(`{}`)},
	})
	msgs := []store.Message{
		{ID: 1, TurnID: "old", Role: store.RoleUser, Content: "hi"},
		{ID: 2, TurnID: "old", Role: store.RoleAssistant, ToolCalls: calls},
		{ID: 3, TurnID: "old", Role: store.RoleTool, ToolCallID: "x1", Content: "ok"},
		{ID: 4, TurnID: "cur", Role: store.RoleUser, Content: "again"},
	}
	got := toProviderMessages(msgs, "cur")
	answered := map[string]bool{}
	for _, m := range got {
		if m.Role == provider.RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	if !answered["x1"] || !answered["x2"] {
		t.Errorf("tool calls without results after repair: %v; a provider would reject the history", answered)
	}
}

// SnapshotProvider maps rows to sandbox bundles one to one, in the order the
// store returns them, with no framing.
func TestSnapshotProvider_MapsRowsInOrder(t *testing.T) {
	st := newFakeStore()
	l := st.lease()
	for _, sha := range []string{"sha1", "sha2"} {
		if err := st.SaveSnapshot(context.Background(), l, sha, []byte("inc-"+sha)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := SnapshotProvider(st)(context.Background(), "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("bundles = %d, want 3", len(got))
	}
	want := []sandbox.Bundle{
		{Seq: 0, SHA: "sha0", Data: []byte("bundle-0")},
		{Seq: 1, SHA: "sha1", Data: []byte("inc-sha1")},
		{Seq: 1, SHA: "sha2", Data: []byte("inc-sha2")},
	}
	for i := range want {
		if got[i].Seq != want[i].Seq || got[i].SHA != want[i].SHA || string(got[i].Data) != string(want[i].Data) {
			t.Errorf("bundle %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRunTurn_UnknownTurnKindIsUnsupported(t *testing.T) {
	h := newHarness(t)
	h.st.turn.Kind = "bogus"
	h.mustRun()
	fin := h.finished()
	if fin.Status != store.TurnFailed || fin.ErrorCode != CodeUnsupportedTurn {
		t.Fatalf("finish = %+v, want failed/%s", fin, CodeUnsupportedTurn)
	}
	if h.prov.calls() != 0 {
		t.Error("the provider must not be called for an unknown turn kind")
	}
}

// The loop hands the store the token classes as the provider reported them,
// with the purpose of the call; pricing and the caps are the store's job.
func TestRunTurn_RecordsTokenClassesAndPurpose(t *testing.T) {
	resp := textResp("done")
	resp.Usage = provider.Usage{InputTokens: 100, CachedInputTokens: 900, CacheWriteTokens: 50, OutputTokens: 7, Reported: true}
	h := newHarness(t, scriptStep{resp: resp})
	h.mustRun()

	if len(h.st.usage) != 1 {
		t.Fatalf("usage records = %d, want 1", len(h.st.usage))
	}
	got := h.st.usage[0]
	if got.InputTokens != 100 || got.CachedInputTokens != 900 || got.CacheWriteTokens != 50 || got.OutputTokens != 7 {
		t.Errorf("recorded classes = %d/%d/%d/%d, want 100/900/50/7",
			got.InputTokens, got.CachedInputTokens, got.CacheWriteTokens, got.OutputTokens)
	}
	if got.Purpose != store.PurposeChat || !got.Reported {
		t.Errorf("purpose/reported = %q/%v, want chat/true", got.Purpose, got.Reported)
	}
}

// A provider that sends no usage (an openai_compatible server may omit it)
// must not make the call free: the caps would never fire. The loop records a
// conservative estimate, marked as not reported, so the ledger can tell it
// apart from real counts.
func TestRunTurn_UnreportedUsageIsRecordedAsAnEstimate(t *testing.T) {
	resp := textResp("done")
	resp.Usage = provider.Usage{}
	h := newHarness(t, scriptStep{resp: resp})
	h.deps.Limits = Limits{MaxOutputTokens: 2048}
	h.mustRun()

	if len(h.st.usage) != 1 {
		t.Fatalf("usage records = %d, want 1", len(h.st.usage))
	}
	got := h.st.usage[0]
	if got.Reported {
		t.Error("an estimate must be recorded with Reported=false")
	}
	req := h.prov.reqs[0]
	if minIn := len(req.System) / 4; got.InputTokens < minIn {
		t.Errorf("input estimate = %d, want at least the system prompt's chars/4 (%d)", got.InputTokens, minIn)
	}
	if got.OutputTokens != 2048 {
		t.Errorf("output estimate = %d, want the max_tokens of the call (2048)", got.OutputTokens)
	}
}

// Without a max_tokens the output estimate is the reply's own size.
func TestRunTurn_UnreportedUsageWithoutMaxTokensEstimatesTheReply(t *testing.T) {
	resp := textResp(strings.Repeat("x", 400))
	resp.Usage = provider.Usage{}
	h := newHarness(t, scriptStep{resp: resp})
	h.mustRun()

	if got := h.st.usage[0]; got.OutputTokens != 100 || got.InputTokens == 0 {
		t.Errorf("estimate = %d in / %d out, want >0 in and 100 out (400 chars / 4)", got.InputTokens, got.OutputTokens)
	}
}

// The summarization call is estimated the same way.
func TestCompaction_UnreportedUsageIsRecordedAsAnEstimate(t *testing.T) {
	sum := textResp("SUMMARY: tasks page.")
	sum.Usage = provider.Usage{}
	h := newHarness(t, scriptStep{resp: sum}, scriptStep{resp: textResp("done")})
	seedOldTurns(h, 4)
	h.deps.Limits = Limits{ContextWindowTokens: 8000, KeepRecentTurns: 2}
	h.mustRun()

	var found bool
	for _, u := range h.st.usage {
		if u.Purpose != store.PurposeCompaction {
			continue
		}
		found = true
		if u.Reported || u.InputTokens == 0 || u.OutputTokens != summaryMaxOutputTokens {
			t.Errorf("compaction record = %+v, want an unreported estimate with output = %d", u, summaryMaxOutputTokens)
		}
	}
	if !found {
		t.Fatal("no compaction usage recorded")
	}
}

// Provider caching is a prefix match: a step only reads what the previous step
// wrote if it starts with the same tools, system prompt and history. The loop
// must never rewrite them between steps of a turn.
func TestRunTurn_StepsShareAByteIdenticalPrefix(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("c1", "src/App.tsx"))},
		scriptStep{resp: toolResp(buildCall("c2", "build"))},
		scriptStep{resp: textResp("Your tasks page is ready.")},
	)
	h.mustRun()

	if h.prov.calls() != 3 {
		t.Fatalf("provider calls = %d, want 3", h.prov.calls())
	}
	for i := 1; i < len(h.prov.reqs); i++ {
		prev, cur := h.prov.reqs[i-1], h.prov.reqs[i]
		if cur.System != prev.System {
			t.Errorf("step %d: the system prompt changed between steps", i+1)
		}
		if !reflect.DeepEqual(cur.Tools, prev.Tools) {
			t.Errorf("step %d: the tool list changed between steps", i+1)
		}
		if len(cur.Messages) <= len(prev.Messages) || !reflect.DeepEqual(cur.Messages[:len(prev.Messages)], prev.Messages) {
			t.Errorf("step %d: the history is not the previous step's history plus new messages", i+1)
		}
	}
}

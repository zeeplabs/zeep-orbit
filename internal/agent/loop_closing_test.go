package agent

// loop_closing_test.go: the closing call after the step limit, and the answer
// to tool calls cut off by the output limit.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

func writeSteps(n int) []scriptStep {
	var script []scriptStep
	for i := 0; i < n; i++ {
		script = append(script, scriptStep{resp: toolResp(writeCall(fmt.Sprintf("w%d", i), "src/a.ts"))})
	}
	return script
}

func TestRunTurn_StepLimitMakesOneClosingCallWithoutTools(t *testing.T) {
	script := append(writeSteps(3), scriptStep{resp: textResp("Done: the form. Missing: the table.")})
	h := newHarness(t, script...)
	h.deps.Limits = Limits{MaxSteps: 3}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached (the closing call must not turn it into completed)", fin.Status)
	}
	if fin.CommitSHA == "" {
		t.Error("the partial work must still be committed")
	}
	if lim := h.st.eventsOf(EventLimitReached); len(lim) != 1 || !strings.Contains(string(lim[0].Payload), LimitSteps) {
		t.Errorf("limit_reached events = %v, want one with %q", lim, LimitSteps)
	}
	if got := h.prov.calls(); got != 4 {
		t.Fatalf("provider calls = %d, want MaxSteps (3) + 1 closing call", got)
	}

	for i, req := range h.prov.reqs[:3] {
		if req.ToolChoice != "" || !req.CacheHint {
			t.Errorf("work call %d: ToolChoice=%q CacheHint=%v, want the normal request", i, req.ToolChoice, req.CacheHint)
		}
	}
	last := h.prov.reqs[3]
	if last.ToolChoice != provider.ToolChoiceNone {
		t.Errorf("closing call ToolChoice = %q, want none", last.ToolChoice)
	}
	if len(last.Tools) == 0 {
		t.Error("closing call must keep the tool definitions: the history holds tool calls")
	}
	if last.CacheHint {
		t.Error("closing call must not ask for a cache write that nothing will read")
	}
	if n := len(last.Messages); n == 0 || last.Messages[n-1].Role != provider.RoleUser || last.Messages[n-1].Content != stepLimitNotice {
		t.Errorf("closing call must end with the closing notice as a user message, got %+v", last.Messages)
	}

	// The summary is persisted; the notice is not.
	var summary bool
	for _, m := range h.st.messagesOf(store.RoleAssistant) {
		if strings.Contains(m.Content, "Missing: the table.") {
			summary = true
		}
	}
	if !summary {
		t.Error("the handover reply was not persisted")
	}
	for _, role := range []string{store.RoleUser, store.RoleAssistant, store.RoleTool} {
		for _, m := range h.st.messagesOf(role) {
			if m.Content == stepLimitNotice {
				t.Errorf("the closing notice must not be persisted (%s message)", role)
			}
		}
	}
	if len(h.st.usage) != 4 {
		t.Errorf("usage records = %d, want 4: the closing call is billed like any other", len(h.st.usage))
	}
}

// A server that ignores tool_choice none may still answer with tool calls.
// They have no result to pair with, so they are neither persisted nor run.
func TestRunTurn_ClosingCallToolCallsAreDropped(t *testing.T) {
	script := append(writeSteps(2), scriptStep{resp: toolResp(writeCall("late", "src/late.ts"))})
	h := newHarness(t, script...)
	h.deps.Limits = Limits{MaxSteps: 2}
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	for _, op := range h.sb.ops {
		if op.path == "src/late.ts" {
			t.Fatalf("a tool call from the closing reply was executed: %+v", h.sb.ops)
		}
	}
	for _, m := range h.st.messagesOf(store.RoleAssistant) {
		if m.TurnID == "turn-1" && strings.Contains(string(m.ToolCalls), "late") {
			t.Errorf("a dropped tool call was persisted: %s", m.ToolCalls)
		}
	}
}

// The closing call is a courtesy: if it fails, the turn is still limit_reached
// and keeps its commit; it is not turned into a failed turn.
func TestRunTurn_ClosingCallFailureKeepsLimitReached(t *testing.T) {
	script := append(writeSteps(2), scriptStep{err: &provider.ErrProvider{Status: 500}})
	h := newHarness(t, script...)
	h.deps.Limits = Limits{MaxSteps: 2}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if fin.CommitSHA == "" {
		t.Error("the partial work must still be committed")
	}
	if got := h.prov.calls(); got != 3 {
		t.Errorf("provider calls = %d, want 3 (the failed closing call is not retried by the loop)", got)
	}
}

// A turn that ends by itself on its last allowed step is complete: the closing
// call exists only for a turn the limit stopped.
func TestRunTurn_FinishingOnTheLastAllowedStepIsNotALimit(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("w1", "src/a.ts"))},
		scriptStep{resp: textResp("All done.")},
	)
	h.deps.Limits = Limits{MaxSteps: 2}
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	if got := h.prov.calls(); got != 2 {
		t.Errorf("provider calls = %d, want 2", got)
	}
	for i, req := range h.prov.reqs {
		if req.ToolChoice != "" {
			t.Errorf("call %d ToolChoice = %q, want none of them restricted", i, req.ToolChoice)
		}
	}
}

func cutResp(calls ...provider.ToolCall) provider.ChatResponse {
	resp := toolResp(calls...)
	resp.FinishReason = provider.FinishLength
	return resp
}

// An unterminated object is what adapters hand over for a call cut mid-JSON:
// the raw text kept as a JSON string.
func cutWrite(id string) provider.ToolCall {
	raw, _ := json.Marshal(`{"path":"src/big.ts","content":"export const x = [`)
	return provider.ToolCall{ID: id, Name: tools.ToolWriteFile, Arguments: raw}
}

func TestRunTurn_CutOffCallGetsAnExplanationNotAGenericError(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: cutResp(cutWrite("c1"))},
		scriptStep{resp: textResp("I will write it in pieces.")},
	)
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	if len(h.sb.ops) != 0 {
		t.Errorf("a cut-off call must not be dispatched: %+v", h.sb.ops)
	}
	res := h.st.messagesOf(store.RoleTool)
	if len(res) != 1 {
		t.Fatalf("tool results = %d, want 1", len(res))
	}
	for _, want := range []string{"output length limit", tools.ToolWriteFile, tools.ToolEditFile} {
		if !strings.Contains(res[0].Content, want) {
			t.Errorf("tool result %q does not mention %q", res[0].Content, want)
		}
	}
	if strings.Contains(res[0].Content, "invalid arguments") {
		t.Errorf("the generic error hides the cause: %q", res[0].Content)
	}
}

// The provider may hand back a partial object that parses: the last call of a
// reply cut by the output limit is never trusted, and a complete call before
// it still runs.
func TestRunTurn_CutOffReplyTrustsOnlyTheCallsBeforeTheLast(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: cutResp(writeCall("ok", "src/ok.ts"), writeCall("last", "src/last.ts"))},
		scriptStep{resp: textResp("done")},
	)
	h.mustRun()

	var paths []string
	for _, op := range h.sb.ops {
		paths = append(paths, op.path)
	}
	if len(paths) != 1 || paths[0] != "src/ok.ts" {
		t.Errorf("dispatched paths = %v, want only src/ok.ts", paths)
	}
	res := h.st.messagesOf(store.RoleTool)
	if len(res) != 2 {
		t.Fatalf("tool results = %d, want one per call", len(res))
	}
	if strings.Contains(res[0].Content, "output length limit") || !strings.Contains(res[1].Content, "output length limit") {
		t.Errorf("results = %q / %q, want the cut explanation on the last call only", res[0].Content, res[1].Content)
	}
}

// Only a reply that really hit the limit is treated this way: a call with bad
// arguments from a normal reply keeps the tool's own error.
func TestRunTurn_BadArgumentsWithoutALengthCutKeepTheToolError(t *testing.T) {
	h := newHarness(t,
		scriptStep{resp: toolResp(cutWrite("c1"))}, // FinishToolCalls, not length
		scriptStep{resp: textResp("sorry")},
	)
	h.mustRun()

	res := h.st.messagesOf(store.RoleTool)
	if len(res) != 1 || strings.Contains(res[0].Content, "output length limit") {
		t.Fatalf("tool results = %+v, want the tool's own invalid-arguments error", res)
	}
}

// The fix limit stops the turn on a failing build: the user gets the same kind
// of handover, with a notice that explains the failure and does not promise a
// fix the model can no longer make in this turn.
func TestRunTurn_FixLimitMakesOneClosingCallWithoutTools(t *testing.T) {
	failing := sandbox.BuildResult{ExitCode: 1, OutputTail: "error TS2304: Cannot find name 'x'"}
	h := newHarness(t,
		scriptStep{resp: toolResp(writeCall("w", "src/a.ts"))},
		scriptStep{resp: toolResp(buildCall("b1", "typecheck"))},
		scriptStep{resp: toolResp(buildCall("b2", "typecheck"))},
		scriptStep{resp: textResp("The build still fails on TS2304 in src/a.ts.")},
	)
	h.sb.builds = []sandbox.BuildResult{failing}
	h.deps.Limits = Limits{MaxFixIterations: 2}
	h.mustRun()

	fin := h.finished()
	if fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if fin.CommitSHA == "" {
		t.Error("the partial work must still be committed")
	}
	if lim := h.st.eventsOf(EventLimitReached); len(lim) != 1 || !strings.Contains(string(lim[0].Payload), LimitFixes) {
		t.Errorf("limit_reached events = %v, want one with %q", lim, LimitFixes)
	}
	if got := h.prov.calls(); got != 4 {
		t.Fatalf("provider calls = %d, want 3 + 1 closing call", got)
	}
	last := h.prov.reqs[3]
	if last.ToolChoice != provider.ToolChoiceNone || len(last.Tools) == 0 || last.CacheHint {
		t.Errorf("closing call: ToolChoice=%q tools=%d CacheHint=%v, want none, tools kept, no cache write", last.ToolChoice, len(last.Tools), last.CacheHint)
	}
	if n := len(last.Messages); n == 0 || last.Messages[n-1].Content != fixLimitNotice {
		t.Errorf("closing call must end with the fix-limit notice, got %+v", last.Messages)
	}
	if fixLimitNotice == stepLimitNotice {
		t.Error("the fix-limit notice must differ from the step-limit one")
	}
	var summary bool
	for _, m := range h.st.messagesOf(store.RoleAssistant) {
		if strings.Contains(m.Content, "TS2304 in src/a.ts") {
			summary = true
		}
		if m.Content == fixLimitNotice {
			t.Error("the closing notice must not be persisted")
		}
	}
	if !summary {
		t.Error("the handover reply was not persisted")
	}
}

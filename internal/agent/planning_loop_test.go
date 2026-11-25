package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

const (
	testQuestions = `{"questions":[{"id":"sign_in","title":"Sign-in","question":"Who signs in?","options":[{"label":"Team"},{"label":"Anyone"}]}]}`
	testPlan      = `{"title":"Clinic CRM","summary":"A CRM for a clinic.","screens":[{"name":"Patients","purpose":"List patients"}],"look":{"direction":"calm and clinical"},"assumptions":["Email sign-in"],` +
		`"milestones":[{"title":"Patient list","deliverable":"See the list"},{"title":"Patient record","deliverable":"Open one"},{"title":"Notes","deliverable":"Add a note"}]}`
)

// newPlanHarness is newHarness with the planning tools registered and the turn
// in mode.
func newPlanHarness(t *testing.T, mode string, script ...scriptStep) *harness {
	t.Helper()
	h := newHarness(t, script...)
	for _, tool := range planning.Tools() {
		if err := h.reg.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name, err)
		}
	}
	h.st.turn.Mode = mode
	h.deps.PlanningEnabled = true
	return h
}

func askCall(id string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: planning.ToolAskUser, Arguments: json.RawMessage(testQuestions)}
}

func planCall(id string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: planning.ToolProposeBuildPlan, Arguments: json.RawMessage(testPlan)}
}

// resolve does what the store does when the user decides on a paused turn: it
// appends the tool result (with its metadata) and makes the turn runnable
// again. mode, when not empty, is the turn's new mode (a plan approval).
func (h *harness) resolve(callID, content, meta, mode string) {
	h.t.Helper()
	if fin := h.finished(); fin.Status != store.TurnAwaitingConfirmation {
		h.t.Fatalf("status = %q, want awaiting_confirmation (events %v)", fin.Status, h.st.eventTypes())
	}
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	var raw json.RawMessage
	if meta != "" {
		raw = json.RawMessage(meta)
	}
	h.st.addLocked(h.st.turn.ID, store.RoleTool, content, raw, callID)
	owner := "w1"
	h.st.turn.Status, h.st.turn.LeaseOwner, h.st.finish = store.TurnRunning, &owner, nil
	if mode != "" {
		h.st.turn.Mode = mode
	}
}

func offeredNames(specs []provider.ToolSpec) string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}

func (h *harness) pendingName() string {
	h.t.Helper()
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(h.finished().PendingProposal, &p); err != nil {
		h.t.Fatalf("pending proposal: %v", err)
	}
	return p.Name
}

func (h *harness) toolResult(callID string) store.Message {
	h.t.Helper()
	for _, m := range h.st.messagesOf(store.RoleTool) {
		if m.ToolCallID == callID {
			return m
		}
	}
	h.t.Fatalf("no tool result for %s", callID)
	return store.Message{}
}

// Spec success criterion 3: in a planning turn nothing can change a file, run
// a build or propose a backend change. The offer has none of those tools, and
// a call to any of them is refused before it reaches the registry or the
// builder.
func TestPlanMode_OffersOnlyReadAndPlanningToolsAndRefusesTheRest(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan)

	// One call to every registered tool the mode does not allow, plus a read.
	var calls []provider.ToolCall
	var refused []string
	for _, s := range h.reg.Specs() {
		kind, _ := h.reg.Kind(s.Name)
		if kind == tools.KindReadOnly || kind == tools.KindPlanning {
			continue
		}
		args := `{"path":"src/a.ts","content":"x","old_string":"a","new_string":"b","step":"build","op":"add_table"}`
		calls = append(calls, provider.ToolCall{ID: "c_" + s.Name, Name: s.Name, Arguments: json.RawMessage(args)})
		refused = append(refused, s.Name)
	}
	if len(refused) < 4 {
		t.Fatalf("expected the workspace tools and the backend proposal to be refused, got %v", refused)
	}
	calls = append(calls, listCall("read"))
	h.prov.script = []scriptStep{{resp: toolResp(calls...)}, {resp: textResp("I can only plan")}}
	h.mustRun()

	if got := offeredNames(h.prov.reqs[0].Tools); got != "list_dir,read_file,get_build_logs,ask_user,propose_build_plan" {
		t.Fatalf("offered tools = %s", got)
	}
	// The refused proposal did not pause the turn.
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	for _, name := range refused {
		m := h.toolResult("c_" + name)
		if !strings.Contains(m.Content, name+" is not available while planning") || !parseToolMeta(m).IsError {
			t.Errorf("%s: result %q (meta %s)", name, m.Content, m.ToolCalls)
		}
	}
	// Only the read reached the builder; nothing was built, written or committed.
	if len(h.sb.ops) != 1 || h.sb.ops[0].kind != sandbox.OpListDir {
		t.Errorf("builder ops = %+v, want one list_dir", h.sb.ops)
	}
	if h.sb.buildSeen != 0 || h.sb.commits != 0 {
		t.Errorf("builds = %d, commits = %d, want none", h.sb.buildSeen, h.sb.commits)
	}
	for _, typ := range []string{EventFileChanged, EventBuildStarted, EventBuildSucceeded, EventProposal} {
		if n := len(h.st.eventsOf(typ)); n != 0 {
			t.Errorf("%d %s events in a planning turn", n, typ)
		}
	}
	for _, u := range h.st.usage {
		if u.Purpose != store.PurposePlanning {
			t.Errorf("usage purpose = %q, want planning", u.Purpose)
		}
	}
}

func TestBuildMode_ToolOfferFollowsThePlanningSetting(t *testing.T) {
	h := newPlanHarness(t, store.ModeBuild, scriptStep{resp: textResp("done")})
	h.mustRun()
	if got := offeredNames(h.prov.reqs[0].Tools); got != "list_dir,read_file,write_file,edit_file,run_build,get_build_logs,propose_backend_change,ask_user,propose_build_plan" {
		t.Fatalf("build turn with planning on offers %s", got)
	}
	if h.st.usage[0].Purpose != store.PurposeChat {
		t.Errorf("usage purpose = %q, want chat", h.st.usage[0].Purpose)
	}

	// Planning off: the two tools are not offered and do not exist for the
	// model; calling one is an error result, not a pause.
	h = newPlanHarness(t, store.ModeBuild,
		scriptStep{resp: toolResp(askCall("a1"), planCall("p1"))},
		scriptStep{resp: textResp("ok")},
	)
	h.deps.PlanningEnabled = false
	h.mustRun()
	if got := offeredNames(h.prov.reqs[0].Tools); strings.Contains(got, "ask_user") || strings.Contains(got, "propose_build_plan") {
		t.Fatalf("planning tools offered while planning is off: %s", got)
	}
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
	for _, id := range []string{"a1", "p1"} {
		if m := h.toolResult(id); !strings.Contains(m.Content, "unknown tool") {
			t.Errorf("%s: %q", id, m.Content)
		}
	}
}

func TestPlanMode_QuestionsAndPlanPauseAndResumeIntoTheBuild(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan,
		scriptStep{resp: toolResp(askCall("a1"))},
		scriptStep{resp: toolResp(planCall("p1"))},
		scriptStep{resp: toolResp(writeCall("w1", "src/Patients.tsx"))},
		scriptStep{resp: textResp("The patient list is ready.")},
	)
	h.mustRun()
	if h.pendingName() != planning.ToolAskUser || len(h.st.eventsOf(EventProposal)) != 1 || len(h.st.eventsOf(EventTurnFinished)) != 0 {
		t.Fatalf("a question round must pause the turn: %s, events %v", h.pendingName(), h.st.eventTypes())
	}
	if h.sb.commits != 0 {
		t.Error("a pause must not commit")
	}

	// The user answers. The worker that claims the turn dies before doing
	// anything; the next one continues from the same state.
	h.resolve("a1", "The user answered:\n- sign_in (Sign-in): Team", `{"build_failed":false,"is_error":false}`, "")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.run(cancelled); err == nil {
		t.Fatal("a cancelled run must return an error")
	}
	if h.prov.calls() != 1 || h.st.finish != nil {
		t.Fatalf("the cancelled run must not call the model or finish the turn (calls %d)", h.prov.calls())
	}
	h.mustRun()
	if h.pendingName() != planning.ToolProposeBuildPlan {
		t.Fatalf("the plan must pause the turn, got %s", h.pendingName())
	}
	second := h.prov.reqs[1]
	if last := second.Messages[len(second.Messages)-1]; last.Role != provider.RoleTool || !strings.Contains(last.Content, "Team") {
		t.Errorf("the model did not receive the answers: %+v", last)
	}
	if strings.Contains(offeredNames(second.Tools), "write_file") {
		t.Error("still a planning turn: write_file must not be offered")
	}

	// Approval makes it a build turn: the same turn now builds.
	h.resolve("p1", "The user approved the plan.", `{"build_failed":false,"is_error":false}`, store.ModeBuild)
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnCompleted || fin.CommitSHA == "" {
		t.Fatalf("the approved turn must build and commit: %+v", fin)
	}
	if !strings.Contains(offeredNames(h.prov.reqs[2].Tools), "write_file") {
		t.Errorf("after the approval the write tools must be offered: %s", offeredNames(h.prov.reqs[2].Tools))
	}
	if len(h.st.eventsOf(EventFileChanged)) != 1 {
		t.Errorf("events = %v, want the file change of the build", h.st.eventTypes())
	}
	if got := []string{h.st.usage[0].Purpose, h.st.usage[1].Purpose, h.st.usage[2].Purpose}; strings.Join(got, ",") != "planning,planning,chat" {
		t.Errorf("usage purposes = %v", got)
	}
}

// The build that starts in the same turn as an approved plan gets the whole
// MaxSteps: the planning steps before the approval were spent in plan mode and
// do not count against the build of milestone 1.
func TestPlanMode_ApprovedBuildGetsItsOwnStepAllowance(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan,
		scriptStep{resp: toolResp(askCall("a1"))},
		scriptStep{resp: toolResp(planCall("p1"))},
		scriptStep{resp: toolResp(writeCall("w1", "src/Patients.tsx"))},
		scriptStep{resp: textResp("The patient list is ready.")},
	)
	h.deps.Limits = Limits{MaxSteps: 2}
	h.mustRun()
	h.resolve("a1", "The user answered: Team", `{"build_failed":false,"is_error":false}`, "")
	h.mustRun()
	h.resolve("p1", "The user approved the plan.", `{"build_failed":false,"is_error":false,"plan_approved":true}`, store.ModeBuild)
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed: two build steps fit a MaxSteps of 2 (events %v)", fin.Status, h.st.eventTypes())
	}
	if h.prov.calls() != 4 {
		t.Errorf("provider calls = %d, want 2 planning + 2 build", h.prov.calls())
	}
}

// The build steps still count: a third build step is over a MaxSteps of 2.
func TestPlanMode_ApprovedBuildStillHitsMaxSteps(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan,
		scriptStep{resp: toolResp(planCall("p1"))},
		scriptStep{resp: toolResp(writeCall("w1", "src/A.tsx"))},
		scriptStep{resp: toolResp(writeCall("w2", "src/B.tsx"))},
		scriptStep{resp: textResp("never reached")},
	)
	h.deps.Limits = Limits{MaxSteps: 2}
	h.mustRun()
	h.resolve("p1", "The user approved the plan.", `{"build_failed":false,"is_error":false,"plan_approved":true}`, store.ModeBuild)
	h.mustRun()

	if fin := h.finished(); fin.Status != store.TurnLimitReached {
		t.Fatalf("status = %q, want limit_reached", fin.Status)
	}
	if h.prov.calls() != 4 {
		t.Errorf("provider calls = %d, want 1 planning + 2 build + 1 closing call", h.prov.calls())
	}
}

func TestPlanMode_InvalidPlanIsAToolErrorNotAPause(t *testing.T) {
	bad := provider.ToolCall{ID: "p1", Name: planning.ToolProposeBuildPlan, Arguments: json.RawMessage(`{"summary":"x","screens":[],"milestones":[]}`)}
	h := newPlanHarness(t, store.ModePlan, scriptStep{resp: toolResp(bad)}, scriptStep{resp: toolResp(planCall("p2"))})
	h.mustRun()
	if m := h.toolResult("p1"); !strings.Contains(m.Content, "assumptions: required") {
		t.Errorf("validation error not shown to the model: %q", m.Content)
	}
	if h.pendingName() != planning.ToolProposeBuildPlan {
		t.Fatalf("the corrected plan must pause the turn")
	}
}

const (
	metaAnswered = `{"build_failed":false,"is_error":false}`
	metaClosed   = `{"build_failed":false,"is_error":false,"questions_closed":true}`
	metaReopened = `{"build_failed":false,"is_error":false,"questions_reopened":true}`
)

// The question bound holds across resumes, because it is counted from the
// persisted tool results: three rounds in a planning turn, one more for each
// time the user sends the plan back to questions.
func TestPlanMode_QuestionBoundAndReopen(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan,
		scriptStep{resp: toolResp(askCall("a1"))},
		scriptStep{resp: toolResp(askCall("a2"))},
		scriptStep{resp: toolResp(askCall("a3"))},
		scriptStep{resp: toolResp(askCall("a4"))}, // over the bound
		scriptStep{resp: toolResp(planCall("p1"))},
		scriptStep{resp: toolResp(askCall("a5"))}, // the reopened round
		scriptStep{resp: toolResp(askCall("a6"))}, // over the bound again
		scriptStep{resp: toolResp(planCall("p2"))},
	)
	for _, id := range []string{"a1", "a2", "a3"} {
		h.mustRun()
		if h.pendingName() != planning.ToolAskUser {
			t.Fatalf("round %s must pause", id)
		}
		h.resolve(id, "answered", metaAnswered, "")
	}
	h.mustRun()
	if m := h.toolResult("a4"); !strings.Contains(m.Content, "question limit reached (3 rounds in this turn)") || !parseToolMeta(m).IsError {
		t.Fatalf("the fourth round must be refused: %q", m.Content)
	}
	if h.pendingName() != planning.ToolProposeBuildPlan {
		t.Fatalf("after the refusal the model proposes the plan")
	}

	h.resolve("p1", "back to questions", metaReopened, "")
	h.mustRun()
	if h.pendingName() != planning.ToolAskUser {
		t.Fatalf("a reopened plan grants one more round")
	}
	h.resolve("a5", "answered", metaAnswered, "")
	h.mustRun()
	if m := h.toolResult("a6"); !strings.Contains(m.Content, "question limit reached") {
		t.Fatalf("only one round is granted per reopen: %q", m.Content)
	}
	if h.pendingName() != planning.ToolProposeBuildPlan {
		t.Fatalf("the turn ends on the second plan")
	}
}

func TestPlanMode_NoQuestionsAfterTheUserEndsThem(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan,
		scriptStep{resp: toolResp(askCall("a1"))},
		scriptStep{resp: toolResp(askCall("a2"))},
		scriptStep{resp: toolResp(planCall("p1"))},
		scriptStep{resp: toolResp(askCall("a3"))},
	)
	h.mustRun()
	h.resolve("a1", planning.SkippedResult, metaClosed, "")
	h.mustRun()
	if m := h.toolResult("a2"); !strings.Contains(m.Content, "the user ended the questions") {
		t.Fatalf("asking after a skip must be refused: %q", m.Content)
	}
	if h.pendingName() != planning.ToolProposeBuildPlan {
		t.Fatal("the model must go on to the plan")
	}
	// Sending the plan back to questions opens them again.
	h.resolve("p1", "more questions please", metaReopened, "")
	h.mustRun()
	if h.pendingName() != planning.ToolAskUser {
		t.Fatalf("questions reopened by the user must be allowed (status %q)", h.finished().Status)
	}
}

func TestBuildMode_OneQuestionRoundPerTurn(t *testing.T) {
	h := newPlanHarness(t, store.ModeBuild,
		scriptStep{resp: toolResp(askCall("a1"))},
		scriptStep{resp: toolResp(askCall("a2"))},
		scriptStep{resp: textResp("going with the default")},
	)
	h.mustRun()
	if h.pendingName() != planning.ToolAskUser {
		t.Fatal("a build turn may ask one round")
	}
	h.resolve("a1", "answered", metaAnswered, "")
	h.mustRun()
	if m := h.toolResult("a2"); !strings.Contains(m.Content, "question limit reached (1 round in a build turn)") {
		t.Fatalf("second round in a build turn: %q", m.Content)
	}
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q", fin.Status)
	}
}

func TestAnalyzeTurn_CountsOnlyAnsweredQuestionRounds(t *testing.T) {
	calls := func(c ...provider.ToolCall) json.RawMessage {
		b, _ := json.Marshal(c)
		return b
	}
	msgs := []store.Message{
		// Another turn's round does not count.
		{ID: 1, TurnID: "old", Role: store.RoleAssistant, ToolCalls: calls(askCall("o1"))},
		{ID: 2, TurnID: "old", Role: store.RoleTool, ToolCallID: "o1", ToolCalls: json.RawMessage(metaAnswered)},
		{ID: 3, TurnID: "t", Role: store.RoleAssistant, ToolCalls: calls(askCall("a1"), listCall("l1"))},
		{ID: 4, TurnID: "t", Role: store.RoleTool, ToolCallID: "a1", ToolCalls: json.RawMessage(metaAnswered)},
		{ID: 5, TurnID: "t", Role: store.RoleTool, ToolCallID: "l1", ToolCalls: json.RawMessage(metaAnswered)},
		// A refused or invalid round is an error result and does not count.
		{ID: 6, TurnID: "t", Role: store.RoleAssistant, ToolCalls: calls(askCall("a2"))},
		{ID: 7, TurnID: "t", Role: store.RoleTool, ToolCallID: "a2", ToolCalls: json.RawMessage(`{"build_failed":false,"is_error":true}`)},
		{ID: 8, TurnID: "t", Role: store.RoleAssistant, ToolCalls: calls(askCall("a3"))},
		{ID: 9, TurnID: "t", Role: store.RoleTool, ToolCallID: "a3", ToolCalls: json.RawMessage(metaClosed)},
	}
	st := analyzeTurn(msgs, "t")
	if st.questionRounds != 1 || !st.questionsClosed || st.questionReopens != 0 {
		t.Fatalf("state = %+v", st)
	}
	msgs = append(msgs,
		store.Message{ID: 10, TurnID: "t", Role: store.RoleAssistant, ToolCalls: calls(planCall("p1"))},
		store.Message{ID: 11, TurnID: "t", Role: store.RoleTool, ToolCallID: "p1", ToolCalls: json.RawMessage(metaReopened)},
	)
	st = analyzeTurn(msgs, "t")
	if st.questionRounds != 1 || st.questionsClosed || st.questionReopens != 1 {
		t.Fatalf("state after a reopen = %+v", st)
	}
}

func briefInput(t *testing.T, version int, workspaceFile string) *BriefInput {
	t.Helper()
	plan, err := planning.ParsePlan(json.RawMessage(testPlan))
	if err != nil {
		t.Fatal(err)
	}
	return &BriefInput{Version: version, Plan: plan, Done: map[int]bool{1: true}, WorkspaceFile: workspaceFile}
}

func withBrief(h *harness, b *BriefInput) {
	h.deps.Prompt = func(context.Context, *store.Session) (PromptInput, error) {
		return PromptInput{Session: SessionContext{Locale: "en"}, Brief: b}, nil
	}
}

func briefOps(h *harness) []opRecord {
	var out []opRecord
	for _, o := range h.sb.ops {
		if o.path == planning.BriefFileName {
			out = append(out, o)
		}
	}
	return out
}

func TestBriefFile_WrittenOnceByABuildTurnAndReplayedOnResume(t *testing.T) {
	h := newPlanHarness(t, store.ModeBuild,
		scriptStep{resp: toolResp(proposeCall("b1", `{"op":"add_table"}`))},
		scriptStep{resp: textResp("done")},
	)
	withBrief(h, briefInput(t, 2, ""))
	h.mustRun()
	ops := briefOps(h)
	if len(ops) != 1 || ops[0].kind != sandbox.OpWriteFile || ops[0].opID != "turn-1:brief-v2" {
		t.Fatalf("brief file ops = %+v", ops)
	}

	// The turn paused before any commit, so the workspace copy is still stale
	// when it resumes: the write is sent again with the same op id and the
	// builder replays it.
	h.resolve("b1", "applied", metaAnswered, "")
	h.mustRun()
	ops = briefOps(h)
	if len(ops) != 2 || ops[1].opID != ops[0].opID {
		t.Fatalf("the resumed turn must replay the same op: %+v", ops)
	}
	if fin := h.finished(); fin.Status != store.TurnCompleted || fin.CommitSHA == "" || h.sb.commits != 1 {
		t.Fatalf("the brief file must be committed with the turn: %+v (commits %d)", fin, h.sb.commits)
	}
}

func TestBriefFile_NotWrittenWhenCurrentOrWhilePlanning(t *testing.T) {
	plan, _ := planning.ParsePlan(json.RawMessage(testPlan))

	h := newPlanHarness(t, store.ModeBuild, scriptStep{resp: textResp("done")})
	withBrief(h, briefInput(t, 2, planning.RenderBriefFile(plan, 2)))
	h.mustRun()
	if ops := briefOps(h); len(ops) != 0 || h.sb.commits != 0 {
		t.Fatalf("an up-to-date brief file must not be rewritten: %+v", ops)
	}

	h = newPlanHarness(t, store.ModePlan, scriptStep{resp: textResp("thinking")})
	withBrief(h, briefInput(t, 2, ""))
	h.mustRun()
	if ops := briefOps(h); len(ops) != 0 {
		t.Fatalf("a planning turn must not write the brief file: %+v", ops)
	}

	// A builder failure on the copy does not fail the turn.
	h = newPlanHarness(t, store.ModeBuild, scriptStep{resp: textResp("done")})
	withBrief(h, briefInput(t, 1, ""))
	h.sb.opErr = fmt.Errorf("builder busy")
	h.mustRun()
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed", fin.Status)
	}
}

func sessionSection(system string) (prefix, session string) {
	i := strings.Index(system, headerSession)
	return system[:i], system[i:]
}

func TestPrompt_BriefIsStableAndTheDirectiveFollowsTheTurn(t *testing.T) {
	run := func(mode string, milestone int, brief *BriefInput, planningOn bool) string {
		h := newPlanHarness(t, mode, scriptStep{resp: textResp("done")})
		h.deps.PlanningEnabled = planningOn
		if milestone > 0 {
			v := 2
			h.st.turn.BriefVersion, h.st.turn.Milestone = &v, &milestone
		}
		withBrief(h, brief)
		h.mustRun()
		return h.prov.reqs[0].System
	}
	plan, _ := planning.ParsePlan(json.RawMessage(testPlan))
	file := planning.RenderBriefFile(plan, 2)

	second := run(store.ModeBuild, 2, briefInput(t, 2, file), true)
	third := run(store.ModeBuild, 3, briefInput(t, 2, file), true)
	prefix2, session2 := sessionSection(second)
	prefix3, session3 := sessionSection(third)
	if prefix2 != prefix3 {
		t.Fatal("everything before the session context must be identical between turns of the same brief")
	}
	if !strings.Contains(prefix2, headerBrief+"\n"+planning.RenderBrief(plan)) {
		t.Fatalf("the brief section is missing or not verbatim:\n%s", prefix2)
	}
	if !strings.Contains(session2, "1. Patient list [done]\n2. Patient record [this turn]\n3. Notes [pending]") ||
		!strings.Contains(session2, "Build milestone 2 only") || !strings.Contains(session2, "milestone 3 (Notes) is next") {
		t.Errorf("directive of milestone 2:\n%s", session2)
	}
	if !strings.Contains(session3, "3. Notes [this turn]") || !strings.Contains(session3, "Build milestone 3 only") {
		t.Errorf("directive of milestone 3:\n%s", session3)
	}

	free := run(store.ModeBuild, 0, briefInput(t, 2, file), true)
	if _, s := sessionSection(free); !strings.Contains(s, "This turn has no milestone") || !strings.Contains(s, "propose_build_plan") {
		t.Errorf("directive of a free turn:\n%s", s)
	}

	planTurn := run(store.ModePlan, 0, nil, true)
	if strings.Contains(planTurn, headerBrief) {
		t.Error("no brief, no brief section")
	}
	if _, s := sessionSection(planTurn); !strings.Contains(s, "Mode: planning") || !strings.Contains(s, "you cannot change files") ||
		!strings.Contains(s, "Do not reply in plain text") {
		t.Errorf("directive of a planning turn:\n%s", s)
	}
	revise := run(store.ModePlan, 0, briefInput(t, 2, file), true)
	if _, s := sessionSection(revise); !strings.Contains(s, "An app brief already exists") {
		t.Errorf("directive of a plan-first turn with a brief:\n%s", s)
	}

	noBrief := run(store.ModeBuild, 0, nil, true)
	if _, s := sessionSection(noBrief); !strings.Contains(s, "There is no app brief yet") {
		t.Errorf("directive of a build turn without a brief:\n%s", s)
	}
	off := run(store.ModeBuild, 0, nil, false)
	if _, s := sessionSection(off); strings.Contains(s, "This turn:") {
		t.Errorf("with planning off and no brief there is nothing to say:\n%s", s)
	}
}

// The store repeats the planning tool names so it does not import the tools.
func TestPlanningToolNamesMatchTheStore(t *testing.T) {
	if store.PlanningToolAskUser != planning.ToolAskUser || store.PlanningToolProposeBuildPlan != planning.ToolProposeBuildPlan {
		t.Fatal("store and planning disagree on the planning tool names")
	}
}

func TestBuildSystemPromptWithBrief_OrderAndPrecedence(t *testing.T) {
	out := BuildSystemPromptWithBrief("CORE", "COMPANY", "GUIDE", "SKILLS", "BRIEF-BODY", SessionContext{Locale: "en", Directive: "DIRECTIVE-BODY"})
	order := []string{headerCore, headerCompany, headerTemplate, headerSkills, headerBrief + "\nBRIEF-BODY", headerSession, "This turn:\nDIRECTIVE-BODY"}
	at := -1
	for _, part := range order {
		i := strings.Index(out, part)
		if i <= at {
			t.Fatalf("%q is missing or out of order in:\n%s", part, out)
		}
		at = i
	}
	if !strings.Contains(headerBrief, "lower priority than CORE RULES and company standards") {
		t.Error("the brief header must state its precedence")
	}
	if strings.Contains(BuildSystemPrompt("CORE", "", "", "", SessionContext{}), headerBrief) {
		t.Error("no brief, no section")
	}
	// A valid brief is at most planning.MaxBriefChars, so it is never cut.
	brief := strings.Repeat("b", planning.MaxBriefChars)
	if out := BuildSystemPromptWithBrief("CORE", "", "", "", brief, SessionContext{}); !strings.Contains(out, brief) || strings.Contains(out, "truncated") {
		t.Error("a brief at the size limit must not be truncated")
	}
	if !strings.Contains(CorePrompt(), "# Planning") || !strings.Contains(CorePrompt(), "app brief, session context") {
		t.Error("the core prompt must carry the planning rules and name the brief in the precedence list")
	}
}

// ai-builder-economy AEC-02: a plan-mode turn's calls go to the planning
// provider and are recorded under its model; a build-mode turn's calls go to
// the build provider even when a planning provider is set.
func TestPlanningProvider_AnswersOnlyPlanModeTurns(t *testing.T) {
	for _, tc := range []struct {
		mode, wantModel, wantPurpose string
		planningCalls, buildCalls    int
	}{
		{store.ModePlan, "fake-plan", store.PurposePlanning, 1, 0},
		{store.ModeBuild, "fake-1", store.PurposeChat, 0, 1},
	} {
		h := newPlanHarness(t, tc.mode, scriptStep{resp: textResp("done")})
		planner := &fakeProvider{modelID: "fake-plan", script: []scriptStep{{resp: textResp("done")}}}
		h.deps.PlanningProvider = planner
		h.mustRun()

		if planner.calls() != tc.planningCalls || h.prov.calls() != tc.buildCalls {
			t.Errorf("%s turn: planning provider %d calls, build provider %d, want %d and %d",
				tc.mode, planner.calls(), h.prov.calls(), tc.planningCalls, tc.buildCalls)
		}
		if len(h.st.usage) != 1 || h.st.usage[0].Model != tc.wantModel || h.st.usage[0].Purpose != tc.wantPurpose {
			t.Errorf("%s turn: usage = %+v, want model %s and purpose %s", tc.mode, h.st.usage, tc.wantModel, tc.wantPurpose)
		}
	}
}

// AEC-01: with no planning provider a plan-mode turn runs on the build one.
func TestPlanningProvider_NilKeepsTheBuildProvider(t *testing.T) {
	h := newPlanHarness(t, store.ModePlan, scriptStep{resp: textResp("done")})
	h.mustRun()
	if h.prov.calls() != 1 || len(h.st.usage) != 1 || h.st.usage[0].Model != "fake-1" || h.st.usage[0].Purpose != store.PurposePlanning {
		t.Fatalf("build provider calls = %d, usage = %+v", h.prov.calls(), h.st.usage)
	}
}

// A project with no backend (builder-no-backend T4): tools that work on the
// backend are not offered, and a call to one is an unknown tool, not a pause.
func TestNoBackend_BackendToolsAreNotOfferedAndDoNotExist(t *testing.T) {
	h := newPlanHarness(t, store.ModeBuild,
		scriptStep{resp: toolResp(provider.ToolCall{ID: "b1", Name: "propose_backend_change", Arguments: json.RawMessage(`{}`)})},
		scriptStep{resp: textResp("ok, a static site")},
	)
	h.st.sess.BackendAppID, h.st.sess.BackendOrigin = "", store.BackendOriginNone
	h.mustRun()

	if got := offeredNames(h.prov.reqs[0].Tools); strings.Contains(got, "propose_backend_change") || !strings.Contains(got, "write_file") {
		t.Fatalf("offered tools = %s", got)
	}
	if m := h.toolResult("b1"); !strings.Contains(m.Content, "unknown tool") || !parseToolMeta(m).IsError {
		t.Errorf("a backend tool call must be an unknown tool: %q", m.Content)
	}
	if fin := h.finished(); fin.Status != store.TurnCompleted {
		t.Fatalf("status = %q, want completed (no pause for a proposal)", fin.Status)
	}
}

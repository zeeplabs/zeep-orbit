package dashboard

// builder_planning_test.go — ai-builder-planning P06: the answers, plan
// approval and plan revision routes, the planning options of the messages
// route, the 409s between backend and planning proposals, the planning events
// and the brief fields of the session detail. Integration tests against a real
// Postgres; the full path runs the agent loop with a scripted model.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

const testQuestions = `{"questions":[` +
	`{"id":"users","title":"Users","question":"Who uses the app?","options":[{"label":"Staff"},{"label":"Patients"}]},` +
	`{"id":"screens","title":"Screens","question":"Which screens?","multiple":true,"options":[{"label":"List"},{"label":"Record"},{"label":"Calendar"}]}]}`

// callBody calls fn as user with a JSON body and several chi URL params.
func callBody(fn http.HandlerFunc, user *DashboardUser, target, body string, params ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != nil {
		req = withUser(req, user)
	}
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(params); i += 2 {
		rctx.URLParams.Add(params[i], params[i+1])
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func (e *bhEnv) answers(user *DashboardUser, sessID, turn, body string) *httptest.ResponseRecorder {
	return callBody(e.h.AnswerQuestions, user, "/x/answers", body, "id", sessID, "turn", turn)
}

func (e *bhEnv) approve(user *DashboardUser, sessID, turn, body string) *httptest.ResponseRecorder {
	return callBody(e.h.ApprovePlan, user, "/x/plan/approve", body, "id", sessID, "turn", turn)
}

func (e *bhEnv) revise(user *DashboardUser, sessID, turn, body string) *httptest.ResponseRecorder {
	return callBody(e.h.RevisePlan, user, "/x/plan/revise", body, "id", sessID, "turn", turn)
}

func (e *bhEnv) sendMessage(user *DashboardUser, sessID, body string) *httptest.ResponseRecorder {
	return callBody(e.h.CreateMessage, user, "/x/messages", body, "id", sessID)
}

// pausePlanning creates a session of e.owner on appA in the given phase and
// leaves its first turn paused on tool (call id callID) with args, as the loop
// does.
func (e *bhEnv) pausePlanning(t *testing.T, phase, tool, callID, args string) (*store.Session, *store.Turn) {
	t.Helper()
	ctx := context.Background()
	sess, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "t",
		TemplateKind: store.TemplateDefault, TemplateVersion: "v0", Phase: phase,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := e.st.CreateTurn(ctx, e.owner.ID, sess.ID, store.KindChat, "a CRM for my clinic")
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	e.pauseOn(t, turn.ID, tool, callID, args)
	return sess, turn
}

// pauseOn claims turnID (draining other sessions' turns first) and pauses it on
// tool, as the loop does after the model called it.
func (e *bhEnv) pauseOn(t *testing.T, turnID, tool, callID, args string) {
	t.Helper()
	ctx := context.Background()
	for {
		claimed, err := e.st.ClaimTurn(ctx, "w1", time.Minute)
		if err != nil {
			t.Fatalf("ClaimTurn: %v", err)
		}
		if claimed.ID == turnID {
			break
		}
		if err := e.st.FinishTurn(ctx, store.Lease{TurnID: claimed.ID, Owner: "w1"}, store.FinishParams{Status: store.TurnCompleted}); err != nil {
			t.Fatalf("FinishTurn (drain): %v", err)
		}
	}
	lease := store.Lease{TurnID: turnID, Owner: "w1"}
	calls := `[{"id":"` + callID + `","name":"` + tool + `","arguments":` + args + `}]`
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleAssistant, ToolCalls: json.RawMessage(calls)}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	pending := `{"tool_call_id":"` + callID + `","name":"` + tool + `","arguments":` + args + `}`
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation, PendingProposal: json.RawMessage(pending)}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
}

func (e *bhEnv) lastToolMessage(t *testing.T, sessID string) store.Message {
	t.Helper()
	msgs, err := e.st.ListMessages(context.Background(), sessID)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == store.RoleTool {
			return msgs[i]
		}
	}
	t.Fatal("no tool message")
	return store.Message{}
}

// servedEvent returns the payload of the last event of eventType as the events
// route serves it.
func (e *bhEnv) servedEvent(t *testing.T, sessID, eventType string) map[string]any {
	t.Helper()
	w := e.do(t, e.h.ListEvents, e.owner, http.MethodGet, "/x/events", "", "id", sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("events: %d %s", w.Code, w.Body.String())
	}
	page := decodeJSON[BuilderEventsPage](t, w)
	var out map[string]any
	for _, ev := range page.Events {
		if ev.Type == eventType {
			out = map[string]any{}
			if err := json.Unmarshal(ev.Payload, &out); err != nil {
				t.Fatal(err)
			}
		}
	}
	if out == nil {
		t.Fatalf("no %s event", eventType)
	}
	return out
}

// ---- scripted model for the full path ----

type scriptedModel struct {
	mu     sync.Mutex
	steps  []func(req provider.ChatRequest) provider.ChatResponse
	offers [][]string // tool names offered on each call
}

func (m *scriptedModel) Name() string  { return "openai_compatible" }
func (m *scriptedModel) Model() string { return "test-model" }
func (m *scriptedModel) Chat(_ context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(req.Tools))
	for _, s := range req.Tools {
		names = append(names, s.Name)
	}
	m.offers = append(m.offers, names)
	if len(m.steps) == 0 {
		return provider.ChatResponse{}, errors.New("scripted model: no step left")
	}
	step := m.steps[0]
	m.steps = m.steps[1:]
	return step(req), nil
}

func toolCallReply(id, name, args string) provider.ChatResponse {
	call := provider.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
	return provider.ChatResponse{
		Message:      provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call}},
		ToolCalls:    []provider.ToolCall{call},
		Usage:        provider.Usage{InputTokens: 10, OutputTokens: 5},
		FinishReason: "tool_calls",
	}
}

func textReply(text string) provider.ChatResponse {
	return provider.ChatResponse{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: text},
		Usage:        provider.Usage{InputTokens: 10, OutputTokens: 5},
		FinishReason: "stop",
	}
}

// lastToolResult is the content of the last tool message the model was sent.
func lastToolResult(req provider.ChatRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == provider.RoleTool {
			return req.Messages[i].Content
		}
	}
	return ""
}

// committingSandbox is the handler fake plus the checkpoint commit of a build
// turn. The deps of the full path have no prompt source, so the loop gets no
// brief and writes no brief file (that write is tested in the agent package
// and the runtime tests).
type committingSandbox struct {
	*bhFakeSandbox
}

func (s *committingSandbox) Commit(context.Context, string, int, string) (sandbox.CommitResult, error) {
	return sandbox.CommitResult{SHA: strings.Repeat("a", 40)}, nil
}

type passBudget struct{}

func (passBudget) Check(context.Context, string, string) ([]store.BudgetWarning, error) {
	return nil, nil
}

// The whole planning path over HTTP: a message in a briefing session starts a
// plan-mode turn, the model asks, the user answers, the model proposes a plan,
// the user approves it, and the SAME turn goes on with the full tool set and
// builds milestone 1. The session then reports the brief and the next
// milestone, and "Continue" accepts only that milestone.
func TestBuilderPlanning_FullPathOverHTTP(t *testing.T) {
	e := newBHEnv(t)
	ctx := context.Background()
	sb := &committingSandbox{bhFakeSandbox: e.fake}

	sess := e.createOK(t, e.owner, e.appA)
	if sess.Phase != store.PhaseBriefing {
		t.Fatalf("phase = %q, want briefing", sess.Phase)
	}
	w := e.sendMessage(e.owner, sess.ID, `{"content":"a CRM for my clinic"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("message: %d %s", w.Code, w.Body.String())
	}
	turnView := decodeJSON[BuilderTurnView](t, w)
	if turnView.Mode != store.ModePlan {
		t.Fatalf("turn mode = %q, want plan", turnView.Mode)
	}

	var writes int
	reg := tools.NewRegistry()
	for _, tl := range planning.Tools() {
		if err := reg.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Register(tools.Tool{
		Name: "fake_write", Description: "write", Schema: json.RawMessage(`{"type":"object"}`), Kind: tools.KindWorkspace,
		Handler: func(context.Context, tools.Call) (tools.Result, error) {
			writes++
			return tools.Result{Content: "written"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	model := &scriptedModel{}
	model.steps = []func(provider.ChatRequest) provider.ChatResponse{
		func(provider.ChatRequest) provider.ChatResponse {
			return toolCallReply("call_q1", planning.ToolAskUser, testQuestions)
		},
		func(req provider.ChatRequest) provider.ChatResponse {
			if got := lastToolResult(req); !strings.Contains(got, "users (Users): Staff") || !strings.Contains(got, "screens (Screens): List; Record") {
				t.Errorf("the model did not get the answers: %q", got)
			}
			return toolCallReply("call_p1", planning.ToolProposeBuildPlan, testBuildPlan)
		},
		func(req provider.ChatRequest) provider.ChatResponse {
			if got := lastToolResult(req); !strings.Contains(got, "Build milestone 1 now: Patient list") {
				t.Errorf("the model did not get the approval: %q", got)
			}
			return toolCallReply("call_w1", "fake_write", `{}`)
		},
		func(provider.ChatRequest) provider.ChatResponse {
			return textReply("Milestone 1 is done. Next: Patient record.")
		},
	}
	deps := agent.Deps{
		Store: e.st, Provider: model, Tools: reg, Builder: sb, Budget: passBudget{}, PlanningEnabled: true,
	}
	run := func() *store.Turn {
		t.Helper()
		claimed, err := e.st.ClaimTurn(ctx, "w", time.Minute)
		if err != nil || claimed.ID != turnView.ID {
			t.Fatalf("claim: %+v %v", claimed, err)
		}
		if err := agent.RunTurn(ctx, deps, store.Lease{TurnID: claimed.ID, Owner: "w"}); err != nil {
			t.Fatalf("RunTurn: %v", err)
		}
		got, err := e.st.GetTurn(ctx, claimed.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// 1. The model asks.
	if got := run(); got.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("after the questions: %+v", got)
	}
	detail := decodeJSON[BuilderSessionDetail](t, e.get(t, e.owner, sess.ID))
	pp := detail.Turns[0].PendingProposal
	if pp == nil || pp.Name != planning.ToolAskUser || pp.ToolCallID != "call_q1" {
		t.Fatalf("pending = %+v", pp)
	}

	// 2. The user answers.
	w = e.answers(e.owner, sess.ID, turnView.ID,
		`{"tool_call_id":"call_q1","answers":[{"id":"users","choices":["Staff"]},{"id":"screens","choices":["List","Record"]}]}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("answers: %d %s", w.Code, w.Body.String())
	}
	answered := e.servedEvent(t, sess.ID, store.EventQuestionsAnswered)
	if answered["skipped"] != false || fmt.Sprint(answered["answers"]) != "map[screens:List; Record users:Staff]" {
		t.Errorf("questions_answered = %v", answered)
	}

	// 3. The model proposes a plan.
	if got := run(); got.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("after the plan: %+v", got)
	}

	// 4. The user approves it.
	w = e.approve(e.owner, sess.ID, turnView.ID, `{"tool_call_id":"call_p1"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	approved := decodeJSON[builderPlanningResult](t, w)
	if approved.BriefVersion != 1 || approved.Milestone != 1 || approved.Turn == nil ||
		approved.Turn.Mode != store.ModeBuild || approved.Turn.Milestone == nil || *approved.Turn.Milestone != 1 {
		t.Fatalf("approve result = %+v", approved)
	}
	ev := e.servedEvent(t, sess.ID, store.EventPlanApproved)
	if ev["version"] != float64(1) || ev["milestones"] != float64(2) || ev["milestone"] != float64(1) || ev["tool_call_id"] != "call_p1" {
		t.Errorf("plan_approved = %v", ev)
	}
	var audits int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM zeep_system.audit_log WHERE action = 'builder.plan.approve'`).Scan(&audits); err != nil || audits != 1 {
		t.Errorf("plan approve audits = %d (%v)", audits, err)
	}

	// 5. The same turn builds milestone 1 with the full tool set.
	if got := run(); got.Status != store.TurnCompleted {
		t.Fatalf("after the build: %+v", got)
	}
	if writes != 1 {
		t.Errorf("fake_write ran %d times, want 1", writes)
	}
	if len(model.offers) != 4 {
		t.Fatalf("model calls = %d, want 4", len(model.offers))
	}
	for i, offered := range model.offers {
		hasWrite := strings.Contains(strings.Join(offered, ","), "fake_write")
		if hasWrite != (i >= 2) {
			t.Errorf("call %d offered %v", i+1, offered)
		}
	}

	// 6. The session reports the brief and the next milestone.
	detail = decodeJSON[BuilderSessionDetail](t, e.get(t, e.owner, sess.ID))
	if detail.Phase != store.PhaseBuilding || detail.BriefVersion != 1 || detail.MilestonesTotal != 2 || detail.MilestonesDone != 1 ||
		detail.NextMilestone == nil || detail.NextMilestone.Index != 2 || detail.NextMilestone.Title != "Patient record" {
		t.Errorf("detail = phase %s brief %d total %d done %d next %+v",
			detail.Phase, detail.BriefVersion, detail.MilestonesTotal, detail.MilestonesDone, detail.NextMilestone)
	}

	// 7. "Continue" accepts only the next pending milestone.
	if w := e.sendMessage(e.owner, sess.ID, `{"content":"continue","milestone":1}`); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "next pending milestone") {
		t.Errorf("milestone 1 again: %d %s", w.Code, w.Body.String())
	}
	w = e.sendMessage(e.owner, sess.ID, `{"content":"continue","milestone":2}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("milestone 2: %d %s", w.Code, w.Body.String())
	}
	next := decodeJSON[BuilderTurnView](t, w)
	if next.Mode != store.ModeBuild || next.Milestone == nil || *next.Milestone != 2 || next.BriefVersion == nil || *next.BriefVersion != 1 {
		t.Errorf("continue turn = %+v", next)
	}
}

func TestBuilderPlanning_AnswerValidationErrorsKeepTheTurnPaused(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_1", testQuestions)

	for name, c := range map[string]struct{ body, want string }{
		"no call id":        {`{"answers":[]}`, "tool_call_id is required"},
		"unknown question":  {`{"tool_call_id":"call_1","answers":[{"id":"nope","choices":[]}]}`, "does not match a question"},
		"not an option":     {`{"tool_call_id":"call_1","answers":[{"id":"users","choices":["Robots"]}]}`, "not one of its options"},
		"two on single":     {`{"tool_call_id":"call_1","answers":[{"id":"users","choices":["Staff","Patients"]}]}`, "accepts one choice"},
		"text too long":     {`{"tool_call_id":"call_1","answers":[{"id":"users","text":"` + strings.Repeat("x", planning.MaxAnswerTextChars+1) + `"}]}`, "too long"},
		"choice and decide": {`{"tool_call_id":"call_1","answers":[{"id":"users","choices":["Staff"],"decide":true}]}`, "decide for me"},
	} {
		w := e.answers(e.owner, sess.ID, turn.ID, c.body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	got, err := e.st.GetTurn(context.Background(), turn.ID)
	if err != nil || got.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("turn = %+v %v, want still awaiting", got, err)
	}
}

func TestBuilderPlanning_SkipEndsTheQuestionsAndKeepsTypedAnswers(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_1", testQuestions)

	w := e.answers(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_1","skip":true,"answers":[{"id":"users","text":"front desk"}]}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("skip: %d %s", w.Code, w.Body.String())
	}
	tm := e.lastToolMessage(t, sess.ID)
	if !strings.Contains(tm.Content, `in the user's words: "front desk"`) || !strings.Contains(tm.Content, "Do not ask again") {
		t.Errorf("tool result = %q", tm.Content)
	}
	if ev := e.servedEvent(t, sess.ID, store.EventQuestionsAnswered); ev["skipped"] != true {
		t.Errorf("questions_answered = %v", ev)
	}

	// A skip with nothing typed.
	sess2, turn2 := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_1", testQuestions)
	if w := e.answers(e.owner, sess2.ID, turn2.ID, `{"tool_call_id":"call_1","skip":true}`); w.Code != http.StatusAccepted {
		t.Fatalf("bare skip: %d %s", w.Code, w.Body.String())
	}
	if tm := e.lastToolMessage(t, sess2.ID); tm.Content != planning.SkippedResult {
		t.Errorf("tool result = %q", tm.Content)
	}
}

func TestBuilderPlanning_ReviseWithAndWithoutMoreQuestions(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)

	if w := e.revise(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "feedback is required") {
		t.Errorf("no feedback: %d %s", w.Code, w.Body.String())
	}
	long := strings.Repeat("y", planning.MaxFeedbackChars+1)
	if w := e.revise(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1","feedback":"`+long+`"}`); w.Code != http.StatusBadRequest {
		t.Errorf("long feedback: %d %s", w.Code, w.Body.String())
	}

	w := e.revise(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1","feedback":"add a calendar"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("revise: %d %s", w.Code, w.Body.String())
	}
	if tm := e.lastToolMessage(t, sess.ID); !strings.Contains(tm.Content, `"add a calendar"`) || !strings.Contains(tm.Content, "propose_build_plan again") {
		t.Errorf("tool result = %q", tm.Content)
	}
	if ev := e.servedEvent(t, sess.ID, store.EventPlanRevisionRequested); ev["more_questions"] != false {
		t.Errorf("plan_revision_requested = %v", ev)
	}
	if b, err := e.st.CurrentBrief(context.Background(), sess.ID); err != nil || b != nil {
		t.Errorf("a revision stores no brief: %+v %v", b, err)
	}

	// Back to questions, with no text.
	e.pauseOn(t, turn.ID, planning.ToolProposeBuildPlan, "call_p2", testBuildPlan)
	if w := e.revise(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p2","more_questions":true}`); w.Code != http.StatusAccepted {
		t.Fatalf("more questions: %d %s", w.Code, w.Body.String())
	}
	if tm := e.lastToolMessage(t, sess.ID); !strings.Contains(tm.Content, "ask one more round") {
		t.Errorf("tool result = %q", tm.Content)
	}
	if ev := e.servedEvent(t, sess.ID, store.EventPlanRevisionRequested); ev["more_questions"] != true {
		t.Errorf("plan_revision_requested = %v", ev)
	}
}

// Each kind of proposal is resolved only by its own routes; the other ones
// answer 409 and leave the turn paused.
func TestBuilderPlanning_WrongKindIs409BothWays(t *testing.T) {
	e := newBHEnv(t)
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Logger: zap.NewNop(), Dashboard: NewHandler(e.pool, nil, zap.NewNop()),
	})

	backend, bturn := e.pausePlanning(t, store.PhaseBuilding, ToolProposeBackendChange, "call_b", bpropAddColumn)
	for name, w := range map[string]*httptest.ResponseRecorder{
		"answers": e.answers(e.owner, backend.ID, bturn.ID, `{"tool_call_id":"call_b"}`),
		"approve": e.approve(e.owner, backend.ID, bturn.ID, `{"tool_call_id":"call_b"}`),
		"revise":  e.revise(e.owner, backend.ID, bturn.ID, `{"tool_call_id":"call_b","feedback":"no"}`),
	} {
		if w.Code != http.StatusConflict {
			t.Errorf("%s on a backend proposal: %d %s", name, w.Code, w.Body.String())
		}
	}

	plan, pturn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p", testBuildPlan)
	for name, fn := range map[string]http.HandlerFunc{"confirm": e.h.ConfirmProposal, "reject": e.h.RejectProposal} {
		w := callWithParams(fn, e.owner, http.MethodPost, "/x/"+name, "id", plan.ID, "turn", pturn.ID)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), builderProposalIsPlanningMsg) {
			t.Errorf("%s on a build plan: %d %s", name, w.Code, w.Body.String())
		}
	}
	// A question round is not a build plan either.
	questions, qturn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_q", testQuestions)
	if w := e.approve(e.owner, questions.ID, qturn.ID, `{"tool_call_id":"call_q"}`); w.Code != http.StatusConflict {
		t.Errorf("approve on a question round: %d %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	for _, id := range []string{bturn.ID, pturn.ID, qturn.ID} {
		if got, err := e.st.GetTurn(ctx, id); err != nil || got.Status != store.TurnAwaitingConfirmation {
			t.Errorf("turn %s = %+v %v, want still awaiting", id, got, err)
		}
	}
}

// A card that was replaced cannot resolve the newer one, and a repeated
// request for the same card is a harmless no-op.
func TestBuilderPlanning_StaleCardIs409AndRepeatsAreNoOps(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_q1", testQuestions)

	if w := e.answers(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_old"}`); w.Code != http.StatusConflict ||
		!strings.Contains(w.Body.String(), "replaced") {
		t.Errorf("stale answers: %d %s", w.Code, w.Body.String())
	}
	body := `{"tool_call_id":"call_q1","answers":[{"id":"users","choices":["Staff"]}]}`
	if w := e.answers(e.owner, sess.ID, turn.ID, body); w.Code != http.StatusAccepted {
		t.Fatalf("answers: %d %s", w.Code, w.Body.String())
	}
	w := e.answers(e.owner, sess.ID, turn.ID, body)
	if w.Code != http.StatusOK || !decodeJSON[builderPlanningResult](t, w).AlreadyResolved {
		t.Errorf("repeated answers: %d %s", w.Code, w.Body.String())
	}

	// The model proposes a plan; approving the old question card does nothing.
	e.pauseOn(t, turn.ID, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_q1"}`); w.Code != http.StatusConflict {
		t.Errorf("approve with the question card's id: %d %s", w.Code, w.Body.String())
	}
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	w = e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`)
	if w.Code != http.StatusOK || !decodeJSON[builderPlanningResult](t, w).AlreadyResolved {
		t.Errorf("double approve: %d %s", w.Code, w.Body.String())
	}
	versions, err := e.st.ListBriefVersions(context.Background(), sess.ID)
	if err != nil || len(versions) != 1 {
		t.Errorf("brief versions = %+v %v, want exactly one", versions, err)
	}
	// Answers sent after the turn moved on are a 409, not a no-op.
	if w := e.answers(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_other"}`); w.Code != http.StatusConflict {
		t.Errorf("answers on a building turn: %d %s", w.Code, w.Body.String())
	}
}

func TestBuilderPlanning_OtherUsersAndArchivedSessionsAre404(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)

	for _, u := range []*DashboardUser{e.outsider, e.viewer, e.super} {
		if w := e.approve(u, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusNotFound {
			t.Errorf("approve by %s: %d %s", u.Email, w.Code, w.Body.String())
		}
	}
	if w := e.approve(e.owner, sess.ID, "99", `{"tool_call_id":"call_p1"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown turn: %d %s", w.Code, w.Body.String())
	}
	if err := e.st.ArchiveSession(context.Background(), e.owner.ID, sess.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusNotFound {
		t.Errorf("archived: %d %s", w.Code, w.Body.String())
	}
}

func TestBuilderMessages_PlanningOptions(t *testing.T) {
	e := newBHEnv(t)
	ctx := context.Background()
	sess, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "t",
		TemplateKind: store.TemplateDefault, TemplateVersion: "v0", Phase: store.PhaseBuilding,
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct{ body, want string }{
		"plan first and milestone": {`{"content":"x","plan_first":true,"milestone":1}`, "cannot be combined"},
		"negative milestone":       {`{"content":"x","milestone":-1}`, "positive"},
		"no brief":                 {`{"content":"x","milestone":1}`, "next pending milestone"},
	} {
		if w := e.sendMessage(e.owner, sess.ID, c.body); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}

	// "Plan first" in a building session runs a plan-mode turn.
	w := e.sendMessage(e.owner, sess.ID, `{"content":"x","plan_first":true}`)
	if w.Code != http.StatusAccepted || decodeJSON[BuilderTurnView](t, w).Mode != store.ModePlan {
		t.Fatalf("plan first: %d %s", w.Code, w.Body.String())
	}

	// With planning off it is refused.
	setPlanningMode(t, e, BuilderPlanningOff)
	sess2, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "t2",
		TemplateKind: store.TemplateDefault, TemplateVersion: "v0", Phase: store.PhaseBriefing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := e.sendMessage(e.owner, sess2.ID, `{"content":"x","plan_first":true}`); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "planning is turned off") {
		t.Errorf("plan first with planning off: %d %s", w.Code, w.Body.String())
	}
	// ...and a briefing session left over from before builds directly.
	w = e.sendMessage(e.owner, sess2.ID, `{"content":"x"}`)
	if w.Code != http.StatusAccepted || decodeJSON[BuilderTurnView](t, w).Mode != store.ModeBuild {
		t.Fatalf("message with planning off: %d %s", w.Code, w.Body.String())
	}
}

// The planning events serve only their whitelisted keys, with the answers
// capped and malformed entries dropped.
func TestSanitizeBuilderEventPayload_PlanningEvents(t *testing.T) {
	long := strings.Repeat("z", builderAnswerEventChars+50)
	raw := json.RawMessage(`{"tool_call_id":"c1","skipped":false,"secret":"x","answers":{"users":"` + long + `","Bad Key":"x","n":3}}`)
	var got map[string]any
	if err := json.Unmarshal(sanitizeBuilderEventPayload(store.EventQuestionsAnswered, raw), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["secret"]; ok {
		t.Errorf("a key off the whitelist was served: %v", got)
	}
	answers, _ := got["answers"].(map[string]any)
	if len(answers) != 1 || len([]rune(answers["users"].(string))) != builderAnswerEventChars+1 {
		t.Errorf("answers = %v", answers)
	}

	raw = json.RawMessage(`{"tool_call_id":"c1","version":2,"milestones":3,"milestone":1,"plan":{"summary":"x"}}`)
	got = nil
	if err := json.Unmarshal(sanitizeBuilderEventPayload(store.EventPlanApproved, raw), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["plan"]; ok || got["version"] != float64(2) {
		t.Errorf("plan_approved = %v", got)
	}
}

func TestBuilderPlanning_ApproveNamesTheProjectUnlessTheUserRenamedIt(t *testing.T) {
	e := newBHEnv(t)
	ctx := context.Background()
	titleOf := func(id string) (string, bool) {
		s, err := e.st.GetSession(ctx, e.owner.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		return s.Title, s.TitleLocked
	}

	open, openTurn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)
	if w := e.approve(e.owner, open.ID, openTurn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	if title, locked := titleOf(open.ID); title != "Clinic CRM" || locked {
		t.Errorf("title = %q locked=%v, want the plan's title and still unlocked", title, locked)
	}

	mine, mineTurn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)
	if _, err := e.st.RenameSession(ctx, e.owner.ID, mine.ID, "My clinic"); err != nil {
		t.Fatal(err)
	}
	if w := e.approve(e.owner, mine.ID, mineTurn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	if title, _ := titleOf(mine.ID); title != "My clinic" {
		t.Errorf("title = %q, a user rename must survive the approval", title)
	}
}

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

const (
	questionArgs = `{"questions":[{"id":"sign_in","title":"Sign-in","question":"Who signs in?","options":[{"label":"Team"},{"label":"Anyone"}]}]}`
	planArgs     = `{"summary":"A CRM","screens":[{"name":"Patients","purpose":"List"}],"assumptions":[],"milestones":[{"title":"List","deliverable":"See the list"},{"title":"Record","deliverable":"Open one"},{"title":"Notes","deliverable":"Add a note"}]}`
)

// briefingSession creates a session that starts in the briefing phase.
func (e *env) briefingSession(t *testing.T) *store.Session {
	t.Helper()
	s, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.userA, BackendAppID: e.appA, Title: "t",
		TemplateKind: store.TemplateDefault, TemplateVersion: "v0", Phase: store.PhaseBriefing,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return s
}

// pausedOn creates a turn on sess with opts, claims it and leaves it paused on
// a call to tool, the way the loop does.
func (e *env) pausedOn(t *testing.T, sess *store.Session, opts store.TurnOptions, tool, args string) *store.Turn {
	t.Helper()
	ctx := context.Background()
	if _, err := e.st.CreateTurnWith(ctx, sess.OwnerUserID, sess.ID, store.KindChat, "a crm for my clinic", opts); err != nil {
		t.Fatalf("CreateTurnWith: %v", err)
	}
	turn, err := e.st.ClaimTurn(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}
	e.pause(t, turn, tool, args)
	got, err := e.st.GetTurn(ctx, turn.ID)
	if err != nil || got.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("setup: turn not awaiting: %+v %v", got, err)
	}
	return got
}

// pause parks a claimed (running) turn on a call to tool.
func (e *env) pause(t *testing.T, turn *store.Turn, tool, args string) {
	t.Helper()
	ctx := context.Background()
	lease := store.Lease{TurnID: turn.ID, Owner: "w1"}
	calls := `[{"id":"call_1","name":"` + tool + `","arguments":` + args + `}]`
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleAssistant, ToolCalls: json.RawMessage(calls)}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	pending := json.RawMessage(`{"tool_call_id":"call_1","name":"` + tool + `","arguments":` + args + `}`)
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation, PendingProposal: pending}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
}

// reclaim claims the resumed turn again and parks it on another tool call.
func (e *env) reclaim(t *testing.T, tool, args string) *store.Turn {
	t.Helper()
	turn, err := e.st.ClaimTurn(context.Background(), "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}
	e.pause(t, turn, tool, args)
	return turn
}

// complete claims the pending turn and ends it with status.
func (e *env) complete(t *testing.T, status string) *store.Turn {
	t.Helper()
	ctx := context.Background()
	turn, err := e.st.ClaimTurn(ctx, "w1", 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}
	if err := e.st.FinishTurn(ctx, store.Lease{TurnID: turn.ID, Owner: "w1"}, store.FinishParams{Status: status}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	return turn
}

func approve(milestone int) func(context.Context, store.PendingProposal) (*store.PlanApproval, error) {
	return func(_ context.Context, p store.PendingProposal) (*store.PlanApproval, error) {
		return &store.PlanApproval{
			Plan: p.Arguments, Milestones: 3, Milestone: milestone,
			Content: func(v int) string { return "approved v" + string(rune('0'+v)) },
		}, nil
	}
}

func answered(content string) func(context.Context, store.PendingProposal) (*store.PlanningResolution, error) {
	return func(context.Context, store.PendingProposal) (*store.PlanningResolution, error) {
		return &store.PlanningResolution{Content: content, EventPayload: map[string]any{"skipped": false}}, nil
	}
}

func (e *env) lastToolMessage(t *testing.T, sessionID string) store.Message {
	t.Helper()
	msgs, err := e.st.ListMessages(context.Background(), sessionID)
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

func (e *env) phase(t *testing.T, sessionID string) string {
	t.Helper()
	s, err := e.st.GetSessionByID(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return s.Phase
}

func TestSessionPhaseAndTurnMode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Sessions default to building, and their turns to build mode.
	building := e.session(t, e.userA, e.appA)
	if building.Phase != store.PhaseBuilding {
		t.Fatalf("default phase = %q", building.Phase)
	}
	turn, err := e.st.CreateTurn(ctx, e.userA, building.ID, store.KindChat, "hi")
	if err != nil || turn.Mode != store.ModeBuild || turn.BriefVersion != nil || turn.Milestone != nil {
		t.Fatalf("default turn: %+v %v", turn, err)
	}
	e.complete(t, store.TurnCompleted)

	// "Plan first" makes one plan turn in a building session.
	turn, err = e.st.CreateTurnWith(ctx, e.userA, building.ID, store.KindChat, "plan it", store.TurnOptions{PlanFirst: true})
	if err != nil || turn.Mode != store.ModePlan {
		t.Fatalf("plan-first turn: %+v %v", turn, err)
	}
	e.complete(t, store.TurnCompleted)
	if got := e.phase(t, building.ID); got != store.PhaseBuilding {
		t.Errorf("plan first must not change the phase, got %q", got)
	}

	// With planning off, plan first is ignored.
	turn, err = e.st.CreateTurnWith(ctx, e.userA, building.ID, store.KindChat, "x", store.TurnOptions{PlanFirst: true, PlanningOff: true})
	if err != nil || turn.Mode != store.ModeBuild {
		t.Fatalf("planning off: %+v %v", turn, err)
	}
	e.complete(t, store.TurnCompleted)

	// A briefing session gives plan turns until planning is turned off, which
	// moves it to building.
	briefing := e.briefingSession(t)
	if briefing.Phase != store.PhaseBriefing {
		t.Fatalf("phase = %q", briefing.Phase)
	}
	turn, err = e.st.CreateTurn(ctx, e.userA, briefing.ID, store.KindChat, "a crm")
	if err != nil || turn.Mode != store.ModePlan {
		t.Fatalf("briefing turn: %+v %v", turn, err)
	}
	e.complete(t, store.TurnFailed)
	turn, err = e.st.CreateTurnWith(ctx, e.userA, briefing.ID, store.KindChat, "go", store.TurnOptions{PlanningOff: true})
	if err != nil || turn.Mode != store.ModeBuild {
		t.Fatalf("turn after planning was turned off: %+v %v", turn, err)
	}
	if got := e.phase(t, briefing.ID); got != store.PhaseBuilding {
		t.Errorf("planning off must move a briefing session to building, got %q", got)
	}

	bad := store.CreateSessionParams{OwnerUserID: e.userA, BackendAppID: e.appA, TemplateKind: store.TemplateDefault, TemplateVersion: "v0", Phase: "thinking"}
	if _, err := e.st.CreateSession(ctx, bad); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("unknown phase: %v", err)
	}
}

func TestAnswerQuestions_ResumesInPlanModeWithMarkers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.briefingSession(t)
	turn := e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolAskUser, questionArgs)

	var asked store.PendingProposal
	got, err := e.st.AnswerQuestions(ctx, e.userA, sess.ID, turn.ID,
		func(_ context.Context, p store.PendingProposal) (*store.PlanningResolution, error) {
			asked = p
			return &store.PlanningResolution{Content: "The user answered: Team", EventPayload: map[string]any{"skipped": false}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if asked.Name != store.PlanningToolAskUser || !sameJSON(asked.Arguments, questionArgs) {
		t.Errorf("decide must receive the questions: %+v", asked)
	}
	if got.Status != store.TurnPending || got.Mode != store.ModePlan || got.PendingProposal != nil {
		t.Fatalf("resumed turn: %+v", got)
	}
	msg := e.lastToolMessage(t, sess.ID)
	if msg.Content != "The user answered: Team" || msg.ToolCallID != "call_1" || !sameJSON(msg.ToolCalls, `{"build_failed":false,"is_error":false}`) {
		t.Errorf("tool message: %+v %s", msg, msg.ToolCalls)
	}
	if e.phase(t, sess.ID) != store.PhaseBriefing {
		t.Error("answering must not leave the briefing phase")
	}
	events, err := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if last := events[len(events)-1]; last.Type != store.EventQuestionsAnswered || !sameJSON(last.Payload, `{"skipped":false}`) {
		t.Errorf("last event: %s %s", last.Type, last.Payload)
	}

	// A skipped round is marked in the tool metadata, where the loop reads it.
	second := e.reclaim(t, store.PlanningToolAskUser, questionArgs)
	if _, err := e.st.AnswerQuestions(ctx, e.userA, sess.ID, second.ID,
		func(context.Context, store.PendingProposal) (*store.PlanningResolution, error) {
			return &store.PlanningResolution{Content: "skipped", QuestionsClosed: true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	if msg := e.lastToolMessage(t, sess.ID); !sameJSON(msg.ToolCalls, `{"build_failed":false,"is_error":false,"questions_closed":true}`) {
		t.Errorf("skip marker: %s", msg.ToolCalls)
	}
}

func TestReviseBuildPlan_StoresNothingAndCanReopenQuestions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.briefingSession(t)
	turn := e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolProposeBuildPlan, planArgs)

	got, err := e.st.ReviseBuildPlan(ctx, e.userA, sess.ID, turn.ID,
		func(context.Context, store.PendingProposal) (*store.PlanningResolution, error) {
			return &store.PlanningResolution{Content: "more questions", EventPayload: map[string]any{"more_questions": true}, QuestionsReopened: true}, nil
		})
	if err != nil || got.Status != store.TurnPending || got.Mode != store.ModePlan {
		t.Fatalf("revised turn: %+v %v", got, err)
	}
	if msg := e.lastToolMessage(t, sess.ID); !sameJSON(msg.ToolCalls, `{"build_failed":false,"is_error":false,"questions_reopened":true}`) {
		t.Errorf("reopen marker: %s", msg.ToolCalls)
	}
	if b, err := e.st.CurrentBrief(ctx, sess.ID); err != nil || b != nil {
		t.Errorf("a revision must not store a brief: %+v %v", b, err)
	}
	if e.phase(t, sess.ID) != store.PhaseBriefing {
		t.Error("a revision must not leave the briefing phase")
	}
}

func TestApproveBuildPlan_IsAtomicAndStartsTheBuild(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.briefingSession(t)
	turn := e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolProposeBuildPlan, planArgs)

	// A failure after the brief row was written (here the tool message, whose
	// content Postgres refuses) rolls everything back.
	_, _, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turn.ID,
		func(_ context.Context, p store.PendingProposal) (*store.PlanApproval, error) {
			return &store.PlanApproval{Plan: p.Arguments, Milestones: 3, Milestone: 1, Content: func(int) string { return "bad\x00content" }}, nil
		})
	if err == nil {
		t.Fatal("an approval whose tool message cannot be stored must fail")
	}
	if b, berr := e.st.CurrentBrief(ctx, sess.ID); berr != nil || b != nil {
		t.Fatalf("failed approval left a brief: %+v %v", b, berr)
	}
	after, err := e.st.GetTurn(ctx, turn.ID)
	if err != nil || after.Status != store.TurnAwaitingConfirmation || after.Mode != store.ModePlan || after.BriefVersion != nil {
		t.Fatalf("failed approval changed the turn: %+v %v", after, err)
	}
	if e.phase(t, sess.ID) != store.PhaseBriefing {
		t.Fatal("failed approval changed the phase")
	}

	// decide errors are returned unchanged and leave the turn paused.
	boom := errors.New("boom")
	if _, _, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turn.ID,
		func(context.Context, store.PendingProposal) (*store.PlanApproval, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("decide error: %v", err)
	}

	got, brief, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turn.ID, approve(1))
	if err != nil {
		t.Fatal(err)
	}
	if brief.Version != 1 || !sameJSON(brief.Plan, planArgs) || brief.CreatedBy != e.userA || brief.TurnID == nil || *brief.TurnID != turn.ID {
		t.Errorf("brief: %+v", brief)
	}
	if got.Status != store.TurnPending || got.Mode != store.ModeBuild || got.BriefVersion == nil || *got.BriefVersion != 1 ||
		got.Milestone == nil || *got.Milestone != 1 || got.PendingProposal != nil {
		t.Fatalf("approved turn must be a pending build turn on milestone 1 of v1: %+v", got)
	}
	if e.phase(t, sess.ID) != store.PhaseBuilding {
		t.Error("approval must move the session to building")
	}
	if msg := e.lastToolMessage(t, sess.ID); msg.Content != "approved v1" || msg.ToolCallID != "call_1" {
		t.Errorf("tool message: %+v", msg)
	}
	events, err := e.st.ListEvents(ctx, e.userA, sess.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if last := events[len(events)-1]; last.Type != store.EventPlanApproved || !sameJSON(last.Payload, `{"tool_call_id":"call_1","version":1,"milestones":3,"milestone":1}`) {
		t.Errorf("last event: %s %s", last.Type, last.Payload)
	}

	// Approving again finds nothing to approve.
	if _, _, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turn.ID, approve(1)); !errors.Is(err, store.ErrProposalNotPending) {
		t.Errorf("second approval: %v", err)
	}
	// The next turn of the session is a build turn.
	e.complete(t, store.TurnCompleted)
	next, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "go on")
	if err != nil || next.Mode != store.ModeBuild {
		t.Fatalf("turn after approval: %+v %v", next, err)
	}
}

// Each resolution handles one tool; the others answer ErrProposalKind and
// leave the turn paused. Ownership is checked before anything else.
func TestPlanningResolutions_WrongToolAndForeignOwner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	noDecide := func(context.Context, store.PendingProposal) (*store.PlanningResolution, error) {
		t.Error("decide must not run")
		return nil, nil
	}
	noApprove := func(context.Context, store.PendingProposal) (*store.PlanApproval, error) {
		t.Error("decide must not run")
		return nil, nil
	}
	noConfirm := func(context.Context, store.PendingProposal) (*store.ProposalResolution, error) {
		t.Error("decide must not run")
		return nil, nil
	}

	sess := e.briefingSession(t)
	turn := e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolAskUser, questionArgs)
	if _, err := e.st.ReviseBuildPlan(ctx, e.userA, sess.ID, turn.ID, noDecide); !errors.Is(err, store.ErrProposalKind) {
		t.Errorf("revise on a question round: %v", err)
	}
	if _, _, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turn.ID, noApprove); !errors.Is(err, store.ErrProposalKind) {
		t.Errorf("approve on a question round: %v", err)
	}
	if _, err := e.st.ResolveProposal(ctx, e.userA, sess.ID, turn.ID, noConfirm); !errors.Is(err, store.ErrProposalKind) {
		t.Errorf("backend resolution on a question round: %v", err)
	}
	if _, err := e.st.AnswerQuestions(ctx, e.userB, sess.ID, turn.ID, noDecide); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another user's turn: %v", err)
	}
	if got, _ := e.st.GetTurn(ctx, turn.ID); got.Status != store.TurnAwaitingConfirmation {
		t.Fatalf("refused resolutions must leave the turn paused: %+v", got)
	}
	if _, err := e.st.AnswerQuestions(ctx, e.userA, sess.ID, turn.ID, answered("ok")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AnswerQuestions(ctx, e.userA, sess.ID, turn.ID, noDecide); !errors.Is(err, store.ErrProposalNotPending) {
		t.Errorf("answering twice: %v", err)
	}

	// A backend proposal is not a planning proposal.
	backend := e.reclaim(t, "propose_backend_change", proposalArgs)
	if _, err := e.st.AnswerQuestions(ctx, e.userA, sess.ID, backend.ID, noDecide); !errors.Is(err, store.ErrProposalKind) {
		t.Errorf("answers on a backend proposal: %v", err)
	}
	if _, _, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, backend.ID, noApprove); !errors.Is(err, store.ErrProposalKind) {
		t.Errorf("plan approval on a backend proposal: %v", err)
	}
}

func TestApproveBuildPlan_ConcurrentApprovalsStoreOneVersion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.briefingSession(t)
	turn := e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolProposeBuildPlan, planArgs)

	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = e.st.ApproveBuildPlan(context.Background(), e.userA, sess.ID, turn.ID, approve(1))
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, store.ErrProposalNotPending), errors.Is(err, store.ErrProposalBusy):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d approvals succeeded, want 1", ok)
	}
	versions, err := e.st.ListBriefVersions(ctx, sess.ID)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("versions: %+v %v", versions, err)
	}
}

func TestMilestoneProgressAndMilestoneTurns(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sess := e.briefingSession(t)

	if b, states, err := e.st.MilestoneProgress(ctx, sess.ID); err != nil || b != nil || states != nil {
		t.Fatalf("no brief: %+v %+v %v", b, states, err)
	}
	// Without a brief there is no milestone to build.
	e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolProposeBuildPlan, planArgs)
	turns, _ := e.st.ListTurns(ctx, e.userA, sess.ID)
	if _, _, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turns[0].ID, approve(1)); err != nil {
		t.Fatal(err)
	}

	states := func() []store.MilestoneState {
		t.Helper()
		_, st, err := e.st.MilestoneProgress(ctx, sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	// The approval turn is still running milestone 1: nothing is done yet.
	if st := states(); len(st) != 3 || st[0].Done || st[0].Title != "List" || st[0].Index != 1 || store.NextPendingMilestone(st) != 1 {
		t.Fatalf("progress before the first build: %+v", st)
	}
	// A turn that does not end completed leaves its milestone pending.
	e.complete(t, store.TurnLimitReached)
	if st := states(); st[0].Done {
		t.Fatalf("a turn that hit a limit must not finish its milestone: %+v", st)
	}

	// Only the next pending milestone is accepted.
	for _, m := range []int{2, 3, 4} {
		if _, err := e.st.CreateTurnWith(ctx, e.userA, sess.ID, store.KindChat, "continue", store.TurnOptions{Milestone: m}); !errors.Is(err, store.ErrMilestoneMismatch) {
			t.Errorf("milestone %d: %v", m, err)
		}
	}
	if _, err := e.st.CreateTurnWith(ctx, e.userA, sess.ID, store.KindChat, "x", store.TurnOptions{Milestone: 1, PlanFirst: true}); !errors.Is(err, store.ErrMilestoneMismatch) {
		t.Errorf("a plan turn cannot build a milestone: %v", err)
	}
	if _, err := e.st.CreateTurnWith(ctx, e.userA, sess.ID, store.KindChat, "x", store.TurnOptions{Milestone: -1}); !errors.Is(err, store.ErrInvalidArgument) {
		t.Errorf("negative milestone: %v", err)
	}
	turn, err := e.st.CreateTurnWith(ctx, e.userA, sess.ID, store.KindChat, "continue", store.TurnOptions{Milestone: 1})
	if err != nil || turn.Mode != store.ModeBuild || turn.BriefVersion == nil || *turn.BriefVersion != 1 || *turn.Milestone != 1 {
		t.Fatalf("milestone turn: %+v %v", turn, err)
	}
	e.complete(t, store.TurnCompleted)
	if st := states(); !st[0].Done || st[1].Done || store.NextPendingMilestone(st) != 2 {
		t.Fatalf("progress after milestone 1: %+v", st)
	}

	// A turn without a milestone changes nothing.
	if _, err := e.st.CreateTurn(ctx, e.userA, sess.ID, store.KindChat, "make it blue"); err != nil {
		t.Fatal(err)
	}
	e.complete(t, store.TurnCompleted)
	if st := states(); store.NextPendingMilestone(st) != 2 {
		t.Fatalf("a free turn advanced the plan: %+v", st)
	}

	// A revised plan is a new version: progress comes from its own done marks.
	revised := `{"summary":"A CRM","screens":[{"name":"Patients","purpose":"List"}],"assumptions":[],"milestones":[{"title":"List","deliverable":"d","done":true},{"title":"Record","deliverable":"d","done":true},{"title":"Export","deliverable":"d"}]}`
	e.pausedOn(t, sess, store.TurnOptions{}, store.PlanningToolProposeBuildPlan, revised)
	turns, _ = e.st.ListTurns(ctx, e.userA, sess.ID)
	_, brief, err := e.st.ApproveBuildPlan(ctx, e.userA, sess.ID, turns[len(turns)-1].ID, approve(3))
	if err != nil || brief.Version != 2 {
		t.Fatalf("second version: %+v %v", brief, err)
	}
	if st := states(); !st[0].Done || !st[1].Done || st[2].Done || st[2].Title != "Export" {
		t.Fatalf("progress of v2: %+v", st)
	}
	e.complete(t, store.TurnCompleted)
	if st := states(); store.NextPendingMilestone(st) != 0 {
		t.Fatalf("every milestone of v2 is done: %+v", st)
	}
	if _, err := e.st.CreateTurnWith(ctx, e.userA, sess.ID, store.KindChat, "continue", store.TurnOptions{Milestone: 3}); !errors.Is(err, store.ErrMilestoneMismatch) {
		t.Errorf("nothing left to build: %v", err)
	}

	versions, err := e.st.ListBriefVersions(ctx, sess.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("versions newest first: %+v %v", versions, err)
	}
	v1, err := e.st.GetBrief(ctx, sess.ID, 1)
	if err != nil || !sameJSON(v1.Plan, planArgs) {
		t.Fatalf("v1: %+v %v", v1, err)
	}
	if _, err := e.st.GetBrief(ctx, sess.ID, 9); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing version: %v", err)
	}
}

func TestRecordUsage_PlanningPurpose(t *testing.T) {
	e := newEnv(t)
	sess := e.session(t, e.userA, e.appA)
	err := e.st.RecordUsage(context.Background(), store.UsageRecord{
		UserID: e.userA, Feature: store.FeatureBuilder, SessionID: sess.ID, Provider: "openai", Model: "m",
		InputTokens: 10, OutputTokens: 5, Purpose: store.PurposePlanning, Reported: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var purpose string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT purpose FROM zeep_system.ai_usage_ledger WHERE session_id = $1`, sess.ID).Scan(&purpose); err != nil || purpose != "planning" {
		t.Fatalf("purpose = %q %v", purpose, err)
	}
}

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

const testBuildPlan = `{"title":"Clinic CRM","summary":"A CRM for a clinic.","screens":[{"name":"Patients","purpose":"List patients"}],"look":{"direction":"calm and clinical"},"assumptions":["Email sign-in"],` +
	`"milestones":[{"title":"Patient list","deliverable":"See the list"},{"title":"Patient record","deliverable":"Open one"}]}`

func setPlanningMode(t *testing.T, e *bhEnv, mode string) {
	t.Helper()
	if _, err := UpsertBuilderSettings(context.Background(), e.pool, e.owner.ID, BuilderSettingsInput{PlanningMode: &mode}); err != nil {
		t.Fatalf("set planning_mode %s: %v", mode, err)
	}
}

// ABP-01, ABP-17: a session created while planning is required starts in the
// briefing phase; with suggested or off it starts building.
func TestBuilderCreate_PhaseFollowsPlanningMode(t *testing.T) {
	e := newBHEnv(t)
	create := func() BuilderSessionView {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(fmt.Sprintf(`{"backend_app_id":%q}`, e.appA)))
		w := httptest.NewRecorder()
		e.h.CreateSession(w, withUser(req, e.owner))
		if w.Code != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
		}
		return decodeJSON[BuilderSessionView](t, w)
	}

	// The default of the setting is required.
	if got := create(); got.Phase != store.PhaseBriefing {
		t.Fatalf("default planning mode: phase = %q, want briefing", got.Phase)
	}
	for mode, want := range map[string]string{
		BuilderPlanningSuggested: store.PhaseBuilding,
		BuilderPlanningOff:       store.PhaseBuilding,
		BuilderPlanningRequired:  store.PhaseBriefing,
	} {
		setPlanningMode(t, e, mode)
		got := create()
		if got.Phase != want {
			t.Errorf("planning_mode %s: phase = %q, want %q", mode, got.Phase, want)
		}
		sess, err := e.st.GetSessionByID(context.Background(), got.ID)
		if err != nil || sess.Phase != want {
			t.Errorf("planning_mode %s: stored phase = %v (%v)", mode, sess, err)
		}
	}
}

func TestBuilderRuntime_PlanningToolsRegisteredAndEnabledBySetting(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	super := mustCreateSuperadmin(t, rt.pool)
	ctx := context.Background()

	reg, err := newBuilderToolRegistry(rt.pool, rt.sandbox, rt.store, rt.skills, quietSlog())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{planning.ToolAskUser, planning.ToolProposeBuildPlan} {
		if k, ok := reg.Kind(name); !ok || k != tools.KindPlanning {
			t.Errorf("%s: kind %v registered=%v, want planning", name, k, ok)
		}
	}
	// The backend proposals keep their own kind: a planning turn refuses them.
	if k, _ := reg.Kind(ToolProposeBackendPlan); k != tools.KindProposal {
		t.Errorf("propose_backend_plan kind = %v", k)
	}

	base := agent.Deps{Provider: unavailableProvider{}}
	if d := rt.prepareDeps(ctx, base); !d.PlanningEnabled {
		t.Error("planning is enabled by default (required)")
	}
	for mode, want := range map[string]bool{BuilderPlanningSuggested: true, BuilderPlanningOff: false, BuilderPlanningRequired: true} {
		m := mode
		if _, err := UpsertBuilderSettings(ctx, rt.pool, super.ID, BuilderSettingsInput{PlanningMode: &m}); err != nil {
			t.Fatal(err)
		}
		if d := rt.prepareDeps(ctx, base); d.PlanningEnabled != want {
			t.Errorf("planning_mode %s: PlanningEnabled = %v, want %v", mode, d.PlanningEnabled, want)
		}
	}
}

// approveTestPlan leaves sess with an approved brief: a turn paused on
// propose_build_plan, approved, and ended with status.
func approveTestPlan(t *testing.T, st *store.Store, sess *store.Session, status string) *store.Brief {
	t.Helper()
	ctx := context.Background()
	if _, err := st.CreateTurn(ctx, sess.OwnerUserID, sess.ID, store.KindChat, "a crm"); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	claim := func() store.Lease {
		turn, err := st.ClaimTurn(ctx, "w1", 30*time.Second)
		if err != nil || turn.SessionID != sess.ID {
			t.Fatalf("ClaimTurn: %+v %v", turn, err)
		}
		return store.Lease{TurnID: turn.ID, Owner: "w1"}
	}
	lease := claim()
	calls := `[{"id":"p1","name":"propose_build_plan","arguments":` + testBuildPlan + `}]`
	if _, err := st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleAssistant, ToolCalls: json.RawMessage(calls)}); err != nil {
		t.Fatal(err)
	}
	pending := json.RawMessage(`{"tool_call_id":"p1","name":"propose_build_plan","arguments":` + testBuildPlan + `}`)
	if err := st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation, PendingProposal: pending}); err != nil {
		t.Fatal(err)
	}
	_, brief, err := st.ApproveBuildPlan(ctx, sess.OwnerUserID, sess.ID, lease.TurnID,
		func(_ context.Context, p store.PendingProposal) (*store.PlanApproval, error) {
			return &store.PlanApproval{Plan: p.Arguments, Milestones: 2, Milestone: 1, Content: func(int) string { return "approved" }}, nil
		})
	if err != nil {
		t.Fatalf("ApproveBuildPlan: %v", err)
	}
	if err := st.FinishTurn(ctx, claim(), store.FinishParams{Status: status}); err != nil {
		t.Fatal(err)
	}
	return brief
}

// ABP-12: the prompt source hands the loop the current brief, what is done and
// the workspace copy of the brief file.
func TestBuilderRuntime_PromptSourceBrief(t *testing.T) {
	// The runtime helper resets the tables the handler env seeds: build it first.
	rt := newTestBuilderRuntime(t)
	e := newBHEnv(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = rt.pool.Exec(context.Background(), `TRUNCATE zeep_system.builder_sessions CASCADE`)
	})

	sess, err := rt.store.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "t",
		TemplateKind: store.TemplateDefault, TemplateVersion: "v0", Phase: store.PhaseBriefing,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A briefing session has no brief yet.
	rt.sandbox = archiveSandbox{data: tarOf(t, map[string]string{"src/main.tsx": "x"}, []string{"src/main.tsx"})}
	in, err := rt.promptSource(ctx, sess)
	if err != nil || in.Brief != nil {
		t.Fatalf("no brief yet: %+v %v", in.Brief, err)
	}

	approveTestPlan(t, rt.store, sess, store.TurnCompleted)
	file := "# App brief (version 1)\nstale"
	rt.sandbox = archiveSandbox{data: tarOf(t,
		map[string]string{planning.BriefFileName: file, "src/main.tsx": "x"},
		[]string{planning.BriefFileName, "src/main.tsx"})}
	in, err = rt.promptSource(ctx, sess)
	if err != nil || in.Brief == nil {
		t.Fatalf("brief missing: %+v %v", in.Brief, err)
	}
	b := in.Brief
	if b.Version != 1 || b.Plan.Summary != "A CRM for a clinic." || len(b.Plan.Milestones) != 2 {
		t.Errorf("brief = %+v", b)
	}
	// The approval turn built milestone 1 and ended completed.
	if !b.Done[1] || b.Done[2] {
		t.Errorf("done = %v, want only milestone 1", b.Done)
	}
	if b.WorkspaceFile != file {
		t.Errorf("workspace file = %q", b.WorkspaceFile)
	}
	if !strings.Contains(in.Session.FileTree, planning.BriefFileName) {
		t.Errorf("the brief file is part of the tree: %q", in.Session.FileTree)
	}

	// The workspace cannot be read: the brief still reaches the prompt, with no
	// workspace copy (the loop then rewrites the file).
	rt.sandbox = archiveSandbox{err: fmt.Errorf("builder down")}
	in, err = rt.promptSource(ctx, sess)
	if err != nil || in.Brief == nil || in.Brief.WorkspaceFile != "" {
		t.Fatalf("brief with an unreadable workspace: %+v %v", in.Brief, err)
	}

	// A stored plan that no longer validates is skipped, not fatal.
	if _, err := rt.pool.Exec(ctx, `UPDATE zeep_system.builder_briefs SET plan = '{"summary":""}'::jsonb WHERE session_id = $1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	in, err = rt.promptSource(ctx, sess)
	if err != nil || in.Brief != nil {
		t.Fatalf("an invalid stored plan must be skipped: %+v %v", in.Brief, err)
	}
}

func TestScanWorkspaceFiles_BriefFile(t *testing.T) {
	big := strings.Repeat("x", maxBriefFileBytes+100)
	scan, err := scanWorkspaceFiles(strings.NewReader(string(tarOf(t,
		map[string]string{"ORBIT_AGENT.md": "guide", planning.BriefFileName: big, "docs/" + planning.BriefFileName: "nested"},
		[]string{"ORBIT_AGENT.md", planning.BriefFileName, "docs/" + planning.BriefFileName}))))
	if err != nil {
		t.Fatal(err)
	}
	if scan.guide != "guide" || len(scan.brief) != maxBriefFileBytes+1 {
		t.Errorf("guide = %q, brief length = %d (want the cap plus one)", scan.guide, len(scan.brief))
	}
	if !strings.Contains(scan.tree, "docs/"+planning.BriefFileName) {
		t.Errorf("tree = %q", scan.tree)
	}
}

// builder-no-backend T4: the prompt source of a session with no backend says
// so and does not look up a backend app.
func TestBuilderRuntime_PromptSourceNoBackend(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	e := newBHEnv(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = rt.pool.Exec(context.Background(), `TRUNCATE zeep_system.builder_sessions CASCADE`)
	})
	sess, err := rt.store.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, Title: "t", TemplateKind: store.TemplateDefault, TemplateVersion: "v0",
		BackendOrigin: store.BackendOriginNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.sandbox = archiveSandbox{data: tarOf(t, map[string]string{"src/main.tsx": "x"}, []string{"src/main.tsx"})}
	in, err := rt.promptSource(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Session.NoBackend || in.Session.BackendAppID != "" || in.Session.BackendAppName != "" {
		t.Errorf("session context = %+v, want NoBackend and no app", in.Session)
	}
}

// builder-no-backend T6: a session with no backend does not list the built-in
// skills written for backend screens; a session with one still does.
func TestBuilderRuntime_PromptSourceSkillsIndexByBackend(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	e := newBHEnv(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = rt.pool.Exec(context.Background(), `TRUNCATE zeep_system.builder_sessions CASCADE`)
	})
	rt.sandbox = archiveSandbox{data: tarOf(t, map[string]string{"src/main.tsx": "x"}, []string{"src/main.tsx"})}

	withBackend, err := rt.store.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "t", TemplateKind: store.TemplateDefault, TemplateVersion: "v0",
	})
	if err != nil {
		t.Fatal(err)
	}
	noBackend, err := rt.store.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, Title: "t", TemplateKind: store.TemplateDefault, TemplateVersion: "v0",
		BackendOrigin: store.BackendOriginNone,
	})
	if err != nil {
		t.Fatal(err)
	}

	in, err := rt.promptSource(ctx, withBackend)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"orbit-crud-screens", "orbit-forms-and-states", "orbit-frontend-design"} {
		if !strings.Contains(in.SkillsIndex, name) {
			t.Errorf("a session with a backend must list %s:\n%s", name, in.SkillsIndex)
		}
	}
	in, err = rt.promptSource(ctx, noBackend)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"orbit-crud-screens", "orbit-forms-and-states"} {
		if strings.Contains(in.SkillsIndex, name) {
			t.Errorf("a session with no backend must not list %s", name)
		}
	}
	for _, name := range []string{"orbit-frontend-design", "orbit-accessibility", "orbit-responsive-layout"} {
		if !strings.Contains(in.SkillsIndex, name) {
			t.Errorf("a session with no backend must still list %s", name)
		}
	}
}

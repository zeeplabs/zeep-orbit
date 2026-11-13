package dashboard

// builder_brief_test.go — ai-builder-planning P07: GET brief, GET brief
// version and GET plan estimate (design §8, §9). Integration tests against a
// real Postgres; the estimate sample is seeded with SQL. The percentiles
// themselves are tested in internal/agent/store/estimate_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// testBuildPlanV2 revises testBuildPlan: the first milestone is already built
// and a third one is added.
const testBuildPlanV2 = `{"summary":"A CRM for a clinic, with a calendar.","screens":[{"name":"Patients","purpose":"List patients"}],"assumptions":["Email sign-in"],` +
	`"milestones":[{"title":"Patient list","deliverable":"See the list","done":true},{"title":"Patient record","deliverable":"Open one"},{"title":"Calendar","deliverable":"Book a visit"}]}`

// callGet calls fn as user with a GET and several chi URL params.
func callGet(fn http.HandlerFunc, user *DashboardUser, params ...string) *httptest.ResponseRecorder {
	req := withUser(httptest.NewRequest(http.MethodGet, "/x", nil), user)
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(params); i += 2 {
		rctx.URLParams.Add(params[i], params[i+1])
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return out
}

// finishClaimed claims turnID and finishes it completed, as the loop does
// after a build.
func (e *bhEnv) finishClaimed(t *testing.T, turnID string) {
	t.Helper()
	ctx := context.Background()
	claimed, err := e.st.ClaimTurn(ctx, "w1", time.Minute)
	if err != nil || claimed.ID != turnID {
		t.Fatalf("ClaimTurn = %v, %v; want %s", claimed, err, turnID)
	}
	if err := e.st.FinishTurn(ctx, store.Lease{TurnID: turnID, Owner: "w1"}, store.FinishParams{Status: store.TurnCompleted}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
}

func TestBuilderBrief_NoBriefIsVersionZero(t *testing.T) {
	e := newBHEnv(t)
	sess, _ := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_q1", testQuestions)

	w := callGet(e.h.GetBrief, e.owner, "id", sess.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("GET brief = %d %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != `{"version":0,"plan":null,"created_at":null,"milestones":[],"versions":[]}`+"\n" {
		t.Fatalf("body = %s", got)
	}
}

// Two approvals give two versions, listed newest first. The progress of the
// current one counts a milestone the plan marks done and one a completed turn
// built; a stored version can be read back.
func TestBuilderBrief_VersionsAndProgress(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve v1 = %d %s", w.Code, w.Body.String())
	}
	e.pauseOn(t, turn.ID, planning.ToolProposeBuildPlan, "call_p2", testBuildPlanV2)
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p2"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve v2 = %d %s", w.Code, w.Body.String())
	}
	// The same turn builds milestone 2 of version 2 and completes.
	e.finishClaimed(t, turn.ID)

	w := callGet(e.h.GetBrief, e.owner, "id", sess.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("GET brief = %d %s", w.Code, w.Body.String())
	}
	brief := decodeBody[BuilderBriefView](t, w)
	if brief.Version != 2 || brief.CreatedAt == nil {
		t.Fatalf("brief = %+v, want version 2 with a date", brief)
	}
	var plan planning.Plan
	if err := json.Unmarshal(brief.Plan, &plan); err != nil || plan.Summary != "A CRM for a clinic, with a calendar." {
		t.Fatalf("plan = %s (%v)", brief.Plan, err)
	}
	want := []BuilderBriefMilestoneView{
		{Index: 1, Title: "Patient list", Done: true},
		{Index: 2, Title: "Patient record", Done: true},
		{Index: 3, Title: "Calendar", Done: false},
	}
	if len(brief.Milestones) != len(want) {
		t.Fatalf("milestones = %+v", brief.Milestones)
	}
	for i := range want {
		if brief.Milestones[i] != want[i] {
			t.Fatalf("milestone %d = %+v, want %+v", i+1, brief.Milestones[i], want[i])
		}
	}
	if len(brief.Versions) != 2 || brief.Versions[0].Version != 2 || brief.Versions[1].Version != 1 {
		t.Fatalf("versions = %+v, want 2 then 1", brief.Versions)
	}

	w = callGet(e.h.GetBriefVersion, e.owner, "id", sess.ID, "v", "1")
	if w.Code != http.StatusOK {
		t.Fatalf("GET brief v1 = %d %s", w.Code, w.Body.String())
	}
	v1 := decodeBody[BuilderBriefVersionDetail](t, w)
	if err := json.Unmarshal(v1.Plan, &plan); err != nil || v1.Version != 1 || plan.Summary != "A CRM for a clinic." || len(plan.Milestones) != 2 {
		t.Fatalf("v1 = %+v (%v)", v1, err)
	}
}

func TestBuilderBrief_NotFoundCases(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve = %d %s", w.Code, w.Body.String())
	}
	for _, v := range []string{"0", "-1", "abc", "2"} {
		if w := callGet(e.h.GetBriefVersion, e.owner, "id", sess.ID, "v", v); w.Code != http.StatusNotFound {
			t.Fatalf("GET brief version %q = %d, want 404", v, w.Code)
		}
	}
	// Another user's session is not found, for every read.
	if w := callGet(e.h.GetBrief, e.other, "id", sess.ID); w.Code != http.StatusNotFound {
		t.Fatalf("GET brief as another user = %d, want 404", w.Code)
	}
	if w := callGet(e.h.GetBriefVersion, e.other, "id", sess.ID, "v", "1"); w.Code != http.StatusNotFound {
		t.Fatalf("GET brief version as another user = %d, want 404", w.Code)
	}
	if w := callGet(e.h.PlanEstimate, e.other, "id", sess.ID, "turn", turn.ID); w.Code != http.StatusNotFound {
		t.Fatalf("GET estimate as another user = %d, want 404", w.Code)
	}
}

// seedBuildTurns inserts n completed build turns of the configured provider
// and model on a session of e.other; turn i (1-based) spent i*100 tokens and
// cost i*1000 micros.
func (e *bhEnv) seedBuildTurns(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	sess, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.other.ID, BackendAppID: e.appB, Title: "past", TemplateKind: store.TemplateDefault, TemplateVersion: "v0",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	at := time.Now().Add(-time.Hour)
	for i := 1; i <= n; i++ {
		var id string
		if err := e.pool.QueryRow(ctx,
			`INSERT INTO zeep_system.builder_turns (session_id, seq, status, mode, created_at)
			 VALUES ($1, $2, 'completed', 'build', $3) RETURNING id`, sess.ID, i, at).Scan(&id); err != nil {
			t.Fatalf("insert turn: %v", err)
		}
		if _, err := e.pool.Exec(ctx,
			`INSERT INTO zeep_system.ai_usage_ledger
			   (user_id, feature, session_id, turn_id, provider, model, input_tokens, output_tokens, cost_micros, created_at)
			 VALUES ($1, 'builder', $2, $3, $4, 'test-model', $5, $6, $7, $8)`,
			e.other.ID, sess.ID, id, aiProviderOpenAICompatible, i*70, i*30, i*1000, at); err != nil {
			t.Fatalf("insert call: %v", err)
		}
	}
}

func TestBuilderPlanEstimate_NeedsAPendingBuildPlan(t *testing.T) {
	e := newBHEnv(t)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolAskUser, "call_q1", testQuestions)
	if w := callGet(e.h.PlanEstimate, e.owner, "id", sess.ID, "turn", turn.ID); w.Code != http.StatusConflict {
		t.Fatalf("estimate on a question round = %d, want 409", w.Code)
	}
	// After the plan was approved the turn is no longer paused.
	sess, turn = e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlan)
	if w := e.approve(e.owner, sess.ID, turn.ID, `{"tool_call_id":"call_p1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("approve = %d %s", w.Code, w.Body.String())
	}
	if w := callGet(e.h.PlanEstimate, e.owner, "id", sess.ID, "turn", turn.ID); w.Code != http.StatusConflict {
		t.Fatalf("estimate after approval = %d, want 409", w.Code)
	}
}

func TestBuilderPlanEstimate_UnavailableWithoutASample(t *testing.T) {
	e := newBHEnv(t)
	e.seedBuildTurns(t, store.EstimateMinSample-1)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlanV2)

	w := callGet(e.h.PlanEstimate, e.owner, "id", sess.ID, "turn", turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("estimate = %d %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != `{"available":false,"sample_turns":9,"currency":"USD","milestones":2,"milestone":null,"plan":null}`+"\n" {
		t.Fatalf("body = %s", got)
	}
}

// The milestone range is the 25th..75th percentile of the seeded turns, and
// the whole plan is that range times the milestones that are not done.
func TestBuilderPlanEstimate_RangeForTheMilestoneAndThePlan(t *testing.T) {
	e := newBHEnv(t)
	e.seedBuildTurns(t, 10)
	sess, turn := e.pausePlanning(t, store.PhaseBriefing, planning.ToolProposeBuildPlan, "call_p1", testBuildPlanV2)

	w := callGet(e.h.PlanEstimate, e.owner, "id", sess.ID, "turn", turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("estimate = %d %s", w.Code, w.Body.String())
	}
	got := decodeBody[BuilderPlanEstimate](t, w)
	if !got.Available || got.SampleTurns != 10 || got.Currency != "USD" || got.Milestones != 2 || got.Milestone == nil || got.Plan == nil {
		t.Fatalf("estimate = %+v", got)
	}
	m, p := *got.Milestone, *got.Plan
	if m.TokensLow != 325 || m.TokensHigh != 775 || m.CostMicrosLow == nil || *m.CostMicrosLow != 3250 || *m.CostMicrosHigh != 7750 {
		t.Fatalf("milestone range = %+v", m)
	}
	if p.TokensLow != 650 || p.TokensHigh != 1550 || p.CostMicrosLow == nil || *p.CostMicrosLow != 6500 || *p.CostMicrosHigh != 15500 {
		t.Fatalf("plan range = %+v", p)
	}

	// With the provider disabled there is no model to estimate for.
	baseURL := "https://llm.internal.example/v1"
	if _, err := UpsertAIProvider(context.Background(), e.pool, aiProviderOpenAICompatible,
		&aiProviderUpsertInput{Model: "test-model", Enabled: false, BaseURL: &baseURL}); err != nil {
		t.Fatalf("disable provider: %v", err)
	}
	w = callGet(e.h.PlanEstimate, e.owner, "id", sess.ID, "turn", turn.ID)
	if got := decodeBody[BuilderPlanEstimate](t, w); got.Available || got.SampleTurns != 0 || got.Milestone != nil {
		t.Fatalf("estimate with the provider disabled = %+v", got)
	}
}

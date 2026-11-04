package dashboard

// builder_plan_confirm_test.go — ai-builder-backend B06: confirm / reject a
// propose_backend_plan proposal. Integration tests against a real Postgres on
// the same fixture as builder_proposals_test.go (backend app with "todos").

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/config"
)

const (
	// Three operations that all apply.
	bplanAllGood = `{"summary":"notes for todos","operations":[
		{"kind":"add_table","add_table":{"name":"notes","columns":[{"name":"body","type":"text"}],"access":"public","access_reason":"shared notes"}},
		{"kind":"add_column","add_column":{"table":"todos","column":{"name":"done","type":"boolean"}}},
		{"kind":"add_index","add_index":{"table":"todos","name":"todos_done_idx","columns":["done"]}}]}`

	// The policy names a column that does not exist: the validator does not
	// look at policy columns, the provisioner refuses it, so operation 4 of 5
	// fails after three applied.
	bplanFailsThird = `{"summary":"private posts","operations":[
		{"kind":"toggle_auth","toggle_auth":{"email_enabled":true}},
		{"kind":"add_table","add_table":{"name":"posts","columns":[{"name":"title","type":"text"}],"access":"policy","access_reason":"rows match a value"}},
		{"kind":"set_rls_mode","set_rls_mode":{"table":"posts","mode":"policy"}},
		{"kind":"add_policy","add_policy":{"table":"posts","template":"value_match","roles":["authenticated"],"column":"ghost","value":"x"}},
		{"kind":"add_column","add_column":{"table":"todos","column":{"name":"done","type":"boolean"}}}]}`

	bplanWithPolicy = `{"summary":"open posts","operations":[
		{"kind":"toggle_auth","toggle_auth":{"email_enabled":true}},
		{"kind":"add_table","add_table":{"name":"posts","columns":[{"name":"title","type":"text"}],"access":"policy","access_reason":"anyone signed in reads"}},
		{"kind":"set_rls_mode","set_rls_mode":{"table":"posts","mode":"policy"}},
		{"kind":"add_policy","add_policy":{"table":"posts","template":"open_read","roles":["authenticated"]}}]}`
)

func (e *bpropEnv) tableNames(t *testing.T) map[string]bool {
	t.Helper()
	app, _, err := GetApp(context.Background(), e.pool, e.appID, e.super)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	out := map[string]bool{}
	for _, tb := range app.Tables {
		out[tb.Name] = true
	}
	return out
}

func (e *bpropEnv) confirmAuditDetails(t *testing.T) map[string]any {
	t.Helper()
	var raw []byte
	if err := e.pool.QueryRow(context.Background(),
		`SELECT metadata FROM zeep_system.audit_log WHERE action = 'builder.proposal.confirm' ORDER BY created_at DESC LIMIT 1`).Scan(&raw); err != nil {
		t.Fatalf("confirm audit row: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("audit details: %v", err)
	}
	return d
}

func (e *bpropEnv) lastEventPayload(t *testing.T, owner *DashboardUser, sessID, typ string) map[string]any {
	t.Helper()
	evs, err := e.st.ListEvents(context.Background(), owner.ID, sessID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == typ {
			var p map[string]any
			if err := json.Unmarshal(evs[i].Payload, &p); err != nil {
				t.Fatalf("event payload: %v", err)
			}
			return p
		}
	}
	t.Fatalf("no %s event", typ)
	return nil
}

func TestPlanConfirm_AllOperationsApplied(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaitingTool(t, e.editor, ToolProposeBackendPlan, bplanAllGood)

	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[builderProposalResult](t, w)
	if !got.Applied || got.Kind != "plan" || got.AppliedCount != 3 || got.Total != 3 || got.Turn == nil || got.Turn.Status != store.TurnPending {
		t.Errorf("response = %+v", got)
	}
	if !e.tableNames(t)["notes"] || !e.hasColumn(t, "done") {
		t.Error("the plan was not applied")
	}

	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 || tm[0].ToolCallID != "call_1" ||
		!strings.Contains(tm[0].Content, "all 3 operations were applied: add_table notes, add_column todos, add_index todos") ||
		!strings.Contains(tm[0].Content, "orbit_get_app_schema") {
		t.Errorf("tool messages = %+v", tm)
	}

	p := e.lastEventPayload(t, e.editor, sess.ID, store.EventProposalConfirmed)
	if p["kind"] != "plan" || p["applied"] != true || p["applied_count"] != float64(3) || p["total"] != float64(3) {
		t.Errorf("event payload = %v", p)
	}
	if _, ok := p["failed_index"]; ok {
		t.Errorf("a fully applied plan has no failed_index: %v", p)
	}
	if ops, _ := p["ops"].([]any); len(ops) != 3 {
		t.Errorf("event ops = %v", p["ops"])
	}
	if pt, _ := p["public_tables"].([]any); len(pt) != 1 || pt[0] != "notes" {
		t.Errorf("public_tables = %v", p["public_tables"])
	}

	d := e.confirmAuditDetails(t)
	kinds, _ := d["kinds"].(map[string]any)
	if d["kind"] != "plan" || d["total"] != float64(3) || d["applied_count"] != float64(3) ||
		kinds["add_table"] != float64(1) || kinds["add_column"] != float64(1) || kinds["add_index"] != float64(1) {
		t.Errorf("audit details = %v", d)
	}
	// No row data or column names in the audit: kinds and counts only.
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "body") || strings.Contains(string(raw), "shared notes") {
		t.Errorf("audit leaks plan content: %s", raw)
	}
	// Each DDL is audited with the builder origin.
	if n := e.builderAudits(t); n != 3 {
		t.Errorf("%d DDL audit rows with origin ai_builder, want 3", n)
	}
}

func TestPlanConfirm_FailureInTheMiddleReportsTheSplit(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaitingTool(t, e.owner, ToolProposeBackendPlan, bplanFailsThird)

	w := e.confirm(t, e.owner, sess.ID, turn.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "applied 3 of 5 operations; operation 4 (add_policy posts) failed") {
		t.Errorf("user message = %s", w.Body.String())
	}
	// Not atomic: the first three stay, the fifth never ran.
	if !e.tableNames(t)["posts"] {
		t.Error("operation 2 should stay applied")
	}
	if e.hasColumn(t, "done") {
		t.Error("operation 5 ran after the failure")
	}

	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 {
		t.Fatalf("tool messages = %+v", tm)
	}
	for _, want := range []string{
		"Applied 3 of 5 operations: toggle_auth, add_table posts, set_rls_mode posts.",
		"Operation 4 (add_policy posts) failed:",
		"Operations 4 to 5 were NOT applied.",
		"Call orbit_get_app_schema, then propose a plan for the remaining changes only.",
	} {
		if !strings.Contains(tm[0].Content, want) {
			t.Errorf("tool result %q does not contain %q", tm[0].Content, want)
		}
	}
	// The turn resumes so the agent can adapt.
	if st := e.turn(t, turn.ID); st.Status != store.TurnPending {
		t.Errorf("turn = %+v", st)
	}
	p := e.lastEventPayload(t, e.owner, sess.ID, store.EventProposalConfirmed)
	if p["applied"] != false || p["applied_count"] != float64(3) || p["failed_index"] != float64(3) || p["total"] != float64(5) {
		t.Errorf("event payload = %v", p)
	}
	d := e.confirmAuditDetails(t)
	if d["applied"] != false || d["applied_count"] != float64(3) {
		t.Errorf("audit details = %v", d)
	}
	// A repeat cannot run the rest: the confirm was recorded as not applied.
	if w := e.confirm(t, e.owner, sess.ID, turn.ID); w.Code != http.StatusConflict {
		t.Errorf("repeat confirm: want 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlanConfirm_SchemaDriftIs400AndTurnStaysPaused(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaitingTool(t, e.editor, ToolProposeBackendPlan, bplanAllGood)

	// While the card is open someone creates "notes" by hand.
	if _, err := e.dash.CreateAppTableForUser(context.Background(), e.owner, e.appID, TableRequestBody{
		Name: "notes", Columns: []config.ColumnConfig{{Name: "body", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "re-plan") {
		t.Fatalf("want 400 re-plan, got %d: %s", w.Code, w.Body.String())
	}
	if e.hasColumn(t, "done") {
		t.Error("a stale plan applied an operation")
	}
	if st := e.turn(t, turn.ID); st.Status != store.TurnAwaitingConfirmation || len(st.PendingProposal) == 0 {
		t.Errorf("turn must stay paused: %+v", st)
	}
	if n := len(e.toolMessages(t, sess.ID)); n != 0 {
		t.Errorf("%d tool results written for a stale plan", n)
	}
	if w := e.reject(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusOK {
		t.Errorf("reject after drift: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPlanConfirm_RBAC(t *testing.T) {
	t.Run("viewer gets 403", func(t *testing.T) {
		e := newBPropEnv(t)
		sess, turn := e.awaitingTool(t, e.viewer, ToolProposeBackendPlan, bplanAllGood)
		if w := e.confirm(t, e.viewer, sess.ID, turn.ID); w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		if e.tableNames(t)["notes"] {
			t.Error("a viewer's confirm changed the schema")
		}
		if st := e.turn(t, turn.ID); st.Status != store.TurnAwaitingConfirmation {
			t.Errorf("turn must stay paused: %+v", st)
		}
	})
	t.Run("editor cannot confirm a plan with a policy", func(t *testing.T) {
		e := newBPropEnv(t)
		sess, turn := e.awaitingTool(t, e.editor, ToolProposeBackendPlan, bplanWithPolicy)
		if w := e.confirm(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		// Checked before anything runs: not even the table was created.
		if e.tableNames(t)["posts"] {
			t.Error("part of the plan ran before the admin check")
		}
		if st := e.turn(t, turn.ID); st.Status != store.TurnAwaitingConfirmation {
			t.Errorf("turn must stay paused: %+v", st)
		}
	})
	t.Run("admin applies a plan with a policy", func(t *testing.T) {
		e := newBPropEnv(t)
		sess, turn := e.awaitingTool(t, e.owner, ToolProposeBackendPlan, bplanWithPolicy)
		if w := e.confirm(t, e.owner, sess.ID, turn.ID); w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
		}
		if !e.tableNames(t)["posts"] {
			t.Error("plan not applied")
		}
	})
	t.Run("lost access is 404", func(t *testing.T) {
		e := newBPropEnv(t)
		sess, turn := e.awaitingTool(t, e.editor, ToolProposeBackendPlan, bplanAllGood)
		if err := RemoveAppMember(context.Background(), e.pool, AppRef{BackendAppID: e.appID}, e.editor.ID); err != nil {
			t.Fatalf("remove member: %v", err)
		}
		if w := e.confirm(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
		}
		if e.tableNames(t)["notes"] {
			t.Error("confirm without access changed the schema")
		}
	})
}

func TestPlanConfirm_ConcurrentConfirmsApplyOnce(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaitingTool(t, e.editor, ToolProposeBackendPlan, bplanAllGood)

	const n = 6
	codes := make([]int, n)
	resolved := make([]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := e.confirm(t, e.editor, sess.ID, turn.ID)
			codes[i] = w.Code
			if w.Code == http.StatusOK {
				resolved[i] = decodeJSON[builderProposalResult](t, w).AlreadyResolved
			}
		}(i)
	}
	wg.Wait()

	applied := 0
	for i := range codes {
		if codes[i] != http.StatusOK {
			t.Errorf("request %d: status %d", i, codes[i])
		}
		if !resolved[i] {
			applied++
		}
	}
	if applied != 1 {
		t.Errorf("%d requests applied the plan, want exactly 1", applied)
	}
	if n := e.builderAudits(t); n != 3 || len(e.toolMessages(t, sess.ID)) != 1 {
		t.Errorf("DDL audits=%d toolResults=%d, want 3 and 1", n, len(e.toolMessages(t, sess.ID)))
	}
}

func TestPlanReject_AppliesNothing(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaitingTool(t, e.editor, ToolProposeBackendPlan, bplanAllGood)

	w := e.reject(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if e.tableNames(t)["notes"] || e.hasColumn(t, "done") || e.builderAudits(t) != 0 {
		t.Error("a rejected plan changed the schema")
	}
	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 || !strings.Contains(tm[0].Content, "rejected by user") {
		t.Errorf("tool messages = %+v", tm)
	}
}

func TestPlanConfirm_ProposalChangeStillWorks(t *testing.T) {
	// The single-change path is untouched by the plan branch.
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)
	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[builderProposalResult](t, w); got.Kind != "add_column" || got.Total != 0 {
		t.Errorf("response = %+v", got)
	}
}

package dashboard

// Tests for the add_policy operation of the AI Frontend Builder
// (ai-builder-backend B04): validation at proposal time, the admin-only rule,
// and applying a template through the same path as the dashboard.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/config"
)

const bpolOwnerWrite = `{"operation":{"kind":"add_policy","add_policy":{"table":"notes","template":"open_read_owner_write","roles":["member"]}}}`

// bpolEnv is a proposals env whose backend app has email auth and a table
// "notes" in policy mode, so every template can be created on it.
func newBPolEnv(t *testing.T) *bpropEnv {
	t.Helper()
	e := newBPropEnv(t)
	ctx := context.Background()
	// Only zeep_system is recreated between tests; an app's Postgres schema (and
	// its policy names) survives, so every test gets its own app name.
	name := fmt.Sprintf("bpol_%d", time.Now().UnixNano()%1_000_000_000)
	app, err := e.dash.CreateAppForUser(ctx, e.owner, AppRequestBody{Name: name, AuthEmailEnabled: true}, "test")
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if _, err := e.dash.CreateAppTableForUser(ctx, e.owner, app.ID, TableRequestBody{
		Name: "notes", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := e.dash.UpdateTableRLSModeForUser(ctx, e.owner, app.ID, "notes", "policy", "test"); err != nil {
		t.Fatalf("set rls mode: %v", err)
	}
	for _, m := range []struct {
		u    *DashboardUser
		role AppRole
	}{{e.editor, AppRoleEditor}, {e.viewer, AppRoleViewer}} {
		if _, err := AddAppMember(ctx, e.pool, AppRef{BackendAppID: app.ID}, m.u.ID, m.role); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	e.appID = app.ID
	return e
}

func (e *bpropEnv) policyNames(t *testing.T) []string {
	t.Helper()
	rows, err := ListTablePoliciesForUser(context.Background(), e.pool, e.owner, e.appID, "notes")
	if err != nil {
		t.Fatalf("list policies: %v", err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.PgPolicyName)
	}
	return names
}

func TestBuilderPolicy_AdminConfirmCreatesTheTemplatePolicies(t *testing.T) {
	e := newBPolEnv(t)
	sess, turn := e.awaiting(t, e.owner, bpolOwnerWrite)

	w := e.confirm(t, e.owner, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[builderProposalResult](t, w)
	if !got.Applied || got.Kind != "add_policy" {
		t.Errorf("response = %+v", got)
	}

	names := strings.Join(e.policyNames(t), ",")
	for _, want := range []string{"tpl_open_read_owner_write_select", "tpl_open_read_owner_write_update", "tpl_open_read_owner_write_delete"} {
		if !strings.Contains(names, want) {
			t.Errorf("policy %s missing, have %s", want, names)
		}
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'app.table_policy.create' AND ip_address = 'ai_builder'`).Scan(&n); err != nil || n != 3 {
		t.Errorf("policy audit rows with the builder origin = %d (%v), want 3", n, err)
	}
}

// Policies decide who can read and write rows, so an editor may not confirm
// one, exactly as in the dashboard. The turn stays paused so it can be rejected.
func TestBuilderPolicy_EditorConfirmIs403AndNothingIsCreated(t *testing.T) {
	e := newBPolEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpolOwnerWrite)

	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if names := e.policyNames(t); len(names) != 0 {
		t.Errorf("no policy may be created, have %v", names)
	}
	if st := e.turn(t, turn.ID); st.Status != store.TurnAwaitingConfirmation {
		t.Errorf("the turn must stay paused, status = %q", st.Status)
	}
}

// A multi-policy template whose last policy collides leaves the first two in
// place; the agent is told so and nothing internal leaks.
func TestBuilderPolicy_PartialFailureIsReportedHonestly(t *testing.T) {
	e := newBPolEnv(t)
	if _, err := e.dash.CreateTablePolicyForUser(context.Background(), e.owner, e.appID, "notes", PolicyDef{
		Name: "tpl_open_read_owner_write_delete", Action: "delete", Roles: []string{"member"},
		Clauses: []PolicyClause{{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"}},
	}, "test"); err != nil {
		t.Fatalf("seed colliding policy: %v", err)
	}
	sess, turn := e.awaiting(t, e.owner, bpolOwnerWrite)

	w := e.confirm(t, e.owner, sess.ID, turn.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "2 of 3 policies were created") {
		t.Errorf("the user must be told what was created: %s", w.Body.String())
	}
	if got := len(e.policyNames(t)); got != 3 {
		t.Errorf("1 seeded + 2 created = 3 policies, have %d", got)
	}
	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 || !strings.Contains(tm[0].Content, "only partly applied") || strings.Contains(tm[0].Content, "Nothing was changed") {
		t.Errorf("the agent must be told the change is partial: %+v", tm)
	}
	for _, leak := range []string{"dashboard:", "pq:", "SQLSTATE", "pgx"} {
		if strings.Contains(w.Body.String()+tm[0].Content, leak) {
			t.Errorf("internal error text %q leaked", leak)
		}
	}
}

// A template that needs the owner column on a table that never had an access
// mode (so no owner_id) is a safe 400, not a 500, and creates nothing.
func TestBuilderPolicy_TemplateNeedingOwnerColumnOnAPlainTableIsSafe400(t *testing.T) {
	e := newBPolEnv(t)
	if _, err := e.dash.CreateAppTableForUser(context.Background(), e.owner, e.appID, TableRequestBody{
		Name: "plain", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("create plain table: %v", err)
	}
	sess, turn := e.awaiting(t, e.owner,
		`{"operation":{"kind":"add_policy","add_policy":{"table":"plain","template":"owner_only","roles":["member"],"actions":["select"]}}}`)

	w := e.confirm(t, e.owner, sess.ID, turn.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "set_rls_mode") {
		t.Errorf("the message must say how to fix it: %s", w.Body.String())
	}
	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 || !strings.Contains(tm[0].Content, "set_rls_mode") || !strings.Contains(tm[0].Content, "Nothing was changed") {
		t.Errorf("the agent must be told what to do first: %+v", tm)
	}
	if got := len(e.policyNames(t)); got != 0 {
		t.Errorf("no policy may be created, have %d", got)
	}
}

func TestBuilderTools_ProposeAddPolicy_ToolErrorsAndAdminRule(t *testing.T) {
	e := newBuilderToolsEnv(t)
	ok := `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"owner_only","roles":["member"],"actions":["select"]}}}`

	res, err := e.call(t, ToolProposeBackendChange, ok)
	if err != nil || res.IsError || !strings.Contains(res.Content, "NOT been applied") {
		t.Fatalf("an admin's valid proposal must be recorded: res=%+v err=%v", res, err)
	}

	for name, args := range map[string]string{
		"no template":        `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks"}}}`,
		"unknown template":   `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"everyone","roles":["member"]}}}`,
		"missing roles":      `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"open_read"}}}`,
		"owner_only actions": `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"owner_only","roles":["member"]}}}`,
		"value_match column": `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"value_match","roles":["member"],"value":"x"}}}`,
		"blocked_by_default": `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"blocked_by_default"}}}`,
	} {
		res, err := e.call(t, ToolProposeBackendChange, args)
		if err != nil || !res.IsError {
			t.Errorf("%s: want a tool error, got res=%+v err=%v", name, res, err)
		}
	}

	// An editor can still propose other changes but not a policy.
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.app_members SET role = 'editor' WHERE backend_app_id = $1 AND user_id = $2`, e.appA, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	res, err = e.call(t, ToolProposeBackendChange, ok)
	if err != nil || !res.IsError || !strings.Contains(res.Content, "admin role") {
		t.Errorf("an editor's policy proposal must be a tool error naming the admin role: res=%+v err=%v", res, err)
	}
	res, err = e.call(t, ToolProposeBackendChange, validProposals["add_column"])
	if err != nil || res.IsError {
		t.Errorf("an editor may still propose other changes: res=%+v err=%v", res, err)
	}
}

func TestParseBuilderProposal_AddPolicyCarriesTheTemplateInputs(t *testing.T) {
	op, err := ParseBuilderProposal(json.RawMessage(
		`{"operation":{"kind":"add_policy","add_policy":{"table":"posts","template":"value_match","roles":["member"],"column":"status","value":"published"}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := op.AddPolicy
	if p == nil || p.Table != "posts" || p.Template != "value_match" || p.Column != "status" || p.Value != "published" || len(p.Roles) != 1 {
		t.Errorf("parsed policy = %+v", p)
	}
}

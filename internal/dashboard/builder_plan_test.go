package dashboard

// Tests for the plan validator of propose_backend_plan (ai-builder-backend
// B05). The pure parts need no database; the handler tests use the shared
// builder tools env against Postgres.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// ---- helpers: build plan arguments ----

func bplPlan(summary string, ops ...string) string {
	return fmt.Sprintf(`{"summary":%q,"operations":[%s]}`, summary, strings.Join(ops, ","))
}

func bplTable(name, access, reason string, cols ...string) string {
	if len(cols) == 0 {
		cols = []string{"name:text"}
	}
	var cs []string
	for _, c := range cols {
		p := strings.SplitN(c, ":", 2)
		cs = append(cs, fmt.Sprintf(`{"name":%q,"type":%q}`, p[0], p[1]))
	}
	acc := ""
	if access != "" {
		acc += fmt.Sprintf(`,"access":%q`, access)
	}
	if reason != "" {
		acc += fmt.Sprintf(`,"access_reason":%q`, reason)
	}
	return fmt.Sprintf(`{"kind":"add_table","add_table":{"name":%q,"columns":[%s]%s}}`, name, strings.Join(cs, ","), acc)
}

func bplMode(table, mode string) string {
	return fmt.Sprintf(`{"kind":"set_rls_mode","set_rls_mode":{"table":%q,"mode":%q}}`, table, mode)
}

func bplPolicy(table, template string) string {
	return fmt.Sprintf(`{"kind":"add_policy","add_policy":{"table":%q,"template":%q,"roles":["member"],"actions":["select"]}}`, table, template)
}

func bplFK(table, col, refTable, refCol string) string {
	return fmt.Sprintf(`{"kind":"add_foreign_key","add_foreign_key":{"table":%q,"column":%q,"ref_table":%q,"ref_column":%q}}`, table, col, refTable, refCol)
}

func bplColumn(table, name, typ string) string {
	return fmt.Sprintf(`{"kind":"add_column","add_column":{"table":%q,"column":{"name":%q,"type":%q}}}`, table, name, typ)
}

func bplExisting() []AppTableRow {
	return []AppTableRow{{
		Name: "clients", RLS: "policy",
		Columns: []config.ColumnConfig{{Name: "name", Type: "text"}, {Name: "owner_ref", Type: "uuid"}},
	}}
}

func toolMsg(t *testing.T, err error) string {
	t.Helper()
	var te *tools.ToolError
	if !errors.As(err, &te) {
		t.Fatalf("want a *tools.ToolError, got %T: %v", err, err)
	}
	return te.Msg
}

// ---- ParseBuilderPlan ----

func TestParseBuilderPlan_AcceptsAWellFormedPlan(t *testing.T) {
	plan, err := ParseBuilderPlan(json.RawMessage(bplPlan("A booking app",
		bplTable("patients", "owner", "each patient sees only their own records"),
		bplMode("patients", "owner"),
		bplTable("services", "public", "the catalog is meant for everyone"),
	)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Summary != "A booking app" || len(plan.Ops) != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Ops[0].Access != "owner" || plan.Ops[0].AccessReason == "" || plan.Ops[1].Access != "" || plan.Ops[2].Access != "public" {
		t.Errorf("access declarations = %+v", plan.Ops)
	}
}

func TestParseBuilderPlan_Rejects(t *testing.T) {
	many := make([]string, builderPlanMaxOps+1)
	for i := range many {
		many[i] = bplColumn("clients", fmt.Sprintf("c%d", i), "text")
	}
	cases := map[string]struct {
		args string
		want string // substring of the tool error
	}{
		"not json":            {`not json`, "expected a JSON object"},
		"unknown top key":     {`{"summary":"x","operations":[],"app_id":"1"}`, "app_id"},
		"no summary":          {bplPlan("", bplTable("a", "public", "r")), "summary"},
		"blank summary":       {bplPlan("   ", bplTable("a", "public", "r")), "summary"},
		"long summary":        {bplPlan(strings.Repeat("x", builderPlanMaxSummary+1), bplTable("a", "public", "r")), "summary"},
		"no operations":       {`{"summary":"x","operations":[]}`, "at least one"},
		"too many operations": {bplPlan("x", many...), "at most 30"},
		"bad operation":       {bplPlan("x", bplTable("a", "public", "r"), `{"kind":"drop_table","drop_table":{}}`), "operations[1]"},
		"two kinds":           {bplPlan("x", `{"kind":"add_table","add_table":{"name":"t","columns":[{"name":"a","type":"text"}]},"add_index":{"table":"t","name":"i","columns":["a"]}}`), "operations[0]"},
		"access missing":      {bplPlan("x", bplTable("a", "", "reason")), "\"access\" is required"},
		"access unknown":      {bplPlan("x", bplTable("a", "everyone", "reason")), "must be public, owner or policy"},
		"reason missing":      {bplPlan("x", bplTable("a", "public", "")), "access_reason"},
		"reason too long":     {bplPlan("x", bplTable("a", "public", strings.Repeat("r", builderAccessReasonMax+1))), "access_reason"},
		"policy bad template": {bplPlan("x", bplPolicy("a", "everyone")), "operations[0]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseBuilderPlan(json.RawMessage(tc.args))
			msg := toolMsg(t, err)
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not mention %q", msg, tc.want)
			}
			for _, leak := range []string{"json:", "ai:", "cannot unmarshal"} {
				if strings.Contains(msg, leak) {
					t.Errorf("internal error text %q leaked: %q", leak, msg)
				}
			}
		})
	}
}

// ---- Validate ----

func TestBuilderPlanValidate_Accepts(t *testing.T) {
	cases := map[string]string{
		"owner table": bplPlan("x",
			bplTable("patients", "owner", "r"), bplMode("patients", "owner")),
		"policy table": bplPlan("x",
			bplTable("posts", "policy", "r"), bplMode("posts", "policy"), bplPolicy("posts", "open_read")),
		"public table": bplPlan("x",
			bplTable("services", "public", "r")),
		"foreign key to a table made earlier": bplPlan("x",
			bplTable("patients", "public", "r"),
			bplTable("visits", "public", "r", "note:text", "patient_id:uuid"),
			bplFK("visits", "patient_id", "patients", "id")),
		"foreign key to an existing table": bplPlan("x",
			bplTable("visits", "public", "r", "client_id:uuid"),
			bplFK("visits", "client_id", "clients", "id")),
		"column added to an existing table": bplPlan("x",
			bplColumn("clients", "phone", "text")),
		"policy on an existing table already in policy mode": bplPlan("x",
			bplPolicy("clients", "read_only")),
		"auth toggled before a restricted mode": bplPlan("x",
			`{"kind":"toggle_auth","toggle_auth":{"email_enabled":true}}`,
			bplTable("t", "owner", "r"), bplMode("t", "owner")),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			plan, err := ParseBuilderPlan(json.RawMessage(args))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			authEmail := !strings.Contains(name, "auth toggled")
			if err := plan.Validate(bplExisting(), authEmail); err != nil {
				t.Fatalf("validate: %v", err)
			}
		})
	}
}

func TestBuilderPlanValidate_Rejects(t *testing.T) {
	cases := map[string]struct {
		args string
		want string
	}{
		"table already exists": {
			bplPlan("x", bplTable("clients", "public", "r")), "operations[0] (add_table)"},
		"duplicate table in the plan": {
			bplPlan("x", bplTable("a", "public", "r"), bplTable("a", "public", "r")), "operations[1] (add_table)"},
		"column on a missing table": {
			bplPlan("x", bplColumn("ghosts", "n", "text")), "table \"ghosts\" does not exist"},
		"column that already exists": {
			bplPlan("x", bplColumn("clients", "name", "text")), "operations[0] (add_column)"},
		"unsupported column type": {
			bplPlan("x", bplColumn("clients", "n", "money")), "unsupported column type"},
		"foreign key to a table created later": {
			bplPlan("x",
				bplTable("visits", "public", "r", "patient_id:uuid"),
				bplFK("visits", "patient_id", "patients", "id"),
				bplTable("patients", "public", "r")), "operations[1] (add_foreign_key)"},
		"foreign key on a missing column": {
			bplPlan("x", bplFK("clients", "nope", "clients", "id")), "column \"nope\" does not exist"},
		"remove a foreign key that is not there": {
			`{"summary":"x","operations":[{"kind":"remove_foreign_key","remove_foreign_key":{"table":"clients","column":"owner_ref"}}]}`, "no foreign key to remove"},
		"index on a missing column": {
			`{"summary":"x","operations":[{"kind":"add_index","add_index":{"table":"clients","name":"clients_x_idx","columns":["nope"]}}]}`, "operations[0] (add_index)"},
		"policy before the access mode": {
			bplPlan("x", bplTable("posts", "policy", "r"), bplPolicy("posts", "open_read"), bplMode("posts", "policy")), "no access mode at this point"},
		"policy on a missing table": {
			bplPlan("x", bplPolicy("ghosts", "open_read")), "does not exist"},
		"access mode on a missing table": {
			bplPlan("x", bplMode("ghosts", "policy")), "does not exist"},
		"invalid access mode": {
			bplPlan("x", bplMode("clients", "everyone")), "invalid"},
		"declared owner without the mode": {
			bplPlan("x", bplTable("a", "owner", "r")), "declared owner"},
		"declared policy without the mode": {
			bplPlan("x", bplTable("a", "policy", "r")), "declared policy"},
		"declared policy without a policy": {
			bplPlan("x", bplTable("a", "policy", "r"), bplMode("a", "policy")), "at least one add_policy"},
		"declared public but restricted": {
			bplPlan("x", bplTable("a", "public", "r"), bplMode("a", "owner")), "declared public"},
		"declared public but with a policy": {
			bplPlan("x", bplTable("a", "public", "r"), bplMode("a", "policy"), bplPolicy("a", "open_read")), "declared public"},
		"owner declared but policy mode set": {
			bplPlan("x", bplTable("a", "owner", "r"), bplMode("a", "policy")), "declared owner"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			plan, err := ParseBuilderPlan(json.RawMessage(tc.args))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			msg := toolMsg(t, plan.Validate(bplExisting(), true))
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not mention %q", msg, tc.want)
			}
		})
	}
}

func TestBuilderPlanValidate_RestrictedModeNeedsEmailAuth(t *testing.T) {
	plan, err := ParseBuilderPlan(json.RawMessage(bplPlan("x", bplTable("a", "owner", "r"), bplMode("a", "owner"))))
	if err != nil {
		t.Fatal(err)
	}
	msg := toolMsg(t, plan.Validate(bplExisting(), false))
	if !strings.Contains(msg, "toggle_auth") {
		t.Errorf("the error must tell the model to enable email auth first: %q", msg)
	}
}

// Validate works on a copy: the caller's tables are never changed.
func TestBuilderPlanValidate_DoesNotModifyTheTables(t *testing.T) {
	tables := bplExisting()
	before, _ := json.Marshal(tables)
	plan, err := ParseBuilderPlan(json.RawMessage(bplPlan("x",
		bplColumn("clients", "phone", "text"), bplMode("clients", "owner"),
		bplFK("clients", "owner_ref", "clients", "id"))))
	if err != nil {
		t.Fatal(err)
	}
	_ = plan.Validate(tables, true)
	after, _ := json.Marshal(tables)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("Validate changed its input:\n%s\n%s", before, after)
	}
}

// ---- handler ----

func (e *builderToolsEnv) plan(t *testing.T, args string) (tools.Result, error) {
	t.Helper()
	bt := &builderTools{deps: BuilderToolDeps{Pool: e.pool, Sessions: e.st}}
	return bt.proposeBackendPlan(context.Background(), tools.Call{
		ID: "call-1", Name: ToolProposeBackendPlan, Arguments: json.RawMessage(args), SessionID: e.sessionID,
	})
}

func TestBuilderTools_ProposeBackendPlan_ValidatesAgainstTheLinkedApp(t *testing.T) {
	e := newBuilderToolsEnv(t) // the app has a table "tasks"

	res, err := e.plan(t, bplPlan("A notes feature",
		bplTable("notes", "public", "notes are shared with everyone"),
		bplColumn("tasks", "done", "boolean")))
	if err != nil || res.IsError || !strings.Contains(res.Content, "NOT been applied") || !strings.Contains(res.Content, "2 operations") {
		t.Fatalf("a valid plan must be recorded: res=%+v err=%v", res, err)
	}

	// The same plan against a table that does not exist is a tool error that
	// names the operation.
	_, err = e.plan(t, bplPlan("x", bplColumn("ghosts", "n", "text")))
	if msg := toolMsg(t, err); !strings.Contains(msg, "operations[0]") || !strings.Contains(msg, "ghosts") {
		t.Errorf("error = %q", msg)
	}
	// A table that already exists in the app.
	_, err = e.plan(t, bplPlan("x", bplTable("tasks", "public", "r")))
	if msg := toolMsg(t, err); !strings.Contains(msg, "operations[0]") {
		t.Errorf("error = %q", msg)
	}
	// Nothing was written.
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.app_tables WHERE app_id = $1`, e.appA).Scan(&n); err != nil || n != 1 {
		t.Errorf("a plan must not change the schema: tables = %d (%v)", n, err)
	}
}

func TestBuilderTools_ProposeBackendPlan_PolicyNeedsAdmin(t *testing.T) {
	e := newBuilderToolsEnv(t)
	policyPlan := bplPlan("x", bplTable("posts", "policy", "r"), bplMode("posts", "policy"), bplPolicy("posts", "open_read"))

	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.app_members SET role = 'editor' WHERE backend_app_id = $1 AND user_id = $2`, e.appA, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	_, err := e.plan(t, policyPlan)
	if msg := toolMsg(t, err); !strings.Contains(msg, "admin role") {
		t.Errorf("an editor's plan with a policy must name the admin role: %q", msg)
	}
	// A plan without a policy is fine for an editor.
	if res, err := e.plan(t, bplPlan("x", bplTable("notes", "public", "r"))); err != nil || res.IsError {
		t.Errorf("an editor may propose a plan without policies: res=%+v err=%v", res, err)
	}
}

func TestBuilderTools_ProposeBackendPlan_OwnerLostAccess(t *testing.T) {
	e := newBuilderToolsEnv(t)
	e.revokeOwnerAccess(t)
	_, err := e.plan(t, bplPlan("x", bplTable("notes", "public", "r")))
	if msg := toolMsg(t, err); !strings.Contains(msg, "no longer has access") {
		t.Errorf("error = %q", msg)
	}
}

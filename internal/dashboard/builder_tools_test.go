package dashboard

// builder_tools_test.go — ai-frontend-builder T16 (AFB-11, AFB-12). Integration
// tests against a real Postgres (TEST_DATABASE_URL); the pure argument-
// validation tests need no database.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type builderToolsEnv struct {
	pool      *db.Pool
	reg       *tools.Registry
	owner     *DashboardUser
	appA      string // the session's backend app (owner is admin)
	appB      string // another app the owner is NOT a member of
	sessionID string
	st        *store.Store
}

func newBuilderToolsEnv(t *testing.T) *builderToolsEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	suffix := time.Now().UnixNano()
	owner, err := CreateUser(ctx, pool, fmt.Sprintf("bt-owner-%d@example.com", suffix), "owner", "hash", "member")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := CreateUser(ctx, pool, fmt.Sprintf("bt-other-%d@example.com", suffix), "other", "hash", "member")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	mkApp := func(name, ownerID string) string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`, name, ownerID,
		).Scan(&id); err != nil {
			t.Fatalf("create app %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
			id, ownerID,
		); err != nil {
			t.Fatalf("seed admin %s: %v", name, err)
		}
		return id
	}
	appA := mkApp("bt-app-a", owner.ID)
	appB := mkApp("bt-app-b", other.ID)

	mkTable := func(appID, name, cols string) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_tables (app_id, name, rls, columns, indexes)
			 VALUES ($1, $2, '', $3::jsonb, '[]'::jsonb)`, appID, name, cols,
		); err != nil {
			t.Fatalf("create table %s: %v", name, err)
		}
	}
	mkTable(appA, "tasks", `[{"name":"title","type":"text","required":true}]`)
	mkTable(appB, "secret_invoices", `[{"name":"amount","type":"integer"}]`)

	st := store.New(pool)
	sess, err := st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: owner.ID, BackendAppID: appA, Title: "t",
		TemplateKind: store.TemplateDefault, TemplateVersion: "v0",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	reg := tools.NewRegistry()
	if err := RegisterBuilderTools(reg, BuilderToolDeps{Pool: pool, Sessions: st}); err != nil {
		t.Fatalf("RegisterBuilderTools: %v", err)
	}
	return &builderToolsEnv{pool: pool, reg: reg, owner: owner, appA: appA, appB: appB, sessionID: sess.ID, st: st}
}

func (e *builderToolsEnv) call(t *testing.T, name, args string) (tools.Result, error) {
	t.Helper()
	return e.reg.Dispatch(context.Background(), tools.Call{
		ID: "call-1", Name: name, Arguments: json.RawMessage(args), SessionID: e.sessionID,
	})
}

func (e *builderToolsEnv) revokeOwnerAccess(t *testing.T) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`DELETE FROM zeep_system.app_members WHERE backend_app_id = $1 AND user_id = $2`, e.appA, e.owner.ID,
	); err != nil {
		t.Fatalf("revoke access: %v", err)
	}
}

func TestBuilderTools_GetAppSchema_ReturnsLinkedApp(t *testing.T) {
	e := newBuilderToolsEnv(t)
	res, err := e.call(t, ToolOrbitGetAppSchema, `{}`)
	if err != nil || res.IsError {
		t.Fatalf("want success, got res=%+v err=%v", res, err)
	}
	var schema AppSchema
	if err := json.Unmarshal([]byte(res.Content), &schema); err != nil {
		t.Fatalf("result is not the schema JSON: %v", err)
	}
	if schema.AppID != e.appA || schema.AppName != "bt-app-a" {
		t.Fatalf("expected the session's app %s, got %s (%s)", e.appA, schema.AppID, schema.AppName)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "tasks" || len(schema.Tables[0].Columns) != 1 || schema.Tables[0].Columns[0].Name != "title" {
		t.Fatalf("unexpected tables: %+v", schema.Tables)
	}
}

func TestBuilderTools_GetAppSchema_IgnoresAppIDArgument(t *testing.T) {
	e := newBuilderToolsEnv(t)
	for _, args := range []string{
		fmt.Sprintf(`{"app_id":%q}`, e.appB),
		`{"app_name":"bt-app-b"}`,
		fmt.Sprintf(`{"appId":%q,"id":%q}`, e.appB, e.appB),
	} {
		res, err := e.call(t, ToolOrbitGetAppSchema, args)
		if err != nil || res.IsError {
			t.Fatalf("args %s: want success on the linked app, got res=%+v err=%v", args, res, err)
		}
		if strings.Contains(res.Content, "secret_invoices") || strings.Contains(res.Content, e.appB) {
			t.Fatalf("args %s: leaked another app's data: %s", args, res.Content)
		}
		if !strings.Contains(res.Content, e.appA) {
			t.Fatalf("args %s: expected the linked app, got %s", args, res.Content)
		}
	}
	// The declared schema must not even offer an app-id argument.
	for _, name := range []string{ToolOrbitGetAppSchema, ToolOrbitListAuthProviders} {
		for _, spec := range e.reg.Specs() {
			if spec.Name != name {
				continue
			}
			var s struct {
				Properties map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(spec.Schema, &s); err != nil || len(s.Properties) != 0 {
				t.Fatalf("%s schema must declare no arguments, got %s", name, spec.Schema)
			}
		}
	}
}

func TestBuilderTools_ListAuthProviders_LinkedAppAndRedacted(t *testing.T) {
	e := newBuilderToolsEnv(t)
	ctx := context.Background()
	const secret = "s3cr3t-value-for-test"
	if _, err := e.pool.Exec(ctx,
		`UPDATE zeep_system.apps SET auth_providers = $2::jsonb WHERE id = $1`, e.appA,
		fmt.Sprintf(`{"google":{"enabled":true,"client_id":"cid-a","client_secret":%q}}`, secret),
	); err != nil {
		t.Fatalf("seed providers A: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`UPDATE zeep_system.apps SET auth_providers = $2::jsonb WHERE id = $1`, e.appB,
		`{"google":{"enabled":true,"client_id":"cid-b-other-app","client_secret":"other-secret"}}`,
	); err != nil {
		t.Fatalf("seed providers B: %v", err)
	}

	res, err := e.call(t, ToolOrbitListAuthProviders, fmt.Sprintf(`{"app_id":%q}`, e.appB))
	if err != nil || res.IsError {
		t.Fatalf("want success, got res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, "cid-a") {
		t.Fatalf("expected the linked app's providers, got %s", res.Content)
	}
	if strings.Contains(res.Content, "cid-b-other-app") || strings.Contains(res.Content, "other-secret") {
		t.Fatalf("leaked another app's providers: %s", res.Content)
	}
	if strings.Contains(res.Content, secret) {
		t.Fatalf("client secret must never be returned: %s", res.Content)
	}
}

func TestBuilderTools_OwnerLostAccess_ToolError(t *testing.T) {
	e := newBuilderToolsEnv(t)
	e.revokeOwnerAccess(t)

	valid := `{"operation":{"kind":"add_column","add_column":{"table":"tasks","column":{"name":"done","type":"boolean"}}}}`
	for _, tc := range []struct{ tool, args string }{
		{ToolOrbitGetAppSchema, `{}`},
		{ToolOrbitListAuthProviders, `{}`},
		{ToolProposeBackendChange, valid},
	} {
		res, err := e.call(t, tc.tool, tc.args)
		if err != nil {
			t.Fatalf("%s: lost access must be a tool error, not a Go error: %v", tc.tool, err)
		}
		if !res.IsError {
			t.Fatalf("%s: expected a tool error after access was revoked, got %+v", tc.tool, res)
		}
		if strings.Contains(res.Content, "tasks") || strings.Contains(res.Content, "bt-app-a") {
			t.Fatalf("%s: error must not leak app data: %s", tc.tool, res.Content)
		}
	}
}

func TestBuilderTools_UnknownSession_IsServerSideError(t *testing.T) {
	e := newBuilderToolsEnv(t)
	_, err := e.reg.Dispatch(context.Background(), tools.Call{
		ID: "c", Name: ToolOrbitGetAppSchema, Arguments: json.RawMessage(`{}`),
		SessionID: "00000000-0000-0000-0000-000000000000",
	})
	if err == nil {
		t.Fatal("a missing session must surface as a Go error (server side), not model text")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected the store's not-found error to be wrapped, got %v", err)
	}
}

func TestBuilderTools_ProposeBackendChange_ValidOperations(t *testing.T) {
	e := newBuilderToolsEnv(t)
	if k, ok := e.reg.Kind(ToolProposeBackendChange); !ok || k != tools.KindProposal {
		t.Fatalf("propose_backend_change must be KindProposal, got %v (registered=%v)", k, ok)
	}
	for _, kind := range []string{"add_table", "add_column", "add_index", "add_reference", "add_foreign_key", "remove_foreign_key", "set_rls_mode", "toggle_auth", "add_policy"} {
		args := validProposals[kind]
		res, err := e.call(t, ToolProposeBackendChange, args)
		if err != nil || res.IsError {
			t.Fatalf("%s: want success, got res=%+v err=%v", kind, res, err)
		}
		if !strings.Contains(res.Content, "NOT been applied") {
			t.Fatalf("%s: result must tell the model nothing was applied: %s", kind, res.Content)
		}
	}
}

// validProposals holds one well-formed propose_backend_change argument object
// per operation kind.
var validProposals = map[string]string{
	"add_table":          `{"operation":{"kind":"add_table","add_table":{"name":"notes","columns":[{"name":"body","type":"text"}]}}}`,
	"add_column":         `{"operation":{"kind":"add_column","add_column":{"table":"tasks","column":{"name":"done","type":"boolean"}}}}`,
	"add_index":          `{"operation":{"kind":"add_index","add_index":{"table":"tasks","name":"tasks_title_idx","columns":["title"],"unique":false}}}`,
	"add_reference":      `{"operation":{"kind":"add_reference","add_reference":{"table":"tasks","column":{"name":"owner_ref","type":"uuid"},"ref_table":"users","ref_column":"id"}}}`,
	"add_foreign_key":    `{"operation":{"kind":"add_foreign_key","add_foreign_key":{"table":"tasks","column":"owner_ref","ref_table":"users","ref_column":"id","on_delete":"cascade"}}}`,
	"remove_foreign_key": `{"operation":{"kind":"remove_foreign_key","remove_foreign_key":{"table":"tasks","column":"owner_ref"}}}`,
	"set_rls_mode":       `{"operation":{"kind":"set_rls_mode","set_rls_mode":{"table":"tasks","mode":"owner"}}}`,
	"toggle_auth":        `{"operation":{"kind":"toggle_auth","toggle_auth":{"email_enabled":true}}}`,
	"add_policy":         `{"operation":{"kind":"add_policy","add_policy":{"table":"tasks","template":"owner_only","roles":["member"],"actions":["select"]}}}`,
}

func TestBuilderTools_ProposeBackendChange_MalformedIsToolError(t *testing.T) {
	e := newBuilderToolsEnv(t)
	for name, args := range malformedProposals {
		res, err := e.call(t, ToolProposeBackendChange, args)
		if err != nil {
			t.Fatalf("%s: malformed proposal must be a tool error, got Go error %v", name, err)
		}
		if !res.IsError {
			t.Fatalf("%s: expected a tool error, got %+v", name, res)
		}
	}
}

var malformedProposals = map[string]string{
	"not json":              `not json`,
	"not an object":         `[1,2]`,
	"missing operation":     `{}`,
	"null operation":        `{"operation":null}`,
	"operation not object":  `{"operation":"add_table"}`,
	"unknown top-level key": `{"operation":{"kind":"toggle_auth","toggle_auth":{"email_enabled":true}},"app_id":"x"}`,
	"missing kind":          `{"operation":{"add_table":{"name":"t","columns":[{"name":"a","type":"text"}]}}}`,
	"unknown kind":          `{"operation":{"kind":"drop_table","drop_table":{"name":"t"}}}`,
	"payload missing":       `{"operation":{"kind":"add_table"}}`,
	"payload null":          `{"operation":{"kind":"add_table","add_table":null}}`,
	"payload mismatches":    `{"operation":{"kind":"add_table","add_column":{"table":"t","column":{"name":"a","type":"text"}}}}`,
	"two operations":        `{"operation":{"kind":"add_table","add_table":{"name":"t","columns":[{"name":"a","type":"text"}]},"add_index":{"table":"t","name":"i","columns":["a"]}}}`,
	"add_table no columns":  `{"operation":{"kind":"add_table","add_table":{"name":"t","columns":[]}}}`,
	"add_table column type": `{"operation":{"kind":"add_table","add_table":{"name":"t","columns":[{"name":"a"}]}}}`,
	"add_column no table":   `{"operation":{"kind":"add_column","add_column":{"column":{"name":"a","type":"text"}}}}`,
	"add_index no columns":  `{"operation":{"kind":"add_index","add_index":{"table":"t","name":"i","columns":[]}}}`,
	"fk missing ref":        `{"operation":{"kind":"add_foreign_key","add_foreign_key":{"table":"t","column":"c"}}}`,
	"toggle_auth implicit":  `{"operation":{"kind":"toggle_auth","toggle_auth":{}}}`,
	"toggle_auth wrong typ": `{"operation":{"kind":"toggle_auth","toggle_auth":{"email_enabled":"yes"}}}`,
}

// TestParseBuilderProposal_NoDatabase covers validation without a database so
// it also runs where TEST_DATABASE_URL is unset.
func TestParseBuilderProposal_NoDatabase(t *testing.T) {
	for kind, args := range validProposals {
		op, err := ParseBuilderProposal(json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: unexpected error %v", kind, err)
		}
		if op.Kind != kind {
			t.Fatalf("%s: got kind %q", kind, op.Kind)
		}
	}
	for name, args := range malformedProposals {
		_, err := ParseBuilderProposal(json.RawMessage(args))
		var te *tools.ToolError
		if !errors.As(err, &te) {
			t.Fatalf("%s: expected a *tools.ToolError, got %v", name, err)
		}
		if strings.Contains(te.Msg, "json:") || strings.Contains(te.Msg, "ai:") {
			t.Fatalf("%s: raw internal error leaked to the model: %q", name, te.Msg)
		}
	}
}

func TestRegisterBuilderTools_RequiresDeps(t *testing.T) {
	if err := RegisterBuilderTools(nil, BuilderToolDeps{}); err == nil {
		t.Fatal("nil registry must be rejected")
	}
	if err := RegisterBuilderTools(tools.NewRegistry(), BuilderToolDeps{}); err == nil {
		t.Fatal("missing pool/session reader must be rejected")
	}
}

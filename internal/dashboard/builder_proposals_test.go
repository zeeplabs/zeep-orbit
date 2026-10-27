package dashboard

// builder_proposals_test.go — ai-frontend-builder T27: confirm / reject a schema
// proposal. Integration tests against a real Postgres: the backend app is a
// really provisioned Orbit app, so a confirmed proposal runs real DDL; the
// turn state is built through the store exactly as the agent loop leaves it
// (assistant tool call, then FinishTurn awaiting_confirmation).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

type bpropEnv struct {
	*bhEnv
	dash   *Handler
	appID  string         // a really provisioned backend app, with table "todos"
	editor *DashboardUser // editor member of the app
	pokes  atomic.Int32
}

const (
	bpropAddColumn  = `{"operation":{"kind":"add_column","add_column":{"table":"todos","column":{"name":"done","type":"boolean"}}}}`
	bpropDupColumn  = `{"operation":{"kind":"add_column","add_column":{"table":"todos","column":{"name":"title","type":"text"}}}}`
	bpropBadType    = `{"operation":{"kind":"add_column","add_column":{"table":"todos","column":{"name":"x","type":"nonsense"}}}}`
	bpropMissingTbl = `{"operation":{"kind":"add_column","add_column":{"table":"nope","column":{"name":"x","type":"text"}}}}`
)

func newBPropEnv(t *testing.T) *bpropEnv {
	t.Helper()
	e := &bpropEnv{bhEnv: newBHEnv(t)}
	ctx := context.Background()

	e.dash = NewHandler(e.pool, registry.New(), zap.NewNop())
	app, err := e.dash.CreateAppForUser(ctx, e.owner, AppRequestBody{Name: "bprop_app"}, "test")
	if err != nil {
		t.Fatalf("create backend app: %v", err)
	}
	e.appID = app.ID
	if _, err := e.dash.CreateAppTableForUser(ctx, e.owner, e.appID, TableRequestBody{
		Name: "todos", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "test"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	e.editor, err = CreateUser(ctx, e.pool, fmt.Sprintf("bprop-editor-%d@example.com", time.Now().UnixNano()), "editor", "hash", "member")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		u    *DashboardUser
		role AppRole
	}{{e.editor, AppRoleEditor}, {e.viewer, AppRoleViewer}} {
		if _, err := AddAppMember(ctx, e.pool, AppRef{BackendAppID: e.appID}, m.u.ID, m.role); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	e.useHandler(BuilderHandlerConfig{Dashboard: e.dash})
	return e
}

// useHandler rebuilds e.h with the shared fakes plus extra.
func (e *bpropEnv) useHandler(extra BuilderHandlerConfig) {
	extra.Store, extra.Sandbox, extra.Logger = e.st, e.fake, zap.NewNop()
	extra.Poke = func() { e.pokes.Add(1) }
	e.h = NewBuilderHandler(e.pool, extra)
}

// awaiting creates a session on the backend app owned by owner and leaves its
// turn paused on a proposal with the given propose_backend_change arguments.
func (e *bpropEnv) awaiting(t *testing.T, owner *DashboardUser, args string) (*store.Session, *store.Turn) {
	t.Helper()
	ctx := context.Background()
	sess, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: owner.ID, BackendAppID: e.appID, Title: "t", TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := e.st.CreateTurn(ctx, owner.ID, sess.ID, store.KindChat, "add a done flag"); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	// The queue is global: finish turns of earlier sessions of the same test
	// (resumed after a decision) until this session's turn comes up.
	var turn *store.Turn
	for {
		claimed, err := e.st.ClaimTurn(ctx, "w1", time.Minute)
		if err != nil {
			t.Fatalf("ClaimTurn: %v", err)
		}
		if claimed.SessionID == sess.ID {
			turn = claimed
			break
		}
		if err := e.st.FinishTurn(ctx, store.Lease{TurnID: claimed.ID, Owner: "w1"}, store.FinishParams{Status: store.TurnCompleted}); err != nil {
			t.Fatalf("FinishTurn (drain): %v", err)
		}
	}
	lease := store.Lease{TurnID: turn.ID, Owner: "w1"}
	calls := `[{"id":"call_1","name":"propose_backend_change","arguments":` + args + `}]`
	if _, err := e.st.AppendMessage(ctx, lease, store.NewMessage{Role: store.RoleAssistant, ToolCalls: json.RawMessage(calls)}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	pending := `{"tool_call_id":"call_1","name":"propose_backend_change","arguments":` + args + `}`
	if err := e.st.FinishTurn(ctx, lease, store.FinishParams{Status: store.TurnAwaitingConfirmation, PendingProposal: json.RawMessage(pending)}); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	return sess, turn
}

// callWithParams calls fn as user with several chi URL params at once (the
// shared do helper keeps only the last one).
func callWithParams(fn http.HandlerFunc, user *DashboardUser, method, target string, params ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
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

func (e *bpropEnv) confirm(t *testing.T, user *DashboardUser, sessID, turn string) *httptest.ResponseRecorder {
	t.Helper()
	return callWithParams(e.h.ConfirmProposal, user, http.MethodPost, "/x/confirm", "id", sessID, "turn", turn)
}

func (e *bpropEnv) reject(t *testing.T, user *DashboardUser, sessID, turn string) *httptest.ResponseRecorder {
	t.Helper()
	return callWithParams(e.h.RejectProposal, user, http.MethodPost, "/x/reject", "id", sessID, "turn", turn)
}

func (e *bpropEnv) turn(t *testing.T, id string) *store.Turn {
	t.Helper()
	got, err := e.st.GetTurn(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	return got
}

func (e *bpropEnv) hasColumn(t *testing.T, name string) bool {
	t.Helper()
	app, _, err := GetApp(context.Background(), e.pool, e.appID, e.super)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	for _, tb := range app.Tables {
		if tb.Name != "todos" {
			continue
		}
		for _, c := range tb.Columns {
			if c.Name == name {
				return true
			}
		}
	}
	return false
}

func (e *bpropEnv) toolMessages(t *testing.T, sessID string) []store.Message {
	t.Helper()
	msgs, err := e.st.ListMessages(context.Background(), sessID)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Message
	for _, m := range msgs {
		if m.Role == store.RoleTool {
			out = append(out, m)
		}
	}
	return out
}

func (e *bpropEnv) eventTypes(t *testing.T, owner *DashboardUser, sessID string) []string {
	t.Helper()
	evs, err := e.st.ListEvents(context.Background(), owner.ID, sessID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Type)
	}
	return out
}

func (e *bpropEnv) builderAudits(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE ip_address = 'ai_builder'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- confirm ----

func TestProposalConfirm_EditorAppliesDDLAndResumesTurn(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)
	if e.hasColumn(t, "done") {
		t.Fatal("setup: column already there")
	}

	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[builderProposalResult](t, w)
	if !got.Applied || got.Kind != "add_column" || got.AlreadyResolved || got.Turn == nil || got.Turn.Status != store.TurnPending {
		t.Errorf("response = %+v", got)
	}
	if got.Turn.PendingProposal != nil {
		t.Errorf("a resolved turn must not expose a pending proposal: %+v", got.Turn.PendingProposal)
	}

	if !e.hasColumn(t, "done") {
		t.Error("the DDL was not applied")
	}
	// The DDL's audit row carries the builder origin.
	var origin string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT ip_address FROM zeep_system.audit_log WHERE action = 'app.table_column.create'`).Scan(&origin); err != nil || origin != "ai_builder" {
		t.Errorf("DDL audit origin = %q (%v), want ai_builder", origin, err)
	}

	// The tool call is answered, the turn is claimable again, a worker was poked.
	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 || tm[0].ToolCallID != "call_1" || !strings.Contains(tm[0].Content, "applied") {
		t.Errorf("tool messages = %+v", tm)
	}
	if st := e.turn(t, turn.ID); st.Status != store.TurnPending || len(st.PendingProposal) != 0 {
		t.Errorf("turn = %+v", st)
	}
	if s := e.sessionStatus(t, sess.ID); s != store.SessionIdle {
		t.Errorf("session status = %q", s)
	}
	if n := e.pokes.Load(); n != 1 {
		t.Errorf("worker poked %d times, want 1", n)
	}
	types := strings.Join(e.eventTypes(t, e.editor, sess.ID), ",")
	if !strings.Contains(types, "proposal_confirmed") {
		t.Errorf("events = %s", types)
	}
	if _, err := e.st.ClaimTurn(context.Background(), "w2", time.Minute); err != nil {
		t.Errorf("resumed turn is not claimable: %v", err)
	}
}

func TestProposalConfirm_ViewerGets403AndTurnStaysPaused(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.viewer, bpropAddColumn)

	w := e.confirm(t, e.viewer, sess.ID, turn.ID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if e.hasColumn(t, "done") {
		t.Error("a viewer's confirm changed the schema")
	}
	if st := e.turn(t, turn.ID); st.Status != store.TurnAwaitingConfirmation || len(st.PendingProposal) == 0 {
		t.Errorf("turn must stay paused: %+v", st)
	}
	if n := len(e.toolMessages(t, sess.ID)); n != 0 {
		t.Errorf("%d tool results written for a refused confirm", n)
	}
	if e.builderAudits(t) != 0 || e.pokes.Load() != 0 {
		t.Error("a refused confirm must leave no audit row and wake nobody")
	}
	// The viewer can still reject it.
	if w := e.reject(t, e.viewer, sess.ID, turn.ID); w.Code != http.StatusOK {
		t.Errorf("reject by the owner: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProposalConfirm_LostAccessIs404AndTurnStaysPaused(t *testing.T) {
	e := newBPropEnv(t)
	// The session owner has no membership on the app at all.
	sess, turn := e.awaiting(t, e.outsider, bpropAddColumn)

	w := e.confirm(t, e.outsider, sess.ID, turn.ID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
	if e.hasColumn(t, "done") || e.turn(t, turn.ID).Status != store.TurnAwaitingConfirmation {
		t.Error("nothing may change when access is lost")
	}
}

func TestProposalConfirm_SuperadminOwnerCanConfirm(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.super, bpropAddColumn)
	if w := e.confirm(t, e.super, sess.ID, turn.ID); w.Code != http.StatusOK || !e.hasColumn(t, "done") {
		t.Fatalf("superadmin confirm: %d %s", w.Code, w.Body.String())
	}
}

func TestProposalConfirm_ValidationErrorsAreSurfacedAndGoToTheAgent(t *testing.T) {
	cases := []struct {
		name    string
		args    string
		wantMsg string
	}{
		{"duplicate column", bpropDupColumn, "already exists"},
		{"disallowed type", bpropBadType, ""},
		{"unknown table", bpropMissingTbl, "does not exist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newBPropEnv(t)
			sess, turn := e.awaiting(t, e.editor, tc.args)

			w := e.confirm(t, e.editor, sess.ID, turn.ID)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
			}
			msg := bhErrorBody(t, w)
			if msg == "" || !strings.Contains(strings.ToLower(msg), tc.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", msg, tc.wantMsg)
			}
			if e.hasColumn(t, "x") {
				t.Error("a refused change was applied")
			}
			// The agent hears about it and the turn resumes.
			tm := e.toolMessages(t, sess.ID)
			if len(tm) != 1 || !strings.Contains(tm[0].Content, "NOT applied") || !strings.Contains(tm[0].Content, msg) {
				t.Errorf("tool result = %+v (user message %q)", tm, msg)
			}
			if !strings.Contains(string(tm[0].ToolCalls), `"is_error": true`) && !strings.Contains(string(tm[0].ToolCalls), `"is_error":true`) {
				t.Errorf("tool result is not marked as an error: %s", tm[0].ToolCalls)
			}
			if e.turn(t, turn.ID).Status != store.TurnPending {
				t.Error("turn must resume so the agent can adapt")
			}
			if e.builderAudits(t) != 0 {
				t.Error("no DDL audit row expected for a refused change")
			}
			// A repeat is not a second attempt.
			if w := e.confirm(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusConflict {
				t.Errorf("repeat after a refused confirm: want 409, got %d", w.Code)
			}
		})
	}
}

func TestProposalConfirm_MalformedStoredProposalIsNotApplied(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, `{"operation":{"kind":"drop_everything"}}`)
	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if e.builderAudits(t) != 0 || e.turn(t, turn.ID).Status != store.TurnPending {
		t.Error("a malformed proposal must be answered to the agent, never applied")
	}
}

func TestProposalConfirm_DoubleConfirmIsANoOp(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)

	if w := e.confirm(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusOK {
		t.Fatalf("first confirm: %d %s", w.Code, w.Body.String())
	}
	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("second confirm: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[builderProposalResult](t, w); !got.AlreadyResolved || !got.Applied || got.Kind != "add_column" {
		t.Errorf("second response = %+v", got)
	}
	var audits int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'app.table_column.create'`).Scan(&audits); err != nil || audits != 1 {
		t.Errorf("DDL audit rows = %d (%v), want exactly 1", audits, err)
	}
	if n := len(e.toolMessages(t, sess.ID)); n != 1 {
		t.Errorf("%d tool results, want 1", n)
	}
	if e.pokes.Load() != 1 {
		t.Errorf("worker poked %d times, want 1", e.pokes.Load())
	}
}

func TestProposalConfirm_ConcurrentConfirmsApplyOnce(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)

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
		t.Errorf("%d requests applied the change, want exactly 1", applied)
	}
	var audits int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'app.table_column.create'`).Scan(&audits)
	if audits != 1 || len(e.toolMessages(t, sess.ID)) != 1 {
		t.Errorf("audits=%d toolResults=%d, want 1 and 1", audits, len(e.toolMessages(t, sess.ID)))
	}
}

func TestProposalConfirm_NeedsDashboardHandler(t *testing.T) {
	e := newBPropEnv(t)
	e.useHandler(BuilderHandlerConfig{}) // no Dashboard
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)
	w := e.confirm(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusServiceUnavailable || bhErrorBody(t, w) != builderNotConfiguredMsg {
		t.Fatalf("want 503 %q, got %d: %s", builderNotConfiguredMsg, w.Code, w.Body.String())
	}
	if e.turn(t, turn.ID).Status != store.TurnAwaitingConfirmation {
		t.Error("turn must stay paused")
	}
}

// ---- reject ----

func TestProposalReject_AnswersTheAgentAndAppliesNothing(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.viewer, bpropAddColumn) // owner-only: no write access needed

	w := e.reject(t, e.viewer, sess.ID, turn.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[builderProposalResult](t, w); !got.Rejected || got.Applied || got.Turn == nil || got.Turn.Status != store.TurnPending {
		t.Errorf("response = %+v", got)
	}
	if e.hasColumn(t, "done") || e.builderAudits(t) != 0 {
		t.Error("a rejected proposal must not touch the schema")
	}
	tm := e.toolMessages(t, sess.ID)
	if len(tm) != 1 || tm[0].ToolCallID != "call_1" || !strings.Contains(tm[0].Content, "rejected by user") {
		t.Errorf("tool messages = %+v", tm)
	}
	if e.turn(t, turn.ID).Status != store.TurnPending || e.pokes.Load() != 1 {
		t.Errorf("turn must resume and wake the worker (pokes=%d)", e.pokes.Load())
	}
	if !strings.Contains(strings.Join(e.eventTypes(t, e.viewer, sess.ID), ","), "proposal_rejected") {
		t.Error("no proposal_rejected event")
	}
}

func TestProposalDecisions_RepeatsAreNoOpsAndConflictsAre409(t *testing.T) {
	e := newBPropEnv(t)

	// reject, reject again: no-op; confirm after reject: 409.
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)
	if w := e.reject(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	w := e.reject(t, e.editor, sess.ID, turn.ID)
	if w.Code != http.StatusOK || !decodeJSON[builderProposalResult](t, w).AlreadyResolved {
		t.Errorf("repeat reject: %d %s", w.Code, w.Body.String())
	}
	if w := e.confirm(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusConflict {
		t.Errorf("confirm after reject: want 409, got %d", w.Code)
	}
	if e.hasColumn(t, "done") || len(e.toolMessages(t, sess.ID)) != 1 {
		t.Error("late decisions must change nothing")
	}

	// confirm, then reject: 409.
	sess2, turn2 := e.awaiting(t, e.editor, `{"operation":{"kind":"add_column","add_column":{"table":"todos","column":{"name":"other","type":"text"}}}}`)
	if w := e.confirm(t, e.editor, sess2.ID, turn2.ID); w.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", w.Code, w.Body.String())
	}
	if w := e.reject(t, e.editor, sess2.ID, turn2.ID); w.Code != http.StatusConflict {
		t.Errorf("reject after confirm: want 409, got %d", w.Code)
	}
	if got := bhErrorBody(t, e.reject(t, e.editor, sess2.ID, turn2.ID)); got != builderProposalNotPending {
		t.Errorf("409 message = %q", got)
	}
}

func TestProposalRoutes_ResolveTurnBySeqAndRejectUnknownOrForeign(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)

	// Unknown turns, in both spellings.
	for _, p := range []string{"99", "00000000-0000-4000-8000-000000000000", "nope", "-1"} {
		if w := e.confirm(t, e.editor, sess.ID, p); w.Code != http.StatusNotFound || bhErrorBody(t, w) != builderTurnNotFoundMsg {
			t.Errorf("turn %q: want 404 %q, got %d: %s", p, builderTurnNotFoundMsg, w.Code, w.Body.String())
		}
	}
	// Another user's session is a 404 for both routes, and nothing changes.
	for _, fn := range []func(*testing.T, *DashboardUser, string, string) *httptest.ResponseRecorder{e.confirm, e.reject} {
		if w := fn(t, e.other, sess.ID, turn.ID); w.Code != http.StatusNotFound || bhErrorBody(t, w) != builderSessionNotFoundMsg {
			t.Errorf("foreign user: want 404, got %d: %s", w.Code, w.Body.String())
		}
	}
	if w := callWithParams(e.h.ConfirmProposal, nil, http.MethodPost, "/x", "id", sess.ID, "turn", turn.ID); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", w.Code)
	}
	if e.turn(t, turn.ID).Status != store.TurnAwaitingConfirmation {
		t.Fatal("refused requests changed the turn")
	}

	// By seq.
	if w := e.confirm(t, e.editor, sess.ID, fmt.Sprint(turn.Seq)); w.Code != http.StatusOK || !e.hasColumn(t, "done") {
		t.Fatalf("confirm by seq: %d %s", w.Code, w.Body.String())
	}
}

func TestProposalRoutes_NotAwaitingTurnIs409(t *testing.T) {
	e := newBPropEnv(t)
	ctx := context.Background()
	sess, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.editor.ID, BackendAppID: e.appID, TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.st.CreateTurn(ctx, e.editor.ID, sess.ID, store.KindChat, "hi")
	if err != nil {
		t.Fatal(err)
	}
	for name, fn := range map[string]func(*testing.T, *DashboardUser, string, string) *httptest.ResponseRecorder{"confirm": e.confirm, "reject": e.reject} {
		if w := fn(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusConflict {
			t.Errorf("%s of a pending turn: want 409, got %d: %s", name, w.Code, w.Body.String())
		}
	}
}

func TestProposalRoutes_ArchivedSessionIs404(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)
	if err := e.st.ArchiveSession(context.Background(), e.editor.ID, sess.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.confirm(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
	if e.hasColumn(t, "done") {
		t.Error("an archived session's proposal was applied")
	}
}

// ---- the turn view ----

func TestBuilderTurnView_ExposesThePendingProposalOnlyWhileAwaiting(t *testing.T) {
	e := newBPropEnv(t)
	sess, turn := e.awaiting(t, e.editor, bpropAddColumn)

	detail := func() BuilderSessionDetail {
		return decodeJSON[BuilderSessionDetail](t, e.get(t, e.editor, sess.ID))
	}
	d := detail()
	if len(d.Turns) != 1 || d.Turns[0].PendingProposal == nil {
		t.Fatalf("awaiting turn must carry its proposal: %+v", d.Turns)
	}
	p := d.Turns[0].PendingProposal
	if p.ToolCallID != "call_1" || p.Name != "propose_backend_change" {
		t.Errorf("proposal = %+v", p)
	}
	var op struct {
		Operation struct {
			Kind string `json:"kind"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(p.Operation, &op); err != nil || op.Operation.Kind != "add_column" {
		t.Errorf("operation = %s (%v)", p.Operation, err)
	}

	if w := e.reject(t, e.editor, sess.ID, turn.ID); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if d := detail(); d.Turns[0].PendingProposal != nil {
		t.Errorf("a resolved turn still exposes its proposal: %+v", d.Turns[0].PendingProposal)
	}
	if raw := e.get(t, e.editor, sess.ID).Body.String(); strings.Contains(raw, "pending_proposal") {
		t.Errorf("pending_proposal must be omitted once resolved: %s", raw)
	}
}

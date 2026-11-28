package dashboard

// builder_attach_test.go — POST .../backend (NB-08, T11e): the request is
// decided and recorded as an attach_backend turn; nothing is created yet.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

func (e *bbEnv) attach(t *testing.T, user *DashboardUser, sessionID, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.AttachBackend, user, http.MethodPost,
		"/dashboard/api/builder/sessions/"+sessionID+"/backend", body, "id", sessionID)
}

// siteSession creates a session with no backend through the real create route.
func (e *bbEnv) siteSession(t *testing.T, user *DashboardUser, title string) BuilderSessionView {
	t.Helper()
	e.fake.siteTemplate = true
	w := e.create(t, user, fmt.Sprintf(`{"backend":{"mode":"none"},"title":%q}`, title))
	if w.Code != http.StatusCreated {
		t.Fatalf("create no-backend session: %d %s", w.Code, w.Body.String())
	}
	return decodeJSON[BuilderSessionView](t, w)
}

func (e *bbEnv) attachTurn(t *testing.T, view BuilderTurnView) (*store.Turn, store.AttachPayload) {
	t.Helper()
	turn, err := e.st.GetTurn(context.Background(), view.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := store.AttachPayloadOf(turn)
	if !ok {
		t.Fatalf("turn %s has no attach payload: %s", turn.Kind, turn.PendingProposal)
	}
	return turn, p
}

func TestBuilderAttach_New_RecordsATurnAndCreatesNothing(t *testing.T) {
	e := newBBEnv(t)
	pokes := e.withPokeBB()
	sess := e.siteSession(t, e.owner, "Padaria Central")
	before := e.appCount(t)

	w := e.attach(t, e.owner, sess.ID, `{"mode":"new"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
	view := decodeJSON[BuilderTurnView](t, w)
	if view.Kind != store.KindAttachBackend || view.Status != store.TurnPending {
		t.Errorf("turn = %+v", view)
	}
	turn, p := e.attachTurn(t, view)
	if turn.Seq != 1 || p.Mode != store.AttachModeNew || p.AppID != "" {
		t.Errorf("turn seq %d, payload %+v", turn.Seq, p)
	}
	if !appNameRe.MatchString(p.AppName) || !strings.HasPrefix(p.AppName, "padaria-central-") {
		t.Errorf("app name %q must come from the session title", p.AppName)
	}
	if !strings.Contains(p.Config, "ORBIT_APP_NAME: string = '"+p.AppName+"'") ||
		!strings.Contains(p.Config, "https://orbit.test.example") {
		t.Errorf("config = %q", p.Config)
	}
	if n := e.appCount(t); n != before {
		t.Errorf("the route creates no app (the worker does): %d -> %d", before, n)
	}
	if got := e.reload(t, &store.Session{ID: sess.ID, OwnerUserID: e.owner.ID}); got.BackendOrigin != store.BackendOriginNone {
		t.Errorf("the session must stay without a backend until the worker attaches: %q", got.BackendOrigin)
	}
	if pokes.Load() != 1 {
		t.Errorf("worker pokes = %d, want 1", pokes.Load())
	}
	var meta string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT metadata::text FROM zeep_system.audit_log WHERE action = 'builder.session.backend.attach'`).Scan(&meta); err != nil ||
		!strings.Contains(meta, `"mode": "new"`) && !strings.Contains(meta, `"mode":"new"`) {
		t.Errorf("audit row: %v %s", err, meta)
	}
}

func TestBuilderAttach_Existing_UsesTheAppNameAndAuditsTheAppID(t *testing.T) {
	e := newBBEnv(t)
	sess := e.siteSession(t, e.viewer, "Loja")

	w := e.attach(t, e.viewer, sess.ID, fmt.Sprintf(`{"mode":"existing","app_id":%q}`, e.appA))
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body.String())
	}
	_, p := e.attachTurn(t, decodeJSON[BuilderTurnView](t, w))
	if p.Mode != store.AttachModeExisting || p.AppID != e.appA || p.AppName != "" {
		t.Errorf("payload = %+v", p)
	}
	if !strings.Contains(p.Config, "ORBIT_APP_NAME: string = 'bh_app_a'") {
		t.Errorf("config = %q", p.Config)
	}
	var meta string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT metadata::text FROM zeep_system.audit_log WHERE action = 'builder.session.backend.attach'`).Scan(&meta); err != nil ||
		!strings.Contains(meta, e.appA) {
		t.Errorf("audit must carry the app id of an existing backend: %v %s", err, meta)
	}
}

func TestBuilderAttach_Rejections(t *testing.T) {
	e := newBBEnv(t)
	sess := e.siteSession(t, e.owner, "Loja")

	cases := []struct {
		name   string
		user   *DashboardUser
		id     string
		body   string
		status int
	}{
		{"no mode", e.owner, sess.ID, `{}`, 400},
		{"mode none", e.owner, sess.ID, `{"mode":"none"}`, 400},
		{"new with an app id", e.owner, sess.ID, fmt.Sprintf(`{"mode":"new","app_id":%q}`, e.appA), 400},
		{"existing without an app id", e.owner, sess.ID, `{"mode":"existing"}`, 400},
		{"existing with a bad app id", e.owner, sess.ID, `{"mode":"existing","app_id":"nope"}`, 400},
		{"malformed body", e.owner, sess.ID, `{`, 400},
		{"existing app the caller cannot see", e.owner, sess.ID, fmt.Sprintf(`{"mode":"existing","app_id":%q}`, e.appB), 404},
		{"someone else's session", e.outsider, sess.ID, `{"mode":"new"}`, 404},
		{"unknown session", e.owner, "00000000-0000-0000-0000-000000000000", `{"mode":"new"}`, 404},
		{"not a uuid", e.owner, "nope", `{"mode":"new"}`, 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := e.attach(t, tc.user, tc.id, tc.body); w.Code != tc.status {
				t.Fatalf("want %d, got %d: %s", tc.status, w.Code, w.Body.String())
			}
		})
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.builder_turns WHERE kind = 'attach_backend'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a rejected request must create no turn: %d %v", n, err)
	}
}

func TestBuilderAttach_SessionThatAlreadyHasABackendIs409(t *testing.T) {
	e := newBBEnv(t)
	sess := e.createOK(t, e.owner, e.appA)
	w := e.attach(t, e.owner, sess.ID, `{"mode":"new"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already has a backend") {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuilderAttach_TurnInProgressIs409AndASecondAttachToo(t *testing.T) {
	e := newBBEnv(t)
	sess := e.siteSession(t, e.owner, "Loja")
	if w := e.attach(t, e.owner, sess.ID, `{"mode":"new"}`); w.Code != http.StatusAccepted {
		t.Fatalf("first attach: %d %s", w.Code, w.Body.String())
	}
	if w := e.attach(t, e.owner, sess.ID, `{"mode":"new"}`); w.Code != http.StatusConflict {
		t.Fatalf("second attach while the first is pending: want 409, got %d: %s", w.Code, w.Body.String())
	}
	if w := e.message(t, e.owner, sess.ID, `{"content":"hello"}`); w.Code != http.StatusConflict {
		t.Fatalf("a message during the attach: want 409, got %d", w.Code)
	}
}

func TestBuilderAttach_CompanyTemplateSessionIs400(t *testing.T) {
	e := newBBEnv(t)
	tpl := "00000000-0000-0000-0000-0000000000aa"
	s, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.owner.ID, TemplateKind: store.TemplateGitHub, TemplateID: &tpl,
		TemplateVersion: "v1", BackendOrigin: store.BackendOriginNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := e.attach(t, e.owner, s.ID, `{"mode":"new"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuilderAttach_BuilderNotConfiguredIs503(t *testing.T) {
	e := newBBEnv(t)
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{Logger: nil})
	w := e.attach(t, e.owner, "00000000-0000-0000-0000-000000000000", `{"mode":"new"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBuilderAttach_NewNeedsTheDashboardHandler(t *testing.T) {
	e := newBBEnv(t)
	sess := e.siteSession(t, e.owner, "Loja")
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, PublicBaseURL: "https://orbit.test.example",
	})
	if w := e.attach(t, e.owner, sess.ID, `{"mode":"new"}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
}

// withPokeBB is withPoke for an env whose handler also creates apps.
func (e *bbEnv) withPokeBB() *atomic.Int32 {
	var n atomic.Int32
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
		Dashboard: e.dash, Poke: func() { n.Add(1) },
	})
	return &n
}

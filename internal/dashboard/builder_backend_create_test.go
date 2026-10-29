package dashboard

// Tests for the backend selection of a new builder session (ai-builder-backend
// B02): the pure helpers, then the real create path against Postgres with the
// shared fake sandbox and a real provisioned backend.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func TestResolveBuilderBackend(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	other := "22222222-2222-2222-2222-222222222222"
	cases := []struct {
		name       string
		b          *builderBackendRequest
		legacy     string
		wantMode   string
		wantID     string
		wantErrSub string
	}{
		{name: "legacy only", legacy: " " + id + " ", wantMode: "existing", wantID: id},
		{name: "legacy empty stays existing", wantMode: "existing", wantID: ""},
		{name: "new", b: &builderBackendRequest{Mode: "new"}, wantMode: "new"},
		{name: "new with spaces", b: &builderBackendRequest{Mode: " new "}, wantMode: "new"},
		{name: "existing with app id", b: &builderBackendRequest{Mode: "existing", AppID: id}, wantMode: "existing", wantID: id},
		{name: "existing falls back to legacy", b: &builderBackendRequest{Mode: "existing"}, legacy: id, wantMode: "existing", wantID: id},
		{name: "existing same id twice", b: &builderBackendRequest{Mode: "existing", AppID: id}, legacy: strings.ToUpper(id), wantMode: "existing", wantID: id},
		{name: "existing ids differ", b: &builderBackendRequest{Mode: "existing", AppID: id}, legacy: other, wantErrSub: "differ"},
		{name: "new with app id", b: &builderBackendRequest{Mode: "new", AppID: id}, wantErrSub: "cannot be combined"},
		{name: "new with legacy id", b: &builderBackendRequest{Mode: "new"}, legacy: id, wantErrSub: "cannot be combined"},
		{name: "unknown mode", b: &builderBackendRequest{Mode: "cloud"}, wantErrSub: "must be"},
		{name: "empty mode", b: &builderBackendRequest{}, wantErrSub: "must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, gotID, err := resolveBuilderBackend(tc.b, tc.legacy)
			if tc.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("want error containing %q, got %v", tc.wantErrSub, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tc.wantMode || !strings.EqualFold(gotID, tc.wantID) {
				t.Errorf("got (%q, %q), want (%q, %q)", mode, gotID, tc.wantMode, tc.wantID)
			}
		})
	}
}

func TestDeriveBuilderAppName(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Clínica São João", "clinica-sao-joao-abc123"},
		{"  My   Booking  App!! ", "my-booking-app-abc123"},
		{"123 reservas", "app-123-reservas-abc123"},
		{"", "app-abc123"},
		{"!!!", "app-abc123"},
		{"日本語のタイトル", "app-abc123"},
		{"under_score and-dash", "under-score-and-dash-abc123"},
		{strings.Repeat("a", 80), strings.Repeat("a", builderAppNameBaseMax) + "-abc123"},
		{strings.Repeat("ab ", 20), "ab-ab-ab-ab-ab-ab-ab-ab-a-abc123"},
	}
	for _, tc := range cases {
		got := deriveBuilderAppName(tc.title, "abc123")
		if !appNameRe.MatchString(got) {
			t.Errorf("deriveBuilderAppName(%q) = %q is not a valid app name", tc.title, got)
		}
		if got != tc.want {
			t.Errorf("deriveBuilderAppName(%q) = %q, want %q", tc.title, got, tc.want)
		}
		if len(got) > 32 || strings.Contains(got, "--") {
			t.Errorf("deriveBuilderAppName(%q) = %q breaks the length or hyphen rules", tc.title, got)
		}
	}
}

// ---- integration ----

// bbEnv is a builder env whose handler can create real backend apps.
type bbEnv struct {
	*bhEnv
	dash *Handler
}

func newBBEnv(t *testing.T) *bbEnv {
	t.Helper()
	e := &bbEnv{bhEnv: newBHEnv(t)}
	e.dash = NewHandler(e.pool, registry.New(), zap.NewNop())
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example", Dashboard: e.dash,
	})
	return e
}

func (e *bbEnv) appCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.apps`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *bbEnv) auditCount(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = $1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBuilderCreate_NewBackend_CreatesAppAndSession(t *testing.T) {
	e := newBBEnv(t)
	before := e.appCount(t)

	w := e.create(t, e.outsider, `{"backend":{"mode":"new"},"title":"Clínica São João"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSessionView](t, w)
	if got.BackendOrigin != store.BackendOriginBuilder {
		t.Errorf("backend_origin = %q, want builder", got.BackendOrigin)
	}
	if got.Title != "Clínica São João" {
		t.Errorf("title = %q", got.Title)
	}
	if n := e.appCount(t); n != before+1 {
		t.Fatalf("exactly one app must be created, apps %d -> %d", before, n)
	}

	var name, ownerID string
	var emailAuth bool
	if err := e.pool.QueryRow(context.Background(),
		`SELECT name, owner_id::text, auth_email_enabled FROM zeep_system.apps WHERE id = $1`, got.BackendAppID,
	).Scan(&name, &ownerID, &emailAuth); err != nil {
		t.Fatalf("the session's backend app must exist: %v", err)
	}
	if !regexp.MustCompile(`^clinica-sao-joao-[0-9a-f]{6}$`).MatchString(name) {
		t.Errorf("app name = %q", name)
	}
	if ownerID != e.outsider.ID {
		t.Errorf("the caller must own the new app: owner %s, caller %s", ownerID, e.outsider.ID)
	}
	if !emailAuth {
		t.Error("email auth must be on so owner-scoped access works later")
	}
	if n := e.auditCount(t, "app.create"); n != 1 {
		t.Errorf("app.create audit rows = %d, want 1", n)
	}
	var meta string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT metadata::text FROM zeep_system.audit_log WHERE action = 'builder.session.create' AND resource_id = $1`, got.ID,
	).Scan(&meta); err != nil || !strings.Contains(meta, `"backend_origin": "builder"`) {
		t.Errorf("session audit must record the origin: %v %s", err, meta)
	}
}

func TestBuilderCreate_NewBackend_DefaultTitle(t *testing.T) {
	e := newBBEnv(t)
	w := e.create(t, e.owner, `{"backend":{"mode":"new"}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[BuilderSessionView](t, w); got.Title != builderDefaultSessionTitle {
		t.Errorf("title = %q, want %q", got.Title, builderDefaultSessionTitle)
	}
}

func TestBuilderCreate_ExistingBackend_BothFormsKeepOrigin(t *testing.T) {
	e := newBBEnv(t)
	before := e.appCount(t)
	for name, body := range map[string]string{
		"legacy":   fmt.Sprintf(`{"backend_app_id":%q}`, e.appA),
		"explicit": fmt.Sprintf(`{"backend":{"mode":"existing","app_id":%q}}`, e.appA),
	} {
		w := e.create(t, e.viewer, body)
		if w.Code != http.StatusCreated {
			t.Fatalf("%s: want 201, got %d: %s", name, w.Code, w.Body.String())
		}
		got := decodeJSON[BuilderSessionView](t, w)
		if got.BackendOrigin != store.BackendOriginExisting || got.BackendAppID != e.appA {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if n := e.appCount(t); n != before {
		t.Errorf("an existing backend must not create apps: %d -> %d", before, n)
	}
}

func TestBuilderCreate_BackendSelectionInvalid400(t *testing.T) {
	e := newBBEnv(t)
	before := e.appCount(t)
	for name, body := range map[string]string{
		"new with app id":    fmt.Sprintf(`{"backend":{"mode":"new","app_id":%q}}`, e.appA),
		"new with legacy id": fmt.Sprintf(`{"backend":{"mode":"new"},"backend_app_id":%q}`, e.appA),
		"unknown mode":       `{"backend":{"mode":"cloud"}}`,
		"empty backend":      `{"backend":{}}`,
		"existing no id":     `{"backend":{"mode":"existing"}}`,
		"existing bad id":    `{"backend":{"mode":"existing","app_id":"nope"}}`,
		"ids differ":         fmt.Sprintf(`{"backend":{"mode":"existing","app_id":%q},"backend_app_id":%q}`, e.appA, e.appB),
	} {
		if w := e.create(t, e.owner, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	if n := e.appCount(t); n != before || e.sessionCount(t) != 0 {
		t.Errorf("a rejected request must create nothing: apps %d -> %d, sessions %d", before, n, e.sessionCount(t))
	}
}

// A request rejected by a cheap check never reaches app creation.
func TestBuilderCreate_NewBackend_RejectedRequestCreatesNoApp(t *testing.T) {
	e := newBBEnv(t)
	before := e.appCount(t)
	long := strings.Repeat("x", builderMaxTitleRunes+1)
	for name, body := range map[string]string{
		"bad locale": `{"backend":{"mode":"new"},"locale":"en US; drop"}`,
		"long title": fmt.Sprintf(`{"backend":{"mode":"new"},"title":%q}`, long),
		"no template": `{"backend":{"mode":"new"},"template_id":"` +
			"33333333-3333-3333-3333-333333333333" + `"}`,
	} {
		if w := e.create(t, e.owner, body); w.Code < 400 {
			t.Errorf("%s: want an error, got %d", name, w.Code)
		}
	}
	if n := e.appCount(t); n != before {
		t.Errorf("rejected requests created apps: %d -> %d", before, n)
	}
}

func TestBuilderCreate_NewBackend_WithoutDashboardIs503AndCreatesNothing(t *testing.T) {
	e := newBHEnv(t) // handler has no Dashboard
	var before int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.apps`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w := e.create(t, e.owner, `{"backend":{"mode":"new"}}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
	var after int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.apps`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("no app may be created: %d -> %d", before, after)
	}
}

// BBE-02: a failure after the app was created removes that app again, with the
// same registry and audit effects as a user delete, and leaks nothing.
func TestBuilderCreate_NewBackend_FailureRemovesTheCreatedApp(t *testing.T) {
	e := newBBEnv(t)
	before := e.appCount(t)
	e.fake.initErr, e.fake.initErrOn = errors.New("boom: /work/secret/path"), 1

	w := e.create(t, e.owner, `{"backend":{"mode":"new"},"title":"Doomed"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
	for _, leak := range []string{"boom", "/work/secret"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("response leaks %q: %s", leak, w.Body.String())
		}
	}
	if n := e.appCount(t); n != before {
		t.Errorf("the created app must be removed: apps %d -> %d", before, n)
	}
	if n := e.sessionCount(t); n != 0 {
		t.Errorf("no session row may remain once its app is removed, found %d", n)
	}
	if n := e.auditCount(t, "app.create"); n != 1 {
		t.Errorf("app.create rows = %d, want 1", n)
	}
	var meta string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT metadata::text FROM zeep_system.audit_log WHERE action = 'app.delete'`).Scan(&meta); err != nil ||
		!strings.Contains(meta, "builder_session_create_failed") {
		t.Errorf("app.delete must be audited with its reason: %v %s", err, meta)
	}
}

// An existing backend is never removed, whatever happens to the session.
func TestBuilderCreate_ExistingBackend_FailureKeepsTheApp(t *testing.T) {
	e := newBBEnv(t)
	before := e.appCount(t)
	e.fake.initErr, e.fake.initErrOn = errors.New("boom"), 1
	if w := e.create(t, e.owner, fmt.Sprintf(`{"backend":{"mode":"existing","app_id":%q}}`, e.appA)); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
	if n := e.appCount(t); n != before {
		t.Errorf("an existing app must stay: %d -> %d", before, n)
	}
	if n := e.auditCount(t, "app.delete"); n != 0 {
		t.Errorf("no app.delete expected, got %d", n)
	}
}

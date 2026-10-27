package dashboard

// builder_handlers_test.go — ai-frontend-builder T24 (status) and T25 (session
// create/list/get/delete). Integration tests: real Postgres through
// TEST_DATABASE_URL, a fake sandbox.Sandbox in place of the builder daemon, a
// mock transport in place of GitHub. The handlers are called directly, like
// the other dashboard handler tests.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ---- fake builder ----

// bhFakeSandbox implements sandbox.Sandbox for the session endpoints: Init,
// Archive, Destroy and Health work; everything else fails loudly, because a
// create/list/get/delete must never call it.
type bhFakeSandbox struct {
	mu sync.Mutex

	files map[string]map[string]string // session id -> path -> content
	n     int

	health     sandbox.Health
	healthErr  error
	initErr    error
	initErrOn  int // fail only the Nth Init call (1-based); 0 = every call
	destroyErr error

	inits     []bhInitCall
	destroyed []string
}

type bhInitCall struct {
	session  string
	template string
	tarFiles map[string]string // nil for a Name template
}

func newBHFakeSandbox() *bhFakeSandbox {
	return &bhFakeSandbox{
		files:  map[string]map[string]string{},
		health: sandbox.Health{Capacity: 4, Version: "test", DefaultTemplateVersion: "7"},
	}
}

func bhDefaultTemplateFiles() map[string]string {
	return map[string]string{
		"package.json":        `{"name":"default-template"}` + "\n",
		"TEMPLATE_VERSION":    "7\n",
		"ORBIT_AGENT.md":      "# Agent guide\n",
		"src/App.tsx":         "export default function App() { return null }\n",
		"src/orbit.config.ts": "export const ORBIT_API_URL: string = 'https://orbit.example.invalid'\nexport const ORBIT_APP_NAME: string = 'your_app_name'\n",
	}
}

func (f *bhFakeSandbox) Init(ctx context.Context, id string, tmpl sandbox.TemplateSource) (sandbox.InitResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := bhInitCall{session: id, template: tmpl.Name}
	if tmpl.Tar != nil {
		files, err := bhReadTar(tmpl.Tar)
		if err != nil {
			return sandbox.InitResult{}, &sandbox.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_archive", Message: "bad archive", Err: err}
		}
		call.tarFiles = files
	}
	f.inits = append(f.inits, call)
	if f.initErr != nil && (f.initErrOn == 0 || f.initErrOn == len(f.inits)) {
		return sandbox.InitResult{}, f.initErr
	}
	if _, exists := f.files[id]; exists {
		return sandbox.InitResult{}, &sandbox.Error{Status: http.StatusConflict, Code: "session_exists", Message: "session already exists"}
	}
	switch {
	case tmpl.Name == "default":
		f.files[id] = bhDefaultTemplateFiles()
	case tmpl.Name != "":
		return sandbox.InitResult{}, &sandbox.Error{Status: http.StatusNotFound, Code: "unknown_template", Message: "unknown template"}
	default:
		f.files[id] = call.tarFiles
	}
	f.n++
	sha := fmt.Sprintf("%040x", f.n)
	return sandbox.InitResult{
		SHA:             sha,
		Bundle:          []byte("bundle:" + sha),
		TemplateVersion: strings.TrimSpace(f.files[id]["TEMPLATE_VERSION"]),
	}, nil
}

func (f *bhFakeSandbox) Archive(ctx context.Context, id, sha string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files, ok := f.files[id]
	if !ok {
		return nil, sandbox.ErrUnknownSession
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// `git archive` starts with a pax global header carrying the commit id.
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": sha}})
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: n, Mode: 0o644, Size: int64(len(files[n]))})
		_, _ = tw.Write([]byte(files[n]))
	}
	_ = tw.Close()
	return io.NopCloser(&buf), nil
}

func (f *bhFakeSandbox) Destroy(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, id)
	if f.destroyErr != nil {
		return f.destroyErr
	}
	delete(f.files, id)
	return nil
}

func (f *bhFakeSandbox) Health(ctx context.Context) (sandbox.Health, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.health, f.healthErr
}

func (f *bhFakeSandbox) Restore(context.Context, string, []sandbox.Bundle) error {
	return errors.New("bhFakeSandbox: Restore must not be called")
}
func (f *bhFakeSandbox) Op(context.Context, string, string, sandbox.FileOp) (sandbox.OpResult, error) {
	return sandbox.OpResult{}, errors.New("bhFakeSandbox: Op must not be called")
}
func (f *bhFakeSandbox) Build(context.Context, string, string, sandbox.BuildStep, string) (sandbox.BuildResult, error) {
	return sandbox.BuildResult{}, errors.New("bhFakeSandbox: Build must not be called")
}
func (f *bhFakeSandbox) BuildLog(context.Context, string, sandbox.BuildStep, int) (string, error) {
	return "", errors.New("bhFakeSandbox: BuildLog must not be called")
}
func (f *bhFakeSandbox) Commit(context.Context, string, int, string) (sandbox.CommitResult, error) {
	return sandbox.CommitResult{}, errors.New("bhFakeSandbox: Commit must not be called")
}
func (f *bhFakeSandbox) Revert(context.Context, string, string, int) (sandbox.CommitResult, error) {
	return sandbox.CommitResult{}, errors.New("bhFakeSandbox: Revert must not be called")
}
func (f *bhFakeSandbox) Preview(string) http.Handler { return http.NotFoundHandler() }

func (f *bhFakeSandbox) snapshot() (inits []bhInitCall, destroyed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bhInitCall(nil), f.inits...), append([]string(nil), f.destroyed...)
}

func (f *bhFakeSandbox) workspace(id string) (map[string]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files, ok := f.files[id]
	return files, ok
}

func bhReadTar(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeDir:
		case tar.TypeReg:
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			out[hdr.Name] = string(b)
		default:
			return nil, fmt.Errorf("unsupported entry type %q", hdr.Typeflag)
		}
	}
}

// ---- environment ----

type bhEnv struct {
	pool *db.Pool
	st   *store.Store
	fake *bhFakeSandbox
	h    *BuilderHandler

	owner    *DashboardUser // admin of appA
	viewer   *DashboardUser // viewer member of appA
	outsider *DashboardUser // no membership anywhere
	other    *DashboardUser // admin of appB only
	super    *DashboardUser

	appA string
	appB string
}

type bhOption func(*bhEnv)

func bhWithoutBuilder() bhOption { return func(e *bhEnv) { e.h = nil } }

// newBHEnv provisions a clean zeep_system, an enabled builder with a usable
// provider, and a handler wired to a fake sandbox.
func newBHEnv(t *testing.T, opts ...bhOption) *bhEnv {
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
	mkUser := func(label, role string) *DashboardUser {
		u, err := CreateUser(ctx, pool, fmt.Sprintf("bh-%s-%d@example.com", label, suffix), label, "hash", role)
		if err != nil {
			t.Fatalf("create user %s: %v", label, err)
		}
		return u
	}
	e := &bhEnv{
		pool:     pool,
		st:       store.New(pool),
		fake:     newBHFakeSandbox(),
		owner:    mkUser("owner", "member"),
		viewer:   mkUser("viewer", "member"),
		outsider: mkUser("outsider", "member"),
		other:    mkUser("other", "member"),
		super:    mkUser("super", "superadmin"),
	}
	mkApp := func(name string, ownerID string) string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`, name, ownerID).Scan(&id); err != nil {
			t.Fatalf("create app %s: %v", name, err)
		}
		return id
	}
	member := func(appID, userID, role string) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, $3)`, appID, userID, role); err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}
	e.appA = mkApp("bh_app_a", e.owner.ID)
	e.appB = mkApp("bh_app_b", e.other.ID)
	member(e.appA, e.owner.ID, "admin")
	member(e.appA, e.viewer.ID, "viewer")
	member(e.appB, e.other.ID, "admin")

	// Builder enabled, provider usable (openai_compatible needs no key).
	baseURL := "https://llm.internal.example/v1"
	if _, err := UpsertAIProvider(ctx, pool, aiProviderOpenAICompatible,
		&aiProviderUpsertInput{Model: "test-model", Enabled: true, BaseURL: &baseURL}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	enabled, prov := true, aiProviderOpenAICompatible
	if _, err := UpsertBuilderSettings(ctx, pool, e.super.ID, BuilderSettingsInput{Enabled: &enabled, Provider: &prov}); err != nil {
		t.Fatalf("seed builder settings: %v", err)
	}

	e.h = NewBuilderHandler(pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
	})
	for _, o := range opts {
		o(e)
	}
	if e.h == nil {
		e.h = NewBuilderHandler(pool, BuilderHandlerConfig{Logger: zap.NewNop()})
	}
	return e
}

func (e *bhEnv) setSettings(t *testing.T, in BuilderSettingsInput) {
	t.Helper()
	if _, err := UpsertBuilderSettings(context.Background(), e.pool, e.super.ID, in); err != nil {
		t.Fatalf("update builder settings: %v", err)
	}
}

// do calls fn as user with chi URL params (key, value, key, value...).
func (e *bhEnv) do(t *testing.T, fn http.HandlerFunc, user *DashboardUser, method, target, body string, params ...string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if user != nil {
		req = withUser(req, user)
	}
	for i := 0; i+1 < len(params); i += 2 {
		req = withURLParam(req, params[i], params[i+1])
	}
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func (e *bhEnv) create(t *testing.T, user *DashboardUser, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.CreateSession, user, http.MethodPost, "/dashboard/api/builder/sessions", body)
}

func (e *bhEnv) createOK(t *testing.T, user *DashboardUser, appID string) BuilderSessionView {
	t.Helper()
	w := e.create(t, user, fmt.Sprintf(`{"backend_app_id":%q}`, appID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create session: want 201, got %d: %s", w.Code, w.Body.String())
	}
	return decodeJSON[BuilderSessionView](t, w)
}

func (e *bhEnv) get(t *testing.T, user *DashboardUser, id string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.GetSession, user, http.MethodGet, "/dashboard/api/builder/sessions/"+id, "", "id", id)
}

func (e *bhEnv) del(t *testing.T, user *DashboardUser, id string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.DeleteSession, user, http.MethodDelete, "/dashboard/api/builder/sessions/"+id, "", "id", id)
}

func (e *bhEnv) sessionCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.builder_sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func (e *bhEnv) sessionStatus(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT status FROM zeep_system.builder_sessions WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("session status: %v", err)
	}
	return s
}

func bhErrorBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeJSON[map[string]string](t, w)["error"]
}

// ---- T24: status ----

func TestBuilderStatus_UnauthenticatedIs401(t *testing.T) {
	e := newBHEnv(t)
	if w := e.do(t, e.h.Status, nil, http.MethodGet, "/dashboard/api/builder/status", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestBuilderStatus_NotConfigured(t *testing.T) {
	e := newBHEnv(t, bhWithoutBuilder())
	w := e.do(t, e.h.Status, e.owner, http.MethodGet, "/dashboard/api/builder/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status must answer without a builder: got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderStatus](t, w)
	if got.Available || got.Reason != "not_configured" {
		t.Errorf("want unavailable/not_configured, got %+v", got)
	}
	// Still tells whether the provider is usable, so the UI can show what is missing.
	if !got.ProviderConfigured {
		t.Errorf("provider_configured should be reported even without a builder: %+v", got)
	}
}

func TestBuilderStatus_Disabled(t *testing.T) {
	e := newBHEnv(t)
	off := false
	e.setSettings(t, BuilderSettingsInput{Enabled: &off})
	got := decodeJSON[BuilderStatus](t, e.do(t, e.h.Status, e.owner, http.MethodGet, "/x", ""))
	if got.Available || got.Reason != "disabled" || !got.ProviderConfigured {
		t.Errorf("want unavailable/disabled with provider configured, got %+v", got)
	}
}

func TestBuilderStatus_ProviderNotConfigured(t *testing.T) {
	e := newBHEnv(t)
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM zeep_system.ai_providers`); err != nil {
		t.Fatalf("remove provider: %v", err)
	}
	got := decodeJSON[BuilderStatus](t, e.do(t, e.h.Status, e.owner, http.MethodGet, "/x", ""))
	if got.Available || got.Reason != "provider_not_configured" || got.ProviderConfigured {
		t.Errorf("want unavailable/provider_not_configured, got %+v", got)
	}
}

func TestBuilderStatus_BuilderUnreachable(t *testing.T) {
	e := newBHEnv(t)
	e.fake.healthErr = errors.New("dial tcp 10.1.2.3:8080: connection refused")
	w := e.do(t, e.h.Status, e.owner, http.MethodGet, "/x", "")
	got := decodeJSON[BuilderStatus](t, w)
	if got.Available || got.Reason != "builder_unreachable" || !got.ProviderConfigured {
		t.Errorf("want unavailable/builder_unreachable, got %+v", got)
	}
	if strings.Contains(w.Body.String(), "10.1.2.3") {
		t.Errorf("the transport error must not reach the response: %s", w.Body.String())
	}
}

func TestBuilderStatus_Available(t *testing.T) {
	e := newBHEnv(t)
	got := decodeJSON[BuilderStatus](t, e.do(t, e.h.Status, e.viewer, http.MethodGet, "/x", ""))
	if !got.Available || !got.ProviderConfigured || got.Reason != "" {
		t.Errorf("want available, got %+v", got)
	}
}

// ---- T25: create ----

func TestBuilderCreate_ViewerCanCreate_RecordsVersionsLocaleAndTurn0(t *testing.T) {
	e := newBHEnv(t)
	// A persona saved before the session is the one pinned to it.
	pv, _, err := SaveBuilderPrompt(context.Background(), e.pool, e.super.ID, BuilderPromptInput{Persona: bhStrPtr("v1 persona"), Rules: bhStrPtr("")})
	if err != nil || pv == nil {
		t.Fatalf("seed prompt: %v", err)
	}

	w := e.create(t, e.viewer, fmt.Sprintf(`{"backend_app_id":%q,"title":"  My site  ","locale":"pt-br"}`, e.appA))
	if w.Code != http.StatusCreated {
		t.Fatalf("a viewer on the backend app can create: want 201, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSessionView](t, w)
	if got.Title != "My site" || got.Locale != "pt-BR" || got.Status != "idle" {
		t.Errorf("unexpected session: %+v", got)
	}
	if got.TemplateKind != "default" || got.TemplateID != nil || got.TemplateVersion != "7" {
		t.Errorf("template must be the default at the builder's version: %+v", got)
	}
	if got.PromptVersionID == nil || *got.PromptVersionID != pv.ID {
		t.Errorf("prompt version not pinned: %+v want %s", got.PromptVersionID, pv.ID)
	}
	if got.BackendAppID != e.appA {
		t.Errorf("backend app = %s, want %s", got.BackendAppID, e.appA)
	}

	// Owner is the caller, not anybody else.
	sess, err := e.st.GetSession(context.Background(), e.viewer.ID, got.ID)
	if err != nil {
		t.Fatalf("session must belong to the creator: %v", err)
	}

	// Snapshot turn 0: head recorded, one full-bundle row.
	if sess.HeadSHA == "" || sess.HeadTurnSeq != 0 || got.HeadSHA != sess.HeadSHA {
		t.Errorf("head not recorded: %+v", sess)
	}
	snaps, err := e.st.ListSnapshots(context.Background(), got.ID)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("want exactly one turn 0 snapshot, got %d (err %v)", len(snaps), err)
	}
	if snaps[0].TurnSeq != 0 || snaps[0].Part != 0 || snaps[0].CommitSHA != sess.HeadSHA || len(snaps[0].Bundle) == 0 {
		t.Errorf("bad turn 0 snapshot: %+v", snaps[0])
	}

	// The workspace the builder keeps is the final one: template files plus the
	// generated config, and the placeholder is gone.
	files, ok := e.fake.workspace(got.ID)
	if !ok {
		t.Fatal("the workspace must exist after create")
	}
	cfg := files["src/orbit.config.ts"]
	for _, want := range []string{"export const ORBIT_API_URL: string = 'https://orbit.test.example'", "export const ORBIT_APP_NAME: string = 'bh_app_a'"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("orbit.config.ts lacks %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "example.invalid") || strings.Contains(cfg, "your_app_name") {
		t.Errorf("placeholder values survived:\n%s", cfg)
	}
	for _, keep := range []string{"package.json", "TEMPLATE_VERSION", "ORBIT_AGENT.md", "src/App.tsx"} {
		if _, ok := files[keep]; !ok {
			t.Errorf("template file %s was lost", keep)
		}
	}
	// The snapshot is the commit of the final workspace (second Init), not the throwaway one.
	inits, destroyed := e.fake.snapshot()
	if len(inits) != 2 || inits[0].template != "default" || inits[1].tarFiles == nil || len(destroyed) != 1 || destroyed[0] != got.ID {
		t.Errorf("default template flow: inits=%+v destroyed=%v", inits, destroyed)
	}
	if string(snaps[0].Bundle) != "bundle:"+sess.HeadSHA {
		t.Errorf("snapshot bundle is not the one returned by the last Init")
	}

	// Audit row.
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'builder.session.create' AND resource_id = $1`, got.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("want one audit row, got %d (err %v)", n, err)
	}
}

func bhStrPtr(s string) *string { return &s }

func TestBuilderCreate_LocaleFromAcceptLanguageAndDefaults(t *testing.T) {
	e := newBHEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(fmt.Sprintf(`{"backend_app_id":%q}`, e.appA)))
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.8")
	w := httptest.NewRecorder()
	e.h.CreateSession(w, withUser(req, e.owner))
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSessionView](t, w)
	if got.Locale != "pt-BR" {
		t.Errorf("locale = %q, want pt-BR from Accept-Language", got.Locale)
	}
	if got.Title != "bh_app_a" {
		t.Errorf("title defaults to the app name, got %q", got.Title)
	}
	if got.PromptVersionID != nil {
		t.Errorf("no persona saved yet: prompt_version_id must be null, got %v", *got.PromptVersionID)
	}
	if again := e.createOK(t, e.owner, e.appA); again.Locale != "en" {
		t.Errorf("locale default = %q, want en", again.Locale)
	}
}

func TestBuilderCreate_UserWithoutAccessGets404AndNothingHappens(t *testing.T) {
	e := newBHEnv(t)
	for name, user := range map[string]*DashboardUser{"outsider": e.outsider, "other app admin": e.other} {
		w := e.create(t, user, fmt.Sprintf(`{"backend_app_id":%q}`, e.appA))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404 (not 403), got %d: %s", name, w.Code, w.Body.String())
		}
	}
	// A nonexistent app is indistinguishable.
	if w := e.create(t, e.owner, `{"backend_app_id":"00000000-0000-0000-0000-000000000000"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown app: want 404, got %d", w.Code)
	}
	if n := e.sessionCount(t); n != 0 {
		t.Errorf("no session may be created, found %d", n)
	}
	if inits, _ := e.fake.snapshot(); len(inits) != 0 {
		t.Errorf("the builder must not be touched, saw %d Init calls", len(inits))
	}
}

func TestBuilderCreate_InvalidInput400(t *testing.T) {
	e := newBHEnv(t)
	long := strings.Repeat("x", builderMaxTitleRunes+1)
	cases := map[string]string{
		"missing app id": `{}`,
		"malformed app":  `{"backend_app_id":"not-a-uuid"}`,
		"bad locale":     fmt.Sprintf(`{"backend_app_id":%q,"locale":"en US; drop"}`, e.appA),
		"long title":     fmt.Sprintf(`{"backend_app_id":%q,"title":%q}`, e.appA, long),
		"not json":       `{`,
	}
	for name, body := range cases {
		if body == `{` {
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
			w := httptest.NewRecorder()
			e.h.CreateSession(w, withUser(req, e.owner))
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: want 400, got %d", name, w.Code)
			}
			continue
		}
		if w := e.create(t, e.owner, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	if n := e.sessionCount(t); n != 0 {
		t.Errorf("no session may be created, found %d", n)
	}
}

func TestBuilderCreate_UnauthenticatedIs401(t *testing.T) {
	e := newBHEnv(t)
	if w := e.create(t, nil, fmt.Sprintf(`{"backend_app_id":%q}`, e.appA)); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

// Without a builder every route except status is a generic 503.
func TestBuilderRoutes_NotConfiguredAre503(t *testing.T) {
	e := newBHEnv(t, bhWithoutBuilder())
	id := "11111111-1111-1111-1111-111111111111"
	routes := map[string]*httptest.ResponseRecorder{
		"create": e.create(t, e.owner, fmt.Sprintf(`{"backend_app_id":%q}`, e.appA)),
		"list":   e.do(t, e.h.ListSessions, e.owner, http.MethodGet, "/x", ""),
		"get":    e.get(t, e.owner, id),
		"delete": e.del(t, e.owner, id),
	}
	for name, w := range routes {
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: want 503, got %d", name, w.Code)
			continue
		}
		if got := bhErrorBody(t, w); got != "builder not configured" {
			t.Errorf("%s: error = %q, want %q", name, got, "builder not configured")
		}
	}
	if n := e.sessionCount(t); n != 0 {
		t.Errorf("nothing may be created, found %d sessions", n)
	}
}

func TestBuilderCreate_BuilderUnavailableIs503Generic(t *testing.T) {
	body := func(e *bhEnv) string { return fmt.Sprintf(`{"backend_app_id":%q}`, e.appA) }

	t.Run("builder down", func(t *testing.T) {
		e := newBHEnv(t)
		e.fake.healthErr = errors.New("dial tcp 10.9.9.9:8081: connect: connection refused")
		w := e.create(t, e.owner, body(e))
		if w.Code != http.StatusServiceUnavailable || bhErrorBody(t, w) != "builder unavailable" {
			t.Fatalf("want 503 builder unavailable, got %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "10.9.9.9") {
			t.Errorf("internal detail leaked: %s", w.Body.String())
		}
		if e.sessionCount(t) != 0 {
			t.Error("no session may be created while the builder is down")
		}
	})
	t.Run("disabled in settings", func(t *testing.T) {
		e := newBHEnv(t)
		off := false
		e.setSettings(t, BuilderSettingsInput{Enabled: &off})
		w := e.create(t, e.owner, body(e))
		if w.Code != http.StatusServiceUnavailable || bhErrorBody(t, w) != "builder unavailable" {
			t.Fatalf("want 503 builder unavailable, got %d: %s", w.Code, w.Body.String())
		}
		if inits, _ := e.fake.snapshot(); len(inits) != 0 {
			t.Error("the builder must not be touched when disabled")
		}
	})
	t.Run("provider not configured", func(t *testing.T) {
		e := newBHEnv(t)
		if _, err := e.pool.Exec(context.Background(), `DELETE FROM zeep_system.ai_providers`); err != nil {
			t.Fatal(err)
		}
		if w := e.create(t, e.owner, body(e)); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d", w.Code)
		}
	})
}

// A failed Init leaves nothing usable: the session is archived, the workspace
// destroyed, the raw error stays in the logs.
func TestBuilderCreate_InitFailureArchivesAndDestroys(t *testing.T) {
	cases := map[string]struct {
		err      error
		on       int
		wantCode int
	}{
		"first init fails":  {err: errors.New("boom: /work/secret/path"), on: 1, wantCode: http.StatusServiceUnavailable},
		"second init fails": {err: &sandbox.Error{Status: 0, Code: "unavailable", Message: "builder unavailable", Err: errors.New("dial tcp 10.0.0.1: refused")}, on: 2, wantCode: http.StatusServiceUnavailable},
		"builder rejects":   {err: &sandbox.Error{Status: http.StatusUnprocessableEntity, Code: "unsafe_archive_entry", Message: "unsafe"}, on: 2, wantCode: http.StatusUnprocessableEntity},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newBHEnv(t)
			e.fake.initErr, e.fake.initErrOn = tc.err, tc.on
			w := e.create(t, e.owner, fmt.Sprintf(`{"backend_app_id":%q}`, e.appA))
			if w.Code != tc.wantCode {
				t.Fatalf("want %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			for _, leak := range []string{"boom", "/work/secret", "10.0.0.1", "dial tcp"} {
				if strings.Contains(w.Body.String(), leak) {
					t.Errorf("response leaks %q: %s", leak, w.Body.String())
				}
			}
			var id, status string
			if err := e.pool.QueryRow(context.Background(),
				`SELECT id, status FROM zeep_system.builder_sessions`).Scan(&id, &status); err != nil {
				t.Fatalf("the failed session row should exist, archived: %v", err)
			}
			if status != store.SessionArchived {
				t.Errorf("status = %q, want archived", status)
			}
			if _, destroyed := e.fake.snapshot(); len(destroyed) == 0 || destroyed[len(destroyed)-1] != id {
				t.Errorf("workspace must be destroyed after a failed create, destroyed=%v", destroyed)
			}
			if _, ok := e.fake.workspace(id); ok {
				t.Error("no workspace may remain")
			}
			list := decodeJSON[[]BuilderSessionView](t, e.do(t, e.h.ListSessions, e.owner, http.MethodGet, "/x", ""))
			if len(list) != 0 {
				t.Errorf("a failed create must not be listed, got %d", len(list))
			}
		})
	}
}

// ---- T25: company templates ----

func bhGzipTarball(t *testing.T, top string, files map[string]string, extra ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "abc"}})
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: top + "/", Mode: 0o755})
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: top + "/" + n, Mode: 0o644, Size: int64(len(files[n]))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(files[n]))
	}
	for i := range extra {
		h := extra[i]
		h.Name = top + "/" + h.Name
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}

// bhSeedGitHubTemplate registers a builder-enabled company template and a
// connected GitHub App, and returns a handler whose GitHub client serves
// tarball for any download.
func (e *bhEnv) seedGitHubTemplate(t *testing.T, enabled bool, tarball []byte) *GitHubTemplate {
	t.Helper()
	ctx := context.Background()
	seedInstalledGitHubConfig(t, e.pool, genTestKeyPEM(t))
	tpl, err := CreateGitHubTemplate(ctx, e.pool, GitHubTemplateInput{
		Name: "Acme", Description: "acme", GitHubOwner: "acme", GitHubRepo: "starter", Framework: "react", CreatedBy: e.super.ID,
		RenderServiceType: "static_site", BuildCommand: "npm run build", PublishPath: "dist",
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}
	if enabled {
		if _, err := SetGitHubTemplateBuilderEnabled(ctx, e.pool, tpl.ID, true); err != nil {
			t.Fatalf("enable template: %v", err)
		}
	}
	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.String(), "access_tokens"):
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		case strings.Contains(req.URL.Path, "/repos/acme/starter/tarball"):
			resp := mockJSONResp(http.StatusOK, "")
			resp.Body = io.NopCloser(bytes.NewReader(tarball))
			resp.ContentLength = int64(len(tarball))
			return resp, nil
		}
		t.Errorf("unexpected GitHub request %s", req.URL)
		return mockJSONResp(http.StatusNotFound, `{}`), nil
	})
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.fake, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example", GitHubHTTPClient: client,
	})
	return tpl
}

func TestBuilderCreate_CompanyTemplateFromGitHubTarball(t *testing.T) {
	e := newBHEnv(t)
	tarball := bhGzipTarball(t, "acme-starter-1a2b3c4", map[string]string{
		"package.json":        `{"name":"acme"}`,
		"ORBIT_AGENT.md":      "# Acme design system\n",
		"src/orbit.config.ts": "export const ORBIT_API_URL: string = 'x'\nexport const ORBIT_APP_NAME: string = 'y'\n",
		"src/App.tsx":         "export {}\n",
	})
	tpl := e.seedGitHubTemplate(t, true, tarball)

	w := e.create(t, e.viewer, fmt.Sprintf(`{"backend_app_id":%q,"template_id":%q}`, e.appA, tpl.ID))
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSessionView](t, w)
	if got.TemplateKind != "github" || got.TemplateID == nil || *got.TemplateID != tpl.ID {
		t.Errorf("template not recorded: %+v", got)
	}
	if got.TemplateVersion != "1a2b3c4" {
		t.Errorf("template version = %q, want the commit sha from the archive", got.TemplateVersion)
	}
	files, _ := e.fake.workspace(got.ID)
	if files["package.json"] != `{"name":"acme"}` || files["ORBIT_AGENT.md"] != "# Acme design system\n" {
		t.Errorf("template files not carried over (top-level directory must be stripped): %v", files)
	}
	if !strings.Contains(files["src/orbit.config.ts"], "'bh_app_a'") || !strings.Contains(files["src/orbit.config.ts"], "'https://orbit.test.example'") {
		t.Errorf("orbit.config.ts not generated: %s", files["src/orbit.config.ts"])
	}
	for name := range files {
		if strings.Contains(name, "acme-starter") || strings.HasPrefix(name, "/") {
			t.Errorf("unexpected path %q", name)
		}
	}
	if inits, destroyed := e.fake.snapshot(); len(inits) != 1 || len(destroyed) != 0 {
		t.Errorf("a company template needs a single Init: inits=%d destroyed=%v", len(inits), destroyed)
	}
	if snaps, _ := e.st.ListSnapshots(context.Background(), got.ID); len(snaps) != 1 {
		t.Errorf("want one turn 0 snapshot, got %d", len(snaps))
	}
}

func TestBuilderCreate_CompanyTemplateWithoutConfigFileGetsOne(t *testing.T) {
	e := newBHEnv(t)
	tpl := e.seedGitHubTemplate(t, true, bhGzipTarball(t, "acme-starter-deadbee", map[string]string{"package.json": "{}"}))
	got := e.createWithTemplate(t, e.owner, tpl.ID)
	files, _ := e.fake.workspace(got.ID)
	if !strings.Contains(files["src/orbit.config.ts"], "ORBIT_APP_NAME") {
		t.Errorf("orbit.config.ts must be added when the template has none: %v", files)
	}
}

func (e *bhEnv) createWithTemplate(t *testing.T, user *DashboardUser, templateID string) BuilderSessionView {
	t.Helper()
	w := e.create(t, user, fmt.Sprintf(`{"backend_app_id":%q,"template_id":%q}`, e.appA, templateID))
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	return decodeJSON[BuilderSessionView](t, w)
}

func TestBuilderCreate_TemplateNotOfferedIs404(t *testing.T) {
	e := newBHEnv(t)
	tpl := e.seedGitHubTemplate(t, false, bhGzipTarball(t, "acme-starter-1a2b3c4", map[string]string{"package.json": "{}"}))
	for name, id := range map[string]string{
		"not builder_enabled": tpl.ID,
		"unknown":             "00000000-0000-0000-0000-000000000000",
		"malformed":           "default",
	} {
		w := e.create(t, e.owner, fmt.Sprintf(`{"backend_app_id":%q,"template_id":%q}`, e.appA, id))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	if e.sessionCount(t) != 0 {
		t.Error("no session may be created for a template that is not offered")
	}
}

func TestBuilderCreate_UnusableTemplateTarballIs422AndCreatesNothing(t *testing.T) {
	cases := map[string][]byte{
		"symlink": bhGzipTarball(t, "acme-starter-1a2b3c4", map[string]string{"package.json": "{}"},
			tar.Header{Typeflag: tar.TypeSymlink, Name: "link", Linkname: "/etc/passwd"}),
		"not a gzip": []byte("this is not a tarball"),
	}
	for name, tarball := range cases {
		t.Run(name, func(t *testing.T) {
			e := newBHEnv(t)
			tpl := e.seedGitHubTemplate(t, true, tarball)
			w := e.create(t, e.owner, fmt.Sprintf(`{"backend_app_id":%q,"template_id":%q}`, e.appA, tpl.ID))
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("want 422, got %d: %s", w.Code, w.Body.String())
			}
			if got := bhErrorBody(t, w); got != "template could not be used" {
				t.Errorf("error = %q", got)
			}
			// A symlink is only discovered while streaming, after the row exists:
			// it must end archived and destroyed. Otherwise no row at all.
			var live int
			_ = e.pool.QueryRow(context.Background(),
				`SELECT count(*) FROM zeep_system.builder_sessions WHERE status <> 'archived'`).Scan(&live)
			if live != 0 {
				t.Errorf("a failed create must leave no live session, found %d", live)
			}
		})
	}
}

func TestBuilderCreate_GitHubNotConnectedIs503(t *testing.T) {
	e := newBHEnv(t)
	tpl, err := CreateGitHubTemplate(context.Background(), e.pool, GitHubTemplateInput{
		Name: "Acme", GitHubOwner: "acme", GitHubRepo: "starter", Framework: "react", CreatedBy: e.super.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetGitHubTemplateBuilderEnabled(context.Background(), e.pool, tpl.ID, true); err != nil {
		t.Fatal(err)
	}
	w := e.create(t, e.owner, fmt.Sprintf(`{"backend_app_id":%q,"template_id":%q}`, e.appA, tpl.ID))
	if w.Code != http.StatusServiceUnavailable || e.sessionCount(t) != 0 {
		t.Fatalf("want 503 and no session, got %d (%d sessions): %s", w.Code, e.sessionCount(t), w.Body.String())
	}
}

// ---- T25: list / get ----

func TestBuilderList_OwnerOnlyNewestFirstExcludesArchived(t *testing.T) {
	e := newBHEnv(t)
	first := e.createOK(t, e.owner, e.appA)
	second := e.createOK(t, e.owner, e.appA)
	archived := e.createOK(t, e.owner, e.appA)
	foreign := e.createOK(t, e.other, e.appB)
	if w := e.del(t, e.owner, archived.ID); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}

	list := decodeJSON[[]BuilderSessionView](t, e.do(t, e.h.ListSessions, e.owner, http.MethodGet, "/x", ""))
	ids := []string{}
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	if len(ids) != 2 || ids[0] != second.ID || ids[1] != first.ID {
		t.Errorf("list = %v, want [%s %s] (own, non-archived, newest first)", ids, second.ID, first.ID)
	}
	for _, id := range ids {
		if id == foreign.ID {
			t.Error("another user's session was listed")
		}
	}
	if w := e.do(t, e.h.ListSessions, e.outsider, http.MethodGet, "/x", ""); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("a user with no sessions gets an empty array, got %d %s", w.Code, w.Body.String())
	}
}

func TestBuilderGet_OwnerOnly(t *testing.T) {
	e := newBHEnv(t)
	mine := e.createOK(t, e.owner, e.appA)

	w := e.get(t, e.owner, mine.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("owner: want 200, got %d: %s", w.Code, w.Body.String())
	}
	detail := decodeJSON[BuilderSessionDetail](t, w)
	if detail.ID != mine.ID || detail.Turns == nil || len(detail.Turns) != 0 {
		t.Errorf("detail = %+v (turns must be an empty array)", detail)
	}
	if !strings.Contains(w.Body.String(), `"turns":[]`) {
		t.Errorf("turns must serialize as []: %s", w.Body.String())
	}

	for name, user := range map[string]*DashboardUser{"other user": e.other, "outsider": e.outsider, "superadmin": e.super, "viewer of the same app": e.viewer} {
		w := e.get(t, user, mine.ID)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: another user's session must be 404 (not 403), got %d", name, w.Code)
		}
	}
	for name, id := range map[string]string{"malformed": "nope", "unknown": "00000000-0000-0000-0000-000000000000"} {
		if w := e.get(t, e.owner, id); w.Code != http.StatusNotFound {
			t.Errorf("%s id: want 404, got %d", name, w.Code)
		}
	}
	if w := e.get(t, nil, mine.ID); w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: want 401, got %d", w.Code)
	}
}

func TestBuilderGet_IncludesTurns(t *testing.T) {
	e := newBHEnv(t)
	mine := e.createOK(t, e.owner, e.appA)
	turn, err := e.st.CreateTurn(context.Background(), e.owner.ID, mine.ID, store.KindChat, "hello")
	if err != nil {
		t.Fatal(err)
	}
	detail := decodeJSON[BuilderSessionDetail](t, e.get(t, e.owner, mine.ID))
	if len(detail.Turns) != 1 || detail.Turns[0].ID != turn.ID || detail.Turns[0].Seq != 1 || detail.Turns[0].Status != "pending" {
		t.Errorf("turns = %+v", detail.Turns)
	}
}

// Edge case "app deleted / access lost": the owner keeps seeing (and can delete)
// their session; a deleted app removes the session with it.
func TestBuilderGet_AccessLostAndAppDeleted(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.viewer, e.appA)

	if _, err := e.pool.Exec(context.Background(),
		`DELETE FROM zeep_system.app_members WHERE backend_app_id = $1 AND user_id = $2`, e.appA, e.viewer.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.get(t, e.viewer, sess.ID); w.Code != http.StatusOK {
		t.Errorf("owner who lost app access can still read the session, got %d", w.Code)
	}
	if w := e.create(t, e.viewer, fmt.Sprintf(`{"backend_app_id":%q}`, e.appA)); w.Code != http.StatusNotFound {
		t.Errorf("but cannot start a new one on that app, got %d", w.Code)
	}

	if _, err := e.pool.Exec(context.Background(), `DELETE FROM zeep_system.apps WHERE id = $1`, e.appA); err != nil {
		t.Fatalf("delete app: %v", err)
	}
	if w := e.get(t, e.viewer, sess.ID); w.Code != http.StatusNotFound {
		t.Errorf("a deleted app takes its sessions with it: want 404, got %d", w.Code)
	}
}

// ---- T25: delete ----

func TestBuilderDelete_ArchivesAndDestroys(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.owner, e.appA)

	w := e.del(t, e.owner, sess.ID)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("want 204 with no body, got %d: %q", w.Code, w.Body.String())
	}
	if got := e.sessionStatus(t, sess.ID); got != store.SessionArchived {
		t.Errorf("status = %q, want archived", got)
	}
	if _, destroyed := e.fake.snapshot(); destroyed[len(destroyed)-1] != sess.ID {
		t.Errorf("workspace must be destroyed, destroyed=%v", destroyed)
	}
	if _, ok := e.fake.workspace(sess.ID); ok {
		t.Error("workspace still present")
	}
	if w := e.get(t, e.owner, sess.ID); w.Code != http.StatusNotFound {
		t.Errorf("archived session is gone for the owner: got %d", w.Code)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'builder.session.delete' AND resource_id = $1`, sess.ID).Scan(&n)
	if n != 1 {
		t.Errorf("want one delete audit row, got %d", n)
	}
}

func TestBuilderDelete_OtherUsersSessionIs404AndUntouched(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.owner, e.appA)
	_, destroyedBefore := e.fake.snapshot()

	for name, user := range map[string]*DashboardUser{"other": e.other, "viewer": e.viewer, "superadmin": e.super} {
		if w := e.del(t, user, sess.ID); w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", name, w.Code)
		}
	}
	if got := e.sessionStatus(t, sess.ID); got == store.SessionArchived {
		t.Error("another user's delete must not archive the session")
	}
	if _, destroyedAfter := e.fake.snapshot(); len(destroyedAfter) != len(destroyedBefore) {
		t.Error("another user's delete must not destroy the workspace")
	}
	if _, ok := e.fake.workspace(sess.ID); !ok {
		t.Error("workspace must survive")
	}
}

// Destroy failing must not leave the session un-archived, and a retry cleans up.
func TestBuilderDelete_DestroyFailureStillArchivesAndRetryDestroysAgain(t *testing.T) {
	e := newBHEnv(t)
	sess := e.createOK(t, e.owner, e.appA)

	e.fake.destroyErr = errors.New("builder volume gone: /work/abc")
	w := e.del(t, e.owner, sess.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204 even though Destroy failed, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "/work") {
		t.Errorf("destroy error leaked: %s", w.Body.String())
	}
	if got := e.sessionStatus(t, sess.ID); got != store.SessionArchived {
		t.Fatalf("status = %q, want archived despite the Destroy failure", got)
	}

	// Retry (archived sessions are still deletable) destroys again and works.
	e.fake.destroyErr = nil
	if w := e.del(t, e.owner, sess.ID); w.Code != http.StatusNoContent {
		t.Fatalf("retry: want 204, got %d", w.Code)
	}
	if _, ok := e.fake.workspace(sess.ID); ok {
		t.Error("the retry must remove the leftover workspace")
	}
	var n int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'builder.session.delete' AND resource_id = $1`, sess.ID).Scan(&n)
	if n != 1 {
		t.Errorf("audit must record the deletion once, got %d", n)
	}
}

// ---- 500s never carry err.Error() ----

func TestBuilderRoutes_ServerErrorsAreGeneric(t *testing.T) {
	e := newBHEnv(t)
	broken, err := db.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	broken.Close() // every query on it now fails
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{Store: store.New(broken), Sandbox: e.fake, Logger: zap.NewNop()})

	sess := "11111111-1111-1111-1111-111111111111"
	for name, w := range map[string]*httptest.ResponseRecorder{
		"list": e.do(t, e.h.ListSessions, e.owner, http.MethodGet, "/x", ""),
		"get":  e.get(t, e.owner, sess),
		"del":  e.del(t, e.owner, sess),
	} {
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s: want 500, got %d: %s", name, w.Code, w.Body.String())
			continue
		}
		if got := bhErrorBody(t, w); got != "internal error" {
			t.Errorf("%s: error = %q, want the fixed generic message", name, got)
		}
		if strings.Contains(strings.ToLower(w.Body.String()), "closed") || strings.Contains(w.Body.String(), "builder_sessions") {
			t.Errorf("%s: internal detail leaked: %s", name, w.Body.String())
		}
	}
}

// ---- pure helpers ----

func TestBuilderLocale(t *testing.T) {
	good := map[string]string{"en": "en", "pt-br": "pt-BR", "PT-BR": "pt-BR", " es-419 ": "es-419", "zh-Hant-TW": "zh-Hant-TW", "fil": "fil"}
	for in, want := range good {
		if got, ok := normalizeBuilderLocale(in); !ok || got != want {
			t.Errorf("normalizeBuilderLocale(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "e", "english", "en_US", "en US", "en-", "pt-BR;q=1", strings.Repeat("a", 40), "../x"} {
		if got, ok := normalizeBuilderLocale(bad); ok {
			t.Errorf("normalizeBuilderLocale(%q) accepted as %q", bad, got)
		}
	}
	for header, want := range map[string]string{
		"":                               "en",
		"pt-BR,pt;q=0.9":                 "pt-BR",
		"*":                              "en",
		"fr-CH, fr;q=0.9, en;q=0.8":      "fr-CH",
		"x_bad, de;q=0.5":                "de",
		"; drop table, es-AR":            "es-AR",
		strings.Repeat("z", 500) + ",it": "it",
	} {
		if got := localeFromAcceptLanguage(header); got != want {
			t.Errorf("localeFromAcceptLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestBuilderAPIBaseURL(t *testing.T) {
	mk := func(cfg string, host string, hdr map[string]string) (string, bool) {
		h := NewBuilderHandler(nil, BuilderHandlerConfig{PublicBaseURL: cfg})
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/dashboard/api/builder/sessions", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		return h.apiBaseURL(req)
	}
	t.Setenv("ORBIT_PUBLIC_URL", "")
	if got, ok := mk("https://orbit.acme.io/", "ignored", nil); !ok || got != "https://orbit.acme.io" {
		t.Errorf("configured URL: %q %v", got, ok)
	}
	if got, ok := mk("", "orbit.local:8080", map[string]string{"X-Forwarded-Proto": "https"}); !ok || got != "https://orbit.local:8080" {
		t.Errorf("derived URL: %q %v", got, ok)
	}
	t.Setenv("ORBIT_PUBLIC_URL", "https://pinned.example.com")
	if got, ok := mk("", "attacker.example", nil); !ok || got != "https://pinned.example.com" {
		t.Errorf("ORBIT_PUBLIC_URL must win over the request: %q %v", got, ok)
	}
	for _, bad := range []string{"ftp://x.example", "https://user:pw@x.example", "https://x.example?q=1", "not a url", "https://x.example#f"} {
		if got, ok := mk(bad, "h", nil); ok {
			t.Errorf("accepted %q as %q", bad, got)
		}
	}
}

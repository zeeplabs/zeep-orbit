package server

// builder_routes_test.go — AI Frontend Builder route registration and wiring
// (ai-frontend-builder T30), through the real chi router.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

type builderRoute struct {
	method, path string
}

const (
	bSID  = "11111111-1111-4111-8111-111111111111"
	bTurn = "22222222-2222-4222-8222-222222222222"
)

// Every authenticated builder route, as registered. Status is separate: it
// answers even when the builder is off.
var builderSessionRoutes = []builderRoute{
	{http.MethodPost, "/dashboard/api/builder/sessions"},
	{http.MethodGet, "/dashboard/api/builder/sessions"},
	{http.MethodGet, "/dashboard/api/builder/sessions/" + bSID},
	{http.MethodDelete, "/dashboard/api/builder/sessions/" + bSID},
	{http.MethodPost, "/dashboard/api/builder/sessions/" + bSID + "/messages"},
	{http.MethodGet, "/dashboard/api/builder/sessions/" + bSID + "/events"},
	{http.MethodPost, "/dashboard/api/builder/sessions/" + bSID + "/revert"},
	{http.MethodPost, "/dashboard/api/builder/sessions/" + bSID + "/turns/" + bTurn + "/confirm"},
	{http.MethodPost, "/dashboard/api/builder/sessions/" + bSID + "/turns/" + bTurn + "/reject"},
	{http.MethodPost, "/dashboard/api/builder/sessions/" + bSID + "/preview-token"},
	{http.MethodPost, "/dashboard/api/builder/sessions/" + bSID + "/publish"},
	{http.MethodGet, "/dashboard/api/builder/sessions/" + bSID + "/download.zip"},
}

var builderConfigRoutes = []builderRoute{
	{http.MethodGet, "/dashboard/api/builder/settings"},
	{http.MethodPut, "/dashboard/api/builder/settings"},
	{http.MethodGet, "/dashboard/api/builder/prompt"},
	{http.MethodPut, "/dashboard/api/builder/prompt"},
	{http.MethodGet, "/dashboard/api/builder/prompt/versions"},
	{http.MethodGet, "/dashboard/api/builder/templates"},
	{http.MethodPatch, "/dashboard/api/github/templates/" + bSID + "/builder"},
}

var builderPreviewPaths = []string{
	"/dashboard/api/builder/sessions/" + bSID + "/p/garbage-token",
	"/dashboard/api/builder/sessions/" + bSID + "/p/garbage-token/",
	"/dashboard/api/builder/sessions/" + bSID + "/p/garbage-token/assets/app.js",
}

func doReq(t *testing.T, base string, rt builderRoute, cookie *http.Cookie) (int, string) {
	t.Helper()
	var body io.Reader
	if rt.method == http.MethodPost || rt.method == http.MethodPut || rt.method == http.MethodPatch {
		body = strings.NewReader(`{}`)
	}
	req, err := http.NewRequest(rt.method, base+rt.path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", rt.method, rt.path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func errorOf(body string) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &e)
	return e.Error
}

// Without a session cookie every authenticated builder route is rejected by
// RequireAuth (a 404 would mean chi never matched it), builder wired or not.
func TestServerBuilderRoutes_UnauthenticatedAre401(t *testing.T) {
	s := newTestServer(t) // BUILDER_URL empty: no builder option
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	all := append([]builderRoute{{http.MethodGet, "/dashboard/api/builder/status"}}, builderSessionRoutes...)
	all = append(all, builderConfigRoutes...)
	for _, r := range all {
		if code, body := doReq(t, ts.URL, r, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d (%s), want 401", r.method, r.path, code, body)
		}
	}
}

// The preview route is token-gated, not cookie-gated: reachable without
// RequireAuth, 401 without a token; and, with the builder off, 503.
func TestServerBuilderPreviewRoute_NoRequireAuth_BuilderOff(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	for _, p := range builderPreviewPaths {
		code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, p}, nil)
		if code != http.StatusServiceUnavailable || errorOf(body) != "builder not configured" {
			t.Errorf("GET %s: %d %s, want 503 builder not configured", p, code, body)
		}
	}
}

func builderRoutesTestPool(t *testing.T) (*db.Pool, *http.Cookie, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := dashboard.ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}
	truncate := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.sessions`)
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.builder_settings, zeep_system.builder_sessions, zeep_system.dashboard_users CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)

	user, err := dashboard.CreateUser(ctx, pool, fmt.Sprintf("builder-route-%d@example.com", time.Now().UnixNano()), "Route User", "hash", "superadmin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := fmt.Sprintf("builder-route-token-%d", time.Now().UnixNano())
	if err := dashboard.CreateSession(ctx, pool, token, user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return pool, &http.Cookie{Name: "zeep_session", Value: token}, user.ID
}

// BUILDER_URL empty, real DB, logged-in user: status answers (not available);
// every session route is 503 "builder not configured"; the config routes still
// work for the superadmin; no worker exists.
func TestServerBuilderRoutes_BuilderOff_StatusAnswersRestIs503(t *testing.T) {
	pool, cookie, _ := builderRoutesTestPool(t)
	s, err := New(registry.New(), pool, 0, WithBuilder(dashboard.BuilderHandlerConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/status"}, cookie)
	if code != http.StatusOK {
		t.Fatalf("status: %d %s, want 200", code, body)
	}
	var st struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil || st.Available {
		t.Fatalf("status body %s: available must be false (err %v)", body, err)
	}

	for _, r := range builderSessionRoutes {
		code, body := doReq(t, ts.URL, r, cookie)
		if code != http.StatusServiceUnavailable || errorOf(body) != "builder not configured" {
			t.Errorf("%s %s: %d %s, want 503 builder not configured", r.method, r.path, code, body)
		}
	}

	// Config routes are plain Orbit configuration and do not need a builder.
	if code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/settings"}, cookie); code != http.StatusOK {
		t.Errorf("GET settings: %d %s, want 200", code, body)
	}
	if code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/prompt"}, cookie); code != http.StatusOK {
		t.Errorf("GET prompt: %d %s, want 200", code, body)
	}
}

// With a builder URL configured (a fake daemon), the routes are mounted:
// auth is required, an authenticated list works, the preview route answers
// without RequireAuth and rejects a missing/foreign token with 401, and a
// valid token for an unknown session is not served.
func TestServerBuilderRoutes_BuilderConfigured(t *testing.T) {
	pool, cookie, userID := builderRoutesTestPool(t)

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/preview") {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("preview-ok"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":0,"capacity":2,"version":"test","default_template_version":"1"}`))
	}))
	defer fake.Close()

	rtm, err := dashboard.NewBuilderRuntime(pool, dashboard.BuilderRuntimeConfig{
		URL: fake.URL, SharedSecret: strings.Repeat("k", 32), Concurrency: 1,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil || rtm == nil {
		t.Fatalf("NewBuilderRuntime: %v", err)
	}
	s, err := New(registry.New(), pool, 0, WithBuilder(rtm.HandlerConfig()))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	// Mounted + auth required.
	for _, r := range builderSessionRoutes {
		if code, body := doReq(t, ts.URL, r, nil); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d %s, want 401", r.method, r.path, code, body)
		}
	}
	// Authenticated: the builder is wired, so the list is a real (empty) answer
	// and an unknown session is a 404, not a 503.
	if code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/sessions"}, cookie); code != http.StatusOK {
		t.Errorf("GET sessions: %d %s, want 200", code, body)
	}
	if code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/sessions/" + bSID}, cookie); code != http.StatusNotFound || errorOf(body) != "session not found" {
		t.Errorf("GET unknown session: %d %s, want 404 session not found", code, body)
	}
	if code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/status"}, cookie); code != http.StatusOK {
		t.Errorf("status: %d %s", code, body)
	}

	// Preview: no RequireAuth. No token -> 401 JSON from the handler itself.
	for _, p := range builderPreviewPaths {
		code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, p}, nil)
		if code != http.StatusUnauthorized || errorOf(body) != "unauthorized" {
			t.Errorf("GET %s without a token: %d %s, want 401 unauthorized", p, code, body)
		}
		// A dashboard cookie is NOT a preview credential.
		if code, _ := doReq(t, ts.URL, builderRoute{http.MethodGet, p}, cookie); code != http.StatusUnauthorized {
			t.Errorf("GET %s with only the dashboard cookie: %d, want 401", p, code)
		}
	}
	// The old query/cookie forms are gone: ?pt= is not a credential.
	if code, _ := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/sessions/" + bSID + "/p/garbage-token/?pt=anything"}, nil); code != http.StatusUnauthorized {
		t.Errorf("garbage preview token: %d, want 401", code)
	}
	// A genuine token for a session that does not exist is not served.
	tok, err := rtm.HandlerConfig().PreviewSigner.SignPreview(bSID, userID)
	if err != nil {
		t.Fatal(err)
	}
	code, body := doReq(t, ts.URL, builderRoute{http.MethodGet, "/dashboard/api/builder/sessions/" + bSID + "/p/" + tok + "/"}, nil)
	if code == http.StatusOK || code == http.StatusServiceUnavailable {
		t.Errorf("valid token for an unknown session: %d %s, want a rejection", code, body)
	}

	// A real session of that user, a valid token: every preview spelling is
	// proxied (with and without the trailing slash, and the wildcard), with
	// the sandbox CSP, and the dashboard cookie is not needed.
	var appID string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`, "builder_route_app", userID).Scan(&appID); err != nil {
		t.Fatalf("create app: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM zeep_system.apps WHERE id = $1`, appID) })
	sess, err := rtm.HandlerConfig().Store.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: userID, BackendAppID: appID, Title: "t",
		TemplateKind: store.TemplateDefault, TemplateVersion: "1",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	tok, err = rtm.HandlerConfig().PreviewSigner.SignPreview(sess.ID, userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/", "/assets/app.js"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/dashboard/api/builder/sessions/"+sess.ID+"/p/"+tok+p, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(b) != "preview-ok" {
			t.Errorf("preview %q: %d %q, want 200 preview-ok", p, resp.StatusCode, b)
		}
		if len(resp.Header.Values("Set-Cookie")) != 0 {
			t.Errorf("preview %q: no cookie may be set, got %q", p, resp.Header.Values("Set-Cookie"))
		}
		if csp := resp.Header.Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox ") || strings.Contains(csp, "allow-same-origin") {
			t.Errorf("preview %q: CSP = %q, want the opaque-origin sandbox", p, csp)
		}
	}
}

func TestServerShutdownTasksRunConcurrentlyAndAreAwaited(t *testing.T) {
	s := newTestServer(t)
	var started, finished atomic.Int32
	gate := make(chan struct{})
	for i := 0; i < 2; i++ {
		s.OnShutdown(func(ctx context.Context) error {
			started.Add(1)
			<-gate
			finished.Add(1)
			return nil
		})
	}
	done := make(chan struct{})
	go func() { s.runShutdownTasks(context.Background()); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for started.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if started.Load() != 2 {
		t.Fatalf("started = %d, want both tasks running at the same time", started.Load())
	}
	select {
	case <-done:
		t.Fatal("runShutdownTasks returned before the tasks finished")
	default:
	}
	close(gate)
	<-done
	if finished.Load() != 2 {
		t.Fatalf("finished = %d, want 2", finished.Load())
	}
}

func TestLogMiddleware_BuilderPathsNeverLogBodiesOrPreviewToken(t *testing.T) {
	buf := dashboard.NewRingBuffer(10)
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"preview-token-plaintext"}`))
	})
	handler := logMiddleware(zapNop(), buf)(echo)

	req := httptest.NewRequest(http.MethodPost, "/dashboard/api/builder/sessions/"+bSID+"/preview-token", strings.NewReader(`{"content":"business secret"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodGet, "/dashboard/api/builder/sessions/"+bSID+"/preview/?pt=preview-token-plaintext&x=1", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	// The token now travels in the path.
	req = httptest.NewRequest(http.MethodGet, "/dashboard/api/builder/sessions/"+bSID+"/p/preview-token-plaintext/assets/app.js", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	entries := buf.Recent(3, "", nil)
	if len(entries) != 3 {
		t.Fatalf("got %d log entries, want 3", len(entries))
	}
	sawPath := false
	for _, e := range entries {
		if strings.Contains(e.Path, "preview-token-plaintext") {
			t.Errorf("preview token leaked into the logged path: %q", e.Path)
		}
		if strings.HasSuffix(e.Path, "/p/***/assets/app.js") {
			sawPath = true
		}
	}
	if !sawPath {
		t.Error("the token path segment must be logged as ***")
	}
	for _, e := range entries {
		if e.ResBody != "" || e.ReqBody != "" {
			t.Errorf("%s: builder bodies must not be captured: %+v", e.Path, e)
		}
		if strings.Contains(e.Query, "preview-token-plaintext") {
			t.Errorf("%s: preview token leaked into the log query: %q", e.Path, e.Query)
		}
	}
	if got := redactBuilderQuery("/dashboard/api/apps", "pt=abc"); got != "pt=abc" {
		t.Errorf("non-builder queries must pass through, got %q", got)
	}
}

func zapNop() *zap.Logger { return zap.NewNop() }

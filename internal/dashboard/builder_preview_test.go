package dashboard

// builder_preview_test.go — ai-frontend-builder T29: the token-gated preview
// proxy and the preview-token endpoint. The proxy runs behind the real chi
// router (so the route wildcard and SecurityHeaders behave as in production) in
// front of the real sandbox HTTP client, which talks to a stub upstream that
// verifies the builder request token and answers with hostile headers.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/builder"
)

const bprevSecret = "0123456789abcdef0123456789abcdef"

type bprevUpstreamCall struct {
	path, rawQuery, cookie, authorization, forwardedFor, method string
	tokenOK                                                     bool
}

type bprevEnv struct {
	*bhEnv
	signer *builder.Signer
	router http.Handler
	sess   *store.Session

	mu    sync.Mutex
	calls []bprevUpstreamCall
	now   time.Time
	// upstreamDown makes the stub close connections instead of answering.
	upstreamDown bool
}

func newBPrevEnv(t *testing.T) *bprevEnv {
	t.Helper()
	e := &bprevEnv{bhEnv: newBHEnv(t), now: time.Now()}
	var err error
	if e.signer, err = builder.NewSigner([]byte(bprevSecret)); err != nil {
		t.Fatal(err)
	}

	e.sess, err = e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "t", TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	if err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		down := e.upstreamDown
		call := bprevUpstreamCall{
			path: r.URL.Path, rawQuery: r.URL.RawQuery, cookie: r.Header.Get("Cookie"),
			authorization: r.Header.Get("Authorization"), forwardedFor: r.Header.Get("X-Forwarded-For"), method: r.Method,
		}
		call.tokenOK = e.signer.VerifyRequest(r.Header.Get(builder.TokenHeader), e.sess.ID, r.Method,
			"/v1/sessions/"+e.sess.ID+"/preview/index.html") == nil ||
			e.signer.VerifyRequest(r.Header.Get(builder.TokenHeader), e.sess.ID, r.Method, r.URL.Path) == nil
		e.calls = append(e.calls, call)
		e.mu.Unlock()
		if down {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		// What a compromised or sloppy builder could answer with.
		w.Header().Set("Content-Security-Policy", "default-src * 'unsafe-inline'")
		w.Header().Set("X-Frame-Options", "ALLOWALL")
		w.Header().Set("Set-Cookie", "evil=1; Path=/")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "preview:%s", r.URL.Path)
	}))
	t.Cleanup(upstream.Close)
	sb, err := sandbox.NewHTTP(upstream.URL, e.signer, nil)
	if err != nil {
		t.Fatal(err)
	}

	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: sb, Logger: zap.NewNop(), PreviewSigner: e.signer,
		Clock: func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now },
	})
	e.router = e.routerFor(e.h)
	return e
}

// routerFor mounts the preview routes the way server wiring (T30) must: inside
// /dashboard (so SecurityHeaders applies) but NOT behind RequireAuth.
func (e *bprevEnv) routerFor(h *BuilderHandler) http.Handler {
	r := chi.NewRouter()
	r.Route("/dashboard", func(r chi.Router) {
		r.Use(SecurityHeaders)
		r.Get("/api/builder/sessions/{id}/p/{token}", h.PreviewProxy)
		r.Get("/api/builder/sessions/{id}/p/{token}/", h.PreviewProxy)
		r.Handle("/api/builder/sessions/{id}/p/{token}/*", http.HandlerFunc(h.PreviewProxy))
	})
	return r
}

func (e *bprevEnv) tokenFor(t *testing.T, sessionID, userID string) string {
	t.Helper()
	tok, err := e.signer.SignPreview(sessionID, userID)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// previewPath is the token-in-path URL of the session's preview: tok is the
// raw token (it is path-safe: base64url and ".") and rest starts with "/" or is
// empty.
func (e *bprevEnv) previewPath(tok, rest string) string {
	return "/dashboard/api/builder/sessions/" + e.sess.ID + "/p/" + tok + rest
}

func (e *bprevEnv) fetch(method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *bprevEnv) upstreamCalls() []bprevUpstreamCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]bprevUpstreamCall(nil), e.calls...)
}

func (e *bprevEnv) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func assertPreviewHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	h := w.Header()
	if got := h.Values("Content-Security-Policy"); len(got) != 1 || got[0] != "sandbox allow-scripts allow-forms allow-popups allow-modals" {
		t.Errorf("Content-Security-Policy = %q", got)
	}
	if got := h.Values("X-Frame-Options"); len(got) != 1 || got[0] != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q (SecurityHeaders' DENY must be overridden)", got)
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("Cache-Control") != "no-store" {
		t.Errorf("hardening headers missing: %v", h)
	}
	// The sandboxed page has an opaque origin, so its crossorigin module script
	// and stylesheet need a wildcard ACAO; credentials must never be allowed.
	if h.Get("Access-Control-Allow-Origin") != "*" || h.Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("CORS headers = %v, want ACAO * and no credentials", h)
	}
	if got := h.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("the preview must never set a cookie, got %q", got)
	}
}

// ---- authorization ----

func TestPreviewProxy_RejectsMissingForgedAndForeignTokens(t *testing.T) {
	e := newBPrevEnv(t)
	other, _ := builder.NewSigner([]byte("another-secret-another-secret-00"))
	foreignSigned, _ := other.SignPreview(e.sess.ID, e.owner.ID)

	otherSession, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	if err != nil {
		t.Fatal(err)
	}

	valid := e.tokenFor(t, e.sess.ID, e.owner.ID)
	cases := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"garbage", "not-a-token"},
		{"truncated", valid[:len(valid)-3]},
		{"signed with another secret", foreignSigned},
		{"another user's token", e.tokenFor(t, e.sess.ID, e.other.ID)},
		{"a token of another session", e.tokenFor(t, otherSession.ID, e.owner.ID)},
		{"a builder request token", mustSignRequest(t, e.signer, e.sess.ID)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// An empty token is a request for /p//index.html.
			w := e.fetch(http.MethodGet, e.previewPath(url.PathEscape(tc.token), "/index.html"), nil)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "preview:") {
				t.Error("upstream content served without a valid token")
			}
			assertPreviewHeaders(t, w)
		})
	}
	if n := len(e.upstreamCalls()); n != 0 {
		t.Errorf("%d requests reached the builder without a valid token", n)
	}
}

func mustSignRequest(t *testing.T, s *builder.Signer, sessionID string) string {
	t.Helper()
	tok, err := s.SignRequest(sessionID, http.MethodGet, "/v1/sessions/"+sessionID+"/preview")
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestPreviewProxy_ExpiredTokenIs401(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)
	target := e.previewPath(tok, "/index.html")

	if w := e.fetch(http.MethodGet, target, nil); w.Code != http.StatusOK {
		t.Fatalf("fresh token: want 200, got %d: %s", w.Code, w.Body.String())
	}
	e.advance(builder.PreviewTokenTTL + time.Second)
	if w := e.fetch(http.MethodGet, target, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: want 401, got %d", w.Code)
	}
	if w := e.fetch(http.MethodGet, e.previewPath(tok, "/assets/a.js"), nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token on an asset: want 401, got %d", w.Code)
	}
}

func TestPreviewProxy_OwnershipAndSessionStateAreRechecked(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)
	target := e.previewPath(tok, "/index.html")

	// The session changes hands (cannot happen through the API, but the token
	// only proves who the owner WAS).
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_sessions SET owner_user_id = $2 WHERE id = $1`, e.sess.ID, e.other.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.fetch(http.MethodGet, target, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("session of another owner: want 401, got %d", w.Code)
	}
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.builder_sessions SET owner_user_id = $2 WHERE id = $1`, e.sess.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.fetch(http.MethodGet, target, nil); w.Code != http.StatusOK {
		t.Fatalf("owner again: want 200, got %d", w.Code)
	}

	// An archived (deleted) session stops serving at once.
	if err := e.st.ArchiveSession(context.Background(), e.owner.ID, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.fetch(http.MethodGet, target, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("archived session: want 401, got %d", w.Code)
	}

	// A malformed or unknown id never reaches the store or the builder.
	n := len(e.upstreamCalls())
	if w := e.fetch(http.MethodGet, "/dashboard/api/builder/sessions/not-a-uuid/p/"+tok+"/x", nil); w.Code != http.StatusNotFound {
		t.Errorf("bad id: want 404, got %d", w.Code)
	}
	if got := len(e.upstreamCalls()); got != n {
		t.Error("a bad id reached the builder")
	}
}

// ---- what is served, what is forwarded ----

func TestPreviewProxy_ServesWithSandboxHeadersAndForwardsNoCredentials(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)

	w := e.fetch(http.MethodGet, e.previewPath(tok, "/index.html")+"?x=1", map[string]string{
		"Cookie":          "zeep_session=dashboard-secret; other=1",
		"Authorization":   "Bearer user-credential",
		"X-Forwarded-For": "203.0.113.9",
	})
	if w.Code != http.StatusOK || w.Body.String() != "preview:/v1/sessions/"+e.sess.ID+"/preview/index.html" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	assertPreviewHeaders(t, w)

	calls := e.upstreamCalls()
	if len(calls) != 1 {
		t.Fatalf("%d upstream calls, want 1", len(calls))
	}
	c := calls[0]
	if c.rawQuery != "" || strings.Contains(c.path, tok) {
		t.Errorf("the preview token (or any query) reached the builder: path=%q query=%q", c.path, c.rawQuery)
	}
	if c.cookie != "" || c.authorization != "" || c.forwardedFor != "" {
		t.Errorf("credentials forwarded upstream: cookie=%q authorization=%q xff=%q", c.cookie, c.authorization, c.forwardedFor)
	}
	if !c.tokenOK {
		t.Error("the builder did not get a valid, session-bound request token")
	}

	if len(w.Result().Cookies()) != 0 || w.Header().Get("Set-Cookie") != "" {
		t.Errorf("no cookie may be set (the builder's evil one included): %v", w.Header().Values("Set-Cookie"))
	}
}

func TestPreviewProxy_SubresourcesCarryTheTokenInThePath(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)

	// What the browser does with ./assets/app.js against the document URL
	// /p/{token}/: it requests /p/{token}/assets/app.js, no cookie, no query.
	w := e.fetch(http.MethodGet, e.previewPath(tok, "/assets/app.js"), nil)
	if w.Code != http.StatusOK || !strings.HasSuffix(w.Body.String(), "/preview/assets/app.js") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	assertPreviewHeaders(t, w)
	c := e.upstreamCalls()
	if len(c) != 1 || strings.Contains(c[0].path, tok) || c[0].rawQuery != "" {
		t.Errorf("the token leaked upstream: %+v", c)
	}

	// The token is bound to the session: replaying it on another session is a 401.
	other, _ := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	rec := e.fetch(http.MethodGet, "/dashboard/api/builder/sessions/"+other.ID+"/p/"+tok+"/assets/app.js", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token replayed on another session: want 401, got %d", rec.Code)
	}
	// Neither the dashboard cookie, the legacy query nor the legacy cookie is a credential.
	for name, req := range map[string]*http.Request{
		"dashboard cookie": reqWith(e.previewPath("bad", "/assets/app.js"), map[string]string{"Cookie": "zeep_session=abc"}),
		"legacy ?pt=":      reqWith(e.previewPath("bad", "/assets/app.js")+"?pt="+tok, nil),
		"legacy cookie":    reqWith(e.previewPath("bad", "/assets/app.js"), map[string]string{"Cookie": "orbit_preview=" + tok}),
	} {
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", name, rec.Code)
		}
	}
}

func reqWith(target string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestPreviewProxy_RedirectsTheBareTokenURLToTheTrailingSlash(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)

	w := e.fetch(http.MethodGet, e.previewPath(tok, "")+"?x=1", nil)
	if w.Code != http.StatusPermanentRedirect {
		t.Fatalf("want 308, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != e.previewPath(tok, "/") {
		t.Errorf("Location = %q (the token path must be kept, the query dropped)", loc)
	}
	assertPreviewHeaders(t, w)
	// An invalid token is a 401, never a redirect that reflects it.
	if w := e.fetch(http.MethodGet, e.previewPath("garbage", ""), nil); w.Code != http.StatusUnauthorized {
		t.Errorf("bad token on the bare URL: want 401, got %d", w.Code)
	}
	if n := len(e.upstreamCalls()); n != 0 {
		t.Errorf("%d redirect requests reached the builder", n)
	}
}

func TestRedactPreviewTokenPath(t *testing.T) {
	const id = "3f1c2a9e-0000-4000-8000-000000000001"
	base := "/dashboard/api/builder/sessions/" + id
	for in, want := range map[string]string{
		base + "/p/SECRET.TOKEN/":              base + "/p/***/",
		base + "/p/SECRET.TOKEN":               base + "/p/***",
		base + "/p/SECRET.TOKEN/assets/a.js":   base + "/p/***/assets/a.js",
		base + "/preview-token":                base + "/preview-token",
		base + "/events":                       base + "/events",
		base + "/p":                            base + "/p",
		"/dashboard/api/apps/x/p/SECRET/y":     "/dashboard/api/apps/x/p/SECRET/y",
		"/api/builder/sessions/" + id + "/p/T": "/api/builder/sessions/" + id + "/p/T",
	} {
		if got := RedactPreviewTokenPath(in); got != want {
			t.Errorf("RedactPreviewTokenPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPreviewProxy_RootAndHeadAndMethods(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)

	// The root only serves with the trailing slash (the bare URL redirects).
	target := e.previewPath(tok, "/")
	if w := e.fetch(http.MethodGet, target, nil); w.Code != http.StatusOK || w.Body.String() != "preview:/v1/sessions/"+e.sess.ID+"/preview/" {
		t.Errorf("%s: got %d %q", target, w.Code, w.Body.String())
	}
	if w := e.fetch(http.MethodHead, e.previewPath(tok, "/index.html"), nil); w.Code != http.StatusOK {
		t.Errorf("HEAD: got %d", w.Code)
	}
	n := len(e.upstreamCalls())
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(m, e.previewPath(tok, "/index.html"), nil)
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		// chi answers 405 for methods without a route; the handler itself would too.
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", m, rec.Code)
		}
	}
	// Called directly (as another router might), the handler refuses them too.
	req := httptest.NewRequest(http.MethodPost, e.previewPath(tok, "/index.html"), nil)
	req = withURLParam(req, "id", e.sess.ID)
	rec := httptest.NewRecorder()
	e.h.PreviewProxy(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("direct POST: want 405, got %d", rec.Code)
	}
	assertPreviewHeaders(t, rec)
	if len(e.upstreamCalls()) != n {
		t.Error("a non-GET request reached the builder")
	}
}

func TestPreviewProxy_PathTraversalIsRejected(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)

	for _, rest := range []string{
		"/../../etc/passwd",
		"/assets/../../secret",
		"/./index.html",
		"/%2e%2e/%2e%2e/etc/passwd",
		"/%2E%2E/secret",
		"/assets/%2e%2e/secret",
		"/a%5c..%5csecret",
		"/a%00b",
		"/..",
	} {
		w := e.fetch(http.MethodGet, e.previewPath(tok, rest), nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d: %s", rest, w.Code, w.Body.String())
			continue
		}
		assertPreviewHeaders(t, w)
	}
	if n := len(e.upstreamCalls()); n != 0 {
		t.Errorf("%d traversal attempts reached the builder", n)
	}
	// Ordinary nested paths still work.
	if w := e.fetch(http.MethodGet, e.previewPath(tok, "/assets/chunks/a-1b2c.js"), nil); w.Code != http.StatusOK {
		t.Errorf("nested path: got %d", w.Code)
	}
}

func TestCleanPreviewPath(t *testing.T) {
	good := map[string]string{
		"":                   "/",
		"index.html":         "/index.html",
		"assets/a.js":        "/assets/a.js",
		"/assets/a.js":       "/assets/a.js",
		"a%20b/c.png":        "/a b/c.png",
		"assets/chunk..js":   "/assets/chunk..js",
		"assets/.well-known": "/assets/.well-known",
	}
	for in, want := range good {
		if got, ok := cleanPreviewPath(in); !ok || got != want {
			t.Errorf("cleanPreviewPath(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"..", "a/..", "../a", "a/./b", "%2e%2e/a", "a%2f..%2fb", "a\\b", "a%5cb", "a\x00b", "a%00b", "a//b", "a%zz", "a\nb"} {
		if got, ok := cleanPreviewPath(in); ok {
			t.Errorf("cleanPreviewPath(%q) = %q, want rejection", in, got)
		}
	}
}

func TestPreviewProxy_UpstreamFailureKeepsHeadersAndLeaksNothing(t *testing.T) {
	e := newBPrevEnv(t)
	e.mu.Lock()
	e.upstreamDown = true
	e.mu.Unlock()
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)

	w := e.fetch(http.MethodGet, e.previewPath(tok, "/index.html"), nil)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "preview is unavailable") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	assertPreviewHeaders(t, w)
	if strings.Contains(w.Body.String(), "127.0.0.1") || strings.Contains(w.Body.String(), "dial") {
		t.Errorf("the error leaks the builder address: %q", w.Body.String())
	}
}

func TestPreviewProxy_NotConfiguredIs503(t *testing.T) {
	e := newBPrevEnv(t)
	tok := e.tokenFor(t, e.sess.ID, e.owner.ID)
	for name, cfg := range map[string]BuilderHandlerConfig{
		"no signer":  {Store: e.st, Sandbox: e.fake},
		"no builder": {PreviewSigner: e.signer},
	} {
		h := NewBuilderHandler(e.pool, cfg)
		rec := httptest.NewRecorder()
		e.routerFor(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, e.previewPath(tok, "/index.html"), nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: want 503, got %d", name, rec.Code)
		}
		assertPreviewHeaders(t, rec)
	}
}

// ---- minting ----

func TestMintPreviewToken_OwnerGetsAWorkingToken(t *testing.T) {
	e := newBPrevEnv(t)
	w := e.do(t, e.h.MintPreviewToken, e.owner, http.MethodPost, "/x", "", "id", e.sess.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
	got := decodeJSON[BuilderPreviewToken](t, w)
	claims, err := e.signer.VerifyPreview(got.Token)
	if err != nil || claims.SessionID != e.sess.ID || claims.UserID != e.owner.ID {
		t.Fatalf("token claims = %+v (%v)", claims, err)
	}
	if d := time.Until(got.ExpiresAt); d < 9*time.Minute || d > builder.PreviewTokenTTL+time.Second {
		t.Errorf("expires_at is %v away, want about %v", d, builder.PreviewTokenTTL)
	}
	wantURL := "/dashboard/api/builder/sessions/" + e.sess.ID + "/p/" + got.Token + "/"
	if got.URL != wantURL {
		t.Errorf("url = %q", got.URL)
	}

	// The returned URL works as an iframe src.
	rec := e.fetch(http.MethodGet, got.URL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", got.URL, rec.Code, rec.Body.String())
	}
	assertPreviewHeaders(t, rec)
}

func TestMintPreviewToken_OwnerOnlyAndGuards(t *testing.T) {
	e := newBPrevEnv(t)

	if w := e.do(t, e.h.MintPreviewToken, e.other, http.MethodPost, "/x", "", "id", e.sess.ID); w.Code != http.StatusNotFound {
		t.Errorf("another user: want 404, got %d", w.Code)
	}
	if w := e.do(t, e.h.MintPreviewToken, e.super, http.MethodPost, "/x", "", "id", e.sess.ID); w.Code != http.StatusNotFound {
		t.Errorf("superadmin gets no one else's session: want 404, got %d", w.Code)
	}
	if w := e.do(t, e.h.MintPreviewToken, nil, http.MethodPost, "/x", "", "id", e.sess.ID); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", w.Code)
	}
	if w := e.do(t, e.h.MintPreviewToken, e.owner, http.MethodPost, "/x", "", "id", "nope"); w.Code != http.StatusNotFound {
		t.Errorf("bad id: want 404, got %d", w.Code)
	}

	noSigner := NewBuilderHandler(e.pool, BuilderHandlerConfig{Store: e.st, Sandbox: e.fake})
	if w := e.do(t, noSigner.MintPreviewToken, e.owner, http.MethodPost, "/x", "", "id", e.sess.ID); w.Code != http.StatusServiceUnavailable {
		t.Errorf("no signer: want 503, got %d", w.Code)
	}

	if err := e.st.ArchiveSession(context.Background(), e.owner.ID, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.do(t, e.h.MintPreviewToken, e.owner, http.MethodPost, "/x", "", "id", e.sess.ID); w.Code != http.StatusNotFound {
		t.Errorf("archived: want 404, got %d", w.Code)
	}
}

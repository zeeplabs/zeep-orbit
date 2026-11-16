package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// seenRequest is what a fake upstream observed.
type seenRequest struct {
	method, escapedPath, rawQuery, host, auth, cookie, userAgent string
}

// fakeUpstream is an httptest registry that records every request.
type fakeUpstream struct {
	*httptest.Server
	mu   sync.Mutex
	seen []seenRequest
}

func newFakeUpstream(t *testing.T, name string, h http.HandlerFunc) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, seenRequest{
			method: r.Method, escapedPath: r.URL.EscapedPath(), rawQuery: r.URL.RawQuery,
			host: r.Host, auth: r.Header.Get("Authorization"), cookie: r.Header.Get("Cookie"),
			userAgent: r.Header.Get("User-Agent"),
		})
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		fmt.Fprintf(w, "from-%s", name)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeUpstream) hits() []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenRequest(nil), f.seen...)
}

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// newTestProxy returns the handler and a buffer holding its log output. The
// handler is driven through httptest.NewRecorder so logging is synchronous.
func newTestProxy(t *testing.T, spec string, env map[string]string) (*proxy, *bytes.Buffer) {
	t.Helper()
	ups, err := parseUpstreams(spec, envMap(env))
	if err != nil {
		t.Fatalf("parseUpstreams: %v", err)
	}
	buf := &bytes.Buffer{}
	return newProxy(ups, slog.New(slog.NewTextHandler(buf, nil))), buf
}

func do(p http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

func TestNonReadMethodsAreRejected(t *testing.T) {
	up := newFakeUpstream(t, "def", nil)
	p, _ := newTestProxy(t, up.URL, nil)

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		rec := do(p, m, "/left-pad", nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s: Allow = %q, want %q", m, got, "GET, HEAD")
		}
	}
	if n := len(up.hits()); n != 0 {
		t.Fatalf("upstream received %d requests, want 0", n)
	}
}

func TestRoutingByScope(t *testing.T) {
	def := newFakeUpstream(t, "def", nil)
	comp := newFakeUpstream(t, "comp", nil)
	p, _ := newTestProxy(t, def.URL+", @company="+comp.URL, nil)

	tests := []struct {
		target   string
		wantBody string
		wantPath string
	}{
		{"/left-pad", "from-def", "/left-pad"},
		{"/left-pad/-/left-pad-1.0.0.tgz", "from-def", "/left-pad/-/left-pad-1.0.0.tgz"},
		{"/@company/pkg", "from-comp", "/@company/pkg"},
		{"/@company%2Fpkg", "from-comp", "/@company%2Fpkg"},
		{"/@COMPANY%2fpkg", "from-comp", "/@COMPANY%2fpkg"},
		{"/@company/pkg/-/pkg-1.0.0.tgz", "from-comp", "/@company/pkg/-/pkg-1.0.0.tgz"},
		// A scope with no dedicated upstream falls back to the default one.
		{"/@other/pkg", "from-def", "/@other/pkg"},
	}
	for _, tc := range tests {
		rec := do(p, http.MethodGet, tc.target, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != tc.wantBody {
			t.Errorf("%s: got %d %q, want 200 %q", tc.target, rec.Code, rec.Body.String(), tc.wantBody)
			continue
		}
		srv := def
		if tc.wantBody == "from-comp" {
			srv = comp
		}
		hits := srv.hits()
		if got := hits[len(hits)-1].escapedPath; got != tc.wantPath {
			t.Errorf("%s: upstream path = %q, want %q", tc.target, got, tc.wantPath)
		}
	}
}

func TestNoDefaultUpstreamRejectsUnscoped(t *testing.T) {
	comp := newFakeUpstream(t, "comp", nil)
	p, _ := newTestProxy(t, "@company="+comp.URL, nil)

	if rec := do(p, http.MethodGet, "/left-pad", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if n := len(comp.hits()); n != 0 {
		t.Fatalf("scoped upstream received %d requests, want 0", n)
	}
}

func TestTokenIsInjectedOnlyForItsOwnUpstream(t *testing.T) {
	def := newFakeUpstream(t, "def", nil)
	comp := newFakeUpstream(t, "comp", nil)
	p, _ := newTestProxy(t, def.URL+",@company="+comp.URL, map[string]string{
		"REGISTRY_PROXY_TOKEN_DEFAULT": "tok-default",
		"REGISTRY_PROXY_TOKEN_COMPANY": "tok-company",
	})

	do(p, http.MethodGet, "/left-pad", nil)
	do(p, http.MethodGet, "/@company/pkg", nil)

	if got := def.hits()[0].auth; got != "Bearer tok-default" {
		t.Errorf("default upstream auth = %q, want its own token", got)
	}
	if got := comp.hits()[0].auth; got != "Bearer tok-company" {
		t.Errorf("scoped upstream auth = %q, want its own token", got)
	}
	for _, h := range def.hits() {
		if strings.Contains(h.auth, "tok-company") {
			t.Error("default upstream received the scoped token")
		}
	}
	for _, h := range comp.hits() {
		if strings.Contains(h.auth, "tok-default") {
			t.Error("scoped upstream received the default token")
		}
	}
}

func TestClientHeadersAreNotForwarded(t *testing.T) {
	noTok := newFakeUpstream(t, "plain", nil)
	p, _ := newTestProxy(t, noTok.URL, nil)

	do(p, http.MethodGet, "/left-pad", map[string]string{
		"Authorization": "Bearer client-secret",
		"Cookie":        "sid=abc",
		"User-Agent":    "evil/1.0",
	})
	h := noTok.hits()[0]
	if h.auth != "" || h.cookie != "" {
		t.Errorf("client credentials leaked upstream: auth=%q cookie=%q", h.auth, h.cookie)
	}
	if h.userAgent != userAgent {
		t.Errorf("User-Agent = %q, want %q", h.userAgent, userAgent)
	}

	// With a configured token the proxy's own value wins over the client's.
	withTok := newFakeUpstream(t, "tok", nil)
	p2, _ := newTestProxy(t, withTok.URL, map[string]string{"REGISTRY_PROXY_TOKEN_DEFAULT": "proxy-token"})
	do(p2, http.MethodGet, "/left-pad", map[string]string{"Authorization": "Bearer client-secret"})
	if got := withTok.hits()[0].auth; got != "Bearer proxy-token" {
		t.Errorf("auth = %q, want the proxy's token", got)
	}
}

func TestHostHeaderIsNotForwarded(t *testing.T) {
	up := newFakeUpstream(t, "def", nil)
	p, _ := newTestProxy(t, up.URL, nil)

	req := httptest.NewRequest(http.MethodGet, "/left-pad", nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got, want := up.hits()[0].host, strings.TrimPrefix(up.URL, "http://"); got != want {
		t.Fatalf("upstream Host = %q, want %q", got, want)
	}
}

func TestLogLineHasNoTokenAndNoQuery(t *testing.T) {
	up := newFakeUpstream(t, "def", nil)
	p, buf := newTestProxy(t, up.URL, map[string]string{"REGISTRY_PROXY_TOKEN_DEFAULT": "tok-super-secret"})

	do(p, http.MethodGet, "/left-pad?auth=qs-secret&x=1", map[string]string{"Authorization": "Bearer client-secret"})

	line := strings.TrimSpace(buf.String())
	if strings.Count(line, "\n") != 0 || line == "" {
		t.Fatalf("want exactly one log line, got %q", line)
	}
	for _, forbidden := range []string{"tok-super-secret", "client-secret", "qs-secret", "?", "auth=", "Authorization", "Bearer"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("log line contains %q: %s", forbidden, line)
		}
	}
	for _, want := range []string{"method=GET", "path=/left-pad", "upstream=DEFAULT", "status=200", "duration_ms="} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %q: %s", want, line)
		}
	}
	// The query string is still forwarded: it is part of the registry API.
	if got := up.hits()[0].rawQuery; got != "auth=qs-secret&x=1" {
		t.Errorf("upstream query = %q", got)
	}
}

func TestMalformedAndAbsoluteTargetsAreRejected(t *testing.T) {
	up := newFakeUpstream(t, "def", nil)
	p, _ := newTestProxy(t, up.URL, nil)

	targets := []string{
		"/a/../b",
		"/a/%2e%2e/b",
		"/a/%2E./b",
		"/%252e%252e/etc",
		"/a%5cb",
		"/a%5Cb",
		"/a%00b",
		"//evil.example/x",
		"http://evil.example/left-pad",
		"https://evil.example:8443/@company/pkg",
	}
	for _, target := range targets {
		rec := do(p, http.MethodGet, target, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
	if n := len(up.hits()); n != 0 {
		t.Fatalf("upstream received %d requests, want 0: %+v", n, up.hits())
	}
}

func TestRedirectToNonAllowlistedHostIsNotFollowed(t *testing.T) {
	evil := newFakeUpstream(t, "evil", nil)
	up := newFakeUpstream(t, "def", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
	})
	p, _ := newTestProxy(t, up.URL, map[string]string{"REGISTRY_PROXY_TOKEN_DEFAULT": "tok-default"})

	rec := do(p, http.MethodGet, "/left-pad", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if n := len(evil.hits()); n != 0 {
		t.Fatalf("off-allowlist host received %d requests (auth=%q)", n, evil.hits()[0].auth)
	}
	if strings.Contains(rec.Body.String(), evil.URL) {
		t.Errorf("response body leaks redirect target: %q", rec.Body.String())
	}
}

func TestRedirectToAllowlistedHostDropsCredentials(t *testing.T) {
	cdn := newFakeUpstream(t, "cdn", nil)
	def := newFakeUpstream(t, "def", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/blob", http.StatusFound)
	})
	p, _ := newTestProxy(t, def.URL+",@cdn="+cdn.URL, map[string]string{"REGISTRY_PROXY_TOKEN_DEFAULT": "tok-default"})

	rec := do(p, http.MethodGet, "/left-pad", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "from-cdn" {
		t.Fatalf("got %d %q, want followed redirect", rec.Code, rec.Body.String())
	}
	if got := cdn.hits()[0].auth; got != "" {
		t.Fatalf("credential replayed to a different host: %q", got)
	}
}

func TestHealthzDoesNotTouchUpstreams(t *testing.T) {
	up := newFakeUpstream(t, "def", nil)
	p, buf := newTestProxy(t, up.URL, nil)

	rec := do(p, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q, want 200 ok", rec.Code, rec.Body.String())
	}
	if n := len(up.hits()); n != 0 {
		t.Fatalf("upstream received %d requests, want 0", n)
	}
	if buf.Len() != 0 {
		t.Errorf("probe was logged: %s", buf.String())
	}
}

func TestHeadIsForwardedWithoutBody(t *testing.T) {
	up := newFakeUpstream(t, "def", nil)
	p, _ := newTestProxy(t, up.URL, nil)

	rec := do(p, http.MethodHead, "/left-pad", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("got %d with %d body bytes", rec.Code, rec.Body.Len())
	}
	if got := up.hits()[0].method; got != http.MethodHead {
		t.Fatalf("upstream method = %s", got)
	}
}

func TestUpstreamErrorIsGenericBadGateway(t *testing.T) {
	dead := httptest.NewServer(nil)
	url := dead.URL
	dead.Close()
	p, buf := newTestProxy(t, url, nil)

	rec := do(p, http.MethodGet, "/left-pad?secret=1", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(buf.String(), "secret") {
		t.Fatal("query string leaked into response or log")
	}
}

func TestParseUpstreams(t *testing.T) {
	t.Run("default when empty", func(t *testing.T) {
		set, err := parseUpstreams("  ", envMap(nil))
		if err != nil {
			t.Fatal(err)
		}
		if set.def == nil || set.def.base.String() != "https://registry.npmjs.org" || set.def.name != "DEFAULT" {
			t.Fatalf("unexpected default: %+v", set.def)
		}
		if _, ok := set.hosts["registry.npmjs.org:443"]; !ok {
			t.Fatalf("allowlist = %v", set.hosts)
		}
	})

	t.Run("token names", func(t *testing.T) {
		for scope, want := range map[string]string{"@company": "COMPANY", "@my-org": "MY_ORG", "@a.b_c": "A_B_C"} {
			if got := scopeTokenName(scope); got != want {
				t.Errorf("scopeTokenName(%q) = %q, want %q", scope, got, want)
			}
		}
	})

	t.Run("token read per upstream", func(t *testing.T) {
		set, err := parseUpstreams("https://registry.npmjs.org,@My-Org=https://npm.company.com/npm/", envMap(map[string]string{
			"REGISTRY_PROXY_TOKEN_MY_ORG": "t1",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if set.def.token != "" || set.scoped["@my-org"].token != "t1" {
			t.Fatalf("tokens misassigned: def=%q scoped=%q", set.def.token, set.scoped["@my-org"].token)
		}
	})

	bad := map[string]struct {
		spec string
		env  map[string]string
	}{
		"not http":            {"ftp://x.example", nil},
		"relative":            {"registry.npmjs.org", nil},
		"userinfo":            {"https://u:p@x.example", nil},
		"scope without url":   {"@company", nil},
		"invalid scope":       {"@Co mpany=https://x.example", nil},
		"duplicate default":   {"https://a.example,https://b.example", nil},
		"colliding scopes":    {"@a-b=https://a.example,@a_b=https://b.example", nil},
		"token over http":     {"http://registry.internal.example", map[string]string{"REGISTRY_PROXY_TOKEN_DEFAULT": "zq9-secret"}},
		"token with newline":  {"https://x.example", map[string]string{"REGISTRY_PROXY_TOKEN_DEFAULT": "a\r\nX-Evil: 1"}},
		"only empty entries":  {",,", nil},
		"query in upstream":   {"https://x.example/?a=b", nil},
		"fragment in upstrm.": {"https://x.example/#a", nil},
	}
	for name, tc := range bad {
		t.Run("reject "+name, func(t *testing.T) {
			_, err := parseUpstreams(tc.spec, envMap(tc.env))
			if err == nil {
				t.Fatal("want error")
			}
			for _, secret := range tc.env {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks token: %v", err)
				}
			}
		})
	}
}

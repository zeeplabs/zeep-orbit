package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Environment variables understood by zeep-registry-proxy.
//
//	REGISTRY_PROXY_ADDR       listen address, default ":4873".
//
//	REGISTRY_PROXY_UPSTREAMS  comma-separated upstream entries. An entry is
//	                          either a bare URL ("https://registry.npmjs.org",
//	                          the default upstream used for every package that
//	                          matches no scope) or "@scope=URL" (a scoped
//	                          upstream that serves package names starting with
//	                          that scope). When empty the value is exactly
//	                          "https://registry.npmjs.org". If entries are given
//	                          but none is bare, unscoped packages are rejected
//	                          with 403: the allowlist is only what is configured,
//	                          nothing is implicit.
//
//	REGISTRY_PROXY_TOKEN_<NAME>  optional bearer token for ONE upstream,
//	                          injected as "Authorization: Bearer <token>" only on
//	                          requests to that upstream. <NAME> is "DEFAULT" for
//	                          the bare upstream; for a scoped upstream it is the
//	                          scope uppercased, with the leading '@' dropped and
//	                          every non-alphanumeric character replaced by '_'
//	                          (@company -> COMPANY, @my-org -> MY_ORG, giving
//	                          REGISTRY_PROXY_TOKEN_COMPANY, REGISTRY_PROXY_TOKEN_MY_ORG).
//	                          A token is refused for plain-http upstreams unless
//	                          the host is loopback, so it can never cross the
//	                          network in clear text.
const (
	envAddr      = "REGISTRY_PROXY_ADDR"
	envUpstreams = "REGISTRY_PROXY_UPSTREAMS"
	envTokenPref = "REGISTRY_PROXY_TOKEN_"

	defaultAddr     = ":4873"
	defaultUpstream = "https://registry.npmjs.org"
	defaultName     = "DEFAULT"

	// userAgent is fixed so the builder cannot influence what upstream
	// registries (and their access logs) see about the caller.
	userAgent = "zeep-registry-proxy/1"

	maxRedirects      = 5
	maxLoggedPathLen  = 256
	upstreamTimeout   = 5 * time.Minute
	dialTimeout       = 5 * time.Second
	tlsTimeout        = 10 * time.Second
	responseHeaderTTL = 30 * time.Second
)

// scopeRE restricts scope names so they cannot smuggle separators into the
// routing table or the token env var name.
var scopeRE = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]*$`)

// errRedirectNotAllowed marks a redirect that would leave the allowlist (or
// downgrade https to http). It is mapped to a fixed 403 for the client.
var errRedirectNotAllowed = errors.New("redirect target not allowed")

// upstream is one configured registry.
type upstream struct {
	name  string // "DEFAULT" or the token NAME of the scope; safe to log.
	scope string // "" for the default upstream, "@scope" otherwise.
	base  *url.URL
	token string // never logged.
}

// upstreamSet is the parsed, validated routing table and host allowlist.
type upstreamSet struct {
	def    *upstream
	scoped map[string]*upstream
	hosts  map[string]struct{} // canonical host:port of every upstream.
}

// scopeTokenName derives <NAME> in REGISTRY_PROXY_TOKEN_<NAME> from a scope.
func scopeTokenName(scope string) string {
	s := strings.ToUpper(strings.TrimPrefix(scope, "@"))
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
}

// canonicalHost returns lowercase host:port with the scheme's default port
// made explicit, so "registry.npmjs.org" and "registry.npmjs.org:443" compare
// equal in the allowlist.
func canonicalHost(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validToken rejects control characters so a token from the environment can
// never inject extra headers.
func validToken(tok string) bool {
	for _, r := range tok {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return tok != ""
}

func parseUpstreamURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("URL must be absolute http(s) with a host")
	}
	// User info would put credentials in the config; RawQuery/Fragment have no
	// meaning for a registry base URL and would be silently dropped.
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("URL must not contain user info, query or fragment")
	}
	return u, nil
}

// parseUpstreams builds the upstream set from the REGISTRY_PROXY_UPSTREAMS
// value and per-upstream tokens read through getenv. Error messages never
// include token values.
func parseUpstreams(spec string, getenv func(string) string) (*upstreamSet, error) {
	if strings.TrimSpace(spec) == "" {
		spec = defaultUpstream
	}
	set := &upstreamSet{scoped: map[string]*upstream{}, hosts: map[string]struct{}{}}
	names := map[string]struct{}{}

	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		up := &upstream{name: defaultName}
		rawURL := entry
		if strings.HasPrefix(entry, "@") {
			i := strings.Index(entry, "=")
			if i < 0 {
				return nil, fmt.Errorf("upstream %q: scoped entry must be @scope=URL", entry)
			}
			up.scope = strings.ToLower(strings.TrimSpace(entry[:i]))
			rawURL = strings.TrimSpace(entry[i+1:])
			if !scopeRE.MatchString(up.scope) {
				return nil, fmt.Errorf("upstream scope %q is invalid", up.scope)
			}
			up.name = scopeTokenName(up.scope)
		}
		base, err := parseUpstreamURL(rawURL)
		if err != nil {
			return nil, fmt.Errorf("upstream %s: %w", up.name, err)
		}
		up.base = base

		if _, dup := names[up.name]; dup {
			return nil, fmt.Errorf("upstream %s configured twice (or two scopes map to the same token name)", up.name)
		}
		names[up.name] = struct{}{}

		if tok := strings.TrimSpace(getenv(envTokenPref + up.name)); tok != "" {
			if !validToken(tok) {
				return nil, fmt.Errorf("%s%s contains whitespace or control characters", envTokenPref, up.name)
			}
			if base.Scheme != "https" && !isLoopbackHost(base.Hostname()) {
				return nil, fmt.Errorf("%s%s is set but upstream %s is not https", envTokenPref, up.name, up.name)
			}
			up.token = tok
		}

		if up.scope == "" {
			set.def = up
		} else {
			set.scoped[up.scope] = up
		}
		set.hosts[canonicalHost(base)] = struct{}{}
	}
	if set.def == nil && len(set.scoped) == 0 {
		return nil, errors.New("no upstreams configured")
	}
	return set, nil
}

// pick returns the upstream for a package scope ("" for unscoped); an unknown
// scope falls back to the default upstream, which may be nil.
func (s *upstreamSet) pick(scope string) *upstream {
	if up, ok := s.scoped[scope]; ok {
		return up
	}
	return s.def
}

func (s *upstreamSet) allowed(u *url.URL) bool {
	_, ok := s.hosts[canonicalHost(u)]
	return ok
}

// proxy is the read-only npm registry reverse proxy.
//
// Tarball URLs: npm packuments contain absolute tarball URLs that point at the
// upstream host. The builder runs npm with npm_config_replace-registry-host=always,
// so npm rewrites those URLs to the proxy host itself and the proxy does NOT
// need to rewrite JSON bodies. [Incerto] how npm behaves with scoped private
// registries under replace-registry-host (it may only rewrite the configured
// default registry host) must be verified in task T10 before relying on it.
type proxy struct {
	ups    *upstreamSet
	client *http.Client
	log    *slog.Logger
}

func newProxy(ups *upstreamSet, logger *slog.Logger) *proxy {
	p := &proxy{ups: ups, log: logger}
	p.client = &http.Client{
		Transport: &http.Transport{
			// Never honour HTTP(S)_PROXY from the environment: the proxy is
			// the egress point and must dial upstreams directly.
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSHandshakeTimeout:   tlsTimeout,
			ResponseHeaderTimeout: responseHeaderTTL,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			// Pass bytes through untouched: the client's Accept-Encoding is
			// forwarded, so we must not let the transport decode gzip.
			DisableCompression: true,
			ForceAttemptHTTP2:  true,
		},
		CheckRedirect: p.checkRedirect,
	}
	return p
}

// checkRedirect keeps redirects inside the allowlist (SSRF defense) and makes
// sure one upstream's credential is never replayed to another host.
func (p *proxy) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errRedirectNotAllowed
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errRedirectNotAllowed
	}
	if !p.ups.allowed(req.URL) {
		return errRedirectNotAllowed
	}
	prev := via[len(via)-1]
	if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errRedirectNotAllowed
	}
	if canonicalHost(prev.URL) != canonicalHost(req.URL) {
		req.Header.Del("Authorization")
	}
	return nil
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// ServeHTTP implements http.Handler.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		p.logged(w, r, "-", func(rec *statusRecorder) string {
			http.Error(rec, "method not allowed", http.StatusMethodNotAllowed)
			return "-"
		})
		return
	}
	// Liveness never touches upstreams, so a registry outage cannot make the
	// orchestrator restart a healthy proxy. Not logged: probes are noise.
	if r.RequestURI == "/healthz" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
		return
	}
	p.logged(w, r, "-", func(rec *statusRecorder) string { return p.forward(rec, r) })
}

// logged runs fn and emits exactly one structured line. Only the path is
// logged: the query string and all headers (Authorization) are deliberately
// omitted because they can carry credentials.
func (p *proxy) logged(w http.ResponseWriter, r *http.Request, name string, fn func(*statusRecorder) string) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w}
	if got := fn(rec); got != "" {
		name = got
	}
	path := r.URL.EscapedPath()
	if len(path) > maxLoggedPathLen {
		path = path[:maxLoggedPathLen]
	}
	p.log.Info("request",
		"method", r.Method,
		"path", path,
		"upstream", name,
		"status", rec.status,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// validatePath rejects request shapes that could bend routing or reach hosts
// outside the allowlist. The check runs on the escaped path, once and twice
// decoded, because upstreams differ in how many times they decode.
func validatePath(r *http.Request) error {
	// Absolute-form ("GET http://evil/x") and protocol-relative ("//evil/x")
	// request targets are how clients try to steer a proxy to another host.
	if !strings.HasPrefix(r.RequestURI, "/") || strings.HasPrefix(r.RequestURI, "//") {
		return errors.New("request target must be an absolute path")
	}
	if r.URL.Scheme != "" || r.URL.Host != "" {
		return errors.New("absolute request URI not allowed")
	}
	escaped := r.URL.EscapedPath()
	once, err := url.PathUnescape(escaped)
	if err != nil {
		return errors.New("invalid path encoding")
	}
	twice, err := url.PathUnescape(once)
	if err != nil {
		twice = once
	}
	for _, p := range []string{escaped, once, twice} {
		if strings.Contains(p, "..") || strings.ContainsAny(p, "\\\x00") {
			return errors.New("path contains forbidden sequence")
		}
	}
	if strings.Contains(strings.ToLower(escaped), "%00") || strings.Contains(strings.ToLower(once), "%00") {
		return errors.New("path contains forbidden sequence")
	}
	return nil
}

// scopeOf extracts "@scope" from the first path segment. npm sends scoped
// packages as /@scope/pkg or /@scope%2Fpkg, plus /@scope/pkg/-/pkg-1.0.0.tgz;
// the segment is decoded first so both spellings resolve identically.
func scopeOf(escapedPath string) string {
	seg := strings.TrimPrefix(escapedPath, "/")
	if i := strings.Index(seg, "/"); i >= 0 {
		seg = seg[:i]
	}
	dec, err := url.PathUnescape(seg)
	if err != nil || !strings.HasPrefix(dec, "@") {
		return ""
	}
	if i := strings.Index(dec, "/"); i >= 0 {
		dec = dec[:i]
	}
	return strings.ToLower(dec)
}

// forwardedRequestHeaders is an allowlist, not a denylist: anything not named
// here (Authorization, Cookie, X-Forwarded-*, arbitrary npm-* headers) never
// reaches upstream.
var forwardedRequestHeaders = []string{
	"Accept", "Accept-Encoding", "If-None-Match", "If-Modified-Since", "Range", "If-Range",
}

var hopByHop = map[string]struct{}{
	"Connection": {}, "Proxy-Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {},
	"Proxy-Authorization": {}, "Te": {}, "Trailer": {}, "Transfer-Encoding": {}, "Upgrade": {},
	// Registries have no business setting cookies on the builder.
	"Set-Cookie": {},
}

// forward validates, routes and proxies one GET/HEAD request. It returns the
// upstream name for the access log.
func (p *proxy) forward(w http.ResponseWriter, r *http.Request) string {
	if err := validatePath(r); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return "-"
	}

	escaped := r.URL.EscapedPath()
	up := p.ups.pick(scopeOf(escaped))
	if up == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "-"
	}

	target := *up.base
	joined := strings.TrimRight(up.base.EscapedPath(), "/") + escaped
	decoded, err := url.PathUnescape(joined)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return up.name
	}
	target.Path, target.RawPath = decoded, joined
	target.RawQuery = r.URL.RawQuery
	// Defense in depth: the host can only ever be the configured one.
	if !p.ups.allowed(&target) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return up.name
	}

	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, r.Method, target.String(), nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return up.name
	}
	// out.Host stays empty so the transport uses target.Host; the inbound Host
	// header is never forwarded.
	for _, h := range forwardedRequestHeaders {
		if v := r.Header.Values(h); len(v) > 0 {
			out.Header[http.CanonicalHeaderKey(h)] = append([]string(nil), v...)
		}
	}
	out.Header.Set("User-Agent", userAgent)
	if up.token != "" {
		out.Header.Set("Authorization", "Bearer "+up.token)
	}

	resp, err := p.client.Do(out)
	if err != nil {
		if errors.Is(err, errRedirectNotAllowed) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return up.name
		}
		// Fixed body: err text embeds the upstream URL, which may carry a
		// query string.
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return up.name
	}
	defer resp.Body.Close()

	conn := map[string]struct{}{}
	for _, v := range resp.Header.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			conn[http.CanonicalHeaderKey(strings.TrimSpace(tok))] = struct{}{}
		}
	}
	for k, vv := range resp.Header {
		if _, skip := hopByHop[k]; skip {
			continue
		}
		if _, skip := conn[k]; skip {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		// A mid-body failure cannot change the status any more; the client
		// sees a truncated body and npm's integrity check rejects it.
		_, _ = io.Copy(w, resp.Body)
	}
	return up.name
}

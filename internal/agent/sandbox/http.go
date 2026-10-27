package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/builder"
)

const (
	// maxSmallResponse bounds JSON answers that hold at most tool output
	// (ops, build, build-log, health, restore).
	maxSmallResponse = 4 << 20
	// maxBundleResponse bounds init/commit/revert answers, which carry a base64
	// git bundle (the builder caps a bundle at 32 MiB by default).
	maxBundleResponse = 64 << 20
	// maxErrorBody bounds an error envelope.
	maxErrorBody = 64 << 10
)

var (
	sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)
	templatePattern  = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
)

// httpSandbox is the P1 [Sandbox]: one builder reachable at a base URL, every
// request signed with a fresh short-lived token (builder.Signer).
type httpSandbox struct {
	base   *url.URL
	signer *builder.Signer
	client *http.Client
}

// NewHTTP returns a [Sandbox] that calls the builder at baseURL
// (BUILDER_URL). signer is built from BUILDER_SHARED_SECRET. client may be nil
// for a default client; it is copied and its redirect policy is replaced so
// that a redirect can never carry X-Builder-Token to another host. The client
// must not set a short overall Timeout: a build step can run for minutes, so
// callers bound calls with the context instead.
func NewHTTP(baseURL string, signer *builder.Signer, client *http.Client) (Sandbox, error) {
	if signer == nil {
		return nil, errors.New("sandbox: signer is required")
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("sandbox: builder URL must be an absolute http(s) URL without credentials, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	c := &http.Client{}
	if client != nil {
		cp := *client
		c = &cp
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &httpSandbox{base: u, signer: signer, client: c}, nil
}

// ---- request plumbing ----

func checkSession(sessionID string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return &Error{Status: http.StatusBadRequest, Code: "invalid_session_id", Message: "invalid session id"}
	}
	return nil
}

func sessionPath(sessionID, suffix string) string {
	return "/v1/sessions/" + sessionID + suffix
}

// newRequest builds a signed request. path is the builder path (no base path,
// no query); the token is bound to it, to the session id and to the method.
func (s *httpSandbox) newRequest(ctx context.Context, method, tokenSession, path, rawQuery string, body io.Reader) (*http.Request, error) {
	tok, err := s.signer.SignRequest(tokenSession, method, path)
	if err != nil {
		return nil, &Error{Code: "invalid_request", Message: "could not sign the builder request", Err: err}
	}
	u := *s.base
	u.Path = s.base.Path + path
	u.RawQuery = rawQuery
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, &Error{Code: "invalid_request", Message: "could not build the builder request", Err: err}
	}
	req.Header.Set(builder.TokenHeader, tok)
	return req, nil
}

// send performs req. Transport failures become an *Error whose message is
// generic; the cause (which may contain the builder URL) stays in Err.
func (s *httpSandbox) send(req *http.Request) (*http.Response, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &Error{Code: "unavailable", Message: "builder is unavailable", Err: err}
	}
	return resp, nil
}

// errorFrom turns a non-2xx response into an error and closes the body. A 404
// "unknown_session" becomes ErrUnknownSession.
func errorFrom(resp *http.Response) error {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	var env builder.ErrorResponse
	if err := json.Unmarshal(raw, &env); err != nil || env.Error.Code == "" {
		// Not the builder's envelope: a proxy or a crashed process answered.
		return &Error{Status: resp.StatusCode, Code: "bad_response", Message: "unexpected response from the builder"}
	}
	if resp.StatusCode == http.StatusNotFound && env.Error.Code == "unknown_session" {
		return ErrUnknownSession
	}
	e := &Error{Status: resp.StatusCode, Code: sanitizeCode(env.Error.Code)}
	switch {
	case e.ToolLevel():
		e.Message = sanitizeMessage(env.Error.Message)
	case resp.StatusCode == http.StatusUnauthorized:
		e.Message = "builder rejected the request credentials"
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Message = "builder is at capacity, retry later"
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 && secs <= 3600 {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
	default:
		e.Message = "builder failed to complete the request"
	}
	return e
}

func sanitizeCode(c string) string {
	if len(c) > 64 {
		c = c[:64]
	}
	for _, r := range c {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "unknown"
		}
	}
	return c
}

// doJSON sends req and decodes a 200 JSON answer (at most max bytes) into out.
func (s *httpSandbox) doJSON(req *http.Request, max int64, out any) error {
	resp, err := s.send(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errorFrom(resp)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return &Error{Status: resp.StatusCode, Code: "unavailable", Message: "builder is unavailable", Err: err}
	}
	if int64(len(body)) > max {
		return &Error{Status: resp.StatusCode, Code: "bad_response", Message: "builder response is too large"}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Status: resp.StatusCode, Code: "bad_response", Message: "unexpected response from the builder", Err: err}
	}
	return nil
}

func (s *httpSandbox) postJSON(ctx context.Context, sessionID, suffix string, in any, max int64, out any) error {
	if err := checkSession(sessionID); err != nil {
		return err
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return &Error{Code: "invalid_request", Message: "could not encode the builder request", Err: err}
	}
	req, err := s.newRequest(ctx, http.MethodPost, sessionID, sessionPath(sessionID, suffix), "", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.doJSON(req, max, out)
}

// ---- Sandbox ----

func (s *httpSandbox) Init(ctx context.Context, sessionID string, tmpl TemplateSource) (InitResult, error) {
	if err := checkSession(sessionID); err != nil {
		return InitResult{}, err
	}
	var (
		query string
		body  io.Reader
	)
	switch {
	case tmpl.Name != "" && tmpl.Tar != nil, tmpl.Name == "" && tmpl.Tar == nil:
		return InitResult{}, &Error{Status: http.StatusBadRequest, Code: "invalid_argument", Message: "set exactly one of template name or tar"}
	case tmpl.Name != "":
		if !templatePattern.MatchString(tmpl.Name) {
			return InitResult{}, &Error{Status: http.StatusUnprocessableEntity, Code: "invalid_template", Message: "template name must match [a-z0-9-]+"}
		}
		query = "template=" + url.QueryEscape(tmpl.Name)
	default:
		body = tmpl.Tar
	}
	req, err := s.newRequest(ctx, http.MethodPost, sessionID, sessionPath(sessionID, "/init"), query, body)
	if err != nil {
		return InitResult{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-tar")
	}
	var resp builder.InitResponse
	if err := s.doJSON(req, maxBundleResponse, &resp); err != nil {
		return InitResult{}, err
	}
	bundle, err := decodeBundle(resp.Bundle)
	if err != nil {
		return InitResult{}, err
	}
	return InitResult{SHA: resp.SHA, Bundle: bundle, TemplateVersion: resp.TemplateVersion}, nil
}

func (s *httpSandbox) Restore(ctx context.Context, sessionID string, bundles []Bundle) error {
	req := builder.RestoreRequest{Bundles: make([]builder.RestoreBundle, len(bundles))}
	for i, b := range bundles {
		req.Bundles[i] = builder.RestoreBundle{Seq: b.Seq, SHA: b.SHA, Data: b.Data}
	}
	var resp builder.RestoreResponse
	return s.postJSON(ctx, sessionID, "/restore", req, maxSmallResponse, &resp)
}

func (s *httpSandbox) Op(ctx context.Context, sessionID, opID string, op FileOp) (OpResult, error) {
	req := builder.OpsRequest{
		OpID: opID,
		Kind: op.Kind,
		Args: builder.OpsArgs{
			Path:       op.Path,
			Offset:     op.Offset,
			Limit:      op.Limit,
			Content:    op.Content,
			OldString:  op.OldString,
			NewString:  op.NewString,
			ReplaceAll: op.ReplaceAll,
		},
	}
	var resp builder.OpsResponse
	if err := s.postJSON(ctx, sessionID, "/ops", req, maxSmallResponse, &resp); err != nil {
		return OpResult{}, err
	}
	return OpResult{OK: resp.OK, Output: resp.Output, Replayed: resp.Replayed}, nil
}

func (s *httpSandbox) Build(ctx context.Context, sessionID, opID string, step BuildStep, previewBase string) (BuildResult, error) {
	req := builder.BuildRequest{OpID: opID, Step: string(step), PreviewBase: previewBase}
	var resp builder.BuildResponse
	if err := s.postJSON(ctx, sessionID, "/build", req, maxSmallResponse, &resp); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		ExitCode:   resp.ExitCode,
		OutputTail: resp.OutputTail,
		DurationMs: resp.DurationMs,
		TimedOut:   resp.TimedOut,
		Replayed:   resp.Replayed,
	}, nil
}

func (s *httpSandbox) BuildLog(ctx context.Context, sessionID string, step BuildStep, tail int) (string, error) {
	if err := checkSession(sessionID); err != nil {
		return "", err
	}
	q := url.Values{"step": {string(step)}}
	if tail > 0 {
		q.Set("tail", strconv.Itoa(tail))
	}
	req, err := s.newRequest(ctx, http.MethodGet, sessionID, sessionPath(sessionID, "/build-log"), q.Encode(), nil)
	if err != nil {
		return "", err
	}
	var resp builder.BuildLogResponse
	if err := s.doJSON(req, maxSmallResponse, &resp); err != nil {
		return "", err
	}
	return resp.Output, nil
}

func (s *httpSandbox) Commit(ctx context.Context, sessionID string, turnSeq int, message string) (CommitResult, error) {
	var resp builder.CommitResponse
	if err := s.postJSON(ctx, sessionID, "/commit", builder.CommitRequest{TurnSeq: turnSeq, Message: message}, maxBundleResponse, &resp); err != nil {
		return CommitResult{}, err
	}
	return commitResult(resp)
}

func (s *httpSandbox) Revert(ctx context.Context, sessionID, toSHA string, turnSeq int) (CommitResult, error) {
	var resp builder.CommitResponse
	if err := s.postJSON(ctx, sessionID, "/revert", builder.RevertRequest{ToSHA: toSHA, TurnSeq: turnSeq}, maxBundleResponse, &resp); err != nil {
		return CommitResult{}, err
	}
	return commitResult(resp)
}

func commitResult(resp builder.CommitResponse) (CommitResult, error) {
	bundle, err := decodeBundle(resp.Bundle)
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{SHA: resp.SHA, Prev: resp.Prev, Changed: resp.Changed, Bundle: bundle}, nil
}

func decodeBundle(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, &Error{Code: "bad_response", Message: "unexpected response from the builder", Err: err}
	}
	return data, nil
}

func (s *httpSandbox) Archive(ctx context.Context, sessionID, sha string) (io.ReadCloser, error) {
	if err := checkSession(sessionID); err != nil {
		return nil, err
	}
	q := ""
	if sha != "" {
		q = url.Values{"sha": {sha}}.Encode()
	}
	req, err := s.newRequest(ctx, http.MethodGet, sessionID, sessionPath(sessionID, "/archive"), q, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.send(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errorFrom(resp)
	}
	return resp.Body, nil
}

func (s *httpSandbox) Destroy(ctx context.Context, sessionID string) error {
	if err := checkSession(sessionID); err != nil {
		return err
	}
	req, err := s.newRequest(ctx, http.MethodDelete, sessionID, sessionPath(sessionID, ""), "", nil)
	if err != nil {
		return err
	}
	resp, err := s.send(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return errorFrom(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (s *httpSandbox) Health(ctx context.Context) (Health, error) {
	req, err := s.newRequest(ctx, http.MethodGet, builder.HealthSessionID, "/v1/health", "", nil)
	if err != nil {
		return Health{}, err
	}
	var resp builder.HealthResponse
	if err := s.doJSON(req, maxSmallResponse, &resp); err != nil {
		return Health{}, err
	}
	return Health{
		Active:                 resp.Active,
		Capacity:               resp.Capacity,
		Version:                resp.Version,
		DefaultTemplateVersion: resp.DefaultTemplateVersion,
	}, nil
}

// Preview forwards GET/HEAD requests to the builder's preview of the session.
// The outbound request carries only a fresh token: the caller's cookies,
// Authorization header and query string (which holds the preview token) are
// dropped, so a user credential never reaches the builder.
func (s *httpSandbox) Preview(sessionID string) http.Handler {
	if checkSession(sessionID) != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
	}
	prefix := sessionPath(sessionID, "/preview")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			tok, err := s.signer.SignRequest(sessionID, pr.In.Method, prefix)
			if err != nil {
				// Unreachable: sessionID and method are non-empty. Send no
				// token and let the builder answer 401.
				tok = ""
			}
			out := pr.Out
			out.URL.Scheme = s.base.Scheme
			out.URL.Host = s.base.Host
			out.URL.Path = s.base.Path + prefix + "/" + strings.TrimLeft(pr.In.URL.Path, "/")
			out.URL.RawPath = ""
			out.URL.RawQuery = ""
			out.Host = s.base.Host
			out.Header.Del("Cookie")
			out.Header.Del("Authorization")
			out.Header.Del("Proxy-Authorization")
			out.Header.Del("X-Forwarded-For")
			out.Header.Set(builder.TokenHeader, tok)
		},
		Transport: s.client.Transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "preview is unavailable", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if badPreviewPath(r.URL.Path) {
			http.Error(w, "invalid preview path", http.StatusBadRequest)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// badPreviewPath rejects what the builder would reject anyway, before spending
// a request: dot segments, empty segments, NUL and backslash.
func badPreviewPath(p string) bool {
	if strings.ContainsAny(p, "\x00\\") {
		return true
	}
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, seg := range segs {
		if seg == "." || seg == ".." || (seg == "" && len(segs) > 1 && i != len(segs)-1) {
			return true
		}
	}
	return false
}

var _ Sandbox = (*httpSandbox)(nil)

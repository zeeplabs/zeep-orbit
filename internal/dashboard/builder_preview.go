package dashboard

// builder_preview.go — the sandboxed preview of a builder session
// (ai-frontend-builder T29; AFB-08, AFB-13, design §10). Two routes, methods of
// *BuilderHandler:
//
//	POST /api/builder/sessions/{id}/preview-token   MintPreviewToken  owner, RequireAuth
//	GET  /api/builder/sessions/{id}/preview/*       PreviewProxy      token-gated, NO RequireAuth
//
// Why the proxy is not behind RequireAuth: the generated app runs in an iframe
// with sandbox="allow-scripts ..." and WITHOUT allow-same-origin, so it has an
// opaque origin and the browser does not send the dashboard cookie (SameSite
// Strict, Path=/dashboard) with its requests. The proxy therefore authenticates
// with a short-lived signed preview token (builder.Signer.SignPreview: HMAC,
// 10 minutes, bound to session id + user id) instead.
//
// The token travels in the URL PATH, not in a cookie or the query string:
//
//	/dashboard/api/builder/sessions/{id}/p/{token}/...
//
// Cookies are not reliable from a sandboxed iframe (third-party cookie
// blocking, Secure on plain http), and a query string is not inherited by the
// page's sub-resources. The preview is built with a RELATIVE vite base ("./"),
// so ./assets/x.js resolves against the document URL and inherits the token
// segment. The document URL must therefore end with "/": /p/{token} is a 308
// redirect to /p/{token}/. Because the token is in the path, request logs must
// redact it (RedactPreviewTokenPath) and the proxy sends no Referer (no-referrer).
//
// What the proxy guarantees:
//   - no valid token for THIS session and ITS current owner: 401, nothing is
//     forwarded; the token of another user, another session, an expired or
//     forged one all look the same;
//   - only GET/HEAD; path traversal (dot segments, backslash, NUL) is a 400;
//   - the token, the dashboard cookie and any Authorization header are never
//     forwarded upstream (the sandbox client also drops them);
//   - every response, error or not, carries `Content-Security-Policy: sandbox
//     allow-scripts allow-forms allow-popups allow-modals` (opaque origin even
//     when opened top-level) and `X-Frame-Options: SAMEORIGIN` (overriding
//     SecurityHeaders' DENY and whatever the builder said), plus nosniff,
//     no-referrer and no-store; upstream Set-Cookie is dropped.
//
// Nothing secret is reachable from the preview: the builder holds only its
// shared secret, the preview is static files of the last successful build, and
// the app talks to the backend with the user's own Orbit credentials like any
// other frontend.

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/builder"
)

const (
	// previewCSP puts every preview response in an opaque origin.
	previewCSP = "sandbox allow-scripts allow-forms allow-popups allow-modals"
)

// PreviewTokenSigner mints and verifies preview tokens. *builder.Signer is the
// production implementation (BUILDER_SHARED_SECRET).
type PreviewTokenSigner interface {
	SignPreview(sessionID, userID string) (string, error)
	VerifyPreview(token string) (builder.PreviewClaims, error)
}

var _ PreviewTokenSigner = (*builder.Signer)(nil)

// BuilderPreviewToken is the body of the preview-token route.
type BuilderPreviewToken struct {
	// Token is the signed preview token; keep it out of logs and of the page URL
	// the user sees.
	Token string `json:"token"`
	// ExpiresAt is when the token stops working; reload the iframe with a new
	// token before then.
	ExpiresAt time.Time `json:"expires_at"`
	// URL is the iframe src: a same-origin absolute path that ends with
	// "/p/<token>/".
	URL string `json:"url"`
}

// MintPreviewToken handles POST /dashboard/api/builder/sessions/{id}/preview-token
// for the session owner (cookie-authenticated like every other builder route).
func (h *BuilderHandler) MintPreviewToken(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	if h.previewSigner == nil {
		h.writeError(w, http.StatusServiceUnavailable, builderNotConfiguredMsg)
		return
	}
	token, err := h.previewSigner.SignPreview(sess.ID, user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, BuilderPreviewToken{
		Token:     token,
		ExpiresAt: h.now().Add(builder.PreviewTokenTTL).UTC(),
		URL:       "/dashboard/api/builder/sessions/" + sess.ID + "/p/" + url.PathEscape(token) + "/",
	})
}

// PreviewProxy handles GET /dashboard/api/builder/sessions/{id}/p/{token}/*. It
// must be mounted OUTSIDE RequireAuth (see the file comment).
func (h *BuilderHandler) PreviewProxy(w http.ResponseWriter, r *http.Request) {
	pw := &previewWriter{ResponseWriter: w}
	w = pw

	if !h.configured() || h.previewSigner == nil {
		h.writeError(w, http.StatusServiceUnavailable, builderNotConfiguredMsg)
		return
	}
	id := chi.URLParam(r, "id")
	if !looksLikeUUID(id) {
		h.writeError(w, http.StatusNotFound, builderSessionNotFoundMsg)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !h.previewAuthorized(r.Context(), w, r, id, chi.URLParam(r, "token")) {
		return
	}

	wild := chi.URLParam(r, "*")
	if wild == "" && !strings.HasSuffix(r.URL.Path, "/") {
		// Relative asset URLs need the document URL to end with "/".
		http.Redirect(w, r, r.URL.EscapedPath()+"/", http.StatusPermanentRedirect)
		return
	}

	rest, ok := cleanPreviewPath(wild)
	if !ok {
		h.writeError(w, http.StatusBadRequest, "invalid preview path")
		return
	}

	// A fresh request: only the method, a clean path and the context go
	// upstream. No token (it is in the incoming path only), no query, no
	// Cookie, no Authorization.
	up := r.Clone(r.Context())
	up.URL = &url.URL{Path: rest}
	up.RequestURI = ""
	up.Header = http.Header{}
	h.sandbox.Preview(id).ServeHTTP(w, up)
}

// RedactPreviewTokenPath replaces the token segment of a preview path
// (.../sessions/{id}/p/{token}/...) with "***" so it never reaches a log. Any
// other path is returned unchanged.
func RedactPreviewTokenPath(path string) string {
	if !strings.HasPrefix(path, "/dashboard/api/builder/sessions/") {
		return path
	}
	// ["", dashboard, api, builder, sessions, {id}, p, {token}, ...]
	parts := strings.Split(path, "/")
	if len(parts) < 8 || parts[6] != "p" {
		return path
	}
	parts[7] = "***"
	return strings.Join(parts, "/")
}

// previewAuthorized verifies token for session id and answers 401/500 itself
// when it is not acceptable. Every rejection is the same 401 so a caller learns
// nothing about which check failed.
func (h *BuilderHandler) previewAuthorized(ctx context.Context, w http.ResponseWriter, r *http.Request, id, token string) bool {
	deny := func() bool {
		h.writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if token == "" {
		return deny()
	}
	claims, err := h.previewSigner.VerifyPreview(token)
	if err != nil {
		return deny()
	}
	// The handler's clock is the authority on expiry (the signer has its own).
	if h.now().Unix() > claims.ExpiresAt {
		return deny()
	}
	if subtle.ConstantTimeCompare([]byte(claims.SessionID), []byte(id)) != 1 {
		return deny()
	}
	// The token proves who the owner WAS when it was minted; confirm it still
	// is, and that the session is still live.
	sess, err := h.store.GetSessionByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return deny()
		}
		h.serverError(w, r, err)
		return false
	}
	if sess.Status == store.SessionArchived || subtle.ConstantTimeCompare([]byte(sess.OwnerUserID), []byte(claims.UserID)) != 1 {
		return deny()
	}
	return true
}

// cleanPreviewPath turns the wildcard of the route into the path inside the
// preview ("/" + rest). It rejects anything that could climb out of the preview
// root or confuse a downstream router: dot segments (also percent-encoded),
// backslashes, NUL and control characters.
func cleanPreviewPath(wild string) (string, bool) {
	dec, err := url.PathUnescape(wild)
	if err != nil {
		return "", false
	}
	for _, s := range []string{wild, dec} {
		if strings.ContainsAny(s, "\x00\\") {
			return "", false
		}
		for _, c := range s {
			if c < 0x20 || c == 0x7f {
				return "", false
			}
		}
		for _, seg := range strings.Split(s, "/") {
			if seg == "." || seg == ".." {
				return "", false
			}
		}
	}
	dec = strings.TrimLeft(dec, "/")
	if strings.Contains(dec, "//") {
		return "", false
	}
	return "/" + dec, true
}

// previewWriter enforces the preview response headers at WriteHeader time, after
// the upstream response headers were copied in, so that nothing the builder (or
// an earlier middleware) set can weaken them.
type previewWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (p *previewWriter) WriteHeader(status int) {
	if !p.wroteHeader {
		p.wroteHeader = true
		h := p.Header()
		h.Set("Content-Security-Policy", previewCSP)
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		// Never let upstream set cookies on the Orbit origin.
		h.Del("Set-Cookie")
		// The page runs in an opaque origin (Origin: null) and Vite marks its
		// module script and stylesheet `crossorigin`, so the browser makes CORS
		// requests and blocks them without this header. A wildcard (never with
		// credentials) is enough: the files are static build output and the token
		// in the URL path is the only credential.
		h.Set("Access-Control-Allow-Origin", "*")
		h.Del("Access-Control-Allow-Credentials")
	}
	p.ResponseWriter.WriteHeader(status)
}

func (p *previewWriter) Write(b []byte) (int, error) {
	if !p.wroteHeader {
		p.WriteHeader(http.StatusOK)
	}
	return p.ResponseWriter.Write(b)
}

// Flush keeps streaming responses working.
func (p *previewWriter) Flush() {
	if !p.wroteHeader {
		p.WriteHeader(http.StatusOK)
	}
	if f, ok := p.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer.
func (p *previewWriter) Unwrap() http.ResponseWriter { return p.ResponseWriter }

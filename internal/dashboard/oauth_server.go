// Package dashboard: OAuthHandler implements the OAuth 2.1
// authorization-code-with-PKCE front door onto the same dashboard_pats
// store PATStore uses — a second token-issuance path, not a second auth
// system (design.md: OAuthServer component). This file starts with the two
// unauthenticated, low-risk endpoints (T17): metadata discovery and dynamic
// client registration. Authorize (T18/T19) and Token (T20) land in later
// tasks.
package dashboard

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// OAuthHandler holds the OAuth-facing HTTP handlers.
type OAuthHandler struct {
	pool *db.Pool
}

// NewOAuthHandler creates a new OAuthHandler.
func NewOAuthHandler(pool *db.Pool) *OAuthHandler {
	return &OAuthHandler{pool: pool}
}

// requestBaseURL derives the externally-visible scheme+host for building
// absolute endpoint URLs in the metadata document. When ORBIT_PUBLIC_URL is
// set, it's used verbatim — this is the safe path, since it comes from
// trusted deployment config rather than the request itself. Without it,
// this falls back to trusting r.Host/X-Forwarded-Proto (reverse-proxy-aware,
// since this service runs behind a load balancer and never terminates TLS
// itself — AGENTS.md), which only an operator without a validating proxy in
// front of Orbit needs to worry about: those headers are otherwise
// attacker-controllable, and this document only affects where an MCP client
// discovers the token/authorize endpoints to be — it never bypasses the
// redirect_uri/PKCE/client_id checks a token exchange itself performs.
func requestBaseURL(r *http.Request) string {
	if configured := strings.TrimSuffix(os.Getenv("ORBIT_PUBLIC_URL"), "/"); configured != "" {
		return configured
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

// oauthMetadataResponse is the subset of RFC 8414's authorization server
// metadata document MCP clients need to discover this server's endpoints.
type oauthMetadataResponse struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// GetMetadata handles GET /.well-known/oauth-authorization-server —
// unauthenticated discovery document pointing at the authorize, token, and
// registration endpoints (spec MCP-19).
func (h *OAuthHandler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	base := requestBaseURL(r)
	resp := oauthMetadataResponse{
		Issuer:                            base,
		AuthorizationEndpoint:             base + "/dashboard/oauth/authorize",
		TokenEndpoint:                     base + "/dashboard/oauth/token",
		RegistrationEndpoint:              base + "/dashboard/oauth/register",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	}
	writeJSON(w, http.StatusOK, resp)
}

// registerClientRequest is POST /dashboard/oauth/register's JSON body
// (RFC 7591-style dynamic client registration — the fields an MCP client
// like Claude Desktop actually sends).
type registerClientRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// registerClientResponse is RFC 7591's minimal successful-registration
// response shape.
type registerClientResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// RegisterClient handles POST /dashboard/oauth/register — dynamic client
// registration, no prior manual setup required (spec MCP-20). Unauthenticated
// by nature (a client has no credentials yet); rate-limited per-IP at the
// route layer (server.go), not here, since registration alone grants no
// data access.
func (h *OAuthHandler) RegisterClient(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body registerClientRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	client, err := RegisterClient(r.Context(), h.pool, RegisterClientInput{
		Name:         body.ClientName,
		RedirectURIs: body.RedirectURIs,
	})
	if err != nil {
		var valErr *ValidationError
		if errors.As(err, &valErr) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": valErr.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusCreated, registerClientResponse{
		ClientID:                client.ID,
		ClientName:              client.Name,
		RedirectURIs:            client.RedirectURIs,
		TokenEndpointAuthMethod: "none",
	})
}

// writeOAuthError writes a standard OAuth-shaped {"error": "..."} JSON body
// (design.md Error Handling Strategy: invalid_client/invalid_request), with
// no redirect performed — redirecting to an unregistered/mismatched
// redirect_uri would itself be an open-redirect risk, so these two
// validation failures are surfaced directly instead of bounced through the
// client's own callback.
func writeOAuthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// Authorize handles GET /dashboard/oauth/authorize (spec MCP-21/MCP-22,
// P1-OAuth AC3/AC4). Validates client_id and redirect_uri before anything
// else — an unknown client or a redirect_uri the client never registered
// is rejected with a JSON error and no redirect at all (never handed to
// the browser as a Location header), exactly the same rule GetClient/
// RegisterClient's design intends. Once validated: no active zeep_session
// redirects to the existing login page (return_to preserves every OAuth
// param so the flow can resume); an active session hands off to the
// consent screen (T19's OAuthConsent.tsx, mounted as a Dashboard UI route)
// by redirecting there with the same query string — this task stops here,
// before any code is ever issued.
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	if clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client")
		return
	}
	client, err := GetClient(r.Context(), h.pool, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client")
		return
	}

	if redirectURI == "" || !oauthRedirectURIRegistered(client, redirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if responseType != "code" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	cookie, cookieErr := r.Cookie(cookieName)
	var user *DashboardUser
	if cookieErr == nil {
		user, err = GetSessionUser(r.Context(), h.pool, cookie.Value)
	}
	if cookieErr != nil || err != nil || user == nil {
		loginURL := "/dashboard/login?return_to=" + url.QueryEscape(r.URL.RequestURI())
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	// client_name travels in the handoff so OAuthConsent.tsx can name the
	// requesting client without a second round-trip — the frontend already
	// has redirect_uri from this same query string, so it derives the
	// redirect origin locally (design.md Risks & Concerns: "display the
	// redirect URI's origin alongside the client's self-declared name").
	consentQuery := q
	consentQuery.Set("client_name", client.Name)
	consentURL := "/dashboard/oauth/consent?" + consentQuery.Encode()
	http.Redirect(w, r, consentURL, http.StatusFound)
}

// decideRequest is POST /dashboard/oauth/authorize's JSON body — submitted
// by OAuthConsent.tsx (T19) after the admin grants or denies. Session-
// authenticated (RequireAuth, mounted in server.go), so Decide always knows
// which DashboardUser is granting consent without trusting a client-
// supplied identity.
type decideRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Decision            string `json:"decision"` // "grant" | "deny"
}

// decideResponse carries the URL the frontend must navigate the browser to
// next — a fetch/XHR POST can't perform a cross-origin browser redirect
// itself, so Decide returns the target instead of an HTTP 302.
type decideResponse struct {
	RedirectURL string `json:"redirect_url"`
}

// oauthCallbackURL appends code/error + state (if present) to redirectURI,
// the same way any OAuth authorization endpoint reports the outcome back
// to the client's own redirect handler.
func oauthCallbackURL(redirectURI, param, value, state string) string {
	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	callback := redirectURI + sep + param + "=" + url.QueryEscape(value)
	if state != "" {
		callback += "&state=" + url.QueryEscape(state)
	}
	return callback
}

// Decide handles POST /dashboard/oauth/authorize (spec MCP-21/MCP-22,
// P1-OAuth AC4/AC7; design.md Error Handling Strategy consent-denial and
// code-issuance rows) — the grant/deny branches T19 adds onto Authorize.
// Re-validates client_id/redirect_uri exactly as the GET branch (T18) does
// (defense in depth: the consent screen's own submission is still an
// untrusted client-side request). On "deny", redirects back with
// error=access_denied and issues no code. On "grant", issues a single-use
// PKCE-bound code (CreateAuthCode, T18) and redirects back with it.
func (h *OAuthHandler) Decide(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body decideRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	if body.ClientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client")
		return
	}
	client, err := GetClient(r.Context(), h.pool, body.ClientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client")
		return
	}
	if body.RedirectURI == "" || !oauthRedirectURIRegistered(client, body.RedirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	switch body.Decision {
	case "deny":
		writeJSON(w, http.StatusOK, decideResponse{
			RedirectURL: oauthCallbackURL(body.RedirectURI, "error", "access_denied", body.State),
		})
	case "grant":
		if body.CodeChallenge == "" || body.CodeChallengeMethod != "S256" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		code, _, err := CreateAuthCode(r.Context(), h.pool, client.ID, user.ID, body.CodeChallenge, body.RedirectURI)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, decideResponse{
			RedirectURL: oauthCallbackURL(body.RedirectURI, "code", code, body.State),
		})
	default:
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
	}
}

// tokenResponse is OAuth 2.1's standard successful token response shape.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// pkceVerify implements RFC 7636's S256 check: codeChallenge must equal
// BASE64URL(SHA256(codeVerifier)) — the only method this server advertises
// (metadata's code_challenge_methods_supported, GetMetadata above).
func pkceVerify(codeChallenge, codeVerifier string) bool {
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == codeChallenge
}

// Token handles POST /dashboard/oauth/token (spec MCP-22/MCP-23/MCP-24).
// Standard OAuth form-encoded body (application/x-www-form-urlencoded),
// per RFC 6749 — the content type every real OAuth client (MCP or
// otherwise) sends to a token endpoint. Dispatches on grant_type;
// unsupported values are rejected without touching any store.
func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	switch r.FormValue("grant_type") {
	case "authorization_code":
		h.tokenAuthorizationCode(w, r)
	case "refresh_token":
		h.tokenRefresh(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

// tokenAuthorizationCode implements the authorization_code grant: consumes
// the presented code exactly once (ConsumeAuthCode, T18 — rejects
// unknown/already-used/expired codes as invalid_grant, no token issued),
// checks the PKCE verifier against the code's stored challenge, and mints
// a fresh access+refresh token pair resolvable through the same
// ResolvePAT path a manually-created PAT uses (spec MCP-23). The code is
// consumed before PKCE is checked — its single-use guarantee holds
// regardless of whether the exchange ultimately succeeds, so a verifier
// mismatch can't be retried against the same code (matches "exchangeable
// exactly once" from spec.md P1-OAuth AC4).
func (h *OAuthHandler) tokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	verifier := r.FormValue("code_verifier")
	if code == "" || verifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	authCode, err := ConsumeAuthCode(r.Context(), h.pool, code)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// RFC 6749 §4.1.3: a public client (no client_secret, our only kind —
	// see RegisterClient) authenticates itself at token exchange by
	// asserting client_id, which must match the client the code was issued
	// to. Checked after ConsumeAuthCode so a mismatch still burns the
	// code's single use, same as every other invalid_grant path here.
	if clientID := r.FormValue("client_id"); clientID == "" || clientID != authCode.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if redirectURI := r.FormValue("redirect_uri"); redirectURI != "" && redirectURI != authCode.RedirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if !pkceVerify(authCode.CodeChallenge, verifier) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	accessToken, row, err := CreateOAuthAccessToken(r.Context(), h.pool, authCode.UserID, authCode.ClientID, "", time.Now().Add(oauthAccessTokenTTL))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	refreshToken, err := SetRefreshToken(r.Context(), h.pool, row.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(oauthAccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
	})
}

// tokenRefresh implements the refresh_token grant: rotates the presented
// refresh token via RotateOAuthRefreshToken (T20's reuse-detection +
// rotation primitive), returning a fresh access+refresh pair on success or
// invalid_grant on any failure — including reuse of an already-rotated
// token, which additionally revokes the whole token family as a side
// effect of RotateOAuthRefreshToken itself (spec P1-OAuth AC7, design.md
// Error Handling Strategy).
func (h *OAuthHandler) tokenRefresh(w http.ResponseWriter, r *http.Request) {
	presented := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")
	if presented == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	accessToken, refreshToken, _, err := RotateOAuthRefreshToken(r.Context(), h.pool, presented, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(oauthAccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
	})
}

// oauthRedirectURIRegistered reports whether redirectURI exactly matches
// one of client's registered redirect URIs — an exact-match requirement
// (not prefix/host matching), per standard OAuth practice and this design's
// own "must match exactly at token exchange" note for the same field.
func oauthRedirectURIRegistered(client OAuthClient, redirectURI string) bool {
	for _, registered := range client.RedirectURIs {
		if registered == redirectURI {
			return true
		}
	}
	return false
}

// Package dashboard: OAuthHandler implements the OAuth 2.1
// authorization-code-with-PKCE front door onto the same dashboard_pats
// store PATStore uses — a second token-issuance path, not a second auth
// system (design.md: OAuthServer component). This file starts with the two
// unauthenticated, low-risk endpoints (T17): metadata discovery and dynamic
// client registration. Authorize (T18/T19) and Token (T20) land in later
// tasks.
package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"

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
// absolute endpoint URLs in the metadata document — reverse-proxy-aware
// (X-Forwarded-Proto), since this service runs behind a load balancer
// (AGENTS.md) and never terminates TLS itself.
func requestBaseURL(r *http.Request) string {
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

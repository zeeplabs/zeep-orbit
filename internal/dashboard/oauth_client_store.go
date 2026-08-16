package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ErrOAuthClientNotFound is returned when a client_id matches no registered
// OAuth client.
var ErrOAuthClientNotFound = errors.New("dashboard: oauth client not found")

// OAuthClient is a row from zeep_system.oauth_clients — a dynamically
// self-registered MCP client (design.md: OAuthClientStore).
type OAuthClient struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

// RegisterClientInput is RegisterClient's input — Name is shown verbatim on
// the consent screen (T19), RedirectURIs are the only URIs Authorize (T18)
// will ever redirect an issued code to for this client.
type RegisterClientInput struct {
	Name         string
	RedirectURIs []string
}

// RegisterClient issues a random client_id (via generateToken — same
// entropy source PATs use) and stores the client's declared name and
// redirect URIs. No client_secret: this is a PKCE-only public client per
// design.md's Tech Decisions (native/desktop MCP clients can't keep a
// secret confidential). No prior manual setup required — any caller may
// register (mitigated by rate limiting at the HTTP layer, not here).
func RegisterClient(ctx context.Context, pool *db.Pool, input RegisterClientInput) (OAuthClient, error) {
	if input.Name == "" {
		return OAuthClient{}, &ValidationError{msg: "name is required"}
	}
	if len(input.RedirectURIs) == 0 {
		return OAuthClient{}, &ValidationError{msg: "redirect_uris is required"}
	}
	for _, redirectURI := range input.RedirectURIs {
		if !isAllowedOAuthRedirectURI(redirectURI) {
			return OAuthClient{}, &ValidationError{msg: fmt.Sprintf("redirect_uris: %q must be an https:// URL, or an http:// URL on localhost/127.0.0.1 (loopback clients only)", redirectURI)}
		}
	}

	clientID, err := generateToken()
	if err != nil {
		return OAuthClient{}, fmt.Errorf("dashboard: generate oauth client_id: %w", err)
	}

	redirectJSON, err := json.Marshal(input.RedirectURIs)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("dashboard: marshal redirect_uris: %w", err)
	}

	var row OAuthClient
	var storedRedirects []byte
	err = pool.QueryRow(ctx,
		`INSERT INTO zeep_system.oauth_clients (id, name, redirect_uris)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, redirect_uris, created_at`,
		clientID, input.Name, redirectJSON,
	).Scan(&row.ID, &row.Name, &storedRedirects, &row.CreatedAt)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("dashboard: register oauth client: %w", err)
	}
	if err := json.Unmarshal(storedRedirects, &row.RedirectURIs); err != nil {
		return OAuthClient{}, fmt.Errorf("dashboard: unmarshal redirect_uris: %w", err)
	}
	return row, nil
}

// isAllowedOAuthRedirectURI rejects any scheme other than https, plus a
// narrow http exception for loopback addresses (native/CLI clients binding
// a local callback server per RFC 8252 — the same native-client case
// design.md's Tech Decisions cites for going PKCE-only). Without this, a
// registered redirect_uri like "javascript:..." would be handed straight to
// window.location.href by OAuthConsent.tsx after a user grants or denies
// consent, running attacker script on the dashboard origin with the admin's
// live session.
func isAllowedOAuthRedirectURI(redirectURI string) bool {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		return u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	default:
		return false
	}
}

// GetClient resolves a client_id to its registered OAuthClient — used by
// Authorize (T18) to validate a redirect_uri actually belongs to the
// requesting client before issuing anything.
func GetClient(ctx context.Context, pool *db.Pool, clientID string) (OAuthClient, error) {
	var row OAuthClient
	var storedRedirects []byte
	err := pool.QueryRow(ctx,
		`SELECT id, name, redirect_uris, created_at FROM zeep_system.oauth_clients WHERE id = $1`,
		clientID,
	).Scan(&row.ID, &row.Name, &storedRedirects, &row.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OAuthClient{}, ErrOAuthClientNotFound
		}
		return OAuthClient{}, fmt.Errorf("dashboard: get oauth client: %w", err)
	}
	if err := json.Unmarshal(storedRedirects, &row.RedirectURIs); err != nil {
		return OAuthClient{}, fmt.Errorf("dashboard: unmarshal redirect_uris: %w", err)
	}
	return row, nil
}

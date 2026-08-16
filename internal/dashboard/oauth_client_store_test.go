package dashboard

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func oauthClientTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.oauth_clients CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	return pool
}

// TestRegisterClient_IssuesClientIDWithoutPriorSetup covers T17's Done-when:
// registering with a name and redirect URI returns a client_id with no
// prior manual setup (spec MCP-20).
func TestRegisterClient_IssuesClientIDWithoutPriorSetup(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	client, err := RegisterClient(ctx, pool, RegisterClientInput{
		Name:         "Claude Desktop",
		RedirectURIs: []string{"https://claude.ai/api/mcp/oauth/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if client.ID == "" {
		t.Fatal("expected a non-empty client_id")
	}
	if client.Name != "Claude Desktop" {
		t.Fatalf("expected name %q, got %q", "Claude Desktop", client.Name)
	}
	if len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != "https://claude.ai/api/mcp/oauth/callback" {
		t.Fatalf("expected the redirect_uris to round-trip exactly, got %+v", client.RedirectURIs)
	}
}

// TestRegisterClient_RequiresNameAndRedirectURIs covers input validation:
// a missing name or empty redirect_uris list is rejected before any row is
// written.
func TestRegisterClient_RequiresNameAndRedirectURIs(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	var valErr *ValidationError

	if _, err := RegisterClient(ctx, pool, RegisterClientInput{RedirectURIs: []string{"https://example.com/cb"}}); !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for missing name, got %v", err)
	}
	if _, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "client"}); !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for empty redirect_uris, got %v", err)
	}
}

// TestRegisterClient_RejectsUnsafeRedirectSchemes guards against a stored-XSS
// path on the dashboard origin: OAuthConsent.tsx hands whatever redirect_uri
// a client registered straight to window.location.href once an admin grants
// or denies consent, so a scheme like "javascript:" registered here would
// execute in the admin's authenticated session.
func TestRegisterClient_RejectsUnsafeRedirectSchemes(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	unsafe := []string{
		"javascript:alert(document.cookie)",
		"data:text/html,<script>alert(1)</script>",
		"http://attacker.example.com/callback",
		"not a url",
		"",
	}
	for _, redirectURI := range unsafe {
		var valErr *ValidationError
		if _, err := RegisterClient(ctx, pool, RegisterClientInput{
			Name:         "client",
			RedirectURIs: []string{redirectURI},
		}); !errors.As(err, &valErr) {
			t.Fatalf("redirect_uri %q: expected *ValidationError, got %v", redirectURI, err)
		}
	}
}

// TestRegisterClient_AllowsLoopbackHTTP covers the RFC 8252 native-client
// exception: a plain http:// redirect_uri is only safe (and only useful) on
// loopback, where no network attacker can intercept it.
func TestRegisterClient_AllowsLoopbackHTTP(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	for _, redirectURI := range []string{
		"http://localhost:51000/callback",
		"http://127.0.0.1:51000/callback",
	} {
		if _, err := RegisterClient(ctx, pool, RegisterClientInput{
			Name:         "client",
			RedirectURIs: []string{redirectURI},
		}); err != nil {
			t.Fatalf("redirect_uri %q: expected no error, got %v", redirectURI, err)
		}
	}
}

func TestIsAllowedOAuthRedirectURI(t *testing.T) {
	cases := []struct {
		uri  string
		want bool
	}{
		{"https://claude.ai/callback", true},
		{"http://localhost:8080/callback", true},
		{"http://127.0.0.1/callback", true},
		{"http://[::1]/callback", true},
		{"http://evil.example.com/callback", false},
		{"javascript:alert(1)", false},
		{"data:text/html,x", false},
		{"", false},
		{"https://", false},
	}
	for _, c := range cases {
		if got := isAllowedOAuthRedirectURI(c.uri); got != c.want {
			t.Errorf("isAllowedOAuthRedirectURI(%q) = %v, want %v", c.uri, got, c.want)
		}
	}
}

// TestGetClient_ResolvesRegisteredClient covers GetClient's happy path —
// used by Authorize (T18) to validate a redirect_uri against what was
// actually registered.
func TestGetClient_ResolvesRegisteredClient(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	client, err := RegisterClient(ctx, pool, RegisterClientInput{
		Name:         "cursor",
		RedirectURIs: []string{"http://localhost:51000/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	got, err := GetClient(ctx, pool, client.ID)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if got.ID != client.ID || got.Name != client.Name {
		t.Fatalf("expected GetClient to resolve the registered client, got %+v", got)
	}
}

// TestGetClient_UnknownClientIDReturnsNotFound covers the not-found path
// Authorize (T18) relies on to reject an unregistered client_id with 400,
// no redirect.
func TestGetClient_UnknownClientIDReturnsNotFound(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	if _, err := GetClient(ctx, pool, "not-a-real-client-id"); !errors.Is(err, ErrOAuthClientNotFound) {
		t.Fatalf("expected ErrOAuthClientNotFound, got %v", err)
	}
}

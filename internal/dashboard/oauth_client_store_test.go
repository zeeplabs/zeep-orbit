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

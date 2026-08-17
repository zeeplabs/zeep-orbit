package dashboard

// app_tokens_foruser_test.go — mcp-read-only-tools T8: ListAppTokensForUser,
// the shared operation behind the ListAppTokens REST handler and
// orbit_list_app_tokens. Same pool-provisioning approach as
// table_policies_handler_test.go, trimmed to what app tokens need (an app,
// a non-owner member, no table fixtures).
//
// Skips if TEST_DATABASE_URL is not set.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func appTokensTestPool(t *testing.T) (*db.Pool, map[string]*DashboardUser, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("drop zeep_system: %v", err)
	}
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	actors := map[string]*DashboardUser{}
	for _, ad := range []struct{ key, role string }{
		{"owner", "member"},
		{"editor", "member"},
		{"outsider", "member"},
	} {
		email := fmt.Sprintf("apptok-%s-%d@example.com", ad.key, time.Now().UnixNano())
		u, err := CreateUser(ctx, pool, email, ad.key, "hash", ad.role)
		if err != nil {
			pool.Close()
			t.Fatalf("create user %s: %v", email, err)
		}
		actors[ad.key] = u
	}

	app, err := CreateApp(ctx, pool, fmt.Sprintf("apptokentest%d", time.Now().UnixNano()), actors["owner"].ID, false)
	if err != nil {
		pool.Close()
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := AddAppMember(ctx, pool, AppRef{BackendAppID: app.ID}, actors["editor"].ID, AppRoleEditor); err != nil {
		pool.Close()
		t.Fatalf("AddAppMember (editor): %v", err)
	}

	return pool, actors, app.ID
}

// TestListAppTokensForUser_HappyPath covers T8's Done-when: the same token
// metadata ListAppTokens itself returns, for a visible app.
func TestListAppTokensForUser_HappyPath(t *testing.T) {
	pool, actors, appID := appTokensTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	created, err := CreateAppToken(ctx, pool, CreateAppTokenInput{AppID: appID, Name: "ci token"})
	if err != nil {
		t.Fatalf("CreateAppToken: %v", err)
	}

	rows, err := ListAppTokensForUser(ctx, pool, actors["owner"], appID)
	if err != nil {
		t.Fatalf("ListAppTokensForUser: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != created.ID || rows[0].Name != "ci token" {
		t.Fatalf("expected 1 token matching %+v, got %+v", created, rows)
	}
}

// TestListAppTokensForUser_NoRoleTierRequired covers T8's Done-when:
// authorization here is visibility-only — an editor (whose role fails
// CanManage()) still succeeds, unlike table policies/members/webhooks. This
// is the "no explicit role tier beyond visibility" behavior the task
// explicitly calls out as intentional, not a bug to fix.
func TestListAppTokensForUser_NoRoleTierRequired(t *testing.T) {
	pool, actors, appID := appTokensTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	if _, err := CreateAppToken(ctx, pool, CreateAppTokenInput{AppID: appID, Name: "editor-visible-token"}); err != nil {
		t.Fatalf("CreateAppToken: %v", err)
	}

	rows, err := ListAppTokensForUser(ctx, pool, actors["editor"], appID)
	if err != nil {
		t.Fatalf("ListAppTokensForUser for editor: expected success (visibility-only tier), got error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 token visible to editor, got %+v", rows)
	}
}

// TestListAppTokensForUser_EmailAuthAppReturnsErrAppTokensNotSupported
// covers T8's Done-when: an app with AuthEmailEnabled == true returns the
// dedicated ErrAppTokensNotSupported sentinel, never [] and never a generic
// error — design.md's flagged risk (the business rule is easy to drop
// during extraction).
func TestListAppTokensForUser_EmailAuthAppReturnsErrAppTokensNotSupported(t *testing.T) {
	pool, actors, _ := appTokensTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	emailApp, err := CreateApp(ctx, pool, fmt.Sprintf("apptokenemailtest%d", time.Now().UnixNano()), actors["owner"].ID, true)
	if err != nil {
		t.Fatalf("CreateApp (email auth): %v", err)
	}

	_, err = ListAppTokensForUser(ctx, pool, actors["owner"], emailApp.ID)
	if !errors.Is(err, ErrAppTokensNotSupported) {
		t.Fatalf("expected ErrAppTokensNotSupported, got %v", err)
	}
}

// TestListAppTokensForUser_UnknownAppReturnsErrNotFound covers T8's
// Done-when: an outsider with no visibility (and not superadmin/
// CanReadAnyApp) gets the same not-found GetApp already returns.
func TestListAppTokensForUser_UnknownAppReturnsErrNotFound(t *testing.T) {
	pool, actors, appID := appTokensTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListAppTokensForUser(ctx, pool, actors["outsider"], appID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an outsider with no app visibility, got %v", err)
	}
}

// TestListAppTokensForUser_EmptyReturnsEmptyResultNeverError covers T8's
// Done-when (spec "Empty-result shape" assumption): a visible app with zero
// issued tokens returns a zero-length result without error.
func TestListAppTokensForUser_EmptyReturnsEmptyResultNeverError(t *testing.T) {
	pool, actors, appID := appTokensTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	rows, err := ListAppTokensForUser(ctx, pool, actors["owner"], appID)
	if err != nil {
		t.Fatalf("ListAppTokensForUser: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected zero tokens on a fresh app, got %+v", rows)
	}
}

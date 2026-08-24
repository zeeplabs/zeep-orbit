package dashboard

// apps_update_app_foruser_test.go — coverage for UpdateAppForUser (ai-edit-chat
// spec T3, AIEC-12/AIEC-13). Derived from tasks.md's T3 Done-when list, not
// from reading the implementation: success path updates auth_email_enabled
// and audits; CanManage() denial returns an authorization error before the
// store is touched; a store-level failure (unknown app) surfaces its own
// error, not a generic one.
//
// CanManage(), not CanWrite(): toggling auth_email_enabled can disable every
// end-user login on the app, so it follows UpdateApp's REST handler tier
// (admin-only), not the CanWrite() tier most edit-chat mutations use. Found
// by the pre-v1.6.0 Opus review (v1.5.0..HEAD, finding H2) — the MCP/chat
// path had been left at CanWrite(), letting a non-admin editor bypass a
// restriction the REST endpoint deliberately enforces.

import (
	"context"
	"errors"
	"testing"
)

// TestUpdateAppForUser_SuccessTogglesAuthEmailEnabled covers T3's success
// path: an actor with write access (admin/owner) can flip
// auth_email_enabled, and the returned row reflects the new value.
func TestUpdateAppForUser_SuccessTogglesAuthEmailEnabled(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// test-app's owner ("loner") has an admin app_members row, so
	// CanWrite() is true — the seeded apps row defaults auth_email_enabled
	// to true (provisioner.go DDL default), so toggling to false is a real
	// change to assert on.
	app, err := h.UpdateAppForUser(ctx, actors["loner"], appID, false, "127.0.0.1")
	if err != nil {
		t.Fatalf("UpdateAppForUser: %v", err)
	}
	if app.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled=false after update, got true")
	}

	// Toggle back to true and confirm the store round-trips both directions,
	// not just false.
	app, err = h.UpdateAppForUser(ctx, actors["loner"], appID, true, "127.0.0.1")
	if err != nil {
		t.Fatalf("UpdateAppForUser (toggle back): %v", err)
	}
	if !app.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled=true after second update, got false")
	}
}

// TestUpdateAppForUser_ViewerForbidden covers T3's RBAC-denied path: a
// viewer (CanManage()==false) is rejected with ErrForbidden before any store
// mutation runs.
func TestUpdateAppForUser_ViewerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.UpdateAppForUser(ctx, actors["appviewer"], appID, false, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a viewer, got %v", err)
	}

	// The app's auth_email_enabled must be unchanged (still the seeded
	// default, true) — a denied request must never reach the store.
	app, _, err := GetApp(ctx, pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if !app.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled to remain true after a denied update, got false")
	}
}

// TestUpdateAppForUser_EditorForbidden covers H2 specifically: an app_members
// "editor" has real CanWrite()==true (they can edit tables) but not
// CanManage() — the same actor UpdateApp's REST handler blocks from touching
// auth_providers/storage/rate_limit. Before this fix, UpdateAppForUser only
// checked CanWrite() and let this exact actor toggle auth_email_enabled via
// MCP/chat despite being 403'd on the equivalent REST call.
func TestUpdateAppForUser_EditorForbidden(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// appsHandlerTestPool already seeds "appeditor" as an "editor" app_member
	// on test-app (see apps_handler_test.go) — no extra setup needed.
	_, err := h.UpdateAppForUser(ctx, actors["appeditor"], appID, false, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanWrite but not CanManage), got %v", err)
	}

	app, _, err := GetApp(ctx, pool, appID, actors["loner"])
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if !app.AuthEmailEnabled {
		t.Fatalf("expected auth_email_enabled to remain true after a denied update, got false")
	}
}

// TestUpdateAppForUser_UnknownAppReturnsStoreError covers T3's store-error
// path: a nonexistent app ID surfaces the store's own not-found error
// instead of a generic internal failure.
func TestUpdateAppForUser_UnknownAppReturnsStoreError(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.UpdateAppForUser(ctx, actors["loner"], "00000000-0000-0000-0000-000000000000", false, "127.0.0.1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown app, got %v", err)
	}
}

package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// TestUpdateTableRLSModeForUser_AllValidValues covers T6's Done-when: all 4
// valid RLS values ("", "owner", "enabled", "policy") are accepted and
// persisted (mcp-server spec MCP-11), against a table on an app with email
// auth enabled (required for the owner-column RLS values per the
// RLS×authEmail rule exercised separately below).
func TestUpdateTableRLSModeForUser_AllValidValues(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// A table on an app with email auth enabled, so every RLS value
	// (including the owner-column ones) is valid for it.
	emailApp, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{
		Name:             "rls-mode-app",
		AuthEmailEnabled: true,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], emailApp.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	for _, rls := range []string{"", "owner", "enabled", "policy"} {
		row, err := h.UpdateTableRLSModeForUser(ctx, actors["loner"], emailApp.ID, table.Name, rls, "127.0.0.1")
		if err != nil {
			t.Fatalf("UpdateTableRLSModeForUser(%q): %v", rls, err)
		}
		if row.RLS != rls {
			t.Fatalf("expected rls %q, got %q", rls, row.RLS)
		}
	}
}

// TestUpdateTableRLSModeForUser_InvalidValueRejected covers T6's Done-when
// invalid-value rejection case.
func TestUpdateTableRLSModeForUser_InvalidValueRejected(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.UpdateTableRLSModeForUser(ctx, actors["loner"], appID, "test_table", "not-a-real-rls-value", "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *ValidationError for an invalid rls value, got %v (%T)", err, err)
	}
}

// TestUpdateTableRLSModeForUser_OwnerRLSWithoutEmailAuthRejected covers the
// RLS×authEmail invariant validateTableInput enforces: an owner-column RLS
// value on an app without email auth is rejected, not silently accepted
// (would be a guaranteed provisioning failure per handler.go's own comment
// on this rule).
func TestUpdateTableRLSModeForUser_OwnerRLSWithoutEmailAuthRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	noEmailApp, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{
		Name:             "no-email-auth-app",
		AuthEmailEnabled: false,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	table, err := h.CreateAppTableForUser(ctx, actors["loner"], noEmailApp.ID, TableRequestBody{
		Name:    "items",
		Columns: []config.ColumnConfig{{Name: "title", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}

	_, err = h.UpdateTableRLSModeForUser(ctx, actors["loner"], noEmailApp.ID, table.Name, "owner", "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *ValidationError for owner RLS without email auth, got %v (%T)", err, err)
	}
}

// TestUpdateTableRLSModeForUser_UnknownTableNotFound covers the by-name
// lookup this function uses instead of UpdateAppTable's by-id lookup.
func TestUpdateTableRLSModeForUser_UnknownTableNotFound(t *testing.T) {
	pool, h, actors, appID, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := h.UpdateTableRLSModeForUser(ctx, actors["loner"], appID, "no-such-table", "", "127.0.0.1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

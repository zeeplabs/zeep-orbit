package dashboard

import (
	"context"
	"errors"
	"testing"
)

// TestCreateAppForUser_HappyPath covers T4's Done-when: calling
// CreateAppForUser directly (no HTTP layer) creates an app and produces the
// same audit_log row shape CreateApp's handler produced before extraction
// (mcp-server spec MCP-06/MCP-10).
func TestCreateAppForUser_HappyPath(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], appRequestBody{
		Name:             "cli-created-app",
		AuthEmailEnabled: true,
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	if app.ID == "" {
		t.Fatal("expected a non-empty app id")
	}
	if app.Name != "cli-created-app" {
		t.Fatalf("expected app name %q, got %q", "cli-created-app", app.Name)
	}
	if app.JWTSecret != "" {
		t.Fatal("expected JWTSecret to be cleared before returning, per the pre-extraction handler's behavior")
	}

	var action, resourceType, resourceID string
	if err := pool.QueryRow(ctx,
		`SELECT action, resource_type, resource_id FROM zeep_system.audit_log
		 WHERE resource_type = 'app' AND resource_id = $1`,
		app.ID,
	).Scan(&action, &resourceType, &resourceID); err != nil {
		t.Fatalf("expected an audit_log row for the created app: %v", err)
	}
	if action != "app.create" {
		t.Fatalf("expected audit action %q, got %q", "app.create", action)
	}
	if resourceType != "app" {
		t.Fatalf("expected audit resource_type %q, got %q", "app", resourceType)
	}
}

// TestCreateAppForUser_InvalidNameReturnsValidationError covers T4's
// Done-when: the same input validation the REST handler enforced before
// extraction (validateAppInput) still runs inside CreateAppForUser, and the
// caller can distinguish it from an internal failure via *ValidationError
// (mcp-server spec MCP-08: structured error naming the specific failure).
func TestCreateAppForUser_InvalidNameReturnsValidationError(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], appRequestBody{
		Name: "Invalid Name!",
	}, "127.0.0.1")
	if app != nil {
		t.Fatal("expected no app to be created for invalid input")
	}
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected a *ValidationError, got %v (%T)", err, err)
	}
	if valErr.Error() == "" {
		t.Fatal("expected a non-empty validation error message naming the specific failure")
	}
}

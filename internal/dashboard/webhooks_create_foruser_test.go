package dashboard

import (
	"context"
	"errors"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"go.uber.org/zap"
)

// TestCreateWebhookForUser_HappyPathCreatesWebhook covers spec P3 AC1: a
// manager can create a webhook, using the same validation the REST handler
// already applies.
func TestCreateWebhookForUser_HappyPathCreatesWebhook(t *testing.T) {
	pool := webhooksTestPool(t)
	h := NewHandler(pool, registry.New(), zap.NewNop())
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	row, err := h.CreateWebhookForUser(ctx, actors["admin"], appID, CreateWebhookInput{
		Name: "orders webhook", Method: "POST", EventTypePath: "eventType",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebhookForUser: %v", err)
	}
	if row.Name != "orders webhook" || row.Method != "POST" {
		t.Fatalf("unexpected webhook row: %+v", row)
	}

	rows, err := ListWebhooksForUser(ctx, pool, actors["admin"], appID)
	if err != nil {
		t.Fatalf("ListWebhooksForUser: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("expected the created webhook to be listed, got %+v", rows)
	}
}

// TestCreateWebhookForUser_SuperadminBypassesMembership covers the spec's
// Edge Cases line: a superadmin with no app_members row still succeeds
// (ResolveAppRole's superadmin bypass grants AppRoleAdmin, satisfying
// CanManage()) — no additional restriction from the MCP layer.
func TestCreateWebhookForUser_SuperadminBypassesMembership(t *testing.T) {
	pool := webhooksTestPool(t)
	h := NewHandler(pool, registry.New(), zap.NewNop())
	ctx := context.Background()
	appID, _ := webhooksTestAppWithMembers(t, pool)
	super := &DashboardUser{ID: testUser(t, pool, "wh-super@example.com", "superadmin"), Role: "superadmin"}

	row, err := h.CreateWebhookForUser(ctx, super, appID, CreateWebhookInput{
		Name: "superadmin webhook", Method: "POST", EventTypePath: "eventType",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebhookForUser as superadmin with no app_members row: %v", err)
	}
	if row.Name != "superadmin webhook" {
		t.Fatalf("unexpected webhook row: %+v", row)
	}
}

// TestCreateWebhookForUser_NonManagerForbidden covers the CanManage() tier
// this endpoint actually enforces (matching webhookRBACGate, not CanWrite()
// like the table-schema tools).
func TestCreateWebhookForUser_NonManagerForbidden(t *testing.T) {
	pool := webhooksTestPool(t)
	h := NewHandler(pool, registry.New(), zap.NewNop())
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	_, err := h.CreateWebhookForUser(ctx, actors["editor"], appID, CreateWebhookInput{
		Name: "orders webhook", Method: "POST", EventTypePath: "eventType",
	}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanManage()==false), got %v", err)
	}
}

// TestCreateWebhookForUser_OutsiderNotFound covers the not-found branch for
// an app the caller has no visibility into.
func TestCreateWebhookForUser_OutsiderNotFound(t *testing.T) {
	pool := webhooksTestPool(t)
	h := NewHandler(pool, registry.New(), zap.NewNop())
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	_, err := h.CreateWebhookForUser(ctx, actors["outsider"], appID, CreateWebhookInput{
		Name: "orders webhook", Method: "POST", EventTypePath: "eventType",
	}, "127.0.0.1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an outsider with no app visibility, got %v", err)
	}
}

// TestCreateWebhookForUser_InvalidMethodRejected covers the same
// bad-method validation the REST handler applies.
func TestCreateWebhookForUser_InvalidMethodRejected(t *testing.T) {
	pool := webhooksTestPool(t)
	h := NewHandler(pool, registry.New(), zap.NewNop())
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	_, err := h.CreateWebhookForUser(ctx, actors["admin"], appID, CreateWebhookInput{
		Name: "orders webhook", Method: "DELETE", EventTypePath: "eventType",
	}, "127.0.0.1")
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError for an invalid method, got %v (%T)", err, err)
	}
}

// TestCreateWebhookForUser_RecordsAudit covers spec's audit requirement,
// reusing the REST handler's existing "webhook.create" action string.
func TestCreateWebhookForUser_RecordsAudit(t *testing.T) {
	pool := webhooksTestPool(t)
	h := NewHandler(pool, registry.New(), zap.NewNop())
	ctx := context.Background()
	appID, actors := webhooksTestAppWithMembers(t, pool)

	row, err := h.CreateWebhookForUser(ctx, actors["admin"], appID, CreateWebhookInput{
		Name: "orders webhook", Method: "POST", EventTypePath: "eventType",
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebhookForUser: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'webhook.create' AND resource_id = $1`,
		row.ID,
	).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit_log row for webhook.create, got %d", count)
	}
}

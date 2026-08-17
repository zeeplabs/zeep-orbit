package dashboard

import (
	"context"
	"errors"
	"testing"
)

// TestSaveEventMappingForUser_HappyPath covers spec P3 AC2: a manager saves
// an event mapping using the same validation SaveEventMapping already
// applies.
func TestSaveEventMappingForUser_HappyPath(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	row, err := h.SaveEventMappingForUser(ctx, actors["appadmin"], appID, created.ID, EventMappingDef{
		EventTypeValue: "employee.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "id", Column: "external_id"},
			{SourcePath: "name", Column: "full_name"},
		},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("SaveEventMappingForUser: %v", err)
	}
	if row.EventTypeValue != "employee.created" || row.TargetTable != "employees" {
		t.Fatalf("unexpected mapping row: %+v", row)
	}

	var auditCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.audit_log WHERE action = 'webhook.mapping.save' AND resource_id = $1`,
		row.ID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit_log row for webhook.mapping.save, got %d", auditCount)
	}
}

// TestSaveEventMappingForUser_UnknownTargetTable covers spec P3 AC3.
func TestSaveEventMappingForUser_UnknownTargetTable(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	_, err := h.SaveEventMappingForUser(ctx, actors["appadmin"], appID, created.ID, EventMappingDef{
		EventTypeValue: "employee.created",
		Action:         "insert",
		TargetTable:    "does_not_exist",
		FieldMappings:  []FieldMappingDef{{SourcePath: "id", Column: "external_id"}},
	}, "127.0.0.1")
	if !errors.Is(err, ErrUnknownTargetTable) {
		t.Fatalf("expected ErrUnknownTargetTable, got %v", err)
	}
}

// TestSaveEventMappingForUser_ConflictLeavesFirstMappingIntact covers spec
// P3 AC4: a conflicting event_type_value is rejected, first mapping intact.
func TestSaveEventMappingForUser_ConflictLeavesFirstMappingIntact(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	created := createTestWebhook(t, h, appID, actors["appadmin"])
	def := EventMappingDef{
		EventTypeValue: "employee.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings:  []FieldMappingDef{{SourcePath: "id", Column: "external_id"}},
	}
	first, err := h.SaveEventMappingForUser(ctx, actors["appadmin"], appID, created.ID, def, "127.0.0.1")
	if err != nil {
		t.Fatalf("SaveEventMappingForUser (1st): %v", err)
	}

	def2 := def
	def2.TargetTable = "employees"
	_, err = h.SaveEventMappingForUser(ctx, actors["appadmin"], appID, created.ID, def2, "127.0.0.1")
	if !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("expected ErrMappingConflict, got %v", err)
	}

	mappings, err := ListEventMappings(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("ListEventMappings: %v", err)
	}
	if len(mappings) != 1 || mappings[0].ID != first.ID {
		t.Fatalf("expected only the first mapping to survive, got %+v", mappings)
	}
}

// TestSaveEventMappingForUser_CrossAppWebhookReturnsNotFound covers the
// cross-app scoping edge case: a webhook_id belonging to a different app.
func TestSaveEventMappingForUser_CrossAppWebhookReturnsNotFound(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	var otherAppID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"webhook-mapping-other-app", actors["appadmin"].ID,
	).Scan(&otherAppID); err != nil {
		t.Fatalf("create other app: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		otherAppID, actors["appadmin"].ID,
	); err != nil {
		t.Fatalf("seed admin membership on other app: %v", err)
	}

	_, err := h.SaveEventMappingForUser(ctx, actors["appadmin"], otherAppID, created.ID, EventMappingDef{
		EventTypeValue: "employee.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings:  []FieldMappingDef{{SourcePath: "id", Column: "external_id"}},
	}, "127.0.0.1")
	if !errors.Is(err, ErrWebhookNotFound) {
		t.Fatalf("expected ErrWebhookNotFound for a webhook belonging to a different app, got %v", err)
	}
}

// TestSaveEventMappingForUser_NonManagerForbidden covers the CanManage()
// tier this endpoint enforces.
func TestSaveEventMappingForUser_NonManagerForbidden(t *testing.T) {
	pool, h, actors, appID, _ := webhooksHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	created := createTestWebhook(t, h, appID, actors["appadmin"])

	_, err := h.SaveEventMappingForUser(ctx, actors["appeditor"], appID, created.ID, EventMappingDef{
		EventTypeValue: "employee.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings:  []FieldMappingDef{{SourcePath: "id", Column: "external_id"}},
	}, "127.0.0.1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanManage()==false), got %v", err)
	}
}

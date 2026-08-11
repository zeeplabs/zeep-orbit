package dashboard

import (
	"context"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

const webhookMappingTestApp = "webhook-mapping-test-app"

// webhookMappingTestRegistry builds a registry with one app exposing an
// "employees" table (the context.md Google Workspace employee-sync use
// case), so SaveEventMapping's registry.GetTable validation has something
// real to check against.
func webhookMappingTestRegistry() *registry.Registry {
	reg := registry.New()
	reg.Register(&registry.App{
		Config:     config.AppConfig{Name: webhookMappingTestApp},
		SchemaName: "webhook_mapping_test_app",
		Tables: map[string]*registry.Table{
			"employees": {
				Name: "employees",
				Columns: []registry.Column{
					{Name: "external_id", Type: "text"},
					{Name: "full_name", Type: "text"},
					{Name: "email", Type: "text"},
				},
			},
		},
	})
	return reg
}

func TestSaveEventMapping_HappyPathInsert(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	row, err := SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
			{SourcePath: "user.name", Column: "full_name"},
		},
	})
	if err != nil {
		t.Fatalf("SaveEventMapping (insert): %v", err)
	}
	if row.Action != "insert" || row.TargetTable != "employees" {
		t.Fatalf("expected action=insert target_table=employees, got action=%q target_table=%q", row.Action, row.TargetTable)
	}
	if len(row.FieldMappings) != 2 {
		t.Fatalf("expected 2 field mappings, got %d", len(row.FieldMappings))
	}
}

func TestSaveEventMapping_UpdateAndDeleteRequireMatchKey(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, err = SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.updated",
		Action:         "update",
		TargetTable:    "employees",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.name", Column: "full_name"},
		},
	})
	if err != ErrMatchKeyRequired {
		t.Fatalf("expected ErrMatchKeyRequired for update with no match key, got %v", err)
	}

	row, err := SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.deleted",
		Action:         "delete",
		TargetTable:    "employees",
		MatchKeyColumn: "external_id",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
		},
	})
	if err != nil {
		t.Fatalf("SaveEventMapping (delete with match key): %v", err)
	}
	if row.MatchKeyColumn == nil || *row.MatchKeyColumn != "external_id" {
		t.Fatalf("expected match_key_column 'external_id', got %v", row.MatchKeyColumn)
	}
}

func TestSaveEventMapping_UnknownTableRejected(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, err = SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "does_not_exist",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
		},
	})
	if err != ErrUnknownTargetTable {
		t.Fatalf("expected ErrUnknownTargetTable, got %v", err)
	}
}

func TestSaveEventMapping_EmptyEventTypeValueRejected(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, err = SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "   ",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
		},
	})
	if err != ErrEventTypeValueRequired {
		t.Fatalf("expected ErrEventTypeValueRequired for a blank event_type_value, got %v", err)
	}
}

func TestSaveEventMapping_EmptyFieldMappingsRejected(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, err = SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "employees",
	})
	if err != ErrFieldMappingsRequired {
		t.Fatalf("expected ErrFieldMappingsRequired for an empty field_mappings, got %v", err)
	}
}

func TestSaveEventMapping_UnknownColumnRejected(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, err = SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.id", Column: "does_not_exist_column"},
		},
	})
	if err != ErrUnknownTargetColumn {
		t.Fatalf("expected ErrUnknownTargetColumn, got %v", err)
	}
}

func TestSaveEventMapping_DuplicateEventTypeValueReturnsConflict(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	def := EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
		},
	}
	if _, err := SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, def); err != nil {
		t.Fatalf("SaveEventMapping (1st): %v", err)
	}
	_, err = SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, def)
	if err != ErrMappingConflict {
		t.Fatalf("expected ErrMappingConflict for duplicate event_type_value on the same webhook, got %v", err)
	}
}

func TestListEventMappings_And_GetEventMappingByType(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	oneFieldMapping := []FieldMappingDef{{SourcePath: "user.id", Column: "external_id"}}
	if _, err := SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.created", Action: "insert", TargetTable: "employees", FieldMappings: oneFieldMapping,
	}); err != nil {
		t.Fatalf("SaveEventMapping (created): %v", err)
	}
	if _, err := SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.deleted", Action: "delete", TargetTable: "employees", MatchKeyColumn: "external_id", FieldMappings: oneFieldMapping,
	}); err != nil {
		t.Fatalf("SaveEventMapping (deleted): %v", err)
	}

	list, err := ListEventMappings(ctx, pool, wh.ID)
	if err != nil {
		t.Fatalf("ListEventMappings: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(list))
	}

	found, err := GetEventMappingByType(ctx, pool, wh.ID, "user.created")
	if err != nil {
		t.Fatalf("GetEventMappingByType: %v", err)
	}
	if found.Action != "insert" {
		t.Fatalf("expected action=insert, got %q", found.Action)
	}

	if _, err := GetEventMappingByType(ctx, pool, wh.ID, "user.unmapped"); err != ErrMappingNotFound {
		t.Fatalf("expected ErrMappingNotFound for an unconfigured event type, got %v", err)
	}
}

func TestDeleteEventMapping(t *testing.T) {
	pool := webhooksTestPool(t)
	appID, userID := webhooksTestApp(t, pool)
	ctx := context.Background()
	reg := webhookMappingTestRegistry()

	wh, _, err := CreateWebhook(ctx, pool, CreateWebhookInput{
		AppID: appID, Name: "hook", Method: "POST", EventTypePath: "eventType", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	row, err := SaveEventMapping(ctx, pool, reg, webhookMappingTestApp, wh.ID, EventMappingDef{
		EventTypeValue: "user.created", Action: "insert", TargetTable: "employees",
		FieldMappings: []FieldMappingDef{{SourcePath: "user.id", Column: "external_id"}},
	})
	if err != nil {
		t.Fatalf("SaveEventMapping: %v", err)
	}

	if err := DeleteEventMapping(ctx, pool, row.ID); err != nil {
		t.Fatalf("DeleteEventMapping: %v", err)
	}
	if err := DeleteEventMapping(ctx, pool, row.ID); err != ErrMappingNotFound {
		t.Fatalf("expected ErrMappingNotFound on second delete, got %v", err)
	}

	list, err := ListEventMappings(ctx, pool, wh.ID)
	if err != nil {
		t.Fatalf("ListEventMappings: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 mappings after delete, got %d", len(list))
	}
}

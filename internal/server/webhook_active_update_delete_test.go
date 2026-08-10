package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// activateWithFullLifecycleMappings saves insert/update/delete mappings for
// three distinct event-type values on the same webhook — the context.md
// Google Workspace employee-sync scenario (create, edit, remove all on one
// webhook) — and activates it. external_id is included in every mapping's
// field_mappings so it doubles as both a written column and the
// update/delete match key.
func (f webhookActiveFixture) activateWithFullLifecycleMappings(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	if _, err := dashboard.SaveEventMapping(ctx, testPool, testReg, f.appName, f.wh.ID, dashboard.EventMappingDef{
		EventTypeValue: "user.created",
		Action:         "insert",
		TargetTable:    "employees",
		FieldMappings: []dashboard.FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
			{SourcePath: "user.name", Column: "full_name"},
		},
	}); err != nil {
		t.Fatalf("SaveEventMapping (insert): %v", err)
	}
	if _, err := dashboard.SaveEventMapping(ctx, testPool, testReg, f.appName, f.wh.ID, dashboard.EventMappingDef{
		EventTypeValue: "user.updated",
		Action:         "update",
		TargetTable:    "employees",
		MatchKeyColumn: "external_id",
		FieldMappings: []dashboard.FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
			{SourcePath: "user.name", Column: "full_name"},
		},
	}); err != nil {
		t.Fatalf("SaveEventMapping (update): %v", err)
	}
	if _, err := dashboard.SaveEventMapping(ctx, testPool, testReg, f.appName, f.wh.ID, dashboard.EventMappingDef{
		EventTypeValue: "user.deleted",
		Action:         "delete",
		TargetTable:    "employees",
		MatchKeyColumn: "external_id",
		FieldMappings: []dashboard.FieldMappingDef{
			{SourcePath: "user.id", Column: "external_id"},
		},
	}); err != nil {
		t.Fatalf("SaveEventMapping (delete): %v", err)
	}
	if err := dashboard.ActivateWebhook(ctx, testPool, f.wh.ID); err != nil {
		t.Fatalf("ActivateWebhook: %v", err)
	}
}

func TestWebhookActive_UpdateHappyPathOverwritesLinkedColumns(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithFullLifecycleMappings(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-1","name":"Ana Souza"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postWebhook(router, f.wh, f.token, `{"eventType":"user.updated","eventId":"evt-2","user":{"id":"u-1","name":"Ana Souza Silva"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var fullName string
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row (update overwrites, does not insert), got %d", count)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT full_name FROM `+f.schema+`.employees WHERE external_id = 'u-1'`).Scan(&fullName); err != nil {
		t.Fatalf("query updated row: %v", err)
	}
	if fullName != "Ana Souza Silva" {
		t.Fatalf("expected full_name updated to 'Ana Souza Silva', got %q", fullName)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 2 || list[0].Outcome != "updated" {
		t.Fatalf("expected newest delivery outcome=updated, got %+v", list)
	}
}

func TestWebhookActive_DeleteHappyPathRemovesRow(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithFullLifecycleMappings(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-1","name":"Ana"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postWebhook(router, f.wh, f.token, `{"eventType":"user.deleted","eventId":"evt-2","user":{"id":"u-1"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees`).Scan(&count); err != nil {
		t.Fatalf("count employees: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after delete (soft-delete disabled by default -> hard DELETE), got %d", count)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 2 || list[0].Outcome != "deleted" {
		t.Fatalf("expected newest delivery outcome=deleted, got %+v", list)
	}
}

func TestWebhookActive_UpdateNoMatchingRowReturns200RowNotFound(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithFullLifecycleMappings(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.updated","eventId":"evt-1","user":{"id":"does-not-exist","name":"Ghost"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for update with no matching row, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "row_not_found" {
		t.Fatalf("expected 1 delivery logged with outcome=row_not_found, got %+v", list)
	}
}

func TestWebhookActive_DeleteNoMatchingRowReturns200RowNotFound(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithFullLifecycleMappings(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.deleted","eventId":"evt-1","user":{"id":"does-not-exist"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for delete with no matching row, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "row_not_found" {
		t.Fatalf("expected 1 delivery logged with outcome=row_not_found, got %+v", list)
	}
}

// TestWebhookActive_FullLifecycleCreateUpdateDelete reproduces the spec's
// Independent Test for the update/delete-with-match-key story: a single
// webhook carrying insert, update, and delete mappings for different
// event-type values, exercised in sequence.
func TestWebhookActive_FullLifecycleCreateUpdateDelete(t *testing.T) {
	f := setupWebhookActiveFixture(t)
	f.activateWithFullLifecycleMappings(t)
	h := NewWebhookHandler(testPool, testReg)
	router := buildActiveWebhookRouter(h)

	rec := postWebhook(router, f.wh, f.token, `{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-99","name":"Carlos"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees WHERE external_id = 'u-99'`).Scan(&count); err != nil {
		t.Fatalf("count after create: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after create, got %d", count)
	}

	rec = postWebhook(router, f.wh, f.token, `{"eventType":"user.updated","eventId":"evt-2","user":{"id":"u-99","name":"Carlos Eduardo"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var fullName string
	if err := testPool.QueryRow(context.Background(), `SELECT full_name FROM `+f.schema+`.employees WHERE external_id = 'u-99'`).Scan(&fullName); err != nil {
		t.Fatalf("query after update: %v", err)
	}
	if fullName != "Carlos Eduardo" {
		t.Fatalf("expected full_name='Carlos Eduardo' after update, got %q", fullName)
	}

	rec = postWebhook(router, f.wh, f.token, `{"eventType":"user.deleted","eventId":"evt-3","user":{"id":"u-99"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+f.schema+`.employees WHERE external_id = 'u-99'`).Scan(&count); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after delete, got %d", count)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, f.wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 delivery log entries (create, update, delete), got %d", len(list))
	}
	if list[0].Outcome != "deleted" || list[1].Outcome != "updated" || list[2].Outcome != "inserted" {
		t.Fatalf("expected newest-first outcomes [deleted, updated, inserted], got [%s, %s, %s]", list[0].Outcome, list[1].Outcome, list[2].Outcome)
	}
}

package ai

// client_edit_test.go — coverage for CallEditModel/editToolDefs/
// EditOperation parsing (ai-edit-chat spec T5). Derived from tasks.md's T5
// Done-when list and design.md's EditOperation shape, not from reading the
// implementation: one test per Kind, asserting the parsed EditOperation
// shape from a mocked tool-call response, plus the shared plain-message
// shape.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// toolCallResponseBody builds a chat-completions response whose message
// carries exactly one tool call for name/argsJSON.
func toolCallResponseBody(name string, args map[string]any) string {
	rawArgs, _ := json.Marshal(args)
	return chatResponseBody(map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []map[string]any{
			{
				"id":   "call_1",
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": string(rawArgs),
				},
			},
		},
	})
}

// TestCallEditModel_MessageShapeWhenNoToolCall mirrors
// TestCallModel_MessageShapeWhenNoToolCall for the edit-mode entry point —
// the model is still asking a clarifying question, no operation proposed
// yet.
func TestCallEditModel_MessageShapeWhenNoToolCall(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, chatResponseBody(map[string]any{
			"role":    "assistant",
			"content": "Which table should the new column go on?",
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add an email column"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.Kind != "message" {
		t.Fatalf("expected Kind %q, got %q", "message", result.Kind)
	}
	if result.EditOp != nil {
		t.Fatal("expected no EditOp on a message-shape result")
	}
}

// TestCallEditModel_AddTable covers propose_add_table -> EditOperation{Kind:
// "add_table", AddTable: ...} (AIEC-07's add_table half by way of spec P2).
func TestCallEditModel_AddTable(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_table", map[string]any{
			"name": "notes",
			"columns": []map[string]any{
				{"name": "body", "type": "text"},
			},
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add a notes table with a body column"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.Kind != "edit_op" {
		t.Fatalf("expected Kind %q, got %q", "edit_op", result.Kind)
	}
	if result.EditOp == nil || result.EditOp.Kind != "add_table" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "add_table", result.EditOp)
	}
	if result.EditOp.AddTable == nil || result.EditOp.AddTable.Name != "notes" {
		t.Fatalf("expected AddTable.Name %q, got %+v", "notes", result.EditOp.AddTable)
	}
	if len(result.EditOp.AddTable.Columns) != 1 || result.EditOp.AddTable.Columns[0].Name != "body" {
		t.Fatalf("expected one column %q, got %+v", "body", result.EditOp.AddTable.Columns)
	}
	if result.EditOp.AddColumn != nil || result.EditOp.AddIndex != nil || result.EditOp.AddReference != nil ||
		result.EditOp.SetRLSMode != nil || result.EditOp.ToggleAuth != nil {
		t.Fatalf("expected only AddTable populated, got %+v", result.EditOp)
	}
}

// TestCallEditModel_AddColumn covers propose_add_column -> EditOperation{
// Kind: "add_column", AddColumn: ...} (AIEC-02).
func TestCallEditModel_AddColumn(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_column", map[string]any{
			"table":  "users",
			"column": map[string]any{"name": "email", "type": "text"},
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add an email column to users"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "add_column" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "add_column", result.EditOp)
	}
	if result.EditOp.AddColumn == nil || result.EditOp.AddColumn.Table != "users" ||
		result.EditOp.AddColumn.Column.Name != "email" || result.EditOp.AddColumn.Column.Type != "text" {
		t.Fatalf("expected AddColumn{Table:users, Column:{email,text}}, got %+v", result.EditOp.AddColumn)
	}
}

// TestCallEditModel_AddIndex covers propose_add_index -> EditOperation{Kind:
// "add_index", AddIndex: ...} (AIEC-07), including the unique flag and a
// composite column list.
func TestCallEditModel_AddIndex(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_index", map[string]any{
			"table":   "users",
			"name":    "users_email_idx",
			"columns": []string{"email"},
			"unique":  true,
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add a unique index on email"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "add_index" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "add_index", result.EditOp)
	}
	idx := result.EditOp.AddIndex
	if idx == nil || idx.Table != "users" || idx.Name != "users_email_idx" || !idx.Unique {
		t.Fatalf("expected AddIndex{Table:users, Name:users_email_idx, Unique:true}, got %+v", idx)
	}
	if len(idx.Columns) != 1 || idx.Columns[0] != "email" {
		t.Fatalf("expected columns [email], got %+v", idx.Columns)
	}
}

// TestCallEditModel_AddReference covers propose_add_reference ->
// EditOperation{Kind: "add_reference", AddReference: ...} (AIEC-09).
func TestCallEditModel_AddReference(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_reference", map[string]any{
			"table":      "tickets",
			"column":     map[string]any{"name": "assignee_id", "type": "uuid"},
			"ref_table":  "users",
			"ref_column": "id",
			"on_delete":  "cascade",
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add an assignee foreign key to tickets"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "add_reference" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "add_reference", result.EditOp)
	}
	ref := result.EditOp.AddReference
	if ref == nil || ref.Table != "tickets" || ref.Column.Name != "assignee_id" || ref.Column.Type != "uuid" ||
		ref.RefTable != "users" || ref.RefColumn != "id" || ref.OnDelete != "cascade" {
		t.Fatalf("expected a fully populated AddReference, got %+v", ref)
	}
}

// TestCallEditModel_AddForeignKey covers propose_add_foreign_key ->
// EditOperation{Kind: "add_foreign_key", AddForeignKey: ...} (column-foreign-key
// spec CFK-15) — unlike propose_add_reference, this targets a column that
// already exists (no nested column object, just its name).
func TestCallEditModel_AddForeignKey(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_foreign_key", map[string]any{
			"table":      "tickets",
			"column":     "assignee_id",
			"ref_table":  "users",
			"ref_column": "id",
			"on_delete":  "cascade",
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add a foreign key from tickets.assignee_id to users.id"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "add_foreign_key" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "add_foreign_key", result.EditOp)
	}
	fk := result.EditOp.AddForeignKey
	if fk == nil || fk.Table != "tickets" || fk.Column != "assignee_id" ||
		fk.RefTable != "users" || fk.RefColumn != "id" || fk.OnDelete != "cascade" {
		t.Fatalf("expected a fully populated AddForeignKey, got %+v", fk)
	}
	if result.EditOp.RemoveForeignKey != nil || result.EditOp.AddReference != nil {
		t.Fatalf("expected only AddForeignKey populated, got %+v", result.EditOp)
	}
}

// TestCallEditModel_RemoveForeignKey covers propose_remove_foreign_key ->
// EditOperation{Kind: "remove_foreign_key", RemoveForeignKey: ...}
// (column-foreign-key spec CFK-15).
func TestCallEditModel_RemoveForeignKey(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_remove_foreign_key", map[string]any{
			"table":  "tickets",
			"column": "assignee_id",
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "remove the foreign key on tickets.assignee_id"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "remove_foreign_key" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "remove_foreign_key", result.EditOp)
	}
	rm := result.EditOp.RemoveForeignKey
	if rm == nil || rm.Table != "tickets" || rm.Column != "assignee_id" {
		t.Fatalf("expected RemoveForeignKey{Table:tickets, Column:assignee_id}, got %+v", rm)
	}
	if result.EditOp.AddForeignKey != nil {
		t.Fatalf("expected only RemoveForeignKey populated, got %+v", result.EditOp)
	}
}

// TestCallEditModel_MalformedForeignKeyArgumentsReturnErrorNotPartialOp
// covers the malformed-arguments case for both new tools — missing required
// fields must return ErrMalformedEditOp, never a partially-populated
// EditOperation.
func TestCallEditModel_MalformedForeignKeyArgumentsReturnErrorNotPartialOp(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_foreign_key", map[string]any{
			"table": "tickets",
			// column/ref_table/ref_column omitted entirely — missing required fields.
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add a foreign key"},
	}, nil)
	if err == nil {
		t.Fatal("expected an error for malformed propose_add_foreign_key arguments")
	}
	if result.EditOp != nil {
		t.Fatalf("expected no partial EditOp on a malformed tool call, got %+v", result.EditOp)
	}
}

// TestCallEditModel_SetRLSMode covers propose_set_rls_mode -> EditOperation{
// Kind: "set_rls_mode", SetRLSMode: ...} (AIEC-11).
func TestCallEditModel_SetRLSMode(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_set_rls_mode", map[string]any{
			"table": "tickets",
			"mode":  "owner",
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "restrict tickets to owner"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "set_rls_mode" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "set_rls_mode", result.EditOp)
	}
	if result.EditOp.SetRLSMode == nil || result.EditOp.SetRLSMode.Table != "tickets" || result.EditOp.SetRLSMode.Mode != "owner" {
		t.Fatalf("expected SetRLSMode{Table:tickets, Mode:owner}, got %+v", result.EditOp.SetRLSMode)
	}
}

// TestCallEditModel_ToggleAuth covers propose_toggle_auth -> EditOperation{
// Kind: "toggle_auth", ToggleAuth: ...} (AIEC-12).
func TestCallEditModel_ToggleAuth(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_toggle_auth", map[string]any{
			"email_enabled": true,
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "enable email auth"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}
	if result.EditOp == nil || result.EditOp.Kind != "toggle_auth" {
		t.Fatalf("expected EditOp.Kind %q, got %+v", "toggle_auth", result.EditOp)
	}
	if result.EditOp.ToggleAuth == nil || !result.EditOp.ToggleAuth.EmailEnabled {
		t.Fatalf("expected ToggleAuth{EmailEnabled:true}, got %+v", result.EditOp.ToggleAuth)
	}
}

// TestCallEditModel_MalformedArgumentsReturnErrorNotPartialOp mirrors
// TestCallModel_MalformedPlanArgumentsReturnErrorNotPartialPlan for the
// edit-mode path: missing required fields (no column on propose_add_column)
// must return ErrMalformedEditOp, never a partially-populated EditOperation.
func TestCallEditModel_MalformedArgumentsReturnErrorNotPartialOp(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolCallResponseBody("propose_add_column", map[string]any{
			"table": "users",
			// column omitted entirely — missing required field.
		}))
	})

	result, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add a column"},
	}, nil)
	if err == nil {
		t.Fatal("expected an error for malformed propose_add_column arguments")
	}
	if result.EditOp != nil {
		t.Fatalf("expected no partial EditOp on a malformed tool call, got %+v", result.EditOp)
	}
}

// TestCallEditModel_RequestSendsEditToolDefsOverTheWire closes lesson
// L-026 from ai-build-chat (applied proactively here, per tasks.md's T7
// Done-when): asserts the actual HTTP request body CallEditModel sends
// carries the edit-mode tool schemas, not merely that editToolDefs()
// returns them in isolation — a wiring bug that dropped the tools field
// from the real request would still pass a test that only inspected
// editToolDefs() directly.
func TestCallEditModel_RequestSendsEditToolDefsOverTheWire(t *testing.T) {
	var capturedToolNames []string
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		for _, tool := range reqBody.Tools {
			capturedToolNames = append(capturedToolNames, tool.Function.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, chatResponseBody(map[string]any{
			"role":    "assistant",
			"content": "ok",
		}))
	})

	_, err := CallEditModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "add an email column"},
	}, nil)
	if err != nil {
		t.Fatalf("CallEditModel: %v", err)
	}

	want := []string{
		"propose_add_table", "propose_add_column", "propose_add_index",
		"propose_add_reference", "propose_add_foreign_key", "propose_remove_foreign_key",
		"propose_set_rls_mode", "propose_toggle_auth",
		"list_apps", "get_app_schema",
	}
	got := make(map[string]bool, len(capturedToolNames))
	for _, name := range capturedToolNames {
		got[name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected the real request's tools array to include %q, got %+v", name, capturedToolNames)
		}
	}
}

// TestEditToolDefs_IncludesAllEightProposalsPlusReadTools closes lesson
// L-026 from ai-build-chat (applied proactively here, per T7's Done-when,
// extended by column-foreign-key spec T11 for the two new propose_*
// tools): asserts the actual tool set editToolDefs() advertises includes
// every one of the 8 propose_* schemas plus the 2 shared read-only tools,
// not just that CallEditModel happens to parse a mocked response correctly.
func TestEditToolDefs_IncludesAllEightProposalsPlusReadTools(t *testing.T) {
	defs := editToolDefs()
	got := make(map[string]bool, len(defs))
	for _, d := range defs {
		got[d.Function.Name] = true
	}

	want := []string{
		"propose_add_table", "propose_add_column", "propose_add_index",
		"propose_add_reference", "propose_add_foreign_key", "propose_remove_foreign_key",
		"propose_set_rls_mode", "propose_toggle_auth",
		"list_apps", "get_app_schema",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected editToolDefs() to include %q, got %+v", name, got)
		}
	}
	if len(defs) != len(want) {
		t.Errorf("expected exactly %d tool defs, got %d: %+v", len(want), len(defs), got)
	}
}

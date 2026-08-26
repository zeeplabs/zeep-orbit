// Package ai is a thin OpenAI Chat Completions client — request building,
// tool schema definitions, and response parsing into the two normalized
// shapes the "Build with AI" chat handler returns to the frontend. Plain
// net/http POST to the Chat Completions REST endpoint; no OpenAI SDK
// dependency (design.md Tech Decisions). Isolated in its own sub-package so
// a future Gemini/Claude client is a sibling file, not a rewrite of this
// one.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// chatCompletionsURL is a var (not a const) so client_test.go can point it
// at an httptest.Server — this package never calls the real OpenAI API in
// tests.
var chatCompletionsURL = "https://api.openai.com/v1/chat/completions"

// httpTimeout bounds every OpenAI call so a slow/hung response fails fast
// into the caller's generic-error path instead of tying up the request
// indefinitely (design.md Risks & Concerns).
const httpTimeout = 30 * time.Second

// Message is one chat turn in the history passed to CallModel — system,
// user, assistant, or (on a read-tool round-trip) tool.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall mirrors the OpenAI response shape for a requested function call.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc is the function name/arguments half of a ToolCall.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// PlanColumn is one column in a propose_app_plan table.
//
// SPEC_DEVIATION (column-enum-type T14): AllowedValues added here so an
// "enum"-typed column proposed via propose_app_plan (build chat) or
// propose_add_column (edit chat) carries its fixed value set through
// unchanged, the same way config.ColumnConfig.AllowedValues already works
// for the manual dashboard form. tasks.md's T14 "Where" field only lists
// ai_build_chat_handlers.go, but the tool schema this struct backs lives in
// this file, not that one — the field has to be added here for
// propose_app_plan's schema (below) to have anywhere to put the value.
type PlanColumn struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	AllowedValues []string `json:"allowed_values,omitempty"`
}

// PlanTable is one table in a propose_app_plan plan.
type PlanTable struct {
	Name    string       `json:"name"`
	Columns []PlanColumn `json:"columns"`
}

// AppPlan is the structured shape produced by the propose_app_plan tool
// call — the only shape the confirm endpoint (T9) ever accepts.
type AppPlan struct {
	Name   string      `json:"name"`
	Tables []PlanTable `json:"tables"`
	Auth   bool        `json:"auth"`
}

// ChatTurnResult is CallModel's return shape — exactly one of a plain
// message or a validated plan, matching the two shapes the frontend
// contract allows (spec.md's function-calling assumption). EditOp is
// populated only on the edit-chat path (CallEditModel, ai-edit-chat spec
// T5) and is always nil on the creation path — Plan and EditOp never
// coexist within one result.
type ChatTurnResult struct {
	Kind    string // "message" | "plan" | "edit_op"
	Content string
	Plan    *AppPlan
	EditOp  *EditOperation
}

// PlanColumnOp is the propose_add_column tool call's arguments — add
// exactly one new column to an existing table (ai-edit-chat spec AIEC-02).
type PlanColumnOp struct {
	Table  string     `json:"table"`
	Column PlanColumn `json:"column"`
}

// PlanIndexOp is the propose_add_index tool call's arguments — add exactly
// one new (optionally composite/unique) index to an existing table
// (AIEC-07).
type PlanIndexOp struct {
	Table   string   `json:"table"`
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

// PlanReferenceOp is the propose_add_reference tool call's arguments — add
// a new column (never an existing one) that's a foreign key to another
// table's column (AIEC-09).
type PlanReferenceOp struct {
	Table     string     `json:"table"`
	Column    PlanColumn `json:"column"`
	RefTable  string     `json:"ref_table"`
	RefColumn string     `json:"ref_column"`
	OnDelete  string     `json:"on_delete,omitempty"`
}

// PlanForeignKeyOp is the propose_add_foreign_key tool call's arguments —
// add a foreign key to a column that already exists on an existing table
// (column-foreign-key spec CFK-15), unlike PlanReferenceOp which only ever
// creates a brand-new column.
type PlanForeignKeyOp struct {
	Table     string `json:"table"`
	Column    string `json:"column"`
	RefTable  string `json:"ref_table"`
	RefColumn string `json:"ref_column"`
	OnDelete  string `json:"on_delete,omitempty"`
}

// PlanRemoveForeignKeyOp is the propose_remove_foreign_key tool call's
// arguments — remove the foreign key from an existing column without
// dropping the column itself (column-foreign-key spec CFK-15).
type PlanRemoveForeignKeyOp struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

// PlanRLSOp is the propose_set_rls_mode tool call's arguments (AIEC-11).
type PlanRLSOp struct {
	Table string `json:"table"`
	Mode  string `json:"mode"`
}

// PlanAuthOp is the propose_toggle_auth tool call's arguments (AIEC-12).
type PlanAuthOp struct {
	EmailEnabled bool `json:"email_enabled"`
}

// EditOperation is the structured shape produced by exactly one of the 6
// propose_* edit-mode tool calls — exactly one field besides Kind is
// non-nil, matching Kind (design.md Data Models). This is what
// EditChatConfirm (ai-edit-chat spec T8) switches on to call exactly one
// *ForUser handler.
type EditOperation struct {
	Kind             string                  `json:"kind"` // "add_table" | "add_column" | "add_index" | "add_reference" | "add_foreign_key" | "remove_foreign_key" | "set_rls_mode" | "toggle_auth"
	AddTable         *PlanTable              `json:"add_table,omitempty"`
	AddColumn        *PlanColumnOp           `json:"add_column,omitempty"`
	AddIndex         *PlanIndexOp            `json:"add_index,omitempty"`
	AddReference     *PlanReferenceOp        `json:"add_reference,omitempty"`
	AddForeignKey    *PlanForeignKeyOp       `json:"add_foreign_key,omitempty"`
	RemoveForeignKey *PlanRemoveForeignKeyOp `json:"remove_foreign_key,omitempty"`
	SetRLSMode       *PlanRLSOp              `json:"set_rls_mode,omitempty"`
	ToggleAuth       *PlanAuthOp             `json:"toggle_auth,omitempty"`
}

// ErrMalformedEditOp mirrors ErrMalformedPlan for the edit-mode tool calls —
// returned when a propose_* tool call's arguments don't parse as valid JSON
// or are missing required fields. Always a provider-class error, never a
// partially-trusted operation.
var ErrMalformedEditOp = errors.New("ai: malformed or incomplete edit tool call arguments")

// ReadToolInvoker is a closure the caller builds, closing over the
// authenticated user and dashboard handlers, so this package never imports
// the dashboard package directly. It resolves a list_apps/get_app_schema
// tool call against List*ForUser/Get*ForUser.
type ReadToolInvoker func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)

// ErrMalformedPlan is returned when a propose_app_plan tool call's
// arguments don't parse as valid JSON or are missing required fields — it
// is always treated as a provider-class error, never a partially-trusted
// plan.
var ErrMalformedPlan = errors.New("ai: malformed or incomplete propose_app_plan arguments")

// maxToolRounds bounds the read-tool round-trip loop (list_apps/
// get_app_schema) so a misbehaving model can't spin the caller forever.
const maxToolRounds = 4

// CallModel sends history plus the 3 available tools (propose_app_plan,
// list_apps, get_app_schema) to model, with tool_choice: "auto". A
// list_apps/get_app_schema tool call is resolved via readTools and fed
// back as a tool-role message before a second (or later) round-trip; the
// final round's result is what's returned to the caller — never an
// intermediate one.
func CallModel(ctx context.Context, model, apiKey string, history []Message, readTools ReadToolInvoker) (ChatTurnResult, error) {
	client := &http.Client{Timeout: httpTimeout}
	messages := append([]Message{}, history...)

	for round := 0; round < maxToolRounds; round++ {
		respMsg, err := callOnce(ctx, client, model, apiKey, messages, toolDefs())
		if err != nil {
			return ChatTurnResult{}, err
		}

		toolCall, isReadTool := firstReadToolCall(respMsg.ToolCalls)
		if planCall := findToolCall(respMsg.ToolCalls, "propose_app_plan"); planCall != nil {
			plan, err := parsePlan(planCall.Function.Arguments)
			if err != nil {
				return ChatTurnResult{}, err
			}
			return ChatTurnResult{Kind: "plan", Plan: plan}, nil
		}

		if !isReadTool {
			return ChatTurnResult{Kind: "message", Content: respMsg.Content}, nil
		}

		result, err := readTools(ctx, toolCall.Function.Name, json.RawMessage(toolCall.Function.Arguments))
		if err != nil {
			return ChatTurnResult{}, fmt.Errorf("ai: read tool %s failed: %w", toolCall.Function.Name, err)
		}

		messages = append(messages, respMsg)
		messages = append(messages, Message{
			Role:       "tool",
			Content:    string(result),
			ToolCallID: toolCall.ID,
		})
	}

	return ChatTurnResult{}, fmt.Errorf("ai: exceeded %d read-tool round-trips without a final answer", maxToolRounds)
}

// editProposalToolNames is the set of the 6 propose_* tool names
// editToolDefs() advertises — anything in this set is an edit-mode
// operation proposal, never a plain message or a read-tool call.
var editProposalToolNames = map[string]bool{
	"propose_add_table":          true,
	"propose_add_column":         true,
	"propose_add_index":          true,
	"propose_add_reference":      true,
	"propose_add_foreign_key":    true,
	"propose_remove_foreign_key": true,
	"propose_set_rls_mode":       true,
	"propose_toggle_auth":        true,
}

// firstEditProposalCall returns the first tool call whose name is one of
// the 8 propose_* edit-mode tools, or nil if respMsg's tool calls contain
// none.
func firstEditProposalCall(calls []ToolCall) *ToolCall {
	for i := range calls {
		if editProposalToolNames[calls[i].Function.Name] {
			return &calls[i]
		}
	}
	return nil
}

// CallEditModel sends history plus editToolDefs()'s 10 available tools (the
// 8 propose_* edit-mode tools, plus the same list_apps/get_app_schema
// read-only tools CallModel offers) to model, with tool_choice: "auto"
// (ai-edit-chat spec T5/T7). Mirrors CallModel's read-tool round-trip loop
// exactly, but returns ChatTurnResult.EditOp instead of .Plan when the
// model proposes an operation.
func CallEditModel(ctx context.Context, model, apiKey string, history []Message, readTools ReadToolInvoker) (ChatTurnResult, error) {
	client := &http.Client{Timeout: httpTimeout}
	messages := append([]Message{}, history...)

	for round := 0; round < maxToolRounds; round++ {
		respMsg, err := callOnce(ctx, client, model, apiKey, messages, editToolDefs())
		if err != nil {
			return ChatTurnResult{}, err
		}

		toolCall, isReadTool := firstReadToolCall(respMsg.ToolCalls)
		if editCall := firstEditProposalCall(respMsg.ToolCalls); editCall != nil {
			op, err := parseEditOperation(editCall.Function.Name, editCall.Function.Arguments)
			if err != nil {
				return ChatTurnResult{}, err
			}
			return ChatTurnResult{Kind: "edit_op", EditOp: op}, nil
		}

		if !isReadTool {
			return ChatTurnResult{Kind: "message", Content: respMsg.Content}, nil
		}

		result, err := readTools(ctx, toolCall.Function.Name, json.RawMessage(toolCall.Function.Arguments))
		if err != nil {
			return ChatTurnResult{}, fmt.Errorf("ai: read tool %s failed: %w", toolCall.Function.Name, err)
		}

		messages = append(messages, respMsg)
		messages = append(messages, Message{
			Role:       "tool",
			Content:    string(result),
			ToolCallID: toolCall.ID,
		})
	}

	return ChatTurnResult{}, fmt.Errorf("ai: exceeded %d read-tool round-trips without a final answer", maxToolRounds)
}

// parseEditOperation parses one propose_* tool call's raw JSON arguments
// into the matching EditOperation shape, validating the required fields
// each operation's design.md shape needs before EditChatConfirm (T8) ever
// sees it — mirroring parsePlan's malformed-input guard below.
func parseEditOperation(toolName, rawArgs string) (*EditOperation, error) {
	if rawArgs == "" {
		return nil, ErrMalformedEditOp
	}

	switch toolName {
	case "propose_add_table":
		var t PlanTable
		if err := json.Unmarshal([]byte(rawArgs), &t); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if t.Name == "" || len(t.Columns) == 0 {
			return nil, ErrMalformedEditOp
		}
		for _, c := range t.Columns {
			if c.Name == "" || c.Type == "" {
				return nil, ErrMalformedEditOp
			}
		}
		return &EditOperation{Kind: "add_table", AddTable: &t}, nil

	case "propose_add_column":
		var op PlanColumnOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if op.Table == "" || op.Column.Name == "" || op.Column.Type == "" {
			return nil, ErrMalformedEditOp
		}
		return &EditOperation{Kind: "add_column", AddColumn: &op}, nil

	case "propose_add_index":
		var op PlanIndexOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if op.Table == "" || op.Name == "" || len(op.Columns) == 0 {
			return nil, ErrMalformedEditOp
		}
		return &EditOperation{Kind: "add_index", AddIndex: &op}, nil

	case "propose_add_reference":
		var op PlanReferenceOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if op.Table == "" || op.Column.Name == "" || op.Column.Type == "" || op.RefTable == "" || op.RefColumn == "" {
			return nil, ErrMalformedEditOp
		}
		return &EditOperation{Kind: "add_reference", AddReference: &op}, nil

	case "propose_add_foreign_key":
		var op PlanForeignKeyOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if op.Table == "" || op.Column == "" || op.RefTable == "" || op.RefColumn == "" {
			return nil, ErrMalformedEditOp
		}
		return &EditOperation{Kind: "add_foreign_key", AddForeignKey: &op}, nil

	case "propose_remove_foreign_key":
		var op PlanRemoveForeignKeyOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if op.Table == "" || op.Column == "" {
			return nil, ErrMalformedEditOp
		}
		return &EditOperation{Kind: "remove_foreign_key", RemoveForeignKey: &op}, nil

	case "propose_set_rls_mode":
		var op PlanRLSOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		if op.Table == "" {
			return nil, ErrMalformedEditOp
		}
		return &EditOperation{Kind: "set_rls_mode", SetRLSMode: &op}, nil

	case "propose_toggle_auth":
		var op PlanAuthOp
		if err := json.Unmarshal([]byte(rawArgs), &op); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformedEditOp, err)
		}
		return &EditOperation{Kind: "toggle_auth", ToggleAuth: &op}, nil

	default:
		return nil, fmt.Errorf("ai: unknown edit tool %q", toolName)
	}
}

func firstReadToolCall(calls []ToolCall) (*ToolCall, bool) {
	for i := range calls {
		if calls[i].Function.Name == "list_apps" || calls[i].Function.Name == "get_app_schema" {
			return &calls[i], true
		}
	}
	return nil, false
}

func findToolCall(calls []ToolCall, name string) *ToolCall {
	for i := range calls {
		if calls[i].Function.Name == name {
			return &calls[i]
		}
	}
	return nil
}

func parsePlan(rawArgs string) (*AppPlan, error) {
	if rawArgs == "" {
		return nil, ErrMalformedPlan
	}
	var plan AppPlan
	if err := json.Unmarshal([]byte(rawArgs), &plan); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedPlan, err)
	}
	if plan.Name == "" || len(plan.Tables) == 0 {
		return nil, ErrMalformedPlan
	}
	for _, t := range plan.Tables {
		if t.Name == "" {
			return nil, ErrMalformedPlan
		}
	}
	return &plan, nil
}

type chatCompletionsRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []toolDef `json:"tools"`
	ToolChoice string    `json:"tool_choice"`
}

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func callOnce(ctx context.Context, client *http.Client, model, apiKey string, messages []Message, tools []toolDef) (Message, error) {
	reqBody := chatCompletionsRequest{
		Model:      model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: "auto",
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, fmt.Errorf("ai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL, bytes.NewReader(payload))
	if err != nil {
		return Message{}, fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("ai: call openai: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("ai: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("ai: openai returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed chatCompletionsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Message{}, fmt.Errorf("ai: decode response: %w", err)
	}
	if parsed.Error != nil {
		return Message{}, fmt.Errorf("ai: openai error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Message{}, errors.New("ai: openai response had no choices")
	}

	return parsed.Choices[0].Message, nil
}

// toolDefs is the fixed 3-tool schema every CallModel request advertises:
// propose_app_plan (the only mutation-shaping tool), and the two read-only
// tools (list_apps/get_app_schema) mirroring orbit_list_apps/
// orbit_get_app_schema.
func toolDefs() []toolDef {
	defs := []toolDef{
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_app_plan",
				Description: "Propose a concrete app plan (name, tables with columns, whether auth is needed) once enough information has been gathered from the user.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"tables": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
									"columns": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"name": map[string]any{"type": "string"},
												"type": map[string]any{"type": "string"},
												"allowed_values": map[string]any{
													"type":        "array",
													"items":       map[string]any{"type": "string"},
													"description": "Required when type is \"enum\": the fixed set of values this column accepts.",
												},
											},
										},
									},
								},
								"required": []string{"name"},
							},
						},
						"auth": map[string]any{"type": "boolean"},
					},
					"required": []string{"name", "tables", "auth"},
				},
			},
		},
	}
	return append(defs, readOnlyToolDefs()...)
}

// readOnlyToolDefs is the pair of read-only tools (list_apps/
// get_app_schema, mirroring orbit_list_apps/orbit_get_app_schema) shared by
// both toolDefs() (creation) and editToolDefs() (ai-edit-chat spec T5) —
// factored out so the two never drift against each other.
func readOnlyToolDefs() []toolDef {
	return []toolDef{
		{
			Type: "function",
			Function: functionDef{
				Name:        "list_apps",
				Description: "List the caller's own existing apps, so the assistant can reference one instead of guessing its schema.",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "get_app_schema",
				Description: "Get the real tables/columns/RLS of one of the caller's existing apps by name.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"app_name": map[string]any{"type": "string"},
					},
					"required": []string{"app_name"},
				},
			},
		},
	}
}

// editToolDefs is the fixed 10-tool schema every CallEditModel request
// advertises: the 8 propose_* operation-shaping tools (ai-edit-chat spec
// T5, plus propose_add_foreign_key/propose_remove_foreign_key from the
// column-foreign-key spec T11), plus the same 2 read-only tools toolDefs()
// offers — the edit-chat system prompt instructs the model to look up the
// app's real current schema via get_app_schema before proposing any
// operation on it, rather than guessing (design.md's editChatSystemPrompt
// note).
func editToolDefs() []toolDef {
	defs := []toolDef{
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_add_table",
				Description: "Propose creating a brand-new table (with its columns) inside the app already open in this edit session.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"columns": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
									"type": map[string]any{"type": "string"},
								},
								"required": []string{"name", "type"},
							},
						},
					},
					"required": []string{"name", "columns"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_add_column",
				Description: "Propose adding exactly one new column to an existing table.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"table": map[string]any{"type": "string"},
						"column": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{"type": "string"},
								"type": map[string]any{"type": "string"},
							},
							"required": []string{"name", "type"},
						},
					},
					"required": []string{"table", "column"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_add_index",
				Description: "Propose adding exactly one new index (optionally composite or unique) to an existing table.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"table":   map[string]any{"type": "string"},
						"name":    map[string]any{"type": "string"},
						"columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"unique":  map[string]any{"type": "boolean"},
					},
					"required": []string{"table", "name", "columns"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_add_reference",
				Description: "Propose adding exactly one new column that is a foreign key to another table's column. Never use this for a column that already exists — use propose_add_foreign_key instead.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"table": map[string]any{"type": "string"},
						"column": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{"type": "string"},
								"type": map[string]any{"type": "string"},
							},
							"required": []string{"name", "type"},
						},
						"ref_table":  map[string]any{"type": "string"},
						"ref_column": map[string]any{"type": "string"},
						"on_delete":  map[string]any{"type": "string"},
					},
					"required": []string{"table", "column", "ref_table", "ref_column"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_add_foreign_key",
				Description: "Propose adding a foreign key to a column that already exists on an existing table, without dropping or recreating the column.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"table":      map[string]any{"type": "string"},
						"column":     map[string]any{"type": "string"},
						"ref_table":  map[string]any{"type": "string"},
						"ref_column": map[string]any{"type": "string"},
						"on_delete":  map[string]any{"type": "string"},
					},
					"required": []string{"table", "column", "ref_table", "ref_column"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_remove_foreign_key",
				Description: "Propose removing the foreign key from an existing column, without dropping the column itself.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"table":  map[string]any{"type": "string"},
						"column": map[string]any{"type": "string"},
					},
					"required": []string{"table", "column"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_set_rls_mode",
				Description: "Propose changing an existing table's row-level security mode.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"table": map[string]any{"type": "string"},
						"mode":  map[string]any{"type": "string"},
					},
					"required": []string{"table", "mode"},
				},
			},
		},
		{
			Type: "function",
			Function: functionDef{
				Name:        "propose_toggle_auth",
				Description: "Propose enabling or disabling email/password authentication for the app.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"email_enabled": map[string]any{"type": "boolean"},
					},
					"required": []string{"email_enabled"},
				},
			},
		},
	}
	return append(defs, readOnlyToolDefs()...)
}

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
type PlanColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
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
// contract allows (spec.md's function-calling assumption).
type ChatTurnResult struct {
	Kind    string // "message" | "plan"
	Content string
	Plan    *AppPlan
}

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
		respMsg, err := callOnce(ctx, client, model, apiKey, messages)
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

func callOnce(ctx context.Context, client *http.Client, model, apiKey string, messages []Message) (Message, error) {
	reqBody := chatCompletionsRequest{
		Model:      model,
		Messages:   messages,
		Tools:      toolDefs(),
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
	return []toolDef{
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

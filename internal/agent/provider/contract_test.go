package provider

// Provider contract table. contractCases/runContract assert behaviors that
// hold for ANY correct Provider and are written to tolerate model
// nondeterminism, so the same table runs both against the deterministic
// httptest fake below (CI) and against real endpoints (contract_real_test.go,
// -tags providercontract).
//
// Parallel tool calls are a REPORTED capability, not a pass/fail: a real model
// may legitimately answer two independent lookups with one call. The case
// logs "parallel supported: yes/no" and only fails when the calls it did get
// are malformed (colliding IDs, invalid arguments, order not preserved).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	contractCaseTimeout = 90 * time.Second

	contractSystem = "You are a test assistant. Follow instructions exactly and keep replies short."

	// contractSecret is a value a model cannot guess; seeing it in the final
	// answer proves the tool result made the round trip.
	contractSecret = "zephyr-quokka-7731"

	contractPlainPrompt    = "Reply with one short friendly sentence. Do not use any tools."
	contractSinglePrompt   = "Use the lookup_item tool to look up the item with id item-42. Call the tool now; do not answer from memory."
	contractParallelPrompt = "I need two independent lookups: item-A and item-B. Call lookup_item once for item-A and once for item-B, both in this same turn, without waiting for any result."
	contractRoundTripUser  = "Use the lookup_item tool to look up item-42, then tell me its label."
	contractRoundTripSys   = contractSystem + " After a tool result arrives, answer the user in plain text and do not call any tool again."
)

// lookupItemTool is a deliberately neutral domain tool.
var lookupItemTool = ToolSpec{
	Name:        "lookup_item",
	Description: "Look up an inventory item by its id and return its label.",
	Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Item id, for example item-42"}},` +
		`"required":["id"],"additionalProperties":false}`),
}

// contractCase is one row of the table. Run returns an optional short note
// that runContract adds to the machine-readable summary line.
type contractCase struct {
	Name string
	Run  func(t *testing.T, p Provider) string
}

// contractCases is the shared table.
func contractCases() []contractCase {
	return []contractCase{
		{Name: "plain_message", Run: casePlainMessage},
		{Name: "single_tool_call", Run: caseSingleToolCall},
		{Name: "parallel_tool_calls", Run: caseParallelToolCalls},
		{Name: "tool_result_round_trip", Run: caseToolResultRoundTrip},
		{Name: "usage_present", Run: caseUsagePresent},
	}
}

// runContract runs every case against p and logs one CONTRACT_RESULT line per
// case (PASS/FAIL/SKIP plus provider and model) that can be pasted into
// design.md Risks. Only the provider name and model are printed: never keys or
// base URLs.
func runContract(t *testing.T, p Provider) {
	t.Helper()
	for _, c := range contractCases() {
		result, note := "PASS", ""
		t.Run(c.Name, func(t *testing.T) {
			defer func() {
				switch {
				case t.Skipped():
					result = "SKIP"
				case t.Failed():
					result = "FAIL"
				}
			}()
			note = c.Run(t, p)
		})
		t.Logf("CONTRACT_RESULT provider=%s model=%s case=%s result=%s note=%q", p.Name(), p.Model(), c.Name, result, note)
	}
}

func contractCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), contractCaseTimeout)
	t.Cleanup(cancel)
	return ctx
}

// mustChat performs one call and enforces the invariants every response must
// satisfy, so every case implicitly re-checks them: no error, an assistant
// message, and usage reported for both input and output. Provider errors are
// generic by design (no key or body), so printing them is safe.
func mustChat(t *testing.T, p Provider, req ChatRequest) ChatResponse {
	t.Helper()
	resp, err := p.Chat(contractCtx(t), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Role != RoleAssistant {
		t.Errorf("response role = %q, want %q", resp.Message.Role, RoleAssistant)
	}
	if resp.Usage.InputTokens <= 0 || resp.Usage.OutputTokens <= 0 {
		t.Errorf("usage missing or zero: %+v", resp.Usage)
	}
	if resp.FinishReason == "" {
		t.Errorf("finish reason is empty")
	}
	if !reflect.DeepEqual(resp.ToolCalls, resp.Message.ToolCalls) {
		t.Errorf("ChatResponse.ToolCalls differs from Message.ToolCalls: %+v vs %+v", resp.ToolCalls, resp.Message.ToolCalls)
	}
	return resp
}

// validateCalls checks what holds for any well-formed set of tool calls:
// non-empty names and IDs, IDs distinct, arguments a JSON object.
func validateCalls(calls []ToolCall) error {
	seen := make(map[string]struct{}, len(calls))
	for i, c := range calls {
		if c.ID == "" {
			return fmt.Errorf("call %d has an empty id", i)
		}
		if _, dup := seen[c.ID]; dup {
			return fmt.Errorf("call %d reuses id %q", i, c.ID)
		}
		seen[c.ID] = struct{}{}
		if c.Name == "" {
			return fmt.Errorf("call %d has an empty name", i)
		}
		var args map[string]any
		if err := json.Unmarshal(c.Arguments, &args); err != nil || args == nil {
			return fmt.Errorf("call %d arguments are not a JSON object: %q", i, string(c.Arguments))
		}
	}
	return nil
}

// itemID returns the "id" string argument of a lookup_item call.
func itemID(c ToolCall) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.Arguments, &args); err != nil || args.ID == "" {
		return "", fmt.Errorf("arguments lack a string id: %q", string(c.Arguments))
	}
	return args.ID, nil
}

func toolResult(id string) string {
	b, _ := json.Marshal(map[string]string{"id": id, "label": contractSecret})
	return string(b)
}

// 1. plain message
func casePlainMessage(t *testing.T, p Provider) string {
	resp := mustChat(t, p, ChatRequest{
		System:   contractSystem,
		Messages: []Message{{Role: RoleUser, Content: contractPlainPrompt}},
	})
	if strings.TrimSpace(resp.Message.Content) == "" {
		t.Errorf("reply is empty")
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("plain message returned %d tool calls", len(resp.ToolCalls))
	}
	if resp.FinishReason == FinishToolCalls {
		t.Errorf("finish reason = %q on a plain reply", resp.FinishReason)
	}
	return ""
}

// 2. single tool call
func caseSingleToolCall(t *testing.T, p Provider) string {
	resp := mustChat(t, p, ChatRequest{
		System:   contractSystem,
		Messages: []Message{{Role: RoleUser, Content: contractSinglePrompt}},
		Tools:    []ToolSpec{lookupItemTool},
	})
	if len(resp.ToolCalls) < 1 {
		t.Fatalf("expected at least 1 tool call, got 0 (content %q)", resp.Message.Content)
	}
	if err := validateCalls(resp.ToolCalls); err != nil {
		t.Fatalf("invalid tool calls: %v", err)
	}
	first := resp.ToolCalls[0]
	if first.Name != lookupItemTool.Name {
		t.Errorf("tool name = %q, want %q", first.Name, lookupItemTool.Name)
	}
	if _, err := itemID(first); err != nil {
		t.Errorf("%v", err)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Errorf("finish reason = %q, want %q", resp.FinishReason, FinishToolCalls)
	}
	return ""
}

// 3. two parallel tool calls: capability report, see the file comment.
func caseParallelToolCalls(t *testing.T, p Provider) string {
	resp := mustChat(t, p, ChatRequest{
		System:   contractSystem,
		Messages: []Message{{Role: RoleUser, Content: contractParallelPrompt}},
		Tools:    []ToolSpec{lookupItemTool},
	})
	if err := validateCalls(resp.ToolCalls); err != nil {
		t.Fatalf("invalid tool calls: %v", err)
	}
	supported := len(resp.ToolCalls) >= 2
	verdict := "no"
	if supported {
		verdict = "yes"
	}
	t.Logf("parallel supported: %s (%d tool call(s) for two independent lookups)", verdict, len(resp.ToolCalls))
	return "parallel_supported=" + verdict
}

// 4. tool-result round trip. The first turn is a real model call so the replay
// uses the exact IDs/arguments the provider produced; when the model does not
// call the tool, a synthetic transcript keeps the case meaningful.
func caseToolResultRoundTrip(t *testing.T, p Provider) string {
	history := []Message{{Role: RoleUser, Content: contractRoundTripUser}}
	mode := "model_call"

	first := mustChat(t, p, ChatRequest{System: contractRoundTripSys, Messages: history, Tools: []ToolSpec{lookupItemTool}})
	calls := first.ToolCalls
	if len(calls) > 0 {
		if err := validateCalls(calls); err != nil {
			t.Fatalf("invalid tool calls: %v", err)
		}
		history = append(history, first.Message)
	} else {
		mode = "synthetic"
		t.Logf("model did not call the tool; replaying a synthetic assistant turn")
		calls = []ToolCall{{ID: "call_contract_1", Name: lookupItemTool.Name, Arguments: json.RawMessage(`{"id":"item-42"}`)}}
		history = append(history, Message{Role: RoleAssistant, ToolCalls: calls})
	}
	for _, c := range calls {
		id, err := itemID(c)
		if err != nil {
			id = "item-42"
		}
		history = append(history, Message{Role: RoleTool, ToolCallID: c.ID, Content: toolResult(id)})
	}

	final := mustChat(t, p, ChatRequest{System: contractRoundTripSys, Messages: history, Tools: []ToolSpec{lookupItemTool}})
	if len(final.ToolCalls) != 0 {
		t.Errorf("expected a plain-text answer after the tool result, got %d tool call(s)", len(final.ToolCalls))
	}
	if !strings.Contains(strings.ToLower(final.Message.Content), contractSecret) {
		t.Errorf("final answer does not mention the tool result value %q: %q", contractSecret, final.Message.Content)
	}
	return "replay=" + mode
}

// 5. usage present on every response. mustChat enforces it on every call of
// every case; this case spells it out for the three response shapes (plain,
// tool call, answer after a tool result) and reports the counts.
func caseUsagePresent(t *testing.T, p Provider) string {
	plain := mustChat(t, p, ChatRequest{System: contractSystem, Messages: []Message{{Role: RoleUser, Content: contractPlainPrompt}}})
	call := mustChat(t, p, ChatRequest{
		System:   contractSystem,
		Messages: []Message{{Role: RoleUser, Content: contractSinglePrompt}},
		Tools:    []ToolSpec{lookupItemTool},
	})
	history := []Message{
		{Role: RoleUser, Content: contractRoundTripUser},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_contract_1", Name: lookupItemTool.Name, Arguments: json.RawMessage(`{"id":"item-42"}`)}}},
		{Role: RoleTool, ToolCallID: "call_contract_1", Content: toolResult("item-42")},
	}
	answer := mustChat(t, p, ChatRequest{System: contractRoundTripSys, Messages: history, Tools: []ToolSpec{lookupItemTool}})

	for name, r := range map[string]ChatResponse{"plain": plain, "tool_call": call, "after_tool_result": answer} {
		if r.Usage.InputTokens <= 0 || r.Usage.OutputTokens <= 0 {
			t.Errorf("%s: usage = %+v, want both counts > 0", name, r.Usage)
		}
	}
	return fmt.Sprintf("in/out plain=%d/%d tool_call=%d/%d after_tool=%d/%d",
		plain.Usage.InputTokens, plain.Usage.OutputTokens,
		call.Usage.InputTokens, call.Usage.OutputTokens,
		answer.Usage.InputTokens, answer.Usage.OutputTokens)
}

// --- CONTRACT_PROVIDERS parsing (shared with the real-endpoint file) -------

// contractEntry is one parsed CONTRACT_PROVIDERS element.
type contractEntry struct {
	Name    string // "openai" or "openai_compatible"
	Model   string
	BaseURL string // empty means the adapter default
	KeyEnv  string // env var holding the API key
}

var keyEnvRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseContractEntries parses
//
//	name:model[@base_url][#KEYENV],name:model[@base_url][#KEYENV],...
//
// The name ends at the FIRST ':' (models such as "llama3:8b" and base URLs
// contain ':' too), the base URL starts after the LAST '@', and the key env
// var name after the LAST '#'. Errors never echo the entry text, since a base
// URL may carry credentials.
func parseContractEntries(spec string) ([]contractEntry, error) {
	var out []contractEntry
	for i, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		e, err := parseContractEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("CONTRACT_PROVIDERS entry %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, errors.New("no provider entries")
	}
	return out, nil
}

func parseContractEntry(s string) (contractEntry, error) {
	name, rest, ok := strings.Cut(s, ":")
	if !ok {
		return contractEntry{}, errors.New("expected name:model")
	}
	if name != "openai" && name != "openai_compatible" {
		return contractEntry{}, fmt.Errorf("unknown provider name %q (want openai or openai_compatible)", name)
	}
	e := contractEntry{Name: name, KeyEnv: "CONTRACT_KEY_" + strings.ToUpper(name)}
	if i := strings.LastIndex(rest, "#"); i >= 0 {
		e.KeyEnv, rest = rest[i+1:], rest[:i]
		if !keyEnvRe.MatchString(e.KeyEnv) {
			return contractEntry{}, errors.New("key env var name after '#' is not a valid identifier")
		}
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		e.BaseURL, rest = rest[i+1:], rest[:i]
	}
	e.Model = rest
	if e.Model == "" {
		return contractEntry{}, errors.New("model is empty")
	}
	if e.Name == "openai_compatible" && e.BaseURL == "" {
		return contractEntry{}, errors.New("openai_compatible requires @base_url")
	}
	return e, nil
}

// buildContractProvider wires one entry to an adapter. When the key env var is
// not set at all it returns a skip reason instead of an error; a var set to
// the empty string is accepted (local servers without auth). The key is never
// logged or returned in messages.
func buildContractProvider(e contractEntry, lookupEnv func(string) (string, bool)) (p Provider, skipReason string, err error) {
	key, ok := lookupEnv(e.KeyEnv)
	if !ok {
		return nil, fmt.Sprintf("env var %s is not set", e.KeyEnv), nil
	}
	o, err := NewOpenAI(OpenAIConfig{Name: e.Name, BaseURL: e.BaseURL, APIKey: key, Model: e.Model})
	if err != nil {
		return nil, "", err
	}
	return o, "", nil
}

// --- deterministic OpenAI chat-completions fake ---------------------------

const contractFakeKey = "contract-fake-key"

var fakeItemIDRe = regexp.MustCompile(`item-[A-Za-z0-9]+`)

type fakeRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role      string  `json:"role"`
		Content   *string `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
}

// newContractFake is a minimal responder deterministic in the request alone:
// last message role tool -> plain answer echoing the tool content; last
// message user with tools present -> one tool call per distinct "item-XYZ" id in
// the prompt (none -> plain text); otherwise plain text. Like the real API it
// rejects a tool message whose tool_call_id no earlier assistant turn issued.
func uniqueItemIDs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range fakeItemIDRe.FindAllString(s, -1) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func newContractFake(t *testing.T) *httptest.Server {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			writeJSON(w, http.StatusNotFound, `{"error":{"message":"not found"}}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+contractFakeKey {
			writeJSON(w, http.StatusUnauthorized, `{"error":{"message":"bad key"}}`)
			return
		}
		var req fakeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || len(req.Messages) == 0 {
			writeJSON(w, http.StatusBadRequest, `{"error":{"message":"invalid request"}}`)
			return
		}
		issued := map[string]bool{}
		for _, m := range req.Messages {
			for _, tc := range m.ToolCalls {
				if tc.ID == "" || tc.Type != "function" || !json.Valid([]byte(tc.Function.Arguments)) {
					writeJSON(w, http.StatusBadRequest, `{"error":{"message":"invalid assistant tool call"}}`)
					return
				}
				issued[tc.ID] = true
			}
			if m.Role == "tool" && !issued[m.ToolCallID] {
				writeJSON(w, http.StatusBadRequest, `{"error":{"message":"invalid tool_call_id"}}`)
				return
			}
		}
		for _, tool := range req.Tools {
			if tool.Type != "function" || tool.Function.Name == "" || !json.Valid(tool.Function.Parameters) {
				writeJSON(w, http.StatusBadRequest, `{"error":{"message":"invalid tool"}}`)
				return
			}
		}

		last := req.Messages[len(req.Messages)-1]
		content := ""
		if last.Content != nil {
			content = *last.Content
		}
		usage := map[string]int{"prompt_tokens": 10 + len(req.Messages), "completion_tokens": 5}

		var message map[string]any
		finish := "stop"
		switch {
		case last.Role == "tool":
			message = map[string]any{"role": "assistant", "content": "The lookup returned: " + content}
		case last.Role == "user" && len(req.Tools) > 0 && len(uniqueItemIDs(content)) > 0:
			var calls []map[string]any
			for i, id := range uniqueItemIDs(content) {
				args, _ := json.Marshal(map[string]string{"id": id})
				calls = append(calls, map[string]any{
					"id":       fmt.Sprintf("call_fake_%d", i),
					"type":     "function",
					"function": map[string]string{"name": "lookup_item", "arguments": string(args)},
				})
			}
			message = map[string]any{"role": "assistant", "content": nil, "tool_calls": calls}
			finish = "tool_calls"
		default:
			message = map[string]any{"role": "assistant", "content": "Hello, happy to help."}
		}
		body, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": message, "finish_reason": finish}},
			"usage":   usage,
		})
		writeJSON(w, http.StatusOK, string(body))
	})
}

func newFakeProvider(t *testing.T) *OpenAI {
	t.Helper()
	srv := newContractFake(t)
	p, err := NewOpenAI(OpenAIConfig{Name: "openai", BaseURL: srv.URL + "/v1", APIKey: contractFakeKey, Model: "fake-model"})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	p.sleep = (&sleepRecorder{}).sleep
	return p
}

// --- CI tests --------------------------------------------------------------

func TestContractFake(t *testing.T) {
	runContract(t, newFakeProvider(t))
}

// The fake is deterministic, so beyond the tolerant table the parallel case can
// be pinned exactly: two calls, distinct IDs, emitted order preserved.
func TestContractFakeParallelOrder(t *testing.T) {
	p := newFakeProvider(t)
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: contractParallelPrompt}},
		Tools:    []ToolSpec{lookupItemTool},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(resp.ToolCalls))
	}
	if err := validateCalls(resp.ToolCalls); err != nil {
		t.Fatalf("validateCalls: %v", err)
	}
	for i, want := range []string{"item-A", "item-B"} {
		got, err := itemID(resp.ToolCalls[i])
		if err != nil || got != want {
			t.Errorf("call %d id = %q (err %v), want %q", i, got, err, want)
		}
	}
}

// The assertions must be able to fail: a provider that hands out colliding or
// malformed tool calls is rejected.
func TestValidateCalls(t *testing.T) {
	ok := ToolCall{ID: "a", Name: "lookup_item", Arguments: json.RawMessage(`{"id":"x"}`)}
	tests := []struct {
		name    string
		calls   []ToolCall
		wantErr bool
	}{
		{"valid pair", []ToolCall{ok, {ID: "b", Name: "lookup_item", Arguments: json.RawMessage(`{}`)}}, false},
		{"duplicate ids", []ToolCall{ok, ok}, true},
		{"empty id", []ToolCall{{Name: "n", Arguments: json.RawMessage(`{}`)}}, true},
		{"empty name", []ToolCall{{ID: "a", Arguments: json.RawMessage(`{}`)}}, true},
		{"arguments not an object", []ToolCall{{ID: "a", Name: "n", Arguments: json.RawMessage(`"oops"`)}}, true},
		{"arguments null", []ToolCall{{ID: "a", Name: "n", Arguments: json.RawMessage(`null`)}}, true},
		{"arguments missing", []ToolCall{{ID: "a", Name: "n"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCalls(tt.calls); (err != nil) != tt.wantErr {
				t.Fatalf("validateCalls err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseContractEntries(t *testing.T) {
	got, err := parseContractEntries(
		"openai:gpt-4.1-mini, openai_compatible:gemini-2.5-flash@https://generativelanguage.googleapis.com/v1beta/openai/#CONTRACT_KEY_GEMINI," +
			"openai_compatible:llama3:8b@http://localhost:11434/v1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []contractEntry{
		{Name: "openai", Model: "gpt-4.1-mini", KeyEnv: "CONTRACT_KEY_OPENAI"},
		{Name: "openai_compatible", Model: "gemini-2.5-flash", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", KeyEnv: "CONTRACT_KEY_GEMINI"},
		{Name: "openai_compatible", Model: "llama3:8b", BaseURL: "http://localhost:11434/v1", KeyEnv: "CONTRACT_KEY_OPENAI_COMPATIBLE"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries =\n%+v\nwant\n%+v", got, want)
	}

	for _, bad := range []string{
		"", " , ", "openai", "anthropic:m", "openai:", "openai_compatible:m",
		"openai_compatible:@http://x", "openai:m#bad-name",
	} {
		if _, err := parseContractEntries(bad); err == nil {
			t.Errorf("parseContractEntries(%q) = nil error, want error", bad)
		}
	}
}

func TestBuildContractProvider(t *testing.T) {
	e := contractEntry{Name: "openai_compatible", Model: "m", BaseURL: "http://localhost:1/v1", KeyEnv: "K"}

	p, skip, err := buildContractProvider(e, func(string) (string, bool) { return "", false })
	if p != nil || err != nil || !strings.Contains(skip, "K") {
		t.Fatalf("unset key: p=%v skip=%q err=%v, want skip naming K", p, skip, err)
	}

	p, skip, err = buildContractProvider(e, func(string) (string, bool) { return "secret-value", true })
	if err != nil || skip != "" || p == nil || p.Name() != "openai_compatible" || p.Model() != "m" {
		t.Fatalf("set key: p=%v skip=%q err=%v", p, skip, err)
	}

	// An empty but set key is allowed (local servers without auth).
	if p, _, err = buildContractProvider(e, func(string) (string, bool) { return "", true }); err != nil || p == nil {
		t.Fatalf("empty key: p=%v err=%v", p, err)
	}

	bad := e
	bad.BaseURL = "not a url"
	if _, _, err = buildContractProvider(bad, func(string) (string, bool) { return "secret-value", true }); err == nil {
		t.Fatal("invalid base URL: want error")
	} else if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), bad.BaseURL) {
		t.Fatalf("error leaks input: %v", err)
	}
}

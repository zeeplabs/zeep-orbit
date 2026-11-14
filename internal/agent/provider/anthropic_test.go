package provider

// anthropic_test.go — the Anthropic Messages adapter against httptest servers
// (ai-builder-costs C06): request mapping, response mapping, error mapping and
// the shared provider contract.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newAnthropicTest(t *testing.T, srv *httptest.Server) (*Anthropic, *sleepRecorder) {
	t.Helper()
	p, err := NewAnthropic(AnthropicConfig{BaseURL: srv.URL, APIKey: testAPIKey, Model: "claude-test"})
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	rec := &sleepRecorder{}
	p.sleep = rec.sleep
	p.backoffBase = 10 * time.Millisecond
	return p, rec
}

const anOK = `{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`

// captureAnthropic serves anOK and hands back the decoded request body and
// headers of the last call.
func captureAnthropic(t *testing.T) (*Anthropic, *map[string]any, *http.Header) {
	t.Helper()
	body := map[string]any{}
	header := http.Header{}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = map[string]any{}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		header = r.Header.Clone()
		if r.URL.Path != "/v1/messages" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want POST /v1/messages", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, anOK)
	})
	p, _ := newAnthropicTest(t, srv)
	return p, &body, &header
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNewAnthropicValidation(t *testing.T) {
	for name, cfg := range map[string]AnthropicConfig{
		"no model": {APIKey: "k"},
		"no key":   {Model: "m"},
		"bad url":  {Model: "m", APIKey: testAPIKey, BaseURL: "not a url"},
	} {
		_, err := NewAnthropic(cfg)
		if err == nil {
			t.Errorf("%s: want error", name)
			continue
		}
		if strings.Contains(err.Error(), testAPIKey) || strings.Contains(err.Error(), "not a url") {
			t.Errorf("%s: error leaks input: %v", name, err)
		}
	}
	p, err := NewAnthropic(AnthropicConfig{Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "anthropic" || p.Model() != "m" || p.endpoint != "https://api.anthropic.com/v1/messages" {
		t.Errorf("name/model/endpoint = %q/%q/%q", p.Name(), p.Model(), p.endpoint)
	}
}

// Headers, the system prompt, the required max_tokens, tools and a full tool
// round: an assistant turn with text and two calls, and the two results merged
// into one user message.
func TestAnthropicRequestShape(t *testing.T) {
	p, body, header := captureAnthropic(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		System: "be brief",
		Messages: []Message{
			{Role: RoleUser, Content: "look up two items"},
			{Role: RoleAssistant, Content: "On it.", ToolCalls: []ToolCall{
				{ID: "toolu_1", Name: "lookup_item", Arguments: json.RawMessage(`{"id":"a"}`)},
				{ID: "toolu_2", Name: "lookup_item", Arguments: json.RawMessage(`{"id":"b"}`)},
			}},
			{Role: RoleTool, ToolCallID: "toolu_1", Content: "label a"},
			{Role: RoleTool, ToolCallID: "toolu_2", Content: "label b"},
			{Role: RoleUser, Content: "thanks, now summarize"},
		},
		Tools: []ToolSpec{lookupItemTool, {Name: "no_schema", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got := header.Get("x-api-key"); got != testAPIKey {
		t.Errorf("x-api-key header not sent as configured")
	}
	if header.Get("anthropic-version") != "2026-10-21" || header.Get("Authorization") != "" {
		t.Errorf("headers: version=%q authorization=%q", header.Get("anthropic-version"), header.Get("Authorization"))
	}
	b := *body
	if b["model"] != "claude-test" || b["system"] != "be brief" || b["max_tokens"] != float64(16000) {
		t.Errorf("model/system/max_tokens = %v/%v/%v", b["model"], b["system"], b["max_tokens"])
	}
	for _, banned := range []string{"temperature", "top_p", "top_k", "thinking", "tool_choice", "stream"} {
		if _, ok := b[banned]; ok {
			t.Errorf("request carries %q", banned)
		}
	}

	wantMessages := `[` +
		`{"content":[{"text":"look up two items","type":"text"}],"role":"user"},` +
		`{"content":[{"text":"On it.","type":"text"},` +
		`{"id":"toolu_1","input":{"id":"a"},"name":"lookup_item","type":"tool_use"},` +
		`{"id":"toolu_2","input":{"id":"b"},"name":"lookup_item","type":"tool_use"}],"role":"assistant"},` +
		`{"content":[{"content":"label a","tool_use_id":"toolu_1","type":"tool_result"},` +
		`{"content":"label b","tool_use_id":"toolu_2","type":"tool_result"},` +
		`{"text":"thanks, now summarize","type":"text"}],"role":"user"}]`
	if got := mustJSON(t, b["messages"]); got != wantMessages {
		t.Errorf("messages =\n%s\nwant\n%s", got, wantMessages)
	}

	tools, _ := b["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", b["tools"])
	}
	first := tools[0].(map[string]any)
	if first["name"] != "lookup_item" || first["description"] == "" || first["input_schema"].(map[string]any)["type"] != "object" {
		t.Errorf("first tool = %v", first)
	}
	if got := mustJSON(t, tools[1].(map[string]any)["input_schema"]); got != `{"properties":{},"type":"object"}` {
		t.Errorf("tool without a schema: input_schema = %s", got)
	}
}

// A loop step asks for the request-level breakpoint so the next step reads its
// prefix; a one-off call must not, or it pays the write premium for nothing.
func TestAnthropicCacheHint(t *testing.T) {
	msgs := []Message{{Role: RoleUser, Content: "hi"}}

	p, body, _ := captureAnthropic(t)
	if _, err := p.Chat(context.Background(), ChatRequest{System: "s", Messages: msgs, CacheHint: true}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := mustJSON(t, (*body)["cache_control"]); got != `{"type":"ephemeral"}` {
		t.Errorf("cache_control with the hint = %s, want the default 5 minute ephemeral entry", got)
	}

	if _, err := p.Chat(context.Background(), ChatRequest{System: "s", Messages: msgs}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, ok := (*body)["cache_control"]; ok {
		t.Errorf("cache_control sent without the hint: %s", mustJSON(t, (*body)["cache_control"]))
	}
}

func TestAnthropicMaxTokens(t *testing.T) {
	for in, want := range map[int]float64{0: 16000, 1200: 4096, 4096: 4096, 32000: 32000} {
		p, body, _ := captureAnthropic(t)
		req := userHi
		req.MaxOutputTokens = in
		if _, err := p.Chat(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if got := (*body)["max_tokens"]; got != want {
			t.Errorf("MaxOutputTokens %d: max_tokens = %v, want %v", in, got, want)
		}
	}
}

// Empty text never becomes a block, tool-call-only turns have no text block,
// a system message in the history joins the system prompt, and an empty
// message is dropped (merging what is around it).
func TestAnthropicRequestEdgeCases(t *testing.T) {
	p, body, _ := captureAnthropic(t)
	_, err := p.Chat(context.Background(), ChatRequest{
		System: "base",
		Messages: []Message{
			{Role: RoleUser, Content: "one"},
			{Role: RoleSystem, Content: "extra rule"},
			{Role: RoleAssistant},
			{Role: RoleUser, Content: "two"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "t"}}},
			{Role: RoleTool, ToolCallID: "c1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := *body
	if b["system"] != "base\n\nextra rule" {
		t.Errorf("system = %q", b["system"])
	}
	want := `[` +
		`{"content":[{"text":"one","type":"text"},{"text":"two","type":"text"}],"role":"user"},` +
		`{"content":[{"id":"c1","input":{},"name":"t","type":"tool_use"}],"role":"assistant"},` +
		`{"content":[{"tool_use_id":"c1","type":"tool_result"}],"role":"user"}]`
	if got := mustJSON(t, b["messages"]); got != want {
		t.Errorf("messages =\n%s\nwant\n%s", got, want)
	}
	if _, ok := b["tools"]; ok {
		t.Errorf("tools sent without any tool")
	}
}

// Arguments that are not a JSON object (kept as a JSON string by an adapter
// when a model emitted invalid JSON) are replayed as an empty input with the
// original text beside the call.
func TestAnthropicInvalidToolArgumentsAreReplayable(t *testing.T) {
	p, body, _ := captureAnthropic(t)
	_, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Name: "write_file", Arguments: json.RawMessage(`"{\"path\": \"a.ts\", "`)},
			{ID: "c2", Name: "read_file", Arguments: json.RawMessage(`[1,2]`)},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "invalid arguments"},
		{Role: RoleTool, ToolCallID: "c2", Content: "invalid arguments"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	msgs := (*body)["messages"].([]any)
	blocks := msgs[1].(map[string]any)["content"].([]any)
	if len(blocks) != 4 {
		t.Fatalf("assistant blocks = %s, want 2 text + 2 tool_use", mustJSON(t, blocks))
	}
	for i, wantType := range []string{"text", "text", "tool_use", "tool_use"} {
		blk := blocks[i].(map[string]any)
		if blk["type"] != wantType {
			t.Errorf("block %d type = %v, want %s", i, blk["type"], wantType)
		}
		if wantType == "tool_use" && mustJSON(t, blk["input"]) != `{}` {
			t.Errorf("block %d input = %s, want {}", i, mustJSON(t, blk["input"]))
		}
	}
	if text := blocks[0].(map[string]any)["text"].(string); !strings.Contains(text, "write_file") || !strings.Contains(text, "a.ts") {
		t.Errorf("first note = %q, want the tool name and the original text", text)
	}
}

// A history produced by another provider replays here: ids with characters the
// API does not take are rewritten the same way on the call and on its result.
func TestAnthropicReplaysForeignToolIDs(t *testing.T) {
	p, body, _ := captureAnthropic(t)
	_, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call:1.a/b", Name: "t", Arguments: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "call:1.a/b", Content: "r"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	msgs := (*body)["messages"].([]any)
	use := msgs[1].(map[string]any)["content"].([]any)[0].(map[string]any)["id"]
	res := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)["tool_use_id"]
	if use != "call_1_a_b" || res != use {
		t.Errorf("tool_use id = %v, tool_result id = %v, want both call_1_a_b", use, res)
	}
	if anToolID("") != "call_unknown" || anToolID("toolu_01Ab-9") != "toolu_01Ab-9" {
		t.Errorf("anToolID edge cases: %q %q", anToolID(""), anToolID("toolu_01Ab-9"))
	}
}

// What this adapter returns replays through the OpenAI adapter and back: the
// neutral history is all either of them needs.
func TestCrossProviderHistoryReplay(t *testing.T) {
	anSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"type":"message","content":[{"type":"thinking","thinking":"","signature":"sig"},`+
			`{"type":"text","text":"Checking."},{"type":"tool_use","id":"toolu_9","name":"lookup_item","input":{"id":"item-42"}}],`+
			`"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":4}}`)
	})
	an, _ := newAnthropicTest(t, anSrv)
	first, err := an.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatal(err)
	}
	history := []Message{userHi.Messages[0], first.Message, {Role: RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: "label"}}

	var oaBody map[string]any
	oaSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&oaBody)
		writeJSON(w, 200, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call_a","type":"function",`+
			`"function":{"name":"lookup_item","arguments":"{\"id\":\"item-7\"}"}}]},"finish_reason":"tool_calls"}]}`)
	})
	oa, _ := newTestProvider(t, oaSrv)
	second, err := oa.Chat(context.Background(), ChatRequest{Messages: history})
	if err != nil {
		t.Fatalf("OpenAI adapter rejected a history produced by the Anthropic adapter: %v", err)
	}
	oaMsgs := oaBody["messages"].([]any)
	call := oaMsgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "toolu_9" || call["function"].(map[string]any)["arguments"] != `{"id":"item-42"}` {
		t.Errorf("OpenAI replay of the Anthropic call = %v", call)
	}
	if oaMsgs[2].(map[string]any)["tool_call_id"] != "toolu_9" {
		t.Errorf("OpenAI replay of the tool result = %v", oaMsgs[2])
	}

	// And back: the OpenAI turn is appended and sent to Anthropic.
	history = append(history, second.Message, Message{Role: RoleTool, ToolCallID: second.ToolCalls[0].ID, Content: "label 7"})
	p, body, _ := captureAnthropic(t)
	if _, err := p.Chat(context.Background(), ChatRequest{Messages: history}); err != nil {
		t.Fatal(err)
	}
	msgs := (*body)["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("messages = %s, want user/assistant/user/assistant/user", mustJSON(t, msgs))
	}
	want := `{"content":[{"id":"call_a","input":{"id":"item-7"},"name":"lookup_item","type":"tool_use"}],"role":"assistant"}`
	if got := mustJSON(t, msgs[3]); got != want {
		t.Errorf("Anthropic replay of the OpenAI call =\n%s\nwant\n%s", got, want)
	}
}

func TestAnthropicResponseMapping(t *testing.T) {
	usage := `"usage":{"input_tokens":3,"output_tokens":2}`
	tests := []struct {
		name       string
		body       string
		content    string
		calls      []ToolCall
		wantFinish string
	}{
		{"text", `{"type":"message","content":[{"type":"text","text":"Hello"},{"type":"text","text":" there"}],"stop_reason":"end_turn",` + usage + `}`,
			"Hello there", nil, FinishStop},
		{"thinking is dropped", `{"type":"message","content":[{"type":"thinking","thinking":"secret plan","signature":"s"},{"type":"text","text":"Done"}],"stop_reason":"end_turn",` + usage + `}`,
			"Done", nil, FinishStop},
		{"tool calls keep order", `{"type":"message","content":[{"type":"text","text":"Let me check."},` +
			`{"type":"tool_use","id":"toolu_1","name":"a","input":{"x":1}},{"type":"tool_use","id":"toolu_2","name":"b","input":{}}],"stop_reason":"tool_use",` + usage + `}`,
			"Let me check.", []ToolCall{
				{ID: "toolu_1", Name: "a", Arguments: json.RawMessage(`{"x":1}`)},
				{ID: "toolu_2", Name: "b", Arguments: json.RawMessage(`{}`)},
			}, FinishToolCalls},
		{"tool call without input", `{"type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"a"}],"stop_reason":"tool_use",` + usage + `}`,
			"", []ToolCall{{ID: "toolu_1", Name: "a", Arguments: json.RawMessage(`{}`)}}, FinishToolCalls},
		{"calls with end_turn are tool_calls", `{"type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"a","input":{}}],"stop_reason":"end_turn",` + usage + `}`,
			"", []ToolCall{{ID: "toolu_1", Name: "a", Arguments: json.RawMessage(`{}`)}}, FinishToolCalls},
		{"max_tokens", `{"type":"message","content":[{"type":"text","text":"cut"}],"stop_reason":"max_tokens",` + usage + `}`, "cut", nil, FinishLength},
		{"context window", `{"type":"message","content":[],"stop_reason":"model_context_window_exceeded",` + usage + `}`, "", nil, FinishLength},
		{"stop sequence", `{"type":"message","content":[{"type":"text","text":"x"}],"stop_reason":"stop_sequence",` + usage + `}`, "x", nil, FinishStop},
		{"refusal", `{"type":"message","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},` + usage + `}`, "", nil, FinishFilter},
		{"unknown reason passes through", `{"type":"message","content":[{"type":"text","text":"x"}],"stop_reason":"pause_turn",` + usage + `}`, "x", nil, "pause_turn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, tt.body) })
			p, _ := newAnthropicTest(t, srv)
			resp, err := p.Chat(context.Background(), userHi)
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if resp.Message.Role != RoleAssistant || resp.Message.Content != tt.content {
				t.Errorf("message = %+v, want assistant %q", resp.Message, tt.content)
			}
			if !reflect.DeepEqual(resp.ToolCalls, tt.calls) || !reflect.DeepEqual(resp.Message.ToolCalls, tt.calls) {
				t.Errorf("tool calls = %+v / %+v, want %+v", resp.ToolCalls, resp.Message.ToolCalls, tt.calls)
			}
			if resp.FinishReason != tt.wantFinish {
				t.Errorf("finish = %q, want %q", resp.FinishReason, tt.wantFinish)
			}
			if strings.Contains(mustJSON(t, resp), "secret plan") {
				t.Errorf("thinking text leaked into the neutral response")
			}
		})
	}
}

func TestAnthropicUsageTokenClasses(t *testing.T) {
	tests := []struct {
		name  string
		usage string
		want  Usage
	}{
		{"no cache fields", `"usage":{"input_tokens":120,"output_tokens":30}`, Usage{InputTokens: 120, OutputTokens: 30, Reported: true}},
		{"cache read and write", `"usage":{"input_tokens":20,"cache_read_input_tokens":900,"cache_creation_input_tokens":150,"output_tokens":30}`,
			Usage{InputTokens: 20, CachedInputTokens: 900, CacheWriteTokens: 150, OutputTokens: 30, Reported: true}},
		{"no usage object", `"x":1`, Usage{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, `{"type":"message","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",`+tt.usage+`}`)
			})
			p, _ := newAnthropicTest(t, srv)
			resp, err := p.Chat(context.Background(), userHi)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Usage != tt.want {
				t.Errorf("usage = %+v, want %+v", resp.Usage, tt.want)
			}
		})
	}
}

func anErrBody(typ, msg string) string {
	return fmt.Sprintf(`{"type":"error","error":{"type":%q,"message":%q},"request_id":"req_1"}`, typ, msg+" "+bodySecret)
}

func TestAnthropicErrorMapping(t *testing.T) {
	var pe *ErrProvider
	tests := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
		calls  int32
	}{
		{"401", 401, anErrBody("authentication_error", "invalid x-api-key"), func(err error) bool { return errors.Is(err, ErrAuth) }, 1},
		{"403", 403, anErrBody("permission_error", "not allowed"), func(err error) bool { return errors.Is(err, ErrAuth) }, 1},
		{"prompt too long", 400, anErrBody("invalid_request_error", "prompt is too long: 1200000 tokens > 1000000 maximum"),
			func(err error) bool { return errors.Is(err, ErrContextTooLong) }, 1},
		{"context window wording", 400, anErrBody("invalid_request_error", "input length and max_tokens exceed context limit"),
			func(err error) bool { return errors.Is(err, ErrContextTooLong) }, 1},
		{"413", 413, anErrBody("request_too_large", "request exceeds the maximum size"),
			func(err error) bool { return errors.Is(err, ErrContextTooLong) }, 1},
		{"other 400", 400, anErrBody("invalid_request_error", "messages: roles must alternate"),
			func(err error) bool {
				return errors.As(err, &pe) && pe.Status == 400 && !errors.Is(err, ErrContextTooLong)
			}, 1},
		{"400 of another type mentioning context", 400, anErrBody("billing_error", "context window add-on not purchased"),
			func(err error) bool { return errors.As(err, &pe) && !errors.Is(err, ErrContextTooLong) }, 1},
		{"404 model", 404, anErrBody("not_found_error", "model: nope"), func(err error) bool { return errors.As(err, &pe) && pe.Status == 404 }, 1},
		{"500 is retried", 500, anErrBody("api_error", "boom"), func(err error) bool { return errors.As(err, &pe) && pe.Status == 500 }, 3},
		{"529 is retried", 529, anErrBody("overloaded_error", "overloaded"), func(err error) bool { return errors.As(err, &pe) && pe.Status == 529 }, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				writeJSON(w, tt.status, tt.body)
			})
			p, _ := newAnthropicTest(t, srv)
			_, err := p.Chat(context.Background(), userHi)
			if err == nil || !tt.check(err) {
				t.Fatalf("err = %v (%T)", err, err)
			}
			assertNoLeak(t, err)
			if got := atomic.LoadInt32(&calls); got != tt.calls {
				t.Errorf("calls = %d, want %d", got, tt.calls)
			}
		})
	}
}

func TestAnthropicRateLimit(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "2")
			writeJSON(w, 429, anErrBody("rate_limit_error", "slow down"))
			return
		}
		writeJSON(w, 200, anOK)
	})
	p, rec := newAnthropicTest(t, srv)
	resp, err := p.Chat(context.Background(), userHi)
	if err != nil || resp.Message.Content != "ok" {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if want := []time.Duration{2 * time.Second}; !reflect.DeepEqual(rec.waits, want) {
		t.Errorf("waits = %v, want Retry-After honored: %v", rec.waits, want)
	}

	// Always limited: retries are used up and the wait is reported.
	srv = serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, 429, anErrBody("rate_limit_error", "slow down"))
	})
	p, _ = newAnthropicTest(t, srv)
	_, err = p.Chat(context.Background(), userHi)
	var rl *ErrRateLimited
	if !errors.As(err, &rl) || rl.RetryAfter != time.Second {
		t.Fatalf("err = %v, want *ErrRateLimited with 1s", err)
	}
	assertNoLeak(t, err)
}

func TestAnthropicMalformedResponses(t *testing.T) {
	for name, body := range map[string]string{
		"not json":           `<html>` + bodySecret + `</html>`,
		"not a message":      `{"type":"something_else","note":"` + bodySecret + `"}`,
		"error with 200":     anErrBody("api_error", "late failure"),
		"empty object":       `{}`,
		"wrong content type": `{"type":"message","content":"text"}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, body) })
			p, _ := newAnthropicTest(t, srv)
			_, err := p.Chat(context.Background(), userHi)
			var pe *ErrProvider
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v (%T), want *ErrProvider", err, err)
			}
			assertNoLeak(t, err)
		})
	}

	// The too-long condition delivered as an error object with a 200.
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, anErrBody("invalid_request_error", "prompt is too long"))
	})
	p, _ := newAnthropicTest(t, srv)
	if _, err := p.Chat(context.Background(), userHi); !errors.Is(err, ErrContextTooLong) {
		t.Errorf("err = %v, want ErrContextTooLong", err)
	}
}

// The response size guard: a body larger than the limit is cut and fails to
// decode instead of being held in memory whole.
func TestAnthropicResponseSizeGuard(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"`)
		chunk := strings.Repeat("a", 1<<20)
		for i := 0; i < (maxResponseBytes>>20)+1; i++ {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `"}],"stop_reason":"end_turn"}`)
	})
	p, _ := newAnthropicTest(t, srv)
	_, err := p.Chat(context.Background(), userHi)
	var pe *ErrProvider
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *ErrProvider for an oversized body", err)
	}
	if len(pe.Body) > maxErrorBodyBytes {
		t.Errorf("kept %d bytes of the body, want at most %d", len(pe.Body), maxErrorBodyBytes)
	}
}

func TestAnthropicContextCancelled(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 500, anErrBody("api_error", "boom")) })
	p, _ := newAnthropicTest(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	p.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := p.Chat(ctx, userHi)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled through *ErrProvider", err)
	}
}

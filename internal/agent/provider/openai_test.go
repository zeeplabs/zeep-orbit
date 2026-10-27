package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testAPIKey = "sk-test-key-DO-NOT-LEAK"
	// bodySecret stands in for any upstream payload fragment (prompt echo,
	// account id, ...) that must stay out of error text.
	bodySecret = "SECRET-UPSTREAM-PAYLOAD-123"
)

// sleepRecorder replaces real sleeping so retry tests run instantly and can
// assert the waits the adapter chose.
type sleepRecorder struct{ waits []time.Duration }

func (s *sleepRecorder) sleep(_ context.Context, d time.Duration) error {
	s.waits = append(s.waits, d)
	return nil
}

func newTestProvider(t *testing.T, srv *httptest.Server) (*OpenAI, *sleepRecorder) {
	t.Helper()
	p, err := NewOpenAI(OpenAIConfig{BaseURL: srv.URL + "/v1", APIKey: testAPIKey, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	rec := &sleepRecorder{}
	p.sleep = rec.sleep
	p.backoffBase = 10 * time.Millisecond
	return p, rec
}

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

var userHi = ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}

// assertNoLeak fails if err's text exposes the API key or upstream body data.
func assertNoLeak(t *testing.T, err error) {
	t.Helper()
	for _, s := range []string{testAPIKey, bodySecret} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error text leaks %q: %q", s, err.Error())
		}
	}
}

func TestChatPlainMessage(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"choices":[{"message":{"role":"assistant","content":"hello there"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	})
	p, _ := newTestProvider(t, srv)

	resp, err := p.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Role != RoleAssistant || resp.Message.Content != "hello there" {
		t.Fatalf("message = %+v", resp.Message)
	}
	if len(resp.ToolCalls) != 0 || len(resp.Message.ToolCalls) != 0 {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
	if resp.FinishReason != FinishStop {
		t.Fatalf("finish = %q", resp.FinishReason)
	}
}

func TestChatSingleToolCall(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
			{"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"src/App.tsx\"}"}}]},
			"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	})
	p, _ := newTestProvider(t, srv)

	resp, err := p.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Fatalf("finish = %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_a" || tc.Name != "read_file" || string(tc.Arguments) != `{"path":"src/App.tsx"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if !reflect.DeepEqual(resp.Message.ToolCalls, resp.ToolCalls) {
		t.Fatalf("Message.ToolCalls must mirror ToolCalls")
	}
}

func TestChatParallelToolCallsKeepOrder(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"choices":[{"message":{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"first","arguments":"{\"n\":1}"}},
			{"id":"c2","type":"function","function":{"name":"second","arguments":"{\"n\":2}"}},
			{"id":"c3","type":"function","function":{"name":"third","arguments":"{}"}}]},
			"finish_reason":"tool_calls"}]}`)
	})
	p, _ := newTestProvider(t, srv)

	resp, err := p.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var got []string
	for _, tc := range resp.ToolCalls {
		got = append(got, tc.ID+":"+tc.Name)
	}
	if want := []string{"c1:first", "c2:second", "c3:third"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestChatUsagePopulated(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"choices":[{"message":{"content":"x"},"finish_reason":"length"}],"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}}`)
	})
	p, _ := newTestProvider(t, srv)

	resp, err := p.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage != (Usage{InputTokens: 123, OutputTokens: 45}) {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if resp.FinishReason != FinishLength {
		t.Fatalf("finish = %q", resp.FinishReason)
	}
}

func TestChatAuthError(t *testing.T) {
	for _, status := range []int{401, 403} {
		srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, status, `{"error":{"message":"bad key `+bodySecret+`"}}`)
		})
		p, rec := newTestProvider(t, srv)

		_, err := p.Chat(context.Background(), userHi)
		if !errors.Is(err, ErrAuth) {
			t.Fatalf("status %d: err = %v, want ErrAuth", status, err)
		}
		assertNoLeak(t, err)
		if len(rec.waits) != 0 {
			t.Fatalf("auth errors must not be retried, waits = %v", rec.waits)
		}
	}
}

func TestChatRateLimitedRetryThenSuccess(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "2")
			writeJSON(w, 429, `{"error":{"message":"slow down"}}`)
			return
		}
		writeJSON(w, 200, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	p, rec := newTestProvider(t, srv)

	resp, err := p.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "ok" || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("content=%q calls=%d", resp.Message.Content, calls)
	}
	if want := []time.Duration{2 * time.Second}; !reflect.DeepEqual(rec.waits, want) {
		t.Fatalf("waits = %v, want Retry-After honored: %v", rec.waits, want)
	}
}

func TestChatRateLimitedExhausted(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "1")
		writeJSON(w, 429, `{"error":{"message":"`+bodySecret+`"}}`)
	})
	p, rec := newTestProvider(t, srv)

	_, err := p.Chat(context.Background(), userHi)
	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *ErrRateLimited", err)
	}
	if rl.RetryAfter != time.Second {
		t.Fatalf("RetryAfter = %v", rl.RetryAfter)
	}
	if atomic.LoadInt32(&calls) != maxAttempts || len(rec.waits) != maxAttempts-1 {
		t.Fatalf("calls=%d waits=%v, want %d attempts", calls, rec.waits, maxAttempts)
	}
	assertNoLeak(t, err)
}

func TestChatRateLimitedHugeRetryAfterNotBlocked(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, 429, `{}`)
	})
	p, rec := newTestProvider(t, srv)

	_, err := p.Chat(context.Background(), userHi)
	var rl *ErrRateLimited
	if !errors.As(err, &rl) || rl.RetryAfter != time.Hour {
		t.Fatalf("err = %v, want *ErrRateLimited{1h}", err)
	}
	if atomic.LoadInt32(&calls) != 1 || len(rec.waits) != 0 {
		t.Fatalf("must return immediately: calls=%d waits=%v", calls, rec.waits)
	}
}

func TestChatServerErrorRetriesWithBackoff(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			writeJSON(w, 503, `{"error":"`+bodySecret+`"}`)
			return
		}
		writeJSON(w, 200, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	p, rec := newTestProvider(t, srv)

	if _, err := p.Chat(context.Background(), userHi); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}; !reflect.DeepEqual(rec.waits, want) {
		t.Fatalf("waits = %v, want exponential %v", rec.waits, want)
	}
}

func TestChatServerErrorExhausted(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, 500, `{"error":"`+bodySecret+`"}`)
	})
	p, _ := newTestProvider(t, srv)

	_, err := p.Chat(context.Background(), userHi)
	var pe *ErrProvider
	if !errors.As(err, &pe) || pe.Status != 500 {
		t.Fatalf("err = %v, want *ErrProvider{500}", err)
	}
	if atomic.LoadInt32(&calls) != maxAttempts {
		t.Fatalf("calls = %d", calls)
	}
	assertNoLeak(t, err)
	if !strings.Contains(pe.Body, bodySecret) {
		t.Fatalf("Body must stay available for server logs, got %q", pe.Body)
	}
}

func TestChatMalformedJSON(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"choices": [`+bodySecret)
	})
	p, rec := newTestProvider(t, srv)

	_, err := p.Chat(context.Background(), userHi)
	var pe *ErrProvider
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *ErrProvider", err)
	}
	if pe.Unwrap() == nil {
		t.Fatalf("raw decode error must be wrapped for logs")
	}
	assertNoLeak(t, err)
	if len(rec.waits) != 0 {
		t.Fatalf("malformed 200 must not be retried, waits = %v", rec.waits)
	}
}

func TestChatNoChoicesAndErrorObject(t *testing.T) {
	for name, body := range map[string]string{
		"no choices":   `{"choices":[]}`,
		"error object": `{"error":{"message":"boom"}}`,
	} {
		srv := serve(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, body) })
		p, _ := newTestProvider(t, srv)
		_, err := p.Chat(context.Background(), userHi)
		var pe *ErrProvider
		if !errors.As(err, &pe) {
			t.Fatalf("%s: err = %v, want *ErrProvider", name, err)
		}
	}
}

func TestChatContextTooLong(t *testing.T) {
	bodies := map[string]string{
		"code":    `{"error":{"code":"context_length_exceeded","message":"` + bodySecret + `"}}`,
		"message": `{"error":{"message":"This model's maximum context length is 128000 tokens. ` + bodySecret + `"}}`,
	}
	for name, body := range bodies {
		srv := serve(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 400, body) })
		p, rec := newTestProvider(t, srv)

		_, err := p.Chat(context.Background(), userHi)
		if !errors.Is(err, ErrContextTooLong) {
			t.Fatalf("%s: err = %v, want ErrContextTooLong", name, err)
		}
		assertNoLeak(t, err)
		if len(rec.waits) != 0 {
			t.Fatalf("%s: must not retry", name)
		}
	}
}

func TestChatOtherBadRequestIsProviderError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 400, `{"error":{"message":"invalid tool schema `+bodySecret+`"}}`)
	})
	p, _ := newTestProvider(t, srv)

	_, err := p.Chat(context.Background(), userHi)
	var pe *ErrProvider
	if !errors.As(err, &pe) || pe.Status != 400 {
		t.Fatalf("err = %v, want *ErrProvider{400}", err)
	}
	assertNoLeak(t, err)
}

func TestChatInsufficientQuotaNotRetried(t *testing.T) {
	var calls int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, 429, `{"error":{"code":"insufficient_quota","message":"billing"}}`)
	})
	p, _ := newTestProvider(t, srv)

	_, err := p.Chat(context.Background(), userHi)
	var pe *ErrProvider
	if !errors.As(err, &pe) || pe.Status != 429 || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("err = %v calls = %d, want single *ErrProvider{429}", err, calls)
	}
}

func TestChatContextCancelledDuringBackoff(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		writeJSON(w, 429, `{}`)
	})
	p, err := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "m"}) // real sleep
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = p.Chat(ctx, userHi)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("backoff ignored ctx, took %v", elapsed)
	}
	var pe *ErrProvider
	if !errors.As(err, &pe) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want *ErrProvider wrapping DeadlineExceeded", err)
	}
}

func TestChatRequestShape(t *testing.T) {
	var (
		gotPath, gotAuth, gotCT string
		gotBody                 map[string]any
	)
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotCT = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeJSON(w, 200, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
	})
	// Trailing slash on the base URL must not produce "//chat/completions".
	p, err := NewOpenAI(OpenAIConfig{Name: "openai_compatible", BaseURL: srv.URL + "/v1beta/openai/", APIKey: testAPIKey, Model: "gemini-x"})
	if err != nil {
		t.Fatal(err)
	}

	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
	_, err = p.Chat(context.Background(), ChatRequest{
		System: "be brief",
		Messages: []Message{
			{Role: RoleUser, Content: "read it"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.ts"}`)}}},
			{Role: RoleTool, ToolCallID: "call_1", Content: "file contents"},
		},
		Tools:           []ToolSpec{{Name: "read_file", Description: "reads a file", Schema: schema}},
		MaxOutputTokens: 777,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if p.Name() != "openai_compatible" || p.Model() != "gemini-x" {
		t.Fatalf("Name/Model = %q/%q", p.Name(), p.Model())
	}
	if gotPath != "/v1beta/openai/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer "+testAPIKey || gotCT != "application/json" {
		t.Fatalf("headers auth=%q ct=%q", gotAuth, gotCT)
	}
	if gotBody["model"] != "gemini-x" || gotBody["max_tokens"] != float64(777) {
		t.Fatalf("model/max_tokens = %v/%v", gotBody["model"], gotBody["max_tokens"])
	}

	msgs := gotBody["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages = %v", msgs)
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "be brief" {
		t.Fatalf("system message = %v", sys)
	}
	asst := msgs[2].(map[string]any)
	if asst["role"] != "assistant" || asst["content"] != nil {
		t.Fatalf("assistant tool-call message must have null content: %v", asst)
	}
	call := asst["tool_calls"].([]any)[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if call["id"] != "call_1" || call["type"] != "function" || fn["name"] != "read_file" || fn["arguments"] != `{"path":"a.ts"}` {
		t.Fatalf("assistant tool call = %v", call)
	}
	tool := msgs[3].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != "file contents" {
		t.Fatalf("tool message = %v", tool)
	}

	tools := gotBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", tools)
	}
	spec := tools[0].(map[string]any)
	specFn := spec["function"].(map[string]any)
	if spec["type"] != "function" || specFn["name"] != "read_file" || specFn["description"] != "reads a file" {
		t.Fatalf("tool spec = %v", spec)
	}
	wantParams := map[string]any{}
	_ = json.Unmarshal(schema, &wantParams)
	if !reflect.DeepEqual(specFn["parameters"], wantParams) {
		t.Fatalf("parameters = %v, want %v", specFn["parameters"], wantParams)
	}
}

func TestChatOmitsOptionalFields(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, 200, `{"choices":[{"message":{"content":"ok"}}]}`)
	})
	p, err := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "llama"}) // keyless, e.g. Ollama
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat(context.Background(), userHi); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization sent without a key: %q", gotAuth)
	}
	for _, k := range []string{"max_tokens", "tools"} {
		if _, ok := gotBody[k]; ok {
			t.Fatalf("%s must be omitted when unset", k)
		}
	}
}

func TestToolCallNormalization(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"choices":[{"message":{"tool_calls":[
			{"function":{"name":"no_id","arguments":""}},
			{"id":"x","function":{"name":"bad_args","arguments":"not json"}}]},
			"finish_reason":"stop"}]}`)
	})
	p, _ := newTestProvider(t, srv)

	resp, err := p.Chat(context.Background(), userHi)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Fatalf("stop with tool calls must normalize to tool_calls, got %q", resp.FinishReason)
	}
	if resp.ToolCalls[0].ID == "" || string(resp.ToolCalls[0].Arguments) != `{}` {
		t.Fatalf("call 0 = %+v", resp.ToolCalls[0])
	}
	// Invalid arguments must survive json.Marshal (they are persisted).
	if _, err := json.Marshal(resp); err != nil {
		t.Fatalf("response not serializable: %v", err)
	}
	if string(resp.ToolCalls[1].Arguments) != `"not json"` {
		t.Fatalf("call 1 args = %s", resp.ToolCalls[1].Arguments)
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	in := ChatResponse{
		Message: Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c", Name: "n", Arguments: json.RawMessage(`{"a":1}`)}}},
		Usage:   Usage{InputTokens: 1, OutputTokens: 2},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Message.ToolCalls[0].ID != "c" || string(out.Message.ToolCalls[0].Arguments) != `{"a":1}` || out.Usage != in.Usage {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestNewOpenAIValidation(t *testing.T) {
	if _, err := NewOpenAI(OpenAIConfig{}); err == nil {
		t.Fatal("empty model must be rejected")
	}
	if _, err := NewOpenAI(OpenAIConfig{Model: "m", BaseURL: "not a url"}); err == nil {
		t.Fatal("invalid base URL must be rejected")
	}
	if _, err := NewOpenAI(OpenAIConfig{Model: "m", BaseURL: "ftp://example.com"}); err == nil {
		t.Fatal("non-http scheme must be rejected")
	}
	p, err := NewOpenAI(OpenAIConfig{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "openai" || p.endpoint != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("defaults: name=%q endpoint=%q", p.Name(), p.endpoint)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Fatalf("seconds = %v", got)
	}
	if got := parseRetryAfter("1.5"); got != 1500*time.Millisecond {
		t.Fatalf("fractional = %v", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Fatalf("empty = %v", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Fatalf("garbage = %v", got)
	}
	if got := parseRetryAfter("-5"); got != 0 {
		t.Fatalf("negative = %v", got)
	}
}

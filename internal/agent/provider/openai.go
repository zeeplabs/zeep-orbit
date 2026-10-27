package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultOpenAIBaseURL is used when OpenAIConfig.BaseURL is empty.
	DefaultOpenAIBaseURL = "https://api.openai.com/v1"

	// defaultHTTPTimeout is longer than the dashboard chat client's 30s: an
	// agent turn can ask for a large code-generation reply that legitimately
	// takes a minute or more. Callers needing a different bound pass their own
	// *http.Client.
	defaultHTTPTimeout = 120 * time.Second

	// maxAttempts is the total number of tries per Chat call (1 + 2 retries).
	maxAttempts = 3

	defaultBackoffBase = 500 * time.Millisecond

	// maxRetryWait bounds how long a single Retry-After is honored. A provider
	// asking for more than this (e.g. a daily quota reset) will not clear
	// within an agent turn, so blocking would only hold the turn open; the
	// caller gets ErrRateLimited with the requested wait instead.
	maxRetryWait = 30 * time.Second

	// maxResponseBytes guards memory against a runaway or hostile upstream.
	maxResponseBytes = 16 << 20

	// maxErrorBodyBytes is how much of a failed response is kept in
	// ErrProvider.Body for server-side logs.
	maxErrorBodyBytes = 4 << 10
)

// OpenAIConfig configures an OpenAI Chat Completions adapter.
type OpenAIConfig struct {
	// Name is what Provider.Name reports; the same adapter serves "openai" and
	// "openai_compatible". Defaults to "openai".
	Name string
	// BaseURL defaults to DefaultOpenAIBaseURL. A trailing slash is trimmed.
	BaseURL string
	// APIKey is sent as a Bearer token. It may be empty for local servers
	// (e.g. Ollama) that do not authenticate; no header is sent then.
	APIKey string
	// Model is required.
	Model string
	// HTTPClient is optional; a client with a 120s timeout is used when nil.
	HTTPClient *http.Client
}

// OpenAI is a Provider over the OpenAI Chat Completions API and any server
// speaking the same wire format (Gemini's compatibility endpoint, Azure,
// Ollama, ...).
type OpenAI struct {
	name     string
	endpoint string
	apiKey   string
	model    string
	client   *http.Client

	// Injectable so tests neither sleep for real nor wait on wall-clock time.
	backoffBase time.Duration
	sleep       func(ctx context.Context, d time.Duration) error
}

// NewOpenAI validates cfg and returns the adapter. It does not contact the
// network.
func NewOpenAI(cfg OpenAIConfig) (*OpenAI, error) {
	if cfg.Model == "" {
		return nil, errors.New("provider: model is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultOpenAIBaseURL
	}
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		// The parse error is dropped on purpose: it echoes the URL, which an
		// operator may have written with embedded credentials.
		return nil, errors.New("provider: base URL must be an absolute http(s) URL")
	}
	name := cfg.Name
	if name == "" {
		name = "openai"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &OpenAI{
		name:        name,
		endpoint:    base + "/chat/completions",
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		client:      client,
		backoffBase: defaultBackoffBase,
		sleep:       sleepCtx,
	}, nil
}

// Name implements Provider.
func (o *OpenAI) Name() string { return o.name }

// Model implements Provider.
func (o *OpenAI) Model() string { return o.model }

// Chat implements Provider. It retries 429 and 5xx up to maxAttempts total,
// honoring Retry-After on 429; transport failures are not retried because the
// request may already have been processed and generation is not idempotent.
func (o *OpenAI) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	payload, err := json.Marshal(o.buildRequest(req))
	if err != nil {
		return ChatResponse{}, &ErrProvider{Err: fmt.Errorf("marshal request: %w", err)}
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := o.sleep(ctx, o.retryDelay(lastErr, attempt-1)); err != nil {
				return ChatResponse{}, &ErrProvider{Err: err}
			}
		}
		resp, err := o.attempt(ctx, payload)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retryable(err) {
			return ChatResponse{}, err
		}
	}
	return ChatResponse{}, lastErr
}

// retryable reports whether err is worth another attempt: 429 within the
// bounded wait, or a 5xx.
func retryable(err error) bool {
	var rl *ErrRateLimited
	if errors.As(err, &rl) {
		return rl.RetryAfter <= maxRetryWait
	}
	var pe *ErrProvider
	return errors.As(err, &pe) && pe.Status >= 500
}

// retryDelay is the provider's Retry-After when it gave one, otherwise
// exponential backoff from backoffBase. failures is the count of failed
// attempts minus one (0 for the first retry).
func (o *OpenAI) retryDelay(err error, failures int) time.Duration {
	var rl *ErrRateLimited
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		return rl.RetryAfter
	}
	return o.backoffBase << failures
}

// attempt performs one HTTP round-trip and classifies the outcome.
func (o *OpenAI) attempt(ctx context.Context, payload []byte) (ChatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, &ErrProvider{Err: fmt.Errorf("build request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, &ErrProvider{Err: err}
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return ChatResponse{}, &ErrProvider{Status: httpResp.StatusCode, Err: fmt.Errorf("read response: %w", err)}
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return ChatResponse{}, classifyStatus(httpResp.StatusCode, httpResp.Header, body)
	}
	return parseResponse(httpResp.StatusCode, body)
}

// classifyStatus maps a non-2xx response onto the typed errors. Only
// ErrProvider keeps the body, and only in a field Error() never prints.
func classifyStatus(status int, h http.Header, body []byte) error {
	lower := strings.ToLower(string(body))
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrAuth
	case status == http.StatusTooManyRequests:
		// insufficient_quota is a 429 that no amount of waiting fixes
		// (billing), so retrying and reporting "rate limited" would mislead.
		if strings.Contains(lower, "insufficient_quota") {
			return &ErrProvider{Status: status, Body: truncate(body)}
		}
		return &ErrRateLimited{RetryAfter: parseRetryAfter(h.Get("Retry-After"))}
	case status == http.StatusBadRequest && isContextTooLong(lower):
		return ErrContextTooLong
	}
	return &ErrProvider{Status: status, Body: truncate(body)}
}

// isContextTooLong matches the context-overflow signals of OpenAI
// (code context_length_exceeded, "maximum context length") and, best-effort,
// compatible servers. lower must already be lowercased.
func isContextTooLong(lower string) bool {
	for _, marker := range []string{
		"context_length_exceeded",
		"maximum context length",
		"context window",
		"exceeds the maximum number of tokens", // Gemini compat; not verified against the live API
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// parseRetryAfter reads the delta-seconds form of Retry-After, falling back to
// the HTTP-date form. Unparseable or past values give 0 (use backoff).
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func truncate(b []byte) string {
	if len(b) > maxErrorBodyBytes {
		b = b[:maxErrorBodyBytes]
	}
	return string(b)
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// --- wire format ---------------------------------------------------------

type oaRequest struct {
	Model    string      `json:"model"`
	Messages []oaMessage `json:"messages"`
	Tools    []oaTool    `json:"tools,omitempty"`
	// MaxTokens uses the legacy max_tokens name rather than
	// max_completion_tokens: OpenAI still accepts it on chat models, while the
	// compatible servers this adapter also targets (Gemini compat, Ollama,
	// vLLM) generally only know max_tokens. Reasoning-model families that
	// reject it would need a per-model switch, which is out of scope until one
	// is configured.
	MaxTokens int `json:"max_tokens,omitempty"`
}

type oaMessage struct {
	Role string `json:"role"`
	// Content is a pointer so an assistant turn that only carries tool calls
	// serializes as null, which is what OpenAI emits and some compatible
	// servers insist on.
	Content    *string      `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function oaToolCallFunc `json:"function"`
}

type oaToolCallFunc struct {
	Name string `json:"name"`
	// Arguments is a JSON-encoded string, as the API defines it.
	Arguments string `json:"arguments"`
}

type oaTool struct {
	Type     string     `json:"type"`
	Function oaToolFunc `json:"function"`
}

type oaToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaResponse struct {
	Choices []struct {
		Message struct {
			Content   string       `json:"content"`
			ToolCalls []oaToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	// Error is raw because compatible servers disagree on its shape; some
	// return it with a 200.
	Error json.RawMessage `json:"error"`
}

// emptyObjectSchema is sent for a tool declared without a schema; the API
// rejects a function whose parameters are absent on several servers.
var emptyObjectSchema = json.RawMessage(`{"type":"object","properties":{}}`)

func (o *OpenAI) buildRequest(req ChatRequest) oaRequest {
	out := oaRequest{
		Model:     o.model,
		Messages:  make([]oaMessage, 0, len(req.Messages)+1),
		MaxTokens: req.MaxOutputTokens,
	}
	if req.System != "" {
		out.Messages = append(out.Messages, oaMessage{Role: string(RoleSystem), Content: strPtr(req.System)})
	}
	for _, m := range req.Messages {
		om := oaMessage{Role: string(m.Role), ToolCallID: m.ToolCallID}
		if m.Content != "" || len(m.ToolCalls) == 0 {
			om.Content = strPtr(m.Content)
		}
		for _, tc := range m.ToolCalls {
			args := string(tc.Arguments)
			if args == "" {
				args = "{}"
			}
			om.ToolCalls = append(om.ToolCalls, oaToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: oaToolCallFunc{Name: tc.Name, Arguments: args},
			})
		}
		out.Messages = append(out.Messages, om)
	}
	for _, t := range req.Tools {
		schema := t.Schema
		if len(schema) == 0 {
			schema = emptyObjectSchema
		}
		out.Tools = append(out.Tools, oaTool{
			Type:     "function",
			Function: oaToolFunc{Name: t.Name, Description: t.Description, Parameters: schema},
		})
	}
	return out
}

func strPtr(s string) *string { return &s }

func parseResponse(status int, body []byte) (ChatResponse, error) {
	var parsed oaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ChatResponse{}, &ErrProvider{Status: status, Err: fmt.Errorf("decode response: %w", err), Body: truncate(body)}
	}
	if e := bytes.TrimSpace(parsed.Error); len(e) > 0 && !bytes.Equal(e, []byte("null")) {
		if isContextTooLong(strings.ToLower(string(e))) {
			return ChatResponse{}, ErrContextTooLong
		}
		return ChatResponse{}, &ErrProvider{Status: status, Err: errors.New("upstream returned an error object"), Body: truncate(body)}
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, &ErrProvider{Status: status, Err: errors.New("response had no choices"), Body: truncate(body)}
	}

	choice := parsed.Choices[0]
	calls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for i, tc := range choice.Message.ToolCalls {
		id := tc.ID
		if id == "" {
			// Some compatible servers omit ids; the tool result message must
			// reference one, so synthesize a stable per-response id.
			id = "call_" + strconv.Itoa(i)
		}
		calls = append(calls, ToolCall{ID: id, Name: tc.Function.Name, Arguments: normalizeArguments(tc.Function.Arguments)})
	}

	msg := Message{Role: RoleAssistant, Content: choice.Message.Content}
	resp := ChatResponse{
		Usage: Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
		},
		FinishReason: normalizeFinishReason(choice.FinishReason, len(calls) > 0),
	}
	if len(calls) > 0 {
		msg.ToolCalls = calls
		resp.ToolCalls = calls
	}
	resp.Message = msg
	return resp, nil
}

// normalizeArguments turns the API's string-encoded arguments into raw JSON
// that is always safe to marshal: empty becomes {}, valid JSON is kept as is,
// anything else is preserved as a JSON string (see ToolCall.Arguments).
func normalizeArguments(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	quoted, _ := json.Marshal(s) // marshaling a string cannot fail
	return quoted
}

// normalizeFinishReason maps vendor values onto the Finish* constants.
// Servers that return "stop" (or nothing) alongside tool calls are corrected
// to tool_calls so the loop can key off FinishReason alone.
func normalizeFinishReason(reason string, hasToolCalls bool) string {
	switch reason {
	case "function_call":
		return FinishToolCalls
	case "", FinishStop:
		if hasToolCalls {
			return FinishToolCalls
		}
		return FinishStop
	}
	return reason
}

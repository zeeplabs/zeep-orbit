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
	"strings"
	"time"
)

const (
	// DefaultAnthropicBaseURL is the official API. The dashboard does not
	// offer another one for this kind; gateways go through openai_compatible.
	DefaultAnthropicBaseURL = "https://api.anthropic.com"

	// anthropicVersion is the pinned anthropic-version header. On this
	// version the API rejects temperature, top_p and top_k; the adapter sends
	// none of them.
	anthropicVersion = "2026-10-21"

	// anthropicDefaultMaxTokens is sent when the caller leaves
	// MaxOutputTokens at 0: the Messages API requires max_tokens.
	anthropicDefaultMaxTokens = 16000

	// anthropicMinMaxTokens is the lowest max_tokens the adapter sends. On
	// current models thinking is always on and its tokens count toward
	// max_tokens, so a small cap (the compaction summary asks for 1200) could
	// be spent before any visible text. The cap is a ceiling, not a target.
	anthropicMinMaxTokens = 4096

	// anthropicHTTPTimeout is longer than the OpenAI adapter's: a reply of
	// anthropicDefaultMaxTokens with thinking can take several minutes without
	// streaming. The turn's context deadline still bounds the call.
	anthropicHTTPTimeout = 10 * time.Minute

	// anthropicName is what Provider.Name reports and the price table key.
	anthropicName = "anthropic"
)

// AnthropicConfig configures an Anthropic Messages API adapter.
type AnthropicConfig struct {
	// BaseURL defaults to DefaultAnthropicBaseURL. A trailing slash is trimmed.
	BaseURL string
	// APIKey is required and sent in the x-api-key header.
	APIKey string
	// Model is required.
	Model string
	// HTTPClient is optional; a client with a 10 minute timeout is used when nil.
	HTTPClient *http.Client
}

// Anthropic is a Provider over the Anthropic Messages API.
//
// Thinking blocks of a reply are not kept: the neutral Message has no place
// for them, so every request replays the history without any, which the API
// accepts. The model then answers each step without the reasoning of the
// previous ones.
type Anthropic struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client

	// Injectable so tests neither sleep for real nor wait on wall-clock time.
	backoffBase time.Duration
	sleep       func(ctx context.Context, d time.Duration) error
}

// NewAnthropic validates cfg and returns the adapter. It does not contact the
// network.
func NewAnthropic(cfg AnthropicConfig) (*Anthropic, error) {
	if cfg.Model == "" {
		return nil, errors.New("provider: model is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("provider: API key is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultAnthropicBaseURL
	}
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		// The parse error is dropped on purpose, as in NewOpenAI.
		return nil, errors.New("provider: base URL must be an absolute http(s) URL")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: anthropicHTTPTimeout}
	}
	return &Anthropic{
		endpoint:    base + "/v1/messages",
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		client:      client,
		backoffBase: defaultBackoffBase,
		sleep:       sleepCtx,
	}, nil
}

// Name implements Provider.
func (a *Anthropic) Name() string { return anthropicName }

// Model implements Provider.
func (a *Anthropic) Model() string { return a.model }

// Chat implements Provider. Like the OpenAI adapter it retries, up to
// maxAttempts total, only what retryable accepts (429, 503, 529 overloaded and
// a connection that could not be opened), honoring Retry-After on 429.
func (a *Anthropic) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	payload, err := json.Marshal(a.buildRequest(req))
	if err != nil {
		return ChatResponse{}, &ErrProvider{Err: fmt.Errorf("marshal request: %w", err)}
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := a.sleep(ctx, retryDelayFor(a.backoffBase, lastErr, attempt-1)); err != nil {
				return ChatResponse{}, &ErrProvider{Err: err}
			}
		}
		resp, err := a.attempt(ctx, payload)
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

// attempt performs one HTTP round-trip and classifies the outcome.
func (a *Anthropic) attempt(ctx context.Context, payload []byte) (ChatResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, &ErrProvider{Err: fmt.Errorf("build request: %w", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	httpResp, err := a.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, &ErrProvider{Err: err}
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return ChatResponse{}, &ErrProvider{Status: httpResp.StatusCode, Err: fmt.Errorf("read response: %w", err)}
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return ChatResponse{}, classifyAnthropicStatus(httpResp.StatusCode, httpResp.Header, body)
	}
	return parseAnthropicResponse(httpResp.StatusCode, body)
}

// classifyAnthropicStatus maps a non-2xx response onto the typed errors. Only
// ErrProvider keeps the body, and only in a field Error() never prints.
func classifyAnthropicStatus(status int, h http.Header, body []byte) error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrAuth
	case status == http.StatusTooManyRequests:
		return &ErrRateLimited{RetryAfter: parseRetryAfter(h.Get("Retry-After"))}
	case status == http.StatusRequestEntityTooLarge:
		return ErrContextTooLong
	case status == http.StatusBadRequest && isAnthropicContextTooLong(body):
		return ErrContextTooLong
	}
	return &ErrProvider{Status: status, Body: truncate(body)}
}

// isAnthropicContextTooLong reports whether a 400 body is the API saying the
// prompt does not fit the model: an invalid_request_error whose message talks
// about the prompt length or the context window. The API has no dedicated
// error type for it, so this matches the message; a miss degrades to
// ErrProvider, which fails the turn instead of compacting.
func isAnthropicContextTooLong(body []byte) bool {
	var parsed struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Error.Type != "invalid_request_error" {
		return false
	}
	msg := strings.ToLower(parsed.Error.Message)
	for _, marker := range []string{"prompt is too long", "context window", "context limit", "context length"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// retryDelayFor is the provider's Retry-After when it gave one, otherwise
// exponential backoff from base. failures is the count of failed attempts
// minus one (0 for the first retry).
func retryDelayFor(base time.Duration, err error, failures int) time.Duration {
	var rl *ErrRateLimited
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		return rl.RetryAfter
	}
	return base << failures
}

// --- wire format ---------------------------------------------------------

type anRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  []anMessage `json:"messages"`
	Tools     []anTool    `json:"tools,omitempty"`
	// CacheControl is the request-level (automatic) breakpoint: the API puts
	// it on the last cacheable block and moves it forward as the history
	// grows, so each step of a loop reads the previous step's prefix.
	CacheControl *anCacheControl `json:"cache_control,omitempty"`
}

// anCacheControl is the default 5 minute entry. A loop step starts well
// under 5 minutes after the previous one, so every read refreshes it and the
// 1 hour entry (2x write) would only cost more.
type anCacheControl struct {
	Type string `json:"type"`
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

// anBlock is a request content block: text, tool_use or tool_result.
type anBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type anTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anResponse struct {
	Type    string `json:"type"`
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	// Usage is a pointer so a response without it reads as "not reported".
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func (a *Anthropic) buildRequest(req ChatRequest) anRequest {
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	out := anRequest{
		Model:     a.model,
		MaxTokens: max(maxTokens, anthropicMinMaxTokens),
		System:    req.System,
		Messages:  make([]anMessage, 0, len(req.Messages)),
	}
	if req.CacheHint {
		out.CacheControl = &anCacheControl{Type: "ephemeral"}
	}
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			// The loop keeps the system prompt in ChatRequest.System; a system
			// message inside the history is folded into it rather than sent
			// in a role the API only accepts on some models.
			if m.Content != "" {
				out.System = strings.TrimSpace(out.System + "\n\n" + m.Content)
			}
			continue
		}
		role, blocks := anBlocks(m)
		if len(blocks) == 0 {
			continue
		}
		// Roles must alternate, and all the results of one assistant turn go
		// in a single user message: consecutive messages of one role merge.
		if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == role {
			out.Messages[n-1].Content = append(out.Messages[n-1].Content, blocks...)
			continue
		}
		out.Messages = append(out.Messages, anMessage{Role: role, Content: blocks})
	}
	for _, t := range req.Tools {
		schema := t.Schema
		if len(schema) == 0 {
			schema = emptyObjectSchema
		}
		out.Tools = append(out.Tools, anTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

// anBlocks renders one neutral message as the API role and content blocks.
// Empty text is never sent (the API rejects an empty text block), so a
// message with nothing to say yields no blocks.
func anBlocks(m Message) (role string, blocks []anBlock) {
	switch m.Role {
	case RoleTool:
		return "user", []anBlock{{Type: "tool_result", ToolUseID: anToolID(m.ToolCallID), Content: m.Content}}
	case RoleAssistant:
		if m.Content != "" {
			blocks = append(blocks, anBlock{Type: "text", Text: m.Content})
		}
		var uses []anBlock
		for _, tc := range m.ToolCalls {
			input := tc.Arguments
			if !isJSONObject(input) {
				// A model (of any provider) produced arguments that are not a
				// JSON object. tool_use.input must be one, so the call is
				// replayed with an empty input and the original text is kept
				// next to it; the tool result that follows already tells the
				// model its arguments were invalid.
				if text := strings.TrimSpace(string(input)); text != "" {
					blocks = append(blocks, anBlock{Type: "text", Text: "Arguments sent to " + tc.Name + " (not a JSON object): " + text})
				}
				input = json.RawMessage(`{}`)
			}
			uses = append(uses, anBlock{Type: "tool_use", ID: anToolID(tc.ID), Name: tc.Name, Input: input})
		}
		return "assistant", append(blocks, uses...)
	default:
		if m.Content != "" {
			blocks = append(blocks, anBlock{Type: "text", Text: m.Content})
		}
		return "user", blocks
	}
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}

// anToolID makes a tool call id acceptable to the API, which only takes
// letters, digits, '_' and '-'. Ids issued by this API pass through; an id
// from another provider replayed here has every other character replaced, the
// same way in the tool_use block and in its tool_result.
func anToolID(id string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		}
		return '_'
	}, id)
	if clean == "" {
		return "call_unknown"
	}
	return clean
}

func parseAnthropicResponse(status int, body []byte) (ChatResponse, error) {
	var parsed anResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ChatResponse{}, &ErrProvider{Status: status, Err: fmt.Errorf("decode response: %w", err), Body: truncate(body)}
	}
	if parsed.Type != "message" {
		if isAnthropicContextTooLong(body) {
			return ChatResponse{}, ErrContextTooLong
		}
		return ChatResponse{}, &ErrProvider{Status: status, Err: errors.New("response is not a message"), Body: truncate(body)}
	}

	var text strings.Builder
	var calls []ToolCall
	for _, b := range parsed.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			input := b.Input
			if len(bytes.TrimSpace(input)) == 0 {
				input = json.RawMessage(`{}`)
			}
			calls = append(calls, ToolCall{ID: b.ID, Name: b.Name, Arguments: input})
		}
		// thinking and any other block type are not part of the neutral reply.
	}

	msg := Message{Role: RoleAssistant, Content: text.String()}
	resp := ChatResponse{
		Usage:        anUsage(parsed),
		FinishReason: anFinishReason(parsed.StopReason, len(calls) > 0),
	}
	if len(calls) > 0 {
		msg.ToolCalls = calls
		resp.ToolCalls = calls
	}
	resp.Message = msg
	return resp, nil
}

// anFinishReason maps stop_reason onto the Finish* constants. A refusal is a
// 200 with no usable content; it is reported as content_filter. Values the
// adapter does not know (pause_turn, future ones) pass through.
func anFinishReason(reason string, hasToolCalls bool) string {
	switch reason {
	case "tool_use":
		return FinishToolCalls
	case "end_turn", "stop_sequence", "":
		if hasToolCalls {
			return FinishToolCalls
		}
		return FinishStop
	case "max_tokens", "model_context_window_exceeded":
		return FinishLength
	case "refusal":
		return FinishFilter
	}
	return reason
}

// anUsage maps the usage object to the neutral token classes. Here
// input_tokens already excludes cache activity, and the cache read and cache
// write counts are separate fields, so the mapping is direct.
func anUsage(parsed anResponse) Usage {
	if parsed.Usage == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:       parsed.Usage.InputTokens,
		CachedInputTokens: parsed.Usage.CacheReadInputTokens,
		CacheWriteTokens:  parsed.Usage.CacheCreationInputTokens,
		OutputTokens:      parsed.Usage.OutputTokens,
		Reported:          true,
	}
}

// Package provider is the provider-neutral chat-with-tools layer the agent
// loop talks to. It defines the request/response shapes and the typed errors
// every adapter must map into, so the loop never branches on a vendor. The
// message types are JSON-serializable on purpose: the agent persists the
// conversation (including assistant tool calls) and replays it on resume.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Role is the author of a Message.
type Role string

// Message roles understood by every adapter.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Finish reasons ChatResponse.FinishReason is normalized to. An adapter passes
// an unrecognized vendor value through unchanged rather than guessing.
const (
	FinishStop      = "stop"
	FinishToolCalls = "tool_calls"
	FinishLength    = "length"
	FinishFilter    = "content_filter"
)

// Provider is one configured model endpoint. Implementations must be safe for
// concurrent use: a single instance is shared across agent runs.
type Provider interface {
	// Chat performs one model round-trip. Errors are always one of ErrAuth,
	// ErrContextTooLong, *ErrRateLimited or *ErrProvider (match with
	// errors.Is / errors.As).
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// Name is the provider kind, e.g. "openai" or "openai_compatible".
	Name() string
	// Model is the model identifier requests are sent with.
	Model() string
}

// ChatRequest is one provider-neutral model call.
type ChatRequest struct {
	// System is sent as a leading system message, kept apart from Messages so
	// the loop can rebuild or compact the history without touching it.
	System   string
	Messages []Message
	Tools    []ToolSpec
	// MaxOutputTokens caps the reply; 0 leaves the provider default.
	MaxOutputTokens int
}

// ChatResponse is the model's reply to one ChatRequest.
type ChatResponse struct {
	// Message is the assistant turn to append to the history. When the model
	// requested tools, Message.ToolCalls carries them so the turn can be
	// persisted and replayed verbatim.
	Message Message `json:"message"`
	// ToolCalls repeats Message.ToolCalls, in the order the model emitted
	// them, for callers that only dispatch tools.
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        Usage      `json:"usage"`
	FinishReason string     `json:"finish_reason"`
}

// Message is one conversation turn.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// ToolCalls is set only on assistant messages that requested tools.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID is set only on tool messages and ties the result to the
	// ToolCall.ID it answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Arguments is the raw JSON object the model produced. Models sometimes
	// emit invalid JSON; adapters then keep the original text as a JSON string
	// so the message stays serializable, and decoding it into the tool's
	// argument struct fails, which the loop reports back to the model.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolSpec declares a tool the model may call.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Schema is the JSON Schema of the arguments object.
	Schema json.RawMessage `json:"schema"`
}

// Usage is the token accounting the provider reported for one call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ErrAuth means the provider rejected the credentials (HTTP 401/403). It is a
// sentinel: match with errors.Is.
var ErrAuth = errors.New("provider: authentication failed")

// ErrContextTooLong means the prompt exceeded the model's context window. The
// agent reacts by compacting the history, so it is distinct from ErrProvider.
// It is a sentinel: match with errors.Is.
var ErrContextTooLong = errors.New("provider: context length exceeded")

// ErrRateLimited means the provider kept answering 429 after the adapter's
// retries were used up (or asked for a wait too long to block on). Match with
// errors.As against *ErrRateLimited.
type ErrRateLimited struct {
	// RetryAfter is the wait the provider asked for, 0 when it gave none.
	RetryAfter time.Duration
}

// Error implements error.
func (e *ErrRateLimited) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("provider: rate limited, retry after %s", e.RetryAfter)
	}
	return "provider: rate limited"
}

// ErrProvider is every other failure: transport errors, non-2xx statuses,
// malformed responses, and context cancellation. Match with errors.As against
// *ErrProvider.
//
// Error() is deliberately generic: Err and Body can carry URLs or upstream
// payload fragments, so they are for server-side logs only and must never be
// forwarded to API clients. Unwrap exposes Err, so errors.Is(err,
// context.Canceled) still works.
type ErrProvider struct {
	// Status is the HTTP status, 0 when no response was received.
	Status int
	// Err is the underlying error, if any.
	Err error
	// Body is the upstream response body, truncated; empty when unavailable.
	Body string
}

// Error implements error without leaking Err or Body.
func (e *ErrProvider) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("provider: upstream request failed (status %d)", e.Status)
	}
	return "provider: upstream request failed"
}

// Unwrap returns the underlying error for server-side inspection.
func (e *ErrProvider) Unwrap() error { return e.Err }

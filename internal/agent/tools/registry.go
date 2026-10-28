// Package tools is the single place where the builder agent's tools are
// declared (JSON schema), classified and dispatched.
//
// Result contract, which the agent loop relies on:
//
//   - A tool failure the model can act on ("old_string not found", unknown
//     tool, invalid arguments, a failed build) is a [Result] with IsError set
//     and a Go error of nil. The model sees Content and adapts.
//   - A Go error from [Registry.Dispatch] means the turn cannot trust the
//     tool: context cancellation, the builder unreachable or misconfigured,
//     the workspace lost ([sandbox.ErrWorkspaceLost]), or a handler bug. It is
//     for server-side logs and turn state only; its text is never sent to the
//     model, so raw internal errors cannot leak.
//
// This package must not import internal/dashboard: the dashboard registers its
// own tools (Orbit reads, propose_backend_change) on the Registry it is given.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
)

// DefaultMaxResultBytes is the result size cap applied when the Registry is
// built without [WithMaxResultBytes]. It sits below the builder's 64 KiB
// read_file cap on purpose: the model gets the explicit marker and asks for
// the next range instead of silently filling its context.
const DefaultMaxResultBytes = 32 << 10

// Kind classifies a tool for the agent loop.
type Kind int

// Tool kinds (design section 3).
const (
	// KindReadOnly tools never change anything.
	KindReadOnly Kind = iota + 1
	// KindWorkspace tools change the session workspace (files, build output).
	KindWorkspace
	// KindProposal tools propose a change that needs the user's confirmation;
	// the turn pauses (awaiting_confirmation).
	KindProposal
	// KindExternalMutating tools change something outside the workspace and
	// pause the turn like a proposal.
	KindExternalMutating
)

// String implements fmt.Stringer.
func (k Kind) String() string {
	switch k {
	case KindReadOnly:
		return "read_only"
	case KindWorkspace:
		return "workspace"
	case KindProposal:
		return "proposal"
	case KindExternalMutating:
		return "external_mutating"
	}
	return "unknown"
}

// Call is one tool invocation requested by the model.
type Call struct {
	// ID is the model's tool call id. Workspace tools use it as the builder
	// op_id, which makes a replayed call (after a crash and resume) return the
	// stored result instead of executing twice.
	ID string
	// Name is the tool name.
	Name string
	// Arguments is the raw JSON object the model produced.
	Arguments json.RawMessage
	// SessionID is the builder session the call runs against.
	SessionID string
}

// Result is what the model is told about a call.
type Result struct {
	// Content is the text sent back as the tool message.
	Content string
	// IsError marks a failed call (see the package comment).
	IsError bool
	// Truncated is set when Content was cut to the registry's size cap.
	Truncated bool
	// BuildFailed is set by run_build when the step exited non-zero or timed
	// out. The loop counts these toward max_fix_iterations (AFB-07).
	BuildFailed bool
	// Meta carries small structured facts about the call for the loop: it is
	// persisted with the tool message and read when events are emitted (for
	// example which skill a load_skill call loaded). It is never sent to the
	// model and never holds content.
	Meta map[string]string
}

// Handler runs one call. See the package comment for how its error is
// interpreted: a *ToolError (or an error wrapping one) becomes an error
// Result; any other error is returned from Dispatch as a Go error.
type Handler func(ctx context.Context, call Call) (Result, error)

// Tool is one declared tool.
type Tool struct {
	// Name is what the model calls it: 1-64 characters of [A-Za-z0-9_-].
	Name string
	// Description tells the model when and how to use it.
	Description string
	// Schema is the JSON Schema (an object) of the arguments.
	Schema json.RawMessage
	// Kind classifies the tool.
	Kind Kind
	// KeepTail makes the registry keep the END of an oversized result (build
	// output, logs: the error is at the bottom) instead of the start.
	KeepTail bool
	// Handler executes the tool.
	Handler Handler
}

// ToolError is a failure the model should see and can act on. Handlers return
// it (directly or wrapped) for bad arguments and tool-level problems; Dispatch
// converts it into an error Result. Msg must be safe to show the model: no
// host paths, no raw internal errors.
type ToolError struct{ Msg string }

// Error implements error.
func (e *ToolError) Error() string { return e.Msg }

// ToolErrorf builds a [ToolError].
func ToolErrorf(format string, args ...any) *ToolError {
	return &ToolError{Msg: fmt.Sprintf(format, args...)}
}

// Option configures a [Registry].
type Option func(*Registry)

// WithMaxResultBytes sets the size cap of a result's Content (default
// [DefaultMaxResultBytes]). Values below 1 KiB are raised to 1 KiB.
func WithMaxResultBytes(n int) Option {
	return func(r *Registry) {
		if n < 1<<10 {
			n = 1 << 10
		}
		r.maxResult = n
	}
}

// WithLogger sets the logger used for handler panics (default slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(r *Registry) {
		if l != nil {
			r.log = l
		}
	}
}

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Registry holds the tools and dispatches calls. It is safe for concurrent
// use; tools are normally registered once at start-up.
type Registry struct {
	mu        sync.RWMutex
	order     []string
	tools     map[string]Tool
	maxResult int
	log       *slog.Logger
}

// NewRegistry returns an empty Registry.
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{tools: make(map[string]Tool), maxResult: DefaultMaxResultBytes, log: slog.Default()}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Register adds a tool. It fails on a bad name, a schema that is not a JSON
// object, a nil handler, an unknown kind or a duplicate name.
func (r *Registry) Register(t Tool) error {
	if !toolNamePattern.MatchString(t.Name) {
		return fmt.Errorf("tools: invalid tool name %q", truncateRunes(t.Name, 64))
	}
	if t.Handler == nil {
		return fmt.Errorf("tools: tool %q has no handler", t.Name)
	}
	if t.Kind < KindReadOnly || t.Kind > KindExternalMutating {
		return fmt.Errorf("tools: tool %q has no valid kind", t.Name)
	}
	var schema map[string]any
	if err := json.Unmarshal(t.Schema, &schema); err != nil || schema == nil {
		return fmt.Errorf("tools: tool %q schema must be a JSON object", t.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.tools[t.Name]; dup {
		return fmt.Errorf("tools: tool %q is already registered", t.Name)
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
	return nil
}

// Specs returns the provider tool declarations in registration order. The
// order is stable so the prompt prefix stays cacheable.
func (r *Registry) Specs() []provider.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]provider.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		out = append(out, provider.ToolSpec{Name: t.Name, Description: t.Description, Schema: t.Schema})
	}
	return out
}

// SpecsWithout is Specs minus the named tools, for tools that are registered
// once on the shared registry but must not be offered in a given turn.
func (r *Registry) SpecsWithout(names ...string) []provider.ToolSpec {
	skip := make(map[string]bool, len(names))
	for _, n := range names {
		skip[n] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]provider.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		if skip[name] {
			continue
		}
		t := r.tools[name]
		out = append(out, provider.ToolSpec{Name: t.Name, Description: t.Description, Schema: t.Schema})
	}
	return out
}

// Kind returns the kind of a registered tool.
func (r *Registry) Kind(name string) (Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t.Kind, ok
}

// Dispatch runs call and returns what to tell the model. See the package
// comment for the Result / Go error contract. Oversized content is truncated
// with an explicit marker.
func (r *Registry) Dispatch(ctx context.Context, call Call) (res Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r.mu.RLock()
	t, ok := r.tools[call.Name]
	r.mu.RUnlock()
	if !ok {
		return Result{Content: fmt.Sprintf("unknown tool %q", truncateRunes(call.Name, 64)), IsError: true}, nil
	}

	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("tool handler panic", "tool", t.Name, "session", call.SessionID, "panic", fmt.Sprint(rec))
			res, err = Result{Content: "the tool failed unexpectedly", IsError: true}, nil
		}
	}()

	res, err = t.Handler(ctx, call)
	if err != nil {
		var te *ToolError
		if errors.As(err, &te) {
			return r.finish(t, Result{Content: te.Msg, IsError: true}), nil
		}
		return Result{}, fmt.Errorf("tools: %s: %w", t.Name, err)
	}
	return r.finish(t, res), nil
}

func (r *Registry) finish(t Tool, res Result) Result {
	res.Content, res.Truncated = truncate(res.Content, r.maxResult, t.KeepTail)
	return res
}

// truncate caps s at max bytes on a rune boundary and appends (or, with
// keepTail, prepends) an explicit marker.
func truncate(s string, max int, keepTail bool) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	if keepTail {
		start := len(s) - max
		for start < len(s) && !utf8.RuneStart(s[start]) {
			start++
		}
		return fmt.Sprintf("…truncated, %d bytes omitted from the start\n%s", start, s[start:]), true
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return fmt.Sprintf("%s\n…truncated, use offset (%d bytes omitted)", s[:end], len(s)-end), true
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// DecodeArgs decodes the model's arguments into dst (a pointer to a struct).
// Unknown fields are rejected, so a misspelled "contents" cannot silently turn
// into an empty write. Failures are *[ToolError] with a message that names the
// problem without echoing Go type names. Empty or null arguments decode as {}.
func DecodeArgs(raw json.RawMessage, dst any) error {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		b = []byte("{}")
	}
	if b[0] != '{' {
		return ToolErrorf("invalid arguments: expected a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return argError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ToolErrorf("invalid arguments: unexpected data after the JSON object")
	}
	return nil
}

func argError(err error) *ToolError {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn), errors.Is(err, io.ErrUnexpectedEOF):
		return ToolErrorf("invalid arguments: not valid JSON")
	case errors.As(err, &typ):
		return ToolErrorf("invalid arguments: field %q must be %s", truncateRunes(typ.Field, 64), jsonTypeName(typ))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return ToolErrorf("invalid arguments: unknown field %q", truncateRunes(name, 64))
	}
	return ToolErrorf("invalid arguments")
}

func jsonTypeName(e *json.UnmarshalTypeError) string {
	if e.Type == nil {
		return "of a different type"
	}
	switch e.Type.Kind().String() {
	case "string":
		return "a string"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return "an integer"
	case "bool":
		return "a boolean"
	}
	return "of a different type"
}

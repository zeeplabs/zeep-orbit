package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
)

// Names of the workspace tools.
const (
	ToolListDir      = "list_dir"
	ToolReadFile     = "read_file"
	ToolWriteFile    = "write_file"
	ToolEditFile     = "edit_file"
	ToolRunBuild     = "run_build"
	ToolGetBuildLogs = "get_build_logs"
)

// PreviewBuildBase returns the vite --base used for the preview build: always
// the relative "./", so asset URLs resolve against the document URL and inherit
// the token path segment the preview is served under (cookies are not sent
// from the sandboxed iframe). Only the preview build uses it; the published and
// exported source keep vite's default base "/".
func PreviewBuildBase(string) string { return "./" }

// WorkspaceConfig configures the workspace tools.
type WorkspaceConfig struct {
	// PreviewBase returns the public URL path (starting and ending with "/")
	// the session's preview is served under; it is passed to the build step as
	// vite's --base. Default [PreviewBuildBase].
	PreviewBase func(sessionID string) string
}

// RegisterWorkspaceTools registers the six workspace tools on r.
//
// sb is used as given: wrap it with [sandbox.NewRestoring] to survive loss of
// the builder volume. File and build calls use the model's tool call id as
// the builder op_id, so a call replayed after a crash is not executed twice.
func RegisterWorkspaceTools(r *Registry, sb sandbox.Sandbox, cfg WorkspaceConfig) error {
	ts, err := NewWorkspaceTools(sb, cfg)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// NewWorkspaceTools returns the six workspace tools bound to sb.
func NewWorkspaceTools(sb sandbox.Sandbox, cfg WorkspaceConfig) ([]Tool, error) {
	if sb == nil {
		return nil, errors.New("tools: sandbox is required")
	}
	if cfg.PreviewBase == nil {
		cfg.PreviewBase = PreviewBuildBase
	}
	w := &workspace{sb: sb, previewBase: cfg.PreviewBase}
	return []Tool{
		{
			Name:        ToolListDir,
			Description: "List the entries of a directory in the project (not recursive). Use path \"\" or \".\" for the project root. Directories end with \"/\".",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory relative to the project root. Default: the root."}},"additionalProperties":false}`),
			Kind:        KindReadOnly,
			Handler:     w.listDir,
		},
		{
			Name:        ToolReadFile,
			Description: "Read a text file with line numbers. For a long file, read a range with offset (1-based first line) and limit (number of lines).",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`),
			Kind:        KindReadOnly,
			Handler:     w.readFile,
		},
		{
			Name:        ToolWriteFile,
			Description: "Create a text file or overwrite it with the full content. Parent directories are created. Prefer edit_file for small changes to an existing file.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`),
			Kind:        KindWorkspace,
			Handler:     w.writeFile,
		},
		{
			Name:        ToolEditFile,
			Description: "Replace an exact string in an existing text file. old_string must match exactly once (include enough surrounding lines to be unique) unless replace_all is true. Read the file first.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old_string","new_string"],"additionalProperties":false}`),
			Kind:        KindWorkspace,
			Handler:     w.editFile,
		},
		{
			Name:        ToolRunBuild,
			Description: "Run one build step in the project: install (npm dependencies), typecheck (tsc), lint, or build (production bundle shown in the preview). Returns the exit code and the end of the output. Fix the reported errors and run it again.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"step":{"type":"string","enum":["install","typecheck","lint","build"]}},"required":["step"],"additionalProperties":false}`),
			Kind:        KindWorkspace,
			KeepTail:    true,
			Handler:     w.runBuild,
		},
		{
			Name:        ToolGetBuildLogs,
			Description: "Get the end of the last output of a build step, when you need more than run_build returned. tail is the number of trailing bytes (default 16384).",
			Schema:      json.RawMessage(`{"type":"object","properties":{"step":{"type":"string","enum":["install","typecheck","lint","build"]},"tail":{"type":"integer","minimum":1}},"required":["step"],"additionalProperties":false}`),
			Kind:        KindReadOnly,
			KeepTail:    true,
			Handler:     w.getBuildLogs,
		},
	}, nil
}

type workspace struct {
	sb          sandbox.Sandbox
	previewBase func(sessionID string) string
}

// ---- argument shapes ----

type pathArgs struct {
	Path string `json:"path"`
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// Pointers distinguish "missing" from "empty": writing an empty file or
// deleting text with an empty new_string is legitimate, omitting the field is
// a mistake.
type writeArgs struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

type editArgs struct {
	Path       string  `json:"path"`
	OldString  string  `json:"old_string"`
	NewString  *string `json:"new_string"`
	ReplaceAll bool    `json:"replace_all"`
}

type stepArgs struct {
	Step string `json:"step"`
	Tail int    `json:"tail"`
}

// ---- handlers ----

func (w *workspace) listDir(ctx context.Context, call Call) (Result, error) {
	var a pathArgs
	if err := DecodeArgs(call.Arguments, &a); err != nil {
		return Result{}, err
	}
	p := strings.TrimSpace(a.Path)
	if p == "." {
		p = ""
	}
	return w.op(ctx, call, false, sandbox.FileOp{Kind: sandbox.OpListDir, Path: p})
}

func (w *workspace) readFile(ctx context.Context, call Call) (Result, error) {
	var a readArgs
	if err := DecodeArgs(call.Arguments, &a); err != nil {
		return Result{}, err
	}
	if a.Path == "" {
		return Result{}, ToolErrorf("path is required")
	}
	if a.Offset < 0 || a.Limit < 0 {
		return Result{}, ToolErrorf("offset and limit must not be negative")
	}
	return w.op(ctx, call, false, sandbox.FileOp{Kind: sandbox.OpReadFile, Path: a.Path, Offset: a.Offset, Limit: a.Limit})
}

func (w *workspace) writeFile(ctx context.Context, call Call) (Result, error) {
	var a writeArgs
	if err := DecodeArgs(call.Arguments, &a); err != nil {
		return Result{}, err
	}
	if a.Path == "" {
		return Result{}, ToolErrorf("path is required")
	}
	if a.Content == nil {
		return Result{}, ToolErrorf("content is required")
	}
	return w.op(ctx, call, true, sandbox.FileOp{Kind: sandbox.OpWriteFile, Path: a.Path, Content: *a.Content})
}

func (w *workspace) editFile(ctx context.Context, call Call) (Result, error) {
	var a editArgs
	if err := DecodeArgs(call.Arguments, &a); err != nil {
		return Result{}, err
	}
	if a.Path == "" {
		return Result{}, ToolErrorf("path is required")
	}
	if a.OldString == "" {
		return Result{}, ToolErrorf("old_string is required and must not be empty")
	}
	if a.NewString == nil {
		return Result{}, ToolErrorf("new_string is required (use an empty string to delete the text)")
	}
	return w.op(ctx, call, true, sandbox.FileOp{
		Kind: sandbox.OpEditFile, Path: a.Path, OldString: a.OldString, NewString: *a.NewString, ReplaceAll: a.ReplaceAll,
	})
}

func (w *workspace) runBuild(ctx context.Context, call Call) (Result, error) {
	var a stepArgs
	if err := DecodeArgs(call.Arguments, &a); err != nil {
		return Result{}, err
	}
	step, err := parseStep(a.Step)
	if err != nil {
		return Result{}, err
	}
	if err := requireSession(call, true); err != nil {
		return Result{}, err
	}
	res, err := w.sb.Build(ctx, call.SessionID, call.ID, step, w.previewBase(call.SessionID))
	if err != nil {
		return fail(err)
	}
	failed := res.ExitCode != 0 || res.TimedOut
	var b strings.Builder
	fmt.Fprintf(&b, "step: %s\n", step)
	switch {
	case res.TimedOut:
		b.WriteString("result: failed, the step timed out\n")
	case failed:
		fmt.Fprintf(&b, "result: failed, exit code %d\n", res.ExitCode)
	default:
		b.WriteString("result: ok\n")
	}
	fmt.Fprintf(&b, "duration: %s\n", (time.Duration(res.DurationMs) * time.Millisecond).Round(10*time.Millisecond))
	b.WriteString("output (end):\n")
	b.WriteString(res.OutputTail)
	// A failed build is a normal result: the output tail is what the model
	// needs to fix the code. It is never a Go error.
	return Result{Content: b.String(), IsError: failed, BuildFailed: failed}, nil
}

func (w *workspace) getBuildLogs(ctx context.Context, call Call) (Result, error) {
	var a stepArgs
	if err := DecodeArgs(call.Arguments, &a); err != nil {
		return Result{}, err
	}
	step, err := parseStep(a.Step)
	if err != nil {
		return Result{}, err
	}
	if a.Tail < 0 {
		return Result{}, ToolErrorf("tail must not be negative")
	}
	if err := requireSession(call, false); err != nil {
		return Result{}, err
	}
	out, err := w.sb.BuildLog(ctx, call.SessionID, step, a.Tail)
	if err != nil {
		return fail(err)
	}
	return Result{Content: out}, nil
}

// ---- helpers ----

// op runs a file operation. Mutating operations need the tool call id as
// op_id: without it a replay after a crash would run twice.
func (w *workspace) op(ctx context.Context, call Call, mutating bool, op sandbox.FileOp) (Result, error) {
	if err := requireSession(call, mutating); err != nil {
		return Result{}, err
	}
	opID := ""
	if mutating {
		opID = call.ID
	}
	res, err := w.sb.Op(ctx, call.SessionID, opID, op)
	if err != nil {
		return fail(err)
	}
	return Result{Content: res.Output}, nil
}

func requireSession(call Call, needID bool) error {
	if call.SessionID == "" {
		return fmt.Errorf("%s: session id is required", call.Name)
	}
	if needID && call.ID == "" {
		return fmt.Errorf("%s: tool call id is required (it is the idempotency key)", call.Name)
	}
	return nil
}

func parseStep(s string) (sandbox.BuildStep, error) {
	step := sandbox.BuildStep(s)
	if !step.Valid() {
		return "", ToolErrorf("step must be one of: install, typecheck, lint, build")
	}
	return step, nil
}

// fail maps a sandbox error. Tool-level failures (bad path, old_string not
// found, no build log yet) go to the model with the builder's own message.
// Everything else, including a lost workspace, goes to the caller as a Go
// error and never reaches the model.
func fail(err error) (Result, error) {
	if msg, ok := sandbox.ToolMessage(err); ok {
		return Result{}, &ToolError{Msg: msg}
	}
	return Result{}, err
}

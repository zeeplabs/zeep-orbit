package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
)

// fakeSandbox records calls and answers from scripted values. Unimplemented
// methods panic on the nil embedded interface, which fails the test loudly.
type fakeSandbox struct {
	sandbox.Sandbox

	ops    []opCall
	builds []buildCall
	logs   []string

	opRes    sandbox.OpResult
	opErr    error
	buildRes sandbox.BuildResult
	buildErr error
	logRes   string
	logErr   error
}

type opCall struct {
	session, opID string
	op            sandbox.FileOp
}

type buildCall struct {
	session, opID string
	step          sandbox.BuildStep
	previewBase   string
}

func (f *fakeSandbox) Op(_ context.Context, sid, opID string, op sandbox.FileOp) (sandbox.OpResult, error) {
	f.ops = append(f.ops, opCall{sid, opID, op})
	return f.opRes, f.opErr
}

func (f *fakeSandbox) Build(_ context.Context, sid, opID string, step sandbox.BuildStep, base string) (sandbox.BuildResult, error) {
	f.builds = append(f.builds, buildCall{sid, opID, step, base})
	return f.buildRes, f.buildErr
}

func (f *fakeSandbox) BuildLog(_ context.Context, sid string, step sandbox.BuildStep, tail int) (string, error) {
	f.logs = append(f.logs, sid+"/"+string(step))
	return f.logRes, f.logErr
}

func newTestRegistry(t *testing.T, sb sandbox.Sandbox, opts ...Option) *Registry {
	t.Helper()
	r := NewRegistry(append([]Option{WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}, opts...)...)
	if err := RegisterWorkspaceTools(r, sb, WorkspaceConfig{}); err != nil {
		t.Fatal(err)
	}
	return r
}

func call(name, args string) Call {
	return Call{ID: "call_1", Name: name, Arguments: json.RawMessage(args), SessionID: "sess-1"}
}

func TestDispatch_UnknownToolIsToolError(t *testing.T) {
	r := newTestRegistry(t, &fakeSandbox{})
	res, err := r.Dispatch(context.Background(), call("rm_rf\x00"+strings.Repeat("x", 200), `{}`))
	if err != nil {
		t.Fatalf("unknown tool must not be a Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "unknown tool") {
		t.Fatalf("got %+v", res)
	}
	if len(res.Content) > 200 {
		t.Fatalf("echoed tool name is not bounded: %d bytes", len(res.Content))
	}
}

func TestDispatch_InvalidArgsAreToolErrors(t *testing.T) {
	cases := []struct{ name, tool, args, want string }{
		{"bad json", ToolWriteFile, `{"path":`, "not valid JSON"},
		{"invalid json text kept as string", ToolWriteFile, `"{not json"`, "JSON object"},
		{"array", ToolReadFile, `[1]`, "JSON object"},
		{"unknown field", ToolWriteFile, `{"path":"a","contents":"x"}`, `unknown field "contents"`},
		{"wrong type", ToolReadFile, `{"path":1}`, `field "path" must be a string`},
		{"wrong int type", ToolReadFile, `{"path":"a","offset":"2"}`, `field "offset" must be an integer`},
		{"missing path", ToolReadFile, `{}`, "path is required"},
		{"missing content", ToolWriteFile, `{"path":"a"}`, "content is required"},
		{"empty old_string", ToolEditFile, `{"path":"a","old_string":"","new_string":"b"}`, "old_string"},
		{"missing new_string", ToolEditFile, `{"path":"a","old_string":"x"}`, "new_string is required"},
		{"bad step", ToolRunBuild, `{"step":"rm -rf /"}`, "step must be one of"},
		{"missing step", ToolGetBuildLogs, `{}`, "step must be one of"},
		{"negative tail", ToolGetBuildLogs, `{"step":"build","tail":-1}`, "tail"},
		{"trailing data", ToolListDir, `{} {}`, "unexpected data"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := &fakeSandbox{}
			r := newTestRegistry(t, fs)
			res, err := r.Dispatch(context.Background(), call(c.tool, c.args))
			if err != nil {
				t.Fatalf("invalid args must not be a Go error: %v", err)
			}
			if !res.IsError || !strings.Contains(res.Content, c.want) {
				t.Fatalf("got %+v, want error containing %q", res, c.want)
			}
			if strings.Contains(res.Content, "workspace.") || strings.Contains(res.Content, "Args") {
				t.Fatalf("Go type names leak into the message: %q", res.Content)
			}
			if len(fs.ops)+len(fs.builds)+len(fs.logs) != 0 {
				t.Fatal("the sandbox must not be called with invalid arguments")
			}
		})
	}
}

func TestDispatch_TruncationMarker(t *testing.T) {
	big := strings.Repeat("é", 3000) // 6000 bytes, multi-byte on purpose
	fs := &fakeSandbox{opRes: sandbox.OpResult{OK: true, Output: big}}
	r := newTestRegistry(t, fs, WithMaxResultBytes(1025))

	res, err := r.Dispatch(context.Background(), call(ToolReadFile, `{"path":"big.ts"}`))
	if err != nil || res.IsError || !res.Truncated {
		t.Fatalf("got %+v, %v", res, err)
	}
	if !strings.Contains(res.Content, "…truncated, use offset") {
		t.Fatalf("missing marker: %q", res.Content[len(res.Content)-60:])
	}
	if !utf8.ValidString(res.Content) {
		t.Fatal("truncation split a multi-byte character")
	}
	if len(res.Content) > 1025+80 {
		t.Fatalf("content is %d bytes, cap not applied", len(res.Content))
	}

	// Under the cap: untouched.
	fs.opRes.Output = "small"
	res, _ = r.Dispatch(context.Background(), call(ToolReadFile, `{"path":"a"}`))
	if res.Truncated || res.Content != "small" {
		t.Fatalf("small result altered: %+v", res)
	}

	// Build output keeps the END, where the error is.
	fs.buildRes = sandbox.BuildResult{ExitCode: 1, OutputTail: strings.Repeat("noise\n", 500) + "error TS2304: final line"}
	res, _ = r.Dispatch(context.Background(), call(ToolRunBuild, `{"step":"typecheck"}`))
	if !res.Truncated || !strings.HasSuffix(res.Content, "error TS2304: final line") || !strings.Contains(res.Content, "…truncated") {
		t.Fatalf("tail truncation: %q", res.Content)
	}
}

func TestRunBuild_FailureReturnsTailNotGoError(t *testing.T) {
	fs := &fakeSandbox{buildRes: sandbox.BuildResult{ExitCode: 2, OutputTail: "src/App.tsx(3,1): error TS2322", DurationMs: 1500}}
	r := newTestRegistry(t, fs)

	res, err := r.Dispatch(context.Background(), call(ToolRunBuild, `{"step":"typecheck"}`))
	if err != nil {
		t.Fatalf("a failed build is a result, not a Go error: %v", err)
	}
	if !res.IsError || !res.BuildFailed || !strings.Contains(res.Content, "error TS2322") || !strings.Contains(res.Content, "exit code 2") {
		t.Fatalf("got %+v", res)
	}

	fs.buildRes = sandbox.BuildResult{ExitCode: 0, OutputTail: "built", DurationMs: 900}
	res, err = r.Dispatch(context.Background(), call(ToolRunBuild, `{"step":"build"}`))
	if err != nil || res.IsError || res.BuildFailed || !strings.Contains(res.Content, "result: ok") {
		t.Fatalf("success: %+v %v", res, err)
	}

	fs.buildRes = sandbox.BuildResult{ExitCode: -1, TimedOut: true, OutputTail: "still installing"}
	res, err = r.Dispatch(context.Background(), call(ToolRunBuild, `{"step":"install"}`))
	if err != nil || !res.BuildFailed || !strings.Contains(res.Content, "timed out") || !strings.Contains(res.Content, "still installing") {
		t.Fatalf("timeout: %+v %v", res, err)
	}
}

func TestWorkspaceTools_OpIDIsToolCallID(t *testing.T) {
	fs := &fakeSandbox{opRes: sandbox.OpResult{OK: true, Output: "ok"}, buildRes: sandbox.BuildResult{}}
	r := NewRegistry(WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	err := RegisterWorkspaceTools(r, fs, WorkspaceConfig{PreviewBase: func(id string) string { return "/p/" + id + "/" }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mk := func(id, name, args string) Call {
		return Call{ID: id, Name: name, Arguments: json.RawMessage(args), SessionID: "s9"}
	}
	for _, c := range []Call{
		mk("call_w", ToolWriteFile, `{"path":"a.ts","content":""}`),
		mk("call_e", ToolEditFile, `{"path":"a.ts","old_string":"x","new_string":"","replace_all":true}`),
		mk("call_r", ToolReadFile, `{"path":"a.ts","offset":3,"limit":10}`),
		mk("call_l", ToolListDir, `{"path":"."}`),
		mk("call_b", ToolRunBuild, `{"step":"build"}`),
	} {
		if _, err := r.Dispatch(ctx, c); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
	}
	if len(fs.ops) != 4 || len(fs.builds) != 1 {
		t.Fatalf("ops %d builds %d", len(fs.ops), len(fs.builds))
	}
	w, e, rd, l := fs.ops[0], fs.ops[1], fs.ops[2], fs.ops[3]
	if w.opID != "call_w" || w.session != "s9" || w.op.Kind != sandbox.OpWriteFile || w.op.Content != "" {
		t.Errorf("write_file: %+v", w)
	}
	if e.opID != "call_e" || !e.op.ReplaceAll || e.op.NewString != "" {
		t.Errorf("edit_file: %+v", e)
	}
	if rd.opID != "" || rd.op.Offset != 3 || rd.op.Limit != 10 {
		t.Errorf("read_file must not carry an op_id: %+v", rd)
	}
	if l.opID != "" || l.op.Path != "" {
		t.Errorf("list_dir: %+v", l)
	}
	if b := fs.builds[0]; b.opID != "call_b" || b.step != sandbox.StepBuild || b.previewBase != "/p/s9/" {
		t.Errorf("run_build: %+v", b)
	}
}

func TestWorkspaceTools_SandboxErrors(t *testing.T) {
	ctx := context.Background()

	// Tool-level: the builder's message goes to the model.
	toolErr := &sandbox.Error{Status: 422, Code: "edit_not_found", Message: "old_string not found in src/a.ts"}
	fs := &fakeSandbox{opErr: toolErr, logErr: &sandbox.Error{Status: 404, Code: "not_found", Message: "no build log for step lint"}}
	r := newTestRegistry(t, fs)
	res, err := r.Dispatch(ctx, call(ToolEditFile, `{"path":"src/a.ts","old_string":"x","new_string":"y"}`))
	if err != nil || !res.IsError || res.Content != "old_string not found in src/a.ts" {
		t.Fatalf("tool-level: %+v %v", res, err)
	}
	res, err = r.Dispatch(ctx, call(ToolGetBuildLogs, `{"step":"lint"}`))
	if err != nil || !res.IsError || !strings.Contains(res.Content, "no build log") {
		t.Fatalf("no build log: %+v %v", res, err)
	}

	// Infrastructure: a Go error for the loop; nothing from it in the Result.
	secret := &sandbox.Error{Code: "unavailable", Message: "builder is unavailable", Err: errors.New("dial tcp 10.0.0.7:8081: refused")}
	for _, infra := range []error{
		secret,
		&sandbox.Error{Status: 401, Code: "unauthorized", Message: "builder rejected the request credentials"},
		&sandbox.Error{Status: 500, Code: "internal_error", Message: "builder failed to complete the request"},
		sandbox.ErrUnknownSession,
		sandbox.ErrWorkspaceLost,
		context.DeadlineExceeded,
	} {
		fs.opErr = infra
		res, err := r.Dispatch(ctx, call(ToolWriteFile, `{"path":"a","content":"b"}`))
		if err == nil {
			t.Fatalf("%v: infrastructure failures must be Go errors", infra)
		}
		if !errors.Is(err, infra) {
			t.Fatalf("cause lost for server-side handling: %v", err)
		}
		if res.Content != "" || res.IsError || res.Meta != nil {
			t.Fatalf("no result may accompany a Go error: %+v", res)
		}
	}
	fs.opErr = sandbox.ErrWorkspaceLost
	_, err = r.Dispatch(ctx, call(ToolReadFile, `{"path":"a"}`))
	if !errors.Is(err, sandbox.ErrWorkspaceLost) {
		t.Fatalf("workspace_lost must reach the loop: %v", err)
	}
}

func TestWorkspaceTools_RequireSessionAndCallID(t *testing.T) {
	fs := &fakeSandbox{}
	r := newTestRegistry(t, fs)
	ctx := context.Background()

	c := call(ToolWriteFile, `{"path":"a","content":"b"}`)
	c.ID = ""
	if _, err := r.Dispatch(ctx, c); err == nil {
		t.Fatal("a mutating call without an id has no idempotency key and must fail")
	}
	c = call(ToolListDir, `{}`)
	c.SessionID = ""
	if _, err := r.Dispatch(ctx, c); err == nil {
		t.Fatal("a call without a session must fail")
	}
	// Reads do not need an id.
	c = call(ToolListDir, `{}`)
	c.ID = ""
	if _, err := r.Dispatch(ctx, c); err != nil {
		t.Fatalf("read without id: %v", err)
	}
	if _, err := NewWorkspaceTools(nil, WorkspaceConfig{}); err == nil {
		t.Fatal("nil sandbox must be rejected")
	}
}

func TestRegistry_SpecsKindsAndExtension(t *testing.T) {
	r := newTestRegistry(t, &fakeSandbox{})

	specs := r.Specs()
	want := []string{ToolListDir, ToolReadFile, ToolWriteFile, ToolEditFile, ToolRunBuild, ToolGetBuildLogs}
	if len(specs) != len(want) {
		t.Fatalf("got %d specs", len(specs))
	}
	for i, s := range specs {
		if s.Name != want[i] || s.Description == "" {
			t.Errorf("spec %d: %+v", i, s)
		}
		var schema map[string]any
		if err := json.Unmarshal(s.Schema, &schema); err != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s: bad schema %s (%v)", s.Name, s.Schema, err)
		}
	}
	kinds := map[string]Kind{
		ToolListDir: KindReadOnly, ToolReadFile: KindReadOnly, ToolGetBuildLogs: KindReadOnly,
		ToolWriteFile: KindWorkspace, ToolEditFile: KindWorkspace, ToolRunBuild: KindWorkspace,
	}
	for name, k := range kinds {
		if got, ok := r.Kind(name); !ok || got != k {
			t.Errorf("%s: kind %v ok=%v, want %v", name, got, ok, k)
		}
	}
	if _, ok := r.Kind("nope"); ok {
		t.Error("unknown tool has no kind")
	}

	// An outside package (T16: the dashboard) registers its own tools.
	err := r.Register(Tool{
		Name: "propose_backend_change", Description: "d", Kind: KindProposal,
		Schema: json.RawMessage(`{"type":"object"}`),
		Handler: func(_ context.Context, c Call) (Result, error) {
			var a struct {
				Operation string `json:"operation"`
			}
			if err := DecodeArgs(c.Arguments, &a); err != nil {
				return Result{}, err
			}
			return Result{}, ToolErrorf("malformed proposal: %s", "bad column")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if k, _ := r.Kind("propose_backend_change"); k != KindProposal {
		t.Fatalf("kind %v", k)
	}
	res, err := r.Dispatch(context.Background(), call("propose_backend_change", `{"operation":"x"}`))
	if err != nil || !res.IsError || !strings.Contains(res.Content, "malformed proposal") {
		t.Fatalf("custom tool: %+v %v", res, err)
	}
	if got := r.Specs(); got[len(got)-1].Name != "propose_backend_change" {
		t.Fatal("registration order must be kept")
	}
}

func TestRegistry_RegisterValidationAndSafety(t *testing.T) {
	ok := func(context.Context, Call) (Result, error) { return Result{Content: "x"}, nil }
	schema := json.RawMessage(`{"type":"object"}`)
	r := NewRegistry(WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	bad := []Tool{
		{Name: "", Schema: schema, Kind: KindReadOnly, Handler: ok},
		{Name: "has space", Schema: schema, Kind: KindReadOnly, Handler: ok},
		{Name: strings.Repeat("a", 65), Schema: schema, Kind: KindReadOnly, Handler: ok},
		{Name: "nohandler", Schema: schema, Kind: KindReadOnly},
		{Name: "nokind", Schema: schema, Handler: ok},
		{Name: "badschema", Schema: json.RawMessage(`[1]`), Kind: KindReadOnly, Handler: ok},
		{Name: "noschema", Kind: KindReadOnly, Handler: ok},
	}
	for _, tl := range bad {
		if err := r.Register(tl); err == nil {
			t.Errorf("Register(%q) should fail", tl.Name)
		}
	}
	if err := r.Register(Tool{Name: "dup", Schema: schema, Kind: KindReadOnly, Handler: ok}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Tool{Name: "dup", Schema: schema, Kind: KindReadOnly, Handler: ok}); err == nil {
		t.Error("duplicate must fail")
	}

	// A panicking handler is contained and reported generically.
	_ = r.Register(Tool{Name: "boom", Schema: schema, Kind: KindReadOnly, Handler: func(context.Context, Call) (Result, error) {
		panic("secret internal detail")
	}})
	res, err := r.Dispatch(context.Background(), Call{Name: "boom"})
	if err != nil || !res.IsError || strings.Contains(res.Content, "secret") {
		t.Fatalf("panic: %+v %v", res, err)
	}

	// An unexpected handler error is a Go error, never model text.
	_ = r.Register(Tool{Name: "leaky", Schema: schema, Kind: KindReadOnly, Handler: func(context.Context, Call) (Result, error) {
		return Result{Content: "ignored"}, errors.New("pq: password authentication failed")
	}})
	res, err = r.Dispatch(context.Background(), Call{Name: "leaky"})
	if err == nil || res.Content != "" {
		t.Fatalf("raw error must not become a result: %+v %v", res, err)
	}

	// A cancelled context stops before running anything.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Dispatch(ctx, Call{Name: "dup"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx: %v", err)
	}
}

func TestSpecsWithoutOmitsNamedTools(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"a_tool", "b_tool", "c_tool"} {
		if err := r.Register(Tool{
			Name: name, Schema: json.RawMessage(`{"type":"object"}`), Kind: KindReadOnly,
			Handler: func(context.Context, Call) (Result, error) { return Result{}, nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	names := func(specs []provider.ToolSpec) string {
		var out []string
		for _, s := range specs {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(r.SpecsWithout("b_tool")); got != "a_tool,c_tool" {
		t.Fatalf("got %s", got)
	}
	if got := names(r.SpecsWithout()); got != names(r.Specs()) {
		t.Fatalf("no names must equal Specs, got %s", got)
	}
	if got := names(r.SpecsWithout("missing", "a_tool", "c_tool")); got != "b_tool" {
		t.Fatalf("got %s", got)
	}
	if got := names(r.Specs()); got != "a_tool,b_tool,c_tool" {
		t.Fatalf("Specs changed: %s", got)
	}
}

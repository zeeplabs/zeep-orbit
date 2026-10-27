package agent

// fakes_test.go: in-memory collaborators for the loop and compaction tests.
// They model the contracts the loop relies on (lease-checked writes, builder
// op_id idempotency, commit bundles), not the real implementations.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

// ---- fake store ----

type fakeEvent struct {
	Type    string
	Payload json.RawMessage
}

type fakeStore struct {
	mu     sync.Mutex
	turn   *store.Turn
	sess   *store.Session
	msgs   []store.Message
	nextID int64
	events []fakeEvent
	snaps  []store.Snapshot // append-only, in (turn_seq, part) order
	usage  []store.UsageRecord
	finish *store.FinishParams

	// hooks
	afterUsage func(n int) // called after the n-th RecordUsage
}

func newFakeStore() *fakeStore {
	owner := "w1"
	return &fakeStore{
		turn: &store.Turn{ID: "turn-1", SessionID: "sess-1", Seq: 1, Kind: store.KindChat, Status: store.TurnRunning, LeaseOwner: &owner},
		sess: &store.Session{ID: "sess-1", OwnerUserID: "user-1", BackendAppID: "app-1", Locale: "en", HeadSHA: "sha0", Status: store.SessionRunning},
		snaps: []store.Snapshot{
			{SessionID: "sess-1", TurnSeq: 0, Part: 0, CommitSHA: "sha0", Bundle: []byte("bundle-0")},
		},
	}
}

func (f *fakeStore) lease() store.Lease { return store.Lease{TurnID: f.turn.ID, Owner: "w1"} }

// seed adds a message without a lease check (history of earlier turns).
func (f *fakeStore) seed(turnID, role, content string) store.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addLocked(turnID, role, content, nil, "")
}

func (f *fakeStore) addLocked(turnID, role, content string, toolCalls json.RawMessage, toolCallID string) store.Message {
	f.nextID++
	m := store.Message{ID: f.nextID, SessionID: f.sess.ID, TurnID: turnID, Role: role, Content: content, ToolCalls: toolCalls, ToolCallID: toolCallID}
	f.msgs = append(f.msgs, m)
	return m
}

// startTurn makes a new running turn owned by "w1" with a user message.
func (f *fakeStore) startTurn(id string, seq int, userText string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := "w1"
	f.turn = &store.Turn{ID: id, SessionID: f.sess.ID, Seq: seq, Kind: store.KindChat, Status: store.TurnRunning, LeaseOwner: &owner}
	f.finish = nil
	f.addLocked(id, store.RoleUser, userText, nil, "")
}

func (f *fakeStore) checkLease(l store.Lease) error {
	if f.turn == nil || f.turn.ID != l.TurnID || f.turn.Status != store.TurnRunning ||
		f.turn.LeaseOwner == nil || *f.turn.LeaseOwner != l.Owner {
		return store.ErrLeaseLost
	}
	return nil
}

func (f *fakeStore) GetTurn(_ context.Context, id string) (*store.Turn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.turn == nil || f.turn.ID != id {
		return nil, store.ErrNotFound
	}
	t := *f.turn
	return &t, nil
}

func (f *fakeStore) GetSessionByID(_ context.Context, id string) (*store.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := *f.sess
	return &s, nil
}

func (f *fakeStore) ListMessages(_ context.Context, _ string) ([]store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Message(nil), f.msgs...), nil
}

func (f *fakeStore) AppendMessage(ctx context.Context, l store.Lease, m store.NewMessage) (*store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.checkLease(l); err != nil {
		return nil, err
	}
	out := f.addLocked(l.TurnID, m.Role, m.Content, m.ToolCalls, m.ToolCallID)
	return &out, nil
}

func (f *fakeStore) AppendEvent(ctx context.Context, l store.Lease, typ string, payload any) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := f.checkLease(l); err != nil {
		return 0, err
	}
	raw := []byte("{}")
	if payload != nil {
		var err error
		if raw, err = json.Marshal(payload); err != nil {
			return 0, err
		}
	}
	f.events = append(f.events, fakeEvent{Type: typ, Payload: raw})
	return int64(len(f.events)), nil
}

func (f *fakeStore) SaveSnapshot(ctx context.Context, l store.Lease, sha string, bundle []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.checkLease(l); err != nil {
		return err
	}
	part := 0
	for _, sn := range f.snaps {
		if sn.TurnSeq == f.turn.Seq {
			part = sn.Part + 1
		}
	}
	f.snaps = append(f.snaps, store.Snapshot{SessionID: f.sess.ID, TurnSeq: f.turn.Seq, Part: part, CommitSHA: sha, Bundle: append([]byte(nil), bundle...)})
	return nil
}

func (f *fakeStore) ListSnapshots(_ context.Context, _ string) ([]store.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Snapshot(nil), f.snaps...), nil
}

func (f *fakeStore) FinishTurn(ctx context.Context, l store.Lease, p store.FinishParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.checkLease(l); err != nil {
		return err
	}
	cp := p
	f.finish = &cp
	f.turn.Status = p.Status
	f.turn.LeaseOwner = nil
	if p.Status == store.TurnAwaitingConfirmation {
		f.turn.PendingProposal = p.PendingProposal
	}
	return nil
}

func (f *fakeStore) RecordUsage(_ context.Context, r store.UsageRecord) error {
	f.mu.Lock()
	f.usage = append(f.usage, r)
	n := len(f.usage)
	hook := f.afterUsage
	f.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return nil
}

func (f *fakeStore) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	for i, e := range f.events {
		out[i] = e.Type
	}
	return out
}

func (f *fakeStore) eventsOf(typ string) []fakeEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeEvent
	for _, e := range f.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func (f *fakeStore) messagesOf(role string) []store.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Message
	for _, m := range f.msgs {
		if m.Role == role {
			out = append(out, m)
		}
	}
	return out
}

// ---- fake provider ----

type scriptStep struct {
	resp provider.ChatResponse
	err  error
}

type fakeProvider struct {
	mu     sync.Mutex
	script []scriptStep
	reqs   []provider.ChatRequest
}

func (p *fakeProvider) Name() string  { return "fake" }
func (p *fakeProvider) Model() string { return "fake-1" }

func (p *fakeProvider) Chat(_ context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	if len(p.script) == 0 {
		return textResp("done"), nil
	}
	s := p.script[0]
	p.script = p.script[1:]
	return s.resp, s.err
}

func (p *fakeProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.reqs)
}

func textResp(s string) provider.ChatResponse {
	return provider.ChatResponse{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: s},
		Usage:        provider.Usage{InputTokens: 10, OutputTokens: 5},
		FinishReason: provider.FinishStop,
	}
}

func toolResp(calls ...provider.ToolCall) provider.ChatResponse {
	return provider.ChatResponse{
		Message:      provider.Message{Role: provider.RoleAssistant, ToolCalls: calls},
		ToolCalls:    calls,
		Usage:        provider.Usage{InputTokens: 10, OutputTokens: 5},
		FinishReason: provider.FinishToolCalls,
	}
}

func writeCall(id, path string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: tools.ToolWriteFile, Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"x"}`, path))}
}

func listCall(id string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: tools.ToolListDir, Arguments: json.RawMessage(`{"path":""}`)}
}

func buildCall(id, step string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: tools.ToolRunBuild, Arguments: json.RawMessage(fmt.Sprintf(`{"step":%q}`, step))}
}

func proposeCall(id, args string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: "propose_backend_change", Arguments: json.RawMessage(args)}
}

// ---- fake sandbox ----

type opRecord struct {
	opID string
	kind string
	path string
}

type fakeSandbox struct {
	mu              sync.Mutex
	sandbox.Sandbox // unused methods panic on nil, which flags unexpected use
	ops             []opRecord
	applied         map[string]sandbox.OpResult
	builds          []sandbox.BuildResult // queue; empty means exit 0
	buildSeen       int
	head            string
	dirty           bool
	commits         int
	opErr           error
	onOp            func(opID string)
	health          sandbox.Health
	healthErr       error
}

func newFakeSandbox() *fakeSandbox {
	return &fakeSandbox{applied: map[string]sandbox.OpResult{}, head: "sha0"}
}

func (s *fakeSandbox) Op(_ context.Context, _, opID string, op sandbox.FileOp) (sandbox.OpResult, error) {
	s.mu.Lock()
	s.ops = append(s.ops, opRecord{opID: opID, kind: op.Kind, path: op.Path})
	hook, opErr := s.onOp, s.opErr
	s.mu.Unlock()
	if hook != nil {
		hook(opID)
	}
	if opErr != nil {
		return sandbox.OpResult{}, opErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if opID != "" {
		if r, ok := s.applied[opID]; ok {
			r.Replayed = true
			return r, nil
		}
	}
	if op.Kind == sandbox.OpWriteFile || op.Kind == sandbox.OpEditFile {
		s.dirty = true
	}
	r := sandbox.OpResult{OK: true, Output: "ok " + op.Kind + " " + op.Path}
	if opID != "" {
		s.applied[opID] = r
	}
	return r, nil
}

func (s *fakeSandbox) Build(_ context.Context, _, _ string, step sandbox.BuildStep, _ string) (sandbox.BuildResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buildSeen++
	if len(s.builds) == 0 {
		return sandbox.BuildResult{OutputTail: "build ok"}, nil
	}
	r := s.builds[0]
	if len(s.builds) > 1 {
		s.builds = s.builds[1:]
	}
	return r, nil
}

func (s *fakeSandbox) Commit(_ context.Context, _ string, _ int, _ string) (sandbox.CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return sandbox.CommitResult{SHA: s.head, Prev: s.head}, nil
	}
	s.commits++
	prev := s.head
	s.head = fmt.Sprintf("sha%d", s.commits)
	s.dirty = false
	return sandbox.CommitResult{SHA: s.head, Prev: prev, Changed: true, Bundle: []byte("bundle-" + s.head)}, nil
}

func (s *fakeSandbox) Health(context.Context) (sandbox.Health, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health, s.healthErr
}

func (s *fakeSandbox) opIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.ops))
	for _, o := range s.ops {
		out = append(out, o.opID)
	}
	return out
}

var _ sandbox.Sandbox = (*fakeSandbox)(nil)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// ---- fake budget ----

type fakeBudget struct {
	mu    sync.Mutex
	calls int
	// fn returns the result of the n-th check (1-based).
	fn func(n int) error
}

func (b *fakeBudget) Check(context.Context, string, string) error {
	b.mu.Lock()
	b.calls++
	n := b.calls
	b.mu.Unlock()
	if b.fn == nil {
		return nil
	}
	return b.fn(n)
}

// ---- harness ----

type harness struct {
	t    *testing.T
	st   *fakeStore
	prov *fakeProvider
	sb   *fakeSandbox
	bud  *fakeBudget
	reg  *tools.Registry
	deps Deps
}

func newHarness(t *testing.T, script ...scriptStep) *harness {
	t.Helper()
	h := &harness{t: t, st: newFakeStore(), prov: &fakeProvider{script: script}, sb: newFakeSandbox(), bud: &fakeBudget{}}
	h.st.seed("turn-1", store.RoleUser, "build me a tasks page")
	h.reg = tools.NewRegistry()
	if err := tools.RegisterWorkspaceTools(h.reg, h.sb, tools.WorkspaceConfig{}); err != nil {
		t.Fatalf("register workspace tools: %v", err)
	}
	if err := h.reg.Register(tools.Tool{
		Name: "propose_backend_change", Description: "propose", Kind: tools.KindProposal,
		Schema: json.RawMessage(`{"type":"object"}`),
		Handler: func(_ context.Context, c tools.Call) (tools.Result, error) {
			if string(c.Arguments) == `{"bad":true}` {
				return tools.Result{}, tools.ToolErrorf("malformed proposal")
			}
			return tools.Result{Content: "recorded"}, nil
		},
	}); err != nil {
		t.Fatalf("register proposal tool: %v", err)
	}
	h.deps = Deps{Store: h.st, Provider: h.prov, Tools: h.reg, Builder: h.sb, Budget: h.bud, Logger: quietLogger()}
	return h
}

func (h *harness) run(ctx context.Context) error {
	h.t.Helper()
	return RunTurn(ctx, h.deps, h.st.lease())
}

func (h *harness) mustRun() {
	h.t.Helper()
	if err := h.run(context.Background()); err != nil {
		h.t.Fatalf("RunTurn: %v", err)
	}
}

func (h *harness) finished() store.FinishParams {
	h.t.Helper()
	if h.st.finish == nil {
		h.t.Fatalf("turn was not finished; events: %v", h.st.eventTypes())
	}
	return *h.st.finish
}

func errProviderSecret() error {
	return &provider.ErrProvider{Status: 500, Err: errors.New("dial secret-host.internal:443: connection refused"), Body: `{"error":"key sk-secret"}`}
}

// fixedClock returns the successive times of a script, then the last one.
func scriptedClock(times ...time.Time) func() time.Time {
	var mu sync.Mutex
	i := 0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := times[min(i, len(times)-1)]
		i++
		return t
	}
}

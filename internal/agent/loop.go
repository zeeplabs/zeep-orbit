// Package agent runs the AI Frontend Builder's agent: the resumable turn loop
// (RunTurn), context compaction and the per-replica turn worker.
//
// Everything a turn needs is persisted in Postgres through [Store]; nothing is
// kept in process memory that another replica would need to continue the turn.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

// Store is the persistence the loop needs. *store.Store satisfies it; loop
// tests use an in-memory fake. Every write takes the turn's lease and fails
// with store.ErrLeaseLost once another replica owns the turn.
type Store interface {
	GetTurn(ctx context.Context, turnID string) (*store.Turn, error)
	GetSessionByID(ctx context.Context, sessionID string) (*store.Session, error)
	ListMessages(ctx context.Context, sessionID string) ([]store.Message, error)
	AppendMessage(ctx context.Context, l store.Lease, m store.NewMessage) (*store.Message, error)
	AppendEvent(ctx context.Context, l store.Lease, eventType string, payload any) (int64, error)
	SaveSnapshot(ctx context.Context, l store.Lease, commitSHA string, bundle []byte) error
	ListSnapshots(ctx context.Context, sessionID string) ([]store.Snapshot, error)
	FinishTurn(ctx context.Context, l store.Lease, p store.FinishParams) error
	RecordUsage(ctx context.Context, r store.UsageRecord) error
}

var (
	_ Store          = (*store.Store)(nil)
	_ WorkerStore    = (*store.Store)(nil)
	_ BudgetChecker  = (*store.BudgetChecker)(nil)
	_ SnapshotLister = (*store.Store)(nil)
)

// BudgetChecker decides whether another model call is allowed. Errors that
// wrap store.ErrBudgetExceeded end the turn with budget_exceeded.
// *store.BudgetChecker satisfies it.
type BudgetChecker interface {
	Check(ctx context.Context, userID, sessionID string) error
}

// PromptInput is the variable part of the system prompt of a session.
type PromptInput struct {
	// Company is the company persona + rules layer (see [CompanyLayer]).
	Company string
	// TemplateGuide is the template's ORBIT_AGENT.md, if any.
	TemplateGuide string
	// PacksIndex is the knowledge pack index (empty in P1).
	PacksIndex string
	// Session is the session context (backend app, schema summary, file tree,
	// locale).
	Session SessionContext
}

// PromptSource builds the [PromptInput] of a session. It runs once per
// RunTurn. When Deps.Prompt is nil the prompt gets only the backend app id and
// the session locale.
type PromptSource func(ctx context.Context, s *store.Session) (PromptInput, error)

// Deps are the collaborators of RunTurn.
type Deps struct {
	Store    Store
	Provider provider.Provider
	Tools    *tools.Registry
	// Builder should be wrapped with sandbox.NewRestoring so a lost builder
	// volume is restored from snapshots (see [SnapshotProvider]).
	Builder sandbox.Sandbox
	Budget  BudgetChecker
	Limits  Limits
	Prompt  PromptSource
	// Clock defaults to time.Now.
	Clock func() time.Time
	// Logger receives the raw errors that must never reach users or events.
	// Defaults to slog.Default().
	Logger *slog.Logger
}

func (d Deps) validate() error {
	switch {
	case d.Store == nil:
		return errors.New("agent: Deps.Store is required")
	case d.Provider == nil:
		return errors.New("agent: Deps.Provider is required")
	case d.Tools == nil:
		return errors.New("agent: Deps.Tools is required")
	case d.Builder == nil:
		return errors.New("agent: Deps.Builder is required")
	case d.Budget == nil:
		return errors.New("agent: Deps.Budget is required")
	}
	return nil
}

// outcome is how a turn ends.
type outcome struct {
	status string // a store.Turn* status
	code   string // generic error code (failed)
	limit  string // Limit* (limit_reached)
	// commit asks for the final Builder.Commit + snapshot.
	commit   bool
	proposal json.RawMessage // awaiting_confirmation
}

const (
	maxAssistantEventChars = 8000
	maxBuildTailChars      = 1500
	commitMessagePrefix    = "turn "
)

// run holds the state of one RunTurn invocation. It is rebuilt from the DB on
// every invocation.
type run struct {
	deps  Deps
	lim   Limits
	lease store.Lease
	turn  *store.Turn
	sess  *store.Session
	log   *slog.Logger
	now   func() time.Time

	start  time.Time
	system string
	msgs   []store.Message
	state  turnState

	// wrote: workspace files were written since the last checkpoint.
	wrote bool

	// snapshot chain bookkeeping, loaded lazily on the first commit.
	snapLoaded bool
	snapParts  []snapPart // bundles already stored for this turn's row
	lastSHA    string     // newest commit known to the stored snapshots
}

// RunTurn runs (or resumes) one turn that the caller has claimed with lease.
//
// It rebuilds its state from the database, so calling it again after a crash
// continues where the previous attempt stopped: tool calls without a result are
// re-sent to the builder with the same op_id (the builder replays them
// idempotently), and the step, fix-iteration and compaction state come from
// persisted messages.
//
// RunTurn returns nil when the turn reached a state it persisted (completed,
// failed, limit_reached, budget_exceeded or awaiting_confirmation). It returns
// an error only when it stopped without finishing the turn: store.ErrLeaseLost
// (another replica owns it now; nothing further was written), the context's
// error (shutdown; the lease expires and the turn resumes elsewhere), or a
// failure to persist the end state.
//
// SPEC_DEVIATION: the design signature is RunTurn(ctx, deps, turnID). The loop
// also needs the lease owner for its lease-checked writes, so it takes the
// store.Lease the worker claimed instead of a bare turn id.
func RunTurn(ctx context.Context, deps Deps, lease store.Lease) error {
	if err := deps.validate(); err != nil {
		return err
	}
	r := &run{deps: deps, lim: deps.Limits.withDefaults(), lease: lease, log: deps.Logger, now: deps.Clock}
	if r.log == nil {
		r.log = slog.Default()
	}
	if r.now == nil {
		r.now = time.Now
	}
	r.start = r.now()

	turn, err := deps.Store.GetTurn(ctx, lease.TurnID)
	if err != nil {
		return fmt.Errorf("agent: load turn: %w", err)
	}
	if turn.Status != store.TurnRunning || turn.LeaseOwner == nil || *turn.LeaseOwner != lease.Owner {
		return store.ErrLeaseLost
	}
	r.turn = turn
	sess, err := deps.Store.GetSessionByID(ctx, turn.SessionID)
	if err != nil {
		return fmt.Errorf("agent: load session: %w", err)
	}
	r.sess = sess

	out, err := r.run(ctx)
	if err != nil {
		return r.abort(ctx, err)
	}
	return r.finish(ctx, out)
}

// abort handles an error that stopped the turn before it had an outcome.
// Lease loss and cancellation end silently; anything else is an infrastructure
// failure: the turn is failed with a generic code on a best-effort basis.
func (r *run) abort(ctx context.Context, err error) error {
	if errors.Is(err, store.ErrLeaseLost) || ctx.Err() != nil {
		return err
	}
	r.log.Error("agent turn aborted", "turn", r.turn.ID, "session", r.turn.SessionID, "err", err)
	if ferr := r.finish(ctx, &outcome{status: store.TurnFailed, code: CodeInternal}); ferr != nil {
		return errors.Join(err, ferr)
	}
	return err
}

func (r *run) run(ctx context.Context) (*outcome, error) {
	if r.turn.Kind != store.KindChat {
		return &outcome{status: store.TurnFailed, code: CodeUnsupportedTurn}, nil
	}

	msgs, err := r.deps.Store.ListMessages(ctx, r.sess.ID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	r.msgs = msgs
	r.state = analyzeTurn(msgs, r.turn.ID)
	r.wrote = r.state.started // work from before a crash may be uncommitted

	tctx, cancel := context.WithTimeout(ctx, r.lim.MaxTurnDuration)
	defer cancel()

	if err := r.emit(ctx, EventTurnStarted, map[string]any{"seq": r.turn.Seq, "resumed": r.state.started}); err != nil {
		return nil, err
	}
	if r.state.lastAssistantNoTools {
		return &outcome{status: store.TurnCompleted, commit: true}, nil
	}

	pin := PromptInput{Session: SessionContext{BackendAppID: r.sess.BackendAppID, Locale: r.sess.Locale}}
	if r.deps.Prompt != nil {
		if pin, err = r.deps.Prompt(ctx, r.sess); err != nil {
			r.log.Error("agent prompt source failed", "session", r.sess.ID, "err", err)
			return &outcome{status: store.TurnFailed, code: CodeInternal}, nil
		}
	}
	r.system = BuildSystemPrompt(CorePrompt(), pin.Company, pin.TemplateGuide, pin.PacksIndex, pin.Session)

	// Tool calls persisted by a previous attempt but not answered yet.
	if len(r.state.pending) > 0 {
		if out, err := r.runCalls(ctx, tctx, r.state.pending); out != nil || err != nil {
			return out, err
		}
	}

	for {
		if out, err := r.checkLimits(ctx, tctx); out != nil || err != nil {
			return out, err
		}
		if out, err := r.checkBudget(ctx, tctx); out != nil || err != nil {
			return out, err
		}

		resp, out, err := r.chat(ctx, tctx)
		if out != nil || err != nil {
			return out, err
		}
		calls, err := r.persistAssistant(ctx, resp)
		if err != nil {
			return nil, err
		}
		if len(calls) == 0 {
			return &outcome{status: store.TurnCompleted, commit: true}, nil
		}
		if out, err := r.runCalls(ctx, tctx, calls); out != nil || err != nil {
			return out, err
		}
	}
}

// checkLimits ends the turn when a limit is reached. Reaching a limit still
// commits the partial work.
func (r *run) checkLimits(ctx, tctx context.Context) (*outcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if tctx.Err() != nil || r.now().Sub(r.start) >= r.lim.MaxTurnDuration {
		return &outcome{status: store.TurnLimitReached, limit: LimitDuration, commit: true}, nil
	}
	if r.state.steps >= r.lim.MaxSteps {
		return &outcome{status: store.TurnLimitReached, limit: LimitSteps, commit: true}, nil
	}
	if r.state.failedBuilds >= r.lim.MaxFixIterations {
		return &outcome{status: store.TurnLimitReached, limit: LimitFixes, commit: true}, nil
	}
	return nil, nil
}

// checkBudget runs before every model call. When the cap is hit no call is
// made.
func (r *run) checkBudget(ctx, tctx context.Context) (*outcome, error) {
	err := r.deps.Budget.Check(tctx, r.sess.OwnerUserID, r.sess.ID)
	if err == nil {
		return nil, nil
	}
	if errors.Is(err, store.ErrBudgetExceeded) {
		return &outcome{status: store.TurnBudgetExceeded, commit: true}, nil
	}
	if out, cerr := r.ctxEnd(ctx, tctx); out != nil || cerr != nil {
		return out, cerr
	}
	r.log.Error("agent budget check failed", "session", r.sess.ID, "err", err)
	return &outcome{status: store.TurnFailed, code: CodeInternal, commit: true}, nil
}

// ctxEnd maps a context failure: the caller's context ending aborts the turn
// (shutdown), the turn's own deadline ends it as limit_reached.
func (r *run) ctxEnd(ctx, tctx context.Context) (*outcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if tctx.Err() != nil {
		return &outcome{status: store.TurnLimitReached, limit: LimitDuration, commit: true}, nil
	}
	return nil, nil
}

// chat performs one model call (compacting the history first when needed),
// records its usage and returns the response. A context-too-long answer is
// retried once with a forced compaction.
func (r *run) chat(ctx, tctx context.Context) (provider.ChatResponse, *outcome, error) {
	var resp provider.ChatResponse
	force := false
	for {
		prompt, err := r.promptMessages(ctx, tctx, force)
		if err != nil {
			return resp, nil, err
		}
		resp, err = r.deps.Provider.Chat(tctx, provider.ChatRequest{
			System:          r.system,
			Messages:        prompt,
			Tools:           r.deps.Tools.Specs(),
			MaxOutputTokens: r.lim.MaxOutputTokens,
		})
		if err == nil {
			break
		}
		if out, cerr := r.ctxEnd(ctx, tctx); out != nil || cerr != nil {
			return resp, out, cerr
		}
		if errors.Is(err, provider.ErrContextTooLong) {
			if !force {
				force = true
				continue
			}
			return resp, &outcome{status: store.TurnFailed, code: CodeContextTooLong, commit: true}, nil
		}
		// Raw provider text only goes to the server log.
		r.log.Error("agent provider call failed", "turn", r.turn.ID, "session", r.sess.ID,
			"provider", r.deps.Provider.Name(), "model", r.deps.Provider.Model(), "err", err, "cause", errors.Unwrap(err))
		return resp, &outcome{status: store.TurnFailed, code: CodeProviderUnavailable, commit: true}, nil
	}

	// Usage first: the tokens are spent whether or not the rest persists.
	if err := r.deps.Store.RecordUsage(ctx, store.UsageRecord{
		UserID: r.sess.OwnerUserID, Feature: store.FeatureBuilder, SessionID: r.sess.ID, TurnID: r.turn.ID,
		Provider: r.deps.Provider.Name(), Model: r.deps.Provider.Model(),
		InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
	}); err != nil {
		return resp, nil, fmt.Errorf("record usage: %w", err)
	}
	return resp, nil, nil
}

// persistAssistant stores the assistant message (with its tool calls) before
// any call runs, and returns the calls to execute.
func (r *run) persistAssistant(ctx context.Context, resp provider.ChatResponse) ([]provider.ToolCall, error) {
	calls := resp.Message.ToolCalls
	if len(calls) == 0 {
		calls = resp.ToolCalls
	}
	calls = append([]provider.ToolCall(nil), calls...)
	for i := range calls {
		if calls[i].ID == "" {
			// Persisted with the message, so the id is stable across a resume.
			calls[i].ID = fmt.Sprintf("call_%d_%d", r.state.steps+1, i)
		}
	}
	nm := store.NewMessage{Role: store.RoleAssistant, Content: resp.Message.Content}
	if len(calls) > 0 {
		raw, err := json.Marshal(calls)
		if err != nil {
			return nil, fmt.Errorf("marshal tool calls: %w", err)
		}
		nm.ToolCalls = raw
	}
	m, err := r.deps.Store.AppendMessage(ctx, r.lease, nm)
	if err != nil {
		return nil, fmt.Errorf("persist assistant message: %w", err)
	}
	r.msgs = append(r.msgs, *m)
	r.state.steps++
	if strings.TrimSpace(resp.Message.Content) != "" {
		if err := r.emit(ctx, EventAssistantMessage, map[string]any{"content": capRunes(resp.Message.Content, maxAssistantEventChars)}); err != nil {
			return nil, err
		}
	}
	return calls, nil
}

// runCalls executes one tool batch in order: each call is announced, executed,
// and its result persisted before the next one. A successful proposal call
// pauses the turn (nothing is committed; the result arrives later with the
// user's decision, and any calls after it run on resume). After a batch that
// wrote files the workspace is committed and snapshotted, so a builder volume
// loss costs at most one batch.
func (r *run) runCalls(ctx, tctx context.Context, calls []provider.ToolCall) (*outcome, error) {
	for _, c := range calls {
		if err := r.emit(ctx, EventToolCall, map[string]any{"name": c.Name, "summary": toolSummary(c.Arguments)}); err != nil {
			return nil, err
		}
		isBuild := c.Name == tools.ToolRunBuild
		if isBuild {
			if err := r.emit(ctx, EventBuildStarted, map[string]any{"step": argString(c.Arguments, "step")}); err != nil {
				return nil, err
			}
		}

		res, err := r.deps.Tools.Dispatch(tctx, tools.Call{ID: c.ID, Name: c.Name, Arguments: c.Arguments, SessionID: r.sess.ID})
		if err != nil {
			return r.toolFailure(ctx, tctx, c, err)
		}

		if kind, ok := r.deps.Tools.Kind(c.Name); ok && !res.IsError &&
			(kind == tools.KindProposal || kind == tools.KindExternalMutating) {
			return r.pauseForProposal(ctx, c)
		}

		meta, _ := json.Marshal(toolMeta{BuildFailed: res.BuildFailed, IsError: res.IsError})
		m, err := r.deps.Store.AppendMessage(ctx, r.lease, store.NewMessage{
			Role: store.RoleTool, Content: res.Content, ToolCallID: c.ID, ToolCalls: meta,
		})
		if err != nil {
			return nil, fmt.Errorf("persist tool result: %w", err)
		}
		r.msgs = append(r.msgs, *m)

		if res.BuildFailed {
			r.state.failedBuilds++
		}
		if err := r.emitResult(ctx, c, res); err != nil {
			return nil, err
		}
		if (c.Name == tools.ToolWriteFile || c.Name == tools.ToolEditFile) && !res.IsError {
			r.wrote = true
		}
	}

	if r.wrote {
		if out, err := r.checkpoint(ctx); out != nil || err != nil {
			return out, err
		}
	}
	return nil, nil
}

func (r *run) emitResult(ctx context.Context, c provider.ToolCall, res tools.Result) error {
	switch c.Name {
	case tools.ToolWriteFile, tools.ToolEditFile:
		if res.IsError {
			return nil
		}
		op := "write"
		if c.Name == tools.ToolEditFile {
			op = "edit"
		}
		return r.emit(ctx, EventFileChanged, map[string]any{"path": argString(c.Arguments, "path"), "op": op})
	case tools.ToolRunBuild:
		step := argString(c.Arguments, "step")
		if res.BuildFailed {
			return r.emit(ctx, EventBuildFailed, map[string]any{"step": step, "tail": tailRunes(res.Content, maxBuildTailChars)})
		}
		if !res.IsError {
			return r.emit(ctx, EventBuildSucceeded, map[string]any{"step": step})
		}
	}
	return nil
}

// toolFailure ends the turn for a Go error from a tool. The error text is
// never shown to the model or put in an event.
func (r *run) toolFailure(ctx, tctx context.Context, c provider.ToolCall, err error) (*outcome, error) {
	if out, cerr := r.ctxEnd(ctx, tctx); out != nil || cerr != nil {
		return out, cerr
	}
	r.log.Error("agent tool failed", "turn", r.turn.ID, "session", r.sess.ID, "tool", c.Name, "err", err)
	if errors.Is(err, sandbox.ErrWorkspaceLost) {
		// The workspace is gone and could not be restored: nothing to commit.
		return &outcome{status: store.TurnFailed, code: CodeWorkspaceLost}, nil
	}
	return &outcome{status: store.TurnFailed, code: CodeToolFailed, commit: true}, nil
}

func (r *run) pauseForProposal(ctx context.Context, c provider.ToolCall) (*outcome, error) {
	args := c.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	pending, err := json.Marshal(map[string]any{"tool_call_id": c.ID, "name": c.Name, "arguments": args})
	if err != nil {
		return nil, fmt.Errorf("marshal proposal: %w", err)
	}
	if err := r.emit(ctx, EventProposal, map[string]any{"tool_call_id": c.ID, "name": c.Name, "operation": args}); err != nil {
		return nil, err
	}
	return &outcome{status: store.TurnAwaitingConfirmation, proposal: pending}, nil
}

// checkpoint commits and snapshots the workspace after a batch that wrote
// files. A failure here fails the turn: durability is the point of it.
func (r *run) checkpoint(ctx context.Context) (*outcome, error) {
	if _, err := r.commitAndSnapshot(ctx); err != nil {
		if errors.Is(err, store.ErrLeaseLost) || ctx.Err() != nil {
			return nil, err
		}
		r.log.Error("agent checkpoint failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
		if errors.Is(err, sandbox.ErrWorkspaceLost) {
			return &outcome{status: store.TurnFailed, code: CodeWorkspaceLost}, nil
		}
		return &outcome{status: store.TurnFailed, code: CodeCommitFailed}, nil
	}
	r.wrote = false
	return nil, nil
}

// commitAndSnapshot commits the working tree as this turn's commit and stores
// the incremental bundle. It returns the SHA of the workspace head.
func (r *run) commitAndSnapshot(ctx context.Context) (string, error) {
	res, err := r.deps.Builder.Commit(ctx, r.sess.ID, r.turn.Seq, commitMessagePrefix+r.turn.ID)
	if err != nil {
		return "", err
	}
	if !res.Changed {
		return res.SHA, nil
	}
	if err := r.loadSnapshotState(ctx); err != nil {
		return "", err
	}
	if r.lastSHA != "" && res.Prev != r.lastSHA {
		// A commit made by a crashed attempt never got its snapshot. The new
		// bundle needs it as a prerequisite, so a restore would fail here; the
		// workspace itself is fine, so keep going and make it loud.
		r.log.Error("agent snapshot chain gap", "turn", r.turn.ID, "session", r.sess.ID, "prev", res.Prev, "stored_head", r.lastSHA)
	}
	parts := append(append([]snapPart(nil), r.snapParts...), snapPart{SHA: res.SHA, Data: res.Bundle})
	if err := r.deps.Store.SaveSnapshot(ctx, r.lease, res.SHA, encodeSnapshotParts(parts)); err != nil {
		return "", fmt.Errorf("save snapshot: %w", err)
	}
	r.snapParts, r.lastSHA = parts, res.SHA
	return res.SHA, nil
}

func (r *run) loadSnapshotState(ctx context.Context) error {
	if r.snapLoaded {
		return nil
	}
	snaps, err := r.deps.Store.ListSnapshots(ctx, r.sess.ID)
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}
	for _, sn := range snaps {
		parts, err := decodeSnapshotParts(sn)
		if err != nil || len(parts) == 0 {
			continue
		}
		r.lastSHA = parts[len(parts)-1].SHA
		if sn.TurnSeq == r.turn.Seq {
			r.snapParts = parts
		}
	}
	r.snapLoaded = true
	return nil
}

// finish persists the outcome: final commit (when asked), the closing events
// and the turn state.
func (r *run) finish(ctx context.Context, out *outcome) error {
	sha := ""
	if out.commit {
		head, err := r.commitAndSnapshot(ctx)
		switch {
		case err == nil:
			if head != r.sess.HeadSHA {
				sha = head
			}
		case errors.Is(err, store.ErrLeaseLost) || ctx.Err() != nil:
			return err
		default:
			r.log.Error("agent final commit failed", "turn", r.turn.ID, "session", r.sess.ID, "err", err)
			if out.status != store.TurnFailed {
				out = &outcome{status: store.TurnFailed, code: CodeCommitFailed}
				if errors.Is(err, sandbox.ErrWorkspaceLost) {
					out.code = CodeWorkspaceLost
				}
			}
		}
	}

	switch out.status {
	case store.TurnLimitReached:
		if err := r.emit(ctx, EventLimitReached, map[string]any{"limit": out.limit}); err != nil {
			return err
		}
	case store.TurnBudgetExceeded:
		if err := r.emit(ctx, EventBudgetExceeded, nil); err != nil {
			return err
		}
	case store.TurnFailed:
		if err := r.emit(ctx, EventError, map[string]any{"code": out.code}); err != nil {
			return err
		}
	}
	if out.status != store.TurnAwaitingConfirmation {
		if err := r.emit(ctx, EventTurnFinished, map[string]any{"status": out.status, "sha": sha}); err != nil {
			return err
		}
	}
	if err := r.deps.Store.FinishTurn(ctx, r.lease, store.FinishParams{
		Status: out.status, CommitSHA: sha, ErrorCode: out.code, PendingProposal: out.proposal,
	}); err != nil {
		return fmt.Errorf("finish turn: %w", err)
	}
	return nil
}

func (r *run) emit(ctx context.Context, typ string, payload any) error {
	if _, err := r.deps.Store.AppendEvent(ctx, r.lease, typ, payload); err != nil {
		return fmt.Errorf("append %s event: %w", typ, err)
	}
	return nil
}

// ---- small helpers ----

// argString reads a top-level string field of tool arguments; anything that is
// not a string (or invalid JSON) yields "".
func argString(args json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m[key], &s) != nil {
		return ""
	}
	return capRunes(s, 256)
}

// toolSummary is the event-safe description of a call: the path or build step
// only, never file contents.
func toolSummary(args json.RawMessage) string {
	if p := argString(args, "path"); p != "" {
		return p
	}
	return argString(args, "step")
}

func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func tailRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return "…" + string(rs[len(rs)-n:])
}

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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/skills"
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
	LatestSnapshotSHA(ctx context.Context, sessionID string) (string, error)
	FinishTurn(ctx context.Context, l store.Lease, p store.FinishParams) error
	RecordUsage(ctx context.Context, r store.UsageRecord) error
	AppendBudgetWarning(ctx context.Context, l store.Lease, w store.BudgetWarning) (bool, error)
}

var (
	_ Store          = (*store.Store)(nil)
	_ WorkerStore    = (*store.Store)(nil)
	_ BudgetChecker  = (*store.BudgetChecker)(nil)
	_ SnapshotLister = (*store.Store)(nil)
)

// BudgetChecker decides whether another model call is allowed. Errors that
// wrap store.ErrBudgetExceeded end the turn with budget_exceeded. When the
// call is allowed it also returns the caps that are close to being reached.
// *store.BudgetChecker satisfies it.
type BudgetChecker interface {
	Check(ctx context.Context, userID, sessionID string) ([]store.BudgetWarning, error)
}

// PromptInput is the variable part of the system prompt of a session.
type PromptInput struct {
	// Company is the company persona + rules layer (see [CompanyLayer]).
	Company string
	// TemplateGuide is the template's ORBIT_AGENT.md, if any.
	TemplateGuide string
	// SkillsIndex is the rendered skills index ([skills.RenderIndex]). Empty
	// means no skills are enabled: the prompt has no skills section and the
	// model is not offered load_skill.
	SkillsIndex string
	// Session is the session context (backend app, schema summary, file tree,
	// locale). Its Directive is set by the loop, which knows the turn.
	Session SessionContext
	// Brief is the session's current brief; nil when it has none.
	Brief *BriefInput
}

// PromptSource builds the [PromptInput] of a session. It runs once per
// RunTurn. When Deps.Prompt is nil the prompt gets only the backend app id and
// the session locale.
type PromptSource func(ctx context.Context, s *store.Session) (PromptInput, error)

// Deps are the collaborators of RunTurn.
type Deps struct {
	Store    Store
	Provider provider.Provider
	// PlanningProvider answers the calls of a plan-mode turn. Nil means
	// Provider answers them. Compaction always uses Provider.
	PlanningProvider provider.Provider
	Tools            *tools.Registry
	// Builder should be wrapped with sandbox.NewRestoring so a lost builder
	// volume is restored from snapshots (see [SnapshotProvider]).
	Builder sandbox.Sandbox
	Budget  BudgetChecker
	Limits  Limits
	Prompt  PromptSource
	// PlanningEnabled offers the planning tools (ask_user, propose_build_plan)
	// in build turns too. False means planning is turned off for the
	// installation; a turn that is already in plan mode still gets them.
	PlanningEnabled bool
	// PreviewBase returns the URL path a session's preview is served under
	// (vite --base); the revert turn's rebuild passes it like run_build does.
	// Default tools.PreviewBuildBase: pass the same function the workspace
	// tools were registered with.
	PreviewBase func(sessionID string) string
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
	budget string // store.BudgetReason (budget_exceeded)
	// commit asks for the final Builder.Commit + snapshot.
	commit   bool
	proposal json.RawMessage // awaiting_confirmation
	// sha is the turn's commit when it was already made (revert); with commit
	// set, finish computes it instead.
	sha string
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
	// skillsVisible: the skills index is in the prompt, so load_skill is
	// offered to the model. Fixed per turn (the index is resolved once).
	skillsVisible bool
	// planMode: the turn only plans (see planning_loop.go). It is read from
	// the turn row on every invocation, so the invocation that follows a plan
	// approval runs in build mode.
	planMode bool
	msgs     []store.Message
	state    turnState

	// wrote: workspace files were written since the last checkpoint.
	wrote bool
	// warned: caps this run already tried to warn about, so the store is
	// asked once per cap and turn (it keeps the once-per-session rule).
	warned map[string]bool
	// closingNotice is set while the closing call after a count limit is being
	// made (see closeAtLimit): tools disabled, this notice added as the last
	// message. Empty for every other call.
	closingNotice string
	// truncatedCalls holds the ids of tool calls whose arguments the model cut
	// off by hitting the output limit. runCalls answers them without
	// dispatching, with a message that says what happened.
	truncatedCalls map[string]bool

	// snapshot chain bookkeeping, loaded lazily on the first commit.
	snapLoaded bool
	lastSHA    string // newest commit known to the stored snapshots
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
	r.planMode = turn.Mode == store.ModePlan
	// The duration limit spans every attempt of the run.
	if turn.RunStartedAt != nil && turn.RunStartedAt.Before(r.start) {
		r.start = *turn.RunStartedAt
	}
	sess, err := deps.Store.GetSessionByID(ctx, turn.SessionID)
	if err != nil {
		return fmt.Errorf("agent: load session: %w", err)
	}
	r.sess = sess

	// Every earlier attempt of this run stopped without finishing the turn:
	// another try would most likely fail the same way, after paying for the
	// same model calls again. 0 is a claim by a replica from before attempts
	// were counted.
	if turn.Attempts > r.lim.MaxTurnAttempts {
		r.log.Error("agent turn failed: too many attempts", "turn", turn.ID, "session", turn.SessionID,
			"attempts", turn.Attempts, "max", r.lim.MaxTurnAttempts)
		return r.finish(ctx, &outcome{status: store.TurnFailed, code: CodeInternal})
	}

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
	switch r.turn.Kind {
	case store.KindChat:
	case store.KindRevert:
		return r.runRevert(ctx)
	default:
		return &outcome{status: store.TurnFailed, code: CodeUnsupportedTurn}, nil
	}

	msgs, err := r.deps.Store.ListMessages(ctx, r.sess.ID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	r.msgs = msgs
	r.state = analyzeTurn(msgs, r.turn.ID)
	r.wrote = r.state.started // work from before a crash may be uncommitted

	tctx, cancel := context.WithTimeout(ctx, r.lim.MaxTurnDuration-r.now().Sub(r.start))
	defer cancel()

	if err := r.emit(ctx, EventTurnStarted, map[string]any{"seq": r.turn.Seq, "resumed": r.state.started}); err != nil {
		return nil, err
	}
	if r.state.lastAssistantNoTools {
		return &outcome{status: store.TurnCompleted, commit: true}, nil
	}

	pin := PromptInput{Session: SessionContext{BackendAppID: r.sess.BackendAppID, NoBackend: r.noBackend(), Locale: r.sess.Locale}}
	if r.deps.Prompt != nil {
		if pin, err = r.deps.Prompt(ctx, r.sess); err != nil {
			r.log.Error("agent prompt source failed", "session", r.sess.ID, "err", err)
			return &outcome{status: store.TurnFailed, code: CodeInternal}, nil
		}
	}
	brief := ""
	if pin.Brief != nil && pin.Brief.Plan != nil {
		brief = planning.RenderBrief(pin.Brief.Plan)
	} else {
		pin.Brief = nil
	}
	pin.Session.Directive = r.directive(pin.Brief)
	r.system = BuildSystemPromptWithBrief(CorePrompt(), pin.Company, pin.TemplateGuide, pin.SkillsIndex, brief, pin.Session)
	r.skillsVisible = strings.TrimSpace(pin.SkillsIndex) != ""

	if err := r.syncBriefFile(ctx, tctx, pin.Brief); err != nil {
		return nil, err
	}

	// Tool calls persisted by a previous attempt but not answered yet.
	if len(r.state.pending) > 0 {
		if out, err := r.runCalls(ctx, tctx, r.state.pending); out != nil || err != nil {
			return out, err
		}
	}

	for {
		if out, err := r.checkLimits(ctx, tctx); out != nil || err != nil {
			if err == nil && (out.limit == LimitSteps || out.limit == LimitFixes) {
				return r.closeAtLimit(ctx, tctx, out)
			}
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
		if resp.FinishReason == provider.FinishLength {
			r.markTruncated(calls)
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
	if r.state.modeSteps >= r.lim.MaxSteps {
		return &outcome{status: store.TurnLimitReached, limit: LimitSteps, commit: true}, nil
	}
	if r.state.failedBuilds >= r.lim.MaxFixIterations {
		return &outcome{status: store.TurnLimitReached, limit: LimitFixes, commit: true}, nil
	}
	return nil, nil
}

// checkBudget runs before every model call. When a cap is hit no call is
// made. A cap that is close (store.BudgetWarning) gets one budget_warning
// event per session.
func (r *run) checkBudget(ctx, tctx context.Context) (*outcome, error) {
	warnings, err := r.deps.Budget.Check(tctx, r.sess.OwnerUserID, r.sess.ID)
	if err == nil {
		for _, w := range warnings {
			if r.warned[w.Cap] {
				continue
			}
			if r.warned == nil {
				r.warned = map[string]bool{}
			}
			r.warned[w.Cap] = true
			if _, err := r.deps.Store.AppendBudgetWarning(ctx, r.lease, w); err != nil {
				return nil, fmt.Errorf("append %s event: %w", EventBudgetWarning, err)
			}
		}
		return nil, nil
	}
	if errors.Is(err, store.ErrBudgetExceeded) {
		return &outcome{status: store.TurnBudgetExceeded, budget: store.BudgetReason(err), commit: true}, nil
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

// modelProvider is the provider that answers this turn's chat calls: the
// planning provider in a plan-mode turn when one is set, else the turn's
// Provider. The usage record of a call names the provider it used.
func (r *run) modelProvider() provider.Provider {
	if r.planMode && r.deps.PlanningProvider != nil {
		return r.deps.PlanningProvider
	}
	return r.deps.Provider
}

// chat performs one model call (compacting the history first when needed),
// records its usage and returns the response. A context-too-long answer is
// retried once with a forced compaction.
func (r *run) chat(ctx, tctx context.Context) (provider.ChatResponse, *outcome, error) {
	var (
		resp provider.ChatResponse
		req  provider.ChatRequest
	)
	force := false
	p := r.modelProvider()
	for {
		prompt, err := r.promptMessages(ctx, tctx, force)
		if err != nil {
			return resp, nil, err
		}
		req = provider.ChatRequest{
			System:          r.system,
			Messages:        prompt,
			Tools:           r.toolSpecs(),
			MaxOutputTokens: r.lim.MaxOutputTokens,
			// The system prompt is built once per turn and the history only
			// grows, so the next step starts with this prefix.
			CacheHint: true,
		}
		if r.closingNotice != "" {
			// No step follows, so a cache write would never be read back. The
			// tools stay in the request: the history holds tool calls, and
			// ToolChoice none is what stops the model from making new ones.
			req.Messages = append(prompt[:len(prompt):len(prompt)], provider.Message{Role: provider.RoleUser, Content: r.closingNotice})
			req.ToolChoice = provider.ToolChoiceNone
			req.CacheHint = false
		}
		resp, err = p.Chat(tctx, req)
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
			"provider", p.Name(), "model", p.Model(), "err", err, "cause", errors.Unwrap(err))
		return resp, &outcome{status: store.TurnFailed, code: CodeProviderUnavailable, commit: true}, nil
	}

	// Usage first: the tokens are spent whether or not the rest persists.
	u := recordedUsage(req, resp)
	if err := r.deps.Store.RecordUsage(ctx, store.UsageRecord{
		UserID: r.sess.OwnerUserID, Feature: store.FeatureBuilder, SessionID: r.sess.ID, TurnID: r.turn.ID,
		Provider: p.Name(), Model: p.Model(),
		Purpose:     r.usagePurpose(),
		InputTokens: u.InputTokens, CachedInputTokens: u.CachedInputTokens,
		CacheWriteTokens: u.CacheWriteTokens, OutputTokens: u.OutputTokens,
		Reported: u.Reported,
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
	// Tool call ids pair each call with its result and scope the builder's
	// idempotency key, so they must be unique within the turn. Providers do
	// not guarantee it: some compatible servers omit ids (the adapter then
	// numbers them per response) or repeat them across responses. An empty or
	// repeated id is replaced; the new id is persisted with the message, so
	// it is stable across a resume.
	used := r.turnCallIDs()
	for i := range calls {
		if calls[i].ID == "" || used[calls[i].ID] {
			id := fmt.Sprintf("call_%d_%d", r.state.steps+1, i)
			for n := 1; used[id]; n++ {
				id = fmt.Sprintf("call_%d_%d_%d", r.state.steps+1, i, n)
			}
			calls[i].ID = id
		}
		used[calls[i].ID] = true
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
	r.state.modeSteps++
	if strings.TrimSpace(resp.Message.Content) != "" {
		if err := r.emit(ctx, EventAssistantMessage, map[string]any{"content": capRunes(resp.Message.Content, maxAssistantEventChars)}); err != nil {
			return nil, err
		}
	}
	return calls, nil
}

// turnCallIDs returns the tool call ids already issued in this turn.
func (r *run) turnCallIDs() map[string]bool {
	used := map[string]bool{}
	for i := range r.msgs {
		m := &r.msgs[i]
		if m.TurnID != r.turn.ID || m.Role != store.RoleAssistant {
			continue
		}
		for _, c := range parseAssistantCalls(*m) {
			used[c.ID] = true
		}
	}
	return used
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
		res, gated := r.gateTruncated(c)
		if !gated {
			res, gated = r.gatePlanning(c)
		}
		if !gated {
			res, gated = r.gateSkillCall(c)
		}
		if !gated && c.Name == tools.ToolRunBuild {
			if err := r.emit(ctx, EventBuildStarted, map[string]any{"step": argString(c.Arguments, "step")}); err != nil {
				return nil, err
			}
		}
		if !gated {
			var err error
			res, err = r.deps.Tools.Dispatch(tctx, tools.Call{ID: c.ID, Name: c.Name, Arguments: c.Arguments, SessionID: r.sess.ID, TurnID: r.turn.ID})
			if err != nil {
				return r.toolFailure(ctx, tctx, c, err)
			}
		}

		if kind, ok := r.deps.Tools.Kind(c.Name); ok && !res.IsError &&
			(kind == tools.KindProposal || kind == tools.KindExternalMutating || kind == tools.KindPlanning) {
			return r.pauseForProposal(ctx, c)
		}

		loaded := ""
		if !res.IsError {
			loaded = res.Meta[skills.MetaSkill]
		}
		meta, _ := json.Marshal(toolMeta{BuildFailed: res.BuildFailed, IsError: res.IsError, Skill: loaded})
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
		if loaded != "" {
			if r.state.skills == nil {
				r.state.skills = map[string]bool{}
			}
			r.state.skills[loaded] = true
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
	case skills.ToolLoadSkill:
		if res.IsError || res.Meta[skills.MetaSkill] == "" {
			return nil
		}
		version, _ := strconv.Atoi(res.Meta[skills.MetaVersion])
		return r.emit(ctx, EventSkillLoaded, map[string]any{
			"name": res.Meta[skills.MetaSkill], "source": res.Meta[skills.MetaSource], "version": version,
		})
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

// gateSkillCall answers a load_skill call without dispatching it when the
// model may not make it: the tool is not offered this turn (no skills), or
// the turn already loaded MaxSkillLoadsPerTurn distinct skills. The count
// comes from persisted tool messages, so it survives a crash and resume.
func (r *run) gateSkillCall(c provider.ToolCall) (tools.Result, bool) {
	if c.Name != skills.ToolLoadSkill {
		return tools.Result{}, false
	}
	if !r.skillsVisible {
		return tools.Result{Content: fmt.Sprintf("unknown tool %q", skills.ToolLoadSkill), IsError: true}, true
	}
	name := strings.TrimSpace(argString(c.Arguments, "name"))
	if name != "" && !r.state.skills[name] && len(r.state.skills) >= MaxSkillLoadsPerTurn {
		return tools.Result{
			Content: fmt.Sprintf("skill load limit reached: at most %d different skills per turn. Continue with the skills you already loaded.", MaxSkillLoadsPerTurn),
			IsError: true,
		}, true
	}
	return tools.Result{}, false
}

// Closing notices are appended (never persisted) to the closing call of a turn
// that a count limit stopped. Orbit's core rules still apply; they only ask for
// a handover.
const (
	stepLimitNotice = "System notice: this is your last step in this turn and tools are now disabled. " +
		"Do not call any tool. Reply to the user with a short summary of what is done, " +
		"what is still missing, and what they should ask next to continue."
	// The fix limit stops the turn on a failing build, so the notice also keeps
	// the model from promising a fix it can no longer make in this turn.
	fixLimitNotice = "System notice: the limit of failed builds for this turn was reached and tools are now disabled. " +
		"Do not call any tool, and do not say you will fix or retry anything in this reply. " +
		"Tell the user briefly what you were building, which build error persists (quote its key line), " +
		"what you already tried, and what they should check or ask next."
)

// closeAtLimit makes one extra model call, outside the MaxSteps and
// MaxFixIterations allowance and with tools disabled, so a turn stopped by one
// of those limits leaves the user a handover instead of ending mid-work. A turn
// that finishes within its limits never gets here, so its outcome does not
// change.
//
// stop is the limit_reached outcome and always stands: when the budget is
// spent, the provider fails or the turn's time is up there is simply no
// handover, and the turn is not failed for it. Tool calls in the reply (a
// server that ignores tool_choice none) are dropped rather than persisted:
// they would have no result.
func (r *run) closeAtLimit(ctx, tctx context.Context, stop *outcome) (*outcome, error) {
	if out, err := r.checkBudget(ctx, tctx); out != nil || err != nil {
		if err != nil {
			return nil, err
		}
		return stop, nil
	}
	r.closingNotice = stepLimitNotice
	if stop.limit == LimitFixes {
		r.closingNotice = fixLimitNotice
	}
	resp, out, err := r.chat(ctx, tctx)
	r.closingNotice = ""
	if err != nil {
		return nil, err
	}
	if out != nil {
		return stop, nil
	}
	resp.Message.ToolCalls, resp.ToolCalls = nil, nil
	if strings.TrimSpace(resp.Message.Content) != "" {
		if _, err := r.persistAssistant(ctx, resp); err != nil {
			return nil, err
		}
	}
	return stop, nil
}

// markTruncated records which calls of a reply cut by the output limit
// (finish reason length) did not arrive whole. The last call is always one of
// them: the cut falls inside the block being written, and a provider may still
// hand back a partial object that happens to parse. An earlier call is one only
// when its arguments are not a JSON object.
func (r *run) markTruncated(calls []provider.ToolCall) {
	for i, c := range calls {
		var obj map[string]json.RawMessage
		if i == len(calls)-1 || json.Unmarshal(c.Arguments, &obj) != nil {
			if r.truncatedCalls == nil {
				r.truncatedCalls = map[string]bool{}
			}
			r.truncatedCalls[c.ID] = true
		}
	}
}

// gateTruncated answers a call that was cut off by the output limit without
// dispatching it. The generic "invalid arguments" error does not tell the
// model why the call failed, so it tends to send the same oversized call again
// and burn steps; this one says what to do instead.
func (r *run) gateTruncated(c provider.ToolCall) (tools.Result, bool) {
	if !r.truncatedCalls[c.ID] {
		return tools.Result{}, false
	}
	return tools.Result{
		Content: fmt.Sprintf("Your reply reached the output length limit, so the arguments of this call were cut off and nothing was applied. "+
			"Send it again with less content: for a large file, %s a short first version and extend it with %s in several smaller steps.",
			tools.ToolWriteFile, tools.ToolEditFile),
		IsError: true,
	}, true
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
	if err := r.snapshot(ctx, res); err != nil {
		return "", err
	}
	return res.SHA, nil
}

// snapshot stores the incremental bundle of a commit the builder just made
// (a no-op when nothing changed).
func (r *run) snapshot(ctx context.Context, res sandbox.CommitResult) error {
	if !res.Changed {
		return nil
	}
	if err := r.loadSnapshotState(ctx); err != nil {
		return err
	}
	if r.lastSHA != "" && res.Prev != r.lastSHA {
		// A commit made by a crashed attempt never got its snapshot. The new
		// bundle needs it as a prerequisite, so a restore would fail here; the
		// workspace itself is fine, so keep going and make it loud.
		r.log.Error("agent snapshot chain gap", "turn", r.turn.ID, "session", r.sess.ID, "prev", res.Prev, "stored_head", r.lastSHA)
	}
	if err := r.deps.Store.SaveSnapshot(ctx, r.lease, res.SHA, res.Bundle); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	r.lastSHA = res.SHA
	return nil
}

func (r *run) loadSnapshotState(ctx context.Context) error {
	if r.snapLoaded {
		return nil
	}
	sha, err := r.deps.Store.LatestSnapshotSHA(ctx, r.sess.ID)
	if err != nil {
		return fmt.Errorf("latest snapshot: %w", err)
	}
	r.lastSHA = sha
	r.snapLoaded = true
	return nil
}

// finish persists the outcome: final commit (when asked), the closing events
// and the turn state.
func (r *run) finish(ctx context.Context, out *outcome) error {
	sha := out.sha
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
		var payload any
		if out.budget != "" {
			payload = map[string]any{"reason": out.budget}
		}
		if err := r.emit(ctx, EventBudgetExceeded, payload); err != nil {
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

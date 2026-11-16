package agent

// planning_eval_run_test.go: the driver of the planning eval
// (.specs/features/ai-builder-economy design §6). It runs the real plan-mode
// turn of the loop against the in-memory fakes of this package, so a model is
// compared on the same briefs and the same scripted answers. Nothing here
// needs a key: the opt-in file planning_eval_test.go (build tag planningeval)
// wires real providers, and a normal test drives this code with a fake one.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/catalog"
	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

// planEvalBrief is one request of testdata/planeval/briefs.json. Schema, when
// set, is what the app-schema tool answers (a backend that already has tables).
type planEvalBrief struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Locale  string `json:"locale"`
	Message string `json:"message"`
	Schema  string `json:"schema"`
}

func loadPlanEvalBriefs(path string) ([]planEvalBrief, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var briefs []planEvalBrief
	if err := json.Unmarshal(raw, &briefs); err != nil {
		return nil, fmt.Errorf("planeval briefs: %w", err)
	}
	seen := map[string]bool{}
	for _, b := range briefs {
		if b.ID == "" || b.Message == "" || seen[b.ID] {
			return nil, fmt.Errorf("planeval briefs: brief %q needs a unique id and a message", b.ID)
		}
		seen[b.ID] = true
	}
	return briefs, nil
}

// Outcomes of one brief.
const (
	planEvalQuestion = "question" // the last pause was a question the script could not move past
	planEvalPlan     = "plan"     // the turn ended with propose_build_plan
	planEvalText     = "text"     // the model answered in plain text: the failure the directive tries to prevent
	planEvalError    = "error"    // the turn failed or the round bound was hit
)

// planEvalResult is what one brief produced on one model.
type planEvalResult struct {
	Provider, Model, Brief, Kind string
	Outcome                      string
	Rounds                       int // ask_user rounds the script answered
	Milestones                   int
	Calls, Tokens                int
	CostMicros                   *int64 // nil when a call has no catalog price
	PlanOK                       bool   // propose_build_plan arguments pass planning.ParsePlan
	Note                         string
	// PlanText is the plan as the user would read it, with no model name.
	PlanText string
}

// planEvalConfig bounds a run.
type planEvalConfig struct {
	// MaxCalls is the model calls allowed to one brief.
	MaxCalls int
}

// runPlanEvalBrief runs one plan-mode turn of p on b: it answers every ask_user
// round with the first option of each question (so models see the same answers)
// and stops at propose_build_plan, which is not approved.
func runPlanEvalBrief(t *testing.T, p provider.Provider, b planEvalBrief, cfg planEvalConfig) (res planEvalResult) {
	t.Helper()
	h := newPlanHarness(t, store.ModePlan)
	h.deps.Provider = p
	h.deps.Limits.MaxSteps = cfg.MaxCalls
	h.st.sess.Locale = b.Locale
	h.st.msgs = nil
	h.st.seed("turn-1", store.RoleUser, b.Message)

	schema := b.Schema
	if schema == "" {
		schema = "App schema: no tables yet."
	}
	if err := h.reg.Register(tools.Tool{
		Name: "orbit_get_app_schema", Description: "Read the tables of the backend app.", Kind: tools.KindReadOnly,
		Schema:  json.RawMessage(`{"type":"object","properties":{}}`),
		Handler: func(context.Context, tools.Call) (tools.Result, error) { return tools.Result{Content: schema}, nil },
	}); err != nil {
		t.Fatalf("register schema tool: %v", err)
	}

	res = planEvalResult{Provider: p.Name(), Model: p.Model(), Brief: b.ID, Kind: b.Kind}
	defer func() { res.Calls, res.Tokens, res.CostMicros = planEvalUsage(h.st.usage) }()

	for {
		if err := h.run(context.Background()); err != nil {
			res.Outcome, res.Note = planEvalError, "RunTurn: "+err.Error()
			return res
		}
		fin := h.st.finish
		if fin == nil {
			res.Outcome, res.Note = planEvalError, "the turn did not finish"
			return res
		}
		switch fin.Status {
		case store.TurnCompleted:
			res.Outcome, res.Note = planEvalText, "the model ended the turn without ask_user or propose_build_plan"
			return res
		case store.TurnAwaitingConfirmation:
		default:
			res.Outcome, res.Note = planEvalError, fmt.Sprintf("status=%s code=%s", fin.Status, fin.ErrorCode)
			return res
		}

		pending, ok := store.ParsePendingProposal(fin.PendingProposal)
		if !ok {
			res.Outcome, res.Note = planEvalError, "the pause has no proposal"
			return res
		}
		switch pending.Name {
		case planning.ToolProposeBuildPlan:
			res.Outcome = planEvalPlan
			plan, err := planning.ParsePlan(pending.Arguments)
			if err != nil {
				res.Note = "plan does not parse: " + err.Error()
				res.PlanText = string(pending.Arguments)
				return res
			}
			res.PlanOK, res.Milestones = true, len(plan.Milestones)
			res.PlanText = planning.RenderBrief(plan)
			return res
		case planning.ToolAskUser:
			res.Rounds++
			content, err := planEvalAnswer(pending.Arguments)
			if err != nil {
				res.Outcome, res.Note = planEvalError, "questions: "+err.Error()
				return res
			}
			h.resolve(pending.ToolCallID, content, "", "")
		default:
			res.Outcome, res.Note = planEvalError, "unexpected pause on "+pending.Name
			return res
		}
	}
}

// planEvalAnswer is the scripted user: the first option of every question.
func planEvalAnswer(args json.RawMessage) (string, error) {
	questions, err := planning.ParseQuestions(args)
	if err != nil {
		return "", err
	}
	answers := make([]planning.Answer, 0, len(questions))
	for _, q := range questions {
		answers = append(answers, planning.Answer{ID: q.ID, Choices: []string{q.Options[0].Label}})
	}
	answers, err = planning.NormalizeAnswers(questions, answers)
	if err != nil {
		return "", err
	}
	return planning.AnswersResult(questions, answers), nil
}

// planEvalUsage totals the calls of a run. The cost is nil as soon as one call
// has no catalog price, the way a ledger row without a price reads.
func planEvalUsage(usage []store.UsageRecord) (calls, tokens int, cost *int64) {
	var sum int64
	priced := true
	for _, u := range usage {
		calls++
		tokens += u.InputTokens + u.CachedInputTokens + u.CacheWriteTokens + u.OutputTokens
		c := planEvalCallCost(u)
		if c == nil {
			priced = false
			continue
		}
		sum += *c
	}
	if calls == 0 || !priced {
		return calls, tokens, nil
	}
	return calls, tokens, &sum
}

// planEvalCallCost prices one call with the catalog, like the ledger does.
func planEvalCallCost(u store.UsageRecord) *int64 {
	m, ok := catalog.Lookup(u.Provider, u.Model)
	if !ok {
		return nil
	}
	v, ok := m.PriceAt(time.Now().UTC())
	if !ok {
		return nil
	}
	tier := v.TierFor(u.InputTokens + u.CachedInputTokens + u.CacheWriteTokens)
	var price store.Price
	var err error
	if price.Input, err = store.ParsePrice(tier.Input); err != nil {
		return nil
	}
	if price.Output, err = store.ParsePrice(tier.Output); err != nil {
		return nil
	}
	for _, opt := range []struct {
		value string
		dst   **int64
	}{{tier.CachedInput, &price.CachedInput}, {tier.CacheWrite, &price.CacheWrite}} {
		if opt.value == "" {
			continue
		}
		micros, err := store.ParsePrice(opt.value)
		if err != nil {
			return nil
		}
		*opt.dst = &micros
	}
	cost, err := store.CostMicros(price, u.InputTokens, u.CachedInputTokens, u.CacheWriteTokens, u.OutputTokens)
	if err != nil {
		return nil
	}
	return &cost
}

// planEvalLine is the machine-readable line of one run, ready to paste into
// design.md. The note is quoted and has no newlines.
func planEvalLine(r planEvalResult) string {
	cost := "unpriced"
	if r.CostMicros != nil {
		cost = fmt.Sprint(*r.CostMicros)
	}
	return fmt.Sprintf("PLANEVAL_RESULT provider=%s model=%s brief=%s kind=%s outcome=%s rounds=%d milestones=%d calls=%d tokens=%d cost_micros=%s plan_ok=%t note=%q",
		r.Provider, r.Model, r.Brief, r.Kind, r.Outcome, r.Rounds, r.Milestones, r.Calls, r.Tokens, cost, r.PlanOK,
		strings.Join(strings.Fields(r.Note), " "))
}

// planEvalTotals is the totals line of one model over its results.
func planEvalTotals(results []planEvalResult) string {
	if len(results) == 0 {
		return "PLANEVAL_TOTAL briefs=0"
	}
	var plans, planOK, questions, texts, errs, calls, tokens int
	var cost int64
	unpriced := false
	for _, r := range results {
		switch r.Outcome {
		case planEvalPlan:
			plans++
		case planEvalQuestion:
			questions++
		case planEvalText:
			texts++
		default:
			errs++
		}
		if r.PlanOK {
			planOK++
		}
		calls += r.Calls
		tokens += r.Tokens
		if r.CostMicros == nil {
			unpriced = true
		} else {
			cost += *r.CostMicros
		}
	}
	costText := fmt.Sprint(cost)
	if unpriced {
		costText += "+unpriced"
	}
	return fmt.Sprintf("PLANEVAL_TOTAL provider=%s model=%s briefs=%d plans=%d plan_ok=%d questions=%d text=%d errors=%d calls=%d tokens=%d cost_micros=%s",
		results[0].Provider, results[0].Model, len(results), plans, planOK, questions, texts, errs, calls, tokens, costText)
}

// planEvalKey says which run a blind file holds.
type planEvalKey struct {
	File                   string
	Provider, Model, Brief string
}

// writePlanEvalBlind writes the plan of every run that produced one to dir, as
// plan-NN.md, with no model name and in an order shuffled by seed, so the
// owner can read them without knowing the model (spec A4). It returns the key
// from file to run, which is printed separately.
func writePlanEvalBlind(dir string, results []planEvalResult, briefs map[string]planEvalBrief, seed int64) ([]planEvalKey, error) {
	var withPlan []planEvalResult
	for _, r := range results {
		if r.PlanText != "" {
			withPlan = append(withPlan, r)
		}
	}
	// A stable order first, so the same seed gives the same shuffle.
	sort.Slice(withPlan, func(i, j int) bool {
		a, b := withPlan[i], withPlan[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Brief < b.Brief
	})
	rand.New(rand.NewSource(seed)).Shuffle(len(withPlan), func(i, j int) { withPlan[i], withPlan[j] = withPlan[j], withPlan[i] })
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	keys := make([]planEvalKey, 0, len(withPlan))
	for i, r := range withPlan {
		name := fmt.Sprintf("plan-%02d.md", i+1)
		body := fmt.Sprintf("# Request\n\n%s\n\n# Plan\n\n%s\n", briefs[r.Brief].Message, r.PlanText)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return nil, err
		}
		keys = append(keys, planEvalKey{File: name, Provider: r.Provider, Model: r.Model, Brief: r.Brief})
	}
	return keys, nil
}

func planEvalKeyLine(k planEvalKey) string {
	return fmt.Sprintf("PLANEVAL_KEY file=%s provider=%s model=%s brief=%s", k.File, k.Provider, k.Model, k.Brief)
}

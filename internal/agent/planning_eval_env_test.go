package agent

// planning_eval_env_test.go: the environment grammar of the planning eval and
// the tests of its helpers. These run in the normal `go test`: they use a fake
// provider and spend nothing. The run against real models is
// planning_eval_test.go (build tag planningeval).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// planEvalEntry is one PLANEVAL_PROVIDERS entry, "<name>:<model>". The name is
// openai or anthropic (the catalog providers the planning model can be set
// for); the key comes from PLANEVAL_KEY_<NAME>.
type planEvalEntry struct {
	Name, Model string
}

func (e planEvalEntry) keyEnv() string { return "PLANEVAL_KEY_" + strings.ToUpper(e.Name) }

func parsePlanEvalEntries(spec string) ([]planEvalEntry, error) {
	var out []planEvalEntry
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, model, ok := strings.Cut(part, ":")
		name, model = strings.TrimSpace(name), strings.TrimSpace(model)
		if !ok || model == "" || (name != "openai" && name != "anthropic") {
			return nil, fmt.Errorf("PLANEVAL_PROVIDERS entry %q must be openai:<model> or anthropic:<model>", part)
		}
		out = append(out, planEvalEntry{Name: name, Model: model})
	}
	return out, nil
}

// planEvalProvider is a provider that plays script for the first brief turn it
// is given. A fresh one is made per brief.
func planEvalFake(script ...scriptStep) provider.Provider {
	return &fakeProvider{script: script}
}

func TestPlanEval_ParsesTheProviderList(t *testing.T) {
	got, err := parsePlanEvalEntries(" anthropic:claude-haiku-5-5 , openai:gpt-5.5,, ")
	if err != nil || len(got) != 2 || got[0] != (planEvalEntry{"anthropic", "claude-haiku-5-5"}) || got[1].keyEnv() != "PLANEVAL_KEY_OPENAI" {
		t.Fatalf("entries = %+v (%v)", got, err)
	}
	for _, bad := range []string{"gpt-5.5", "openai:", "gemini:x"} {
		if _, err := parsePlanEvalEntries(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestPlanEval_BriefSetHasTheDocumentedShape(t *testing.T) {
	briefs, err := loadPlanEvalBriefs("testdata/planeval/briefs.json")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	pt, en := 0, 0
	for _, b := range briefs {
		kinds[b.Kind]++
		if b.Locale == "en" {
			en++
		} else {
			pt++
		}
	}
	if len(briefs) != 12 || kinds["vague"] != 4 || kinds["complete"] != 4 || kinds["backend"] != 2 || kinds["english"] != 2 || en != 2 || pt != 10 {
		t.Fatalf("briefs = %d, kinds = %v, locales pt=%d en=%d", len(briefs), kinds, pt, en)
	}
	for _, b := range briefs {
		if b.Kind == "backend" && b.Schema == "" {
			t.Errorf("brief %s has an existing backend and no schema", b.ID)
		}
	}
}

// A dry run through the real loop: a question round, then a plan.
func TestPlanEval_DryRunPrintsOneLinePerBriefAndWritesBlindFiles(t *testing.T) {
	briefs, err := loadPlanEvalBriefs("testdata/planeval/briefs.json")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]planEvalBrief{}
	var results []planEvalResult
	for _, b := range briefs {
		byID[b.ID] = b
		p := planEvalFake(scriptStep{resp: toolResp(askCall("a1"))}, scriptStep{resp: toolResp(planCall("p1"))})
		results = append(results, runPlanEvalBrief(t, p, b, planEvalConfig{MaxCalls: 6}))
	}
	for _, r := range results {
		if r.Outcome != planEvalPlan || !r.PlanOK || r.Rounds != 1 || r.Milestones != 3 || r.Calls != 2 || r.Tokens != 30 {
			t.Fatalf("result = %+v", r)
		}
		if line := planEvalLine(r); strings.Contains(line, "\n") || !strings.Contains(line, "brief="+r.Brief) || !strings.Contains(line, "outcome=plan") {
			t.Errorf("line = %q", line)
		}
	}
	if total := planEvalTotals(results); !strings.Contains(total, "briefs=12 plans=12 plan_ok=12 questions=0 text=0 errors=0 calls=24 tokens=360") {
		t.Errorf("totals = %q", total)
	}

	dir := t.TempDir()
	keys, err := writePlanEvalBlind(dir, results, byID, 7)
	if err != nil || len(keys) != 12 {
		t.Fatalf("blind files: %d keys, %v", len(keys), err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "plan-*.md"))
	if len(files) != 12 {
		t.Fatalf("%d files written, want 12", len(files))
	}
	for _, f := range files {
		body, _ := os.ReadFile(f)
		if strings.Contains(string(body), "fake") {
			t.Errorf("%s names the model", f)
		}
	}
	// The same seed shuffles the same way; another seed does not have to.
	again, _ := writePlanEvalBlind(t.TempDir(), results, byID, 7)
	for i := range keys {
		if keys[i] != again[i] {
			t.Fatalf("key %d differs with the same seed: %+v vs %+v", i, keys[i], again[i])
		}
	}
	if line := planEvalKeyLine(keys[0]); !strings.HasPrefix(line, "PLANEVAL_KEY file=plan-01.md provider=fake model=fake-1 brief=") {
		t.Errorf("key line = %q", line)
	}
}

func TestPlanEval_ClassifiesWhatTheModelDid(t *testing.T) {
	brief := planEvalBrief{ID: "x", Kind: "vague", Locale: "en", Message: "an app"}
	cfg := planEvalConfig{MaxCalls: 6}

	text := runPlanEvalBrief(t, planEvalFake(scriptStep{resp: textResp("what do you want?")}), brief, cfg)
	if text.Outcome != planEvalText || text.PlanOK {
		t.Errorf("plain text: %+v", text)
	}

	failed := runPlanEvalBrief(t, planEvalFake(scriptStep{err: errProviderSecret()}), brief, cfg)
	if failed.Outcome != planEvalError || strings.Contains(failed.Note, "secret") {
		t.Errorf("provider failure: %+v (the note must not carry provider text)", failed)
	}

	// A plan that does not parse never pauses the turn: the loop answers the
	// model with the error and the model tries again (here: and then succeeds).
	bad := provider.ToolCall{ID: "p0", Name: planning.ToolProposeBuildPlan, Arguments: []byte(`{"summary":"x","milestones":[]}`)}
	retried := runPlanEvalBrief(t, planEvalFake(scriptStep{resp: toolResp(bad)}, scriptStep{resp: toolResp(planCall("p1"))}), brief, cfg)
	if retried.Outcome != planEvalPlan || !retried.PlanOK || retried.Calls != 2 {
		t.Errorf("a plan that is fixed after an error: %+v", retried)
	}
	giveUp := runPlanEvalBrief(t, planEvalFake(scriptStep{resp: toolResp(bad)}, scriptStep{resp: textResp("sorry")}), brief, cfg)
	if giveUp.Outcome != planEvalText || giveUp.PlanOK {
		t.Errorf("a model that gives up after the error: %+v", giveUp)
	}
}

// A run that stops with a priced model reports a cost; an unknown one does not.
func TestPlanEval_CostFollowsTheCatalog(t *testing.T) {
	usage := []store.UsageRecord{
		{Provider: "anthropic", Model: "claude-haiku-5-5", InputTokens: 1000, OutputTokens: 100},
		{Provider: "anthropic", Model: "claude-haiku-5-5", InputTokens: 2000, OutputTokens: 200},
	}
	calls, tokens, cost := planEvalUsage(usage)
	if calls != 2 || tokens != 3300 || cost == nil || *cost <= 0 {
		t.Fatalf("usage = %d calls, %d tokens, cost %v", calls, tokens, cost)
	}
	usage[1].Model = "not-in-catalog"
	if _, _, cost := planEvalUsage(usage); cost != nil {
		t.Errorf("a call without a price must make the total unpriced, got %d", *cost)
	}
}

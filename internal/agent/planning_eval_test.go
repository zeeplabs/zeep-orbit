//go:build planningeval

package agent

// planning_eval_test.go: the opt-in run of the planning eval against real
// models (.specs/features/ai-builder-economy design §6, spec US-2). It spends
// real tokens, so it is excluded from normal builds by the planningeval tag.
//
// Env:
//
//	PLANEVAL_PROVIDERS        comma-separated <name>:<model>, name openai or anthropic.
//	                          Empty skips the test.
//	PLANEVAL_KEY_OPENAI, PLANEVAL_KEY_ANTHROPIC
//	                          API keys. An entry whose key is unset is skipped.
//	                          Keys are never printed.
//	PLANEVAL_OUT              directory for the anonymized plans (outside the repo).
//	                          Required, so a blind read is always possible.
//	PLANEVAL_MAX_CALLS        model calls allowed to one brief (default 12).
//	PLANEVAL_MAX_COST_MICROS  stop when the priced total reaches this (default
//	                          2000000 = US$ 2), checked between briefs.
//
// Example:
//
//	PLANEVAL_PROVIDERS='anthropic:claude-sonnet-5-5,anthropic:claude-haiku-5-5' \
//	PLANEVAL_KEY_ANTHROPIC=... PLANEVAL_OUT="$HOME/planeval-out" \
//	go test -tags planningeval -run TestPlanningEval -v ./internal/agent/
//
// Output: PLANEVAL_RESULT per model and brief, PLANEVAL_TOTAL per model, and
// PLANEVAL_KEY lines (file -> run) that you should not read before the blind
// read of the files.

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
)

func TestPlanningEval(t *testing.T) {
	spec := os.Getenv("PLANEVAL_PROVIDERS")
	if strings.TrimSpace(spec) == "" {
		t.Skip("PLANEVAL_PROVIDERS is empty; nothing to run against real models")
	}
	entries, err := parsePlanEvalEntries(spec)
	if err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("PLANEVAL_OUT")
	if out == "" {
		t.Fatal("PLANEVAL_OUT is required: the plans are written there for the blind read")
	}
	maxCalls := envInt(t, "PLANEVAL_MAX_CALLS", 12)
	maxCost := int64(envInt(t, "PLANEVAL_MAX_COST_MICROS", 2_000_000))

	briefs, err := loadPlanEvalBriefs("testdata/planeval/briefs.json")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]planEvalBrief{}
	for _, b := range briefs {
		byID[b.ID] = b
	}

	var all []planEvalResult
	var spent int64
	for _, e := range entries {
		key, ok := os.LookupEnv(e.keyEnv())
		if !ok {
			t.Logf("PLANEVAL_RESULT provider=%s model=%s brief=* outcome=skipped note=%q", e.Name, e.Model, e.keyEnv()+" is not set")
			continue
		}
		var results []planEvalResult
		for _, b := range briefs {
			if spent >= maxCost {
				t.Logf("PLANEVAL_STOP cost cap of %d micros reached before brief %s of %s", maxCost, b.ID, e.Model)
				break
			}
			p, err := buildPlanEvalProvider(e, key)
			if err != nil {
				t.Fatalf("build provider %s/%s: %v", e.Name, e.Model, err)
			}
			r := runPlanEvalBrief(t, p, b, planEvalConfig{MaxCalls: maxCalls})
			if r.CostMicros != nil {
				spent += *r.CostMicros
			}
			t.Log(planEvalLine(r))
			results = append(results, r)
		}
		t.Log(planEvalTotals(results))
		all = append(all, results...)
	}

	keys, err := writePlanEvalBlind(out, all, byID, 1)
	if err != nil {
		t.Fatalf("write plans: %v", err)
	}
	t.Logf("PLANEVAL_OUT wrote %d anonymized plans to %s. Read them before the key below.", len(keys), out)
	for _, k := range keys {
		t.Log(planEvalKeyLine(k))
	}
}

func buildPlanEvalProvider(e planEvalEntry, key string) (provider.Provider, error) {
	if e.Name == "anthropic" {
		return provider.NewAnthropic(provider.AnthropicConfig{APIKey: key, Model: e.Model})
	}
	return provider.NewOpenAI(provider.OpenAIConfig{Name: e.Name, APIKey: key, Model: e.Model})
}

func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("%s must be a positive integer", name)
	}
	return n
}

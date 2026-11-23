package planning

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

func TestTools_RegisterAndValidate(t *testing.T) {
	reg := tools.NewRegistry()
	for _, tool := range Tools() {
		if err := reg.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{ToolAskUser, ToolProposeBuildPlan} {
		if k, ok := reg.Kind(name); !ok || k != tools.KindPlanning {
			t.Fatalf("%s: kind %v ok=%v", name, k, ok)
		}
	}
	for _, s := range reg.Specs() {
		var schema map[string]any
		if err := json.Unmarshal(s.Schema, &schema); err != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s: bad schema (%v)", s.Name, err)
		}
		if s.Description == "" {
			t.Errorf("%s: no description", s.Name)
		}
	}

	dispatch := func(name string, args json.RawMessage) tools.Result {
		t.Helper()
		res, err := reg.Dispatch(context.Background(), tools.Call{ID: "c1", Name: name, Arguments: args, SessionID: "s", TurnID: "t"})
		if err != nil {
			t.Fatalf("%s: a bad call must be an error result, not a Go error: %v", name, err)
		}
		return res
	}

	if res := dispatch(ToolAskUser, questionsJSON(t, validQuestion("a"))); res.IsError {
		t.Errorf("valid questions refused: %s", res.Content)
	}
	if res := dispatch(ToolAskUser, questionsJSON(t)); !res.IsError || !strings.Contains(res.Content, "1 to 4 questions") {
		t.Errorf("empty round: %+v", res)
	}
	if res := dispatch(ToolProposeBuildPlan, mustJSON(t, validPlan())); res.IsError {
		t.Errorf("valid plan refused: %s", res.Content)
	}
	noDirection := validPlan()
	delete(noDirection["look"].(map[string]any), "direction")
	if res := dispatch(ToolProposeBuildPlan, mustJSON(t, noDirection)); !res.IsError || !strings.Contains(res.Content, "look.direction: required") {
		t.Errorf("plan without a direction must be a tool error the model can fix: %+v", res)
	}
	bad := validPlan()
	delete(bad, "assumptions")
	if res := dispatch(ToolProposeBuildPlan, mustJSON(t, bad)); !res.IsError || !strings.Contains(res.Content, "assumptions: required") {
		t.Errorf("plan without assumptions: %+v", res)
	}
}

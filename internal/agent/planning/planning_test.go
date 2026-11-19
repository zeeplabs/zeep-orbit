package planning

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func validQuestion(id string) map[string]any {
	return map[string]any{
		"id": id, "title": "Sign-in", "question": "Who signs in?",
		"options": []map[string]any{{"label": "Only my team"}, {"label": "Anyone", "description": "Open sign-up"}},
	}
}

func questionsJSON(t *testing.T, qs ...map[string]any) json.RawMessage {
	return mustJSON(t, map[string]any{"questions": qs})
}

func TestParseQuestions_ValidAndNormalized(t *testing.T) {
	q := validQuestion("sign_in")
	q["title"] = "  Sign-in\n"
	q["question"] = "Who\n\nsigns   in?"
	q["multiple"] = true
	got, err := ParseQuestions(questionsJSON(t, q, validQuestion("look")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Sign-in" || got[0].Question != "Who signs in?" || !got[0].Multiple || got[1].Multiple {
		t.Fatalf("unexpected questions: %+v", got)
	}
	if got[0].Options[1].Description != "Open sign-up" {
		t.Errorf("description lost: %+v", got[0].Options)
	}
}

func TestParseQuestions_Bounds(t *testing.T) {
	with := func(mut func(q map[string]any)) json.RawMessage {
		q := validQuestion("a")
		mut(q)
		return questionsJSON(t, q)
	}
	opts := func(n int) []map[string]any {
		out := make([]map[string]any, n)
		for i := range out {
			out[i] = map[string]any{"label": fmt.Sprintf("Option %d", i)}
		}
		return out
	}
	five := make([]map[string]any, 5)
	for i := range five {
		five[i] = validQuestion(fmt.Sprintf("q%d", i))
	}

	cases := []struct {
		name string
		raw  json.RawMessage
		want string // substring of the error; "" means valid
	}{
		{"not json", json.RawMessage(`nope`), `"questions" array`},
		{"no questions", questionsJSON(t), "1 to 4 questions"},
		{"five questions", questionsJSON(t, five...), "1 to 4 questions per round are allowed (got 5)"},
		{"four questions", questionsJSON(t, five[:4]...), ""},
		{"bad id", with(func(q map[string]any) { q["id"] = "Sign In" }), "questions[0].id"},
		{"empty id", with(func(q map[string]any) { q["id"] = "" }), "questions[0].id"},
		{"long id", with(func(q map[string]any) { q["id"] = "a" + strings.Repeat("b", 32) }), "questions[0].id"},
		{"duplicate id", questionsJSON(t, validQuestion("a"), validQuestion("a")), `"a" is used twice`},
		{"empty title", with(func(q map[string]any) { q["title"] = "  " }), "questions[0].title: required"},
		{"long title", with(func(q map[string]any) { q["title"] = strings.Repeat("x", 41) }), "questions[0].title: at most 40 characters (got 41)"},
		{"title at limit", with(func(q map[string]any) { q["title"] = strings.Repeat("é", 40) }), ""},
		{"long question", with(func(q map[string]any) { q["question"] = strings.Repeat("x", 301) }), "questions[0].question: at most 300"},
		{"one option", with(func(q map[string]any) { q["options"] = opts(1) }), "2 to 5 suggested answers are required (got 1)"},
		{"six options", with(func(q map[string]any) { q["options"] = opts(6) }), "2 to 5 suggested answers are required (got 6)"},
		{"five options", with(func(q map[string]any) { q["options"] = opts(5) }), ""},
		{"empty label", with(func(q map[string]any) { q["options"] = []map[string]any{{"label": ""}, {"label": "b"}} }), "questions[0].options[0].label: required"},
		{"long label", with(func(q map[string]any) {
			q["options"] = []map[string]any{{"label": strings.Repeat("x", 81)}, {"label": "b"}}
		}), "options[0].label: at most 80"},
		{"long description", with(func(q map[string]any) {
			q["options"] = []map[string]any{{"label": "a", "description": strings.Repeat("x", 161)}, {"label": "b"}}
		}), "options[0].description: at most 160"},
		{"duplicate label", with(func(q map[string]any) {
			q["options"] = []map[string]any{{"label": "Same"}, {"label": " Same "}}
		}), `"Same" is used twice`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseQuestions(tc.raw)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func validPlan() map[string]any {
	return map[string]any{
		"summary": "A CRM for a small clinic.",
		"screens": []map[string]any{
			{"name": "Patients", "purpose": "List and search patients"},
			{"name": "Patient", "purpose": "One patient's record"},
		},
		"data": []map[string]any{
			{"table": "patients", "holds": "People the clinic attends", "access": "signed_in"},
			{"table": "notes", "holds": "Notes per visit", "access": "role", "access_note": "doctors write, reception reads"},
		},
		"look":         map[string]any{"style": "clean", "colors": "teal", "ui_language": "pt-BR"},
		"assumptions":  []string{"Email and password sign-in"},
		"out_of_scope": []string{"Billing"},
		"milestones": []map[string]any{
			{"title": "Sign-in and patient list", "deliverable": "Sign in and see the list"},
			{"title": "Patient record", "deliverable": "Open a patient and add a note"},
		},
	}
}

func TestParsePlan_ValidAndNormalized(t *testing.T) {
	m := validPlan()
	m["summary"] = "  A CRM\nfor a small   clinic. "
	m["extra_field"] = "ignored"
	p, err := ParsePlan(mustJSON(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if p.Summary != "A CRM for a small clinic." {
		t.Errorf("summary = %q", p.Summary)
	}
	if len(p.Screens) != 2 || len(p.Data) != 2 || len(p.Milestones) != 2 || p.Look.UILanguage != "pt-BR" {
		t.Fatalf("unexpected plan: %+v", p)
	}
	// The normalized plan drops what the schema does not know.
	if strings.Contains(string(mustJSON(t, p)), "extra_field") {
		t.Error("unknown field kept in the normalized plan")
	}
	if p.HasPublicData() {
		t.Error("no entry is public")
	}
}

func TestParsePlan_EmptyDataAndAssumptions(t *testing.T) {
	m := validPlan()
	delete(m, "data")
	m["assumptions"] = []string{}
	delete(m, "out_of_scope")
	delete(m, "look")
	p, err := ParsePlan(mustJSON(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if p.Data == nil || len(p.Data) != 0 {
		t.Errorf("data must be an empty list, got %#v", p.Data)
	}
	if !strings.Contains(string(mustJSON(t, p)), `"data":[]`) {
		t.Errorf("stored plan must carry an empty data list: %s", mustJSON(t, p))
	}
}

func TestParsePlan_Bounds(t *testing.T) {
	with := func(mut func(m map[string]any)) json.RawMessage {
		m := validPlan()
		mut(m)
		return mustJSON(t, m)
	}
	screens := func(n int) []map[string]any {
		out := make([]map[string]any, n)
		for i := range out {
			out[i] = map[string]any{"name": fmt.Sprintf("S%d", i), "purpose": "p"}
		}
		return out
	}
	milestones := func(n int) []map[string]any {
		out := make([]map[string]any, n)
		for i := range out {
			out[i] = map[string]any{"title": fmt.Sprintf("M%d", i), "deliverable": "d"}
		}
		return out
	}
	strs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("item %d", i)
		}
		return out
	}
	data := func(access, note string) []map[string]any {
		return []map[string]any{{"table": "t", "holds": "h", "access": access, "access_note": note}}
	}

	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"not an object", json.RawMessage(`[]`), "must be a JSON object"},
		{"wrong type", with(func(m map[string]any) { m["screens"] = "many" }), "do not match the schema"},
		{"no assumptions key", with(func(m map[string]any) { delete(m, "assumptions") }), "assumptions: required"},
		{"no summary", with(func(m map[string]any) { m["summary"] = "" }), "summary: required"},
		{"long summary", with(func(m map[string]any) { m["summary"] = strings.Repeat("x", 601) }), "summary: at most 600"},
		{"no screens", with(func(m map[string]any) { m["screens"] = screens(0) }), "screens: 1 to 20 items are allowed (got 0)"},
		{"21 screens", with(func(m map[string]any) { m["screens"] = screens(21) }), "screens: 1 to 20 items are allowed (got 21)"},
		{"20 screens", with(func(m map[string]any) { m["screens"] = screens(20) }), ""},
		{"screen without purpose", with(func(m map[string]any) {
			m["screens"] = []map[string]any{{"name": "A"}}
		}), "screens[0].purpose: required"},
		{"bad access", with(func(m map[string]any) { m["data"] = data("everyone", "") }), "data[0].access: must be"},
		{"public access", with(func(m map[string]any) { m["data"] = data("public", "") }), ""},
		{"owner access", with(func(m map[string]any) { m["data"] = data("owner", "") }), ""},
		{"role without note", with(func(m map[string]any) { m["data"] = data("role", " ") }), "data[0].access_note: required"},
		{"16 assumptions", with(func(m map[string]any) { m["assumptions"] = strs(16) }), "assumptions: 0 to 15 items are allowed (got 16)"},
		{"empty assumption", with(func(m map[string]any) { m["assumptions"] = []string{" "} }), "assumptions[0]: required"},
		{"11 out of scope", with(func(m map[string]any) { m["out_of_scope"] = strs(11) }), "out_of_scope: 0 to 10"},
		{"no milestones", with(func(m map[string]any) { m["milestones"] = milestones(0) }), "milestones: 1 to 8 items are allowed (got 0)"},
		{"nine milestones", with(func(m map[string]any) { m["milestones"] = milestones(9) }), "milestones: 1 to 8 items are allowed (got 9)"},
		{"eight milestones", with(func(m map[string]any) { m["milestones"] = milestones(8) }), ""},
		{"long milestone title", with(func(m map[string]any) {
			m["milestones"] = []map[string]any{{"title": strings.Repeat("x", 81), "deliverable": "d"}}
		}), "milestones[0].title: at most 80"},
		{"long look field", with(func(m map[string]any) {
			m["look"] = map[string]any{"style": strings.Repeat("x", 201)}
		}), "look.style: at most 200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePlan(tc.raw)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A plan can be inside every per-field bound and still be too large as a
// whole: both size limits name the excess, so the model knows how much to cut.
func TestParsePlan_SizeLimits(t *testing.T) {
	big := func(items, chars int) map[string]any {
		m := validPlan()
		screens := make([]map[string]any, items)
		data := make([]map[string]any, items)
		for i := range screens {
			screens[i] = map[string]any{"name": fmt.Sprintf("Screen %d", i), "purpose": strings.Repeat("p", chars)}
			data[i] = map[string]any{"table": fmt.Sprintf("table_%d", i), "holds": strings.Repeat("h", chars), "access": "owner"}
		}
		m["screens"], m["data"] = screens, data
		return m
	}

	_, err := ParsePlan(mustJSON(t, big(20, 240)))
	if err == nil || !strings.Contains(err.Error(), "the plan is too long") || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("a plan over the byte limit must be refused by bytes: %v", err)
	}

	raw := mustJSON(t, big(20, 140))
	if len(raw) > MaxPlanBytes {
		t.Fatalf("test plan must fit the byte limit to reach the rendered check (%d bytes)", len(raw))
	}
	_, err = ParsePlan(raw)
	if err == nil || !strings.Contains(err.Error(), "renders to") || !strings.Contains(err.Error(), "Shorten the descriptions by at least") {
		t.Fatalf("a plan over the rendered limit must say by how much: %v", err)
	}

	// Every accepted plan renders within the limit.
	p, err := ParsePlan(mustJSON(t, big(15, 100)))
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(RenderBrief(p)); n > MaxBriefChars {
		t.Fatalf("accepted plan renders to %d characters", n)
	}
}

func TestNextMilestone(t *testing.T) {
	p := &Plan{Milestones: []Milestone{{Title: "a", Done: true}, {Title: "b"}, {Title: "c"}}}
	if got := p.NextMilestone(nil); got != 2 {
		t.Errorf("next = %d, want 2", got)
	}
	if got := p.NextMilestone(map[int]bool{2: true}); got != 3 {
		t.Errorf("next = %d, want 3", got)
	}
	if got := p.NextMilestone(map[int]bool{2: true, 3: true}); got != 0 {
		t.Errorf("next = %d, want 0 (all done)", got)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"  a  b ":         "a b",
		"a\nb\r\n\tc":     "a b c",
		"a\x00b\x1bc":     "abc",
		"\n\n":            "",
		"- x\n- injected": "- x - injected",
		"ação válida":     "ação válida",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

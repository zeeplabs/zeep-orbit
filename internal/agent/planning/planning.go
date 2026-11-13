// Package planning holds the planning step of the AI builder
// (ai-builder-planning spec): the arguments of the two planning tools
// (ask_user and propose_build_plan), their validation, and the texts rendered
// from them (the brief in the system prompt, the brief file in the workspace,
// and the tool results the agent reads after the user decides).
//
// It is pure: no database, no HTTP. Validation errors are written for the
// model, which corrects the call and tries again, so they name the field and
// the bound.
package planning

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tool names.
const (
	ToolAskUser          = "ask_user"
	ToolProposeBuildPlan = "propose_build_plan"
)

// Bounds of a question round (ABP-02, ABP-04).
const (
	MaxQuestionsPerRound = 4
	MinOptions           = 2
	MaxOptions           = 5
	MaxTitleChars        = 40
	MaxQuestionChars     = 300
	MaxOptionLabelChars  = 80
	MaxOptionDescChars   = 160
	// MaxAnswerTextChars caps the text a user types for one question.
	MaxAnswerTextChars = 500
	// MaxFeedbackChars caps the text a user sends when asking for plan changes.
	MaxFeedbackChars = 2000

	// MaxPlanRounds and MaxBuildRounds are the question rounds one turn may
	// ask in plan mode and in build mode.
	MaxPlanRounds  = 3
	MaxBuildRounds = 1
)

// Bounds of a build plan (ABP-07, ABP-14).
const (
	// MaxPlanBytes is the size of the propose_build_plan arguments. It equals
	// the cap of an object in a builder event, so the proposal event carries
	// the whole plan.
	MaxPlanBytes = 8 << 10
	// MaxBriefChars is the size of the rendered brief ([RenderBrief]). A plan
	// that renders larger is refused, so the brief in the prompt is never
	// truncated.
	MaxBriefChars = 6000

	MaxSummaryChars     = 600
	MaxScreens          = 20
	MaxDataEntries      = 20
	MaxAssumptions      = 15
	MaxOutOfScope       = 10
	MaxMilestones       = 8
	maxNameChars        = 60
	maxLineChars        = 240
	maxMilestoneTitle   = 80
	maxLookFieldChars   = 200
	maxAccessNoteChars  = 160
	maxOutOfScopeChars  = 160
	maxUILanguageChars  = 40
	maxQuestionIDLength = 32
)

// Access values of a data entry: who can reach the table.
const (
	AccessPublic   = "public"    // anyone, signed in or not
	AccessSignedIn = "signed_in" // any signed-in user
	AccessOwner    = "owner"     // each user only their own rows
	AccessRole     = "role"      // depends on the user's role (see the note)
)

var questionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// Option is one suggested answer of a question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Question is one question of an ask_user round.
type Question struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Question string   `json:"question"`
	Options  []Option `json:"options"`
	// Multiple lets the user choose several options.
	Multiple bool `json:"multiple,omitempty"`
}

type questionsArgs struct {
	Questions []Question `json:"questions"`
}

// ParseQuestions decodes and validates the arguments of ask_user. The returned
// questions are normalized (trimmed, whitespace collapsed).
func ParseQuestions(raw json.RawMessage) ([]Question, error) {
	var args questionsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errors.New("arguments must be a JSON object with a \"questions\" array")
	}
	n := len(args.Questions)
	if n < 1 || n > MaxQuestionsPerRound {
		return nil, fmt.Errorf("questions: 1 to %d questions per round are allowed (got %d)", MaxQuestionsPerRound, n)
	}
	seen := make(map[string]bool, n)
	for i := range args.Questions {
		q := &args.Questions[i]
		at := fmt.Sprintf("questions[%d]", i)
		q.ID = strings.TrimSpace(q.ID)
		if !questionIDPattern.MatchString(q.ID) {
			return nil, fmt.Errorf("%s.id: must be a short lowercase identifier such as \"sign_in\" (letters, digits and _, at most %d characters)", at, maxQuestionIDLength)
		}
		if seen[q.ID] {
			return nil, fmt.Errorf("%s.id: %q is used twice; ids must be unique in the round", at, q.ID)
		}
		seen[q.ID] = true

		var err error
		if q.Title, err = field(at+".title", q.Title, 1, MaxTitleChars); err != nil {
			return nil, err
		}
		if q.Question, err = field(at+".question", q.Question, 1, MaxQuestionChars); err != nil {
			return nil, err
		}
		if len(q.Options) < MinOptions || len(q.Options) > MaxOptions {
			return nil, fmt.Errorf("%s.options: %d to %d suggested answers are required (got %d); do not add \"other\" or \"you decide\", the user always has both",
				at, MinOptions, MaxOptions, len(q.Options))
		}
		labels := make(map[string]bool, len(q.Options))
		for j := range q.Options {
			o := &q.Options[j]
			oat := fmt.Sprintf("%s.options[%d]", at, j)
			if o.Label, err = field(oat+".label", o.Label, 1, MaxOptionLabelChars); err != nil {
				return nil, err
			}
			if o.Description, err = field(oat+".description", o.Description, 0, MaxOptionDescChars); err != nil {
				return nil, err
			}
			if labels[o.Label] {
				return nil, fmt.Errorf("%s.label: %q is used twice in the question", oat, o.Label)
			}
			labels[o.Label] = true
		}
	}
	return args.Questions, nil
}

// Screen is one screen of the planned app.
type Screen struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

// DataEntry is one thing the app stores, in words, and who can reach it. It is
// intent: the table itself is created later by a backend proposal.
type DataEntry struct {
	Table      string `json:"table"`
	Holds      string `json:"holds"`
	Access     string `json:"access"`
	AccessNote string `json:"access_note,omitempty"`
}

// Look is how the app should look.
type Look struct {
	Style      string `json:"style,omitempty"`
	Colors     string `json:"colors,omitempty"`
	UILanguage string `json:"ui_language,omitempty"`
	Reference  string `json:"reference,omitempty"`
}

// Milestone is one step of the plan, small enough to end in one visible build.
type Milestone struct {
	Title       string `json:"title"`
	Deliverable string `json:"deliverable"`
	// Done marks a milestone that is already built. Only meaningful when an
	// approved plan is revised.
	Done bool `json:"done,omitempty"`
}

// Plan is the arguments of propose_build_plan and, once approved, the brief of
// the session.
type Plan struct {
	Summary     string      `json:"summary"`
	Screens     []Screen    `json:"screens"`
	Data        []DataEntry `json:"data"`
	Look        Look        `json:"look"`
	Assumptions []string    `json:"assumptions"`
	OutOfScope  []string    `json:"out_of_scope"`
	Milestones  []Milestone `json:"milestones"`
}

// ParsePlan decodes and validates the arguments of propose_build_plan. The
// returned plan is normalized; marshal it (not raw) to store the brief.
func ParsePlan(raw json.RawMessage) (*Plan, error) {
	if len(raw) > MaxPlanBytes {
		return nil, fmt.Errorf("the plan is too long: %d bytes, the limit is %d. Shorten the descriptions; keep every screen, table and milestone", len(raw), MaxPlanBytes)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, errors.New("arguments must be a JSON object")
	}
	if _, ok := keys["assumptions"]; !ok {
		return nil, errors.New("assumptions: required. List everything you decided without the user saying it, or send an empty list when there is nothing")
	}
	var p Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("arguments do not match the schema: check the type of every field")
	}

	var err error
	if p.Summary, err = field("summary", p.Summary, 1, MaxSummaryChars); err != nil {
		return nil, err
	}
	if err := count("screens", len(p.Screens), 1, MaxScreens); err != nil {
		return nil, err
	}
	for i := range p.Screens {
		s := &p.Screens[i]
		at := fmt.Sprintf("screens[%d]", i)
		if s.Name, err = field(at+".name", s.Name, 1, maxNameChars); err != nil {
			return nil, err
		}
		if s.Purpose, err = field(at+".purpose", s.Purpose, 1, maxLineChars); err != nil {
			return nil, err
		}
	}
	if err := count("data", len(p.Data), 0, MaxDataEntries); err != nil {
		return nil, err
	}
	for i := range p.Data {
		d := &p.Data[i]
		at := fmt.Sprintf("data[%d]", i)
		if d.Table, err = field(at+".table", d.Table, 1, maxNameChars); err != nil {
			return nil, err
		}
		if d.Holds, err = field(at+".holds", d.Holds, 1, maxLineChars); err != nil {
			return nil, err
		}
		d.Access = strings.TrimSpace(d.Access)
		switch d.Access {
		case AccessPublic, AccessSignedIn, AccessOwner, AccessRole:
		default:
			return nil, fmt.Errorf("%s.access: must be %q, %q, %q or %q", at, AccessPublic, AccessSignedIn, AccessOwner, AccessRole)
		}
		if d.AccessNote, err = field(at+".access_note", d.AccessNote, 0, maxAccessNoteChars); err != nil {
			return nil, err
		}
		if d.Access == AccessRole && d.AccessNote == "" {
			return nil, fmt.Errorf("%s.access_note: required when access is %q: say which role can do what", at, AccessRole)
		}
	}
	if p.Look.Style, err = field("look.style", p.Look.Style, 0, maxLookFieldChars); err != nil {
		return nil, err
	}
	if p.Look.Colors, err = field("look.colors", p.Look.Colors, 0, maxLookFieldChars); err != nil {
		return nil, err
	}
	if p.Look.UILanguage, err = field("look.ui_language", p.Look.UILanguage, 0, maxUILanguageChars); err != nil {
		return nil, err
	}
	if p.Look.Reference, err = field("look.reference", p.Look.Reference, 0, maxLookFieldChars); err != nil {
		return nil, err
	}
	if p.Assumptions, err = lines("assumptions", p.Assumptions, MaxAssumptions, maxLineChars); err != nil {
		return nil, err
	}
	if p.OutOfScope, err = lines("out_of_scope", p.OutOfScope, MaxOutOfScope, maxOutOfScopeChars); err != nil {
		return nil, err
	}
	if err := count("milestones", len(p.Milestones), 1, MaxMilestones); err != nil {
		return nil, err
	}
	for i := range p.Milestones {
		m := &p.Milestones[i]
		at := fmt.Sprintf("milestones[%d]", i)
		if m.Title, err = field(at+".title", m.Title, 1, maxMilestoneTitle); err != nil {
			return nil, err
		}
		if m.Deliverable, err = field(at+".deliverable", m.Deliverable, 1, maxLineChars); err != nil {
			return nil, err
		}
	}
	if p.Data == nil {
		p.Data = []DataEntry{}
	}

	if n := utf8.RuneCountInString(RenderBrief(&p)); n > MaxBriefChars {
		return nil, fmt.Errorf("the plan is too long: it renders to %d characters, the limit is %d. Shorten the descriptions by at least %d characters; keep every screen, table and milestone",
			n, MaxBriefChars, n-MaxBriefChars)
	}
	return &p, nil
}

// NextMilestone returns the 1-based index of the first milestone that is not
// done, counting the plan's own done marks and the indexes in done (milestones
// finished by a turn). It returns 0 when every milestone is done.
func (p *Plan) NextMilestone(done map[int]bool) int {
	for i, m := range p.Milestones {
		if !m.Done && !done[i+1] {
			return i + 1
		}
	}
	return 0
}

// HasPublicData reports whether any data entry is open to everyone.
func (p *Plan) HasPublicData() bool {
	for _, d := range p.Data {
		if d.Access == AccessPublic {
			return true
		}
	}
	return false
}

func count(name string, n, min, max int) error {
	if n < min || n > max {
		return fmt.Errorf("%s: %d to %d items are allowed (got %d)", name, min, max, n)
	}
	return nil
}

func lines(name string, in []string, maxItems, maxChars int) ([]string, error) {
	if err := count(name, len(in), 0, maxItems); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(in))
	for i, s := range in {
		v, err := field(fmt.Sprintf("%s[%d]", name, i), s, 1, maxChars)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// field normalizes one text field (see [Clean]) and checks its length in
// characters.
func field(name, s string, min, max int) (string, error) {
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("%s: must be valid text", name)
	}
	s = Clean(s)
	n := utf8.RuneCountInString(s)
	switch {
	case n < min:
		return "", fmt.Errorf("%s: required", name)
	case n > max:
		return "", fmt.Errorf("%s: at most %d characters (got %d)", name, max, n)
	}
	return s, nil
}

// Clean makes s a single line: control characters are dropped and every run
// of whitespace (line breaks included) becomes one space. Text from the model
// or the user that goes through it cannot add lines to a rendered brief or
// tool result.
func Clean(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
		case unicode.IsControl(r) || r == utf8.RuneError:
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

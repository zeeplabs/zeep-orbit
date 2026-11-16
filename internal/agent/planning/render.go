package planning

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// BriefFileName is the workspace file the approved brief is written to, at the
// root next to the template guide.
const BriefFileName = "ORBIT_BRIEF.md"

// RenderBrief renders the plan as the compact text placed in the system
// prompt. It is deterministic: the same plan always yields the same bytes, so
// the prompt prefix that contains it stays stable between turns. Progress is
// not part of it (see [RenderProgress]).
func RenderBrief(p *Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Summary: %s\n", p.Summary)

	b.WriteString("\nScreens:\n")
	for _, s := range p.Screens {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Purpose)
	}

	if len(p.Data) > 0 {
		b.WriteString("\nData (intent; tables are created through backend proposals):\n")
		for _, d := range p.Data {
			fmt.Fprintf(&b, "- %s [%s]: %s\n", d.Table, accessText(d), d.Holds)
		}
	}

	if look := lookText(p.Look); look != "" {
		fmt.Fprintf(&b, "\nLook: %s\n", look)
	}

	if len(p.Assumptions) > 0 {
		b.WriteString("\nAssumptions (decided by the agent, not said by the user):\n")
		for _, a := range p.Assumptions {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	if len(p.OutOfScope) > 0 {
		b.WriteString("\nOut of scope:\n")
		for _, o := range p.OutOfScope {
			fmt.Fprintf(&b, "- %s\n", o)
		}
	}

	b.WriteString("\nMilestones:\n")
	for i, m := range p.Milestones {
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, m.Title, m.Deliverable)
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderBriefFile renders the content of [BriefFileName] for version.
func RenderBriefFile(p *Plan, version int) string {
	return fmt.Sprintf("# App brief (version %d)\n\n"+
		"What was agreed with the user before building. The AI builder rewrites this file when a new plan is approved; edits made here are overwritten.\n\n"+
		"%s\n", version, RenderBrief(p))
}

// RenderProgress renders the milestone list with what is done, for the
// per-turn part of the prompt. done holds the 1-based indexes finished by a
// turn; current is the milestone of this turn (0 for none).
func RenderProgress(p *Plan, done map[int]bool, current int) string {
	var b strings.Builder
	for i, m := range p.Milestones {
		state := "pending"
		switch {
		case m.Done || done[i+1]:
			state = "done"
		case i+1 == current:
			state = "this turn"
		}
		fmt.Fprintf(&b, "%d. %s [%s]\n", i+1, m.Title, state)
	}
	return strings.TrimRight(b.String(), "\n")
}

func accessText(d DataEntry) string {
	var who string
	switch d.Access {
	case AccessPublic:
		who = "open to everyone, signed in or not"
	case AccessSignedIn:
		who = "any signed-in user (anyone can create an account, so this is not limited to a team)"
	case AccessOwner:
		who = "each user only their own rows"
	default:
		who = "by role"
	}
	if d.AccessNote != "" {
		who += "; " + d.AccessNote
	}
	return who
}

func lookText(l Look) string {
	parts := make([]string, 0, 4)
	if l.Style != "" {
		parts = append(parts, "style: "+l.Style)
	}
	if l.Colors != "" {
		parts = append(parts, "colors: "+l.Colors)
	}
	if l.UILanguage != "" {
		parts = append(parts, "interface language: "+l.UILanguage)
	}
	if l.Reference != "" {
		parts = append(parts, "reference: "+l.Reference)
	}
	return strings.Join(parts, "; ")
}

// Answer is the user's answer to one question of a round.
type Answer struct {
	ID string `json:"id"`
	// Choices are labels of the question's options.
	Choices []string `json:"choices"`
	// Text is what the user typed instead of, or in addition to, a choice.
	Text string `json:"text"`
	// Decide is "decide for me": the agent chooses and records an assumption.
	Decide bool `json:"decide"`
}

// NormalizeAnswers validates answers against the questions that were asked
// and returns one answer per question, in the order of the questions. A
// question with no answer, or an answer with neither a choice nor text, is
// "decide for me". The errors are shown to the user's client (English).
func NormalizeAnswers(questions []Question, answers []Answer) ([]Answer, error) {
	byID := make(map[string]Question, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
	}
	given := make(map[string]Answer, len(answers))
	for _, a := range answers {
		q, ok := byID[a.ID]
		if !ok {
			return nil, fmt.Errorf("answer %q does not match a question of this round", capText(a.ID, 64))
		}
		if _, dup := given[a.ID]; dup {
			return nil, fmt.Errorf("question %q is answered twice", a.ID)
		}
		if !utf8.ValidString(a.Text) {
			return nil, fmt.Errorf("the answer to %q contains invalid characters", a.ID)
		}
		a.Text = Clean(a.Text)
		if utf8.RuneCountInString(a.Text) > MaxAnswerTextChars {
			return nil, fmt.Errorf("the answer to %q is too long (at most %d characters)", a.ID, MaxAnswerTextChars)
		}
		if len(a.Choices) > 1 && !q.Multiple {
			return nil, fmt.Errorf("question %q accepts one choice", a.ID)
		}
		seen := make(map[string]bool, len(a.Choices))
		for _, c := range a.Choices {
			if !hasOption(q, c) {
				return nil, fmt.Errorf("the answer to %q has a choice that is not one of its options", a.ID)
			}
			if seen[c] {
				return nil, fmt.Errorf("the answer to %q repeats a choice", a.ID)
			}
			seen[c] = true
		}
		if a.Decide && (len(a.Choices) > 0 || a.Text != "") {
			return nil, fmt.Errorf("the answer to %q cannot be both a choice and \"decide for me\"", a.ID)
		}
		given[a.ID] = a
	}

	out := make([]Answer, 0, len(questions))
	for _, q := range questions {
		a, ok := given[q.ID]
		if !ok || (len(a.Choices) == 0 && a.Text == "") {
			a = Answer{ID: q.ID, Decide: true}
		}
		if a.Choices == nil {
			a.Choices = []string{}
		}
		out = append(out, a)
	}
	return out, nil
}

func hasOption(q Question, label string) bool {
	for _, o := range q.Options {
		if o.Label == label {
			return true
		}
	}
	return false
}

// AnswersResult is the tool result of an answered round: one line per
// question. answers must come from [NormalizeAnswers]. What the user typed is
// quoted, so it reads as data and cannot be taken for another line.
func AnswersResult(questions []Question, answers []Answer) string {
	var b strings.Builder
	b.WriteString("The user answered:\n")
	for i, q := range questions {
		fmt.Fprintf(&b, "- %s (%s): %s\n", q.ID, q.Title, answerText(answers[i], true))
	}
	b.WriteString("Use these answers. Where the user let you decide, choose a sensible default and list it under assumptions in the plan.")
	return b.String()
}

// AnswersSummary is the short form of the answers kept in the event log: the
// question id mapped to the answer, each capped at maxChars characters.
func AnswersSummary(questions []Question, answers []Answer, maxChars int) map[string]string {
	out := make(map[string]string, len(questions))
	for i, q := range questions {
		out[q.ID] = capText(answerText(answers[i], false), maxChars)
	}
	return out
}

func answerText(a Answer, forModel bool) string {
	if a.Decide {
		if forModel {
			return "no preference, you decide"
		}
		return ""
	}
	parts := make([]string, 0, len(a.Choices)+1)
	parts = append(parts, a.Choices...)
	if a.Text != "" {
		if forModel {
			parts = append(parts, fmt.Sprintf("in the user's words: %q", a.Text))
		} else {
			parts = append(parts, a.Text)
		}
	}
	return strings.Join(parts, "; ")
}

// SkippedResult is the tool result when the user ends the questions.
const SkippedResult = "The user ended the questions and wants to move on. Do not ask again in this turn. " +
	"Call propose_build_plan now with what you know, and list every open point under assumptions."

// ApprovedResult is the tool result of an approved plan. milestone is the
// 1-based milestone to build now, or 0 when none is pending.
func ApprovedResult(p *Plan, version, milestone int) string {
	if milestone < 1 || milestone > len(p.Milestones) {
		return fmt.Sprintf("The user approved the plan. It is now the app brief, version %d. No milestone is pending: tell the user and stop.", version)
	}
	m := p.Milestones[milestone-1]
	return fmt.Sprintf("The user approved the plan. It is now the app brief, version %d. "+
		"Build milestone %d now: %s (%s). Build only this milestone, make the build pass, then stop and tell the user what was done and what the next milestone is.",
		version, milestone, m.Title, m.Deliverable)
}

// ErrFeedbackRequired: a plan revision without more questions needs the
// user's text.
var ErrFeedbackRequired = errors.New("feedback is required")

// RevisionResult is the tool result when the user did not approve the plan.
// feedback is the user's text; moreQuestions sends the agent back to one more
// round of questions. It returns ErrFeedbackRequired when there is neither.
func RevisionResult(feedback string, moreQuestions bool) (string, error) {
	if !utf8.ValidString(feedback) {
		return "", errors.New("feedback contains invalid characters")
	}
	feedback = Clean(feedback)
	if utf8.RuneCountInString(feedback) > MaxFeedbackChars {
		return "", fmt.Errorf("feedback is too long (at most %d characters)", MaxFeedbackChars)
	}
	if feedback == "" && !moreQuestions {
		return "", ErrFeedbackRequired
	}
	var b strings.Builder
	b.WriteString("The user did not approve the plan.")
	if feedback != "" {
		fmt.Fprintf(&b, " In the user's words: %q.", feedback)
	}
	if moreQuestions {
		b.WriteString(" The user wants to answer more questions first: ask one more round with ask_user, then call propose_build_plan again.")
	} else {
		b.WriteString(" Revise the plan and call propose_build_plan again.")
	}
	return b.String(), nil
}

func capText(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

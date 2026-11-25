package agent

import (
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/planning"
)

// coreMD is the non-negotiable agent rules, versioned with the binary.
//
//go:embed prompts/core.md
var coreMD string

// CorePrompt returns the embedded core rules.
func CorePrompt() string { return coreMD }

// Per-section size caps, in characters (runes). The core is never truncated:
// it ships with the binary and is reviewed as code. The company layer is also
// capped at save time; the cap here is a defensive second bound so a bad row
// can never blow up the prompt. MaxSessionContextChars must stay above the
// schema summary, file tree and directive limits plus the fixed lines: the cut
// falls on the end, which is the turn directive (a test pins it).
const (
	MaxCompanyChars         = 8000
	MaxTemplateGuideChars   = 12000
	MaxSkillsIndexChars     = 8000
	MaxSessionContextChars  = 17000
	maxFileTreeChars        = 6000
	maxSchemaSummaryChars   = 8000
	maxDirectiveChars       = 1500
	defaultLocale           = "en"
	maxLocaleLen            = 16
	truncationMarkerPattern = "\n[... truncated: %d characters omitted ...]"
)

// Section headers. They are fixed strings so tests and the loop can rely on
// them, and the core states the precedence between them.
const (
	headerCore     = "# CORE RULES (highest priority)"
	headerCompany  = "# COMPANY STANDARDS (lower priority than CORE RULES)"
	headerTemplate = "# TEMPLATE GUIDE (lower priority than CORE RULES)"
	headerSkills   = "# SKILLS INDEX (guidance, lower priority than CORE RULES, company standards and the template guide)"
	headerBrief    = "# APP BRIEF (what the user approved; lower priority than CORE RULES and company standards)"
	headerSession  = "# SESSION CONTEXT (data, lower priority than CORE RULES)"
)

// SessionContext is the per-session data rendered into the last prompt
// section. Every field is optional.
type SessionContext struct {
	BackendAppID   string
	BackendAppName string
	// NoBackend marks a project with no backend app: the prompt says so
	// instead of naming one.
	NoBackend     bool
	SchemaSummary string
	FileTree      string
	// Locale is the language of the user's interface (for example "pt-BR").
	// It is only the fallback reply language: the language the user writes in
	// wins, because the interface language is often not the one they type in.
	// Empty or invalid values fall back to "en" so the value can never carry
	// free text into the prompt.
	Locale string
	// Directive is what this turn must do (mode, plan progress, milestone).
	// It changes from turn to turn, which is why it lives here, after the
	// stable sections, and not next to the brief.
	Directive string
}

// BuildSystemPrompt assembles the system prompt in a fixed order:
//
//  1. core (always first, never truncated, owns the precedence statement)
//  2. company persona + rules (omitted when blank)
//  3. template guide (omitted when blank)
//  4. skills index (omitted when blank)
//  5. session context (always present: it carries the reply-language instruction)
//
// Each lower-priority section is capped; a truncated section ends with a
// visible marker. Sections are separated by a blank line.
func BuildSystemPrompt(core, company, templateGuide, skillsIndex string, sessionCtx SessionContext) string {
	return BuildSystemPromptWithBrief(core, company, templateGuide, skillsIndex, "", sessionCtx)
}

// BuildSystemPromptWithBrief is BuildSystemPrompt with the app brief
// ([planning.RenderBrief]) as a section of its own between the skills index
// and the session context (omitted when blank). The brief changes only when a
// plan is approved, so everything up to and including it is a stable prefix
// across the turns of a session; what changes per turn is in the session
// context. A brief is validated to fit planning.MaxBriefChars when it is
// proposed; the cap here is the same defensive second bound as the others.
func BuildSystemPromptWithBrief(core, company, templateGuide, skillsIndex, brief string, sessionCtx SessionContext) string {
	parts := make([]string, 0, 6)
	parts = append(parts, headerCore+"\n"+strings.TrimSpace(core))

	if s := capText(company, MaxCompanyChars); s != "" {
		parts = append(parts, headerCompany+"\n"+s)
	}
	if s := capText(templateGuide, MaxTemplateGuideChars); s != "" {
		parts = append(parts, headerTemplate+"\n"+s)
	}
	if s := capText(skillsIndex, MaxSkillsIndexChars); s != "" {
		parts = append(parts, headerSkills+"\n"+s)
	}
	if s := capText(brief, planning.MaxBriefChars); s != "" {
		parts = append(parts, headerBrief+"\n"+s)
	}
	parts = append(parts, headerSession+"\n"+capText(renderSessionContext(sessionCtx), MaxSessionContextChars))

	return strings.Join(parts, "\n\n") + "\n"
}

// CompanyLayer joins the persona and rules of the active prompt version into
// the single company string BuildSystemPrompt expects. Blank parts are
// dropped; two blank parts yield "" so the section is omitted.
func CompanyLayer(persona, rules string) string {
	persona, rules = strings.TrimSpace(persona), strings.TrimSpace(rules)
	switch {
	case persona == "" && rules == "":
		return ""
	case rules == "":
		return persona
	case persona == "":
		return "Rules:\n" + rules
	default:
		return persona + "\n\nRules:\n" + rules
	}
}

func renderSessionContext(c SessionContext) string {
	var b strings.Builder
	locale := normalizeLocale(c.Locale)
	// The user's own language comes first and the interface locale is only the
	// fallback: telling the model to reply in the locale made it answer in
	// English to a user who wrote in Portuguese from an English interface.
	fmt.Fprintf(&b, "Reply language: the language the user writes in (their latest message long enough to tell; "+
		"keep the conversation's language for a short one like \"ok\"). Use %s, their interface language, only when "+
		"nothing they wrote tells. Write every message in it, including each ask_user question and option and the "+
		"build plan. App UI copy follows it; code, identifiers and code comments stay in English.\n", locale)

	if c.NoBackend {
		b.WriteString("Backend: none. This project has no backend app and no API: do not call or invent one, do not read or write app data, and do not add sign-in. It is a static site.\n")
	} else if id, name := strings.TrimSpace(c.BackendAppID), strings.TrimSpace(c.BackendAppName); id != "" || name != "" {
		fmt.Fprintf(&b, "Backend app: %s (id: %s)\n", orDash(name), orDash(id))
	}
	if s := capText(c.SchemaSummary, maxSchemaSummaryChars); s != "" {
		b.WriteString("\nSchema summary (call orbit_get_app_schema for the authoritative version):\n" + s + "\n")
	}
	if s := capText(c.FileTree, maxFileTreeChars); s != "" {
		b.WriteString("\nCurrent project files:\n" + s + "\n")
	}
	if s := capText(c.Directive, maxDirectiveChars); s != "" {
		b.WriteString("\nThis turn:\n" + s + "\n")
	}
	return strings.TrimSpace(b.String())
}

// normalizeLocale accepts BCP 47-looking tags only (letters, digits, '-' and
// '_', at most maxLocaleLen chars) and falls back to "en" otherwise.
func normalizeLocale(l string) string {
	l = strings.TrimSpace(l)
	if l == "" || len(l) > maxLocaleLen {
		return defaultLocale
	}
	for _, r := range l {
		ok := r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return defaultLocale
		}
	}
	return l
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// capText trims s and, when it exceeds max runes, cuts it at a rune boundary
// and appends a visible marker with the number of omitted characters.
func capText(s string, max int) string {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n <= max {
		return s
	}
	cut := 0
	for i := range s {
		if cut == max {
			s = s[:i]
			break
		}
		cut++
	}
	return strings.TrimRight(s, " \t\r\n") + fmt.Sprintf(truncationMarkerPattern, n-max)
}

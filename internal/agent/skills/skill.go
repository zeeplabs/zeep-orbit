// Package skills holds the builder's "skills": short markdown guides the agent
// loads on demand. A skill is a name, a one-line description (always visible
// to the model through the skills index) and a body (loaded by load_skill).
//
// The package owns parsing, validation, the index text and the tool. It never
// imports internal/dashboard: the dashboard implements Source against
// Postgres and registers the tool.
package skills

import (
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits are constants, not settings: nothing here is configurable per
// deployment, so there is no env var or settings column to document.
const (
	MaxNameLen                 = 64
	MaxDescriptionChars        = 1024
	MaxBuiltinDescriptionChars = 200
	MaxLicenseChars            = 100
	MaxSourceChars             = 200
	MaxBodyBytes               = 24 << 10
	MaxFrontmatterBytes        = 4 << 10
	// MaxContentBytes bounds the raw uploaded file (frontmatter + body).
	MaxContentBytes = 64 << 10

	maxWarningKeyChars = 64
)

// Field error codes. They are stable machine values: the API returns them in
// `fields[].code` and the UI maps them to localized text.
const (
	CodeNameInvalid          = "name_invalid"
	CodeNameTooLong          = "name_too_long"
	CodeDescriptionRequired  = "description_required"
	CodeDescriptionTooLong   = "description_too_long"
	CodeBodyEmpty            = "body_empty"
	CodeBodyTooLarge         = "body_too_large"
	CodeFrontmatterMissing   = "frontmatter_missing"
	CodeFrontmatterInvalid   = "frontmatter_invalid"
	CodeFrontmatterDuplicate = "frontmatter_duplicate_key"
	CodeFrontmatterTooLarge  = "frontmatter_too_large"
	CodeContentNotUTF8       = "content_not_utf8"
	CodeContentTooLarge      = "content_too_large"
	CodeCredentialsDetected  = "credentials_detected"
)

// WarningIgnoredKey is returned for every frontmatter key that is parsed but
// not used. Keys such as allowed-tools, hooks or model change agent behavior
// in other tools; here they are dropped on purpose.
const WarningIgnoredKey = "ignored_key"

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var keyRe = regexp.MustCompile(`^([A-Za-z0-9_-]+):(?:[ \t]+(.*))?$`)

// Parsed is the validated content of a skill file.
type Parsed struct {
	Name        string
	Description string // single line, already sanitized
	License     string
	Body        string
	// Version and Source are only set for built-ins (ParseBuiltin).
	Version int
	Source  string
}

// Warning reports something that was accepted but not used.
type Warning struct {
	Code string `json:"code"`
	Key  string `json:"key,omitempty"`
}

// FieldError is one validation failure tied to a field of the file.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// ValidationError carries every field failure found in one pass.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Code
	}
	return "invalid skill: " + strings.Join(parts, ", ")
}

// AsValidationError unwraps a *ValidationError from err.
func AsValidationError(err error) (*ValidationError, bool) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

// Parse validates a custom (uploaded) skill file. Only name, description and
// license are used; every other key, including version and source, is ignored
// with a warning.
func Parse(content []byte) (Parsed, []Warning, error) {
	return parse(content, false)
}

// ParseBuiltin validates an embedded built-in. It additionally requires an
// integer version and a source (provenance), and enforces the tighter
// built-in description limit.
func ParseBuiltin(content []byte) (Parsed, []Warning, error) {
	return parse(content, true)
}

// ValidateName returns "" for a valid skill name, otherwise a field error
// code. It does not apply the reserved-prefix rule: that depends on whether
// the caller is a built-in or a custom upload.
func ValidateName(name string) string {
	switch {
	case name == "":
		return CodeNameInvalid
	case len(name) > MaxNameLen:
		return CodeNameTooLong
	case !nameRe.MatchString(name):
		return CodeNameInvalid
	}
	return ""
}

// SanitizeDescription turns arbitrary text into a safe single line for the
// skills index. Every whitespace run (newlines included) collapses to one
// space, control and invisible format characters are dropped, and leading
// '#' and '-' runs are removed, so a description can never open a prompt
// section or a list item of its own.
func SanitizeDescription(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// dropped
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return strings.TrimLeft(b.String(), "#- ")
}

type fmBlock struct {
	key  string
	rest string
	cont []string
}

func parse(content []byte, builtin bool) (Parsed, []Warning, error) {
	if len(content) > MaxContentBytes {
		return Parsed{}, nil, fail("content", CodeContentTooLarge)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return Parsed{}, nil, fail("content", CodeContentNotUTF8)
	}

	text := strings.TrimPrefix(string(content), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	if strings.TrimRight(lines[0], " \t") != "---" {
		return Parsed{}, nil, fail("frontmatter", CodeFrontmatterMissing)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Parsed{}, nil, fail("frontmatter", CodeFrontmatterInvalid)
	}
	fmLines := lines[1:end]
	if len(strings.Join(fmLines, "\n")) > MaxFrontmatterBytes {
		return Parsed{}, nil, fail("frontmatter", CodeFrontmatterTooLarge)
	}

	blocks, err := splitBlocks(fmLines)
	if err != nil {
		return Parsed{}, nil, err
	}

	var (
		out      Parsed
		warnings []Warning
		ve       ValidationError
		seen     = map[string]bool{}
		values   = map[string]string{}
		invalid  = map[string]bool{}
	)
	known := map[string]bool{"name": true, "description": true, "license": true}
	if builtin {
		known["version"] = true
		known["source"] = true
	}

	for _, blk := range blocks {
		if seen[blk.key] {
			ve.Fields = append(ve.Fields, FieldError{Field: blk.key, Code: CodeFrontmatterDuplicate})
			continue
		}
		seen[blk.key] = true
		if !known[blk.key] {
			warnings = append(warnings, Warning{Code: WarningIgnoredKey, Key: warningKey(blk.key)})
			continue
		}
		v, ok := scalar(blk)
		if !ok {
			ve.Fields = append(ve.Fields, FieldError{Field: blk.key, Code: CodeFrontmatterInvalid})
			invalid[blk.key] = true
			continue
		}
		values[blk.key] = v
	}

	out.Name = values["name"]
	if code := ValidateName(out.Name); code != "" && !invalid["name"] {
		ve.Fields = append(ve.Fields, FieldError{Field: "name", Code: code})
	}

	descMax := MaxDescriptionChars
	if builtin {
		descMax = MaxBuiltinDescriptionChars
	}
	out.Description = SanitizeDescription(values["description"])
	switch {
	case invalid["description"]:
		// already reported as frontmatter_invalid
	case out.Description == "":
		ve.Fields = append(ve.Fields, FieldError{Field: "description", Code: CodeDescriptionRequired})
	case utf8.RuneCountInString(out.Description) > descMax:
		ve.Fields = append(ve.Fields, FieldError{Field: "description", Code: CodeDescriptionTooLong})
	}

	out.License = SanitizeDescription(values["license"])
	if utf8.RuneCountInString(out.License) > MaxLicenseChars {
		ve.Fields = append(ve.Fields, FieldError{Field: "license", Code: CodeFrontmatterInvalid})
	}

	if builtin {
		n, convErr := strconv.Atoi(values["version"])
		if (convErr != nil || n < 1) && !invalid["version"] {
			ve.Fields = append(ve.Fields, FieldError{Field: "version", Code: CodeFrontmatterInvalid})
		}
		out.Version = n
		out.Source = SanitizeDescription(values["source"])
		if (out.Source == "" || utf8.RuneCountInString(out.Source) > MaxSourceChars) && !invalid["source"] {
			ve.Fields = append(ve.Fields, FieldError{Field: "source", Code: CodeFrontmatterInvalid})
		}
	}

	out.Body = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	switch {
	case out.Body == "":
		ve.Fields = append(ve.Fields, FieldError{Field: "body", Code: CodeBodyEmpty})
	case len(out.Body) > MaxBodyBytes:
		ve.Fields = append(ve.Fields, FieldError{Field: "body", Code: CodeBodyTooLarge})
	}

	if len(ve.Fields) > 0 {
		return Parsed{}, warnings, &ve
	}
	return out, warnings, nil
}

func fail(field, code string) error {
	return &ValidationError{Fields: []FieldError{{Field: field, Code: code}}}
}

// splitBlocks groups frontmatter lines into top-level `key: value` blocks.
// Indented and blank lines belong to the block above them (nested maps, lists
// and block scalars); comment lines at column 0 are skipped.
func splitBlocks(lines []string) ([]fmBlock, error) {
	var blocks []fmBlock
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			if n := len(blocks); n > 0 {
				blocks[n-1].cont = append(blocks[n-1].cont, "")
			}
		case line[0] == ' ' || line[0] == '\t':
			if len(blocks) == 0 {
				return nil, fail("frontmatter", CodeFrontmatterInvalid)
			}
			n := len(blocks) - 1
			blocks[n].cont = append(blocks[n].cont, trimmed)
		case line[0] == '#':
			// comment
		default:
			m := keyRe.FindStringSubmatch(strings.TrimRight(line, " \t"))
			if m == nil {
				return nil, fail("frontmatter", CodeFrontmatterInvalid)
			}
			blocks = append(blocks, fmBlock{key: m[1], rest: strings.TrimSpace(m[2])})
		}
	}
	return blocks, nil
}

// scalar resolves the value of a known key: plain, single or double quoted,
// block scalar (| or >, folded to one line) or a plain multi-line value.
// Anchors, aliases and tags are rejected so no YAML feature beyond text is
// ever interpreted.
func scalar(b fmBlock) (string, bool) {
	join := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, " ")
	}
	rest := b.rest

	switch {
	case rest == "":
		return join(b.cont...), true
	case rest[0] == '|' || rest[0] == '>':
		if len(rest) > 2 || (len(rest) == 2 && rest[1] != '+' && rest[1] != '-') {
			return "", false
		}
		return join(b.cont...), true
	case rest[0] == '&' || rest[0] == '*' || rest[0] == '!':
		return "", false
	case rest[0] == '"' || rest[0] == '\'':
		if len(join(b.cont...)) > 0 {
			return "", false
		}
		return quoted(rest)
	}
	return join(append([]string{stripComment(rest)}, b.cont...)...), true
}

// stripComment drops a trailing ` # comment` from a plain scalar, as YAML does.
func stripComment(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// quoted decodes a single- or double-quoted scalar that closes on the same
// line. Only a trailing ` # comment` may follow the closing quote.
func quoted(s string) (string, bool) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '\'' && c == '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			return b.String(), trailingOK(s[i+1:])
		case q == '"' && c == '"':
			return b.String(), trailingOK(s[i+1:])
		case q == '"' && c == '\\':
			i++
			if i >= len(s) {
				return "", false
			}
			switch s[i] {
			case '"', '\\':
				b.WriteByte(s[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return "", false
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", false
}

func trailingOK(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

// warningKey caps a key echoed back in a warning. Keys already match
// [A-Za-z0-9_-]+, so they are printable ASCII.
func warningKey(k string) string {
	if len(k) > maxWarningKeyChars {
		return k[:maxWarningKeyChars]
	}
	return k
}

// Origin says where a skill comes from.
type Origin string

const (
	OriginBuiltin Origin = "built-in"
	OriginCustom  Origin = "custom"
)

// Entry is one row of the skills index: enough to advertise a skill to the
// model, never its body.
type Entry struct {
	Name        string
	Description string
	Source      Origin
	Version     int
	Enabled     bool
}

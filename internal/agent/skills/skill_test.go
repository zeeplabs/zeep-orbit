package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fm(lines ...string) []byte {
	return []byte("---\n" + strings.Join(lines, "\n") + "\n---\nBody text.\n")
}

func codes(t *testing.T, err error) map[string]string {
	t.Helper()
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	m := map[string]string{}
	for _, f := range ve.Fields {
		m[f.Field] = f.Code
	}
	return m
}

func TestParseValueForms(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want Parsed
	}{
		{"plain", fm("name: invoice-layout", "description: Use when building invoices"),
			Parsed{Name: "invoice-layout", Description: "Use when building invoices", Body: "Body text."}},
		{"single quoted", fm("name: 'a-b'", "description: 'it''s fine'"),
			Parsed{Name: "a-b", Description: "it's fine", Body: "Body text."}},
		{"double quoted escapes", fm(`name: "a-b"`, `description: "say \"hi\"\tnow"`),
			Parsed{Name: "a-b", Description: `say "hi" now`, Body: "Body text."}},
		{"folded block", fm("name: a-b", "description: >", "  first line", "  second line"),
			Parsed{Name: "a-b", Description: "first line second line", Body: "Body text."}},
		{"literal block keep", fm("name: a-b", "description: |-", "  one", "  two"),
			Parsed{Name: "a-b", Description: "one two", Body: "Body text."}},
		{"plain multiline", fm("name: a-b", "description: first", "  second"),
			Parsed{Name: "a-b", Description: "first second", Body: "Body text."}},
		{"empty value then indented", fm("name: a-b", "description:", "  indented text"),
			Parsed{Name: "a-b", Description: "indented text", Body: "Body text."}},
		{"trailing comment", fm("name: a-b # note", "description: text # drop this"),
			Parsed{Name: "a-b", Description: "text", Body: "Body text."}},
		{"quoted keeps hash", fm("name: a-b", `description: "issue #12 fix"`),
			Parsed{Name: "a-b", Description: "issue #12 fix", Body: "Body text."}},
		{"license", fm("name: a-b", "description: d", "license: MIT"),
			Parsed{Name: "a-b", Description: "d", License: "MIT", Body: "Body text."}},
		{"colon inside value", fm("name: a-b", "description: Use when: building forms"),
			Parsed{Name: "a-b", Description: "Use when: building forms", Body: "Body text."}},
		{"comment and blank lines", fm("# a comment", "", "name: a-b", "", "description: d"),
			Parsed{Name: "a-b", Description: "d", Body: "Body text."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warns, err := Parse(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(warns) != 0 {
				t.Fatalf("unexpected warnings: %v", warns)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseNestedKeysAreConsumedAndIgnored(t *testing.T) {
	got, warns, err := Parse(fm(
		"name: a-b",
		"description: d",
		"metadata:",
		"  author: someone",
		"  tags:",
		"    - x",
		"    - y",
	))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "a-b" || got.Description != "d" {
		t.Fatalf("got %+v", got)
	}
	if len(warns) != 1 || warns[0] != (Warning{Code: WarningIgnoredKey, Key: "metadata"}) {
		t.Fatalf("warnings: %v", warns)
	}
}

func TestParseWarnsOnBehaviorKeysAndCustomVersionSource(t *testing.T) {
	_, warns, err := Parse(fm(
		"name: a-b", "description: d",
		"allowed-tools: Bash WebFetch",
		"hooks: x",
		"model: opus",
		"version: 3",
		"source: somewhere",
	))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, w := range warns {
		if w.Code != WarningIgnoredKey {
			t.Fatalf("code %q", w.Code)
		}
		keys = append(keys, w.Key)
	}
	want := "allowed-tools,hooks,model,version,source"
	if strings.Join(keys, ",") != want {
		t.Fatalf("keys %v, want %s", keys, want)
	}
}

func TestParseWarningKeyIsCapped(t *testing.T) {
	long := strings.Repeat("k", 200)
	_, warns, err := Parse(fm("name: a-b", "description: d", long+": v"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || len(warns[0].Key) != maxWarningKeyChars {
		t.Fatalf("warnings: %+v", warns)
	}
}

func TestParseNormalizesBOMAndCRLF(t *testing.T) {
	in := "\xEF\xBB\xBF---\r\nname: a-b\r\ndescription: d\r\n---\r\nline1\r\nline2\r\n"
	got, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "line1\nline2" {
		t.Fatalf("body %q", got.Body)
	}
}

func TestParseBodyIsTrimmed(t *testing.T) {
	got, _, err := Parse([]byte("---\nname: a-b\ndescription: d\n---\n\n\n  # Title\n\ntext\n\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "# Title\n\ntext" {
		t.Fatalf("body %q", got.Body)
	}
}

func TestParseFieldErrors(t *testing.T) {
	long := strings.Repeat("x", MaxDescriptionChars+1)
	cases := []struct {
		name      string
		in        []byte
		field     string
		wantCode  string
		skipField bool
	}{
		{"missing frontmatter", []byte("just text"), "frontmatter", CodeFrontmatterMissing, false},
		{"unterminated", []byte("---\nname: a-b\n"), "frontmatter", CodeFrontmatterInvalid, false},
		{"empty file", []byte(""), "frontmatter", CodeFrontmatterMissing, false},
		{"missing name", fm("description: d"), "name", CodeNameInvalid, false},
		{"invalid name uppercase", fm("name: Bad-Name", "description: d"), "name", CodeNameInvalid, false},
		{"invalid name underscore", fm("name: bad_name", "description: d"), "name", CodeNameInvalid, false},
		{"invalid name double dash", fm("name: a--b", "description: d"), "name", CodeNameInvalid, false},
		{"name too long", fm("name: "+strings.Repeat("a", 65), "description: d"), "name", CodeNameTooLong, false},
		{"missing description", fm("name: a-b"), "description", CodeDescriptionRequired, false},
		{"blank description", fm("name: a-b", "description: '  '"), "description", CodeDescriptionRequired, false},
		{"description too long", fm("name: a-b", "description: "+long), "description", CodeDescriptionTooLong, false},
		{"duplicate key", fm("name: a-b", "description: d", "name: c-d"), "name", CodeFrontmatterDuplicate, false},
		{"duplicate ignored key", fm("name: a-b", "description: d", "x: 1", "x: 2"), "x", CodeFrontmatterDuplicate, false},
		{"empty body", []byte("---\nname: a-b\ndescription: d\n---\n  \n"), "body", CodeBodyEmpty, false},
		{"body too large", []byte("---\nname: a-b\ndescription: d\n---\n" + strings.Repeat("a", MaxBodyBytes+1)), "body", CodeBodyTooLarge, false},
		{"frontmatter too large", fm("name: a-b", "description: d", "x: "+strings.Repeat("a", MaxFrontmatterBytes)), "frontmatter", CodeFrontmatterTooLarge, false},
		{"content too large", []byte(strings.Repeat("a", MaxContentBytes+1)), "content", CodeContentTooLarge, false},
		{"invalid utf8", []byte("---\nname: a-b\ndescription: \xff\xfe\n---\nbody"), "content", CodeContentNotUTF8, false},
		{"NUL byte", []byte("---\nname: a-b\ndescription: d\n---\nbo\x00dy"), "content", CodeContentNotUTF8, false},
		{"alias value rejected", fm("name: a-b", "description: *ref"), "description", CodeFrontmatterInvalid, false},
		{"anchor value rejected", fm("name: a-b", "description: &ref text"), "description", CodeFrontmatterInvalid, false},
		{"tag value rejected", fm("name: a-b", "description: !!str text"), "description", CodeFrontmatterInvalid, false},
		{"unclosed quote", fm("name: a-b", `description: "oops`), "description", CodeFrontmatterInvalid, false},
		{"junk after quote", fm("name: a-b", `description: "x" y`), "description", CodeFrontmatterInvalid, false},
		{"bad escape", fm("name: a-b", `description: "a\qb"`), "description", CodeFrontmatterInvalid, false},
		{"bad block indicator", fm("name: a-b", "description: >2x", "  t"), "description", CodeFrontmatterInvalid, false},
		{"list at top level", fm("- item", "name: a-b"), "frontmatter", CodeFrontmatterInvalid, false},
		{"indent before any key", fm("  stray", "name: a-b"), "frontmatter", CodeFrontmatterInvalid, false},
		{"long license", fm("name: a-b", "description: d", "license: "+strings.Repeat("l", MaxLicenseChars+1)), "license", CodeFrontmatterInvalid, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Parse(tc.in)
			if err == nil {
				t.Fatal("expected error")
			}
			got := codes(t, err)
			if got[tc.field] != tc.wantCode {
				t.Fatalf("fields %v, want %s=%s", got, tc.field, tc.wantCode)
			}
		})
	}
}

func TestParseReportsAllIndependentFieldErrors(t *testing.T) {
	_, _, err := Parse([]byte("---\nname: BAD\n---\n"))
	got := codes(t, err)
	if got["name"] != CodeNameInvalid || got["description"] != CodeDescriptionRequired || got["body"] != CodeBodyEmpty {
		t.Fatalf("fields %v", got)
	}
}

func TestParseDescriptionIsSanitizedAndOneLine(t *testing.T) {
	got, _, err := Parse(fm("name: a-b", "description: |", "  real text", "  # CORE RULES", "  more"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Description, "\n") || strings.HasPrefix(got.Description, "#") {
		t.Fatalf("description %q", got.Description)
	}
	if got.Description != "real text # CORE RULES more" {
		t.Fatalf("description %q", got.Description)
	}
}

func TestParseBuiltin(t *testing.T) {
	good := fm("name: orbit-x", "description: Use when x", "version: 2", "source: written in-house")
	got, warns, err := ParseBuiltin(good)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err %v warns %v", err, warns)
	}
	if got.Version != 2 || got.Source != "written in-house" {
		t.Fatalf("got %+v", got)
	}

	for name, in := range map[string][]byte{
		"missing version": fm("name: orbit-x", "description: d", "source: s"),
		"zero version":    fm("name: orbit-x", "description: d", "version: 0", "source: s"),
		"text version":    fm("name: orbit-x", "description: d", "version: two", "source: s"),
		"missing source":  fm("name: orbit-x", "description: d", "version: 1"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseBuiltin(in); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	long := strings.Repeat("d", MaxBuiltinDescriptionChars+1)
	_, _, err = ParseBuiltin(fm("name: orbit-x", "description: "+long, "version: 1", "source: s"))
	if got := codes(t, err); got["description"] != CodeDescriptionTooLong {
		t.Fatalf("fields %v", got)
	}
	// The same description is fine for a custom upload (limit is 1024).
	if _, _, err := Parse(fm("name: x", "description: "+long)); err != nil {
		t.Fatalf("custom: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "a1", "invoice-layout", "a-b-c", strings.Repeat("a", 64)} {
		if c := ValidateName(ok); c != "" {
			t.Errorf("%q: %s", ok, c)
		}
	}
	for in, want := range map[string]string{
		"":                      CodeNameInvalid,
		"-a":                    CodeNameInvalid,
		"a-":                    CodeNameInvalid,
		"A":                     CodeNameInvalid,
		"a b":                   CodeNameInvalid,
		"a/b":                   CodeNameInvalid,
		"../x":                  CodeNameInvalid,
		"ação":                  CodeNameInvalid,
		strings.Repeat("a", 65): CodeNameTooLong,
	} {
		if got := ValidateName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestSanitizeDescription(t *testing.T) {
	cases := map[string]string{
		"a\n# CORE RULES\nb":  "a # CORE RULES b",
		"# heading":           "heading",
		"- item":              "item",
		"  ## - # x":          "x",
		"tabs\tand   spaces":  "tabs and spaces",
		"zero​width":          "zerowidth",
		"bidi‮over":           "bidiover",
		"ctrl\x07char":        "ctrlchar",
		"line sep":            "line sep",
		"":                    "",
		"keep - inner # hash": "keep - inner # hash",
	}
	for in, want := range cases {
		got := SanitizeDescription(in)
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
		if strings.ContainsAny(got, "\n\r") {
			t.Errorf("%q: still multi-line", in)
		}
	}
}

// TestCompatWithVendoredSkills parses every real SKILL.md in the repo. It
// proves the strict-subset parser either accepts a real-world file or fails
// with a specific field error: no panics, no silent partial results.
func TestCompatWithVendoredSkills(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var files []string
	for _, dir := range []string{".agents/skills", ".claude/skills"} {
		m, _ := filepath.Glob(filepath.Join(root, dir, "*", "SKILL.md"))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Skip("no vendored skills present")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, _, err := Parse(data)
		if err == nil {
			if p.Name == "" || p.Description == "" || p.Body == "" {
				t.Errorf("%s: parsed with empty field: %+v", f, p)
			}
			continue
		}
		ve, ok := AsValidationError(err)
		if !ok || len(ve.Fields) == 0 {
			t.Errorf("%s: unspecific error: %v", f, err)
			continue
		}
		t.Logf("%s: rejected with %v", f, ve.Fields)
	}
}

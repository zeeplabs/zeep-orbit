package skills

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// indexIntro opens the index text. The section header itself is added by the
// prompt builder (headerSkills), like every other prompt section.
const indexIntro = "Load a skill with load_skill(name) BEFORE writing code when its description matches the task. Do not load skills that do not match."

// RenderIndex renders the enabled entries as the skills index text: the intro
// sentence and one "- name: description" line per skill, built-ins first and
// then custom skills, each group by name. Descriptions are sanitized again so
// a caller that skipped Parse still cannot inject a multi-line section.
//
// maxChars bounds the text in characters (runes). Whole entries are dropped,
// never cut, and once one entry does not fit every later one is dropped too,
// so the visible set is a stable prefix. The dropped names are returned in
// index order. With no enabled entries, or when not even the first one fits,
// the text is empty.
func RenderIndex(entries []Entry, maxChars int) (text string, dropped []string) {
	sorted := sortedEnabled(entries)
	if len(sorted) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString(indexIntro)
	used := utf8.RuneCountInString(indexIntro)
	kept := 0
	for i, e := range sorted {
		line := indexLine(e)
		n := 1 + utf8.RuneCountInString(line) // leading newline
		if used+n > maxChars {
			for _, d := range sorted[i:] {
				dropped = append(dropped, d.Name)
			}
			break
		}
		b.WriteByte('\n')
		b.WriteString(line)
		used += n
		kept++
	}
	if kept == 0 {
		return "", dropped
	}
	return b.String(), dropped
}

// IndexChars is the size of the index text for entries with an unlimited
// budget, intro included (0 when nothing is enabled). It is the single
// measure behind both the runtime budget and the upload/enable checks, so
// the two can never disagree.
func IndexChars(entries []Entry) int {
	text, _ := RenderIndex(entries, int(^uint(0)>>1))
	return utf8.RuneCountInString(text)
}

// EntryChars is how many characters one entry adds to the index (its line and
// the newline before it). It lets a UI show each skill's share of the budget.
func EntryChars(e Entry) int {
	return 1 + utf8.RuneCountInString(indexLine(e))
}

func indexLine(e Entry) string {
	return "- " + e.Name + ": " + SanitizeDescription(e.Description)
}

func sortedEnabled(entries []Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Enabled {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := out[i].Source == OriginBuiltin, out[j].Source == OriginBuiltin
		if bi != bj {
			return bi
		}
		return out[i].Name < out[j].Name
	})
	return out
}

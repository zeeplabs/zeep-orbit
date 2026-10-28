package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

var updateLock = flag.Bool("update-lock", false, "rewrite builtin/builtin.lock from the current built-ins")

var (
	urlRe       = regexp.MustCompile(`(?i)https?://`)
	shellRe     = regexp.MustCompile(`(?i)\b(npx|curl|wget|sudo|bash)\b|pip install`)
	foreignBare = regexp.MustCompile(`\b(WebFetch|WebSearch|Grep|Glob|TodoWrite)\b`)
	foreignTick = regexp.MustCompile("`(Task|Read|Write|Edit)`")
	toolCallRe  = regexp.MustCompile("`([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\\(")
)

type lockEntry struct {
	version int
	sha     string
}

func parseLock(raw []byte) (map[string]lockEntry, error) {
	out := map[string]lockEntry{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, fmt.Errorf("builtin.lock line %d: want 'name version sha256'", i+1)
		}
		v, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("builtin.lock line %d: bad version", i+1)
		}
		out[f[0]] = lockEntry{version: v, sha: f[2]}
	}
	return out, nil
}

func formatLock(skills []Skill) []byte {
	var b strings.Builder
	b.WriteString("# name version sha256-of-SKILL.md\n")
	b.WriteString("# Regenerate after editing a built-in (and bumping its `version`):\n")
	b.WriteString("#   go test ./internal/agent/skills -run TestBuiltinLint -update-lock\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "%s %d %s\n", s.Name, s.Version, s.SHA256)
	}
	return []byte(b.String())
}

// lintBuiltins checks every built-in under fsys (root = the builtin dir) and
// returns one message per violation. It takes an fs.FS so the rules are
// proven against fixtures before any real built-in exists.
func lintBuiltins(fsys fs.FS) []string {
	var v []string
	add := func(name, format string, args ...any) {
		v = append(v, name+": "+fmt.Sprintf(format, args...))
	}

	dirs, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return []string{"read builtin dir: " + err.Error()}
	}

	var skills []Skill
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(fsys, d.Name()+"/SKILL.md")
		if err != nil {
			add(d.Name(), "missing SKILL.md")
			continue
		}
		p, _, err := ParseBuiltin(raw)
		if err != nil {
			if ve, ok := AsValidationError(err); ok {
				for _, f := range ve.Fields {
					add(d.Name(), "parse: %s %s", f.Field, f.Code)
				}
			} else {
				add(d.Name(), "parse: %v", err)
			}
			continue
		}
		sum := sha256.Sum256(raw)
		sk := Skill{Parsed: p, Origin: OriginBuiltin, SHA256: hex.EncodeToString(sum[:])}
		skills = append(skills, sk)

		if !HasReservedPrefix(p.Name) {
			add(p.Name, "name must start with %q", ReservedPrefix)
		}
		if p.Name != d.Name() {
			add(p.Name, "name does not match directory %q", d.Name())
		}

		text := p.Description + "\n" + p.License + "\n" + p.Body
		if urlRe.MatchString(text) {
			add(p.Name, "contains an http(s) link (only the source field may cite one)")
		}
		if m := shellRe.FindString(text); m != "" {
			add(p.Name, "mentions shell/network tooling %q", m)
		}
		if m := foreignBare.FindString(text); m != "" {
			add(p.Name, "mentions foreign agent tool %q", m)
		}
		if m := foreignTick.FindString(text); m != "" {
			add(p.Name, "mentions foreign agent tool %s", m)
		}
		for _, m := range toolCallRe.FindAllStringSubmatch(text, -1) {
			if !KnownToolName(m[1]) {
				add(p.Name, "calls unknown tool %q", m[1])
			}
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })

	entries := make([]Entry, len(skills))
	for i, s := range skills {
		entries[i] = s.Entry(true)
	}
	if n := IndexChars(entries); n > MaxBuiltinIndexChars {
		add("index", "built-in index is %d chars, max %d", n, MaxBuiltinIndexChars)
	}

	rawLock, err := fs.ReadFile(fsys, "builtin.lock")
	if err != nil {
		return append(v, "builtin.lock: missing")
	}
	lock, err := parseLock(rawLock)
	if err != nil {
		return append(v, err.Error())
	}
	seen := map[string]bool{}
	for _, s := range skills {
		seen[s.Name] = true
		e, ok := lock[s.Name]
		switch {
		case !ok:
			add(s.Name, "not in builtin.lock (run the test with -update-lock)")
		case s.Version < e.version:
			add(s.Name, "version %d is lower than the locked %d", s.Version, e.version)
		case s.Version == e.version && s.SHA256 != e.sha:
			add(s.Name, "content changed without a version bump")
		case s.Version > e.version || s.SHA256 != e.sha:
			add(s.Name, "builtin.lock is stale (run the test with -update-lock)")
		}
	}
	for name := range lock {
		if !seen[name] {
			add(name, "listed in builtin.lock but no such built-in")
		}
	}
	sort.Strings(v)
	return v
}

func skillFile(name string, version int, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\nversion: %d\nsource: written in-house\n---\n%s\n",
		name, description, version, body)
}

// fixtureFS builds a builtin dir with the given skills and a matching lock.
func fixtureFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{"NOTICE.md": {Data: []byte("notice")}}
	var skills []Skill
	for name, content := range files {
		fsys[name+"/SKILL.md"] = &fstest.MapFile{Data: []byte(content)}
		p, _, err := ParseBuiltin([]byte(content))
		if err != nil {
			continue
		}
		sum := sha256.Sum256([]byte(content))
		skills = append(skills, Skill{Parsed: p, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	fsys["builtin.lock"] = &fstest.MapFile{Data: formatLock(skills)}
	return fsys
}

func cleanSkill() string {
	return skillFile("orbit-clean", 1, "Use when testing the lint", "Call `read_file(path)` then `run_build()`.\n\nKeep it short.")
}

func mustContain(t *testing.T, violations []string, sub string) {
	t.Helper()
	for _, v := range violations {
		if strings.Contains(v, sub) {
			return
		}
	}
	t.Fatalf("no violation containing %q in %q", sub, violations)
}

func TestLintZeroBuiltinsIsValid(t *testing.T) {
	fsys := fixtureFS(nil)
	if v := lintBuiltins(fsys); len(v) != 0 {
		t.Fatalf("violations: %v", v)
	}
	list, err := loadBuiltins(fsys)
	if err != nil || len(list) != 0 {
		t.Fatalf("list %v err %v", list, err)
	}
}

func TestLintCleanFixturePasses(t *testing.T) {
	if v := lintBuiltins(fixtureFS(map[string]string{"orbit-clean": cleanSkill()})); len(v) != 0 {
		t.Fatalf("violations: %v", v)
	}
}

func TestLintFailures(t *testing.T) {
	cases := []struct {
		name string
		file string
		dir  string
		want string
	}{
		{"missing prefix", skillFile("plain-name", 1, "Use when x", "body"), "plain-name", `must start with "orbit-"`},
		{"dir mismatch", skillFile("orbit-a", 1, "Use when x", "body"), "orbit-b", "does not match directory"},
		{"missing version", "---\nname: orbit-a\ndescription: Use when x\nsource: s\n---\nbody\n", "orbit-a", "version"},
		{"missing source", "---\nname: orbit-a\ndescription: Use when x\nversion: 1\n---\nbody\n", "orbit-a", "source"},
		{"description too long", skillFile("orbit-a", 1, strings.Repeat("d", MaxBuiltinDescriptionChars+1), "body"), "orbit-a", "description_too_long"},
		{"body too large", skillFile("orbit-a", 1, "Use when x", strings.Repeat("b", MaxBodyBytes+1)), "orbit-a", "body_too_large"},
		{"https link", skillFile("orbit-a", 1, "Use when x", "See https://example.com/doc for more."), "orbit-a", "http(s) link"},
		{"npx", skillFile("orbit-a", 1, "Use when x", "Run npx something."), "orbit-a", `"npx"`},
		{"curl", skillFile("orbit-a", 1, "Use when x", "Then curl the endpoint."), "orbit-a", `"curl"`},
		{"sudo", skillFile("orbit-a", 1, "Use when x", "Use sudo here."), "orbit-a", `"sudo"`},
		{"pip install", skillFile("orbit-a", 1, "Use when x", "Do pip install x."), "orbit-a", "pip install"},
		{"bash fence", skillFile("orbit-a", 1, "Use when x", "```bash\nls\n```"), "orbit-a", `"bash"`},
		{"foreign WebFetch", skillFile("orbit-a", 1, "Use when x", "Use WebFetch to read it."), "orbit-a", "WebFetch"},
		{"foreign Bash tool", skillFile("orbit-a", 1, "Use when x", "Use the `Bash` tool."), "orbit-a", "Bash"},
		{"foreign Task tool", skillFile("orbit-a", 1, "Use when x", "Spawn a `Task` for it."), "orbit-a", "`Task`"},
		{"unknown tool call", skillFile("orbit-a", 1, "Use when x", "Call `fetch_page(url)` first."), "orbit-a", `unknown tool "fetch_page"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := lintBuiltins(fixtureFS(map[string]string{tc.dir: tc.file}))
			mustContain(t, v, tc.want)
		})
	}
}

func TestLintAllowsProseThatLooksLikeForeignTools(t *testing.T) {
	// "Task" at the start of a sentence and plain prose are fine; only the
	// backticked tool form and unambiguous tool names are flagged.
	body := "Task lists need a checkbox. Read the file, then Edit it by hand. Use `edit_file(path)`."
	if v := lintBuiltins(fixtureFS(map[string]string{"orbit-a": skillFile("orbit-a", 1, "Use when x", body)})); len(v) != 0 {
		t.Fatalf("violations: %v", v)
	}
}

func TestLintIndexBudget(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("orbit-skill-%02d", i)
		files[name] = skillFile(name, 1, strings.Repeat("d", 190), "body")
	}
	mustContain(t, lintBuiltins(fixtureFS(files)), "built-in index is")
}

func TestLintLock(t *testing.T) {
	good := cleanSkill()
	base := fixtureFS(map[string]string{"orbit-clean": good})

	t.Run("content changed without version bump", func(t *testing.T) {
		fsys := fixtureFS(map[string]string{"orbit-clean": good})
		fsys["orbit-clean/SKILL.md"] = &fstest.MapFile{Data: []byte(strings.Replace(good, "Keep it short.", "Keep it tiny.", 1))}
		mustContain(t, lintBuiltins(fsys), "content changed without a version bump")
	})
	t.Run("stale after bump", func(t *testing.T) {
		fsys := fixtureFS(map[string]string{"orbit-clean": good})
		fsys["orbit-clean/SKILL.md"] = &fstest.MapFile{Data: []byte(skillFile("orbit-clean", 2, "Use when testing the lint", "Changed."))}
		mustContain(t, lintBuiltins(fsys), "builtin.lock is stale")
	})
	t.Run("version went backwards", func(t *testing.T) {
		fsys := fixtureFS(map[string]string{"orbit-clean": skillFile("orbit-clean", 3, "Use when x", "b")})
		fsys["orbit-clean/SKILL.md"] = &fstest.MapFile{Data: []byte(skillFile("orbit-clean", 2, "Use when x", "b"))}
		mustContain(t, lintBuiltins(fsys), "lower than the locked")
	})
	t.Run("not in lock", func(t *testing.T) {
		fsys := fixtureFS(map[string]string{"orbit-clean": good})
		fsys["builtin.lock"] = &fstest.MapFile{Data: []byte("# empty\n")}
		mustContain(t, lintBuiltins(fsys), "not in builtin.lock")
	})
	t.Run("lock lists unknown skill", func(t *testing.T) {
		fsys := fixtureFS(map[string]string{"orbit-clean": good})
		fsys["builtin.lock"] = &fstest.MapFile{Data: append(fsys["builtin.lock"].Data, []byte("orbit-ghost 1 abc\n")...)}
		mustContain(t, lintBuiltins(fsys), "no such built-in")
	})
	t.Run("missing lock file", func(t *testing.T) {
		delete(base, "builtin.lock")
		mustContain(t, lintBuiltins(base), "builtin.lock: missing")
	})
	t.Run("malformed lock", func(t *testing.T) {
		fsys := fixtureFS(nil)
		fsys["builtin.lock"] = &fstest.MapFile{Data: []byte("only-two fields\n")}
		mustContain(t, lintBuiltins(fsys), "builtin.lock line 1")
	})
}

func TestKnownToolName(t *testing.T) {
	for _, ok := range []string{"read_file", "write_file", "edit_file", "list_dir", "run_build", "get_build_logs",
		"propose_backend_change", "load_skill", "orbit_get_app_schema", "orbit_list_auth_providers"} {
		if !KnownToolName(ok) {
			t.Errorf("%s should be known", ok)
		}
	}
	for _, bad := range []string{"fetch_page", "bash", "run_shell", "readfile"} {
		if KnownToolName(bad) {
			t.Errorf("%s should be unknown", bad)
		}
	}
}

func TestLoadBuiltinsSortedAndRejectsBroken(t *testing.T) {
	fsys := fixtureFS(map[string]string{
		"orbit-b": skillFile("orbit-b", 1, "Use when b", "body"),
		"orbit-a": skillFile("orbit-a", 1, "Use when a", "body"),
	})
	list, err := loadBuiltins(fsys)
	if err != nil || len(list) != 2 || list[0].Name != "orbit-a" || list[1].Name != "orbit-b" {
		t.Fatalf("list %v err %v", list, err)
	}
	if list[0].SHA256 == "" || list[0].Origin != OriginBuiltin {
		t.Fatalf("skill %+v", list[0])
	}

	broken := fstest.MapFS{"orbit-x/SKILL.md": {Data: []byte("not a skill")}}
	if _, err := loadBuiltins(broken); err == nil {
		t.Fatal("expected error for broken built-in")
	}
	noFile := fstest.MapFS{"orbit-x/other.md": {Data: []byte("x")}}
	if _, err := loadBuiltins(noFile); err == nil {
		t.Fatal("expected error for missing SKILL.md")
	}
}

// TestBuiltinLint lints the built-ins embedded in the binary. With
// -update-lock it first rewrites builtin/builtin.lock, but never accepts a
// content change that did not bump the version.
func TestBuiltinLint(t *testing.T) {
	sub, err := fs.Sub(builtinFS, "builtin")
	if err != nil {
		t.Fatal(err)
	}
	if *updateLock {
		list, err := loadBuiltins(sub)
		if err != nil {
			t.Fatal(err)
		}
		old := map[string]lockEntry{}
		if raw, err := fs.ReadFile(sub, "builtin.lock"); err == nil {
			if old, err = parseLock(raw); err != nil {
				t.Fatal(err)
			}
		}
		for _, s := range list {
			if e, ok := old[s.Name]; ok && e.version == s.Version && e.sha != s.SHA256 {
				t.Fatalf("%s: content changed without a version bump", s.Name)
			}
		}
		if err := os.WriteFile("builtin/builtin.lock", formatLock(list), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("builtin/builtin.lock updated; re-run without -update-lock (the embedded copy is rebuilt)")
		return
	}
	if v := lintBuiltins(sub); len(v) != 0 {
		t.Fatalf("built-in lint failed:\n%s", strings.Join(v, "\n"))
	}
	a, b := Builtins(), Builtins()
	if len(a) != len(b) {
		t.Fatal("Builtins not stable")
	}
	for i := 1; i < len(a); i++ {
		if a[i-1].Name >= a[i].Name {
			t.Fatalf("Builtins not sorted: %s >= %s", a[i-1].Name, a[i].Name)
		}
	}
}

package dashboard

// builder_skills_store_test.go: integration tests (real Postgres through
// TEST_DATABASE_URL) for the skills store (ai-builder-skills S06).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/skills"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func builderSkillsTestPool(t *testing.T) (*db.Pool, string) {
	t.Helper()
	pool, _ := builderSettingsTestPool(t)
	// Start from an empty built-in set so budget arithmetic does not depend on
	// the shipped skills; tests that need built-ins call withBuiltinSkills.
	withBuiltinSkills(t)
	truncate := func() {
		_, _ = pool.Exec(context.Background(),
			`TRUNCATE zeep_system.builder_skills, zeep_system.builder_skill_versions, zeep_system.builder_skill_builtin_state CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)
	return pool, mustCreateSuperadmin(t, pool).ID
}

// withBuiltinSkills swaps the embedded set for fixtures (the real one is empty
// until the curated skills exist).
func withBuiltinSkills(t *testing.T, list ...skills.Skill) {
	t.Helper()
	old := builderBuiltinSkills
	builderBuiltinSkills = func() []skills.Skill { return list }
	t.Cleanup(func() { builderBuiltinSkills = old })
}

func fixtureBuiltin(name, desc string) skills.Skill {
	return skills.Skill{
		Parsed: skills.Parsed{Name: name, Description: desc, Body: "BUILTIN-BODY-" + name, Version: 2, Source: "written in-house"},
		Origin: skills.OriginBuiltin,
	}
}

func parsedSkill(name, desc, body string) skills.Parsed {
	return skills.Parsed{Name: name, Description: desc, Body: body}
}

func mustSave(t *testing.T, pool *db.Pool, author string, p skills.Parsed) *BuilderSkillSaveResult {
	t.Helper()
	r, err := SaveBuilderSkill(context.Background(), pool, author, p)
	if err != nil {
		t.Fatalf("save %s: %v", p.Name, err)
	}
	return r
}

func rowCount(t *testing.T, pool *db.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.`+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func indexChars(t *testing.T, pool *db.Pool) int {
	t.Helper()
	_, info, err := ListBuilderSkills(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	return info.Chars
}

func TestBuilderSkillsStore_VersionLifecycle(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	ctx := context.Background()

	r := mustSave(t, pool, user, parsedSkill("invoice-layout", "Use when building invoices", "v1 body"))
	if !r.Created || r.Version != 1 || !r.NewLineage {
		t.Fatalf("v1: %+v", r)
	}

	r = mustSave(t, pool, user, parsedSkill("invoice-layout", "Use when building invoices", "v2 body"))
	if !r.Created || r.Version != 2 || r.NewLineage {
		t.Fatalf("v2: %+v", r)
	}

	r = mustSave(t, pool, user, parsedSkill("invoice-layout", "Use when building invoices", "v2 body"))
	if r.Created || r.Version != 2 {
		t.Fatalf("unchanged content must be a no-op: %+v", r)
	}
	if n := rowCount(t, pool, "builder_skill_versions"); n != 2 {
		t.Fatalf("version rows = %d, want 2", n)
	}

	src := NewBuilderSkillSource(pool)
	got, err := src.Get(ctx, "invoice-layout")
	if err != nil || got.Version != 2 || got.Body != "v2 body" || got.Source != skills.OriginCustom {
		t.Fatalf("Get: %+v %v", got, err)
	}

	if err := DeleteBuilderSkill(ctx, pool, user, "invoice-layout"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Get(ctx, "invoice-layout"); !errors.Is(err, skills.ErrNotAvailable) {
		t.Fatalf("deleted skill must be unavailable, got %v", err)
	}
	if d, _ := GetBuilderSkill(ctx, pool, "invoice-layout", 0); d != nil {
		t.Fatal("deleted skill still visible in the detail")
	}
	if n := rowCount(t, pool, "builder_skill_versions"); n != 2 {
		t.Fatalf("soft delete must keep versions, got %d rows", n)
	}

	// Same name again starts a new lineage at version 1.
	r = mustSave(t, pool, user, parsedSkill("invoice-layout", "New purpose", "fresh"))
	if !r.Created || r.Version != 1 || !r.NewLineage {
		t.Fatalf("re-upload after delete: %+v", r)
	}
	if n := rowCount(t, pool, "builder_skills"); n != 2 {
		t.Fatalf("lineages = %d, want 2 (one deleted, one live)", n)
	}
	d, err := GetBuilderSkill(ctx, pool, "invoice-layout", 0)
	if err != nil || d == nil || d.Body != "fresh" || len(d.Versions) != 1 {
		t.Fatalf("detail of the new lineage: %+v %v", d, err)
	}
}

func TestBuilderSkillsStore_DetailVersionsAndHistory(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	ctx := context.Background()
	mustSave(t, pool, user, parsedSkill("a-skill", "d1", "body1"))
	mustSave(t, pool, user, parsedSkill("a-skill", "d2", "body2"))

	d, err := GetBuilderSkill(ctx, pool, "a-skill", 0)
	if err != nil || d == nil || d.Version != 2 || d.Body != "body2" || d.Description != "d2" || len(d.Versions) != 2 {
		t.Fatalf("latest: %+v %v", d, err)
	}
	if d.Versions[0].Version != 2 || d.Versions[1].Version != 1 || d.Versions[0].ContentSHA256 == d.Versions[1].ContentSHA256 {
		t.Fatalf("history: %+v", d.Versions)
	}
	old, err := GetBuilderSkill(ctx, pool, "a-skill", 1)
	if err != nil || old == nil || old.Body != "body1" || old.Description != "d1" {
		t.Fatalf("v1: %+v %v", old, err)
	}
	if none, _ := GetBuilderSkill(ctx, pool, "a-skill", 9); none != nil {
		t.Fatal("unknown version must be nil")
	}
	if none, _ := GetBuilderSkill(ctx, pool, "missing", 0); none != nil {
		t.Fatal("unknown skill must be nil")
	}
}

func TestBuilderSkillsStore_NameUniquenessIsCaseInsensitiveAmongLive(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	mustSave(t, pool, user, parsedSkill("foo", "d", "b"))
	_, err := pool.Exec(context.Background(),
		`INSERT INTO zeep_system.builder_skills (name, created_by) VALUES ('FOO', $1::uuid)`, user)
	if err == nil {
		t.Fatal("a second live skill differing only by case must be rejected by the index")
	}
	if err := DeleteBuilderSkill(context.Background(), pool, user, "foo"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO zeep_system.builder_skills (name, created_by) VALUES ('FOO', $1::uuid)`, user); err != nil {
		t.Fatalf("a deleted name must be free again: %v", err)
	}
}

func TestBuilderSkillsStore_BuiltinAndReservedNames(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	withBuiltinSkills(t, fixtureBuiltin("orbit-frontend-design", "Use when designing"))
	ctx := context.Background()

	if _, err := SaveBuilderSkill(ctx, pool, user, parsedSkill("orbit-frontend-design", "x", "y")); !errors.Is(err, ErrBuilderSkillBuiltin) {
		t.Fatalf("built-in name: %v", err)
	}
	if _, err := SaveBuilderSkill(ctx, pool, user, parsedSkill("orbit-other", "x", "y")); !errors.Is(err, ErrBuilderSkillReserved) {
		t.Fatalf("reserved prefix: %v", err)
	}
	if err := DeleteBuilderSkill(ctx, pool, user, "orbit-frontend-design"); !errors.Is(err, ErrBuilderSkillBuiltin) {
		t.Fatalf("delete built-in: %v", err)
	}
	if n := rowCount(t, pool, "builder_skills"); n != 0 {
		t.Fatalf("rejected writes left %d rows", n)
	}
	if err := DeleteBuilderSkill(ctx, pool, user, "missing"); !errors.Is(err, ErrBuilderSkillNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestBuilderSkillsStore_BuiltinEnabledByDefaultAndToggle(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	withBuiltinSkills(t, fixtureBuiltin("orbit-a", "Use when a"), fixtureBuiltin("orbit-b", "Use when b"))
	ctx := context.Background()
	src := NewBuilderSkillSource(pool)

	list, _, err := ListBuilderSkills(ctx, pool)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	for _, s := range list {
		if !s.Enabled || !s.Locked || s.Source != "built-in" || s.Version != 2 || s.Provenance != "written in-house" {
			t.Fatalf("built-in default: %+v", s)
		}
	}

	changed, err := SetBuilderSkillEnabled(ctx, pool, user, "orbit-a", false)
	if err != nil || !changed {
		t.Fatalf("disable: %v %v", changed, err)
	}
	if again, _ := SetBuilderSkillEnabled(ctx, pool, user, "orbit-a", false); again {
		t.Fatal("repeating the same state must report no change")
	}
	entries, _ := src.Entries(ctx)
	if len(entries) != 1 || entries[0].Name != "orbit-b" {
		t.Fatalf("entries after disable: %+v", entries)
	}
	if _, err := src.Get(ctx, "orbit-a"); !errors.Is(err, skills.ErrNotAvailable) {
		t.Fatalf("disabled built-in: %v", err)
	}
	got, err := src.Get(ctx, "orbit-b")
	if err != nil || got.Body != "BUILTIN-BODY-orbit-b" || got.Source != skills.OriginBuiltin || got.Version != 2 {
		t.Fatalf("Get built-in: %+v %v", got, err)
	}

	// The toggle persists and flips back.
	list, _, _ = ListBuilderSkills(ctx, pool)
	if list[0].Name != "orbit-a" || list[0].Enabled || list[0].UpdatedAt == nil {
		t.Fatalf("persisted state: %+v", list[0])
	}
	if _, err := SetBuilderSkillEnabled(ctx, pool, user, "orbit-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Get(ctx, "orbit-a"); err != nil {
		t.Fatalf("re-enabled built-in: %v", err)
	}
	if _, err := SetBuilderSkillEnabled(ctx, pool, user, "nope", true); !errors.Is(err, ErrBuilderSkillNotFound) {
		t.Fatalf("unknown name: %v", err)
	}
}

func TestBuilderSkillsStore_StaleBuiltinStateRowsAreIgnored(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	withBuiltinSkills(t, fixtureBuiltin("orbit-a", "Use when a"))
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.builder_skill_builtin_state (name, enabled, updated_by) VALUES ('orbit-removed-in-upgrade', false, $1::uuid)`, user); err != nil {
		t.Fatal(err)
	}
	list, _, err := ListBuilderSkills(ctx, pool)
	if err != nil || len(list) != 1 || list[0].Name != "orbit-a" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err := NewBuilderSkillSource(pool).Get(ctx, "orbit-removed-in-upgrade"); !errors.Is(err, skills.ErrNotAvailable) {
		t.Fatalf("stale state: %v", err)
	}
}

func TestBuilderSkillsStore_CustomToggleAndSourceFilters(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	ctx := context.Background()
	src := NewBuilderSkillSource(pool)
	mustSave(t, pool, user, parsedSkill("custom-a", "Use when a", "BODY-A"))

	if changed, err := SetBuilderSkillEnabled(ctx, pool, user, "custom-a", false); err != nil || !changed {
		t.Fatalf("disable: %v %v", changed, err)
	}
	if entries, _ := src.Entries(ctx); len(entries) != 0 {
		t.Fatalf("disabled skill in the index: %+v", entries)
	}
	if _, err := src.Get(ctx, "custom-a"); !errors.Is(err, skills.ErrNotAvailable) {
		t.Fatalf("disabled custom: %v", err)
	}
	// A new version of a disabled skill keeps it disabled.
	mustSave(t, pool, user, parsedSkill("custom-a", "Use when a2", "BODY-A2"))
	if _, err := src.Get(ctx, "custom-a"); !errors.Is(err, skills.ErrNotAvailable) {
		t.Fatalf("new version re-enabled a disabled skill: %v", err)
	}
	if _, err := SetBuilderSkillEnabled(ctx, pool, user, "custom-a", true); err != nil {
		t.Fatal(err)
	}
	entries, _ := src.Entries(ctx)
	if len(entries) != 1 || entries[0].Description != "Use when a2" || entries[0].Version != 2 || entries[0].Source != skills.OriginCustom {
		t.Fatalf("entries: %+v", entries)
	}
	got, err := src.Get(ctx, "custom-a")
	if err != nil || got.Body != "BODY-A2" {
		t.Fatalf("Get: %+v %v", got, err)
	}
	if _, err := src.Get(ctx, "unknown"); !errors.Is(err, skills.ErrNotAvailable) {
		t.Fatalf("unknown: %v", err)
	}
}

// fillIndex saves n enabled skills with 280-char descriptions and returns the
// resulting index size.
func fillIndex(t *testing.T, pool *db.Pool, user string, n int) int {
	t.Helper()
	desc := strings.Repeat("d", 280)
	for i := 0; i < n; i++ {
		mustSave(t, pool, user, parsedSkill(fmt.Sprintf("fill-%02d", i), desc, "b"))
	}
	return indexChars(t, pool)
}

func TestBuilderSkillsStore_BudgetOnSave(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	used := fillIndex(t, pool, user, 26)
	room := agent.MaxSkillsIndexChars - used
	if room < 100 || room > 290 {
		t.Fatalf("fixture assumption broken: room = %d", room)
	}

	_, err := SaveBuilderSkill(context.Background(), pool, user, parsedSkill("too-big", strings.Repeat("x", 300), "b"))
	var be *BuilderSkillsBudgetError
	if !errors.As(err, &be) || be.Max != agent.MaxSkillsIndexChars || be.Chars <= be.Max {
		t.Fatalf("want budget error, got %v", err)
	}
	if n := rowCount(t, pool, "builder_skills"); n != 26 {
		t.Fatalf("rejected upload left a row (%d skills)", n)
	}
	// A skill that fits is accepted.
	mustSave(t, pool, user, parsedSkill("small", "tiny", "b"))
}

func TestBuilderSkillsStore_ConcurrentUploadsNeverExceedBudget(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	used := fillIndex(t, pool, user, 26)
	candDesc := strings.Repeat("c", 200)
	one := skills.EntryChars(skills.Entry{Name: "cand-a", Description: candDesc})
	if used+one > agent.MaxSkillsIndexChars || used+2*one <= agent.MaxSkillsIndexChars {
		t.Fatalf("fixture assumption broken: used=%d one=%d", used, one)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, name := range []string{"cand-a", "cand-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = SaveBuilderSkill(context.Background(), pool, user, parsedSkill(name, candDesc, "b"))
		}()
	}
	wg.Wait()

	ok := 0
	for _, e := range errs {
		var be *BuilderSkillsBudgetError
		switch {
		case e == nil:
			ok++
		case !errors.As(e, &be):
			t.Fatalf("unexpected error: %v", e)
		}
	}
	if ok != 1 {
		t.Fatalf("%d concurrent uploads succeeded, want exactly 1 (errs: %v)", ok, errs)
	}
	if n := indexChars(t, pool); n > agent.MaxSkillsIndexChars {
		t.Fatalf("index grew to %d, over the %d budget", n, agent.MaxSkillsIndexChars)
	}
}

func TestBuilderSkillsStore_ConcurrentEnablesNeverExceedBudget(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	ctx := context.Background()
	candDesc := strings.Repeat("c", 200)
	for _, name := range []string{"cand-a", "cand-b"} {
		mustSave(t, pool, user, parsedSkill(name, candDesc, "b"))
		if _, err := SetBuilderSkillEnabled(ctx, pool, user, name, false); err != nil {
			t.Fatal(err)
		}
	}
	used := fillIndex(t, pool, user, 26)
	one := skills.EntryChars(skills.Entry{Name: "cand-a", Description: candDesc})
	if used+one > agent.MaxSkillsIndexChars || used+2*one <= agent.MaxSkillsIndexChars {
		t.Fatalf("fixture assumption broken: used=%d one=%d", used, one)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, name := range []string{"cand-a", "cand-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = SetBuilderSkillEnabled(ctx, pool, user, name, true)
		}()
	}
	wg.Wait()

	ok := 0
	for _, e := range errs {
		var be *BuilderSkillsBudgetError
		switch {
		case e == nil:
			ok++
		case !errors.As(e, &be):
			t.Fatalf("unexpected error: %v", e)
		}
	}
	if ok != 1 {
		t.Fatalf("%d concurrent enables succeeded, want exactly 1 (errs: %v)", ok, errs)
	}
	if n := indexChars(t, pool); n > agent.MaxSkillsIndexChars {
		t.Fatalf("index grew to %d, over the %d budget", n, agent.MaxSkillsIndexChars)
	}
}

func TestBuilderSkillsStore_ListReportsIndexAndOrder(t *testing.T) {
	pool, user := builderSkillsTestPool(t)
	withBuiltinSkills(t, fixtureBuiltin("orbit-z", "Use when z"), fixtureBuiltin("orbit-a", "Use when a"))
	mustSave(t, pool, user, parsedSkill("zeta", "Use when zeta", "b"))
	mustSave(t, pool, user, parsedSkill("alpha", "Use when alpha", "b"))

	list, info, err := ListBuilderSkills(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
		if !s.InAgentView || s.Chars <= 0 {
			t.Fatalf("%s: in_agent_view=%v chars=%d", s.Name, s.InAgentView, s.Chars)
		}
	}
	if strings.Join(names, ",") != "orbit-a,orbit-z,alpha,zeta" {
		t.Fatalf("order: %v", names)
	}
	if info.Max != agent.MaxSkillsIndexChars || info.Chars <= 0 || len(info.Dropped) != 0 || info.Dropped == nil {
		t.Fatalf("index info: %+v", info)
	}
	if list[2].UpdatedByEmail == nil || *list[2].UpdatedByEmail == "" {
		t.Fatalf("custom skill lacks the author: %+v", list[2])
	}
}

func TestCheckSkillsBudget_ShrinkingAnOverBudgetIndexIsAllowed(t *testing.T) {
	big := func(name string, n int) skills.Entry {
		return skills.Entry{Name: name, Description: strings.Repeat("x", n), Enabled: true, Source: skills.OriginCustom}
	}
	var before []skills.Entry
	for i := 0; i < 30; i++ { // ~9300 chars: over budget (as after an upgrade)
		before = append(before, big(fmt.Sprintf("s-%02d", i), 295))
	}
	if skills.IndexChars(before) <= agent.MaxSkillsIndexChars {
		t.Fatal("fixture must start over budget")
	}
	if err := checkSkillsBudget(before, withEntry(before, big("s-00", 10))); err != nil {
		t.Fatalf("shrinking must be allowed: %v", err)
	}
	if err := checkSkillsBudget(before, withEntry(before, big("s-00", 300))); err == nil {
		t.Fatal("growing an over-budget index must be rejected")
	}
}

// The built-ins that ship in the binary: listed first, locked, enabled by
// default, loadable, and inside the budget reserved for them.
func TestBuilderSkillsStore_ShippedBuiltinsAreListedLockedAndLoadable(t *testing.T) {
	pool, _ := builderSettingsTestPool(t) // no withBuiltinSkills: the real embedded set
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `TRUNCATE zeep_system.builder_skills, zeep_system.builder_skill_versions, zeep_system.builder_skill_builtin_state CASCADE`)

	want := []string{
		"orbit-accessibility", "orbit-crud-screens", "orbit-forms-and-states",
		"orbit-frontend-design", "orbit-react-patterns", "orbit-responsive-layout",
	}
	list, info, err := ListBuilderSkills(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
		if !s.Locked || !s.Enabled || s.Source != "built-in" || !s.InAgentView || s.Provenance == "" {
			t.Errorf("%s: %+v", s.Name, s)
		}
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("built-ins: %v", names)
	}
	if info.Chars > skills.MaxBuiltinIndexChars || len(info.Dropped) != 0 {
		t.Fatalf("built-in index %d chars (reserved %d), dropped %v", info.Chars, skills.MaxBuiltinIndexChars, info.Dropped)
	}

	src := NewBuilderSkillSource(pool)
	for _, n := range want {
		l, err := src.Get(ctx, n)
		if err != nil || l.Source != skills.OriginBuiltin || len(l.Body) < 1000 {
			t.Fatalf("%s: %v (body %d bytes)", n, err, len(l.Body))
		}
	}
}

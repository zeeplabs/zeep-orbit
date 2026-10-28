package dashboard

// builder_runtime_test.go — the builder wiring (ai-frontend-builder T30):
// the off switch, the secret requirement, per-turn provider/limits resolution,
// the pinned prompt layer and the workspace scan. DB tests skip without
// TEST_DATABASE_URL; nothing here touches the network beyond loopback.

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/skills"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

func TestBuilderRuntime_EmptyURLIsOffAndNilSafe(t *testing.T) {
	for _, url := range []string{"", "   "} {
		rt, err := NewBuilderRuntime(nil, BuilderRuntimeConfig{URL: url, SharedSecret: "ignored"})
		if err != nil || rt != nil {
			t.Fatalf("URL %q: got (%v, %v), want (nil, nil)", url, rt, err)
		}
	}

	var rt *BuilderRuntime
	cfg := rt.HandlerConfig()
	if cfg.Sandbox != nil || cfg.Store != nil || cfg.PreviewSigner != nil || cfg.Poke != nil {
		t.Fatalf("nil runtime must yield a zero handler config, got %+v", cfg)
	}
	rt.Start(context.Background()) // no goroutine, no panic
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop on a nil runtime: %v", err)
	}
	// The handler built from it is "not configured": the interface is a true nil.
	if NewBuilderHandler(nil, cfg).configured() {
		t.Fatal("handler must not be configured without a builder")
	}
}

func TestBuilderRuntime_SecretIsRequiredWhenURLSet(t *testing.T) {
	pool, _ := builderSettingsTestPool(t)
	for name, secret := range map[string]string{"missing": "", "too short": "q7z9x"} {
		t.Run(name, func(t *testing.T) {
			rt, err := NewBuilderRuntime(pool, BuilderRuntimeConfig{URL: "http://builder:8090", SharedSecret: secret})
			if err == nil || rt != nil {
				t.Fatalf("got (%v, %v), want an error", rt, err)
			}
			if !strings.Contains(err.Error(), EnvBuilderSharedSecret) {
				t.Errorf("error should name %s: %v", EnvBuilderSharedSecret, err)
			}
			if secret != "" && strings.Contains(err.Error(), secret) {
				t.Errorf("error must not contain the secret: %v", err)
			}
		})
	}
	if _, err := NewBuilderRuntime(pool, BuilderRuntimeConfig{URL: "not a url", SharedSecret: strings.Repeat("s", 32)}); err == nil {
		t.Error("an invalid BUILDER_URL must fail")
	}
}

func newTestBuilderRuntime(t *testing.T) *BuilderRuntime {
	t.Helper()
	pool, _ := builderSettingsTestPool(t)
	rt, err := NewBuilderRuntime(pool, BuilderRuntimeConfig{
		URL: "http://builder.invalid:8090", SharedSecret: strings.Repeat("s", 32), Concurrency: 1,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil || rt == nil {
		t.Fatalf("NewBuilderRuntime: %v", err)
	}
	cfg := rt.HandlerConfig()
	if cfg.Sandbox == nil || cfg.Store == nil || cfg.PreviewSigner == nil || cfg.Poke == nil {
		t.Fatalf("handler config incomplete: %+v", cfg)
	}
	return rt
}

// The provider and the limits are read from the tables on every turn, so a
// superadmin change applies to the next turn without a restart.
func TestBuilderRuntime_PrepareDepsResolvesProviderAndLimitsPerTurn(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	pool := rt.pool
	h := NewHandler(pool, nil, nil)
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()

	var gotAuth string
	var gotBody map[string]any
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	}))
	defer llm.Close()

	base := agent.Deps{Provider: unavailableProvider{}}

	// No provider configured yet: the turn gets a provider whose Chat fails.
	d := rt.prepareDeps(ctx, base)
	if _, err := d.Provider.Chat(ctx, provider.ChatRequest{}); err == nil {
		t.Fatal("an unconfigured provider must fail Chat")
	} else if pe := new(provider.ErrProvider); !errors.As(err, &pe) {
		t.Fatalf("error = %v, want *provider.ErrProvider (maps to provider_unavailable)", err)
	}

	// Configure openai_compatible on loopback (allowed) + builder settings.
	if w := putOpenAICompatible(t, h, super, "openai_compatible",
		`{"model":"llama3.1","api_key":"sk-local","base_url":"`+llm.URL+`/v1","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT provider: %d %s", w.Code, w.Body.String())
	}
	prov, steps, fixes, secs, window := "openai_compatible", 7, 2, 90, 32000
	if _, err := UpsertBuilderSettings(ctx, pool, super.ID, BuilderSettingsInput{
		Provider: &prov, MaxStepsPerTurn: &steps, MaxFixIterations: &fixes, MaxTurnSeconds: &secs, ContextWindowTokens: &window,
	}); err != nil {
		t.Fatalf("settings: %v", err)
	}

	d = rt.prepareDeps(ctx, base)
	if d.Provider.Name() != "openai_compatible" || d.Provider.Model() != "llama3.1" {
		t.Fatalf("provider = %s/%s", d.Provider.Name(), d.Provider.Model())
	}
	want := agent.Limits{MaxSteps: 7, MaxFixIterations: 2, MaxTurnDuration: 90 * time.Second, ContextWindowTokens: 32000}
	if d.Limits != want {
		t.Errorf("limits = %+v, want %+v", d.Limits, want)
	}
	resp, err := d.Provider.Chat(ctx, provider.ChatRequest{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat through the guarded client to a loopback server: %v", err)
	}
	if resp.Message.Content != "ok" || gotAuth != "Bearer sk-local" || gotBody["model"] != "llama3.1" {
		t.Errorf("resp=%q auth=%q body model=%v", resp.Message.Content, gotAuth, gotBody["model"])
	}

	// A later change applies to the very next turn.
	steps = 9
	if _, err := UpsertBuilderSettings(ctx, pool, super.ID, BuilderSettingsInput{MaxStepsPerTurn: &steps}); err != nil {
		t.Fatal(err)
	}
	if d = rt.prepareDeps(ctx, base); d.Limits.MaxSteps != 9 {
		t.Errorf("MaxSteps = %d after the change, want 9", d.Limits.MaxSteps)
	}
	disabled := `{"model":"llama3.1","enabled":false}`
	if w := putOpenAICompatible(t, h, super, "openai_compatible", disabled); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	if d = rt.prepareDeps(ctx, base); d.Provider.Name() != "unavailable" {
		t.Errorf("a disabled provider must resolve to the unavailable provider, got %s", d.Provider.Name())
	}
}

type archiveSandbox struct {
	sandbox.Sandbox
	data []byte
	err  error
}

func (a archiveSandbox) Archive(context.Context, string, string) (io.ReadCloser, error) {
	if a.err != nil {
		return nil, a.err
	}
	return io.NopCloser(bytes.NewReader(a.data)), nil
}

func tarOf(t *testing.T, files map[string]string, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range order {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestScanWorkspaceArchive_GuideAndDepthLimitedTree(t *testing.T) {
	files := map[string]string{
		"ORBIT_AGENT.md":             "# Guide\nUse the design system.",
		"package.json":               "{}",
		"src/App.tsx":                "x",
		"src/pages/Home.tsx":         "x",
		"src/components/ui/a/b/c.ts": "x",
		"node_modules/x/index.js":    "x",
		".git/config":                "x",
	}
	order := []string{"ORBIT_AGENT.md", "package.json", "src/App.tsx", "src/pages/Home.tsx", "src/components/ui/a/b/c.ts", "node_modules/x/index.js", ".git/config"}
	guide, tree, err := scanWorkspaceArchive(bytes.NewReader(tarOf(t, files, order)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(guide, "Use the design system.") {
		t.Errorf("guide = %q", guide)
	}
	lines := strings.Split(tree, "\n")
	for _, want := range []string{"ORBIT_AGENT.md", "package.json", "src/App.tsx", "src/pages/Home.tsx", "src/components/ui/"} {
		found := false
		for _, l := range lines {
			found = found || l == want
		}
		if !found {
			t.Errorf("tree missing %q:\n%s", want, tree)
		}
	}
	if strings.Contains(tree, "node_modules") || strings.Contains(tree, ".git") || strings.Contains(tree, "c.ts") {
		t.Errorf("tree must skip excluded dirs and collapse deep paths:\n%s", tree)
	}
}

func TestScanWorkspaceArchive_NoGuideAndGarbage(t *testing.T) {
	guide, tree, err := scanWorkspaceArchive(bytes.NewReader(tarOf(t, map[string]string{"a.txt": "x"}, []string{"a.txt"})))
	if err != nil || guide != "" || tree != "a.txt" {
		t.Errorf("got (%q, %q, %v)", guide, tree, err)
	}
	if _, _, err := scanWorkspaceArchive(strings.NewReader("this is not a tar archive at all......................................................................................................................................................................................................")); err == nil {
		t.Error("garbage must surface a read error")
	}
}

// The company layer is the version pinned on the session; the template guide
// and file tree come from the workspace; a scan failure degrades, a pinned
// prompt lookup failure does not.
func TestBuilderRuntime_PromptSource(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	pool := rt.pool
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()

	v1, _, err := SaveBuilderPrompt(ctx, pool, super.ID, BuilderPromptInput{Persona: strPtr("Persona v1"), Rules: strPtr("Rule v1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SaveBuilderPrompt(ctx, pool, super.ID, BuilderPromptInput{Persona: strPtr("Persona v2"), Rules: strPtr("Rule v2")}); err != nil {
		t.Fatal(err)
	}

	tarData := tarOf(t, map[string]string{"ORBIT_AGENT.md": "guide text", "src/main.tsx": "x"}, []string{"ORBIT_AGENT.md", "src/main.tsx"})
	rt.sandbox = archiveSandbox{data: tarData}

	sess := &store.Session{ID: "11111111-1111-4111-8111-111111111111", BackendAppID: "22222222-2222-4222-8222-222222222222", Locale: "pt-BR", PromptVersionID: &v1.ID}
	in, err := rt.promptSource(ctx, sess)
	if err != nil {
		t.Fatalf("promptSource: %v", err)
	}
	if want := agent.CompanyLayer("Persona v1", "Rule v1"); in.Company != want {
		t.Errorf("company = %q, want the PINNED version %q (not the newest)", in.Company, want)
	}
	if in.TemplateGuide != "guide text" || !strings.Contains(in.Session.FileTree, "src/main.tsx") {
		t.Errorf("guide=%q tree=%q", in.TemplateGuide, in.Session.FileTree)
	}
	if in.Session.Locale != "pt-BR" || in.Session.BackendAppID != sess.BackendAppID {
		t.Errorf("session ctx = %+v", in.Session)
	}

	// No pinned prompt: no company layer. Workspace unreachable: degraded, no error.
	sess.PromptVersionID = nil
	rt.sandbox = archiveSandbox{err: sandbox.ErrWorkspaceLost}
	in, err = rt.promptSource(ctx, sess)
	if err != nil {
		t.Fatalf("a workspace scan failure must degrade, not fail the turn: %v", err)
	}
	if in.Company != "" || in.TemplateGuide != "" || in.Session.FileTree != "" {
		t.Errorf("expected an empty input, got %+v", in)
	}
}

func strPtr(s string) *string { return &s }

// Start runs the worker, Stop drains it and returns promptly when idle; both
// are idempotent.
func TestBuilderRuntime_StartStop(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	rt.Start(context.Background())
	rt.Start(context.Background()) // second call is a no-op
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := rt.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("an idle worker took %s to stop", d)
	}
	if err := rt.Stop(ctx); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

// Skills reach the agent through the prompt index and load_skill, resolved on
// every turn: a disable applies to the next turn, and a database failure fails
// the turn instead of silently dropping company skills (ai-builder-skills S08).
func TestBuilderRuntime_PromptSourceServesSkillsIndexPerTurn(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	pool := rt.pool
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()
	rt.sandbox = archiveSandbox{err: sandbox.ErrWorkspaceLost}
	sess := &store.Session{ID: "11111111-1111-4111-8111-111111111111", BackendAppID: "22222222-2222-4222-8222-222222222222", Locale: "en"}

	truncate := func() {
		_, _ = pool.Exec(ctx, `TRUNCATE zeep_system.builder_skills, zeep_system.builder_skill_versions, zeep_system.builder_skill_builtin_state CASCADE`)
	}
	truncate()
	t.Cleanup(truncate)
	withBuiltinSkills(t, fixtureBuiltin("orbit-a", "Use when a"))

	// A session created before skills existed: only the built-in, no custom.
	in, err := rt.promptSource(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(in.SkillsIndex, "- orbit-a: Use when a") || strings.Contains(in.SkillsIndex, "invoice-layout") {
		t.Fatalf("index: %q", in.SkillsIndex)
	}

	mustSave(t, pool, super.ID, parsedSkill("invoice-layout", "Use when building invoices", "INVOICE-BODY"))
	in, err = rt.promptSource(ctx, sess)
	if err != nil || !strings.Contains(in.SkillsIndex, "- invoice-layout: Use when building invoices") {
		t.Fatalf("custom skill missing from the index: %q %v", in.SkillsIndex, err)
	}
	if strings.Contains(in.SkillsIndex, "INVOICE-BODY") {
		t.Fatal("the index carries a skill body")
	}

	// Disable both: the very next turn has no skills section at all.
	for _, n := range []string{"orbit-a", "invoice-layout"} {
		if _, err := SetBuilderSkillEnabled(ctx, pool, super.ID, n, false); err != nil {
			t.Fatal(err)
		}
	}
	in, err = rt.promptSource(ctx, sess)
	if err != nil || in.SkillsIndex != "" {
		t.Fatalf("with no enabled skills the index must be empty: %q %v", in.SkillsIndex, err)
	}
}

func TestBuilderRuntime_PromptSourceFailsTheTurnWhenSkillsCannotBeRead(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	rt.sandbox = archiveSandbox{err: sandbox.ErrWorkspaceLost}
	rt.pool.Close()
	sess := &store.Session{ID: "11111111-1111-4111-8111-111111111111", BackendAppID: "22222222-2222-4222-8222-222222222222", Locale: "en"}
	if _, err := rt.promptSource(context.Background(), sess); err == nil {
		t.Fatal("a database failure must fail the prompt (the loop maps it to internal_error)")
	}
}

// load_skill is registered on the shared registry next to the other tools and
// serves what is enabled right now.
func TestBuilderRuntime_ToolRegistryHasLoadSkillAndOnlyKnownTools(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	pool := rt.pool
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `TRUNCATE zeep_system.builder_skills, zeep_system.builder_skill_versions, zeep_system.builder_skill_builtin_state CASCADE`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.builder_skills, zeep_system.builder_skill_versions, zeep_system.builder_skill_builtin_state CASCADE`)
	})

	reg, err := newBuilderToolRegistry(pool, rt.sandbox, rt.store, rt.skills, quietSlog())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, spec := range reg.Specs() {
		names = append(names, spec.Name)
		// Built-in skill lint accepts exactly the tools the builder registers;
		// a new tool must be added to skills.KnownToolName.
		if !skills.KnownToolName(spec.Name) {
			t.Errorf("registered tool %q is unknown to the built-in skill lint (skills.KnownToolName)", spec.Name)
		}
	}
	if indexOfString(names, skills.ToolLoadSkill) < 0 {
		t.Fatalf("load_skill is not registered: %v", names)
	}

	mustSave(t, pool, super.ID, parsedSkill("invoice-layout", "Use when building invoices", "INVOICE-BODY"))
	res, err := reg.Dispatch(ctx, tools.Call{ID: "c1", Name: skills.ToolLoadSkill, Arguments: json.RawMessage(`{"name":"invoice-layout"}`), SessionID: "s"})
	if err != nil || res.IsError || !strings.Contains(res.Content, "INVOICE-BODY") || res.Meta[skills.MetaSkill] != "invoice-layout" {
		t.Fatalf("load_skill: %+v %v", res, err)
	}

	if _, err := SetBuilderSkillEnabled(ctx, pool, super.ID, "invoice-layout", false); err != nil {
		t.Fatal(err)
	}
	res, err = reg.Dispatch(ctx, tools.Call{ID: "c2", Name: skills.ToolLoadSkill, Arguments: json.RawMessage(`{"name":"invoice-layout"}`), SessionID: "s"})
	if err != nil || !res.IsError || strings.Contains(res.Content, "INVOICE-BODY") {
		t.Fatalf("a disabled skill must be unavailable on the next call: %+v %v", res, err)
	}
}

func quietSlog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func indexOfString(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

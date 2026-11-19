package dashboard

// builder_handlers_real_test.go — the default-template create flow against the
// REAL builder HTTP server (internal/builder) and git, not the fake sandbox:
// it proves the Init -> Archive -> Destroy -> Init(tar) round trip that puts the
// generated src/orbit.config.ts into the turn 0 commit actually works with the
// builder's own tar rules (ai-frontend-builder T25). Skipped when git is not on
// PATH. npm/npx are no-op scripts: no step is built here.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/builder"
)

func newRealBuilderSandbox(t *testing.T) sandbox.Sandbox {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	work, tmpl, ctl := t.TempDir(), t.TempDir(), t.TempDir()
	put := func(rel, content string) {
		p := filepath.Join(tmpl, "default", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, content := range bhDefaultTemplateFiles() {
		put(rel, content)
	}
	put(".gitignore", "node_modules/\ndist/\n.orbit/\n")
	script := func(name string) string {
		p := filepath.Join(ctl, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ex, err := builder.NewExecutor(builder.ExecConfig{
		NpmPath: script("npm"), NpxPath: script("npx"), Path: "/usr/bin:/bin", Home: ctl,
		WorkDir: work, MaxOutputTail: 4096, WaitDelay: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := builder.NewGitRepo(builder.GitConfig{HomeDir: filepath.Join(t.TempDir(), "home")})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := builder.NewSigner([]byte("dashboard-real-builder-test-secret-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := builder.NewServer(builder.ServerConfig{
		WorkDir: work, TemplatesDir: tmpl, Signer: signer, Executor: ex, Git: g,
		MaxActiveSessions: 4, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	sb, err := sandbox.NewHTTP(ts.URL, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	return sb
}

func TestBuilderCreate_DefaultTemplateAgainstRealBuilder(t *testing.T) {
	e := newBHEnv(t)
	real := newRealBuilderSandbox(t)
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: real, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
	})

	w := e.create(t, e.viewer, fmt.Sprintf(`{"backend_app_id":%q}`, e.appA))
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSessionView](t, w)
	if got.TemplateKind != "default" || got.TemplateVersion != "7" {
		t.Errorf("template = %s@%s, want default@7 (the builder's own TEMPLATE_VERSION)", got.TemplateKind, got.TemplateVersion)
	}
	if len(got.HeadSHA) != 40 {
		t.Fatalf("head sha = %q", got.HeadSHA)
	}

	// The turn 0 commit itself holds the generated config.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, err := real.Archive(ctx, got.ID, got.HeadSHA)
	if err != nil {
		t.Fatalf("archive turn 0: %v", err)
	}
	defer rc.Close()
	files, err := bhReadTar(rc)
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["src/orbit.config.ts"]
	if !strings.Contains(cfg, "'https://orbit.test.example'") || !strings.Contains(cfg, "'bh_app_a'") || strings.Contains(cfg, "example.invalid") {
		t.Errorf("turn 0 commit lacks the generated config:\n%s", cfg)
	}
	for _, keep := range []string{"package.json", "TEMPLATE_VERSION", "ORBIT_AGENT.md", "src/App.tsx", ".gitignore"} {
		if _, ok := files[keep]; !ok {
			t.Errorf("%s missing from turn 0", keep)
		}
	}

	// One full snapshot, whose commit is the session head.
	snaps, err := e.st.ListSnapshots(ctx, got.ID)
	if err != nil || len(snaps) != 1 || snaps[0].CommitSHA != got.HeadSHA || len(snaps[0].Bundle) == 0 {
		t.Fatalf("snapshots = %+v err=%v", snaps, err)
	}

	// The snapshot restores a workspace that is exactly turn 0: destroy, restore, same tree.
	if err := real.Destroy(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if err := real.Restore(ctx, got.ID, []sandbox.Bundle{{Seq: 0, SHA: snaps[0].CommitSHA, Data: snaps[0].Bundle}}); err != nil {
		t.Fatalf("restore from the stored snapshot: %v", err)
	}
	rc2, err := real.Archive(ctx, got.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc2.Close()
	restored, err := bhReadTar(rc2)
	if err != nil || restored["src/orbit.config.ts"] != cfg {
		t.Errorf("restored workspace lost the generated config (err %v)", err)
	}

	// Delete archives and removes the real workspace.
	if w := e.del(t, e.viewer, got.ID); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if st := e.sessionStatus(t, got.ID); st != store.SessionArchived {
		t.Errorf("status = %s", st)
	}
	if _, err := real.Archive(ctx, got.ID, ""); err == nil {
		t.Error("the workspace must be gone after delete")
	}
}

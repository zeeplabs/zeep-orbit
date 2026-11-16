package sandbox

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/builder"
)

const testSecret = "sandbox-test-secret-0123456789abcdef"

// newBuilderServer starts the REAL builder HTTP server (T07) in-process over
// a real workspace, executor and git. Only npm/npx are fake scripts: the
// "build" step writes a tiny dist/index.html.
func newBuilderServer(t *testing.T) (*httptest.Server, *builder.Signer) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	work, tmpl, ctl := t.TempDir(), t.TempDir(), t.TempDir()

	def := filepath.Join(tmpl, "default")
	writeFile(t, filepath.Join(def, "package.json"), `{"name":"tpl"}`)
	writeFile(t, filepath.Join(def, "TEMPLATE_VERSION"), "7\n")
	writeFile(t, filepath.Join(def, ".gitignore"), "node_modules/\ndist/\n.orbit/\n")
	writeFile(t, filepath.Join(def, "src", "main.ts"), "export {}\n")

	script := func(name, body string) string {
		p := filepath.Join(ctl, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	npx := `case "$*" in *vite*)
  rm -rf dist
  mkdir -p dist/assets
  printf '<html>INDEX</html>' > dist/index.html
  ;;
esac
exit 0`
	ex, err := builder.NewExecutor(builder.ExecConfig{
		NpmPath:       script("npm", "exit 0"),
		NpxPath:       script("npx", npx),
		Path:          "/usr/bin:/bin",
		Home:          ctl,
		WorkDir:       work,
		MaxOutputTail: 4096,
		WaitDelay:     200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := builder.NewGitRepo(builder.GitConfig{HomeDir: filepath.Join(t.TempDir(), "home")})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := builder.NewSigner([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := builder.NewServer(builder.ServerConfig{
		WorkDir:           work,
		TemplatesDir:      tmpl,
		Signer:            signer,
		Executor:          ex,
		Git:               g,
		MaxActiveSessions: 4,
		Version:           "test-1",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, signer
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newClient(t *testing.T) (Sandbox, *httptest.Server) {
	t.Helper()
	ts, signer := newBuilderServer(t)
	sb, err := NewHTTP(ts.URL, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	return sb, ts
}

func TestHTTPSandbox_FullOpCycle(t *testing.T) {
	sb, _ := newClient(t)
	ctx := context.Background()
	const sid = "sess-1"

	init, err := sb.Init(ctx, sid, TemplateSource{Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if len(init.SHA) != 40 || len(init.Bundle) == 0 || init.TemplateVersion != "7" {
		t.Fatalf("init result: %+v", init)
	}

	if _, err := sb.Op(ctx, sid, "op-1", FileOp{Kind: OpWriteFile, Path: "src/App.tsx", Content: "const a = 1\n"}); err != nil {
		t.Fatal(err)
	}
	res, err := sb.Op(ctx, sid, "op-2", FileOp{Kind: OpEditFile, Path: "src/App.tsx", OldString: "a = 1", NewString: "a = 2"})
	if err != nil || !res.OK {
		t.Fatalf("edit: %+v %v", res, err)
	}
	read, err := sb.Op(ctx, sid, "", FileOp{Kind: OpReadFile, Path: "src/App.tsx"})
	if err != nil || !strings.Contains(read.Output, "a = 2") {
		t.Fatalf("read: %+v %v", read, err)
	}
	list, err := sb.Op(ctx, sid, "", FileOp{Kind: OpListDir, Path: "src"})
	if err != nil || !strings.Contains(list.Output, "App.tsx") {
		t.Fatalf("list: %+v %v", list, err)
	}

	b, err := sb.Build(ctx, sid, "op-3", StepBuild, "/preview/x/")
	if err != nil || b.ExitCode != 0 {
		t.Fatalf("build: %+v %v", b, err)
	}
	if _, err := sb.BuildLog(ctx, sid, StepBuild, 100); err != nil {
		t.Fatalf("build log: %v", err)
	}

	c, err := sb.Commit(ctx, sid, 1, "first")
	if err != nil || !c.Changed || c.Prev != init.SHA || len(c.Bundle) == 0 {
		t.Fatalf("commit: %+v %v", c, err)
	}
	noop, err := sb.Commit(ctx, sid, 2, "")
	if err != nil || noop.Changed || len(noop.Bundle) != 0 {
		t.Fatalf("second commit should be a no-op: %+v %v", noop, err)
	}

	rv, err := sb.Revert(ctx, sid, init.SHA, 3)
	if err != nil || !rv.Changed || rv.SHA == c.SHA {
		t.Fatalf("revert: %+v %v", rv, err)
	}

	rc, err := sb.Archive(ctx, sid, "")
	if err != nil {
		t.Fatal(err)
	}
	names := tarNames(t, rc)
	if !names["src/main.ts"] || names["src/App.tsx"] {
		t.Fatalf("archive after revert should hold the template tree only: %v", names)
	}

	h, err := sb.Health(ctx)
	if err != nil || h.Capacity != 4 || h.Version != "test-1" || h.DefaultTemplateVersion != "7" {
		t.Fatalf("health: %+v %v", h, err)
	}

	if err := sb.Destroy(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if err := sb.Destroy(ctx, sid); err != nil {
		t.Fatalf("destroying an absent session must succeed: %v", err)
	}
	if _, err := sb.Op(ctx, sid, "", FileOp{Kind: OpListDir}); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("op after destroy: got %v, want ErrUnknownSession", err)
	}
}

func tarNames(t *testing.T, rc io.ReadCloser) map[string]bool {
	t.Helper()
	defer rc.Close()
	names := map[string]bool{}
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names[h.Name] = true
	}
}

func TestHTTPSandbox_OpIDReplay(t *testing.T) {
	sb, _ := newClient(t)
	ctx := context.Background()
	if _, err := sb.Init(ctx, "s2", TemplateSource{Name: "default"}); err != nil {
		t.Fatal(err)
	}
	op := FileOp{Kind: OpWriteFile, Path: "a.txt", Content: "one"}
	first, err := sb.Op(ctx, "s2", "same-id", op)
	if err != nil || first.Replayed {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := sb.Op(ctx, "s2", "same-id", FileOp{Kind: OpWriteFile, Path: "a.txt", Content: "two"})
	if err != nil || !second.Replayed {
		t.Fatalf("repeated op_id must replay the stored result: %+v %v", second, err)
	}
	read, _ := sb.Op(ctx, "s2", "", FileOp{Kind: OpReadFile, Path: "a.txt"})
	if !strings.Contains(read.Output, "one") || strings.Contains(read.Output, "two") {
		t.Fatalf("replay must not re-execute: %q", read.Output)
	}
}

func TestHTTPSandbox_ErrorMapping(t *testing.T) {
	sb, _ := newClient(t)
	ctx := context.Background()
	if _, err := sb.Init(ctx, "s3", TemplateSource{Name: "default"}); err != nil {
		t.Fatal(err)
	}

	// Tool-level: the model may see the message.
	_, err := sb.Op(ctx, "s3", "e1", FileOp{Kind: OpEditFile, Path: "src/main.ts", OldString: "nope", NewString: "x"})
	var be *Error
	if !errors.As(err, &be) || !be.ToolLevel() || be.Code != "edit_not_found" {
		t.Fatalf("edit miss: %#v", err)
	}
	if msg, ok := ToolMessage(err); !ok || msg == "" {
		t.Fatalf("ToolMessage = %q, %v", msg, ok)
	}
	_, err = sb.Op(ctx, "s3", "e2", FileOp{Kind: OpWriteFile, Path: "../escape", Content: "x"})
	if _, ok := ToolMessage(err); !ok {
		t.Fatalf("path escape should be tool-level: %v", err)
	}

	// Unknown step is the builder's 422, still tool-level.
	if _, err := sb.Build(ctx, "s3", "b1", BuildStep("rm -rf"), ""); err == nil {
		t.Fatal("unknown step must fail")
	}

	// Client-side session id check: never sent, never traverses.
	if _, err := sb.Op(ctx, "../x", "", FileOp{Kind: OpListDir}); err == nil {
		t.Fatal("invalid session id must fail")
	}
}

func TestHTTPSandbox_BadSecretIsNotToolLevel(t *testing.T) {
	ts, _ := newBuilderServer(t)
	wrong, err := builder.NewSigner([]byte("another-secret-0123456789abcdef!!"))
	if err != nil {
		t.Fatal(err)
	}
	sb, err := NewHTTP(ts.URL, wrong, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sb.Op(context.Background(), "s4", "", FileOp{Kind: OpListDir})
	var be *Error
	if !errors.As(err, &be) || be.Status != http.StatusUnauthorized {
		t.Fatalf("want 401 *Error, got %#v", err)
	}
	if _, ok := ToolMessage(err); ok {
		t.Fatal("a credential failure must never be shown to the model")
	}
	if strings.Contains(err.Error(), ts.URL) {
		t.Fatalf("error leaks the builder URL: %v", err)
	}
}

func TestHTTPSandbox_TransportErrorIsGeneric(t *testing.T) {
	signer, _ := builder.NewSigner([]byte(testSecret))
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	sb, err := NewHTTP(url, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sb.Health(context.Background())
	var be *Error
	if !errors.As(err, &be) || be.Status != 0 || be.Code != "unavailable" || be.Unwrap() == nil {
		t.Fatalf("got %#v", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("Error() leaks the host: %v", err)
	}
	if _, ok := ToolMessage(err); ok {
		t.Fatal("transport failure must not reach the model")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sb.Health(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must stay matchable: %v", err)
	}
}

func TestHTTPSandbox_PreviewProxiesWithoutCredentials(t *testing.T) {
	sb, _ := newClient(t)
	ctx := context.Background()
	if _, err := sb.Init(ctx, "s5", TemplateSource{Name: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Build(ctx, "s5", "b1", StepBuild, "/p/"); err != nil {
		t.Fatal(err)
	}
	h := http.StripPrefix("/api/preview", sb.Preview("s5"))

	req := httptest.NewRequest(http.MethodGet, "/api/preview/?pt=secret-preview-token", nil)
	req.Header.Set("Cookie", "orbit_session=abc")
	req.Header.Set("Authorization", "Bearer user")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "INDEX") {
		t.Fatalf("preview: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("builder CSP header must pass through: %v", rec.Header())
	}

	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/preview/", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", post.Code)
	}
	trav := httptest.NewRecorder()
	h.ServeHTTP(trav, httptest.NewRequest(http.MethodGet, "/api/preview/a/../../x", nil))
	if trav.Code != http.StatusBadRequest {
		t.Fatalf("traversal: %d", trav.Code)
	}
}

// TestHTTPSandbox_RequestsAreSignedAndSanitized talks to a capture server: every
// request must carry a token bound to session, method and path; the preview
// request must carry nothing of the caller; a redirect must not be followed.
func TestHTTPSandbox_RequestsAreSignedAndSanitized(t *testing.T) {
	signer, _ := builder.NewSigner([]byte(testSecret))
	var (
		mu       sync.Mutex
		seen     []*http.Request
		leakSeen bool
	)
	leak := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		leakSeen = true
		mu.Unlock()
	}))
	defer leak.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Clone(context.Background()))
		mu.Unlock()
		if r.URL.Path == "/v1/sessions/redir/ops" {
			http.Redirect(w, r, leak.URL+"/steal", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"output":"x"}`))
	}))
	defer srv.Close()
	sb, err := NewHTTP(srv.URL+"/", signer, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sb.Op(context.Background(), "abc", "o1", FileOp{Kind: OpListDir}); err != nil {
		t.Fatal(err)
	}
	prev := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodGet, "/assets/a.js?pt=SECRET", nil)
	preq.Header.Set("Cookie", "c=1")
	preq.Header.Set("Authorization", "Bearer u")
	sb.Preview("abc").ServeHTTP(prev, preq)

	if _, err := sb.Op(context.Background(), "redir", "o2", FileOp{Kind: OpListDir}); err == nil {
		t.Fatal("a redirect must surface as an error, not be followed")
	}

	mu.Lock()
	defer mu.Unlock()
	if leakSeen {
		t.Fatal("redirect was followed: token could have leaked to another host")
	}
	if len(seen) < 2 {
		t.Fatalf("captured %d requests", len(seen))
	}
	for _, r := range seen[:2] {
		tok := r.Header.Get(builder.TokenHeader)
		if err := signer.VerifyRequest(tok, "abc", r.Method, r.URL.Path); err != nil {
			t.Fatalf("%s %s: token does not verify: %v", r.Method, r.URL.Path, err)
		}
	}
	p := seen[1]
	if p.URL.Path != "/v1/sessions/abc/preview/assets/a.js" || p.URL.RawQuery != "" {
		t.Fatalf("preview target: %s?%s", p.URL.Path, p.URL.RawQuery)
	}
	if p.Header.Get("Cookie") != "" || p.Header.Get("Authorization") != "" {
		t.Fatalf("caller credentials reached the builder: %v", p.Header)
	}
}

func TestNewHTTP_Validation(t *testing.T) {
	signer, _ := builder.NewSigner([]byte(testSecret))
	for _, u := range []string{"", "builder:8081", "ftp://x", "http://", "http://u:p@x", "http://x?a=1", "http://x#f"} {
		if _, err := NewHTTP(u, signer, nil); err == nil {
			t.Errorf("NewHTTP(%q) should fail", u)
		}
	}
	if _, err := NewHTTP("http://builder:8081", nil, nil); err == nil {
		t.Error("nil signer must fail")
	}
	if _, err := NewHTTP("https://builder.internal/base/", signer, nil); err != nil {
		t.Errorf("valid URL rejected: %v", err)
	}
}

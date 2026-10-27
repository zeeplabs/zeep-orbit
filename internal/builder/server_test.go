package builder

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const srvSecret = "server-test-secret-0123456789abcdef"

// ---- fixture ----

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type srvOpts struct {
	git    func(*GitConfig)
	server func(*ServerConfig)
}

type srvFixture struct {
	t      *testing.T
	srv    *Server
	ts     *httptest.Server
	signer *Signer
	work   string
	tmpl   string // templates dir
	ctl    string // fake npx control files
	logs   *syncBuffer
}

// newSrvFixture builds a Server over the REAL workspace, executor, git and
// signer. Only npm/npx are fake scripts; the "build" step writes dist/ with a
// tiny index.html, and the scripts can be made to block via ctl/block.
func newSrvFixture(t *testing.T, o srvOpts) *srvFixture {
	t.Helper()
	requireGit(t)
	work := t.TempDir()
	tmpl := t.TempDir()
	ctl := t.TempDir()

	def := filepath.Join(tmpl, "default")
	mustWriteFile(t, filepath.Join(def, "package.json"), `{"name":"tpl"}`)
	mustWriteFile(t, filepath.Join(def, "TEMPLATE_VERSION"), "7\n")
	mustWriteFile(t, filepath.Join(def, ".gitignore"), "node_modules/\ndist/\n.orbit/\n")
	mustWriteFile(t, filepath.Join(def, "src", "main.ts"), "export {}\n")
	mustWriteFile(t, filepath.Join(def, "node_modules", "x", "index.js"), "junk")
	mustWriteFile(t, filepath.Join(def, "dist", "old.js"), "junk")
	if err := os.Symlink("/etc/passwd", filepath.Join(def, "link")); err != nil {
		t.Fatal(err)
	}

	npx := fmt.Sprintf(`if [ -f %[1]s/block ]; then : > %[1]s/started; while [ ! -f %[1]s/release ]; do sleep 0.05; done; fi
case "$*" in *vite*)
  rm -rf dist
  mkdir -p dist/assets
  printf '<html>INDEX</html>' > dist/index.html
  printf 'console.log(1)' > dist/assets/a.js
  ;;
esac
if [ -f %[1]s/fail ]; then echo "boom" >&2; exit 3; fi
exit 0`, ctl)
	ex, err := NewExecutor(ExecConfig{
		NpmPath:       writeExecScript(t, ctl, "npm", "exit 0"),
		NpxPath:       writeExecScript(t, ctl, "npx", npx),
		Path:          testExecPath,
		Home:          ctl,
		WorkDir:       work,
		MaxOutputTail: 4096,
		WaitDelay:     200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	gc := GitConfig{HomeDir: filepath.Join(t.TempDir(), "home")}
	if o.git != nil {
		o.git(&gc)
	}
	g, err := NewGitRepo(gc)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner([]byte(srvSecret))
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	cfg := ServerConfig{
		WorkDir:           work,
		TemplatesDir:      tmpl,
		Signer:            signer,
		Executor:          ex,
		Git:               g,
		MaxActiveSessions: 4,
		Version:           "test-1",
		Logger:            slog.New(slog.NewTextHandler(logs, nil)),
	}
	if o.server != nil {
		o.server(&cfg)
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &srvFixture{t: t, srv: srv, ts: ts, signer: signer, work: work, tmpl: tmpl, ctl: ctl, logs: logs}
}

type rq struct {
	method   string
	id       string // session id the token is minted for
	path     string // exactly what is sent, query included
	signPath string // path the token is minted for (default: path without query)
	body     []byte
	ctype    string
	header   map[string]string
	token    *string // override: nil means "mint a valid one"; pointer to "" means none
}

func (f *srvFixture) do(r rq) *http.Response {
	f.t.Helper()
	req, err := http.NewRequest(r.method, f.ts.URL+r.path, bytes.NewReader(r.body))
	if err != nil {
		f.t.Fatal(err)
	}
	if r.ctype != "" {
		req.Header.Set("Content-Type", r.ctype)
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	tok := ""
	if r.token != nil {
		tok = *r.token
	} else {
		sp := r.signPath
		if sp == "" {
			sp, _, _ = strings.Cut(r.path, "?")
		}
		if tok, err = f.signer.SignRequest(r.id, r.method, sp); err != nil {
			f.t.Fatal(err)
		}
	}
	if tok != "" {
		req.Header.Set(TokenHeader, tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp
}

func (f *srvFixture) json(method, id, path string, v any) *http.Response {
	f.t.Helper()
	var body []byte
	if v != nil {
		var err error
		if body, err = json.Marshal(v); err != nil {
			f.t.Fatal(err)
		}
	}
	return f.do(rq{method: method, id: id, path: path, body: body, ctype: "application/json"})
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeBody(t *testing.T, resp *http.Response, want int, v any) {
	t.Helper()
	b := readBody(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, b)
	}
	if v != nil {
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("decode %q: %v", b, err)
		}
	}
}

// wantErr checks the status and the envelope code and returns the message.
func wantErr(t *testing.T, resp *http.Response, status int, code string) string {
	t.Helper()
	var er ErrorResponse
	decodeBody(t, resp, status, &er)
	if er.Error.Code != code {
		t.Fatalf("code = %q, want %q (message %q)", er.Error.Code, code, er.Error.Message)
	}
	if er.Error.Message == "" {
		t.Fatal("empty error message")
	}
	return er.Error.Message
}

func (f *srvFixture) initSession(id string) InitResponse {
	f.t.Helper()
	var out InitResponse
	decodeBody(f.t, f.json("POST", id, "/v1/sessions/"+id+"/init?template=default", nil), 200, &out)
	return out
}

func (f *srvFixture) ops(id, opID, kind string, args OpsArgs) *http.Response {
	return f.json("POST", id, "/v1/sessions/"+id+"/ops", OpsRequest{OpID: opID, Kind: kind, Args: args})
}

func (f *srvFixture) build(id, opID, step, base string) *http.Response {
	return f.json("POST", id, "/v1/sessions/"+id+"/build", BuildRequest{OpID: opID, Step: step, PreviewBase: base})
}

func (f *srvFixture) commit(id string, turn int) CommitResponse {
	f.t.Helper()
	var out CommitResponse
	decodeBody(f.t, f.json("POST", id, "/v1/sessions/"+id+"/commit", CommitRequest{TurnSeq: turn, Message: "t"}), 200, &out)
	return out
}

func (f *srvFixture) root(id string) string { return filepath.Join(f.work, id) }

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	return b
}

func assertPreviewHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": "sandbox allow-scripts allow-forms allow-popups allow-modals",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if xfo := resp.Header.Get("X-Frame-Options"); xfo != "" {
		t.Errorf("X-Frame-Options must be left to the proxy, got %q", xfo)
	}
}

// ---- auth ----

func TestServerRejectsMissingOrWrongToken(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")

	empty := ""
	garbage := "not.a-token"
	other, _ := f.signer.SignRequest("sess-b", "POST", "/v1/sessions/sess-b/ops")
	wrongMethod, _ := f.signer.SignRequest("sess-a", "GET", "/v1/sessions/sess-a/ops")
	wrongPath, _ := f.signer.SignRequest("sess-a", "POST", "/v1/sessions/sess-a/build")
	prefixOnly, _ := f.signer.SignRequest("sess-a", "POST", "/v1/sessions/sess-a/ops/extra")
	previewTok, _ := f.signer.SignPreview("sess-a", "user-1")

	body, _ := json.Marshal(OpsRequest{Kind: "list_dir", Args: OpsArgs{Path: "."}})
	tests := []struct {
		name  string
		token *string
	}{
		{"no header", &empty},
		{"garbage", &garbage},
		{"token for another session", &other},
		{"token for another method", &wrongMethod},
		{"token for another path", &wrongPath},
		{"token for a longer prefix", &prefixOnly},
		{"preview token", &previewTok},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := f.do(rq{method: "POST", id: "sess-a", path: "/v1/sessions/sess-a/ops", body: body, token: tc.token})
			wantErr(t, resp, 401, "unauthorized")
		})
	}
	// A prefix token covering the whole session works on every route of it.
	pre, _ := f.signer.SignRequest("sess-a", "POST", "/v1/sessions/sess-a")
	resp := f.do(rq{method: "POST", id: "sess-a", path: "/v1/sessions/sess-a/ops", body: body, token: &pre})
	decodeBody(t, resp, 200, nil)

	// Unauthenticated routes of every kind are refused, including health.
	for _, p := range [][2]string{{"GET", "/v1/health"}, {"DELETE", "/v1/sessions/sess-a"}, {"GET", "/v1/sessions/sess-a/archive"}} {
		wantErr(t, f.do(rq{method: p[0], path: p[1], token: &empty}), 401, "unauthorized")
	}
	if _, err := os.Stat(f.root("sess-a")); err != nil {
		t.Fatalf("unauthenticated DELETE must not remove the session: %v", err)
	}
}

func TestServerRejectsInvalidSessionIDAfterAuth(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	wantErr(t, f.json("POST", "a.b", "/v1/sessions/a.b/ops", OpsRequest{Kind: "list_dir"}), 400, "invalid_session_id")
}

// ---- happy path, init, restore, revert ----

func TestServerInitOpsCommitHappyPath(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	ini := f.initSession("sess-a")
	if len(ini.SHA) != 40 || ini.TemplateVersion != "7" || len(unb64(t, ini.Bundle)) == 0 {
		t.Fatalf("init response: %+v", ini)
	}
	// The template's protected dirs and symlinks were not copied.
	for _, p := range []string{"node_modules", "dist", "link"} {
		if _, err := os.Lstat(filepath.Join(f.root("sess-a"), p)); err == nil {
			t.Errorf("%s must not be part of the session", p)
		}
	}

	var w OpsResponse
	decodeBody(t, f.ops("sess-a", "op-1", "write_file", OpsArgs{Path: "src/app.ts", Content: "export const a = 1\n"}), 200, &w)
	if !w.OK || w.Replayed {
		t.Fatalf("write: %+v", w)
	}
	var again OpsResponse
	decodeBody(t, f.ops("sess-a", "op-1", "write_file", OpsArgs{Path: "src/app.ts", Content: "export const a = 1\n"}), 200, &again)
	if !again.Replayed {
		t.Fatalf("same op_id must replay: %+v", again)
	}
	var r OpsResponse
	decodeBody(t, f.ops("sess-a", "", "read_file", OpsArgs{Path: "src/app.ts"}), 200, &r)
	if !strings.Contains(r.Output, "export const a = 1") {
		t.Fatalf("read output: %q", r.Output)
	}
	var l OpsResponse
	decodeBody(t, f.ops("sess-a", "", "list_dir", OpsArgs{Path: "."}), 200, &l)
	if strings.Contains(l.Output, "node_modules") || !strings.Contains(l.Output, "src/") {
		t.Fatalf("list output: %q", l.Output)
	}

	c := f.commit("sess-a", 1)
	if !c.Changed || c.Prev != ini.SHA || c.SHA == ini.SHA || len(unb64(t, c.Bundle)) == 0 {
		t.Fatalf("commit: %+v", c)
	}
	idle := f.commit("sess-a", 2)
	if idle.Changed || idle.Bundle != "" || idle.SHA != c.SHA {
		t.Fatalf("second commit without changes: %+v", idle)
	}
}

func TestServerInitTwiceIsAConflict(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	wantErr(t, f.json("POST", "sess-a", "/v1/sessions/sess-a/init?template=default", nil), 409, "session_exists")
	// ...and the first session is untouched.
	decodeBody(t, f.ops("sess-a", "", "list_dir", OpsArgs{Path: "."}), 200, nil)
}

func TestServerInitSourcesAndFailureCleanup(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	tarOf := func(entries ...tarEntry) []byte { return buildTar(t, entries) }

	t.Run("tar upload", func(t *testing.T) {
		body := tarOf(tarEntry{name: "package.json", body: "{}"}, tarEntry{name: "src/a.ts", body: "x"})
		resp := f.do(rq{method: "POST", id: "up-1", path: "/v1/sessions/up-1/init", body: body, ctype: "application/x-tar"})
		var out InitResponse
		decodeBody(t, resp, 200, &out)
		if len(out.SHA) != 40 || out.TemplateVersion != "" || out.Bundle == "" {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("unsafe tar is refused and the session is not left behind", func(t *testing.T) {
		body := tarOf(tarEntry{name: "evil", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"})
		resp := f.do(rq{method: "POST", id: "up-2", path: "/v1/sessions/up-2/init", body: body, ctype: "application/x-tar"})
		wantErr(t, resp, 422, "unsafe_archive_entry")
		if _, err := os.Stat(f.root("up-2")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed init must remove the session dir, stat err = %v", err)
		}
		decodeBody(t, f.json("POST", "up-2", "/v1/sessions/up-2/init?template=default", nil), 200, nil)
	})
	t.Run("media type", func(t *testing.T) {
		resp := f.do(rq{method: "POST", id: "up-3", path: "/v1/sessions/up-3/init", body: []byte("{}"), ctype: "application/json"})
		wantErr(t, resp, 415, "unsupported_media_type")
	})
	t.Run("tar and template together", func(t *testing.T) {
		resp := f.do(rq{method: "POST", id: "up-4", path: "/v1/sessions/up-4/init?template=default", ctype: "application/x-tar"})
		wantErr(t, resp, 400, "invalid_request")
	})
	t.Run("template names", func(t *testing.T) {
		wantErr(t, f.json("POST", "up-5", "/v1/sessions/up-5/init?template=..%2Fx", nil), 422, "invalid_template")
		wantErr(t, f.json("POST", "up-5", "/v1/sessions/up-5/init?template=Default", nil), 422, "invalid_template")
		wantErr(t, f.json("POST", "up-5", "/v1/sessions/up-5/init?template=nope", nil), 404, "unknown_template")
		if _, err := os.Stat(f.root("up-5")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a rejected template must not create the session")
		}
	})
	t.Run("template that is a symlink is not followed", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(f.tmpl, "default"), filepath.Join(f.tmpl, "linked")); err != nil {
			t.Fatal(err)
		}
		wantErr(t, f.json("POST", "up-6", "/v1/sessions/up-6/init?template=linked", nil), 404, "unknown_template")
	})
	t.Run("stale half-created session is replaced, a real one is not", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(f.root("stale"), ".orbit"), 0o700); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(f.root("stale"), "leftover.txt"), "x")
		decodeBody(t, f.json("POST", "stale", "/v1/sessions/stale/init?template=default", nil), 200, nil)
		wantErr(t, f.json("POST", "stale", "/v1/sessions/stale/init?template=default", nil), 409, "session_exists")
	})
}

func TestServerInitRefusesOversizedBodies(t *testing.T) {
	f := newSrvFixture(t, srvOpts{git: func(c *GitConfig) {
		c.Tar = TarLimits{MaxTotalBytes: 1 << 10, MaxFileBytes: 1 << 10, MaxFiles: 10}
	}})
	// Both a body far beyond the derived cap (junk) and a valid tar whose
	// entry exceeds the tar limits are refused, and neither leaves a session.
	junk := bytes.Repeat([]byte("a"), 3<<20)
	resp := f.do(rq{method: "POST", id: "big", path: "/v1/sessions/big/init", body: junk, ctype: "application/x-tar"})
	if resp.StatusCode != 413 && resp.StatusCode != 422 {
		t.Fatalf("junk body: status %d: %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()
	big := buildTar(t, []tarEntry{{name: "big.bin", body: strings.Repeat("x", 4<<10)}})
	resp = f.do(rq{method: "POST", id: "big", path: "/v1/sessions/big/init", body: big, ctype: "application/x-tar"})
	wantErr(t, resp, 413, "archive_too_large")
	if _, err := os.Stat(f.root("big")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("oversized init must not leave a session behind")
	}
}

func TestServerRestoreReproducesHeadInANewSession(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	ini := f.initSession("sess-a")
	decodeBody(t, f.ops("sess-a", "w1", "write_file", OpsArgs{Path: "src/app.ts", Content: "const restored = true\n"}), 200, nil)
	c := f.commit("sess-a", 1)

	req := RestoreRequest{Bundles: []RestoreBundle{
		{Seq: 0, SHA: ini.SHA, Data: unb64(t, ini.Bundle)},
		{Seq: 1, SHA: c.SHA, Data: unb64(t, c.Bundle)},
	}}
	var out RestoreResponse
	decodeBody(t, f.json("POST", "sess-b", "/v1/sessions/sess-b/restore", req), 200, &out)
	if out.SHA != c.SHA {
		t.Fatalf("restored head %s, want %s", out.SHA, c.SHA)
	}
	var r OpsResponse
	decodeBody(t, f.ops("sess-b", "", "read_file", OpsArgs{Path: "src/app.ts"}), 200, &r)
	if !strings.Contains(r.Output, "const restored = true") {
		t.Fatalf("restored file: %q", r.Output)
	}
	// Restoring over an existing session is a conflict.
	wantErr(t, f.json("POST", "sess-b", "/v1/sessions/sess-b/restore", req), 409, "session_exists")

	// A corrupt bundle fails with 422 and leaves nothing behind, so it can be retried.
	bad := RestoreRequest{Bundles: []RestoreBundle{{Seq: 0, Data: []byte("not a bundle")}}}
	wantErr(t, f.json("POST", "sess-c", "/v1/sessions/sess-c/restore", bad), 422, "bundle_verify_failed")
	if _, err := os.Stat(f.root("sess-c")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed restore must remove the session dir")
	}
	decodeBody(t, f.json("POST", "sess-c", "/v1/sessions/sess-c/restore", req), 200, nil)

	// Wire-level problems.
	resp := f.do(rq{method: "POST", id: "sess-d", path: "/v1/sessions/sess-d/restore",
		body: []byte(`{"bundles":[{"seq":0,"data":"***not base64***"}]}`), ctype: "application/json"})
	wantErr(t, resp, 400, "invalid_json")
}

func TestServerRevert(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	ini := f.initSession("sess-a")
	decodeBody(t, f.ops("sess-a", "w1", "write_file", OpsArgs{Path: "src/new.ts", Content: "x\n"}), 200, nil)
	c := f.commit("sess-a", 1)

	var rv CommitResponse
	decodeBody(t, f.json("POST", "sess-a", "/v1/sessions/sess-a/revert", RevertRequest{ToSHA: ini.SHA, TurnSeq: 2}), 200, &rv)
	if !rv.Changed || rv.Prev != c.SHA || rv.SHA == c.SHA || rv.Bundle == "" {
		t.Fatalf("revert: %+v", rv)
	}
	if _, err := os.Stat(filepath.Join(f.root("sess-a"), "src", "new.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("revert must remove files absent from the target")
	}
	wantErr(t, f.json("POST", "sess-a", "/v1/sessions/sess-a/revert", RevertRequest{ToSHA: "abc"}), 422, "invalid_sha")
	wantErr(t, f.json("POST", "sess-a", "/v1/sessions/sess-a/revert", RevertRequest{ToSHA: strings.Repeat("a", 40)}), 422, "unknown_commit")
}

func TestServerArchive(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	ini := f.initSession("sess-a")
	decodeBody(t, f.ops("sess-a", "w1", "write_file", OpsArgs{Path: "src/app.ts", Content: "archived\n"}), 200, nil)
	f.commit("sess-a", 1)

	read := func(query string) map[string]string {
		resp := f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/archive" + query})
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-tar" {
			t.Fatalf("status %d type %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), readBody(t, resp))
		}
		files := map[string]string{}
		tr := tar.NewReader(resp.Body)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(tr)
			files[h.Name] = string(b)
		}
		resp.Body.Close()
		return files
	}
	head := read("")
	if head["src/app.ts"] != "archived\n" || head["package.json"] == "" {
		t.Fatalf("HEAD archive: %v", keys(head))
	}
	if _, ok := read("?sha=" + ini.SHA)["src/app.ts"]; ok {
		t.Fatal("archive of the first commit must not contain later files")
	}
	wantErr(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/archive?sha=zzz"}), 422, "invalid_sha")
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- sessions that do not exist ----

func TestServerUnknownSessionIs404(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	routes := []struct{ method, path string }{
		{"POST", "/v1/sessions/ghost/ops"},
		{"POST", "/v1/sessions/ghost/build"},
		{"GET", "/v1/sessions/ghost/build-log?step=build"},
		{"POST", "/v1/sessions/ghost/commit"},
		{"POST", "/v1/sessions/ghost/revert"},
		{"GET", "/v1/sessions/ghost/archive"},
		{"GET", "/v1/sessions/ghost/preview/"},
		{"GET", "/v1/sessions/ghost/preview/index.html"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			resp := f.do(rq{method: rt.method, id: "ghost", path: rt.path, body: []byte(`{}`), ctype: "application/json"})
			if strings.Contains(rt.path, "/preview") {
				assertPreviewHeaders(t, resp)
			}
			wantErr(t, resp, 404, "unknown_session")
		})
	}
}

func TestServerDeleteIsIdempotent(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	decodeBody(t, f.ops("sess-a", "w1", "write_file", OpsArgs{Path: "a.txt", Content: "x"}), 200, nil)
	for i := 0; i < 2; i++ {
		resp := f.json("DELETE", "sess-a", "/v1/sessions/sess-a", nil)
		if resp.StatusCode != 204 {
			t.Fatalf("delete #%d: status %d: %s", i+1, resp.StatusCode, readBody(t, resp))
		}
		resp.Body.Close()
	}
	if _, err := os.Stat(f.root("sess-a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("session dir must be gone")
	}
	wantErr(t, f.ops("sess-a", "", "list_dir", OpsArgs{Path: "."}), 404, "unknown_session")
	// ...and the id can be used again (Orbit restores after volume loss).
	f.initSession("sess-a")
}

// ---- ops ----

func TestServerOpsErrorMapping(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	decodeBody(t, f.ops("sess-a", "w1", "write_file", OpsArgs{Path: "src/a.ts", Content: "hello\n"}), 200, nil)
	if err := os.WriteFile(filepath.Join(f.root("sess-a"), "bin.dat"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		opID   string
		kind   string
		args   OpsArgs
		status int
		code   string
		msg    string // substring the LLM must be able to read
	}{
		{"edit not found", "e1", "edit_file", OpsArgs{Path: "src/a.ts", OldString: "nope", NewString: "x"}, 422, "edit_not_found", "old_string not found"},
		{"edit empty old", "e2", "edit_file", OpsArgs{Path: "src/a.ts", NewString: "x"}, 422, "edit_empty_old", "must not be empty"},
		{"edit noop", "e3", "edit_file", OpsArgs{Path: "src/a.ts", OldString: "a", NewString: "a"}, 422, "edit_noop", "identical"},
		{"traversal", "", "read_file", OpsArgs{Path: "../../etc/passwd"}, 403, "path_outside", "outside"},
		{"absolute", "", "read_file", OpsArgs{Path: "/etc/passwd"}, 403, "path_outside", ""},
		{"git dir", "", "read_file", OpsArgs{Path: ".git/config"}, 403, "path_forbidden", "protected"},
		{"node_modules", "", "list_dir", OpsArgs{Path: "node_modules"}, 403, "path_forbidden", ""},
		{"orbit dir", "", "read_file", OpsArgs{Path: ".orbit/ops.jsonl"}, 403, "path_forbidden", ""},
		{"empty path", "", "read_file", OpsArgs{}, 422, "invalid_path", ""},
		{"unknown kind", "", "rm_rf", OpsArgs{}, 422, "unknown_op", "unknown operation kind"},
		{"write without op_id", "", "write_file", OpsArgs{Path: "x.txt", Content: "x"}, 422, "invalid_argument", "op_id"},
		{"missing file", "", "read_file", OpsArgs{Path: "src/missing.ts"}, 404, "not_found", ""},
		{"directory", "", "read_file", OpsArgs{Path: "src"}, 422, "is_directory", ""},
		{"binary", "", "read_file", OpsArgs{Path: "bin.dat"}, 422, "binary_file", ""},
		{"op id reused", "w1", "write_file", OpsArgs{Path: "src/other.ts", Content: "x"}, 409, "op_id_reused", ""},
		{"file too large", "big", "write_file", OpsArgs{Path: "big.txt", Content: strings.Repeat("a", 600<<10)}, 413, "file_too_large", ""},
		{"invalid content", "nul", "write_file", OpsArgs{Path: "nul.txt", Content: "a\x00b"}, 422, "invalid_content", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := wantErr(t, f.ops("sess-a", tc.opID, tc.kind, tc.args), tc.status, tc.code)
			if tc.msg != "" && !strings.Contains(msg, tc.msg) {
				t.Fatalf("message %q does not contain %q", msg, tc.msg)
			}
			if strings.Contains(msg, f.work) {
				t.Fatalf("message leaks a host path: %q", msg)
			}
		})
	}

	t.Run("oversized body", func(t *testing.T) {
		wantErr(t, f.ops("sess-a", "huge", "write_file", OpsArgs{Path: "h.txt", Content: strings.Repeat("a", 3<<20)}), 413, "body_too_large")
	})
	t.Run("malformed JSON", func(t *testing.T) {
		resp := f.do(rq{method: "POST", id: "sess-a", path: "/v1/sessions/sess-a/ops", body: []byte(`{"kind":`), ctype: "application/json"})
		wantErr(t, resp, 400, "invalid_json")
	})
	t.Run("unknown field and trailing data", func(t *testing.T) {
		for _, b := range []string{`{"kind":"list_dir","bogus":1}`, `{"kind":"list_dir"} {"x":1}`} {
			resp := f.do(rq{method: "POST", id: "sess-a", path: "/v1/sessions/sess-a/ops", body: []byte(b), ctype: "application/json"})
			wantErr(t, resp, 400, "invalid_json")
		}
	})
}

func TestServerInternalErrorsAreGenericAndLogged(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	// An ops.jsonl that is a directory makes loading the idempotency log fail
	// with an infrastructure error (ErrOpLog) whose cause holds a host path.
	root := f.root("sess-a")
	if err := os.MkdirAll(filepath.Join(root, ".orbit", "ops.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	msg := wantErr(t, f.ops("sess-a", "", "list_dir", OpsArgs{Path: "."}), 500, "internal_error")
	if msg != "internal error" || strings.Contains(msg, root) {
		t.Fatalf("5xx message must be generic, got %q", msg)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "builder request failed") || !strings.Contains(logs, "internal_error") {
		t.Fatalf("a 5xx must be logged server side:\n%s", logs)
	}
}

// ---- build, build-log, preview ----

func TestServerBuildValidationAndLog(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")

	wantErr(t, f.build("sess-a", "b1", "rm -rf /", "/p/"), 422, "unknown_step")
	wantErr(t, f.build("sess-a", "b1", "build", "https://evil.example/"), 422, "invalid_preview_base")
	wantErr(t, f.build("sess-a", "b1", "build", ""), 422, "invalid_preview_base")
	wantErr(t, f.build("sess-a", "", "typecheck", ""), 422, "invalid_argument")

	wantErr(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/build-log?step=build"}), 404, "no_build_log")
	wantErr(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/build-log?step=bogus"}), 422, "unknown_step")
	wantErr(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/build-log"}), 422, "unknown_step")
	wantErr(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/build-log?step=build&tail=-1"}), 422, "invalid_argument")

	// A failing step is a normal 200 result and its output is readable later.
	if err := os.WriteFile(filepath.Join(f.ctl, "fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var b BuildResponse
	decodeBody(t, f.build("sess-a", "b2", "typecheck", ""), 200, &b)
	if b.ExitCode != 3 || !strings.Contains(b.OutputTail, "boom") || b.Replayed {
		t.Fatalf("failing step: %+v", b)
	}
	var replay BuildResponse
	decodeBody(t, f.build("sess-a", "b2", "typecheck", ""), 200, &replay)
	if !replay.Replayed || replay.ExitCode != 3 {
		t.Fatalf("replay: %+v", replay)
	}
	wantErr(t, f.build("sess-a", "b2", "lint", ""), 409, "op_id_reused")
	var lg BuildLogResponse
	decodeBody(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/build-log?step=typecheck&tail=100"}), 200, &lg)
	if !strings.Contains(lg.Output, "boom") {
		t.Fatalf("build log: %q", lg.Output)
	}
}

func TestServerPreviewBeforeAndAfterBuild(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	prev := func(path string, hdr map[string]string) *http.Response {
		return f.do(rq{method: "GET", id: "sess-a", path: "/v1/sessions/sess-a/preview" + path, header: hdr})
	}

	// Before the first successful build: 404 no_preview, still with the headers.
	resp := prev("/", nil)
	assertPreviewHeaders(t, resp)
	wantErr(t, resp, 404, "no_preview")

	var b BuildResponse
	decodeBody(t, f.build("sess-a", "b1", "build", "/preview-base/"), 200, &b)
	if b.ExitCode != 0 {
		t.Fatalf("build: %+v", b)
	}

	resp = prev("/", nil)
	assertPreviewHeaders(t, resp)
	if body := readBody(t, resp); resp.StatusCode != 200 || string(body) != "<html>INDEX</html>" ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("index: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	resp = prev("", nil) // without the trailing slash
	assertPreviewHeaders(t, resp)
	if body := readBody(t, resp); resp.StatusCode != 200 || string(body) != "<html>INDEX</html>" {
		t.Fatalf("index without slash: %d %q", resp.StatusCode, body)
	}
	resp = prev("/assets/a.js", nil)
	assertPreviewHeaders(t, resp)
	if body := readBody(t, resp); resp.StatusCode != 200 || string(body) != "console.log(1)" ||
		!strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("asset: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	// SPA fallback: deep links without an extension get the app shell.
	for _, p := range []string{"/deep/link", "/deep/link/", "/assets"} {
		resp = prev(p, nil)
		assertPreviewHeaders(t, resp)
		if body := readBody(t, resp); resp.StatusCode != 200 || string(body) != "<html>INDEX</html>" {
			t.Fatalf("fallback %s: %d %q", p, resp.StatusCode, body)
		}
	}
	// A missing asset stays a 404 (no HTML masquerading as a script)...
	resp = prev("/assets/missing.js", map[string]string{"Accept": "*/*"})
	assertPreviewHeaders(t, resp)
	wantErr(t, resp, 404, "not_found")
	// ...unless it is a browser navigation.
	resp = prev("/assets/missing.js", map[string]string{"Accept": "text/html,application/xhtml+xml"})
	if body := readBody(t, resp); resp.StatusCode != 200 || string(body) != "<html>INDEX</html>" {
		t.Fatalf("navigation fallback: %d %q", resp.StatusCode, body)
	}

	// A later failing build keeps serving the last good preview.
	if err := os.WriteFile(filepath.Join(f.ctl, "fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	decodeBody(t, f.build("sess-a", "b2", "build", "/preview-base/"), 200, &b)
	if b.ExitCode == 0 {
		t.Fatal("expected a failing build")
	}
	resp = prev("/", nil)
	if body := readBody(t, resp); resp.StatusCode != 200 || string(body) != "<html>INDEX</html>" {
		t.Fatalf("preview after failed build: %d %q", resp.StatusCode, body)
	}
}

func TestServerPreviewRejectsTraversalAndSymlinks(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	decodeBody(t, f.build("sess-a", "b1", "build", "/p/"), 200, nil)
	root := f.root("sess-a")
	secret := filepath.Join(root, "secret.txt")
	mustWriteFile(t, secret, "TOP-SECRET")
	preview := PreviewDir(root)

	// Symlinks planted inside the preview dir (a build tool could do this).
	if err := os.Symlink(secret, filepath.Join(preview, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../secret.txt", filepath.Join(preview, "rel.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(preview, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(preview, "index2.html")); err != nil {
		t.Fatal(err)
	}

	get := func(path string, hdr map[string]string) (*http.Response, string) {
		resp := f.do(rq{method: "GET", id: "sess-a", path: path, signPath: "/v1/sessions/sess-a/preview", header: hdr})
		return resp, string(readBody(t, resp))
	}
	base := "/v1/sessions/sess-a/preview"
	t.Run("traversal", func(t *testing.T) {
		for _, p := range []string{
			base + "/../../secret.txt",
			base + "/../secret.txt",
			base + "/%2e%2e/%2e%2e/secret.txt",
			base + "/..%2f..%2fsecret.txt",
			base + "/%2e%2e%2f%2e%2e%2fsecret.txt",
			base + "/assets/../../../secret.txt",
			base + "//etc/passwd",
			base + "/..%5c..%5csecret.txt",
			base + "/a%00b",
		} {
			resp, body := get(p, nil)
			if resp.StatusCode == 200 || strings.Contains(body, "TOP-SECRET") || strings.Contains(body, "root:") {
				t.Errorf("%s: status %d body %q", p, resp.StatusCode, body)
			}
			if resp.StatusCode != 400 && resp.StatusCode != 404 {
				t.Errorf("%s: unexpected status %d", p, resp.StatusCode)
			}
		}
	})
	t.Run("symlinks are not followed", func(t *testing.T) {
		for _, p := range []string{"/leak.txt", "/rel.txt", "/linkdir/secret.txt", "/index2.html"} {
			resp, body := get(base+p, nil)
			assertPreviewHeaders(t, resp)
			if resp.StatusCode != 404 || strings.Contains(body, "TOP-SECRET") || strings.Contains(body, "localhost") {
				t.Errorf("%s: status %d body %q", p, resp.StatusCode, body)
			}
			// Even as a navigation the answer is the app shell, never the target.
			resp, body = get(base+p, map[string]string{"Accept": "text/html"})
			if body != "<html>INDEX</html>" {
				t.Errorf("%s (navigation): status %d body %q", p, resp.StatusCode, body)
			}
		}
		resp, body := get(base+"/linkdir", nil)
		if body != "<html>INDEX</html>" {
			t.Errorf("symlinked directory: status %d body %q", resp.StatusCode, body)
		}
	})
	t.Run("preview directory itself being a symlink", func(t *testing.T) {
		if err := os.RemoveAll(preview); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(root, "elsewhere")
		mustWriteFile(t, filepath.Join(other, "index.html"), "TOP-SECRET")
		if err := os.Symlink(other, preview); err != nil {
			t.Fatal(err)
		}
		resp, body := get(base+"/", nil)
		if resp.StatusCode != 404 || strings.Contains(body, "TOP-SECRET") {
			t.Fatalf("status %d body %q", resp.StatusCode, body)
		}
	})
}

// ---- capacity ----

func TestServerCapacityReturns429ForANewSessionOnly(t *testing.T) {
	f := newSrvFixture(t, srvOpts{server: func(c *ServerConfig) { c.MaxActiveSessions = 1 }})
	f.initSession("sess-a")
	f.initSession("sess-b")

	// Hold sess-a active with a build that blocks inside the fake npx.
	if err := os.WriteFile(filepath.Join(f.ctl, "block"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan *http.Response, 1)
	go func() { done <- f.build("sess-a", "slow", "build", "/p/") }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(f.ctl, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the build never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	health := func() HealthResponse {
		var h HealthResponse
		decodeBody(t, f.do(rq{method: "GET", id: HealthSessionID, path: "/v1/health"}), 200, &h)
		return h
	}
	if h := health(); h.Active != 1 || h.Capacity != 1 {
		t.Fatalf("health while busy: %+v", h)
	}

	resp := f.ops("sess-b", "", "list_dir", OpsArgs{Path: "."})
	if resp.Header.Get("Retry-After") != "5" {
		t.Errorf("Retry-After = %q", resp.Header.Get("Retry-After"))
	}
	wantErr(t, resp, 429, "at_capacity")
	wantErr(t, f.build("sess-b", "x", "typecheck", ""), 429, "at_capacity")
	wantErr(t, f.json("POST", "sess-c", "/v1/sessions/sess-c/init?template=default", nil), 429, "at_capacity")
	if _, err := os.Stat(f.root("sess-c")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused init must not create the session")
	}

	// The active session is always admitted; commit is never refused.
	decodeBody(t, f.ops("sess-a", "", "list_dir", OpsArgs{Path: "."}), 200, nil)
	f.commit("sess-b", 1)

	if err := os.WriteFile(filepath.Join(f.ctl, "release"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var b BuildResponse
	decodeBody(t, <-done, 200, &b)
	if b.ExitCode != 0 {
		t.Fatalf("blocked build: %+v", b)
	}
	// Capacity frees up again.
	decodeBody(t, f.ops("sess-b", "", "list_dir", OpsArgs{Path: "."}), 200, nil)
	if h := health(); h.Active != 0 {
		t.Fatalf("health when idle: %+v", h)
	}
}

func TestActiveTracker(t *testing.T) {
	tr := &activeTracker{capacity: 2, counts: map[string]int{}}
	relA, n, ok := tr.acquire("a", true)
	if !ok || n != 1 {
		t.Fatal("first acquire")
	}
	relA2, n, ok := tr.acquire("a", true)
	if !ok || n != 2 {
		t.Fatal("same session is always admitted")
	}
	relB, _, ok := tr.acquire("b", true)
	if !ok {
		t.Fatal("second session fits")
	}
	if _, _, ok := tr.acquire("c", true); ok {
		t.Fatal("third session must be refused")
	}
	relC, _, ok := tr.acquire("c", false)
	if !ok || tr.count() != 3 {
		t.Fatal("non-enforcing acquire counts but is never refused")
	}
	relC()
	relC() // releasing twice must not corrupt the count
	relB()
	relA()
	if tr.count() != 1 {
		t.Fatalf("count = %d, want 1", tr.count())
	}
	relA2()
	if tr.count() != 0 {
		t.Fatalf("count = %d, want 0", tr.count())
	}
}

// ---- health, logging, middleware ----

func TestServerHealth(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	wantErr(t, f.do(rq{method: "GET", id: "-", path: "/v1/health", token: new(string)}), 401, "unauthorized")
	// A token minted for a real session id does not open health.
	wantErr(t, f.do(rq{method: "GET", id: "sess-a", path: "/v1/health"}), 401, "unauthorized")
	var h HealthResponse
	decodeBody(t, f.do(rq{method: "GET", id: HealthSessionID, path: "/v1/health"}), 200, &h)
	if h != (HealthResponse{Active: 0, Capacity: 4, Version: "test-1", DefaultTemplateVersion: "7"}) {
		t.Fatalf("health: %+v", h)
	}
}

func TestServerRequestLogHasNoSecrets(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	f.initSession("sess-a")
	tok, _ := f.signer.SignRequest("sess-a", "POST", "/v1/sessions/sess-a/ops")
	body, _ := json.Marshal(OpsRequest{OpID: "w1", Kind: "write_file", Args: OpsArgs{Path: "a.txt", Content: "SECRET-FILE-CONTENT"}})
	resp := f.do(rq{method: "POST", id: "sess-a", path: "/v1/sessions/sess-a/ops?x=SECRET-QUERY", body: body, token: &tok, signPath: "/v1/sessions/sess-a/ops"})
	decodeBody(t, resp, 200, nil)

	logs := f.logs.String()
	for _, want := range []string{`route="POST /v1/sessions/{id}/ops"`, "session=sess-a", "status=200", "duration="} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %q:\n%s", want, logs)
		}
	}
	for _, bad := range []string{tok, "SECRET-FILE-CONTENT", "SECRET-QUERY", srvSecret} {
		if strings.Contains(logs, bad) {
			t.Errorf("log leaks %q", bad)
		}
	}
}

func TestServerMiddlewareRecoversPanicsWithoutLeaking(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	h := f.srv.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom /secret/host/path") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/health", nil))
	var er ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &er); err != nil || rec.Code != 500 || er.Error.Code != "internal_error" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("panic value leaked to the client")
	}
	if !strings.Contains(f.logs.String(), "kaboom") {
		t.Fatal("panic must be logged")
	}
}

func TestServerUnknownRouteIsJSON404(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	resp, err := http.Get(f.ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	wantErr(t, resp, 404, "not_found")
}

func TestNewServerValidatesConfig(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	base := ServerConfig{WorkDir: f.work, Signer: f.signer, Executor: f.srv.exec, Git: f.srv.git}
	tests := []struct {
		name string
		mut  func(*ServerConfig)
	}{
		{"no signer", func(c *ServerConfig) { c.Signer = nil }},
		{"no executor", func(c *ServerConfig) { c.Executor = nil }},
		{"no git", func(c *ServerConfig) { c.Git = nil }},
		{"relative work dir", func(c *ServerConfig) { c.WorkDir = "work" }},
		{"missing work dir", func(c *ServerConfig) { c.WorkDir = filepath.Join(f.work, "nope") }},
		{"registry elsewhere", func(c *ServerConfig) { c.Registry = NewWorkspaceRegistry(t.TempDir(), WorkspaceLimits{}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)
			if _, err := NewServer(cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	s, err := NewServer(base)
	if err != nil {
		t.Fatal(err)
	}
	if s.capacity != DefaultMaxActiveSessions || s.version != "dev" {
		t.Fatalf("defaults: capacity %d version %q", s.capacity, s.version)
	}
}

// ---- units ----

func TestMapError(t *testing.T) {
	var mbe error = &http.MaxBytesError{Limit: 1}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		msg    string // exact message when non-empty
	}{
		{"edit not found", opErr(ErrEditNotFound, ""), 422, "edit_not_found", ""},
		{"edit not unique", &OpError{Err: ErrEditNotUnique, Msg: "old_string is not unique (3 matches)"}, 422, "edit_not_unique", "old_string is not unique (3 matches)"},
		{"edit empty", opErr(ErrEditEmptyOld, ""), 422, "edit_empty_old", ""},
		{"edit noop", opErr(ErrEditNoop, ""), 422, "edit_noop", ""},
		{"invalid path", opErr(ErrInvalidPath, ""), 422, "invalid_path", ""},
		{"invalid argument", fmt.Errorf("x: %w", ErrInvalidArgument), 422, "invalid_argument", ""},
		{"unknown op", opErr(ErrUnknownOp, ""), 422, "unknown_op", ""},
		{"invalid content", opErr(ErrInvalidContent, "a"), 422, "invalid_content", ""},
		{"binary", opErr(ErrBinaryFile, "a"), 422, "binary_file", ""},
		{"is dir", opErr(ErrIsDirectory, "a"), 422, "is_directory", ""},
		{"not dir", opErr(ErrNotDirectory, "a"), 422, "not_directory", ""},
		{"not regular", opErr(ErrNotRegular, "a"), 422, "not_regular", ""},
		{"unknown step", opErr(ErrUnknownStep, ""), 422, "unknown_step", ""},
		{"preview base", opErr(ErrInvalidPreviewBase, ""), 422, "invalid_preview_base", ""},
		{"invalid sha", &GitError{Err: ErrInvalidSHA}, 422, "invalid_sha", ""},
		{"unknown commit", &GitError{Err: ErrUnknownCommit}, 422, "unknown_commit", ""},
		{"not ancestor", &GitError{Err: ErrNotAncestor}, 422, "not_ancestor", ""},
		{"invalid archive", &GitError{Err: ErrInvalidArchive}, 422, "invalid_archive", ""},
		{"unsafe entry", &GitError{Err: ErrUnsafeArchiveEntry}, 422, "unsafe_archive_entry", ""},
		{"bundle verify", &GitError{Err: ErrBundleVerify, Op: "restore"}, 422, "bundle_verify_failed", ""},
		{"unsafe content", &GitError{Err: ErrUnsafeContent}, 422, "unsafe_content", ""},
		{"outside", opErr(ErrPathOutside, "../x"), 403, "path_outside", ""},
		{"forbidden", opErr(ErrPathForbidden, ".git"), 403, "path_forbidden", ""},
		{"symlink", opErr(ErrSymlink, "l"), 403, "symlink_forbidden", ""},
		{"not found", opErr(ErrNotFound, "a"), 404, "not_found", ""},
		{"session not found", ErrSessionNotFound, 404, "unknown_session", ""},
		{"no repo", &GitError{Err: ErrNoRepo}, 404, "unknown_session", ""},
		{"invalid session id", ErrInvalidSessionID, 400, "invalid_session_id", ""},
		{"file too large", opErr(ErrFileTooLarge, "a"), 413, "file_too_large", ""},
		{"session too large", opErr(ErrSessionTooLarge, ""), 413, "session_too_large", ""},
		{"archive too large", &GitError{Err: ErrArchiveTooLarge}, 413, "archive_too_large", ""},
		{"bundle too large", &GitError{Err: ErrBundleTooLarge}, 413, "bundle_too_large", ""},
		{"body too large", fmt.Errorf("read: %w", mbe), 413, "body_too_large", "request body is too large"},
		{"op id reused", opErr(ErrOpIDReused, ""), 409, "op_id_reused", ""},
		{"repo exists", &GitError{Err: ErrRepoExists}, 409, "session_exists", ""},
		{"root not empty", &GitError{Err: ErrRootNotEmpty}, 409, "session_exists", ""},
		{"api error", badRequest("invalid_request", "nope"), 400, "invalid_request", "nope"},
		{"deadline", fmt.Errorf("wait: %w", context.DeadlineExceeded), 504, "timeout", ""},
		{"git timeout", &GitError{Err: ErrGitTimeout, Op: "bundle create"}, 504, "timeout", "the operation timed out"},
		{"canceled", context.Canceled, 503, "canceled", ""},
		{"io hides cause", &OpError{Err: ErrIO, Path: "p", cause: errors.New("open /srv/work/x: permission denied")}, 500, "internal_error", "internal error"},
		{"op log", &OpError{Err: ErrOpLog, cause: errors.New("disk full at /srv/work")}, 500, "internal_error", "internal error"},
		{"git failed hides stderr", gitErr(ErrGitFailed, "commit-tree", errors.New("fatal: /srv/work/x")), 500, "internal_error", "internal error"},
		{"exec start", &OpError{Err: ErrExecStart, cause: errors.New("fork /usr/bin/npm")}, 500, "internal_error", "internal error"},
		{"unknown", errors.New("boom at /secret/path"), 500, "internal_error", "internal error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, code, msg := mapError(tc.err)
			if status != tc.status || code != tc.code {
				t.Fatalf("got %d %s, want %d %s", status, code, tc.status, tc.code)
			}
			if msg == "" || (tc.msg != "" && msg != tc.msg) {
				t.Fatalf("message %q (want %q)", msg, tc.msg)
			}
			if status >= 500 && (strings.Contains(msg, "/") || msg != "internal error" && status == 500) {
				t.Fatalf("5xx message must be generic, got %q", msg)
			}
			if code != strings.ToLower(code) || strings.ContainsAny(code, " -") {
				t.Fatalf("code %q is not snake_case", code)
			}
		})
	}
}

func TestHasUncleanPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/":                       false,
		"/v1/health":              false,
		"/v1/sessions/a/preview/": false,
		"/a/../b":                 true,
		"/a/./b":                  true,
		"/a//b":                   true,
		"//a":                     true,
		"/a/..":                   true,
		"/a\\b":                   true,
		"/a\x00b":                 true,
		"":                        true,
		"a":                       true,
	} {
		if got := hasUncleanPath(p); got != want {
			t.Errorf("hasUncleanPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestSplitPreviewPath(t *testing.T) {
	good := map[string][]string{
		"":              nil,
		"a":             {"a"},
		"a/b.js":        {"a", "b.js"},
		"a/b/":          {"a", "b"},
		"assets/x-1.js": {"assets", "x-1.js"},
	}
	for in, want := range good {
		got, ok := splitPreviewPath(in)
		if !ok || strings.Join(got, "/") != strings.Join(want, "/") {
			t.Errorf("splitPreviewPath(%q) = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"..", "../a", "a/..", "a/../b", "./a", "a//b", "/a", "a\\b", "a\x00", "a\nb", strings.Repeat("a", 2000), strings.Repeat("a", 300)} {
		if _, ok := splitPreviewPath(in); ok {
			t.Errorf("splitPreviewPath(%q) must be rejected", in)
		}
	}
}

func TestTarDirectorySkipsProtectedAndNonRegular(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "A")
	mustWriteFile(t, filepath.Join(dir, "sub", "b.sh"), "B")
	if err := os.Chmod(filepath.Join(dir, "sub", "b.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "node_modules", "x.js"), "x")
	mustWriteFile(t, filepath.Join(dir, "sub", "node_modules", "y.js"), "y")
	mustWriteFile(t, filepath.Join(dir, ".git", "config"), "g")
	mustWriteFile(t, filepath.Join(dir, "dist", "d.js"), "d")
	mustWriteFile(t, filepath.Join(dir, ".orbit", "ops.jsonl"), "o")
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "sub", "loop")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := tarDirectory(dir, &buf); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		got[h.Name] = fmt.Sprintf("%c %o %s", h.Typeflag, h.Mode, b)
	}
	want := map[string]string{
		"a.txt":    "0 644 A",
		"sub/":     "5 755 ",
		"sub/b.sh": "0 755 B",
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestReadVersionFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		mustWriteFile(t, p, content)
		return p
	}
	if got := readVersionFile(write("ok", " 1.2.3-rc1\n")); got != "1.2.3-rc1" {
		t.Errorf("ok = %q", got)
	}
	for name, content := range map[string]string{
		"empty":   "\n",
		"spaces":  "a b",
		"markup":  "<script>",
		"toolong": strings.Repeat("1", 200),
	} {
		if got := readVersionFile(write(name, content)); got != "" {
			t.Errorf("%s = %q, want empty", name, got)
		}
	}
	if got := readVersionFile(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing = %q", got)
	}
	if err := os.Symlink(filepath.Join(dir, "ok"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if got := readVersionFile(filepath.Join(dir, "link")); got != "" {
		t.Errorf("symlink must not be followed, got %q", got)
	}
}

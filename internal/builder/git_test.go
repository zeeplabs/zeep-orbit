package builder

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

func newTestGitRepo(t *testing.T, mutate func(*GitConfig)) *GitRepo {
	t.Helper()
	requireGit(t)
	cfg := GitConfig{HomeDir: filepath.Join(t.TempDir(), "home")}
	if mutate != nil {
		mutate(&cfg)
	}
	g, err := NewGitRepo(cfg)
	if err != nil {
		t.Fatalf("NewGitRepo: %v", err)
	}
	return g
}

type tarEntry struct {
	name     string
	typeflag byte
	body     string
	mode     int64
	linkname string
}

func buildTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: tf, Mode: mode, Linkname: e.linkname}
		if tf == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %q: %v", e.name, err)
		}
		if tf == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func defaultTemplate(t *testing.T) []byte {
	return buildTar(t, []tarEntry{
		{name: "./", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./package.json", body: "{\"name\":\"app\"}\n"},
		{name: "./index.html", body: "<html></html>\n"},
		{name: "./src/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./src/main.tsx", body: "console.log('hi')\n"},
		{name: "./scripts/run.sh", body: "#!/bin/sh\n", mode: 0o755},
		{name: "./.gitignore", body: "*.log\n"},
	})
}

// newInitedRepo returns a GitRepo and a session root initialized from the
// default template.
func newInitedRepo(t *testing.T) (*GitRepo, string, CommitResult) {
	t.Helper()
	g := newTestGitRepo(t, nil)
	root := t.TempDir()
	res, err := g.Init(context.Background(), root, bytes.NewReader(defaultTemplate(t)))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return g, root, res
}

// snapshotTree returns relative path -> content of the committable files:
// everything except .git, .orbit, node_modules, dist and *.log (ignored by the
// default template).
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if hasProtectedSegment(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".log") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func treeKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sameTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("file sets differ: got %v want %v", treeKeys(got), treeKeys(want))
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Fatalf("file %q differs: got %q (present=%v) want %q", k, g, ok, v)
		}
	}
}

func TestGitInitCreatesFirstCommit(t *testing.T) {
	g, root, res := newInitedRepo(t)
	ctx := context.Background()

	if !isFullSHA(res.SHA) || !res.Changed || res.Prev != "" || len(res.Bundle) == 0 {
		t.Fatalf("unexpected init result: %+v", res)
	}
	head, err := g.Head(ctx, root)
	if err != nil || head != res.SHA {
		t.Fatalf("Head = %q, %v; want %q", head, err, res.SHA)
	}
	log, err := g.Log(ctx, root)
	if err != nil || len(log) != 1 || log[0].Subject != "turn 0" {
		t.Fatalf("log = %+v, %v", log, err)
	}
	if !bytes.HasPrefix(res.Bundle, []byte("# v2 git bundle")) && !bytes.HasPrefix(res.Bundle, []byte("# v3 git bundle")) {
		t.Fatalf("bundle has no git bundle signature: %q", res.Bundle[:20])
	}

	gi := mustReadFile(t, filepath.Join(root, ".gitignore"))
	for _, want := range []string{"*.log", "node_modules/", "dist/", ".orbit/"} {
		if !strings.Contains(gi, want+"\n") {
			t.Fatalf(".gitignore lacks %q: %q", want, gi)
		}
	}
	// Template modes are normalized to 0644/0755.
	fi, err := os.Stat(filepath.Join(root, "scripts", "run.sh"))
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("run.sh mode = %v, %v", fi.Mode(), err)
	}
	fi, _ = os.Stat(filepath.Join(root, "package.json"))
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("package.json mode = %v", fi.Mode())
	}

	// A template that already ignores everything is not duplicated.
	g2 := newTestGitRepo(t, nil)
	root2 := t.TempDir()
	tpl := buildTar(t, []tarEntry{{name: ".gitignore", body: "/node_modules\ndist/\n.orbit"}, {name: "a.txt", body: "a"}})
	if _, err := g2.Init(ctx, root2, bytes.NewReader(tpl)); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(root2, ".gitignore")); got != "/node_modules\ndist/\n.orbit" {
		t.Fatalf(".gitignore was modified although complete: %q", got)
	}

	// Init into a root that already has a repository fails.
	if _, err := g.Init(ctx, root, bytes.NewReader(defaultTemplate(t))); !errors.Is(err, ErrRepoExists) {
		t.Fatalf("second Init err = %v, want ErrRepoExists", err)
	}
}

func TestGitCommitRestoreRoundTrip(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()

	mustWriteFile(t, filepath.Join(root, "src", "App.tsx"), "export const App = () => null\n")
	// Ignored content must not travel in bundles.
	mustWriteFile(t, filepath.Join(root, "node_modules", "x", "i.js"), "x")
	mustWriteFile(t, filepath.Join(root, "dist", "index.js"), "built")
	mustWriteFile(t, filepath.Join(root, ".orbit", "ops.jsonl"), "{}\n")
	mustWriteFile(t, filepath.Join(root, "debug.log"), "noise")
	c1, err := g.Commit(ctx, root, 1, "add app")
	if err != nil || !c1.Changed || c1.Prev != init.SHA || len(c1.Bundle) == 0 {
		t.Fatalf("Commit 1 = %+v, %v", c1, err)
	}

	mustWriteFile(t, filepath.Join(root, "index.html"), "<html>v2</html>\n")
	if err := os.Remove(filepath.Join(root, "package.json")); err != nil {
		t.Fatal(err)
	}
	c2, err := g.Commit(ctx, root, 2, "edit\nhtml")
	if err != nil || !c2.Changed || c2.Prev != c1.SHA {
		t.Fatalf("Commit 2 = %+v, %v", c2, err)
	}
	log, _ := g.Log(ctx, root)
	if len(log) != 3 || log[0].Subject != "turn 2: edit html" || log[1].Subject != "turn 1: add app" {
		t.Fatalf("log = %+v", log)
	}

	// Rebuild from stored bundles into a different root and builder instance.
	g2 := newTestGitRepo(t, nil)
	root2 := t.TempDir()
	head, err := g2.Restore(ctx, root2, []Bundle{
		{Seq: 0, SHA: init.SHA, Data: init.Bundle},
		{Seq: 1, SHA: c1.SHA, Data: c1.Bundle},
		{Seq: 2, SHA: c2.SHA, Data: c2.Bundle},
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if head != c2.SHA {
		t.Fatalf("restored head %s != %s", head, c2.SHA)
	}
	sameTree(t, snapshotTree(t, root2), snapshotTree(t, root))
	log2, err := g2.Log(ctx, root2)
	if err != nil || len(log2) != 3 || log2[2].SHA != init.SHA {
		t.Fatalf("restored log = %+v, %v", log2, err)
	}
	// No bundle files or restore refs are left behind.
	if _, err := os.Stat(filepath.Join(root2, ".orbit", "tmp")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(root2, ".orbit", "tmp"))
		if len(entries) != 0 {
			t.Fatalf("restore temp dir not cleaned: %v", entries)
		}
	}
	// The restored repo keeps working: a further turn commits incrementally.
	mustWriteFile(t, filepath.Join(root2, "next.txt"), "n")
	c3, err := g2.Commit(ctx, root2, 3, "")
	if err != nil || !c3.Changed || c3.Prev != c2.SHA {
		t.Fatalf("Commit after restore = %+v, %v", c3, err)
	}

	// Ignored paths were never committed: the archive holds none of them.
	var tarBuf bytes.Buffer
	if err := g.Archive(ctx, root, c2.SHA, &tarBuf); err != nil {
		t.Fatal(err)
	}
	for _, name := range tarNames(t, tarBuf.Bytes()) {
		for _, seg := range strings.Split(name, "/") {
			if seg == "node_modules" || seg == "dist" || seg == ".orbit" || seg == ".git" || name == "debug.log" {
				t.Fatalf("archive contains ignored path %q", name)
			}
		}
	}
}

func tarNames(t *testing.T, data []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatalf("reading tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		names = append(names, strings.TrimSuffix(hdr.Name, "/"))
	}
}

func TestGitNoChangeTurn(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()

	// Only ignored files change: still a no-op turn.
	mustWriteFile(t, filepath.Join(root, "node_modules", "p", "x.js"), "x")
	mustWriteFile(t, filepath.Join(root, ".orbit", "ops.jsonl"), "{}\n")
	res, err := g.Commit(ctx, root, 1, "nothing")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.Changed || res.Bundle != nil || res.SHA != init.SHA || res.Prev != init.SHA {
		t.Fatalf("no-op result = %+v", res)
	}
	log, _ := g.Log(ctx, root)
	if len(log) != 1 {
		t.Fatalf("log grew on a no-op turn: %+v", log)
	}
}

func TestGitIgnoredDirsSurviveGitignoreTampering(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()

	// The agent can rewrite .gitignore; negating the protected dirs must not
	// get build output or builder state committed.
	mustWriteFile(t, filepath.Join(root, ".gitignore"), "!node_modules/\n!dist/\n!.orbit/\n")
	mustWriteFile(t, filepath.Join(root, "dist", "a.js"), "a")
	mustWriteFile(t, filepath.Join(root, "node_modules", "m", "a.js"), "a")
	mustWriteFile(t, filepath.Join(root, ".orbit", "ops.jsonl"), "{}")
	mustWriteFile(t, filepath.Join(root, "src", "dist", "deep.js"), "deep")
	res, err := g.Commit(ctx, root, 1, "tamper")
	if err != nil || !res.Changed {
		t.Fatalf("Commit = %+v, %v", res, err)
	}
	var buf bytes.Buffer
	if err := g.Archive(ctx, root, res.SHA, &buf); err != nil {
		t.Fatal(err)
	}
	names := tarNames(t, buf.Bytes())
	for _, n := range names {
		if strings.Contains(n, "dist") || strings.Contains(n, "node_modules") || strings.Contains(n, ".orbit") {
			t.Fatalf("protected path committed: %q (all: %v)", n, names)
		}
	}
	_ = init
}

func TestGitCommitRejectsSymlinkAndRetries(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()

	if err := os.Symlink("/etc/passwd", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "ok.txt"), "ok")
	_, err := g.Commit(ctx, root, 1, "with link")
	if !errors.Is(err, ErrUnsafeContent) {
		t.Fatalf("Commit err = %v, want ErrUnsafeContent", err)
	}
	if strings.Contains(err.Error(), "/etc/passwd") || strings.Contains(err.Error(), root) {
		t.Fatalf("error leaks host paths: %v", err)
	}
	if head, _ := g.Head(ctx, root); head != init.SHA {
		t.Fatal("HEAD moved on a rejected commit")
	}
	// After removing the link the turn can be retried.
	if err := os.Remove(filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if res, err := g.Commit(ctx, root, 1, "retry"); err != nil || !res.Changed {
		t.Fatalf("retry = %+v, %v", res, err)
	}
}

func TestGitRevertKeepsHistoryAndTree(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()

	// Turn 1: add a file, modify one.
	mustWriteFile(t, filepath.Join(root, "added.txt"), "added in turn 1\n")
	mustWriteFile(t, filepath.Join(root, "index.html"), "<html>turn1</html>\n")
	c1, err := g.Commit(ctx, root, 1, "t1")
	if err != nil {
		t.Fatal(err)
	}
	targetTree := snapshotTree(t, root)

	// Turn 2: delete a file, add another (in a new dir), modify one.
	if err := os.Remove(filepath.Join(root, "added.txt")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "newdir", "later.txt"), "later\n")
	mustWriteFile(t, filepath.Join(root, "index.html"), "<html>turn2</html>\n")
	c2, err := g.Commit(ctx, root, 2, "t2")
	if err != nil {
		t.Fatal(err)
	}
	// Uncommitted garbage and ignored dirs around the revert.
	mustWriteFile(t, filepath.Join(root, "uncommitted.txt"), "x")
	mustWriteFile(t, filepath.Join(root, "dist", "keep.js"), "keep")
	mustWriteFile(t, filepath.Join(root, ".orbit", "ops.jsonl"), "keep")
	before, _ := g.Log(ctx, root)

	rev, err := g.Revert(ctx, root, c1.SHA, 3)
	if err != nil || !rev.Changed || rev.Prev != c2.SHA || len(rev.Bundle) == 0 {
		t.Fatalf("Revert = %+v, %v", rev, err)
	}
	after, _ := g.Log(ctx, root)
	if len(after) != len(before)+1 || after[0].SHA != rev.SHA || after[1].SHA != c2.SHA {
		t.Fatalf("history not preserved: before=%d after=%+v", len(before), after)
	}
	if !strings.HasPrefix(after[0].Subject, "turn 3: revert to "+c1.SHA[:12]) {
		t.Fatalf("subject = %q", after[0].Subject)
	}
	sameTree(t, snapshotTree(t, root), targetTree)
	if mustReadFile(t, filepath.Join(root, "dist", "keep.js")) != "keep" {
		t.Fatal("ignored dist content was touched")
	}
	if _, err := os.Stat(filepath.Join(root, "newdir")); err == nil {
		t.Fatal("directory added after the target survived the revert")
	}
	if mustReadFile(t, filepath.Join(root, ".orbit", "ops.jsonl")) != "keep" {
		t.Fatal(".orbit content was touched")
	}
	// Tree of the new commit equals the tree of the target.
	var a, b bytes.Buffer
	if err := g.Archive(ctx, root, rev.SHA, &a); err != nil {
		t.Fatal(err)
	}
	if err := g.Archive(ctx, root, c1.SHA, &b); err != nil {
		t.Fatal(err)
	}
	an, bn := tarNames(t, a.Bytes()), tarNames(t, b.Bytes())
	if strings.Join(an, ",") != strings.Join(bn, ",") {
		t.Fatalf("archive entries differ: %v vs %v", an, bn)
	}

	// The incremental bundle replays on top of the previous snapshots.
	g2 := newTestGitRepo(t, nil)
	root2 := t.TempDir()
	head, err := g2.Restore(ctx, root2, []Bundle{
		{SHA: init.SHA, Data: init.Bundle}, {SHA: c1.SHA, Data: c1.Bundle},
		{SHA: c2.SHA, Data: c2.Bundle}, {SHA: rev.SHA, Data: rev.Bundle},
	})
	if err != nil || head != rev.SHA {
		t.Fatalf("Restore after revert = %q, %v", head, err)
	}
	sameTree(t, snapshotTree(t, root2), targetTree)

	// Reverting to HEAD's own tree is a no-op.
	noop, err := g.Revert(ctx, root, rev.SHA, 4)
	if err != nil || noop.Changed || noop.Bundle != nil || noop.SHA != rev.SHA {
		t.Fatalf("no-op revert = %+v, %v", noop, err)
	}
}

func TestGitRevertValidation(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "a")
	c1, err := g.Commit(ctx, root, 1, "a")
	if err != nil {
		t.Fatal(err)
	}

	// A commit that exists but is not an ancestor of HEAD: an orphan commit
	// built directly with git in the same repository.
	orphan := gitForTest(t, root, "commit-tree", "-m", "orphan", c1.SHA+"^{tree}")
	if _, err := g.Revert(ctx, root, orphan, 2); !errors.Is(err, ErrNotAncestor) {
		t.Fatalf("revert to non-ancestor err = %v, want ErrNotAncestor", err)
	}
	// Well-formed but unknown.
	if _, err := g.Revert(ctx, root, strings.Repeat("a", 40), 2); !errors.Is(err, ErrUnknownCommit) {
		t.Fatalf("revert to unknown err = %v, want ErrUnknownCommit", err)
	}
	// A tree object is not a commit.
	tree := gitForTest(t, root, "rev-parse", init.SHA+"^{tree}")
	if _, err := g.Revert(ctx, root, tree, 2); !errors.Is(err, ErrUnknownCommit) {
		t.Fatalf("revert to tree err = %v, want ErrUnknownCommit", err)
	}
	if head, _ := g.Head(ctx, root); head != c1.SHA {
		t.Fatal("HEAD moved after rejected reverts")
	}
	if _, err := g.Revert(ctx, root, init.SHA, -1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("negative turn err = %v", err)
	}
}

// gitForTest runs plain git for test setup/inspection, outside the code under
// test.
func gitForTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t.invalid"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(root, ".git"), "GIT_WORK_TREE="+root,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitInvalidSHAs(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()
	bad := []string{
		"", "--upload-pack=x", "--output=/tmp/x", "-1", "HEAD;rm -rf /", "HEAD~1", "main",
		init.SHA[:7], init.SHA[:39], init.SHA + "0", strings.ToUpper(init.SHA),
		init.SHA[:39] + "g", init.SHA + "\n", " " + init.SHA, "HEAD^{tree}",
	}
	for _, sha := range bad {
		if _, err := g.Revert(ctx, root, sha, 1); !errors.Is(err, ErrInvalidSHA) {
			t.Errorf("Revert(%q) err = %v, want ErrInvalidSHA", sha, err)
		}
		var buf bytes.Buffer
		if sha == "HEAD" {
			continue
		}
		if err := g.Archive(ctx, root, sha, &buf); !errors.Is(err, ErrInvalidSHA) {
			t.Errorf("Archive(%q) err = %v, want ErrInvalidSHA", sha, err)
		}
		if buf.Len() != 0 {
			t.Errorf("Archive(%q) wrote output", sha)
		}
	}
	// "HEAD" is valid for Archive only.
	var buf bytes.Buffer
	if err := g.Archive(ctx, root, "HEAD", &buf); err != nil || buf.Len() == 0 {
		t.Fatalf("Archive(HEAD) = %v, %d bytes", err, buf.Len())
	}
	if _, err := g.Revert(ctx, root, "HEAD", 1); !errors.Is(err, ErrInvalidSHA) {
		t.Fatalf("Revert(HEAD) err = %v", err)
	}
	// Bundles with a malformed expected SHA are rejected before any work.
	if _, err := g.Restore(ctx, t.TempDir(), []Bundle{{SHA: "--x", Data: init.Bundle}}); !errors.Is(err, ErrInvalidSHA) {
		t.Fatalf("Restore bad sha err = %v", err)
	}
}

func TestGitArchiveContents(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()
	mustWriteFile(t, filepath.Join(root, "src", "new.ts"), "export {}\n")
	mustWriteFile(t, filepath.Join(root, "node_modules", "a", "b.js"), "x")
	mustWriteFile(t, filepath.Join(root, "dist", "o.js"), "x")
	c1, err := g.Commit(ctx, root, 1, "x")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := g.Archive(ctx, root, c1.SHA, &buf); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(tarNames(t, buf.Bytes()), ",")
	for _, want := range []string{"package.json", "index.html", "src/main.tsx", "src/new.ts", ".gitignore", "scripts/run.sh"} {
		if !strings.Contains(got, want) {
			t.Fatalf("archive lacks %q: %s", want, got)
		}
	}
	for _, bad := range []string{"node_modules", "dist", ".orbit", ".git/", "ops.jsonl"} {
		if strings.Contains(got, bad) {
			t.Fatalf("archive contains %q: %s", bad, got)
		}
	}
	// Archiving an older commit gives the older tree.
	var old bytes.Buffer
	if err := g.Archive(ctx, root, init.SHA, &old); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(tarNames(t, old.Bytes()), ","), "src/new.ts") {
		t.Fatal("old archive contains a later file")
	}
	// The archive can seed a new session (company template round trip, which
	// also exercises the pax global header git archive writes).
	g2 := newTestGitRepo(t, nil)
	root2 := t.TempDir()
	if _, err := g2.Init(ctx, root2, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("Init from archive: %v", err)
	}
	sameTree(t, snapshotTree(t, root2), snapshotTree(t, root))

	// Output cap.
	small := newTestGitRepo(t, func(c *GitConfig) { c.MaxArchiveBytes = 1024 })
	if err := small.Archive(ctx, root, c1.SHA, io.Discard); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("capped archive err = %v, want ErrArchiveTooLarge", err)
	}
	// A failing writer surfaces as a git failure, not a hang.
	if err := g.Archive(ctx, root, c1.SHA, failingWriter{}); !errors.Is(err, ErrGitFailed) {
		t.Fatalf("failing writer err = %v, want ErrGitFailed", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

func TestGitInitRejectsUnsafeTarEntries(t *testing.T) {
	requireGit(t)
	long := strings.Repeat("a/", maxEntryDepth+1) + "f"
	cases := []struct {
		name  string
		entry tarEntry
	}{
		{"dotdot", tarEntry{name: "../evil.txt", body: "x"}},
		{"dotdot nested", tarEntry{name: "a/../../evil.txt", body: "x"}},
		{"dotdot in middle", tarEntry{name: "a/../b.txt", body: "x"}},
		{"absolute", tarEntry{name: "/etc/evil.txt", body: "x"}},
		{"dot segment", tarEntry{name: "a/./b.txt", body: "x"}},
		{"double slash", tarEntry{name: "a//b.txt", body: "x"}},
		{"backslash", tarEntry{name: "a\\b.txt", body: "x"}},
		{"symlink", tarEntry{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}},
		{"symlink relative", tarEntry{name: "link", typeflag: tar.TypeSymlink, linkname: "../outside"}},
		{"hardlink", tarEntry{name: "hl", typeflag: tar.TypeLink, linkname: "package.json"}},
		{"char device", tarEntry{name: "dev", typeflag: tar.TypeChar}},
		{"block device", tarEntry{name: "dev", typeflag: tar.TypeBlock}},
		{"fifo", tarEntry{name: "pipe", typeflag: tar.TypeFifo}},
		{".git file", tarEntry{name: ".git/config", body: "[core]\n\thooksPath=x"}},
		{".git hook", tarEntry{name: ".git/hooks/post-commit", body: "#!/bin/sh\n", mode: 0o755}},
		{".git nested", tarEntry{name: "sub/.git/config", body: "x"}},
		{".GIT case", tarEntry{name: ".GIT/config", body: "x"}},
		{".orbit", tarEntry{name: ".orbit/ops.jsonl", body: "{}"}},
		{"node_modules", tarEntry{name: "node_modules/x/index.js", body: "x"}},
		{"dist", tarEntry{name: "dist/index.js", body: "x"}},
		{".git dir entry", tarEntry{name: ".git/", typeflag: tar.TypeDir}},
		{"too deep", tarEntry{name: long, body: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGitRepo(t, nil)
			outside := t.TempDir()
			root := filepath.Join(outside, "session")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			data := buildTar(t, []tarEntry{{name: "ok.txt", body: "fine"}, tc.entry})
			_, err := g.Init(context.Background(), root, bytes.NewReader(data))
			if !errors.Is(err, ErrUnsafeArchiveEntry) {
				t.Fatalf("Init err = %v, want ErrUnsafeArchiveEntry", err)
			}
			if strings.Contains(err.Error(), outside) {
				t.Fatalf("error leaks host path: %v", err)
			}
			// Nothing escaped, and the root was emptied for a retry.
			if entries, _ := os.ReadDir(outside); len(entries) != 1 {
				t.Fatalf("files escaped the session root: %v", entries)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatalf("root not cleaned after failed Init: %v", entries)
			}
		})
	}
}

func TestGitInitTarEdgeCases(t *testing.T) {
	g := newTestGitRepo(t, nil)
	ctx := context.Background()

	// Duplicate file entries and file/directory collisions are rejected.
	for name, entries := range map[string][]tarEntry{
		"duplicate":       {{name: "a.txt", body: "1"}, {name: "a.txt", body: "2"}},
		"file then dir":   {{name: "a", body: "1"}, {name: "a/b.txt", body: "2"}},
		"dir then file":   {{name: "a/", typeflag: tar.TypeDir}, {name: "a", body: "2"}},
		"nested overlaps": {{name: "a/b", body: "1"}, {name: "a/b/c", body: "2"}},
	} {
		root := t.TempDir()
		_, err := g.Init(ctx, root, bytes.NewReader(buildTar(t, entries)))
		if !errors.Is(err, ErrUnsafeArchiveEntry) {
			t.Errorf("%s: err = %v, want ErrUnsafeArchiveEntry", name, err)
		}
	}
	// Not a tar at all.
	if _, err := g.Init(ctx, t.TempDir(), strings.NewReader(strings.Repeat("not a tar", 200))); !errors.Is(err, ErrInvalidArchive) {
		t.Errorf("garbage err = %v, want ErrInvalidArchive", err)
	}
	// Leading "./" is stripped; a dot-prefixed benign name is accepted.
	root := t.TempDir()
	if _, err := g.Init(ctx, root, bytes.NewReader(buildTar(t, []tarEntry{
		{name: "./.env.example", body: "A=1"}, {name: "././x/y.txt", body: "y"},
	}))); err != nil {
		t.Fatalf("benign tar: %v", err)
	}
	if mustReadFile(t, filepath.Join(root, "x", "y.txt")) != "y" {
		t.Fatal("entry was not extracted at the stripped path")
	}
	// A setuid or group-writable mode is not preserved.
	root = t.TempDir()
	if _, err := g.Init(ctx, root, bytes.NewReader(buildTar(t, []tarEntry{{name: "tool", body: "x", mode: 0o4777}, {name: "doc", body: "x", mode: 0o666}}))); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]fs.FileMode{"tool": 0o755, "doc": 0o644} {
		fi, err := os.Stat(filepath.Join(root, name))
		if err != nil || fi.Mode() != want {
			t.Fatalf("%s mode = %v, %v; want %v", name, fi.Mode(), err, want)
		}
	}
	// A nil stream is an argument error.
	if _, err := g.Init(ctx, t.TempDir(), nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil stream err = %v", err)
	}
}

func TestGitInitTarLimits(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	cases := []struct {
		name string
		mut  func(*GitConfig)
		tar  []tarEntry
	}{
		{"total bytes", func(c *GitConfig) { c.Tar.MaxTotalBytes = 100; c.Tar.MaxFileBytes = 100 },
			[]tarEntry{{name: "a", body: strings.Repeat("x", 60)}, {name: "b", body: strings.Repeat("x", 60)}}},
		{"file bytes", func(c *GitConfig) { c.Tar.MaxFileBytes = 50 },
			[]tarEntry{{name: "a", body: strings.Repeat("x", 51)}}},
		{"file count", func(c *GitConfig) { c.Tar.MaxFiles = 3 },
			[]tarEntry{{name: "a", body: "1"}, {name: "b", body: "1"}, {name: "c", body: "1"}, {name: "d", body: "1"}}},
		{"dirs count", func(c *GitConfig) { c.Tar.MaxFiles = 2 },
			[]tarEntry{{name: "a/", typeflag: tar.TypeDir}, {name: "b/", typeflag: tar.TypeDir}, {name: "c/", typeflag: tar.TypeDir}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGitRepo(t, tc.mut)
			root := t.TempDir()
			_, err := g.Init(ctx, root, bytes.NewReader(buildTar(t, tc.tar)))
			if !errors.Is(err, ErrArchiveTooLarge) {
				t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatalf("root not cleaned: %v", entries)
			}
		})
	}
	// The raw-stream cap cuts a reader that never ends.
	cr := &capReader{r: zeroReader{}, left: 10000}
	if n, err := io.Copy(io.Discard, cr); !errors.Is(err, errStreamLimit) || n != 10000 {
		t.Fatalf("capReader copied %d bytes, err = %v", n, err)
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestGitRestoreRejections(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "a")
	c1, err := g.Commit(ctx, root, 1, "a")
	if err != nil {
		t.Fatal(err)
	}
	full := []Bundle{{SHA: init.SHA, Data: init.Bundle}}

	t.Run("root with a repository", func(t *testing.T) {
		if _, err := g.Restore(ctx, root, full); !errors.Is(err, ErrRepoExists) {
			t.Fatalf("err = %v, want ErrRepoExists", err)
		}
		// The existing repository was not touched.
		if head, _ := g.Head(ctx, root); head != c1.SHA {
			t.Fatal("rejected restore changed the repository")
		}
	})
	t.Run("non-empty root", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "keep.txt"), "keep")
		if _, err := g.Restore(ctx, dir, full); !errors.Is(err, ErrRootNotEmpty) {
			t.Fatalf("err = %v, want ErrRootNotEmpty", err)
		}
		if mustReadFile(t, filepath.Join(dir, "keep.txt")) != "keep" {
			t.Fatal("existing file was removed")
		}
	})
	t.Run("only .orbit is allowed", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, ".orbit", "ops.jsonl"), "{}\n")
		if _, err := g.Restore(ctx, dir, full); err != nil {
			t.Fatalf("err = %v", err)
		}
		if mustReadFile(t, filepath.Join(dir, ".orbit", "ops.jsonl")) != "{}\n" {
			t.Fatal(".orbit was altered")
		}
	})
	t.Run("incremental bundle first", func(t *testing.T) {
		dir := t.TempDir()
		_, err := g.Restore(ctx, dir, []Bundle{{SHA: c1.SHA, Data: c1.Bundle}})
		if !errors.Is(err, ErrBundleVerify) {
			t.Fatalf("err = %v, want ErrBundleVerify", err)
		}
		for _, e := range mustReadDir(t, dir) {
			if e.Name() != ".orbit" {
				t.Fatalf("root not cleaned: %v", e.Name())
			}
		}
	})
	t.Run("garbage bundle", func(t *testing.T) {
		_, err := g.Restore(ctx, t.TempDir(), []Bundle{{Data: []byte("# v2 git bundle\nnonsense")}})
		if !errors.Is(err, ErrBundleVerify) {
			t.Fatalf("err = %v, want ErrBundleVerify", err)
		}
	})
	t.Run("wrong expected sha", func(t *testing.T) {
		_, err := g.Restore(ctx, t.TempDir(), []Bundle{{SHA: strings.Repeat("b", 40), Data: init.Bundle}})
		if !errors.Is(err, ErrBundleVerify) {
			t.Fatalf("err = %v, want ErrBundleVerify", err)
		}
	})
	t.Run("empty and nil input", func(t *testing.T) {
		if _, err := g.Restore(ctx, t.TempDir(), nil); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("nil bundles err = %v", err)
		}
		if _, err := g.Restore(ctx, t.TempDir(), []Bundle{{}}); !errors.Is(err, ErrBundleVerify) {
			t.Fatalf("empty bundle err = %v", err)
		}
	})
	t.Run("size cap", func(t *testing.T) {
		small := newTestGitRepo(t, func(c *GitConfig) { c.MaxBundleBytes = 10 })
		if _, err := small.Restore(ctx, t.TempDir(), full); !errors.Is(err, ErrBundleTooLarge) {
			t.Fatalf("err = %v, want ErrBundleTooLarge", err)
		}
	})
	t.Run("missing root", func(t *testing.T) {
		if _, err := g.Restore(ctx, filepath.Join(t.TempDir(), "nope"), full); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("err = %v, want ErrSessionNotFound", err)
		}
	})
}

func TestGitBundleTooLargeRollsBackCommit(t *testing.T) {
	g := newTestGitRepo(t, func(c *GitConfig) { c.MaxBundleBytes = 700 })
	root := t.TempDir()
	ctx := context.Background()
	init, err := g.Init(ctx, root, bytes.NewReader(buildTar(t, []tarEntry{{name: "a.txt", body: "a"}})))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Incompressible-enough content that pushes the bundle over the cap.
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&sb, "line %d %x\n", i, i*7919)
	}
	mustWriteFile(t, filepath.Join(root, "big.txt"), sb.String())
	if _, err := g.Commit(ctx, root, 1, "big"); !errors.Is(err, ErrBundleTooLarge) {
		t.Fatalf("err = %v, want ErrBundleTooLarge", err)
	}
	if head, _ := g.Head(ctx, root); head != init.SHA {
		t.Fatal("HEAD was not rolled back after the bundle failed")
	}
	// Retry with room succeeds and still produces one commit.
	g.cfg.MaxBundleBytes = 1 << 20
	res, err := g.Commit(ctx, root, 1, "big")
	if err != nil || !res.Changed || res.Prev != init.SHA {
		t.Fatalf("retry = %+v, %v", res, err)
	}
	if log, _ := g.Log(ctx, root); len(log) != 2 {
		t.Fatalf("log length = %d, want 2", len(log))
	}
}

// TestGitHooksAndConfigDoNotRun plants hooks and a hostile fsmonitor in the
// repository and checks none of them executes during Commit, Revert or
// Archive.
func TestGitHooksAndConfigDoNotRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks")
	}
	g, root, init := newInitedRepo(t)
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "marker")

	script := "#!/bin/sh\necho \"$0\" >> " + marker + "\n"
	hooks := []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "post-checkout",
		"post-index-change", "reference-transaction", "pre-auto-gc", "post-merge", "pre-rebase"}
	hookDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, h := range hooks {
		if err := os.WriteFile(filepath.Join(hookDir, h), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fsmon := filepath.Join(t.TempDir(), "fsmon.sh")
	if err := os.WriteFile(fsmon, []byte(script+"printf '\\0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitForTest(t, root, "config", "core.fsmonitor", fsmon)
	gitForTest(t, root, "config", "core.editor", "touch "+marker)
	gitForTest(t, root, "config", "commit.gpgsign", "true")
	gitForTest(t, root, "config", "gpg.program", fsmon)

	// Control: the planted hooks really fire for plain git, otherwise this
	// test proves nothing (reference-transaction fires on update-ref).
	gitForTest(t, root, "update-ref", "refs/orbit/control", init.SHA)
	if _, err := os.Stat(marker); err != nil {
		t.Skip("planted hooks do not fire with this git; cannot assert hardening")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	mustWriteFile(t, filepath.Join(root, "a.txt"), "a")
	c1, err := g.Commit(ctx, root, 1, "a")
	if err != nil || !c1.Changed {
		t.Fatalf("Commit = %+v, %v", c1, err)
	}
	mustWriteFile(t, filepath.Join(root, "b.txt"), "b")
	if _, err := g.Commit(ctx, root, 2, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Revert(ctx, root, c1.SHA, 3); err != nil {
		t.Fatal(err)
	}
	if err := g.Archive(ctx, root, "HEAD", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Log(ctx, root); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a hook or config command executed:\n%s", b)
	}
}

func TestGitConcurrentCommitsAreSerialized(t *testing.T) {
	g, root, init := newInitedRepo(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	results := make([]CommitResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine adds its own file, then commits; the lock
			// serializes the commit calls, so every commit must succeed.
			mustWriteFile(t, filepath.Join(root, fmt.Sprintf("f%d.txt", i)), fmt.Sprint(i))
			results[i], errs[i] = g.Commit(ctx, root, i+1, "c")
		}()
	}
	wg.Wait()
	changed := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("commit %d: %v", i, errs[i])
		}
		if results[i].Changed {
			changed++
		}
	}
	log, err := g.Log(ctx, root)
	if err != nil || len(log) != changed+1 || log[len(log)-1].SHA != init.SHA {
		t.Fatalf("history not linear: changed=%d log=%d err=%v", changed, len(log), err)
	}
	// Every file is in HEAD whichever commit picked it up.
	files := snapshotTree(t, root)
	for i := 0; i < n; i++ {
		if _, ok := files[fmt.Sprintf("f%d.txt", i)]; !ok {
			t.Fatalf("f%d.txt missing", i)
		}
	}
	// The lock map does not leak entries.
	g.mu.Lock()
	left := len(g.locks)
	g.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d root locks leaked", left)
	}
}

func TestGitTimeoutAndUnavailable(t *testing.T) {
	requireGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	if _, err := NewGitRepo(GitConfig{GitBin: "definitely-not-git-xyz", HomeDir: t.TempDir()}); !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("missing binary err = %v, want ErrGitUnavailable", err)
	}

	// A "git" that hangs (and keeps a child alive holding the pipes) is killed
	// by the per-command timeout.
	script := filepath.Join(t.TempDir(), "fakegit")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30 &\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	g, err := NewGitRepo(GitConfig{GitBin: script, HomeDir: filepath.Join(t.TempDir(), "h"), Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = g.Head(context.Background(), root)
	if !errors.Is(err, ErrGitTimeout) {
		t.Fatalf("err = %v, want ErrGitTimeout", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
	// A canceled caller context is reported as such.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Head(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestGitErrorsDoNotLeakHostPaths(t *testing.T) {
	g, root, _ := newInitedRepo(t)
	ctx := context.Background()
	// Corrupt the repository so git fails with a message naming host paths.
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := g.Commit(ctx, root, 1, "x")
	if err == nil {
		t.Fatal("expected an error from a corrupt repository")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("error leaks the host path: %v", err)
	}
	var ge *GitError
	if !errors.As(err, &ge) {
		t.Fatalf("error is not a *GitError: %T", err)
	}
	if ge.Cause() == nil {
		t.Fatal("cause for server logs is missing")
	}
}

func TestGitRootValidation(t *testing.T) {
	g := newTestGitRepo(t, nil)
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := g.Head(ctx, filepath.Join(dir, "missing")); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing root err = %v", err)
	}
	if _, err := g.Head(ctx, dir); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("no repo err = %v, want ErrNoRepo", err)
	}
	if _, err := g.Commit(ctx, dir, 1, ""); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("Commit without repo err = %v", err)
	}
	if _, err := g.Head(ctx, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty root err = %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := g.Head(ctx, link); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("symlink root err = %v, want ErrInvalidArgument", err)
	}
	if _, err := g.Commit(ctx, dir, -1, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("negative turn err = %v", err)
	}
}

func TestCommitSubject(t *testing.T) {
	long := strings.Repeat("é", 300)
	cases := map[string]string{
		"":                         "turn 7",
		"  add page  ":             "turn 7: add page",
		"line1\nline2\x00\x1b[31m": "turn 7: line1 line2  [31m",
		"-n --amend":               "turn 7: -n --amend",
	}
	for in, want := range cases {
		if got := commitSubject(7, in); got != want {
			t.Errorf("commitSubject(%q) = %q, want %q", in, got, want)
		}
	}
	got := commitSubject(1, long)
	if len(got) > len("turn 1: ")+maxCommitSubject || !strings.HasPrefix(got, "turn 1: é") {
		t.Errorf("long subject not bounded: %d bytes", len(got))
	}
}

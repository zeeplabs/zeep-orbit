package builder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestWorkspace(t *testing.T, limits WorkspaceLimits) (*Workspace, string) {
	t.Helper()
	root := t.TempDir()
	w, err := NewWorkspace(root, limits)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return w, root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSessionRootValidatesID(t *testing.T) {
	good := []string{"3f2b1c9e-8a4d-4e0b-9c1d-5a6b7c8d9e0f", "abc123", "A-b-C"}
	for _, id := range good {
		got, err := SessionRoot("/work", id)
		if err != nil {
			t.Errorf("SessionRoot(%q): unexpected error %v", id, err)
		}
		if want := filepath.Join("/work", id); got != want {
			t.Errorf("SessionRoot(%q) = %q, want %q", id, got, want)
		}
	}
	bad := []string{"", ".", "..", "../x", "a/b", `a\b`, "a b", "-lead", "a\x00b", "é", strings.Repeat("a", 65)}
	for _, id := range bad {
		if _, err := SessionRoot("/work", id); !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("SessionRoot(%q) err = %v, want ErrInvalidSessionID", id, err)
		}
	}
}

func TestNewWorkspaceRoot(t *testing.T) {
	if _, err := NewWorkspace(filepath.Join(t.TempDir(), "missing"), WorkspaceLimits{}); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("missing root err = %v, want ErrSessionNotFound", err)
	}
	file := filepath.Join(t.TempDir(), "f")
	mustWriteFile(t, file, "x")
	if _, err := NewWorkspace(file, WorkspaceLimits{}); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("file root err = %v, want ErrNotDirectory", err)
	}
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, err := NewWorkspace(link, WorkspaceLimits{}); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("symlink root err = %v, want ErrNotDirectory", err)
	}
}

func TestPathRejections(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	cases := []struct {
		name string
		path string
		want error
	}{
		{"dotdot", "../x.txt", ErrPathOutside},
		{"dotdot nested", "a/../../x.txt", ErrPathOutside},
		{"dotdot only", "..", ErrPathOutside},
		{"absolute", "/etc/passwd", ErrPathOutside},
		{"absolute inside root", filepath.Join(root, "x.txt"), ErrPathOutside},
		{"empty", "", ErrInvalidPath},
		{"nul", "a\x00b.txt", ErrInvalidPath},
		{"git", ".git/config", ErrPathForbidden},
		{"git nested", "src/.git/config", ErrPathForbidden},
		{"git case", ".GIT/config", ErrPathForbidden},
		{"node_modules", "node_modules/x/index.js", ErrPathForbidden},
		{"dist", "dist/index.html", ErrPathForbidden},
		{"orbit", ".orbit/ops.jsonl", ErrPathForbidden},
		{"cleaned into protected", "src/../.git/config", ErrPathForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := w.WriteFile(tc.path, "x"); !errors.Is(err, tc.want) {
				t.Errorf("WriteFile err = %v, want %v", err, tc.want)
			}
			if _, err := w.ReadFile(tc.path, 0, 0); !errors.Is(err, tc.want) {
				t.Errorf("ReadFile err = %v, want %v", err, tc.want)
			}
			if _, err := w.EditFile(tc.path, "a", "b", false); !errors.Is(err, tc.want) {
				t.Errorf("EditFile err = %v, want %v", err, tc.want)
			}
		})
	}
	// Nothing must have been created anywhere by the rejected writes.
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Error(".git was created")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "x.txt")); err == nil {
		t.Error("file escaped the root")
	}
}

func TestErrorsNeverLeakHostPaths(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	mustWriteFile(t, filepath.Join(root, "file.txt"), "x")
	var errs []error
	for _, f := range []func() (OpResult, error){
		func() (OpResult, error) { return w.ReadFile("missing.txt", 0, 0) },
		func() (OpResult, error) { return w.ReadFile("file.txt/child", 0, 0) },
		func() (OpResult, error) { return w.WriteFile("file.txt/child", "x") },
		func() (OpResult, error) { return w.ListDir("file.txt") },
		func() (OpResult, error) { return w.WriteFile("../x", "x") },
	} {
		_, err := f()
		errs = append(errs, err)
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("case %d: expected error", i)
			continue
		}
		if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), filepath.Dir(root)) {
			t.Errorf("case %d: error leaks host path: %v", i, err)
		}
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.txt"), "top secret")

	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "linkfile")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "does-not-exist"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}

	if _, err := w.ReadFile("linkdir/secret.txt", 0, 0); !errors.Is(err, ErrPathOutside) {
		t.Errorf("read through dir symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.ReadFile("linkfile", 0, 0); !errors.Is(err, ErrPathOutside) {
		t.Errorf("read file symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.WriteFile("linkdir/new.txt", "pwn"); !errors.Is(err, ErrPathOutside) {
		t.Errorf("write through dir symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.WriteFile("linkdir/sub/deep.txt", "pwn"); !errors.Is(err, ErrPathOutside) {
		t.Errorf("deep write through dir symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.WriteFile("linkfile", "pwn"); !errors.Is(err, ErrPathOutside) {
		t.Errorf("write file symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.EditFile("linkfile", "top", "bottom", false); !errors.Is(err, ErrPathOutside) {
		t.Errorf("edit file symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.ListDir("linkdir"); !errors.Is(err, ErrPathOutside) {
		t.Errorf("list through symlink err = %v, want ErrPathOutside", err)
	}
	if _, err := w.WriteFile("dangling", "pwn"); !errors.Is(err, ErrPathOutside) {
		t.Errorf("write dangling symlink err = %v, want ErrPathOutside", err)
	}

	if got := mustReadFile(t, filepath.Join(outside, "secret.txt")); got != "top secret" {
		t.Errorf("outside file modified: %q", got)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 {
		t.Errorf("outside dir has %d entries, want 1", len(entries))
	}
}

func TestSymlinkInsideRoot(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	mustWriteFile(t, filepath.Join(root, "real.txt"), "hello\n")
	mustWriteFile(t, filepath.Join(root, ".git", "config"), "[core]\n")
	if err := os.Symlink("real.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if err := os.Symlink(".git", filepath.Join(root, "gitlink")); err != nil {
		t.Fatal(err)
	}

	// Reading through an in-root symlink is fine.
	if res, err := w.ReadFile("alias.txt", 0, 0); err != nil || !strings.Contains(res.Output, "hello") {
		t.Errorf("read alias = %q, %v", res.Output, err)
	}
	// Writing when the target itself is a symlink is refused.
	if _, err := w.WriteFile("alias.txt", "x"); !errors.Is(err, ErrSymlink) {
		t.Errorf("write alias err = %v, want ErrSymlink", err)
	}
	if got := mustReadFile(t, filepath.Join(root, "real.txt")); got != "hello\n" {
		t.Errorf("real.txt modified through symlink: %q", got)
	}
	// An in-root symlink must not become a way into a protected directory.
	if _, err := w.ReadFile("gitlink/config", 0, 0); !errors.Is(err, ErrPathForbidden) {
		t.Errorf("read via symlink to .git err = %v, want ErrPathForbidden", err)
	}
	if _, err := w.WriteFile("gitlink/hooks", "x"); !errors.Is(err, ErrPathForbidden) {
		t.Errorf("write via symlink to .git err = %v, want ErrPathForbidden", err)
	}
}

func TestWriteFileBasics(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	res, err := w.WriteFile("src/components/App.tsx", "export {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !strings.Contains(res.Output, "created src/components/App.tsx") {
		t.Errorf("unexpected result %+v", res)
	}
	p := filepath.Join(root, "src/components/App.tsx")
	if got := mustReadFile(t, p); got != "export {}\n" {
		t.Errorf("content = %q", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
	res, err = w.WriteFile("src/components/App.tsx", "export const a = 1\n")
	if err != nil || !strings.Contains(res.Output, "updated") {
		t.Errorf("overwrite = %+v, %v", res, err)
	}
	// Temp files live under .orbit and must not linger.
	if left, _ := os.ReadDir(filepath.Join(root, ".orbit", "tmp")); len(left) != 0 {
		t.Errorf("%d temp files left behind", len(left))
	}
	if _, err := w.WriteFile("src", "x"); !errors.Is(err, ErrIsDirectory) {
		t.Errorf("write to dir err = %v, want ErrIsDirectory", err)
	}
	if _, err := w.WriteFile("src/components/App.tsx/x", "x"); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("write below file err = %v, want ErrNotDirectory", err)
	}
}

func TestWriteFileSizeLimits(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{MaxFileBytes: 100, MaxSessionBytes: 250})

	if _, err := w.WriteFile("big.txt", strings.Repeat("a", 101)); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("oversized file err = %v, want ErrFileTooLarge", err)
	}
	if _, err := os.Stat(filepath.Join(root, "big.txt")); err == nil {
		t.Error("oversized file was written")
	}
	if _, err := w.WriteFile("a.txt", strings.Repeat("a", 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteFile("b.txt", strings.Repeat("b", 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteFile("c.txt", strings.Repeat("c", 100)); !errors.Is(err, ErrSessionTooLarge) {
		t.Errorf("session cap err = %v, want ErrSessionTooLarge", err)
	}
	// Overwriting replaces the old size instead of adding to it.
	if _, err := w.WriteFile("a.txt", strings.Repeat("x", 50)); err != nil {
		t.Errorf("shrinking overwrite failed: %v", err)
	}
	// Protected directories do not count toward the cap.
	mustWriteFile(t, filepath.Join(root, "node_modules", "pkg", "big.js"), strings.Repeat("z", 10_000))
	if _, err := w.WriteFile("c.txt", strings.Repeat("c", 100)); err != nil {
		t.Errorf("write after node_modules growth failed: %v", err)
	}
}

func TestWriteFileRejectsNonText(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	cases := map[string]string{
		"nul":            "abc\x00def",
		"invalid utf8":   "abc\xff\xfedef",
		"truncated rune": "abc\xe2\x82",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := w.WriteFile("f.txt", content); !errors.Is(err, ErrInvalidContent) {
				t.Errorf("err = %v, want ErrInvalidContent", err)
			}
			if _, err := os.Stat(filepath.Join(root, "f.txt")); err == nil {
				t.Error("file was written")
			}
		})
	}
	// Valid multi-byte UTF-8 is fine.
	if _, err := w.WriteFile("ok.txt", "olá, mundo — ✓\n"); err != nil {
		t.Errorf("valid utf-8 rejected: %v", err)
	}
	// edit_file must not smuggle a NUL in through new_string.
	if _, err := w.EditFile("ok.txt", "olá", "x\x00y", false); !errors.Is(err, ErrInvalidContent) {
		t.Errorf("edit with NUL err = %v, want ErrInvalidContent", err)
	}
}

func TestReadFileBinary(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	mustWriteFile(t, filepath.Join(root, "img.png"), "\x89PNG\r\n\x1a\n\x00\x00")
	if _, err := w.ReadFile("img.png", 0, 0); !errors.Is(err, ErrBinaryFile) {
		t.Errorf("err = %v, want ErrBinaryFile", err)
	}
	if _, err := w.EditFile("img.png", "PNG", "JPG", false); !errors.Is(err, ErrBinaryFile) {
		t.Errorf("edit err = %v, want ErrBinaryFile", err)
	}
	if err := func() error { _, err := w.ReadFile("img.png", 0, 0); return err }(); err == nil || err.Error() != `binary file: "img.png"` {
		t.Errorf("message = %v", err)
	}
}

func TestEditFile(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	p := filepath.Join(root, "a.txt")

	t.Run("not found", func(t *testing.T) {
		mustWriteFile(t, p, "one two three\n")
		_, err := w.EditFile("a.txt", "four", "4", false)
		if !errors.Is(err, ErrEditNotFound) || err.Error() != "old_string not found" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		mustWriteFile(t, p, "x y x z x\n")
		_, err := w.EditFile("a.txt", "x", "w", false)
		want := "old_string is not unique (3 matches), add more context or set replace_all"
		if !errors.Is(err, ErrEditNotUnique) || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if got := mustReadFile(t, p); got != "x y x z x\n" {
			t.Errorf("file changed on failed edit: %q", got)
		}
	})
	t.Run("replace all", func(t *testing.T) {
		mustWriteFile(t, p, "x y x z x\n")
		res, err := w.EditFile("a.txt", "x", "w", true)
		if err != nil {
			t.Fatal(err)
		}
		if got := mustReadFile(t, p); got != "w y w z w\n" {
			t.Errorf("content = %q", got)
		}
		if !strings.Contains(res.Output, "3 replacement") {
			t.Errorf("output = %q", res.Output)
		}
	})
	t.Run("unique", func(t *testing.T) {
		mustWriteFile(t, p, "alpha beta\n")
		if _, err := w.EditFile("a.txt", "beta", "gamma", false); err != nil {
			t.Fatal(err)
		}
		if got := mustReadFile(t, p); got != "alpha gamma\n" {
			t.Errorf("content = %q", got)
		}
	})
	t.Run("identical", func(t *testing.T) {
		if _, err := w.EditFile("a.txt", "alpha", "alpha", false); !errors.Is(err, ErrEditNoop) {
			t.Errorf("err = %v, want ErrEditNoop", err)
		}
	})
	t.Run("empty old", func(t *testing.T) {
		if _, err := w.EditFile("a.txt", "", "x", false); !errors.Is(err, ErrEditEmptyOld) {
			t.Errorf("err = %v, want ErrEditEmptyOld", err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, err := w.EditFile("nope.txt", "a", "b", false); !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("preserves mode", func(t *testing.T) {
		script := filepath.Join(root, "run.sh")
		mustWriteFile(t, script, "echo a\n")
		if err := os.Chmod(script, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := w.EditFile("run.sh", "echo a", "echo b", false); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Stat(script); fi.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
		}
	})
	t.Run("growth past file cap", func(t *testing.T) {
		small, sroot := newTestWorkspace(t, WorkspaceLimits{MaxFileBytes: 20})
		mustWriteFile(t, filepath.Join(sroot, "f.txt"), "aaaa")
		if _, err := small.EditFile("f.txt", "aaaa", strings.Repeat("b", 21), false); !errors.Is(err, ErrFileTooLarge) {
			t.Errorf("err = %v, want ErrFileTooLarge", err)
		}
	})
}

func TestReadFileOffsetLimitAndTruncation(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{MaxReadLines: 5})
	var sb strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	mustWriteFile(t, filepath.Join(root, "f.txt"), sb.String())

	res, err := w.ReadFile("f.txt", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "     1\tline 1\n") || !strings.Contains(res.Output, "     5\tline 5\n") {
		t.Errorf("numbering wrong:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "line 6") {
		t.Errorf("read past default line cap:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "…truncated, use offset 6") {
		t.Errorf("missing truncation marker:\n%s", res.Output)
	}

	res, err = w.ReadFile("f.txt", 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Output, "     4\tline 4\n     5\tline 5\n") || !strings.Contains(res.Output, "use offset 6") {
		t.Errorf("offset/limit wrong:\n%s", res.Output)
	}

	res, err = w.ReadFile("f.txt", 11, 5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "truncated") || !strings.Contains(res.Output, "    12\tline 12") {
		t.Errorf("tail read wrong:\n%s", res.Output)
	}

	res, err = w.ReadFile("f.txt", 99, 0)
	if err != nil || !strings.Contains(res.Output, "beyond the end") {
		t.Errorf("offset beyond EOF = %q, %v", res.Output, err)
	}
	if _, err := w.ReadFile("f.txt", -1, 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative offset err = %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "empty.txt"), "")
	if res, err := w.ReadFile("empty.txt", 0, 0); err != nil || res.Output != "(empty file)" {
		t.Errorf("empty = %q, %v", res.Output, err)
	}
}

func TestReadFileByteCap(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{MaxReadBytes: 64})
	var sb strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&sb, "0123456789-%d\n", i)
	}
	sb.WriteString(strings.Repeat("é", 500) + "\n")
	mustWriteFile(t, filepath.Join(root, "f.txt"), sb.String())

	res, err := w.ReadFile("f.txt", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "…truncated, use offset") {
		t.Errorf("missing marker:\n%s", res.Output)
	}
	if len(res.Output) > 64+80 {
		t.Errorf("output not capped: %d bytes", len(res.Output))
	}

	// A single line bigger than the budget is cut on a rune boundary.
	res, err = w.ReadFile("f.txt", 11, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "line 11 was cut") {
		t.Errorf("missing cut note:\n%s", res.Output)
	}
	if strings.ContainsRune(res.Output, '�') {
		t.Error("output has a split rune")
	}

	// Files above the scan cap are refused rather than loaded.
	huge, hroot := newTestWorkspace(t, WorkspaceLimits{MaxFileBytes: 10, MaxScanBytes: 10})
	mustWriteFile(t, filepath.Join(hroot, "big.txt"), strings.Repeat("a", 11))
	if _, err := huge.ReadFile("big.txt", 0, 0); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("err = %v, want ErrFileTooLarge", err)
	}
}

func TestListDirSkipsProtectedNames(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	for _, d := range []string{".git", "node_modules", "dist", ".orbit", "src", "public"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "package.json"), "{}")
	mustWriteFile(t, filepath.Join(root, "README.md"), "hi")
	if err := os.Symlink("package.json", filepath.Join(root, "link")); err != nil {
		t.Log("symlink skipped")
	}

	for _, p := range []string{"", "."} {
		res, err := w.ListDir(p)
		if err != nil {
			t.Fatal(err)
		}
		want := "README.md\npackage.json\npublic/\nsrc/"
		if res.Output != want {
			t.Errorf("ListDir(%q) = %q, want %q", p, res.Output, want)
		}
	}
	if _, err := w.ListDir("dist"); !errors.Is(err, ErrPathForbidden) {
		t.Errorf("list dist err = %v, want ErrPathForbidden", err)
	}
	if _, err := w.ListDir("package.json"); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("list file err = %v, want ErrNotDirectory", err)
	}
	if _, err := w.ListDir("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("list missing err = %v, want ErrNotFound", err)
	}
	if res, err := w.ListDir("src"); err != nil || res.Output != "(empty directory)" {
		t.Errorf("empty dir = %q, %v", res.Output, err)
	}
}

func TestListDirEntryCap(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{MaxListEntries: 3})
	for i := 0; i < 5; i++ {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("f%d.txt", i)), "x")
	}
	res, err := w.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "f0.txt\nf1.txt\nf2.txt\n…truncated, 2 more entries" {
		t.Errorf("output = %q", res.Output)
	}
}

func TestApplyIdempotentReplay(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	p := filepath.Join(root, "a.txt")

	first, err := w.Apply("op-1", FileOp{Kind: FileOpWriteFile, Path: "a.txt", Content: "v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed {
		t.Error("first call must not be a replay")
	}

	// Something else changes the file; a replay must not clobber it.
	mustWriteFile(t, p, "changed externally\n")
	second, err := w.Apply("op-1", FileOp{Kind: FileOpWriteFile, Path: "a.txt", Content: "v1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Output != first.Output || !second.OK {
		t.Errorf("replay = %+v, first = %+v", second, first)
	}
	if got := mustReadFile(t, p); got != "changed externally\n" {
		t.Errorf("replay re-applied the write: %q", got)
	}

	// Same for edit_file: a re-applied edit would now fail with not found.
	if _, err := w.Apply("op-2", FileOp{Kind: FileOpEditFile, Path: "a.txt", OldString: "changed", NewString: "edited"}); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, p, "something else\n")
	res, err := w.Apply("op-2", FileOp{Kind: FileOpEditFile, Path: "a.txt", OldString: "changed", NewString: "edited"})
	if err != nil || !res.Replayed {
		t.Errorf("edit replay = %+v, %v", res, err)
	}
	if got := mustReadFile(t, p); got != "something else\n" {
		t.Errorf("edit replay touched the file: %q", got)
	}

	// Reusing an op_id for another operation is a client bug, not a replay.
	if _, err := w.Apply("op-1", FileOp{Kind: FileOpWriteFile, Path: "other.txt", Content: "x"}); !errors.Is(err, ErrOpIDReused) {
		t.Errorf("different path err = %v, want ErrOpIDReused", err)
	}
	if _, err := w.Apply("op-1", FileOp{Kind: FileOpEditFile, Path: "a.txt", OldString: "a", NewString: "b"}); !errors.Is(err, ErrOpIDReused) {
		t.Errorf("different kind err = %v, want ErrOpIDReused", err)
	}
	if _, err := os.Stat(filepath.Join(root, "other.txt")); err == nil {
		t.Error("conflicting op was executed")
	}
}

func TestApplyFailuresAreNotRecorded(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello\n")

	op := FileOp{Kind: FileOpEditFile, Path: "a.txt", OldString: "world", NewString: "there"}
	if _, err := w.Apply("op-x", op); !errors.Is(err, ErrEditNotFound) {
		t.Fatalf("err = %v, want ErrEditNotFound", err)
	}
	if _, ok := w.LookupOp("op-x"); ok {
		t.Error("failed op was recorded")
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello world\n")
	if res, err := w.Apply("op-x", op); err != nil || res.Replayed {
		t.Errorf("retry = %+v, %v", res, err)
	}
}

func TestApplyDispatchAndValidation(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hi\n")

	if res, err := w.Apply("", FileOp{Kind: FileOpListDir}); err != nil || res.Output != "a.txt" {
		t.Errorf("list_dir = %+v, %v", res, err)
	}
	if res, err := w.Apply("", FileOp{Kind: FileOpReadFile, Path: "a.txt"}); err != nil || !strings.Contains(res.Output, "hi") {
		t.Errorf("read_file = %+v, %v", res, err)
	}
	if _, err := w.Apply("", FileOp{Kind: FileOpWriteFile, Path: "b.txt", Content: "x"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("write without op_id err = %v, want ErrInvalidArgument", err)
	}
	if _, err := w.Apply(strings.Repeat("a", 129), FileOp{Kind: FileOpWriteFile, Path: "b.txt", Content: "x"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("long op_id err = %v, want ErrInvalidArgument", err)
	}
	if _, err := w.Apply("op", FileOp{Kind: "rm_rf", Path: "a.txt"}); !errors.Is(err, ErrUnknownOp) {
		t.Errorf("unknown kind err = %v, want ErrUnknownOp", err)
	}
}

func TestOpLogSurvivesRestart(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	if _, err := w.Apply("op-1", FileOp{Kind: FileOpWriteFile, Path: "a.txt", Content: "v1\n"}); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordOp("build-1", "build", map[string]any{"exit_code": 0, "output_tail": "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordOp("build-1", "build", map[string]any{"exit_code": 1}); !errors.Is(err, ErrOpIDReused) {
		t.Errorf("duplicate RecordOp err = %v, want ErrOpIDReused", err)
	}

	// New instance on the same root, as after a process restart.
	w2, err := NewWorkspace(root, WorkspaceLimits{})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "changed\n")
	res, err := w2.Apply("op-1", FileOp{Kind: FileOpWriteFile, Path: "a.txt", Content: "v1\n"})
	if err != nil || !res.Replayed {
		t.Fatalf("replay after restart = %+v, %v", res, err)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "changed\n" {
		t.Errorf("file clobbered after restart: %q", got)
	}
	rec, ok := w2.LookupOp("build-1")
	if !ok || rec.Kind != "build" || !strings.Contains(string(rec.Result), `"output_tail":"ok"`) {
		t.Errorf("LookupOp(build-1) = %+v, %v", rec, ok)
	}
	if _, ok := w2.LookupOp("nope"); ok {
		t.Error("LookupOp found a missing op")
	}
}

func TestOpLogToleratesTornWrite(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	if _, err := w.Apply("op-1", FileOp{Kind: FileOpWriteFile, Path: "a.txt", Content: "v1\n"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash in the middle of appending the next record.
	logPath := filepath.Join(root, ".orbit", "ops.jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"op_id":"op-torn","kind":"wri`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	w2, err := NewWorkspace(root, WorkspaceLimits{})
	if err != nil {
		t.Fatalf("restart after torn write: %v", err)
	}
	if _, ok := w2.LookupOp("op-1"); !ok {
		t.Error("intact record lost")
	}
	if _, ok := w2.LookupOp("op-torn"); ok {
		t.Error("torn record was accepted")
	}
	// A new record must land on its own line and survive another restart.
	if _, err := w2.Apply("op-2", FileOp{Kind: FileOpWriteFile, Path: "b.txt", Content: "v2\n"}); err != nil {
		t.Fatal(err)
	}
	w3, err := NewWorkspace(root, WorkspaceLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w3.LookupOp("op-2"); !ok {
		t.Error("record appended after a torn line was lost")
	}
}

func TestOpLogRefusesSymlinkedOrbitDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".orbit")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, err := NewWorkspace(root, WorkspaceLimits{}); !errors.Is(err, ErrOpLog) {
		t.Errorf("err = %v, want ErrOpLog", err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	const n = 32

	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op := FileOp{Kind: FileOpWriteFile, Path: fmt.Sprintf("dir%d/file%d.txt", i%4, i), Content: fmt.Sprintf("content %d\n", i)}
			id := fmt.Sprintf("op-%d", i)
			if _, err := w.Apply(id, op); err != nil {
				errs <- err
				return
			}
			// Duplicate delivery of the same op racing with others.
			res, err := w.Apply(id, op)
			if err != nil {
				errs <- err
				return
			}
			if !res.Replayed {
				errs <- fmt.Errorf("op %s was applied twice", id)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for i := 0; i < n; i++ {
		p := filepath.Join(root, fmt.Sprintf("dir%d/file%d.txt", i%4, i))
		if got := mustReadFile(t, p); got != fmt.Sprintf("content %d\n", i) {
			t.Errorf("%s = %q", p, got)
		}
	}
	w2, err := NewWorkspace(root, WorkspaceLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, ok := w2.LookupOp(fmt.Sprintf("op-%d", i)); !ok {
			t.Errorf("op-%d missing from the persisted log", i)
		}
	}
}

func TestWorkspaceRegistry(t *testing.T) {
	workDir := t.TempDir()
	reg := NewWorkspaceRegistry(workDir, WorkspaceLimits{})

	if _, err := reg.Get("../evil"); !errors.Is(err, ErrInvalidSessionID) {
		t.Errorf("traversal id err = %v, want ErrInvalidSessionID", err)
	}
	if _, err := reg.Get("sess-1"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("unknown session err = %v, want ErrSessionNotFound", err)
	}
	if err := os.Mkdir(filepath.Join(workDir, "sess-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := reg.Get("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := reg.Get("sess-1")
	if a != b {
		t.Error("registry returned two Workspaces for one session")
	}
	reg.Forget("sess-1")
	if c, _ := reg.Get("sess-1"); c == a {
		t.Error("Forget did not drop the cached Workspace")
	}
}

func TestIsProtectedName(t *testing.T) {
	for _, n := range []string{".git", "node_modules", "dist", ".orbit", ".GIT", "Dist"} {
		if !IsProtectedName(n) {
			t.Errorf("%q should be protected", n)
		}
	}
	for _, n := range []string{".gitignore", "distro", "src", "node_modules2", ""} {
		if IsProtectedName(n) {
			t.Errorf("%q should not be protected", n)
		}
	}
}

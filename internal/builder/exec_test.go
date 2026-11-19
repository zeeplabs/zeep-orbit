package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The tests drive the executor with fake npm/npx shell scripts, so they need
// neither Node nor network. The scripts live outside the session root.

const testExecPath = "/usr/bin:/bin"

type execFixture struct {
	ex   *Executor
	root string // session root
	dir  string // scripts and bookkeeping files, outside the root
}

func writeExecScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// newExecFixture builds an Executor wired to fake npm/npx scripts.
func newExecFixture(t *testing.T, npmBody, npxBody string, mutate func(*ExecConfig)) *execFixture {
	t.Helper()
	dir := t.TempDir()
	cfg := ExecConfig{
		NpmPath:       writeExecScript(t, dir, "npm", npmBody),
		NpxPath:       writeExecScript(t, dir, "npx", npxBody),
		Path:          testExecPath,
		Home:          dir,
		NpmRegistry:   "http://registry.invalid:4873/",
		NpmCache:      filepath.Join(dir, "cache"),
		MaxOutputTail: 4096,
		WaitDelay:     200 * time.Millisecond,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	ex, err := NewExecutor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &execFixture{ex: ex, root: t.TempDir(), dir: dir}
}

// argvScript prints one argument per line, prefixed, then exits with code.
func argvScript(code int) string {
	return fmt.Sprintf(`for a in "$@"; do printf 'ARG:%%s\n' "$a"; done
exit %d`, code)
}

func argvLines(out string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "ARG:"); ok {
			got = append(got, v)
		}
	}
	return got
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(b)))
}

// procAlive reports whether pid is a live process (zombies count as dead: an
// orphan reparented to a PID 1 that never reaps stays a zombie forever).
func procAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s != "" && !strings.HasPrefix(s, "Z")
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pid file %s: %v", filepath.Base(path), err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestExecArgvPerStep(t *testing.T) {
	f := newExecFixture(t, argvScript(0), argvScript(0), nil)
	ctx := context.Background()
	const base = "/preview/abc-123/"

	cases := []struct {
		name string
		step BuildStep
		want []string
	}{
		{"install without lockfile", StepInstall, []string{"install", "--ignore-scripts", "--no-audit", "--no-fund"}},
		{"typecheck", StepTypecheck, []string{"--no-install", "tsc", "-b"}},
		{"lint", StepLint, []string{"run", "lint", "--if-present"}},
		{"build", StepBuild, []string{"--no-install", "vite", "build", "--base", base}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := f.ex.Run(ctx, f.root, tc.step, base)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := argvLines(res.OutputTail); !eqStrings(got, tc.want) {
				t.Fatalf("argv = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("install with lockfile uses npm ci", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(f.root, "package-lock.json"), "{}")
		res, err := f.ex.Run(ctx, f.root, StepInstall, "")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"ci", "--ignore-scripts", "--no-audit", "--no-fund", "--prefer-offline"}
		if got := argvLines(res.OutputTail); !eqStrings(got, want) {
			t.Fatalf("argv = %q, want %q", got, want)
		}
	})
}

func TestExecChildEnvIsAllowlisted(t *testing.T) {
	t.Setenv("ORBIT_SENTINEL_SECRET", "must-not-leak")
	t.Setenv("BUILDER_SHARED_SECRET", "also-secret")
	t.Setenv("NODE_ENV", "development")

	f := newExecFixture(t, `env`, `env`, nil)
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatal(err)
	}
	out := res.OutputTail
	for _, leak := range []string{"ORBIT_SENTINEL_SECRET", "must-not-leak", "BUILDER_SHARED_SECRET", "NODE_ENV"} {
		if strings.Contains(out, leak) {
			t.Fatalf("parent env leaked %q into the child:\n%s", leak, out)
		}
	}
	for _, want := range []string{
		"PATH=" + testExecPath,
		"HOME=" + f.dir,
		"npm_config_registry=http://registry.invalid:4873/",
		"npm_config_cache=" + filepath.Join(f.dir, "cache"),
		"CI=true",
		"npm_config_update_notifier=false",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Fatalf("missing %q in child env:\n%s", want, out)
		}
	}
	// Whatever else is there must come from the shell itself, not the parent.
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "CI": true, "PWD": true, "SHLVL": true, "_": true, "OLDPWD": true,
		"npm_config_registry": true, "npm_config_cache": true,
		"npm_config_update_notifier": true, "npm_config_fund": true, "npm_config_audit": true,
	}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		name, _, _ := strings.Cut(l, "=")
		if !allowed[name] && name != "npm_config_replace-registry-host" {
			t.Fatalf("unexpected variable %q in child env", name)
		}
	}
}

func TestExecWorkingDirIsSessionRoot(t *testing.T) {
	f := newExecFixture(t, `pwd -P`, `pwd -P`, nil)
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(f.root)
	if got := strings.TrimSpace(res.OutputTail); got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
}

func TestExecTimeoutKillsProcessGroup(t *testing.T) {
	pids := t.TempDir()
	body := fmt.Sprintf(`sleep 60 &
echo $! > %[1]s/child.pid
echo $$ > %[1]s/parent.pid
wait`, pids)
	f := newExecFixture(t, body, body, func(c *ExecConfig) {
		c.Timeouts.Lint = 800 * time.Millisecond
	})
	start := time.Now()
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout was not enforced promptly: %v", elapsed)
	}
	if !res.TimedOut || res.ExitCode != -1 {
		t.Fatalf("want TimedOut and exit -1, got %+v", res)
	}
	if !strings.Contains(res.OutputTail, "timed out") {
		t.Fatalf("tail lacks the timeout note: %q", res.OutputTail)
	}
	for _, name := range []string{"parent.pid", "child.pid"} {
		pid := readPID(t, filepath.Join(pids, name))
		deadline := time.Now().Add(3 * time.Second)
		for procAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		if procAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("%s (pid %d) survived the timeout", name, pid)
		}
	}
}

func TestExecCallerCancelIsAnError(t *testing.T) {
	f := newExecFixture(t, `sleep 30`, `sleep 30`, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, err := f.ex.Run(ctx, f.root, StepLint, "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context error, got res=%+v err=%v", res, err)
	}
}

func TestExecOutputTruncationKeepsTail(t *testing.T) {
	f := newExecFixture(t, `seq 1 5000`, `seq 1 5000`, func(c *ExecConfig) { c.MaxOutputTail = 200 })
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for i := 1; i <= 5000; i++ {
		total += len(strconv.Itoa(i)) + 1
	}
	if res.OutputBytes != int64(total) {
		t.Fatalf("OutputBytes = %d, want %d", res.OutputBytes, total)
	}
	marker, rest, ok := strings.Cut(res.OutputTail, "\n")
	if !ok || !strings.HasPrefix(marker, "…[output truncated, showing last ") || !strings.HasSuffix(marker, " bytes]…") {
		t.Fatalf("missing truncation marker: %q", res.OutputTail)
	}
	if !strings.HasSuffix(rest, "4999\n5000\n") || len(rest) > 200 {
		t.Fatalf("tail is wrong (len %d): %q", len(rest), rest)
	}
	if !strings.Contains(marker, fmt.Sprintf("last %d bytes", len(rest))) {
		t.Fatalf("marker does not state the shown size %d: %q", len(rest), marker)
	}

	t.Run("short output is untouched", func(t *testing.T) {
		g := newExecFixture(t, `printf 'hello\n'`, `printf 'hello\n'`, nil)
		res, err := g.ex.Run(context.Background(), g.root, StepLint, "")
		if err != nil || res.OutputTail != "hello\n" {
			t.Fatalf("got %q, %v", res.OutputTail, err)
		}
	})
}

func TestExecOutputIsSanitized(t *testing.T) {
	body := `printf 'a\000b\377c\n'`
	f := newExecFixture(t, body, body, nil)
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(res.OutputTail, 0) {
		t.Fatal("NUL byte survived")
	}
	if res.OutputTail != "ab�c\n" {
		t.Fatalf("got %q", res.OutputTail)
	}

	// A multi-byte rune cut in half by the tail window must not leak as garbage.
	h := newExecFixture(t, `i=0; while [ $i -lt 100 ]; do printf 'é'; i=$((i+1)); done; printf '\n'`, `true`,
		func(c *ExecConfig) { c.MaxOutputTail = 65 }) // odd size forces a split rune
	res, err = h.ex.Run(context.Background(), h.root, StepLint, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(res.OutputTail, '�') {
		t.Fatalf("split rune leaked as replacement char: %q", res.OutputTail)
	}
}

func TestExecNonZeroExitIsAResult(t *testing.T) {
	f := newExecFixture(t, `echo "boom" >&2; exit 3`, `exit 0`, nil)
	res, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if err != nil {
		t.Fatalf("non-zero exit must not be a Go error: %v", err)
	}
	if res.ExitCode != 3 || res.TimedOut || !strings.Contains(res.OutputTail, "boom") {
		t.Fatalf("got %+v", res)
	}

	t.Run("killed by signal", func(t *testing.T) {
		g := newExecFixture(t, `kill -9 $$`, `true`, nil)
		res, err := g.ex.Run(context.Background(), g.root, StepLint, "")
		if err != nil || res.ExitCode != 128+9 || res.TimedOut {
			t.Fatalf("got %+v, %v", res, err)
		}
	})
}

func TestExecRejectsInvalidPreviewBase(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	script := fmt.Sprintf(`echo ran >> %s`, marker)
	f := newExecFixture(t, script, script, nil)

	bad := []string{
		"",
		"x/",
		"/x",
		"/x/ --outDir /etc/",
		"/x/;rm -rf /;/",
		"/x/$(id)/",
		"/x/`id`/",
		"/../x/",
		"/a/../",
		"/./",
		"//evil.example/",
		"/a//b/",
		"/a b/",
		"/x/\n",
		"/x/\x00/",
		"/é/",
		"/" + strings.Repeat("a", 300) + "/",
		"--base=/x/",
		".",
		"./x/",
		"../",
		"./ --outDir /etc/",
		"https://evil.example/",
	}
	for _, base := range bad {
		_, err := f.ex.Run(context.Background(), f.root, StepBuild, base)
		if !errors.Is(err, ErrInvalidPreviewBase) {
			t.Errorf("base %q: want ErrInvalidPreviewBase, got %v", base, err)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("npx ran for an invalid base")
	}
	for _, ok := range []string{"./", "/", "/preview/", "/preview/abc-123/", "/a.b/c_d/e~f/"} {
		if err := ValidatePreviewBase(ok); err != nil {
			t.Errorf("base %q should be valid: %v", ok, err)
		}
	}
	// Other steps do not need a base.
	if _, err := f.ex.Run(context.Background(), f.root, StepTypecheck, ""); err != nil {
		t.Fatalf("typecheck with empty base: %v", err)
	}
}

func TestExecUnknownStepAndBadRoot(t *testing.T) {
	f := newExecFixture(t, `true`, `true`, nil)
	ctx := context.Background()

	if _, err := ParseBuildStep("rm -rf /"); !errors.Is(err, ErrUnknownStep) {
		t.Fatalf("ParseBuildStep: %v", err)
	}
	for _, s := range []string{"install", "typecheck", "lint", "build"} {
		if _, err := ParseBuildStep(s); err != nil {
			t.Fatalf("ParseBuildStep(%q): %v", s, err)
		}
	}
	if _, err := f.ex.Run(ctx, f.root, BuildStep("test"), ""); !errors.Is(err, ErrUnknownStep) {
		t.Fatalf("Run unknown step: %v", err)
	}
	if _, err := f.ex.Run(ctx, "relative/dir", StepLint, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("relative root: %v", err)
	}
	file := filepath.Join(f.dir, "file")
	mustWriteFile(t, file, "x")
	if _, err := f.ex.Run(ctx, file, StepLint, ""); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("file root: %v", err)
	}
	if _, err := f.ex.Run(ctx, filepath.Join(f.dir, "missing"), StepLint, ""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing root: %v", err)
	}
	link := filepath.Join(f.dir, "link")
	if err := os.Symlink(f.root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ex.Run(ctx, link, StepLint, ""); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("symlink root: %v", err)
	}
}

func TestExecWorkDirContainment(t *testing.T) {
	work := t.TempDir()
	inside := filepath.Join(work, "sess-1")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	f := newExecFixture(t, `true`, `true`, func(c *ExecConfig) { c.WorkDir = work })
	if _, err := f.ex.Run(context.Background(), inside, StepLint, ""); err != nil {
		t.Fatalf("inside: %v", err)
	}
	if _, err := f.ex.Run(context.Background(), f.root, StepLint, ""); !errors.Is(err, ErrPathOutside) {
		t.Fatalf("outside: %v", err)
	}
	if _, err := f.ex.Run(context.Background(), work, StepLint, ""); !errors.Is(err, ErrPathOutside) {
		t.Fatalf("work dir itself: %v", err)
	}
}

func TestExecStartFailureIsAnError(t *testing.T) {
	f := newExecFixture(t, `true`, `true`, func(c *ExecConfig) {
		c.NpmPath = "/nonexistent/npm"
	})
	_, err := f.ex.Run(context.Background(), f.root, StepLint, "")
	if !errors.Is(err, ErrExecStart) {
		t.Fatalf("want ErrExecStart, got %v", err)
	}
	if strings.Contains(err.Error(), "/nonexistent") {
		t.Fatalf("error leaks the host path: %v", err)
	}

	g, err := NewExecutor(ExecConfig{Path: "/nonexistent-dir"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(context.Background(), t.TempDir(), StepLint, ""); !errors.Is(err, ErrExecStart) {
		t.Fatalf("npm missing from PATH: %v", err)
	}
	if _, err := NewExecutor(ExecConfig{NpmPath: "npm"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("relative NpmPath accepted: %v", err)
	}
}

func TestExecInstallFallsBackWhenLockfileOutOfSync(t *testing.T) {
	newFx := func(t *testing.T, ciBody string) (*execFixture, string) {
		dir := t.TempDir()
		calls := filepath.Join(dir, "calls")
		body := fmt.Sprintf(`echo "$1" >> %s
if [ "$1" = ci ]; then
%s
fi
echo "ok $1"`, calls, ciBody)
		f := newExecFixture(t, body, `true`, nil)
		return f, calls
	}
	callList := func(t *testing.T, path string) []string {
		b, _ := os.ReadFile(path)
		return strings.Fields(string(b))
	}

	t.Run("out of sync with npm 10 wording retries with npm install", func(t *testing.T) {
		f, calls := newFx(t, "echo 'npm error `npm ci` can only install packages when your package.json and package-lock.json or npm-shrinkwrap.json are in sync.' >&2; exit 1")
		mustWriteFile(t, filepath.Join(f.root, "package-lock.json"), "{}")
		res, err := f.ex.Run(context.Background(), f.root, StepInstall, "")
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("got %+v", res)
		}
		if got := callList(t, calls); !eqStrings(got, []string{"ci", "install"}) {
			t.Fatalf("calls = %q", got)
		}
	})

	t.Run("out of sync retries once with npm install", func(t *testing.T) {
		f, calls := newFx(t, `echo "npm ERR! npm ci can only install packages when your package.json and package-lock.json are in sync" >&2; exit 1`)
		mustWriteFile(t, filepath.Join(f.root, "package-lock.json"), "{}")
		res, err := f.ex.Run(context.Background(), f.root, StepInstall, "")
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 || !strings.Contains(res.OutputTail, "ok install") {
			t.Fatalf("got %+v", res)
		}
		if got := callList(t, calls); !eqStrings(got, []string{"ci", "install"}) {
			t.Fatalf("calls = %q", got)
		}
		if !strings.Contains(res.OutputTail, "retrying with npm install") {
			t.Fatalf("tail does not explain the retry: %q", res.OutputTail)
		}
	})

	t.Run("other npm ci failure is not retried", func(t *testing.T) {
		f, calls := newFx(t, `echo "npm ERR! network" >&2; exit 1`)
		mustWriteFile(t, filepath.Join(f.root, "package-lock.json"), "{}")
		res, err := f.ex.Run(context.Background(), f.root, StepInstall, "")
		if err != nil || res.ExitCode != 1 {
			t.Fatalf("got %+v, %v", res, err)
		}
		if got := callList(t, calls); !eqStrings(got, []string{"ci"}) {
			t.Fatalf("calls = %q", got)
		}
	})

	t.Run("no lockfile goes straight to npm install", func(t *testing.T) {
		f, calls := newFx(t, `exit 1`)
		res, err := f.ex.Run(context.Background(), f.root, StepInstall, "")
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("got %+v, %v", res, err)
		}
		if got := callList(t, calls); !eqStrings(got, []string{"install"}) {
			t.Fatalf("calls = %q", got)
		}
	})
}

func TestRunStepIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	f := newExecFixture(t, `true`, fmt.Sprintf(`echo run >> %s; sleep 0.1; echo built`, count), nil)
	w, err := NewWorkspace(f.root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	first, err := f.ex.RunStep(ctx, w, "op-1", StepTypecheck, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.ExitCode != 0 || !strings.Contains(first.OutputTail, "built") {
		t.Fatalf("first = %+v", first)
	}
	second, err := f.ex.RunStep(ctx, w, "op-1", StepTypecheck, "")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.OutputTail != first.OutputTail || second.DurationMs != first.DurationMs {
		t.Fatalf("second = %+v, first = %+v", second, first)
	}
	if n := countLines(t, count); n != 1 {
		t.Fatalf("script ran %d times, want 1", n)
	}

	if _, err := f.ex.RunStep(ctx, w, "op-1", StepLint, ""); !errors.Is(err, ErrOpIDReused) {
		t.Fatalf("op_id reuse for another step: %v", err)
	}
	if _, err := f.ex.RunStep(ctx, w, "", StepLint, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty op_id: %v", err)
	}
	if _, err := f.ex.RunStep(ctx, w, "op-x", BuildStep("nope"), ""); !errors.Is(err, ErrUnknownStep) {
		t.Fatalf("unknown step: %v", err)
	}
	if _, err := f.ex.RunStep(ctx, w, "op-b", StepBuild, "/bad"); !errors.Is(err, ErrInvalidPreviewBase) {
		t.Fatalf("bad base: %v", err)
	}
	// An invalid request is not recorded, so the op_id can be used properly.
	if _, ok := w.LookupOp("op-b"); ok {
		t.Fatal("rejected request was recorded")
	}

	// The record survives a restart of the workspace (new Workspace, same root).
	w2, err := NewWorkspace(f.root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.ex.RunStep(ctx, w2, "op-1", StepTypecheck, "")
	if err != nil || !again.Replayed {
		t.Fatalf("after restart: %+v, %v", again, err)
	}
	if n := countLines(t, count); n != 1 {
		t.Fatalf("script ran %d times after restart, want 1", n)
	}
}

func TestRunStepSerializesDuplicateOpIDs(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	f := newExecFixture(t, `true`, fmt.Sprintf(`echo run >> %s; sleep 0.2; echo built`, count), nil)
	w, err := NewWorkspace(f.root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}

	const n = 6
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		replayed int
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := f.ex.RunStep(context.Background(), w, "same-op", StepTypecheck, "")
			if err != nil {
				t.Errorf("RunStep: %v", err)
				return
			}
			if res.Replayed {
				mu.Lock()
				replayed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if c := countLines(t, count); c != 1 {
		t.Fatalf("script ran %d times for one op_id, want 1", c)
	}
	if replayed != n-1 {
		t.Fatalf("replayed = %d, want %d", replayed, n-1)
	}
}

func TestRunStepWaitHonorsContext(t *testing.T) {
	f := newExecFixture(t, `true`, `sleep 1`, nil)
	w, err := NewWorkspace(f.root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = f.ex.RunStep(context.Background(), w, "slow", StepTypecheck, "")
	}()
	time.Sleep(150 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := f.ex.RunStep(ctx, w, "slow", StepTypecheck, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting duplicate should honor ctx, got %v", err)
	}
	<-done
}

// buildScript writes dist/ from <dir>/content and exits with <dir>/code.
func buildScript(dir string) string {
	return fmt.Sprintf(`rm -rf dist
mkdir -p dist/assets
cat %[1]s/content > dist/index.html
echo asset > dist/assets/a.js
if [ -f %[1]s/link ]; then ln -s /etc/passwd dist/leak; fi
exit $(cat %[1]s/code)`, dir)
}

func TestPreviewCopiedOnSuccessAndKeptWhenLaterBuildFails(t *testing.T) {
	ctl := t.TempDir()
	f := newExecFixture(t, `true`, buildScript(ctl), nil)
	ctx := context.Background()
	set := func(content string, code int) {
		mustWriteFile(t, filepath.Join(ctl, "content"), content)
		mustWriteFile(t, filepath.Join(ctl, "code"), strconv.Itoa(code))
	}
	preview := PreviewDir(f.root)
	if _, err := os.Stat(preview); err == nil {
		t.Fatal("preview exists before any build")
	}
	if d := f.ex.LastBuildDir(f.root); d != "" {
		t.Fatalf("LastBuildDir before build = %q", d)
	}

	set("v1", 0)
	res, err := f.ex.Run(ctx, f.root, StepBuild, "/preview/s/")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("build 1: %+v, %v", res, err)
	}
	if got := mustReadFile(t, filepath.Join(preview, "index.html")); got != "v1" {
		t.Fatalf("preview index = %q", got)
	}
	if got := mustReadFile(t, filepath.Join(preview, "assets", "a.js")); got != "asset\n" {
		t.Fatalf("preview asset = %q", got)
	}
	if d := f.ex.LastBuildDir(f.root); d != filepath.Join(f.root, "dist") {
		t.Fatalf("LastBuildDir = %q", d)
	}

	// A failing build wipes and half-writes dist/, but the preview stays on v1.
	set("v2-broken", 2)
	res, err = f.ex.Run(ctx, f.root, StepBuild, "/preview/s/")
	if err != nil || res.ExitCode != 2 {
		t.Fatalf("build 2: %+v, %v", res, err)
	}
	if got := mustReadFile(t, filepath.Join(preview, "index.html")); got != "v1" {
		t.Fatalf("preview changed after failed build: %q", got)
	}

	// A later success replaces it atomically and leaves no temp/old dirs.
	set("v3", 0)
	if res, err := f.ex.Run(ctx, f.root, StepBuild, "/preview/s/"); err != nil || res.ExitCode != 0 {
		t.Fatalf("build 3: %+v, %v", res, err)
	}
	if got := mustReadFile(t, filepath.Join(preview, "index.html")); got != "v3" {
		t.Fatalf("preview index = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Join(f.root, ".orbit"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "preview.") {
			t.Fatalf("leftover %q in .orbit", e.Name())
		}
	}
}

func TestPreviewSkipsSymlinksAndRejectsMissingDist(t *testing.T) {
	ctl := t.TempDir()
	f := newExecFixture(t, `true`, buildScript(ctl), nil)
	mustWriteFile(t, filepath.Join(ctl, "content"), "ok")
	mustWriteFile(t, filepath.Join(ctl, "code"), "0")
	mustWriteFile(t, filepath.Join(ctl, "link"), "1")

	if res, err := f.ex.Run(context.Background(), f.root, StepBuild, "/p/"); err != nil || res.ExitCode != 0 {
		t.Fatalf("build: %+v, %v", res, err)
	}
	if _, err := os.Lstat(filepath.Join(PreviewDir(f.root), "leak")); err == nil {
		t.Fatal("symlink from dist/ was copied into the preview")
	}

	// Exit 0 without dist/ is a failed build from the agent's point of view.
	g := newExecFixture(t, `true`, `rm -rf dist; echo done`, nil)
	res, err := g.ex.Run(context.Background(), g.root, StepBuild, "/p/")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.OutputTail, "dist/ was not produced") {
		t.Fatalf("got %+v", res)
	}
	if _, err := os.Stat(PreviewDir(g.root)); err == nil {
		t.Fatal("preview created without dist/")
	}
}

func TestPreviewSizeLimit(t *testing.T) {
	f := newExecFixture(t, `true`,
		`mkdir -p dist; head -c 5000 /dev/zero > dist/big.bin`,
		func(c *ExecConfig) { c.MaxPreviewBytes = 1000 })
	res, err := f.ex.Run(context.Background(), f.root, StepBuild, "/p/")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.OutputTail, "preview size limit") {
		t.Fatalf("got %+v", res)
	}
	if _, err := os.Stat(PreviewDir(f.root)); err == nil {
		t.Fatal("oversized dist became the preview")
	}
	// No temp directory is left behind.
	entries, _ := os.ReadDir(filepath.Join(f.root, ".orbit"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "preview.tmp-") {
			t.Fatalf("leftover %q", e.Name())
		}
	}
}

func TestBuildLogPersistedAndReadable(t *testing.T) {
	f := newExecFixture(t, `seq 1 3000`, `echo typecheck-ok`, nil)
	w, err := NewWorkspace(f.root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := ReadBuildLog(w, StepLint, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before any run: %v", err)
	}
	if _, err := ReadBuildLog(w, BuildStep("../x"), 0); !errors.Is(err, ErrUnknownStep) {
		t.Fatalf("bad step: %v", err)
	}

	if _, err := f.ex.RunStep(ctx, w, "o1", StepTypecheck, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadBuildLog(w, StepTypecheck, 0); err != nil || got != "typecheck-ok\n" {
		t.Fatalf("typecheck log = %q, %v", got, err)
	}

	res, err := f.ex.RunStep(ctx, w, "o2", StepLint, "")
	if err != nil {
		t.Fatal(err)
	}
	full, err := ReadBuildLog(w, StepLint, 1<<20)
	if err != nil || full != res.OutputTail {
		t.Fatalf("full log differs from the result: %v", err)
	}
	tail, err := ReadBuildLog(w, StepLint, 20)
	if err != nil {
		t.Fatal(err)
	}
	marker, rest, _ := strings.Cut(tail, "\n")
	if !strings.HasPrefix(marker, "…[output truncated, showing last ") || !strings.HasSuffix(rest, "2999\n3000\n") || len(rest) > 20 {
		t.Fatalf("tail = %q", tail)
	}

	// A replay does not rewrite the log of another step, and logs are per step.
	if got, _ := ReadBuildLog(w, StepTypecheck, 0); got != "typecheck-ok\n" {
		t.Fatalf("typecheck log was clobbered: %q", got)
	}
	// The build log is a file in the protected .orbit directory.
	if !IsProtectedName(orbitDirName) {
		t.Fatal(".orbit must stay protected from agent file ops")
	}
}

func TestReadBuildLogRefusesSymlink(t *testing.T) {
	f := newExecFixture(t, `true`, `true`, nil)
	w, err := NewWorkspace(f.root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(f.dir, "secret")
	mustWriteFile(t, secret, "TOP-SECRET")
	if err := os.MkdirAll(filepath.Join(f.root, ".orbit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(f.root, ".orbit", "build-lint.log")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBuildLog(w, StepLint, 0)
	if err == nil || strings.Contains(got, "TOP-SECRET") {
		t.Fatalf("symlinked log was followed: %q, %v", got, err)
	}

	// Writing replaces the symlink instead of writing through it.
	if _, err := f.ex.Run(context.Background(), f.root, StepLint, ""); err != nil {
		t.Fatal(err)
	}
	if mustReadFile(t, secret) != "TOP-SECRET" {
		t.Fatal("write went through the symlink")
	}
	if _, err := ReadBuildLog(w, StepLint, 0); err != nil {
		t.Fatalf("log unreadable after rewrite: %v", err)
	}
}

func TestKeyedLockerReleasesEntries(t *testing.T) {
	var k keyedLocker
	un, err := k.acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := k.acquire(ctx, "a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquire: %v", err)
	}
	if un2, err := k.acquire(context.Background(), "b"); err != nil {
		t.Fatalf("other key blocked: %v", err)
	} else {
		un2()
	}
	un()
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.m) != 0 {
		t.Fatalf("entries leaked: %d", len(k.m))
	}
}

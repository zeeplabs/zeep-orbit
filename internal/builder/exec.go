package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Build steps run package-manager commands on code written by an LLM that may
// have been prompt-injected. The guarantees implemented here:
//
//   - every step maps to a fixed argv executed directly (never through a
//     shell); the only caller-controlled value, preview_base, is validated
//     against a strict pattern and travels as its own argv element;
//   - the child environment is built from scratch from an allowlist, so the
//     builder's own environment (BUILDER_SHARED_SECRET and friends) never
//     reaches code that the agent can influence;
//   - each step has a hard timeout, and the whole process group is killed on
//     timeout, cancellation or when the step ends, so no straggler survives;
//   - output is captured through a bounded tail buffer (memory is capped no
//     matter how much a step prints).
//
// What is NOT enforced in-process: CPU and memory limits. Go's os/exec cannot
// set rlimits on the child portably without cgo or a wrapper binary, and a
// wrapper (prlimit, a shell) is exactly what this package refuses to add. Those
// limits are enforced by the container's cgroups (compose/k8s manifests,
// T38-T40); a step killed by the kernel OOM killer shows up as exit code
// 128+9. Timeout and output cap are the only resource controls that hold even
// when the process runs outside a container.
//
// Accepted risk, same as the design: "npm run lint" and the vite config are
// code the agent wrote, so they can run arbitrary commands as the builder user.
// The blast radius is the container: no secrets in the env, writable /work and
// /tmp only, and the only network path is the registry proxy.
//
// This file uses syscall.Setpgid and kill(-pgid), so it is Unix-only (Linux in
// production, macOS for development).

// Build step names accepted by [ParseBuildStep].
const (
	// StepInstall installs dependencies with lifecycle scripts disabled.
	StepInstall BuildStep = "install"
	// StepTypecheck runs the TypeScript project build in check mode (tsc -b).
	StepTypecheck BuildStep = "typecheck"
	// StepLint runs the project's lint script when it defines one.
	StepLint BuildStep = "lint"
	// StepBuild produces the production bundle with vite.
	StepBuild BuildStep = "build"
)

// OpKindBuild is the idempotency-log kind recorded for build steps.
const OpKindBuild = "build"

const (
	defaultExecPath       = "/usr/local/bin:/usr/bin:/bin"
	defaultExecHome       = "/tmp"
	defaultMaxOutputTail  = 16 << 10
	minMaxOutputTail      = 64
	maxMaxOutputTail      = 1 << 20
	defaultWaitDelay      = 2 * time.Second
	defaultMaxPreviewSize = 256 << 20
	maxPreviewFiles       = 20000

	// detectWindow is how much of the output is always retained internally so
	// the npm-ci lockfile marker can be found even when the caller asked for a
	// tiny tail.
	detectWindow = 64 << 10

	maxPreviewBaseLen = 256
	maxBuildLogRead   = 1 << 20

	previewDirName = "preview"
	distDirName    = "dist"

	// npmCIOutOfSync is printed by `npm ci` when package.json and
	// package-lock.json disagree. It is the trigger for the npm install
	// fallback. The leading "npm ci" is left out on purpose: npm 10 prints it
	// wrapped in backticks, older versions bare, so only the stable tail matches
	// both.
	npmCIOutOfSync = "can only install packages when your package.json and package-lock.json"
)

// Errors specific to build execution. Like the workspace errors they are
// wrapped in [*OpError] and safe to show to the model.
var (
	// ErrUnknownStep is returned for a step outside install/typecheck/lint/build.
	ErrUnknownStep = errors.New("unknown build step")
	// ErrInvalidPreviewBase is returned when preview_base is not a safe
	// absolute URL path prefix.
	ErrInvalidPreviewBase = errors.New("invalid preview_base")
	// ErrExecStart is returned when the build process cannot be started
	// (missing executable, bad working directory). The cause may contain host
	// paths: see [OpError.Cause].
	ErrExecStart = errors.New("cannot start build process")

	// errPreviewOutput marks a successful build whose dist/ cannot become the
	// preview (missing, a symlink, too large). It is the agent's problem, so it
	// is reported as a failed build rather than as an infrastructure error.
	errPreviewOutput = errors.New("invalid build output")
)

// previewBasePattern accepts "/" or a sequence of non-empty segments of URL
// unreserved characters, each followed by "/". It excludes "//" (a
// scheme-relative URL), whitespace, quotes and anything a shell or a flag
// parser could interpret. "." and ".." segments are rejected separately.
var previewBasePattern = regexp.MustCompile(`^/([A-Za-z0-9._~-]+/)*$`)

// BuildStep identifies one of the fixed build commands.
type BuildStep string

// ParseBuildStep validates s as a build step name.
func ParseBuildStep(s string) (BuildStep, error) {
	step := BuildStep(s)
	if err := step.validate(); err != nil {
		return "", err
	}
	return step, nil
}

func (s BuildStep) validate() error {
	switch s {
	case StepInstall, StepTypecheck, StepLint, StepBuild:
		return nil
	}
	return &OpError{Err: ErrUnknownStep, Msg: fmt.Sprintf("unknown build step %q", truncateUTF8(string(s), 64))}
}

// ValidatePreviewBase checks the vite --base value. It must start and end with
// "/", contain only URL-unreserved characters, no empty and no "."/".."
// segments, and stay under 256 bytes.
func ValidatePreviewBase(base string) error {
	bad := func(reason string) error {
		return &OpError{Err: ErrInvalidPreviewBase, Msg: "invalid preview_base: " + reason}
	}
	if len(base) > maxPreviewBaseLen {
		return bad("too long")
	}
	if !previewBasePattern.MatchString(base) {
		return bad("must start and end with '/' and contain only letters, digits, '.', '_', '~' and '-'")
	}
	for _, seg := range strings.Split(base, "/") {
		if seg == "." || seg == ".." {
			return bad("dot segments are not allowed")
		}
	}
	return nil
}

// ExecTimeouts bounds each step. Zero or negative fields use the defaults
// (install 5m, typecheck 2m, lint 2m, build 3m).
type ExecTimeouts struct {
	// Install bounds StepInstall, including the npm install fallback.
	Install time.Duration
	// Typecheck bounds StepTypecheck.
	Typecheck time.Duration
	// Lint bounds StepLint.
	Lint time.Duration
	// Build bounds StepBuild.
	Build time.Duration
}

func (t ExecTimeouts) forStep(s BuildStep) time.Duration {
	switch s {
	case StepInstall:
		return t.Install
	case StepTypecheck:
		return t.Typecheck
	case StepLint:
		return t.Lint
	default:
		return t.Build
	}
}

func (t ExecTimeouts) withDefaults() ExecTimeouts {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&t.Install, 5*time.Minute)
	def(&t.Typecheck, 2*time.Minute)
	def(&t.Lint, 2*time.Minute)
	def(&t.Build, 3*time.Minute)
	return t
}

// ExecConfig configures an [Executor]. The zero value is usable but runs with
// the default PATH and HOME and no registry/cache override.
type ExecConfig struct {
	// NpmPath and NpxPath are absolute paths of the executables. When empty
	// they are looked up, at run time, in the directories of Path (the same
	// PATH the child gets), so what is tested is what runs.
	NpmPath string
	NpxPath string
	// Path is the child's PATH. Default "/usr/local/bin:/usr/bin:/bin". It
	// must let npm/npx find node (they are #!/usr/bin/env node scripts).
	Path string
	// Home is the child's HOME (npm reads ~/.npmrc from it). Default /tmp,
	// which is writable in the builder image and holds no npmrc.
	Home string
	// NpmRegistry is exported as npm_config_registry (the registry proxy).
	NpmRegistry string
	// NpmCache is exported as npm_config_cache (shared /work/.npm-cache).
	NpmCache string
	// WorkDir, when set, is the directory every session root must live under.
	// Symlinks are resolved on both sides before comparing.
	WorkDir string
	// Timeouts bounds each step.
	Timeouts ExecTimeouts
	// MaxOutputTail is how many trailing output bytes are kept (default 16 KiB,
	// clamped to 64 B..1 MiB).
	MaxOutputTail int
	// WaitDelay is how long to wait for the output pipes to close after the
	// process is gone before giving up on them (default 2s). It matters when a
	// grandchild keeps a pipe open.
	WaitDelay time.Duration
	// MaxPreviewBytes caps the size of the dist/ copy kept as the preview
	// (default 256 MiB).
	MaxPreviewBytes int64
}

func (c ExecConfig) withDefaults() ExecConfig {
	if c.Path == "" {
		c.Path = defaultExecPath
	}
	if c.Home == "" {
		c.Home = defaultExecHome
	}
	c.Timeouts = c.Timeouts.withDefaults()
	switch {
	case c.MaxOutputTail <= 0:
		c.MaxOutputTail = defaultMaxOutputTail
	case c.MaxOutputTail < minMaxOutputTail:
		c.MaxOutputTail = minMaxOutputTail
	case c.MaxOutputTail > maxMaxOutputTail:
		c.MaxOutputTail = maxMaxOutputTail
	}
	if c.WaitDelay <= 0 {
		c.WaitDelay = defaultWaitDelay
	}
	if c.MaxPreviewBytes <= 0 {
		c.MaxPreviewBytes = defaultMaxPreviewSize
	}
	return c
}

// BuildResult is what the model sees after a build step. A non-zero ExitCode
// is a normal result, not a Go error.
type BuildResult struct {
	// ExitCode is the process exit status; 128+N when killed by signal N; -1
	// when the step timed out. A build that exits 0 but leaves no usable dist/
	// is reported as 1 with an explanation in OutputTail.
	ExitCode int `json:"exit_code"`
	// OutputTail is the last bytes of combined stdout+stderr, valid UTF-8
	// without NULs, prefixed with a marker when truncated.
	OutputTail string `json:"output_tail"`
	// DurationMs is the wall time of the command(s) in milliseconds.
	DurationMs int64 `json:"duration_ms"`
	// TimedOut is true when the step hit its timeout and was killed.
	TimedOut bool `json:"timed_out,omitempty"`
	// OutputBytes is the total number of output bytes the step produced, even
	// though only the tail is kept.
	OutputBytes int64 `json:"output_bytes"`
	// Replayed is true when the result came from the idempotency log and the
	// step was not executed again. It is never persisted.
	Replayed bool `json:"replayed,omitempty"`
}

// buildOpRecord is what RunStep stores in the idempotency log. The step is
// kept so reusing an op_id for another step is detected.
type buildOpRecord struct {
	Step BuildStep `json:"step"`
	BuildResult
}

// Executor runs build steps. Create one per process; it is safe for concurrent
// use. Steps on the same session root are serialized (two npm processes on one
// node_modules would corrupt it); different sessions run in parallel.
type Executor struct {
	cfg          ExecConfig
	sessionLocks keyedLocker // by session root: one build step at a time
	opLocks      keyedLocker // by root+op_id: duplicates wait, then replay
}

// NewExecutor validates cfg and applies defaults.
func NewExecutor(cfg ExecConfig) (*Executor, error) {
	for name, p := range map[string]string{"NpmPath": cfg.NpmPath, "NpxPath": cfg.NpxPath, "WorkDir": cfg.WorkDir} {
		if p != "" && !filepath.IsAbs(p) {
			return nil, fmt.Errorf("%s must be an absolute path: %w", name, ErrInvalidArgument)
		}
	}
	return &Executor{cfg: cfg.withDefaults()}, nil
}

// Run executes one build step in root (an absolute path of a session
// directory) and returns its result. Non-zero exits and timeouts are results;
// an error means the step could not run (bad step, bad preview_base, bad root,
// cannot start the process) or the caller's ctx was cancelled.
//
// After a successful build the dist/ output is copied to the preview
// directory and every step's output is persisted as the step's build log. If
// that bookkeeping fails Run returns the valid result together with an error
// wrapping ErrIO, the way [Workspace.Apply] does for its log.
//
// previewBase is only used (and only validated) for StepBuild.
func (e *Executor) Run(ctx context.Context, root string, step BuildStep, previewBase string) (BuildResult, error) {
	if err := step.validate(); err != nil {
		return BuildResult{}, err
	}
	if step == StepBuild {
		if err := ValidatePreviewBase(previewBase); err != nil {
			return BuildResult{}, err
		}
	}
	if err := e.checkRoot(root); err != nil {
		return BuildResult{}, err
	}
	unlock, err := e.sessionLocks.acquire(ctx, filepath.Clean(root))
	if err != nil {
		return BuildResult{}, err
	}
	defer unlock()

	res, err := e.execute(ctx, root, step, previewBase)
	if err != nil {
		return BuildResult{}, err
	}

	var stateErr error
	if step == StepBuild && res.ExitCode == 0 {
		switch perr := publishPreview(root, e.cfg.MaxPreviewBytes); {
		case perr == nil:
		case errors.Is(perr, errPreviewOutput):
			res.ExitCode = 1
			res.OutputTail += "\n[build exited 0 but " + perr.Error() + "; the preview was not updated]\n"
		default:
			stateErr = perr
		}
	}
	if lerr := writeBuildLog(root, step, res.OutputTail); lerr != nil && stateErr == nil {
		stateErr = lerr
	}
	return res, stateErr
}

// RunStep is Run with idempotency on w's op log: a repeated opID returns the
// stored result (Replayed set) without executing again, and reusing an opID for
// a different step is rejected with ErrOpIDReused. Only completed steps are
// recorded (cancelled or failed-to-start ones can be retried). Concurrent
// calls with the same opID are serialized here, as the Workspace.LookupOp
// contract requires: the second waits for the first and then replays it.
//
// If the step ran but recording failed, RunStep returns the result together
// with the error.
func (e *Executor) RunStep(ctx context.Context, w *Workspace, opID string, step BuildStep, previewBase string) (BuildResult, error) {
	if w == nil {
		return BuildResult{}, fmt.Errorf("workspace is required: %w", ErrInvalidArgument)
	}
	if err := validOpID(opID); err != nil {
		return BuildResult{}, err
	}
	if err := step.validate(); err != nil {
		return BuildResult{}, err
	}
	unlock, err := e.opLocks.acquire(ctx, w.root+"\x00"+opID)
	if err != nil {
		return BuildResult{}, err
	}
	defer unlock()

	if rec, ok := w.LookupOp(opID); ok {
		var stored buildOpRecord
		if rec.Kind != OpKindBuild {
			return BuildResult{}, opErr(ErrOpIDReused, "")
		}
		if err := json.Unmarshal(rec.Result, &stored); err != nil {
			return BuildResult{}, &OpError{Err: ErrOpLog, cause: err}
		}
		if stored.Step != step {
			return BuildResult{}, opErr(ErrOpIDReused, "")
		}
		stored.BuildResult.Replayed = true
		return stored.BuildResult, nil
	}

	res, err := e.Run(ctx, w.root, step, previewBase)
	if err != nil {
		return res, err
	}
	res.Replayed = false
	if err := w.RecordOp(opID, OpKindBuild, buildOpRecord{Step: step, BuildResult: res}); err != nil {
		return res, err
	}
	return res, nil
}

// LastBuildDir returns <root>/dist when it exists as a real directory, else "".
// It reflects the most recent build attempt, which may have failed halfway;
// the preview server must use [PreviewDir] instead.
func (e *Executor) LastBuildDir(root string) string {
	d := filepath.Join(root, distDirName)
	if li, err := os.Lstat(d); err == nil && li.IsDir() {
		return d
	}
	return ""
}

// PreviewDir returns where the copy of the last SUCCESSFUL build lives
// (<root>/.orbit/preview). It is replaced atomically after each successful
// build, so it keeps serving the previous good build while a later build runs
// or fails (vite empties dist/ at the start of every build). The path may not
// exist yet if no build has succeeded. Callers serving it must not follow
// symlinks: the directory is written by the builder, but a lint script or vite
// config runs with the session root as its working directory.
func PreviewDir(root string) string {
	return filepath.Join(root, orbitDirName, previewDirName)
}

// ReadBuildLog returns the last tail bytes of the most recent output of step,
// persisted at <root>/.orbit/build-<step>.log. tail <= 0 means 16 KiB; it is
// capped at 1 MiB. Returns ErrNotFound when the step never ran. The file is
// opened with O_NOFOLLOW and must be a regular file: build tools run in the
// session root and could plant a symlink there to make the builder read (and
// show to the model) a file such as /proc/self/environ.
func ReadBuildLog(w *Workspace, step BuildStep, tail int) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace is required: %w", ErrInvalidArgument)
	}
	if err := step.validate(); err != nil {
		return "", err
	}
	if tail <= 0 {
		tail = defaultMaxOutputTail
	}
	if tail > maxBuildLogRead {
		tail = maxBuildLogRead
	}
	notFound := &OpError{Err: ErrNotFound, Msg: fmt.Sprintf("no build log for step %s", step)}

	if err := ensureRealDirNoCreate(w.orbitDir()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", notFound
		}
		return "", err
	}
	f, err := os.OpenFile(buildLogPath(w.root, step), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", notFound
		}
		return "", &OpError{Err: ErrIO, cause: err}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", &OpError{Err: ErrIO, cause: err}
	}
	if !fi.Mode().IsRegular() {
		return "", opErr(ErrNotRegular, "")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBuildLogRead))
	if err != nil {
		return "", &OpError{Err: ErrIO, cause: err}
	}
	s := string(data)
	if len(s) <= tail {
		return s, nil
	}
	cut := s[len(s)-tail:]
	for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
		cut = cut[1:]
	}
	return truncationMarker(len(cut)) + cut, nil
}

func ensureRealDirNoCreate(dir string) error {
	li, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return &OpError{Err: ErrIO, cause: fmt.Errorf("%s is not a plain directory", filepath.Base(dir))}
	}
	return nil
}

func buildLogPath(root string, step BuildStep) string {
	// step is a validated enum, so it cannot carry a separator.
	return filepath.Join(root, orbitDirName, "build-"+string(step)+".log")
}

// checkRoot requires an absolute, real (non-symlink) directory and, when a
// WorkDir is configured, that it lives under it.
func (e *Executor) checkRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return &OpError{Err: ErrInvalidArgument, Msg: "session root must be an absolute path"}
	}
	li, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrSessionNotFound
		}
		return &OpError{Err: ErrIO, cause: err}
	}
	if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return opErr(ErrNotDirectory, "")
	}
	if e.cfg.WorkDir != "" {
		realRoot, err1 := filepath.EvalSymlinks(root)
		realWork, err2 := filepath.EvalSymlinks(e.cfg.WorkDir)
		if err1 != nil || err2 != nil {
			return &OpError{Err: ErrIO, cause: errors.Join(err1, err2)}
		}
		if realRoot == realWork || !within(realWork, realRoot) {
			return opErr(ErrPathOutside, "")
		}
	}
	return nil
}

// childEnv builds the child environment from nothing. Adding a variable here
// is a security decision: everything the builder process itself holds stays out.
// NODE_ENV is deliberately unset: vite needs devDependencies at build time.
func (e *Executor) childEnv() []string {
	env := []string{
		"PATH=" + e.cfg.Path,
		"HOME=" + e.cfg.Home,
		"npm_config_replace-registry-host=always",
		"npm_config_update_notifier=false",
		"npm_config_fund=false",
		"npm_config_audit=false",
		"CI=true",
	}
	if e.cfg.NpmRegistry != "" {
		env = append(env, "npm_config_registry="+e.cfg.NpmRegistry)
	}
	if e.cfg.NpmCache != "" {
		env = append(env, "npm_config_cache="+e.cfg.NpmCache)
	}
	return env
}

// resolveBinary returns the absolute path of the configured executable, or
// searches name in the child's PATH. Looking in the child's PATH rather than the
// parent's keeps "what we run" equal to "what the child's own PATH resolves".
func (e *Executor) resolveBinary(configured, name string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	for _, dir := range filepath.SplitList(e.cfg.Path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", &OpError{Err: ErrExecStart, Msg: fmt.Sprintf("%s not found in the configured PATH", name), cause: exec.ErrNotFound}
}

// Fixed argv per step. Nothing here is derived from model input except
// previewBase, which ValidatePreviewBase has already restricted.
var (
	argvNpmCI      = []string{"ci", "--ignore-scripts", "--no-audit", "--no-fund", "--prefer-offline"}
	argvNpmInstall = []string{"install", "--ignore-scripts", "--no-audit", "--no-fund"}
	argvLint       = []string{"run", "lint", "--if-present"}
	argvTypecheck  = []string{"--no-install", "tsc", "-b"}
)

func argvBuild(previewBase string) []string {
	return []string{"--no-install", "vite", "build", "--base", previewBase}
}

func (e *Executor) execute(ctx context.Context, root string, step BuildStep, previewBase string) (BuildResult, error) {
	var (
		bin  string
		args []string
		err  error
	)
	switch step {
	case StepInstall:
		bin, err = e.resolveBinary(e.cfg.NpmPath, "npm")
	case StepLint:
		bin, err = e.resolveBinary(e.cfg.NpmPath, "npm")
		args = argvLint
	case StepTypecheck:
		bin, err = e.resolveBinary(e.cfg.NpxPath, "npx")
		args = argvTypecheck
	case StepBuild:
		bin, err = e.resolveBinary(e.cfg.NpxPath, "npx")
		args = argvBuild(previewBase)
	}
	if err != nil {
		return BuildResult{}, err
	}

	env := e.childEnv()
	tb := newTailBuffer(max(e.cfg.MaxOutputTail, detectWindow))
	timeout := e.cfg.Timeouts.forStep(step)
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	var code int
	if step == StepInstall {
		code, err = e.runInstall(stepCtx, root, bin, env, tb)
	} else {
		code, err = e.runOnce(stepCtx, root, bin, args, env, tb)
	}
	if err != nil {
		return BuildResult{}, err
	}

	res := BuildResult{ExitCode: code}
	if code != 0 && stepCtx.Err() != nil {
		if ctx.Err() != nil {
			// The caller gave up; this is not a build outcome.
			return BuildResult{}, ctx.Err()
		}
		res.TimedOut = true
		res.ExitCode = -1
		fmt.Fprintf(tb, "\n[step %s timed out after %s and was killed]\n", step, timeout)
	}
	res.DurationMs = time.Since(start).Milliseconds()
	res.OutputBytes = tb.total
	res.OutputTail = tb.render(e.cfg.MaxOutputTail)
	return res, nil
}

// runInstall implements the install policy:
//
//   - no package-lock.json: `npm install` (npm ci would refuse to run);
//   - otherwise `npm ci`; if it fails because the lockfile is out of sync with
//     package.json (the agent edited dependencies), retry once with
//     `npm install`, which also repairs the lockfile. Any other npm ci failure
//     is returned as is.
//
// Both attempts share one deadline (the install timeout) and one output buffer.
func (e *Executor) runInstall(ctx context.Context, root, npm string, env []string, tb *tailBuffer) (int, error) {
	if _, err := os.Lstat(filepath.Join(root, "package-lock.json")); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return 0, &OpError{Err: ErrIO, cause: err}
		}
		return e.runOnce(ctx, root, npm, argvNpmInstall, env, tb)
	}
	code, err := e.runOnce(ctx, root, npm, argvNpmCI, env, tb)
	if err != nil || code == 0 || ctx.Err() != nil {
		return code, err
	}
	if !bytes.Contains(tb.bytes(), []byte(npmCIOutOfSync)) {
		return code, nil
	}
	fmt.Fprint(tb, "\n[package-lock.json is out of sync with package.json, retrying with npm install]\n")
	return e.runOnce(ctx, root, npm, argvNpmInstall, env, tb)
}

// runOnce starts bin with args (no shell) in root and waits for it. The child
// leads its own process group so the whole tree can be killed; ctx expiry kills
// the group, not just the leader. It returns the exit code; a non-zero exit is
// not an error. Output goes to tb.
func (e *Executor) runOnce(ctx context.Context, root, bin string, args, env []string, tb *tailBuffer) (int, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	cmd.Env = env // explicit and non-nil: never falls back to the parent's env
	cmd.Stdin = nil
	cmd.Stdout = tb
	cmd.Stderr = tb
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = e.cfg.WaitDelay
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	if err := cmd.Start(); err != nil {
		return 0, &OpError{Err: ErrExecStart, Msg: "cannot start build process", cause: err}
	}
	pid := cmd.Process.Pid
	waitErr := cmd.Wait()
	// Reap stragglers (dev servers, watchers, background jobs) that outlived
	// the leader. While any member is alive the pgid cannot be recycled, and if
	// none is, kill returns ESRCH, so this cannot hit an unrelated group in
	// practice.
	_ = syscall.Kill(-pid, syscall.SIGKILL)

	switch {
	case waitErr == nil, errors.Is(waitErr, exec.ErrWaitDelay):
		// ErrWaitDelay: the process succeeded but a grandchild held the pipes.
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return ee.ExitCode(), nil
	}
	// Context error from Wait after the group was killed: report as a failed
	// run; the caller classifies it as timeout or cancellation via ctx.
	if ctx.Err() != nil {
		return -1, nil
	}
	return 0, &OpError{Err: ErrExecStart, Msg: "build process failed", cause: waitErr}
}

// tailBuffer is an io.Writer that keeps only the last keep bytes written and
// counts everything it saw, so memory stays bounded however much a step prints.
type tailBuffer struct {
	keep  int
	buf   []byte
	total int64
}

func newTailBuffer(keep int) *tailBuffer { return &tailBuffer{keep: keep} }

// Write implements io.Writer. It never fails.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.total += int64(len(p))
	if len(p) >= t.keep {
		t.buf = append(t.buf[:0], p[len(p)-t.keep:]...)
		return len(p), nil
	}
	t.buf = append(t.buf, p...)
	// Compact lazily so appends stay amortized O(1).
	if len(t.buf) > 2*t.keep {
		n := copy(t.buf, t.buf[len(t.buf)-t.keep:])
		t.buf = t.buf[:n]
	}
	return len(p), nil
}

// bytes returns the retained tail (at most keep bytes).
func (t *tailBuffer) bytes() []byte {
	if len(t.buf) > t.keep {
		return t.buf[len(t.buf)-t.keep:]
	}
	return t.buf
}

func truncationMarker(n int) string {
	return fmt.Sprintf("…[output truncated, showing last %d bytes]…\n", n)
}

// render returns the last limit bytes as safe text: NUL bytes are dropped,
// invalid UTF-8 (including a rune cut in half at the start) is replaced, and a
// marker is prepended when anything was cut.
func (t *tailBuffer) render(limit int) string {
	b := t.bytes()
	if len(b) > limit {
		b = b[len(b)-limit:]
	}
	truncated := t.total > int64(len(b))
	for truncated && len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	s := strings.ToValidUTF8(string(bytes.ReplaceAll(b, []byte{0}, nil)), "�")
	if truncated {
		return truncationMarker(len(s)) + s
	}
	return s
}

// writeBuildLog persists the step's output atomically (temp file + rename), so
// a reader never sees a half-written log and a planted symlink at the final
// name is replaced, not followed.
func writeBuildLog(root string, step BuildStep, content string) error {
	dir := filepath.Join(root, orbitDirName)
	if err := ensureRealDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "build-"+string(step)+".log.tmp-*")
	if err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return &OpError{Err: ErrIO, cause: err}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return &OpError{Err: ErrIO, cause: err}
	}
	if err := tmp.Close(); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	if err := os.Rename(name, buildLogPath(root, step)); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	ok = true
	return nil
}

// publishPreview copies <root>/dist to <root>/.orbit/preview. The copy is built
// in a temp directory next to the destination and swapped in with renames, so
// the preview is either the previous good build or the new one. The swap is two
// renames (old out, new in): there is a window of a few microseconds where
// PreviewDir does not exist, and a crash inside it is repaired on the next
// call, which puts the old directory back before swapping. Callers must hold
// the session lock (Run does).
func publishPreview(root string, maxBytes int64) error {
	src := filepath.Join(root, distDirName)
	li, err := os.Lstat(src)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("dist/ was not produced: %w", errPreviewOutput)
	case err != nil:
		return &OpError{Err: ErrIO, cause: err}
	case li.Mode()&fs.ModeSymlink != 0 || !li.IsDir():
		return fmt.Errorf("dist is not a plain directory: %w", errPreviewOutput)
	}

	orbit := filepath.Join(root, orbitDirName)
	if err := ensureRealDir(orbit); err != nil {
		return err
	}
	final := PreviewDir(root)
	old := final + ".old"

	// Repair a swap that crashed between its two renames.
	if _, err := os.Lstat(final); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Lstat(old); err == nil {
			_ = os.Rename(old, final)
		}
	}

	tmp, err := os.MkdirTemp(orbit, "preview.tmp-")
	if err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	swapped := false
	defer func() {
		if !swapped {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := copyTree(src, tmp, maxBytes); err != nil {
		return err
	}

	if err := os.RemoveAll(old); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	hadOld := false
	if _, err := os.Lstat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return &OpError{Err: ErrIO, cause: err}
		}
		hadOld = true
	}
	if err := os.Rename(tmp, final); err != nil {
		if hadOld {
			_ = os.Rename(old, final) // roll back to the previous good build
		}
		return &OpError{Err: ErrIO, cause: err}
	}
	swapped = true
	_ = os.RemoveAll(old)
	return nil
}

// copyTree copies regular files and directories from src to dst. Symlinks and
// special files are skipped: dist/ is produced by code the agent controls, so
// following a link would copy host files into a directory the preview server
// exposes. Files are opened with O_NOFOLLOW as a second line of defence against
// a swap after the walk saw them. Size and file count are capped.
func copyTree(src, dst string, maxBytes int64) error {
	var (
		bytesLeft = maxBytes
		files     int
	)
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &OpError{Err: ErrIO, cause: err}
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return &OpError{Err: ErrIO, cause: err}
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			if rel == "." {
				return nil
			}
			if err := os.Mkdir(target, 0o700); err != nil {
				return &OpError{Err: ErrIO, cause: err}
			}
			return nil
		case d.Type().IsRegular():
			files++
			if files > maxPreviewFiles {
				return fmt.Errorf("dist/ has more than %d files: %w", maxPreviewFiles, errPreviewOutput)
			}
			n, err := copyRegular(p, target, bytesLeft)
			if err != nil {
				return err
			}
			bytesLeft -= n
			return nil
		default:
			return nil // symlink, socket, device...: not part of the preview
		}
	})
}

func copyRegular(src, dst string, limit int64) (int64, error) {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, &OpError{Err: ErrIO, cause: err}
	}
	defer in.Close()
	if fi, err := in.Stat(); err != nil || !fi.Mode().IsRegular() {
		return 0, nil
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, &OpError{Err: ErrIO, cause: err}
	}
	// Copy one byte past the limit so "exactly at the limit" is accepted and
	// "over" is detected without reading an unbounded file.
	n, err := io.Copy(out, io.LimitReader(in, limit+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, &OpError{Err: ErrIO, cause: err}
	}
	if n > limit {
		return n, fmt.Errorf("dist/ exceeds the preview size limit: %w", errPreviewOutput)
	}
	return n, nil
}

// keyedLocker is a map of context-aware mutexes created on demand and dropped
// when unused, so the map does not grow with the number of sessions or op ids.
// The zero value is ready to use.
type keyedLocker struct {
	mu sync.Mutex
	m  map[string]*keyedEntry
}

type keyedEntry struct {
	ch   chan struct{} // capacity 1: holding a slot means holding the lock
	refs int
}

// acquire blocks until key is free or ctx is done. The returned func releases it.
func (k *keyedLocker) acquire(ctx context.Context, key string) (func(), error) {
	k.mu.Lock()
	if k.m == nil {
		k.m = make(map[string]*keyedEntry)
	}
	en := k.m[key]
	if en == nil {
		en = &keyedEntry{ch: make(chan struct{}, 1)}
		k.m[key] = en
	}
	en.refs++
	k.mu.Unlock()

	drop := func() {
		k.mu.Lock()
		en.refs--
		if en.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
	select {
	case en.ch <- struct{}{}:
		return func() { <-en.ch; drop() }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

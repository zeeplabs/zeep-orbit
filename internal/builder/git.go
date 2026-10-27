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
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

// Session snapshots are git repositories. Every turn ends with a commit and an
// incremental bundle that Orbit stores; if the builder volume is lost, the
// ordered bundles rebuild the repository byte for byte (same SHAs).
//
// The repository content is produced by an LLM that may have been
// prompt-injected, and the template tar may come from a company, so git is run
// defensively:
//
//   - the real git binary, fixed argv, no shell; the only caller-controlled
//     argv values are 40-hex SHAs (validated), "HEAD", a numeric turn and a
//     sanitized commit message passed as the value of -m;
//   - a from-scratch environment (no inherited variables) and a fixed -c list
//     on every call, so system/global/user config, hooks, GPG signing and
//     fsmonitor can never run, whatever sits in .git/config or the tree;
//   - commits are made with plumbing (write-tree, commit-tree, update-ref) so
//     no porcelain hook point is ever reached;
//   - the repository is addressed with GIT_DIR/GIT_WORK_TREE, so git never
//     discovers a repository in a parent directory;
//   - builder-private and build-output directories (.orbit, node_modules,
//     dist) are kept out of commits twice: .git/info/exclude (the agent cannot
//     edit .git) plus explicit pathspec excludes, and the index is validated
//     (no symlinks, no gitlinks, no protected segments) before every commit;
//   - tar extraction rejects traversal, absolute paths, links, devices and
//     protected names, and caps entries, bytes and per-file size;
//   - errors only carry fixed git subcommand names; stderr (which may contain
//     host paths) is available through GitError.Cause for server logs only.
//
// Operations on one session root are serialized by [GitRepo]. The lock is
// in-process: two builder processes must never share a work directory.

// Typed git errors. Callers match them with errors.Is.
var (
	// ErrGitFailed means a git command exited unexpectedly. The cause (with
	// stderr) is on [GitError.Cause] for server logs.
	ErrGitFailed = errors.New("git command failed")
	// ErrGitTimeout means a git command exceeded its time limit.
	ErrGitTimeout = errors.New("git command timed out")
	// ErrGitUnavailable means the git binary could not be found.
	ErrGitUnavailable = errors.New("git binary not available")
	// ErrNoRepo means the session root has no git repository or no commits.
	ErrNoRepo = errors.New("session has no git repository")
	// ErrRepoExists means the session root already contains a git repository.
	ErrRepoExists = errors.New("session already has a git repository")
	// ErrRootNotEmpty means the session root has files where an empty root
	// is required (init and restore).
	ErrRootNotEmpty = errors.New("session root is not empty")
	// ErrInvalidSHA means a commit id is not a full lowercase 40-hex SHA
	// (or HEAD where that is allowed).
	ErrInvalidSHA = errors.New("invalid commit sha")
	// ErrUnknownCommit means a well-formed SHA does not name a commit.
	ErrUnknownCommit = errors.New("unknown commit")
	// ErrNotAncestor means the target commit is not an ancestor of HEAD.
	ErrNotAncestor = errors.New("commit is not an ancestor of HEAD")
	// ErrInvalidArchive means the template tar stream is malformed.
	ErrInvalidArchive = errors.New("invalid tar stream")
	// ErrUnsafeArchiveEntry means a tar entry was rejected: traversal,
	// absolute path, link, device, protected name or conflicting path.
	ErrUnsafeArchiveEntry = errors.New("unsafe tar entry")
	// ErrArchiveTooLarge means a tar (extracted or produced) exceeds its
	// size, file-count or per-file limit.
	ErrArchiveTooLarge = errors.New("tar exceeds the size limit")
	// ErrBundleTooLarge means a git bundle exceeds the configured limit.
	ErrBundleTooLarge = errors.New("bundle exceeds the size limit")
	// ErrBundleVerify means a bundle is corrupt, misses prerequisites, fails
	// object validation, or does not yield the expected commit.
	ErrBundleVerify = errors.New("bundle verification failed")
	// ErrUnsafeContent means the work tree holds something that must never be
	// committed (symlink, nested repository, protected directory).
	ErrUnsafeContent = errors.New("work tree contains content that cannot be committed")
)

// GitError is the error type of every git operation. It wraps one of the
// sentinels above (errors.Is works) and never exposes git's stderr or host
// paths through Error.
type GitError struct {
	// Err is the sentinel describing the failure.
	Err error
	// Op is the fixed git subcommand or step that failed (for example
	// "bundle create"). It never contains caller input.
	Op string

	cause error
}

// Error implements error. It never includes host paths.
func (e *GitError) Error() string {
	if e.Op == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s (%s)", e.Err.Error(), e.Op)
}

// Unwrap returns the sentinel so errors.Is(err, ErrBundleVerify) etc. work.
func (e *GitError) Unwrap() error { return e.Err }

// Cause returns the underlying error, which may hold git's stderr and host
// paths. Log it server-side; never send it to a client or the model.
func (e *GitError) Cause() error { return e.cause }

func gitErr(sentinel error, op string, cause error) error {
	return &GitError{Err: sentinel, Op: op, cause: cause}
}

// TarLimits bounds what Init unpacks from a template tar. Zero or negative
// fields fall back to the defaults from [DefaultGitConfig].
type TarLimits struct {
	// MaxTotalBytes caps the sum of all file sizes.
	MaxTotalBytes int64
	// MaxFileBytes caps one file.
	MaxFileBytes int64
	// MaxFiles caps the number of entries (files and directories).
	MaxFiles int
}

// GitConfig configures a [GitRepo]. Zero or negative fields fall back to the
// defaults from [DefaultGitConfig].
type GitConfig struct {
	// GitBin is the git executable (name looked up on PATH, or a path).
	// Injectable so tests and unusual images can point at a specific binary.
	GitBin string
	// HomeDir is the HOME (and XDG_CONFIG_HOME) given to git. It should be an
	// empty private directory: git reads no user config from it, but it needs
	// a writable HOME-like location for some commands. Empty means a private
	// directory under os.TempDir().
	HomeDir string
	// Timeout bounds every single git invocation.
	Timeout time.Duration
	// MaxOutputBytes caps stdout of ordinary git commands.
	MaxOutputBytes int64
	// MaxBundleBytes caps one bundle, produced or restored.
	MaxBundleBytes int64
	// MaxArchiveBytes caps the tar produced by Archive.
	MaxArchiveBytes int64
	// MaxRestoreBytes caps the total size of all bundles passed to Restore.
	MaxRestoreBytes int64
	// Tar bounds template extraction in Init.
	Tar TarLimits
}

// DefaultGitConfig returns the production defaults.
func DefaultGitConfig() GitConfig {
	return GitConfig{
		GitBin:          "git",
		Timeout:         30 * time.Second,
		MaxOutputBytes:  1 << 20,
		MaxBundleBytes:  32 << 20,
		MaxArchiveBytes: 64 << 20,
		MaxRestoreBytes: 256 << 20,
		Tar: TarLimits{
			MaxTotalBytes: 64 << 20,
			MaxFileBytes:  16 << 20,
			MaxFiles:      10000,
		},
	}
}

func (c GitConfig) withDefaults() GitConfig {
	d := DefaultGitConfig()
	if c.GitBin == "" {
		c.GitBin = d.GitBin
	}
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.MaxOutputBytes <= 0 {
		c.MaxOutputBytes = d.MaxOutputBytes
	}
	if c.MaxBundleBytes <= 0 {
		c.MaxBundleBytes = d.MaxBundleBytes
	}
	if c.MaxArchiveBytes <= 0 {
		c.MaxArchiveBytes = d.MaxArchiveBytes
	}
	if c.MaxRestoreBytes <= 0 {
		c.MaxRestoreBytes = d.MaxRestoreBytes
	}
	if c.Tar.MaxTotalBytes <= 0 {
		c.Tar.MaxTotalBytes = d.Tar.MaxTotalBytes
	}
	if c.Tar.MaxFileBytes <= 0 {
		c.Tar.MaxFileBytes = d.Tar.MaxFileBytes
	}
	if c.Tar.MaxFiles <= 0 {
		c.Tar.MaxFiles = d.Tar.MaxFiles
	}
	return c
}

// Bundle is one stored snapshot: the git bundle of a turn.
type Bundle struct {
	// Seq is the turn sequence the bundle belongs to (informational; the
	// caller passes bundles to Restore already ordered).
	Seq int
	// SHA is the commit the bundle must yield. Optional but strongly
	// recommended: Restore verifies it, which catches a wrong or reordered
	// snapshot.
	SHA string
	// Data is the bundle file content.
	Data []byte
}

// CommitResult is the outcome of Init, Commit and Revert.
type CommitResult struct {
	// SHA is the commit now at HEAD.
	SHA string
	// Prev is the previous HEAD ("" for Init).
	Prev string
	// Bundle is a full bundle (Init) or the incremental bundle Prev..SHA. It
	// is nil when Changed is false: a turn without changes produces no
	// commit and nothing to store.
	Bundle []byte
	// Changed reports whether a new commit was created.
	Changed bool
}

// LogEntry is one commit in [GitRepo.Log].
type LogEntry struct {
	// SHA is the full commit id.
	SHA string
	// Subject is the first line of the commit message.
	Subject string
}

const (
	gitDirName       = ".git"
	gitUserName      = "Orbit Builder"
	gitUserEmail     = "builder@orbit.invalid"
	gitMainBranch    = "main"
	zeroSHA          = "0000000000000000000000000000000000000000"
	maxCommitSubject = 200
	maxStderrBytes   = 4 << 10
	maxEntryPath     = 1024
	maxEntryDepth    = 40
	maxBundles       = 10000
	restoreRefPrefix = "refs/orbit/restore-"
)

// gitCommonConfig is passed as -c on every invocation. It pins identity and
// neutralizes every config-driven code execution or environment dependence:
// hooks, GPG, fsmonitor, auto gc, file transport, CRLF and symlink handling.
var gitCommonConfig = []string{
	"user.name=" + gitUserName,
	"user.email=" + gitUserEmail,
	"commit.gpgsign=false",
	"tag.gpgsign=false",
	"core.hooksPath=" + os.DevNull,
	"protocol.file.allow=never",
	"core.fsmonitor=false",
	"gc.auto=0",
	"core.autocrlf=false",
	"core.symlinks=false",
	"core.quotepath=false",
	"tar.umask=0022",
}

// stagePathspec lists what `git add -A` may stage. The excludes mirror the
// protected names of the workspace: .gitignore is agent-editable (a "!dist/"
// line would otherwise win over .git/info/exclude), pathspec excludes are not.
var stagePathspec = []string{
	"--", ".",
	":(exclude,glob)**/.orbit/**",
	":(exclude,glob)**/node_modules/**",
	":(exclude,glob)**/dist/**",
}

// excludeFileContent is written to .git/info/exclude. The agent cannot touch
// .git, so these entries hold even when it rewrites .gitignore.
const excludeFileContent = "# managed by the Orbit builder\nnode_modules/\ndist/\n.orbit/\n"

// requiredIgnores are appended to the template's .gitignore when missing.
var requiredIgnores = []string{"node_modules/", "dist/", ".orbit/"}

var fullSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isFullSHA(s string) bool { return fullSHAPattern.MatchString(s) }

// rootLock is a refcounted per-root mutex, so the map does not grow with the
// number of sessions ever seen.
type rootLock struct {
	mu   sync.Mutex
	refs int
}

// GitRepo runs git for session roots. It is safe for concurrent use; calls on
// the same root are serialized, calls on different roots run in parallel.
type GitRepo struct {
	cfg GitConfig
	bin string
	env []string

	mu    sync.Mutex // guards locks
	locks map[string]*rootLock
}

// NewGitRepo resolves the git binary and prepares the private HOME. It fails
// with ErrGitUnavailable when git is not installed.
func NewGitRepo(cfg GitConfig) (*GitRepo, error) {
	cfg = cfg.withDefaults()
	bin, err := exec.LookPath(cfg.GitBin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrGitUnavailable, filepath.Base(cfg.GitBin))
	}
	if abs, aerr := filepath.Abs(bin); aerr == nil {
		bin = abs
	}
	home := cfg.HomeDir
	if home == "" {
		home = filepath.Join(os.TempDir(), "orbit-builder-git-home")
	}
	if err := prepareHome(home); err != nil {
		return nil, err
	}
	// A fixed PATH plus the git directory: git needs to find its own helpers
	// but must not pick up whatever the host PATH holds.
	pathEnv := "/usr/local/bin:/usr/bin:/bin"
	if dir := filepath.Dir(bin); !strings.Contains(":"+pathEnv+":", ":"+dir+":") {
		pathEnv = dir + ":" + pathEnv
	}
	env := []string{
		"PATH=" + pathEnv,
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + home,
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	}
	return &GitRepo{cfg: cfg, bin: bin, env: env, locks: make(map[string]*rootLock)}, nil
}

// prepareHome makes sure the git HOME is a private real directory. Both
// "created by us" and "provided by the caller" must end up 0700 and not be a
// symlink, otherwise another local user could plant config in it.
func prepareHome(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	li, err := os.Lstat(dir)
	if err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return fmt.Errorf("git home is not a plain directory: %w", ErrInvalidArgument)
	}
	if li.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return &OpError{Err: ErrIO, cause: err}
		}
	}
	return nil
}

// resolveRoot returns the symlink-resolved absolute root. The root itself must
// be a real directory (as in [NewWorkspace]).
func resolveRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("session root is empty: %w", ErrInvalidArgument)
	}
	li, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", opErr(ErrSessionNotFound, "")
		}
		return "", &OpError{Err: ErrIO, cause: err}
	}
	if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return "", fmt.Errorf("session root is not a plain directory: %w", ErrInvalidArgument)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", &OpError{Err: ErrIO, cause: err}
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", &OpError{Err: ErrIO, cause: err}
	}
	return abs, nil
}

// lockRoot validates the root and takes its mutex. unlock must be called once.
func (g *GitRepo) lockRoot(root string) (string, func(), error) {
	resolved, err := resolveRoot(root)
	if err != nil {
		return "", nil, err
	}
	g.mu.Lock()
	l := g.locks[resolved]
	if l == nil {
		l = &rootLock{}
		g.locks[resolved] = l
	}
	l.refs++
	g.mu.Unlock()
	l.mu.Lock()
	return resolved, func() {
		l.mu.Unlock()
		g.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(g.locks, resolved)
		}
		g.mu.Unlock()
	}, nil
}

// requireRepo checks that root/.git is a real directory.
func requireRepo(root string) error {
	li, err := os.Lstat(filepath.Join(root, gitDirName))
	if err != nil || li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return &GitError{Err: ErrNoRepo}
	}
	return nil
}

// clearStaleLocks removes lock files a killed git (timeout, crash) may have
// left behind. Safe because the per-root mutex guarantees no git of this
// process is running on the repository.
func clearStaleLocks(root string) {
	for _, name := range []string{"index.lock", "HEAD.lock", "refs/heads/" + gitMainBranch + ".lock"} {
		_ = os.Remove(filepath.Join(root, gitDirName, filepath.FromSlash(name)))
	}
}

// ensureExclude (re)writes .git/info/exclude.
func ensureExclude(root string) error {
	dir := filepath.Join(root, gitDirName, "info")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	path := filepath.Join(dir, "exclude")
	if cur, err := os.ReadFile(path); err == nil && string(cur) == excludeFileContent {
		return nil
	}
	if err := os.WriteFile(path, []byte(excludeFileContent), 0o644); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	return nil
}

// ---- process execution ----

var (
	errOutputLimit = errors.New("output limit exceeded")
	errStreamLimit = errors.New("stream limit exceeded")
)

// gitCall describes one git invocation.
type gitCall struct {
	root string   // resolved session root; also the working directory
	op   string   // fixed label for errors
	args []string // argv after the global -c flags
	// extraCfg are additional -c entries; they come after the common ones, so
	// they win.
	extraCfg []string
	// noGitDir skips GIT_DIR/GIT_WORK_TREE (only for `git init`).
	noGitDir bool
	// out receives stdout; nil captures it into gitOut.stdout.
	out io.Writer
	// maxOut caps stdout; 0 means the configured MaxOutputBytes.
	maxOut int64
	// tooLarge is the sentinel returned when stdout exceeds maxOut.
	tooLarge error
	// failAs replaces ErrGitFailed for a non-accepted exit status.
	failAs error
	// okExit lists non-zero exit statuses that are not errors.
	okExit []int
}

type gitOut struct {
	stdout string
	exit   int
}

// limitWriter stops a runaway producer: once the cap is hit it fails every
// write, which closes the pipe and makes git die on SIGPIPE.
type limitWriter struct {
	w        io.Writer
	left     int64
	exceeded bool
	werr     error // error from the underlying writer, if any
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		l.exceeded = true
		return 0, errOutputLimit
	}
	n, err := l.w.Write(p)
	l.left -= int64(n)
	if err != nil {
		l.werr = err
	}
	return n, err
}

// headWriter keeps the first maxStderrBytes bytes and discards the rest.
type headWriter struct{ buf bytes.Buffer }

func (h *headWriter) Write(p []byte) (int, error) {
	if room := maxStderrBytes - h.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		h.buf.Write(p[:room])
	}
	return len(p), nil
}

func (g *GitRepo) run(ctx context.Context, c gitCall, stdin io.Reader) (gitOut, error) {
	ctx, cancel := context.WithTimeout(ctx, g.cfg.Timeout)
	defer cancel()

	argv := make([]string, 0, 2*(len(gitCommonConfig)+len(c.extraCfg))+len(c.args))
	for _, kv := range gitCommonConfig {
		argv = append(argv, "-c", kv)
	}
	for _, kv := range c.extraCfg {
		argv = append(argv, "-c", kv)
	}
	argv = append(argv, c.args...)

	cmd := exec.CommandContext(ctx, g.bin, argv...)
	cmd.Dir = c.root
	env := append([]string(nil), g.env...)
	if !c.noGitDir {
		env = append(env,
			"GIT_DIR="+filepath.Join(c.root, gitDirName),
			"GIT_WORK_TREE="+c.root,
		)
	}
	cmd.Env = env
	// Without WaitDelay a grandchild holding the pipes open would keep Wait
	// blocked after the context kills git.
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = stdin

	var capture bytes.Buffer
	dst := c.out
	if dst == nil {
		dst = &capture
	}
	maxOut := c.maxOut
	if maxOut <= 0 {
		maxOut = g.cfg.MaxOutputBytes
	}
	lw := &limitWriter{w: dst, left: maxOut}
	var stderr headWriter
	cmd.Stdout = lw
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	if lw.exceeded {
		sentinel := c.tooLarge
		if sentinel == nil {
			sentinel = ErrGitFailed
		}
		return gitOut{}, gitErr(sentinel, c.op, errOutputLimit)
	}
	if lw.werr != nil {
		return gitOut{}, gitErr(ErrGitFailed, c.op, fmt.Errorf("writing output: %w", lw.werr))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return gitOut{}, gitErr(ErrGitTimeout, c.op, ctxErr)
		}
		return gitOut{}, gitErr(ctxErr, c.op, ctxErr)
	}
	out := gitOut{stdout: capture.String()}
	if runErr == nil {
		return out, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		out.exit = ee.ExitCode()
		for _, ok := range c.okExit {
			if ok == out.exit {
				return out, nil
			}
		}
		sentinel := c.failAs
		if sentinel == nil {
			sentinel = ErrGitFailed
		}
		return out, gitErr(sentinel, c.op, fmt.Errorf("exit status %d: %s", out.exit, strings.TrimSpace(stderr.buf.String())))
	}
	return out, gitErr(ErrGitFailed, c.op, runErr)
}

// git runs a command that must exit 0 and returns its trimmed stdout.
func (g *GitRepo) git(ctx context.Context, root, op string, args ...string) (string, error) {
	out, err := g.run(ctx, gitCall{root: root, op: op, args: args}, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.stdout), nil
}

// revParse resolves rev (a fixed expression built from a validated SHA or
// HEAD) to a full SHA. A revision that does not resolve is ErrUnknownCommit.
func (g *GitRepo) revParse(ctx context.Context, root, rev string) (string, error) {
	out, err := g.run(ctx, gitCall{
		root: root, op: "rev-parse",
		args:   []string{"rev-parse", "--verify", "--quiet", rev},
		okExit: []int{1},
	}, nil)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(out.stdout)
	if out.exit != 0 || !isFullSHA(sha) {
		return "", &GitError{Err: ErrUnknownCommit, Op: "rev-parse"}
	}
	return sha, nil
}

func (g *GitRepo) head(ctx context.Context, root string) (string, error) {
	sha, err := g.revParse(ctx, root, "HEAD")
	if errors.Is(err, ErrUnknownCommit) {
		return "", &GitError{Err: ErrNoRepo, Op: "rev-parse"}
	}
	return sha, err
}

// ---- staging ----

// stageAll stages the work tree and validates the resulting index.
func (g *GitRepo) stageAll(ctx context.Context, root string) error {
	if err := ensureExclude(root); err != nil {
		return err
	}
	args := append([]string{"add", "-A"}, stagePathspec...)
	if _, err := g.git(ctx, root, "add", args...); err != nil {
		return err
	}
	return g.checkIndex(ctx, root)
}

// checkIndex rejects anything in the index that must never be committed. On a
// violation the index is reset to HEAD (when there is one) so a later retry
// starts clean, and the error names only the relative path.
func (g *GitRepo) checkIndex(ctx context.Context, root string) error {
	out, err := g.run(ctx, gitCall{
		root: root, op: "ls-files",
		args: []string{"ls-files", "-s", "-z"},
		// The index of a large template can exceed the ordinary cap.
		maxOut: 8 << 20,
	}, nil)
	if err != nil {
		return err
	}
	for _, rec := range strings.Split(out.stdout, "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) < 3 {
			return gitErr(ErrGitFailed, "ls-files", errors.New("unparseable index entry"))
		}
		if mode := fields[0]; (mode != "100644" && mode != "100755") || hasProtectedSegment(path) {
			// Best effort: failing to reset does not change the verdict.
			_, _ = g.git(ctx, root, "reset", "reset", "-q")
			return opErr(ErrUnsafeContent, truncateUTF8(path, 200))
		}
	}
	return nil
}

// commitSubject builds "turn N[: message]" with the message reduced to one
// bounded line of printable text, so it cannot smuggle options or control
// characters into git.
func commitSubject(turnSeq int, message string) string {
	base := fmt.Sprintf("turn %d", turnSeq)
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return ' '
		}
		return r
	}, message)
	clean = truncateUTF8(strings.TrimSpace(clean), maxCommitSubject)
	if clean == "" {
		return base
	}
	return base + ": " + clean
}

// bundle writes `git bundle create - <rev>` into memory, bounded.
func (g *GitRepo) bundle(ctx context.Context, root, rev string) ([]byte, error) {
	var buf bytes.Buffer
	_, err := g.run(ctx, gitCall{
		root: root, op: "bundle create",
		args:     []string{"bundle", "create", "-", rev},
		out:      &buf,
		maxOut:   g.cfg.MaxBundleBytes,
		tooLarge: ErrBundleTooLarge,
	}, nil)
	if err != nil {
		return nil, err
	}
	if buf.Len() == 0 {
		return nil, gitErr(ErrGitFailed, "bundle create", errors.New("empty bundle"))
	}
	return buf.Bytes(), nil
}

// finishTurn turns the staged index into a commit on top of prev and builds
// the incremental bundle. If the index tree equals prev's tree there is
// nothing to record: Changed is false and Bundle is nil.
//
// The bundle is built after HEAD moved (a bundle needs a ref). If building it
// fails, HEAD is moved back to prev so the caller can retry the turn; the
// index still holds the staged tree and the next attempt simply re-stages.
func (g *GitRepo) finishTurn(ctx context.Context, root, prev, subject string) (CommitResult, error) {
	tree, err := g.git(ctx, root, "write-tree", "write-tree")
	if err != nil {
		return CommitResult{}, err
	}
	prevTree, err := g.revParse(ctx, root, prev+"^{tree}")
	if err != nil {
		return CommitResult{}, err
	}
	if !isFullSHA(tree) {
		return CommitResult{}, gitErr(ErrGitFailed, "write-tree", errors.New("unexpected output"))
	}
	if tree == prevTree {
		return CommitResult{SHA: prev, Prev: prev}, nil
	}
	sha, err := g.git(ctx, root, "commit-tree", "commit-tree", tree, "-p", prev, "-m", subject)
	if err != nil {
		return CommitResult{}, err
	}
	if !isFullSHA(sha) {
		return CommitResult{}, gitErr(ErrGitFailed, "commit-tree", errors.New("unexpected output"))
	}
	if _, err := g.git(ctx, root, "update-ref", "update-ref", "-m", subject, "HEAD", sha, prev); err != nil {
		return CommitResult{}, err
	}
	data, err := g.bundle(ctx, root, prev+"..HEAD")
	if err != nil {
		// The caller's context may be what failed; roll back regardless.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = g.git(rctx, root, "update-ref", "update-ref", "HEAD", prev, sha)
		return CommitResult{}, err
	}
	return CommitResult{SHA: sha, Prev: prev, Bundle: data, Changed: true}, nil
}

// ---- public operations ----

// Commit records the work tree as the commit of turn turnSeq and returns the
// incremental bundle Prev..SHA. Ignored directories (.orbit, node_modules,
// dist) are never committed. If nothing changed no commit is created and the
// result has Changed=false, SHA==Prev==HEAD and a nil Bundle: the caller
// stores no snapshot for that turn.
func (g *GitRepo) Commit(ctx context.Context, root string, turnSeq int, message string) (CommitResult, error) {
	if turnSeq < 0 {
		return CommitResult{}, fmt.Errorf("turn sequence must not be negative: %w", ErrInvalidArgument)
	}
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return CommitResult{}, err
	}
	defer unlock()
	if err := requireRepo(r); err != nil {
		return CommitResult{}, err
	}
	clearStaleLocks(r)
	prev, err := g.head(ctx, r)
	if err != nil {
		return CommitResult{}, err
	}
	if err := g.stageAll(ctx, r); err != nil {
		return CommitResult{}, err
	}
	return g.finishTurn(ctx, r, prev, commitSubject(turnSeq, message))
}

// Revert creates a new commit on top of HEAD whose tree equals the tree of
// toSHA, keeping history. Files present now but absent from the target are
// removed; ignored directories are untouched. toSHA must be a full lowercase
// 40-hex commit id that is an ancestor of (or equal to) HEAD. Uncommitted
// changes in the work tree are discarded. If the resulting tree equals the
// current HEAD tree the result has Changed=false and a nil Bundle.
func (g *GitRepo) Revert(ctx context.Context, root, toSHA string, turnSeq int) (CommitResult, error) {
	if !isFullSHA(toSHA) {
		return CommitResult{}, &GitError{Err: ErrInvalidSHA, Op: "revert"}
	}
	if turnSeq < 0 {
		return CommitResult{}, fmt.Errorf("turn sequence must not be negative: %w", ErrInvalidArgument)
	}
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return CommitResult{}, err
	}
	defer unlock()
	if err := requireRepo(r); err != nil {
		return CommitResult{}, err
	}
	clearStaleLocks(r)
	prev, err := g.head(ctx, r)
	if err != nil {
		return CommitResult{}, err
	}
	target, err := g.revParse(ctx, r, toSHA+"^{commit}")
	if err != nil {
		return CommitResult{}, err
	}
	anc, err := g.run(ctx, gitCall{
		root: r, op: "merge-base",
		args:   []string{"merge-base", "--is-ancestor", target, "HEAD"},
		okExit: []int{1},
	}, nil)
	if err != nil {
		return CommitResult{}, err
	}
	if anc.exit != 0 {
		return CommitResult{}, &GitError{Err: ErrNotAncestor, Op: "merge-base"}
	}
	if err := ensureExclude(r); err != nil {
		return CommitResult{}, err
	}
	// Order matters: first drop untracked files (they could block the
	// checkout), then force index and work tree to the target tree (this also
	// deletes tracked files absent from it), then drop leftovers. The -e
	// patterns keep protected directories even if .gitignore negates them.
	clean := []string{"clean", "-fdq", "-e", ".orbit/", "-e", "node_modules/", "-e", "dist/"}
	if _, err := g.git(ctx, r, "clean", clean...); err != nil {
		return CommitResult{}, err
	}
	if _, err := g.git(ctx, r, "read-tree", "read-tree", "-u", "--reset", target); err != nil {
		return CommitResult{}, err
	}
	if _, err := g.git(ctx, r, "clean", clean...); err != nil {
		return CommitResult{}, err
	}
	subject := commitSubject(turnSeq, "revert to "+target[:12])
	return g.finishTurn(ctx, r, prev, subject)
}

// Head returns the full SHA of HEAD.
func (g *GitRepo) Head(ctx context.Context, root string) (string, error) {
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := requireRepo(r); err != nil {
		return "", err
	}
	return g.head(ctx, r)
}

// Log returns the first-parent history of HEAD, newest first. Output is
// capped at the configured MaxOutputBytes.
func (g *GitRepo) Log(ctx context.Context, root string) ([]LogEntry, error) {
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := requireRepo(r); err != nil {
		return nil, err
	}
	out, err := g.run(ctx, gitCall{
		root: r, op: "log",
		args: []string{"log", "-z", "--first-parent", "--format=%H%x1f%s", "HEAD"},
	}, nil)
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, rec := range strings.Split(out.stdout, "\x00") {
		if rec == "" {
			continue
		}
		sha, subject, _ := strings.Cut(strings.TrimLeft(rec, "\n"), "\x1f")
		if !isFullSHA(sha) {
			return nil, gitErr(ErrGitFailed, "log", errors.New("unparseable log entry"))
		}
		entries = append(entries, LogEntry{SHA: sha, Subject: subject})
	}
	return entries, nil
}

// Archive writes a tar of the tree at sha ("HEAD" or a full 40-hex commit id)
// to w using `git archive`. Only committed content is included, so ignored
// directories are absent by construction. The output is capped at
// MaxArchiveBytes (ErrArchiveTooLarge). A failing or slow w holds the per-root
// lock until the command timeout.
func (g *GitRepo) Archive(ctx context.Context, root, sha string, w io.Writer) error {
	if sha != "HEAD" && !isFullSHA(sha) {
		return &GitError{Err: ErrInvalidSHA, Op: "archive"}
	}
	if w == nil {
		return fmt.Errorf("writer is nil: %w", ErrInvalidArgument)
	}
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := requireRepo(r); err != nil {
		return err
	}
	commit, err := g.revParse(ctx, r, sha+"^{commit}")
	if err != nil {
		return err
	}
	_, err = g.run(ctx, gitCall{
		root: r, op: "archive",
		args:     []string{"archive", "--format=tar", commit},
		out:      w,
		maxOut:   g.cfg.MaxArchiveBytes,
		tooLarge: ErrArchiveTooLarge,
	}, nil)
	return err
}

// Init unpacks a template tar into the empty session root, enforces the
// required .gitignore entries, creates the repository on branch main and
// records the first commit "turn 0". The result carries a full bundle of
// HEAD. On any failure the root is emptied again so Init can be retried.
//
// The tar is plain (not compressed). Symlinks, hardlinks, device files,
// absolute or ".." paths and entries under .git, .orbit, node_modules or dist
// are rejected with ErrUnsafeArchiveEntry; sizes are bounded by
// [GitConfig.Tar]. File modes are normalized to 0644 or 0755.
func (g *GitRepo) Init(ctx context.Context, root string, tarStream io.Reader) (res CommitResult, err error) {
	if tarStream == nil {
		return CommitResult{}, fmt.Errorf("template stream is nil: %w", ErrInvalidArgument)
	}
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return CommitResult{}, err
	}
	defer unlock()
	if err = checkEmptyRoot(r); err != nil {
		return CommitResult{}, err
	}
	defer func() {
		if err != nil {
			clearRoot(r)
		}
	}()

	if err = g.extractTar(r, tarStream); err != nil {
		return CommitResult{}, err
	}
	if err = ensureGitignore(r); err != nil {
		return CommitResult{}, err
	}
	if _, err = g.run(ctx, gitCall{
		root: r, op: "init", noGitDir: true,
		args: []string{"init", "-q", "--template=", "-b", gitMainBranch},
	}, nil); err != nil {
		return CommitResult{}, err
	}
	if err = g.stageAll(ctx, r); err != nil {
		return CommitResult{}, err
	}
	var tree, sha string
	if tree, err = g.git(ctx, r, "write-tree", "write-tree"); err != nil {
		return CommitResult{}, err
	}
	if !isFullSHA(tree) {
		err = gitErr(ErrGitFailed, "write-tree", errors.New("unexpected output"))
		return CommitResult{}, err
	}
	if sha, err = g.git(ctx, r, "commit-tree", "commit-tree", tree, "-m", commitSubject(0, "")); err != nil {
		return CommitResult{}, err
	}
	if !isFullSHA(sha) {
		err = gitErr(ErrGitFailed, "commit-tree", errors.New("unexpected output"))
		return CommitResult{}, err
	}
	if _, err = g.git(ctx, r, "update-ref", "update-ref", "HEAD", sha, zeroSHA); err != nil {
		return CommitResult{}, err
	}
	var data []byte
	if data, err = g.bundle(ctx, r, "HEAD"); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{SHA: sha, Bundle: data, Changed: true}, nil
}

// Restore rebuilds a repository from ordered bundles (a full bundle first,
// then the incremental ones) in an empty session root and materializes the
// work tree at the last tip on branch main, with full history. It returns the
// restored HEAD. Bundles are verified, fetched with object validation, and
// each Bundle.SHA that is set must match the tip the bundle yields. A root
// that already holds a .git (ErrRepoExists) or any file other than the
// builder-private .orbit (ErrRootNotEmpty) is rejected. On failure the root
// is emptied again so Restore can be retried.
func (g *GitRepo) Restore(ctx context.Context, root string, bundles []Bundle) (head string, err error) {
	if len(bundles) == 0 || len(bundles) > maxBundles {
		return "", fmt.Errorf("bundle count out of range: %w", ErrInvalidArgument)
	}
	var total int64
	for _, b := range bundles {
		if b.SHA != "" && !isFullSHA(b.SHA) {
			return "", &GitError{Err: ErrInvalidSHA, Op: "restore"}
		}
		n := int64(len(b.Data))
		if n == 0 {
			return "", &GitError{Err: ErrBundleVerify, Op: "restore"}
		}
		total += n
		if n > g.cfg.MaxBundleBytes || total > g.cfg.MaxRestoreBytes {
			return "", &GitError{Err: ErrBundleTooLarge, Op: "restore"}
		}
	}
	r, unlock, err := g.lockRoot(root)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err = checkEmptyRoot(r); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			clearRoot(r)
		}
	}()

	// Bundle files live in a private 0700 dir under .orbit/tmp (never
	// committed, never world readable) and are removed afterwards.
	orbit := filepath.Join(r, orbitDirName)
	if err = ensureRealDir(orbit); err != nil {
		return "", err
	}
	tmpRoot := filepath.Join(orbit, tmpDirName)
	if err = ensureRealDir(tmpRoot); err != nil {
		return "", err
	}
	tmp, merr := os.MkdirTemp(tmpRoot, "restore-")
	if merr != nil {
		err = &OpError{Err: ErrIO, cause: merr}
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if _, err = g.run(ctx, gitCall{
		root: r, op: "init", noGitDir: true,
		args: []string{"init", "-q", "--template=", "-b", gitMainBranch},
	}, nil); err != nil {
		return "", err
	}
	if err = ensureExclude(r); err != nil {
		return "", err
	}

	var tip string
	for i, b := range bundles {
		file := filepath.Join(tmp, fmt.Sprintf("%04d.bundle", i))
		if werr := writePrivateFile(file, b.Data); werr != nil {
			err = &OpError{Err: ErrIO, cause: werr}
			return "", err
		}
		if _, err = g.run(ctx, gitCall{
			root: r, op: "bundle verify", failAs: ErrBundleVerify,
			args: []string{"bundle", "verify", file},
		}, nil); err != nil {
			return "", err
		}
		// protocol.file.allow=never (global hardening) also blocks fetching
		// a bundle file. The path is a file this function just wrote and
		// verified, so re-allowing it for this call only is safe; fsck
		// rejects malformed or hostile objects (".git" tree entries...).
		ref := fmt.Sprintf("%s%d", restoreRefPrefix, i)
		if _, err = g.run(ctx, gitCall{
			root: r, op: "fetch", failAs: ErrBundleVerify,
			extraCfg: []string{"protocol.file.allow=always", "transfer.fsckObjects=true"},
			args:     []string{"fetch", "-q", "--no-tags", "--no-write-fetch-head", file, "HEAD:" + ref},
		}, nil); err != nil {
			return "", err
		}
		if tip, err = g.revParse(ctx, r, ref); err != nil {
			err = &GitError{Err: ErrBundleVerify, Op: "fetch"}
			return "", err
		}
		if b.SHA != "" && b.SHA != tip {
			err = &GitError{Err: ErrBundleVerify, Op: "restore"}
			return "", err
		}
		if _, err = g.git(ctx, r, "update-ref", "update-ref", "-d", ref); err != nil {
			return "", err
		}
		// The next bundle's prerequisites must already be reachable; keep the
		// tip alive until then (gc.auto=0, but be explicit).
		if _, err = g.git(ctx, r, "update-ref", "update-ref", "refs/heads/"+gitMainBranch, tip); err != nil {
			return "", err
		}
	}
	if _, err = g.git(ctx, r, "reset", "reset", "-q", "--hard", "HEAD"); err != nil {
		return "", err
	}
	if head, err = g.head(ctx, r); err != nil {
		return "", err
	}
	if head != tip {
		err = &GitError{Err: ErrBundleVerify, Op: "restore"}
		return "", err
	}
	return head, nil
}

// ---- root state ----

// checkEmptyRoot requires a root with nothing but the builder-private .orbit.
func checkEmptyRoot(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	var hasGit, hasOther bool
	for _, e := range entries {
		switch e.Name() {
		case orbitDirName:
		case gitDirName:
			hasGit = true
		default:
			hasOther = true
		}
	}
	switch {
	case hasGit:
		return &GitError{Err: ErrRepoExists}
	case hasOther:
		return &GitError{Err: ErrRootNotEmpty}
	}
	return nil
}

// clearRoot empties the root except .orbit (the idempotency log is not ours
// to delete). Best effort: it runs on failure paths only.
func clearRoot(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == orbitDirName {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

func writePrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ensureGitignore appends the required ignore lines missing from the
// template's .gitignore (creating it when absent). Template lines are kept.
func ensureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	var cur []byte
	li, err := os.Lstat(path)
	switch {
	case err == nil:
		if !li.Mode().IsRegular() {
			return opErr(ErrUnsafeArchiveEntry, ".gitignore")
		}
		if cur, err = os.ReadFile(path); err != nil {
			return &OpError{Err: ErrIO, cause: err}
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return &OpError{Err: ErrIO, cause: err}
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(cur), "\n") {
		line = strings.TrimSpace(line)
		have[strings.Trim(line, "/")] = true
	}
	var add []string
	for _, want := range requiredIgnores {
		if !have[strings.Trim(want, "/")] {
			add = append(add, want)
		}
	}
	if len(add) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.Write(cur)
	if len(cur) > 0 && cur[len(cur)-1] != '\n' {
		b.WriteByte('\n')
	}
	for _, l := range add {
		b.WriteString(l + "\n")
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return &OpError{Err: ErrIO, cause: err}
	}
	return nil
}

// ---- tar extraction ----

// capReader fails once more than left bytes were read, bounding what a
// decompression-free but padded or header-heavy stream can make us consume.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errStreamLimit
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// cleanEntryName validates a tar entry name and returns the slash-separated
// relative path ("" for the root directory entry).
func cleanEntryName(name string) (string, error) {
	if name == "" || len(name) > maxEntryPath || strings.ContainsAny(name, "\x00\\") {
		return "", errors.New("bad name")
	}
	if strings.HasPrefix(name, "/") {
		return "", errors.New("absolute path")
	}
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	name = strings.TrimRight(name, "/")
	if name == "" || name == "." {
		return "", nil
	}
	segs := strings.Split(name, "/")
	if len(segs) > maxEntryDepth {
		return "", errors.New("too deep")
	}
	for _, s := range segs {
		switch {
		case s == "" || s == "." || s == "..":
			return "", errors.New("non-canonical path")
		case IsProtectedName(s):
			return "", errors.New("protected name")
		}
	}
	return name, nil
}

// extractTar unpacks the stream into the empty root. Because the root starts
// empty and this function never creates links, no path component can be a
// symlink, so joining validated relative names under root cannot escape it.
func (g *GitRepo) extractTar(root string, r io.Reader) error {
	lim := g.cfg.Tar
	// Headers and padding are not counted in file sizes; bound them too.
	streamMax := lim.MaxTotalBytes + int64(lim.MaxFiles+16)*4096
	tr := tar.NewReader(&capReader{r: r, left: streamMax})

	var total int64
	var count int
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if errors.Is(err, errStreamLimit) {
				return &GitError{Err: ErrArchiveTooLarge, Op: "tar"}
			}
			return gitErr(ErrInvalidArchive, "tar", err)
		}
		shown := truncateUTF8(hdr.Name, 200)
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			// `git archive` emits one (commit id); it carries no file.
			continue
		case tar.TypeReg, tar.TypeDir:
		default:
			// Symlinks, hardlinks, devices, fifos: rejected, not neutralized,
			// so the template author learns about them.
			return opErr(ErrUnsafeArchiveEntry, shown)
		}
		rel, nerr := cleanEntryName(hdr.Name)
		if nerr != nil {
			return opErr(ErrUnsafeArchiveEntry, shown)
		}
		if rel == "" {
			continue
		}
		count++
		if count > lim.MaxFiles {
			return &GitError{Err: ErrArchiveTooLarge, Op: "tar"}
		}
		full := filepath.Join(root, filepath.FromSlash(rel))

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(full, 0o755); err != nil {
				return entryIOErr(rel, err)
			}
			if err := os.Chmod(full, 0o755); err != nil {
				return entryIOErr(rel, err)
			}
			continue
		}

		if hdr.Size < 0 || hdr.Size > lim.MaxFileBytes {
			return &GitError{Err: ErrArchiveTooLarge, Op: "tar"}
		}
		total += hdr.Size
		if total > lim.MaxTotalBytes {
			return &GitError{Err: ErrArchiveTooLarge, Op: "tar"}
		}
		mode := fs.FileMode(0o644)
		if hdr.Mode&0o111 != 0 {
			mode = 0o755
		}
		if err := writeTarFile(full, tr, hdr.Size, mode); err != nil {
			if errors.Is(err, errStreamLimit) {
				return &GitError{Err: ErrArchiveTooLarge, Op: "tar"}
			}
			return entryIOErr(rel, err)
		}
	}
}

// entryIOErr classifies a filesystem error during extraction: a path that
// collides with an earlier entry (file vs directory, duplicate) is a hostile
// or broken archive, anything else is a plain I/O error.
func entryIOErr(rel string, err error) error {
	if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EISDIR) {
		return opErr(ErrUnsafeArchiveEntry, truncateUTF8(rel, 200))
	}
	return ioErr(rel, err)
}

// writeTarFile creates a new file (O_EXCL: a duplicate entry fails instead of
// overwriting) and copies exactly size bytes.
func writeTarFile(full string, src io.Reader, size int64, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, src, size); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(full, mode)
}

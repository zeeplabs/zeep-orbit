// Package sandbox is the Orbit-side client of the builder daemon
// (internal/builder), behind an interface so another driver (a per-session
// container, say) can replace the single shared builder later.
//
// The agent never touches the builder wire format: it talks to [Sandbox], and
// every error that reaches the model passes through [ToolMessage], which only
// releases messages the builder marks as tool-level (path outside the
// workspace, old_string not found, ...). Everything else (transport failures,
// 5xx, bad credentials) is reported to the caller as an [*Error] whose Error()
// is generic and whose Unwrap carries the raw cause for server-side logs.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

// BuildStep names one of the builder's fixed build commands.
type BuildStep string

// Build steps accepted by the builder.
const (
	StepInstall   BuildStep = "install"
	StepTypecheck BuildStep = "typecheck"
	StepLint      BuildStep = "lint"
	StepBuild     BuildStep = "build"
)

// Valid reports whether s is one of the four known steps.
func (s BuildStep) Valid() bool {
	switch s {
	case StepInstall, StepTypecheck, StepLint, StepBuild:
		return true
	}
	return false
}

// File operation kinds accepted by [Sandbox.Op].
const (
	OpListDir   = "list_dir"
	OpReadFile  = "read_file"
	OpWriteFile = "write_file"
	OpEditFile  = "edit_file"
)

// TemplateSource is where a new session's files come from. Set exactly one
// field.
type TemplateSource struct {
	// Name selects a template known to the builder (BUILDER_TEMPLATES_DIR),
	// e.g. "default". It must match [a-z0-9-]+.
	Name string
	// Tar is a plain tar stream of the initial tree.
	Tar io.Reader
}

// Bundle is one stored git snapshot, passed to Restore in apply order: a full
// bundle first, then incremental ones.
type Bundle struct {
	// Seq is the turn the bundle belongs to (informational).
	Seq int
	// SHA is the commit the bundle must yield. The builder verifies it when
	// set, which catches a wrong or reordered snapshot.
	SHA string
	// Data is the git bundle.
	Data []byte
}

// FileOp is one workspace operation. Only the fields that belong to Kind are
// used.
type FileOp struct {
	Kind       string
	Path       string
	Offset     int
	Limit      int
	Content    string
	OldString  string
	NewString  string
	ReplaceAll bool
}

// OpResult is the outcome of a successful [Sandbox.Op].
type OpResult struct {
	OK bool
	// Output is the text shown to the model.
	Output string
	// Replayed is true when the builder returned the stored result of op_id
	// instead of executing the operation again.
	Replayed bool
}

// BuildResult is the outcome of [Sandbox.Build]. A non-zero ExitCode is a
// normal result, not an error.
type BuildResult struct {
	ExitCode   int
	OutputTail string
	DurationMs int64
	TimedOut   bool
	Replayed   bool
}

// InitResult is the outcome of [Sandbox.Init]: the "turn 0" commit.
type InitResult struct {
	SHA string
	// Bundle is the full git bundle of HEAD, to store as the first snapshot.
	Bundle []byte
	// TemplateVersion is the template's TEMPLATE_VERSION, "" when absent.
	TemplateVersion string
}

// CommitResult is the outcome of [Sandbox.Commit] and [Sandbox.Revert].
type CommitResult struct {
	SHA  string
	Prev string
	// Changed is false when nothing changed and no commit was created; Bundle
	// is then empty.
	Changed bool
	// Bundle is the incremental bundle Prev..SHA.
	Bundle []byte
}

// Health is the builder's capacity report.
type Health struct {
	Active                 int
	Capacity               int
	Version                string
	DefaultTemplateVersion string
}

// Sandbox is the workspace the agent edits and builds in. Implementations
// must be safe for concurrent use.
//
// Mutating calls (Op with write_file/edit_file, Build) are idempotent per
// opID: repeating a call with the same opID after a crash returns the stored
// result without executing again. The agent uses the model's tool call id as
// opID.
//
// Every method that takes a session id returns [ErrUnknownSession] when the
// builder has no workspace for it (volume loss); see [WithRestore].
type Sandbox interface {
	// Init creates the session from a template and returns the turn 0 commit.
	Init(ctx context.Context, sessionID string, tmpl TemplateSource) (InitResult, error)
	// Restore rebuilds the session from stored bundles.
	Restore(ctx context.Context, sessionID string, bundles []Bundle) error
	// Op runs one file operation.
	Op(ctx context.Context, sessionID, opID string, op FileOp) (OpResult, error)
	// Build runs one fixed build step. previewBase is the public URL path the
	// built app is served under (vite --base); only the build step uses it.
	Build(ctx context.Context, sessionID, opID string, step BuildStep, previewBase string) (BuildResult, error)
	// BuildLog returns the tail of the last output of step (tail <= 0 means the
	// builder default). It is a tool-level error ("no build log") when the step
	// never ran.
	BuildLog(ctx context.Context, sessionID string, step BuildStep, tail int) (string, error)
	// Commit snapshots the working tree as turn turnSeq.
	Commit(ctx context.Context, sessionID string, turnSeq int, message string) (CommitResult, error)
	// Revert adds a commit whose tree equals toSHA (history is kept).
	Revert(ctx context.Context, sessionID, toSHA string, turnSeq int) (CommitResult, error)
	// Archive streams the tree at sha ("" means HEAD) as a tar. The caller
	// closes the reader, and ctx must outlive reading it.
	Archive(ctx context.Context, sessionID, sha string) (io.ReadCloser, error)
	// Preview returns a handler serving the builder's preview of the session.
	// The request path is the path inside the preview, so callers strip their
	// own route prefix first (http.StripPrefix). Only GET and HEAD are
	// forwarded; cookies, credentials and the query string never are.
	Preview(sessionID string) http.Handler
	// Destroy removes the session workspace. Destroying an absent session is
	// not an error.
	Destroy(ctx context.Context, sessionID string) error
	// Health reports builder capacity.
	Health(ctx context.Context) (Health, error)
}

var (
	// ErrUnknownSession means the builder has no workspace for the session
	// (its volume was lost). The caller restores from snapshots and retries
	// once; [WithRestore] does exactly that. Match with errors.Is.
	ErrUnknownSession = errors.New("sandbox: unknown session")

	// ErrWorkspaceLost means the workspace is gone and restoring it did not
	// work: no snapshots, a failed restore, or the session still unknown
	// right after a restore. The turn should fail with code workspace_lost.
	// Match with errors.Is.
	ErrWorkspaceLost = errors.New("sandbox: workspace lost")
)

// maxMessageLen bounds the builder message kept in an [Error].
const maxMessageLen = 512

// Error is a failure reported by (or while reaching) the builder.
//
// Error() never includes the underlying cause: Err can carry URLs and host
// names and is for server-side logs only (reach it with errors.Unwrap).
type Error struct {
	// Status is the HTTP status, 0 when no response was received.
	Status int
	// Code is the builder's stable snake_case code ("edit_not_found",
	// "at_capacity", ...), or "unavailable" / "bad_response" for failures that
	// did not come from the builder's own error envelope.
	Code string
	// Message is a short English description. For errors where [Error.ToolLevel]
	// is true it is the builder's own message; otherwise it is fixed and
	// generic.
	Message string
	// RetryAfter is the wait the builder asked for on 429, 0 otherwise.
	RetryAfter time.Duration
	// Err is the underlying transport or decoding error, if any.
	Err error
}

// Error implements error without leaking Err.
func (e *Error) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("sandbox: builder error (status %d, code %s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("sandbox: %s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for server-side inspection.
func (e *Error) Unwrap() error { return e.Err }

// ToolLevel reports whether the failure is one the model can act on and
// should therefore see: a 4xx from the builder about the request itself (bad
// path, old_string not found, file too large). Authentication failures (401)
// and capacity (429) are operator problems, not the model's.
func (e *Error) ToolLevel() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusUnauthorized && e.Status != http.StatusTooManyRequests
}

// ToolMessage returns the message to show the model for err, and whether err
// is a tool-level failure at all. When ok is false the caller must treat err
// as infrastructure: log it, never forward its text.
func ToolMessage(err error) (msg string, ok bool) {
	var e *Error
	if errors.As(err, &e) && e.ToolLevel() {
		return e.Message, true
	}
	return "", false
}

// sanitizeMessage keeps the first maxMessageLen bytes of a builder message,
// cut on a rune boundary, with control characters replaced by spaces.
func sanitizeMessage(s string) string {
	if len(s) > maxMessageLen {
		s = s[:maxMessageLen]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}

// SnapshotProvider returns the stored bundles of a session in apply order
// (full bundle first). The agent supplies it from its store; this package
// does not know about the database.
type SnapshotProvider func(ctx context.Context, sessionID string) ([]Bundle, error)

// WithRestore runs fn; if it fails with [ErrUnknownSession] it restores the
// session from snapshots and runs fn once more. A second unknown-session
// answer, a missing snapshot set, or a failed restore return an error that
// matches [ErrWorkspaceLost]. Any other error (including context
// cancellation) is returned unchanged and nothing is restored.
//
// A restore that finds the session already there (another caller restored it
// first, builder code session_exists) counts as success.
//
// The restored tree is the last committed snapshot: work done in the current
// turn before the volume was lost is gone, and the retried call runs against
// the older tree.
func WithRestore[T any](ctx context.Context, sb Sandbox, sessionID string, snapshots SnapshotProvider, fn func(ctx context.Context) (T, error)) (T, error) {
	res, err := fn(ctx)
	if !errors.Is(err, ErrUnknownSession) {
		return res, err
	}
	var zero T
	if snapshots == nil {
		return zero, fmt.Errorf("%w: no snapshot provider", ErrWorkspaceLost)
	}
	bundles, serr := snapshots(ctx, sessionID)
	if serr != nil {
		if ctx.Err() != nil {
			return zero, serr
		}
		return zero, fmt.Errorf("%w: load snapshots: %w", ErrWorkspaceLost, serr)
	}
	if len(bundles) == 0 {
		return zero, fmt.Errorf("%w: no snapshots to restore from", ErrWorkspaceLost)
	}
	if rerr := sb.Restore(ctx, sessionID, bundles); rerr != nil {
		var be *Error
		exists := errors.As(rerr, &be) && be.Code == "session_exists"
		if !exists {
			if ctx.Err() != nil {
				return zero, rerr
			}
			return zero, fmt.Errorf("%w: restore: %w", ErrWorkspaceLost, rerr)
		}
	}
	res, err = fn(ctx)
	if errors.Is(err, ErrUnknownSession) {
		return zero, fmt.Errorf("%w: session still unknown after restore", ErrWorkspaceLost)
	}
	return res, err
}

// NewRestoring wraps sb so that Op, Build, BuildLog, Commit, Revert and
// Archive transparently go through [WithRestore]. The other methods pass
// through: Init, Restore and Destroy are never retried, and a preview of a
// lost session is simply a 404.
func NewRestoring(sb Sandbox, snapshots SnapshotProvider) Sandbox {
	return &restoring{Sandbox: sb, snapshots: snapshots}
}

type restoring struct {
	Sandbox
	snapshots SnapshotProvider
}

func (r *restoring) Op(ctx context.Context, sessionID, opID string, op FileOp) (OpResult, error) {
	return WithRestore(ctx, r.Sandbox, sessionID, r.snapshots, func(ctx context.Context) (OpResult, error) {
		return r.Sandbox.Op(ctx, sessionID, opID, op)
	})
}

func (r *restoring) Build(ctx context.Context, sessionID, opID string, step BuildStep, previewBase string) (BuildResult, error) {
	return WithRestore(ctx, r.Sandbox, sessionID, r.snapshots, func(ctx context.Context) (BuildResult, error) {
		return r.Sandbox.Build(ctx, sessionID, opID, step, previewBase)
	})
}

func (r *restoring) BuildLog(ctx context.Context, sessionID string, step BuildStep, tail int) (string, error) {
	return WithRestore(ctx, r.Sandbox, sessionID, r.snapshots, func(ctx context.Context) (string, error) {
		return r.Sandbox.BuildLog(ctx, sessionID, step, tail)
	})
}

func (r *restoring) Commit(ctx context.Context, sessionID string, turnSeq int, message string) (CommitResult, error) {
	return WithRestore(ctx, r.Sandbox, sessionID, r.snapshots, func(ctx context.Context) (CommitResult, error) {
		return r.Sandbox.Commit(ctx, sessionID, turnSeq, message)
	})
}

func (r *restoring) Revert(ctx context.Context, sessionID, toSHA string, turnSeq int) (CommitResult, error) {
	return WithRestore(ctx, r.Sandbox, sessionID, r.snapshots, func(ctx context.Context) (CommitResult, error) {
		return r.Sandbox.Revert(ctx, sessionID, toSHA, turnSeq)
	})
}

func (r *restoring) Archive(ctx context.Context, sessionID, sha string) (io.ReadCloser, error) {
	return WithRestore(ctx, r.Sandbox, sessionID, r.snapshots, func(ctx context.Context) (io.ReadCloser, error) {
		return r.Sandbox.Archive(ctx, sessionID, sha)
	})
}

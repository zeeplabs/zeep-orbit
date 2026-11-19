package builder

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"
)

// Workspace operations run on file paths chosen by an LLM that may have been
// prompt-injected, so every path is treated as hostile input. The guarantees
// implemented here:
//
//   - paths are relative, clean, and never leave the (symlink-resolved)
//     session root, even through symlinks planted inside it;
//   - .git, node_modules, dist and .orbit are unreachable for every operation
//     (the agent sees build output through build tools, not raw files);
//   - content is UTF-8 text without NUL bytes, bounded per file and per session;
//   - errors returned to the caller carry only the relative path, never a host
//     path, so they are safe to hand back to the model.
//
// Remaining TOCTOU limit: a path is resolved first and used afterwards, so a
// symlink swapped in between the two steps could still redirect an operation.
// Closing that fully needs openat2(RESOLVE_BENEATH) or fd-relative syscalls.
// It is accepted because the builder is single-tenant per process, the only
// writer inside a session root is this package (driven serially by the agent)
// and build tools run with --ignore-scripts, so nothing else creates symlinks
// concurrently with an operation.

// Names that are never reachable through workspace operations, at any depth.
var protectedNames = []string{".git", "node_modules", "dist", orbitDirName}

const (
	// orbitDirName holds builder-private state (idempotency log, temp files).
	// It lives inside the session root, so it must be listed in the
	// repository's .gitignore by the init/commit step (T05) or it would end
	// up in commits and bundles.
	orbitDirName = ".orbit"
	opsLogName   = "ops.jsonl"
	tmpDirName   = "tmp"

	maxOpIDLen      = 128
	maxSessionIDLen = 64
)

// File operation kinds accepted by [Workspace.Apply].
const (
	// FileOpListDir lists a directory (non-recursive).
	FileOpListDir = "list_dir"
	// FileOpReadFile reads a text file with line numbers.
	FileOpReadFile = "read_file"
	// FileOpWriteFile creates or overwrites a text file.
	FileOpWriteFile = "write_file"
	// FileOpEditFile replaces an exact string in a text file.
	FileOpEditFile = "edit_file"
)

// sessionIDPattern restricts session ids to a charset that cannot express a
// path separator or dot segment, so the id alone can never traverse.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// Typed workspace errors. Callers (the HTTP layer) match them with errors.Is;
// messages are English and safe to show to the LLM.
var (
	// ErrInvalidSessionID means the session id is empty, too long or has
	// characters outside [A-Za-z0-9-].
	ErrInvalidSessionID = errors.New("invalid session id")
	// ErrSessionNotFound means the session root directory does not exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrInvalidPath means the path is empty or contains a NUL byte.
	ErrInvalidPath = errors.New("invalid path")
	// ErrPathOutside means the path is absolute, traverses above the session
	// root, or resolves (through symlinks) outside of it.
	ErrPathOutside = errors.New("path is outside the session")
	// ErrPathForbidden means the path touches .git, node_modules, dist or .orbit.
	ErrPathForbidden = errors.New("path is in a protected directory")
	// ErrSymlink means a write targets a symlink.
	ErrSymlink = errors.New("refusing to write through a symlink")
	// ErrNotFound means the path does not exist.
	ErrNotFound = errors.New("no such file or directory")
	// ErrIsDirectory means a file operation targeted a directory.
	ErrIsDirectory = errors.New("is a directory")
	// ErrNotDirectory means a path component or list target is not a directory.
	ErrNotDirectory = errors.New("not a directory")
	// ErrNotRegular means the target is neither a regular file nor a directory.
	ErrNotRegular = errors.New("not a regular file")
	// ErrBinaryFile means a read or edit hit a file that is not UTF-8 text.
	ErrBinaryFile = errors.New("binary file")
	// ErrInvalidContent means content to write is not UTF-8 text or has NUL bytes.
	ErrInvalidContent = errors.New("content must be valid UTF-8 text without NUL bytes")
	// ErrFileTooLarge means a file exceeds the per-file size limit.
	ErrFileTooLarge = errors.New("file exceeds the size limit")
	// ErrSessionTooLarge means the write would exceed the per-session size cap.
	ErrSessionTooLarge = errors.New("session size limit exceeded")
	// ErrEditEmptyOld means edit_file got an empty old_string.
	ErrEditEmptyOld = errors.New("old_string must not be empty")
	// ErrEditNoop means old_string and new_string are identical.
	ErrEditNoop = errors.New("old_string and new_string are identical")
	// ErrEditNotFound means old_string does not occur in the file.
	ErrEditNotFound = errors.New("old_string not found")
	// ErrEditNotUnique means old_string occurs more than once and replace_all is off.
	ErrEditNotUnique = errors.New("old_string is not unique")
	// ErrInvalidArgument means an operation argument is out of range or missing.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrUnknownOp means the operation kind is not supported.
	ErrUnknownOp = errors.New("unknown operation kind")
	// ErrOpIDReused means an op_id was already recorded for a different operation.
	ErrOpIDReused = errors.New("op_id already used for a different operation")
	// ErrOpLog means the idempotency log could not be read or written. When
	// returned from [Workspace.Apply] together with a result, the operation
	// itself was applied but could not be recorded.
	ErrOpLog = errors.New("idempotency log error")
	// ErrIO is the catch-all for unexpected filesystem failures. The
	// underlying error is available through [OpError.Cause] for server-side
	// logging and is deliberately not part of the message.
	ErrIO = errors.New("i/o error")
)

// OpError is the error type of every workspace operation. It wraps one of the
// sentinel errors above (errors.Is works) and only ever carries the relative
// path the caller supplied.
type OpError struct {
	// Err is the sentinel describing the failure.
	Err error
	// Path is the caller-supplied relative path, if the error concerns one.
	Path string
	// Msg overrides the generated message when set (used for edit errors,
	// whose wording is part of the tool contract).
	Msg string

	cause error
}

// Error implements error. It never includes host paths.
func (e *OpError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Path == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %q", e.Err.Error(), e.Path)
}

// Unwrap returns the sentinel so errors.Is(err, ErrPathOutside) etc. work.
func (e *OpError) Unwrap() error { return e.Err }

// Cause returns the underlying OS error, which may contain host paths. Log it
// server-side; never send it to the client.
func (e *OpError) Cause() error { return e.cause }

func opErr(sentinel error, path string) error {
	return &OpError{Err: sentinel, Path: path}
}

// ioErr maps an OS error to a typed, path-free error.
func ioErr(path string, cause error) error {
	sentinel := ErrIO
	switch {
	case errors.Is(cause, fs.ErrNotExist):
		sentinel = ErrNotFound
	case errors.Is(cause, syscall.ENOTDIR):
		sentinel = ErrNotDirectory
	case errors.Is(cause, syscall.EISDIR):
		sentinel = ErrIsDirectory
	}
	return &OpError{Err: sentinel, Path: path, cause: cause}
}

// WorkspaceLimits bounds what a session may read and write. Zero or negative
// fields fall back to the defaults from [DefaultWorkspaceLimits].
type WorkspaceLimits struct {
	// MaxFileBytes is the largest file that write_file/edit_file accept.
	MaxFileBytes int64
	// MaxSessionBytes caps the total size of all files in the session root,
	// excluding the protected directories.
	MaxSessionBytes int64
	// MaxScanBytes is the largest file read_file will load to number lines.
	// Template files (lockfiles) can exceed MaxFileBytes, so it is separate.
	MaxScanBytes int64
	// MaxReadBytes caps the output of a single read_file call.
	MaxReadBytes int
	// MaxReadLines is the default and maximum line count of a read_file call.
	MaxReadLines int
	// MaxListEntries caps the entries returned by list_dir.
	MaxListEntries int
}

// DefaultWorkspaceLimits returns the production defaults.
func DefaultWorkspaceLimits() WorkspaceLimits {
	return WorkspaceLimits{
		MaxFileBytes:    512 << 10,
		MaxSessionBytes: 20 << 20,
		MaxScanBytes:    4 << 20,
		MaxReadBytes:    64 << 10,
		MaxReadLines:    2000,
		MaxListEntries:  500,
	}
}

func (l WorkspaceLimits) withDefaults() WorkspaceLimits {
	d := DefaultWorkspaceLimits()
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxSessionBytes <= 0 {
		l.MaxSessionBytes = d.MaxSessionBytes
	}
	if l.MaxScanBytes <= 0 {
		l.MaxScanBytes = d.MaxScanBytes
	}
	if l.MaxScanBytes < l.MaxFileBytes {
		l.MaxScanBytes = l.MaxFileBytes
	}
	if l.MaxReadBytes <= 0 {
		l.MaxReadBytes = d.MaxReadBytes
	}
	if l.MaxReadLines <= 0 {
		l.MaxReadLines = d.MaxReadLines
	}
	if l.MaxListEntries <= 0 {
		l.MaxListEntries = d.MaxListEntries
	}
	return l
}

// FileOp is one tool call against a session workspace. Only the fields that
// belong to Kind are used.
type FileOp struct {
	// Kind is one of the FileOp* constants.
	Kind string `json:"kind"`
	// Path is relative to the session root. list_dir treats "" as the root.
	Path string `json:"path"`
	// Offset is the 1-based first line for read_file (0 means 1).
	Offset int `json:"offset,omitempty"`
	// Limit is the max number of lines for read_file (0 means the default).
	Limit int `json:"limit,omitempty"`
	// Content is the full new file content for write_file.
	Content string `json:"content,omitempty"`
	// OldString is the exact text edit_file looks for.
	OldString string `json:"old_string,omitempty"`
	// NewString replaces OldString in edit_file.
	NewString string `json:"new_string,omitempty"`
	// ReplaceAll lets edit_file replace every occurrence of OldString.
	ReplaceAll bool `json:"replace_all,omitempty"`
}

// OpResult is what the LLM sees after a successful operation.
type OpResult struct {
	// OK is true for every successful operation.
	OK bool `json:"ok"`
	// Output is the text returned to the model.
	Output string `json:"output"`
	// Replayed is true when the result came from the idempotency log and the
	// operation was not executed again. It is never persisted.
	Replayed bool `json:"replayed,omitempty"`
}

// OpRecord is one line of the idempotency log: the stored outcome of op_id.
type OpRecord struct {
	// OpID is the caller-chosen idempotency key.
	OpID string `json:"op_id"`
	// Kind is the operation kind (file op kinds, or e.g. "build").
	Kind string `json:"kind"`
	// Path is the path the operation targeted, when it has one.
	Path string `json:"path,omitempty"`
	// Result is the stored result, as arbitrary JSON.
	Result json.RawMessage `json:"result"`
}

// SessionRoot returns <workDir>/<sessionID> after validating the id. It is the
// only sanctioned way to build a session root from untrusted input.
func SessionRoot(workDir, sessionID string) (string, error) {
	if workDir == "" {
		return "", fmt.Errorf("work dir is empty: %w", ErrInvalidArgument)
	}
	if len(sessionID) == 0 || len(sessionID) > maxSessionIDLen || !sessionIDPattern.MatchString(sessionID) {
		return "", ErrInvalidSessionID
	}
	return filepath.Join(workDir, sessionID), nil
}

// IsProtectedName reports whether a single path segment is one of the names
// workspace operations never touch (.git, node_modules, dist, .orbit). The
// match is case-insensitive so it also holds on case-insensitive filesystems.
func IsProtectedName(name string) bool {
	for _, p := range protectedNames {
		if strings.EqualFold(name, p) {
			return true
		}
	}
	return false
}

func hasProtectedSegment(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if IsProtectedName(seg) {
			return true
		}
	}
	return false
}

// Workspace executes file operations inside one session root. Create exactly
// one Workspace per session (see [WorkspaceRegistry]): the mutex serializes
// operations of that instance, and two instances on the same root would not
// exclude each other. All methods are safe for concurrent use.
type Workspace struct {
	root   string // symlink-resolved, absolute
	limits WorkspaceLimits

	mu      sync.Mutex // serializes ops and guards ops/needsNL
	ops     map[string]OpRecord
	needsNL bool // log's last line lacks a trailing newline (torn write)
}

// NewWorkspace binds a Workspace to an existing session root directory and
// loads its idempotency log. The root itself must not be a symlink.
func NewWorkspace(root string, limits WorkspaceLimits) (*Workspace, error) {
	li, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrSessionNotFound
		}
		return nil, &OpError{Err: ErrIO, cause: err}
	}
	if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
		return nil, fmt.Errorf("session root is not a directory: %w", ErrNotDirectory)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, &OpError{Err: ErrIO, cause: err}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, &OpError{Err: ErrIO, cause: err}
	}
	w := &Workspace{
		root:   resolved,
		limits: limits.withDefaults(),
		ops:    make(map[string]OpRecord),
	}
	if err := w.loadOpLog(); err != nil {
		return nil, err
	}
	return w, nil
}

// WorkspaceRegistry hands out one Workspace per session id, which is the
// invariant Workspace relies on for its locking.
type WorkspaceRegistry struct {
	workDir string
	limits  WorkspaceLimits

	mu sync.Mutex
	m  map[string]*Workspace
}

// NewWorkspaceRegistry creates a registry rooted at workDir (e.g. /work).
func NewWorkspaceRegistry(workDir string, limits WorkspaceLimits) *WorkspaceRegistry {
	return &WorkspaceRegistry{workDir: workDir, limits: limits, m: make(map[string]*Workspace)}
}

// Get returns the Workspace of an existing session, creating it on first use.
// It returns ErrInvalidSessionID or ErrSessionNotFound as appropriate.
func (r *WorkspaceRegistry) Get(sessionID string) (*Workspace, error) {
	root, err := SessionRoot(r.workDir, sessionID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if w, ok := r.m[sessionID]; ok {
		// The directory may have been removed behind our back (volume loss).
		if _, err := os.Lstat(root); err == nil {
			return w, nil
		}
		delete(r.m, sessionID)
	}
	w, err := NewWorkspace(root, r.limits)
	if err != nil {
		return nil, err
	}
	r.m[sessionID] = w
	return w, nil
}

// Forget drops the cached Workspace of a session (after DELETE or re-init).
func (r *WorkspaceRegistry) Forget(sessionID string) {
	r.mu.Lock()
	delete(r.m, sessionID)
	r.mu.Unlock()
}

// resolvedPath is a validated location inside the session root.
type resolvedPath struct {
	abs  string      // symlink-free absolute path (may not exist yet)
	rel  string      // slash-separated path relative to root, "." for root
	info fs.FileInfo // nil when the path does not exist
}

// resolve validates an LLM-supplied relative path and maps it to a location
// inside the root. For writes it also refuses a symlink as the final element.
func (w *Workspace) resolve(p string, write bool) (resolvedPath, error) {
	if p == "" || strings.ContainsRune(p, 0) {
		return resolvedPath{}, opErr(ErrInvalidPath, "")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return resolvedPath{}, opErr(ErrPathOutside, p)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return resolvedPath{}, opErr(ErrPathOutside, p)
	}
	if hasProtectedSegment(clean) {
		return resolvedPath{}, opErr(ErrPathForbidden, p)
	}

	full := filepath.Join(w.root, clean)

	// Find the deepest ancestor that exists; the rest cannot be symlinks yet.
	existing := full
	var rest []string
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.ENOTDIR) {
			return resolvedPath{}, ioErr(p, err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return resolvedPath{}, ioErr(p, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return resolvedPath{}, opErr(ErrPathOutside, p)
		}
		rest = append([]string{filepath.Base(existing)}, rest...)
		existing = parent
	}

	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		// Dangling or looping symlink: it cannot be proven to stay inside.
		return resolvedPath{}, &OpError{Err: ErrPathOutside, Path: p, cause: err}
	}
	if !within(w.root, resolvedExisting) {
		return resolvedPath{}, opErr(ErrPathOutside, p)
	}

	final := resolvedExisting
	if len(rest) > 0 {
		final = filepath.Join(append([]string{resolvedExisting}, rest...)...)
	}
	rel, err := filepath.Rel(w.root, final)
	if err != nil {
		return resolvedPath{}, opErr(ErrPathOutside, p)
	}
	// A symlink inside the root may point at a protected directory.
	if hasProtectedSegment(rel) {
		return resolvedPath{}, opErr(ErrPathForbidden, p)
	}

	r := resolvedPath{abs: final, rel: filepath.ToSlash(rel)}
	if len(rest) == 0 {
		li, err := os.Lstat(existing)
		if err != nil {
			return resolvedPath{}, ioErr(p, err)
		}
		if write && li.Mode()&fs.ModeSymlink != 0 {
			return resolvedPath{}, opErr(ErrSymlink, p)
		}
		info, err := os.Stat(final)
		if err != nil {
			return resolvedPath{}, ioErr(p, err)
		}
		r.info = info
		return r, nil
	}
	// Something below an existing ancestor: that ancestor must be a directory.
	ai, err := os.Stat(resolvedExisting)
	if err != nil {
		return resolvedPath{}, ioErr(p, err)
	}
	if !ai.IsDir() {
		return resolvedPath{}, opErr(ErrNotDirectory, p)
	}
	return r, nil
}

func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

func isText(b []byte) bool {
	return bytes.IndexByte(b, 0) < 0 && utf8.Valid(b)
}

// ListDir lists a directory, one entry per line, directories suffixed "/".
func (w *Workspace) ListDir(path string) (OpResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.listDir(path)
}

func (w *Workspace) listDir(path string) (OpResult, error) {
	if path == "" {
		path = "."
	}
	r, err := w.resolve(path, false)
	if err != nil {
		return OpResult{}, err
	}
	if r.info == nil {
		return OpResult{}, opErr(ErrNotFound, path)
	}
	if !r.info.IsDir() {
		return OpResult{}, opErr(ErrNotDirectory, path)
	}
	entries, err := os.ReadDir(r.abs)
	if err != nil {
		return OpResult{}, ioErr(path, err)
	}
	var names []string
	for _, e := range entries { // ReadDir returns entries sorted by name
		if IsProtectedName(e.Name()) {
			continue
		}
		switch {
		case e.IsDir():
			names = append(names, e.Name()+"/")
		case e.Type().IsRegular():
			names = append(names, e.Name())
		}
		// Symlinks and special files are hidden: the agent never creates them.
	}
	if len(names) == 0 {
		return OpResult{OK: true, Output: "(empty directory)"}, nil
	}
	more := 0
	if len(names) > w.limits.MaxListEntries {
		more = len(names) - w.limits.MaxListEntries
		names = names[:w.limits.MaxListEntries]
	}
	out := strings.Join(names, "\n")
	if more > 0 {
		out += fmt.Sprintf("\n…truncated, %d more entries", more)
	}
	return OpResult{OK: true, Output: out}, nil
}

// ReadFile returns a text file with 1-based line numbers (cat -n style).
// offset is the first line (0 means 1); limit is the max number of lines
// (0 means the configured default). When output is cut short it ends with an
// explicit "…truncated, use offset N to continue" marker.
func (w *Workspace) ReadFile(path string, offset, limit int) (OpResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.readFile(path, offset, limit)
}

func (w *Workspace) readFile(path string, offset, limit int) (OpResult, error) {
	if offset < 0 || limit < 0 {
		return OpResult{}, &OpError{Err: ErrInvalidArgument, Msg: "offset and limit must not be negative"}
	}
	if offset == 0 {
		offset = 1
	}
	if limit == 0 || limit > w.limits.MaxReadLines {
		limit = w.limits.MaxReadLines
	}
	r, err := w.resolve(path, false)
	if err != nil {
		return OpResult{}, err
	}
	data, err := w.loadText(r, path, w.limits.MaxScanBytes)
	if err != nil {
		return OpResult{}, err
	}
	lines := splitLines(string(data))
	total := len(lines)
	if total == 0 {
		return OpResult{OK: true, Output: "(empty file)"}, nil
	}
	if offset > total {
		return OpResult{OK: true, Output: fmt.Sprintf("(offset %d is beyond the end of the file, which has %d lines)", offset, total)}, nil
	}

	var b strings.Builder
	next := offset // first line not yet emitted
	cutLine := 0
	for i := offset - 1; i < total && next-offset < limit; i++ {
		row := fmt.Sprintf("%6d\t%s\n", i+1, lines[i])
		if b.Len()+len(row) > w.limits.MaxReadBytes {
			if b.Len() == 0 {
				// A single line larger than the budget: show its head.
				room := w.limits.MaxReadBytes - 8
				if room < 0 {
					room = 0
				}
				head := truncateUTF8(lines[i], room)
				b.WriteString(fmt.Sprintf("%6d\t%s\n", i+1, head))
				cutLine = i + 1
				next = i + 2
			}
			break
		}
		b.WriteString(row)
		next = i + 2
	}
	switch {
	case next-1 < total:
		fmt.Fprintf(&b, "…truncated, use offset %d to continue", next)
		if cutLine > 0 {
			fmt.Fprintf(&b, " (line %d was cut)", cutLine)
		}
		b.WriteString("\n")
	case cutLine > 0:
		fmt.Fprintf(&b, "…truncated (line %d was cut, end of file)\n", cutLine)
	}
	return OpResult{OK: true, Output: b.String()}, nil
}

// loadText reads a regular text file of at most maxBytes into memory.
func (w *Workspace) loadText(r resolvedPath, path string, maxBytes int64) ([]byte, error) {
	if r.info == nil {
		return nil, opErr(ErrNotFound, path)
	}
	if r.info.IsDir() {
		return nil, opErr(ErrIsDirectory, path)
	}
	if !r.info.Mode().IsRegular() {
		return nil, opErr(ErrNotRegular, path)
	}
	if r.info.Size() > maxBytes {
		return nil, opErr(ErrFileTooLarge, path)
	}
	f, err := os.Open(r.abs)
	if err != nil {
		return nil, ioErr(path, err)
	}
	defer f.Close()
	// Bound the read again: the file may have grown since Stat.
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, ioErr(path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, opErr(ErrFileTooLarge, path)
	}
	if !isText(data) {
		return nil, opErr(ErrBinaryFile, path)
	}
	return data, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// truncateUTF8 cuts s to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// WriteFile creates or overwrites a text file atomically (temp file + rename),
// creating parent directories as needed. It does not consult the idempotency
// log; use [Workspace.Apply] for that.
func (w *Workspace) WriteFile(path, content string) (OpResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeFile(path, content)
}

func (w *Workspace) writeFile(path, content string) (OpResult, error) {
	data := []byte(content)
	if int64(len(data)) > w.limits.MaxFileBytes {
		return OpResult{}, opErr(ErrFileTooLarge, path)
	}
	if !isText(data) {
		return OpResult{}, opErr(ErrInvalidContent, path)
	}
	r, err := w.resolve(path, true)
	if err != nil {
		return OpResult{}, err
	}
	if r.info != nil {
		if r.info.IsDir() {
			return OpResult{}, opErr(ErrIsDirectory, path)
		}
		if !r.info.Mode().IsRegular() {
			return OpResult{}, opErr(ErrNotRegular, path)
		}
	}
	verb := "created"
	if r.info != nil {
		verb = "updated"
	}
	if err := w.commitFile(r, path, data, 0o644); err != nil {
		return OpResult{}, err
	}
	return OpResult{OK: true, Output: fmt.Sprintf("%s %s (%d bytes)", verb, r.rel, len(data))}, nil
}

// EditFile replaces oldStr with newStr. oldStr must occur exactly once unless
// replaceAll is set. The error wording is part of the tool contract the model
// relies on to self-correct.
func (w *Workspace) EditFile(path, oldStr, newStr string, replaceAll bool) (OpResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.editFile(path, oldStr, newStr, replaceAll)
}

func (w *Workspace) editFile(path, oldStr, newStr string, replaceAll bool) (OpResult, error) {
	if oldStr == "" {
		return OpResult{}, &OpError{Err: ErrEditEmptyOld}
	}
	if oldStr == newStr {
		return OpResult{}, &OpError{Err: ErrEditNoop}
	}
	if !isText([]byte(newStr)) {
		return OpResult{}, opErr(ErrInvalidContent, path)
	}
	r, err := w.resolve(path, true)
	if err != nil {
		return OpResult{}, err
	}
	data, err := w.loadText(r, path, w.limits.MaxFileBytes)
	if err != nil {
		return OpResult{}, err
	}
	content := string(data)
	n := strings.Count(content, oldStr)
	switch {
	case n == 0:
		return OpResult{}, &OpError{Err: ErrEditNotFound}
	case n > 1 && !replaceAll:
		return OpResult{}, &OpError{
			Err: ErrEditNotUnique,
			Msg: fmt.Sprintf("old_string is not unique (%d matches), add more context or set replace_all", n),
		}
	}
	limit := 1
	if replaceAll {
		limit = -1
	}
	updated := []byte(strings.Replace(content, oldStr, newStr, limit))
	if int64(len(updated)) > w.limits.MaxFileBytes {
		return OpResult{}, opErr(ErrFileTooLarge, path)
	}
	if err := w.commitFile(r, path, updated, r.info.Mode().Perm()); err != nil {
		return OpResult{}, err
	}
	replaced := 1
	if replaceAll {
		replaced = n
	}
	return OpResult{OK: true, Output: fmt.Sprintf("edited %s (%d replacement(s), %d bytes)", r.rel, replaced, len(updated))}, nil
}

// commitFile enforces the session cap and writes data to r atomically. The
// temp file lives in .orbit/tmp, on the same filesystem as the root, so the
// rename is atomic and a crash leaves no stray file that git could pick up.
func (w *Workspace) commitFile(r resolvedPath, path string, data []byte, mode fs.FileMode) error {
	used, err := w.sessionBytes()
	if err != nil {
		return ioErr(path, err)
	}
	var existing int64
	if r.info != nil {
		existing = r.info.Size()
	}
	if used-existing+int64(len(data)) > w.limits.MaxSessionBytes {
		return opErr(ErrSessionTooLarge, path)
	}

	// The parent was resolved inside the root, so creating missing
	// directories below it cannot escape.
	if err := os.MkdirAll(filepath.Dir(r.abs), 0o755); err != nil {
		return ioErr(path, err)
	}
	tmpDir, err := w.ensureTmpDir()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(tmpDir, "w-*")
	if err != nil {
		return ioErr(path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return ioErr(path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return ioErr(path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return ioErr(path, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return ioErr(path, err)
	}
	if err := os.Rename(tmpName, r.abs); err != nil {
		cleanup()
		return ioErr(path, err)
	}
	return nil
}

// sessionBytes sums the size of regular files under the root, skipping the
// protected directories and never following symlinks. It walks instead of
// tracking incrementally so the number stays correct after external changes
// (template init, restore, build output cleanup).
func (w *Workspace) sessionBytes() (int64, error) {
	var total int64
	err := filepath.WalkDir(w.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // vanished mid-walk
			}
			return err
		}
		if p != w.root && IsProtectedName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// ensureRealDir creates dir (0700) if missing and refuses a symlink or a
// non-directory in its place.
func ensureRealDir(dir string) error {
	li, err := os.Lstat(dir)
	switch {
	case err == nil:
		if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
			return fmt.Errorf("%s is not a plain directory: %w", filepath.Base(dir), ErrOpLog)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return &OpError{Err: ErrIO, cause: err}
		}
		return nil
	default:
		return &OpError{Err: ErrIO, cause: err}
	}
}

func (w *Workspace) orbitDir() string { return filepath.Join(w.root, orbitDirName) }

func (w *Workspace) ensureTmpDir() (string, error) {
	if err := ensureRealDir(w.orbitDir()); err != nil {
		return "", err
	}
	tmp := filepath.Join(w.orbitDir(), tmpDirName)
	if err := ensureRealDir(tmp); err != nil {
		return "", err
	}
	return tmp, nil
}

// Apply executes one file operation. write_file and edit_file require a
// non-empty opID and are idempotent: a repeated opID returns the stored result
// without touching the filesystem (Replayed is set), even if the file changed
// in between. Reusing an opID for a different kind or path is rejected with
// ErrOpIDReused. Only successful operations are recorded, so a failed
// operation can be retried. list_dir and read_file ignore opID.
//
// If the operation succeeded but the log append failed, Apply returns the
// result together with an error wrapping ErrOpLog.
func (w *Workspace) Apply(opID string, op FileOp) (OpResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch op.Kind {
	case FileOpListDir:
		return w.listDir(op.Path)
	case FileOpReadFile:
		return w.readFile(op.Path, op.Offset, op.Limit)
	case FileOpWriteFile, FileOpEditFile:
	default:
		return OpResult{}, &OpError{Err: ErrUnknownOp, Msg: fmt.Sprintf("unknown operation kind %q", truncateUTF8(op.Kind, 64))}
	}

	if err := validOpID(opID); err != nil {
		return OpResult{}, err
	}
	if rec, ok := w.ops[opID]; ok {
		if rec.Kind != op.Kind || (rec.Path != "" && rec.Path != op.Path) {
			return OpResult{}, opErr(ErrOpIDReused, "")
		}
		var res OpResult
		if err := json.Unmarshal(rec.Result, &res); err != nil {
			return OpResult{}, &OpError{Err: ErrOpLog, cause: err}
		}
		res.Replayed = true
		return res, nil
	}

	var (
		res OpResult
		err error
	)
	if op.Kind == FileOpWriteFile {
		res, err = w.writeFile(op.Path, op.Content)
	} else {
		res, err = w.editFile(op.Path, op.OldString, op.NewString, op.ReplaceAll)
	}
	if err != nil {
		return OpResult{}, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return res, &OpError{Err: ErrOpLog, cause: err}
	}
	if err := w.appendOp(OpRecord{OpID: opID, Kind: op.Kind, Path: op.Path, Result: raw}); err != nil {
		return res, err
	}
	return res, nil
}

func validOpID(id string) error {
	if id == "" || len(id) > maxOpIDLen {
		return &OpError{Err: ErrInvalidArgument, Msg: "op_id must be 1-128 characters"}
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return &OpError{Err: ErrInvalidArgument, Msg: "op_id must not contain control characters"}
		}
	}
	return nil
}

// LookupOp returns the stored record for opID. The build step uses it (with
// [Workspace.RecordOp]) to get the same replay semantics for non-file
// operations. Callers that must not run the same opID twice concurrently
// need their own serialization between LookupOp and RecordOp.
func (w *Workspace) LookupOp(opID string) (OpRecord, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec, ok := w.ops[opID]
	return rec, ok
}

// RecordOp appends opID -> result (any JSON-marshalable value) to the log.
// The first record for an opID wins; recording an existing opID returns
// ErrOpIDReused and leaves the log untouched.
func (w *Workspace) RecordOp(opID, kind string, result any) error {
	if err := validOpID(opID); err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return &OpError{Err: ErrOpLog, cause: err}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.ops[opID]; ok {
		return opErr(ErrOpIDReused, "")
	}
	return w.appendOp(OpRecord{OpID: opID, Kind: kind, Result: raw})
}

// appendOp persists rec and then publishes it in memory. The caller holds w.mu.
func (w *Workspace) appendOp(rec OpRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return &OpError{Err: ErrOpLog, cause: err}
	}
	if err := ensureRealDir(w.orbitDir()); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(w.orbitDir(), opsLogName), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return &OpError{Err: ErrOpLog, cause: err}
	}
	defer f.Close()
	buf := make([]byte, 0, len(line)+2)
	if w.needsNL {
		// A crash tore the previous line; start a fresh one so this record
		// is not glued to the garbage.
		buf = append(buf, '\n')
	}
	buf = append(append(buf, line...), '\n')
	// One Write call keeps the record contiguous under O_APPEND.
	if _, err := f.Write(buf); err != nil {
		return &OpError{Err: ErrOpLog, cause: err}
	}
	if err := f.Sync(); err != nil {
		return &OpError{Err: ErrOpLog, cause: err}
	}
	w.needsNL = false
	w.ops[rec.OpID] = rec
	return nil
}

// loadOpLog reads .orbit/ops.jsonl into memory. Lines that do not parse (a
// torn final write after a crash) are skipped; the first record of an op_id wins.
func (w *Workspace) loadOpLog() error {
	dir := w.orbitDir()
	if li, err := os.Lstat(dir); err == nil {
		if li.Mode()&fs.ModeSymlink != 0 || !li.IsDir() {
			return &OpError{Err: ErrOpLog, cause: errors.New(".orbit is not a plain directory")}
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else {
		return &OpError{Err: ErrOpLog, cause: err}
	}
	f, err := os.Open(filepath.Join(dir, opsLogName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return &OpError{Err: ErrOpLog, cause: err}
	}
	defer f.Close()

	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			complete := line[len(line)-1] == '\n'
			var rec OpRecord
			if json.Unmarshal(bytes.TrimSpace(line), &rec) == nil && rec.OpID != "" {
				if _, dup := w.ops[rec.OpID]; !dup {
					w.ops[rec.OpID] = rec
				}
			}
			w.needsNL = !complete
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return &OpError{Err: ErrOpLog, cause: err}
		}
	}
}

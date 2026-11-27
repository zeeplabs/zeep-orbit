package builder

// overlay.go: the backend overlay (builder-no-backend NB-08, T11d). A session
// created with no backend runs on the "site" template; attaching a backend
// later adds the data layer of the default template to the workspace without
// touching any file the user or the agent wrote. The overlay lives in the
// builder image (templates/backend-overlay), so the SDK version has one home.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// OverlayBackendName is the only overlay today.
	OverlayBackendName = "backend-overlay"
	// overlayManifest is the overlay's description, next to its files.
	overlayManifest = "overlay.json"
	// OverlayRouteName is the name clients send in OverlayRequest.Name.
	OverlayRouteName = "backend"
)

// ErrOverlayConflict means the overlay cannot be applied without overwriting or
// contradicting something in the workspace. Nothing was written.
var ErrOverlayConflict = errors.New("overlay conflict")

// ErrOverlayInvalid means the overlay itself (its manifest or the workspace's
// package.json) cannot be used.
var ErrOverlayInvalid = errors.New("overlay cannot be applied")

// overlayManifestDoc is overlay.json.
type overlayManifestDoc struct {
	Version int `json:"version"`
	// Files are copied from the overlay directory to the same path in the
	// workspace. They must not exist there.
	Files []string `json:"files"`
	// Dynamic are files the caller sends (rendered per session). They must not
	// exist in the workspace either.
	Dynamic []string `json:"dynamic"`
	// Replace are template-owned guidance files, overwritten when present.
	Replace []string `json:"replace"`
	// Dependencies are added under "dependencies" of package.json.
	Dependencies map[string]string `json:"dependencies"`
}

// OverlayResult is what ApplyOverlay did.
type OverlayResult struct {
	Written      []string `json:"written"`
	Replaced     []string `json:"replaced"`
	Dependencies []string `json:"dependencies"`
	// Replayed is true when the opID was already applied (nothing was written).
	Replayed bool `json:"replayed"`
}

// loadOverlay reads the manifest and the static files of the overlay directory.
// It never follows a symlink out of dir.
func loadOverlay(dir string) (overlayManifestDoc, map[string]string, error) {
	var m overlayManifestDoc
	rt, err := os.OpenRoot(dir)
	if err != nil {
		return m, nil, &OpError{Err: ErrOverlayInvalid, Msg: "overlay is not available"}
	}
	defer rt.Close()
	raw, err := fs.ReadFile(rt.FS(), overlayManifest)
	if err != nil || json.Unmarshal(raw, &m) != nil || m.Version != 1 {
		return m, nil, &OpError{Err: ErrOverlayInvalid, Msg: "overlay manifest is not valid"}
	}
	files := map[string]string{}
	for _, p := range append(append([]string{}, m.Files...), m.Replace...) {
		if !safeOverlayPath(p) {
			return m, nil, &OpError{Err: ErrOverlayInvalid, Msg: "overlay manifest has an unsafe path"}
		}
		b, err := fs.ReadFile(rt.FS(), p)
		if err != nil {
			return m, nil, &OpError{Err: ErrOverlayInvalid, Msg: "overlay file is missing"}
		}
		files[p] = string(b)
	}
	for _, p := range m.Dynamic {
		if !safeOverlayPath(p) {
			return m, nil, &OpError{Err: ErrOverlayInvalid, Msg: "overlay manifest has an unsafe path"}
		}
	}
	return m, files, nil
}

func safeOverlayPath(p string) bool {
	c := filepath.ToSlash(filepath.Clean(p))
	return p != "" && c == p && !strings.HasPrefix(c, "/") && c != ".." && !strings.HasPrefix(c, "../") && !hasProtectedSegment(c)
}

// ApplyOverlay applies the overlay under the workspace lock: every conflict is
// found before the first write, so a refused overlay leaves the tree as it was.
// dynamic carries the per-session files (exactly the manifest's Dynamic set).
// A repeated opID returns the stored result and writes nothing.
func (w *Workspace) ApplyOverlay(opID, overlayDir string, dynamic map[string]string) (OverlayResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := validOpID(opID); err != nil {
		return OverlayResult{}, err
	}
	if rec, ok := w.ops[opID]; ok {
		if rec.Kind != "overlay" {
			return OverlayResult{}, opErr(ErrOpIDReused, "")
		}
		var res OverlayResult
		if err := json.Unmarshal(rec.Result, &res); err != nil {
			return OverlayResult{}, &OpError{Err: ErrOpLog, cause: err}
		}
		res.Replayed = true
		return res, nil
	}

	m, static, err := loadOverlay(overlayDir)
	if err != nil {
		return OverlayResult{}, err
	}
	if len(dynamic) != len(m.Dynamic) {
		return OverlayResult{}, &OpError{Err: ErrInvalidArgument, Msg: "files must be exactly the overlay's dynamic files"}
	}
	for _, p := range m.Dynamic {
		if _, ok := dynamic[p]; !ok {
			return OverlayResult{}, &OpError{Err: ErrInvalidArgument, Msg: "files must be exactly the overlay's dynamic files"}
		}
	}

	// 1. Conflicts: a file that must be new exists, or package.json disagrees.
	var conflicts []string
	toCreate := append(append([]string{}, m.Files...), m.Dynamic...)
	for _, p := range toCreate {
		r, err := w.resolve(p, true)
		if err != nil {
			return OverlayResult{}, err
		}
		if r.info != nil {
			conflicts = append(conflicts, p)
		}
	}
	pkgPath := "package.json"
	pkgRes, err := w.resolve(pkgPath, true)
	if err != nil {
		return OverlayResult{}, err
	}
	if pkgRes.info == nil {
		return OverlayResult{}, &OpError{Err: ErrOverlayInvalid, Msg: "package.json is missing"}
	}
	pkgRaw, err := w.loadText(pkgRes, pkgPath, w.limits.MaxFileBytes)
	if err != nil {
		return OverlayResult{}, err
	}
	pkgNew, added, depConflicts, err := mergeDependencies(pkgRaw, m.Dependencies)
	if err != nil {
		return OverlayResult{}, err
	}
	conflicts = append(conflicts, depConflicts...)
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return OverlayResult{}, &OpError{Err: ErrOverlayConflict, Msg: "overlay conflict: " + strings.Join(conflicts, ", ")}
	}

	// 2. Writes. Replace files first only if they exist (otherwise they are new).
	res := OverlayResult{Written: []string{}, Replaced: []string{}, Dependencies: added}
	for _, p := range m.Files {
		if _, err := w.writeFile(p, static[p]); err != nil {
			return OverlayResult{}, err
		}
		res.Written = append(res.Written, p)
	}
	for _, p := range m.Dynamic {
		if _, err := w.writeFile(p, dynamic[p]); err != nil {
			return OverlayResult{}, err
		}
		res.Written = append(res.Written, p)
	}
	for _, p := range m.Replace {
		r, err := w.resolve(p, true)
		if err != nil {
			return OverlayResult{}, err
		}
		existed := r.info != nil
		if _, err := w.writeFile(p, static[p]); err != nil {
			return OverlayResult{}, err
		}
		if existed {
			res.Replaced = append(res.Replaced, p)
		} else {
			res.Written = append(res.Written, p)
		}
	}
	if len(added) > 0 {
		if _, err := w.writeFile(pkgPath, string(pkgNew)); err != nil {
			return OverlayResult{}, err
		}
		res.Written = append(res.Written, pkgPath)
	}

	raw, err := json.Marshal(res)
	if err != nil {
		return res, &OpError{Err: ErrOpLog, cause: err}
	}
	if err := w.appendOp(OpRecord{OpID: opID, Kind: "overlay", Result: raw}); err != nil {
		return res, err
	}
	return res, nil
}

// kv is one member of a JSON object, in source order.
type kv struct {
	key string
	val json.RawMessage
}

func decodeOrdered(raw []byte) ([]kv, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	var out []kv
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok {
			return nil, errors.New("bad key")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, kv{key, v})
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("unterminated object")
	}
	return out, nil
}

func encodeOrdered(members []kv) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// mergeDependencies adds deps under "dependencies" of a package.json, keeping
// the order of every other key and sorting only the new names into place (npm
// keeps dependencies alphabetical). A name already present with the same
// version is left alone; with another version it is a conflict, reported as
// "package.json (name)". added lists the names that were inserted.
func mergeDependencies(pkg []byte, deps map[string]string) (out []byte, added, conflicts []string, err error) {
	invalid := &OpError{Err: ErrOverlayInvalid, Msg: "package.json is not valid"}
	top, derr := decodeOrdered(pkg)
	if derr != nil {
		return nil, nil, nil, invalid
	}
	idx := -1
	for i, m := range top {
		if m.key == "dependencies" {
			idx = i
		}
	}
	var current []kv
	if idx >= 0 {
		if current, derr = decodeOrdered(top[idx].val); derr != nil {
			return nil, nil, nil, invalid
		}
	}
	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		want, _ := json.Marshal(deps[n])
		found := false
		for _, c := range current {
			if c.key != n {
				continue
			}
			found = true
			var have string
			if json.Unmarshal(c.val, &have) != nil || have != deps[n] {
				conflicts = append(conflicts, fmt.Sprintf("package.json (%s)", n))
			}
		}
		if found {
			continue
		}
		pos := len(current)
		for i, c := range current {
			if c.key > n {
				pos = i
				break
			}
		}
		current = append(current, kv{})
		copy(current[pos+1:], current[pos:])
		current[pos] = kv{n, want}
		added = append(added, n)
	}
	if len(conflicts) > 0 || len(added) == 0 {
		return pkg, added, conflicts, nil
	}
	depsRaw := encodeOrdered(current)
	if idx >= 0 {
		top[idx].val = depsRaw
	} else {
		top = append(top, kv{"dependencies", depsRaw})
	}
	var ind bytes.Buffer
	if err := json.Indent(&ind, encodeOrdered(top), "", "  "); err != nil {
		return nil, nil, nil, invalid
	}
	ind.WriteByte('\n')
	return ind.Bytes(), added, nil, nil
}

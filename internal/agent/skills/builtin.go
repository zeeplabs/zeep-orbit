package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

// MaxBuiltinIndexChars is the part of the index budget reserved for
// built-ins. CI enforces it, so shipping a new built-in can never push a
// company's already-enabled custom skills out of the agent's view.
const MaxBuiltinIndexChars = 2500

// ReservedPrefix is the name prefix owned by built-in skills. Custom uploads
// can never use it, so a custom skill cannot shadow or impersonate a built-in.
const ReservedPrefix = "orbit-"

// builtinFS embeds the built-in skills as builtin/<name>/SKILL.md, plus
// NOTICE.md (attributions) and builtin.lock (version and hash per skill).
// Because they live in the binary, built-ins are immutable by construction.
//
//go:embed builtin
var builtinFS embed.FS

// Skill is a loaded, validated skill.
type Skill struct {
	Parsed
	Origin Origin
	// SHA256 is the hex digest of the raw SKILL.md, recorded in builtin.lock.
	SHA256 string
}

// Entry is the index row for the skill.
func (s Skill) Entry(enabled bool) Entry {
	return Entry{Name: s.Name, Description: s.Description, Source: s.Origin, Version: s.Version, Enabled: enabled}
}

var (
	builtinOnce sync.Once
	builtinList []Skill
	builtinSet  map[string]Skill
)

// Builtins returns the embedded built-in skills sorted by name. They are
// parsed once. A malformed embedded skill is a build-time mistake that the
// lint test catches, so it panics here instead of being silently skipped.
func Builtins() []Skill {
	builtinOnce.Do(func() {
		sub, err := fs.Sub(builtinFS, "builtin")
		if err != nil {
			panic(fmt.Sprintf("skills: embedded builtin dir: %v", err))
		}
		list, err := loadBuiltins(sub)
		if err != nil {
			panic(fmt.Sprintf("skills: invalid built-in skill: %v", err))
		}
		builtinList = list
		builtinSet = make(map[string]Skill, len(list))
		for _, s := range list {
			builtinSet[s.Name] = s
		}
	})
	out := make([]Skill, len(builtinList))
	copy(out, builtinList)
	return out
}

// GetBuiltin returns the built-in with the given name.
func GetBuiltin(name string) (Skill, bool) {
	Builtins()
	s, ok := builtinSet[name]
	return s, ok
}

// IsBuiltinName reports whether name is an embedded built-in.
func IsBuiltinName(name string) bool {
	_, ok := GetBuiltin(name)
	return ok
}

// HasReservedPrefix reports whether name uses the built-in-only prefix.
func HasReservedPrefix(name string) bool {
	return strings.HasPrefix(name, ReservedPrefix)
}

// loadBuiltins reads <name>/SKILL.md for every directory in fsys. Files at
// the root (NOTICE.md, builtin.lock) are not skills.
func loadBuiltins(fsys fs.FS) ([]Skill, error) {
	dirs, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(fsys, d.Name()+"/SKILL.md")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Name(), err)
		}
		p, _, err := ParseBuiltin(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Name(), err)
		}
		sum := sha256.Sum256(raw)
		out = append(out, Skill{Parsed: p, Origin: OriginBuiltin, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// registeredToolNames is the single list of non-prefixed tool names a skill
// may reference. The dashboard test that builds the real registry asserts
// every registered name passes KnownToolName, so this cannot drift silently.
var registeredToolNames = map[string]bool{
	tools.ToolListDir:        true,
	tools.ToolReadFile:       true,
	tools.ToolWriteFile:      true,
	tools.ToolEditFile:       true,
	tools.ToolRunBuild:       true,
	tools.ToolGetBuildLogs:   true,
	"propose_backend_change": true,
	ToolLoadSkill:            true,
}

// KnownToolName reports whether name is a tool the builder registers:
// workspace tools, orbit_* read tools, propose_backend_change or load_skill.
func KnownToolName(name string) bool {
	return registeredToolNames[name] || strings.HasPrefix(name, "orbit_")
}

// Package webhookengine contains the pure, dependency-free logic that turns
// a raw webhook payload plus a saved field mapping into a write-ready
// map[string]any. No I/O, no DB, no HTTP — see design.md's MappingEngine
// component: kept unit-testable without any fixture beyond a plain Go map.
package webhookengine

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldMapping is one field→column link: SourcePath is a dot-notation path
// into the payload (e.g. "user.id", "items.0.email"); Column is the target
// table column it resolves into.
type FieldMapping struct {
	SourcePath string
	Column     string
}

// ExtractPath resolves a dot-notation path against payload. A numeric
// segment indexes into a JSON array (payload's []any, since encoding/json
// decodes arrays that way); any other segment looks up a map key. Returns
// found=false (no error) when the path doesn't resolve — a missing field is
// not itself an error, since callers (event-type resolution, capture-mode
// display) frequently need to distinguish "absent" from "present but empty".
func ExtractPath(payload map[string]any, path string) (value any, found bool) {
	if path == "" {
		return nil, false
	}

	segments := strings.Split(path, ".")
	var current any = payload

	for _, segment := range segments {
		switch node := current.(type) {
		case map[string]any:
			v, ok := node[segment]
			if !ok {
				return nil, false
			}
			current = v
		case []any:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			current = node[idx]
		default:
			return nil, false
		}
	}

	return current, true
}

// ResolveFields applies every mapping's SourcePath → Column link against
// payload, returning a map ready to hand to query.BuildInsert/BuildUpdate.
// Returns an error naming the missing source path the first time a mapping
// can't be resolved — applying a saved mapping is all-or-nothing, no
// partial write (spec P2 AC6 in spirit: a failed mapping application must
// not produce a partially-populated row).
func ResolveFields(payload map[string]any, mappings []FieldMapping) (map[string]any, error) {
	result := make(map[string]any, len(mappings))
	for _, m := range mappings {
		value, found := ExtractPath(payload, m.SourcePath)
		if !found {
			return nil, fmt.Errorf("webhookengine: source path %q not found in payload", m.SourcePath)
		}
		result[m.Column] = value
	}
	return result, nil
}

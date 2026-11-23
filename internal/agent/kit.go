package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// UI kit declaration (builder-design-quality spec, DQ-10..DQ-12). A template
// declares its component kit in orbit-template.json under "ui_kit". The agent
// never gets that text: ParseUIKit validates every field against a strict
// pattern and PromptBlock writes a fixed sentence around the values, so a
// template cannot use the declaration to inject instructions.

// MaxKitManifestBytes bounds the orbit-template.json that is read for a kit.
const MaxKitManifestBytes = 16 << 10

// UIKit is a validated ui_kit declaration.
type UIKit struct {
	Name       string // display name, for example "orbit-ui"
	Components string // directory with the components, relative, no trailing slash
	Tokens     string // file that holds the design tokens
	Notes      string // optional file that documents the kit; "" when absent
}

var (
	kitNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,39}$`)
	kitPathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

const maxKitPathChars = 120

// ParseUIKit reads orbit-template.json and returns its ui_kit, or (nil, nil)
// when the manifest declares none. An invalid declaration (unknown key, a
// missing required field, a path that is absolute, climbs with ".." or has
// unusual characters) returns an error and no kit: the caller logs it and the
// turn goes on without a kit block.
func ParseUIKit(manifest []byte) (*UIKit, error) {
	if len(manifest) > MaxKitManifestBytes {
		return nil, fmt.Errorf("orbit-template.json is larger than %d bytes", MaxKitManifestBytes)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &top); err != nil {
		return nil, errors.New("orbit-template.json is not a JSON object")
	}
	raw, ok := top["ui_kit"]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var in struct {
		Name       string `json:"name"`
		Components string `json:"components"`
		Tokens     string `json:"tokens"`
		Notes      string `json:"notes"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("ui_kit: %v", err)
	}
	if !kitNameRe.MatchString(in.Name) {
		return nil, errors.New("ui_kit.name: 1 to 40 letters, digits, spaces, dots, dashes or underscores")
	}
	kit := &UIKit{Name: in.Name}
	var err error
	if kit.Components, err = cleanKitPath("ui_kit.components", in.Components, true); err != nil {
		return nil, err
	}
	if kit.Tokens, err = cleanKitPath("ui_kit.tokens", in.Tokens, true); err != nil {
		return nil, err
	}
	if kit.Notes, err = cleanKitPath("ui_kit.notes", in.Notes, false); err != nil {
		return nil, err
	}
	return kit, nil
}

func cleanKitPath(field, p string, required bool) (string, error) {
	p = strings.TrimSuffix(strings.TrimSpace(p), "/")
	if p == "" {
		if required {
			return "", fmt.Errorf("%s: required", field)
		}
		return "", nil
	}
	if len(p) > maxKitPathChars || !kitPathRe.MatchString(p) {
		return "", fmt.Errorf("%s: a relative path of letters, digits, dots, dashes and underscores", field)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("%s: must not contain . or .. segments", field)
		}
	}
	return p, nil
}

// PromptBlock is the fixed text that tells the agent about the kit. Only the
// validated values are interpolated.
func (k *UIKit) PromptBlock() string {
	var b strings.Builder
	fmt.Fprintf(&b, "UI KIT: this template ships the %q component kit. Import its components from `%s` and create a new component only when the kit has none. "+
		"Keep the look in the design tokens in `%s`; do not hardcode colors, fonts or radii in components.", k.Name, k.Components, k.Tokens)
	if k.Notes != "" {
		fmt.Fprintf(&b, " Read `%s` with `read_file` for the components' props before using them.", k.Notes)
	}
	return b.String()
}

package builder

// Checks on the built-in templates in builder/templates: the "default" template
// (a project with a backend) and the "site" template (no backend) share the
// orbit-ui kit as two checked-in copies, and this test fails when they drift.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func templatesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	// internal/builder/templates_test.go -> repo root -> builder/templates
	return filepath.Join(filepath.Dir(file), "..", "..", "builder", "templates")
}

func readTemplateFile(t *testing.T, root, name, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, name, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s/%s: %v", name, rel, err)
	}
	return b
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestTemplates_KitCopiesAreIdentical(t *testing.T) {
	root := templatesRoot(t)
	uiDefault := listFiles(t, filepath.Join(root, "default", "src", "components", "ui"))
	uiSite := listFiles(t, filepath.Join(root, "site", "src", "components", "ui"))
	if strings.Join(uiDefault, ",") != strings.Join(uiSite, ",") {
		t.Fatalf("kit files differ: default %v, site %v", uiDefault, uiSite)
	}
	if len(uiDefault) == 0 {
		t.Fatal("no kit files found")
	}
	shared := []string{"src/lib/cn.ts", "UI_KIT.md"}
	for _, f := range uiDefault {
		shared = append(shared, "src/components/ui/"+f)
	}
	for _, rel := range shared {
		if !bytes.Equal(readTemplateFile(t, root, "default", rel), readTemplateFile(t, root, "site", rel)) {
			t.Errorf("%s differs between the default and site templates: copy the change to both", rel)
		}
	}
}

// The kit reads CSS variables, so both token files must define the same names
// (their values and the rest of the file may differ).
func TestTemplates_TokenNamesMatch(t *testing.T) {
	root := templatesRoot(t)
	re := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:`)
	names := func(name string) string {
		set := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(string(readTemplateFile(t, root, name, "src/index.css")), -1) {
			set[m[1]] = true
		}
		var out []string
		for n := range set {
			out = append(out, n)
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	if d, s := names("default"), names("site"); d != s || d == "" {
		t.Errorf("token names differ:\n default: %s\n site:    %s", d, s)
	}
}

func TestTemplates_SiteHasNoBackendLayer(t *testing.T) {
	root := templatesRoot(t)
	for _, rel := range []string{"src/orbit.config.ts", "src/lib/orbit.ts", "src/components/BackendStatus.tsx"} {
		if _, err := os.Stat(filepath.Join(root, "site", filepath.FromSlash(rel))); err == nil {
			t.Errorf("site template must not contain %s", rel)
		}
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(readTemplateFile(t, root, "site", "package.json"), &pkg); err != nil {
		t.Fatal(err)
	}
	for dep := range pkg.Dependencies {
		if strings.HasPrefix(dep, "@zeeptech/") {
			t.Errorf("site template must not depend on %s", dep)
		}
	}
	for _, name := range []string{"default", "site"} {
		if v := strings.TrimSpace(string(readTemplateFile(t, root, name, "TEMPLATE_VERSION"))); v == "" {
			t.Errorf("%s: TEMPLATE_VERSION is empty", name)
		}
		var m struct {
			UIKit struct {
				Name string `json:"name"`
			} `json:"ui_kit"`
			Publish string `json:"publish_path"`
		}
		if err := json.Unmarshal(readTemplateFile(t, root, name, "orbit-template.json"), &m); err != nil {
			t.Fatal(err)
		}
		if m.UIKit.Name != "orbit-ui" || m.Publish != "dist" {
			t.Errorf("%s: orbit-template.json = %+v", name, m)
		}
	}
}

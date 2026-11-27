package builder

// overlay_test.go: the backend overlay (builder-no-backend NB-08, T11d).

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sitePackageJSON = `{
  "name": "my-site",
  "private": true,
  "scripts": {
    "build": "tsc -b && vite build"
  },
  "dependencies": {
    "@base-ui/react": "1.9.0",
    "react": "19.3.1",
    "react-dom": "19.3.1"
  },
  "devDependencies": {
    "vite": "7.0.0"
  }
}
`

const orbitConfigFile = "export const ORBIT_API_URL = 'https://x.example'\nexport const ORBIT_APP_NAME = 'shop_ab12'\n"

func realOverlayDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(templatesRoot(t), OverlayBackendName)
}

// siteWorkspace is a workspace the way a "site" session leaves it: the site
// template's guide, a package.json and some files the agent wrote.
func siteWorkspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	w, root := newTestWorkspace(t, WorkspaceLimits{})
	mustWriteFile(t, filepath.Join(root, "package.json"), sitePackageJSON)
	mustWriteFile(t, filepath.Join(root, "ORBIT_AGENT.md"), "# site guide\n")
	mustWriteFile(t, filepath.Join(root, "src", "App.tsx"), "export default function App() { return null }\n")
	return w, root
}

var dyn = map[string]string{"src/orbit.config.ts": orbitConfigFile}

func TestApplyOverlay_AddsTheDataLayerAndNothingElse(t *testing.T) {
	w, root := siteWorkspace(t)
	res, err := w.ApplyOverlay("attach-1", realOverlayDir(t), dyn)
	if err != nil {
		t.Fatalf("ApplyOverlay: %v", err)
	}
	for _, p := range []string{"src/lib/orbit.ts", "src/lib/storage.ts", "src/orbit.config.ts"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("%s not written: %v", p, err)
		}
	}
	if got := mustReadFile(t, filepath.Join(root, "src/orbit.config.ts")); got != orbitConfigFile {
		t.Errorf("config = %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "ORBIT_AGENT.md")); got == "# site guide\n" {
		t.Error("the guide was not replaced")
	}
	if got := mustReadFile(t, filepath.Join(root, "src/App.tsx")); got != "export default function App() { return null }\n" {
		t.Errorf("a file the agent wrote changed: %q", got)
	}
	if !reflect.DeepEqual(res.Replaced, []string{"ORBIT_AGENT.md"}) {
		t.Errorf("Replaced = %v", res.Replaced)
	}
	if !reflect.DeepEqual(res.Dependencies, []string{"@zeeptech/orbit-client", "react-router-dom"}) {
		t.Errorf("Dependencies = %v", res.Dependencies)
	}
	if res.Replayed {
		t.Error("first call must not be a replay")
	}
}

func TestApplyOverlay_PackageJSONKeepsOrderAndSortsNewDependencies(t *testing.T) {
	w, root := siteWorkspace(t)
	if _, err := w.ApplyOverlay("attach-1", realOverlayDir(t), dyn); err != nil {
		t.Fatal(err)
	}
	raw := mustReadFile(t, filepath.Join(root, "package.json"))
	if !strings.HasSuffix(raw, "}\n") {
		t.Error("package.json must end with a newline")
	}
	// Top-level key order is the source order.
	var order []string
	for _, k := range []string{`"name"`, `"private"`, `"scripts"`, `"dependencies"`, `"devDependencies"`} {
		order = append(order, k)
	}
	last := -1
	for _, k := range order {
		i := strings.Index(raw, k)
		if i <= last {
			t.Fatalf("key %s out of order in:\n%s", k, raw)
		}
		last = i
	}
	// Dependencies stay alphabetical with the new names in place.
	var doc struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("package.json is not valid JSON: %v", err)
	}
	if doc.Dependencies["@zeeptech/orbit-client"] == "" || doc.Dependencies["react-router-dom"] == "" || doc.Dependencies["react"] != "19.3.1" {
		t.Errorf("dependencies = %v", doc.Dependencies)
	}
	iBase, iSDK, iReact, iRouter := strings.Index(raw, "@base-ui/react"), strings.Index(raw, "@zeeptech/orbit-client"), strings.Index(raw, `"react":`), strings.Index(raw, "react-router-dom")
	if !(iBase < iSDK && iSDK < iReact && iReact < iRouter) {
		t.Errorf("dependencies are not alphabetical:\n%s", raw)
	}
}

func TestApplyOverlay_RefusesBeforeWritingAnything(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, root string)
		want  string
	}{
		"the agent already wrote src/lib/orbit.ts": {
			func(t *testing.T, root string) { mustWriteFile(t, filepath.Join(root, "src/lib/orbit.ts"), "mine\n") },
			"src/lib/orbit.ts",
		},
		"src/orbit.config.ts exists": {
			func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, "src/orbit.config.ts"), "mine\n")
			},
			"src/orbit.config.ts",
		},
		"the SDK is there with another version": {
			func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, "package.json"), strings.Replace(sitePackageJSON, `"react": "19.3.1",`, `"react": "19.3.1",`+"\n"+`    "@zeeptech/orbit-client": "9.9.9",`, 1))
			},
			"package.json (@zeeptech/orbit-client)",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w, root := siteWorkspace(t)
			tc.setup(t, root)
			before := overlayTree(t, root)
			_, err := w.ApplyOverlay("attach-1", realOverlayDir(t), dyn)
			if !errors.Is(err, ErrOverlayConflict) {
				t.Fatalf("want ErrOverlayConflict, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q", err, tc.want)
			}
			if after := overlayTree(t, root); !reflect.DeepEqual(before, after) {
				t.Errorf("a refused overlay changed the tree:\nbefore %v\nafter  %v", before, after)
			}
			// The opID was not consumed by the refusal: after the conflict is gone it applies.
		})
	}
}

func TestApplyOverlay_ListsEveryConflictAtOnce(t *testing.T) {
	w, root := siteWorkspace(t)
	mustWriteFile(t, filepath.Join(root, "src/lib/orbit.ts"), "a\n")
	mustWriteFile(t, filepath.Join(root, "src/lib/storage.ts"), "b\n")
	_, err := w.ApplyOverlay("attach-1", realOverlayDir(t), dyn)
	if !errors.Is(err, ErrOverlayConflict) || !strings.Contains(err.Error(), "src/lib/orbit.ts") || !strings.Contains(err.Error(), "src/lib/storage.ts") {
		t.Fatalf("want both paths in the conflict, got %v", err)
	}
}

func TestApplyOverlay_ReplayWritesNothingAndSurvivesAnAgentEdit(t *testing.T) {
	w, root := siteWorkspace(t)
	first, err := w.ApplyOverlay("attach-1", realOverlayDir(t), dyn)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "src/lib/orbit.ts"), "edited by the agent\n")
	again, err := w.ApplyOverlay("attach-1", realOverlayDir(t), dyn)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !again.Replayed || !reflect.DeepEqual(again.Written, first.Written) {
		t.Errorf("replay = %+v, first = %+v", again, first)
	}
	if got := mustReadFile(t, filepath.Join(root, "src/lib/orbit.ts")); got != "edited by the agent\n" {
		t.Errorf("a replay overwrote a file: %q", got)
	}
	// A new opID after the first apply is a real second attempt: it conflicts.
	if _, err := w.ApplyOverlay("attach-2", realOverlayDir(t), dyn); !errors.Is(err, ErrOverlayConflict) {
		t.Errorf("second attach: want ErrOverlayConflict, got %v", err)
	}
	// An opID used for a file operation is not reusable for an overlay.
	if _, err := w.Apply("w1", FileOp{Kind: FileOpWriteFile, Path: "x.txt", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApplyOverlay("w1", realOverlayDir(t), dyn); !errors.Is(err, ErrOpIDReused) {
		t.Errorf("reused op id: want ErrOpIDReused, got %v", err)
	}
}

func TestApplyOverlay_RejectsBadInput(t *testing.T) {
	w, root := siteWorkspace(t)
	dir := realOverlayDir(t)
	if _, err := w.ApplyOverlay("", dir, dyn); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty op id: got %v", err)
	}
	for name, files := range map[string]map[string]string{
		"missing the dynamic file": {},
		"an extra file":            {"src/orbit.config.ts": "x", "src/evil.ts": "y"},
		"the wrong file":           {"src/other.ts": "x"},
	} {
		if _, err := w.ApplyOverlay("a-"+strings.ReplaceAll(name, " ", "-"), dir, files); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", name, err)
		}
	}
	if _, err := w.ApplyOverlay("b", filepath.Join(root, "no-such-overlay"), dyn); !errors.Is(err, ErrOverlayInvalid) {
		t.Errorf("missing overlay: got %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "package.json"), "not json")
	if _, err := w.ApplyOverlay("c", dir, dyn); !errors.Is(err, ErrOverlayInvalid) {
		t.Errorf("bad package.json: got %v", err)
	}
	if err := os.Remove(filepath.Join(root, "package.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ApplyOverlay("d", dir, dyn); !errors.Is(err, ErrOverlayInvalid) {
		t.Errorf("no package.json: got %v", err)
	}
}

func TestLoadOverlay_RejectsAnUnsafeManifest(t *testing.T) {
	for _, bad := range []string{`"../x"`, `"/etc/passwd"`, `"a/../b"`, `".git/config"`, `""`} {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "overlay.json"), `{"version":1,"files":[`+bad+`],"dynamic":[],"replace":[],"dependencies":{}}`)
		if _, _, err := loadOverlay(dir); !errors.Is(err, ErrOverlayInvalid) {
			t.Errorf("manifest path %s: want ErrOverlayInvalid, got %v", bad, err)
		}
	}
}

func TestMergeDependencies_SameVersionIsANoopAndMissingKeyIsCreated(t *testing.T) {
	deps := map[string]string{"a": "1.0.0"}
	out, added, conflicts, err := mergeDependencies([]byte(`{"name":"x","dependencies":{"a":"1.0.0"}}`), deps)
	if err != nil || len(added) != 0 || len(conflicts) != 0 || !strings.Contains(string(out), `"a":"1.0.0"`) {
		t.Errorf("same version: out=%s added=%v conflicts=%v err=%v", out, added, conflicts, err)
	}
	out, added, _, err = mergeDependencies([]byte(`{"name":"x"}`), deps)
	if err != nil || !reflect.DeepEqual(added, []string{"a"}) || !strings.Contains(string(out), `"dependencies": {`) {
		t.Errorf("no dependencies key: out=%s added=%v err=%v", out, added, err)
	}
	if _, _, _, err := mergeDependencies([]byte(`[]`), deps); !errors.Is(err, ErrOverlayInvalid) {
		t.Errorf("array package.json: got %v", err)
	}
}

// overlayTree returns path -> content for every file under root.
func overlayTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".orbit" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ---- the route ----

func installOverlay(t *testing.T, dst string) {
	t.Helper()
	src := realOverlayDir(t)
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		mustWriteFile(t, filepath.Join(dst, OverlayBackendName, rel), string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServerOverlay(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	installOverlay(t, f.tmpl)
	f.initSession("sess-a")
	body := OverlayRequest{OpID: "attach-1", Name: OverlayRouteName, Files: dyn}
	post := func(id string, b OverlayRequest) *http.Response {
		return f.json("POST", id, "/v1/sessions/"+id+"/overlay", b)
	}

	var res OverlayResult
	decodeBody(t, post("sess-a", body), 200, &res)
	if res.Replayed || len(res.Written) == 0 || !reflect.DeepEqual(res.Dependencies, []string{"@zeeptech/orbit-client", "react-router-dom"}) {
		t.Fatalf("first apply: %+v", res)
	}
	var again OverlayResult
	decodeBody(t, post("sess-a", body), 200, &again)
	if !again.Replayed {
		t.Errorf("same op id must replay: %+v", again)
	}
	// A new op id is a second attempt: the files exist now.
	body.OpID = "attach-2"
	wantErr(t, post("sess-a", body), 409, "overlay_conflict")

	body.Name = "frontend"
	wantErr(t, post("sess-a", body), 422, "unknown_overlay")
	wantErr(t, post("nope", OverlayRequest{OpID: "x", Name: OverlayRouteName, Files: dyn}), 404, "unknown_session")
	badTok := "bad"
	wantErr(t, f.do(rq{method: "POST", id: "sess-a", path: "/v1/sessions/sess-a/overlay", body: []byte(`{}`), ctype: "application/json", token: &badTok}), 401, "unauthorized")
}

func TestServerOverlay_IsNotATemplateAndMissingOverlayIsReported(t *testing.T) {
	f := newSrvFixture(t, srvOpts{})
	// No overlay directory in this image: the route says so instead of failing in the dark.
	f.initSession("sess-a")
	wantErr(t, f.json("POST", "sess-a", "/v1/sessions/sess-a/overlay", OverlayRequest{OpID: "a", Name: OverlayRouteName, Files: dyn}), 404, "unknown_template")

	installOverlay(t, f.tmpl)
	wantErr(t, f.json("POST", "sess-b", "/v1/sessions/sess-b/init?template="+OverlayBackendName, nil), 404, "unknown_template")
}

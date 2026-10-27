package dashboard

// builder_publish_test.go — ai-frontend-builder T32: publish a session as a
// frontend app and the ZIP download. Integration tests against a real Postgres,
// a fake builder (sandbox.Sandbox), a fake GitHub (Git Data API, org repos,
// deploy keys) behind the mock transport the other GitHub handler tests use,
// and a fake deploy provider in place of Render.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/deploy"
)

// ---- fake GitHub ----

type bpubCommit struct {
	sha, message, tree string
	parents            []string
}

type bpubRepo struct {
	head    string
	commits []bpubCommit
}

type bpubGitHub struct {
	mu sync.Mutex

	repos   map[string]*bpubRepo // by repo name under acme-org
	taken   map[string]bool      // names that answer 422 "already exists"
	blobs   map[string][]byte    // blob sha -> content
	trees   map[string][]bpubTreeEntry
	calls   []string // "METHOD path"
	nBlob   int
	nTree   int
	nCommit int
	nKey    int

	// Switches.
	failBlobs   bool // every blob answers 500 with a secret-looking message
	emptyRepo   bool // blobs answer 409 "empty" until the Contents API bootstrap ran
	bootstrap   map[string]bool
	archived    []string
	repoGate    chan struct{} // when set, creating a repository waits for it
	repoGateHit chan struct{}
}

type bpubTreeEntry struct{ path, blob string }

func newBPubGitHub() *bpubGitHub {
	return &bpubGitHub{
		repos: map[string]*bpubRepo{}, taken: map[string]bool{}, blobs: map[string][]byte{},
		trees: map[string][]bpubTreeEntry{}, bootstrap: map[string]bool{},
	}
}

func (g *bpubGitHub) record(r *http.Request) {
	g.calls = append(g.calls, r.Method+" "+r.URL.EscapedPath())
}

// callsMatching counts recorded calls whose "METHOD path" contains sub.
func (g *bpubGitHub) callsMatching(sub string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// apiCalls counts calls other than the installation-token exchange.
func (g *bpubGitHub) apiCalls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if !strings.Contains(c, "access_tokens") {
			n++
		}
	}
	return n
}

func (g *bpubGitHub) client() *http.Client {
	return mockGitHubClient(g.roundTrip)
}

func bpubJSON(status int, body string) (*http.Response, error) {
	return mockJSONResp(status, body), nil
}

func (g *bpubGitHub) hex40(prefix string, n int) string {
	return fmt.Sprintf("%s%0*d", prefix, 40-len(prefix), n)
}

func (g *bpubGitHub) roundTrip(r *http.Request) (*http.Response, error) {
	var body map[string]any
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
	}
	p := r.URL.EscapedPath()

	g.mu.Lock()
	g.record(r)
	gate, hit := g.repoGate, g.repoGateHit
	isRepoCreate := r.Method == http.MethodPost && strings.HasPrefix(p, "/orgs/acme-org/repos")
	g.mu.Unlock()

	if isRepoCreate && gate != nil {
		select {
		case hit <- struct{}{}:
		default:
		}
		<-gate
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	switch {
	case strings.Contains(p, "access_tokens"):
		return bpubJSON(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`)

	case isRepoCreate:
		name, _ := body["name"].(string)
		if g.taken[name] || g.repos[name] != nil {
			return bpubJSON(http.StatusUnprocessableEntity, `{"message":"Repository creation failed.","errors":[{"message":"name already exists on this account"}]}`)
		}
		g.repos[name] = &bpubRepo{}
		return bpubJSON(http.StatusCreated, fmt.Sprintf(
			`{"name":%q,"full_name":"acme-org/%s","html_url":"https://github.com/acme-org/%s","clone_url":"https://github.com/acme-org/%s.git","default_branch":"main","private":true}`,
			name, name, name, name))
	}

	// /repos/acme-org/{repo}/...
	rest, ok := strings.CutPrefix(p, "/repos/acme-org/")
	if !ok {
		return bpubJSON(http.StatusNotFound, `{"message":"unexpected request"}`)
	}
	name, tail, _ := strings.Cut(rest, "/")
	repo := g.repos[name]
	if repo == nil {
		return bpubJSON(http.StatusNotFound, `{"message":"Not Found"}`)
	}

	switch {
	case r.Method == http.MethodPatch && tail == "":
		g.archived = append(g.archived, name)
		return bpubJSON(http.StatusOK, `{}`)

	case r.Method == http.MethodPost && tail == "keys":
		g.nKey++
		return bpubJSON(http.StatusCreated, fmt.Sprintf(`{"id":%d}`, 7000+g.nKey))

	case r.Method == http.MethodPut && strings.HasPrefix(tail, "contents/"):
		g.bootstrap[name] = true
		g.nCommit++
		sha := g.hex40("b", g.nCommit)
		repo.commits = append(repo.commits, bpubCommit{sha: sha, message: "Initialize repository"})
		repo.head = sha
		return bpubJSON(http.StatusCreated, fmt.Sprintf(`{"commit":{"sha":%q}}`, sha))

	case r.Method == http.MethodGet && strings.HasPrefix(tail, "git/ref/"):
		if repo.head == "" {
			return bpubJSON(http.StatusNotFound, `{"message":"Not Found"}`)
		}
		return bpubJSON(http.StatusOK, fmt.Sprintf(`{"object":{"sha":%q}}`, repo.head))

	case r.Method == http.MethodPost && tail == "git/blobs":
		if g.failBlobs {
			return bpubJSON(http.StatusInternalServerError, `{"message":"internal secret text from github"}`)
		}
		if g.emptyRepo && !g.bootstrap[name] {
			return bpubJSON(http.StatusConflict, `{"message":"Git Repository is empty."}`)
		}
		g.nBlob++
		sha := g.hex40("a", g.nBlob)
		content, _ := base64.StdEncoding.DecodeString(fmt.Sprint(body["content"]))
		g.blobs[sha] = content
		return bpubJSON(http.StatusCreated, fmt.Sprintf(`{"sha":%q}`, sha))

	case r.Method == http.MethodPost && tail == "git/trees":
		g.nTree++
		sha := g.hex40("c", g.nTree)
		var entries []bpubTreeEntry
		list, _ := body["tree"].([]any)
		for _, it := range list {
			m, _ := it.(map[string]any)
			entries = append(entries, bpubTreeEntry{path: fmt.Sprint(m["path"]), blob: fmt.Sprint(m["sha"])})
		}
		g.trees[sha] = entries
		return bpubJSON(http.StatusCreated, fmt.Sprintf(`{"sha":%q}`, sha))

	case r.Method == http.MethodPost && tail == "git/commits":
		g.nCommit++
		sha := g.hex40("d", g.nCommit)
		var parents []string
		if ps, ok := body["parents"].([]any); ok {
			for _, x := range ps {
				parents = append(parents, fmt.Sprint(x))
			}
		}
		repo.commits = append(repo.commits, bpubCommit{
			sha: sha, message: fmt.Sprint(body["message"]), tree: fmt.Sprint(body["tree"]), parents: parents,
		})
		return bpubJSON(http.StatusCreated, fmt.Sprintf(`{"sha":%q}`, sha))

	case r.Method == http.MethodPost && tail == "git/refs":
		if repo.head != "" && !g.emptyRepo {
			return bpubJSON(http.StatusUnprocessableEntity, `{"message":"Reference already exists"}`)
		}
		repo.head = fmt.Sprint(body["sha"])
		return bpubJSON(http.StatusCreated, `{}`)

	case r.Method == http.MethodPatch && strings.HasPrefix(tail, "git/refs/"):
		repo.head = fmt.Sprint(body["sha"])
		return bpubJSON(http.StatusOK, `{}`)
	}
	return bpubJSON(http.StatusNotFound, `{"message":"unexpected request"}`)
}

// treeOf returns path -> content of commit sha of repo.
func (g *bpubGitHub) treeOf(t *testing.T, repo, sha string) map[string]string {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.repos[repo]
	if r == nil {
		t.Fatalf("no repo %q", repo)
	}
	for _, c := range r.commits {
		if c.sha != sha {
			continue
		}
		out := map[string]string{}
		for _, e := range g.trees[c.tree] {
			out[e.path] = string(g.blobs[e.blob])
		}
		return out
	}
	t.Fatalf("no commit %s in %s", sha, repo)
	return nil
}

func (g *bpubGitHub) repo(name string) *bpubRepo {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.repos[name]
}

func (g *bpubGitHub) repoNames() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for n := range g.repos {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ---- fake deploy provider ----

type bpubRender struct {
	mu       sync.Mutex
	created  []deploy.CreateServiceParams
	createEr error
	factEr   error
}

func (r *bpubRender) factory() DeployProviderFactory {
	return func(context.Context, *DeployProviderConfig) (deploy.DeployProvider, error) {
		if r.factEr != nil {
			return nil, r.factEr
		}
		return r, nil
	}
}

func (r *bpubRender) CreateService(_ context.Context, p deploy.CreateServiceParams) (deploy.ServiceInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, p)
	if r.createEr != nil {
		return deploy.ServiceInfo{}, r.createEr
	}
	return deploy.ServiceInfo{ServiceID: fmt.Sprintf("srv-%d", len(r.created)), URL: "https://" + p.RepoName + ".onrender.test"}, nil
}

func (r *bpubRender) DeleteService(context.Context, string) error { return nil }

func (r *bpubRender) services() []deploy.CreateServiceParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]deploy.CreateServiceParams(nil), r.created...)
}

// ---- fake builder with a switchable Archive ----

type bpubSandbox struct {
	*bhFakeSandbox
	mu         sync.Mutex
	archiveErr error
	archives   int
}

func (s *bpubSandbox) archiveCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.archives
}

func (s *bpubSandbox) Archive(ctx context.Context, id, sha string) (io.ReadCloser, error) {
	s.mu.Lock()
	err := s.archiveErr
	s.archives++
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.bhFakeSandbox.Archive(ctx, id, sha)
}

// ---- environment ----

type bpubEnv struct {
	*bhEnv
	gh     *bpubGitHub
	render *bpubRender
	sb     *bpubSandbox
}

type bpubOption func(*bpubEnvConfig)

type bpubEnvConfig struct {
	noGitHub, noDeploy bool
}

func bpubNoGitHub() bpubOption { return func(c *bpubEnvConfig) { c.noGitHub = true } }
func bpubNoDeploy() bpubOption { return func(c *bpubEnvConfig) { c.noDeploy = true } }

func newBPubEnv(t *testing.T, opts ...bpubOption) *bpubEnv {
	t.Helper()
	var cfg bpubEnvConfig
	for _, o := range opts {
		o(&cfg)
	}
	e := &bpubEnv{bhEnv: newBHEnv(t), gh: newBPubGitHub(), render: &bpubRender{}}
	e.sb = &bpubSandbox{bhFakeSandbox: e.fake}
	ctx := context.Background()
	if !cfg.noGitHub {
		seedInstalledGitHubConfig(t, e.pool, genTestKeyPEM(t))
		if err := UpdateGitHubInstallation(ctx, e.pool, 999, "acme-org", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if !cfg.noDeploy {
		if err := UpsertDeployProviderConfig(ctx, e.pool, "render", "not-a-real-key", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: e.sb, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
		GitHubHTTPClient: e.gh.client(), DeployProviderFactory: e.render.factory(),
	})
	return e
}

// session creates a default-template session owned by e.owner on appA and puts
// the project files the builder would hold into the fake workspace.
func (e *bpubEnv) session(t *testing.T) BuilderSessionView {
	t.Helper()
	s := e.createOK(t, e.owner, e.appA)
	e.setFiles(s.ID, map[string]string{
		"package.json":               `{"name":"my-app"}` + "\n",
		"src/App.tsx":                "export default function App() { return null }\n",
		"src/orbit.config.ts":        "export const ORBIT_API_URL = 'https://orbit.test.example'\n",
		"node_modules/lib/index.js":  "module.exports = 1\n",
		"dist/index.html":            "<html></html>\n",
		".git/config":                "[core]\n",
		".orbit/ops.jsonl":           "{}\n",
		"packages/ui/node_modules/x": "x\n",
		"public/favicon.svg":         "<svg/>\n",
		"scripts/run.sh":             "#!/bin/sh\n",
	})
	return s
}

func (e *bpubEnv) setFiles(id string, files map[string]string) {
	e.fake.mu.Lock()
	defer e.fake.mu.Unlock()
	e.fake.files[id] = files
}

func (e *bpubEnv) editFile(id, path, content string) {
	e.fake.mu.Lock()
	defer e.fake.mu.Unlock()
	e.fake.files[id][path] = content
}

func (e *bpubEnv) publish(t *testing.T, user *DashboardUser, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.Publish, user, http.MethodPost, "/dashboard/api/builder/sessions/"+id+"/publish", body, "id", id)
}

func (e *bpubEnv) publishOK(t *testing.T, id string) BuilderPublishResult {
	t.Helper()
	w := e.publish(t, e.owner, id, "")
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	return decodeJSON[BuilderPublishResult](t, w)
}

func (e *bpubEnv) frontendAppCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.frontend_apps`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *bpubEnv) dbSession(t *testing.T, id string) *store.Session {
	t.Helper()
	s, err := e.st.GetSessionByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *bpubEnv) eventTypes(t *testing.T, id string) []string {
	t.Helper()
	evs, err := e.st.ListEvents(context.Background(), e.owner.ID, id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Type)
	}
	return out
}

func conflictReason(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}
	m := decodeJSON[map[string]string](t, w)
	if m["error"] == "" {
		t.Errorf("409 without an error message: %v", m)
	}
	return m["reason"]
}

// assertNoSideEffects checks that nothing was created or called.
func (e *bpubEnv) assertNoSideEffects(t *testing.T, sessID string) {
	t.Helper()
	e.assertNoSideEffectsExcept(t, sessID, false)
}

// assertNoSideEffectsExcept is assertNoSideEffects; failedEvent allows the one
// publish_failed event a publish that failed after its preconditions records.
func (e *bpubEnv) assertNoSideEffectsExcept(t *testing.T, sessID string, failedEvent bool) {
	t.Helper()
	if n := e.gh.apiCalls(); n != 0 {
		t.Errorf("%d GitHub API calls before the preconditions held: %v", n, e.gh.calls)
	}
	if n := e.frontendAppCount(t); n != 0 {
		t.Errorf("%d frontend apps created", n)
	}
	if n := len(e.render.services()); n != 0 {
		t.Errorf("%d deploy services created", n)
	}
	s := e.dbSession(t, sessID)
	if s.FrontendAppID != nil || s.PublishedSHA != "" {
		t.Errorf("session was touched: %+v", s)
	}
	var tpls int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.github_templates WHERE created_by = $1`, builderDefaultTemplateBy).Scan(&tpls)
	if tpls != 0 {
		t.Errorf("the hidden default template row was created by a rejected publish")
	}
	for _, typ := range e.eventTypes(t, sessID) {
		if strings.HasPrefix(typ, "publish_") && !(failedEvent && (typ == "publish_failed" || typ == "publish_started")) {
			t.Errorf("event %q recorded for a rejected publish", typ)
		}
	}
}

// ---- first publish ----

func TestPublish_FirstPublishCreatesRepoCommitFrontendAppAndService(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)

	w := e.publish(t, e.owner, s.ID, `{"subdomain":"My-App"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	res := decodeJSON[BuilderPublishResult](t, w)
	app := res.FrontendApp
	if app == nil || res.Republish || len(res.PublishedSHA) != 40 {
		t.Fatalf("result = %+v", res)
	}

	// The frontend app is linked to the backend app and owned by the publisher.
	if app.BackendAppID == nil || *app.BackendAppID != e.appA || app.OwnerID != e.owner.ID || app.Status != "ready" {
		t.Errorf("frontend app = %+v", app)
	}
	if app.Name != s.Title || app.Slug != "bh-app-a" {
		t.Errorf("name/slug = %q/%q, want the session title (%q) and its slug", app.Name, app.Slug, s.Title)
	}
	if want := "https://github.com/acme-org/" + app.Slug; app.GithubRepoURL != want {
		t.Errorf("repo url = %q, want %q", app.GithubRepoURL, want)
	}
	role, err := ResolveAppRole(context.Background(), e.pool, e.owner, AppRef{FrontendAppID: app.ID})
	if err != nil || role != AppRoleAdmin {
		t.Errorf("the publisher's role on the new app = %q (%v), want admin", role, err)
	}

	// The session records the link and the GitHub commit.
	got := e.dbSession(t, s.ID)
	if got.FrontendAppID == nil || *got.FrontendAppID != app.ID || got.PublishedSHA != res.PublishedSHA {
		t.Errorf("session = %+v", got)
	}

	// GitHub: one private repo, one root commit on main with the project tree.
	repo := e.gh.repo(app.Slug)
	if repo == nil || len(repo.commits) != 1 || len(repo.commits[0].parents) != 0 || repo.head != res.PublishedSHA {
		t.Fatalf("repo = %+v", repo)
	}
	tree := e.gh.treeOf(t, app.Slug, res.PublishedSHA)
	wantTree := map[string]string{
		"package.json":        `{"name":"my-app"}` + "\n",
		"src/App.tsx":         "export default function App() { return null }\n",
		"src/orbit.config.ts": "export const ORBIT_API_URL = 'https://orbit.test.example'\n",
		"public/favicon.svg":  "<svg/>\n",
		"scripts/run.sh":      "#!/bin/sh\n",
	}
	if len(tree) != len(wantTree) {
		t.Errorf("tree has %d files %v, want %d", len(tree), bpubKeys(tree), len(wantTree))
	}
	for p, c := range wantTree {
		if tree[p] != c {
			t.Errorf("tree[%q] = %q, want %q", p, tree[p], c)
		}
	}
	for p := range tree {
		if strings.Contains(p, "node_modules") || strings.HasPrefix(p, "dist/") || strings.HasPrefix(p, ".git/") || strings.HasPrefix(p, ".orbit/") {
			t.Errorf("excluded path %q was published", p)
		}
	}
	if !strings.Contains(repo.commits[0].message, "Orbit AI Builder") {
		t.Errorf("commit message = %q", repo.commits[0].message)
	}

	// Deploy key for the local clone, and the Render service from the template.
	if e.gh.callsMatching("POST /repos/acme-org/"+app.Slug+"/keys") != 1 {
		t.Errorf("deploy key was not added: %v", e.gh.calls)
	}
	svcs := e.render.services()
	if len(svcs) != 1 {
		t.Fatalf("%d services, want 1", len(svcs))
	}
	if p := svcs[0]; p.RepoOwner != "acme-org" || p.RepoName != app.Slug || p.ServiceType != "static_site" ||
		p.BuildCommand != "npm ci && npm run build" || p.PublishPath != "dist" {
		t.Errorf("service params = %+v", p)
	}
	if app.DeployServiceID != "srv-1" || app.DeployStatus != "ready" || app.DeployURL == "" {
		t.Errorf("deploy fields = %+v", app)
	}

	// Events and audit.
	types := strings.Join(e.eventTypes(t, s.ID), ",")
	if !strings.Contains(types, "publish_started") || !strings.Contains(types, "publish_succeeded") {
		t.Errorf("events = %s", types)
	}
	var audits int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM zeep_system.audit_log WHERE action = 'builder.session.publish'`).Scan(&audits)
	if audits != 1 {
		t.Errorf("%d publish audit rows, want 1", audits)
	}
	// No credential or file content in the response.
	if strings.Contains(w.Body.String(), "private_key") || strings.Contains(w.Body.String(), "not-a-real-key") {
		t.Error("response leaks configuration")
	}
}

func bpubKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestPublish_DefaultTemplateRowIsHiddenInactiveAndReused(t *testing.T) {
	e := newBPubEnv(t)
	s1, s2 := e.session(t), e.session(t)
	a1, a2 := e.publishOK(t, s1.ID).FrontendApp, e.publishOK(t, s2.ID).FrontendApp
	if a1.TemplateID != a2.TemplateID || a1.TemplateName != builderDefaultTemplateName {
		t.Errorf("template ids/names = %q/%q, %q", a1.TemplateID, a2.TemplateID, a1.TemplateName)
	}
	var n int
	var active, enabled bool
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*), bool_or(active), bool_or(builder_enabled) FROM zeep_system.github_templates WHERE created_by = $1`,
		builderDefaultTemplateBy).Scan(&n, &active, &enabled); err != nil {
		t.Fatal(err)
	}
	if n != 1 || active || enabled {
		t.Errorf("default template rows=%d active=%v builder_enabled=%v, want one hidden row", n, active, enabled)
	}
	// Neither the regular frontend-app creation nor the builder picker lists it.
	active1, _ := ListGitHubTemplates(context.Background(), e.pool, true)
	picker, _ := ListBuilderEnabledGitHubTemplates(context.Background(), e.pool)
	if len(active1) != 0 || len(picker) != 0 {
		t.Errorf("hidden template leaked into listings: %v %v", active1, picker)
	}
	// Its deploy metadata is refreshed from the built-in constants.
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.github_templates SET build_command = 'stale' WHERE created_by = $1`, builderDefaultTemplateBy); err != nil {
		t.Fatal(err)
	}
	s3 := e.session(t)
	e.publishOK(t, s3.ID)
	if got := e.render.services()[2].BuildCommand; got != builderDefaultTemplateBuildCmd {
		t.Errorf("build command = %q, want the built-in %q", got, builderDefaultTemplateBuildCmd)
	}
}

func TestPublish_SlugAndRepoNameCollisionsGetASuffix(t *testing.T) {
	e := newBPubEnv(t)
	e.gh.taken["bh-app-a"] = true // the name is free in Orbit but taken on GitHub
	s := e.session(t)
	app := e.publishOK(t, s.ID).FrontendApp
	if app.Slug != "bh-app-a" || !strings.HasPrefix(app.GithubRepoURL, "https://github.com/acme-org/bh-app-a-") {
		t.Errorf("repo-name collision: slug=%q repo=%q", app.Slug, app.GithubRepoURL)
	}

	// An Orbit slug collision gets a suffix too.
	s2 := e.session(t)
	app2 := e.publishOK(t, s2.ID).FrontendApp
	if app2.Slug == app.Slug || !strings.HasPrefix(app2.Slug, "bh-app-a-") {
		t.Errorf("slug collision: %q vs %q", app2.Slug, app.Slug)
	}
}

func TestPublish_CompanyTemplateIsUsedForTheDeploy(t *testing.T) {
	e := newBPubEnv(t)
	tpl, err := CreateGitHubTemplate(context.Background(), e.pool, GitHubTemplateInput{
		Name: "Acme", GitHubOwner: "acme", GitHubRepo: "starter", Framework: "react", CreatedBy: e.super.ID,
		RenderServiceType: "static_site", BuildCommand: "pnpm build", PublishPath: "build",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := e.githubSession(t, tpl)
	app := e.publishOK(t, s.ID).FrontendApp
	if app.TemplateID != tpl.ID {
		t.Errorf("template id = %q, want %q", app.TemplateID, tpl.ID)
	}
	if p := e.render.services()[0]; p.BuildCommand != "pnpm build" || p.PublishPath != "build" || p.ServiceType != "static_site" {
		t.Errorf("service params = %+v", p)
	}
}

// githubSession creates a session on a company template directly in the store
// (the create endpoint is covered by T25), with workspace files.
func (e *bpubEnv) githubSession(t *testing.T, tpl *GitHubTemplate) *store.Session {
	t.Helper()
	ctx := context.Background()
	sess, err := e.st.CreateSession(ctx, store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, Title: "Acme site", TemplateKind: store.TemplateGitHub,
		TemplateID: &tpl.ID, TemplateVersion: "abc1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveInitialSnapshot(ctx, e.owner.ID, sess.ID, strings.Repeat("e", 40), []byte("bundle")); err != nil {
		t.Fatal(err)
	}
	e.fake.mu.Lock()
	e.fake.files[sess.ID] = map[string]string{"package.json": "{}\n", "index.html": "<html/>\n"}
	e.fake.mu.Unlock()
	return e.dbSession(t, sess.ID)
}

// ---- republish ----

func TestPublish_RepublishPushesWithTheLastPublishedCommitAsParent(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	first := e.publishOK(t, s.ID)
	e.editFile(s.ID, "src/App.tsx", "export default function App() { return 'v2' }\n")

	w := e.publish(t, e.owner, s.ID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("republish: want 200, got %d: %s", w.Code, w.Body.String())
	}
	second := decodeJSON[BuilderPublishResult](t, w)
	if !second.Republish || second.PublishedSHA == first.PublishedSHA || second.FrontendApp.ID != first.FrontendApp.ID {
		t.Fatalf("second = %+v", second)
	}

	repo := e.gh.repo(first.FrontendApp.Slug)
	if len(repo.commits) != 2 || len(repo.commits[1].parents) != 1 || repo.commits[1].parents[0] != first.PublishedSHA {
		t.Fatalf("commits = %+v, want a second commit whose parent is the first publish", repo.commits)
	}
	if repo.head != second.PublishedSHA {
		t.Errorf("head = %q, want %q", repo.head, second.PublishedSHA)
	}
	if got := e.gh.treeOf(t, first.FrontendApp.Slug, second.PublishedSHA)["src/App.tsx"]; !strings.Contains(got, "v2") {
		t.Errorf("republished tree has %q", got)
	}
	if got := e.dbSession(t, s.ID).PublishedSHA; got != second.PublishedSHA {
		t.Errorf("session published_sha = %q", got)
	}
	// Nothing new on Orbit's side: one app, one repo, one deploy service (Render
	// redeploys on push by itself).
	if e.frontendAppCount(t) != 1 || len(e.gh.repoNames()) != 1 || len(e.render.services()) != 1 {
		t.Errorf("apps=%d repos=%v services=%d", e.frontendAppCount(t), e.gh.repoNames(), len(e.render.services()))
	}
	if n := e.gh.callsMatching("/keys"); n != 1 {
		t.Errorf("%d deploy-key calls, want 1 (first publish only)", n)
	}
}

func TestPublish_RemoteMovedIs409AndNothingIsPushed(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	first := e.publishOK(t, s.ID)

	// Somebody pushed to the repository outside the builder.
	repo := e.gh.repo(first.FrontendApp.Slug)
	e.gh.mu.Lock()
	repo.head = strings.Repeat("f", 40)
	e.gh.mu.Unlock()
	blobsBefore, commitsBefore := e.gh.callsMatching("git/blobs"), e.gh.callsMatching("git/commits")
	e.editFile(s.ID, "src/App.tsx", "changed\n")

	w := e.publish(t, e.owner, s.ID, "")
	if reason := conflictReason(t, w); reason != PublishReasonRemoteMoved {
		t.Fatalf("reason = %q", reason)
	}
	if got := bhErrorBody(t, w); got != "repository changed outside the builder" {
		t.Errorf("message = %q", got)
	}
	if e.gh.callsMatching("git/blobs") != blobsBefore || e.gh.callsMatching("git/commits") != commitsBefore || e.gh.callsMatching("PATCH /repos/acme-org/"+first.FrontendApp.Slug+"/git/refs") != 0 {
		t.Errorf("a push was attempted against a moved remote: %v", e.gh.calls)
	}
	e.gh.mu.Lock()
	head := repo.head
	e.gh.mu.Unlock()
	if head != strings.Repeat("f", 40) {
		t.Error("the foreign commit was overwritten")
	}
	if got := e.dbSession(t, s.ID).PublishedSHA; got != first.PublishedSHA {
		t.Errorf("published_sha moved to %q", got)
	}
	if !strings.Contains(strings.Join(e.eventTypes(t, s.ID), ","), "publish_failed") {
		t.Error("no publish_failed event")
	}
}

func TestPublish_RepublishNeedsWriteAccessOnTheFrontendApp(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	first := e.publishOK(t, s.ID)
	commits := len(e.gh.repo(first.FrontendApp.Slug).commits)

	for name, role := range map[string]string{"viewer": "viewer", "removed": ""} {
		if role == "" {
			if _, err := e.pool.Exec(context.Background(),
				`DELETE FROM zeep_system.app_members WHERE frontend_app_id = $1 AND user_id = $2`, first.FrontendApp.ID, e.owner.ID); err != nil {
				t.Fatal(err)
			}
		} else if _, err := e.pool.Exec(context.Background(),
			`UPDATE zeep_system.app_members SET role = $3 WHERE frontend_app_id = $1 AND user_id = $2`, first.FrontendApp.ID, e.owner.ID, role); err != nil {
			t.Fatal(err)
		}
		if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusForbidden {
			t.Errorf("%s: want 403, got %d: %s", name, w.Code, w.Body.String())
		}
		if len(e.gh.repo(first.FrontendApp.Slug).commits) != commits {
			t.Errorf("%s: a commit was pushed without write access", name)
		}
	}
	// An editor may republish.
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'editor')`, first.FrontendApp.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusOK {
		t.Errorf("editor: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPublish_ArchivedFrontendAppIsReplacedByANewOne(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	first := e.publishOK(t, s.ID)
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE zeep_system.frontend_apps SET archived_at = now() WHERE id = $1`, first.FrontendApp.ID); err != nil {
		t.Fatal(err)
	}
	second := e.publishOK(t, s.ID)
	if second.Republish || second.FrontendApp.ID == first.FrontendApp.ID {
		t.Fatalf("second = %+v", second)
	}
	if got := e.dbSession(t, s.ID); got.FrontendAppID == nil || *got.FrontendAppID != second.FrontendApp.ID {
		t.Errorf("session link = %+v", got.FrontendAppID)
	}
	if n := len(e.gh.repo(second.FrontendApp.Slug).commits); n != 1 {
		t.Errorf("new repo has %d commits", n)
	}
}

// ---- preconditions: 409 with a reason, before any side effect ----

func TestPublish_GitHubNotConfiguredIs409BeforeAnySideEffect(t *testing.T) {
	e := newBPubEnv(t, bpubNoGitHub())
	s := e.session(t)
	archivesBefore := e.sb.archiveCalls()
	w := e.publish(t, e.owner, s.ID, "")
	if reason := conflictReason(t, w); reason != PublishReasonGitHubNotConfigured {
		t.Fatalf("reason = %q", reason)
	}
	e.assertNoSideEffects(t, s.ID)
	if e.sb.archiveCalls() != archivesBefore {
		t.Error("the builder was asked for the tree before the preconditions held")
	}
}

func TestPublish_GitHubInstalledWithoutInstallationIs409(t *testing.T) {
	e := newBPubEnv(t, bpubNoGitHub())
	seedInstalledGitHubConfig(t, e.pool, genTestKeyPEM(t))
	if _, err := e.pool.Exec(context.Background(), `UPDATE zeep_system.github_app_config SET installation_id = NULL`); err != nil {
		t.Fatal(err)
	}
	s := e.session(t)
	if reason := conflictReason(t, e.publish(t, e.owner, s.ID, "")); reason != PublishReasonGitHubNotConfigured {
		t.Fatalf("reason = %q", reason)
	}
	e.assertNoSideEffects(t, s.ID)
}

func TestPublish_DeployProviderNotConfiguredIs409BeforeAnySideEffect(t *testing.T) {
	e := newBPubEnv(t, bpubNoDeploy())
	s := e.session(t)
	if reason := conflictReason(t, e.publish(t, e.owner, s.ID, "")); reason != PublishReasonDeployNotConfigured {
		t.Fatalf("reason = %q", reason)
	}
	e.assertNoSideEffects(t, s.ID)
}

func TestPublish_TemplateWithoutDeployMetadataIs409BeforeAnySideEffect(t *testing.T) {
	cases := map[string]GitHubTemplateInput{
		"no render_service_type": {RenderServiceType: "", BuildCommand: "npm run build", PublishPath: "dist"},
		"no build_command":       {RenderServiceType: "static_site", BuildCommand: "", PublishPath: "dist"},
		"no publish_path":        {RenderServiceType: "static_site", BuildCommand: "npm run build", PublishPath: ""},
		"no metadata at all":     {},
		"web service no start":   {RenderServiceType: "web_service", BuildCommand: "npm run build"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			e := newBPubEnv(t)
			in.Name, in.GitHubOwner, in.GitHubRepo, in.Framework, in.CreatedBy = "Acme", "acme", "starter", "react", e.super.ID
			tpl, err := CreateGitHubTemplate(context.Background(), e.pool, in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := SetGitHubTemplateBuilderEnabled(context.Background(), e.pool, tpl.ID, true); err != nil {
				t.Fatal(err)
			}
			s := e.githubSession(t, tpl)
			w := e.publish(t, e.owner, s.ID, "")
			if reason := conflictReason(t, w); reason != PublishReasonTemplateNotDeploy {
				t.Fatalf("reason = %q", reason)
			}
			if msg := bhErrorBody(t, w); !strings.Contains(msg, "missing") {
				t.Errorf("message does not say what is missing: %q", msg)
			}
			e.assertNoSideEffects(t, s.ID)
		})
	}
}

func TestPublish_MissingTemplateIs409(t *testing.T) {
	e := newBPubEnv(t)
	tpl, err := CreateGitHubTemplate(context.Background(), e.pool, GitHubTemplateInput{
		Name: "Gone", GitHubOwner: "acme", GitHubRepo: "gone", CreatedBy: e.super.ID,
		RenderServiceType: "static_site", BuildCommand: "x", PublishPath: "dist",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := e.githubSession(t, tpl)
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM zeep_system.github_templates WHERE id = $1`, tpl.ID); err != nil {
		t.Fatal(err)
	}
	if reason := conflictReason(t, e.publish(t, e.owner, s.ID, "")); reason != PublishReasonTemplateUnavailable {
		t.Fatalf("reason = %q", reason)
	}
	e.assertNoSideEffects(t, s.ID)
}

func TestPublish_TurnInProgressAndNothingToPublishAre409(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	for _, status := range []string{store.SessionRunning, store.SessionQueued, store.SessionAwaitingConfirmation} {
		if _, err := e.pool.Exec(context.Background(), `UPDATE zeep_system.builder_sessions SET status = $2 WHERE id = $1`, s.ID, status); err != nil {
			t.Fatal(err)
		}
		if reason := conflictReason(t, e.publish(t, e.owner, s.ID, "")); reason != PublishReasonTurnInProgress {
			t.Errorf("%s: reason = %q", status, reason)
		}
	}
	e.assertNoSideEffects(t, s.ID)

	// A failed session (last turn failed) can still be published: its work is committed.
	if _, err := e.pool.Exec(context.Background(), `UPDATE zeep_system.builder_sessions SET status = 'failed' WHERE id = $1`, s.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusCreated {
		t.Errorf("failed session: want 201, got %d: %s", w.Code, w.Body.String())
	}

	// No commit yet.
	empty, err := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reason := conflictReason(t, e.publish(t, e.owner, empty.ID, "")); reason != PublishReasonNothingToPublish {
		t.Errorf("reason = %q", reason)
	}
}

func TestPublish_ConcurrentPublishOfOneSessionIsRefused(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	e.gh.repoGate, e.gh.repoGateHit = make(chan struct{}), make(chan struct{}, 1)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.publish(t, e.owner, s.ID, "") }()
	select {
	case <-e.gh.repoGateHit:
	case <-time.After(10 * time.Second):
		t.Fatal("the first publish never reached GitHub")
	}

	w := e.publish(t, e.owner, s.ID, "")
	if reason := conflictReason(t, w); reason != PublishReasonInProgress {
		t.Errorf("concurrent publish: reason = %q", reason)
	}
	close(e.gh.repoGate)
	first := <-done
	if first.Code != http.StatusCreated {
		t.Fatalf("first publish: %d %s", first.Code, first.Body.String())
	}
	if e.frontendAppCount(t) != 1 || len(e.gh.repoNames()) != 1 {
		t.Errorf("apps=%d repos=%v, want one of each", e.frontendAppCount(t), e.gh.repoNames())
	}
	// The lock is gone with the first publish: the next one is a normal republish.
	if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusOK {
		t.Errorf("republish after the lock was released: %d %s", w.Code, w.Body.String())
	}
}

// ---- failures ----

func TestPublish_RenderFailureKeepsTheAppFailedAndTheSessionIntact(t *testing.T) {
	cases := map[string]func(*bpubRender){
		"service creation fails":   func(r *bpubRender) { r.createEr = errors.New("render exploded") },
		"provider cannot be built": func(r *bpubRender) { r.factEr = errors.New("bad api key") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newBPubEnv(t)
			setup(e.render)
			s := e.session(t)

			w := e.publish(t, e.owner, s.ID, "")
			if w.Code != http.StatusCreated {
				t.Fatalf("want 201 (the code is published), got %d: %s", w.Code, w.Body.String())
			}
			res := decodeJSON[BuilderPublishResult](t, w)
			app := res.FrontendApp
			if app.DeployStatus != "failed" || app.DeployErrorMessage == "" || app.DeployServiceID != "" {
				t.Errorf("deploy fields = %q %q %q", app.DeployStatus, app.DeployErrorMessage, app.DeployServiceID)
			}
			// The frontend app exists and points at a repo that holds the code, so
			// the existing deploy retry applies.
			if app.GithubRepoURL == "" || len(e.gh.repo(app.Slug).commits) != 1 {
				t.Errorf("repo not published: %+v", app)
			}
			// The session is intact: linked, idle, same head, and publishable again.
			got := e.dbSession(t, s.ID)
			if got.Status != store.SessionIdle || got.HeadSHA != s.HeadSHA || got.FrontendAppID == nil ||
				*got.FrontendAppID != app.ID || got.PublishedSHA != res.PublishedSHA {
				t.Errorf("session = %+v", got)
			}
			if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusOK {
				t.Errorf("republish after a deploy failure: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPublish_GitHubPushFailureCreatesNoAppAndNeverLeaksTheError(t *testing.T) {
	e := newBPubEnv(t)
	e.gh.failBlobs = true
	s := e.session(t)

	w := e.publish(t, e.owner, s.ID, "")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret text") || strings.Contains(w.Body.String(), "github.com") {
		t.Errorf("response leaks the GitHub error: %s", w.Body.String())
	}
	if e.frontendAppCount(t) != 0 || len(e.render.services()) != 0 {
		t.Error("a frontend app or service exists for a publish that never pushed")
	}
	if got := e.dbSession(t, s.ID); got.FrontendAppID != nil || got.PublishedSHA != "" || got.Status != store.SessionIdle {
		t.Errorf("session = %+v", got)
	}
	// The empty repository does not linger (archived, best effort).
	e.gh.mu.Lock()
	archived := append([]string(nil), e.gh.archived...)
	e.gh.mu.Unlock()
	if len(archived) != 1 {
		t.Errorf("archived repos = %v", archived)
	}
	var failed int
	for _, typ := range e.eventTypes(t, s.ID) {
		if typ == "publish_failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("%d publish_failed events", failed)
	}
	// A retry works once GitHub is back, with the same session.
	e.gh.mu.Lock()
	e.gh.failBlobs = false
	e.gh.mu.Unlock()
	if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusCreated {
		t.Errorf("retry: %d %s", w.Code, w.Body.String())
	}
}

func TestPublish_BuilderFailureIs503AndNothingHappens(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)
	e.sb.archiveErr = &sandbox.Error{Status: 502, Code: "unavailable", Message: "builder unavailable", Err: errors.New("dial tcp 10.0.0.1: refused")}

	w := e.publish(t, e.owner, s.ID, "")
	if w.Code != http.StatusServiceUnavailable || bhErrorBody(t, w) != builderUnavailableMsg {
		t.Fatalf("want 503 %q, got %d: %s", builderUnavailableMsg, w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "10.0.0.1") {
		t.Error("the builder address leaked")
	}
	e.assertNoSideEffectsExcept(t, s.ID, true)
	if !strings.Contains(strings.Join(e.eventTypes(t, s.ID), ","), "publish_failed") {
		t.Error("no publish_failed event")
	}
}

func TestPublish_EmptyRepositoryFallbackIsTransparent(t *testing.T) {
	// [Incerto] GitHub may refuse blobs on a repository without commits; the
	// writer then bootstraps through the Contents API. Publish does not care.
	e := newBPubEnv(t)
	e.gh.emptyRepo = true
	s := e.session(t)
	res := e.publishOK(t, s.ID)
	repo := e.gh.repo(res.FrontendApp.Slug)
	if len(repo.commits) != 2 || repo.head != res.PublishedSHA || len(repo.commits[1].parents) != 1 {
		t.Fatalf("commits = %+v", repo.commits)
	}
	if got := e.gh.treeOf(t, res.FrontendApp.Slug, res.PublishedSHA); got["package.json"] == "" {
		t.Errorf("tree = %v", bpubKeys(got))
	}
	// And a republish builds on the real commit, not on the placeholder.
	e.editFile(s.ID, "src/App.tsx", "v2\n")
	if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("republish: %d %s", w.Code, w.Body.String())
	}
}

// ---- access and input ----

func TestPublish_OwnerOnlyAndInputValidation(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)

	for _, u := range []*DashboardUser{e.other, e.super, e.viewer, e.outsider} {
		if w := e.publish(t, u, s.ID, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", u.Email, w.Code)
		}
	}
	if w := e.publish(t, nil, s.ID, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", w.Code)
	}
	for _, body := range []string{`{"subdomain":"Bad_Name!"}`, `{"subdomain":"-x"}`, `{"subdomain":` + `"` + strings.Repeat("a", 64) + `"}`, `{not json`} {
		if w := e.publish(t, e.owner, s.ID, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: want 400, got %d", body, w.Code)
		}
	}
	e.assertNoSideEffects(t, s.ID)

	// Archived sessions cannot be published.
	if err := e.st.ArchiveSession(context.Background(), e.owner.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.publish(t, e.owner, s.ID, ""); w.Code != http.StatusNotFound {
		t.Errorf("archived: want 404, got %d", w.Code)
	}
}

func TestPublish_NotConfiguredBuilderIs503(t *testing.T) {
	e := newBPubEnv(t)
	h := NewBuilderHandler(e.pool, BuilderHandlerConfig{})
	for name, fn := range map[string]http.HandlerFunc{"publish": h.Publish, "zip": h.DownloadZip} {
		if w := e.do(t, fn, e.owner, http.MethodPost, "/x", "", "id", "00000000-0000-4000-8000-000000000000"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: want 503, got %d", name, w.Code)
		}
	}
}

// ---- ZIP ----

func (e *bpubEnv) zip(t *testing.T, user *DashboardUser, id string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, e.h.DownloadZip, user, http.MethodGet, "/dashboard/api/builder/sessions/"+id+"/download.zip", "", "id", id)
}

func TestDownloadZip_ContainsTheTreeButNotNodeModules(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)

	w := e.zip(t, e.owner, s.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="bh-app-a.zip"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	want := map[string]string{
		"package.json":        `{"name":"my-app"}` + "\n",
		"src/App.tsx":         "export default function App() { return null }\n",
		"src/orbit.config.ts": "export const ORBIT_API_URL = 'https://orbit.test.example'\n",
		"public/favicon.svg":  "<svg/>\n",
		"scripts/run.sh":      "#!/bin/sh\n",
	}
	if len(got) != len(want) {
		t.Errorf("zip has %v, want %v", bpubKeys(got), bpubKeys(want))
	}
	for p, c := range want {
		if got[p] != c {
			t.Errorf("%s = %q, want %q", p, got[p], c)
		}
	}
	for p := range got {
		if strings.Contains(p, "node_modules") || strings.HasPrefix(p, "dist/") || strings.HasPrefix(p, ".git/") || strings.HasPrefix(p, ".orbit/") {
			t.Errorf("excluded path %q in the zip", p)
		}
	}
	var audits int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.audit_log WHERE action = 'builder.session.download'`).Scan(&audits)
	if audits != 1 {
		t.Errorf("%d download audit rows, want 1", audits)
	}
}

func TestDownloadZip_OwnerOnlyAndFailureModes(t *testing.T) {
	e := newBPubEnv(t)
	s := e.session(t)

	for _, u := range []*DashboardUser{e.other, e.super, e.outsider} {
		if w := e.zip(t, u, s.ID); w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", u.Email, w.Code)
		}
	}
	if w := e.zip(t, nil, s.ID); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", w.Code)
	}

	// Builder down: 503, no zip headers, no leak.
	e.sb.archiveErr = &sandbox.Error{Status: 0, Code: "unavailable", Message: "x", Err: errors.New("dial tcp 10.0.0.1: refused")}
	w := e.zip(t, e.owner, s.ID)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Content-Type") == "application/zip" || strings.Contains(w.Body.String(), "10.0.0.1") {
		t.Errorf("builder down: %d %q (%s)", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}

	// No commit yet.
	empty, _ := e.st.CreateSession(context.Background(), store.CreateSessionParams{
		OwnerUserID: e.owner.ID, BackendAppID: e.appA, TemplateKind: store.TemplateDefault, TemplateVersion: "7",
	})
	e.sb.archiveErr = nil
	if w := e.zip(t, e.owner, empty.ID); w.Code != http.StatusConflict {
		t.Errorf("no commit: want 409, got %d", w.Code)
	}

	// Archived.
	if err := e.st.ArchiveSession(context.Background(), e.owner.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.zip(t, e.owner, s.ID); w.Code != http.StatusNotFound {
		t.Errorf("archived: want 404, got %d", w.Code)
	}
}

func TestDownloadZip_NeedsNeitherGitHubNorADeployProvider(t *testing.T) {
	e := newBPubEnv(t, bpubNoGitHub(), bpubNoDeploy())
	s := e.session(t)
	if w := e.zip(t, e.owner, s.ID); w.Code != http.StatusOK {
		t.Errorf("zip without integrations: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- tree helpers ----

func TestTreeExcludedAndCleanTreePath(t *testing.T) {
	for _, p := range []string{"node_modules/a/b.js", "a/node_modules/b", ".git/config", "dist/index.html", "packages/x/dist/y.js", ".orbit/ops.jsonl"} {
		if !treeExcluded(p) {
			t.Errorf("treeExcluded(%q) = false", p)
		}
	}
	for _, p := range []string{"src/node_modules_notes.md", "src/dist.ts", "distribution/a", ".github/workflows/ci.yml", ".gitignore", "a/.orbit.json"} {
		if treeExcluded(p) {
			t.Errorf("treeExcluded(%q) = true", p)
		}
	}
	good := map[string]string{"a/b.txt": "a/b.txt", "./a/b.txt": "a/b.txt", "a//b": "a/b", "a/./b": "a/b"}
	for in, want := range good {
		if got, ok := cleanTreePath(in); !ok || got != want {
			t.Errorf("cleanTreePath(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "/etc/passwd", "../x", "a/../../x", "..", "a\\b", "a\x00b", "."} {
		if got, ok := cleanTreePath(in); ok {
			t.Errorf("cleanTreePath(%q) = %q, want rejection", in, got)
		}
	}
}

func bpubTar(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range entries {
		h := h
		h.Size = int64(len("x"))
		if h.Typeflag == tar.TypeSymlink || h.Typeflag == tar.TypeDir {
			h.Size = 0
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			_, _ = tw.Write([]byte("x"))
		}
	}
	_ = tw.Close()
	return buf.Bytes()
}

func TestWriteTreeZipAndReadPublishTree_RejectUnsafeArchives(t *testing.T) {
	evil := map[string][]tar.Header{
		"zip slip":      {{Typeflag: tar.TypeReg, Name: "../evil.sh", Mode: 0o644}},
		"absolute path": {{Typeflag: tar.TypeReg, Name: "/etc/cron.d/x", Mode: 0o644}},
		"symlink":       {{Typeflag: tar.TypeSymlink, Name: "link", Linkname: "/etc/passwd"}},
	}
	for name, hdrs := range evil {
		t.Run(name, func(t *testing.T) {
			raw := bpubTar(t, hdrs...)
			var out bytes.Buffer
			zw := zip.NewWriter(&out)
			if name != "symlink" { // a symlink is skipped by the zip, refused by publish
				if err := writeTreeZip(zw, bytes.NewReader(raw)); err == nil {
					t.Error("writeTreeZip accepted an unsafe entry")
				}
			}
			fake := newBHFakeSandbox()
			e := &BuilderHandler{sandbox: archiveOnlySandbox{Sandbox: fake, raw: raw}}
			if _, err := e.readPublishTree(context.Background(), "s"); err == nil {
				t.Error("readPublishTree accepted an unsafe archive")
			} else {
				var te *publishTreeError
				if !errors.As(err, &te) {
					t.Errorf("error = %v, want a publishTreeError", err)
				}
			}
		})
	}
}

type archiveOnlySandbox struct {
	sandbox.Sandbox
	raw []byte
}

func (a archiveOnlySandbox) Archive(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(a.raw)), nil
}

func TestReadPublishTree_KeepsTheExecutableBitAndSkipsExcluded(t *testing.T) {
	raw := bpubTar(t,
		tar.Header{Typeflag: tar.TypeReg, Name: "run.sh", Mode: 0o755},
		tar.Header{Typeflag: tar.TypeReg, Name: "a.txt", Mode: 0o644},
		tar.Header{Typeflag: tar.TypeReg, Name: "node_modules/x/y.js", Mode: 0o644},
		tar.Header{Typeflag: tar.TypeDir, Name: "src/", Mode: 0o755},
	)
	h := &BuilderHandler{sandbox: archiveOnlySandbox{Sandbox: newBHFakeSandbox(), raw: raw}}
	files, err := h.readPublishTree(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	for _, f := range files {
		if f.Path == "run.sh" && !f.Executable || f.Path == "a.txt" && f.Executable {
			t.Errorf("mode lost: %+v", f)
		}
	}
}

func TestReadPublishTree_EmptyTreeIsRefused(t *testing.T) {
	raw := bpubTar(t, tar.Header{Typeflag: tar.TypeReg, Name: "node_modules/only.js", Mode: 0o644})
	h := &BuilderHandler{sandbox: archiveOnlySandbox{Sandbox: newBHFakeSandbox(), raw: raw}}
	if _, err := h.readPublishTree(context.Background(), "s"); err == nil {
		t.Error("a tree with nothing publishable was accepted")
	}
}

// ---- against the real builder (git, real archive format) ----

func TestPublishAndZip_AgainstRealBuilderArchive(t *testing.T) {
	e := newBPubEnv(t)
	real := newRealBuilderSandbox(t)
	e.h = NewBuilderHandler(e.pool, BuilderHandlerConfig{
		Store: e.st, Sandbox: real, Logger: zap.NewNop(), PublicBaseURL: "https://orbit.test.example",
		GitHubHTTPClient: e.gh.client(), DeployProviderFactory: e.render.factory(),
	})
	s := e.createOK(t, e.owner, e.appA)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := real.Op(ctx, s.ID, "op-1", sandbox.FileOp{Kind: sandbox.OpWriteFile, Path: "src/Extra.tsx", Content: "export const x = 1\n"}); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if _, err := real.Commit(ctx, s.ID, 1, "turn 1"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	res := e.publishOK(t, s.ID)
	tree := e.gh.treeOf(t, res.FrontendApp.Slug, res.PublishedSHA)
	for _, want := range []string{"package.json", "src/App.tsx", "src/Extra.tsx", "src/orbit.config.ts", ".gitignore", "ORBIT_AGENT.md"} {
		if _, ok := tree[want]; !ok {
			t.Errorf("%s missing from the published tree %v", want, bpubKeys(tree))
		}
	}
	if !strings.Contains(tree["src/orbit.config.ts"], "'bh_app_a'") {
		t.Errorf("published config = %q", tree["src/orbit.config.ts"])
	}
	for p := range tree {
		if strings.Contains(p, "pax_global_header") || strings.HasSuffix(p, "/") {
			t.Errorf("tar bookkeeping entry %q published", p)
		}
	}

	w := e.zip(t, e.owner, s.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("zip: %d %s", w.Code, w.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["src/Extra.tsx"] || !names["package.json"] || names["pax_global_header"] {
		t.Errorf("zip entries = %v", names)
	}
}

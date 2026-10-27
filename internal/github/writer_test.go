package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const (
	headSHA   = "1111111111111111111111111111111111111111"
	treeSHA   = "2222222222222222222222222222222222222222"
	commitSHA = "3333333333333333333333333333333333333333"
	otherSHA  = "9999999999999999999999999999999999999999"
)

// recordedReq is one request the fake GitHub received.
type recordedReq struct {
	Method string
	Path   string // escaped path
	Body   map[string]any
}

// fakeGitHub records requests and answers them through route, which returns a
// status and a JSON body (or ("", 0) to answer 500). Safe for concurrent use:
// CommitTree uploads blobs in parallel.
type fakeGitHub struct {
	mu    sync.Mutex
	reqs  []recordedReq
	route func(r recordedReq) (int, string)
}

func (f *fakeGitHub) client(t *testing.T) *Client {
	t.Helper()
	return newTestClient(t, func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Errorf("request body is not JSON: %v", err)
				}
			}
		}
		r := recordedReq{Method: req.Method, Path: req.URL.EscapedPath(), Body: body}
		f.mu.Lock()
		f.reqs = append(f.reqs, r)
		f.mu.Unlock()
		status, payload := f.route(r)
		if status == 0 {
			status, payload = http.StatusInternalServerError, `{"message":"boom"}`
		}
		return mockJSONResponse(status, payload), nil
	})
}

// count returns how many recorded requests match method and a path containing sub.
func (f *fakeGitHub) count(method, sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if r.Method == method && strings.Contains(r.Path, sub) {
			n++
		}
	}
	return n
}

func (f *fakeGitHub) first(method, sub string) *recordedReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.reqs {
		if f.reqs[i].Method == method && strings.Contains(f.reqs[i].Path, sub) {
			return &f.reqs[i]
		}
	}
	return nil
}

// writes counts the mutating requests received.
func (f *fakeGitHub) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if r.Method != http.MethodGet {
			n++
		}
	}
	return n
}

// happyRoute serves the whole Git Data flow. refHead is what GET ref returns
// ("" = 404, branch absent); refUpdateStatus / refCreateStatus override the
// ref write responses.
func happyRoute(refHead string, refUpdateStatus, refCreateStatus int) func(recordedReq) (int, string) {
	blobN := 0
	var mu sync.Mutex
	return func(r recordedReq) (int, string) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.Path, "/git/ref/"):
			if refHead == "" {
				return http.StatusNotFound, `{"message":"Not Found"}`
			}
			return http.StatusOK, fmt.Sprintf(`{"object":{"sha":%q}}`, refHead)
		case r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/blobs"):
			mu.Lock()
			blobN++
			n := blobN
			mu.Unlock()
			return http.StatusCreated, fmt.Sprintf(`{"sha":"%040d"}`, n)
		case r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/trees"):
			return http.StatusCreated, fmt.Sprintf(`{"sha":%q}`, treeSHA)
		case r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/commits"):
			return http.StatusCreated, fmt.Sprintf(`{"sha":%q}`, commitSHA)
		case r.Method == http.MethodPatch && strings.Contains(r.Path, "/git/refs/"):
			if refUpdateStatus == 0 {
				refUpdateStatus = http.StatusOK
			}
			return refUpdateStatus, `{"message":"Update is not a fast forward"}`
		case r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/refs"):
			if refCreateStatus == 0 {
				refCreateStatus = http.StatusCreated
			}
			return refCreateStatus, `{"message":"Reference already exists"}`
		}
		return http.StatusNotFound, `{"message":"unexpected request"}`
	}
}

func sampleFiles() []TreeFile {
	return []TreeFile{
		{Path: "package.json", Content: []byte(`{"name":"app"}`)},
		{Path: "src/main.tsx", Content: []byte("console.log('hi')\n")},
		{Path: "scripts/build.sh", Content: []byte("#!/bin/sh\n"), Executable: true},
	}
}

// First commit (no parent): the ref does not exist, so it is created; the
// commit has no parent, the tree is built without base_tree and nothing moves
// an existing ref.
func TestCommitTree_FirstCommitWithoutParentCreatesRef(t *testing.T) {
	gh := &fakeGitHub{route: happyRoute("", 0, 0)}
	client := gh.client(t)

	sha, err := client.CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "Publish from Orbit builder", "")
	if err != nil {
		t.Fatalf("CommitTree() error = %v", err)
	}
	if sha != commitSHA {
		t.Errorf("sha = %q, want %q", sha, commitSHA)
	}
	if n := gh.count(http.MethodPost, "/git/blobs"); n != 3 {
		t.Errorf("blob requests = %d, want 3", n)
	}

	tree := gh.first(http.MethodPost, "/git/trees")
	if tree == nil {
		t.Fatal("no tree request")
	}
	if _, has := tree.Body["base_tree"]; has {
		t.Error("the tree must be built from scratch: base_tree must be absent")
	}
	entries, _ := tree.Body["tree"].([]any)
	if len(entries) != 3 {
		t.Fatalf("tree entries = %d, want 3", len(entries))
	}
	modes := map[string]string{}
	for _, e := range entries {
		m := e.(map[string]any)
		modes[m["path"].(string)] = m["mode"].(string)
		if m["type"] != "blob" || m["sha"] == "" {
			t.Errorf("bad tree entry: %v", m)
		}
	}
	if modes["package.json"] != "100644" || modes["scripts/build.sh"] != "100755" {
		t.Errorf("file modes = %v", modes)
	}

	commit := gh.first(http.MethodPost, "/git/commits")
	parents, ok := commit.Body["parents"].([]any)
	if !ok || len(parents) != 0 {
		t.Errorf("a first commit must have an empty parents array, got %v", commit.Body["parents"])
	}
	if commit.Body["tree"] != treeSHA || commit.Body["message"] != "Publish from Orbit builder" {
		t.Errorf("commit body = %v", commit.Body)
	}

	ref := gh.first(http.MethodPost, "/git/refs")
	if ref == nil || ref.Body["ref"] != "refs/heads/main" || ref.Body["sha"] != commitSHA {
		t.Fatalf("expected the ref to be created at the commit, got %+v", ref)
	}
	if gh.count(http.MethodPatch, "/git/refs") != 0 {
		t.Error("a first commit must not update a ref")
	}
}

// Second commit whose expected parent matches the remote head: the commit's
// parent is that head and the ref is fast-forwarded with force:false.
func TestCommitTree_SecondCommitWithMatchingParentFastForwards(t *testing.T) {
	gh := &fakeGitHub{route: happyRoute(headSHA, 0, 0)}
	client := gh.client(t)

	sha, err := client.CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "Republish", headSHA)
	if err != nil {
		t.Fatalf("CommitTree() error = %v", err)
	}
	if sha != commitSHA {
		t.Errorf("sha = %q, want %q", sha, commitSHA)
	}

	commit := gh.first(http.MethodPost, "/git/commits")
	parents, _ := commit.Body["parents"].([]any)
	if len(parents) != 1 || parents[0] != headSHA {
		t.Errorf("parents = %v, want [%s]", parents, headSHA)
	}
	patch := gh.first(http.MethodPatch, "/git/refs/heads/main")
	if patch == nil {
		t.Fatal("expected a PATCH of refs/heads/main")
	}
	if patch.Body["force"] != false || patch.Body["sha"] != commitSHA {
		t.Errorf("ref update body = %v, want force:false and the new commit", patch.Body)
	}
	for _, r := range gh.reqs {
		if r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/refs") {
			t.Error("an existing branch must be updated, not re-created")
		}
	}
}

// A remote head that is not the expected parent: ErrRemoteMoved before any
// write (no blobs, tree, commit or ref update).
func TestCommitTree_MismatchedHeadReturnsErrRemoteMovedWithoutWrites(t *testing.T) {
	gh := &fakeGitHub{route: happyRoute(otherSHA, 0, 0)}
	client := gh.client(t)

	_, err := client.CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "Republish", headSHA)
	if !errors.Is(err, ErrRemoteMoved) {
		t.Fatalf("error = %v, want ErrRemoteMoved", err)
	}
	if n := gh.writes(); n != 0 {
		t.Errorf("no write may happen when the head moved, got %d", n)
	}
}

// An expected parent with a branch that no longer exists, and a first commit
// into a branch that already exists, are both "the remote moved".
func TestCommitTree_BranchGoneOrAppearedReturnsErrRemoteMoved(t *testing.T) {
	gone := &fakeGitHub{route: happyRoute("", 0, 0)}
	if _, err := gone.client(t).CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "m", headSHA); !errors.Is(err, ErrRemoteMoved) {
		t.Errorf("deleted branch: error = %v, want ErrRemoteMoved", err)
	}
	if gone.writes() != 0 {
		t.Error("deleted branch: no writes expected")
	}

	appeared := &fakeGitHub{route: happyRoute(headSHA, 0, 0)}
	if _, err := appeared.client(t).CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "m", ""); !errors.Is(err, ErrRemoteMoved) {
		t.Errorf("first commit into an existing branch: error = %v, want ErrRemoteMoved", err)
	}
	if appeared.writes() != 0 {
		t.Error("first commit into an existing branch: no writes expected")
	}
}

// The remote moves between the head check and the ref update: GitHub refuses
// the non-fast-forward (422, force:false) and that maps to ErrRemoteMoved. The
// same for a first commit racing another creator of the branch.
func TestCommitTree_RefUpdateRaceReturnsErrRemoteMoved(t *testing.T) {
	update := &fakeGitHub{route: happyRoute(headSHA, http.StatusUnprocessableEntity, 0)}
	if _, err := update.client(t).CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "m", headSHA); !errors.Is(err, ErrRemoteMoved) {
		t.Errorf("non-fast-forward: error = %v, want ErrRemoteMoved", err)
	}

	create := &fakeGitHub{route: happyRoute("", 0, http.StatusUnprocessableEntity)}
	if _, err := create.client(t).CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "m", ""); !errors.Is(err, ErrRemoteMoved) {
		t.Errorf("ref already exists: error = %v, want ErrRemoteMoved", err)
	}
}

// A failed blob upload aborts the commit: no tree, commit or ref write.
func TestCommitTree_BlobFailureAbortsBeforeRefUpdate(t *testing.T) {
	base := happyRoute(headSHA, 0, 0)
	var mu sync.Mutex
	blobs := 0
	gh := &fakeGitHub{route: func(r recordedReq) (int, string) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/blobs") {
			mu.Lock()
			blobs++
			n := blobs
			mu.Unlock()
			if n == 2 { // the second blob upload fails
				return http.StatusInternalServerError, `{"message":"boom"}`
			}
		}
		return base(r)
	}}
	client := gh.client(t)

	_, err := client.CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "Republish", headSHA)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrRemoteMoved) {
		t.Errorf("a blob failure is not ErrRemoteMoved: %v", err)
	}
	for _, sub := range []string{"/git/trees", "/git/commits", "/git/refs"} {
		if n := gh.count(http.MethodPost, sub) + gh.count(http.MethodPatch, sub); n != 0 {
			t.Errorf("%s must not be called after a blob failure, got %d", sub, n)
		}
	}
}

// Binary content survives (base64, byte for byte) and rate limiting is
// reported as ErrRateLimited.
func TestCommitTree_BlobEncodingAndRateLimit(t *testing.T) {
	bin := []byte{0x00, 0xff, 0x10, 0x80, 'a', '\n', 0x00}
	gh := &fakeGitHub{route: happyRoute("", 0, 0)}
	if _, err := gh.client(t).CommitTree(context.Background(), "acme", "my-app", "main",
		[]TreeFile{{Path: "logo.png", Content: bin}}, "m", ""); err != nil {
		t.Fatalf("CommitTree: %v", err)
	}
	blob := gh.first(http.MethodPost, "/git/blobs")
	got, err := base64.StdEncoding.DecodeString(blob.Body["content"].(string))
	if err != nil || string(got) != string(bin) || blob.Body["encoding"] != "base64" {
		t.Errorf("blob content must be base64 of the exact bytes: %v %v", blob.Body, err)
	}

	limited := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/git/ref/") {
			return mockJSONResponse(http.StatusNotFound, `{}`), nil
		}
		resp := mockJSONResponse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
		resp.Header.Set("X-RateLimit-Remaining", "0")
		resp.Header.Set("X-RateLimit-Reset", "1900000000")
		return resp, nil
	})
	_, err = limited.CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "m", "")
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("error = %v, want ErrRateLimited", err)
	}
}

// A repository with no commits rejects blobs (409 empty): the client creates a
// placeholder commit through the Contents API and builds on top of it.
func TestCommitTree_EmptyRepositoryBootstrapsThenCommits(t *testing.T) {
	base := happyRoute("", 0, 0)
	var mu sync.Mutex
	bootstrapped := false
	gh := &fakeGitHub{route: func(r recordedReq) (int, string) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPut && strings.Contains(r.Path, "/contents/"):
			bootstrapped = true
			return http.StatusCreated, fmt.Sprintf(`{"commit":{"sha":%q}}`, otherSHA)
		case r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/git/blobs") && !bootstrapped:
			return http.StatusConflict, `{"message":"Git Repository is empty."}`
		}
		return base(r)
	}}
	client := gh.client(t)

	sha, err := client.CommitTree(context.Background(), "acme", "my-app", "main", sampleFiles(), "First publish", "")
	if err != nil {
		t.Fatalf("CommitTree() error = %v", err)
	}
	if sha != commitSHA {
		t.Errorf("sha = %q", sha)
	}
	put := gh.first(http.MethodPut, "/contents/")
	if put == nil || put.Body["branch"] != "main" {
		t.Fatalf("expected a Contents API bootstrap on branch main, got %+v", put)
	}
	commit := gh.first(http.MethodPost, "/git/commits")
	if parents, _ := commit.Body["parents"].([]any); len(parents) != 1 || parents[0] != otherSHA {
		t.Errorf("the real commit must sit on the bootstrap commit, parents = %v", commit.Body["parents"])
	}
	if patch := gh.first(http.MethodPatch, "/git/refs/heads/main"); patch == nil || patch.Body["force"] != false {
		t.Errorf("expected a non-forced ref update after bootstrap, got %+v", patch)
	}
}

func TestCommitTree_RejectsInvalidInputWithoutRequests(t *testing.T) {
	gh := &fakeGitHub{route: happyRoute("", 0, 0)}
	client := gh.client(t)
	ctx := context.Background()
	ok := sampleFiles()

	cases := []struct {
		name    string
		owner   string
		branch  string
		files   []TreeFile
		message string
		parent  string
	}{
		{"no files", "acme", "main", nil, "m", ""},
		{"empty message", "acme", "main", ok, "   ", ""},
		{"empty branch", "acme", "", ok, "m", ""},
		{"bad owner", "ac/me", "main", ok, "m", ""},
		{"bad branch", "acme", "../main", ok, "m", ""},
		{"bad parent sha", "acme", "main", ok, "m", "not-a-sha"},
		{"parent too short", "acme", "main", ok, "m", "abc123"},
		{"path traversal", "acme", "main", []TreeFile{{Path: "../etc/passwd", Content: []byte("x")}}, "m", ""},
		{"nested traversal", "acme", "main", []TreeFile{{Path: "src/../../x", Content: []byte("x")}}, "m", ""},
		{"absolute path", "acme", "main", []TreeFile{{Path: "/etc/passwd", Content: []byte("x")}}, "m", ""},
		{"git dir", "acme", "main", []TreeFile{{Path: ".git/config", Content: []byte("x")}}, "m", ""},
		{"nested git dir", "acme", "main", []TreeFile{{Path: "a/.GIT/hooks/x", Content: []byte("x")}}, "m", ""},
		{"backslash", "acme", "main", []TreeFile{{Path: `a\b`, Content: []byte("x")}}, "m", ""},
		{"empty segment", "acme", "main", []TreeFile{{Path: "a//b", Content: []byte("x")}}, "m", ""},
		{"empty path", "acme", "main", []TreeFile{{Path: "", Content: []byte("x")}}, "m", ""},
		{"duplicate path", "acme", "main", []TreeFile{{Path: "a", Content: nil}, {Path: "a", Content: nil}}, "m", ""},
	}
	for _, c := range cases {
		if _, err := client.CommitTree(ctx, c.owner, "my-app", c.branch, c.files, c.message, c.parent); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
	gh.mu.Lock()
	n := len(gh.reqs)
	gh.mu.Unlock()
	if n != 0 {
		t.Errorf("invalid input must never reach GitHub, got %d requests", n)
	}
}

func TestCommitTree_BranchWithSlashIsEscapedPerSegment(t *testing.T) {
	gh := &fakeGitHub{route: happyRoute(headSHA, 0, 0)}
	if _, err := gh.client(t).CommitTree(context.Background(), "acme", "my-app", "release/v1", sampleFiles(), "m", headSHA); err != nil {
		t.Fatalf("CommitTree: %v", err)
	}
	if gh.first(http.MethodGet, "/git/ref/heads/release/v1") == nil || gh.first(http.MethodPatch, "/git/refs/heads/release/v1") == nil {
		t.Errorf("expected ref calls on heads/release/v1, got %+v", gh.reqs)
	}
}

// ---- CreateEmptyRepo ----

func TestCreateEmptyRepo_CreatesWithoutAutoInit(t *testing.T) {
	var got recordedReq
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		got = recordedReq{Method: req.Method, Path: req.URL.Path, Body: body}
		return mockJSONResponse(http.StatusCreated, `{"name":"my-app","full_name":"acme/my-app","html_url":"https://github.com/acme/my-app","clone_url":"https://github.com/acme/my-app.git","default_branch":"main","private":true}`), nil
	})

	info, err := client.CreateEmptyRepo(context.Background(), "acme", "my-app", true)
	if err != nil {
		t.Fatalf("CreateEmptyRepo() error = %v", err)
	}
	if got.Method != http.MethodPost || got.Path != "/orgs/acme/repos" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	if got.Body["name"] != "my-app" || got.Body["private"] != true || got.Body["auto_init"] != false {
		t.Errorf("body = %v, want name, private:true, auto_init:false", got.Body)
	}
	if info.FullName != "acme/my-app" || info.DefaultBranch != "main" || !info.Private ||
		info.HTMLURL != "https://github.com/acme/my-app" || info.CloneURL == "" {
		t.Errorf("info = %+v", info)
	}
}

func TestCreateEmptyRepo_ErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		rl     bool
		check  func(error) bool
	}{
		{"exists", http.StatusUnprocessableEntity, `{"message":"Repository creation failed.","errors":[{"message":"name already exists on this account"}]}`, false,
			func(e error) bool { return errors.Is(e, ErrRepoExists) }},
		{"invalid name", http.StatusUnprocessableEntity, `{"message":"Repository creation failed."}`, false,
			func(e error) bool { return !errors.Is(e, ErrRepoExists) && strings.Contains(e.Error(), "422") }},
		{"org not found", http.StatusNotFound, `{"message":"Not Found"}`, false,
			func(e error) bool { return strings.Contains(e.Error(), "not found or not accessible") }},
		{"no permission", http.StatusForbidden, `{"message":"Resource not accessible by integration"}`, false,
			func(e error) bool {
				return strings.Contains(e.Error(), "Administration") && !errors.Is(e, ErrRateLimited)
			}},
		{"rate limited", http.StatusForbidden, `{"message":"rate limit"}`, true,
			func(e error) bool { return errors.Is(e, ErrRateLimited) }},
	}
	for _, tc := range cases {
		client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
			resp := mockJSONResponse(tc.status, tc.body)
			if tc.rl {
				resp.Header.Set("X-RateLimit-Remaining", "0")
				resp.Header.Set("X-RateLimit-Reset", "1900000000")
			}
			return resp, nil
		})
		if _, err := client.CreateEmptyRepo(context.Background(), "acme", "my-app", true); err == nil || !tc.check(err) {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
	}
}

func TestCreateEmptyRepo_RejectsInvalidNamesWithoutRequest(t *testing.T) {
	called := false
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		called = true
		return mockJSONResponse(http.StatusCreated, `{}`), nil
	})
	for _, c := range [][2]string{{"acme", ""}, {"acme", "a/b"}, {"acme", ".."}, {"", "app"}, {"ac me", "app"}, {"acme", "app?x"}} {
		if _, err := client.CreateEmptyRepo(context.Background(), c[0], c[1], true); err == nil {
			t.Errorf("CreateEmptyRepo(%q, %q): expected an error", c[0], c[1])
		}
	}
	if called {
		t.Error("invalid input must not reach GitHub")
	}
}

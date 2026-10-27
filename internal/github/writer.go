package github

// writer.go — the GitHub "writer" half of the client (ai-frontend-builder T31,
// AFB-22, design §11): create an empty repository and commit a full tree to a
// branch through the Git Data API, without ever overwriting commits somebody
// else pushed.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/sync/errgroup"
)

const (
	// Limits of a single CommitTree call. They bound the number of API calls
	// (one blob each) and the memory held for the request bodies; the builder's
	// templates and generated apps are orders of magnitude below them.
	maxCommitFiles      = 5000
	maxCommitFileBytes  = 25 << 20
	maxCommitTotalBytes = 100 << 20
	maxCommitPathLen    = 1024
	maxCommitMessageLen = 10000
	blobUploadWorkers   = 4

	fileModeRegular    = "100644"
	fileModeExecutable = "100755"
)

// ErrRemoteMoved is returned by CommitTree when the remote branch is not where
// the caller expects: it holds commits the caller did not make (head differs
// from expectedParent), it was deleted, or it appeared while the caller assumed
// a first commit. Nothing is overwritten in that case. Callers surface it as
// "the repository changed outside the builder" (design §12: HTTP 409).
var ErrRemoteMoved = errors.New("github: the remote branch changed since the last known commit")

// ErrRepoExists is returned by CreateEmptyRepo when a repository with that name
// already exists in the organization.
var ErrRepoExists = errors.New("github: repository already exists")

// RepoInfo describes a repository returned by CreateEmptyRepo.
type RepoInfo struct {
	Name          string
	FullName      string
	HTMLURL       string
	CloneURL      string
	DefaultBranch string
	Private       bool
}

// TreeFile is one file of a commit. Path is repository-relative with forward
// slashes. Content is stored byte for byte (binary safe).
type TreeFile struct {
	Path       string
	Content    []byte
	Executable bool
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// CreateEmptyRepo creates an empty repository (no auto-init: no README, no
// commits) named name under the organization owner via POST /orgs/{org}/repos.
// The installation must have the Administration permission; a user (non-org)
// account is not supported by this endpoint. ErrRepoExists when the name is
// taken.
func (c *Client) CreateEmptyRepo(ctx context.Context, owner, name string, private bool) (RepoInfo, error) {
	if err := validRepoRef(owner, name, ""); err != nil {
		return RepoInfo{}, err
	}
	reqBody, err := json.Marshal(map[string]any{"name": name, "private": private, "auto_init": false})
	if err != nil {
		return RepoInfo{}, fmt.Errorf("github: marshal create-repo request body: %w", err)
	}
	endpoint := fmt.Sprintf("%s/orgs/%s/repos", githubAPIBaseURL, url.PathEscape(owner))
	resp, body, err := c.doAuthenticated(ctx, http.MethodPost, endpoint, reqBody)
	if err != nil {
		return RepoInfo{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var parsed struct {
			Name          string `json:"name"`
			FullName      string `json:"full_name"`
			HTMLURL       string `json:"html_url"`
			CloneURL      string `json:"clone_url"`
			DefaultBranch string `json:"default_branch"`
			Private       bool   `json:"private"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return RepoInfo{}, fmt.Errorf("github: parse create-repo response: %w", err)
		}
		if parsed.FullName == "" || parsed.HTMLURL == "" {
			return RepoInfo{}, errors.New("github: create-repo response is missing full_name or html_url")
		}
		return RepoInfo{
			Name: parsed.Name, FullName: parsed.FullName, HTMLURL: parsed.HTMLURL,
			CloneURL: parsed.CloneURL, DefaultBranch: parsed.DefaultBranch, Private: parsed.Private,
		}, nil
	case http.StatusUnprocessableEntity:
		if strings.Contains(strings.ToLower(string(body)), "already exists") {
			return RepoInfo{}, fmt.Errorf("%w: %s/%s", ErrRepoExists, owner, name)
		}
		return RepoInfo{}, fmt.Errorf("github: cannot create repository %q (422): the name is invalid or not allowed", name)
	case http.StatusNotFound:
		return RepoInfo{}, fmt.Errorf("github: organization %q not found or not accessible to the installation", owner)
	case http.StatusForbidden, http.StatusTooManyRequests:
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return RepoInfo{}, rlErr
		}
		if resp.StatusCode == http.StatusForbidden {
			return RepoInfo{}, fmt.Errorf("github: the installation lacks permission to create repositories in %q (needs Administration)", owner)
		}
		return RepoInfo{}, unexpectedStatusError(resp, body)
	default:
		return RepoInfo{}, unexpectedStatusError(resp, body)
	}
}

// CommitTree writes files as the complete tree of one new commit on branch of
// owner/repo and returns the commit sha. The tree is built from scratch (no
// base_tree): a file that is absent from files is absent from the commit.
//
// expectedParent is the commit the caller last published ("" for the first
// commit of an empty repository). The new commit's parent is expectedParent and
// the branch ref is moved with force:false, so GitHub itself refuses anything
// that is not a fast-forward. If the remote head is not expectedParent (checked
// before any write, and enforced again by the ref update to close the race),
// CommitTree returns ErrRemoteMoved and has not moved the ref. A blob/tree/
// commit failure aborts before the ref is touched; the unreferenced objects it
// leaves behind are garbage-collected by GitHub.
//
// [Incerto, verify against live GitHub] The Git Data API rejects blob creation
// on a repository with no commits at all ("Git Repository is empty", 409). In
// that case, for a first commit, CommitTree first creates a placeholder commit
// through the Contents API and builds the real commit on top of it, so history
// then has two commits (the placeholder's file is not in the final tree).
func (c *Client) CommitTree(ctx context.Context, owner, repo, branch string, files []TreeFile, message, expectedParent string) (string, error) {
	if err := validRepoRef(owner, repo, branch); err != nil {
		return "", err
	}
	if branch == "" {
		return "", errors.New("github: branch is required")
	}
	if expectedParent != "" && !shaPattern.MatchString(expectedParent) {
		return "", errors.New("github: expectedParent must be a 40-character commit sha or empty")
	}
	message = strings.TrimSpace(message)
	if message == "" || len(message) > maxCommitMessageLen {
		return "", errors.New("github: commit message is required and must be under 10000 bytes")
	}
	if err := validateTreeFiles(files); err != nil {
		return "", err
	}

	head, exists, err := c.branchHead(ctx, owner, repo, branch)
	if err != nil {
		return "", err
	}
	switch {
	case exists && (expectedParent == "" || !strings.EqualFold(head, expectedParent)):
		return "", ErrRemoteMoved
	case !exists && expectedParent != "":
		return "", ErrRemoteMoved
	}

	sha, err := c.commitOnTop(ctx, owner, repo, branch, files, message, expectedParent, exists)
	if errors.Is(err, errEmptyRepository) && !exists && expectedParent == "" {
		boot, berr := c.bootstrapEmptyRepo(ctx, owner, repo, branch)
		if berr != nil {
			return "", berr
		}
		return c.commitOnTop(ctx, owner, repo, branch, files, message, boot, true)
	}
	return sha, err
}

// errEmptyRepository marks GitHub's 409 "Git Repository is empty".
var errEmptyRepository = errors.New("github: repository is empty")

func validateTreeFiles(files []TreeFile) error {
	if len(files) == 0 {
		return errors.New("github: at least one file is required")
	}
	if len(files) > maxCommitFiles {
		return fmt.Errorf("github: too many files (%d, limit %d)", len(files), maxCommitFiles)
	}
	seen := make(map[string]bool, len(files))
	var total int64
	for _, f := range files {
		if err := validateTreePath(f.Path); err != nil {
			return err
		}
		if seen[f.Path] {
			return fmt.Errorf("github: duplicate file path %q", f.Path)
		}
		seen[f.Path] = true
		if len(f.Content) > maxCommitFileBytes {
			return fmt.Errorf("github: file %q is too large (limit %d bytes)", f.Path, maxCommitFileBytes)
		}
		total += int64(len(f.Content))
		if total > maxCommitTotalBytes {
			return fmt.Errorf("github: files are too large in total (limit %d bytes)", maxCommitTotalBytes)
		}
	}
	return nil
}

func validateTreePath(p string) error {
	if p == "" || len(p) > maxCommitPathLen || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") {
		return fmt.Errorf("github: invalid file path %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.EqualFold(seg, ".git") {
			return fmt.Errorf("github: invalid file path %q", p)
		}
	}
	return nil
}

// refPath is the escaped "heads/<branch>" fragment used by the git/ref(s) endpoints.
func refPath(branch string) string {
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "heads/" + strings.Join(segs, "/")
}

func gitAPI(owner, repo, tail string) string {
	return fmt.Sprintf("%s/repos/%s/%s/git/%s", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo), tail)
}

// branchHead returns the commit sha branch points to. exists=false when the
// branch (or the whole repository, for an empty one) has no such ref.
func (c *Client) branchHead(ctx context.Context, owner, repo, branch string) (sha string, exists bool, err error) {
	resp, body, err := c.doAuthenticated(ctx, http.MethodGet, gitAPI(owner, repo, "ref/"+refPath(branch)), nil)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || parsed.Object.SHA == "" {
			return "", false, errors.New("github: unexpected ref response")
		}
		return parsed.Object.SHA, true, nil
	case http.StatusNotFound, http.StatusConflict: // 409: empty repository
		return "", false, nil
	default:
		return "", false, apiStatusError(resp, body)
	}
}

// apiStatusError maps a non-success response to a RateLimitError when GitHub is
// rate limiting, and to the generic unexpected-status error otherwise.
func apiStatusError(resp *http.Response, body []byte) error {
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return rlErr
		}
	}
	return unexpectedStatusError(resp, body)
}

// commitOnTop uploads the blobs, builds the tree and commit with parent (empty
// for a root commit) and moves or creates the branch ref without force.
func (c *Client) commitOnTop(ctx context.Context, owner, repo, branch string, files []TreeFile, message, parent string, branchExists bool) (string, error) {
	blobs, err := c.createBlobs(ctx, owner, repo, files)
	if err != nil {
		return "", err
	}

	type entry struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	entries := make([]entry, len(files))
	for i, f := range files {
		mode := fileModeRegular
		if f.Executable {
			mode = fileModeExecutable
		}
		entries[i] = entry{Path: f.Path, Mode: mode, Type: "blob", SHA: blobs[i]}
	}
	treeSHA, err := c.postSHA(ctx, gitAPI(owner, repo, "trees"), map[string]any{"tree": entries}, "create tree")
	if err != nil {
		return "", err
	}

	parents := []string{}
	if parent != "" {
		parents = append(parents, parent)
	}
	commitSHA, err := c.postSHA(ctx, gitAPI(owner, repo, "commits"),
		map[string]any{"message": message, "tree": treeSHA, "parents": parents}, "create commit")
	if err != nil {
		return "", err
	}

	if branchExists {
		return commitSHA, c.updateRef(ctx, owner, repo, branch, commitSHA)
	}
	return commitSHA, c.createRef(ctx, owner, repo, branch, commitSHA)
}

// createBlobs uploads every file as a base64 blob and returns the shas in the
// order of files. The first blob goes alone so that the empty-repository case
// fails once, not once per worker; the rest run with limited concurrency and
// the first failure cancels the others.
func (c *Client) createBlobs(ctx context.Context, owner, repo string, files []TreeFile) ([]string, error) {
	shas := make([]string, len(files))
	first, err := c.createBlob(ctx, owner, repo, files[0].Content)
	if err != nil {
		return nil, err
	}
	shas[0] = first

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(blobUploadWorkers)
	for i := 1; i < len(files); i++ {
		i := i
		g.Go(func() error {
			sha, err := c.createBlob(gctx, owner, repo, files[i].Content)
			if err != nil {
				return err
			}
			shas[i] = sha
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return shas, nil
}

func (c *Client) createBlob(ctx context.Context, owner, repo string, content []byte) (string, error) {
	reqBody, err := json.Marshal(map[string]string{
		"content":  base64.StdEncoding.EncodeToString(content),
		"encoding": "base64",
	})
	if err != nil {
		return "", fmt.Errorf("github: marshal blob request body: %w", err)
	}
	resp, body, err := c.doAuthenticated(ctx, http.MethodPost, gitAPI(owner, repo, "blobs"), reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var parsed struct {
			SHA string `json:"sha"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || parsed.SHA == "" {
			return "", errors.New("github: create blob: unexpected response")
		}
		return parsed.SHA, nil
	}
	if resp.StatusCode == http.StatusConflict && strings.Contains(strings.ToLower(string(body)), "empty") {
		return "", errEmptyRepository
	}
	return "", fmt.Errorf("github: create blob: %w", apiStatusError(resp, body))
}

// postSHA POSTs payload and returns the "sha" of the 201 response.
func (c *Client) postSHA(ctx context.Context, endpoint string, payload any, what string) (string, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("github: marshal %s request body: %w", what, err)
	}
	resp, body, err := c.doAuthenticated(ctx, http.MethodPost, endpoint, reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github: %s: %w", what, apiStatusError(resp, body))
	}
	var parsed struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.SHA == "" {
		return "", fmt.Errorf("github: %s: unexpected response", what)
	}
	return parsed.SHA, nil
}

// updateRef fast-forwards the branch to sha with force:false. GitHub answers
// 422 when sha is not a descendant of the current head (someone pushed after
// our head check), which maps to ErrRemoteMoved.
func (c *Client) updateRef(ctx context.Context, owner, repo, branch, sha string) error {
	reqBody, err := json.Marshal(map[string]any{"sha": sha, "force": false})
	if err != nil {
		return fmt.Errorf("github: marshal update-ref request body: %w", err)
	}
	resp, body, err := c.doAuthenticated(ctx, http.MethodPatch, gitAPI(owner, repo, "refs/"+refPath(branch)), reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnprocessableEntity, http.StatusNotFound:
		// 422 "Update is not a fast forward" / "Reference does not exist".
		return ErrRemoteMoved
	default:
		return fmt.Errorf("github: update ref: %w", apiStatusError(resp, body))
	}
}

// createRef creates refs/heads/<branch> pointing at sha. 422 "Reference already
// exists" means the branch appeared after our check: ErrRemoteMoved.
func (c *Client) createRef(ctx context.Context, owner, repo, branch, sha string) error {
	reqBody, err := json.Marshal(map[string]string{"ref": "refs/" + refPathRaw(branch), "sha": sha})
	if err != nil {
		return fmt.Errorf("github: marshal create-ref request body: %w", err)
	}
	resp, body, err := c.doAuthenticated(ctx, http.MethodPost, gitAPI(owner, repo, "refs"), reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusUnprocessableEntity:
		if strings.Contains(strings.ToLower(string(body)), "already exists") {
			return ErrRemoteMoved
		}
		return fmt.Errorf("github: create ref: %w", unexpectedStatusError(resp, body))
	default:
		return fmt.Errorf("github: create ref: %w", apiStatusError(resp, body))
	}
}

// refPathRaw is "heads/<branch>" unescaped, for JSON bodies.
func refPathRaw(branch string) string { return "heads/" + branch }

// bootstrapEmptyRepo creates the first commit of an empty repository through
// the Contents API (the only write GitHub accepts on a repository without
// commits) and returns its sha.
func (c *Client) bootstrapEmptyRepo(ctx context.Context, owner, repo, branch string) (string, error) {
	reqBody, err := json.Marshal(map[string]string{
		"message": "Initialize repository",
		"content": base64.StdEncoding.EncodeToString([]byte("initialized\n")),
		"branch":  branch,
	})
	if err != nil {
		return "", fmt.Errorf("github: marshal bootstrap request body: %w", err)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/.orbit-init", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo))
	resp, body, err := c.doAuthenticated(ctx, http.MethodPut, endpoint, reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github: initialize empty repository: %w", apiStatusError(resp, body))
	}
	var parsed struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Commit.SHA == "" {
		return "", errors.New("github: initialize empty repository: unexpected response")
	}
	return parsed.Commit.SHA, nil
}

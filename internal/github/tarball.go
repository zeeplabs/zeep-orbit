package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// MaxTarballBytes caps a downloaded repository tarball (compressed). A
	// company template with a design system is far below this; the cap protects
	// the dashboard and the builder from a repository that is not a template
	// (a monorepo, a repo with vendored binaries).
	MaxTarballBytes = 64 << 20

	// tarballDownloadTimeout bounds a whole download (headers and body) when the
	// caller did not provide its own HTTP client. Callers should still pass a
	// context with a deadline.
	tarballDownloadTimeout = 2 * time.Minute

	// maxErrorSnippetBytes is how much of an error response body is read.
	maxErrorSnippetBytes = 512
)

// ErrTarballTooLarge is returned by DownloadTarball (declared size) or by the
// returned reader (streamed size) when the tarball exceeds MaxTarballBytes.
var ErrTarballTooLarge = errors.New("github: tarball exceeds the size limit")

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,98}[A-Za-z0-9])?$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	refPattern   = regexp.MustCompile(`^[A-Za-z0-9._/+@-]{1,255}$`)
)

// validRepoRef reports whether owner/repo/ref are safe to place in a request
// path. ref may be empty (the default branch).
func validRepoRef(owner, repo, ref string) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("github: invalid repository owner")
	}
	if !repoPattern.MatchString(repo) || repo == "." || repo == ".." {
		return fmt.Errorf("github: invalid repository name")
	}
	if ref != "" {
		if !refPattern.MatchString(ref) {
			return fmt.Errorf("github: invalid git ref")
		}
		for _, seg := range strings.Split(ref, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return fmt.Errorf("github: invalid git ref")
			}
		}
	}
	return nil
}

// DownloadTarball streams the gzip-compressed tarball of owner/repo at ref (a
// branch, tag or commit sha; "" means the default branch) via
// GET /repos/{owner}/{repo}/tarball/{ref}. GitHub answers with a redirect to
// codeload.github.com; the HTTP client follows it (Go drops the Authorization
// header on the cross-domain hop, so the installation token is not sent to the
// download host).
//
// The caller must Close the reader. Reading fails with ErrTarballTooLarge as
// soon as more than MaxTarballBytes arrive, and DownloadTarball fails up front
// when the declared Content-Length already exceeds it, so a runaway download is
// cut instead of truncated silently. The archive has a single top-level
// directory named "<owner>-<repo>-<sha>/"; extracting it is the caller's job.
//
// The Client's HTTP client must follow redirects (a *http.Client does).
func (c *Client) DownloadTarball(ctx context.Context, owner, repo, ref string) (io.ReadCloser, error) {
	if err := validRepoRef(owner, repo, ref); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/tarball", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo))
	if ref != "" {
		segs := strings.Split(ref, "/")
		for i, s := range segs {
			segs[i] = url.PathEscape(s)
		}
		endpoint += "/" + strings.Join(segs, "/")
	}

	token, err := c.tokens.Token(c.installationID)
	if err != nil {
		return nil, fmt.Errorf("github: obtain installation token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	var client httpDoer = c.downloadClient
	if client == nil {
		client = c.httpClient
	}
	if client == nil {
		client = &http.Client{Timeout: tarballDownloadTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		if resp.ContentLength > MaxTarballBytes {
			resp.Body.Close()
			return nil, ErrTarballTooLarge
		}
		return &cappedReadCloser{rc: resp.Body, remaining: MaxTarballBytes}, nil
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, fmt.Errorf("github: repository %s/%s (ref %q) not found or not accessible", owner, repo, ref)
	case http.StatusForbidden, http.StatusTooManyRequests:
		defer resp.Body.Close()
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return nil, rlErr
		}
		if resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("github: insufficient permissions to download repository %s/%s", owner, repo)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippetBytes))
		return nil, unexpectedStatusError(resp, body)
	default:
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippetBytes))
		return nil, unexpectedStatusError(resp, body)
	}
}

// cappedReadCloser reads at most `remaining` bytes from rc and fails with
// ErrTarballTooLarge when rc has more.
type cappedReadCloser struct {
	rc        io.ReadCloser
	remaining int64
}

func (c *cappedReadCloser) Read(p []byte) (int, error) {
	if c.remaining < 0 {
		return 0, ErrTarballTooLarge
	}
	// Read one byte past the cap so exceeding it is detected, not truncated.
	if int64(len(p)) > c.remaining+1 {
		p = p[:c.remaining+1]
	}
	n, err := c.rc.Read(p)
	c.remaining -= int64(n)
	if c.remaining < 0 {
		return n - 1, ErrTarballTooLarge // the extra byte is not handed out
	}
	return n, err
}

func (c *cappedReadCloser) Close() error { return c.rc.Close() }

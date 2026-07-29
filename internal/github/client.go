// This file implements the REST client used to verify template repositories
// and create new repositories from them, built on top of the installation
// token cache in token.go.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPIBaseURL = "https://api.github.com"
)

// AppConfig holds the GitHub App credentials and installation used to
// authenticate every request made by Client. HTTPClient is optional: when
// nil, a default *http.Client with httpClientTimeout is used; callers (e.g.
// tests) may override it with a client wired to a mock transport so no real
// network calls reach the GitHub API.
type AppConfig struct {
	AppID          string
	InstallationID string
	PrivateKeyPEM  []byte
	HTTPClient     *http.Client
}

// Client is a small REST client for the subset of the GitHub API needed to
// verify template repositories and create new repositories from them. All
// authenticated calls go through the embedded InstallationTokenCache, which
// handles App JWT signing and installation token renewal.
type Client struct {
	tokens         *InstallationTokenCache
	installationID string
	httpClient     httpDoer
	appID          string
	privateKeyPEM  []byte
}

// NewClient builds a Client for the given GitHub App configuration.
func NewClient(cfg AppConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: httpClientTimeout}
	}

	return &Client{
		tokens: &InstallationTokenCache{
			AppID:         cfg.AppID,
			PrivateKeyPEM: cfg.PrivateKeyPEM,
			HTTPClient:    httpClient,
		},
		installationID: cfg.InstallationID,
		httpClient:     httpClient,
		appID:          cfg.AppID,
		privateKeyPEM:  cfg.PrivateKeyPEM,
	}
}

// RateLimitError indicates the call failed because the GitHub App
// installation has exhausted its rate limit. ResetAt is when GitHub reports
// the limit will refresh. Use errors.Is(err, ErrRateLimited) to detect this
// condition without depending on the exact reset time.
type RateLimitError struct {
	ResetAt time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limited, resets at %s", e.ResetAt.Format(time.RFC3339))
}

// Is allows errors.Is(err, ErrRateLimited) to match any *RateLimitError,
// regardless of its specific ResetAt value.
func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

// ErrRateLimited is a sentinel usable with errors.Is to detect rate-limit
// failures returned as *RateLimitError.
var ErrRateLimited = errors.New("github: rate limited")

// StatusResult reports the outcome of a lightweight connectivity check.
type StatusResult struct {
	Connected bool
	Error     string
}

// Status confirms that the configured GitHub App installation can still
// obtain a valid installation access token. It does not call any repository
// endpoint, since no specific repository is guaranteed to exist.
func (c *Client) Status(ctx context.Context) (StatusResult, error) {
	_, err := c.tokens.Token(c.installationID)
	if err != nil {
		return StatusResult{Connected: false, Error: err.Error()}, nil
	}
	return StatusResult{Connected: true}, nil
}

// VerifyAppCredentials confirms that appID/privateKeyPEM produce a JWT
// GitHub accepts, by calling GET /app authenticated as the App itself (not
// an installation access token — no InstallationID is required or used).
// This lets callers validate App credentials before an installation exists.
func (c *Client) VerifyAppCredentials(ctx context.Context) error {
	resp, body, err := c.doAsApp(ctx, http.MethodGet, githubAPIBaseURL+"/app")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return unexpectedStatusError(resp, body)
	}
	return nil
}

// GetInstallation fetches installation metadata for installationID,
// authenticated as the App itself (per GitHub's docs, GET
// /app/installations/{installation_id} is an "as the app" endpoint, not an
// installation-token endpoint). It returns the installation's account login,
// which works for both organization and user installations.
func (c *Client) GetInstallation(ctx context.Context, installationID string) (string, error) {
	url := fmt.Sprintf("%s/app/installations/%s", githubAPIBaseURL, installationID)
	resp, body, err := c.doAsApp(ctx, http.MethodGet, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", unexpectedStatusError(resp, body)
	}

	var parsed struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("github: parse installation response: %w", err)
	}
	if parsed.Account.Login == "" {
		return "", fmt.Errorf("github: installation response missing account.login")
	}
	return parsed.Account.Login, nil
}

// doAsApp builds and executes a GitHub API request authenticated as the App
// itself using a freshly-signed App JWT, as opposed to doAuthenticated which
// uses an installation access token. It is used for endpoints that operate
// at the App/installation level (GET /app, GET /app/installations/{id})
// rather than on a specific installation's repositories.
func (c *Client) doAsApp(ctx context.Context, method, url string) (*http.Response, []byte, error) {
	appJWT, err := generateAppJWT(c.appID, c.privateKeyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("github: generate app jwt: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: httpClientTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("github: request failed: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("github: read response body: %w", err)
	}
	resp.Body = io.NopCloser(strings.NewReader(string(body)))

	return resp, body, nil
}

// VerifyTemplateRepo confirms that owner/repo exists, is accessible to the
// configured installation, and is marked as a template repository. It
// returns a descriptive error otherwise.
func (c *Client) VerifyTemplateRepo(ctx context.Context, owner, repo string) error {
	url := fmt.Sprintf("%s/repos/%s/%s", githubAPIBaseURL, owner, repo)

	resp, body, err := c.doAuthenticated(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed struct {
			IsTemplate bool `json:"is_template"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return fmt.Errorf("github: parse repository response: %w", err)
		}
		if !parsed.IsTemplate {
			return fmt.Errorf("github: repository %s/%s is not a template repository", owner, repo)
		}
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("github: repository %s/%s not found or not accessible", owner, repo)
	case http.StatusForbidden:
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return rlErr
		}
		return fmt.Errorf("github: insufficient permissions to access repository %s/%s", owner, repo)
	case http.StatusTooManyRequests:
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return rlErr
		}
		return unexpectedStatusError(resp, body)
	default:
		return unexpectedStatusError(resp, body)
	}
}

// CreateRepoFromTemplate creates a new private repository named newRepoSlug,
// owned by templateOwner, from the given template repository, returning the
// new repository's canonical web URL. The request explicitly sets "owner" in
// the request body: GitHub's generate endpoint defaults to the authenticated
// user when owner is omitted, but installation access tokens have no
// authenticated-user context, so omitting it fails with 422 "Invalid owner."
func (c *Client) CreateRepoFromTemplate(ctx context.Context, templateOwner, templateRepo, newRepoSlug string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/generate", githubAPIBaseURL, templateOwner, templateRepo)

	reqBody, err := json.Marshal(struct {
		Name    string `json:"name"`
		Owner   string `json:"owner"`
		Private bool   `json:"private"`
	}{
		Name:    newRepoSlug,
		Owner:   templateOwner,
		Private: true,
	})
	if err != nil {
		return "", fmt.Errorf("github: marshal create-repo request body: %w", err)
	}

	resp, body, err := c.doAuthenticated(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var parsed struct {
			HTMLURL string `json:"html_url"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("github: parse create-repo response: %w", err)
		}
		if parsed.HTMLURL == "" {
			return "", fmt.Errorf("github: create-repo response missing html_url")
		}
		return parsed.HTMLURL, nil
	case http.StatusUnprocessableEntity:
		return "", fmt.Errorf("github: cannot create repository %q from template %s/%s (422): name already exists, template is not a template repo, or name is invalid. GitHub response: %s", newRepoSlug, templateOwner, templateRepo, string(body))
	case http.StatusForbidden:
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("github: app lacks permission to create repositories from template %s/%s — check installation permissions (Administration) and reinstall if needed", templateOwner, templateRepo)
	case http.StatusTooManyRequests:
		if rlErr := rateLimitErrorFromResponse(resp); rlErr != nil {
			return "", rlErr
		}
		return "", unexpectedStatusError(resp, body)
	default:
		return "", unexpectedStatusError(resp, body)
	}
}

// ArchiveRepo marks a repository as archived via PATCH /repos/{owner}/{repo}.
// This is a best-effort operation — callers should not block on its failure.
func (c *Client) ArchiveRepo(ctx context.Context, owner, repo string) error {
	url := fmt.Sprintf("%s/repos/%s/%s", githubAPIBaseURL, owner, repo)

	reqBody, err := json.Marshal(map[string]bool{"archived": true})
	if err != nil {
		return fmt.Errorf("github: marshal archive request body: %w", err)
	}

	resp, body, err := c.doAuthenticated(ctx, http.MethodPatch, url, reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return unexpectedStatusError(resp, body)
}

func (c *Client) AddDeployKey(ctx context.Context, owner, repo, title, publicKey string) (int64, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/keys", githubAPIBaseURL, owner, repo)

	reqBody, err := json.Marshal(map[string]interface{}{
		"title":     title,
		"key":       publicKey,
		"read_only": false,
	})
	if err != nil {
		return 0, fmt.Errorf("github: marshal deploy key request body: %w", err)
	}

	resp, body, err := c.doAuthenticated(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var parsed struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return 0, fmt.Errorf("github: parse deploy key response: %w", err)
		}
		return parsed.ID, nil
	}
	return 0, unexpectedStatusError(resp, body)
}

func (c *Client) RevokeDeployKey(ctx context.Context, owner, repo string, keyID int64) error {
	url := fmt.Sprintf("%s/repos/%s/%s/keys/%d", githubAPIBaseURL, owner, repo, keyID)

	resp, body, err := c.doAuthenticated(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return unexpectedStatusError(resp, body)
}

// doAuthenticated builds and executes an authenticated GitHub API request,
// returning the raw response (caller must close the body) along with the
// already-read body bytes for convenience. It does not inspect status codes
// beyond generic rate-limit checking on 429, which callers should still
// handle explicitly for 403 since that status is shared with permission
// errors.
func (c *Client) doAuthenticated(ctx context.Context, method, url string, body []byte) (*http.Response, []byte, error) {
	token, err := c.tokens.Token(c.installationID)
	if err != nil {
		return nil, nil, fmt.Errorf("github: obtain installation token: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: httpClientTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("github: request failed: %w", err)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("github: read response body: %w", err)
	}
	// Callers expect to read resp.Body themselves via the returned bytes but
	// still need to Close() the original response; replace the body so a
	// stray direct read still works and Close() remains valid.
	resp.Body = io.NopCloser(strings.NewReader(string(respBody)))

	return resp, respBody, nil
}

// rateLimitErrorFromResponse checks GitHub's rate-limit headers on a 403 or
// 429 response and returns a *RateLimitError if the installation has
// exhausted its quota (X-RateLimit-Remaining == "0"). It returns nil when the
// headers are absent or indicate remaining quota, meaning the caller should
// treat the response as a genuine error rather than rate limiting.
func rateLimitErrorFromResponse(resp *http.Response) error {
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining == "" {
		return nil
	}
	n, err := strconv.Atoi(remaining)
	if err != nil || n != 0 {
		return nil
	}

	resetAt := time.Time{}
	if resetStr := resp.Header.Get("X-RateLimit-Reset"); resetStr != "" {
		if unix, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			resetAt = time.Unix(unix, 0)
		}
	}

	return &RateLimitError{ResetAt: resetAt}
}

// unexpectedStatusError builds a generic error for status codes not
// explicitly handled by a caller, without leaking the raw response body
// beyond a bounded prefix useful for debugging.
func unexpectedStatusError(resp *http.Response, body []byte) error {
	snippet := string(body)
	const maxSnippet = 200
	if len(snippet) > maxSnippet {
		snippet = snippet[:maxSnippet] + "..."
	}
	return fmt.Errorf("github: unexpected response status %d: %s", resp.StatusCode, snippet)
}

package dashboard

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const githubLatestReleaseURL = "https://api.github.com/repos/zeeplabs/zeep-orbit/releases/latest"

const releaseCacheTTL = 1 * time.Hour

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

var (
	releaseCacheMu sync.Mutex
	releaseCache   *githubRelease
	releaseCacheAt time.Time
)

// fetchLatestRelease returns the latest published GitHub release for
// zeeplabs/zeep-orbit, cached in memory to avoid hitting GitHub's unauthenticated
// rate limit (60 req/hour) on every dashboard load.
func fetchLatestRelease() (*githubRelease, error) {
	releaseCacheMu.Lock()
	if releaseCache != nil && time.Since(releaseCacheAt) < releaseCacheTTL {
		cached := releaseCache
		releaseCacheMu.Unlock()
		return cached, nil
	}
	releaseCacheMu.Unlock()

	req, err := http.NewRequest(http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "zeep-orbit-dashboard")
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errStatusFromGithub(resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}

	releaseCacheMu.Lock()
	releaseCache = &rel
	releaseCacheAt = time.Now()
	releaseCacheMu.Unlock()

	return &rel, nil
}

type githubStatusError struct {
	status int
}

func (e *githubStatusError) Error() string {
	return http.StatusText(e.status)
}

func errStatusFromGithub(status int) error {
	return &githubStatusError{status: status}
}

// VersionCheckHandler exposes the latest zeep-orbit GitHub release so the
// dashboard can show an update banner. Degrades to an empty response on any
// upstream failure — this is purely informational, never blocking.
func VersionCheckHandler(w http.ResponseWriter, r *http.Request) {
	rel, err := fetchLatestRelease()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"tag_name": "", "html_url": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"tag_name": rel.TagName,
		"html_url": rel.HTMLURL,
	})
}

// Package github provides authentication primitives for calling the GitHub
// API as a GitHub App: signing short-lived App JWTs and exchanging them for
// installation access tokens, with an in-memory cache so concurrent requests
// don't hammer GitHub for a new token on every call.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

const (
	// jwtClockSkew is subtracted from now() for the iat claim to tolerate
	// clock drift between this host and GitHub's servers.
	jwtClockSkew = 60 * time.Second
	// jwtMaxLifetime is GitHub's documented maximum lifetime for App JWTs.
	jwtMaxLifetime = 10 * time.Minute
	// refreshBuffer forces renewal this long before the cached installation
	// token's real expires_at, so in-flight requests don't race expiry.
	refreshBuffer = 5 * time.Minute

	installationTokenURLFormat = "https://api.github.com/app/installations/%s/access_tokens"
	githubAPIVersion           = "2022-11-28"
)

// generateAppJWT creates a signed RS256 JWT for GitHub App authentication.
// appID is the GitHub App's numeric ID (sent as the iss claim, per spec as a
// string). privateKeyPEM is the App's PEM-encoded RSA private key.
func generateAppJWT(appID string, privateKeyPEM []byte) (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("github: parse app private key: %w", err)
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-jwtClockSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtMaxLifetime)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("github: sign app jwt: %w", err)
	}
	return signed, nil
}

// installationTokenEntry is a cached installation access token together with
// its real expiry, as reported by GitHub.
type installationTokenEntry struct {
	token     string
	expiresAt time.Time
}

// fresh reports whether the cached entry can still be used, applying
// refreshBuffer so callers renew before GitHub actually expires the token.
func (e installationTokenEntry) fresh() bool {
	return time.Now().Before(e.expiresAt.Add(-refreshBuffer))
}

// httpDoer is the subset of *http.Client used by InstallationTokenCache,
// allowing tests to substitute a mock transport.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// InstallationTokenCache signs App JWTs and exchanges them for installation
// access tokens, caching the result per installation ID until it is close to
// expiring.
type InstallationTokenCache struct {
	AppID         string
	PrivateKeyPEM []byte
	HTTPClient    httpDoer

	mu      sync.RWMutex
	entries map[string]installationTokenEntry

	// group collapses concurrent Token() misses/renewals for the same
	// installationID into a single in-flight fetchInstallationToken call,
	// so a burst of concurrent callers doesn't each fire its own JWT sign +
	// HTTP exchange against GitHub. Zero value is ready to use.
	group singleflight.Group
}

// NewInstallationTokenCache builds a cache for the given App ID and PEM
// private key, using http.DefaultClient for the token exchange.
func NewInstallationTokenCache(appID string, privateKeyPEM []byte) *InstallationTokenCache {
	return &InstallationTokenCache{
		AppID:         appID,
		PrivateKeyPEM: privateKeyPEM,
		HTTPClient:    http.DefaultClient,
	}
}

type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Token returns a valid installation access token for installationID,
// reusing a cached token when it is still fresh and otherwise generating a
// new App JWT and exchanging it with GitHub for a fresh installation token.
func (c *InstallationTokenCache) Token(installationID string) (string, error) {
	if installationID == "" {
		return "", fmt.Errorf("github: installationID must not be empty")
	}

	c.mu.RLock()
	entry, ok := c.entries[installationID]
	c.mu.RUnlock()
	if ok && entry.fresh() {
		return entry.token, nil
	}

	// singleflight.Do collapses concurrent callers keyed on installationID
	// into one in-flight fetch: if two goroutines both observe a stale/missing
	// entry above, only the first actually reaches fetchInstallationToken —
	// the rest block here and share its result, avoiding redundant JWT
	// signing + HTTP calls against GitHub.
	v, err, _ := c.group.Do(installationID, func() (interface{}, error) {
		// Re-check freshness now that we hold the singleflight slot: another
		// goroutine may have just completed a fetch and populated the cache
		// while we were waiting to enter this function.
		c.mu.RLock()
		e, ok := c.entries[installationID]
		c.mu.RUnlock()
		if ok && e.fresh() {
			return e, nil
		}

		e, err := c.fetchInstallationToken(installationID)
		if err != nil {
			return installationTokenEntry{}, err
		}

		c.mu.Lock()
		if c.entries == nil {
			c.entries = make(map[string]installationTokenEntry)
		}
		c.entries[installationID] = e
		c.mu.Unlock()

		return e, nil
	})
	if err != nil {
		return "", err
	}

	return v.(installationTokenEntry).token, nil
}

func (c *InstallationTokenCache) fetchInstallationToken(installationID string) (installationTokenEntry, error) {
	appJWT, err := generateAppJWT(c.AppID, c.PrivateKeyPEM)
	if err != nil {
		return installationTokenEntry{}, err
	}

	url := fmt.Sprintf(installationTokenURLFormat, installationID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return installationTokenEntry{}, fmt.Errorf("github: build installation token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return installationTokenEntry{}, fmt.Errorf("github: installation token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return installationTokenEntry{}, fmt.Errorf("github: read installation token response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return installationTokenEntry{}, fmt.Errorf("github: installation token exchange failed: status %d: %s", resp.StatusCode, string(body))
	}

	var parsed installationTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return installationTokenEntry{}, fmt.Errorf("github: parse installation token response: %w", err)
	}

	return installationTokenEntry{token: parsed.Token, expiresAt: parsed.ExpiresAt}, nil
}

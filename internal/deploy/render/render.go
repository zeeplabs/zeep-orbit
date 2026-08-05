package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/deploy"
)

const renderAPIBase = "https://api.render.com/v1"

type Client struct {
	apiKey     string
	httpClient *http.Client
	// baseURL defaults to renderAPIBase; overridable in tests to point at an
	// httptest.Server instead of the real Render API.
	baseURL string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: renderAPIBase,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, []byte, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("render: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("render: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("render: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("render: read body: %w", err)
	}

	return resp, respBody, nil
}

type ownerResponse struct {
	Owner struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"owner"`
}

type environmentEntry struct {
	Environment struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ProjectID string `json:"projectId"`
	} `json:"environment"`
}

// resolveEnvironmentID looks up the single Environment belonging to projectID.
// The Render API assigns new services to an Environment, not directly to a
// Project — a Project ID stored in deploy config (e.g. "prj-...") cannot be
// sent as-is on service creation, it has to be translated to that Environment's
// ID first. Errors out if the project has zero or more than one environment,
// since there is nothing in our config to disambiguate which one to use.
func resolveEnvironmentID(ctx context.Context, client *Client, projectID string) (string, error) {
	resp, body, err := client.do(ctx, http.MethodGet, "/environments?projectId="+projectID, nil)
	if err != nil {
		return "", fmt.Errorf("render: fetch environments: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("render: fetch environments: %d: %s", resp.StatusCode, string(body))
	}
	var envs []environmentEntry
	if err := json.Unmarshal(body, &envs); err != nil {
		return "", fmt.Errorf("render: parse environments: %w", err)
	}
	if len(envs) == 0 {
		return "", fmt.Errorf("render: project %q has no environments", projectID)
	}
	if len(envs) > 1 {
		return "", fmt.Errorf("render: project %q has %d environments, expected exactly 1 — configure which environment to deploy into", projectID, len(envs))
	}
	return envs[0].Environment.ID, nil
}

func ValidateAPIKey(ctx context.Context, apiKey string) error {
	client := NewClient(apiKey)
	resp, body, err := client.do(ctx, http.MethodGet, "/owners", nil)
	if err != nil {
		return fmt.Errorf("render: validate api key: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var owners []ownerResponse
		if err := json.Unmarshal(body, &owners); err != nil {
			return fmt.Errorf("render: parse owners: %w", err)
		}
		if len(owners) == 0 {
			return fmt.Errorf("render: api key is valid but no owners found")
		}
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("render: invalid api key")
	}
	return fmt.Errorf("render: unexpected status %d: %s", resp.StatusCode, string(body))
}

type RenderProvider struct {
	client        *Client
	ownerID       string
	environmentID string
}

// NewRenderProvider builds a provider scoped to the given API key. If
// environmentID is set, it's used as-is. Otherwise, if projectID is set, its
// Environment is resolved automatically (only works for projects with
// exactly one Environment — see resolveEnvironmentID). Both empty means
// services are created in the workspace's default location.
func NewRenderProvider(ctx context.Context, apiKey, projectID, environmentID string) (*RenderProvider, error) {
	client := NewClient(apiKey)

	resp, body, err := client.do(ctx, http.MethodGet, "/owners", nil)
	if err != nil {
		return nil, fmt.Errorf("render: fetch owner: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("render: fetch owner: %d: %s", resp.StatusCode, string(body))
	}
	var owners []ownerResponse
	if err := json.Unmarshal(body, &owners); err != nil {
		return nil, fmt.Errorf("render: parse owners: %w", err)
	}
	if len(owners) == 0 {
		return nil, fmt.Errorf("render: no owners found for this api key")
	}

	if environmentID == "" && projectID != "" {
		environmentID, err = resolveEnvironmentID(ctx, client, projectID)
		if err != nil {
			return nil, err
		}
	}

	return &RenderProvider{
		client:        client,
		ownerID:       owners[0].Owner.ID,
		environmentID: environmentID,
	}, nil
}

func toEnvVarList(envVars map[string]string) []map[string]string {
	if envVars == nil {
		return nil
	}
	var list []map[string]string
	for k, v := range envVars {
		list = append(list, map[string]string{"key": k, "value": v})
	}
	return list
}

type createServiceRequest struct {
	Type           string              `json:"type"`
	Name           string              `json:"name"`
	OwnerID        string              `json:"ownerId"`
	EnvironmentID  string              `json:"environmentId,omitempty"`
	Repo           string              `json:"repo"`
	AutoDeploy     string              `json:"autoDeploy"`
	ServiceDetails interface{}         `json:"serviceDetails"`
	EnvVars        []map[string]string `json:"envVars,omitempty"`
}

type staticSiteDetails struct {
	BuildCommand string `json:"buildCommand"`
	PublishPath  string `json:"publishPath"`
}

type webServiceDetails struct {
	BuildCommand string `json:"buildCommand"`
	StartCommand string `json:"startCommand"`
}

type createServiceResponse struct {
	Service struct {
		ID             string `json:"id"`
		ServiceSlug    string `json:"slug"`
		ServiceDetails struct {
			URL string `json:"url"`
		} `json:"serviceDetails"`
	} `json:"service"`
}

func (p *RenderProvider) CreateService(ctx context.Context, params deploy.CreateServiceParams) (deploy.ServiceInfo, error) {
	repoURL := fmt.Sprintf("https://github.com/%s/%s", params.RepoOwner, params.RepoName)

	var serviceDetails interface{}
	if params.ServiceType == "static_site" {
		serviceDetails = staticSiteDetails{
			BuildCommand: params.BuildCommand,
			PublishPath:  params.PublishPath,
		}
	} else if params.ServiceType == "web_service" {
		serviceDetails = webServiceDetails{
			BuildCommand: params.BuildCommand,
			StartCommand: params.StartCommand,
		}
	} else {
		return deploy.ServiceInfo{}, fmt.Errorf("render: unknown service type %q, expected static_site or web_service", params.ServiceType)
	}

	req := createServiceRequest{
		Type:           params.ServiceType,
		Name:           params.RepoName,
		OwnerID:        p.ownerID,
		EnvironmentID:  p.environmentID,
		Repo:           repoURL,
		AutoDeploy:     "yes",
		ServiceDetails: serviceDetails,
		EnvVars:        toEnvVarList(params.EnvVars),
	}
	reqJSON, _ := json.Marshal(req)

	// The repo backing this service was typically created via the GitHub API
	// moments earlier. Render's own GitHub App integration can take a few
	// seconds to notice a brand-new repo, which surfaces here as a transient
	// 404 ("repo not found") or 500 ("internal server error") even though the
	// repo and the App's access to it are both fine. Retry those with a short
	// backoff before giving up; anything else (400 name conflict, 429 rate
	// limit, ...) is not a sync race and fails immediately.
	var resp *http.Response
	var body []byte
	const maxAttempts = 4
	backoff := 2 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var err error
		resp, body, err = p.client.do(ctx, http.MethodPost, "/services", req)
		if err != nil {
			return deploy.ServiceInfo{}, err
		}
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusInternalServerError {
			break
		}
		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return deploy.ServiceInfo{}, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	if resp.StatusCode == http.StatusCreated {
		var parsed createServiceResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return deploy.ServiceInfo{}, fmt.Errorf("render: parse create service response: %w", err)
		}
		info := deploy.ServiceInfo{
			ServiceID: parsed.Service.ID,
			URL:       parsed.Service.ServiceDetails.URL,
		}

		return info, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return deploy.ServiceInfo{}, fmt.Errorf("render: repo not found — ensure the Render GitHub App is installed with access to all repositories")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return deploy.ServiceInfo{}, fmt.Errorf("render: rate limit reached, try again later")
	}

	return deploy.ServiceInfo{}, fmt.Errorf("render: create service failed (status %d): sent=%s response=%s", resp.StatusCode, string(reqJSON), string(body))
}

func (p *RenderProvider) DeleteService(ctx context.Context, serviceID string) error {
	resp, body, err := p.client.do(ctx, http.MethodDelete, "/services/"+serviceID, nil)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil // already deleted — not an error for best-effort
	}
	return fmt.Errorf("render: delete service failed (status %d): %s", resp.StatusCode, string(body))
}

// Deploy is one entry in a service's deploy history, as returned by
// GET /services/{id}/deploys.
type Deploy struct {
	ID         string
	Status     string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

type deployListEntry struct {
	Deploy struct {
		ID         string     `json:"id"`
		Status     string     `json:"status"`
		CreatedAt  time.Time  `json:"createdAt"`
		FinishedAt *time.Time `json:"finishedAt"`
	} `json:"deploy"`
}

// ListDeploys returns up to limit recent deploys for serviceID, filtered to
// statuses. Read-only — used to populate the dashboard's "Recent Deploys"
// list, no side effects on Render.
func (c *Client) ListDeploys(ctx context.Context, serviceID string, limit int, statuses []string) ([]Deploy, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	for _, s := range statuses {
		q.Add("status", s)
	}

	resp, body, err := c.do(ctx, http.MethodGet, "/services/"+serviceID+"/deploys?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("render: list deploys: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("render: list deploys failed (status %d): %s", resp.StatusCode, string(body))
	}

	var entries []deployListEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("render: parse deploys: %w", err)
	}

	deploys := make([]Deploy, 0, len(entries))
	for _, e := range entries {
		deploys = append(deploys, Deploy{
			ID:         e.Deploy.ID,
			Status:     e.Deploy.Status,
			CreatedAt:  e.Deploy.CreatedAt,
			FinishedAt: e.Deploy.FinishedAt,
		})
	}
	return deploys, nil
}

func (p *RenderProvider) AddCustomDomain(ctx context.Context, serviceID, domain string) error {
	resp, body, err := p.client.do(ctx, http.MethodPost, "/services/"+serviceID+"/custom-domains", map[string]string{
		"name": domain,
	})
	if err != nil {
		return err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("render: add custom domain failed (status %d): %s", resp.StatusCode, string(body))
}

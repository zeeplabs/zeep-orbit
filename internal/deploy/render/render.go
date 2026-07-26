package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/deploy"
)

const renderAPIBase = "https://api.render.com/v1"

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, []byte, error) {
	url := renderAPIBase + path

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
	ID   string `json:"id"`
	Name string `json:"name"`
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
	client  *Client
	ownerID string
}

func NewRenderProvider(ctx context.Context, apiKey string) (*RenderProvider, error) {
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

	return &RenderProvider{
		client:  client,
		ownerID: owners[0].ID,
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
	Type           string             `json:"type"`
	Name           string             `json:"name"`
	OwnerID        string             `json:"ownerId"`
	Repo           string             `json:"repo"`
	AutoDeploy     string             `json:"autoDeploy"`
	ServiceDetails interface{}        `json:"serviceDetails"`
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
		ID           string `json:"id"`
		ServiceSlug  string `json:"slug"`
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
		Repo:           repoURL,
		AutoDeploy:     "yes",
		ServiceDetails: serviceDetails,
		EnvVars:        toEnvVarList(params.EnvVars),
	}

	resp, body, err := p.client.do(ctx, http.MethodPost, "/services", req)
	if err != nil {
		return deploy.ServiceInfo{}, err
	}

	if resp.StatusCode == http.StatusCreated {
		var parsed createServiceResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return deploy.ServiceInfo{}, fmt.Errorf("render: parse create service response: %w", err)
		}
		return deploy.ServiceInfo{
			ServiceID: parsed.Service.ID,
			URL:       parsed.Service.ServiceDetails.URL,
		}, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return deploy.ServiceInfo{}, fmt.Errorf("render: repo not found — ensure the Render GitHub App is installed with access to all repositories")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return deploy.ServiceInfo{}, fmt.Errorf("render: rate limit reached, try again later")
	}

	return deploy.ServiceInfo{}, fmt.Errorf("render: create service failed (status %d): %s", resp.StatusCode, string(body))
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

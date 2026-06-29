package orbit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	config ClientConfig
	http   *http.Client
}

func New(cfg ClientConfig) *Client {
	return &Client{
		config: cfg,
		http:   &http.Client{},
	}
}

func (c *Client) url(path string) string {
	return fmt.Sprintf("%s/%s/%s", c.config.BaseURL, c.config.App, strings.TrimPrefix(path, "/"))
}

func (c *Client) request(method, path string, body, dest any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("orbit: marshal: %w", err)
		}
		r = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.url(path), r)
	if err != nil {
		return fmt.Errorf("orbit: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.JWT)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("orbit: do: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNoContent {
		return nil
	}

	if res.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("orbit: %s", errResp.Error)
		}
		return fmt.Errorf("orbit: HTTP %d", res.StatusCode)
	}

	if dest != nil {
		return json.NewDecoder(res.Body).Decode(dest)
	}
	return nil
}

func (c *Client) Table(name string) *TableClient {
	return &TableClient{client: c, table: name}
}

func (c *Client) Auth() *AuthClient {
	return &AuthClient{client: c}
}

func (c *Client) Files() *FilesClient {
	return &FilesClient{client: c}
}

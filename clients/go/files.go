package orbit

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

type FilesClient struct {
	client *Client
}

func (f *FilesClient) path(p string) string {
	return "files/" + strings.TrimPrefix(p, "/")
}

func (f *FilesClient) Upload(filename string, body io.Reader, mimeType string) (*FileResponse, error) {
	var b strings.Builder
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("orbit: create form file: %w", err)
	}
	if _, err := io.Copy(fw, body); err != nil {
		return nil, fmt.Errorf("orbit: copy file: %w", err)
	}
	w.Close()

	req, err := http.NewRequest("POST", f.client.url(f.path("")), strings.NewReader(b.String()))
	if err != nil {
		return nil, fmt.Errorf("orbit: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.client.config.JWT)
	req.Header.Set("Content-Type", w.FormDataContentType())

	res, err := f.client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orbit: upload do: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("orbit: upload: HTTP %d", res.StatusCode)
	}

	var file FileResponse
	if err := json.NewDecoder(res.Body).Decode(&file); err != nil {
		return nil, fmt.Errorf("orbit: decode upload: %w", err)
	}
	return &file, nil
}

func (f *FilesClient) List(limit, offset int) ([]FileResponse, error) {
	var files []FileResponse
	err := f.client.request("GET", f.path(fmt.Sprintf("?limit=%d&offset=%d", limit, offset)), nil, &files)
	return files, err
}

func (f *FilesClient) Get(id string) (*FileResponse, error) {
	var file FileResponse
	err := f.client.request("GET", f.path(id), nil, &file)
	return &file, err
}

func (f *FilesClient) Delete(id string) error {
	return f.client.request("DELETE", f.path(id), nil, nil)
}

func (f *FilesClient) SignedURL(id string, ttl int) (string, error) {
	var resp SignedURLResponse
	err := f.client.request("GET", f.path(fmt.Sprintf("%s/url?ttl=%d", id, ttl)), nil, &resp)
	return resp.URL, err
}

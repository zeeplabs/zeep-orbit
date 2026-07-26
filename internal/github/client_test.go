package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a Client wired to a mock HTTP transport and a fake
// installation token cache that always succeeds without real JWT signing or
// network calls, so tests exercise only the client.go logic under test.
func newTestClient(t *testing.T, handler func(req *http.Request) (*http.Response, error)) *Client {
	t.Helper()

	keyPEM := generateTestPrivateKeyPEM(t)
	tokenClient := newMockClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusCreated, `{"token":"test-installation-token","expires_at":"`+
			time.Now().Add(1*time.Hour).Format(time.RFC3339)+`"}`), nil
	})

	return &Client{
		tokens: &InstallationTokenCache{
			AppID:         "app-1",
			PrivateKeyPEM: keyPEM,
			HTTPClient:    tokenClient,
		},
		installationID: "install-1",
		httpClient:     newMockClient(handler),
	}
}

func TestVerifyTemplateRepo_OK(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", req.Method)
		}
		if want := "https://api.github.com/repos/acme/starter"; req.URL.String() != want {
			t.Errorf("url = %q, want %q", req.URL.String(), want)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-installation-token" {
			t.Errorf("Authorization header = %q", got)
		}
		if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept header = %q", got)
		}
		if got := req.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
			t.Errorf("X-GitHub-Api-Version header = %q", got)
		}
		return mockJSONResponse(http.StatusOK, `{"is_template":true}`), nil
	})

	if err := client.VerifyTemplateRepo(context.Background(), "acme", "starter"); err != nil {
		t.Fatalf("VerifyTemplateRepo() error = %v, want nil", err)
	}
}

func TestVerifyTemplateRepo_NotTemplate(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusOK, `{"is_template":false}`), nil
	})

	err := client.VerifyTemplateRepo(context.Background(), "acme", "starter")
	if err == nil {
		t.Fatal("expected error for non-template repository, got nil")
	}
	if !strings.Contains(err.Error(), "not a template repository") {
		t.Errorf("error = %q, want mention of 'not a template repository'", err.Error())
	}
}

func TestVerifyTemplateRepo_NotFound(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
	})

	err := client.VerifyTemplateRepo(context.Background(), "acme", "missing")
	if err == nil {
		t.Fatal("expected error for missing repository, got nil")
	}
	if !strings.Contains(err.Error(), "not found or not accessible") {
		t.Errorf("error = %q, want mention of 'not found or not accessible'", err.Error())
	}
	// Must not leak the raw GitHub response body.
	if strings.Contains(err.Error(), "Not Found") {
		t.Errorf("error leaked raw GitHub body: %q", err.Error())
	}
}

func TestVerifyTemplateRepo_ForbiddenPermissions(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		resp := mockJSONResponse(http.StatusForbidden, `{"message":"Forbidden"}`)
		// No X-RateLimit-Remaining header set: genuine permissions error, not
		// a rate limit.
		return resp, nil
	})

	err := client.VerifyTemplateRepo(context.Background(), "acme", "starter")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected genuine permissions error, got rate-limit error: %v", err)
	}
	if !strings.Contains(err.Error(), "insufficient permissions") {
		t.Errorf("error = %q, want mention of insufficient permissions", err.Error())
	}
}

func TestVerifyTemplateRepo_RateLimited(t *testing.T) {
	resetAt := time.Now().Add(30 * time.Minute).Unix()

	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		resp := mockJSONResponse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
		resp.Header.Set("X-RateLimit-Remaining", "0")
		resp.Header.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
		return resp, nil
	})

	err := client.VerifyTemplateRepo(context.Background(), "acme", "starter")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected errors.Is(err, ErrRateLimited) to be true, err = %v", err)
	}

	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected errors.As to extract *RateLimitError, err = %v", err)
	}
	if rlErr.ResetAt.Unix() != resetAt {
		t.Errorf("ResetAt = %v, want unix %d", rlErr.ResetAt, resetAt)
	}
}

func TestCreateRepoFromTemplate_OK(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", req.Method)
		}
		want := "https://api.github.com/repos/acme/starter/generate"
		if req.URL.String() != want {
			t.Errorf("url = %q, want %q", req.URL.String(), want)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q, want application/json", got)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		bodyStr := string(body)
		if !strings.Contains(bodyStr, `"name":"new-repo"`) {
			t.Errorf("request body = %q, want it to contain name field", bodyStr)
		}
		if !strings.Contains(bodyStr, `"private":true`) {
			t.Errorf("request body = %q, want private:true", bodyStr)
		}

		return mockJSONResponse(http.StatusCreated, `{"html_url":"https://github.com/acme/new-repo","url":"https://api.github.com/repos/acme/new-repo"}`), nil
	})

	url, err := client.CreateRepoFromTemplate(context.Background(), "acme", "starter", "new-repo")
	if err != nil {
		t.Fatalf("CreateRepoFromTemplate() error = %v, want nil", err)
	}
	if want := "https://github.com/acme/new-repo"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestCreateRepoFromTemplate_Conflict(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusUnprocessableEntity, `{"message":"Repository creation failed.","errors":[{"message":"name already exists on this account"}]}`), nil
	})

	url, err := client.CreateRepoFromTemplate(context.Background(), "acme", "starter", "existing-repo")
	if err == nil {
		t.Fatalf("expected error for name conflict, got url = %q", url)
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want mention of already exists", err.Error())
	}
}

func TestCreateRepoFromTemplate_RateLimited(t *testing.T) {
	resetAt := time.Now().Add(10 * time.Minute).Unix()

	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		resp := mockJSONResponse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
		resp.Header.Set("X-RateLimit-Remaining", "0")
		resp.Header.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
		return resp, nil
	})

	_, err := client.CreateRepoFromTemplate(context.Background(), "acme", "starter", "new-repo")
	if err == nil {
		t.Fatal("expected rate-limit error, got nil")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected errors.Is(err, ErrRateLimited) to be true, err = %v", err)
	}
}

func TestCreateRepoFromTemplate_ForbiddenPermissions(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		// No rate-limit headers: this is a genuine permissions error.
		return mockJSONResponse(http.StatusForbidden, `{"message":"Resource not accessible by integration"}`), nil
	})

	_, err := client.CreateRepoFromTemplate(context.Background(), "acme", "starter", "new-repo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected genuine permissions error, got rate-limit error: %v", err)
	}
	if !strings.Contains(err.Error(), "Administration") {
		t.Errorf("error = %q, want actionable mention of Administration permission", err.Error())
	}
}

func TestStatus_Connected(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		t.Fatal("Status() should not make a repository HTTP call")
		return nil, nil
	})

	result, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if !result.Connected {
		t.Errorf("Connected = false, want true; Error = %q", result.Error)
	}
}

func TestStatus_TokenExchangeFails(t *testing.T) {
	keyPEM := generateTestPrivateKeyPEM(t)
	tokenClient := newMockClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusUnauthorized, `{"message":"Bad credentials"}`), nil
	})

	client := &Client{
		tokens: &InstallationTokenCache{
			AppID:         "app-1",
			PrivateKeyPEM: keyPEM,
			HTTPClient:    tokenClient,
		},
		installationID: "install-1",
		httpClient:     http.DefaultClient,
	}

	result, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v, want nil (error reported in result)", err)
	}
	if result.Connected {
		t.Error("Connected = true, want false when token exchange fails")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error message when token exchange fails")
	}
}

package github

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// rewriteTransport sends every request to the httptest server at target while
// keeping the request's original URL (host, path) visible to the server through
// X-Original-Host. http.Client still makes its redirect and header-stripping
// decisions on the original hosts (api.github.com vs codeload.github.com).
type rewriteTransport struct {
	target *url.URL
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("X-Original-Host", req.URL.Host)
	r.URL.Scheme = rt.target.Scheme
	r.URL.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func TestDownloadTarball_FollowsRedirectOverHTTP(t *testing.T) {
	payload := []byte("fake-gzip-tarball-bytes")
	var apiAuth, cdnAuth string
	var apiPath, cdnPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Original-Host") {
		case "api.github.com":
			apiAuth, apiPath = r.Header.Get("Authorization"), r.URL.Path
			http.Redirect(w, r, "https://codeload.github.com/acme/starter/legacy.tar.gz/refs/heads/main", http.StatusFound)
		case "codeload.github.com":
			cdnAuth, cdnPath = r.Header.Get("Authorization"), r.URL.Path
			w.Header().Set("Content-Type", "application/x-gzip")
			_, _ = w.Write(payload)
		default:
			http.Error(w, "unexpected host", http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	target, _ := url.Parse(srv.URL)
	client := newTestClient(t, nil)
	client.httpClient = &http.Client{Transport: rewriteTransport{target: target}}

	rc, err := client.DownloadTarball(context.Background(), "acme", "starter", "main")
	if err != nil {
		t.Fatalf("DownloadTarball() error = %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read tarball: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("body = %q, want %q", got, payload)
	}
	if apiPath != "/repos/acme/starter/tarball/main" {
		t.Errorf("API path = %q", apiPath)
	}
	if apiAuth != "Bearer test-installation-token" {
		t.Errorf("the API request must carry the installation token, got %q", apiAuth)
	}
	if cdnPath != "/acme/starter/legacy.tar.gz/refs/heads/main" {
		t.Errorf("redirect target path = %q", cdnPath)
	}
	if cdnAuth != "" {
		t.Errorf("the installation token must not follow the redirect to the download host, got %q", cdnAuth)
	}
}

func TestDownloadTarball_DefaultBranchAndRefEscaping(t *testing.T) {
	var paths []string
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.EscapedPath())
		return mockJSONResponse(http.StatusOK, "x"), nil
	})

	for _, ref := range []string{"", "feature/new-ui", "v1.2.3+build", "0123456789abcdef0123456789abcdef01234567"} {
		rc, err := client.DownloadTarball(context.Background(), "acme", "starter.app", ref)
		if err != nil {
			t.Fatalf("ref %q: %v", ref, err)
		}
		rc.Close()
	}
	want := []string{
		"/repos/acme/starter.app/tarball",
		"/repos/acme/starter.app/tarball/feature/new-ui",
		"/repos/acme/starter.app/tarball/v1.2.3+build",
		"/repos/acme/starter.app/tarball/0123456789abcdef0123456789abcdef01234567",
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestDownloadTarball_RejectsUnsafeInputsWithoutRequest(t *testing.T) {
	called := false
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		called = true
		return mockJSONResponse(http.StatusOK, "x"), nil
	})
	cases := []struct{ owner, repo, ref string }{
		{"", "repo", ""},
		{"ac/me", "repo", ""},
		{"acme", "", ""},
		{"acme", "..", ""},
		{"acme", "re/po", ""},
		{"acme", "repo?x=1", ""},
		{"acme", "repo", "../../etc"},
		{"acme", "repo", "a//b"},
		{"acme", "repo", "main?x=1"},
		{"acme", "repo", "main#frag"},
		{"acme", "repo", "has space"},
		{"-acme", "repo", ""},
	}
	for _, c := range cases {
		if rc, err := client.DownloadTarball(context.Background(), c.owner, c.repo, c.ref); err == nil {
			rc.Close()
			t.Errorf("DownloadTarball(%q, %q, %q): expected an error", c.owner, c.repo, c.ref)
		}
	}
	if called {
		t.Error("no request may be sent for invalid input")
	}
}

func TestDownloadTarball_NotFoundDoesNotLeakBody(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusNotFound, `{"message":"Not Found","documentation_url":"https://docs.github.com"}`), nil
	})
	_, err := client.DownloadTarball(context.Background(), "acme", "missing", "main")
	if err == nil || !strings.Contains(err.Error(), "not found or not accessible") {
		t.Fatalf("error = %v, want not found", err)
	}
	if strings.Contains(err.Error(), "documentation_url") {
		t.Errorf("error leaked the GitHub body: %q", err)
	}
}

func TestDownloadTarball_RateLimitedAndPermissionAndServerError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		remain string
		check  func(error) bool
	}{
		{"rate limited 403", http.StatusForbidden, "0", func(e error) bool { return errors.Is(e, ErrRateLimited) }},
		{"rate limited 429", http.StatusTooManyRequests, "0", func(e error) bool { return errors.Is(e, ErrRateLimited) }},
		{"permission 403", http.StatusForbidden, "", func(e error) bool {
			return !errors.Is(e, ErrRateLimited) && strings.Contains(e.Error(), "insufficient permissions")
		}},
		{"server error", http.StatusBadGateway, "", func(e error) bool { return strings.Contains(e.Error(), "502") }},
	}
	for _, tc := range cases {
		client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
			resp := mockJSONResponse(tc.status, `{"message":"x"}`)
			if tc.remain != "" {
				resp.Header.Set("X-RateLimit-Remaining", tc.remain)
				resp.Header.Set("X-RateLimit-Reset", "1900000000")
			}
			return resp, nil
		})
		_, err := client.DownloadTarball(context.Background(), "acme", "starter", "")
		if err == nil || !tc.check(err) {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
	}
}

func TestDownloadTarball_DeclaredSizeOverCapRejectedUpFront(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		resp := mockJSONResponse(http.StatusOK, "ignored")
		resp.ContentLength = MaxTarballBytes + 1
		resp.Header.Set("Content-Length", strconv.FormatInt(MaxTarballBytes+1, 10))
		return resp, nil
	})
	rc, err := client.DownloadTarball(context.Background(), "acme", "starter", "")
	if !errors.Is(err, ErrTarballTooLarge) {
		if rc != nil {
			rc.Close()
		}
		t.Fatalf("error = %v, want ErrTarballTooLarge", err)
	}
}

func TestCappedReadCloser_CutsStreamPastCapNotTruncatedSilently(t *testing.T) {
	// Exactly at the cap: fine.
	c := &cappedReadCloser{rc: io.NopCloser(bytes.NewReader(make([]byte, 100))), remaining: 100}
	n, err := io.Copy(io.Discard, c)
	if err != nil || n != 100 {
		t.Fatalf("at the cap: n=%d err=%v, want 100 bytes and no error", n, err)
	}

	// One byte over: the error surfaces, and no more than the cap is delivered.
	c = &cappedReadCloser{rc: io.NopCloser(bytes.NewReader(make([]byte, 101))), remaining: 100}
	n, err = io.Copy(io.Discard, c)
	if !errors.Is(err, ErrTarballTooLarge) {
		t.Fatalf("over the cap: err = %v, want ErrTarballTooLarge", err)
	}
	if n != 100 {
		t.Errorf("delivered %d bytes, want exactly the cap (100)", n)
	}
	if _, err := c.Read(make([]byte, 8)); !errors.Is(err, ErrTarballTooLarge) {
		t.Errorf("a read after the cap must keep failing, got %v", err)
	}
}

func TestDownloadTarball_StreamingOverCapSurfacesOnRead(t *testing.T) {
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		// No Content-Length: the size is only known by reading.
		resp := mockJSONResponse(http.StatusOK, "")
		resp.ContentLength = -1
		resp.Body = io.NopCloser(io.LimitReader(zeroReader{}, MaxTarballBytes+10))
		return resp, nil
	})
	rc, err := client.DownloadTarball(context.Background(), "acme", "starter", "")
	if err != nil {
		t.Fatalf("DownloadTarball: %v", err)
	}
	defer rc.Close()
	n, err := io.Copy(io.Discard, rc)
	if !errors.Is(err, ErrTarballTooLarge) || n != MaxTarballBytes {
		t.Fatalf("n=%d err=%v, want %d bytes then ErrTarballTooLarge", n, err, int64(MaxTarballBytes))
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

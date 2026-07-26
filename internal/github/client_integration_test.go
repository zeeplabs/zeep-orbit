// This file contains a one-off spike test (T-04) that exercises the real
// GitHub API against a sandbox GitHub App installation. It validates a
// design assumption marked [Provável] in design.md: that an installation
// with repository_selection: selected automatically gains access to a repo
// created via CreateRepoFromTemplate, without any human re-authorization
// step. It is skipped unless the sandbox credentials are present in the
// environment (see the GITHUB_INTEGRATION_* vars below).
package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestInstallationAutoAccess validates that the same cached installation
// token used to create a repo from a template can immediately access the
// newly created repo's contents endpoint, with no re-authorization step in
// between. This is a live-API spike, not a permanent regression test: it is
// skipped unless the sandbox environment variables below are all set.
func TestInstallationAutoAccess(t *testing.T) {
	appID := os.Getenv("GITHUB_INTEGRATION_APP_ID")
	installationID := os.Getenv("GITHUB_INTEGRATION_INSTALLATION_ID")
	privateKeyPath := os.Getenv("GITHUB_INTEGRATION_PRIVATE_KEY_PATH")
	templateOwner := os.Getenv("GITHUB_INTEGRATION_TEMPLATE_OWNER")
	templateRepo := os.Getenv("GITHUB_INTEGRATION_TEMPLATE_REPO")

	if appID == "" || installationID == "" || privateKeyPath == "" || templateOwner == "" || templateRepo == "" {
		t.Skip("GITHUB_INTEGRATION_* env vars not set")
	}

	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		t.Fatalf("read private key file: %v", err)
	}

	client := NewClient(AppConfig{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKeyPEM:  privateKeyPEM,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	newRepoSlug := fmt.Sprintf("zeep-orbit-t04-spike-%d", time.Now().UnixNano())

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()

		deleteURL := fmt.Sprintf("%s/repos/%s/%s", githubAPIBaseURL, templateOwner, newRepoSlug)
		resp, body, err := client.doAuthenticated(cleanupCtx, http.MethodDelete, deleteURL, nil)
		if err != nil {
			t.Logf("cleanup: delete repo %s/%s failed: %v", templateOwner, newRepoSlug, err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Logf("cleanup: delete repo %s/%s returned status %d: %s", templateOwner, newRepoSlug, resp.StatusCode, string(body))
		}
	})

	repoURL, err := client.CreateRepoFromTemplate(ctx, templateOwner, templateRepo, newRepoSlug)
	if err != nil {
		t.Fatalf("CreateRepoFromTemplate: %v", err)
	}
	if repoURL == "" {
		t.Fatal("CreateRepoFromTemplate: returned empty repo URL")
	}
	t.Logf("created repo: %s", repoURL)

	// The actual assertion under test: without any new installation/re-auth
	// step, the SAME client (same cached installation token) must be able to
	// access the newly created repo. GitHub's template-generate endpoint
	// returns 201 before the template's file contents finish copying into the
	// new repo, so /contents can transiently 404 with "This repository is
	// empty" for a few seconds even when access is already granted — that is
	// a population-lag response, not an access-denied one (an installation
	// truly lacking access gets a bare 404 "Not Found", never the "empty"
	// detail, since GitHub hides repo existence from unauthorized callers).
	// A 403 at any point, by contrast, is unambiguous: access was denied.
	//
	// GET /repos/{owner}/{repo} (no /contents) has no such lag: it reflects
	// the installation's access to the repo itself, independent of whether
	// template content has finished copying, so it is checked first as an
	// immediate, unambiguous access signal.
	repoURLCheck := fmt.Sprintf("%s/repos/%s/%s", githubAPIBaseURL, templateOwner, newRepoSlug)
	repoResp, repoBody, err := client.doAuthenticated(ctx, http.MethodGet, repoURLCheck, nil)
	if err != nil {
		t.Fatalf("GET repo metadata on newly created repo: %v", err)
	}
	repoResp.Body.Close()
	if repoResp.StatusCode == http.StatusForbidden {
		t.Fatalf("EMPIRICAL RESULT: installation did NOT get automatic access to the generated repo — "+
			"GET %s returned 403: %s", newRepoSlug, string(repoBody))
	}
	if repoResp.StatusCode != http.StatusOK {
		t.Fatalf("GET repo metadata on newly created repo: unexpected status %d: %s", repoResp.StatusCode, string(repoBody))
	}
	t.Logf("immediate access confirmed: GET /repos/%s/%s returned 200", templateOwner, newRepoSlug)

	contentsURL := fmt.Sprintf("%s/repos/%s/%s/contents", githubAPIBaseURL, templateOwner, newRepoSlug)
	const (
		maxAttempts = 8
		pollDelay   = 2 * time.Second
	)

	var (
		lastStatus int
		lastBody   []byte
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, body, err := client.doAuthenticated(ctx, http.MethodGet, contentsURL, nil)
		if err != nil {
			t.Fatalf("GET contents on newly created repo (attempt %d): %v", attempt, err)
		}
		resp.Body.Close()
		lastStatus, lastBody = resp.StatusCode, body

		if resp.StatusCode == http.StatusForbidden {
			t.Fatalf("EMPIRICAL RESULT: installation did NOT get automatic access to the generated repo — "+
				"GET %s/contents returned 403 on attempt %d: %s", newRepoSlug, attempt, string(body))
		}
		if resp.StatusCode == http.StatusOK {
			t.Logf("EMPIRICAL RESULT: installation got automatic access to the generated repo — "+
				"GET contents returned 200 on attempt %d (no re-authorization step performed)", attempt)
			return
		}

		t.Logf("attempt %d: GET contents returned %d (template content likely still copying), retrying: %s",
			attempt, resp.StatusCode, string(body))
		time.Sleep(pollDelay)
	}

	t.Fatalf("GET contents on newly created repo never returned 200 after %d attempts; last status %d: %s",
		maxAttempts, lastStatus, string(lastBody))
}

package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// githubTemplatesHandlerTestPool provisions the full zeep_system schema and
// truncates the tables this test file touches before every test.
func githubTemplatesHandlerTestPool(t *testing.T) *db.Pool {
	t.Helper()
	pool := githubHandlerTestPool(t)

	truncate := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.github_templates`)
	}
	truncate()
	t.Cleanup(truncate)

	return pool
}

func newGitHubTemplatesTestHandler(pool *db.Pool, httpClient *http.Client) *GitHubTemplatesHandler {
	return &GitHubTemplatesHandler{pool: pool, httpClient: httpClient}
}

// seedInstalledGitHubConfig persists a GitHub App config with a real
// installation_id, satisfying the "GitHub connected" precondition that
// Create/Update rely on to build an authenticated client.
func seedInstalledGitHubConfig(t *testing.T, pool *db.Pool, keyPEM string) {
	t.Helper()
	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := UpdateGitHubInstallation(context.Background(), pool, 999, "acme-org", time.Now()); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
}

// withURLParam attaches a chi URL param to the request context, mimicking
// what chi's router does when a route like /templates/{id} matches.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func templateRepoResponse(isTemplate bool) string {
	if isTemplate {
		return `{"id":1,"name":"repo","is_template":true}`
	}
	return `{"id":1,"name":"repo","is_template":false}`
}

func TestGitHubTemplates_CreateValidPersistsActive(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)
	seedInstalledGitHubConfig(t, pool, keyPEM)

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "access_tokens") {
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		}
		if !strings.Contains(req.URL.String(), "/repos/acme/template-app") {
			t.Errorf("unexpected request to %s", req.URL.String())
		}
		return mockJSONResp(http.StatusOK, templateRepoResponse(true)), nil
	})
	h := newGitHubTemplatesTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"name":         "My Template",
		"description":  "A great starter",
		"github_owner": "acme",
		"github_repo":  "template-app",
		"framework":    "node",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/templates", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}

	var resp GitHubTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Active {
		t.Error("expected created template to be active")
	}
	if resp.CreatedBy != admin.ID {
		t.Errorf("CreatedBy = %q, want %q", resp.CreatedBy, admin.ID)
	}

	templates, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 persisted template, got %d", len(templates))
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 1 || entries[0].Action != "github.template.create" {
		t.Fatalf("expected 1 github.template.create audit entry, got total=%d entries=%+v", total, entries)
	}
}

func TestGitHubTemplates_CreateNotFoundRepoRejectedNotPersisted(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)
	seedInstalledGitHubConfig(t, pool, keyPEM)

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "access_tokens") {
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		}
		return mockJSONResp(http.StatusNotFound, `{"message":"Not Found"}`), nil
	})
	h := newGitHubTemplatesTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"name":         "My Template",
		"github_owner": "acme",
		"github_repo":  "does-not-exist",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/templates", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	templates, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(templates) != 0 {
		t.Fatalf("expected no templates persisted, got %d", len(templates))
	}
}

func TestGitHubTemplates_CreateNonTemplateRepoRejected(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)
	seedInstalledGitHubConfig(t, pool, keyPEM)

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "access_tokens") {
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		}
		return mockJSONResp(http.StatusOK, templateRepoResponse(false)), nil
	})
	h := newGitHubTemplatesTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"name":         "Not A Template",
		"github_owner": "acme",
		"github_repo":  "regular-repo",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/templates", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if !strings.Contains(resp["error"], "not a template") {
		t.Errorf("error message = %q, want it to mention 'not a template'", resp["error"])
	}

	templates, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(templates) != 0 {
		t.Fatalf("expected no templates persisted, got %d", len(templates))
	}
}

func TestGitHubTemplates_CreateGitHubNotConnectedRejected(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)

	body, _ := json.Marshal(map[string]string{
		"name":         "My Template",
		"github_owner": "acme",
		"github_repo":  "template-app",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/templates", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	templates, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(templates) != 0 {
		t.Fatalf("expected no templates persisted, got %d", len(templates))
	}
}

func TestGitHubTemplates_CreateGitHubConfiguredButNotInstalledRejected(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)

	// Configured but never installed: installation_id is null.
	if err := UpsertGitHubConfig(context.Background(), pool, GitHubAppConfigInput{
		AppID: "12345", AppSlug: "my-app", ClientID: "Iv1.abc", PrivateKey: keyPEM,
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	h := newGitHubTemplatesTestHandler(pool, nil)

	body, _ := json.Marshal(map[string]string{
		"name":         "My Template",
		"github_owner": "acme",
		"github_repo":  "template-app",
	})
	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/templates", strings.NewReader(string(body))), admin)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestGitHubTemplates_CreateNonSuperadminForbidden(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	user := mustCreateRegularUser(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)

	req := withUser(httptest.NewRequest(http.MethodPost, "/api/github/templates", strings.NewReader(`{}`)), user)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestGitHubTemplates_UpdateReVerifiesAndPersists(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)
	seedInstalledGitHubConfig(t, pool, keyPEM)

	created, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: "Old Name", GitHubOwner: "acme", GitHubRepo: "old-repo", Framework: "node", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}

	var lastRepoRequested string
	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "access_tokens") {
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		}
		lastRepoRequested = req.URL.String()
		return mockJSONResp(http.StatusOK, templateRepoResponse(true)), nil
	})
	h := newGitHubTemplatesTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"name":         "New Name",
		"description":  "updated",
		"github_owner": "acme",
		"github_repo":  "new-repo",
		"framework":    "go",
	})
	req := withUser(httptest.NewRequest(http.MethodPut, "/api/github/templates/"+created.ID, strings.NewReader(string(body))), admin)
	req = withURLParam(req, "id", created.ID)
	w := httptest.NewRecorder()
	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(lastRepoRequested, "/repos/acme/new-repo") {
		t.Errorf("expected re-verification against new-repo, got request to %s", lastRepoRequested)
	}

	var resp GitHubTemplate
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp.Name != "New Name" || resp.GitHubRepo != "new-repo" {
		t.Errorf("unexpected updated template: %+v", resp)
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 1 || entries[0].Action != "github.template.update" {
		t.Fatalf("expected 1 github.template.update audit entry, got total=%d entries=%+v", total, entries)
	}
}

func TestGitHubTemplates_UpdateInvalidRepoRejectedNotPersisted(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)
	seedInstalledGitHubConfig(t, pool, keyPEM)

	created, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: "Old Name", GitHubOwner: "acme", GitHubRepo: "old-repo", Framework: "node", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "access_tokens") {
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		}
		return mockJSONResp(http.StatusNotFound, `{"message":"Not Found"}`), nil
	})
	h := newGitHubTemplatesTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"name":         "New Name",
		"github_owner": "acme",
		"github_repo":  "gone-repo",
	})
	req := withUser(httptest.NewRequest(http.MethodPut, "/api/github/templates/"+created.ID, strings.NewReader(string(body))), admin)
	req = withURLParam(req, "id", created.ID)
	w := httptest.NewRecorder()
	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	stored, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(stored) != 1 || stored[0].Name != "Old Name" || stored[0].GitHubRepo != "old-repo" {
		t.Fatalf("expected template unchanged, got %+v", stored)
	}
}

func TestGitHubTemplates_UpdateNotFoundReturns404(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	keyPEM := genTestKeyPEM(t)
	seedInstalledGitHubConfig(t, pool, keyPEM)

	client := mockGitHubClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.String(), "access_tokens") {
			return mockJSONResp(http.StatusCreated, `{"token":"tok","expires_at":"`+time.Now().Add(time.Hour).Format(time.RFC3339)+`"}`), nil
		}
		return mockJSONResp(http.StatusOK, templateRepoResponse(true)), nil
	})
	h := newGitHubTemplatesTestHandler(pool, client)

	body, _ := json.Marshal(map[string]string{
		"name":         "New Name",
		"github_owner": "acme",
		"github_repo":  "some-repo",
	})
	fakeID := "00000000-0000-0000-0000-000000000000"
	req := withUser(httptest.NewRequest(http.MethodPut, "/api/github/templates/"+fakeID, strings.NewReader(string(body))), admin)
	req = withURLParam(req, "id", fakeID)
	w := httptest.NewRecorder()
	h.Update(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

func TestGitHubTemplates_DeleteSoftTogglesActiveFalse(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)

	created, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: "Template", GitHubOwner: "acme", GitHubRepo: "repo", Framework: "node", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}

	h := newGitHubTemplatesTestHandler(pool, nil)
	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/github/templates/"+created.ID, nil), admin)
	req = withURLParam(req, "id", created.ID)
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	stored, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected row to still exist after soft delete, got %d rows", len(stored))
	}
	if stored[0].Active {
		t.Error("expected active=false after delete")
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 1 || entries[0].Action != "github.template.delete" {
		t.Fatalf("expected 1 github.template.delete audit entry, got total=%d entries=%+v", total, entries)
	}
}

func TestGitHubTemplates_DeleteNotFoundReturns404(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)

	fakeID := "00000000-0000-0000-0000-000000000000"
	req := withUser(httptest.NewRequest(http.MethodDelete, "/api/github/templates/"+fakeID, nil), admin)
	req = withURLParam(req, "id", fakeID)
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

func TestGitHubTemplates_ReactivateViaSetActive(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)

	created, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: "Template", GitHubOwner: "acme", GitHubRepo: "repo", Framework: "node", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}

	h := newGitHubTemplatesTestHandler(pool, nil)

	// Deactivate first.
	delReq := withUser(httptest.NewRequest(http.MethodDelete, "/api/github/templates/"+created.ID, nil), admin)
	delReq = withURLParam(delReq, "id", created.ID)
	delW := httptest.NewRecorder()
	h.Delete(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want 200", delW.Code)
	}

	// Reactivate.
	body, _ := json.Marshal(map[string]bool{"active": true})
	reactReq := withUser(httptest.NewRequest(http.MethodPut, "/api/github/templates/"+created.ID+"/active", strings.NewReader(string(body))), admin)
	reactReq = withURLParam(reactReq, "id", created.ID)
	reactW := httptest.NewRecorder()
	h.SetActive(reactW, reactReq)

	if reactW.Code != http.StatusOK {
		t.Fatalf("reactivate: status = %d, want 200; body = %s", reactW.Code, reactW.Body.String())
	}

	stored, err := ListGitHubTemplates(context.Background(), pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	if len(stored) != 1 || !stored[0].Active {
		t.Fatalf("expected template reactivated (active=true), got %+v", stored)
	}

	entries, total, err := ListAuditLog(context.Background(), pool, AuditLogFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 audit entries (deactivate + activate), got %d", total)
	}
	if entries[0].Action != "github.template.activate" {
		t.Errorf("most recent audit action = %q, want github.template.activate", entries[0].Action)
	}
}

func TestGitHubTemplates_ListOnlyActiveFilter(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	admin := mustCreateSuperadmin(t, pool)

	active, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: "Active Template", GitHubOwner: "acme", GitHubRepo: "repo1", Framework: "node", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("seed active template: %v", err)
	}
	inactive, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: "Inactive Template", GitHubOwner: "acme", GitHubRepo: "repo2", Framework: "node", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatalf("seed inactive template: %v", err)
	}
	if err := SetGitHubTemplateActive(context.Background(), pool, inactive.ID, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	h := newGitHubTemplatesTestHandler(pool, nil)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/templates?active_only=true", nil), admin)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp []GitHubTemplate
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if len(resp) != 1 || resp[0].ID != active.ID {
		t.Fatalf("expected only the active template, got %+v", resp)
	}

	// Without the filter, both templates are returned.
	reqAll := withUser(httptest.NewRequest(http.MethodGet, "/api/github/templates", nil), admin)
	wAll := httptest.NewRecorder()
	h.List(wAll, reqAll)
	var respAll []GitHubTemplate
	json.Unmarshal(wAll.Body.Bytes(), &respAll) //nolint:errcheck
	if len(respAll) != 2 {
		t.Fatalf("expected both templates without filter, got %d", len(respAll))
	}
}

func TestGitHubTemplates_ListNonSuperadminForbidden(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	user := mustCreateRegularUser(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/github/templates", nil), user)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

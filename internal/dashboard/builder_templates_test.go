package dashboard

// builder_templates_test.go — integration tests (real Postgres) for the
// builder base templates (ai-frontend-builder T23, AFB-26): the superadmin
// toggle on a registered GitHub template and the picker list.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func seedBuilderTemplate(t *testing.T, pool *db.Pool, creatorID, name string) *GitHubTemplate {
	t.Helper()
	tpl, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name: name, Description: name + " description", GitHubOwner: "acme", GitHubRepo: strings.ToLower(name) + "-repo",
		Framework: "react", CreatedBy: creatorID,
		RenderServiceType: "static_site", BuildCommand: "npm run build", PublishPath: "dist",
	})
	if err != nil {
		t.Fatalf("seed template %s: %v", name, err)
	}
	return tpl
}

func patchBuilderEnabled(t *testing.T, h *GitHubTemplatesHandler, user *DashboardUser, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/dashboard/api/github/templates/x/builder", strings.NewReader(body))
	if user != nil {
		req = withUser(req, user)
	}
	req = withURLParam(req, "id", id)
	w := httptest.NewRecorder()
	h.SetBuilderEnabled(w, req)
	return w
}

func listBuilderTemplates(t *testing.T, h *GitHubTemplatesHandler, user *DashboardUser) (*httptest.ResponseRecorder, []BuilderTemplate) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/builder/templates", nil)
	if user != nil {
		req = withUser(req, user)
	}
	w := httptest.NewRecorder()
	h.ListBuilderTemplates(w, req)
	var out []BuilderTemplate
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list: %v (%s)", err, w.Body.String())
		}
	}
	return w, out
}

// The toggle is superadmin-only: admin gets 403, anonymous 401, and the flag
// is not touched.
func TestBuilderTemplates_ToggleForbiddenForNonSuperadmin(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	super := mustCreateSuperadmin(t, pool)
	admin := mustCreateRegularUser(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)
	tpl := seedBuilderTemplate(t, pool, super.ID, "Alpha")

	if w := patchBuilderEnabled(t, h, admin, tpl.ID, `{"builder_enabled":true}`); w.Code != http.StatusForbidden {
		t.Fatalf("admin: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if w := patchBuilderEnabled(t, h, nil, tpl.ID, `{"builder_enabled":true}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: expected 401, got %d", w.Code)
	}
	got, _ := ListGitHubTemplates(context.Background(), pool, false)
	if len(got) != 1 || got[0].BuilderEnabled {
		t.Fatalf("a forbidden toggle must not change the flag: %+v", got)
	}
}

// A superadmin toggles the flag on and off; the response is the template, an
// audit row records each change, and the existing list exposes the flag.
func TestBuilderTemplates_SuperadminTogglesAndAudits(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	super := mustCreateSuperadmin(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)
	tpl := seedBuilderTemplate(t, pool, super.ID, "Alpha")
	if tpl.BuilderEnabled {
		t.Fatal("a new template must not be builder-enabled by default")
	}

	w := patchBuilderEnabled(t, h, super, tpl.ID, `{"builder_enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp GitHubTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.BuilderEnabled || resp.ID != tpl.ID || resp.Name != "Alpha" {
		t.Errorf("unexpected response: %+v", resp)
	}
	listed, _ := ListGitHubTemplates(context.Background(), pool, false)
	if len(listed) != 1 || !listed[0].BuilderEnabled {
		t.Fatalf("the flag must be persisted and listed: %+v", listed)
	}

	if w := patchBuilderEnabled(t, h, super, tpl.ID, `{"builder_enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d", w.Code)
	}

	for action, want := range map[string]int{"github.template.builder_enable": 1, "github.template.builder_disable": 1} {
		entries, _, err := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: action})
		if err != nil {
			t.Fatalf("ListAuditLog: %v", err)
		}
		if len(entries) != want || entries[0].ResourceID != tpl.ID || entries[0].UserEmail != super.Email {
			t.Errorf("%s: expected %d audit row for the template, got %+v", action, want, entries)
		}
	}
}

// Unknown ids (including malformed ones) are 404 and a body without
// builder_enabled is 400.
func TestBuilderTemplates_ToggleNotFoundAndBadBody(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	super := mustCreateSuperadmin(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)
	tpl := seedBuilderTemplate(t, pool, super.ID, "Alpha")

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid", "1' OR '1'='1"} {
		if w := patchBuilderEnabled(t, h, super, id, `{"builder_enabled":true}`); w.Code != http.StatusNotFound {
			t.Errorf("id %q: expected 404, got %d: %s", id, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{``, `{}`, `{"builder_enabled":null}`, `{"builder_enabled":"true"}`, `not json`} {
		if w := patchBuilderEnabled(t, h, super, tpl.ID, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
	if got, _ := ListGitHubTemplates(context.Background(), pool, false); got[0].BuilderEnabled {
		t.Error("rejected requests must not change the flag")
	}
}

// The picker lists the built-in default template first, then only templates
// that are builder-enabled AND active, without exposing the GitHub repository.
func TestBuilderTemplates_ListIncludesDefaultAndOnlyEnabled(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	super := mustCreateSuperadmin(t, pool)
	user := mustCreateRegularUser(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)

	alpha := seedBuilderTemplate(t, pool, super.ID, "Alpha")
	seedBuilderTemplate(t, pool, super.ID, "Bravo") // never enabled
	charlie := seedBuilderTemplate(t, pool, super.ID, "Charlie")
	delta := seedBuilderTemplate(t, pool, super.ID, "Delta")
	for _, id := range []string{alpha.ID, charlie.ID, delta.ID} {
		if _, err := SetGitHubTemplateBuilderEnabled(context.Background(), pool, id, true); err != nil {
			t.Fatalf("enable %s: %v", id, err)
		}
	}
	// Charlie is enabled but later deactivated: it must disappear.
	if err := SetGitHubTemplateActive(context.Background(), pool, charlie.ID, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Any authenticated user (here a non-superadmin) can list.
	w, list := listBuilderTemplates(t, h, user)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(list) != 3 {
		t.Fatalf("expected default + Alpha + Delta, got %d: %+v", len(list), list)
	}
	if list[0].Kind != "default" || list[0].ID != "" || list[0].Name == "" ||
		list[0].RenderServiceType != "static_site" || list[0].BuildCommand == "" || list[0].PublishPath != "dist" {
		t.Errorf("the default template must come first with its build metadata: %+v", list[0])
	}
	if list[1].Kind != "github" || list[1].ID != alpha.ID || list[1].Name != "Alpha" ||
		list[1].BuildCommand != "npm run build" || list[1].PublishPath != "dist" {
		t.Errorf("second entry should be Alpha: %+v", list[1])
	}
	if list[2].ID != delta.ID {
		t.Errorf("third entry should be Delta: %+v", list[2])
	}
	for _, e := range list {
		if e.Name == "Bravo" || e.Name == "Charlie" {
			t.Errorf("%s must not be listed (not enabled / inactive)", e.Name)
		}
	}
	for _, leaked := range []string{"github_owner", "github_repo", "acme"} {
		if strings.Contains(w.Body.String(), leaked) {
			t.Errorf("the picker must not expose the repository (%q found): %s", leaked, w.Body.String())
		}
	}
}

// With nothing enabled the builder still offers the default template; the
// endpoint needs an authenticated user.
func TestBuilderTemplates_ListDefaultOnlyAndAuthRequired(t *testing.T) {
	pool := githubTemplatesHandlerTestPool(t)
	defer pool.Close()
	user := mustCreateRegularUser(t, pool)
	h := newGitHubTemplatesTestHandler(pool, nil)

	w, list := listBuilderTemplates(t, h, user)
	if w.Code != http.StatusOK || len(list) != 1 || list[0].Kind != "default" {
		t.Fatalf("expected only the default template, got %d %+v", w.Code, list)
	}
	if w, _ := listBuilderTemplates(t, h, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: expected 401, got %d", w.Code)
	}
}

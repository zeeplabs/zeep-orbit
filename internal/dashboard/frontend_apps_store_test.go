package dashboard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func frontendAppsTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS zeep_system`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zeep_system.github_templates (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name         TEXT NOT NULL,
			description  TEXT NOT NULL DEFAULT '',
			github_owner TEXT NOT NULL,
			github_repo  TEXT NOT NULL,
			framework    TEXT NOT NULL DEFAULT '',
			active       BOOLEAN NOT NULL DEFAULT true,
			created_by   TEXT NOT NULL,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("create github_templates table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS zeep_system.frontend_apps (
			id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name            TEXT NOT NULL,
			slug            TEXT NOT NULL,
			template_id     UUID NOT NULL,
			github_repo_url TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'ready',
			error_message   TEXT NOT NULL DEFAULT '',
			created_by      TEXT NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			archived_at     TIMESTAMPTZ
		)`); err != nil {
		t.Fatalf("create frontend_apps table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_frontend_apps_slug
		ON zeep_system.frontend_apps (slug) WHERE archived_at IS NULL`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.frontend_apps, zeep_system.github_templates`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.frontend_apps, zeep_system.github_templates`)
	})

	return pool
}

func testTemplate(t *testing.T, pool *db.Pool, name string) *GitHubTemplate {
	t.Helper()
	tmpl, err := CreateGitHubTemplate(context.Background(), pool, GitHubTemplateInput{
		Name:        name,
		Description: "Test template",
		GitHubOwner: "testowner",
		GitHubRepo:  "test-repo",
		Framework:   "React",
		CreatedBy:   "test@example.com",
	})
	if err != nil {
		t.Fatalf("create test template: %v", err)
	}
	return tmpl
}

func TestCreateFrontendApp(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "React Template")

	input := FrontendAppInput{
		Name:          "My Frontend App",
		Slug:          "my-frontend-app",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "https://github.com/testowner/my-frontend-app",
		Status:        "ready",
		ErrorMessage:  "",
		CreatedBy:     "user@example.com",
	}

	app, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	if app == nil {
		t.Fatal("expected app, got nil")
	}
	if app.ID == "" {
		t.Error("app.ID should be populated")
	}
	if app.Name != input.Name {
		t.Errorf("Name = %q, want %q", app.Name, input.Name)
	}
	if app.Slug != input.Slug {
		t.Errorf("Slug = %q, want %q", app.Slug, input.Slug)
	}
	if app.TemplateID != input.TemplateID {
		t.Errorf("TemplateID = %q, want %q", app.TemplateID, input.TemplateID)
	}
	if app.TemplateName != tmpl.Name {
		t.Errorf("TemplateName = %q, want %q", app.TemplateName, tmpl.Name)
	}
	if app.GithubRepoURL != input.GithubRepoURL {
		t.Errorf("GithubRepoURL = %q, want %q", app.GithubRepoURL, input.GithubRepoURL)
	}
	if app.Status != "ready" {
		t.Errorf("Status = %q, want ready", app.Status)
	}
	if app.CreatedBy != input.CreatedBy {
		t.Errorf("CreatedBy = %q, want %q", app.CreatedBy, input.CreatedBy)
	}
	if app.CreatedAt.IsZero() {
		t.Error("CreatedAt should be populated")
	}
	if app.ArchivedAt != nil {
		t.Error("ArchivedAt should be nil for new app")
	}
}

func TestCreateFrontendAppWithFailedStatus(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Failed App Template")

	input := FrontendAppInput{
		Name:          "Failed App",
		Slug:          "failed-app",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "failed",
		ErrorMessage:  "rate limit exceeded",
		CreatedBy:     "user@example.com",
	}

	app, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	if app.Status != "failed" {
		t.Errorf("Status = %q, want failed", app.Status)
	}
	if app.ErrorMessage != "rate limit exceeded" {
		t.Errorf("ErrorMessage = %q, want rate limit exceeded", app.ErrorMessage)
	}
}

func TestGetFrontendApp(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Get App Template")

	input := FrontendAppInput{
		Name:          "Get Me",
		Slug:          "get-me",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "https://github.com/test/get-me",
		Status:        "ready",
		CreatedBy:     "user@example.com",
	}

	created, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	app, err := GetFrontendApp(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("GetFrontendApp: %v", err)
	}

	if app.ID != created.ID {
		t.Errorf("ID = %q, want %q", app.ID, created.ID)
	}
	if app.Name != input.Name {
		t.Errorf("Name = %q, want %q", app.Name, input.Name)
	}
}

func TestGetFrontendAppNotFound(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := GetFrontendApp(ctx, pool, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetFrontendAppExcludesArchived(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Archived App Template")

	input := FrontendAppInput{
		Name:          "Archive Me",
		Slug:          "archive-me",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "ready",
		CreatedBy:     "user@example.com",
	}

	created, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	if err := ArchiveFrontendApp(ctx, pool, created.ID); err != nil {
		t.Fatalf("ArchiveFrontendApp: %v", err)
	}

	_, err = GetFrontendApp(ctx, pool, created.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for archived app, got %v", err)
	}
}

func TestListFrontendApps(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "List App Template")

	inputs := []FrontendAppInput{
		{Name: "App 1", Slug: "app-1", TemplateID: tmpl.ID, Status: "ready", CreatedBy: "u1@e.com"},
		{Name: "App 2", Slug: "app-2", TemplateID: tmpl.ID, Status: "failed", ErrorMessage: "oops", CreatedBy: "u2@e.com"},
		{Name: "App 3", Slug: "app-3", TemplateID: tmpl.ID, Status: "ready", CreatedBy: "u3@e.com"},
	}

	for _, input := range inputs {
		_, err := CreateFrontendApp(ctx, pool, input)
		if err != nil {
			t.Fatalf("CreateFrontendApp: %v", err)
		}
	}

	apps, err := ListFrontendApps(ctx, pool)
	if err != nil {
		t.Fatalf("ListFrontendApps: %v", err)
	}

	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}

	if apps[0].Name != "App 3" {
		t.Errorf("first app Name = %q, want App 3 (DESC order)", apps[0].Name)
	}
}

func TestListFrontendAppsExcludesArchived(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "List Excludes Template")

	inputs := []FrontendAppInput{
		{Name: "Keep", Slug: "keep", TemplateID: tmpl.ID, Status: "ready", CreatedBy: "u1@e.com"},
		{Name: "Archive", Slug: "archive", TemplateID: tmpl.ID, Status: "ready", CreatedBy: "u2@e.com"},
	}

	var archivedID string
	for i, input := range inputs {
		app, err := CreateFrontendApp(ctx, pool, input)
		if err != nil {
			t.Fatalf("CreateFrontendApp: %v", err)
		}
		if i == 1 {
			archivedID = app.ID
		}
	}

	if err := ArchiveFrontendApp(ctx, pool, archivedID); err != nil {
		t.Fatalf("ArchiveFrontendApp: %v", err)
	}

	apps, err := ListFrontendApps(ctx, pool)
	if err != nil {
		t.Fatalf("ListFrontendApps: %v", err)
	}

	if len(apps) != 1 {
		t.Errorf("expected 1 app (only non-archived), got %d", len(apps))
	}
	if apps[0].Name != "Keep" {
		t.Errorf("Name = %q, want Keep", apps[0].Name)
	}
}

func TestUpdateFrontendAppStatus(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Update Status Template")

	input := FrontendAppInput{
		Name:          "Status App",
		Slug:          "status-app",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "failed",
		ErrorMessage:  "original error",
		CreatedBy:     "user@example.com",
	}

	created, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	app, err := UpdateFrontendAppStatus(ctx, pool, created.ID, "ready", "", "https://github.com/test/repo")
	if err != nil {
		t.Fatalf("UpdateFrontendAppStatus: %v", err)
	}

	if app.Status != "ready" {
		t.Errorf("Status = %q, want ready", app.Status)
	}
	if app.ErrorMessage != "" {
		t.Errorf("ErrorMessage = %q, want empty", app.ErrorMessage)
	}
	if app.GithubRepoURL != "https://github.com/test/repo" {
		t.Errorf("GithubRepoURL = %q, want https://github.com/test/repo", app.GithubRepoURL)
	}
}

func TestUpdateFrontendAppStatusNotFound(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := UpdateFrontendAppStatus(ctx, pool, "00000000-0000-0000-0000-000000000000", "ready", "", "")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestArchiveFrontendApp(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Archive Template")

	input := FrontendAppInput{
		Name:          "Archive This",
		Slug:          "archive-this",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "ready",
		CreatedBy:     "user@example.com",
	}

	created, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	if err := ArchiveFrontendApp(ctx, pool, created.ID); err != nil {
		t.Fatalf("ArchiveFrontendApp: %v", err)
	}

	// Should not be found by Get (which filters archived_at IS NULL).
	_, err = GetFrontendApp(ctx, pool, created.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after archive, got %v", err)
	}

	// Should not appear in list.
	apps, err := ListFrontendApps(ctx, pool)
	if err != nil {
		t.Fatalf("ListFrontendApps: %v", err)
	}
	for _, a := range apps {
		if a.ID == created.ID {
			t.Error("archived app should not appear in list")
		}
	}
}

func TestArchiveFrontendAppNotFound(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	err := ArchiveFrontendApp(ctx, pool, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSlugExists(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Slug Template")

	input := FrontendAppInput{
		Name:          "Slug Exists App",
		Slug:          "slug-exists-app",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "ready",
		CreatedBy:     "user@example.com",
	}

	_, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	exists, err := SlugExists(ctx, pool, "slug-exists-app")
	if err != nil {
		t.Fatalf("SlugExists: %v", err)
	}
	if !exists {
		t.Error("SlugExists should return true for existing slug")
	}

	exists, err = SlugExists(ctx, pool, "nonexistent-slug")
	if err != nil {
		t.Fatalf("SlugExists: %v", err)
	}
	if exists {
		t.Error("SlugExists should return false for nonexistent slug")
	}
}

func TestSlugExistsIgnoresArchived(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Slug Archived Template")

	input := FrontendAppInput{
		Name:          "Slug Reuse App",
		Slug:          "slug-reuse",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "ready",
		CreatedBy:     "user@example.com",
	}

	created, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp: %v", err)
	}

	if err := ArchiveFrontendApp(ctx, pool, created.ID); err != nil {
		t.Fatalf("ArchiveFrontendApp: %v", err)
	}

	exists, err := SlugExists(ctx, pool, "slug-reuse")
	if err != nil {
		t.Fatalf("SlugExists: %v", err)
	}
	if exists {
		t.Error("SlugExists should return false for slug of archived app (allowed to reuse)")
	}
}

func TestSlugUniqueIndexEnforced(t *testing.T) {
	pool := frontendAppsTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	tmpl := testTemplate(t, pool, "Unique Slug Template")

	input := FrontendAppInput{
		Name:          "First App",
		Slug:          "collision-slug",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "ready",
		CreatedBy:     "user@example.com",
	}

	_, err := CreateFrontendApp(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateFrontendApp 1: %v", err)
	}

	// Try to create another with the same slug.
	_, err = CreateFrontendApp(ctx, pool, FrontendAppInput{
		Name:          "Second App",
		Slug:          "collision-slug",
		TemplateID:    tmpl.ID,
		GithubRepoURL: "",
		Status:        "ready",
		CreatedBy:     "user2@example.com",
	})
	if err == nil {
		t.Error("expected unique constraint violation, got nil")
	}
}

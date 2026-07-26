package dashboard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func githubTemplatesTestPool(t *testing.T) *db.Pool {
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
		t.Fatalf("create table: %v", err)
	}

	// Clean slate for each test.
	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.github_templates`); err != nil {
		t.Fatalf("truncate table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.github_templates`)
	})

	return pool
}

func TestCreateGitHubTemplate(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	input := GitHubTemplateInput{
		Name:        "React App Template",
		Description: "A template for React applications",
		GitHubOwner: "zeeplabs",
		GitHubRepo:  "react-template",
		Framework:   "React",
		CreatedBy:   "user@example.com",
	}

	template, err := CreateGitHubTemplate(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateGitHubTemplate: %v", err)
	}

	if template == nil {
		t.Fatal("expected template, got nil")
	}
	if template.ID == "" {
		t.Error("template.ID should be populated")
	}
	if template.Name != input.Name {
		t.Errorf("Name = %q, want %q", template.Name, input.Name)
	}
	if template.Description != input.Description {
		t.Errorf("Description = %q, want %q", template.Description, input.Description)
	}
	if template.GitHubOwner != input.GitHubOwner {
		t.Errorf("GitHubOwner = %q, want %q", template.GitHubOwner, input.GitHubOwner)
	}
	if template.GitHubRepo != input.GitHubRepo {
		t.Errorf("GitHubRepo = %q, want %q", template.GitHubRepo, input.GitHubRepo)
	}
	if template.Framework != input.Framework {
		t.Errorf("Framework = %q, want %q", template.Framework, input.Framework)
	}
	if !template.Active {
		t.Error("Active should be true by default")
	}
	if template.CreatedBy != input.CreatedBy {
		t.Errorf("CreatedBy = %q, want %q", template.CreatedBy, input.CreatedBy)
	}
	if template.CreatedAt.IsZero() {
		t.Error("CreatedAt should be populated")
	}
}

func TestListGitHubTemplatesReturnedAllTemplates(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Create 3 templates.
	inputs := []GitHubTemplateInput{
		{
			Name:        "Template 1",
			Description: "Description 1",
			GitHubOwner: "owner1",
			GitHubRepo:  "repo1",
			Framework:   "React",
			CreatedBy:   "user1@example.com",
		},
		{
			Name:        "Template 2",
			Description: "Description 2",
			GitHubOwner: "owner2",
			GitHubRepo:  "repo2",
			Framework:   "Vue",
			CreatedBy:   "user2@example.com",
		},
		{
			Name:        "Template 3",
			Description: "Description 3",
			GitHubOwner: "owner3",
			GitHubRepo:  "repo3",
			Framework:   "Angular",
			CreatedBy:   "user3@example.com",
		},
	}

	for _, input := range inputs {
		_, err := CreateGitHubTemplate(ctx, pool, input)
		if err != nil {
			t.Fatalf("CreateGitHubTemplate: %v", err)
		}
	}

	// List all (onlyActive=false).
	templates, err := ListGitHubTemplates(ctx, pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates(false): %v", err)
	}

	if len(templates) != 3 {
		t.Errorf("expected 3 templates, got %d", len(templates))
	}

	// Verify they're sorted by created_at DESC (reverse order of creation).
	if templates[0].Name != "Template 3" {
		t.Errorf("first template Name = %q, want Template 3", templates[0].Name)
	}
	if templates[1].Name != "Template 2" {
		t.Errorf("second template Name = %q, want Template 2", templates[1].Name)
	}
	if templates[2].Name != "Template 1" {
		t.Errorf("third template Name = %q, want Template 1", templates[2].Name)
	}
}

func TestListGitHubTemplatesFiltersInactiveWhenOnlyActiveTrue(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Create 3 templates and deactivate 1.
	inputs := []GitHubTemplateInput{
		{
			Name:        "Active Template 1",
			Description: "Active",
			GitHubOwner: "owner1",
			GitHubRepo:  "repo1",
			Framework:   "React",
			CreatedBy:   "user1@example.com",
		},
		{
			Name:        "Inactive Template",
			Description: "Will be inactive",
			GitHubOwner: "owner2",
			GitHubRepo:  "repo2",
			Framework:   "Vue",
			CreatedBy:   "user2@example.com",
		},
		{
			Name:        "Active Template 2",
			Description: "Active",
			GitHubOwner: "owner3",
			GitHubRepo:  "repo3",
			Framework:   "Angular",
			CreatedBy:   "user3@example.com",
		},
	}

	templateIDs := make([]string, 0)
	for _, input := range inputs {
		tmpl, err := CreateGitHubTemplate(ctx, pool, input)
		if err != nil {
			t.Fatalf("CreateGitHubTemplate: %v", err)
		}
		templateIDs = append(templateIDs, tmpl.ID)
	}

	// Deactivate the second template.
	if err := SetGitHubTemplateActive(ctx, pool, templateIDs[1], false); err != nil {
		t.Fatalf("SetGitHubTemplateActive: %v", err)
	}

	// List with onlyActive=true should return 2.
	activeTemplates, err := ListGitHubTemplates(ctx, pool, true)
	if err != nil {
		t.Fatalf("ListGitHubTemplates(true): %v", err)
	}
	if len(activeTemplates) != 2 {
		t.Errorf("expected 2 active templates, got %d", len(activeTemplates))
	}

	// Verify no deactivated templates are in the list.
	for _, tmpl := range activeTemplates {
		if tmpl.Name == "Inactive Template" {
			t.Errorf("Inactive Template should not appear in active list")
		}
	}

	// List with onlyActive=false should return all 3.
	allTemplates, err := ListGitHubTemplates(ctx, pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates(false): %v", err)
	}
	if len(allTemplates) != 3 {
		t.Errorf("expected 3 total templates, got %d", len(allTemplates))
	}

	// Verify the deactivated template is in the full list.
	found := false
	for _, tmpl := range allTemplates {
		if tmpl.Name == "Inactive Template" && !tmpl.Active {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Inactive Template should appear in full list with active=false")
	}
}

func TestUpdateGitHubTemplate(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Create a template.
	original := GitHubTemplateInput{
		Name:        "Original Name",
		Description: "Original Description",
		GitHubOwner: "original-owner",
		GitHubRepo:  "original-repo",
		Framework:   "React",
		CreatedBy:   "original@example.com",
	}
	created, err := CreateGitHubTemplate(ctx, pool, original)
	if err != nil {
		t.Fatalf("CreateGitHubTemplate: %v", err)
	}

	// Deactivate it to confirm update doesn't touch active.
	if err := SetGitHubTemplateActive(ctx, pool, created.ID, false); err != nil {
		t.Fatalf("SetGitHubTemplateActive: %v", err)
	}

	// Update the template.
	updated := GitHubTemplateInput{
		Name:        "Updated Name",
		Description: "Updated Description",
		GitHubOwner: "updated-owner",
		GitHubRepo:  "updated-repo",
		Framework:   "Vue",
		CreatedBy:   "updated@example.com", // Note: this should NOT update created_by
	}
	result, err := UpdateGitHubTemplate(ctx, pool, created.ID, updated)
	if err != nil {
		t.Fatalf("UpdateGitHubTemplate: %v", err)
	}

	// Verify all updatable fields changed.
	if result.Name != updated.Name {
		t.Errorf("Name = %q, want %q", result.Name, updated.Name)
	}
	if result.Description != updated.Description {
		t.Errorf("Description = %q, want %q", result.Description, updated.Description)
	}
	if result.GitHubOwner != updated.GitHubOwner {
		t.Errorf("GitHubOwner = %q, want %q", result.GitHubOwner, updated.GitHubOwner)
	}
	if result.GitHubRepo != updated.GitHubRepo {
		t.Errorf("GitHubRepo = %q, want %q", result.GitHubRepo, updated.GitHubRepo)
	}
	if result.Framework != updated.Framework {
		t.Errorf("Framework = %q, want %q", result.Framework, updated.Framework)
	}

	// Verify immutable fields are unchanged.
	if result.Active {
		t.Error("Active should remain false (not updated by UpdateGitHubTemplate)")
	}
	if result.CreatedBy != original.CreatedBy {
		t.Errorf("CreatedBy = %q, want unchanged %q", result.CreatedBy, original.CreatedBy)
	}
	if result.CreatedAt != created.CreatedAt {
		t.Error("CreatedAt should not change")
	}
}

func TestUpdateGitHubTemplateReturnsErrorIfNotFound(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	input := GitHubTemplateInput{
		Name:        "Test",
		Description: "Test",
		GitHubOwner: "owner",
		GitHubRepo:  "repo",
		Framework:   "React",
		CreatedBy:   "user@example.com",
	}

	// Use a valid UUID that doesn't exist in the database.
	nonExistentID := "00000000-0000-0000-0000-000000000000"
	result, err := UpdateGitHubTemplate(ctx, pool, nonExistentID, input)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on not found, got %v", result)
	}
}

func TestSetGitHubTemplateActiveTogglesFalseToTrue(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Create and immediately deactivate a template.
	input := GitHubTemplateInput{
		Name:        "Test Template",
		Description: "Test",
		GitHubOwner: "owner",
		GitHubRepo:  "repo",
		Framework:   "React",
		CreatedBy:   "user@example.com",
	}
	created, err := CreateGitHubTemplate(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateGitHubTemplate: %v", err)
	}

	if err := SetGitHubTemplateActive(ctx, pool, created.ID, false); err != nil {
		t.Fatalf("SetGitHubTemplateActive(false): %v", err)
	}

	// Verify it's inactive.
	templates, err := ListGitHubTemplates(ctx, pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	found := false
	for _, tmpl := range templates {
		if tmpl.ID == created.ID {
			if tmpl.Active {
				t.Error("expected template to be inactive")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("template not found after deactivation")
	}

	// Re-activate it.
	if err := SetGitHubTemplateActive(ctx, pool, created.ID, true); err != nil {
		t.Fatalf("SetGitHubTemplateActive(true): %v", err)
	}

	// Verify it's active again.
	activeTemplates, err := ListGitHubTemplates(ctx, pool, true)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}
	found = false
	for _, tmpl := range activeTemplates {
		if tmpl.ID == created.ID {
			if !tmpl.Active {
				t.Error("expected template to be active after toggle")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("template not found in active list after re-activation")
	}
}

func TestSetGitHubTemplateActiveReturnsErrorIfNotFound(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Use a valid UUID that doesn't exist in the database.
	nonExistentID := "00000000-0000-0000-0000-000000000000"
	err := SetGitHubTemplateActive(ctx, pool, nonExistentID, false)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSetGitHubTemplateActiveNeverDeletes(t *testing.T) {
	pool := githubTemplatesTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Create a template.
	input := GitHubTemplateInput{
		Name:        "Test Template",
		Description: "Test",
		GitHubOwner: "owner",
		GitHubRepo:  "repo",
		Framework:   "React",
		CreatedBy:   "user@example.com",
	}
	created, err := CreateGitHubTemplate(ctx, pool, input)
	if err != nil {
		t.Fatalf("CreateGitHubTemplate: %v", err)
	}

	// Toggle active multiple times.
	for i := 0; i < 5; i++ {
		active := i%2 == 0
		if err := SetGitHubTemplateActive(ctx, pool, created.ID, active); err != nil {
			t.Fatalf("SetGitHubTemplateActive iteration %d: %v", i, err)
		}
	}

	// Verify the template still exists in the database (count it).
	templates, err := ListGitHubTemplates(ctx, pool, false)
	if err != nil {
		t.Fatalf("ListGitHubTemplates: %v", err)
	}

	found := false
	for _, tmpl := range templates {
		if tmpl.ID == created.ID {
			found = true
			// After 5 toggles (0=true, 1=false, 2=true, 3=false, 4=true), should be active.
			if !tmpl.Active {
				t.Error("expected template to be active after 5 toggles (last was true)")
			}
			break
		}
	}
	if !found {
		t.Fatal("template was deleted (not found in database)")
	}
}

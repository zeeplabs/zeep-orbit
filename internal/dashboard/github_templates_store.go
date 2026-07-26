package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// GitHubTemplate represents a template row from zeep_system.github_templates.
type GitHubTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	GitHubOwner string    `json:"github_owner"`
	GitHubRepo  string    `json:"github_repo"`
	Framework   string    `json:"framework"`
	Active      bool      `json:"active"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// GitHubTemplateInput is the input struct for Create and Update operations.
type GitHubTemplateInput struct {
	Name        string
	Description string
	GitHubOwner string
	GitHubRepo  string
	Framework   string
	CreatedBy   string
}

// ListGitHubTemplates lists all GitHub templates.
// If onlyActive is true, returns only rows where active = true.
// If onlyActive is false, returns all templates regardless of active status.
func ListGitHubTemplates(ctx context.Context, pool *db.Pool, onlyActive bool) ([]GitHubTemplate, error) {
	var query string
	if onlyActive {
		query = `SELECT id, name, description, github_owner, github_repo, framework, active, created_by, created_at
		         FROM zeep_system.github_templates
		         WHERE active = true
		         ORDER BY created_at DESC`
	} else {
		query = `SELECT id, name, description, github_owner, github_repo, framework, active, created_by, created_at
		         FROM zeep_system.github_templates
		         ORDER BY created_at DESC`
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list github templates: %w", err)
	}
	defer rows.Close()

	var templates []GitHubTemplate
	for rows.Next() {
		var t GitHubTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.GitHubOwner, &t.GitHubRepo, &t.Framework, &t.Active, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: list github templates scan: %w", err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: list github templates rows: %w", err)
	}

	// Return empty slice instead of nil for JSON serialization.
	if templates == nil {
		templates = make([]GitHubTemplate, 0)
	}

	return templates, nil
}

// CreateGitHubTemplate inserts a new GitHub template with active defaulting to true.
// Returns the created template row with ID and CreatedAt populated.
func CreateGitHubTemplate(ctx context.Context, pool *db.Pool, input GitHubTemplateInput) (*GitHubTemplate, error) {
	var t GitHubTemplate
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, name, description, github_owner, github_repo, framework, active, created_by, created_at`,
		input.Name, input.Description, input.GitHubOwner, input.GitHubRepo, input.Framework, input.CreatedBy,
	).Scan(&t.ID, &t.Name, &t.Description, &t.GitHubOwner, &t.GitHubRepo, &t.Framework, &t.Active, &t.CreatedBy, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create github template: %w", err)
	}

	return &t, nil
}

// UpdateGitHubTemplate updates name, description, github_owner, github_repo, and framework for a given template ID.
// Does not update active, created_by, or created_at.
// Returns an error if the template does not exist.
func UpdateGitHubTemplate(ctx context.Context, pool *db.Pool, id string, input GitHubTemplateInput) (*GitHubTemplate, error) {
	var t GitHubTemplate
	err := pool.QueryRow(ctx,
		`UPDATE zeep_system.github_templates
		 SET name = $2, description = $3, github_owner = $4, github_repo = $5, framework = $6
		 WHERE id = $1
		 RETURNING id, name, description, github_owner, github_repo, framework, active, created_by, created_at`,
		id, input.Name, input.Description, input.GitHubOwner, input.GitHubRepo, input.Framework,
	).Scan(&t.ID, &t.Name, &t.Description, &t.GitHubOwner, &t.GitHubRepo, &t.Framework, &t.Active, &t.CreatedBy, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: update github template: %w", err)
	}

	return &t, nil
}

// SetGitHubTemplateActive soft-toggles the active flag for a given template ID.
// Never deletes the row; only updates the active column.
// Returns an error if the template does not exist.
func SetGitHubTemplateActive(ctx context.Context, pool *db.Pool, id string, active bool) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.github_templates
		 SET active = $2
		 WHERE id = $1`,
		id, active,
	)
	if err != nil {
		return fmt.Errorf("dashboard: set github template active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

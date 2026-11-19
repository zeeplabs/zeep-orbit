package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type GitHubTemplate struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	GitHubOwner       string    `json:"github_owner"`
	GitHubRepo        string    `json:"github_repo"`
	Framework         string    `json:"framework"`
	Active            bool      `json:"active"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	RenderServiceType string    `json:"render_service_type"`
	BuildCommand      string    `json:"build_command"`
	PublishPath       string    `json:"publish_path"`
	StartCommand      string    `json:"start_command"`
	// BuilderEnabled marks the template as a base template of the AI Frontend
	// Builder (ai-frontend-builder AFB-26). Independent of Active.
	BuilderEnabled bool `json:"builder_enabled"`
}

type GitHubTemplateInput struct {
	Name              string
	Description       string
	GitHubOwner       string
	GitHubRepo        string
	Framework         string
	CreatedBy         string
	RenderServiceType string
	BuildCommand      string
	PublishPath       string
	StartCommand      string
}

const githubTemplateCols = `id, name, description, github_owner, github_repo, framework, active, created_by, created_at,
	COALESCE(render_service_type, ''), COALESCE(build_command, ''), COALESCE(publish_path, ''), COALESCE(start_command, ''), builder_enabled`

func scanTemplate(t *GitHubTemplate, row pgx.Row) error {
	return row.Scan(&t.ID, &t.Name, &t.Description, &t.GitHubOwner, &t.GitHubRepo,
		&t.Framework, &t.Active, &t.CreatedBy, &t.CreatedAt,
		&t.RenderServiceType, &t.BuildCommand, &t.PublishPath, &t.StartCommand, &t.BuilderEnabled)
}

func scanTemplateRows(t *GitHubTemplate, rows pgx.Rows) error {
	return rows.Scan(&t.ID, &t.Name, &t.Description, &t.GitHubOwner, &t.GitHubRepo,
		&t.Framework, &t.Active, &t.CreatedBy, &t.CreatedAt,
		&t.RenderServiceType, &t.BuildCommand, &t.PublishPath, &t.StartCommand, &t.BuilderEnabled)
}

func ListGitHubTemplates(ctx context.Context, pool *db.Pool, onlyActive bool) ([]GitHubTemplate, error) {
	var query string
	if onlyActive {
		query = `SELECT ` + githubTemplateCols + `
		         FROM zeep_system.github_templates
		         WHERE active = true
		         ORDER BY created_at DESC`
	} else {
		query = `SELECT ` + githubTemplateCols + `
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
		if err := scanTemplateRows(&t, rows); err != nil {
			return nil, fmt.Errorf("dashboard: list github templates scan: %w", err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: list github templates rows: %w", err)
	}
	if templates == nil {
		templates = make([]GitHubTemplate, 0)
	}
	return templates, nil
}

func CreateGitHubTemplate(ctx context.Context, pool *db.Pool, input GitHubTemplateInput) (*GitHubTemplate, error) {
	var t GitHubTemplate
	err := scanTemplate(&t,
		pool.QueryRow(ctx,
			`INSERT INTO zeep_system.github_templates
			 (name, description, github_owner, github_repo, framework, created_by,
			  render_service_type, build_command, publish_path, start_command)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 RETURNING `+githubTemplateCols,
			input.Name, input.Description, input.GitHubOwner, input.GitHubRepo, input.Framework, input.CreatedBy,
			input.RenderServiceType, input.BuildCommand, input.PublishPath, input.StartCommand,
		))
	if err != nil {
		return nil, fmt.Errorf("dashboard: create github template: %w", err)
	}
	return &t, nil
}

func UpdateGitHubTemplate(ctx context.Context, pool *db.Pool, id string, input GitHubTemplateInput) (*GitHubTemplate, error) {
	var t GitHubTemplate
	err := scanTemplate(&t,
		pool.QueryRow(ctx,
			`UPDATE zeep_system.github_templates
			 SET name = $2, description = $3, github_owner = $4, github_repo = $5, framework = $6,
			     render_service_type = $7, build_command = $8, publish_path = $9, start_command = $10
			 WHERE id = $1
			 RETURNING `+githubTemplateCols,
			id, input.Name, input.Description, input.GitHubOwner, input.GitHubRepo, input.Framework,
			input.RenderServiceType, input.BuildCommand, input.PublishPath, input.StartCommand,
		))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: update github template: %w", err)
	}
	return &t, nil
}

func SetGitHubTemplateActive(ctx context.Context, pool *db.Pool, id string, active bool) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.github_templates SET active = $2 WHERE id = $1`,
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

// SetGitHubTemplateBuilderEnabled marks (or unmarks) a template as a base
// template of the AI Frontend Builder and returns the updated row. ErrNotFound
// when id matches no template (a malformed id counts as not found, instead of
// surfacing Postgres' uuid syntax error as a 500).
func SetGitHubTemplateBuilderEnabled(ctx context.Context, pool *db.Pool, id string, enabled bool) (*GitHubTemplate, error) {
	if !looksLikeUUID(id) {
		return nil, ErrNotFound
	}
	var t GitHubTemplate
	err := scanTemplate(&t, pool.QueryRow(ctx,
		`UPDATE zeep_system.github_templates SET builder_enabled = $2 WHERE id = $1
		 RETURNING `+githubTemplateCols,
		id, enabled,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: set github template builder_enabled: %w", err)
	}
	return &t, nil
}

// ListBuilderEnabledGitHubTemplates returns the templates a builder user may
// start from: builder_enabled AND active (a deactivated template stays hidden
// even if it was enabled before), ordered by name.
func ListBuilderEnabledGitHubTemplates(ctx context.Context, pool *db.Pool) ([]GitHubTemplate, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+githubTemplateCols+`
		 FROM zeep_system.github_templates
		 WHERE builder_enabled = true AND active = true
		 ORDER BY name, created_at`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list builder github templates: %w", err)
	}
	defer rows.Close()

	templates := make([]GitHubTemplate, 0)
	for rows.Next() {
		var t GitHubTemplate
		if err := scanTemplateRows(&t, rows); err != nil {
			return nil, fmt.Errorf("dashboard: list builder github templates scan: %w", err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: list builder github templates rows: %w", err)
	}
	return templates, nil
}

// looksLikeUUID reports whether s has the canonical 8-4-4-4-12 hex shape, so a
// path parameter can be rejected as "not found" before it reaches a uuid
// column.
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			c := s[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

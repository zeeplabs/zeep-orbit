package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type FrontendApp struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	TemplateID    string     `json:"template_id"`
	TemplateName  string     `json:"template_name"`
	GithubRepoURL string     `json:"github_repo_url"`
	Status        string     `json:"status"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	ArchivedAt    *time.Time `json:"archived_at,omitempty"`
}

type FrontendAppInput struct {
	Name          string
	Slug          string
	TemplateID    string
	GithubRepoURL string
	Status        string
	ErrorMessage  string
	CreatedBy     string
}

func scanApp(a *FrontendApp, row pgx.Row) error {
	return row.Scan(&a.ID, &a.Name, &a.Slug, &a.TemplateID, &a.TemplateName,
		&a.GithubRepoURL, &a.Status, &a.ErrorMessage, &a.CreatedBy, &a.CreatedAt, &a.ArchivedAt)
}

func scanAppRows(a *FrontendApp, rows pgx.Rows) error {
	return rows.Scan(&a.ID, &a.Name, &a.Slug, &a.TemplateID, &a.TemplateName,
		&a.GithubRepoURL, &a.Status, &a.ErrorMessage, &a.CreatedBy, &a.CreatedAt, &a.ArchivedAt)
}

func CreateFrontendApp(ctx context.Context, pool *db.Pool, input FrontendAppInput) (*FrontendApp, error) {
	var a FrontendApp
	err := scanApp(&a,
		pool.QueryRow(ctx,
			`INSERT INTO zeep_system.frontend_apps
			 (name, slug, template_id, github_repo_url, status, error_message, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING id, name, slug, template_id,
			   (SELECT COALESCE(gt.name, '') FROM zeep_system.github_templates gt WHERE gt.id = template_id),
			   github_repo_url, status, error_message, created_by, created_at, archived_at`,
			input.Name, input.Slug, input.TemplateID,
			input.GithubRepoURL, input.Status, input.ErrorMessage, input.CreatedBy,
		))
	if err != nil {
		return nil, fmt.Errorf("dashboard: create frontend app: %w", err)
	}
	return &a, nil
}

func GetFrontendApp(ctx context.Context, pool *db.Pool, id string) (*FrontendApp, error) {
	var a FrontendApp
	err := scanApp(&a,
		pool.QueryRow(ctx,
			`SELECT fa.id, fa.name, fa.slug, fa.template_id,
			 COALESCE(gt.name, ''), fa.github_repo_url, fa.status,
			 fa.error_message, fa.created_by, fa.created_at, fa.archived_at
			 FROM zeep_system.frontend_apps fa
			 LEFT JOIN zeep_system.github_templates gt ON gt.id = fa.template_id
			 WHERE fa.id = $1 AND fa.archived_at IS NULL`,
			id,
		))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get frontend app: %w", err)
	}
	return &a, nil
}

func ListFrontendApps(ctx context.Context, pool *db.Pool) ([]FrontendApp, error) {
	rows, err := pool.Query(ctx,
		`SELECT fa.id, fa.name, fa.slug, fa.template_id,
		 COALESCE(gt.name, ''), fa.github_repo_url, fa.status,
		 fa.error_message, fa.created_by, fa.created_at, fa.archived_at
		 FROM zeep_system.frontend_apps fa
		 LEFT JOIN zeep_system.github_templates gt ON gt.id = fa.template_id
		 WHERE fa.archived_at IS NULL
		 ORDER BY fa.created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list frontend apps: %w", err)
	}
	defer rows.Close()

	var apps []FrontendApp
	for rows.Next() {
		var a FrontendApp
		if err := scanAppRows(&a, rows); err != nil {
			return nil, fmt.Errorf("dashboard: list frontend apps scan: %w", err)
		}
		apps = append(apps, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: list frontend apps rows: %w", err)
	}
	if apps == nil {
		apps = make([]FrontendApp, 0)
	}
	return apps, nil
}

func UpdateFrontendAppStatus(ctx context.Context, pool *db.Pool, id, status, errorMessage, repoURL string) (*FrontendApp, error) {
	var a FrontendApp
	err := scanApp(&a,
		pool.QueryRow(ctx,
			`UPDATE zeep_system.frontend_apps
			 SET status = $2, error_message = $3, github_repo_url = $4
			 WHERE id = $1 AND archived_at IS NULL
			 RETURNING id, name, slug, template_id,
			   (SELECT COALESCE(gt.name, '') FROM zeep_system.github_templates gt WHERE gt.id = template_id),
			   github_repo_url, status, error_message, created_by, created_at, archived_at`,
			id, status, errorMessage, repoURL,
		))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: update frontend app status: %w", err)
	}
	return &a, nil
}

func ArchiveFrontendApp(ctx context.Context, pool *db.Pool, id string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.frontend_apps
		 SET archived_at = now()
		 WHERE id = $1 AND archived_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("dashboard: archive frontend app: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func SlugExists(ctx context.Context, pool *db.Pool, slug string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM zeep_system.frontend_apps
			WHERE slug = $1 AND archived_at IS NULL
		)`, slug,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("dashboard: slug exists: %w", err)
	}
	return exists, nil
}

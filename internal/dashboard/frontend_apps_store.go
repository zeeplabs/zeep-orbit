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
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Slug               string     `json:"slug"`
	TemplateID         string     `json:"template_id"`
	TemplateName       string     `json:"template_name"`
	GithubRepoURL      string     `json:"github_repo_url"`
	Status             string     `json:"status"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	CreatedBy          string     `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	BackendAppID       *string    `json:"backend_app_id,omitempty"`
	DeployServiceID    string     `json:"deploy_service_id,omitempty"`
	DeployURL          string     `json:"deploy_url,omitempty"`
	DeployStatus       string     `json:"deploy_status,omitempty"`
	DeployErrorMessage string     `json:"deploy_error_message,omitempty"`
	CustomDomain       string     `json:"custom_domain,omitempty"`
	OwnerID            string     `json:"owner_id"`
	OwnerEmail         string     `json:"owner_email,omitempty"`
	OwnerName          string     `json:"owner_name,omitempty"`
}

type FrontendAppInput struct {
	Name               string
	Slug               string
	TemplateID         string
	GithubRepoURL      string
	Status             string
	ErrorMessage       string
	CreatedBy          string
	OwnerID            string
	BackendAppID       string
	DeployServiceID    string
	DeployURL          string
	DeployStatus       string
	DeployErrorMessage string
}

const faExtraColsSelect = `COALESCE(fa.backend_app_id::text, ''),
	COALESCE(fa.deploy_service_id, ''),
	COALESCE(fa.deploy_url, ''),
	COALESCE(fa.deploy_status, 'pending'),
	COALESCE(fa.deploy_error_message, ''),
	COALESCE(fa.custom_domain, ''),
	COALESCE(fa.owner_id::text, ''),
	COALESCE(u.email, ''),
	COALESCE(u.name, '')`

const faExtraColsReturning = `COALESCE(backend_app_id::text, ''),
	COALESCE(deploy_service_id, ''),
	COALESCE(deploy_url, ''),
	COALESCE(deploy_status, 'pending'),
	COALESCE(deploy_error_message, ''),
	COALESCE(custom_domain, ''),
	COALESCE(owner_id::text, ''),
	(SELECT COALESCE(email, '') FROM zeep_system.dashboard_users WHERE id = owner_id),
	(SELECT COALESCE(name, '') FROM zeep_system.dashboard_users WHERE id = owner_id)`

func scanApp(a *FrontendApp, row pgx.Row) error {
	return row.Scan(&a.ID, &a.Name, &a.Slug, &a.TemplateID, &a.TemplateName,
		&a.GithubRepoURL, &a.Status, &a.ErrorMessage, &a.CreatedBy, &a.CreatedAt, &a.ArchivedAt,
		&a.BackendAppID, &a.DeployServiceID, &a.DeployURL, &a.DeployStatus, &a.DeployErrorMessage, &a.CustomDomain,
		&a.OwnerID, &a.OwnerEmail, &a.OwnerName)
}

func scanAppRows(a *FrontendApp, rows pgx.Rows) error {
	return rows.Scan(&a.ID, &a.Name, &a.Slug, &a.TemplateID, &a.TemplateName,
		&a.GithubRepoURL, &a.Status, &a.ErrorMessage, &a.CreatedBy, &a.CreatedAt, &a.ArchivedAt,
		&a.BackendAppID, &a.DeployServiceID, &a.DeployURL, &a.DeployStatus, &a.DeployErrorMessage, &a.CustomDomain,
		&a.OwnerID, &a.OwnerEmail, &a.OwnerName)
}

func CreateFrontendApp(ctx context.Context, pool *db.Pool, input FrontendAppInput) (*FrontendApp, error) {
	var a FrontendApp
	err := scanApp(&a,
		pool.QueryRow(ctx,
			`INSERT INTO zeep_system.frontend_apps
			 (name, slug, template_id, github_repo_url, status, error_message, created_by, owner_id, backend_app_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, NULLIF($9, '')::uuid)
			 RETURNING id, name, slug, template_id,
			   (SELECT COALESCE(gt.name, '') FROM zeep_system.github_templates gt WHERE gt.id = template_id),
			   github_repo_url, status, error_message, created_by, created_at, archived_at,
			   `+faExtraColsReturning,
			input.Name, input.Slug, input.TemplateID,
			input.GithubRepoURL, input.Status, input.ErrorMessage, input.CreatedBy, input.OwnerID, input.BackendAppID,
		))
	if err != nil {
		return nil, fmt.Errorf("dashboard: create frontend app: %w", err)
	}
	return &a, nil
}

// Returns the frontend app plus the user's role on it. ErrNotFound is returned
// both when the app doesn't exist (including archived — archived apps are
// invisible to everyone) and when the user has no access.
// "Doesn't exist for you" is the security-sensitive default.
func GetFrontendApp(ctx context.Context, pool *db.Pool, id string, user *DashboardUser) (*FrontendApp, AppRole, error) {
	role, err := ResolveAppRole(ctx, pool, user, AppRef{FrontendAppID: id})
	if err != nil {
		if errors.Is(err, ErrInvalidAppRef) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	if !role.Effective() {
		return nil, "", ErrNotFound
	}

	var a FrontendApp
	err = scanApp(&a,
		pool.QueryRow(ctx,
			`SELECT fa.id, fa.name, fa.slug, fa.template_id,
			 COALESCE(gt.name, ''), fa.github_repo_url, fa.status,
			 fa.error_message, fa.created_by, fa.created_at, fa.archived_at,
			 `+faExtraColsSelect+`
			 FROM zeep_system.frontend_apps fa
			 LEFT JOIN zeep_system.github_templates gt ON gt.id = fa.template_id
			 LEFT JOIN zeep_system.dashboard_users u ON u.id = fa.owner_id
			 WHERE fa.id = $1 AND fa.archived_at IS NULL`,
			id,
		))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("dashboard: get frontend app: %w", err)
	}
	return &a, role, nil
}

// superadmin or CanReadAnyApp (admin/auditor global) → all non-archived apps.
// Members → only apps in app_members for this user. Archived apps are always
// invisible.
func ListFrontendApps(ctx context.Context, pool *db.Pool, user *DashboardUser) ([]FrontendApp, error) {
	if user == nil {
		return nil, errors.New("dashboard: ListFrontendApps called with nil user")
	}

	var (
		rows pgx.Rows
		err  error
	)

	if user.Role == "superadmin" || CanReadAnyApp(user.Role) {
		rows, err = pool.Query(ctx,
			`SELECT fa.id, fa.name, fa.slug, fa.template_id,
			 COALESCE(gt.name, ''), fa.github_repo_url, fa.status,
			 fa.error_message, fa.created_by, fa.created_at, fa.archived_at,
			 `+faExtraColsSelect+`
			 FROM zeep_system.frontend_apps fa
			 LEFT JOIN zeep_system.github_templates gt ON gt.id = fa.template_id
			 LEFT JOIN zeep_system.dashboard_users u ON u.id = fa.owner_id
			 WHERE fa.archived_at IS NULL
			 ORDER BY fa.created_at DESC`,
		)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT fa.id, fa.name, fa.slug, fa.template_id,
			 COALESCE(gt.name, ''), fa.github_repo_url, fa.status,
			 fa.error_message, fa.created_by, fa.created_at, fa.archived_at,
			 `+faExtraColsSelect+`
			 FROM zeep_system.frontend_apps fa
			 LEFT JOIN zeep_system.github_templates gt ON gt.id = fa.template_id
			 LEFT JOIN zeep_system.dashboard_users u ON u.id = fa.owner_id
			 INNER JOIN zeep_system.app_members m ON m.frontend_app_id = fa.id AND m.user_id = $1
			 WHERE fa.archived_at IS NULL
			 ORDER BY fa.created_at DESC`,
			user.ID,
		)
	}
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

// DeployedApp is the minimal shape needed to fan out to the deploy provider's
// API for the "Recent Deploys" widget — no need for the full FrontendApp.
type DeployedApp struct {
	ID              string
	Name            string
	DeployServiceID string
}

// ListWithDeployService returns up to limit non-archived frontend apps that
// have a deploy service already created, most recently created first. There
// is no updated_at column on frontend_apps, so creation order is the best
// available proxy for "recently active."
func ListWithDeployService(ctx context.Context, pool *db.Pool, limit int) ([]DeployedApp, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, deploy_service_id
		 FROM zeep_system.frontend_apps
		 WHERE archived_at IS NULL AND deploy_service_id != ''
		 ORDER BY created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list frontend apps with deploy service: %w", err)
	}
	defer rows.Close()

	apps := make([]DeployedApp, 0)
	for rows.Next() {
		var a DeployedApp
		if err := rows.Scan(&a.ID, &a.Name, &a.DeployServiceID); err != nil {
			return nil, fmt.Errorf("dashboard: list frontend apps with deploy service scan: %w", err)
		}
		apps = append(apps, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: list frontend apps with deploy service rows: %w", err)
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
			   github_repo_url, status, error_message, created_by, created_at, archived_at,
			   `+faExtraColsReturning,
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

func UpdateFrontendAppDeploy(ctx context.Context, pool *db.Pool, id, deployServiceID, deployURL, deployStatus, deployErrorMessage string) (*FrontendApp, error) {
	var a FrontendApp
	err := scanApp(&a,
		pool.QueryRow(ctx,
			`UPDATE zeep_system.frontend_apps
			 SET deploy_service_id = $2, deploy_url = $3, deploy_status = $4, deploy_error_message = $5
			 WHERE id = $1 AND archived_at IS NULL
			 RETURNING id, name, slug, template_id,
			   (SELECT COALESCE(gt.name, '') FROM zeep_system.github_templates gt WHERE gt.id = template_id),
			   github_repo_url, status, error_message, created_by, created_at, archived_at,
			   `+faExtraColsReturning,
			id, deployServiceID, deployURL, deployStatus, deployErrorMessage,
		))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: update frontend app deploy: %w", err)
	}
	return &a, nil
}

func UpdateFrontendAppDomain(ctx context.Context, pool *db.Pool, id, customDomain, deployURL string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.frontend_apps
		 SET custom_domain = $2, deploy_url = $3
		 WHERE id = $1 AND archived_at IS NULL`,
		id, customDomain, deployURL,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update frontend app domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Requires CanManage() — admin only. Returns ErrForbidden if the user can see
// the app but cannot archive it; ErrNotFound if the app doesn't exist (or is
// already archived — archived apps are always invisible) or the user
// has no access at all.
func ArchiveFrontendApp(ctx context.Context, pool *db.Pool, id string, user *DashboardUser) error {
	role, err := ResolveAppRole(ctx, pool, user, AppRef{FrontendAppID: id})
	if err != nil {
		return err
	}
	if !role.CanManage() {
		return ErrForbidden
	}

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

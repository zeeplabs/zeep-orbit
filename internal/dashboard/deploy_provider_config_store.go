package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type DeployProviderConfig struct {
	Provider        string    `json:"provider"`
	APIKey          string    `json:"-"`
	RenderProjectID string    `json:"render_project_id"`
	BaseDomain      string    `json:"base_domain"`
	ConnectedAt     time.Time `json:"connected_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func UpsertDeployProviderConfig(ctx context.Context, pool *db.Pool, provider, apiKeyEncrypted, renderProjectID, baseDomain string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.deploy_provider_config (provider, api_key, render_project_id, base_domain, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT ((TRUE)) DO UPDATE SET
		     provider = $1,
		     api_key = $2,
		     render_project_id = $3,
		     base_domain = $4,
		     updated_at = now()`,
		provider, apiKeyEncrypted, renderProjectID, baseDomain,
	)
	if err != nil {
		return fmt.Errorf("dashboard: upsert deploy provider config: %w", err)
	}
	return nil
}

func UpdateDeployProviderConfigFields(ctx context.Context, pool *db.Pool, renderProjectID, baseDomain string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.deploy_provider_config
		 SET render_project_id = $1, base_domain = $2, updated_at = now()
		 WHERE (TRUE)`,
		renderProjectID, baseDomain,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update deploy provider config fields: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func GetDeployProviderConfig(ctx context.Context, pool *db.Pool) (*DeployProviderConfig, error) {
	var cfg DeployProviderConfig
	err := pool.QueryRow(ctx,
		`SELECT provider, api_key, COALESCE(render_project_id, ''), COALESCE(base_domain, ''), connected_at, updated_at
		 FROM zeep_system.deploy_provider_config
		 LIMIT 1`,
	).Scan(&cfg.Provider, &cfg.APIKey, &cfg.RenderProjectID, &cfg.BaseDomain, &cfg.ConnectedAt, &cfg.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get deploy provider config: %w", err)
	}
	return &cfg, nil
}

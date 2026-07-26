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
	Provider    string    `json:"provider"`
	APIKey      string    `json:"-"`
	ConnectedAt time.Time `json:"connected_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func UpsertDeployProviderConfig(ctx context.Context, pool *db.Pool, provider, apiKeyEncrypted string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.deploy_provider_config (provider, api_key, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT ((TRUE)) DO UPDATE SET
		     provider = $1,
		     api_key = $2,
		     updated_at = now()`,
		provider, apiKeyEncrypted,
	)
	if err != nil {
		return fmt.Errorf("dashboard: upsert deploy provider config: %w", err)
	}
	return nil
}

func GetDeployProviderConfig(ctx context.Context, pool *db.Pool) (*DeployProviderConfig, error) {
	var cfg DeployProviderConfig
	err := pool.QueryRow(ctx,
		`SELECT provider, api_key, connected_at, updated_at
		 FROM zeep_system.deploy_provider_config
		 LIMIT 1`,
	).Scan(&cfg.Provider, &cfg.APIKey, &cfg.ConnectedAt, &cfg.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get deploy provider config: %w", err)
	}
	return &cfg, nil
}

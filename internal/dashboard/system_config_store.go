package dashboard

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// SystemConfig represents the zeep_system.system_config singleton row.
type SystemConfig struct {
	SoftDeleteEnabled bool `json:"soft_delete_enabled"`
}

// GetSystemConfig reads the singleton system_config row.
func GetSystemConfig(ctx context.Context, pool *db.Pool) (*SystemConfig, error) {
	var cfg SystemConfig
	err := pool.QueryRow(ctx,
		`SELECT soft_delete_enabled FROM zeep_system.system_config LIMIT 1`,
	).Scan(&cfg.SoftDeleteEnabled)
	if err != nil {
		return &SystemConfig{}, nil
	}
	return &cfg, nil
}

// UpsertSystemConfig inserts or updates the singleton system_config row.
func UpsertSystemConfig(ctx context.Context, pool *db.Pool, softDeleteEnabled bool) (*SystemConfig, error) {
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled)
		 VALUES ($1)
		 ON CONFLICT ((TRUE)) DO UPDATE SET soft_delete_enabled = $1`,
		softDeleteEnabled,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert system config: %w", err)
	}
	return &SystemConfig{SoftDeleteEnabled: softDeleteEnabled}, nil
}

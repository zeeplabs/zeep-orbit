package dashboard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type GlobalStorageConfig struct {
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
}

type SystemConfig struct {
	SoftDeleteEnabled bool                `json:"soft_delete_enabled"`
	StorageConfig     *GlobalStorageConfig `json:"storage_config,omitempty"`
}

func GetSystemConfig(ctx context.Context, pool *db.Pool) (*SystemConfig, error) {
	var cfg SystemConfig
	var rawStorage []byte
	err := pool.QueryRow(ctx,
		`SELECT soft_delete_enabled, storage_config FROM zeep_system.system_config LIMIT 1`,
	).Scan(&cfg.SoftDeleteEnabled, &rawStorage)
	if err != nil {
		return &SystemConfig{}, nil
	}
	if len(rawStorage) > 0 && string(rawStorage) != "{}" {
		var sc GlobalStorageConfig
		if json.Unmarshal(rawStorage, &sc) == nil && sc.Bucket != "" {
			cfg.StorageConfig = &sc
		}
	}
	return &cfg, nil
}

func UpsertSystemConfig(ctx context.Context, pool *db.Pool, softDeleteEnabled bool, storageConfig *GlobalStorageConfig) (*SystemConfig, error) {
	var rawJSON string
	if storageConfig != nil && storageConfig.Bucket != "" {
		b, _ := json.Marshal(storageConfig)
		rawJSON = string(b)
	}
	if rawJSON == "" {
		rawJSON = "{}"
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled, storage_config)
		 VALUES ($1, $2::jsonb)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET soft_delete_enabled = $1,
		       storage_config = $2::jsonb`,
		softDeleteEnabled, rawJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert system config: %w", err)
	}
	return GetSystemConfig(ctx, pool)
}

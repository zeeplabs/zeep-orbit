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
	SoftDeleteEnabled  bool                 `json:"soft_delete_enabled"`
	StorageConfig      *GlobalStorageConfig `json:"storage_config,omitempty"`
	MaxCSVExportRows   int                  `json:"max_csv_export_rows"`
	StatementTimeoutMs int                  `json:"statement_timeout_ms"`
	RequireRLSDefault  bool                 `json:"require_rls_default"`
}

// systemConfigPatch is a partial update: a nil field means "leave unchanged".
type systemConfigPatch struct {
	SoftDeleteEnabled  *bool                `json:"soft_delete_enabled,omitempty"`
	StorageConfig      *GlobalStorageConfig `json:"storage_config,omitempty"`
	MaxCSVExportRows   *int                 `json:"max_csv_export_rows,omitempty"`
	StatementTimeoutMs *int                 `json:"statement_timeout_ms,omitempty"`
	RequireRLSDefault  *bool                `json:"require_rls_default,omitempty"`
}

// mergeSystemConfig overlays only the fields present in the patch onto the
// current config (merge-on-absent — see mergeProviderConfig for the pattern).
func mergeSystemConfig(cur SystemConfig, patch systemConfigPatch) SystemConfig {
	if patch.SoftDeleteEnabled != nil {
		cur.SoftDeleteEnabled = *patch.SoftDeleteEnabled
	}
	if patch.StorageConfig != nil {
		cur.StorageConfig = patch.StorageConfig
	}
	if patch.MaxCSVExportRows != nil {
		cur.MaxCSVExportRows = *patch.MaxCSVExportRows
	}
	if patch.StatementTimeoutMs != nil {
		cur.StatementTimeoutMs = *patch.StatementTimeoutMs
	}
	if patch.RequireRLSDefault != nil {
		cur.RequireRLSDefault = *patch.RequireRLSDefault
	}
	return cur
}

func GetSystemConfig(ctx context.Context, pool *db.Pool) (*SystemConfig, error) {
	var cfg SystemConfig
	var rawStorage []byte
	err := pool.QueryRow(ctx,
		`SELECT soft_delete_enabled, storage_config, COALESCE(max_csv_export_rows, 10000), COALESCE(statement_timeout_ms, 30000), COALESCE(require_rls_default, false)
		 FROM zeep_system.system_config LIMIT 1`,
	).Scan(&cfg.SoftDeleteEnabled, &rawStorage, &cfg.MaxCSVExportRows, &cfg.StatementTimeoutMs, &cfg.RequireRLSDefault)
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

func UpsertSystemConfig(ctx context.Context, pool *db.Pool, cfg *SystemConfig) (*SystemConfig, error) {
	var rawJSON string
	if cfg.StorageConfig != nil && cfg.StorageConfig.Bucket != "" {
		b, _ := json.Marshal(cfg.StorageConfig)
		rawJSON = string(b)
	}
	if rawJSON == "" {
		rawJSON = "{}"
	}
	maxCSV := cfg.MaxCSVExportRows
	if maxCSV <= 0 {
		maxCSV = 10000
	}
	stmtTimeout := cfg.StatementTimeoutMs
	if stmtTimeout < 0 {
		stmtTimeout = 0
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.system_config (soft_delete_enabled, storage_config, max_csv_export_rows, statement_timeout_ms, require_rls_default)
		 VALUES ($1, $2::jsonb, $3, $4, $5)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET soft_delete_enabled = $1,
		       storage_config = $2::jsonb,
		       max_csv_export_rows = $3,
		       statement_timeout_ms = $4,
		       require_rls_default = $5`,
		cfg.SoftDeleteEnabled, rawJSON, maxCSV, stmtTimeout, cfg.RequireRLSDefault,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert system config: %w", err)
	}
	return GetSystemConfig(ctx, pool)
}

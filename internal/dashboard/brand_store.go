package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// BrandConfig represents the brand_config singleton row.
type BrandConfig struct {
	Theme       string    `json:"theme"`
	CompanyName string    `json:"company_name"`
	LogoURL     string    `json:"logo_url"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetBrandConfig returns the brand config from the DB.
// Returns nil if no row exists yet.
func GetBrandConfig(ctx context.Context, pool *db.Pool) (*BrandConfig, error) {
	var c BrandConfig
	err := pool.QueryRow(ctx,
		`SELECT theme, company_name, logo_url, updated_at
		 FROM zeep_system.brand_config
		 LIMIT 1`,
	).Scan(&c.Theme, &c.CompanyName, &c.LogoURL, &c.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get brand config: %w", err)
	}
	return &c, nil
}

// UpsertBrandConfig inserts or updates the singleton brand_config row.
func UpsertBrandConfig(ctx context.Context, pool *db.Pool, theme, companyName, logoURL string) (*BrandConfig, error) {
	var c BrandConfig
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.brand_config (theme, company_name, logo_url)
		 VALUES ($1, $2, $3)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET theme = COALESCE(NULLIF($1, ''), brand_config.theme),
		       company_name = COALESCE(NULLIF($2, ''), brand_config.company_name),
		       logo_url = COALESCE(NULLIF($3, ''), brand_config.logo_url),
		       updated_at = now()
		 RETURNING theme, company_name, logo_url, updated_at`,
		theme, companyName, logoURL,
	).Scan(&c.Theme, &c.CompanyName, &c.LogoURL, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert brand config: %w", err)
	}
	return &c, nil
}

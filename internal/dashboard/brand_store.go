package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type BrandConfig struct {
	Theme       string    `json:"theme"`
	CompanyName string    `json:"company_name"`
	LogoURL     string    `json:"logo_url"`
	IconURL     string    `json:"icon_url"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func GetBrandConfig(ctx context.Context, pool *db.Pool) (*BrandConfig, error) {
	var c BrandConfig
	err := pool.QueryRow(ctx,
		`SELECT theme, company_name, COALESCE(logo_url, ''), COALESCE(icon_url, ''), updated_at
		 FROM zeep_system.brand_config
		 LIMIT 1`,
	).Scan(&c.Theme, &c.CompanyName, &c.LogoURL, &c.IconURL, &c.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get brand config: %w", err)
	}
	return &c, nil
}

// SeedBrandConfig creates the singleton brand_config row on first boot only.
// Unlike UpsertBrandConfig, it never overwrites an existing row — safe to
// call on every server start without resetting user-saved settings.
func SeedBrandConfig(ctx context.Context, pool *db.Pool, theme, companyName string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.brand_config (theme, company_name)
		 VALUES ($1, $2)
		 ON CONFLICT ((TRUE)) DO NOTHING`,
		theme, companyName,
	)
	if err != nil {
		return fmt.Errorf("dashboard: seed brand config: %w", err)
	}
	return nil
}

func UpsertBrandConfig(ctx context.Context, pool *db.Pool, theme, companyName, logoURL, iconURL string) (*BrandConfig, error) {
	var c BrandConfig
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.brand_config (theme, company_name, logo_url, icon_url)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT ((TRUE)) DO UPDATE
		   SET theme = COALESCE(NULLIF($1, ''), brand_config.theme),
		       company_name = COALESCE(NULLIF($2, ''), brand_config.company_name),
		       logo_url = COALESCE(NULLIF($3, ''), brand_config.logo_url),
		       icon_url = COALESCE(NULLIF($4, ''), brand_config.icon_url),
		       updated_at = now()
		 RETURNING theme, company_name, COALESCE(logo_url, ''), COALESCE(icon_url, ''), updated_at`,
		theme, companyName, logoURL, iconURL,
	).Scan(&c.Theme, &c.CompanyName, &c.LogoURL, &c.IconURL, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert brand config: %w", err)
	}
	return &c, nil
}

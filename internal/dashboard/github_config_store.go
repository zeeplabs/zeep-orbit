package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// GitHubAppConfig is the decrypted representation of zeep_system.github_app_config.
// This store is an internal/trusted boundary (not an HTTP response), so secrets are
// returned in cleartext for callers (e.g. the GitHub client builder) to use directly.
type GitHubAppConfig struct {
	AppID          string
	ClientID       string
	ClientSecret   string // decrypted
	PrivateKey     string // decrypted PEM
	WebhookSecret  string // decrypted
	OrgLogin       string
	InstallationID *int64
	InstalledAt    *time.Time
	UpdatedAt      time.Time
}

// GitHubAppConfigInput is the input for UpsertGitHubConfig. Empty sensitive fields
// mean "keep the existing encrypted value" (partial update semantics).
type GitHubAppConfigInput struct {
	AppID         string
	ClientID      string
	ClientSecret  string // empty means "keep existing"
	PrivateKey    string // empty means "keep existing"
	WebhookSecret string // empty means "keep existing"
}

// githubAppConfigRow mirrors the raw (still-encrypted) row.
type githubAppConfigRow struct {
	AppID          string
	ClientID       string
	ClientSecret   string
	PrivateKey     string
	WebhookSecret  string
	OrgLogin       string
	InstallationID *int64
	InstalledAt    *time.Time
	UpdatedAt      time.Time
}

// GetGitHubConfig reads the singleton github_app_config row and decrypts the
// sensitive fields. Returns (nil, nil) if no row exists yet (not configured).
func GetGitHubConfig(ctx context.Context, pool *db.Pool) (*GitHubAppConfig, error) {
	var row githubAppConfigRow
	err := pool.QueryRow(ctx,
		`SELECT app_id, client_id, client_secret, private_key, webhook_secret,
		        org_login, installation_id, installed_at, updated_at
		 FROM zeep_system.github_app_config
		 LIMIT 1`,
	).Scan(&row.AppID, &row.ClientID, &row.ClientSecret, &row.PrivateKey, &row.WebhookSecret,
		&row.OrgLogin, &row.InstallationID, &row.InstalledAt, &row.UpdatedAt)
	if err != nil {
		return nil, nil
	}

	return decryptGitHubConfigRow(&row)
}

// UpsertGitHubConfig creates or updates the singleton github_app_config row,
// encrypting client_secret, private_key, and webhook_secret. When a sensitive
// field in the input is empty, the existing encrypted value is preserved.
func UpsertGitHubConfig(ctx context.Context, pool *db.Pool, input GitHubAppConfigInput) error {
	var existing githubAppConfigRow
	hasExisting := true
	err := pool.QueryRow(ctx,
		`SELECT client_secret, private_key, webhook_secret
		 FROM zeep_system.github_app_config
		 LIMIT 1`,
	).Scan(&existing.ClientSecret, &existing.PrivateKey, &existing.WebhookSecret)
	if err != nil {
		hasExisting = false
	}

	clientSecretEnc, err := preserveOrEncrypt(input.ClientSecret, hasExisting, existing.ClientSecret)
	if err != nil {
		return fmt.Errorf("dashboard: encrypt client_secret: %w", err)
	}
	privateKeyEnc, err := preserveOrEncrypt(input.PrivateKey, hasExisting, existing.PrivateKey)
	if err != nil {
		return fmt.Errorf("dashboard: encrypt private_key: %w", err)
	}
	webhookSecretEnc, err := preserveOrEncrypt(input.WebhookSecret, hasExisting, existing.WebhookSecret)
	if err != nil {
		return fmt.Errorf("dashboard: encrypt webhook_secret: %w", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO zeep_system.github_app_config
		    (app_id, client_id, client_secret, private_key, webhook_secret, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT ((TRUE)) DO UPDATE SET
		    app_id = $1,
		    client_id = $2,
		    client_secret = $3,
		    private_key = $4,
		    webhook_secret = $5,
		    updated_at = now()`,
		input.AppID, input.ClientID, clientSecretEnc, privateKeyEnc, webhookSecretEnc,
	)
	if err != nil {
		return fmt.Errorf("dashboard: upsert github config: %w", err)
	}

	return nil
}

// preserveOrEncrypt encrypts newValue if non-empty; otherwise it returns the
// existing encrypted value unchanged (no decrypt/re-encrypt round-trip needed).
func preserveOrEncrypt(newValue string, hasExisting bool, existingEncrypted string) (string, error) {
	if newValue == "" {
		if hasExisting {
			return existingEncrypted, nil
		}
		return "", nil
	}
	return crypto.Encrypt(newValue)
}

func decryptGitHubConfigRow(row *githubAppConfigRow) (*GitHubAppConfig, error) {
	cfg := &GitHubAppConfig{
		AppID:          row.AppID,
		ClientID:       row.ClientID,
		OrgLogin:       row.OrgLogin,
		InstallationID: row.InstallationID,
		InstalledAt:    row.InstalledAt,
		UpdatedAt:      row.UpdatedAt,
	}

	if row.ClientSecret != "" {
		decrypted, err := crypto.Decrypt(row.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("dashboard: decrypt client_secret: %w", err)
		}
		cfg.ClientSecret = decrypted
	}

	if row.PrivateKey != "" {
		decrypted, err := crypto.Decrypt(row.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("dashboard: decrypt private_key: %w", err)
		}
		cfg.PrivateKey = decrypted
	}

	if row.WebhookSecret != "" {
		decrypted, err := crypto.Decrypt(row.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("dashboard: decrypt webhook_secret: %w", err)
		}
		cfg.WebhookSecret = decrypted
	}

	return cfg, nil
}

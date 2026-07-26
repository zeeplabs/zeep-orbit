package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/crypto"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// GitHubAppConfig is the decrypted representation of zeep_system.github_app_config.
// This store is an internal/trusted boundary (not an HTTP response), so secrets are
// returned in cleartext for callers (e.g. the GitHub client builder) to use directly.
type GitHubAppConfig struct {
	AppID          string
	AppSlug        string // not a secret: visible on the GitHub App's own settings page
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
	AppSlug       string
	ClientID      string
	ClientSecret  string // empty means "keep existing"
	PrivateKey    string // empty means "keep existing"
	WebhookSecret string // empty means "keep existing"
}

// githubAppConfigRow mirrors the raw (still-encrypted) row.
type githubAppConfigRow struct {
	AppID          string
	AppSlug        string
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
		`SELECT app_id, app_slug, client_id, client_secret, private_key, webhook_secret,
		        org_login, installation_id, installed_at, updated_at
		 FROM zeep_system.github_app_config
		 LIMIT 1`,
	).Scan(&row.AppID, &row.AppSlug, &row.ClientID, &row.ClientSecret, &row.PrivateKey, &row.WebhookSecret,
		&row.OrgLogin, &row.InstallationID, &row.InstalledAt, &row.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: get github config: %w", err)
	}

	return decryptGitHubConfigRow(&row)
}

// UpsertGitHubConfig creates or updates the singleton github_app_config row,
// encrypting client_secret, private_key, and webhook_secret. When a sensitive
// field in the input is empty, the existing encrypted value is preserved.
//
// Preservation is enforced in SQL (CASE WHEN $n = ” THEN <existing column>
// ELSE $n END), not via a Go-side pre-read: a pre-read that fails for reasons
// other than "no row yet" (connection blip, timeout, context cancellation)
// must never be allowed to silently wipe an existing secret to empty string.
// The database itself is the single source of truth for "keep existing".
func UpsertGitHubConfig(ctx context.Context, pool *db.Pool, input GitHubAppConfigInput) error {
	clientSecretEnc, err := encryptIfNonEmpty(input.ClientSecret)
	if err != nil {
		return fmt.Errorf("dashboard: encrypt client_secret: %w", err)
	}
	privateKeyEnc, err := encryptIfNonEmpty(input.PrivateKey)
	if err != nil {
		return fmt.Errorf("dashboard: encrypt private_key: %w", err)
	}
	webhookSecretEnc, err := encryptIfNonEmpty(input.WebhookSecret)
	if err != nil {
		return fmt.Errorf("dashboard: encrypt webhook_secret: %w", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO zeep_system.github_app_config
		    (app_id, app_slug, client_id, client_secret, private_key, webhook_secret, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now())
		 ON CONFLICT ((TRUE)) DO UPDATE SET
		    app_id = $1,
		    app_slug = $2,
		    client_id = $3,
		    client_secret = CASE WHEN $4 = '' THEN github_app_config.client_secret ELSE $4 END,
		    private_key = CASE WHEN $5 = '' THEN github_app_config.private_key ELSE $5 END,
		    webhook_secret = CASE WHEN $6 = '' THEN github_app_config.webhook_secret ELSE $6 END,
		    updated_at = now()`,
		input.AppID, input.AppSlug, input.ClientID, clientSecretEnc, privateKeyEnc, webhookSecretEnc,
	)
	if err != nil {
		return fmt.Errorf("dashboard: upsert github config: %w", err)
	}

	return nil
}

// UpdateGitHubInstallation persists the installation details learned from the
// GitHub App installation callback onto the existing singleton config row.
// It does not touch credential fields.
func UpdateGitHubInstallation(ctx context.Context, pool *db.Pool, installationID int64, orgLogin string, installedAt time.Time) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.github_app_config
		 SET installation_id = $1, org_login = $2, installed_at = $3
		 WHERE (TRUE)`,
		installationID, orgLogin, installedAt,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update github installation: %w", err)
	}
	return nil
}

// DeleteGitHubConfig removes the singleton github_app_config row entirely.
// Templates in zeep_system.github_templates are a separate table and are
// left untouched, so previously generated repos remain discoverable even
// after credentials are removed; only new repo-generation calls fail once
// there is no config to build a GitHub client from.
func DeleteGitHubConfig(ctx context.Context, pool *db.Pool) error {
	_, err := pool.Exec(ctx, `DELETE FROM zeep_system.github_app_config`)
	if err != nil {
		return fmt.Errorf("dashboard: delete github config: %w", err)
	}
	return nil
}

// encryptIfNonEmpty encrypts newValue if non-empty; an empty string is passed
// through unchanged so the SQL CASE WHEN guard in UpsertGitHubConfig can tell
// "keep existing" apart from a legitimate encrypted value.
func encryptIfNonEmpty(newValue string) (string, error) {
	if newValue == "" {
		return "", nil
	}
	return crypto.Encrypt(newValue)
}

func decryptGitHubConfigRow(row *githubAppConfigRow) (*GitHubAppConfig, error) {
	cfg := &GitHubAppConfig{
		AppID:          row.AppID,
		AppSlug:        row.AppSlug,
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

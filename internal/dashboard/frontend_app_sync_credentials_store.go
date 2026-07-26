package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

type SyncCredential struct {
	ID                   string    `json:"id"`
	FrontendAppID        string    `json:"frontend_app_id"`
	GithubKeyID          *int64    `json:"github_key_id,omitempty"`
	PublicKey            string    `json:"public_key"`
	PrivateKeyEncrypted  string    `json:"-"`
	SyncStatus           string    `json:"sync_status"`
	ErrorMessage         string    `json:"error_message,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func CreateSyncCredential(ctx context.Context, pool *db.Pool, frontendAppID string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.frontend_app_sync_credentials (frontend_app_id)
		 VALUES ($1)`,
		frontendAppID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: create sync credential: %w", err)
	}
	return nil
}

func GetSyncCredential(ctx context.Context, pool *db.Pool, frontendAppID string) (*SyncCredential, error) {
	var sc SyncCredential
	err := pool.QueryRow(ctx,
		`SELECT id, frontend_app_id, github_key_id, public_key,
		        private_key_encrypted, sync_status, error_message,
		        created_at, updated_at
		 FROM zeep_system.frontend_app_sync_credentials
		 WHERE frontend_app_id = $1`,
		frontendAppID,
	).Scan(&sc.ID, &sc.FrontendAppID, &sc.GithubKeyID, &sc.PublicKey,
		&sc.PrivateKeyEncrypted, &sc.SyncStatus, &sc.ErrorMessage,
		&sc.CreatedAt, &sc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get sync credential: %w", err)
	}
	return &sc, nil
}

func UpdateSyncCredentialSuccess(ctx context.Context, pool *db.Pool, frontendAppID string, githubKeyID int64, publicKey, privateKeyEncrypted string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.frontend_app_sync_credentials
		 SET sync_status = 'ready',
		     github_key_id = $2,
		     public_key = $3,
		     private_key_encrypted = $4,
		     error_message = '',
		     updated_at = now()
		 WHERE frontend_app_id = $1`,
		frontendAppID, githubKeyID, publicKey, privateKeyEncrypted,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update sync credential success: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func UpdateSyncCredentialFailure(ctx context.Context, pool *db.Pool, frontendAppID, errorMessage string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.frontend_app_sync_credentials
		 SET sync_status = 'failed',
		     error_message = $2,
		     updated_at = now()
		 WHERE frontend_app_id = $1`,
		frontendAppID, errorMessage,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update sync credential failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

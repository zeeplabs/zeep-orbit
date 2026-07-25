package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func randomJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b2 := make([]byte, 16)
		for i := range b2 {
			b2[i] = byte(i * 17)
		}
		return hex.EncodeToString(b2)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b)
}

type AppTokenRow struct {
	ID         string     `json:"id"`
	AppID      string     `json:"app_id"`
	Name       string     `json:"name"`
	JTI        string     `json:"jti"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateAppTokenInput struct {
	AppID     string
	Name      string
	ExpiresAt *time.Time
}

func ListAppTokens(ctx context.Context, pool *db.Pool, appID string) ([]AppTokenRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, app_id, name, jti, expires_at, revoked_at, last_used_at, created_at
		 FROM zeep_system.app_tokens WHERE app_id = $1 ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []AppTokenRow
	for rows.Next() {
		var t AppTokenRow
		if err := rows.Scan(&t.ID, &t.AppID, &t.Name, &t.JTI, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func CreateAppToken(ctx context.Context, pool *db.Pool, input CreateAppTokenInput) (*AppTokenRow, error) {
	jti := randomJTI()
	var t AppTokenRow
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.app_tokens (app_id, name, jti, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, app_id, name, jti, expires_at, revoked_at, last_used_at, created_at`,
		input.AppID, input.Name, jti, input.ExpiresAt,
	).Scan(&t.ID, &t.AppID, &t.Name, &t.JTI, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func RevokeAppToken(ctx context.Context, pool *db.Pool, tokenID string, appID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE id = $1 AND app_id = $2 AND revoked_at IS NULL`,
		tokenID, appID)
	return err
}

func RevokeAllAppTokens(ctx context.Context, pool *db.Pool, appID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE app_id = $1 AND revoked_at IS NULL`,
		appID)
	return err
}

func GetAppTokenByJTI(ctx context.Context, pool *db.Pool, jti string) (*AppTokenRow, error) {
	var t AppTokenRow
	err := pool.QueryRow(ctx,
		`SELECT id, app_id, name, jti, expires_at, revoked_at, last_used_at, created_at
		 FROM zeep_system.app_tokens WHERE jti = $1`, jti,
	).Scan(&t.ID, &t.AppID, &t.Name, &t.JTI, &t.ExpiresAt, &t.RevokedAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func TouchAppToken(ctx context.Context, pool *db.Pool, jti string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.app_tokens SET last_used_at = now() WHERE jti = $1`, jti)
	return err
}

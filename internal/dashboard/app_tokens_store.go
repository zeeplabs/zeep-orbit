package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/tokencache"
)

// ErrAppTokensNotSupported is returned by ListAppTokensForUser when the app
// has AuthEmailEnabled == true — app tokens are only issued for apps whose
// end users don't authenticate with email/password (handler.go:2916-2919's
// existing business rule, preserved verbatim during extraction). This is a
// feature-availability check, not a permission check — the caller sees a
// distinct error, never an empty list (which would otherwise read as "zero
// tokens issued" instead of "not applicable to this app").
var ErrAppTokensNotSupported = errors.New("app tokens only available for apps without email auth")

func randomJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("app_tokens: failed to generate jti: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b), nil
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

// ListAppTokensForUser is the shared operation behind the ListAppTokens REST
// handler and orbit_list_app_tokens (mcp-read-only-tools T8/T9): resolve+
// authorize the app, enforce the existing email-auth business rule, then
// return token metadata. Authorization here is visibility-only — GetApp's
// not-found check, no CanManage() tier on top (matching handler.go's
// pre-extraction body exactly, which never checked role at all for this
// endpoint). This is intentionally different from the CanManage() tier used
// by table policies/members/webhooks — see design.md's Authorization Matrix.
func ListAppTokensForUser(ctx context.Context, pool *db.Pool, user *DashboardUser, appID string) ([]AppTokenRow, error) {
	app, _, err := GetApp(ctx, pool, appID, user)
	if err != nil {
		return nil, err
	}
	if app.AuthEmailEnabled {
		return nil, ErrAppTokensNotSupported
	}
	return ListAppTokens(ctx, pool, appID)
}

func CreateAppToken(ctx context.Context, pool *db.Pool, input CreateAppTokenInput) (*AppTokenRow, error) {
	jti, err := randomJTI()
	if err != nil {
		return nil, err
	}
	var t AppTokenRow
	err = pool.QueryRow(ctx,
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
	var jti string
	err := pool.QueryRow(ctx,
		`UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE id = $1 AND app_id = $2 AND revoked_at IS NULL RETURNING jti`,
		tokenID, appID).Scan(&jti)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	tokencache.Invalidate(jti)
	return nil
}

func RevokeAllAppTokens(ctx context.Context, pool *db.Pool, appID string) error {
	rows, err := pool.Query(ctx,
		`UPDATE zeep_system.app_tokens SET revoked_at = now() WHERE app_id = $1 AND revoked_at IS NULL RETURNING jti`,
		appID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var jtis []string
	for rows.Next() {
		var jti string
		if err := rows.Scan(&jti); err != nil {
			return err
		}
		jtis = append(jtis, jti)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, jti := range jtis {
		tokencache.Invalidate(jti)
	}
	return nil
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

package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// authCodeTTL is how long an authorization code is valid before Token (T20)
// must reject it — short-lived by design (design.md Data Models), unlike a
// manual PAT which has no forced expiry.
const authCodeTTL = 10 * time.Minute

// ErrAuthCodeNotFound is returned when a presented code's hash matches no
// row.
var ErrAuthCodeNotFound = errors.New("dashboard: oauth authorization code not found")

// ErrAuthCodeUsed is returned when a presented code has already been
// consumed once — OAuth 2.1 treats a second exchange attempt as a
// compromise signal (design.md Error Handling Strategy), not a retryable
// error.
var ErrAuthCodeUsed = errors.New("dashboard: oauth authorization code already used")

// ErrAuthCodeExpired is returned when a presented code's expires_at has
// passed.
var ErrAuthCodeExpired = errors.New("dashboard: oauth authorization code expired")

// AuthCodeRow is a row from zeep_system.oauth_auth_codes. code_hash is
// intentionally not a field — ConsumeAuthCode/CreateAuthCode never expose
// it, so there's no accidental leak path.
type AuthCodeRow struct {
	ID            string
	ClientID      string
	UserID        string
	CodeChallenge string
	RedirectURI   string
	UsedAt        *time.Time
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

// hashOAuthCode is the one-way SHA-256 hash used to store/resolve an
// authorization code — same pattern as PATStore's hashPATToken, kept local
// to this file rather than shared: both are a plain 3-line sha256-hex
// primitive, not a shared abstraction worth introducing across files.
func hashOAuthCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// CreateAuthCode generates a single-use authorization code (via
// generateToken — same entropy source PATs use), stores only its SHA-256
// hash bound to clientID/userID/codeChallenge/redirectURI, and returns the
// plaintext exactly once. Consumed by the grant branch of Authorize (T19).
func CreateAuthCode(ctx context.Context, pool *db.Pool, clientID, userID, codeChallenge, redirectURI string) (string, AuthCodeRow, error) {
	code, err := generateToken()
	if err != nil {
		return "", AuthCodeRow{}, fmt.Errorf("dashboard: generate oauth auth code: %w", err)
	}
	codeHash := hashOAuthCode(code)
	expiresAt := time.Now().Add(authCodeTTL)

	var row AuthCodeRow
	err = pool.QueryRow(ctx,
		`INSERT INTO zeep_system.oauth_auth_codes (code_hash, client_id, user_id, code_challenge, redirect_uri, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, client_id, user_id, code_challenge, redirect_uri, used_at, expires_at, created_at`,
		codeHash, clientID, userID, codeChallenge, redirectURI, expiresAt,
	).Scan(&row.ID, &row.ClientID, &row.UserID, &row.CodeChallenge, &row.RedirectURI, &row.UsedAt, &row.ExpiresAt, &row.CreatedAt)
	if err != nil {
		return "", AuthCodeRow{}, fmt.Errorf("dashboard: create oauth auth code: %w", err)
	}
	return code, row, nil
}

// ConsumeAuthCode looks up a presented code by hash and marks it used in
// the same transaction (SELECT ... FOR UPDATE, so two concurrent exchange
// attempts for the same code can't both observe used_at IS NULL) — a
// second exchange attempt for an already-used code always sees
// ErrAuthCodeUsed, never a race where both succeed. Rejects an unknown,
// already-used, or expired code without consuming it further (design.md
// Error Handling Strategy: reused/expired code -> invalid_grant, no token
// issued). Consumed by Token's authorization_code grant (T20).
func ConsumeAuthCode(ctx context.Context, pool *db.Pool, presentedCode string) (AuthCodeRow, error) {
	codeHash := hashOAuthCode(presentedCode)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return AuthCodeRow{}, fmt.Errorf("dashboard: consume oauth auth code begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var row AuthCodeRow
	err = tx.QueryRow(ctx,
		`SELECT id, client_id, user_id, code_challenge, redirect_uri, used_at, expires_at, created_at
		 FROM zeep_system.oauth_auth_codes WHERE code_hash = $1 FOR UPDATE`,
		codeHash,
	).Scan(&row.ID, &row.ClientID, &row.UserID, &row.CodeChallenge, &row.RedirectURI, &row.UsedAt, &row.ExpiresAt, &row.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AuthCodeRow{}, ErrAuthCodeNotFound
		}
		return AuthCodeRow{}, fmt.Errorf("dashboard: consume oauth auth code lookup: %w", err)
	}
	if row.UsedAt != nil {
		return AuthCodeRow{}, ErrAuthCodeUsed
	}
	if row.ExpiresAt.Before(time.Now()) {
		return AuthCodeRow{}, ErrAuthCodeExpired
	}

	if _, err := tx.Exec(ctx, `UPDATE zeep_system.oauth_auth_codes SET used_at = now() WHERE id = $1`, row.ID); err != nil {
		return AuthCodeRow{}, fmt.Errorf("dashboard: mark oauth auth code used: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AuthCodeRow{}, fmt.Errorf("dashboard: consume oauth auth code commit: %w", err)
	}
	return row, nil
}

// PurgeExpiredAuthCodes hard-deletes authorization codes past their
// expires_at — unconditional (no retention-days knob like
// PurgeExpiredSoftDeletes' RetentionDays config), since a code is only ever
// useful for the ~10 minutes before Token would reject it as expired
// anyway (design.md Data Models: "purged ... more aggressively" than the
// webhook_deliveries retention pattern).
func PurgeExpiredAuthCodes(ctx context.Context, pool *db.Pool) (int, error) {
	tag, err := pool.Exec(ctx, `DELETE FROM zeep_system.oauth_auth_codes WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("dashboard: purge expired oauth auth codes: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

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

// PAT kinds — see design.md Data Models (dashboard_pats.kind).
const (
	PATKindManual    = "manual"
	PATKindEphemeral = "ephemeral"
	PATKindOAuth     = "oauth"
)

// ErrPATNotFound is returned when a presented token's hash matches no row,
// when a PAT id doesn't belong to the requesting user (ownership-scoped
// lookups never distinguish "doesn't exist" from "not yours" — mirrors the
// webhook-mapping ownership-scoping fix), or when the PAT's owning
// dashboard_users row no longer exists.
var ErrPATNotFound = errors.New("dashboard: personal access token not found")

// ErrPATRevoked is returned by ResolvePAT for a token whose revoked_at is set.
var ErrPATRevoked = errors.New("dashboard: personal access token revoked")

// ErrPATExpired is returned by ResolvePAT for a token whose expires_at has
// passed.
var ErrPATExpired = errors.New("dashboard: personal access token expired")

// ErrPATExpiryRequired is returned by CreatePAT when kind is "ephemeral" or
// "oauth" and no expiresAt was supplied — only "manual" PATs (Settings UI)
// have no forced expiry in V1 (design.md Data Models).
var ErrPATExpiryRequired = errors.New("dashboard: expires_at is required for ephemeral and oauth PATs")

// PATRow is a row from zeep_system.dashboard_pats. token_hash is
// intentionally not a field here — ListPATs/CreatePAT never expose it, so
// there's no accidental JSON leak path.
type PATRow struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	OAuthClientID *string    `json:"oauth_client_id,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// hashPATToken is the one-way SHA-256 hash used to store/resolve a PAT —
// never redisplayed after creation, so a one-way hash is sufficient (design.md
// Tech Decisions: PAT storage).
func hashPATToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreatePAT generates a token via generateToken (handler.go — same entropy
// source already trusted for webhook tokens), stores only its SHA-256 hash,
// and returns the plaintext exactly once. kind="manual" allows a nil
// expiresAt (no forced expiry); kind="ephemeral"/"oauth" require one.
func CreatePAT(ctx context.Context, pool *db.Pool, userID, name, kind string, expiresAt *time.Time) (string, PATRow, error) {
	if (kind == PATKindEphemeral || kind == PATKindOAuth) && expiresAt == nil {
		return "", PATRow{}, ErrPATExpiryRequired
	}

	token, err := generateToken()
	if err != nil {
		return "", PATRow{}, fmt.Errorf("dashboard: generate PAT: %w", err)
	}
	tokenHash := hashPATToken(token)

	var row PATRow
	err = pool.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_pats (user_id, name, token_hash, kind, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, name, kind, oauth_client_id, expires_at, revoked_at, last_used_at, created_at`,
		userID, name, tokenHash, kind, expiresAt,
	).Scan(&row.ID, &row.UserID, &row.Name, &row.Kind, &row.OAuthClientID, &row.ExpiresAt, &row.RevokedAt, &row.LastUsedAt, &row.CreatedAt)
	if err != nil {
		return "", PATRow{}, fmt.Errorf("dashboard: create PAT: %w", err)
	}
	return token, row, nil
}

// ResolvePAT hashes the presented plaintext token and resolves it to the
// issuing admin's DashboardUser — the same shape RequireAuth's session check
// produces, so downstream code can't tell which auth path ran. Every call
// hits the DB directly (no caching), per AGENTS.md's no-in-memory-session
// rule: revocation and owning-user deletion must take effect immediately,
// with no propagation delay.
func ResolvePAT(ctx context.Context, pool *db.Pool, presentedToken string) (*DashboardUser, error) {
	tokenHash := hashPATToken(presentedToken)

	var revokedAt, expiresAt *time.Time
	var userID string
	err := pool.QueryRow(ctx,
		`SELECT user_id, revoked_at, expires_at FROM zeep_system.dashboard_pats WHERE token_hash = $1`,
		tokenHash,
	).Scan(&userID, &revokedAt, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPATNotFound
		}
		return nil, fmt.Errorf("dashboard: resolve PAT: %w", err)
	}
	if revokedAt != nil {
		return nil, ErrPATRevoked
	}
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return nil, ErrPATExpired
	}

	user, err := GetUser(ctx, pool, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrPATNotFound
		}
		return nil, fmt.Errorf("dashboard: resolve PAT owning user: %w", err)
	}
	return user, nil
}

// ListPATs returns every PAT belonging to userID, newest first. Never
// includes the token value (PATRow has no such field).
func ListPATs(ctx context.Context, pool *db.Pool, userID string) ([]PATRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, name, kind, oauth_client_id, expires_at, revoked_at, last_used_at, created_at
		 FROM zeep_system.dashboard_pats WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list PATs: %w", err)
	}
	defer rows.Close()

	result := make([]PATRow, 0)
	for rows.Next() {
		var row PATRow
		if err := rows.Scan(&row.ID, &row.UserID, &row.Name, &row.Kind, &row.OAuthClientID, &row.ExpiresAt, &row.RevokedAt, &row.LastUsedAt, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: scan PAT row: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// RevokePAT sets revoked_at, scoped to the requesting user's own tokens — a
// PAT id must never be revocable by another user guessing/reusing it (mirrors
// the webhook-mapping IDOR fix). Returns ErrPATNotFound both when the id
// doesn't exist and when it belongs to someone else, so neither case leaks
// which one occurred.
func RevokePAT(ctx context.Context, pool *db.Pool, userID, patID string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.dashboard_pats SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		patID, userID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: revoke PAT %s: %w", patID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPATNotFound
	}
	return nil
}

// TouchLastUsed best-effort updates last_used_at for the Settings UI's "last
// used" column. Callers (RequirePAT) must never fail the underlying request
// on this call's error — it's a courtesy write, not part of the auth
// decision.
func TouchLastUsed(ctx context.Context, pool *db.Pool, patID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.dashboard_pats SET last_used_at = now() WHERE id = $1`,
		patID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: touch PAT last_used_at %s: %w", patID, err)
	}
	return nil
}

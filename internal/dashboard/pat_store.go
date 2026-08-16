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
	user, _, err := ResolvePATWithID(ctx, pool, presentedToken)
	return user, err
}

// ResolvePATWithID is ResolvePAT plus the resolved PAT row's own id — used by
// internal/mcpserver's RequirePAT, which needs the id both to fire
// TouchLastUsed and (from T9 on) to key its per-PAT rate limiter. Kept as a
// separate exported function rather than changing ResolvePAT's signature so
// every existing ResolvePAT caller/test is unaffected; both share the same
// validation logic below.
func ResolvePATWithID(ctx context.Context, pool *db.Pool, presentedToken string) (*DashboardUser, string, error) {
	tokenHash := hashPATToken(presentedToken)

	var revokedAt, expiresAt *time.Time
	var patID, userID string
	err := pool.QueryRow(ctx,
		`SELECT id, user_id, revoked_at, expires_at FROM zeep_system.dashboard_pats WHERE token_hash = $1`,
		tokenHash,
	).Scan(&patID, &userID, &revokedAt, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrPATNotFound
		}
		return nil, "", fmt.Errorf("dashboard: resolve PAT: %w", err)
	}
	if revokedAt != nil {
		return nil, "", ErrPATRevoked
	}
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return nil, "", ErrPATExpired
	}

	user, err := GetUser(ctx, pool, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, "", ErrPATNotFound
		}
		return nil, "", fmt.Errorf("dashboard: resolve PAT owning user: %w", err)
	}
	return user, patID, nil
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

// ErrRefreshTokenNotFound is returned by RotateOAuthRefreshToken when a
// presented refresh token's hash matches no row.
var ErrRefreshTokenNotFound = errors.New("dashboard: refresh token not found")

// ErrRefreshTokenClientMismatch is returned by RotateOAuthRefreshToken when
// the presented client_id doesn't match the token's issuing client (RFC
// 6749 §6 / OAuth 2.1 §4.3: a public client must identify itself at refresh
// the same as at the initial token exchange). No rotation or revocation
// happens on this path — an attacker presenting a stolen token under the
// wrong client_id gets no signal either way, and the legitimate token stays
// valid for the client that actually owns it.
var ErrRefreshTokenClientMismatch = errors.New("dashboard: refresh token client_id mismatch")

// ErrRefreshTokenReused is returned by RotateOAuthRefreshToken when a
// presented refresh token matches a row that was already superseded by an
// earlier rotation (or otherwise revoked) — the standard refresh-token-
// reuse signal (design.md Tech Decisions). The entire token family sharing
// that row's family_id is revoked as a side effect before this error is
// returned, so the caller doesn't need a separate revocation step.
var ErrRefreshTokenReused = errors.New("dashboard: refresh token reused; token family revoked")

// oauthAccessTokenTTL is how long an OAuth-issued access token (PAT
// kind="oauth") is valid before Token's refresh_token grant (T20) must be
// used to obtain a new one — short by design (design.md Data Models:
// "set for ephemeral and oauth tokens"), unlike a manual PAT's no forced
// expiry.
const oauthAccessTokenTTL = time.Hour

// CreateOAuthAccessToken mints a kind="oauth" PAT row for oauthClientID,
// stamped with familyID — the lineage every row descended from the same
// OAuth grant (the original code exchange, plus every refresh rotation
// since) shares, so RotateOAuthRefreshToken can revoke the whole family in
// one UPDATE on reuse detection. Pass familyID="" for a brand-new grant (a
// fresh family_id is generated); pass the existing family's id when minting
// a rotated replacement. Returns the plaintext access token exactly once,
// same one-way-hash contract as CreatePAT.
func CreateOAuthAccessToken(ctx context.Context, q db.Querier, userID, oauthClientID, familyID string, expiresAt time.Time) (string, PATRow, error) {
	token, err := generateToken()
	if err != nil {
		return "", PATRow{}, fmt.Errorf("dashboard: generate oauth access token: %w", err)
	}
	tokenHash := hashPATToken(token)

	var family *string
	if familyID != "" {
		family = &familyID
	}

	var row PATRow
	err = q.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_pats (user_id, name, token_hash, kind, oauth_client_id, family_id, expires_at)
		 VALUES ($1, 'oauth', $2, 'oauth', $3, COALESCE($4, gen_random_uuid()), $5)
		 RETURNING id, user_id, name, kind, oauth_client_id, expires_at, revoked_at, last_used_at, created_at`,
		userID, tokenHash, oauthClientID, family, expiresAt,
	).Scan(&row.ID, &row.UserID, &row.Name, &row.Kind, &row.OAuthClientID, &row.ExpiresAt, &row.RevokedAt, &row.LastUsedAt, &row.CreatedAt)
	if err != nil {
		return "", PATRow{}, fmt.Errorf("dashboard: create oauth access token: %w", err)
	}
	return token, row, nil
}

// SetRefreshToken generates a new refresh token (via generateToken, same
// entropy source every other token in this design uses) and stores its
// hash on patID's row — the "follow-up call" design.md's OAuthServer
// component describes attaching a refresh token onto the access-token PAT
// row CreateOAuthAccessToken just created. Returns the plaintext exactly
// once.
func SetRefreshToken(ctx context.Context, q db.Querier, patID string) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("dashboard: generate refresh token: %w", err)
	}
	tokenHash := hashPATToken(token)

	if _, err := q.Exec(ctx,
		`UPDATE zeep_system.dashboard_pats SET refresh_token_hash = $1 WHERE id = $2`,
		tokenHash, patID,
	); err != nil {
		return "", fmt.Errorf("dashboard: set refresh token for PAT %s: %w", patID, err)
	}
	return token, nil
}

// RotateOAuthRefreshToken implements OAuth 2.1 refresh-token rotation with
// reuse detection (design.md Tech Decisions). Looks up presentedRefreshToken
// by hash:
//   - unknown hash -> ErrRefreshTokenNotFound.
//   - hash belongs to a row that's already revoked (superseded by an
//     earlier rotation, or otherwise revoked) -> this is reuse of a leaked
//     or replayed refresh token; every row sharing that family_id is
//     revoked (the access token issued alongside it, and any tokens minted
//     from later rotations), then ErrRefreshTokenReused is returned.
//   - hash belongs to the current, unrevoked row -> standard rotation: that
//     row is revoked (superseded), and a brand-new PAT row is minted in the
//     same family with fresh access+refresh tokens.
func RotateOAuthRefreshToken(ctx context.Context, pool *db.Pool, presentedRefreshToken, expectedClientID string) (accessToken, refreshToken string, row PATRow, err error) {
	tokenHash := hashPATToken(presentedRefreshToken)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", "", PATRow{}, fmt.Errorf("dashboard: rotate refresh token begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var patID, userID string
	var oauthClientID, familyID *string
	var revokedAt *time.Time
	// FOR UPDATE: two concurrent rotations presenting the same refresh
	// token must not both observe revoked_at IS NULL — without this lock,
	// both could pass the check below and each mint a fresh, live token
	// family, defeating reuse detection exactly when it matters (a leaked
	// token replayed at the same moment as the legitimate client's refresh).
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, oauth_client_id, family_id, revoked_at FROM zeep_system.dashboard_pats WHERE refresh_token_hash = $1 FOR UPDATE`,
		tokenHash,
	).Scan(&patID, &userID, &oauthClientID, &familyID, &revokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", PATRow{}, ErrRefreshTokenNotFound
		}
		return "", "", PATRow{}, fmt.Errorf("dashboard: rotate refresh token lookup: %w", err)
	}
	if familyID == nil {
		return "", "", PATRow{}, fmt.Errorf("dashboard: oauth PAT %s has no family_id", patID)
	}

	tokenClientID := ""
	if oauthClientID != nil {
		tokenClientID = *oauthClientID
	}
	if expectedClientID == "" || expectedClientID != tokenClientID {
		return "", "", PATRow{}, ErrRefreshTokenClientMismatch
	}

	if revokedAt != nil {
		if _, revokeErr := tx.Exec(ctx,
			`UPDATE zeep_system.dashboard_pats SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`,
			*familyID,
		); revokeErr != nil {
			return "", "", PATRow{}, fmt.Errorf("dashboard: revoke oauth family %s: %w", *familyID, revokeErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return "", "", PATRow{}, fmt.Errorf("dashboard: revoke oauth family %s commit: %w", *familyID, err)
		}
		return "", "", PATRow{}, ErrRefreshTokenReused
	}

	if _, err := tx.Exec(ctx, `UPDATE zeep_system.dashboard_pats SET revoked_at = now() WHERE id = $1`, patID); err != nil {
		return "", "", PATRow{}, fmt.Errorf("dashboard: supersede oauth PAT %s: %w", patID, err)
	}

	newAccessToken, newRow, err := CreateOAuthAccessToken(ctx, tx, userID, tokenClientID, *familyID, time.Now().Add(oauthAccessTokenTTL))
	if err != nil {
		return "", "", PATRow{}, err
	}
	newRefreshToken, err := SetRefreshToken(ctx, tx, newRow.ID)
	if err != nil {
		return "", "", PATRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", PATRow{}, fmt.Errorf("dashboard: rotate refresh token commit: %w", err)
	}
	return newAccessToken, newRefreshToken, newRow, nil
}

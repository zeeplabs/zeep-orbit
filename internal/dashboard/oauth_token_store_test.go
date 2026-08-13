package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCreateOAuthAccessToken_SetsKindClientAndFamily covers T20's
// CreateOAuthAccessToken: mints a kind="oauth" row tied to the given
// client, with a family_id generated when none is passed.
func TestCreateOAuthAccessToken_SetsKindClientAndFamily(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "oauth-token-create@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	token, row, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty plaintext access token")
	}
	if row.Kind != PATKindOAuth {
		t.Fatalf("expected kind %q, got %q", PATKindOAuth, row.Kind)
	}
	if row.OAuthClientID == nil || *row.OAuthClientID != client.ID {
		t.Fatalf("expected oauth_client_id %q, got %v", client.ID, row.OAuthClientID)
	}

	var familyID *string
	if err := pool.QueryRow(ctx, `SELECT family_id FROM zeep_system.dashboard_pats WHERE id = $1`, row.ID).Scan(&familyID); err != nil {
		t.Fatalf("read family_id: %v", err)
	}
	if familyID == nil || *familyID == "" {
		t.Fatal("expected a generated, non-empty family_id")
	}

	// Resolves through the exact same ResolvePAT path a manual PAT does
	// (spec MCP-23).
	resolved, err := ResolvePAT(ctx, pool, token)
	if err != nil {
		t.Fatalf("ResolvePAT: %v", err)
	}
	if resolved.ID != userID {
		t.Fatalf("expected resolved user id %s, got %s", userID, resolved.ID)
	}
}

// TestRotateOAuthRefreshToken_ValidTokenRotatesOnce covers T20's Done-when:
// a valid refresh token exchange issues a new access+refresh pair and
// invalidates the old refresh token.
func TestRotateOAuthRefreshToken_ValidTokenRotatesOnce(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "oauth-rotate-once@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	oldAccessToken, row, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}
	oldRefreshToken, err := SetRefreshToken(ctx, pool, row.ID)
	if err != nil {
		t.Fatalf("SetRefreshToken: %v", err)
	}

	newAccessToken, newRefreshToken, newRow, err := RotateOAuthRefreshToken(ctx, pool, oldRefreshToken)
	if err != nil {
		t.Fatalf("RotateOAuthRefreshToken: %v", err)
	}
	if newAccessToken == "" || newRefreshToken == "" {
		t.Fatal("expected non-empty new access and refresh tokens")
	}
	if newRow.ID == row.ID {
		t.Fatal("expected rotation to mint a brand-new PAT row, not reuse the old one")
	}

	// Old access token is superseded (revoked) by the rotation.
	if _, err := ResolvePAT(ctx, pool, oldAccessToken); !errors.Is(err, ErrPATRevoked) {
		t.Fatalf("expected the old access token to be revoked after rotation, got %v", err)
	}
	// New access token resolves.
	if _, err := ResolvePAT(ctx, pool, newAccessToken); err != nil {
		t.Fatalf("expected the new access token to resolve, got %v", err)
	}
	// Old refresh token can no longer rotate again on its own (it's now
	// the reuse-of-superseded-token case).
	if _, _, _, err := RotateOAuthRefreshToken(ctx, pool, oldRefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("expected ErrRefreshTokenReused for the old refresh token, got %v", err)
	}
}

// TestRotateOAuthRefreshToken_UnknownTokenReturnsNotFound covers the
// not-found path.
func TestRotateOAuthRefreshToken_UnknownTokenReturnsNotFound(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	if _, _, _, err := RotateOAuthRefreshToken(ctx, pool, "not-a-real-refresh-token"); !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Fatalf("expected ErrRefreshTokenNotFound, got %v", err)
	}
}

// TestRotateOAuthRefreshToken_ReuseRevokesEntireFamily covers T20's
// Done-when: reusing an already-rotated refresh token is rejected AND
// revokes the access token issued alongside it (confirm a subsequent
// ResolvePAT/tool call with that access token now fails) — here asserted
// on the *current* (post-rotation) access token, proving the whole family
// is revoked, not just the reused row.
func TestRotateOAuthRefreshToken_ReuseRevokesEntireFamily(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "oauth-rotate-reuse@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	_, row, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}
	firstRefreshToken, err := SetRefreshToken(ctx, pool, row.ID)
	if err != nil {
		t.Fatalf("SetRefreshToken: %v", err)
	}

	// Legitimate rotation once — firstRefreshToken becomes superseded.
	secondAccessToken, secondRefreshToken, _, err := RotateOAuthRefreshToken(ctx, pool, firstRefreshToken)
	if err != nil {
		t.Fatalf("RotateOAuthRefreshToken (legitimate): %v", err)
	}
	_ = secondRefreshToken

	// Reuse of the superseded firstRefreshToken must be rejected AND revoke
	// the whole family, including the currently-active secondAccessToken.
	if _, _, _, err := RotateOAuthRefreshToken(ctx, pool, firstRefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("expected ErrRefreshTokenReused, got %v", err)
	}
	if _, err := ResolvePAT(ctx, pool, secondAccessToken); !errors.Is(err, ErrPATRevoked) {
		t.Fatalf("expected the current access token to be revoked by family-wide reuse revocation, got %v", err)
	}
}

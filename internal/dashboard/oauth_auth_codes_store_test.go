package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCreateAuthCode_ReturnsPlaintextOnceAndStoresOnlyHash covers T18's
// design.md Data Models: the plaintext code is returned exactly once and
// the stored row's hash never equals it.
func TestCreateAuthCode_ReturnsPlaintextOnceAndStoresOnlyHash(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "authcode-owner@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	code, row, err := CreateAuthCode(ctx, pool, client.ID, userID, "challenge-abc", "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	if code == "" {
		t.Fatal("expected a non-empty plaintext code")
	}
	if row.ID == "" {
		t.Fatal("expected a non-empty auth code id")
	}

	var storedHash string
	if err := pool.QueryRow(ctx, `SELECT code_hash FROM zeep_system.oauth_auth_codes WHERE id = $1`, row.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored code_hash: %v", err)
	}
	if storedHash == code {
		t.Fatal("stored code_hash must never equal the plaintext code")
	}
	if storedHash != hashOAuthCode(code) {
		t.Fatal("stored code_hash must be the SHA-256 hash of the plaintext code")
	}
	if row.CodeChallenge != "challenge-abc" || row.RedirectURI != "https://example.com/cb" {
		t.Fatalf("expected code_challenge/redirect_uri to round-trip exactly, got %+v", row)
	}
}

// TestConsumeAuthCode_ValidCodeSucceedsOnce covers design.md's "PKCE-bound
// authorization code, exchangeable exactly once" requirement (spec P1-OAuth
// AC4): a valid code resolves once, and a second exchange of the same code
// is rejected as already-used.
func TestConsumeAuthCode_ValidCodeSucceedsOnce(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "authcode-consume@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	code, created, err := CreateAuthCode(ctx, pool, client.ID, userID, "challenge-abc", "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}

	row, err := ConsumeAuthCode(ctx, pool, code)
	if err != nil {
		t.Fatalf("ConsumeAuthCode: %v", err)
	}
	if row.ID != created.ID || row.UserID != userID || row.ClientID != client.ID {
		t.Fatalf("expected the resolved row to match the created code, got %+v", row)
	}

	if _, err := ConsumeAuthCode(ctx, pool, code); !errors.Is(err, ErrAuthCodeUsed) {
		t.Fatalf("expected ErrAuthCodeUsed on reuse, got %v", err)
	}
}

// TestConsumeAuthCode_UnknownCodeReturnsNotFound covers the not-found path.
func TestConsumeAuthCode_UnknownCodeReturnsNotFound(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()

	if _, err := ConsumeAuthCode(ctx, pool, "not-a-real-code"); !errors.Is(err, ErrAuthCodeNotFound) {
		t.Fatalf("expected ErrAuthCodeNotFound, got %v", err)
	}
}

// TestConsumeAuthCode_ExpiredCodeRejected covers design.md Error Handling
// Strategy: "authorization code reused, expired, or its PKCE verifier
// doesn't match -> reject the token exchange without issuing any token."
func TestConsumeAuthCode_ExpiredCodeRejected(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "authcode-expired@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	code, created, err := CreateAuthCode(ctx, pool, client.ID, userID, "challenge-abc", "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.oauth_auth_codes SET expires_at = $1 WHERE id = $2`, time.Now().Add(-time.Minute), created.ID); err != nil {
		t.Fatalf("force-expire code: %v", err)
	}

	if _, err := ConsumeAuthCode(ctx, pool, code); !errors.Is(err, ErrAuthCodeExpired) {
		t.Fatalf("expected ErrAuthCodeExpired, got %v", err)
	}
}

// TestPurgeExpiredAuthCodes_DeletesOnlyExpiredRows covers the purge job
// (T18/design.md: purged more aggressively than webhook_deliveries).
func TestPurgeExpiredAuthCodes_DeletesOnlyExpiredRows(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "authcode-purge@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	_, liveRow, err := CreateAuthCode(ctx, pool, client.ID, userID, "live", "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode (live): %v", err)
	}
	_, expiredRow, err := CreateAuthCode(ctx, pool, client.ID, userID, "expired", "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode (expired): %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.oauth_auth_codes SET expires_at = $1 WHERE id = $2`, time.Now().Add(-time.Minute), expiredRow.ID); err != nil {
		t.Fatalf("force-expire code: %v", err)
	}

	deleted, err := PurgeExpiredAuthCodes(ctx, pool)
	if err != nil {
		t.Fatalf("PurgeExpiredAuthCodes: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected exactly 1 row purged, got %d", deleted)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM zeep_system.oauth_auth_codes WHERE id = $1`, liveRow.ID).Scan(&count); err != nil {
		t.Fatalf("count live row: %v", err)
	}
	if count != 1 {
		t.Fatal("expected the live (unexpired) row to survive the purge")
	}
}

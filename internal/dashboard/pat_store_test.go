package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func patTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.dashboard_pats, zeep_system.dashboard_users CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	return pool
}

// TestCreatePAT_ReturnsPlaintextOnceAndStoresOnlyHash covers spec MCP-01:
// the token is shown once and the stored row never contains the plaintext.
func TestCreatePAT_ReturnsPlaintextOnceAndStoresOnlyHash(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-owner@example.com", "admin")

	token, row, err := CreatePAT(ctx, pool, userID, "laptop", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty plaintext token")
	}
	if row.ID == "" {
		t.Fatal("expected a non-empty PAT id")
	}

	var storedHash string
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM zeep_system.dashboard_pats WHERE id = $1`, row.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored token_hash: %v", err)
	}
	if storedHash == token {
		t.Fatal("stored token_hash must never equal the plaintext token")
	}
	if storedHash != hashPATToken(token) {
		t.Fatal("stored token_hash must be the SHA-256 hash of the plaintext token")
	}
}

// TestCreatePAT_ManualDefaultsToNoExpiry / TestCreatePAT_EphemeralAndOAuthRequireExpiry
// cover the Done-when: manual PATs get expires_at=nil; ephemeral/oauth require it.
func TestCreatePAT_ManualDefaultsToNoExpiry(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-manual@example.com", "admin")

	_, row, err := CreatePAT(ctx, pool, userID, "manual token", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	if row.ExpiresAt != nil {
		t.Fatalf("expected nil expires_at for kind=manual, got %v", row.ExpiresAt)
	}
}

func TestCreatePAT_EphemeralAndOAuthRequireExpiry(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-ephemeral@example.com", "admin")

	if _, _, err := CreatePAT(ctx, pool, userID, "ephemeral", PATKindEphemeral, nil); !errors.Is(err, ErrPATExpiryRequired) {
		t.Fatalf("expected ErrPATExpiryRequired for kind=ephemeral with nil expiresAt, got %v", err)
	}
	if _, _, err := CreatePAT(ctx, pool, userID, "oauth", PATKindOAuth, nil); !errors.Is(err, ErrPATExpiryRequired) {
		t.Fatalf("expected ErrPATExpiryRequired for kind=oauth with nil expiresAt, got %v", err)
	}

	exp := time.Now().Add(time.Hour)
	_, row, err := CreatePAT(ctx, pool, userID, "ephemeral", PATKindEphemeral, &exp)
	if err != nil {
		t.Fatalf("CreatePAT with expiresAt: %v", err)
	}
	if row.ExpiresAt == nil {
		t.Fatal("expected non-nil expires_at for kind=ephemeral with an explicit expiresAt")
	}
}

// TestResolvePAT_ValidTokenReturnsOwningUser covers spec MCP-02.
func TestResolvePAT_ValidTokenReturnsOwningUser(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-resolve@example.com", "admin")

	token, _, err := CreatePAT(ctx, pool, userID, "cli", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	user, err := ResolvePAT(ctx, pool, token)
	if err != nil {
		t.Fatalf("ResolvePAT: %v", err)
	}
	if user.ID != userID {
		t.Fatalf("expected resolved user id %s, got %s", userID, user.ID)
	}
	if user.Email != "pat-resolve@example.com" {
		t.Fatalf("expected resolved user email pat-resolve@example.com, got %s", user.Email)
	}
}

// TestResolvePAT_UnknownTokenReturnsNotFound covers spec MCP-03.
func TestResolvePAT_UnknownTokenReturnsNotFound(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()

	if _, err := ResolvePAT(ctx, pool, "not-a-real-token"); !errors.Is(err, ErrPATNotFound) {
		t.Fatalf("expected ErrPATNotFound, got %v", err)
	}
}

// TestResolvePAT_RevokedTokenRejected covers spec MCP-03/MCP-04.
func TestResolvePAT_RevokedTokenRejected(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-revoked@example.com", "admin")

	token, row, err := CreatePAT(ctx, pool, userID, "cli", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	if err := RevokePAT(ctx, pool, userID, row.ID); err != nil {
		t.Fatalf("RevokePAT: %v", err)
	}

	if _, err := ResolvePAT(ctx, pool, token); !errors.Is(err, ErrPATRevoked) {
		t.Fatalf("expected ErrPATRevoked, got %v", err)
	}
}

// TestResolvePAT_ExpiredEphemeralTokenRejected covers spec MCP-03, edge case
// "ephemeral PAT past expires_at rejected same as revoked" (design.md Risks).
func TestResolvePAT_ExpiredEphemeralTokenRejected(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-expired@example.com", "admin")

	past := time.Now().Add(-time.Hour)
	token, _, err := CreatePAT(ctx, pool, userID, "chat-drawer", PATKindEphemeral, &past)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	if _, err := ResolvePAT(ctx, pool, token); !errors.Is(err, ErrPATExpired) {
		t.Fatalf("expected ErrPATExpired, got %v", err)
	}
}

// TestResolvePAT_OwningUserDeletedRejected covers the spec Edge Case: "PAT's
// issuing admin is deactivated/deleted after mint → reject on next request,
// derived live, not cached at mint time."
func TestResolvePAT_OwningUserDeletedRejected(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-deleted-owner@example.com", "admin")

	token, _, err := CreatePAT(ctx, pool, userID, "cli", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	if err := DeleteUser(ctx, pool, userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := ResolvePAT(ctx, pool, token); !errors.Is(err, ErrPATNotFound) {
		t.Fatalf("expected ErrPATNotFound after owning user deletion, got %v", err)
	}
}

// TestRevokePAT_ScopedToOwningUser covers the Done-when ownership-scoping
// test (mirrors the webhook-mapping IDOR fix): another user's PAT id must
// not be revocable.
func TestRevokePAT_ScopedToOwningUser(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	ownerID := testUser(t, pool, "pat-real-owner@example.com", "admin")
	attackerID := testUser(t, pool, "pat-attacker@example.com", "admin")

	_, row, err := CreatePAT(ctx, pool, ownerID, "cli", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	if err := RevokePAT(ctx, pool, attackerID, row.ID); !errors.Is(err, ErrPATNotFound) {
		t.Fatalf("expected ErrPATNotFound when revoking another user's PAT, got %v", err)
	}

	// Confirm it's still live (not actually revoked by the failed attempt).
	list, err := ListPATs(ctx, pool, ownerID)
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(list) != 1 || list[0].RevokedAt != nil {
		t.Fatalf("expected the PAT to remain unrevoked after a forbidden revoke attempt, got %+v", list)
	}
}

// TestDeletingOwningUser_CascadesToDeletePATs exercises the FK ON DELETE
// CASCADE directly (design.md Data Models: dashboard_pats.user_id →
// dashboard_users.id ON DELETE CASCADE).
func TestDeletingOwningUser_CascadesToDeletePATs(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-cascade@example.com", "admin")

	_, row, err := CreatePAT(ctx, pool, userID, "cli", PATKindManual, nil)
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM zeep_system.dashboard_users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete owning user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM zeep_system.dashboard_pats WHERE id = $1`, row.ID).Scan(&count); err != nil {
		t.Fatalf("count remaining PAT rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the PAT row to be cascade-deleted with its owning user, found %d rows", count)
	}
}

// TestListPATs_NeverIncludesTokenHash covers the Done-when: ListPATs output
// never includes token_hash in its JSON shape. PATRow has no such field, so
// this asserts on the actual returned struct's field set via its JSON tags.
func TestListPATs_NeverIncludesTokenHash(t *testing.T) {
	pool := patTestPool(t)
	ctx := context.Background()
	userID := testUser(t, pool, "pat-list@example.com", "admin")

	if _, _, err := CreatePAT(ctx, pool, userID, "cli", PATKindManual, nil); err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	list, err := ListPATs(ctx, pool, userID)
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 PAT, got %d", len(list))
	}

	data, err := json.Marshal(list[0])
	if err != nil {
		t.Fatalf("marshal PATRow: %v", err)
	}
	if bytes.Contains(data, []byte("token_hash")) || bytes.Contains(data, []byte("TokenHash")) {
		t.Fatalf("expected no token_hash field in the JSON shape, got %s", data)
	}
}

package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// pkceChallengeFor computes the S256 code_challenge for a given verifier —
// the same formula pkceVerify checks against.
func pkceChallengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// oauthTestTTL is a fixed 1-hour expiry for fixtures below — the exact
// duration doesn't matter to these tests, only that expires_at is non-nil
// and in the future.
func oauthTestTTL() time.Time {
	return time.Now().Add(time.Hour)
}

// TestTokenHandler_AuthorizationCode_ValidExchangeResolvesLikeManualPAT
// covers T20's Done-when: valid code + matching PKCE verifier exchanges
// for an access token that ResolvePAT resolves to the consenting admin
// (same identity path a manual PAT resolves through).
func TestTokenHandler_AuthorizationCode_ValidExchangeResolvesLikeManualPAT(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	h := NewOAuthHandler(pool)
	userID := testUser(t, pool, "token-valid-exchange@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	verifier := "test-verifier-1234567890123456789012345"
	code, _, err := CreateAuthCode(ctx, pool, client.ID, userID, pkceChallengeFor(verifier), "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {client.ID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {"https://example.com/cb"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.Token(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected non-empty access_token and refresh_token")
	}

	resolved, err := ResolvePAT(ctx, pool, resp.AccessToken)
	if err != nil {
		t.Fatalf("ResolvePAT on the issued access token: %v", err)
	}
	if resolved.ID != userID {
		t.Fatalf("expected the access token to resolve to the consenting admin %s, got %s", userID, resolved.ID)
	}
}

// TestTokenHandler_AuthorizationCode_ReusedExpiredOrMismatchedPKCERejected
// covers T20's Done-when: reused code, expired code, or mismatched PKCE
// verifier all return 400 invalid_grant, no token issued.
func TestTokenHandler_AuthorizationCode_ReusedExpiredOrMismatchedPKCERejected(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	h := NewOAuthHandler(pool)
	userID := testUser(t, pool, "token-invalid-grant@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	exchange := func(code, verifier string) int {
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"code_verifier": {verifier},
		}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.Token(rr, req)
		return rr.Code
	}

	// Mismatched PKCE verifier.
	verifier := "correct-verifier-123456789012345678901"
	code, _, err := CreateAuthCode(ctx, pool, client.ID, userID, pkceChallengeFor(verifier), "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	if code := exchange(code, "wrong-verifier-1234567890123456789012"); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched PKCE verifier, got %d", code)
	}

	// Reused code: even a *correct* verifier now fails, since the
	// mismatched attempt above already consumed the code (single-use,
	// per tokenAuthorizationCode's doc comment).
	if code := exchange(code, verifier); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a reused code, got %d", code)
	}

	// Expired code.
	expiredCode, expiredRow, err := CreateAuthCode(ctx, pool, client.ID, userID, pkceChallengeFor(verifier), "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode (expired fixture): %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE zeep_system.oauth_auth_codes SET expires_at = now() - interval '1 minute' WHERE id = $1`, expiredRow.ID); err != nil {
		t.Fatalf("force-expire code: %v", err)
	}
	if code := exchange(expiredCode, verifier); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an expired code, got %d", code)
	}
}

// TestTokenHandler_AuthorizationCode_MissingOrMismatchedClientIDRejected
// covers RFC 6749 §4.1.3: a public client must assert client_id at token
// exchange, and it must match the client the code was actually issued to —
// otherwise one registered client could redeem a code obtained via another
// client's authorization request.
func TestTokenHandler_AuthorizationCode_MissingOrMismatchedClientIDRejected(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	h := NewOAuthHandler(pool)
	userID := testUser(t, pool, "token-client-id-check@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	otherClient, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "other", RedirectURIs: []string{"https://other.example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient (other): %v", err)
	}

	exchange := func(code, verifier, clientID string) int {
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"code_verifier": {verifier},
		}
		if clientID != "" {
			form.Set("client_id", clientID)
		}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.Token(rr, req)
		return rr.Code
	}

	verifier := "test-verifier-1234567890123456789012345"

	code, _, err := CreateAuthCode(ctx, pool, client.ID, userID, pkceChallengeFor(verifier), "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	if got := exchange(code, verifier, ""); got != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing client_id, got %d", got)
	}

	code2, _, err := CreateAuthCode(ctx, pool, client.ID, userID, pkceChallengeFor(verifier), "https://example.com/cb")
	if err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	if got := exchange(code2, verifier, otherClient.ID); got != http.StatusBadRequest {
		t.Fatalf("expected 400 for a client_id that doesn't match the code's issuing client, got %d", got)
	}
}

// TestTokenHandler_RefreshToken_ValidExchangeRotates covers T20's
// Done-when: valid refresh token exchange issues a new access+refresh
// pair and invalidates the old refresh token, at the HTTP layer.
func TestTokenHandler_RefreshToken_ValidExchangeRotates(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	h := NewOAuthHandler(pool)
	userID := testUser(t, pool, "token-refresh-valid@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	_, row, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", oauthTestTTL())
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}
	refreshToken, err := SetRefreshToken(ctx, pool, row.ID)
	if err != nil {
		t.Fatalf("SetRefreshToken: %v", err)
	}

	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {refreshToken}}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.Token(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" || resp.RefreshToken == refreshToken {
		t.Fatalf("expected a fresh, non-empty access+refresh pair, got %+v", resp)
	}
}

// TestTokenHandler_RefreshToken_ReuseRejectedAndBlocksFurtherCalls covers
// T20's Done-when: reusing an already-rotated refresh token is rejected AND
// revokes the access token issued alongside it (confirm a subsequent
// orbit_list_apps-equivalent call — here, ResolvePAT — with that access
// token now fails), at the HTTP layer.
func TestTokenHandler_RefreshToken_ReuseRejectedAndBlocksFurtherCalls(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	h := NewOAuthHandler(pool)
	userID := testUser(t, pool, "token-refresh-reuse@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	_, row, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", oauthTestTTL())
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}
	firstRefreshToken, err := SetRefreshToken(ctx, pool, row.ID)
	if err != nil {
		t.Fatalf("SetRefreshToken: %v", err)
	}

	doRefresh := func(token string) (int, tokenResponse) {
		form := url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {token}}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.Token(rr, req)
		var resp tokenResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		return rr.Code, resp
	}

	code, resp := doRefresh(firstRefreshToken)
	if code != http.StatusOK {
		t.Fatalf("expected the first (legitimate) refresh to succeed, got %d", code)
	}
	currentAccessToken := resp.AccessToken

	reuseCode, _ := doRefresh(firstRefreshToken)
	if reuseCode != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid_grant on reuse of the superseded refresh token, got %d", reuseCode)
	}

	if _, err := ResolvePAT(ctx, pool, currentAccessToken); !errors.Is(err, ErrPATRevoked) {
		t.Fatalf("expected the current access token to be revoked by family-wide reuse revocation, got %v", err)
	}
}

// TestTokenHandler_RefreshToken_MissingOrMismatchedClientIDRejected covers
// RFC 6749 §6 / OAuth 2.1 §4.3: a public client must identify itself with
// client_id at refresh too, not just at the initial authorization_code
// exchange — and a mismatch must not rotate or revoke the presented token
// (the legitimate client must still be able to use it afterward).
func TestTokenHandler_RefreshToken_MissingOrMismatchedClientIDRejected(t *testing.T) {
	pool := oauthClientTestPool(t)
	ctx := context.Background()
	h := NewOAuthHandler(pool)
	userID := testUser(t, pool, "token-refresh-client-id-check@example.com", "admin")
	client, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "cli", RedirectURIs: []string{"https://example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	otherClient, err := RegisterClient(ctx, pool, RegisterClientInput{Name: "other", RedirectURIs: []string{"https://other.example.com/cb"}})
	if err != nil {
		t.Fatalf("RegisterClient (other): %v", err)
	}
	_, row, err := CreateOAuthAccessToken(ctx, pool, userID, client.ID, "", oauthTestTTL())
	if err != nil {
		t.Fatalf("CreateOAuthAccessToken: %v", err)
	}
	refreshToken, err := SetRefreshToken(ctx, pool, row.ID)
	if err != nil {
		t.Fatalf("SetRefreshToken: %v", err)
	}

	doRefresh := func(clientID string) int {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
		if clientID != "" {
			form.Set("client_id", clientID)
		}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.Token(rr, req)
		return rr.Code
	}

	if got := doRefresh(""); got != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing client_id, got %d", got)
	}
	if got := doRefresh(otherClient.ID); got != http.StatusBadRequest {
		t.Fatalf("expected 400 for a client_id that doesn't own the refresh token, got %d", got)
	}

	// The token must still be fully usable by its real owner after both
	// rejected attempts above — neither should have rotated or revoked it.
	if got := doRefresh(client.ID); got != http.StatusOK {
		t.Fatalf("expected the legitimate client_id to still succeed, got %d", got)
	}
}

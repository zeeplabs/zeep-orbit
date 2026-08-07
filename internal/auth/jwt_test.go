package auth

import (
	"testing"
	"time"
)

func TestIssueJWT_RoleClaim_CustomRole(t *testing.T) {
	secret := []byte("test-secret")

	token, err := IssueJWT(secret, "user-1", "approver@test.com", "myapp", "approver")
	if err != nil {
		t.Fatalf("IssueJWT failed: %v", err)
	}

	claims, err := ParseJWT(secret, token)
	if err != nil {
		t.Fatalf("ParseJWT failed: %v", err)
	}
	if claims.Role != "approver" {
		t.Fatalf("expected role claim %q, got %q", "approver", claims.Role)
	}
	if claims.Email != "approver@test.com" {
		t.Fatalf("expected email claim %q, got %q", "approver@test.com", claims.Email)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("expected sub claim %q, got %q", "user-1", claims.Subject)
	}
}

func TestIssueJWT_RoleClaim_DefaultMemberRole(t *testing.T) {
	secret := []byte("test-secret")

	token, err := IssueJWT(secret, "user-2", "member@test.com", "myapp", "member")
	if err != nil {
		t.Fatalf("IssueJWT failed: %v", err)
	}

	claims, err := ParseJWT(secret, token)
	if err != nil {
		t.Fatalf("ParseJWT failed: %v", err)
	}
	if claims.Role != "member" {
		t.Fatalf("expected default role claim %q, got %q", "member", claims.Role)
	}
}

func TestIssueAppTokenJWT_RoundTrip(t *testing.T) {
	secret := []byte("test-secret")
	jti := "test-jti-123"
	appName := "myapp"

	token, err := IssueAppTokenJWT(secret, jti, appName, nil)
	if err != nil {
		t.Fatalf("IssueAppTokenJWT failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := ParseAppTokenJWT(secret, token)
	if err != nil {
		t.Fatalf("ParseAppTokenJWT failed: %v", err)
	}
	if claims.TokenType != "app_token" {
		t.Fatalf("expected token_type 'app_token', got %q", claims.TokenType)
	}
	if claims.ID != jti {
		t.Fatalf("expected jti %q, got %q", jti, claims.ID)
	}
	if claims.Subject != appName {
		t.Fatalf("expected sub %q, got %q", appName, claims.Subject)
	}
}

func TestIssueAppTokenJWT_WithExpiration(t *testing.T) {
	secret := []byte("test-secret")
	expiresAt := time.Now().Add(1 * time.Hour)

	token, err := IssueAppTokenJWT(secret, "jti-exp", "myapp", &expiresAt)
	if err != nil {
		t.Fatalf("IssueAppTokenJWT failed: %v", err)
	}

	claims, err := ParseAppTokenJWT(secret, token)
	if err != nil {
		t.Fatalf("ParseAppTokenJWT failed: %v", err)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("expected expiration claim")
	}
}

func TestIssueAppTokenJWT_WrongSecret(t *testing.T) {
	secret := []byte("correct-secret")
	wrongSecret := []byte("wrong-secret")

	token, err := IssueAppTokenJWT(secret, "jti-wrong", "myapp", nil)
	if err != nil {
		t.Fatalf("IssueAppTokenJWT failed: %v", err)
	}

	_, err = ParseAppTokenJWT(wrongSecret, token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestIssueAppTokenJWT_NoExpiration(t *testing.T) {
	secret := []byte("test-secret")

	token, err := IssueAppTokenJWT(secret, "jti-never", "myapp", nil)
	if err != nil {
		t.Fatalf("IssueAppTokenJWT failed: %v", err)
	}

	claims, err := ParseAppTokenJWT(secret, token)
	if err != nil {
		t.Fatalf("ParseAppTokenJWT failed: %v", err)
	}
	if claims.ExpiresAt != nil {
		t.Fatal("expected no expiration for nil expiresAt")
	}
}

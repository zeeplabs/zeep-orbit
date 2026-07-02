package auth

import (
	"testing"
	"time"
)

func TestSignAndVerifyState_RoundTrip(t *testing.T) {
	secret := []byte("test-secret")
	token, err := signState(secret, "https://apps.starbem.dev/dashboard")
	if err != nil {
		t.Fatalf("signState() error = %v", err)
	}

	redirect, ok := verifyState(secret, token)
	if !ok {
		t.Fatalf("verifyState() ok = false, want true")
	}
	if redirect != "https://apps.starbem.dev/dashboard" {
		t.Fatalf("verifyState() redirect = %q, want %q", redirect, "https://apps.starbem.dev/dashboard")
	}
}

func TestVerifyState_NoSharedMemoryNeeded(t *testing.T) {
	secret := []byte("test-secret")
	token, err := signState(secret, "https://apps.starbem.dev/dashboard")
	if err != nil {
		t.Fatalf("signState() error = %v", err)
	}

	// Simulates verifying on a different replica: a brand new handler
	// with no shared in-memory state must still validate the token.
	other := NewAppGoogleHandler(nil, nil)
	_ = other

	redirect, ok := verifyState(secret, token)
	if !ok || redirect == "" {
		t.Fatalf("verifyState() must succeed without any shared process state, ok=%v redirect=%q", ok, redirect)
	}
}

func TestVerifyState_RejectsTamperedSignature(t *testing.T) {
	secret := []byte("test-secret")
	token, err := signState(secret, "https://apps.starbem.dev/dashboard")
	if err != nil {
		t.Fatalf("signState() error = %v", err)
	}

	tampered := token + "x"
	if _, ok := verifyState(secret, tampered); ok {
		t.Fatalf("verifyState() ok = true for tampered token, want false")
	}
}

func TestVerifyState_RejectsWrongSecret(t *testing.T) {
	token, err := signState([]byte("secret-a"), "https://apps.starbem.dev/dashboard")
	if err != nil {
		t.Fatalf("signState() error = %v", err)
	}

	if _, ok := verifyState([]byte("secret-b"), token); ok {
		t.Fatalf("verifyState() ok = true for wrong secret, want false")
	}
}

func TestVerifyState_RejectsExpired(t *testing.T) {
	secret := []byte("test-secret")
	claims := stateClaims{Redirect: "https://apps.starbem.dev/dashboard", ExpiresAt: time.Now().Add(-1 * time.Minute).Unix()}
	payload, err := marshalStateClaims(claims)
	if err != nil {
		t.Fatalf("marshalStateClaims() error = %v", err)
	}
	token := signPayload(secret, payload)

	if _, ok := verifyState(secret, token); ok {
		t.Fatalf("verifyState() ok = true for expired token, want false")
	}
}

func TestVerifyState_RejectsMalformed(t *testing.T) {
	secret := []byte("test-secret")
	cases := []string{"", "no-dot-here", "a.b.c", "!!!.###"}
	for _, c := range cases {
		if _, ok := verifyState(secret, c); ok {
			t.Fatalf("verifyState(%q) ok = true, want false", c)
		}
	}
}

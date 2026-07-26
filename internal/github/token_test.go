package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// generateTestPrivateKeyPEM creates a fresh RSA key pair, PKCS1-PEM encoded,
// for use in tests. No real GitHub App credentials are involved.
func generateTestPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test rsa key: %v", err)
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return pem.EncodeToMemory(block)
}

func TestGenerateAppJWT_Valid(t *testing.T) {
	keyPEM := generateTestPrivateKeyPEM(t)

	tokenStr, err := generateAppJWT("12345", keyPEM)
	if err != nil {
		t.Fatalf("generateAppJWT returned error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}

	// Parse the raw private key back out to get the public key for verification.
	rawKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyPEM)
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}

	claims := jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(tokenStr, &claims, func(tok *jwt.Token) (interface{}, error) {
		if tok.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", tok.Header["alg"])
		}
		return &rawKey.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("failed to parse/verify generated jwt: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("expected parsed token to be valid")
	}

	if claims.Issuer != "12345" {
		t.Errorf("iss = %q, want %q", claims.Issuer, "12345")
	}

	now := time.Now()
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		t.Fatal("expected iat and exp claims to be set")
	}

	iatDelta := now.Sub(claims.IssuedAt.Time)
	if iatDelta < jwtClockSkew-5*time.Second || iatDelta > jwtClockSkew+5*time.Second {
		t.Errorf("iat drift from now = %v, want ~%v", iatDelta, jwtClockSkew)
	}

	expDelta := claims.ExpiresAt.Time.Sub(now)
	if expDelta < jwtMaxLifetime-5*time.Second || expDelta > jwtMaxLifetime+5*time.Second {
		t.Errorf("exp from now = %v, want ~%v", expDelta, jwtMaxLifetime)
	}
}

func TestGenerateAppJWT_InvalidPrivateKey(t *testing.T) {
	_, err := generateAppJWT("12345", []byte("not a valid PEM key"))
	if err == nil {
		t.Fatal("expected error for malformed private key, got nil")
	}
}

// roundTripFunc adapts a function to the http.RoundTripper interface, used to
// mock HTTP calls without a real network dependency.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockClient(handler func(req *http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: roundTripFunc(handler)}
}

func TestInstallationTokenCache_CacheHitNoHTTPCall(t *testing.T) {
	keyPEM := generateTestPrivateKeyPEM(t)

	var callCount int32
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&callCount, 1)
		return mockJSONResponse(http.StatusCreated, fmt.Sprintf(
			`{"token":"first-token","expires_at":%q}`,
			time.Now().Add(1*time.Hour).Format(time.RFC3339),
		)), nil
	})

	cache := &InstallationTokenCache{
		AppID:         "app-1",
		PrivateKeyPEM: keyPEM,
		HTTPClient:    client,
	}

	tok1, err := cache.Token("install-1")
	if err != nil {
		t.Fatalf("first Token() call failed: %v", err)
	}
	if tok1 != "first-token" {
		t.Errorf("tok1 = %q, want %q", tok1, "first-token")
	}
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected 1 HTTP call after first fetch, got %d", got)
	}

	tok2, err := cache.Token("install-1")
	if err != nil {
		t.Fatalf("second Token() call failed: %v", err)
	}
	if tok2 != "first-token" {
		t.Errorf("tok2 = %q, want cached %q", tok2, "first-token")
	}
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected cache hit to avoid new HTTP call, callCount = %d, want 1", got)
	}
}

func TestInstallationTokenCache_ExpiredForcesRenewal(t *testing.T) {
	keyPEM := generateTestPrivateKeyPEM(t)

	var callCount int32
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&callCount, 1)
		// First response is already within the refresh buffer (expires in 1
		// minute, well under the 5-minute refreshBuffer), so the next Token()
		// call must trigger a renewal rather than reuse it.
		if n == 1 {
			return mockJSONResponse(http.StatusCreated, fmt.Sprintf(
				`{"token":"stale-token","expires_at":%q}`,
				time.Now().Add(1*time.Minute).Format(time.RFC3339),
			)), nil
		}
		return mockJSONResponse(http.StatusCreated, fmt.Sprintf(
			`{"token":"renewed-token","expires_at":%q}`,
			time.Now().Add(1*time.Hour).Format(time.RFC3339),
		)), nil
	})

	cache := &InstallationTokenCache{
		AppID:         "app-1",
		PrivateKeyPEM: keyPEM,
		HTTPClient:    client,
	}

	tok1, err := cache.Token("install-1")
	if err != nil {
		t.Fatalf("first Token() call failed: %v", err)
	}
	if tok1 != "stale-token" {
		t.Errorf("tok1 = %q, want %q", tok1, "stale-token")
	}

	tok2, err := cache.Token("install-1")
	if err != nil {
		t.Fatalf("second Token() call failed: %v", err)
	}
	if tok2 != "renewed-token" {
		t.Errorf("tok2 = %q, want renewed token %q, cache did not renew near-expired entry", tok2, "renewed-token")
	}
	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Fatalf("expected 2 HTTP calls (initial + forced renewal), got %d", got)
	}
}

func TestInstallationTokenCache_ExchangeErrorPropagates(t *testing.T) {
	keyPEM := generateTestPrivateKeyPEM(t)

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		return mockJSONResponse(http.StatusUnauthorized, `{"message":"Bad credentials"}`), nil
	})

	cache := &InstallationTokenCache{
		AppID:         "app-1",
		PrivateKeyPEM: keyPEM,
		HTTPClient:    client,
	}

	_, err := cache.Token("install-1")
	if err == nil {
		t.Fatal("expected error for non-201 response, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
}

func mockJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

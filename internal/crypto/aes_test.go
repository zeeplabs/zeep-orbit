package crypto

import (
	"os"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret")
	defer os.Unsetenv("DASHBOARD_BOOTSTRAP_SECRET")

	encoded, err := Encrypt("hello world")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	plain, err := Decrypt(encoded)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plain != "hello world" {
		t.Fatalf("expected round-trip to return the original plaintext, got %q", plain)
	}
}

func TestWebhookToken_RoundTrip(t *testing.T) {
	os.Setenv("DASHBOARD_BOOTSTRAP_SECRET", "test-secret")
	defer os.Unsetenv("DASHBOARD_BOOTSTRAP_SECRET")

	encoded, err := EncryptWebhookToken("wh-token-123")
	if err != nil {
		t.Fatalf("EncryptWebhookToken: %v", err)
	}
	plain, err := DecryptWebhookToken(encoded)
	if err != nil {
		t.Fatalf("DecryptWebhookToken: %v", err)
	}
	if plain != "wh-token-123" {
		t.Fatalf("expected round-trip to return the original token, got %q", plain)
	}
}

func TestWebhookToken_DedicatedKeyIndependentFromGoogleOAuthKey(t *testing.T) {
	os.Setenv("GOOGLE_OAUTH_ENCRYPTION_KEY", "google-oauth-key-32-bytes-long!!")
	os.Setenv("WEBHOOK_TOKEN_ENCRYPTION_KEY", "webhook-token-key-different-value")
	defer os.Unsetenv("GOOGLE_OAUTH_ENCRYPTION_KEY")
	defer os.Unsetenv("WEBHOOK_TOKEN_ENCRYPTION_KEY")

	encoded, err := EncryptWebhookToken("wh-token-456")
	if err != nil {
		t.Fatalf("EncryptWebhookToken: %v", err)
	}

	// A ciphertext produced under the dedicated webhook key must not decrypt
	// under the generic (Google-OAuth-keyed) Decrypt — proves the two key
	// spaces are actually independent, not just two names for the same key.
	if _, err := Decrypt(encoded); err == nil {
		t.Fatal("expected Decrypt (Google OAuth key) to fail against a token encrypted with the dedicated webhook key")
	}

	plain, err := DecryptWebhookToken(encoded)
	if err != nil {
		t.Fatalf("DecryptWebhookToken: %v", err)
	}
	if plain != "wh-token-456" {
		t.Fatalf("expected round-trip to return the original token, got %q", plain)
	}
}

func TestWebhookToken_ErrorsLoudlyWithNoKeyConfigured(t *testing.T) {
	os.Unsetenv("WEBHOOK_TOKEN_ENCRYPTION_KEY")
	os.Unsetenv("DASHBOARD_BOOTSTRAP_SECRET")

	if _, err := EncryptWebhookToken("wh-token-789"); err == nil {
		t.Fatal("expected EncryptWebhookToken to fail loudly when neither WEBHOOK_TOKEN_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set, not silently encrypt under a zero-byte key")
	}
}

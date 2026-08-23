package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// Falls back to DASHBOARD_BOOTSTRAP_SECRET if GOOGLE_OAUTH_ENCRYPTION_KEY is not set.
func encryptionKey() []byte {
	key := os.Getenv("GOOGLE_OAUTH_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("DASHBOARD_BOOTSTRAP_SECRET")
	}
	return normalizeKey(key)
}

// webhookTokenEncryptionKey resolves the key used for webhook token
// encryption — deliberately its own env var, not GOOGLE_OAUTH_ENCRYPTION_KEY,
// so rotating either secret doesn't also invalidate the other's ciphertexts.
// Falls back to DASHBOARD_BOOTSTRAP_SECRET (a required var, see README), so
// this only errors if that fallback was itself left unset.
func webhookTokenEncryptionKey() ([]byte, error) {
	key := os.Getenv("WEBHOOK_TOKEN_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("DASHBOARD_BOOTSTRAP_SECRET")
	}
	if key == "" {
		return nil, errors.New("crypto: neither WEBHOOK_TOKEN_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set")
	}
	return normalizeKey(key), nil
}

// aiProviderEncryptionKey resolves the key used for AI provider (e.g.
// OpenAI) API key encryption — deliberately its own env var, not
// GOOGLE_OAUTH_ENCRYPTION_KEY or WEBHOOK_TOKEN_ENCRYPTION_KEY, so rotating
// any one of these secrets doesn't also invalidate the others' ciphertexts.
// Falls back to DASHBOARD_BOOTSTRAP_SECRET (a required var, see README), so
// this only errors if that fallback was itself left unset.
func aiProviderEncryptionKey() ([]byte, error) {
	key := os.Getenv("AI_PROVIDER_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("DASHBOARD_BOOTSTRAP_SECRET")
	}
	if key == "" {
		return nil, errors.New("crypto: neither AI_PROVIDER_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set")
	}
	return normalizeKey(key), nil
}

func normalizeKey(key string) []byte {
	if len(key) >= 32 {
		return []byte(key[:32])
	}
	padded := make([]byte, 32)
	copy(padded, key)
	return padded
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a base64-encoded ciphertext.
func Encrypt(plaintext string) (string, error) {
	return encryptWithKey(plaintext, encryptionKey())
}

// Decrypt decrypts a base64-encoded ciphertext produced by Encrypt.
func Decrypt(encoded string) (string, error) {
	return decryptWithKey(encoded, encryptionKey())
}

// EncryptWebhookToken encrypts a webhook's plaintext token under the
// dedicated webhook-token key (see webhookTokenEncryptionKey).
func EncryptWebhookToken(plaintext string) (string, error) {
	key, err := webhookTokenEncryptionKey()
	if err != nil {
		return "", err
	}
	return encryptWithKey(plaintext, key)
}

// DecryptWebhookToken decrypts a ciphertext produced by EncryptWebhookToken.
func DecryptWebhookToken(encoded string) (string, error) {
	key, err := webhookTokenEncryptionKey()
	if err != nil {
		return "", err
	}
	return decryptWithKey(encoded, key)
}

// EncryptAIProviderKey encrypts an AI provider's plaintext API key under the
// dedicated AI-provider key (see aiProviderEncryptionKey).
func EncryptAIProviderKey(plaintext string) (string, error) {
	key, err := aiProviderEncryptionKey()
	if err != nil {
		return "", err
	}
	return encryptWithKey(plaintext, key)
}

// DecryptAIProviderKey decrypts a ciphertext produced by EncryptAIProviderKey.
func DecryptAIProviderKey(encoded string) (string, error) {
	key, err := aiProviderEncryptionKey()
	if err != nil {
		return "", err
	}
	return decryptWithKey(encoded, key)
}

func encryptWithKey(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func decryptWithKey(encoded string, key []byte) (string, error) {
	ciphertext, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("crypto: decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("crypto: ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}

	return string(plaintext), nil
}

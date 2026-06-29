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
	if len(key) >= 32 {
		return []byte(key[:32])
	}
	padded := make([]byte, 32)
	copy(padded, key)
	return padded
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a base64-encoded ciphertext.
func Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(encryptionKey())
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

// Decrypt decrypts a base64-encoded ciphertext produced by Encrypt.
func Decrypt(encoded string) (string, error) {
	ciphertext, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("crypto: decode: %w", err)
	}

	block, err := aes.NewCipher(encryptionKey())
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

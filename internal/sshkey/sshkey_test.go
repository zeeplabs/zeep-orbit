package sshkey

import (
	"strings"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Errorf("public key should start with 'ssh-ed25519 ', got: %s", pub)
	}
	if len(pub) < 20 {
		t.Errorf("public key too short: %d chars", len(pub))
	}

	if !strings.HasPrefix(priv, "-----BEGIN PRIVATE KEY-----") {
		t.Errorf("private key should be PEM-encoded, got prefix: %s", priv[:50])
	}
	if !strings.Contains(priv, "-----END PRIVATE KEY-----") {
		t.Error("private key missing PEM footer")
	}
}

func TestGenerateKeyPairIsDeterministicallyDifferent(t *testing.T) {
	pub1, priv1, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair 1: %v", err)
	}
	pub2, priv2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair 2: %v", err)
	}

	if pub1 == pub2 {
		t.Error("two generated public keys should differ")
	}
	if priv1 == priv2 {
		t.Error("two generated private keys should differ")
	}
}

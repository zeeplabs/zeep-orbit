package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

func GenerateKeyPair() (publicKey string, privateKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("sshkey: generate ed25519: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("sshkey: marshal public key: %w", err)
	}
	publicKey = string(ssh.MarshalAuthorizedKey(sshPub))
	publicKey = publicKey[:len(publicKey)-1] // trim trailing newline added by MarshalAuthorizedKey

	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", fmt.Errorf("sshkey: marshal private key: %w", err)
	}
	privateKey = string(pem.EncodeToMemory(block))

	return publicKey, privateKey, nil
}

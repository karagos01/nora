package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
)

// GenerateNonce generates 32 random bytes as a hex string
func GenerateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// VerifySignature verifies an ed25519 signature of the nonce
func VerifySignature(publicKey, nonce, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(publicKey, nonce, signature)
}

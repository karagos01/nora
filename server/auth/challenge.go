package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
)

// GenerateNonce generuje 32 random bajtů jako hex string
func GenerateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// VerifySignature ověří ed25519 podpis nonce
func VerifySignature(publicKey, nonce, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(publicKey, nonce, signature)
}

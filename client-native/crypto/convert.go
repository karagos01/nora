package crypto

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"

	"filippo.io/edwards25519"
)

// Ed25519SeedToX25519Private converts an ed25519 seed (32B) to an x25519 private key (32B scalar).
// Compatible with @noble/curves ed25519.utils.toMontgomerySecret()
func Ed25519SeedToX25519Private(seedHex string) ([]byte, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		return nil, errors.New("invalid seed")
	}

	// SHA-512(seed) → clamp first 32 bytes
	h := sha512.Sum512(seed)
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64

	// Copy the x25519 scalar before clearing the full hash
	scalar := make([]byte, 32)
	copy(scalar, h[:32])
	for i := range h {
		h[i] = 0
	}

	return scalar, nil
}

// Ed25519PubToX25519Public converts an ed25519 public key (32B Edwards point) to an x25519 public key (32B Montgomery point).
// Compatible with @noble/curves ed25519.utils.toMontgomery()
func Ed25519PubToX25519Public(pubHex string) ([]byte, error) {
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != 32 {
		return nil, errors.New("invalid public key")
	}

	edPoint, err := new(edwards25519.Point).SetBytes(pubBytes)
	if err != nil {
		return nil, errors.New("invalid Edwards point")
	}

	mont := edPoint.BytesMontgomery()

	// Validation: x25519 public key must not be zero (low-order point)
	allZero := true
	for _, b := range mont {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil, errors.New("x25519 public key is a zero point")
	}

	return mont, nil
}

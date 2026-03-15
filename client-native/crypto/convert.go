package crypto

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"

	"filippo.io/edwards25519"
)

// Ed25519SeedToX25519Private převede ed25519 seed (32B) na x25519 private key (32B scalar).
// Kompatibilní s @noble/curves ed25519.utils.toMontgomerySecret()
func Ed25519SeedToX25519Private(seedHex string) ([]byte, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		return nil, errors.New("neplatný seed")
	}

	// SHA-512(seed) → clamp prvních 32 bajtů
	h := sha512.Sum512(seed)
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64

	return h[:32], nil
}

// Ed25519PubToX25519Public převede ed25519 public key (32B Edwards point) na x25519 public key (32B Montgomery point).
// Kompatibilní s @noble/curves ed25519.utils.toMontgomery()
func Ed25519PubToX25519Public(pubHex string) ([]byte, error) {
	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != 32 {
		return nil, errors.New("neplatný public key")
	}

	edPoint, err := new(edwards25519.Point).SetBytes(pubBytes)
	if err != nil {
		return nil, errors.New("neplatný Edwards bod")
	}

	mont := edPoint.BytesMontgomery()

	// Validace: x25519 public key nesmí být nulový (low-order point)
	allZero := true
	for _, b := range mont {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil, errors.New("x25519 public key je nulový bod")
	}

	return mont, nil
}

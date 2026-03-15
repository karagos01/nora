package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

type Keypair struct {
	PublicKey  string // hex
	SecretKey string // hex (32B seed)
}

// ClearBytes zeroes out a byte slice (best-effort clearing of sensitive data from memory)
func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// GenerateKeypair generates a new ed25519 keypair.
// SecretKey is 32B seed (not 64B expanded).
func GenerateKeypair() (*Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ed25519 generate: %w", err)
	}
	seed := priv.Seed()
	return &Keypair{
		PublicKey:  hex.EncodeToString(pub),
		SecretKey:  hex.EncodeToString(seed),
	}, nil
}

// PublicKeyFromSeed derives the public key from a seed.
func PublicKeyFromSeed(seedHex string) (string, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		return "", errors.New("invalid seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return hex.EncodeToString(pub), nil
}

// Sign signs data with an ed25519 key.
func Sign(seedHex string, dataHex string) (string, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		return "", errors.New("invalid seed")
	}
	defer ClearBytes(seed)
	data, err := hex.DecodeString(dataHex)
	if err != nil {
		return "", errors.New("invalid data")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	defer ClearBytes(priv)
	sig := ed25519.Sign(priv, data)
	return hex.EncodeToString(sig), nil
}

const (
	pbkdf2Iterations       = 800000 // Current iterations (2026)
	pbkdf2IterationsLegacy = 600000 // Legacy iterations (backward compatibility)
)

// EncryptKey encrypts the private key with a password (PBKDF2 + AES-256-GCM).
// Format: hex(salt[16] + iv[12] + ciphertext+tag)
func EncryptKey(secretKeyHex string, password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}

	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, 32, sha256.New)
	defer ClearBytes(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nil, iv, []byte(secretKeyHex), nil)

	result := make([]byte, 16+12+len(ciphertext))
	copy(result[0:16], salt)
	copy(result[16:28], iv)
	copy(result[28:], ciphertext)

	return hex.EncodeToString(result), nil
}

// DecryptKey decrypts the private key with a password.
// Tries current iterations (800k), then falls back to legacy (600k) for backward compatibility.
func DecryptKey(encrypted string, password string) (string, error) {
	data, err := hex.DecodeString(encrypted)
	if err != nil {
		return "", errors.New("invalid hex")
	}
	if len(data) < 28+16 { // salt(16) + iv(12) + at least tag(16)
		return "", errors.New("data too short")
	}

	salt := data[0:16]
	iv := data[16:28]
	ciphertext := data[28:]

	// Try current iterations, then legacy
	for _, iters := range []int{pbkdf2Iterations, pbkdf2IterationsLegacy} {
		key := pbkdf2.Key([]byte(password), salt, iters, 32, sha256.New)
		block, err := aes.NewCipher(key)
		if err != nil {
			ClearBytes(key)
			continue
		}
		aesGCM, err := cipher.NewGCM(block)
		if err != nil {
			ClearBytes(key)
			continue
		}
		plaintext, err := aesGCM.Open(nil, iv, ciphertext, nil)
		ClearBytes(key)
		if err == nil {
			return string(plaintext), nil
		}
	}
	return "", errors.New("wrong password")
}

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"crypto/sha256"
	"io"
)

// deriveSharedKey — ECDH(ed25519→x25519) + HKDF-SHA256 → 32B AES key.
// Compatible with the JS client (dm.ts deriveSharedKey).
func deriveSharedKey(mySecretHex, theirPubHex string) ([]byte, error) {
	// ed25519 → x25519 conversion
	myX25519Priv, err := Ed25519SeedToX25519Private(mySecretHex)
	if err != nil {
		return nil, err
	}
	defer ClearBytes(myX25519Priv)

	theirX25519Pub, err := Ed25519PubToX25519Public(theirPubHex)
	if err != nil {
		return nil, err
	}

	// X25519 ECDH → shared secret
	sharedSecret, err := curve25519.X25519(myX25519Priv, theirX25519Pub)
	if err != nil {
		return nil, err
	}
	defer ClearBytes(sharedSecret)

	// HKDF-SHA256 with info="nora-dm-e2e", salt=empty → 32B AES key
	// Empty salt is intentional — compatibility with the JS client (dm.ts).
	// HKDF without salt is secure when the input material (ECDH shared secret) has sufficient entropy.
	hkdfReader := hkdf.New(sha256.New, sharedSecret, []byte{}, []byte("nora-dm-e2e"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	return key, nil
}

// EncryptDM encrypts plaintext for DM. Output: hex(nonce[12] + ciphertext+tag).
// Compatible with the JS client (dm.ts encryptDM).
func EncryptDM(mySecretHex, theirPubHex, plaintext string) (string, error) {
	key, err := deriveSharedKey(mySecretHex, theirPubHex)
	if err != nil {
		return "", err
	}
	defer ClearBytes(key)

	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nil, nonce, []byte(plaintext), nil)

	result := make([]byte, 12+len(ciphertext))
	copy(result[0:12], nonce)
	copy(result[12:], ciphertext)

	return hex.EncodeToString(result), nil
}

// DecryptDM decrypts a DM message. Input: hex(nonce[12] + ciphertext+tag).
// Compatible with the JS client (dm.ts decryptDM).
func DecryptDM(mySecretHex, theirPubHex, encryptedHex string) (string, error) {
	data, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", errors.New("invalid hex")
	}
	if len(data) < 28 { // nonce(12) + AES-GCM tag(16)
		return "", errors.New("data too short")
	}

	nonce := data[:12]
	ciphertext := data[12:]

	key, err := deriveSharedKey(mySecretHex, theirPubHex)
	if err != nil {
		return "", err
	}
	defer ClearBytes(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("decryption failed")
	}

	return string(plaintext), nil
}

// DeriveStorageKey derives a 32-byte AES key from the ed25519 seed for local file encryption.
// Uses HKDF-SHA256 with info="nora-local-storage" (different from DM key derivation).
func DeriveStorageKey(seedHex string) ([]byte, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		return nil, errors.New("invalid seed")
	}
	defer ClearBytes(seed)

	hkdfReader := hkdf.New(sha256.New, seed, []byte{}, []byte("nora-local-storage"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// EncryptLocal encrypts data with AES-256-GCM using the storage key.
// Returns nonce(12) + ciphertext+tag.
func EncryptLocal(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := aesGCM.Seal(nil, nonce, plaintext, nil)
	result := make([]byte, 12+len(ciphertext))
	copy(result[:12], nonce)
	copy(result[12:], ciphertext)
	return result, nil
}

// DecryptLocal decrypts data encrypted by EncryptLocal.
// Input: nonce(12) + ciphertext+tag.
func DecryptLocal(key, data []byte) ([]byte, error) {
	if len(data) < 28 {
		return nil, errors.New("data too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aesGCM.Open(nil, data[:12], data[12:], nil)
}

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

// deriveSharedKey — ECDH(ed25519→x25519) + HKDF-SHA256 → 32B AES klíč.
// Kompatibilní s JS klientem (dm.ts deriveSharedKey).
func deriveSharedKey(mySecretHex, theirPubHex string) ([]byte, error) {
	// ed25519 → x25519 konverze
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

	// HKDF-SHA256 s info="nora-dm-e2e", salt=empty → 32B AES klíč
	// Prázdný salt je záměrný — kompatibilita s JS klientem (dm.ts).
	// HKDF bez saltu je bezpečný pokud vstupní materiál (ECDH shared secret) má dostatečnou entropii.
	hkdfReader := hkdf.New(sha256.New, sharedSecret, []byte{}, []byte("nora-dm-e2e"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	return key, nil
}

// EncryptDM zašifruje plaintext pro DM. Výstup: hex(nonce[12] + ciphertext+tag).
// Kompatibilní s JS klientem (dm.ts encryptDM).
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

// DecryptDM dešifruje DM zprávu. Vstup: hex(nonce[12] + ciphertext+tag).
// Kompatibilní s JS klientem (dm.ts decryptDM).
func DecryptDM(mySecretHex, theirPubHex, encryptedHex string) (string, error) {
	data, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", errors.New("neplatný hex")
	}
	if len(data) < 28 { // nonce(12) + AES-GCM tag(16)
		return "", errors.New("příliš krátká data")
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
		return "", errors.New("dešifrování selhalo")
	}

	return string(plaintext), nil
}

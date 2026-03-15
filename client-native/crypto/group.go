package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// GenerateGroupKey generuje náhodný 32B AES-256-GCM klíč pro skupinu.
func GenerateGroupKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}

// EncryptGroupMessage zašifruje zprávu sdíleným group klíčem (AES-256-GCM).
// Výstup: hex(nonce[12] + ciphertext+tag).
func EncryptGroupMessage(groupKeyHex, plaintext string) (string, error) {
	key, err := hex.DecodeString(groupKeyHex)
	if err != nil {
		return "", err
	}
	defer ClearBytes(key)
	if len(key) != 32 {
		return "", errors.New("invalid group key length")
	}

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

// DecryptGroupMessage dešifruje zprávu sdíleným group klíčem (AES-256-GCM).
// Vstup: hex(nonce[12] + ciphertext+tag).
func DecryptGroupMessage(groupKeyHex, encryptedHex string) (string, error) {
	key, err := hex.DecodeString(groupKeyHex)
	if err != nil {
		return "", err
	}
	defer ClearBytes(key)
	if len(key) != 32 {
		return "", errors.New("invalid group key length")
	}

	data, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", errors.New("invalid hex")
	}
	if len(data) < 28 { // nonce(12) + AES-GCM tag(16)
		return "", errors.New("data too short")
	}

	nonce := data[:12]
	ciphertext := data[12:]

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

// EncryptGroupKeyForMember šifruje group key pro konkrétního člena přes ECDH (DM šifrování).
// Používá existující DM šifrování — ECDH + HKDF + AES-256-GCM.
func EncryptGroupKeyForMember(mySecretHex, memberPubHex, groupKeyHex string) (string, error) {
	return EncryptDM(mySecretHex, memberPubHex, groupKeyHex)
}

// DecryptGroupKeyFromMember dešifruje group key od člena přes ECDH.
func DecryptGroupKeyFromMember(mySecretHex, senderPubHex, encryptedKeyHex string) (string, error) {
	return DecryptDM(mySecretHex, senderPubHex, encryptedKeyHex)
}

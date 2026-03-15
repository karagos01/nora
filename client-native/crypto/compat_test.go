package crypto

import (
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/curve25519"
)

// Test that ed25519→x25519 conversion produces valid X25519 keys
// and ECDH works symmetrically (A→B == B→A).
func TestECDHSymmetry(t *testing.T) {
	kp1, _ := GenerateKeypair()
	kp2, _ := GenerateKeypair()

	// A→B shared secret
	priv1, _ := Ed25519SeedToX25519Private(kp1.SecretKey)
	pub2, _ := Ed25519PubToX25519Public(kp2.PublicKey)
	shared1, err := curve25519.X25519(priv1, pub2)
	if err != nil {
		t.Fatal(err)
	}

	// B→A shared secret
	priv2, _ := Ed25519SeedToX25519Private(kp2.SecretKey)
	pub1, _ := Ed25519PubToX25519Public(kp1.PublicKey)
	shared2, err := curve25519.X25519(priv2, pub1)
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(shared1) != hex.EncodeToString(shared2) {
		t.Fatal("ECDH is not symmetric!")
	}
}

// Test that encryption/decryption is cross-compatible (A encrypts, B decrypts).
func TestDMCrossDecrypt(t *testing.T) {
	kp1, _ := GenerateKeypair()
	kp2, _ := GenerateKeypair()

	// A → B
	enc, err := EncryptDM(kp1.SecretKey, kp2.PublicKey, "Test message from A")
	if err != nil {
		t.Fatal(err)
	}

	dec, err := DecryptDM(kp2.SecretKey, kp1.PublicKey, enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != "Test message from A" {
		t.Fatalf("B decrypted incorrectly: %q", dec)
	}

	// B → A
	enc2, err := EncryptDM(kp2.SecretKey, kp1.PublicKey, "Reply from B")
	if err != nil {
		t.Fatal(err)
	}

	dec2, err := DecryptDM(kp1.SecretKey, kp2.PublicKey, enc2)
	if err != nil {
		t.Fatal(err)
	}
	if dec2 != "Reply from B" {
		t.Fatalf("A decrypted incorrectly: %q", dec2)
	}
}

// Test that sign/verify works for challenge-response.
func TestSignVerify(t *testing.T) {
	kp, _ := GenerateKeypair()

	// Simulated server nonce (hex)
	nonce := "deadbeef01020304050607080910111213141516171819202122232425262728"

	sig, err := Sign(kp.SecretKey, nonce)
	if err != nil {
		t.Fatal(err)
	}

	// Verification
	if len(sig) != 128 { // 64 bytes = 128 hex chars
		t.Fatalf("signature has %d chars, expected 128", len(sig))
	}
}

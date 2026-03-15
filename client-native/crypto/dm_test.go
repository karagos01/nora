package crypto

import (
	"testing"
)

func TestEncryptDecryptDM(t *testing.T) {
	// Generate two keypairs
	kp1, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	plaintext := "Hello, this is a test message!"

	// Encryption by side 1
	encrypted, err := EncryptDM(kp1.SecretKey, kp2.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Decryption by side 2
	decrypted, err := DecryptDM(kp2.SecretKey, kp1.PublicKey, encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("mismatch: %q != %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptKey(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	password := "test-heslo-123"

	encrypted, err := EncryptKey(kp.SecretKey, password)
	if err != nil {
		t.Fatalf("key encryption failed: %v", err)
	}

	decrypted, err := DecryptKey(encrypted, password)
	if err != nil {
		t.Fatalf("key decryption failed: %v", err)
	}

	if decrypted != kp.SecretKey {
		t.Fatalf("key mismatch: %q != %q", decrypted, kp.SecretKey)
	}

	// Wrong password
	_, err = DecryptKey(encrypted, "wrong-password")
	if err == nil {
		t.Fatal("should fail with wrong password")
	}
}

func TestEd25519X25519Conversion(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	priv, err := Ed25519SeedToX25519Private(kp.SecretKey)
	if err != nil {
		t.Fatalf("private key conversion failed: %v", err)
	}
	if len(priv) != 32 {
		t.Fatalf("x25519 private key has %d bytes, expected 32", len(priv))
	}

	pub, err := Ed25519PubToX25519Public(kp.PublicKey)
	if err != nil {
		t.Fatalf("public key conversion failed: %v", err)
	}
	if len(pub) != 32 {
		t.Fatalf("x25519 public key has %d bytes, expected 32", len(pub))
	}
}

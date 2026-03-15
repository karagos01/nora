package crypto

import (
	"testing"
)

func TestEncryptDecryptDM(t *testing.T) {
	// Vygenerujeme dva keypáry
	kp1, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	plaintext := "Ahoj, toto je testovací zpráva!"

	// Šifrování stranou 1
	encrypted, err := EncryptDM(kp1.SecretKey, kp2.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("šifrování selhalo: %v", err)
	}

	// Dešifrování stranou 2
	decrypted, err := DecryptDM(kp2.SecretKey, kp1.PublicKey, encrypted)
	if err != nil {
		t.Fatalf("dešifrování selhalo: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("nesouhlasí: %q != %q", decrypted, plaintext)
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
		t.Fatalf("šifrování klíče selhalo: %v", err)
	}

	decrypted, err := DecryptKey(encrypted, password)
	if err != nil {
		t.Fatalf("dešifrování klíče selhalo: %v", err)
	}

	if decrypted != kp.SecretKey {
		t.Fatalf("klíč nesouhlasí: %q != %q", decrypted, kp.SecretKey)
	}

	// Špatné heslo
	_, err = DecryptKey(encrypted, "spatne-heslo")
	if err == nil {
		t.Fatal("mělo selhat se špatným heslem")
	}
}

func TestEd25519X25519Conversion(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	priv, err := Ed25519SeedToX25519Private(kp.SecretKey)
	if err != nil {
		t.Fatalf("konverze private selhala: %v", err)
	}
	if len(priv) != 32 {
		t.Fatalf("x25519 private key má %d bajtů, očekáváno 32", len(priv))
	}

	pub, err := Ed25519PubToX25519Public(kp.PublicKey)
	if err != nil {
		t.Fatalf("konverze public selhala: %v", err)
	}
	if len(pub) != 32 {
		t.Fatalf("x25519 public key má %d bajtů, očekáváno 32", len(pub))
	}
}

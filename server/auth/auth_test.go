package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Challenge / Signature ---

func TestGenerateNonce(t *testing.T) {
	n1, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	if len(n1) != 64 { // 32 bytes hex
		t.Fatalf("nonce length: got %d, want 64", len(n1))
	}

	n2, _ := GenerateNonce()
	if n1 == n2 {
		t.Fatal("two nonces should be different")
	}
}

func TestVerifySignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	nonce, _ := GenerateNonce()
	nonceBytes, _ := hex.DecodeString(nonce)
	sig := ed25519.Sign(priv, nonceBytes)

	if !VerifySignature(pub, nonceBytes, sig) {
		t.Fatal("valid signature should pass")
	}

	// Bad signature
	sig[0] ^= 0xFF
	if VerifySignature(pub, nonceBytes, sig) {
		t.Fatal("corrupted signature should fail")
	}

	// Bad key length
	if VerifySignature([]byte("short"), nonceBytes, sig) {
		t.Fatal("short key should fail")
	}
}

// --- JWT ---

func TestJWTGenerateValidate(t *testing.T) {
	svc := NewJWTService("test-secret-key-123456789", 15*time.Minute)

	token, err := svc.Generate("user123", "alice", false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("UserID: got %q, want %q", claims.UserID, "user123")
	}
	if claims.Username != "alice" {
		t.Errorf("Username: got %q, want %q", claims.Username, "alice")
	}
	if claims.IsOwner {
		t.Error("IsOwner should be false")
	}
}

func TestJWTOwner(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)

	token, _ := svc.Generate("owner1", "admin", true)
	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	if !claims.IsOwner {
		t.Error("IsOwner should be true")
	}
}

func TestJWTExpired(t *testing.T) {
	svc := NewJWTService("test-secret", 1*time.Millisecond)

	token, _ := svc.Generate("user1", "bob", false)
	time.Sleep(10 * time.Millisecond)

	_, err := svc.Validate(token)
	if err == nil {
		t.Fatal("expired token should fail validation")
	}
}

func TestJWTWrongSecret(t *testing.T) {
	svc1 := NewJWTService("secret-1", 15*time.Minute)
	svc2 := NewJWTService("secret-2", 15*time.Minute)

	token, _ := svc1.Generate("user1", "alice", false)

	_, err := svc2.Validate(token)
	if err == nil {
		t.Fatal("token signed with different secret should fail")
	}
}

func TestJWTInvalidToken(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)

	_, err := svc.Validate("not.a.valid.token")
	if err == nil {
		t.Fatal("garbage token should fail")
	}

	_, err = svc.Validate("")
	if err == nil {
		t.Fatal("empty token should fail")
	}
}

func TestJWTRejectsNonHMAC(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)
	// Token with "none" algorithm should not pass
	_, err := svc.Validate("eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1aWQiOiJ0ZXN0IiwidXNyIjoiaGFjayJ9.")
	if err == nil {
		t.Fatal("none algorithm should be rejected")
	}
}

// --- Middleware ---

func TestMiddlewareValidToken(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)
	token, _ := svc.Generate("user1", "alice", false)

	var gotUser *ContextUser
	handler := Middleware(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if gotUser == nil {
		t.Fatal("user should be set in context")
	}
	if gotUser.ID != "user1" || gotUser.Username != "alice" {
		t.Errorf("user: got %+v", gotUser)
	}
}

func TestMiddlewareMissingHeader(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)

	handler := Middleware(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestMiddlewareInvalidToken(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)

	handler := Middleware(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestMiddlewareBannedUser(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)
	token, _ := svc.Generate("banned-user", "evil", false)

	isBanned := func(userID string) bool {
		return userID == "banned-user"
	}

	handler := Middleware(svc, isBanned)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for banned user")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403", rec.Code)
	}
}

func TestMiddlewareInvalidFormat(t *testing.T) {
	svc := NewJWTService("test-secret", 15*time.Minute)

	handler := Middleware(svc, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

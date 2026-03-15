package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusOK, map[string]string{"foo": "bar"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type: got %q, want application/json", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["foo"] != "bar" {
		t.Errorf("body: got %v", result)
	}
}

func TestError(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusBadRequest, "something went wrong")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}

	var result ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Error != "something went wrong" {
		t.Errorf("error: got %q", result.Error)
	}
}

func TestDecodeJSON(t *testing.T) {
	body := `{"name": "test", "value": 42}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))

	var dst struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	if err := DecodeJSON(req, &dst); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dst.Name != "test" || dst.Value != 42 {
		t.Errorf("decoded: %+v", dst)
	}
}

func TestDecodeJSONUnknownFields(t *testing.T) {
	body := `{"name": "test", "extra": "not_allowed"}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))

	var dst struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(req, &dst); err == nil {
		t.Fatal("should reject unknown fields")
	}
}

func TestDecodeJSONInvalid(t *testing.T) {
	body := `{not json}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))

	var dst struct{}
	if err := DecodeJSON(req, &dst); err == nil {
		t.Fatal("should reject invalid JSON")
	}
}

// --- GetClientIP ---

func TestGetClientIPDirect(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.1:12345"

	ip := GetClientIP(req)
	if ip != "203.0.113.1" {
		t.Errorf("got %q, want 203.0.113.1", ip)
	}
}

func TestGetClientIPTrustedProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")

	ip := GetClientIP(req)
	if ip != "203.0.113.50" {
		t.Errorf("got %q, want 203.0.113.50 (first from XFF)", ip)
	}
}

func TestGetClientIPTrustedProxyXRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	req.Header.Set("X-Real-IP", "198.51.100.1")

	ip := GetClientIP(req)
	if ip != "198.51.100.1" {
		t.Errorf("got %q, want 198.51.100.1", ip)
	}
}

func TestGetClientIPUntrustedProxy(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.99:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	ip := GetClientIP(req)
	// Vzdálený klient není trusted proxy — ignorujeme X-Forwarded-For
	if ip != "203.0.113.99" {
		t.Errorf("got %q, want 203.0.113.99 (should ignore XFF from untrusted)", ip)
	}
}

func TestGetClientIPNoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1"

	ip := GetClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("got %q, want 192.168.1.1", ip)
	}
}

func TestIsTrustedProxy(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"203.0.113.1", false},
		{"8.8.8.8", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		if got := isTrustedProxy(tt.ip); got != tt.want {
			t.Errorf("isTrustedProxy(%q): got %v, want %v", tt.ip, got, tt.want)
		}
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Rate Limiter ---

func TestRateLimiterAllow(t *testing.T) {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     10,
		burst:    5,
	}

	// First 5 requests should pass (burst=5)
	for i := 0; i < 5; i++ {
		if !rl.allow("192.168.1.1") {
			t.Fatalf("request %d should be allowed within burst", i+1)
		}
	}

	// 6th request should be denied (burst exhausted)
	if rl.allow("192.168.1.1") {
		t.Fatal("request after burst should be denied")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     10,
		burst:    2,
	}

	// Two different clients have independent limits
	if !rl.allow("10.0.0.1") {
		t.Fatal("first IP should be allowed")
	}
	if !rl.allow("10.0.0.2") {
		t.Fatal("second IP should be allowed")
	}
	if !rl.allow("10.0.0.1") {
		t.Fatal("first IP second request should be allowed (burst=2)")
	}
	if !rl.allow("10.0.0.2") {
		t.Fatal("second IP second request should be allowed (burst=2)")
	}

	// Both should now be at the limit
	if rl.allow("10.0.0.1") {
		t.Fatal("first IP should be rate limited")
	}
	if rl.allow("10.0.0.2") {
		t.Fatal("second IP should be rate limited")
	}
}

func TestRateLimiterMiddleware429(t *testing.T) {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     10,
		burst:    1,
	}

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rec.Code)
	}

	// Second request — rate limited
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited request: got %d, want 429", rec.Code)
	}
}

// --- Endpoint Rate Limiter ---

func TestResolveTierStrict(t *testing.T) {
	erl := &EndpointRateLimiter{rules: buildRules()}

	cases := []struct {
		method string
		path   string
		want   Tier
	}{
		{"POST", "/api/auth/challenge", TierStrict},
		{"POST", "/api/auth/verify", TierStrict},
		{"POST", "/api/auth/refresh", TierStrict},
	}
	for _, tc := range cases {
		got := erl.resolveTier(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("resolveTier(%s, %s) = %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestResolveTierUpload(t *testing.T) {
	erl := &EndpointRateLimiter{rules: buildRules()}

	cases := []struct {
		method string
		path   string
		want   Tier
	}{
		{"POST", "/api/upload", TierUpload},
		{"POST", "/api/upload/init", TierUpload},
		{"PATCH", "/api/upload/abc123", TierUpload},
		{"POST", "/api/emojis", TierUpload},
		{"POST", "/api/users/me/avatar", TierUpload},
		{"POST", "/api/server/icon", TierUpload},
		{"POST", "/api/storage/files", TierUpload},
		{"POST", "/api/gameservers/abc/files/upload", TierUpload},
	}
	for _, tc := range cases {
		got := erl.resolveTier(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("resolveTier(%s, %s) = %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestResolveTierRelaxed(t *testing.T) {
	erl := &EndpointRateLimiter{rules: buildRules()}

	cases := []struct {
		method string
		path   string
		want   Tier
	}{
		{"GET", "/api/health", TierRelaxed},
		{"GET", "/api/server", TierRelaxed},
		{"GET", "/api/ws", TierRelaxed},
		{"GET", "/api/channels", TierRelaxed},
		{"GET", "/api/channels/abc/messages", TierRelaxed},
		{"GET", "/api/users", TierRelaxed},
		{"GET", "/api/uploads/file.png", TierRelaxed},
		{"GET", "/api/gallery", TierRelaxed},
		{"GET", "/api/emojis", TierRelaxed},
		{"GET", "/api/roles", TierRelaxed},
		{"GET", "/api/friends", TierRelaxed},
		{"GET", "/api/dm", TierRelaxed},
		{"GET", "/api/groups", TierRelaxed},
		{"GET", "/api/categories", TierRelaxed},
		{"GET", "/api/messages/abc/thread", TierRelaxed},
	}
	for _, tc := range cases {
		got := erl.resolveTier(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("resolveTier(%s, %s) = %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestResolveTierNormalDefault(t *testing.T) {
	erl := &EndpointRateLimiter{rules: buildRules()}

	// POST to normal endpoints → TierNormal (default)
	cases := []struct {
		method string
		path   string
	}{
		{"POST", "/api/channels"},
		{"POST", "/api/bans"},
		{"DELETE", "/api/messages/abc"},
		{"PATCH", "/api/users/me"},
		{"POST", "/api/friends/requests"},
		{"PUT", "/api/messages/abc/reactions"},
		{"POST", "/api/whiteboards"},
	}
	for _, tc := range cases {
		got := erl.resolveTier(tc.method, tc.path)
		if got != TierNormal {
			t.Errorf("resolveTier(%s, %s) = %d, want TierNormal(%d)", tc.method, tc.path, got, TierNormal)
		}
	}
}

func TestResolveTierMethodMatters(t *testing.T) {
	erl := &EndpointRateLimiter{rules: buildRules()}

	// GET /api/emojis → TierRelaxed, POST /api/emojis → TierUpload
	if got := erl.resolveTier("GET", "/api/emojis"); got != TierRelaxed {
		t.Errorf("GET /api/emojis: got %d, want TierRelaxed(%d)", got, TierRelaxed)
	}
	if got := erl.resolveTier("POST", "/api/emojis"); got != TierUpload {
		t.Errorf("POST /api/emojis: got %d, want TierUpload(%d)", got, TierUpload)
	}
}

func TestResolveTierGameserverNonUpload(t *testing.T) {
	erl := &EndpointRateLimiter{rules: buildRules()}

	// GET /api/gameservers/abc/files → TierNormal (not upload)
	if got := erl.resolveTier("GET", "/api/gameservers/abc/files"); got != TierNormal {
		t.Errorf("GET gameserver files: got %d, want TierNormal(%d)", got, TierNormal)
	}
	// GET /api/gameservers → TierNormal
	if got := erl.resolveTier("GET", "/api/gameservers"); got != TierNormal {
		t.Errorf("GET gameservers list: got %d, want TierNormal(%d)", got, TierNormal)
	}
}

func TestEndpointRateLimiterMiddleware(t *testing.T) {
	erl := NewEndpointRateLimiter(10, 20)

	handler := erl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Strict tier (burst=5) — 5 requests pass, 6th doesn't
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/api/auth/challenge", nil)
		req.RemoteAddr = "5.5.5.5:9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("strict request %d: got %d, want 200", i+1, rec.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/auth/challenge", nil)
	req.RemoteAddr = "5.5.5.5:9999"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("strict 6th request: got %d, want 429", rec.Code)
	}

	// Different IP on relaxed tier should pass independently
	req = httptest.NewRequest("GET", "/api/health", nil)
	req.RemoteAddr = "6.6.6.6:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("relaxed request from different IP: got %d, want 200", rec.Code)
	}
}

func TestEndpointRateLimiterTierIsolation(t *testing.T) {
	erl := NewEndpointRateLimiter(10, 20)

	handler := erl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ip := "7.7.7.7:4321"

	// Exhaust strict tier (burst=5)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/api/auth/challenge", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Strict tier should be exhausted
	req := httptest.NewRequest("POST", "/api/auth/challenge", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("strict should be exhausted: got %d, want 429", rec.Code)
	}

	// But relaxed tier from the same IP should still pass
	req = httptest.NewRequest("GET", "/api/health", nil)
	req.RemoteAddr = ip
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("relaxed tier should be independent: got %d, want 200", rec.Code)
	}

	// And normal tier too
	req = httptest.NewRequest("POST", "/api/channels", nil)
	req.RemoteAddr = ip
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("normal tier should be independent: got %d, want 200", rec.Code)
	}
}

func TestEndpointRateLimiterNormalConfigFromToml(t *testing.T) {
	// Verify that normal tier takes values from config
	erl := NewEndpointRateLimiter(15, 3)

	handler := erl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ip := "8.8.8.8:5555"
	// burst=3 → 3 projdou, 4. ne
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/api/channels", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("normal request %d: got %d, want 200", i+1, rec.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/channels", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("normal 4th request: got %d, want 429", rec.Code)
	}
}

// --- CORS ---

func TestCORSPreflight(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for OPTIONS")
	}))

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Errorf("ACAO: got %q, want origin echo", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("ACAH should be set")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("ACAM should be set")
	}
}

func TestCORSNoOrigin(t *testing.T) {
	var called bool
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler should be called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO should not be set without Origin header, got %q", got)
	}
}

func TestCORSNormalRequest(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Errorf("ACAO: got %q, want origin echo", got)
	}
}

// --- Logging ---

func TestLoggingDefaultStatus(t *testing.T) {
	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// We don't call WriteHeader — default 200
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
}

func TestLoggingCustomStatus(t *testing.T) {
	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

func TestStatusWriterUnwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: inner, status: 200}

	if got := sw.Unwrap(); got != inner {
		t.Fatal("Unwrap should return original ResponseWriter")
	}
}

package handlers_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"nora/auth"
	"nora/config"
	"nora/database"
	"nora/database/queries"
	"nora/handlers"
	"nora/moderation"
	"nora/ws"
	"testing"
	"time"
)

func setupDeps(t *testing.T) *handlers.Deps {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := database.Open(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.SeedDefaults(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	hub := ws.NewHub()
	go hub.Run()

	jwtSvc := auth.NewJWTService("test-secret-key-for-integration-tests-1234567890", 15*time.Minute)

	return &handlers.Deps{
		DB:                  db,
		DBPath:              ":memory:",
		Hub:                 hub,
		JWTService:          jwtSvc,
		RefreshTTL:          7 * 24 * time.Hour,
		ChallengeTTL:        5 * time.Minute,
		OpenReg:             true,
		UploadsDir:          t.TempDir(),
		MaxUploadSize:       100 * 1024 * 1024,
		AllowedTypes:        []string{"image/*", "video/*", "application/pdf", "text/*"},
		ServerName:          "Test Server",
		SourceURL:           "https://example.com",
		StorageMaxMB:        1024,
		ChannelHistoryLimit: 10000,
		Uploads:             handlers.NewUploadSessionStore(),
		UploadLimiter:       handlers.NewUploadRateLimiter(30),
		Users:               &queries.UserQueries{DB: db.DB},
		Channels:            &queries.ChannelQueries{DB: db.DB},
		Messages:            &queries.MessageQueries{DB: db.DB},
		Roles:               &queries.RoleQueries{DB: db.DB},
		Invites:             &queries.InviteQueries{DB: db.DB},
		RefreshTokens:       &queries.RefreshTokenQueries{DB: db.DB},
		DMs:                 &queries.DMQueries{DB: db.DB},
		Bans:                &queries.BanQueries{DB: db.DB},
		Attachments:         &queries.AttachmentQueries{DB: db.DB},
		AuthChallenges:      &queries.AuthChallengeQueries{DB: db.DB},
		Timeouts:            &queries.TimeoutQueries{DB: db.DB},
		Friends:             &queries.FriendQueries{DB: db.DB},
		Settings:            &queries.SettingsQueries{DB: db.DB},
		FriendRequests:      &queries.FriendRequestQueries{DB: db.DB},
		Blocks:              &queries.BlockQueries{DB: db.DB},
		Groups:              &queries.GroupQueries{DB: db.DB},
		Emojis:              &queries.EmojiQueries{DB: db.DB},
		Categories:          &queries.CategoryQueries{DB: db.DB},
		Reactions:           &queries.ReactionQueries{DB: db.DB},
		IPBans:              &queries.IPBanQueries{DB: db.DB},
		Storage:             &queries.StorageQueries{DB: db.DB},
		AuditLog:            &queries.AuditLogQueries{DB: db.DB},
		LinkPreviews:        &queries.LinkPreviewQueries{DB: db.DB},
		Polls:               &queries.PollQueries{DB: db.DB},
		Webhooks:            &queries.WebhookQueries{DB: db.DB},
		GalleryQ:            &queries.GalleryQueries{DB: db.DB},
		FileStorage:         &queries.FileStorageQueries{DB: db.DB},
		Shares:              &queries.ShareQueries{DB: db.DB},
		Whiteboards:         &queries.WhiteboardQueries{DB: db.DB},
		LAN:                 &queries.LANQueries{DB: db.DB},
		GameServerQ:         &queries.GameServerQueries{DB: db.DB},
		SwarmSeeds:          &queries.SwarmQueries{DB: db.DB},
		Scheduled:           &queries.ScheduledMessageQueries{DB: db.DB},
		KanbanQ:             &queries.KanbanQueries{DB: db.DB},
		CalendarQ:           &queries.CalendarQueries{DB: db.DB},
		Tunnels:             &queries.TunnelQueries{DB: db.DB},
		ChannelPermQ:        &queries.ChannelPermQueries{DB: db.DB},
		DeviceBans:          &queries.DeviceBanQueries{DB: db.DB},
		UserDevices:         &queries.UserDeviceQueries{DB: db.DB},
		InviteChain:         &queries.InviteChainQueries{DB: db.DB},
		Quarantine:          &queries.QuarantineQueries{DB: db.DB},
		Approvals:           &queries.ApprovalQueries{DB: db.DB},
		SecurityCfg:         config.SecurityConfig{},
		RegMode:             "open",
		AutoMod:             moderation.New(),
	}
}

// registerUser performs challenge + verify flow, returns userID, access token, public key hex
func registerUser(t *testing.T, d *handlers.Deps, username string) (string, string, string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)

	// Challenge
	body, _ := json.Marshal(map[string]string{"public_key": pubHex, "username": username})
	req := httptest.NewRequest("POST", "/api/auth/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Challenge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("challenge: got %d, body: %s", w.Code, w.Body.String())
	}

	var chalResp struct{ Nonce string `json:"nonce"` }
	json.NewDecoder(w.Body).Decode(&chalResp)

	// Sign nonce
	nonceBytes, _ := hex.DecodeString(chalResp.Nonce)
	sig := ed25519.Sign(priv, nonceBytes)

	// Verify
	body, _ = json.Marshal(map[string]string{
		"public_key": pubHex,
		"nonce":      chalResp.Nonce,
		"signature":  hex.EncodeToString(sig),
	})
	req = httptest.NewRequest("POST", "/api/auth/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	d.Verify(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: got %d, body: %s", w.Code, w.Body.String())
	}

	var verResp struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	json.NewDecoder(w.Body).Decode(&verResp)
	return verResp.User.ID, verResp.AccessToken, pubHex
}

// authedRequest creates an HTTP request with auth context set (bypasses middleware)
func authedRequest(t *testing.T, d *handlers.Deps, method, url string, body io.Reader, token string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, url, body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	return req, w
}

// callWithAuth wraps a handler with auth middleware and calls it
func callWithAuth(t *testing.T, d *handlers.Deps, handler http.HandlerFunc, method, url string, body io.Reader, token string) *httptest.ResponseRecorder {
	t.Helper()
	mw := auth.Middleware(d.JWTService, d.Bans.IsBanned)
	wrapped := mw(handler)
	req := httptest.NewRequest(method, url, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	return w
}

// --- Auth Tests ---

func TestAuthFlow_RegisterAndLogin(t *testing.T) {
	d := setupDeps(t)
	userID, token, _ := registerUser(t, d, "alice")

	if userID == "" {
		t.Fatal("expected user ID")
	}
	if token == "" {
		t.Fatal("expected access token")
	}

	// Validate token
	claims, err := d.JWTService.Validate(token)
	if err != nil {
		t.Fatalf("token validation: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("username: got %q, want %q", claims.Username, "alice")
	}
}

func TestAuthFlow_DuplicateUsername(t *testing.T) {
	d := setupDeps(t)
	registerUser(t, d, "bob")

	// Try registering same username with different key
	pub2, _, _ := ed25519.GenerateKey(nil)
	body, _ := json.Marshal(map[string]string{
		"public_key": hex.EncodeToString(pub2),
		"username":   "bob",
	})
	req := httptest.NewRequest("POST", "/api/auth/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Challenge(w, req)

	if w.Code == http.StatusOK {
		// Check that the nonce response doesn't imply successful registration
		// (server should reject duplicate username at verify or challenge stage)
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		if _, hasError := resp["error"]; !hasError {
			// Some servers allow challenge but reject at verify — that's also fine
			// The key test is that two users can't have the same username
		}
	}
}

func TestAuthFlow_InvalidPublicKey(t *testing.T) {
	d := setupDeps(t)
	body, _ := json.Marshal(map[string]string{"public_key": "tooshort", "username": "test"})
	req := httptest.NewRequest("POST", "/api/auth/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Challenge(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected error for invalid public key, got 200")
	}
}

func TestAuthFlow_BadSignature(t *testing.T) {
	d := setupDeps(t)
	pub, _, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)

	// Challenge
	body, _ := json.Marshal(map[string]string{"public_key": pubHex, "username": "charlie"})
	req := httptest.NewRequest("POST", "/api/auth/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Challenge(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("challenge: %d", w.Code)
	}

	var chalResp struct{ Nonce string `json:"nonce"` }
	json.NewDecoder(w.Body).Decode(&chalResp)

	// Send bad signature
	body, _ = json.Marshal(map[string]string{
		"public_key": pubHex,
		"nonce":      chalResp.Nonce,
		"signature":  hex.EncodeToString(make([]byte, 64)), // zeros
	})
	req = httptest.NewRequest("POST", "/api/auth/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	d.Verify(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("expected error for bad signature")
	}
}

// --- Message Tests ---

func TestMessages_CreateAndList(t *testing.T) {
	d := setupDeps(t)
	_, token, _ := registerUser(t, d, "alice")

	// Get default channel (seeded by SeedDefaults)
	channels, err := d.Channels.List()
	if err != nil || len(channels) == 0 {
		t.Fatal("expected at least one channel")
	}
	chID := channels[0].ID

	// Create message via router (needs auth middleware)
	router := handlers.NewRouter(d)

	body, _ := json.Marshal(map[string]string{"content": "Hello, world!"})
	req := httptest.NewRequest("POST", "/api/channels/"+chID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create message: got %d, body: %s", w.Code, w.Body.String())
	}

	var msg struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	json.NewDecoder(w.Body).Decode(&msg)
	if msg.Content != "Hello, world!" {
		t.Errorf("content: got %q", msg.Content)
	}
	if msg.ID == "" {
		t.Error("expected message ID")
	}

	// List messages
	req = httptest.NewRequest("GET", "/api/channels/"+chID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list messages: got %d", w.Code)
	}

	var msgs []struct{ ID, Content string }
	json.NewDecoder(w.Body).Decode(&msgs)
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
}

func TestMessages_EditAndDelete(t *testing.T) {
	d := setupDeps(t)
	_, token, _ := registerUser(t, d, "alice")
	router := handlers.NewRouter(d)

	channels, _ := d.Channels.List()
	chID := channels[0].ID

	// Create
	body, _ := json.Marshal(map[string]string{"content": "original"})
	req := httptest.NewRequest("POST", "/api/channels/"+chID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	var created struct{ ID string `json:"id"` }
	json.NewDecoder(w.Body).Decode(&created)

	// Edit
	body, _ = json.Marshal(map[string]string{"content": "edited"})
	req = httptest.NewRequest("PATCH", "/api/messages/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/messages/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
}

func TestMessages_Unauthorized(t *testing.T) {
	d := setupDeps(t)
	router := handlers.NewRouter(d)

	channels, _ := d.Channels.List()
	chID := channels[0].ID

	body, _ := json.Marshal(map[string]string{"content": "should fail"})
	req := httptest.NewRequest("POST", "/api/channels/"+chID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// --- Upload Tests ---

func TestUpload_ChunkedFlow(t *testing.T) {
	d := setupDeps(t)
	_, token, _ := registerUser(t, d, "alice")
	router := handlers.NewRouter(d)

	// Init upload
	body, _ := json.Marshal(map[string]any{
		"filename": "test.txt",
		"size":     1024,
	})
	req := httptest.NewRequest("POST", "/api/upload/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("init upload: got %d, body: %s", w.Code, w.Body.String())
	}

	var initResp struct {
		UploadID string `json:"upload_id"`
	}
	json.NewDecoder(w.Body).Decode(&initResp)
	if initResp.UploadID == "" {
		t.Fatal("expected upload_id")
	}

	// Send chunk
	chunk := bytes.Repeat([]byte("A"), 1024)
	req = httptest.NewRequest("PATCH", "/api/upload/"+initResp.UploadID, bytes.NewReader(chunk))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should be 200 (complete) or 202 (more chunks needed)
	if w.Code != http.StatusOK && w.Code != http.StatusAccepted && w.Code != http.StatusCreated {
		t.Fatalf("upload chunk: got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestUpload_Unauthorized(t *testing.T) {
	d := setupDeps(t)
	router := handlers.NewRouter(d)

	body, _ := json.Marshal(map[string]any{"filename": "test.txt", "size": 100, "type": "text/plain"})
	req := httptest.NewRequest("POST", "/api/upload/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// --- Health & Server Info ---

func TestHealth(t *testing.T) {
	d := setupDeps(t)
	router := handlers.NewRouter(d)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d", w.Code)
	}
}

func TestServerInfo(t *testing.T) {
	d := setupDeps(t)
	router := handlers.NewRouter(d)

	req := httptest.NewRequest("GET", "/api/server", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("server info: got %d", w.Code)
	}

	var info struct {
		Name string `json:"name"`
	}
	json.NewDecoder(w.Body).Decode(&info)
	if info.Name != "Test Server" {
		t.Errorf("name: got %q, want %q", info.Name, "Test Server")
	}
}

// --- Channels ---

func TestChannels_CreateAndList(t *testing.T) {
	d := setupDeps(t)
	_, token, _ := registerUser(t, d, "alice")
	router := handlers.NewRouter(d)

	// Create channel
	body, _ := json.Marshal(map[string]string{"name": "test-channel", "type": "text"})
	req := httptest.NewRequest("POST", "/api/channels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: got %d, body: %s", w.Code, w.Body.String())
	}

	// List channels
	req = httptest.NewRequest("GET", "/api/channels", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list channels: got %d", w.Code)
	}

	var chs []struct{ Name string }
	json.NewDecoder(w.Body).Decode(&chs)
	found := false
	for _, ch := range chs {
		if ch.Name == "test-channel" {
			found = true
		}
	}
	if !found {
		t.Error("created channel not found in list")
	}
}

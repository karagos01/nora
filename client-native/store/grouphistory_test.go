package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestHistory() *GroupHistory {
	return &GroupHistory{
		publicKey: "test-key-0123456789abcdef",
		messages:  make(map[string][]StoredGroupMessage),
		keys:      make(map[string]string),
		oldKeys:   make(map[string][]string),
		msgCount:  make(map[string]int),
	}
}

// --- Messages ---

func TestAddAndGetMessages(t *testing.T) {
	h := newTestHistory()

	msg := StoredGroupMessage{
		ID:        "msg1",
		GroupID:   "grp1",
		SenderID:  "user1",
		Content:   "hello",
		CreatedAt: time.Now(),
	}
	h.AddMessage(msg)

	msgs := h.GetMessages("grp1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("content: got %q", msgs[0].Content)
	}
}

func TestAddMessageDedup(t *testing.T) {
	h := newTestHistory()

	msg := StoredGroupMessage{ID: "msg1", GroupID: "grp1", Content: "first"}
	h.AddMessage(msg)
	h.AddMessage(msg) // duplicate
	msg2 := StoredGroupMessage{ID: "msg1", GroupID: "grp1", Content: "second"} // same ID
	h.AddMessage(msg2)

	msgs := h.GetMessages("grp1")
	if len(msgs) != 1 {
		t.Fatalf("dedup failed: expected 1 message, got %d", len(msgs))
	}
}

func TestGetMessagesEmpty(t *testing.T) {
	h := newTestHistory()
	msgs := h.GetMessages("nonexistent")
	if len(msgs) != 0 {
		t.Errorf("expected empty, got %d", len(msgs))
	}
}

func TestDeleteGroup(t *testing.T) {
	h := newTestHistory()
	h.AddMessage(StoredGroupMessage{ID: "m1", GroupID: "grp1", Content: "x"})
	h.SetKey("grp1", "aabbcc")
	h.IncrementCount("grp1")

	h.DeleteGroup("grp1")

	if len(h.GetMessages("grp1")) != 0 {
		t.Error("messages should be deleted")
	}
	if h.GetKey("grp1") != "" {
		t.Error("key should be deleted")
	}
	if h.NeedsRotation("grp1") {
		t.Error("counter should be reset")
	}
}

// --- Keys ---

func TestSetGetKey(t *testing.T) {
	h := newTestHistory()
	h.SetKey("grp1", "abcdef0123456789")

	if got := h.GetKey("grp1"); got != "abcdef0123456789" {
		t.Errorf("got %q", got)
	}
	if got := h.GetKey("nonexistent"); got != "" {
		t.Errorf("nonexistent should be empty, got %q", got)
	}
}

// --- Rotation ---

func TestRotateKey(t *testing.T) {
	h := newTestHistory()
	h.SetKey("grp1", "key-old")

	h.RotateKey("grp1", "key-new")

	if got := h.GetKey("grp1"); got != "key-new" {
		t.Errorf("current key: got %q, want key-new", got)
	}

	allKeys := h.GetAllKeys("grp1")
	if len(allKeys) != 2 {
		t.Fatalf("expected 2 keys (current + old), got %d", len(allKeys))
	}
	if allKeys[0] != "key-new" || allKeys[1] != "key-old" {
		t.Errorf("keys: %v", allKeys)
	}
}

func TestRotateKeyMaxHistory(t *testing.T) {
	h := newTestHistory()
	h.SetKey("grp1", "key-0")

	// 12 rotations — history should have max 10 old keys
	for i := 1; i <= 12; i++ {
		h.RotateKey("grp1", "key-"+string(rune('a'+i-1)))
	}

	allKeys := h.GetAllKeys("grp1")
	// 1 current + max 10 old = max 11
	if len(allKeys) > 11 {
		t.Errorf("too many keys: %d (max 11)", len(allKeys))
	}
}

func TestRotateKeyResetsCounter(t *testing.T) {
	h := newTestHistory()
	h.SetKey("grp1", "old")

	for i := 0; i < 100; i++ {
		h.IncrementCount("grp1")
	}

	h.RotateKey("grp1", "new")

	if h.NeedsRotation("grp1") {
		t.Error("counter should be reset after rotation")
	}
}

// --- Counter ---

func TestIncrementCount(t *testing.T) {
	h := newTestHistory()

	for i := 1; i <= 5; i++ {
		got := h.IncrementCount("grp1")
		if got != i {
			t.Errorf("increment %d: got %d", i, got)
		}
	}
}

func TestNeedsRotation(t *testing.T) {
	h := newTestHistory()

	// Below threshold
	for i := 0; i < keyRotationThreshold-1; i++ {
		h.IncrementCount("grp1")
	}
	if h.NeedsRotation("grp1") {
		t.Error("should not need rotation before threshold")
	}

	// At threshold
	h.IncrementCount("grp1")
	if !h.NeedsRotation("grp1") {
		t.Error("should need rotation at threshold")
	}
}

// --- GetAllKeys ---

func TestGetAllKeysNoKey(t *testing.T) {
	h := newTestHistory()
	keys := h.GetAllKeys("grp1")
	if len(keys) != 0 {
		t.Errorf("expected empty, got %v", keys)
	}
}

func TestGetAllKeysOnlyCurrent(t *testing.T) {
	h := newTestHistory()
	h.SetKey("grp1", "current")

	keys := h.GetAllKeys("grp1")
	if len(keys) != 1 || keys[0] != "current" {
		t.Errorf("expected [current], got %v", keys)
	}
}

func TestGetAllKeysWithHistory(t *testing.T) {
	h := newTestHistory()
	h.SetKey("grp1", "key1")
	h.RotateKey("grp1", "key2")
	h.RotateKey("grp1", "key3")

	keys := h.GetAllKeys("grp1")
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
	}
	// Order: current, then old (newest first)
	if keys[0] != "key3" || keys[1] != "key2" || keys[2] != "key1" {
		t.Errorf("wrong key order: %v", keys)
	}
}

// --- Encryption ---

func TestEncryptedSaveLoad(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	// Use a real ed25519 seed (32 bytes hex = 64 hex chars)
	seedHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	pubKey := "test-enc-key-0123456789abcdef"

	// Create and populate encrypted history
	h := NewGroupHistory(pubKey, seedHex)
	h.AddMessage(StoredGroupMessage{ID: "m1", GroupID: "g1", Content: "secret message", CreatedAt: time.Now()})
	h.SetKey("g1", "groupkey123")
	h.Save()

	// Verify file on disk is NOT valid JSON (it's encrypted binary)
	data, err := os.ReadFile(h.historyPath())
	if err != nil {
		t.Fatal(err)
	}
	var test map[string]any
	if json.Unmarshal(data, &test) == nil {
		t.Error("encrypted file should not be valid JSON")
	}

	// Load into a new history instance — should decrypt
	h2 := NewGroupHistory(pubKey, seedHex)
	msgs := h2.GetMessages("g1")
	if len(msgs) != 1 || msgs[0].Content != "secret message" {
		t.Errorf("decrypted messages: got %v", msgs)
	}
	if h2.GetKey("g1") != "groupkey123" {
		t.Errorf("decrypted key: got %q", h2.GetKey("g1"))
	}
}

func TestPlaintextMigration(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	seedHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	pubKey := "test-mig-key-0123456789abcdef"

	// Write a plaintext JSON file manually
	noraPath := filepath.Join(tmp, ".nora")
	os.MkdirAll(noraPath, 0700)

	plainData := groupHistoryData{
		Messages: map[string][]StoredGroupMessage{
			"g1": {{ID: "m1", GroupID: "g1", Content: "old plain"}},
		},
		Keys:    map[string]string{"g1": "oldkey"},
		OldKeys: map[string][]string{},
	}
	data, _ := json.Marshal(plainData)
	short := pubKey[:16]
	os.WriteFile(filepath.Join(noraPath, "group_history_"+short+".json"), data, 0600)

	// Load with encryption key — should read plaintext and mark dirty
	h := NewGroupHistory(pubKey, seedHex)
	msgs := h.GetMessages("g1")
	if len(msgs) != 1 || msgs[0].Content != "old plain" {
		t.Errorf("migration failed: got %v", msgs)
	}

	// Save — should write encrypted
	h.Save()

	// Verify file is now encrypted (not JSON)
	encData, _ := os.ReadFile(h.historyPath())
	var test map[string]any
	if json.Unmarshal(encData, &test) == nil {
		t.Error("migrated file should be encrypted, not JSON")
	}

	// Reload — should still work
	h2 := NewGroupHistory(pubKey, seedHex)
	msgs2 := h2.GetMessages("g1")
	if len(msgs2) != 1 || msgs2[0].Content != "old plain" {
		t.Errorf("post-migration load failed: got %v", msgs2)
	}
}

// --- Dirty flag ---

func TestDirtyFlag(t *testing.T) {
	h := newTestHistory()
	if h.dirty {
		t.Error("fresh history should not be dirty")
	}

	h.SetKey("grp1", "x")
	if !h.dirty {
		t.Error("SetKey should mark dirty")
	}

	// Reset
	h.dirty = false
	h.AddMessage(StoredGroupMessage{ID: "m1", GroupID: "grp1"})
	if !h.dirty {
		t.Error("AddMessage should mark dirty")
	}

	h.dirty = false
	h.IncrementCount("grp1")
	if !h.dirty {
		t.Error("IncrementCount should mark dirty")
	}

	h.dirty = false
	h.DeleteGroup("grp1")
	if !h.dirty {
		t.Error("DeleteGroup should mark dirty")
	}

	h.dirty = false
	h.RotateKey("grp1", "new")
	if !h.dirty {
		t.Error("RotateKey should mark dirty")
	}
}

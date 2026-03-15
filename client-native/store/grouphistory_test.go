package store

import (
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

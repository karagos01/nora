package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanDiskUsage(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	noraDir := filepath.Join(tmp, ".nora")
	os.MkdirAll(filepath.Join(noraDir, "cache", "server1"), 0700)
	os.MkdirAll(filepath.Join(noraDir, "sounds"), 0700)

	// Fiktivní soubory
	os.WriteFile(filepath.Join(noraDir, "identities.json"), make([]byte, 100), 0600)
	os.WriteFile(filepath.Join(noraDir, "dm_history_abcd1234.json"), make([]byte, 500), 0600)
	os.WriteFile(filepath.Join(noraDir, "group_history_abcd1234.json"), make([]byte, 300), 0600)
	os.WriteFile(filepath.Join(noraDir, "cache", "server1", "file1.dat"), make([]byte, 1000), 0600)
	os.WriteFile(filepath.Join(noraDir, "sounds", "notif.wav"), make([]byte, 200), 0600)

	u := ScanDiskUsage()

	if u.DMHistory != 500 {
		t.Errorf("DMHistory = %d, want 500", u.DMHistory)
	}
	if u.GroupHistory != 300 {
		t.Errorf("GroupHistory = %d, want 300", u.GroupHistory)
	}
	if u.Cache != 1000 {
		t.Errorf("Cache = %d, want 1000", u.Cache)
	}
	if u.Sounds != 200 {
		t.Errorf("Sounds = %d, want 200", u.Sounds)
	}
	if u.Other != 100 {
		t.Errorf("Other = %d, want 100", u.Other)
	}
	if u.Total != 2100 {
		t.Errorf("Total = %d, want 2100", u.Total)
	}
}

func TestCleanupCache(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	cacheDir := filepath.Join(tmp, ".nora", "cache")
	os.MkdirAll(cacheDir, 0700)

	// Vytvořit 3 soubory s různým ModTime (oldest → newest)
	files := []struct {
		name string
		size int
		age  time.Duration
	}{
		{"old.dat", 400, 3 * time.Hour},
		{"mid.dat", 300, 2 * time.Hour},
		{"new.dat", 300, 1 * time.Hour},
	}

	now := time.Now()
	for _, f := range files {
		path := filepath.Join(cacheDir, f.name)
		os.WriteFile(path, make([]byte, f.size), 0600)
		os.Chtimes(path, now.Add(-f.age), now.Add(-f.age))
	}

	// Celkem 1000B, limit 600B → měl by smazat old.dat (400B) = freed 400B
	freed, err := CleanupCache(600)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 400 {
		t.Errorf("freed = %d, want 400", freed)
	}

	// old.dat by neměl existovat
	if _, err := os.Stat(filepath.Join(cacheDir, "old.dat")); !os.IsNotExist(err) {
		t.Error("old.dat should be deleted")
	}
	// mid.dat a new.dat by měly zůstat
	if _, err := os.Stat(filepath.Join(cacheDir, "mid.dat")); err != nil {
		t.Error("mid.dat should exist")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "new.dat")); err != nil {
		t.Error("new.dat should exist")
	}
}

func TestCleanupCacheAll(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	cacheDir := filepath.Join(tmp, ".nora", "cache", "sub")
	os.MkdirAll(cacheDir, 0700)
	os.WriteFile(filepath.Join(cacheDir, "file.dat"), []byte("data"), 0600)

	if err := CleanupCacheAll(); err != nil {
		t.Fatal(err)
	}

	// cache adresář by měl existovat, ale být prázdný
	parent := filepath.Join(tmp, ".nora", "cache")
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal("cache dir should exist after cleanup")
	}
	if len(entries) != 0 {
		t.Errorf("cache dir should be empty, got %d entries", len(entries))
	}
}

func TestDMDeleteOlderThan(t *testing.T) {
	h := &DMHistory{
		publicKey: "test",
		messages:  make(map[string][]StoredDMMessage),
	}

	now := time.Now()
	h.messages["conv1"] = []StoredDMMessage{
		{ID: "1", ConversationID: "conv1", Content: "old", CreatedAt: now.Add(-48 * time.Hour)},
		{ID: "2", ConversationID: "conv1", Content: "new", CreatedAt: now.Add(-1 * time.Hour)},
	}
	h.messages["conv2"] = []StoredDMMessage{
		{ID: "3", ConversationID: "conv2", Content: "very old", CreatedAt: now.Add(-72 * time.Hour)},
	}

	deleted := h.DeleteOlderThan(24 * time.Hour)
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	// conv1 by měla mít jen 1 zprávu
	if len(h.messages["conv1"]) != 1 {
		t.Errorf("conv1 messages = %d, want 1", len(h.messages["conv1"]))
	}

	// conv2 by měla být smazána (prázdná)
	if _, ok := h.messages["conv2"]; ok {
		t.Error("conv2 should be deleted (empty)")
	}

	if !h.dirty {
		t.Error("dirty should be true")
	}
}

func TestGroupDeleteOlderThanPreservesKeys(t *testing.T) {
	h := &GroupHistory{
		publicKey: "test",
		messages:  make(map[string][]StoredGroupMessage),
		keys:      make(map[string]string),
		oldKeys:   make(map[string][]string),
		msgCount:  make(map[string]int),
	}

	now := time.Now()
	h.messages["g1"] = []StoredGroupMessage{
		{ID: "1", GroupID: "g1", Content: "old", CreatedAt: now.Add(-48 * time.Hour)},
	}
	h.keys["g1"] = "abc123"
	h.oldKeys["g1"] = []string{"old1", "old2"}
	h.msgCount["g1"] = 42

	deleted := h.DeleteOlderThan(24 * time.Hour)
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// Zprávy smazané, ale klíče zachovány
	if h.messages["g1"] != nil && len(h.messages["g1"]) > 0 {
		t.Error("messages should be nil/empty")
	}
	if h.keys["g1"] != "abc123" {
		t.Errorf("key should be preserved, got %q", h.keys["g1"])
	}
	if len(h.oldKeys["g1"]) != 2 {
		t.Errorf("oldKeys should be preserved, got %d", len(h.oldKeys["g1"]))
	}
	if h.msgCount["g1"] != 42 {
		t.Errorf("msgCount should be preserved, got %d", h.msgCount["g1"])
	}
}

func TestMessageCount(t *testing.T) {
	dm := &DMHistory{
		publicKey: "test",
		messages: map[string][]StoredDMMessage{
			"c1": {{ID: "1"}, {ID: "2"}},
			"c2": {{ID: "3"}},
		},
	}
	if c := dm.MessageCount(); c != 3 {
		t.Errorf("DM MessageCount = %d, want 3", c)
	}

	gh := &GroupHistory{
		publicKey: "test",
		messages: map[string][]StoredGroupMessage{
			"g1": {{ID: "1"}, {ID: "2"}, {ID: "3"}},
		},
		keys:    make(map[string]string),
		oldKeys: make(map[string][]string),
		msgCount: make(map[string]int),
	}
	if c := gh.MessageCount(); c != 3 {
		t.Errorf("Group MessageCount = %d, want 3", c)
	}
}

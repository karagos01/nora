package gameserver

import (
	"os"
	"path/filepath"
	"testing"
)

// --- SafePath ---

func TestSafePathValid(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	path, err := m.SafePath(gsID, "server.toml")
	if err != nil {
		t.Fatalf("SafePath: %v", err)
	}
	expected := filepath.Join(dir, gsID, "server.toml")
	absExpected, _ := filepath.Abs(expected)
	if path != absExpected {
		t.Errorf("got %q, want %q", path, absExpected)
	}
}

func TestSafePathRoot(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	// Prázdná cesta = kořen serveru — povoleno
	path, err := m.SafePath(gsID, "")
	if err != nil {
		t.Fatalf("SafePath root: %v", err)
	}
	absBase, _ := filepath.Abs(filepath.Join(dir, gsID))
	if path != absBase {
		t.Errorf("got %q, want %q", path, absBase)
	}
}

func TestSafePathTraversal(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	_, err := m.SafePath(gsID, "../../../etc/passwd")
	if err == nil {
		t.Fatal("path traversal should be denied")
	}
}

func TestSafePathTraversalDotDot(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	_, err := m.SafePath(gsID, "..")
	if err == nil {
		t.Fatal(".. should be denied")
	}
}

func TestSafePathSubdir(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID, "config"), 0755)

	path, err := m.SafePath(gsID, "config/settings.yml")
	if err != nil {
		t.Fatalf("SafePath subdir: %v", err)
	}
	expected := filepath.Join(dir, gsID, "config", "settings.yml")
	absExpected, _ := filepath.Abs(expected)
	if path != absExpected {
		t.Errorf("got %q, want %q", path, absExpected)
	}
}

// --- ListFiles ---

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	gsDir := filepath.Join(dir, gsID)
	os.MkdirAll(gsDir, 0755)
	os.WriteFile(filepath.Join(gsDir, "server.toml"), []byte("test"), 0644)
	os.MkdirAll(filepath.Join(gsDir, "data"), 0755)

	entries, err := m.ListFiles(gsID, "")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Najít server.toml a data/
	var foundFile, foundDir bool
	for _, e := range entries {
		if e.Name == "server.toml" && !e.IsDir {
			foundFile = true
			if e.Size != 4 {
				t.Errorf("server.toml size: got %d, want 4", e.Size)
			}
		}
		if e.Name == "data" && e.IsDir {
			foundDir = true
		}
	}
	if !foundFile {
		t.Error("server.toml not found")
	}
	if !foundDir {
		t.Error("data/ not found")
	}
}

func TestListFilesTraversal(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	_, err := m.ListFiles(gsID, "../../")
	if err == nil {
		t.Fatal("should deny path traversal")
	}
}

// --- ReadFile / WriteFile ---

func TestReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	content := "hello world"
	if err := m.WriteFile(gsID, "test.txt", content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := m.ReadFile(gsID, "test.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestReadFileDirectory(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID, "subdir"), 0755)

	_, err := m.ReadFile(gsID, "subdir")
	if err == nil {
		t.Fatal("reading directory should fail")
	}
}

func TestReadFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	// 1MB + 1 byte
	data := make([]byte, 1<<20+1)
	os.WriteFile(filepath.Join(dir, gsID, "big.bin"), data, 0644)

	_, err := m.ReadFile(gsID, "big.bin")
	if err == nil {
		t.Fatal("should reject files > 1MB")
	}
}

func TestWriteFileCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	// Zápis do neexistujícího podadresáře
	if err := m.WriteFile(gsID, "config/deep/file.txt", "data"); err != nil {
		t.Fatalf("WriteFile nested: %v", err)
	}

	got, err := m.ReadFile(gsID, "config/deep/file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != "data" {
		t.Errorf("got %q", got)
	}
}

// --- DeleteFile ---

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)
	os.WriteFile(filepath.Join(dir, gsID, "removeme.txt"), []byte("bye"), 0644)

	if err := m.DeleteFile(gsID, "removeme.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, gsID, "removeme.txt")); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestDeleteFileRoot(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	// Nelze smazat kořen serveru
	if err := m.DeleteFile(gsID, ""); err == nil {
		t.Fatal("should deny deleting root")
	}
	if err := m.DeleteFile(gsID, "."); err == nil {
		t.Fatal("should deny deleting root (.)")
	}
	if err := m.DeleteFile(gsID, "/"); err == nil {
		t.Fatal("should deny deleting root (/)")
	}
}

func TestDeleteDirectory(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	subdir := filepath.Join(dir, gsID, "logs")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "latest.log"), []byte("log"), 0644)

	if err := m.DeleteFile(gsID, "logs"); err != nil {
		t.Fatalf("DeleteFile dir: %v", err)
	}

	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Fatal("directory should be deleted")
	}
}

// --- Mkdir ---

func TestMkdir(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	if err := m.Mkdir(gsID, "plugins/mods"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, gsID, "plugins", "mods"))
	if err != nil {
		t.Fatal("directory should exist")
	}
	if !info.IsDir() {
		t.Fatal("should be a directory")
	}
}

func TestMkdirTraversal(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{DataDir: dir}
	gsID := "test-gs"
	os.MkdirAll(filepath.Join(dir, gsID), 0755)

	if err := m.Mkdir(gsID, "../../evil"); err == nil {
		t.Fatal("should deny path traversal in mkdir")
	}
}

// --- CreateServerDir / DeleteServerDir ---

func TestCreateDeleteServerDir(t *testing.T) {
	dir := t.TempDir()
	presetsDir := filepath.Join(dir, "presets")
	SeedPresets(presetsDir)
	m := &Manager{DataDir: dir, PresetsDir: presetsDir}

	gsID := "new-server"
	if err := m.CreateServerDir(gsID, "minecraft"); err != nil {
		t.Fatalf("CreateServerDir: %v", err)
	}

	// server.toml by měl existovat
	tomlPath := filepath.Join(dir, gsID, "server.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatal("server.toml should exist")
	}

	// Config by měl být validní
	cfg, err := ReadConfig(dir, gsID)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Image == "" {
		t.Error("image should not be empty")
	}

	// Smazat
	if err := m.DeleteServerDir(gsID); err != nil {
		t.Fatalf("DeleteServerDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, gsID)); !os.IsNotExist(err) {
		t.Fatal("server dir should be deleted")
	}
}

func TestCreateServerDirUnknownPreset(t *testing.T) {
	dir := t.TempDir()
	presetsDir := filepath.Join(dir, "presets")
	SeedPresets(presetsDir)
	m := &Manager{DataDir: dir, PresetsDir: presetsDir}

	// Neznámý preset — fallback na první dostupný
	if err := m.CreateServerDir("fallback-test", "nonexistent"); err != nil {
		t.Fatalf("CreateServerDir with unknown preset: %v", err)
	}

	tomlPath := filepath.Join(dir, "fallback-test", "server.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatal("server.toml should exist even with unknown preset")
	}
}

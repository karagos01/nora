package gameserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadConfig(t *testing.T) {
	dir := t.TempDir()
	gsID := "test-server"
	gsDir := filepath.Join(dir, gsID)
	os.MkdirAll(gsDir, 0755)

	content := `
image = "itzg/minecraft-server:latest"
memory = "4096m"
restart = "always"
stop_timeout = 30
console_command = "rcon-cli"

[ports]
"25565/tcp" = 25565

[env]
EULA = "TRUE"
TYPE = "PAPER"

[volumes]
"/data" = "./data"
`
	os.WriteFile(filepath.Join(gsDir, "server.toml"), []byte(content), 0644)

	cfg, err := ReadConfig(dir, gsID)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	if cfg.Image != "itzg/minecraft-server:latest" {
		t.Errorf("Image: got %q", cfg.Image)
	}
	if cfg.Memory != "4096m" {
		t.Errorf("Memory: got %q", cfg.Memory)
	}
	if cfg.Restart != "always" {
		t.Errorf("Restart: got %q", cfg.Restart)
	}
	if cfg.StopTimeout != 30 {
		t.Errorf("StopTimeout: got %d", cfg.StopTimeout)
	}
	if cfg.ConsoleCommand != "rcon-cli" {
		t.Errorf("ConsoleCommand: got %q", cfg.ConsoleCommand)
	}
	if cfg.Ports["25565/tcp"] != 25565 {
		t.Errorf("Ports: %v", cfg.Ports)
	}
	if cfg.Env["EULA"] != "TRUE" || cfg.Env["TYPE"] != "PAPER" {
		t.Errorf("Env: %v", cfg.Env)
	}
	if cfg.Volumes["/data"] != "./data" {
		t.Errorf("Volumes: %v", cfg.Volumes)
	}
}

func TestReadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	gsID := "minimal"
	gsDir := filepath.Join(dir, gsID)
	os.MkdirAll(gsDir, 0755)

	// Minimální config — jen image
	content := `image = "nginx:latest"`
	os.WriteFile(filepath.Join(gsDir, "server.toml"), []byte(content), 0644)

	cfg, err := ReadConfig(dir, gsID)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	if cfg.Memory != "2048m" {
		t.Errorf("default Memory: got %q, want 2048m", cfg.Memory)
	}
	if cfg.Restart != "unless-stopped" {
		t.Errorf("default Restart: got %q, want unless-stopped", cfg.Restart)
	}
	if cfg.StopTimeout != 10 {
		t.Errorf("default StopTimeout: got %d, want 10", cfg.StopTimeout)
	}
}

func TestReadConfigMissing(t *testing.T) {
	_, err := ReadConfig("/tmp/nonexistent", "no-such-server")
	if err == nil {
		t.Fatal("should fail for missing config")
	}
}

func TestReadConfigInvalid(t *testing.T) {
	dir := t.TempDir()
	gsID := "bad"
	gsDir := filepath.Join(dir, gsID)
	os.MkdirAll(gsDir, 0755)
	os.WriteFile(filepath.Join(gsDir, "server.toml"), []byte("[[[bad"), 0644)

	_, err := ReadConfig(dir, gsID)
	if err == nil {
		t.Fatal("should fail on invalid TOML")
	}
}

func TestReadConfigRaw(t *testing.T) {
	dir := t.TempDir()
	gsID := "raw-test"
	gsDir := filepath.Join(dir, gsID)
	os.MkdirAll(gsDir, 0755)

	content := `image = "test:latest"`
	os.WriteFile(filepath.Join(gsDir, "server.toml"), []byte(content), 0644)

	raw, err := ReadConfigRaw(dir, gsID)
	if err != nil {
		t.Fatalf("ReadConfigRaw: %v", err)
	}
	if raw != content {
		t.Errorf("got %q, want %q", raw, content)
	}
}

func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	gsID := "write-test"
	gsDir := filepath.Join(dir, gsID)
	os.MkdirAll(gsDir, 0755)

	content := `image = "new:latest"`
	if err := WriteConfig(dir, gsID, content); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(gsDir, "server.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("got %q, want %q", string(data), content)
	}
}

func TestBuildDockerArgs(t *testing.T) {
	cfg := &ServerConfig{
		Image:   "itzg/minecraft-server:latest",
		Memory:  "2048m",
		Restart: "unless-stopped",
		Ports:   map[string]int{"25565/tcp": 25565},
		Env:     map[string]string{"EULA": "TRUE"},
		Volumes: map[string]string{"/data": "./data"},
	}

	args := cfg.BuildDockerArgs("mc-test", "/opt/gs/test")
	joined := strings.Join(args, " ")

	// Základní argumenty
	if !strings.Contains(joined, "run -d --name mc-test") {
		t.Errorf("missing run/name: %s", joined)
	}
	if !strings.Contains(joined, "--memory 2048m") {
		t.Errorf("missing memory: %s", joined)
	}
	if !strings.Contains(joined, "--restart unless-stopped") {
		t.Errorf("missing restart: %s", joined)
	}

	// Port mapping
	if !strings.Contains(joined, "-p 25565:25565/tcp") {
		t.Errorf("missing port: %s", joined)
	}

	// Env
	if !strings.Contains(joined, "-e EULA=TRUE") {
		t.Errorf("missing env: %s", joined)
	}

	// Image na konci
	if args[len(args)-1] != "itzg/minecraft-server:latest" {
		t.Errorf("last arg should be image, got %q", args[len(args)-1])
	}
}

func TestBuildDockerArgsAbsoluteVolume(t *testing.T) {
	cfg := &ServerConfig{
		Image:   "test:latest",
		Memory:  "512m",
		Restart: "no",
		Volumes: map[string]string{"/data": "/absolute/path"},
	}

	args := cfg.BuildDockerArgs("test", "/opt/gs/test")
	joined := strings.Join(args, " ")

	// Absolutní cesta by měla zůstat absolutní
	if !strings.Contains(joined, "-v /absolute/path:/data") {
		t.Errorf("absolute volume path: %s", joined)
	}
}

func TestBuildDockerArgsUDPPort(t *testing.T) {
	cfg := &ServerConfig{
		Image:   "test:latest",
		Memory:  "512m",
		Restart: "no",
		Ports:   map[string]int{"27015/udp": 27015},
	}

	args := cfg.BuildDockerArgs("test", "/opt")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-p 27015:27015/udp") {
		t.Errorf("UDP port: %s", joined)
	}
}

func TestSeedPresets(t *testing.T) {
	dir := t.TempDir()
	presetsDir := filepath.Join(dir, "presets")

	if err := SeedPresets(presetsDir); err != nil {
		t.Fatalf("SeedPresets: %v", err)
	}

	// Ověřit že se vytvořily soubory
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 preset files, got %d", len(entries))
	}

	// Druhé volání by nemělo nic přepsat (adresář není prázdný)
	if err := SeedPresets(presetsDir); err != nil {
		t.Fatalf("SeedPresets second call: %v", err)
	}
}

func TestListPresets(t *testing.T) {
	dir := t.TempDir()
	presetsDir := filepath.Join(dir, "presets")
	SeedPresets(presetsDir)

	presets := ListPresets(presetsDir)
	if len(presets) != 5 {
		t.Fatalf("expected 5 presets, got %d", len(presets))
	}

	// Ověřit že minecraft je v seznamu
	found := false
	for _, p := range presets {
		if p.Name == "minecraft" {
			found = true
		}
	}
	if !found {
		t.Error("minecraft preset missing from list")
	}
}

func TestReadPresetMinecraft(t *testing.T) {
	dir := t.TempDir()
	presetsDir := filepath.Join(dir, "presets")
	SeedPresets(presetsDir)

	content, err := ReadPreset(presetsDir, "minecraft")
	if err != nil {
		t.Fatalf("ReadPreset: %v", err)
	}
	if !strings.Contains(content, "itzg/minecraft-server") {
		t.Error("minecraft preset should reference itzg/minecraft-server image")
	}
	if !strings.Contains(content, "25565") {
		t.Error("minecraft preset should contain port 25565")
	}
}

func TestReadPresetMissing(t *testing.T) {
	_, err := ReadPreset("/tmp/nonexistent", "nosuch")
	if err == nil {
		t.Fatal("should fail for missing preset")
	}
}

func TestListPresetsEmptyDir(t *testing.T) {
	presets := ListPresets("/tmp/nonexistent-presets-dir")
	if presets != nil {
		t.Errorf("should return nil for nonexistent dir, got %v", presets)
	}
}

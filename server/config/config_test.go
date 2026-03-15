package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Neexistující soubor → vrátí defaults
	cfg, err := Load("/tmp/nonexistent-nora-config-test.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port: got %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host: got %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Database.Path != "data/nora.db" {
		t.Errorf("Database.Path: got %q", cfg.Database.Path)
	}
	if cfg.Auth.AccessTokenTTL.Duration != 15*time.Minute {
		t.Errorf("AccessTokenTTL: got %v", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL.Duration != 7*24*time.Hour {
		t.Errorf("RefreshTokenTTL: got %v", cfg.Auth.RefreshTokenTTL)
	}
	if cfg.RateLimit.Burst != 30 {
		t.Errorf("RateLimit.Burst: got %d, want 30", cfg.RateLimit.Burst)
	}
	if !cfg.GameServers.Enabled {
		t.Error("GameServers should be enabled by default")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")

	content := `
[server]
host = "127.0.0.1"
port = 9021
name = "Test Server"

[database]
path = "/tmp/test.db"

[auth]
jwt_secret = "my-secret"
access_token_ttl = "30m"
refresh_token_ttl = "24h"
challenge_ttl = "10m"

[ratelimit]
requests_per_second = 50.0
burst = 100

[registration]
open = true
`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Host: got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 9021 {
		t.Errorf("Port: got %d, want 9021", cfg.Server.Port)
	}
	if cfg.Server.Name != "Test Server" {
		t.Errorf("Name: got %q", cfg.Server.Name)
	}
	if cfg.Database.Path != "/tmp/test.db" {
		t.Errorf("DB Path: got %q", cfg.Database.Path)
	}
	if cfg.Auth.JWTSecret != "my-secret" {
		t.Errorf("JWTSecret: got %q", cfg.Auth.JWTSecret)
	}
	if cfg.Auth.AccessTokenTTL.Duration != 30*time.Minute {
		t.Errorf("AccessTokenTTL: got %v, want 30m", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL.Duration != 24*time.Hour {
		t.Errorf("RefreshTokenTTL: got %v, want 24h", cfg.Auth.RefreshTokenTTL)
	}
	if cfg.RateLimit.Burst != 100 {
		t.Errorf("Burst: got %d, want 100", cfg.RateLimit.Burst)
	}
	if !cfg.Registration.Open {
		t.Error("Registration.Open should be true")
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")

	if err := os.WriteFile(path, []byte("[[[invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("should fail on invalid TOML")
	}
}

func TestDurationUnmarshal(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("5m30s")); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}
	if d.Duration != 5*time.Minute+30*time.Second {
		t.Errorf("got %v, want 5m30s", d.Duration)
	}
}

func TestDurationUnmarshalInvalid(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("not-a-duration")); err == nil {
		t.Fatal("should fail on invalid duration")
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.toml")

	// Jen server port — zbytek by měly být defaults
	content := `
[server]
port = 3000
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 3000 {
		t.Errorf("Port: got %d, want 3000", cfg.Server.Port)
	}
	// Default host by měl zůstat
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Host should keep default, got %q", cfg.Server.Host)
	}
}

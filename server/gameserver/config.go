package gameserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// allowedRestartPolicies are the valid Docker restart policies.
var allowedRestartPolicies = map[string]bool{
	"no":             true,
	"always":         true,
	"unless-stopped": true,
	"on-failure":     true,
}

// allowedConsoleCommands are the whitelisted console commands for docker exec.
var allowedConsoleCommands = map[string]bool{
	"rcon-cli": true,
	"mc":       true,
	"":         true, // empty = disabled
}

// imageRe validates Docker image names (alphanum, dash, underscore, dot, slash, colon).
var imageRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._\-/:]+(:[a-zA-Z0-9._\-]+)?$`)

type ServerConfig struct {
	Image          string            `toml:"image"`
	Memory         string            `toml:"memory"`
	Restart        string            `toml:"restart"`
	StopTimeout    int               `toml:"stop_timeout"`
	ConsoleCommand string            `toml:"console_command"`
	RCONPort       int               `toml:"rcon_port"`
	RCONPassword   string            `toml:"rcon_password"`
	Ports          map[string]int    `toml:"ports"`
	Env            map[string]string `toml:"env"`
	Volumes        map[string]string `toml:"volumes"`
}

// ReadConfig reads and parses server.toml for a given game server
func ReadConfig(dataDir, gsID string) (*ServerConfig, error) {
	path := filepath.Join(dataDir, gsID, "server.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ServerConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Defaults
	if cfg.Memory == "" {
		cfg.Memory = "2048m"
	}
	if cfg.Restart == "" {
		cfg.Restart = "unless-stopped"
	}
	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = 10
	}

	return &cfg, nil
}

// ReadConfigRaw returns the raw content of server.toml
func ReadConfigRaw(dataDir, gsID string) (string, error) {
	path := filepath.Join(dataDir, gsID, "server.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteConfig writes content to server.toml
func WriteConfig(dataDir, gsID, content string) error {
	path := filepath.Join(dataDir, gsID, "server.toml")
	return os.WriteFile(path, []byte(content), 0644)
}

// ValidateConfig checks the server config for dangerous or invalid values.
func (c *ServerConfig) ValidateConfig() error {
	// Validate image name
	if c.Image != "" && !imageRe.MatchString(c.Image) {
		return fmt.Errorf("invalid image name: %q", c.Image)
	}

	// Validate restart policy
	restart := c.Restart
	if restart == "" {
		restart = "unless-stopped"
	}
	if !allowedRestartPolicies[restart] {
		return fmt.Errorf("invalid restart policy: %q", restart)
	}

	// Validate console command
	if c.ConsoleCommand != "" && !allowedConsoleCommands[c.ConsoleCommand] {
		return fmt.Errorf("disallowed console_command: %q (allowed: rcon-cli, mc)", c.ConsoleCommand)
	}

	// Validate volumes — only relative paths allowed for host side
	for containerPath, hostPath := range c.Volumes {
		if filepath.IsAbs(hostPath) {
			return fmt.Errorf("absolute host path not allowed in volumes: %q", hostPath)
		}
		if strings.Contains(hostPath, "..") {
			return fmt.Errorf("path traversal not allowed in volumes: %q", hostPath)
		}
		if filepath.IsAbs(containerPath) && strings.Contains(containerPath, "..") {
			return fmt.Errorf("invalid container path in volumes: %q", containerPath)
		}
	}

	// Validate env keys — no shell metacharacters
	for k := range c.Env {
		for _, r := range k {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
				return fmt.Errorf("invalid env key: %q (only alphanumeric and underscore allowed)", k)
			}
		}
	}

	return nil
}

// BuildDockerArgs builds docker run arguments from config.
// ValidateConfig MUST be called before this method.
func (c *ServerConfig) BuildDockerArgs(containerName, gsDataDir string) []string {
	args := []string{"run", "-d", "--name", containerName}
	args = append(args, "--memory", c.Memory)
	args = append(args, "--restart", c.Restart)

	// Security hardening
	args = append(args, "--no-new-privileges")
	args = append(args, "--cap-drop", "ALL")

	// Port mapping
	for spec, hostPort := range c.Ports {
		// spec = "25565/tcp", hostPort = 25565
		parts := strings.SplitN(spec, "/", 2)
		containerPort := parts[0]
		proto := "tcp"
		if len(parts) == 2 {
			proto = parts[1]
		}
		args = append(args, "-p", fmt.Sprintf("%d:%s/%s", hostPort, containerPort, proto))
	}

	// Volumes — only relative paths (validated by ValidateConfig)
	for containerPath, hostPath := range c.Volumes {
		resolved := filepath.Join(gsDataDir, hostPath)
		os.MkdirAll(resolved, 0755)
		args = append(args, "-v", resolved+":"+containerPath)
	}

	// Env vars
	for k, v := range c.Env {
		args = append(args, "-e", k+"="+v)
	}

	args = append(args, c.Image)
	return args
}

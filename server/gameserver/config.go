package gameserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

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

// ReadConfig načte a parsuje server.toml pro daný game server
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

// ReadConfigRaw vrátí surový obsah server.toml
func ReadConfigRaw(dataDir, gsID string) (string, error) {
	path := filepath.Join(dataDir, gsID, "server.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteConfig zapíše obsah do server.toml
func WriteConfig(dataDir, gsID, content string) error {
	path := filepath.Join(dataDir, gsID, "server.toml")
	return os.WriteFile(path, []byte(content), 0644)
}

// BuildDockerArgs sestaví argumenty pro docker run z konfigurace
func (c *ServerConfig) BuildDockerArgs(containerName, gsDataDir string) []string {
	args := []string{"run", "-d", "--name", containerName}
	args = append(args, "--memory", c.Memory)
	args = append(args, "--restart", c.Restart)

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

	// Volumes — relativní cesty se resolvují vůči gsDataDir
	for containerPath, hostPath := range c.Volumes {
		resolved := hostPath
		if !filepath.IsAbs(hostPath) {
			resolved = filepath.Join(gsDataDir, hostPath)
		}
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

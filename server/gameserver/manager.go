package gameserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	DataDir    string
	PresetsDir string
	mu         sync.Mutex
}

type FileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

func NewManager(dataDir, presetsDir string) *Manager {
	os.MkdirAll(dataDir, 0755)
	SeedPresets(presetsDir)
	return &Manager{DataDir: dataDir, PresetsDir: presetsDir}
}

func (m *Manager) DockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func (m *Manager) PullImage(image string) error {
	cmd := exec.Command("docker", "pull", image)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}

// Start starts a game server — reads server.toml and runs docker run
func (m *Manager) Start(gsID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := ReadConfig(m.DataDir, gsID)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}

	if cfg.Image == "" {
		return "", fmt.Errorf("no image specified in server.toml")
	}

	// Validate config before building Docker args
	if err := cfg.ValidateConfig(); err != nil {
		return "", fmt.Errorf("invalid config: %w", err)
	}

	containerName := "nora-gs-" + gsID
	gsDataDir, _ := filepath.Abs(filepath.Join(m.DataDir, gsID))

	args := cfg.BuildDockerArgs(containerName, gsDataDir)

	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, string(out))
	}

	// Firewall is set up by handler after start (refreshGameServerFirewall)

	return strings.TrimSpace(string(out)), nil
}

// Stop stops a container — reads stop_timeout from config
func (m *Manager) Stop(containerID, gsID string) error {
	timeout := 10
	cfg, cfgErr := ReadConfig(m.DataDir, gsID)
	if cfgErr == nil && cfg.StopTimeout > 0 {
		timeout = cfg.StopTimeout
	}
	cmd := exec.Command("docker", "stop", "-t", fmt.Sprintf("%d", timeout), containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}

	// Firewall cleanup
	if cfgErr == nil {
		m.CleanupFirewall(cfg)
	}

	return nil
}

// runIPTables runs an iptables command, logs errors
func (m *Manager) runIPTables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("iptables command failed", "args", args, "output", strings.TrimSpace(string(out)), "error", err)
	}
	return err
}

// SetupFirewall sets up iptables DOCKER-USER chain for game server ports
func (m *Manager) SetupFirewall(cfg *ServerConfig, mode string, memberIPs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for spec, hostPort := range cfg.Ports {
		parts := strings.SplitN(spec, "/", 2)
		proto := "tcp"
		if len(parts) == 2 {
			proto = parts[1]
		}

		chain := fmt.Sprintf("NORA-GS-%d-%s", hostPort, proto)

		// Delete old chain (ignore errors — may not exist)
		m.runIPTables("-D", "DOCKER-USER", "-p", proto, "-m", "conntrack", "--ctorigdstport", fmt.Sprintf("%d", hostPort), "-j", chain)
		m.runIPTables("-F", chain)
		m.runIPTables("-X", chain)

		// Create new chain
		if err := m.runIPTables("-N", chain); err != nil {
			slog.Warn("iptables: cannot create chain, skipping", "chain", chain)
			continue
		}

		// Jump from DOCKER-USER
		m.runIPTables("-I", "DOCKER-USER", "-p", proto, "-m", "conntrack", "--ctorigdstport", fmt.Sprintf("%d", hostPort), "-j", chain)

		if mode == "room" {
			// Allow only member IP addresses
			for _, ip := range memberIPs {
				m.runIPTables("-A", chain, "-s", ip, "-j", "RETURN")
			}
			// Drop the rest
			m.runIPTables("-A", chain, "-j", "DROP")
		} else {
			// Open mode — allow everything
			m.runIPTables("-A", chain, "-j", "RETURN")
		}
	}
}

// CleanupFirewall deletes iptables chain for game server ports
func (m *Manager) CleanupFirewall(cfg *ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for spec, hostPort := range cfg.Ports {
		parts := strings.SplitN(spec, "/", 2)
		proto := "tcp"
		if len(parts) == 2 {
			proto = parts[1]
		}

		chain := fmt.Sprintf("NORA-GS-%d-%s", hostPort, proto)
		m.runIPTables("-D", "DOCKER-USER", "-p", proto, "-m", "conntrack", "--ctorigdstport", fmt.Sprintf("%d", hostPort), "-j", chain)
		m.runIPTables("-F", chain)
		m.runIPTables("-X", chain)
	}
}

func (m *Manager) Remove(containerID string) error {
	cmd := exec.Command("docker", "rm", "-f", containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}

func (m *Manager) RemoveByName(gsID string) error {
	containerName := "nora-gs-" + gsID
	cmd := exec.Command("docker", "rm", "-f", containerName)
	cmd.CombinedOutput()
	return nil
}

func (m *Manager) IsRunning(containerID string) bool {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerID)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func (m *Manager) Stats(containerID string) (*GameServerStats, error) {
	cmd := exec.Command("docker", "stats", "--no-stream", "--format",
		"{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}", containerID)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %v", err)
	}

	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, "\t")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected stats format: %s", line)
	}

	stats := &GameServerStats{
		CPUPercent: strings.TrimSpace(parts[0]),
		NetIO:      strings.TrimSpace(parts[2]),
	}

	memParts := strings.SplitN(strings.TrimSpace(parts[1]), " / ", 2)
	if len(memParts) == 2 {
		stats.MemUsage = strings.TrimSpace(memParts[0])
		stats.MemLimit = strings.TrimSpace(memParts[1])
	} else {
		stats.MemUsage = strings.TrimSpace(parts[1])
	}

	uptimeCmd := exec.Command("docker", "inspect", "-f", "{{.State.StartedAt}}", containerID)
	if uptimeOut, err := uptimeCmd.Output(); err == nil {
		stats.Uptime = strings.TrimSpace(string(uptimeOut))
	}

	return stats, nil
}

// GameServerStats — moved here from models (still used by handlers)
type GameServerStats struct {
	CPUPercent string `json:"cpu_percent"`
	MemUsage   string `json:"mem_usage"`
	MemLimit   string `json:"mem_limit"`
	NetIO      string `json:"net_io"`
	Uptime     string `json:"uptime"`
}

func (m *Manager) StreamLogs(ctx context.Context, containerID string, lines int) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", fmt.Sprintf("%d", lines), containerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		cmd.Wait()
	}()

	return stdout, nil
}

// isValidContainerID validates Docker container ID/name (hex, alphanum, dash, underscore, dot)
func isValidContainerID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

// SendCommand — reads console_command from config
func (m *Manager) SendCommand(containerID, gsID, command string) error {
	if !isValidContainerID(containerID) {
		return fmt.Errorf("invalid container ID")
	}

	consoleCmd := "rcon-cli"
	if cfg, err := ReadConfig(m.DataDir, gsID); err == nil && cfg.ConsoleCommand != "" {
		if !allowedConsoleCommands[cfg.ConsoleCommand] {
			return fmt.Errorf("disallowed console command: %q", cfg.ConsoleCommand)
		}
		consoleCmd = cfg.ConsoleCommand
	}

	// Sanitize command — newlines can be interpreted as command separators
	command = strings.ReplaceAll(command, "\n", " ")
	command = strings.ReplaceAll(command, "\r", "")

	// "--" prevents interpretation of containerID as a docker flag
	cmd := exec.Command("docker", "exec", "--", containerID, consoleCmd, command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}

// RecursiveFileEntry is a file found by recursive listing
type RecursiveFileEntry struct {
	RelPath string    `json:"rel_path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// --- File operations ---

// CreateServerDir creates a directory for a game server + server.toml from preset
func (m *Manager) CreateServerDir(gsID, preset string) error {
	dir := filepath.Join(m.DataDir, gsID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Load preset content from disk
	content, err := ReadPreset(m.PresetsDir, preset)
	if err != nil {
		// Fallback — first available preset or empty config
		presets := ListPresets(m.PresetsDir)
		if len(presets) > 0 {
			content, _ = ReadPreset(m.PresetsDir, presets[0].Name)
		}
		if content == "" {
			content = "# Game server configuration\nimage = \"\"\nmemory = \"2048m\"\n\n[ports]\n\n[env]\n\n[volumes]\n"
		}
	}

	return os.WriteFile(filepath.Join(dir, "server.toml"), []byte(content), 0644)
}

// DeleteServerDir deletes the entire server directory
func (m *Manager) DeleteServerDir(gsID string) error {
	dir := filepath.Join(m.DataDir, gsID)
	return os.RemoveAll(dir)
}

// SafePath validates path and protects against path traversal (including symlinks).
func (m *Manager) SafePath(gsID, relPath string) (string, error) {
	base := filepath.Join(m.DataDir, gsID)
	target := filepath.Join(base, relPath)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("invalid base path")
	}
	if absTarget != absBase && !strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal denied")
	}

	// Resolve symlinks and re-check (prevent symlink escape)
	if real, err := filepath.EvalSymlinks(absTarget); err == nil {
		realBase, _ := filepath.EvalSymlinks(absBase)
		if realBase == "" {
			realBase = absBase
		}
		if real != realBase && !strings.HasPrefix(real, realBase+string(filepath.Separator)) {
			return "", fmt.Errorf("path traversal denied (symlink)")
		}
	}

	return absTarget, nil
}

// ListFiles returns a directory listing
func (m *Manager) ListFiles(gsID, relPath string) ([]FileEntry, error) {
	dir, err := m.SafePath(gsID, relPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var result []FileEntry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, FileEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return result, nil
}

// ReadFile reads the content of a file (max 1MB)
func (m *Manager) ReadFile(gsID, relPath string) (string, error) {
	path, err := m.SafePath(gsID, relPath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot read directory")
	}
	if info.Size() > 1<<20 { // 1MB
		return "", fmt.Errorf("file too large (max 1MB)")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteFile writes content to a file
func (m *Manager) WriteFile(gsID, relPath, content string) error {
	path, err := m.SafePath(gsID, relPath)
	if err != nil {
		return err
	}

	// Create parent directory if it doesn't exist
	os.MkdirAll(filepath.Dir(path), 0755)

	return os.WriteFile(path, []byte(content), 0644)
}

// DeleteFile deletes a file or directory
func (m *Manager) DeleteFile(gsID, relPath string) error {
	if relPath == "" || relPath == "." || relPath == "/" {
		return fmt.Errorf("cannot delete root directory")
	}
	path, err := m.SafePath(gsID, relPath)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// RenameFile renames a file or directory within a game server volume.
func (m *Manager) RenameFile(gsID, relPath, newName string) error {
	if relPath == "" || relPath == "." || relPath == "/" {
		return fmt.Errorf("cannot rename root directory")
	}
	if newName == "" || newName == "." || newName == ".." {
		return fmt.Errorf("invalid new name")
	}
	if strings.ContainsAny(newName, "/\\") {
		return fmt.Errorf("new name must not contain path separators")
	}
	if len(newName) > 255 {
		return fmt.Errorf("new name too long")
	}
	oldPath, err := m.SafePath(gsID, relPath)
	if err != nil {
		return err
	}
	// Resolve symlinks on the parent directory to prevent escape via symlinked dirs
	parentDir, err := filepath.EvalSymlinks(filepath.Dir(oldPath))
	if err != nil {
		return fmt.Errorf("invalid parent path")
	}
	newPath := filepath.Join(parentDir, newName)
	// Validate the new path stays inside game server directory
	absNew, err := filepath.Abs(newPath)
	if err != nil {
		return fmt.Errorf("invalid path")
	}
	absBase, err := filepath.Abs(filepath.Join(m.DataDir, gsID))
	if err != nil {
		return fmt.Errorf("invalid base path")
	}
	// Also resolve symlinks on the base to compare real paths
	realBase, err := filepath.EvalSymlinks(absBase)
	if err != nil {
		realBase = absBase
	}
	if absNew != realBase && !strings.HasPrefix(absNew, realBase+string(filepath.Separator)) {
		return fmt.Errorf("path traversal denied")
	}
	return os.Rename(oldPath, newPath)
}

// Mkdir creates a directory
func (m *Manager) Mkdir(gsID, relPath string) error {
	path, err := m.SafePath(gsID, relPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(path, 0755)
}

// maxRecursiveFiles is the limit for recursive listing (DoS protection)
const maxRecursiveFiles = 10000

// ListFilesRecursive returns a recursive listing of files in a directory (files only, not directories)
func (m *Manager) ListFilesRecursive(gsID, relPath string) ([]RecursiveFileEntry, error) {
	dir, err := m.SafePath(gsID, relPath)
	if err != nil {
		return nil, err
	}

	var result []RecursiveFileEntry
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}
		if d.IsDir() {
			return nil
		}
		if len(result) >= maxRecursiveFiles {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		result = append(result, RecursiveFileEntry{
			RelPath: rel,
			Name:    d.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	return result, err
}

// FilePath returns the absolute path to a file (for ServeFile).
// Additionally checks that the file after resolving symlinks still resides in the game server directory.
func (m *Manager) FilePath(gsID, relPath string) (string, error) {
	path, err := m.SafePath(gsID, relPath)
	if err != nil {
		return "", err
	}
	// Resolve symlinks and re-check the path
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	absBase, err := filepath.Abs(filepath.Join(m.DataDir, gsID))
	if err != nil {
		return "", fmt.Errorf("invalid base path")
	}
	realBase, err := filepath.EvalSymlinks(absBase)
	if err != nil {
		realBase = absBase
	}
	if real != realBase && !strings.HasPrefix(real, realBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal denied (symlink)")
	}
	return path, nil
}

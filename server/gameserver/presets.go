package gameserver

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PresetEntry — metadata presetu pro API
type PresetEntry struct {
	Name string `json:"name"`
}

// SeedPresets vytvoří výchozí presety pokud adresář neexistuje nebo je prázdný
func SeedPresets(presetsDir string) error {
	os.MkdirAll(presetsDir, 0755)

	entries, _ := os.ReadDir(presetsDir)
	if len(entries) > 0 {
		return nil
	}

	for name, content := range defaultPresets {
		path := filepath.Join(presetsDir, name+".toml")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

// ListPresets vrátí dostupné presety z adresáře
func ListPresets(presetsDir string) []PresetEntry {
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		return nil
	}

	var presets []PresetEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".toml")
		presets = append(presets, PresetEntry{Name: name})
	}
	sort.Slice(presets, func(i, j int) bool {
		return presets[i].Name < presets[j].Name
	})
	return presets
}

// ReadPreset načte obsah preset souboru
func ReadPreset(presetsDir, name string) (string, error) {
	path := filepath.Join(presetsDir, name+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// --- Výchozí preset obsahy ---

var defaultPresets = map[string]string{
	"minecraft": presetMinecraft,
	"valheim":   presetValheim,
	"factorio":  presetFactorio,
	"terraria":  presetTerraria,
	"cs2":       presetCS2,
}

const presetMinecraft = `# Minecraft Java Edition (Paper)
# Image: https://hub.docker.com/r/itzg/minecraft-server
image = "itzg/minecraft-server:latest"

# Memory limit for the container
memory = "2048m"

# Restart policy: "no", "always", "unless-stopped", "on-failure"
restart = "unless-stopped"

# Seconds to wait before killing the container on stop
stop_timeout = 15

# Console command for RCON access (docker exec fallback)
console_command = "rcon-cli"

# Source RCON protocol (direct TCP, preferred over console_command)
rcon_port = 25575
rcon_password = "minecraft"

[ports]
"25565/tcp" = 25565

[env]
EULA = "TRUE"
TYPE = "PAPER"
MEMORY = "2G"
RCON_PASSWORD = "minecraft"

# Volume mounts (relative paths resolved from server data directory)
[volumes]
"/data" = "./data"
`

const presetValheim = `# Valheim Dedicated Server
# Image: https://hub.docker.com/r/lloesche/valheim-server
image = "lloesche/valheim-server"

memory = "4096m"
restart = "unless-stopped"
stop_timeout = 30

# Valheim server has no RCON — leave empty
console_command = ""

[ports]
"2456/udp" = 2456
"2457/udp" = 2457

[env]
SERVER_NAME = "My Valheim Server"
WORLD_NAME = "MyWorld"
SERVER_PASS = "changeme"

[volumes]
"/config" = "./config"
"/opt/valheim" = "./valheim"
`

const presetFactorio = `# Factorio Dedicated Server
# Image: https://hub.docker.com/r/factoriotools/factorio
image = "factoriotools/factorio:stable"

memory = "2048m"
restart = "unless-stopped"
stop_timeout = 15

console_command = ""

# Source RCON protocol (Factorio uses RCON on port 27015)
rcon_port = 27015
rcon_password = "changeme"

[ports]
"34197/udp" = 34197
"27015/tcp" = 27015

[env]

[volumes]
"/factorio" = "./data"
`

const presetTerraria = `# Terraria Dedicated Server
# Image: https://hub.docker.com/r/ryshe/terraria
image = "ryshe/terraria:latest"

memory = "1024m"
restart = "unless-stopped"
stop_timeout = 15

console_command = ""

[ports]
"7777/tcp" = 7777

[env]

[volumes]
"/root/.local/share/Terraria/Worlds" = "./worlds"
`

const presetCS2 = `# Counter-Strike 2 Dedicated Server
# Image: https://hub.docker.com/r/joedwards32/cs2
image = "joedwards32/cs2:latest"

memory = "4096m"
restart = "unless-stopped"
stop_timeout = 15

console_command = ""

# Source RCON protocol
rcon_port = 27015
rcon_password = "changeme"

[ports]
"27015/tcp" = 27015
"27015/udp" = 27015
"27020/udp" = 27020

[env]
CS2_SERVERNAME = "My CS2 Server"
CS2_PORT = "27015"
CS2_RCONPW = "changeme"

[volumes]
"/home/steam/cs2-dedicated" = "./data"
`

//go:build linux

package device

import (
	"os"
	"path/filepath"
	"strings"
)

func deviceIDPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "fontcache", ".uuid")
}

func platformHWParts() []string {
	var parts []string

	// CPU model
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				idx := strings.Index(line, ":")
				if idx >= 0 {
					parts = append(parts, strings.TrimSpace(line[idx+1:]))
					break
				}
			}
		}
	}

	// Disk serial / product serial
	serialPaths := []string{
		"/sys/class/dmi/id/product_serial",
		"/sys/class/dmi/id/board_serial",
	}
	for _, p := range serialPaths {
		if data, err := os.ReadFile(p); err == nil {
			s := strings.TrimSpace(string(data))
			if s != "" && s != "To Be Filled By O.E.M." {
				parts = append(parts, s)
				break
			}
		}
	}

	return parts
}

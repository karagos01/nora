//go:build windows

package device

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func deviceIDPath() string {
	appdata := os.Getenv("LOCALAPPDATA")
	if appdata == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		appdata = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(appdata, "Microsoft", "Fonts", ".cache")
}

func platformHWParts() []string {
	var parts []string

	// CPU name via wmic
	if out, err := exec.Command("wmic", "cpu", "get", "Name", "/value").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Name=") {
				parts = append(parts, strings.TrimPrefix(line, "Name="))
				break
			}
		}
	}

	// Disk serial via wmic
	if out, err := exec.Command("wmic", "diskdrive", "get", "SerialNumber", "/value").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "SerialNumber=") {
				serial := strings.TrimPrefix(line, "SerialNumber=")
				if serial != "" {
					parts = append(parts, serial)
					break
				}
			}
		}
	}

	return parts
}

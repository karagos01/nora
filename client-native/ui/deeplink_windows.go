//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RegisterURLScheme registers the nora:// URL scheme on Windows via the registry.
func RegisterURLScheme() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.Abs(exePath)

	// HKCU\Software\Classes\nora
	cmds := [][]string{
		{"reg", "add", `HKCU\Software\Classes\nora`, "/ve", "/d", "URL:NORA Protocol", "/f"},
		{"reg", "add", `HKCU\Software\Classes\nora`, "/v", "URL Protocol", "/d", "", "/f"},
		{"reg", "add", `HKCU\Software\Classes\nora\shell\open\command`, "/ve", "/d", fmt.Sprintf(`"%s" "%%1"`, exePath), "/f"},
	}

	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("reg add: %w", err)
		}
	}

	return nil
}

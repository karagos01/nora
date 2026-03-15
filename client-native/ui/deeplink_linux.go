//go:build linux

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RegisterURLScheme registers the nora:// URL scheme on Linux via a .desktop file.
func RegisterURLScheme() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.Abs(exePath)

	desktopDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0755); err != nil {
		return err
	}

	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=NORA
Exec=%s %%u
MimeType=x-scheme-handler/nora;
NoDisplay=true
`, exePath)

	path := filepath.Join(desktopDir, "nora-handler.desktop")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	// Register as handler
	exec.Command("xdg-mime", "default", "nora-handler.desktop", "x-scheme-handler/nora").Run()
	exec.Command("update-desktop-database", desktopDir).Run()

	return nil
}

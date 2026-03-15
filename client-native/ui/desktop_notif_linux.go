//go:build linux

package ui

import (
	"os/exec"
	"time"
)

// sendDesktopNotification odesle OS-level notifikaci na Linuxu (notify-send).
func sendDesktopNotification(title, body, icon string) error {
	args := []string{"-a", "NORA"}
	if icon != "" {
		args = append(args, "-i", icon)
	}
	args = append(args, title, body)

	cmd := exec.Command("notify-send", args...)
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

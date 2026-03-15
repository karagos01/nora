//go:build !linux && !windows

package ui

// sendDesktopNotification je no-op na nepodporovaných platformach.
func sendDesktopNotification(title, body, icon string) error {
	return nil
}

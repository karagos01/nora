//go:build !linux && !windows

package ui

// sendDesktopNotification is a no-op on unsupported platforms.
func sendDesktopNotification(title, body, icon string) error {
	return nil
}

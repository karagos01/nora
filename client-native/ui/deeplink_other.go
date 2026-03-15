//go:build !linux && !windows

package ui

import "fmt"

// RegisterURLScheme — stub for unsupported platforms.
func RegisterURLScheme() error {
	return fmt.Errorf("URL scheme registration not supported on this platform")
}

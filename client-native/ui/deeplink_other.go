//go:build !linux && !windows

package ui

import "fmt"

// RegisterURLScheme — stub pro nepodporované platformy.
func RegisterURLScheme() error {
	return fmt.Errorf("URL scheme registration not supported on this platform")
}

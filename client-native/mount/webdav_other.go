//go:build !windows

package mount

// FixWindowsWebDAVRegistry is a no-op on non-Windows platforms.
func FixWindowsWebDAVRegistry() {}

//go:build !windows

package mount

import "fmt"

func mapDrive(webdavURL string) (string, error) {
	return "", fmt.Errorf("drive mapping is only supported on Windows")
}

func mapDrivePreferred(webdavURL string, preferred string) (string, error) {
	return "", fmt.Errorf("drive mapping is only supported on Windows")
}

func unmapDrive(letter string) {
	// No-op on non-Windows
}

func renameDrive(letter, label string) {
	// No-op on non-Windows
}

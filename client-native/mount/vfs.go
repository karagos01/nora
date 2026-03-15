package mount

import (
	"io"
	"time"
)

// FSEntry — universal file/directory entry for VirtualFS.
type FSEntry struct {
	Name       string
	IsDir      bool
	Size       int64
	ModifiedAt time.Time
}

// VirtualFS — interface for virtual filesystem (share, game server, ...).
type VirtualFS interface {
	// FSName returns the filesystem name (for logs, UI).
	FSName() string

	// CanWrite returns true if the filesystem is writable.
	CanWrite() bool

	// SetCanWrite sets write permissions.
	SetCanWrite(v bool)

	// ListDir returns directory contents.
	ListDir(path string) ([]FSEntry, error)

	// Stat returns information about a file/directory.
	Stat(path string) (*FSEntry, error)

	// GetFile returns a ReadSeekCloser and file size.
	GetFile(path string) (io.ReadSeekCloser, int64, error)

	// PutFile uploads a file from tempPath.
	PutFile(path string, tempPath string, size int64) error

	// DeleteFile deletes a file.
	DeleteFile(relativePath, fileName string) error

	// MkDir creates a directory.
	MkDir(path string) error

	// RefreshListing refreshes the listing for a given path.
	RefreshListing(path string) error
}

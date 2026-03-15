package mount

import (
	"io"
	"time"
)

// FSEntry — univerzální záznam souboru/adresáře pro VirtualFS.
type FSEntry struct {
	Name       string
	IsDir      bool
	Size       int64
	ModifiedAt time.Time
}

// VirtualFS — interface pro virtuální filesystem (share, game server, ...).
type VirtualFS interface {
	// FSName vrátí název filesystému (pro logy, UI).
	FSName() string

	// CanWrite vrátí true pokud je filesystem zapisovatelný.
	CanWrite() bool

	// SetCanWrite nastaví write oprávnění.
	SetCanWrite(v bool)

	// ListDir vrátí obsah adresáře.
	ListDir(path string) ([]FSEntry, error)

	// Stat vrátí informace o souboru/adresáři.
	Stat(path string) (*FSEntry, error)

	// GetFile vrátí ReadSeekCloser a velikost souboru.
	GetFile(path string) (io.ReadSeekCloser, int64, error)

	// PutFile nahraje soubor z tempPath.
	PutFile(path string, tempPath string, size int64) error

	// DeleteFile smaže soubor.
	DeleteFile(relativePath, fileName string) error

	// MkDir vytvoří adresář.
	MkDir(path string) error

	// RefreshListing obnoví listing pro danou cestu.
	RefreshListing(path string) error
}

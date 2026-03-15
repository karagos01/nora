package mount

import (
	"os"
	"path/filepath"
	"strings"
)

// Cache manages local file cache for downloaded files from shared directories.
// Structure: ~/.nora/cache/{serverAddr}/{shareID}/{relativePath}/{fileName}
type Cache struct {
	baseDir string // ~/.nora/cache
}

func NewCache() *Cache {
	home, _ := os.UserHomeDir()
	return &Cache{
		baseDir: filepath.Join(home, ".nora", "cache"),
	}
}

// Dir returns the root cache directory.
func (c *Cache) Dir() string {
	return c.baseDir
}

// FilePath returns the path to a file in cache.
func (c *Cache) FilePath(serverAddr, shareID, relativePath, fileName string) string {
	// Sanitize serverAddr — replace : with _
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")

	rel := strings.TrimPrefix(relativePath, "/")
	return filepath.Join(c.baseDir, safe, shareID, rel, fileName)
}

// Has returns true if the file exists in cache and has the expected size.
func (c *Cache) Has(serverAddr, shareID, relativePath, fileName string, expectedSize int64) bool {
	path := c.FilePath(serverAddr, shareID, relativePath, fileName)
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Size() == expectedSize
}

// EnsureDir ensures the directory for a file exists in cache.
func (c *Cache) EnsureDir(serverAddr, shareID, relativePath string) error {
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")
	rel := strings.TrimPrefix(relativePath, "/")
	dir := filepath.Join(c.baseDir, safe, shareID, rel)
	return os.MkdirAll(dir, 0700)
}

// Open opens a file from cache for reading.
func (c *Cache) Open(serverAddr, shareID, relativePath, fileName string) (*os.File, error) {
	path := c.FilePath(serverAddr, shareID, relativePath, fileName)
	return os.Open(path)
}

// ShareDir returns the path to the root directory of a specific share in cache.
func (c *Cache) ShareDir(serverAddr, shareID string) string {
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")
	return filepath.Join(c.baseDir, safe, shareID)
}

// Remove deletes a file from cache.
func (c *Cache) Remove(serverAddr, shareID, relativePath, fileName string) error {
	path := c.FilePath(serverAddr, shareID, relativePath, fileName)
	return os.Remove(path)
}

// RemoveShare deletes the entire cache for a given share.
func (c *Cache) RemoveShare(serverAddr, shareID string) error {
	return os.RemoveAll(c.ShareDir(serverAddr, shareID))
}

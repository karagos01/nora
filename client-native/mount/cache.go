package mount

import (
	"os"
	"path/filepath"
	"strings"
)

// Cache spravuje lokální souborový cache pro stažené soubory ze sdílených adresářů.
// Struktura: ~/.nora/cache/{serverAddr}/{shareID}/{relativePath}/{fileName}
type Cache struct {
	baseDir string // ~/.nora/cache
}

func NewCache() *Cache {
	home, _ := os.UserHomeDir()
	return &Cache{
		baseDir: filepath.Join(home, ".nora", "cache"),
	}
}

// Dir vrátí kořenový adresář cache.
func (c *Cache) Dir() string {
	return c.baseDir
}

// FilePath vrátí cestu k souboru v cache.
func (c *Cache) FilePath(serverAddr, shareID, relativePath, fileName string) string {
	// Sanitizace serverAddr — nahradit : za _
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")

	rel := strings.TrimPrefix(relativePath, "/")
	return filepath.Join(c.baseDir, safe, shareID, rel, fileName)
}

// Has vrátí true pokud soubor existuje v cache a má očekávanou velikost.
func (c *Cache) Has(serverAddr, shareID, relativePath, fileName string, expectedSize int64) bool {
	path := c.FilePath(serverAddr, shareID, relativePath, fileName)
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Size() == expectedSize
}

// EnsureDir zajistí existenci adresáře pro soubor v cache.
func (c *Cache) EnsureDir(serverAddr, shareID, relativePath string) error {
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")
	rel := strings.TrimPrefix(relativePath, "/")
	dir := filepath.Join(c.baseDir, safe, shareID, rel)
	return os.MkdirAll(dir, 0700)
}

// Open otevře soubor z cache pro čtení.
func (c *Cache) Open(serverAddr, shareID, relativePath, fileName string) (*os.File, error) {
	path := c.FilePath(serverAddr, shareID, relativePath, fileName)
	return os.Open(path)
}

// ShareDir vrátí cestu ke kořenovému adresáři konkrétního share v cache.
func (c *Cache) ShareDir(serverAddr, shareID string) string {
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")
	return filepath.Join(c.baseDir, safe, shareID)
}

// Remove smaže soubor z cache.
func (c *Cache) Remove(serverAddr, shareID, relativePath, fileName string) error {
	path := c.FilePath(serverAddr, shareID, relativePath, fileName)
	return os.Remove(path)
}

// RemoveShare smaže celý cache pro daný share.
func (c *Cache) RemoveShare(serverAddr, shareID string) error {
	return os.RemoveAll(c.ShareDir(serverAddr, shareID))
}

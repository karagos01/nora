package mount

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"nora-client/api"
)

// DownloadFunc — callback pro stažení souboru do cache.
// Blokující — vrací se až po dokončení stahování.
type DownloadFunc func(shareID, fileID, savePath string) error

// UploadFunc — callback pro nahrání souboru do share přes P2P.
// Blokující — vrací se až po dokončení uploadu.
type UploadFunc func(shareID, fileName, relativePath, stagedPath string, fileSize int64) error

// DeleteFunc — callback pro smazání souboru ze share přes server API.
type DeleteFunc func(shareID, relativePath, fileName string) error

// ShareFS implementuje virtuální filesystem nad sdíleným adresářem.
type ShareFS struct {
	mu        sync.RWMutex
	shareID   string
	shareName string
	cache     *Cache
	serverAddr string
	client    *api.Client

	// Cache listingů: path → []SharedFileEntry
	listings map[string][]api.SharedFileEntry
	// Čas posledního refreshe listingu
	listingTime map[string]time.Time

	// Callback pro stažení souboru
	downloadFn DownloadFunc

	// Callback pro nahrání souboru
	uploadFn UploadFunc

	// Callback pro smazání souboru
	deleteFn DeleteFunc

	// Oprávnění — pokud false, filesystem je read-only (file modes 0444/0555)
	canWrite bool

	// Lokálně vytvořené adresáře (Mkdir z mount klienta)
	pendingDirs map[string]bool

	// Deduplikace concurrent downloadů — path → done channel
	pendingMu        sync.Mutex
	pendingDownloads map[string]chan struct{}
}

// FSName vrátí název share pro logy a UI.
func (fs *ShareFS) FSName() string {
	return fs.shareName
}

// CanWrite vrátí true pokud uživatel má write oprávnění.
func (fs *ShareFS) CanWrite() bool {
	return fs.canWrite
}

// SetCanWrite nastaví write oprávnění.
func (fs *ShareFS) SetCanWrite(v bool) {
	fs.canWrite = v
}

func NewShareFS(shareID, shareName, serverAddr string, client *api.Client, cache *Cache, downloadFn DownloadFunc, uploadFn UploadFunc, deleteFn DeleteFunc) *ShareFS {
	return &ShareFS{
		shareID:          shareID,
		shareName:        shareName,
		cache:            cache,
		serverAddr:       serverAddr,
		client:           client,
		listings:         make(map[string][]api.SharedFileEntry),
		listingTime:      make(map[string]time.Time),
		downloadFn:       downloadFn,
		uploadFn:         uploadFn,
		deleteFn:         deleteFn,
		pendingDirs:      make(map[string]bool),
		pendingDownloads: make(map[string]chan struct{}),
	}
}

// StagingDir vrátí cestu ke staging adresáři pro dočasné soubory.
func StagingDir() string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf("%s/.nora/staging", home)
}

// DeleteFile smaže soubor ze share přes deleteFn. Po úspěchu refreshne listing.
func (fs *ShareFS) DeleteFile(relativePath, fileName string) error {
	if fs.deleteFn == nil {
		return fmt.Errorf("delete not available")
	}
	if err := fs.deleteFn(fs.shareID, relativePath, fileName); err != nil {
		return err
	}
	fs.refreshListing(relativePath)
	return nil
}

// PutFile nahraje soubor do share přes uploadFn. Po úspěchu refreshne listing.
func (fs *ShareFS) PutFile(path string, tempPath string, size int64) error {
	if fs.uploadFn == nil {
		return fmt.Errorf("upload not available")
	}

	parent, name := splitPath(path)

	if err := fs.uploadFn(fs.shareID, name, parent, tempPath, size); err != nil {
		return err
	}

	// Refresh listing po úspěšném uploadu
	fs.refreshListing(parent)
	return nil
}

// MkDir vytvoří virtuální adresář (lokální pending + entry v listings cache).
func (fs *ShareFS) MkDir(path string) error {
	fs.mu.Lock()
	fs.pendingDirs[path] = true

	// Přidat do listings cache jako virtuální entry
	parent, name := splitPath(path)
	entries := fs.listings[parent]
	// Kontrola duplicity
	for _, e := range entries {
		if e.FileName == name && e.IsDir {
			fs.mu.Unlock()
			return nil
		}
	}
	fs.listings[parent] = append(entries, api.SharedFileEntry{
		FileName: name,
		IsDir:    true,
	})
	fs.mu.Unlock()
	return nil
}

// ListDir vrátí obsah adresáře z cache listingů (nebo ze serveru).
func (fs *ShareFS) ListDir(path string) ([]FSEntry, error) {
	if path == "" {
		path = "/"
	}

	fs.mu.RLock()
	entries, ok := fs.listings[path]
	t := fs.listingTime[path]
	fs.mu.RUnlock()

	// Refresh pokud starší než 30 sekund
	if !ok || time.Since(t) > 30*time.Second {
		var err error
		entries, err = fs.refreshListing(path)
		if err != nil {
			// Vrátit starý cache pokud máme
			if ok {
				return shareEntriesToFS(entries), nil
			}
			return nil, err
		}
	}

	return shareEntriesToFS(entries), nil
}

// Stat vrátí informace o souboru/adresáři.
func (fs *ShareFS) Stat(path string) (*FSEntry, error) {
	if path == "/" || path == "" {
		return &FSEntry{
			Name:  fs.shareName,
			IsDir: true,
		}, nil
	}

	parent, name := splitPath(path)

	entries, err := fs.ListDir(parent)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if e.Name == name {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("not found: %s", path)
}

// statRaw vrátí raw SharedFileEntry pro interní potřeby (cache, download).
func (fs *ShareFS) statRaw(path string) (*api.SharedFileEntry, error) {
	if path == "/" || path == "" {
		return &api.SharedFileEntry{FileName: fs.shareName, IsDir: true}, nil
	}

	parent, name := splitPath(path)

	if parent == "" {
		parent = "/"
	}

	fs.mu.RLock()
	entries, ok := fs.listings[parent]
	t := fs.listingTime[parent]
	fs.mu.RUnlock()

	if !ok || time.Since(t) > 30*time.Second {
		var err error
		entries, err = fs.refreshListing(parent)
		if err != nil {
			if ok {
				// Použít starý cache
			} else {
				return nil, err
			}
		}
	}

	for _, e := range entries {
		if e.FileName == name {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("not found: %s", path)
}

// GetFile vrátí ReadSeekCloser pro soubor. Pokud není v cache, stáhne ho.
// Concurrent GETs na stejný soubor čekají na jeden download (deduplikace).
func (fs *ShareFS) GetFile(path string) (io.ReadSeekCloser, int64, error) {
	entry, err := fs.statRaw(path)
	if err != nil {
		return nil, 0, err
	}
	if entry.IsDir {
		return nil, 0, fmt.Errorf("is a directory: %s", path)
	}

	parent, _ := splitPath(path)

	// Zkusit cache
	if fs.cache.Has(fs.serverAddr, fs.shareID, parent, entry.FileName, entry.FileSize) {
		f, err := fs.cache.Open(fs.serverAddr, fs.shareID, parent, entry.FileName)
		if err == nil {
			log.Printf("ShareFS: cache hit for %s (%d bytes)", path, entry.FileSize)
			return f, entry.FileSize, nil
		}
	}

	// Stáhnout — s deduplikací concurrent downloadů
	if fs.downloadFn == nil {
		return nil, 0, fmt.Errorf("download not available")
	}

	// Čekat pokud jiný goroutine už stahuje stejný soubor
	fs.pendingMu.Lock()
	if ch, ok := fs.pendingDownloads[path]; ok {
		fs.pendingMu.Unlock()
		log.Printf("ShareFS: waiting for pending download of %s", path)
		<-ch // Čekat na dokončení
		// Zkusit cache znovu
		if fs.cache.Has(fs.serverAddr, fs.shareID, parent, entry.FileName, entry.FileSize) {
			f, err := fs.cache.Open(fs.serverAddr, fs.shareID, parent, entry.FileName)
			if err == nil {
				log.Printf("ShareFS: cache hit after pending download of %s", path)
				return f, entry.FileSize, nil
			}
		}
		return nil, 0, fmt.Errorf("download failed for %s", path)
	}
	// Zaregistrovat pending download
	done := make(chan struct{})
	fs.pendingDownloads[path] = done
	fs.pendingMu.Unlock()

	defer func() {
		fs.pendingMu.Lock()
		delete(fs.pendingDownloads, path)
		close(done)
		fs.pendingMu.Unlock()
	}()

	savePath := fs.cache.FilePath(fs.serverAddr, fs.shareID, parent, entry.FileName)
	if err := fs.cache.EnsureDir(fs.serverAddr, fs.shareID, parent); err != nil {
		return nil, 0, fmt.Errorf("create cache dir: %w", err)
	}

	log.Printf("ShareFS: downloading %s (expected %d bytes) to %s", path, entry.FileSize, savePath)
	if err := fs.downloadFn(fs.shareID, entry.ID, savePath); err != nil {
		return nil, 0, fmt.Errorf("download: %w", err)
	}

	// Ověřit velikost staženého souboru
	if fi, serr := os.Stat(savePath); serr == nil {
		log.Printf("ShareFS: downloaded %s (%d bytes, expected %d)", path, fi.Size(), entry.FileSize)
	}

	f, err := os.Open(savePath)
	if err != nil {
		return nil, 0, err
	}
	return f, entry.FileSize, nil
}

// RefreshListing obnoví listing ze serveru pro danou cestu.
func (fs *ShareFS) RefreshListing(path string) error {
	_, err := fs.refreshListing(path)
	return err
}

func (fs *ShareFS) refreshListing(path string) ([]api.SharedFileEntry, error) {
	entries, err := fs.client.GetShareFiles(fs.shareID, path)
	if err != nil {
		return nil, err
	}

	fs.mu.Lock()
	fs.listings[path] = entries
	fs.listingTime[path] = time.Now()
	fs.mu.Unlock()

	log.Printf("ShareFS: refreshed listing for %s/%s: %d entries", fs.shareID, path, len(entries))
	return entries, nil
}

// RefreshAll obnoví všechny cachované listingy.
func (fs *ShareFS) RefreshAll() {
	fs.mu.RLock()
	paths := make([]string, 0, len(fs.listings))
	for p := range fs.listings {
		paths = append(paths, p)
	}
	fs.mu.RUnlock()

	for _, p := range paths {
		fs.refreshListing(p)
	}
}

// shareEntriesToFS konvertuje []api.SharedFileEntry na []FSEntry.
func shareEntriesToFS(entries []api.SharedFileEntry) []FSEntry {
	result := make([]FSEntry, len(entries))
	for i, e := range entries {
		var modTime time.Time
		if e.ModifiedAt != nil {
			modTime = *e.ModifiedAt
		}
		result[i] = FSEntry{
			Name:       e.FileName,
			IsDir:      e.IsDir,
			Size:       e.FileSize,
			ModifiedAt: modTime,
		}
	}
	return result
}

// splitPath rozdělí cestu na parent a name.
func splitPath(path string) (parent, name string) {
	path = strings.TrimSuffix(path, "/")
	if path == "" || path == "/" {
		return "/", ""
	}
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "/", path
	}
	parent = path[:idx]
	if parent == "" {
		parent = "/"
	}
	name = path[idx+1:]
	return
}

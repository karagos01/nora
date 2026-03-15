package mount

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nora-client/api"
)

// GameServerFS implements VirtualFS for game server files via HTTP API.
type GameServerFS struct {
	mu         sync.RWMutex
	serverID   string
	serverName string
	serverAddr string
	client     *api.Client
	cache      *Cache
	canWrite   bool

	// Listing cache: path → []api.GameServerFileEntry
	listings    map[string][]api.GameServerFileEntry
	listingTime map[string]time.Time

	// Deduplication of concurrent downloads
	pendingMu        sync.Mutex
	pendingDownloads map[string]chan struct{}
}

func NewGameServerFS(serverID, serverName, serverAddr string, client *api.Client, cache *Cache) *GameServerFS {
	return &GameServerFS{
		serverID:         serverID,
		serverName:       serverName,
		serverAddr:       serverAddr,
		client:           client,
		cache:            cache,
		canWrite:         true,
		listings:         make(map[string][]api.GameServerFileEntry),
		listingTime:      make(map[string]time.Time),
		pendingDownloads: make(map[string]chan struct{}),
	}
}

func (fs *GameServerFS) FSName() string   { return fs.serverName }
func (fs *GameServerFS) CanWrite() bool   { return fs.canWrite }
func (fs *GameServerFS) SetCanWrite(v bool) { fs.canWrite = v }

func (fs *GameServerFS) ListDir(path string) ([]FSEntry, error) {
	if path == "" || path == "/" {
		path = ""
	}

	fs.mu.RLock()
	entries, ok := fs.listings[path]
	t := fs.listingTime[path]
	fs.mu.RUnlock()

	if !ok || time.Since(t) > 30*time.Second {
		var err error
		entries, err = fs.refreshListing(path)
		if err != nil {
			if ok {
				return gsEntriesToFS(entries), nil
			}
			return nil, err
		}
	}

	return gsEntriesToFS(entries), nil
}

func (fs *GameServerFS) Stat(path string) (*FSEntry, error) {
	if path == "/" || path == "" {
		return &FSEntry{
			Name:  fs.serverName,
			IsDir: true,
		}, nil
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if parent == "." {
		parent = ""
	}

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

func (fs *GameServerFS) GetFile(path string) (io.ReadSeekCloser, int64, error) {
	// Get entry for size
	entry, err := fs.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	if entry.IsDir {
		return nil, 0, fmt.Errorf("is a directory: %s", path)
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if parent == "." {
		parent = ""
	}

	// Cache ID — for game servers we use "gs-{serverID}" as shareID
	cacheID := "gs-" + fs.serverID

	// Try cache
	if fs.cache.Has(fs.serverAddr, cacheID, parent, name, entry.Size) {
		f, err := fs.cache.Open(fs.serverAddr, cacheID, parent, name)
		if err == nil {
			log.Printf("GameServerFS: cache hit for %s (%d bytes)", path, entry.Size)
			return f, entry.Size, nil
		}
	}

	// Deduplication of concurrent downloads
	fs.pendingMu.Lock()
	if ch, ok := fs.pendingDownloads[path]; ok {
		fs.pendingMu.Unlock()
		log.Printf("GameServerFS: waiting for pending download of %s", path)
		<-ch
		if fs.cache.Has(fs.serverAddr, cacheID, parent, name, entry.Size) {
			f, err := fs.cache.Open(fs.serverAddr, cacheID, parent, name)
			if err == nil {
				return f, entry.Size, nil
			}
		}
		return nil, 0, fmt.Errorf("download failed for %s", path)
	}
	done := make(chan struct{})
	fs.pendingDownloads[path] = done
	fs.pendingMu.Unlock()

	defer func() {
		fs.pendingMu.Lock()
		delete(fs.pendingDownloads, path)
		close(done)
		fs.pendingMu.Unlock()
	}()

	savePath := fs.cache.FilePath(fs.serverAddr, cacheID, parent, name)
	if err := fs.cache.EnsureDir(fs.serverAddr, cacheID, parent); err != nil {
		return nil, 0, fmt.Errorf("create cache dir: %w", err)
	}

	log.Printf("GameServerFS: downloading %s (%d bytes) to %s", path, entry.Size, savePath)
	if err := fs.client.DownloadGameServerFile(fs.serverID, path, savePath); err != nil {
		return nil, 0, fmt.Errorf("download: %w", err)
	}

	f, err := os.Open(savePath)
	if err != nil {
		return nil, 0, err
	}
	return f, entry.Size, nil
}

func (fs *GameServerFS) PutFile(path string, tempPath string, size int64) error {
	if !fs.canWrite {
		return fmt.Errorf("read-only filesystem")
	}

	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if parent == "." || parent == "/" {
		parent = ""
	}

	data, err := os.ReadFile(tempPath)
	if err != nil {
		return fmt.Errorf("read temp file: %w", err)
	}

	uploadPath := parent
	if uploadPath == "" {
		uploadPath = "."
	}

	if err := fs.client.UploadGameServerFile(fs.serverID, uploadPath, name, data); err != nil {
		return err
	}

	fs.refreshListing(parent)
	return nil
}

func (fs *GameServerFS) DeleteFile(relativePath, fileName string) error {
	if !fs.canWrite {
		return fmt.Errorf("read-only filesystem")
	}

	path := fileName
	if relativePath != "" && relativePath != "/" {
		path = relativePath + "/" + fileName
	}

	if err := fs.client.DeleteGameServerFile(fs.serverID, path); err != nil {
		return err
	}

	fs.refreshListing(relativePath)
	return nil
}

func (fs *GameServerFS) MkDir(path string) error {
	if !fs.canWrite {
		return fmt.Errorf("read-only filesystem")
	}

	// Remove leading slash — game server API does not want leading slash
	cleanPath := path
	if len(cleanPath) > 0 && cleanPath[0] == '/' {
		cleanPath = cleanPath[1:]
	}

	if err := fs.client.MkdirGameServer(fs.serverID, cleanPath); err != nil {
		return err
	}

	parent := filepath.Dir(path)
	if parent == "." {
		parent = ""
	}
	fs.refreshListing(parent)
	return nil
}

func (fs *GameServerFS) RefreshListing(path string) error {
	_, err := fs.refreshListing(path)
	return err
}

func (fs *GameServerFS) refreshListing(path string) ([]api.GameServerFileEntry, error) {
	entries, err := fs.client.ListGameServerFiles(fs.serverID, path)
	if err != nil {
		return nil, err
	}

	fs.mu.Lock()
	fs.listings[path] = entries
	fs.listingTime[path] = time.Now()
	fs.mu.Unlock()

	log.Printf("GameServerFS: refreshed listing for %s/%s: %d entries", fs.serverID, path, len(entries))
	return entries, nil
}

// gsEntriesToFS converts []api.GameServerFileEntry to []FSEntry.
func gsEntriesToFS(entries []api.GameServerFileEntry) []FSEntry {
	result := make([]FSEntry, len(entries))
	for i, e := range entries {
		result[i] = FSEntry{
			Name:       e.Name,
			IsDir:      e.IsDir,
			Size:       e.Size,
			ModifiedAt: e.ModTime,
		}
	}
	return result
}

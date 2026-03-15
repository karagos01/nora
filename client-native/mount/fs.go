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

// DownloadFunc — callback for downloading a file to cache.
// Blocking — returns only after download is complete.
type DownloadFunc func(shareID, fileID, savePath string) error

// UploadFunc — callback for uploading a file to share via P2P.
// Blocking — returns only after upload is complete.
type UploadFunc func(shareID, fileName, relativePath, stagedPath string, fileSize int64) error

// DeleteFunc — callback for deleting a file from share via server API.
type DeleteFunc func(shareID, relativePath, fileName string) error

// ShareFS implements a virtual filesystem over a shared directory.
type ShareFS struct {
	mu        sync.RWMutex
	shareID   string
	shareName string
	cache     *Cache
	serverAddr string
	client    *api.Client

	// Listing cache: path → []SharedFileEntry
	listings map[string][]api.SharedFileEntry
	// Time of last listing refresh
	listingTime map[string]time.Time

	// Callback for downloading a file
	downloadFn DownloadFunc

	// Callback for uploading a file
	uploadFn UploadFunc

	// Callback for deleting a file
	deleteFn DeleteFunc

	// Permissions — if false, filesystem is read-only (file modes 0444/0555)
	canWrite bool

	// Locally created directories (Mkdir from mount client)
	pendingDirs map[string]bool

	// Deduplication of concurrent downloads — path → done channel
	pendingMu        sync.Mutex
	pendingDownloads map[string]chan struct{}
}

// FSName returns the share name for logs and UI.
func (fs *ShareFS) FSName() string {
	return fs.shareName
}

// CanWrite returns true if the user has write permissions.
func (fs *ShareFS) CanWrite() bool {
	return fs.canWrite
}

// SetCanWrite sets write permissions.
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

// StagingDir returns the path to the staging directory for temporary files.
func StagingDir() string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf("%s/.nora/staging", home)
}

// DeleteFile deletes a file from share via deleteFn. Refreshes listing on success.
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

// PutFile uploads a file to share via uploadFn. Refreshes listing on success.
func (fs *ShareFS) PutFile(path string, tempPath string, size int64) error {
	if fs.uploadFn == nil {
		return fmt.Errorf("upload not available")
	}

	parent, name := splitPath(path)

	if err := fs.uploadFn(fs.shareID, name, parent, tempPath, size); err != nil {
		return err
	}

	// Refresh listing after successful upload
	fs.refreshListing(parent)
	return nil
}

// MkDir creates a virtual directory (local pending + entry in listings cache).
func (fs *ShareFS) MkDir(path string) error {
	fs.mu.Lock()
	fs.pendingDirs[path] = true

	// Add to listings cache as a virtual entry
	parent, name := splitPath(path)
	entries := fs.listings[parent]
	// Check for duplicate
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

// ListDir returns directory contents from listing cache (or from server).
func (fs *ShareFS) ListDir(path string) ([]FSEntry, error) {
	if path == "" {
		path = "/"
	}

	fs.mu.RLock()
	entries, ok := fs.listings[path]
	t := fs.listingTime[path]
	fs.mu.RUnlock()

	// Refresh if older than 30 seconds
	if !ok || time.Since(t) > 30*time.Second {
		var err error
		entries, err = fs.refreshListing(path)
		if err != nil {
			// Return old cache if available
			if ok {
				return shareEntriesToFS(entries), nil
			}
			return nil, err
		}
	}

	return shareEntriesToFS(entries), nil
}

// Stat returns information about a file/directory.
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

// statRaw returns a raw SharedFileEntry for internal use (cache, download).
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
				// Use old cache
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

// GetFile returns a ReadSeekCloser for a file. If not cached, downloads it.
// Concurrent GETs on the same file wait for a single download (deduplication).
func (fs *ShareFS) GetFile(path string) (io.ReadSeekCloser, int64, error) {
	entry, err := fs.statRaw(path)
	if err != nil {
		return nil, 0, err
	}
	if entry.IsDir {
		return nil, 0, fmt.Errorf("is a directory: %s", path)
	}

	parent, _ := splitPath(path)

	// Try cache
	if fs.cache.Has(fs.serverAddr, fs.shareID, parent, entry.FileName, entry.FileSize) {
		f, err := fs.cache.Open(fs.serverAddr, fs.shareID, parent, entry.FileName)
		if err == nil {
			log.Printf("ShareFS: cache hit for %s (%d bytes)", path, entry.FileSize)
			return f, entry.FileSize, nil
		}
	}

	// Download — with concurrent download deduplication
	if fs.downloadFn == nil {
		return nil, 0, fmt.Errorf("download not available")
	}

	// Wait if another goroutine is already downloading the same file
	fs.pendingMu.Lock()
	if ch, ok := fs.pendingDownloads[path]; ok {
		fs.pendingMu.Unlock()
		log.Printf("ShareFS: waiting for pending download of %s", path)
		<-ch // Wait for completion
		// Try cache znovu
		if fs.cache.Has(fs.serverAddr, fs.shareID, parent, entry.FileName, entry.FileSize) {
			f, err := fs.cache.Open(fs.serverAddr, fs.shareID, parent, entry.FileName)
			if err == nil {
				log.Printf("ShareFS: cache hit after pending download of %s", path)
				return f, entry.FileSize, nil
			}
		}
		return nil, 0, fmt.Errorf("download failed for %s", path)
	}
	// Register pending download
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

	// Verify downloaded file size
	if fi, serr := os.Stat(savePath); serr == nil {
		log.Printf("ShareFS: downloaded %s (%d bytes, expected %d)", path, fi.Size(), entry.FileSize)
	}

	f, err := os.Open(savePath)
	if err != nil {
		return nil, 0, err
	}
	return f, entry.FileSize, nil
}

// RefreshListing refreshes the listing from server for the given path.
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

// RefreshAll refreshes all cached listings.
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

// shareEntriesToFS converts []api.SharedFileEntry to []FSEntry.
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

// splitPath splits a path into parent and name.
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

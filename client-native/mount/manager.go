package mount

import (
	"fmt"
	"log"
	"runtime"
	"sync"

	"nora-client/api"
)

// MountInfo drží informace o jednom mountu.
type MountInfo struct {
	ShareID     string
	ShareName   string
	Type        string // "fuse" nebo "webdav"
	Path        string // mount path (FUSE) nebo WebDAV URL
	DriveLetter string // Windows: mapované písmeno disku (např. "Z:")
	Port        int    // WebDAV port (pro reuse při restartu)
	CanWrite    bool   // Write oprávnění

	fuse   *FuseMount
	webdav *WebDAVServer
	fs     VirtualFS
}

// MountManager spravuje všechny mounty pro jedno server connection.
type MountManager struct {
	mu         sync.Mutex
	mounts     map[string]*MountInfo // shareID → MountInfo
	cache      *Cache
	serverAddr string
	client     *api.Client
	downloadFn DownloadFunc
	uploadFn   UploadFunc
	deleteFn   DeleteFunc
}

func NewMountManager(serverAddr string, client *api.Client, downloadFn DownloadFunc, uploadFn UploadFunc, deleteFn DeleteFunc) *MountManager {
	return &MountManager{
		mounts:     make(map[string]*MountInfo),
		cache:      NewCache(),
		serverAddr: serverAddr,
		client:     client,
		downloadFn: downloadFn,
		uploadFn:   uploadFn,
		deleteFn:   deleteFn,
	}
}

// CleanupStaleDrive odpojí starý síťový disk z předchozí session (Windows).
func CleanupStaleDrive(driveLetter string) {
	if driveLetter != "" {
		log.Printf("MountManager: cleaning up stale drive %s", driveLetter)
		unmapDrive(driveLetter)
	}
}

// MountFS připojí obecný VirtualFS. Na Linuxu FUSE, na Windows WebDAV.
func (m *MountManager) MountFS(id, name string, vfs VirtualFS) (*MountInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.mounts[id]; ok {
		return info, nil // už mountnutý
	}

	// Refresh listing předem
	if err := vfs.RefreshListing("/"); err != nil {
		log.Printf("MountManager: refresh listing for %s: %v", id, err)
	}

	info := &MountInfo{
		ShareID:   id,
		ShareName: name,
		CanWrite:  vfs.CanWrite(),
		fs:        vfs,
	}

	if runtime.GOOS == "linux" {
		mountPath := DefaultMountPath(m.serverAddr, name)
		fuseMount, err := MountFUSE(vfs, mountPath)
		if err != nil {
			return nil, fmt.Errorf("FUSE mount: %w", err)
		}
		info.Type = "fuse"
		info.Path = mountPath
		info.fuse = fuseMount
	} else {
		webdav, err := StartWebDAV(vfs)
		if err != nil {
			return nil, fmt.Errorf("WebDAV start: %w", err)
		}
		if runtime.GOOS == "windows" {
			if err := webdav.MapDrive(); err != nil {
				log.Printf("MountManager: drive mapping failed: %v (WebDAV still accessible at %s)", err, webdav.URL())
			} else if webdav.DriveLetter != "" {
				renameDrive(webdav.DriveLetter, "NORA - "+name)
			}
		}
		info.Type = "webdav"
		info.DriveLetter = webdav.DriveLetter
		info.Port = webdav.Port()
		if webdav.DriveLetter != "" {
			info.Path = webdav.DriveLetter
		} else {
			info.Path = webdav.URL()
		}
		info.webdav = webdav
	}

	m.mounts[id] = info
	log.Printf("MountManager: mounted %s (%s) at %s", name, info.Type, info.Path)
	return info, nil
}

// Mount připojí sdílený adresář. Na Linuxu FUSE, na Windows WebDAV.
func (m *MountManager) Mount(shareID, shareName string, canWrite bool) (*MountInfo, error) {
	shareFS := NewShareFS(shareID, shareName, m.serverAddr, m.client, m.cache, m.downloadFn, m.uploadFn, m.deleteFn)
	shareFS.SetCanWrite(canWrite)
	return m.MountFS(shareID, shareName, shareFS)
}

// MountPreferred připojí sdílený adresář s preferovaným drive letterem a portem (Windows remount).
func (m *MountManager) MountPreferred(shareID, shareName, preferredLetter string, preferredPort int, canWrite bool) (*MountInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.mounts[shareID]; ok {
		return info, nil
	}

	shareFS := NewShareFS(shareID, shareName, m.serverAddr, m.client, m.cache, m.downloadFn, m.uploadFn, m.deleteFn)
	shareFS.SetCanWrite(canWrite)

	if err := shareFS.RefreshListing("/"); err != nil {
		log.Printf("MountManager: refresh listing for %s: %v", shareID, err)
	}

	info := &MountInfo{
		ShareID:   shareID,
		ShareName: shareName,
		CanWrite:  canWrite,
		fs:        shareFS,
	}

	if runtime.GOOS == "linux" {
		mountPath := DefaultMountPath(m.serverAddr, shareName)
		fuseMount, err := MountFUSE(shareFS, mountPath)
		if err != nil {
			return nil, fmt.Errorf("FUSE mount: %w", err)
		}
		info.Type = "fuse"
		info.Path = mountPath
		info.fuse = fuseMount
	} else {
		webdav, err := StartWebDAVOnPort(shareFS, preferredPort)
		if err != nil {
			return nil, fmt.Errorf("WebDAV start: %w", err)
		}
		if runtime.GOOS == "windows" {
			if preferredLetter != "" {
				unmapDrive(preferredLetter)
			}
			if err := webdav.MapDrivePreferred(preferredLetter); err != nil {
				log.Printf("MountManager: drive mapping failed: %v (WebDAV still accessible at %s)", err, webdav.URL())
			} else if webdav.DriveLetter != "" {
				renameDrive(webdav.DriveLetter, "NORA - "+shareName)
			}
		}
		info.Type = "webdav"
		info.DriveLetter = webdav.DriveLetter
		info.Port = webdav.Port()
		if webdav.DriveLetter != "" {
			info.Path = webdav.DriveLetter
		} else {
			info.Path = webdav.URL()
		}
		info.webdav = webdav
	}

	m.mounts[shareID] = info
	log.Printf("MountManager: mounted %s (%s) at %s", shareName, info.Type, info.Path)
	return info, nil
}

// Unmount odpojí sdílený adresář.
func (m *MountManager) Unmount(shareID string) error {
	m.mu.Lock()
	info, ok := m.mounts[shareID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.mounts, shareID)
	m.mu.Unlock()

	var err error
	if info.fuse != nil {
		err = info.fuse.Unmount()
	}
	if info.webdav != nil {
		err = info.webdav.Stop()
	}
	log.Printf("MountManager: unmounted %s", info.ShareName)
	return err
}

// UnmountAll odpojí všechny mounty (při disconnect).
func (m *MountManager) UnmountAll() {
	m.mu.Lock()
	mounts := make(map[string]*MountInfo, len(m.mounts))
	for k, v := range m.mounts {
		mounts[k] = v
	}
	m.mounts = make(map[string]*MountInfo)
	m.mu.Unlock()

	for _, info := range mounts {
		if info.fuse != nil {
			info.fuse.Unmount()
		}
		if info.webdav != nil {
			info.webdav.Stop()
		}
	}
	log.Printf("MountManager: unmounted all")
}

// IsMounted vrátí true pokud je share mountnutý.
func (m *MountManager) IsMounted(shareID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.mounts[shareID]
	return ok
}

// GetMountInfo vrátí info o mountu (nil pokud není mountnutý).
func (m *MountManager) GetMountInfo(shareID string) *MountInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mounts[shareID]
}

// UpdateCanWrite aktualizuje write oprávnění na existujícím mountu.
func (m *MountManager) UpdateCanWrite(shareID string, canWrite bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.mounts[shareID]; ok {
		info.CanWrite = canWrite
		if info.fs != nil {
			info.fs.SetCanWrite(canWrite)
		}
	}
}

// GetAllMounts vrátí kopii všech aktivních mountů.
func (m *MountManager) GetAllMounts() []MountInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]MountInfo, 0, len(m.mounts))
	for _, info := range m.mounts {
		result = append(result, *info)
	}
	return result
}

// Cache vrátí odkaz na file cache.
func (m *MountManager) Cache() *Cache {
	return m.cache
}

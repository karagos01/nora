package mount

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

// WebDAVServer — lokální WebDAV server pro mapování jako síťový disk.
type WebDAVServer struct {
	server      *http.Server
	listener    net.Listener
	port        int
	vfs         VirtualFS
	DriveLetter string // Windows: mapované písmeno disku (např. "Z:")
}

// StartWebDAV spustí WebDAV server na localhost s automaticky přiřazeným portem.
func StartWebDAV(vfs VirtualFS) (*WebDAVServer, error) {
	return startWebDAVListener(vfs, "127.0.0.1:0")
}

// StartWebDAVOnPort zkusí spustit WebDAV na konkrétním portu (reuse z předchozí session).
// Pokud port není volný, fallback na random.
func StartWebDAVOnPort(vfs VirtualFS, port int) (*WebDAVServer, error) {
	if port > 0 {
		ws, err := startWebDAVListener(vfs, fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ws, nil
		}
		log.Printf("WebDAV: port %d busy, falling back to random", port)
	}
	return StartWebDAV(vfs)
}

func startWebDAVListener(vfs VirtualFS, addr string) (*WebDAVServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	davFS := &webdavFS{vfs: vfs}
	handler := &webdav.Handler{
		FileSystem: davFS,
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				log.Printf("WebDAV %s %s: %v", r.Method, r.URL.Path, err)
			} else {
				log.Printf("WebDAV %s %s OK", r.Method, r.URL.Path)
			}
		},
	}

	srv := &http.Server{
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	ws := &WebDAVServer{
		server:   srv,
		listener: listener,
		port:     port,
		vfs:      vfs,
	}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("WebDAV server error: %v", err)
		}
	}()

	log.Printf("WebDAV: started on http://127.0.0.1:%d/", port)
	return ws, nil
}

// Stop zastaví WebDAV server a odpojí síťový disk.
func (w *WebDAVServer) Stop() error {
	// Nejdřív odpojit drive letter (pokud namapovaný)
	if w.DriveLetter != "" {
		unmapDrive(w.DriveLetter)
		w.DriveLetter = ""
	}
	if w.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Printf("WebDAV: stopping on port %d", w.port)
		return w.server.Shutdown(ctx)
	}
	return nil
}

// MapDrive namapuje WebDAV server jako síťový disk na Windows.
func (w *WebDAVServer) MapDrive() error {
	letter, err := mapDrive(w.URL())
	if err != nil {
		return err
	}
	w.DriveLetter = letter
	log.Printf("WebDAV: mapped as %s", letter)
	return nil
}

// MapDrivePreferred namapuje WebDAV server s preferovaným drive letterem.
func (w *WebDAVServer) MapDrivePreferred(preferred string) error {
	letter, err := mapDrivePreferred(w.URL(), preferred)
	if err != nil {
		return err
	}
	w.DriveLetter = letter
	log.Printf("WebDAV: mapped as %s (preferred %s)", letter, preferred)
	return nil
}

// Port vrátí port na kterém WebDAV server poslouchá.
func (w *WebDAVServer) Port() int {
	return w.port
}

// URL vrátí URL WebDAV serveru.
func (w *WebDAVServer) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", w.port)
}

// --- WebDAV FileSystem implementace ---

type webdavFS struct {
	vfs VirtualFS
}

func (wfs *webdavFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if !wfs.vfs.CanWrite() {
		return os.ErrPermission
	}
	name = normalizePath(name)
	return wfs.vfs.MkDir(name)
}

func (wfs *webdavFS) RemoveAll(ctx context.Context, name string) error {
	log.Printf("WebDAV RemoveAll called: %s (canWrite=%v)", name, wfs.vfs.CanWrite())
	if !wfs.vfs.CanWrite() {
		log.Printf("WebDAV RemoveAll %s: denied (read-only)", name)
		return os.ErrPermission
	}
	name = normalizePath(name)
	parent, fileName := splitPath(name)
	if fileName == "" {
		return os.ErrPermission // nelze smazat root
	}
	if err := wfs.vfs.DeleteFile(parent, fileName); err != nil {
		log.Printf("WebDAV RemoveAll %s: %v", name, err)
		return err
	}
	log.Printf("WebDAV RemoveAll %s: OK", name)
	return nil
}

func (wfs *webdavFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

func (wfs *webdavFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = normalizePath(name)

	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
		if !wfs.vfs.CanWrite() {
			return nil, os.ErrPermission
		}
		// Write mode — staging temp soubor
		staging := StagingDir()
		if err := os.MkdirAll(staging, 0700); err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(staging, "upload-*")
		if err != nil {
			return nil, err
		}
		_, fileName := splitPath(name)
		return &webdavWriteFile{
			vfs:      wfs.vfs,
			path:     name,
			fileName: fileName,
			tmp:      tmp,
		}, nil
	}

	entry, err := wfs.vfs.Stat(name)
	if err != nil {
		return nil, os.ErrNotExist
	}

	if entry.IsDir {
		return &webdavDir{
			vfs:   wfs.vfs,
			path:  name,
			entry: entry,
		}, nil
	}

	return &webdavFile{
		vfs:   wfs.vfs,
		path:  name,
		entry: entry,
	}, nil
}

func (wfs *webdavFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	name = normalizePath(name)
	entry, err := wfs.vfs.Stat(name)
	if err != nil {
		return nil, os.ErrNotExist
	}
	return entryToFileInfo(entry, wfs.vfs.CanWrite()), nil
}

// --- webdavDir ---

type webdavDir struct {
	vfs   VirtualFS
	path  string
	entry *FSEntry
}

func (d *webdavDir) Close() error                                 { return nil }
func (d *webdavDir) Read(p []byte) (int, error)                   { return 0, os.ErrInvalid }
func (d *webdavDir) Write(p []byte) (int, error)                  { return 0, os.ErrPermission }
func (d *webdavDir) Seek(offset int64, whence int) (int64, error) { return 0, os.ErrInvalid }

func (d *webdavDir) Stat() (os.FileInfo, error) {
	return entryToFileInfo(d.entry, d.vfs.CanWrite()), nil
}

func (d *webdavDir) Readdir(count int) ([]os.FileInfo, error) {
	entries, err := d.vfs.ListDir(d.path)
	if err != nil {
		return nil, err
	}

	result := make([]os.FileInfo, 0, len(entries))
	for i := range entries {
		result = append(result, entryToFileInfo(&entries[i], d.vfs.CanWrite()))
	}

	if count > 0 && count < len(result) {
		return result[:count], nil
	}
	return result, nil
}

// --- webdavFile ---

type webdavFile struct {
	vfs   VirtualFS
	path  string
	entry *FSEntry
	rc    io.ReadSeekCloser
}

func (f *webdavFile) Close() error {
	if f.rc != nil {
		return f.rc.Close()
	}
	return nil
}

func (f *webdavFile) Read(p []byte) (int, error) {
	if err := f.ensureOpen(); err != nil {
		return 0, err
	}
	return f.rc.Read(p)
}

func (f *webdavFile) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *webdavFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.ensureOpen(); err != nil {
		return 0, err
	}
	return f.rc.Seek(offset, whence)
}

func (f *webdavFile) Stat() (os.FileInfo, error) {
	return entryToFileInfo(f.entry, f.vfs.CanWrite()), nil
}

func (f *webdavFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

// ContentType implementuje webdav.ContentTyper na File.
// Bez toho webdav handler volá Read() pro content sniffing → triggeruje download.
func (f *webdavFile) ContentType(ctx context.Context) (string, error) {
	ext := path.Ext(f.entry.Name)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	return ct, nil
}

// ETag implementuje webdav.ETager na File.
func (f *webdavFile) ETag(ctx context.Context) (string, error) {
	return fmt.Sprintf(`"%x-%x"`, f.entry.Size, f.entry.ModifiedAt.UnixNano()), nil
}

func (f *webdavFile) ensureOpen() error {
	if f.rc != nil {
		return nil
	}
	log.Printf("WebDAV: ensureOpen %s (size=%d)", f.path, f.entry.Size)
	rc, _, err := f.vfs.GetFile(f.path)
	if err != nil {
		log.Printf("WebDAV: ensureOpen %s FAILED: %v", f.path, err)
		return err
	}
	f.rc = rc
	return nil
}

// --- webdavWriteFile ---

type webdavWriteFile struct {
	vfs      VirtualFS
	path     string
	fileName string
	tmp      *os.File
	written  int64
	closed   bool
}

func (w *webdavWriteFile) Write(p []byte) (int, error) {
	n, err := w.tmp.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *webdavWriteFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.tmp.Close()

	tmpPath := w.tmp.Name()
	err := w.vfs.PutFile(w.path, tmpPath, w.written)
	os.Remove(tmpPath)
	return err
}

func (w *webdavWriteFile) Read(p []byte) (int, error)                   { return 0, os.ErrInvalid }
func (w *webdavWriteFile) Seek(offset int64, whence int) (int64, error) { return 0, os.ErrInvalid }
func (w *webdavWriteFile) Readdir(count int) ([]os.FileInfo, error)     { return nil, os.ErrInvalid }

func (w *webdavWriteFile) Stat() (os.FileInfo, error) {
	return &writeFileInfo{name: w.fileName, size: w.written}, nil
}

type writeFileInfo struct {
	name string
	size int64
}

func (fi *writeFileInfo) Name() string        { return fi.name }
func (fi *writeFileInfo) Size() int64         { return fi.size }
func (fi *writeFileInfo) Mode() os.FileMode   { return 0644 }
func (fi *writeFileInfo) ModTime() time.Time  { return time.Now() }
func (fi *writeFileInfo) IsDir() bool         { return false }
func (fi *writeFileInfo) Sys() interface{}    { return nil }

// --- os.FileInfo implementace ---

type fsEntryInfo struct {
	entry    *FSEntry
	canWrite bool
}

func entryToFileInfo(e *FSEntry, canWrite bool) os.FileInfo {
	return &fsEntryInfo{entry: e, canWrite: canWrite}
}

func (fi *fsEntryInfo) Name() string { return fi.entry.Name }

func (fi *fsEntryInfo) Size() int64 { return fi.entry.Size }

func (fi *fsEntryInfo) Mode() os.FileMode {
	if fi.entry.IsDir {
		if fi.canWrite {
			return 0755 | os.ModeDir
		}
		return 0555 | os.ModeDir
	}
	if fi.canWrite {
		return 0644
	}
	return 0444
}

func (fi *fsEntryInfo) ModTime() time.Time {
	if !fi.entry.ModifiedAt.IsZero() {
		return fi.entry.ModifiedAt
	}
	return time.Now()
}

func (fi *fsEntryInfo) IsDir() bool { return fi.entry.IsDir }

func (fi *fsEntryInfo) Sys() interface{} { return nil }

// ContentType implementuje webdav.ContentTyper na os.FileInfo.
// Bez toho webdav handler volá Read() na soubor pro content sniffing,
// což triggeruje download při pouhém PROPFIND.
func (fi *fsEntryInfo) ContentType(ctx context.Context) (string, error) {
	if fi.entry.IsDir {
		return "", webdav.ErrNotImplemented
	}
	ext := path.Ext(fi.entry.Name)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	return ct, nil
}

// --- Helpers ---

func normalizePath(p string) string {
	if p == "" || p == "/" {
		return "/"
	}
	p = strings.TrimSuffix(p, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

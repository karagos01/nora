//go:build linux

package mount

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// FuseMount drží stav FUSE mountu.
type FuseMount struct {
	server    *fuse.Server
	mountPath string
	vfs       VirtualFS
}

// MountFUSE připojí virtuální filesystem jako FUSE mount.
func MountFUSE(vfs VirtualFS, mountPath string) (*FuseMount, error) {
	if err := os.MkdirAll(mountPath, 0700); err != nil {
		return nil, err
	}

	root := &fuseRoot{vfs: vfs}

	server, err := fs.Mount(mountPath, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			FsName: "nora-share",
			Name:   "nora",
		},
	})
	if err != nil {
		return nil, err
	}

	log.Printf("FUSE: mounted %s at %s", vfs.FSName(), mountPath)

	return &FuseMount{
		server:    server,
		mountPath: mountPath,
		vfs:       vfs,
	}, nil
}

// Unmount odpojí FUSE mount.
func (m *FuseMount) Unmount() error {
	if m.server != nil {
		err := m.server.Unmount()
		log.Printf("FUSE: unmounted %s", m.mountPath)
		return err
	}
	return nil
}

// Path vrátí cestu mountu.
func (m *FuseMount) Path() string {
	return m.mountPath
}

// --- FUSE nodes ---

// fuseRoot — kořenový uzel FUSE filesystem.
type fuseRoot struct {
	fs.Inode
	vfs VirtualFS
}

var _ = (fs.NodeReaddirer)((*fuseRoot)(nil))
var _ = (fs.NodeLookuper)((*fuseRoot)(nil))
var _ = (fs.NodeUnlinker)((*fuseRoot)(nil))

func (r *fuseRoot) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return readdir(r.vfs, "/")
}

func (r *fuseRoot) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return lookup(r, r.vfs, "/", name, out)
}

// fuseDir — adresářový uzel.
type fuseDir struct {
	fs.Inode
	vfs  VirtualFS
	path string
}

var _ = (fs.NodeReaddirer)((*fuseDir)(nil))
var _ = (fs.NodeLookuper)((*fuseDir)(nil))
var _ = (fs.NodeGetattrer)((*fuseDir)(nil))
var _ = (fs.NodeCreater)((*fuseDir)(nil))
var _ = (fs.NodeMkdirer)((*fuseDir)(nil))
var _ = (fs.NodeUnlinker)((*fuseDir)(nil))

func (d *fuseDir) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return readdir(d.vfs, d.path)
}

func (d *fuseDir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return lookup(d, d.vfs, d.path, name, out)
}

func (d *fuseDir) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if d.vfs.CanWrite() {
		out.Mode = 0755 | syscall.S_IFDIR
	} else {
		out.Mode = 0555 | syscall.S_IFDIR
	}
	return 0
}

func (d *fuseDir) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (node *fs.Inode, fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	if !d.vfs.CanWrite() {
		return nil, nil, 0, syscall.EACCES
	}
	return fuseCreate(d, d.vfs, d.path, ctx, name, out)
}

func (d *fuseDir) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if !d.vfs.CanWrite() {
		return nil, syscall.EACCES
	}
	return fuseMkdir(d, d.vfs, d.path, ctx, name, out)
}

func (d *fuseDir) Unlink(ctx context.Context, name string) syscall.Errno {
	if !d.vfs.CanWrite() {
		return syscall.EACCES
	}
	if err := d.vfs.DeleteFile(d.path, name); err != nil {
		log.Printf("FUSE unlink %s/%s: %v", d.path, name, err)
		return syscall.EIO
	}
	return 0
}

// fuseFile — souborový uzel.
type fuseFile struct {
	fs.Inode
	vfs  VirtualFS
	path string // plná cesta v share (relativní)
	size int64
}

var _ = (fs.NodeGetattrer)((*fuseFile)(nil))
var _ = (fs.NodeOpener)((*fuseFile)(nil))
var _ = (fs.NodeReader)((*fuseFile)(nil))

func (f *fuseFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if f.vfs.CanWrite() {
		out.Mode = 0644 | syscall.S_IFREG
	} else {
		out.Mode = 0444 | syscall.S_IFREG
	}
	out.Size = uint64(f.size)
	return 0
}

func (f *fuseFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, fuse.FOPEN_KEEP_CACHE, 0
}

func (f *fuseFile) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	rc, _, err := f.vfs.GetFile(f.path)
	if err != nil {
		log.Printf("FUSE read %s: %v", f.path, err)
		return nil, syscall.EIO
	}
	defer rc.Close()

	if _, err := rc.Seek(off, 0); err != nil {
		return nil, syscall.EIO
	}

	buf := make([]byte, len(dest))
	n, err := rc.Read(buf)
	if n == 0 && err != nil {
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(buf[:n]), 0
}

// --- fuseRoot write support ---

var _ = (fs.NodeCreater)((*fuseRoot)(nil))
var _ = (fs.NodeMkdirer)((*fuseRoot)(nil))

func (r *fuseRoot) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (node *fs.Inode, fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	if !r.vfs.CanWrite() {
		return nil, nil, 0, syscall.EACCES
	}
	return fuseCreate(r, r.vfs, "/", ctx, name, out)
}

func (r *fuseRoot) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if !r.vfs.CanWrite() {
		return nil, syscall.EACCES
	}
	return fuseMkdir(r, r.vfs, "/", ctx, name, out)
}

func (r *fuseRoot) Unlink(ctx context.Context, name string) syscall.Errno {
	if !r.vfs.CanWrite() {
		return syscall.EACCES
	}
	if err := r.vfs.DeleteFile("/", name); err != nil {
		log.Printf("FUSE unlink /%s: %v", name, err)
		return syscall.EIO
	}
	return 0
}

// --- fuseWriteNode — uzel pro zápis souboru ---

type fuseWriteNode struct {
	fs.Inode
	vfs  VirtualFS
	path string
	name string
}

var _ = (fs.NodeGetattrer)((*fuseWriteNode)(nil))

func (n *fuseWriteNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0644 | syscall.S_IFREG
	return 0
}

// --- fuseWriteHandle — file handle pro zápis ---

type fuseWriteHandle struct {
	vfs     VirtualFS
	path    string
	name    string
	tmp     *os.File
	written int64
}

var _ = (fs.FileWriter)((*fuseWriteHandle)(nil))
var _ = (fs.FileReleaser)((*fuseWriteHandle)(nil))

func (h *fuseWriteHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	n, err := h.tmp.WriteAt(data, off)
	if err != nil {
		return 0, syscall.EIO
	}
	newEnd := off + int64(n)
	if newEnd > h.written {
		h.written = newEnd
	}
	return uint32(n), 0
}

func (h *fuseWriteHandle) Release(ctx context.Context) syscall.Errno {
	h.tmp.Close()
	tmpPath := h.tmp.Name()

	if err := h.vfs.PutFile(h.path, tmpPath, h.written); err != nil {
		log.Printf("FUSE write release %s: %v", h.path, err)
		os.Remove(tmpPath)
		return syscall.EIO
	}
	os.Remove(tmpPath)
	return 0
}

// --- Společné helpery ---

func fuseCreate(parent fs.InodeEmbedder, vfs VirtualFS, parentPath string, ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	staging := StagingDir()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return nil, nil, 0, syscall.EIO
	}
	tmp, err := os.CreateTemp(staging, "upload-*")
	if err != nil {
		return nil, nil, 0, syscall.EIO
	}

	var childPath string
	if parentPath == "/" {
		childPath = "/" + name
	} else {
		childPath = parentPath + "/" + name
	}

	child := &fuseWriteNode{
		vfs:  vfs,
		path: childPath,
		name: name,
	}
	out.Mode = 0644 | syscall.S_IFREG

	inode := parent.EmbeddedInode().NewInode(ctx, child, fs.StableAttr{Mode: syscall.S_IFREG})

	handle := &fuseWriteHandle{
		vfs:  vfs,
		path: childPath,
		name: name,
		tmp:  tmp,
	}

	return inode, handle, fuse.FOPEN_DIRECT_IO, 0
}

func fuseMkdir(parent fs.InodeEmbedder, vfs VirtualFS, parentPath string, ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	var childPath string
	if parentPath == "/" {
		childPath = "/" + name
	} else {
		childPath = parentPath + "/" + name
	}

	if err := vfs.MkDir(childPath); err != nil {
		return nil, syscall.EIO
	}

	child := &fuseDir{
		vfs:  vfs,
		path: childPath,
	}
	out.Mode = 0755 | syscall.S_IFDIR

	return parent.EmbeddedInode().NewInode(ctx, child, fs.StableAttr{Mode: syscall.S_IFDIR}), 0
}

func readdir(vfs VirtualFS, path string) (fs.DirStream, syscall.Errno) {
	entries, err := vfs.ListDir(path)
	if err != nil {
		return nil, syscall.EIO
	}

	result := make([]fuse.DirEntry, 0, len(entries))
	for _, e := range entries {
		mode := uint32(syscall.S_IFREG)
		if e.IsDir {
			mode = syscall.S_IFDIR
		}
		result = append(result, fuse.DirEntry{
			Name: e.Name,
			Mode: mode,
		})
	}
	return fs.NewListDirStream(result), 0
}

func lookup(parent fs.InodeEmbedder, vfs VirtualFS, parentPath, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	var childPath string
	if parentPath == "/" {
		childPath = "/" + name
	} else {
		childPath = parentPath + "/" + name
	}

	entry, err := vfs.Stat(childPath)
	if err != nil {
		return nil, syscall.ENOENT
	}

	if entry.IsDir {
		child := &fuseDir{
			vfs:  vfs,
			path: childPath,
		}
		if vfs.CanWrite() {
			out.Mode = 0755 | syscall.S_IFDIR
		} else {
			out.Mode = 0555 | syscall.S_IFDIR
		}
		return parent.EmbeddedInode().NewInode(context.Background(), child, fs.StableAttr{Mode: syscall.S_IFDIR}), 0
	}

	child := &fuseFile{
		vfs:  vfs,
		path: childPath,
		size: entry.Size,
	}
	if vfs.CanWrite() {
		out.Mode = 0644 | syscall.S_IFREG
	} else {
		out.Mode = 0444 | syscall.S_IFREG
	}
	out.Size = uint64(entry.Size)

	// Nastavit ModifiedAt pokud je dostupný
	if !entry.ModifiedAt.IsZero() {
		t := entry.ModifiedAt
		out.SetTimes(nil, &t, nil)
	}

	return parent.EmbeddedInode().NewInode(context.Background(), child, fs.StableAttr{Mode: syscall.S_IFREG}), 0
}

// DefaultMountPath vrátí výchozí cestu pro FUSE mount.
func DefaultMountPath(serverAddr, shareName string) string {
	home, _ := os.UserHomeDir()
	safe := strings.ReplaceAll(serverAddr, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")
	safeName := filepath.Clean(shareName)
	return filepath.Join(home, ".nora", "mounts", safe, safeName)
}

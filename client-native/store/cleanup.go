package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiskUsage struct {
	DMHistory    int64
	GroupHistory int64
	Sounds       int64
	Cache        int64
	Other        int64 // identities.json, save_dirs.json, wg-helper-token
	Total        int64
}

// ScanDiskUsage spočítá velikost souborů v ~/.nora/.
func ScanDiskUsage() DiskUsage {
	dir := noraDir()
	var u DiskUsage

	entries, err := os.ReadDir(dir)
	if err != nil {
		return u
	}

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			size := dirSize(path)
			switch e.Name() {
			case "sounds":
				u.Sounds = size
			case "cache":
				u.Cache = size
			default:
				u.Other += size
			}
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		sz := info.Size()

		name := e.Name()
		switch {
		case strings.HasPrefix(name, "dm_history_"):
			u.DMHistory += sz
		case strings.HasPrefix(name, "group_history_"):
			u.GroupHistory += sz
		default:
			u.Other += sz
		}
	}

	u.Total = u.DMHistory + u.GroupHistory + u.Sounds + u.Cache + u.Other
	return u
}

func dirSize(path string) int64 {
	var size int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}

type cachedFile struct {
	path    string
	size    int64
	modTime int64 // UnixNano
}

// CleanupCache maže nejstarší soubory z ~/.nora/cache/ dokud celková velikost nepřesáhne maxBytes.
func CleanupCache(maxBytes int64) (freedBytes int64, err error) {
	cacheDir := filepath.Join(noraDir(), "cache")

	var files []cachedFile
	var totalSize int64

	err = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sz := info.Size()
		totalSize += sz
		files = append(files, cachedFile{path: path, size: sz, modTime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return 0, err
	}

	if totalSize <= maxBytes {
		return 0, nil
	}

	// Seřadit oldest-first
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime < files[j].modTime
	})

	for _, f := range files {
		if totalSize <= maxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			totalSize -= f.size
			freedBytes += f.size
		}
	}

	// Odstranit prázdné adresáře (os.Remove selže na neprázdných — to je OK)
	filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == cacheDir {
			return nil
		}
		os.Remove(path)
		return nil
	})

	return freedBytes, nil
}

// CleanupCacheAll smaže celý ~/.nora/cache/ a znovu vytvoří prázdný adresář.
func CleanupCacheAll() error {
	cacheDir := filepath.Join(noraDir(), "cache")
	if err := os.RemoveAll(cacheDir); err != nil {
		return err
	}
	return os.MkdirAll(cacheDir, 0700)
}

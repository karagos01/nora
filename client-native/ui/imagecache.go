package ui

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"gioui.org/op/paint"

	_ "golang.org/x/image/webp"
)

// ImageCache downloads and caches images from server URLs.
type ImageCache struct {
	mu      sync.RWMutex
	images  map[string]*cachedImage
	pending map[string]bool
}

type cachedImage struct {
	img image.Image
	op  paint.ImageOp
	ok  bool // false = failed to load
}

func NewImageCache() *ImageCache {
	return &ImageCache{
		images:  make(map[string]*cachedImage),
		pending: make(map[string]bool),
	}
}

// Get returns the cached image op for the given URL, or nil if not yet loaded.
// If not cached and not pending, starts a background download.
func (c *ImageCache) Get(url string, invalidate func()) *cachedImage {
	c.mu.RLock()
	if ci, ok := c.images[url]; ok {
		c.mu.RUnlock()
		return ci
	}
	c.mu.RUnlock()

	c.mu.Lock()
	// Double-check
	if ci, ok := c.images[url]; ok {
		c.mu.Unlock()
		return ci
	}
	if c.pending[url] {
		c.mu.Unlock()
		return nil
	}
	c.pending[url] = true
	c.mu.Unlock()

	go func() {
		ci := c.download(url)
		c.mu.Lock()
		c.images[url] = ci
		delete(c.pending, url)
		c.mu.Unlock()
		if invalidate != nil {
			invalidate()
		}
	}()

	return nil
}

func (c *ImageCache) download(url string) *cachedImage {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("ImageCache: download failed %s: %v", url, err)
		return &cachedImage{ok: false}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("ImageCache: HTTP %d for %s", resp.StatusCode, url)
		io.Copy(io.Discard, resp.Body)
		return &cachedImage{ok: false}
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		log.Printf("ImageCache: decode failed %s: %v", url, err)
		return &cachedImage{ok: false}
	}

	return &cachedImage{
		img: img,
		op:  paint.NewImageOp(img),
		ok:  true,
	}
}

// InvalidatePrefix removes all cached entries whose URL contains the given substring.
func (c *ImageCache) InvalidatePrefix(substring string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for url := range c.images {
		if strings.Contains(url, substring) {
			delete(c.images, url)
		}
	}
	for url := range c.pending {
		if strings.Contains(url, substring) {
			delete(c.pending, url)
		}
	}
}

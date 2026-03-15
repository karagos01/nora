package ui

import (
	"fmt"
	"image"
	"image/draw"
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
	xdraw "golang.org/x/image/draw"
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

	// Convert to NRGBA for consistent GPU rendering (paletted PNGs can cause issues)
	if _, ok := img.(*image.NRGBA); !ok {
		b := img.Bounds()
		nrgba := image.NewNRGBA(b)
		draw.Draw(nrgba, b, img, b.Min, draw.Over)
		img = nrgba
	}

	return &cachedImage{
		img: img,
		op:  paint.NewImageOp(img),
		ok:  true,
	}
}

// GetScaled returns a paint.ImageOp for a pre-scaled version of the image.
// The scaled version is cached by URL + target size to avoid per-frame allocation.
func (c *ImageCache) GetScaled(url string, w, h int, src image.Image) paint.ImageOp {
	key := fmt.Sprintf("%s@%dx%d", url, w, h)
	c.mu.RLock()
	if ci, ok := c.images[key]; ok {
		c.mu.RUnlock()
		return ci.op
	}
	c.mu.RUnlock()

	// Pre-scale on CPU using high-quality BiLinear interpolation
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	imgOp := paint.NewImageOp(dst)

	c.mu.Lock()
	c.images[key] = &cachedImage{img: dst, op: imgOp, ok: true}
	c.mu.Unlock()

	return imgOp
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

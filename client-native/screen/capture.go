package screen

import (
	"bytes"
	"image"
	"image/jpeg"

	"github.com/kbinani/screenshot"
)

// CaptureRaw captures the primary display, downscales to maxW x maxH,
// and returns raw RGBA bytes with even dimensions (ffmpeg requires it).
func CaptureRaw(maxW, maxH int) (rgba []byte, width, height int, err error) {
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, 0, 0, err
	}

	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	// Downscale if needed
	newW, newH := w, h
	if w > maxW || h > maxH {
		scaleW := float64(maxW) / float64(w)
		scaleH := float64(maxH) / float64(h)
		scale := scaleW
		if scaleH < scale {
			scale = scaleH
		}
		newW = int(float64(w) * scale)
		newH = int(float64(h) * scale)
		if newW < 1 {
			newW = 1
		}
		if newH < 1 {
			newH = 1
		}
	}

	// Align to even dimensions (ffmpeg H.264 requires it)
	newW &^= 1
	newH &^= 1
	if newW < 2 {
		newW = 2
	}
	if newH < 2 {
		newH = 2
	}

	// Create output buffer
	out := make([]byte, newW*newH*4)
	srcStride := img.Stride
	srcPix := img.Pix
	minX := img.Bounds().Min.X
	minY := img.Bounds().Min.Y

	for y := 0; y < newH; y++ {
		srcY := y*h/newH + minY
		for x := 0; x < newW; x++ {
			srcX := x*w/newW + minX
			si := srcY*srcStride + srcX*4
			di := (y*newW + x) * 4
			out[di] = srcPix[si]
			out[di+1] = srcPix[si+1]
			out[di+2] = srcPix[si+2]
			out[di+3] = srcPix[si+3]
		}
	}

	return out, newW, newH, nil
}

// CaptureJPEG captures the primary display, downscales to maxW x maxH,
// and encodes as JPEG with the given quality (1-100).
func CaptureJPEG(maxW, maxH, quality int) ([]byte, error) {
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, err
	}

	var encoded image.Image = img
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	if w > maxW || h > maxH {
		scaleW := float64(maxW) / float64(w)
		scaleH := float64(maxH) / float64(h)
		scale := scaleW
		if scaleH < scale {
			scale = scaleH
		}
		newW := int(float64(w) * scale)
		newH := int(float64(h) * scale)
		if newW < 1 {
			newW = 1
		}
		if newH < 1 {
			newH = 1
		}

		scaled := image.NewRGBA(image.Rect(0, 0, newW, newH))
		srcStride := img.Stride
		dstStride := scaled.Stride
		srcPix := img.Pix
		dstPix := scaled.Pix
		minX := img.Bounds().Min.X
		minY := img.Bounds().Min.Y

		for y := 0; y < newH; y++ {
			srcY := y*h/newH + minY
			for x := 0; x < newW; x++ {
				srcX := x*w/newW + minX
				si := srcY*srcStride + srcX*4
				di := y*dstStride + x*4
				dstPix[di] = srcPix[si]
				dstPix[di+1] = srcPix[si+1]
				dstPix[di+2] = srcPix[si+2]
				dstPix[di+3] = srcPix[si+3]
			}
		}
		encoded = scaled
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, encoded, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

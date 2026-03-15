package ui

import (
	"image"
	"image/color"
	"log"
	"strings"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/video"
)

// isVideoMIME vrátí true pro video/* MIME typy.
func isVideoMIME(mime string) bool {
	return strings.HasPrefix(mime, "video/")
}

// videoThumbnailCache drží generované thumbnaily (NRGBA frames z ffmpeg).
var videoThumbnailCache struct {
	mu      sync.RWMutex
	thumbs  map[string]*videoThumb
	pending map[string]bool
}

type videoThumb struct {
	img *image.NRGBA
	op  paint.ImageOp
	ok  bool
}

func init() {
	videoThumbnailCache.thumbs = make(map[string]*videoThumb)
	videoThumbnailCache.pending = make(map[string]bool)
}

// getVideoThumbnail vrátí thumbnail pro video URL, nebo nil pokud ještě není ready.
func getVideoThumbnail(url string, invalidate func()) *videoThumb {
	videoThumbnailCache.mu.RLock()
	if t, ok := videoThumbnailCache.thumbs[url]; ok {
		videoThumbnailCache.mu.RUnlock()
		return t
	}
	videoThumbnailCache.mu.RUnlock()

	videoThumbnailCache.mu.Lock()
	if t, ok := videoThumbnailCache.thumbs[url]; ok {
		videoThumbnailCache.mu.Unlock()
		return t
	}
	if videoThumbnailCache.pending[url] {
		videoThumbnailCache.mu.Unlock()
		return nil
	}
	videoThumbnailCache.pending[url] = true
	videoThumbnailCache.mu.Unlock()

	go func() {
		img := video.GenerateThumbnail(url)
		t := &videoThumb{ok: false}
		if img != nil {
			t.img = img
			t.op = paint.NewImageOp(img)
			t.ok = true
		}

		videoThumbnailCache.mu.Lock()
		videoThumbnailCache.thumbs[url] = t
		delete(videoThumbnailCache.pending, url)
		videoThumbnailCache.mu.Unlock()

		if invalidate != nil {
			invalidate()
		}
	}()

	return nil
}

// layoutVideoPreview renderuje video thumbnail s play ikonou overlay.
func (v *MessageView) layoutVideoPreview(gtx layout.Context, msgIdx, attIdx int, att api.Attachment, serverURL string) layout.Dimensions {
	url := serverURL + att.URL
	btn := &v.actions[msgIdx].attBtns[attIdx]

	thumb := getVideoThumbnail(url, func() { v.app.Window.Invalidate() })

	if thumb == nil {
		// Loading
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Loading "+att.Filename+"...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}

	if !thumb.ok {
		// Thumbnail failed — zobrazit jako link
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconVideocam, 16, ColorAccent)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+") — click to play")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		})
	}

	// Thumbnail s play overlay
	imgBounds := thumb.img.Bounds()
	imgW := imgBounds.Dx()
	imgH := imgBounds.Dy()
	maxW := gtx.Dp(400)
	maxH := gtx.Dp(300)

	if imgW > maxW {
		imgH = imgH * maxW / imgW
		imgW = maxW
	}
	if imgH > maxH {
		imgW = imgW * maxH / imgH
		imgH = maxH
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				origW := float32(thumb.img.Bounds().Dx())
				origH := float32(thumb.img.Bounds().Dy())
				scaleX := float32(imgW) / origW
				scaleY := float32(imgH) / origH

				rr := gtx.Dp(6)
				defer clip.RRect{
					Rect: image.Rect(0, 0, imgW, imgH),
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Push(gtx.Ops).Pop()

				// Thumbnail
				func() {
					defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()
					thumb.op.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
				}()

				// Semi-transparentní overlay
				paint.FillShape(gtx.Ops, color.NRGBA{A: 80}, clip.Rect{Max: image.Pt(imgW, imgH)}.Op())

				// Play trojúhelník uprostřed
				cx := imgW / 2
				cy := imgH / 2
				triR := gtx.Dp(20)

				// Tmavý kruh
				circR := triR + gtx.Dp(8)
				circRect := image.Rect(cx-circR, cy-circR, cx+circR, cy+circR)
				paint.FillShape(gtx.Ops, color.NRGBA{A: 160}, clip.Ellipse{Min: circRect.Min, Max: circRect.Max}.Op(gtx.Ops))

				// Play ikona
				iconSize := unit.Dp(32)
				sz := gtx.Dp(iconSize)
				iconOff := op.Offset(image.Pt(cx-sz/2+gtx.Dp(2), cy-sz/2)).Push(gtx.Ops)
				layoutIcon(gtx, IconPlayArrow, iconSize, color.NRGBA{R: 255, G: 255, B: 255, A: 230})
				iconOff.Pop()

				return layout.Dimensions{Size: image.Pt(imgW, imgH)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+")")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
		)
	})
}

// YouTubeEmbedState drží stav pro YouTube embed v rámci zprávy.
type YouTubeEmbedState struct {
	btn     widget.Clickable
	videoID string
	loading bool
	failed  bool
}

// youtubeEmbedCache — cache pro YouTube embed stavy per message
var youtubeEmbedStates struct {
	mu     sync.Mutex
	states map[string]*YouTubeEmbedState // key = messageID
}

func init() {
	youtubeEmbedStates.states = make(map[string]*YouTubeEmbedState)
}

func getYouTubeEmbedState(msgID, videoID string) *YouTubeEmbedState {
	youtubeEmbedStates.mu.Lock()
	defer youtubeEmbedStates.mu.Unlock()
	if s, ok := youtubeEmbedStates.states[msgID]; ok {
		return s
	}
	s := &YouTubeEmbedState{videoID: videoID}
	youtubeEmbedStates.states[msgID] = s
	return s
}

// layoutYouTubeEmbed renderuje YouTube thumbnail + play button pod zprávou.
func layoutYouTubeEmbed(gtx layout.Context, app *App, msgID, ytURL string) layout.Dimensions {
	videoID := video.YouTubeVideoID(ytURL)
	if videoID == "" {
		return layout.Dimensions{}
	}

	state := getYouTubeEmbedState(msgID, videoID)

	// Click handler
	if state.btn.Clicked(gtx) && !state.loading {
		state.loading = true
		go func() {
			info, err := video.FetchYouTubeInfo(videoID)
			if err != nil {
				log.Printf("video: youtube fetch failed: %v", err)
				state.failed = true
				state.loading = false
				// Fallback na browser
				openURL(ytURL)
				app.Window.Invalidate()
				return
			}
			state.loading = false
			app.VideoPlayer.Play(info.StreamURL, info.Title)
			app.Window.Invalidate()
		}()
	}

	// YouTube thumbnail URL
	thumbURL := video.YouTubeThumbnailURL(videoID)
	ci := app.Images.Get(thumbURL, func() { app.Window.Invalidate() })

	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if ci == nil {
			// Loading thumbnail
			return state.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconVideocam, 16, ColorDanger)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(app.Theme.Material, "YouTube — loading...")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		}

		if !ci.ok {
			// Thumbnail failed — jen text link
			return state.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconVideocam, 16, ColorDanger)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(app.Theme.Material, "YouTube — click to play")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		}

		// Thumbnail s play overlay
		imgBounds := ci.img.Bounds()
		imgW := imgBounds.Dx()
		imgH := imgBounds.Dy()
		maxW := gtx.Dp(360)
		maxH := gtx.Dp(200)

		if imgW > maxW {
			imgH = imgH * maxW / imgW
			imgW = maxW
		}
		if imgH > maxH {
			imgW = imgW * maxH / imgH
			imgH = maxH
		}

		return state.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					origW := float32(ci.img.Bounds().Dx())
					origH := float32(ci.img.Bounds().Dy())
					scaleX := float32(imgW) / origW
					scaleY := float32(imgH) / origH

					rr := gtx.Dp(6)
					defer clip.RRect{
						Rect: image.Rect(0, 0, imgW, imgH),
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Push(gtx.Ops).Pop()

					// Thumbnail
					func() {
						defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()
						ci.op.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
					}()

					// Semi-transparentní overlay
					paint.FillShape(gtx.Ops, color.NRGBA{A: 60}, clip.Rect{Max: image.Pt(imgW, imgH)}.Op())

					// Play ikona
					cx := imgW / 2
					cy := imgH / 2
					circR := gtx.Dp(24)
					circRect := image.Rect(cx-circR, cy-circR, cx+circR, cy+circR)
					paint.FillShape(gtx.Ops, color.NRGBA{R: 255, A: 200}, clip.Ellipse{Min: circRect.Min, Max: circRect.Max}.Op(gtx.Ops))

					iconSize := unit.Dp(32)
					sz := gtx.Dp(iconSize)
					iconOff := op.Offset(image.Pt(cx-sz/2+gtx.Dp(2), cy-sz/2)).Push(gtx.Ops)
					layoutIcon(gtx, IconPlayArrow, iconSize, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
					iconOff.Pop()

					// "YouTube" badge vlevo nahoře
					badgeOff := op.Offset(image.Pt(gtx.Dp(6), gtx.Dp(6))).Push(gtx.Ops)
					func() {
						lbl := material.Caption(app.Theme.Material, "YouTube")
						lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 220}
						lbl.Layout(gtx)
					}()
					badgeOff.Pop()

					// Loading indicator
					if state.loading {
						loadOff := op.Offset(image.Pt(gtx.Dp(6), imgH-gtx.Dp(20))).Push(gtx.Ops)
						func() {
							lbl := material.Caption(app.Theme.Material, "Loading stream...")
							lbl.Color = color.NRGBA{R: 255, G: 255, B: 100, A: 255}
							lbl.Layout(gtx)
						}()
						loadOff.Pop()
					}

					return layout.Dimensions{Size: image.Pt(imgW, imgH)}
				}),
			)
		})
	})
}

// findYouTubeURLInText hledá první YouTube URL v textu zprávy.
func findYouTubeURLInText(text string) string {
	for _, prefix := range []string{"https://www.youtube.com/watch?v=", "https://youtube.com/watch?v=", "https://youtu.be/", "https://www.youtube.com/shorts/", "http://www.youtube.com/watch?v=", "http://youtu.be/"} {
		idx := strings.Index(text, prefix)
		if idx == -1 {
			continue
		}
		end := strings.IndexAny(text[idx:], " \n\t")
		if end == -1 {
			return text[idx:]
		}
		return text[idx : idx+end]
	}
	return ""
}

func findURLInText(text string) string {
	for _, prefix := range []string{"https://", "http://"} {
		idx := strings.Index(text, prefix)
		if idx == -1 {
			continue
		}
		end := strings.IndexAny(text[idx:], " \n\t")
		if end == -1 {
			return text[idx:]
		}
		return text[idx : idx+end]
	}
	return ""
}

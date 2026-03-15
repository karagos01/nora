package ui

import (
	"image"
	"image/color"
	"log"
	"strings"
	"sync"

	"gioui.org/f32"
	"gioui.org/io/pointer"
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

// isVideoMIME returns true for video/* MIME types.
func isVideoMIME(mime string) bool {
	return strings.HasPrefix(mime, "video/")
}

// videoThumbnailCache holds generated thumbnails (NRGBA frames from ffmpeg).
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

// getVideoThumbnail returns a thumbnail for the video URL, or nil if not yet ready.
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

// layoutVideoPreview renders a video thumbnail with a play icon overlay.
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
		// Thumbnail failed — display as a link
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if btn.Hovered() {
				pointer.CursorPointer.Add(gtx.Ops)
			}
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconPlayArrow, 16, ColorAccent)
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

	// Thumbnail with play overlay
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

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if btn.Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
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

				// Semi-transparent overlay
				paint.FillShape(gtx.Ops, color.NRGBA{A: 80}, clip.Rect{Max: image.Pt(imgW, imgH)}.Op())

				// Dark circle in center
				cx := float32(imgW) / 2
				cy := float32(imgH) / 2
				circR := float32(gtx.Dp(28))
				circRect := image.Rect(int(cx-circR), int(cy-circR), int(cx+circR), int(cy+circR))
				paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Ellipse{Min: circRect.Min, Max: circRect.Max}.Op(gtx.Ops))

				// Play triangle (drawn as path — more reliable than icon image)
				triR := float32(gtx.Dp(14))
				var p clip.Path
				p.Begin(gtx.Ops)
				p.MoveTo(f32.Pt(cx-triR*0.6, cy-triR))
				p.LineTo(f32.Pt(cx+triR, cy))
				p.LineTo(f32.Pt(cx-triR*0.6, cy+triR))
				p.Close()
				paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255},
					clip.Outline{Path: p.End()}.Op())

				return layout.Dimensions{Size: image.Pt(imgW, imgH)}
			})
		}),
		// Filename + Save button row
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+")")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if attIdx >= len(v.actions[msgIdx].vidSaveBtns) {
						return layout.Dimensions{}
					}
					saveBtn := &v.actions[msgIdx].vidSaveBtns[attIdx]
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return saveBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							if saveBtn.Hovered() {
								pointer.CursorPointer.Add(gtx.Ops)
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconSave, 14, ColorAccent)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, "Save")
										lbl.Color = ColorAccent
										if saveBtn.Hovered() {
											lbl.Color = ColorAccentHover
										}
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					})
				}),
			)
		}),
	)
}

// YouTubeEmbedState holds the state for a YouTube embed within a message.
type YouTubeEmbedState struct {
	btn       widget.Clickable
	videoID   string
	loading   bool
	failed    bool
	info      *video.YouTubeInfo // prefetched info (nil until ready)
	fetching  bool               // true while prefetch is running
}

// youtubeEmbedCache — cache for YouTube embed states per message
var youtubeEmbedStates struct {
	mu     sync.Mutex
	states map[string]*YouTubeEmbedState // key = messageID
}

func init() {
	youtubeEmbedStates.states = make(map[string]*YouTubeEmbedState)
}

func getYouTubeEmbedState(msgID, videoID string, invalidate func()) *YouTubeEmbedState {
	youtubeEmbedStates.mu.Lock()
	defer youtubeEmbedStates.mu.Unlock()
	if s, ok := youtubeEmbedStates.states[msgID]; ok {
		return s
	}
	s := &YouTubeEmbedState{videoID: videoID, fetching: true}
	youtubeEmbedStates.states[msgID] = s
	// Prefetch info in background so click is instant
	go func() {
		info, err := video.FetchYouTubeInfo(videoID)
		if err != nil {
			log.Printf("video: youtube prefetch failed: %v", err)
			s.fetching = false
			if invalidate != nil {
				invalidate()
			}
			return
		}
		s.info = info
		s.fetching = false
		if invalidate != nil {
			invalidate()
		}
	}()
	return s
}

// layoutYouTubeEmbed renders a YouTube thumbnail + play button below the message.
func layoutYouTubeEmbed(gtx layout.Context, app *App, msgID, ytURL string) layout.Dimensions {
	videoID := video.YouTubeVideoID(ytURL)
	if videoID == "" {
		return layout.Dimensions{}
	}

	state := getYouTubeEmbedState(msgID, videoID, func() { app.Window.Invalidate() })

	// Click handler
	if state.btn.Clicked(gtx) && !state.loading {
		if state.info != nil {
			// Prefetched — play immediately
			app.VideoPlayer.PlayYouTube(state.info)
		} else if !state.fetching {
			// Prefetch failed — fetch now
			state.loading = true
			go func() {
				info, err := video.FetchYouTubeInfo(videoID)
				if err != nil {
					log.Printf("video: youtube fetch failed: %v", err)
					state.failed = true
					state.loading = false
					openURL(ytURL)
					app.Window.Invalidate()
					return
				}
				state.loading = false
				app.VideoPlayer.PlayYouTube(info)
				app.Window.Invalidate()
			}()
		}
		// else: still fetching, ignore click (will be ready soon)
	}

	// YouTube thumbnail URL
	thumbURL := video.YouTubeThumbnailURL(videoID)
	ci := app.Images.Get(thumbURL, func() { app.Window.Invalidate() })

	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if ci == nil {
			// Loading thumbnail
			return state.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if state.btn.Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
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
			// Thumbnail failed — just a text link
			return state.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if state.btn.Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
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

		// Thumbnail with play overlay
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
			if state.btn.Hovered() {
				pointer.CursorPointer.Add(gtx.Ops)
			}
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

					// Semi-transparent overlay
					paint.FillShape(gtx.Ops, color.NRGBA{A: 60}, clip.Rect{Max: image.Pt(imgW, imgH)}.Op())

					// Play icon
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

					// "YouTube" badge top left
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

// findYouTubeURLInText finds the first YouTube URL in the message text.
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

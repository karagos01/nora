package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/store"
	"nora-client/video"
)

// VideoPlayerUI is a video player overlay over the message area.
type VideoPlayerUI struct {
	app     *App
	player  *video.Player
	Visible bool
	Title   string
	SourceURL string // pro "Open in browser" fallback

	closeBtn       widget.Clickable
	playPauseBtn   widget.Clickable
	openBrowserBtn widget.Clickable
	progressBar    Slider
	volumeSlider   Slider

	// YouTube quality picker
	ytVideoID    string
	ytAudioItag  int // best audio-only itag
	ytAudioURL   string // direct audio stream URL (from yt-dlp)
	ytFormats    []video.YouTubeFormat
	ytQualityIdx int // index into ytFormats, -1 = not youtube
	ytLoading    bool // true while fetching YouTube stream (prevents auto-close)
	ytCleanup    func() // cleanup temp files
	qualityBtn   widget.Clickable
	qualityOpen  bool
	qualityBtns  [8]widget.Clickable

	errMsg string
}

func NewVideoPlayerUI(a *App) *VideoPlayerUI {
	v := &VideoPlayerUI{
		app: a,
	}
	v.volumeSlider.Min = 0
	v.volumeSlider.Max = 200
	v.volumeSlider.Value = 100
	v.progressBar.Min = 0
	v.progressBar.Max = 1000 // promile
	return v
}

// SetVolume sets the volume on both the player and the slider.
func (v *VideoPlayerUI) SetVolume(vol int) {
	v.volumeSlider.Value = float32(vol)
	if v.player != nil {
		v.player.SetVolume(vol)
	}
}

// Play starts video playback (non-YouTube).
func (v *VideoPlayerUI) Play(url, title string) {
	v.ytVideoID = ""
	v.ytAudioItag = 0
	v.ytAudioURL = ""
	v.ytFormats = nil
	v.ytQualityIdx = -1
	v.qualityOpen = false

	if video.CheckFFmpeg() == "" {
		v.errMsg = video.FFmpegInstallHint()
		v.Visible = true
		v.Title = title
		v.SourceURL = url
		return
	}

	v.errMsg = ""
	v.Title = title
	v.SourceURL = url
	v.Visible = true

	v.ensurePlayer()
	v.player.LoadAndPlay(url)
}

// PlayYouTube starts YouTube video playback with quality info.
func (v *VideoPlayerUI) PlayYouTube(info *video.YouTubeInfo) {
	v.ytVideoID = info.VideoID
	v.ytAudioItag = info.BestAudioTag
	v.ytAudioURL = info.BestAudioURL
	v.ytFormats = info.Formats
	v.qualityOpen = false

	// Select preferred quality from saved setting
	v.ytQualityIdx = 0
	prefHeight := v.app.YouTubeQuality
	if prefHeight > 0 {
		for i, f := range info.Formats {
			if f.Height == prefHeight {
				v.ytQualityIdx = i
				break
			}
		}
	}

	if video.CheckFFmpeg() == "" {
		v.errMsg = video.FFmpegInstallHint()
		v.Visible = true
		v.Title = info.Title
		return
	}

	v.errMsg = ""
	v.Title = info.Title
	v.Visible = true

	if len(info.Formats) == 0 {
		v.errMsg = "No video formats available"
		return
	}

	chosen := info.Formats[v.ytQualityIdx]
	w, h := chosen.Width, chosen.Height
	dur := info.Duration

	// If yt-dlp provided stream URLs, play immediately (no extra yt-dlp call)
	if chosen.URL != "" {
		audioURL := ""
		if !chosen.Muxed {
			audioURL = info.BestAudioURL
		}
		log.Printf("video: streaming YouTube %dx%d (URL from info)", w, h)
		v.ensurePlayer()
		v.player.LoadAndPlayWithHints(chosen.URL, audioURL, w, h, dur, 0)
		return
	}

	// Fallback: need to extract URLs separately
	v.ytLoading = true
	vidID := info.VideoID
	itagNo := chosen.ItagNo
	audioItag := info.BestAudioTag
	if chosen.Muxed {
		audioItag = 0
	}

	go func() {
		log.Printf("video: resolving YouTube itag=%d audioItag=%d", itagNo, audioItag)

		videoURL, audioURL, err := video.GetYouTubeStreamURLs(vidID, itagNo, audioItag)
		if err == nil {
			log.Printf("video: streaming YouTube %dx%d", w, h)
			v.ytLoading = false
			v.ensurePlayer()
			v.player.LoadAndPlayWithHints(videoURL, audioURL, w, h, dur, 0)
			v.app.Window.Invalidate()
			return
		}
		log.Printf("video: stream URL failed: %v, falling back to download", err)

		videoPath, audioPath, cleanup, err := video.DownloadYouTubeStreams(vidID, itagNo, audioItag)
		if err != nil {
			v.ytLoading = false
			v.errMsg = "Playback failed: " + err.Error()
			v.app.Window.Invalidate()
			return
		}

		if v.ytCleanup != nil {
			v.ytCleanup()
		}
		v.ytCleanup = cleanup

		v.ytLoading = false
		v.ensurePlayer()
		v.player.LoadAndPlayWithHints(videoPath, audioPath, w, h, dur, 0)
		v.app.Window.Invalidate()
	}()
}

func (v *VideoPlayerUI) ensurePlayer() {
	if v.player == nil {
		v.player = video.NewPlayer(
			func() { v.app.Window.Invalidate() },
			func(msg string) {
				v.errMsg = msg
				v.app.Window.Invalidate()
			},
		)
	}
}

// Close closes the player.
func (v *VideoPlayerUI) Close() {
	if v.player != nil {
		v.player.Stop()
	}
	v.Visible = false
	v.Title = ""
	v.SourceURL = ""
	v.errMsg = ""
	v.ytVideoID = ""
	v.ytAudioItag = 0
	v.ytAudioURL = ""
	v.ytFormats = nil
	v.ytQualityIdx = -1
	v.ytLoading = false
	v.qualityOpen = false
	if v.ytCleanup != nil {
		v.ytCleanup()
		v.ytCleanup = nil
	}
}

// Layout renders the video overlay.
func (v *VideoPlayerUI) Layout(gtx layout.Context) layout.Dimensions {
	// Event handling
	if v.closeBtn.Clicked(gtx) {
		v.Close()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	if v.openBrowserBtn.Clicked(gtx) {
		if v.SourceURL != "" {
			openURL(v.SourceURL)
		}
	}

	// Quality picker toggle (only if multiple formats available)
	if v.qualityBtn.Clicked(gtx) && len(v.ytFormats) >= 2 {
		v.qualityOpen = !v.qualityOpen
	}

	// Quality option clicks
	for i := 0; i < len(v.ytFormats) && i < len(v.qualityBtns); i++ {
		if v.qualityBtns[i].Clicked(gtx) {
			if i != v.ytQualityIdx {
				v.ytQualityIdx = i
				v.qualityOpen = false
				ytFmt := v.ytFormats[i]
				// Persist preference
				v.app.YouTubeQuality = ytFmt.Height
				go store.UpdateYouTubeQuality(v.app.PublicKey, ytFmt.Height)
				dur := v.player.Duration()
				posMs := v.player.Position() - 1000
				if posMs < 0 {
					posMs = 0
				}

				// If URL available from info, switch instantly
				if ytFmt.URL != "" {
					audioURL := ""
					if !ytFmt.Muxed {
						audioURL = v.ytAudioURL
					}
					if v.ytCleanup != nil {
						v.ytCleanup()
						v.ytCleanup = nil
					}
					v.ensurePlayer()
					v.player.LoadAndPlayWithHints(ytFmt.URL, audioURL, ytFmt.Width, ytFmt.Height, dur, posMs)
				} else {
					// Fallback: extract URLs
					vidID := v.ytVideoID
					audioItag := v.ytAudioItag
					if ytFmt.Muxed {
						audioItag = 0
					}
					v.ytLoading = true
					go func() {
						videoURL, audioURL, err := video.GetYouTubeStreamURLs(vidID, ytFmt.ItagNo, audioItag)
						if err == nil {
							if v.ytCleanup != nil {
								v.ytCleanup()
								v.ytCleanup = nil
							}
							v.ytLoading = false
							v.ensurePlayer()
							v.player.LoadAndPlayWithHints(videoURL, audioURL, ytFmt.Width, ytFmt.Height, dur, posMs)
							v.app.Window.Invalidate()
							return
						}
						videoPath, audioPath, cleanup, err := video.DownloadYouTubeStreams(vidID, ytFmt.ItagNo, audioItag)
						if err != nil {
							v.ytLoading = false
							v.errMsg = "Quality switch failed"
							v.app.Window.Invalidate()
							return
						}
						if v.ytCleanup != nil {
							v.ytCleanup()
						}
						v.ytCleanup = cleanup
						v.ytLoading = false
						v.ensurePlayer()
						v.player.LoadAndPlayWithHints(videoPath, audioPath, ytFmt.Width, ytFmt.Height, dur, posMs)
						v.app.Window.Invalidate()
					}()
				}
			} else {
				v.qualityOpen = false
			}
		}
	}

	if v.player != nil {
		if v.playPauseBtn.Clicked(gtx) {
			switch v.player.GetState() {
			case video.StatePlaying:
				v.player.Pause()
			case video.StatePaused:
				v.player.Resume()
			}
		}

		if v.progressBar.Changed() {
			dur := v.player.Duration()
			if dur > 0 {
				posMs := int64(float64(v.progressBar.Value) / 1000.0 * float64(dur.Milliseconds()))
				v.player.SeekTo(posMs)
			}
		}

		if v.volumeSlider.Changed() {
			vol := int(v.volumeSlider.Value)
			v.player.SetVolume(vol)
			go store.UpdateVideoVolume(v.app.PublicKey, vol)
		}
	}

	maxSize := gtx.Constraints.Max

	dims := layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutHeader(gtx)
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				// Video frame area
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.layoutVideoArea(gtx)
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				// Controls
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutControls(gtx)
				}),
			)
		},
	)

	// Quality dropdown as top-level overlay (not clipped by controls)
	if v.qualityOpen && len(v.ytFormats) > 0 {
		v.layoutQualityDropdown(gtx, maxSize)
	}

	return dims
}

func (v *VideoPlayerUI) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					// Video icon
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconVideocam, 18, ColorAccent)
					}),
					// Title
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							title := v.Title
							if title == "" {
								title = "Video"
							}
							lbl := material.Body2(v.app.Theme.Material, "Playing: "+title)
							lbl.Color = ColorText
							lbl.MaxLines = 1
							return lbl.Layout(gtx)
						})
					}),
					// Open in browser
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.SourceURL == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutIconBtn(gtx, &v.openBrowserBtn, IconMonitor, ColorTextDim, false)
						})
					}),
					// Close button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutIconBtn(gtx, &v.closeBtn, IconClose, ColorDanger, v.closeBtn.Hovered())
					}),
				)
			})
		},
	)
}

func (v *VideoPlayerUI) layoutVideoArea(gtx layout.Context) layout.Dimensions {
	// Error state
	if v.errMsg != "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body1(v.app.Theme.Material, v.errMsg)
					lbl.Color = ColorDanger
					return lbl.Layout(gtx)
				}),
			)
		})
	}

	if v.ytLoading {
		return layoutCentered(gtx, v.app.Theme, "Connecting to YouTube...", ColorTextDim)
	}

	if v.player == nil {
		return layoutCentered(gtx, v.app.Theme, "No video loaded", ColorTextDim)
	}

	state := v.player.GetState()

	switch state {
	case video.StateError:
		return layoutCentered(gtx, v.app.Theme, "Playback error", ColorDanger)
	case video.StateIdle:
		// Auto-close player when video ends
		v.Close()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Show frame if available (even during loading/buffering — keeps last frame visible)
	frame := v.player.Frame()
	if frame == nil {
		if state == video.StateLoading {
			return layoutCentered(gtx, v.app.Theme, "Loading video...", ColorTextDim)
		}
		return layoutCentered(gtx, v.app.Theme, "Buffering...", ColorTextDim)
	}

	imgWidget := widget.Image{
		Src:      paint.NewImageOp(frame),
		Fit:      widget.Contain,
		Position: layout.Center,
	}
	dims := imgWidget.Layout(gtx)

	// Loading/buffering overlay on top of last frame
	if state == video.StateLoading || v.player.Buffering() {
		paint.FillShape(gtx.Ops, color.NRGBA{A: 120}, clip.Rect{Max: dims.Size}.Op())
		label := "Buffering..."
		if state == video.StateLoading {
			label = "Loading..."
		}
		layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(v.app.Theme.Material, label)
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 220}
			return lbl.Layout(gtx)
		})
	}

	return dims
}

func (v *VideoPlayerUI) layoutControls(gtx layout.Context) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					// Play/Pause button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						icon := IconPlayArrow
						if v.player != nil && v.player.GetState() == video.StatePlaying {
							icon = IconPause
						}
						return v.layoutIconBtn(gtx, &v.playPauseBtn, icon, ColorText, v.playPauseBtn.Hovered())
					}),
					// Current time
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						pos := int64(0)
						if v.player != nil {
							pos = v.player.Position()
						}
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, formatDuration(time.Duration(pos)*time.Millisecond))
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}),
					// Progress slider
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							// Update progress position
							if v.player != nil {
								dur := v.player.Duration()
								if dur > 0 {
									pos := v.player.Position()
									v.progressBar.Value = float32(pos) / float32(dur.Milliseconds()) * 1000
								}
							}
							return v.progressBar.Layout(gtx, 0)
						})
					}),
					// Duration
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						dur := time.Duration(0)
						if v.player != nil {
							dur = v.player.Duration()
						}
						return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, formatDuration(dur))
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
					// Quality button (YouTube only)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if len(v.ytFormats) == 0 {
							return layout.Dimensions{}
						}
						return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.qualityBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								if v.qualityBtn.Hovered() {
									pointer.CursorPointer.Add(gtx.Ops)
								}
								label := "720p"
								if v.ytQualityIdx >= 0 && v.ytQualityIdx < len(v.ytFormats) {
									label = v.ytFormats[v.ytQualityIdx].Label
								}
								bg := ColorInput
								if v.qualityBtn.Hovered() || v.qualityOpen {
									bg = ColorHover
								}
								return layout.Background{}.Layout(gtx,
									func(gtx layout.Context) layout.Dimensions {
										bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
										rr := gtx.Dp(4)
										paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
										return layout.Dimensions{Size: bounds.Max}
									},
									func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, label)
											lbl.Color = ColorText
											return lbl.Layout(gtx)
										})
									},
								)
							})
						})
					}),
					// Volume icon
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconVolumeUp, 16, ColorTextDim)
					}),
					// Volume slider
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.volumeSlider.Layout(gtx, unit.Dp(80))
						})
					}),
				)
			})
		},
	)
}

// layoutQualityDropdown renders the quality option list above the controls bar.
// maxSize is the total player area size — dropdown is positioned from the bottom.
func (v *VideoPlayerUI) layoutQualityDropdown(gtx layout.Context, maxSize image.Point) {
	if len(v.ytFormats) == 0 {
		return
	}

	itemH := gtx.Dp(28)
	padV := gtx.Dp(4)
	dropW := gtx.Dp(90)
	count := len(v.ytFormats)
	if count > len(v.qualityBtns) {
		count = len(v.qualityBtns)
	}
	dropH := count*itemH + padV*2
	controlsH := gtx.Dp(44) // approximate controls bar height

	// Position: bottom-right, above the controls bar
	x := maxSize.X - dropW - gtx.Dp(100)
	if x < 0 {
		x = 0
	}
	y := maxSize.Y - controlsH - dropH - gtx.Dp(4)

	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()

	// Background with border
	rr := gtx.Dp(6)
	bounds := image.Rect(0, 0, dropW, dropH)
	// Shadow/border
	paint.FillShape(gtx.Ops, color.NRGBA{A: 60}, clip.RRect{
		Rect: image.Rect(-1, -1, dropW+1, dropH+1), NE: rr, NW: rr, SE: rr, SW: rr,
	}.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, color.NRGBA{R: 30, G: 30, B: 35, A: 250}, clip.RRect{
		Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
	}.Op(gtx.Ops))

	// Items
	off := padV
	for i := 0; i < count; i++ {
		func(idx int) {
			defer op.Offset(image.Pt(0, off)).Push(gtx.Ops).Pop()
			itemGtx := gtx
			itemGtx.Constraints.Min = image.Pt(dropW, itemH)
			itemGtx.Constraints.Max = itemGtx.Constraints.Min

			v.qualityBtns[idx].Layout(itemGtx, func(gtx layout.Context) layout.Dimensions {
				if v.qualityBtns[idx].Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
				bg := color.NRGBA{A: 0}
				if idx == v.ytQualityIdx {
					bg = ColorAccent
					bg.A = 40
				}
				if v.qualityBtns[idx].Hovered() {
					bg = ColorHover
				}
				paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(dropW, itemH)}.Op())

				return layout.Inset{Left: unit.Dp(8), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, v.ytFormats[idx].Label)
					if idx == v.ytQualityIdx {
						lbl.Color = ColorAccent
					} else {
						lbl.Color = ColorText
					}
					return lbl.Layout(gtx)
				})
			})
		}(i)
		off += itemH
	}
}

func (v *VideoPlayerUI) layoutIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, clr color.NRGBA, hovered bool) layout.Dimensions {
	size := gtx.Dp(28)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min
		bg := color.NRGBA{A: 0}
		if hovered {
			bg = ColorHover
		}
		rr := size / 4
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layoutIcon(gtx, icon, 18, clr)
		})
	})
}

func formatDuration(d time.Duration) string {
	total := int(d.Seconds())
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// Destroy releases resources.
func (v *VideoPlayerUI) Destroy() {
	if v.player != nil {
		v.player.Destroy()
		v.player = nil
	}
}

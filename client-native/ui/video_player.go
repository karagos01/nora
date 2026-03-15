package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/store"
	"nora-client/video"
)

// VideoPlayerUI je overlay přehrávač videa přes message area.
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

// SetVolume nastaví hlasitost přehrávače i slideru.
func (v *VideoPlayerUI) SetVolume(vol int) {
	v.volumeSlider.Value = float32(vol)
	if v.player != nil {
		v.player.SetVolume(vol)
	}
}

// Play spustí přehrávání videa.
func (v *VideoPlayerUI) Play(url, title string) {
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

	if v.player == nil {
		v.player = video.NewPlayer(
			func() { v.app.Window.Invalidate() },
			func(msg string) {
				v.errMsg = msg
				v.app.Window.Invalidate()
			},
		)
	}

	v.player.LoadAndPlay(url)
}

// Close zavře přehrávač.
func (v *VideoPlayerUI) Close() {
	if v.player != nil {
		v.player.Stop()
	}
	v.Visible = false
	v.Title = ""
	v.SourceURL = ""
	v.errMsg = ""
}

// Layout renderuje video overlay.
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

	return layout.Background{}.Layout(gtx,
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

	if v.player == nil {
		return layoutCentered(gtx, v.app.Theme, "No video loaded", ColorTextDim)
	}

	state := v.player.GetState()

	switch state {
	case video.StateLoading:
		return layoutCentered(gtx, v.app.Theme, "Loading video...", ColorTextDim)
	case video.StateError:
		return layoutCentered(gtx, v.app.Theme, "Playback error", ColorDanger)
	case video.StateIdle:
		return layoutCentered(gtx, v.app.Theme, "Video ended", ColorTextDim)
	}

	// StatePlaying nebo StatePaused — zobrazit frame
	frame := v.player.Frame()
	if frame == nil {
		return layoutCentered(gtx, v.app.Theme, "Buffering...", ColorTextDim)
	}

	imgWidget := widget.Image{
		Src:      paint.NewImageOp(frame),
		Fit:      widget.Contain,
		Position: layout.Center,
	}
	return imgWidget.Layout(gtx)
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

// Destroy uvolní resources.
func (v *VideoPlayerUI) Destroy() {
	if v.player != nil {
		v.player.Destroy()
		v.player = nil
	}
}

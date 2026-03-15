package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/update"
)

// updateState describes the current state of the update bar.
type updateState int

const (
	updateStateAvailable   updateState = iota // update available, waiting for action
	updateStateDownloading                    // download in progress
	updateStateReady                          // downloaded + applied, waiting for restart
	updateStateError                          // error during download or apply
)

type UpdateBar struct {
	app *App

	available  bool
	newVersion string
	downloadURL string
	sha256      string

	state    updateState
	progress atomic.Int64 // 0-1000 (per mille), atomic for goroutine
	errMsg   string
	tmpPath  string // path to downloaded file

	downloadBtn widget.Clickable
	restartBtn  widget.Clickable
	retryBtn    widget.Clickable
	dismissBtn  widget.Clickable
}

func NewUpdateBar(a *App) *UpdateBar {
	return &UpdateBar{app: a}
}

func (u *UpdateBar) SetAvailable(version, url, sha256 string) {
	u.available = true
	u.newVersion = version
	u.downloadURL = url
	u.sha256 = sha256
	u.state = updateStateAvailable
	u.errMsg = ""
	u.tmpPath = ""
	u.progress.Store(0)
}

// getProgress returns progress as float64 0.0-1.0
func (u *UpdateBar) getProgress() float64 {
	return float64(u.progress.Load()) / 1000.0
}

func (u *UpdateBar) Layout(gtx layout.Context) layout.Dimensions {
	if !u.available {
		return layout.Dimensions{}
	}

	// Dismiss — only in available and error states
	if u.dismissBtn.Clicked(gtx) {
		u.available = false
		return layout.Dimensions{}
	}

	// Start download
	if u.downloadBtn.Clicked(gtx) && u.state == updateStateAvailable {
		u.state = updateStateDownloading
		u.errMsg = ""
		u.progress.Store(0)
		go u.doDownloadAndApply()
	}

	// Retry after error
	if u.retryBtn.Clicked(gtx) && u.state == updateStateError {
		u.state = updateStateDownloading
		u.errMsg = ""
		u.progress.Store(0)
		go u.doDownloadAndApply()
	}

	// Restart
	if u.restartBtn.Clicked(gtx) && u.state == updateStateReady {
		u.doRestart()
	}

	// Background color based on state
	bgColor := u.bgColorForState()

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			paint.FillShape(gtx.Ops, bgColor, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				switch u.state {
				case updateStateDownloading:
					return u.layoutDownloading(gtx)
				case updateStateReady:
					return u.layoutReady(gtx)
				case updateStateError:
					return u.layoutError(gtx)
				default:
					return u.layoutAvailable(gtx)
				}
			})
		},
	)
}

// bgColorForState returns the background color based on state.
func (u *UpdateBar) bgColorForState() color.NRGBA {
	switch u.state {
	case updateStateAvailable:
		// Yellow/amber bar
		return color.NRGBA{R: 100, G: 80, B: 20, A: 255}
	case updateStateDownloading:
		// Yellow/amber bar
		return color.NRGBA{R: 100, G: 80, B: 20, A: 255}
	case updateStateReady:
		// Green bar
		return color.NRGBA{R: 30, G: 90, B: 50, A: 255}
	case updateStateError:
		// Red bar
		return color.NRGBA{R: 120, G: 35, B: 35, A: 255}
	}
	return color.NRGBA{R: 100, G: 80, B: 20, A: 255}
}

// layoutAvailable — "Update available (build X)" + Download + Dismiss
func (u *UpdateBar) layoutAvailable(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			text := fmt.Sprintf("Update available (build %s)", u.newVersion)
			lbl := material.Body2(u.app.Theme.Material, text)
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.layoutButton(gtx, &u.downloadBtn, "Download", ColorAccent, ColorAccentHover)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.layoutDismiss(gtx)
				}),
			)
		}),
	)
}

// layoutDownloading — "Downloading... X%" + progress bar
func (u *UpdateBar) layoutDownloading(gtx layout.Context) layout.Dimensions {
	prog := u.getProgress()
	pct := int(prog * 100)

	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
		// Text + progress bar
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					text := fmt.Sprintf("Downloading update (build %s)... %d%%", u.newVersion, pct)
					lbl := material.Body2(u.app.Theme.Material, text)
					lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					return lbl.Layout(gtx)
				}),
				// Progress bar
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return u.layoutProgressBar(gtx, prog)
					})
				}),
			)
		}),
	)
}

// layoutReady — "Update ready! Restart to apply." + Restart
func (u *UpdateBar) layoutReady(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(u.app.Theme.Material, "Update ready! Restart to apply.")
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btnColor := color.NRGBA{R: 60, G: 160, B: 90, A: 255}
			btnHover := color.NRGBA{R: 80, G: 190, B: 110, A: 255}
			return u.layoutButton(gtx, &u.restartBtn, "Restart now", btnColor, btnHover)
		}),
	)
}

// layoutError — "Update failed: ..." + Retry + Dismiss
func (u *UpdateBar) layoutError(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			text := "Update failed: " + u.errMsg
			lbl := material.Body2(u.app.Theme.Material, text)
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.layoutButton(gtx, &u.retryBtn, "Retry", ColorAccent, ColorAccentHover)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.layoutDismiss(gtx)
				}),
			)
		}),
	)
}

// layoutProgressBar renders a progress bar (thin strip).
func (u *UpdateBar) layoutProgressBar(gtx layout.Context, progress float64) layout.Dimensions {
	barHeight := gtx.Dp(4)
	totalWidth := gtx.Constraints.Max.X

	// Background (dark)
	bgRect := image.Rect(0, 0, totalWidth, barHeight)
	bgColor := color.NRGBA{R: 0, G: 0, B: 0, A: 80}
	paint.FillShape(gtx.Ops, bgColor, clip.RRect{
		Rect: bgRect,
		NE:   2, NW: 2, SE: 2, SW: 2,
	}.Op(gtx.Ops))

	// Fill (white)
	fillWidth := int(float64(totalWidth) * progress)
	if fillWidth > 0 {
		fillRect := image.Rect(0, 0, fillWidth, barHeight)
		fillColor := color.NRGBA{R: 255, G: 255, B: 255, A: 200}
		paint.FillShape(gtx.Ops, fillColor, clip.RRect{
			Rect: fillRect,
			NE:   2, NW: 2, SE: 2, SW: 2,
		}.Op(gtx.Ops))
	}

	return layout.Dimensions{Size: image.Pt(totalWidth, barHeight)}
}

// layoutButton renders a colored button.
func (u *UpdateBar) layoutButton(gtx layout.Context, btn *widget.Clickable, text string, bgNormal, bgHover color.NRGBA) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				c := bgNormal
				if btn.Hovered() {
					c = bgHover
				}
				paint.FillShape(gtx.Ops, c, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE:   rr, SW:   rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(u.app.Theme.Material, text)
						lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						return lbl.Layout(gtx)
					})
				})
			},
		)
	})
}

// layoutDismiss renders a text Dismiss button.
func (u *UpdateBar) layoutDismiss(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return u.dismissBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				c := color.NRGBA{R: 200, G: 200, B: 200, A: 200}
				if u.dismissBtn.Hovered() {
					c = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				}
				lbl := material.Caption(u.app.Theme.Material, "Dismiss")
				lbl.Color = c
				return lbl.Layout(gtx)
			})
		})
	})
}

// doDownloadAndApply downloads an update and applies it (runs in a goroutine).
func (u *UpdateBar) doDownloadAndApply() {
	// Download the binary
	tmpPath, err := update.Download(u.downloadURL, u.sha256, func(downloaded, total int64) {
		if total > 0 {
			prom := int64(float64(downloaded) / float64(total) * 1000)
			u.progress.Store(prom)
		}
		u.app.Window.Invalidate()
	})
	if err != nil {
		u.errMsg = err.Error()
		u.state = updateStateError
		u.app.Window.Invalidate()
		return
	}

	// Apply (rename binary)
	if err := update.Apply(tmpPath); err != nil {
		u.errMsg = err.Error()
		u.state = updateStateError
		u.app.Window.Invalidate()
		return
	}

	u.tmpPath = tmpPath
	u.state = updateStateReady
	u.app.Window.Invalidate()
}

// doRestart restartuje aplikaci.
func (u *UpdateBar) doRestart() {
	log.Println("Update applied, restarting...")

	exe, err := os.Executable()
	if err != nil {
		log.Printf("restart: cannot find executable: %v", err)
		return
	}

	// Linux: syscall.Exec replaces the current process
	if runtime.GOOS != "windows" {
		err := syscall.Exec(exe, os.Args, os.Environ())
		if err != nil {
			log.Printf("syscall.Exec failed: %v, falling back to Restart()", err)
		}
	}

	// Windows (or fallback): starts a new process and exits the current one
	update.Restart()
}

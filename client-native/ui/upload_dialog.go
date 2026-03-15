package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type UploadDialog struct {
	app     *App
	Visible bool

	editor      widget.Editor
	fileList    widget.List
	overlayBtn  widget.Clickable
	cardBtn     widget.Clickable
	sendBtn     widget.Clickable
	cancelBtn   widget.Clickable
	autoSendBtn widget.Clickable
	clearBtn    widget.Clickable
	closeBtn    widget.Clickable
	addFilesBtn widget.Clickable
	removeBtns  []widget.Clickable
	needsFocus  bool
}

func NewUploadDialog(a *App) *UploadDialog {
	d := &UploadDialog{app: a}
	d.fileList.Axis = layout.Vertical
	d.editor.Submit = true
	return d
}

func (d *UploadDialog) Show() {
	d.Visible = true
	d.needsFocus = true
}

func (d *UploadDialog) Hide() {
	d.Visible = false
}

func (d *UploadDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	// Focus na editor při otevření
	if d.needsFocus {
		d.needsFocus = false
		gtx.Execute(key.FocusCmd{Tag: &d.editor})
	}

	mv := d.app.MsgView

	// Zjistit stav uploadů
	mv.uploadMu.Lock()
	uploads := make([]*pendingUpload, len(mv.pendingUploads))
	copy(uploads, mv.pendingUploads)
	mv.uploadMu.Unlock()

	allDone := len(uploads) > 0
	for _, u := range uploads {
		if u.status == 0 {
			allDone = false
			break
		}
	}

	// Handle editor submit (Enter)
	for {
		ev, ok := d.editor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit && allDone {
			d.doSend(mv)
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}

	// Handle clicks
	if d.closeBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.cancelBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.sendBtn.Clicked(gtx) && allDone {
		d.doSend(mv)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.addFilesBtn.Clicked(gtx) {
		go mv.pickFiles()
	}
	if d.autoSendBtn.Clicked(gtx) {
		mv.autoSend = !mv.autoSend
	}
	if d.clearBtn.Clicked(gtx) {
		mv.uploadMu.Lock()
		for _, u := range mv.pendingUploads {
			u.removed = true
		}
		mv.pendingUploads = nil
		mv.uploadMu.Unlock()
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Handle per-file remove buttons
	if len(d.removeBtns) < len(uploads) {
		d.removeBtns = make([]widget.Clickable, len(uploads)+10)
	}
	for i := range uploads {
		if d.removeBtns[i].Clicked(gtx) {
			uploads[i].removed = true
			mv.uploadMu.Lock()
			filtered := mv.pendingUploads[:0]
			for _, u := range mv.pendingUploads {
				if !u.removed {
					filtered = append(filtered, u)
				}
			}
			mv.pendingUploads = filtered
			mv.uploadMu.Unlock()
			if len(mv.pendingUploads) == 0 {
				d.Hide()
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}
		}
	}

	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Channel name pro header
	channelName := ""
	if conn := d.app.Conn(); conn != nil {
		d.app.mu.RLock()
		channelName = conn.ActiveChannelName
		d.app.mu.RUnlock()
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		// Backdrop
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		// Card
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			cardW := gtx.Dp(420)
			if cardW > gtx.Constraints.Max.X-gtx.Dp(40) {
				cardW = gtx.Constraints.Max.X - gtx.Dp(40)
			}
			maxH := gtx.Constraints.Max.Y * 60 / 100
			gtx.Constraints.Max.X = cardW
			gtx.Constraints.Min.X = cardW
			gtx.Constraints.Max.Y = maxH

			return d.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(12)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
							Rect: bounds,
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Header
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutHeader(gtx, channelName)
								}),
								// File list
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.layoutFileList(gtx, uploads)
									})
								}),
								// Message editor
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutEditor(gtx, channelName)
								}),
								// Bottom controls
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutControls(gtx, allDone)
								}),
							)
						})
					},
				)
			})
		}),
	)
}

// doSend přenese text z dialogového editoru do hlavního a odešle zprávu.
func (d *UploadDialog) doSend(mv *MessageView) {
	text := strings.TrimSpace(d.editor.Text())
	mv.editor.SetText(text)
	d.editor.SetText("")
	d.Hide()
	mv.sendMessage()
}

func (d *UploadDialog) layoutHeader(gtx layout.Context, channelName string) layout.Dimensions {
	title := "Upload"
	if channelName != "" {
		title = "Upload to #" + channelName
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(d.app.Theme.Material, title)
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if d.closeBtn.Hovered() {
					clr = ColorText
				}
				lbl := material.Body1(d.app.Theme.Material, "X")
				lbl.Color = clr
				return lbl.Layout(gtx)
			})
		}),
	)
}

func (d *UploadDialog) layoutFileList(gtx layout.Context, uploads []*pendingUpload) layout.Dimensions {
	if len(uploads) == 0 {
		lbl := material.Body2(d.app.Theme.Material, "No files selected")
		lbl.Color = ColorTextDim
		return lbl.Layout(gtx)
	}

	return material.List(d.app.Theme.Material, &d.fileList).Layout(gtx, len(uploads), func(gtx layout.Context, idx int) layout.Dimensions {
		u := uploads[idx]
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return d.layoutFileRow(gtx, u, idx)
		})
	})
}

func (d *UploadDialog) layoutFileRow(gtx layout.Context, u *pendingUpload, idx int) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Filename + size + remove
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Filename (bold) + size (dim)
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(d.app.Theme.Material, u.filename)
							lbl.Color = ColorText
							lbl.Font.Weight = 600
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(d.app.Theme.Material, FormatBytes(u.size))
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
					)
				}),
				// Remove button
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return d.removeBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						clr := ColorTextDim
						if d.removeBtns[idx].Hovered() {
							clr = ColorDanger
						}
						lbl := material.Body2(d.app.Theme.Material, "X")
						lbl.Color = clr
						return lbl.Layout(gtx)
					})
				}),
			)
		}),
		// Status text
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				text, clr := d.statusText(u)
				lbl := material.Caption(d.app.Theme.Material, text)
				lbl.Color = clr
				return lbl.Layout(gtx)
			})
		}),
		// Progress bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.status != 0 {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutProgressBar(gtx, u)
			})
		}),
	)
}

func (d *UploadDialog) statusText(u *pendingUpload) (string, color.NRGBA) {
	switch u.status {
	case 0: // uploading
		pct := 0
		if u.size > 0 {
			pct = int(u.sent * 100 / u.size)
		}
		elapsed := time.Since(u.startTime).Seconds()
		if elapsed < 0.5 || u.sent == 0 {
			return fmt.Sprintf("%d%%", pct), ColorAccent
		}
		speed := float64(u.sent) / elapsed
		text := fmt.Sprintf("%d%% · %s/s", pct, FormatBytes(int64(speed)))
		remaining := float64(u.size-u.sent) / speed
		if remaining > 0 && remaining < 3600 {
			if remaining < 60 {
				text += fmt.Sprintf(" · ~%ds", int(remaining))
			} else {
				text += fmt.Sprintf(" · ~%dm%ds", int(remaining)/60, int(remaining)%60)
			}
		}
		return text, ColorAccent
	case 1:
		return "Done", ColorSuccess
	case 2:
		return u.err, ColorDanger
	}
	return "", ColorTextDim
}

func (d *UploadDialog) layoutProgressBar(gtx layout.Context, u *pendingUpload) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	h := gtx.Dp(3)
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect(image.Rect(0, 0, maxW, h)).Op())
	pct := float32(0)
	if u.size > 0 {
		pct = float32(u.sent) / float32(u.size)
	}
	barW := int(pct * float32(maxW))
	if barW > 0 {
		paint.FillShape(gtx.Ops, ColorAccent, clip.Rect(image.Rect(0, 0, barW, h)).Op())
	}
	return layout.Dimensions{Size: image.Pt(maxW, h)}
}

func (d *UploadDialog) layoutEditor(gtx layout.Context, channelName string) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				maxH := gtx.Dp(80)
				if gtx.Constraints.Max.Y > maxH {
					gtx.Constraints.Max.Y = maxH
				}
				return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					hint := "Add a comment..."
					if channelName != "" {
						hint = "Message #" + channelName
					}
					ed := material.Editor(d.app.Theme.Material, &d.editor, hint)
					ed.Color = ColorText
					ed.HintColor = ColorTextDim
					return ed.Layout(gtx)
				})
			},
		)
	})
}

func (d *UploadDialog) layoutControls(gtx layout.Context, allDone bool) layout.Dimensions {
	mv := d.app.MsgView

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Controls row
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					// Auto-send toggle
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return d.autoSendBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							check := "[ ]"
							if mv.autoSend {
								check = "[x]"
							}
							lbl := material.Caption(d.app.Theme.Material, check+" Auto-send")
							lbl.Color = ColorTextDim
							if d.autoSendBtn.Hovered() {
								lbl.Color = ColorText
							}
							return lbl.Layout(gtx)
						})
					}),
					// Add files
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.addFilesBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								clr := ColorAccent
								if d.addFilesBtn.Hovered() {
									clr = ColorAccentHover
								}
								lbl := material.Caption(d.app.Theme.Material, "+ Add files")
								lbl.Color = clr
								return lbl.Layout(gtx)
							})
						})
					}),
					// Clear all
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.clearBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(d.app.Theme.Material, "Clear all")
								lbl.Color = ColorDanger
								return lbl.Layout(gtx)
							})
						})
					}),
					// Spacer
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{}
					}),
					// Cancel
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return d.layoutBtn(gtx, &d.cancelBtn, "Cancel", ColorInput, ColorText)
					}),
					// Send (disabled pokud uploads běží)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bg := ColorAccentDim
						fg := color.NRGBA{R: 180, G: 180, B: 180, A: 255}
						if allDone {
							bg = ColorAccent
							fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						}
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutBtn(gtx, &d.sendBtn, "Send", bg, fg)
						})
					}),
				)
			})
		}),
	)
}

func (d *UploadDialog) layoutBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hoverBg := bg
		if btn.Hovered() {
			hoverBg = color.NRGBA{
				R: min8(bg.R + 20),
				G: min8(bg.G + 20),
				B: min8(bg.B + 20),
				A: 255,
			}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(d.app.Theme.Material, text)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

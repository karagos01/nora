package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Durace ban (sekundy)
var banDurations = []struct {
	Label    string
	Seconds  int
}{
	{"1 day", 86400},
	{"7 days", 604800},
	{"30 days", 2592000},
	{"Permanent", 0},
}

type BanDialog struct {
	app *App

	Visible  bool
	Username string
	userID   string

	reasonEd       widget.Editor
	banIPBtn       widget.Clickable
	banIP          bool
	banDeviceBtn   widget.Clickable
	banDevice      bool
	revokeInvBtn   widget.Clickable
	revokeInvites  bool
	deleteMsgsBtn  widget.Clickable
	deleteMessages bool
	durationBtns   [4]widget.Clickable
	durationIdx    int // index do banDurations
	confirmBtn     widget.Clickable
	cancelBtn      widget.Clickable
	overlayBtn     widget.Clickable
	cardBtn        widget.Clickable
}

func NewBanDialog(a *App) *BanDialog {
	d := &BanDialog{app: a}
	d.reasonEd.SingleLine = true
	return d
}

func (d *BanDialog) Show(userID, username string) {
	d.Visible = true
	d.userID = userID
	d.Username = username
	d.reasonEd.SetText("")
	d.banIP = false
	d.banDevice = true
	d.revokeInvites = true
	d.deleteMessages = false
	d.durationIdx = 3 // Permanent
}

func (d *BanDialog) Hide() {
	d.Visible = false
	d.userID = ""
	d.Username = ""
}

func (d *BanDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	userID := d.userID

	if d.banIPBtn.Clicked(gtx) {
		d.banIP = !d.banIP
	}
	if d.banDeviceBtn.Clicked(gtx) {
		d.banDevice = !d.banDevice
	}
	if d.revokeInvBtn.Clicked(gtx) {
		d.revokeInvites = !d.revokeInvites
	}
	if d.deleteMsgsBtn.Clicked(gtx) {
		d.deleteMessages = !d.deleteMessages
	}
	for i := range d.durationBtns {
		if d.durationBtns[i].Clicked(gtx) {
			d.durationIdx = i
		}
	}

	if d.confirmBtn.Clicked(gtx) {
		reason := d.reasonEd.Text()
		banIP := d.banIP
		banDevice := d.banDevice
		revokeInv := d.revokeInvites
		deleteMsgs := d.deleteMessages
		duration := banDurations[d.durationIdx].Seconds
		d.Hide()
		go func() {
			conn := d.app.Conn()
			if conn == nil {
				return
			}
			if err := conn.Client.BanUser(userID, reason, banIP, banDevice, revokeInv, deleteMsgs, duration); err != nil {
				log.Printf("BanUser error: %v", err)
			}
		}()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.cancelBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(380)
			gtx.Constraints.Min.X = gtx.Dp(380)

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
						return layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, fmt.Sprintf("Ban %s", d.Username))
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								// Warning
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(d.app.Theme.Material, "This user will be banned and disconnected.")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Duration label
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(d.app.Theme.Material, "DURATION")
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								// Duration buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return d.layoutDurationBtn(gtx, 0)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return d.layoutDurationBtn(gtx, 1)
												})
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return d.layoutDurationBtn(gtx, 2)
												})
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return d.layoutDurationBtn(gtx, 3)
												})
											}),
										)
									})
								}),
								// Reason label
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(d.app.Theme.Material, "REASON (OPTIONAL)")
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								// Reason input
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										gtx.Constraints.Min.X = gtx.Constraints.Max.X
										return layout.Background{}.Layout(gtx,
											func(gtx layout.Context) layout.Dimensions {
												bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
												rr := gtx.Dp(6)
												paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
													Rect: bounds,
													NE:   rr, NW: rr, SE:   rr, SW: rr,
												}.Op(gtx.Ops))
												return layout.Dimensions{Size: bounds.Max}
											},
											func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													ed := material.Editor(d.app.Theme.Material, &d.reasonEd, "Ban reason...")
													ed.Color = ColorText
													ed.HintColor = ColorTextDim
													return ed.Layout(gtx)
												})
											},
										)
									})
								}),
								// Checkboxes
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutCheckbox(gtx, &d.banIPBtn, d.banIP, "Also ban IP address")
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutCheckbox(gtx, &d.banDeviceBtn, d.banDevice, "Ban device")
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutCheckbox(gtx, &d.revokeInvBtn, d.revokeInvites, "Revoke invite codes")
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.layoutCheckbox(gtx, &d.deleteMsgsBtn, d.deleteMessages, "Delete all messages")
									})
								}),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return d.layoutBtn(gtx, &d.cancelBtn, "Cancel", ColorInput, ColorText)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return d.layoutBtn(gtx, &d.confirmBtn, "Ban", ColorDanger, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
											})
										}),
									)
								}),
							)
						})
					},
				)
			})
		}),
	)
}

func (d *BanDialog) layoutDurationBtn(gtx layout.Context, idx int) layout.Dimensions {
	selected := d.durationIdx == idx
	bg := ColorInput
	fg := ColorTextDim
	if selected {
		bg = ColorAccent
		fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return d.durationBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, banDurations[idx].Label)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (d *BanDialog) layoutCheckbox(gtx layout.Context, btn *widget.Clickable, checked bool, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				if bg.A > 0 {
					rr := gtx.Dp(4)
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							check := "[ ]"
							clr := ColorTextDim
							if checked {
								check = "[x]"
								clr = ColorAccent
							}
							lbl := material.Body2(d.app.Theme.Material, check)
							lbl.Color = clr
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(d.app.Theme.Material, text)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	})
}

func (d *BanDialog) layoutBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
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

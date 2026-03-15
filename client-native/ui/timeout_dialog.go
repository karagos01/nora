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

type timeoutOption struct {
	btn     widget.Clickable
	label   string
	seconds int
}

type TimeoutDialog struct {
	app      *App
	Visible  bool
	Username string
	userID   string

	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
	options    [5]timeoutOption
}

func NewTimeoutDialog(a *App) *TimeoutDialog {
	d := &TimeoutDialog{app: a}
	d.options[0] = timeoutOption{label: "1 Minute", seconds: 60}
	d.options[1] = timeoutOption{label: "5 Minutes", seconds: 300}
	d.options[2] = timeoutOption{label: "1 Hour", seconds: 3600}
	d.options[3] = timeoutOption{label: "1 Day", seconds: 86400}
	d.options[4] = timeoutOption{label: "7 Days", seconds: 604800}
	return d
}

func (d *TimeoutDialog) Show(userID, username string) {
	d.Visible = true
	d.userID = userID
	d.Username = username
}

func (d *TimeoutDialog) Hide() {
	d.Visible = false
	d.userID = ""
	d.Username = ""
}

func (d *TimeoutDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	userID := d.userID

	for i := range d.options {
		if d.options[i].btn.Clicked(gtx) {
			seconds := d.options[i].seconds
			d.Hide()
			go func() {
				conn := d.app.Conn()
				if conn == nil {
					return
				}
				if _, err := conn.Client.TimeoutUser(userID, seconds, ""); err != nil {
					log.Printf("TimeoutUser error: %v", err)
				}
			}()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
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
			gtx.Constraints.Max.X = gtx.Dp(280)
			gtx.Constraints.Min.X = gtx.Dp(280)

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
							var items []layout.FlexChild

							// Title
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.H6(d.app.Theme.Material, fmt.Sprintf("Timeout %s", d.Username))
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							}))

							// Subtitle
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(d.app.Theme.Material, "Select duration:")
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								})
							}))

							// Duration buttons
							for i := range d.options {
								idx := i
								items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.layoutDurationBtn(gtx, &d.options[idx].btn, d.options[idx].label)
									})
								}))
							}

							return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
						})
					},
				)
			})
		}),
	)
}

func (d *TimeoutDialog) layoutDurationBtn(gtx layout.Context, btn *widget.Clickable, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
			bg = ColorHover
		}
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(d.app.Theme.Material, text)
					lbl.Color = ColorText
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

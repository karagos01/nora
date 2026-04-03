package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ConfirmDialog is a reusable modal confirmation dialog.
type ConfirmDialog struct {
	app *App

	Visible bool
	Title   string
	Message string

	confirmBtn  widget.Clickable
	secondBtn   widget.Clickable
	cancelBtn   widget.Clickable
	overlayBtn  widget.Clickable
	cardBtn     widget.Clickable

	onConfirm    func()
	onSecond     func()
	onCancel     func()
	ConfirmText  string
	SecondText   string     // empty → no second button
	CancelText   string     // empty → "Cancel"
	confirmColor color.NRGBA // confirm button color (default ColorDanger)
}

func NewConfirmDialog(a *App) *ConfirmDialog {
	return &ConfirmDialog{app: a}
}

func (d *ConfirmDialog) Show(title, message string, onConfirm func()) {
	d.Visible = true
	d.Title = title
	d.Message = message
	d.ConfirmText = "Delete"
	d.confirmColor = ColorDanger
	d.onConfirm = onConfirm
	d.onCancel = nil
}

func (d *ConfirmDialog) ShowWithText(title, message, confirmText string, onConfirm func()) {
	d.Visible = true
	d.Title = title
	d.Message = message
	d.ConfirmText = confirmText
	d.confirmColor = ColorDanger
	d.onConfirm = onConfirm
	d.onCancel = nil
}

func (d *ConfirmDialog) ShowWithCancel(title, message, confirmText string, onConfirm, onCancel func()) {
	d.Visible = true
	d.Title = title
	d.Message = message
	d.ConfirmText = confirmText
	d.confirmColor = ColorDanger
	d.onConfirm = onConfirm
	d.onCancel = onCancel
}

// ShowConfirm shows a dialog with Cancel/Yes (accent color, not red).
func (d *ConfirmDialog) ShowConfirm(title, message string, onConfirm func()) {
	d.Visible = true
	d.Title = title
	d.Message = message
	d.ConfirmText = "Yes"
	d.confirmColor = ColorAccent
	d.onConfirm = onConfirm
	d.onCancel = nil
}

// ShowWithOptions shows a dialog with 3 action buttons (danger, secondary, dismiss).
func (d *ConfirmDialog) ShowWithOptions(title, message, confirmText, secondText, cancelText string, onConfirm, onSecond func()) {
	d.Visible = true
	d.Title = title
	d.Message = message
	d.ConfirmText = confirmText
	d.SecondText = secondText
	d.CancelText = cancelText
	d.confirmColor = ColorDanger
	d.onConfirm = onConfirm
	d.onSecond = onSecond
	d.onCancel = nil
}

func (d *ConfirmDialog) Hide() {
	d.Visible = false
	d.onConfirm = nil
	d.onSecond = nil
	d.onCancel = nil
	d.SecondText = ""
}

func (d *ConfirmDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	if d.confirmBtn.Clicked(gtx) {
		fn := d.onConfirm
		d.Hide()
		if fn != nil {
			go fn()
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.secondBtn.Clicked(gtx) {
		fn := d.onSecond
		d.Hide()
		if fn != nil {
			go fn()
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.cancelBtn.Clicked(gtx) {
		fn := d.onCancel
		d.Hide()
		if fn != nil {
			go fn()
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		fn := d.onCancel
		d.Hide()
		if fn != nil {
			go fn()
		}
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
			gtx.Constraints.Max.X = gtx.Dp(320)
			gtx.Constraints.Min.X = gtx.Dp(320)

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
						return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, d.Title)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								// Message
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(d.app.Theme.Material, d.Message)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											cancelText := d.CancelText
											if cancelText == "" {
												cancelText = "Cancel"
											}
											return layoutDialogBtn(gtx, d.app.Theme, &d.cancelBtn, cancelText, ColorInput, ColorText)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if d.SecondText == "" {
												return layout.Dimensions{}
											}
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutDialogBtn(gtx, d.app.Theme, &d.secondBtn, d.SecondText, ColorInput, ColorText)
											})
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												confirmText := d.ConfirmText
												if confirmText == "" {
													confirmText = "Delete"
												}
												btnColor := d.confirmColor
												if btnColor == (color.NRGBA{}) {
													btnColor = ColorDanger
												}
												return layoutDialogBtn(gtx, d.app.Theme, &d.confirmBtn, confirmText, btnColor, ColorWhite)
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


func min8(v uint8) uint8 {
	if v < 20 { // overflow protection
		return 255
	}
	return v
}

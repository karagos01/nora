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

// InputDialog is a simple text input modal dialog.
type InputDialog struct {
	app     *App
	Visible bool

	title     string
	hint      string
	confirmTx string
	onConfirm func(string)

	editor     widget.Editor
	confirmBtn widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
}

func NewInputDialog(a *App) *InputDialog {
	d := &InputDialog{app: a}
	d.editor.SingleLine = true
	return d
}

func (d *InputDialog) Show(title, hint, confirmText, initialValue string, onConfirm func(string)) {
	d.Visible = true
	d.title = title
	d.hint = hint
	d.confirmTx = confirmText
	d.onConfirm = onConfirm
	d.editor.SetText(initialValue)
}

func (d *InputDialog) Hide() {
	d.Visible = false
}

func (d *InputDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
	}
	if d.cancelBtn.Clicked(gtx) {
		d.Hide()
	}

	// Enter to confirm
	for {
		ev, ok := d.editor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			if d.onConfirm != nil {
				d.onConfirm(d.editor.Text())
			}
			d.Hide()
		}
	}
	d.editor.Submit = true

	if d.confirmBtn.Clicked(gtx) {
		if d.onConfirm != nil {
			d.onConfirm(d.editor.Text())
		}
		d.Hide()
	}

	// Overlay
	return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return d.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(350)
				gtx.Constraints.Max.X = gtx.Dp(350)
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(12)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, d.title)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
								// Editor
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutEditor(gtx, &d.editor, d.hint)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutDialogBtn(gtx, d.app.Theme, &d.cancelBtn, "Cancel", ColorInput, ColorText)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutDialogBtn(gtx, d.app.Theme, &d.confirmBtn, d.confirmTx, ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
										}),
									)
								}),
							)
						})
					},
				)
			})
		})
	})
}

func (d *InputDialog) layoutEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
			return layout.Dimensions{Size: sz.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(d.app.Theme.Material, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}


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

	qrcode "github.com/skip2/go-qrcode"
)

type QRDialog struct {
	app     *App
	Visible bool
	title   string
	data    string
	qrImg   image.Image
	qrOp    paint.ImageOp
	qrReady bool

	closeBtn   widget.Clickable
	copyBtn    widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
}

func NewQRDialog(a *App) *QRDialog {
	return &QRDialog{app: a}
}

func (d *QRDialog) Show(title, data string) {
	d.Visible = true
	d.title = title
	d.data = data
	d.qrReady = false

	// Generovat QR kód
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return
	}
	d.qrImg = qr.Image(256)
	d.qrOp = paint.NewImageOp(d.qrImg)
	d.qrReady = true
}

func (d *QRDialog) Hide() {
	d.Visible = false
}

func (d *QRDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
	}
	d.cardBtn.Clicked(gtx)
	if d.closeBtn.Clicked(gtx) {
		d.Hide()
	}
	if d.copyBtn.Clicked(gtx) {
		copyToClipboard(d.data)
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(320)
			gtx.Constraints.Max.X = gtx.Dp(320)

			return d.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(12)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, d.title)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
								// QR image
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !d.qrReady {
										return layout.Dimensions{}
									}
									sz := gtx.Dp(256)
									gtx.Constraints.Min = image.Pt(sz, sz)
									gtx.Constraints.Max = gtx.Constraints.Min
									d.qrOp.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									return layout.Dimensions{Size: image.Pt(sz, sz)}
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
								// Data text
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									text := d.data
									if len(text) > 60 {
										text = text[:60] + "..."
									}
									lbl := material.Caption(d.app.Theme.Material, text)
									lbl.Color = ColorTextDim
									lbl.MaxLines = 2
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return d.layoutBtn(gtx, &d.copyBtn, "Copy Link", ColorInput, ColorText)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return d.layoutBtn(gtx, &d.closeBtn, "Close", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
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

func (d *QRDialog) layoutBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(d.app.Theme.Material, text)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

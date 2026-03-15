package ui

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/p2p"
)

// P2POfferDialog — zobrazí se příjemci, když dostane file.offer.
type P2POfferDialog struct {
	app     *App
	Visible bool

	conn       *ServerConnection
	transfer   *p2p.Transfer
	senderName string

	saveBtn    widget.Clickable
	declineBtn widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
}

func NewP2POfferDialog(a *App) *P2POfferDialog {
	return &P2POfferDialog{app: a}
}

func (d *P2POfferDialog) Show(conn *ServerConnection, t *p2p.Transfer, senderName string) {
	d.Visible = true
	d.conn = conn
	d.transfer = t
	d.senderName = senderName
}

func (d *P2POfferDialog) Hide() {
	d.Visible = false
	d.conn = nil
	d.transfer = nil
	d.senderName = ""
}

func (d *P2POfferDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible || d.transfer == nil {
		return layout.Dimensions{}
	}

	if d.saveBtn.Clicked(gtx) {
		t := d.transfer
		conn := d.conn
		d.Hide()
		// Otevřít save dialog v goroutine (blokuje)
		go func() {
			savePath := saveFileDialog(t.FileName)
			if savePath == "" {
				// Uživatel zrušil save dialog → reject
				if conn != nil && conn.P2P != nil {
					conn.P2P.RejectTransfer(t.ID)
				}
				return
			}
			if conn != nil && conn.P2P != nil {
				conn.P2P.AcceptTransfer(t.ID, savePath)
			}
			d.app.Window.Invalidate()
		}()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	if d.declineBtn.Clicked(gtx) {
		t := d.transfer
		conn := d.conn
		d.Hide()
		if conn != nil && conn.P2P != nil {
			conn.P2P.RejectTransfer(t.ID)
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		t := d.transfer
		conn := d.conn
		d.Hide()
		if conn != nil && conn.P2P != nil {
			conn.P2P.RejectTransfer(t.ID)
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
			cardW := gtx.Dp(380)
			if cardW > gtx.Constraints.Max.X-gtx.Dp(40) {
				cardW = gtx.Constraints.Max.X - gtx.Dp(40)
			}
			gtx.Constraints.Max.X = cardW
			gtx.Constraints.Min.X = cardW

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
									lbl := material.H6(d.app.Theme.Material, "Incoming file")
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								// Message
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										msg := fmt.Sprintf("%s wants to send you %s (%s)",
											d.senderName, d.transfer.FileName, FormatBytes(d.transfer.FileSize))
										lbl := material.Body2(d.app.Theme.Material, msg)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Tlačítka
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return d.layoutBtn(gtx, &d.declineBtn, "Decline", ColorInput, ColorText)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return d.layoutBtn(gtx, &d.saveBtn, "Save as...", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
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

func (d *P2POfferDialog) layoutBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
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

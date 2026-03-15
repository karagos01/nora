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

	"nora-client/store"
)

// LobbyJoinDialog — popup dialog when clicking on a lobby channel
type LobbyJoinDialog struct {
	app        *App
	visible    bool
	lobbyID    string
	lobbyName  string
	nameEditor widget.Editor
	passEditor widget.Editor
	joinBtn    widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable

	// Result after clicking Join
	resultReady    bool
	resultLobbyID  string
	resultName     string
	resultPassword string
}

func NewLobbyJoinDialog(a *App) *LobbyJoinDialog {
	d := &LobbyJoinDialog{app: a}
	d.nameEditor.SingleLine = true
	d.nameEditor.Submit = true
	d.passEditor.SingleLine = true
	d.passEditor.Submit = true
	return d
}

// Show opens the dialog for joining a lobby, pre-fills from cache
func (d *LobbyJoinDialog) Show(lobbyID, lobbyName string, prefs store.LobbyPrefs) {
	d.visible = true
	d.lobbyID = lobbyID
	d.lobbyName = lobbyName
	d.resultReady = false
	d.nameEditor.SetText(prefs.LastName)
	d.passEditor.SetText(prefs.LastPassword)
}

func (d *LobbyJoinDialog) Hide() {
	d.visible = false
}

func (d *LobbyJoinDialog) submit() {
	name := d.nameEditor.Text()
	pass := d.passEditor.Text()
	d.resultReady = true
	d.resultLobbyID = d.lobbyID
	d.resultName = name
	d.resultPassword = pass
	d.Hide()
}

// HandleResult returns the filled values after clicking Join
func (d *LobbyJoinDialog) HandleResult() (lobbyID, name, password string, ok bool) {
	if !d.resultReady {
		return "", "", "", false
	}
	d.resultReady = false
	return d.resultLobbyID, d.resultName, d.resultPassword, true
}

func (d *LobbyJoinDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.visible {
		return layout.Dimensions{}
	}

	// Enter to confirm
	for {
		ev, ok := d.nameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			d.submit()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}
	for {
		ev, ok := d.passEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			d.submit()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}

	if d.joinBtn.Clicked(gtx) {
		d.submit()
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
							return d.layoutContent(gtx)
						})
					},
				)
			})
		}),
	)
}

func (d *LobbyJoinDialog) layoutContent(gtx layout.Context) layout.Dimensions {
	th := d.app.Theme.Material

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Title
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(th, "Join Lobby")
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		}),
		// Subtitle
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th, d.lobbyName)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		// Room name
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th, "Room name:")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutEditor(gtx, &d.nameEditor, "My Room")
			})
		}),
		// Password
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th, "Password (optional):")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutEditor(gtx, &d.passEditor, "")
			})
		}),
		// Buttons
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutDialogBtn(gtx, d.app.Theme, &d.cancelBtn, "Cancel", ColorInput, ColorText)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutDialogBtn(gtx, d.app.Theme, &d.joinBtn, "Join", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
						})
					}),
				)
			})
		}),
	)
}

func (d *LobbyJoinDialog) layoutEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(d.app.Theme.Material, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}


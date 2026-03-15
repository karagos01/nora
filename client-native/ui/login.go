package ui

import (
	"image"
	"image/color"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/store"
)

type LoginView struct {
	app *App

	usernameEditor widget.Editor
	passwordEditor widget.Editor
	unlockBtn      widget.Clickable
	btnHover       gesture.Hover

	errMsg  string
	loading bool

	initialized    bool
	focusRequested bool

	// Create mode — for explicitly creating a new identity
	createMode     bool
	hasIdentities  bool
	toggleModeBtn  widget.Clickable
}

func NewLoginView(a *App) *LoginView {
	v := &LoginView{app: a}
	v.usernameEditor.SingleLine = true
	v.usernameEditor.Submit = true
	v.passwordEditor.SingleLine = true
	v.passwordEditor.Submit = true
	v.passwordEditor.Mask = rune(0x25CF) // ●
	return v
}

func (v *LoginView) init() {
	if v.initialized {
		return
	}
	v.initialized = true

	ids, err := store.LoadIdentities()
	if err == nil && len(ids) > 0 {
		v.hasIdentities = true
		v.usernameEditor.SetText(ids[0].Username)
	}
}

func (v *LoginView) Layout(gtx layout.Context) layout.Dimensions {
	v.init()

	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Handle Enter/Submit from editors
	for {
		ev, ok := v.usernameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.doUnlock()
		}
	}
	for {
		ev, ok := v.passwordEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.doUnlock()
		}
	}

	// Toggle create/unlock mode
	if v.toggleModeBtn.Clicked(gtx) {
		v.createMode = !v.createMode
		v.errMsg = ""
		v.focusRequested = false // re-focus
		if v.createMode {
			v.usernameEditor.SetText("")
		} else if v.hasIdentities {
			ids, _ := store.LoadIdentities()
			if len(ids) > 0 {
				v.usernameEditor.SetText(ids[0].Username)
			}
		}
		v.passwordEditor.SetText("")
	}

	// Auto-focus
	if !v.focusRequested {
		v.focusRequested = true
		if v.hasIdentities && !v.createMode {
			gtx.Execute(key.FocusCmd{Tag: &v.passwordEditor})
		} else {
			gtx.Execute(key.FocusCmd{Tag: &v.usernameEditor})
		}
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(400)
		gtx.Constraints.Min.X = gtx.Dp(400)

		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					title := material.H3(v.app.Theme.Material, "NORA")
					title.Color = ColorAccent
					title.Alignment = 1
					return title.Layout(gtx)
				})
			}),

			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(32)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					sub := material.Body1(v.app.Theme.Material, "No-Oversight Realtime Alternative")
					sub.Color = ColorTextDim
					sub.Alignment = 1
					return sub.Layout(gtx)
				})
			}),

			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutCard(gtx)
			}),

			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.errMsg == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, v.errMsg)
					lbl.Color = ColorDanger
					lbl.Alignment = 1
					return lbl.Layout(gtx)
				})
			}),

			// Version
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.app.Version == "" || v.app.Version == "dev" {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, "build "+v.app.Version)
					lbl.Color = ColorTextDim
					lbl.Alignment = 1
					return lbl.Layout(gtx)
				})
			}),
		)
	})
}

func (v *LoginView) layoutCard(gtx layout.Context) layout.Dimensions {
	isUnlockMode := v.hasIdentities && !v.createMode

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
			return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutField(gtx, v.app.Theme, "Username", &v.usernameEditor)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),

					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutField(gtx, v.app.Theme, "Password", &v.passwordEditor)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						hint := "Your password encrypts your private key locally."
						if isUnlockMode {
							hint = "Enter your password to unlock your identity."
						}
						lbl := material.Caption(v.app.Theme.Material, hint)
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),

					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutButton(gtx)
					}),

					// Toggle mode link
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !v.hasIdentities && !v.createMode {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.toggleModeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								text := "New Identity"
								if v.createMode {
									text = "Back to Unlock"
								}
								lbl := material.Caption(v.app.Theme.Material, text)
								lbl.Color = ColorAccent
								if v.toggleModeBtn.Hovered() {
									pointer.CursorPointer.Add(gtx.Ops)
									lbl.Color = ColorAccentHover
								}
								lbl.Alignment = 1
								return lbl.Layout(gtx)
							})
						})
					}),
				)
			})
		},
	)
}

func (v *LoginView) doUnlock() {
	if v.loading {
		return
	}
	v.loading = true
	v.errMsg = ""
	go func() {
		username := v.usernameEditor.Text()
		password := v.passwordEditor.Text()
		var err error
		if v.createMode {
			err = v.app.CreateNewIdentity(username, password)
		} else {
			err = v.app.Unlock(username, password)
		}
		if err != nil {
			v.errMsg = err.Error()
		} else {
			v.passwordEditor.SetText("")
		}
		v.loading = false
		v.app.Window.Invalidate()
	}()
}

func (v *LoginView) layoutButton(gtx layout.Context) layout.Dimensions {
	if v.unlockBtn.Clicked(gtx) && !v.loading {
		v.doUnlock()
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			c := ColorAccent
			if v.loading {
				c = ColorTextDim
			} else if v.unlockBtn.Hovered() {
				pointer.CursorPointer.Add(gtx.Ops)
				c = ColorAccentHover
			}
			paint.FillShape(gtx.Ops, c, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.unlockBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					text := "Unlock"
					if v.createMode || !v.hasIdentities {
						text = "Create Identity"
					}
					if v.loading {
						text = "..."
					}
					lbl := material.Body1(v.app.Theme.Material, text)
					lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					lbl.Alignment = 1
					return lbl.Layout(gtx)
				})
			})
		},
	)
}

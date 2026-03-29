package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type AddServerView struct {
	app *App

	serverEditor   widget.Editor
	usernameEditor widget.Editor
	connectBtn     widget.Clickable
	cancelBtn      widget.Clickable

	errMsg        string
	loading       bool
	needsUsername bool // server doesn't know this key, registration required
}

func NewAddServerView(a *App) *AddServerView {
	v := &AddServerView{app: a}
	v.serverEditor.SingleLine = true
	v.serverEditor.Submit = true
	v.serverEditor.SetText("194.8.253.161")
	v.usernameEditor.SingleLine = true
	v.usernameEditor.Submit = true
	return v
}

func (v *AddServerView) Reset() {
	v.errMsg = ""
	v.loading = false
	v.needsUsername = false
	v.usernameEditor.SetText(v.app.Username)
}

func (v *AddServerView) Layout(gtx layout.Context) layout.Dimensions {
	// Semi-transparent overlay
	paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Handle Enter/Submit from editors
	for {
		ev, ok := v.serverEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.doConnect()
		}
	}
	for {
		ev, ok := v.usernameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.doConnect()
		}
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(400)
		gtx.Constraints.Min.X = gtx.Dp(400)

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
					var children []layout.FlexChild

					// Title
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.H6(v.app.Theme.Material, "Add Server")
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}))

					// Server address
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutField(gtx, v.app.Theme, "Server address (IP or domain)", &v.serverEditor)
					}))
					children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout))

					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "IP/domain, or invite link (host:port/code). Default port: 9021.")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					}))

					// Username field (shown when registration is needed)
					if v.needsUsername {
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutField(gtx, v.app.Theme, "Username for this server", &v.usernameEditor)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, "Your key is not registered on this server. Choose a username.")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}))
					}

					// Error message
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.errMsg == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, v.errMsg)
							lbl.Color = ColorDanger
							return lbl.Layout(gtx)
						})
					}))

					// Buttons
					children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout))
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.layoutCancelBtn(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.layoutConnectBtn(gtx)
							}),
						)
					}))

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			},
		)
	})
}

func (v *AddServerView) layoutCancelBtn(gtx layout.Context) layout.Dimensions {
	if v.cancelBtn.Clicked(gtx) {
		v.app.ShowAddServer = false
		v.errMsg = ""
		v.needsUsername = false
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			c := ColorCard
			if v.cancelBtn.Hovered() {
				pointer.CursorPointer.Add(gtx.Ops)
				c = ColorHover
			}
			paint.FillShape(gtx.Ops, c, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return v.cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, "Cancel")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			})
		},
	)
}

func (v *AddServerView) layoutConnectBtn(gtx layout.Context) layout.Dimensions {
	if v.connectBtn.Clicked(gtx) && !v.loading {
		v.doConnect()
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			c := ColorAccent
			if v.loading {
				c = ColorTextDim
			} else if v.connectBtn.Hovered() {
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
			return v.connectBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					text := "Connect"
					if v.loading {
						text = "Connecting..."
					}
					lbl := material.Body2(v.app.Theme.Material, text)
					lbl.Color = ColorWhite
					return lbl.Layout(gtx)
				})
			})
		},
	)
}

// parseInviteLink parses "host:port/code" format into server URL and invite code.
func parseInviteLink(input string) (serverURL, inviteCode string) {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimPrefix(input, "https://")

	// Look for invite code: host:port/code
	parts := strings.SplitN(input, "/", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[0], parts[1]
	}
	return input, ""
}

func (v *AddServerView) doConnect() {
	if v.loading {
		return
	}

	username := ""
	if v.needsUsername {
		username = strings.TrimSpace(v.usernameEditor.Text())
		if username == "" {
			v.errMsg = "Username is required for registration"
			return
		}
	}

	serverAddr, inviteCode := parseInviteLink(v.serverEditor.Text())

	v.loading = true
	v.errMsg = ""
	go func() {
		var err error
		if inviteCode != "" {
			err = v.app.ConnectServer(serverAddr, username, inviteCode)
		} else {
			err = v.app.ConnectServer(serverAddr, username)
		}
		if err != nil {
			errStr := err.Error()
			// Server doesn't know this key — need username for registration
			if strings.Contains(errStr, "unknown key") {
				v.needsUsername = true
				v.usernameEditor.SetText(v.app.Username)
				v.errMsg = ""
			} else {
				v.errMsg = errStr
			}
		}
		v.loading = false
		v.app.Window.Invalidate()
	}()
}

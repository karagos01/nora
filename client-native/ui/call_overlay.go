package ui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/voice"
)

// CallOverlay zobrazuje stav DM hovoru přes message area.
type CallOverlay struct {
	app *App

	acceptBtn  widget.Clickable
	declineBtn widget.Clickable
	hangupBtn  widget.Clickable
	cancelBtn  widget.Clickable
	muteBtn    widget.Clickable
	deafenBtn  widget.Clickable
}

func NewCallOverlay(a *App) *CallOverlay {
	return &CallOverlay{app: a}
}

func (o *CallOverlay) Layout(gtx layout.Context, call *voice.CallManager) layout.Dimensions {
	if call == nil || !call.IsActive() {
		return layout.Dimensions{}
	}

	state, peerID, _, startTime := call.GetCallState()

	// Handle button clicks
	if o.acceptBtn.Clicked(gtx) {
		StopCallRingLoop()
		call.AcceptCall()
	}
	if o.declineBtn.Clicked(gtx) {
		StopCallRingLoop()
		call.DeclineCall()
		PlayCallEndSound()
	}
	if o.hangupBtn.Clicked(gtx) {
		StopCallRingLoop()
		call.HangupCall()
		PlayCallEndSound()
	}
	if o.cancelBtn.Clicked(gtx) {
		StopCallRingLoop()
		call.HangupCall()
		PlayCallEndSound()
	}
	if o.muteBtn.Clicked(gtx) {
		call.ToggleMute()
	}
	if o.deafenBtn.Clicked(gtx) {
		call.ToggleDeafen()
	}

	// Resolve peer name
	peerName := "Unknown"
	conn := o.app.Conn()
	if conn != nil {
		o.app.mu.RLock()
		for _, u := range conn.Users {
			if u.ID == peerID {
				peerName = o.app.ResolveUserName(&u)
				break
			}
		}
		o.app.mu.RUnlock()
	}

	// Poloprůhledné tmavé pozadí
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, color.NRGBA{R: 15, G: 15, B: 30, A: 220}, clip.Rect{Max: gtx.Constraints.Max}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(60)}.Layout(gtx)
				}),

				// Avatar kruh
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutCentered2(gtx, func(gtx layout.Context) layout.Dimensions {
						size := gtx.Dp(80)
						clr := UserColor(peerName)

						// Speaking indikátor (zelený ring kolem avataru)
						_, peerSpeaking := call.GetSpeakingState()
						if state == voice.CallConnected && peerSpeaking {
							gtx.Execute(op.InvalidateCmd{At: time.Now().Add(100 * time.Millisecond)})
							ringSize := size + gtx.Dp(8)
							rr := ringSize / 2
							off := (ringSize - size) / 2
							st := op.Offset(image.Pt(-off, -off)).Push(gtx.Ops)
							paint.FillShape(gtx.Ops, ColorOnline, clip.RRect{
								Rect: image.Rect(0, 0, ringSize, ringSize),
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							st.Pop()
						}

						// Pulsující efekt při zvonění
						if state == voice.CallRingingOut || state == voice.CallRingingIn {
							gtx.Execute(op.InvalidateCmd{At: time.Now().Add(50 * time.Millisecond)})
							pulse := float32(time.Now().UnixMilli()%1000) / 1000.0
							if pulse > 0.5 {
								pulse = 1.0 - pulse
							}
							pulseSize := size + int(pulse*float32(gtx.Dp(12)))
							rr := pulseSize / 2
							pulseClr := color.NRGBA{R: clr.R, G: clr.G, B: clr.B, A: 60}
							off := (pulseSize - size) / 2
							st := op.Offset(image.Pt(-off, -off)).Push(gtx.Ops)
							paint.FillShape(gtx.Ops, pulseClr, clip.RRect{
								Rect: image.Rect(0, 0, pulseSize, pulseSize),
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							st.Pop()
						}

						rr := size / 2
						paint.FillShape(gtx.Ops, clr, clip.RRect{
							Rect: image.Rect(0, 0, size, size),
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))

						// Iniciála
						return layout.Stack{Alignment: layout.Center}.Layout(gtx,
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{Size: image.Pt(size, size)}
							}),
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								initial := "?"
								if len(peerName) > 0 {
									initial = string([]rune(peerName)[0])
								}
								lbl := material.H5(o.app.Theme.Material, initial)
								lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
								return lbl.Layout(gtx)
							}),
						)
					})
				}),

				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
				}),

				// Jméno
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutCentered2(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.H6(o.app.Theme.Material, peerName)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					})
				}),

				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(8)}.Layout(gtx)
				}),

				// Status text
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutCentered2(gtx, func(gtx layout.Context) layout.Dimensions {
						var text string
						switch state {
						case voice.CallRingingOut:
							text = "Calling..."
						case voice.CallRingingIn:
							text = "Incoming call"
						case voice.CallConnected:
							dur := time.Since(startTime)
							text = voice.FormatCallDuration(dur)
							gtx.Execute(op.InvalidateCmd{At: time.Now().Add(1 * time.Second)})
						}
						lbl := material.Body1(o.app.Theme.Material, text)
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),

				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Spacer{Height: unit.Dp(32)}.Layout(gtx)
				}),

				// Tlačítka
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutCentered2(gtx, func(gtx layout.Context) layout.Dimensions {
						switch state {
						case voice.CallRingingOut:
							return o.layoutRoundBtn(gtx, &o.cancelBtn, "Cancel", ColorDanger)

						case voice.CallRingingIn:
							return layout.Flex{Spacing: layout.SpaceEvenly}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return o.layoutRoundBtn(gtx, &o.acceptBtn, "Accept", ColorOnline)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Spacer{Width: unit.Dp(24)}.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return o.layoutRoundBtn(gtx, &o.declineBtn, "Decline", ColorDanger)
								}),
							)

						case voice.CallConnected:
							muted, deafened := call.GetMuteState()

							muteText := "Mute"
							if muted {
								muteText = "Unmute"
							}
							deafenText := "Deafen"
							if deafened {
								deafenText = "Undeafen"
							}

							return layout.Flex{Spacing: layout.SpaceEvenly}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									bg := ColorInput
									if muted {
										bg = ColorDanger
									}
									return o.layoutRoundBtn(gtx, &o.muteBtn, muteText, bg)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									bg := ColorInput
									if deafened {
										bg = ColorDanger
									}
									return o.layoutRoundBtn(gtx, &o.deafenBtn, deafenText, bg)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return o.layoutRoundBtn(gtx, &o.hangupBtn, "Hang up", ColorDanger)
								}),
							)
						}
						return layout.Dimensions{}
					})
				}),
			)
		}),
	)
}

// layoutRoundBtn renderuje kulaté tlačítko s textem.
func (o *CallOverlay) layoutRoundBtn(gtx layout.Context, btn *widget.Clickable, text string, bg color.NRGBA) layout.Dimensions {
	hoverBg := bg
	if btn.Hovered() {
		hoverBg = color.NRGBA{
			R: min8(bg.R + 20),
			G: min8(bg.G + 20),
			B: min8(bg.B + 20),
			A: 255,
		}
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(20)
				paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(o.app.Theme.Material, text)
					lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

// layoutCentered2 vycentruje widget horizontálně.
func layoutCentered2(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{}
			})
		}),
		layout.Rigid(w),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{}
		}),
	)
}

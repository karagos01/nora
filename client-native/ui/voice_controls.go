package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/screen"
	"nora-client/voice"
)

// VoiceControls renders the voice channel controls bar at the bottom of the sidebar.
type VoiceControls struct {
	app       *App
	leaveBtn  widget.Clickable
	muteBtn   widget.Clickable
	deafenBtn widget.Clickable
	streamBtn widget.Clickable
	liveWBBtn widget.Clickable
	volumeBtn widget.Clickable

	// Call controls (DM call)
	callHangupBtn widget.Clickable
	callMuteBtn   widget.Clickable
	callDeafenBtn widget.Clickable

	micSlider     Slider
	speakerSlider Slider
	showVolume    bool

	// Stream settings
	streamSettingsBtn widget.Clickable
	showStreamSettings bool
	streamFPS     int // 10, 15, 20, 30
	streamMaxH    int // 720, 1080
	streamQuality int // 0=fast, 1=balanced, 2=quality

	// FPS option buttons
	fpsBtn10 widget.Clickable
	fpsBtn15 widget.Clickable
	fpsBtn20 widget.Clickable
	fpsBtn30 widget.Clickable

	// Resolution option buttons
	resBtn720  widget.Clickable
	resBtn1080 widget.Clickable

	// Quality option buttons
	qualFast     widget.Clickable
	qualBalanced widget.Clickable
	qualHigh     widget.Clickable
}

func NewVoiceControls(a *App) *VoiceControls {
	vc := &VoiceControls{app: a}
	vc.micSlider.Min = 0
	vc.micSlider.Max = 2.0
	vc.micSlider.Value = 1.0
	vc.speakerSlider.Min = 0
	vc.speakerSlider.Max = 2.0
	vc.speakerSlider.Value = 1.0
	vc.streamFPS = 20
	vc.streamMaxH = 1080
	vc.streamQuality = 1 // balanced
	return vc
}

func (vc *VoiceControls) Layout(gtx layout.Context) layout.Dimensions {
	conn := vc.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}

	// DM call has priority
	if conn.Call != nil && conn.Call.IsActive() {
		return vc.layoutCallControls(gtx, conn)
	}

	if conn.Voice == nil || !conn.Voice.IsActive() {
		return layout.Dimensions{}
	}

	// Handle buttons
	if vc.leaveBtn.Clicked(gtx) {
		conn.Voice.Leave()
	}
	if vc.muteBtn.Clicked(gtx) {
		conn.Voice.ToggleMute()
	}
	if vc.deafenBtn.Clicked(gtx) {
		conn.Voice.ToggleDeafen()
	}
	if vc.streamBtn.Clicked(gtx) {
		if conn.Voice != nil {
			if conn.Voice.IsStreaming() {
				chID, _, _ := conn.Voice.GetState()
				conn.Voice.StopStream()
				conn.WS.SendJSON("screen.share", map[string]any{
					"channel_id": chID,
					"sharing":    false,
				})
			} else {
				// Apply settings to manager before starting
				conn.Voice.StreamFPS = vc.streamFPS
				conn.Voice.StreamQuality = vc.streamQuality
				if vc.streamMaxH == 720 {
					conn.Voice.StreamMaxW = 1280
					conn.Voice.StreamMaxH = 720
				} else {
					conn.Voice.StreamMaxW = 1920
					conn.Voice.StreamMaxH = 1080
				}
				conn.Voice.StartStream(func() ([]byte, error) {
					maxW, maxH := 1920, 1080
					if vc.streamMaxH == 720 {
						maxW, maxH = 1280, 720
					}
					return screen.CaptureJPEG(maxW, maxH, 75)
				})
				chID, _, _ := conn.Voice.GetState()
				conn.WS.SendJSON("screen.share", map[string]any{
					"channel_id": chID,
					"sharing":    true,
				})
			}
		}
	}
	if vc.liveWBBtn.Clicked(gtx) {
		if conn.Voice != nil {
			chID, _, _ := conn.Voice.GetState()
			if chID != "" && vc.app.LiveWB != nil {
				if conn.LiveWhiteboards[chID] != "" {
					// Active WB exists — just open it
					vc.app.LiveWB.Open(chID, conn.LiveWhiteboards[chID])
				} else {
					// No active WB — start one
					vc.app.LiveWB.Start(chID)
				}
			}
		}
	}
	if vc.volumeBtn.Clicked(gtx) {
		vc.showVolume = !vc.showVolume
		if vc.showVolume {
			vc.showStreamSettings = false
		}
	}
	if vc.streamSettingsBtn.Clicked(gtx) {
		vc.showStreamSettings = !vc.showStreamSettings
		if vc.showStreamSettings {
			vc.showVolume = false
		}
	}

	// Stream settings buttons
	if vc.fpsBtn10.Clicked(gtx) { vc.streamFPS = 10 }
	if vc.fpsBtn15.Clicked(gtx) { vc.streamFPS = 15 }
	if vc.fpsBtn20.Clicked(gtx) { vc.streamFPS = 20 }
	if vc.fpsBtn30.Clicked(gtx) { vc.streamFPS = 30 }
	if vc.resBtn720.Clicked(gtx)  { vc.streamMaxH = 720 }
	if vc.resBtn1080.Clicked(gtx) { vc.streamMaxH = 1080 }
	if vc.qualFast.Clicked(gtx)     { vc.streamQuality = 0 }
	if vc.qualBalanced.Clicked(gtx) { vc.streamQuality = 1 }
	if vc.qualHigh.Clicked(gtx)     { vc.streamQuality = 2 }

	// Handle slider changes
	if vc.micSlider.Changed() {
		conn.Voice.SetMicVolume(vc.micSlider.Value)
	}
	if vc.speakerSlider.Changed() {
		conn.Voice.SetSpeakerVolume(vc.speakerSlider.Value)
	}

	channelID, muted, deafened := conn.Voice.GetState()
	selfSpeaking, _ := conn.Voice.GetSpeakingState()
	streaming := conn.Voice != nil && conn.Voice.IsStreaming()

	if channelID == "" {
		return layout.Dimensions{}
	}

	// Find channel name
	channelName := channelID
	if len(channelName) > 8 {
		channelName = channelName[:8]
	}
	vc.app.mu.RLock()
	for _, ch := range conn.Channels {
		if ch.ID == channelID {
			channelName = ch.Name
			break
		}
	}
	vc.app.mu.RUnlock()

	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Top divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: bounds.Max}.Op())
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Voice Connected label + channel name
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									// Green dot — pulses when self speaking
									size := gtx.Dp(8)
									dotColor := ColorSuccess
									if selfSpeaking && !muted {
										dotColor = color.NRGBA{R: 100, G: 255, B: 100, A: 255}
									}
									paint.FillShape(gtx.Ops, dotColor, clip.Ellipse{Max: image.Pt(size, size)}.Op(gtx.Ops))
									return layout.Dimensions{Size: image.Pt(size, size)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(vc.app.Theme.Material, "Voice Connected")
										lbl.Color = ColorSuccess
										return lbl.Layout(gtx)
									})
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconVolumeUp, 14, ColorTextDim)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(vc.app.Theme.Material, channelName)
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						}),

						// Row 1: Mic | Sound | Leave
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Spacing: layout.SpaceEvenly}.Layout(gtx,
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										icon := IconMic
										fg := ColorText
										bg := ColorInput
										label := "Mic"
										if muted {
											icon = IconMicOff
											fg = ColorDanger
											bg = withAlpha(ColorDanger, 40)
										}
										return vc.layoutControlIconLabelBtn(gtx, &vc.muteBtn, icon, label, bg, fg)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											icon := IconVolumeUp
											fg := ColorText
											bg := ColorInput
											if deafened {
												icon = IconVolumeOff
												fg = ColorDanger
												bg = withAlpha(ColorDanger, 40)
											}
											return vc.layoutControlIconLabelBtn(gtx, &vc.deafenBtn, icon, "Sound", bg, fg)
										})
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return vc.layoutControlIconLabelBtn(gtx, &vc.leaveBtn, IconCallEnd, "Leave", withAlpha(ColorDanger, 60), ColorDanger)
										})
									}),
								)
							})
						}),

						// Row 2: Stream | WB | StreamCfg | Volume
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Spacing: layout.SpaceEvenly}.Layout(gtx,
									// Stream toggle
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										fg := ColorTextDim
										bg := ColorInput
										label := "Stream"
										if streaming {
											fg = ColorSuccess
											bg = withAlpha(ColorSuccess, 40)
											label = "Live"
										}
										return vc.layoutControlIconLabelBtn(gtx, &vc.streamBtn, IconMonitor, label, bg, fg)
									}),
									// Live whiteboard
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											bg := ColorInput
											fg := ColorTextDim
											chID, _, _ := conn.Voice.GetState()
											if conn.LiveWhiteboards[chID] != "" {
												bg = withAlpha(ColorAccent, 60)
												fg = ColorAccent
											}
											return vc.layoutControlIconLabelBtn(gtx, &vc.liveWBBtn, IconEdit, "WB", bg, fg)
										})
									}),
									// Stream settings
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											bg := ColorInput
											fg := ColorTextDim
											if vc.showStreamSettings {
												bg = withAlpha(ColorAccent, 60)
												fg = ColorAccent
											}
											return vc.layoutControlIconLabelBtn(gtx, &vc.streamSettingsBtn, IconSettings, "Cfg", bg, fg)
										})
									}),
									// Volume toggle
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											bg := ColorInput
											fg := ColorTextDim
											if vc.showVolume {
												bg = withAlpha(ColorAccent, 60)
												fg = ColorAccent
											}
											return vc.layoutControlIconLabelBtn(gtx, &vc.volumeBtn, IconHeadset, "Vol", bg, fg)
										})
									}),
								)
							})
						}),

						// Stream settings panel
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !vc.showStreamSettings {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									// Resolution
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return vc.layoutOptionRow(gtx, "Res",
											[]optionItem{
												{label: "720p", btn: &vc.resBtn720, active: vc.streamMaxH == 720},
												{label: "1080p", btn: &vc.resBtn1080, active: vc.streamMaxH == 1080},
											},
										)
									}),
									// FPS
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return vc.layoutOptionRow(gtx, "FPS",
												[]optionItem{
													{label: "10", btn: &vc.fpsBtn10, active: vc.streamFPS == 10},
													{label: "15", btn: &vc.fpsBtn15, active: vc.streamFPS == 15},
													{label: "20", btn: &vc.fpsBtn20, active: vc.streamFPS == 20},
													{label: "30", btn: &vc.fpsBtn30, active: vc.streamFPS == 30},
												},
											)
										})
									}),
									// Quality
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return vc.layoutOptionRow(gtx, "Qual",
												[]optionItem{
													{label: "Fast", btn: &vc.qualFast, active: vc.streamQuality == 0},
													{label: "Good", btn: &vc.qualBalanced, active: vc.streamQuality == 1},
													{label: "Best", btn: &vc.qualHigh, active: vc.streamQuality == 2},
												},
											)
										})
									}),
								)
							})
						}),

						// Volume sliders (shown when volumeBtn toggled)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !vc.showVolume {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									// Mic volume
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return vc.layoutSliderRow(gtx, "Mic", &vc.micSlider)
									}),
									// Speaker volume
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return vc.layoutSliderRow(gtx, "Speaker", &vc.speakerSlider)
										})
									}),
								)
							})
						}),
					)
				})
			},
		)
		}),
	)
}

type optionItem struct {
	label  string
	btn    *widget.Clickable
	active bool
}

func (vc *VoiceControls) layoutOptionRow(gtx layout.Context, label string, items []optionItem) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(36)
			lbl := material.Caption(vc.app.Theme.Material, label)
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
	}

	for _, item := range items {
		item := item
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg := ColorInput
				fg := ColorTextDim
				if item.active {
					bg = withAlpha(ColorAccent, 80)
					fg = ColorText
				}
				return item.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					hoverBg := bg
					if item.btn.Hovered() {
						pointer.CursorPointer.Add(gtx.Ops)
						hoverBg = color.NRGBA{
							R: min8(bg.R + 15),
							G: min8(bg.G + 15),
							B: min8(bg.B + 15),
							A: bg.A,
						}
						if bg.A == 0 { hoverBg.A = 60 }
					}
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
								Rect: bounds,
								NE: rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(vc.app.Theme.Material, item.label)
									lbl.Color = fg
									lbl.TextSize = vc.app.Theme.Sp(10)
									return lbl.Layout(gtx)
								})
							})
						},
					)
				})
			})
		}))
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

func (vc *VoiceControls) layoutSliderRow(gtx layout.Context, label string, slider *Slider) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(50)
			lbl := material.Caption(vc.app.Theme.Material, label)
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return slider.Layout(gtx, 0) // 0 = use max available width
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				pct := int(slider.Value / slider.Max * 100)
				lbl := material.Caption(vc.app.Theme.Material, fmt.Sprintf("%d%%", pct))
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
	)
}

// layoutCallControls renders the DM call state in the voice controls bar.
func (vc *VoiceControls) layoutCallControls(gtx layout.Context, conn *ServerConnection) layout.Dimensions {
	call := conn.Call

	// Handle buttons
	if vc.callHangupBtn.Clicked(gtx) {
		StopCallRingLoop()
		call.HangupCall()
		PlayCallEndSound()
	}
	if vc.callMuteBtn.Clicked(gtx) {
		call.ToggleMute()
	}
	if vc.callDeafenBtn.Clicked(gtx) {
		call.ToggleDeafen()
	}

	state, peerID, _, startTime := call.GetCallState()
	muted, deafened := call.GetMuteState()

	// Resolve peer name
	peerName := "Unknown"
	vc.app.mu.RLock()
	for _, u := range conn.Users {
		if u.ID == peerID {
			peerName = vc.app.ResolveUserName(&u)
			break
		}
	}
	vc.app.mu.RUnlock()

	var statusText string
	statusColor := ColorAccent
	switch state {
	case voice.CallRingingOut:
		statusText = "Calling " + peerName + "..."
	case voice.CallRingingIn:
		statusText = peerName + " is calling"
	case voice.CallConnected:
		dur := time.Since(startTime)
		statusText = "In call — " + voice.FormatCallDuration(dur)
		statusColor = ColorSuccess
		gtx.Execute(op.InvalidateCmd{At: time.Now().Add(1 * time.Second)})
	}

	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Top divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: bounds.Max}.Op())
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Status
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									size := gtx.Dp(8)
									paint.FillShape(gtx.Ops, statusColor, clip.Ellipse{Max: image.Pt(size, size)}.Op(gtx.Ops))
									return layout.Dimensions{Size: image.Pt(size, size)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(vc.app.Theme.Material, statusText)
										lbl.Color = statusColor
										return lbl.Layout(gtx)
									})
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(vc.app.Theme.Material, peerName)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),

						// Buttons (only when connected or ringing)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								if state == voice.CallConnected {
									return layout.Flex{Spacing: layout.SpaceEvenly}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											label := "Mic"
											fg := ColorText
											bg := ColorInput
											if muted {
												label = "Mic OFF"
												fg = ColorDanger
												bg = withAlpha(ColorDanger, 40)
											}
											return vc.layoutControlBtn(gtx, &vc.callMuteBtn, label, bg, fg)
										}),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												label := "Sound"
												fg := ColorText
												bg := ColorInput
												if deafened {
													label = "Deaf"
													fg = ColorDanger
													bg = withAlpha(ColorDanger, 40)
												}
												return vc.layoutControlBtn(gtx, &vc.callDeafenBtn, label, bg, fg)
											})
										}),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return vc.layoutControlIconBtn(gtx, &vc.callHangupBtn, IconCallEnd, withAlpha(ColorDanger, 60), ColorDanger)
											})
										}),
									)
								}
								// Ringing — only hangup/cancel
								return vc.layoutControlIconBtn(gtx, &vc.callHangupBtn, IconClose, withAlpha(ColorDanger, 60), ColorDanger)
							})
						}),
					)
				})
			},
		)
		}),
	)
}

func (vc *VoiceControls) layoutControlIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, bg, fg color.NRGBA) layout.Dimensions {
	return vc.layoutControlIconLabelBtn(gtx, btn, icon, "", bg, fg)
}

func (vc *VoiceControls) layoutControlIconLabelBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, label string, bg, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hoverBg := bg
		if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			hoverBg = color.NRGBA{
				R: min8(bg.R + 15),
				G: min8(bg.G + 15),
				B: min8(bg.B + 15),
				A: bg.A,
			}
			if bg.A == 0 {
				hoverBg.A = 60
			}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				if label == "" {
					return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, icon, 18, fg)
						})
					})
				}
				return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, icon, 16, fg)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(vc.app.Theme.Material, label)
								lbl.Color = fg
								lbl.TextSize = vc.app.Theme.Sp(9)
								return lbl.Layout(gtx)
							}),
						)
					})
				})
			},
		)
	})
}

func (vc *VoiceControls) layoutControlBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hoverBg := bg
		if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			hoverBg = color.NRGBA{
				R: min8(bg.R + 15),
				G: min8(bg.G + 15),
				B: min8(bg.B + 15),
				A: bg.A,
			}
			if bg.A == 0 {
				hoverBg.A = 60
			}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(vc.app.Theme.Material, text)
						lbl.Color = fg
						return lbl.Layout(gtx)
					})
				})
			},
		)
	})
}

package ui

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"nora-client/api"
)

func (v *MessageView) layoutPoll(gtx layout.Context, idx int, msg api.Message) layout.Dimensions {
	poll := msg.Poll
	if poll == nil {
		return layout.Dimensions{}
	}

	th := v.app.Theme.Material
	conn := v.app.Conn()
	myUserID := ""
	if conn != nil {
		myUserID = conn.UserID
	}

	// Total vote count
	totalVotes := 0
	for _, opt := range poll.Options {
		totalVotes += opt.Count
	}

	// Type badge text
	typeLabel := "Single choice"
	if poll.PollType == "multi" {
		typeLabel = "Multiple choice"
	} else if poll.PollType == "anonymous" {
		typeLabel = "Anonymous"
	}

	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var children []layout.FlexChild

					// Type badge + expiration
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(th, typeLabel)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								expLabel := FormatExpiration(poll.ExpiresAt)
								if expLabel == "" {
									return layout.Dimensions{}
								}
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(th, expLabel)
									if expLabel == "Expired" {
										lbl.Color = color.NRGBA{R: 255, G: 80, B: 80, A: 255}
									} else {
										lbl.Color = ColorTextDim
									}
									return lbl.Layout(gtx)
								})
							}),
						)
					}))

					// Question
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body1(th, poll.Question)
							lbl.Color = ColorText
							lbl.Font.Weight = 700
							return lbl.Layout(gtx)
						})
					}))

					// Options
					for j, opt := range poll.Options {
						if j >= len(v.actions[idx].pollOptBtns) {
							break
						}
						optCopy := opt
						btnIdx := j

						// Check if I voted on this option
						iVoted := false
						for _, uid := range optCopy.UserIDs {
							if uid == myUserID {
								iVoted = true
								break
							}
						}
						// For anonymous: cannot check via count, but server returns user_ids=nil.
						// If anonymous, we cannot locally determine if I voted — leave as false.

						pct := 0
						if totalVotes > 0 {
							pct = optCopy.Count * 100 / totalVotes
						}

						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.actions[idx].pollOptBtns[btnIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									maxW := gtx.Constraints.Max.X
									barW := maxW * pct / 100

									bg := ColorInput
									if v.actions[idx].pollOptBtns[btnIdx].Hovered() {
										bg = ColorHover
									}

									return layout.Background{}.Layout(gtx,
										func(gtx layout.Context) layout.Dimensions {
											h := gtx.Constraints.Min.Y
											bounds := image.Rect(0, 0, maxW, h)
											rr := gtx.Dp(4)
											paint.FillShape(gtx.Ops, bg, clip.RRect{
												Rect: bounds,
												NE:   rr, NW: rr, SE: rr, SW: rr,
											}.Op(gtx.Ops))

											// Progress fill
											if barW > 0 {
												fillColor := ColorAccentDim
												if iVoted {
													fillColor = ColorAccent
												}
												fillBounds := image.Rect(0, 0, barW, h)
												paint.FillShape(gtx.Ops, fillColor, clip.RRect{
													Rect: fillBounds,
													NE:   rr, NW: rr, SE: rr, SW: rr,
												}.Op(gtx.Ops))
											}
											return layout.Dimensions{Size: bounds.Max}
										},
										func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx,
													layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
														col := ColorText
														if iVoted {
															col = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
														}
														lbl := material.Body2(th, optCopy.Label)
														lbl.Color = col
														return lbl.Layout(gtx)
													}),
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														txt := fmt.Sprintf("%d", optCopy.Count)
														if totalVotes > 0 {
															txt = fmt.Sprintf("%d (%d%%)", optCopy.Count, pct)
														}
														lbl := material.Caption(th, txt)
														lbl.Color = ColorTextDim
														return lbl.Layout(gtx)
													}),
												)
											})
										},
									)
								})
							})
						}))
					}

					// Total
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							txt := fmt.Sprintf("%d votes", totalVotes)
							if totalVotes == 1 {
								txt = "1 vote"
							}
							lbl := material.Caption(th, txt)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}))

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			},
		)
	})
}

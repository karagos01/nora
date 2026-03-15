package ui

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

var reactionEmojis = []string{
	"\U0001f44d", "\U0001f44e", "\u2764\ufe0f", "\U0001f602",
	"\U0001f622", "\U0001f621", "\U0001f60e", "\U0001f389",
	"\U0001f44f", "\U0001f525", "\U0001f914", "\U0001f440",
}

func (v *MessageView) layoutReactions(gtx layout.Context, idx int, msg api.Message) layout.Dimensions {
	conn := v.app.Conn()
	myUserID := ""
	if conn != nil {
		myUserID = conn.UserID
	}

	var items []layout.FlexChild
	for j, r := range msg.Reactions {
		if j >= len(v.actions[idx].reactionBtns) {
			break
		}
		reaction := r
		btnIdx := j
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			btn := &v.actions[idx].reactionBtns[btnIdx]

			// Check if current user reacted
			isMine := false
			for _, uid := range reaction.UserIDs {
				if uid == myUserID {
					isMine = true
					break
				}
			}

			bg := ColorInput
			if isMine {
				bg = color.NRGBA{R: 60, G: 40, B: 100, A: 255}
			}
			if btn.Hovered() {
				bg = ColorHover
			}

			return layout.Inset{Right: unit.Dp(4), Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, bg, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								text := fmt.Sprintf("%s %d", reaction.Emoji, reaction.Count)
								lbl := material.Caption(v.app.Theme.Material, text)
								if isMine {
									lbl.Color = ColorAccent
								} else {
									lbl.Color = ColorText
								}
								return lbl.Layout(gtx)
							})
						},
					)
				})
			})
		}))
	}
	return layout.Flex{Alignment: layout.End}.Layout(gtx, items...)
}

func (v *MessageView) layoutReactionPicker(gtx layout.Context, idx int) layout.Dimensions {
	if v.reactPickerMsgIdx != idx {
		return layout.Dimensions{}
	}
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.End}.Layout(gtx,
						func() []layout.FlexChild {
							var items []layout.FlexChild
							for j := range reactionEmojis {
								ej := j
								items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return v.reactPickerBtns[ej].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										bg := color.NRGBA{}
										if v.reactPickerBtns[ej].Hovered() {
											bg = ColorHover
										}
										return layout.Background{}.Layout(gtx,
											func(gtx layout.Context) layout.Dimensions {
												bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
												if bg.A > 0 {
													rr := gtx.Dp(4)
													paint.FillShape(gtx.Ops, bg, clip.RRect{
														Rect: bounds,
														NE:   rr, NW: rr, SE: rr, SW: rr,
													}.Op(gtx.Ops))
												}
												return layout.Dimensions{Size: bounds.Max}
											},
											func(gtx layout.Context) layout.Dimensions {
												return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body1(v.app.Theme.Material, reactionEmojis[ej])
													lbl.Color = ColorText
													return lbl.Layout(gtx)
												})
											},
										)
									})
								}))
							}
							return items
						}()...,
					)
				})
			},
		)
	})
}


func (v *MessageView) layoutEmojiPicker(gtx layout.Context) layout.Dimensions {
	emojis := v.getEmojis()

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		maxH := gtx.Dp(240)
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
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Category tabs
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutEmojiCategoryTabs(gtx, emojis)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						// Emoji grid
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Max.Y = maxH
							if v.emojiCategoryIdx == 0 {
								return v.layoutCustomEmojiList(gtx, emojis)
							}
							catIdx := v.emojiCategoryIdx - 1
							if catIdx >= len(UnicodeEmojiCategories) {
								catIdx = 0
							}
							return v.layoutUnicodeEmojiGrid(gtx, catIdx)
						}),
					)
				})
			},
		)
	})
}

func (v *MessageView) layoutEmojiCategoryTabs(gtx layout.Context, customEmojis []api.CustomEmoji) layout.Dimensions {
	var items []layout.FlexChild

	// Custom emoji tab
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutEmojiTab(gtx, &v.emojiCatBtns[0], "Server", v.emojiCategoryIdx == 0)
	}))

	// Unicode categories
	for i, cat := range UnicodeEmojiCategories {
		idx := i
		name := cat.Name
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutEmojiTab(gtx, &v.emojiCatBtns[idx+1], name, v.emojiCategoryIdx == idx+1)
		}))
	}

	return layout.Flex{}.Layout(gtx, items...)
}

func (v *MessageView) layoutEmojiTab(gtx layout.Context, btn *widget.Clickable, name string, active bool) layout.Dimensions {
	return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bg := color.NRGBA{}
			if active {
				bg = ColorAccentDim
			} else if btn.Hovered() {
				bg = ColorHover
			}
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
					if bg.A > 0 {
						rr := gtx.Dp(4)
						paint.FillShape(gtx.Ops, bg, clip.RRect{
							Rect: bounds,
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
					}
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, name)
						if active {
							lbl.Color = ColorText
						} else {
							lbl.Color = ColorTextDim
						}
						return lbl.Layout(gtx)
					})
				},
			)
		})
	})
}

func (v *MessageView) layoutCustomEmojiList(gtx layout.Context, emojis []api.CustomEmoji) layout.Dimensions {
	if len(emojis) == 0 {
		lbl := material.Caption(v.app.Theme.Material, "No custom emojis on this server")
		lbl.Color = ColorTextDim
		return lbl.Layout(gtx)
	}

	v.emojiList.Axis = layout.Vertical
	return material.List(v.app.Theme.Material, &v.emojiList).Layout(gtx, len(emojis), func(gtx layout.Context, idx int) layout.Dimensions {
		e := emojis[idx]
		return v.emojiClickBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bg := color.NRGBA{}
			if v.emojiClickBtns[idx].Hovered() {
				bg = ColorHover
			}
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					if bg.A == 0 {
						return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, gtx.Constraints.Min.Y)}
					}
					bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(4)
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.End}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if e.URL == "" {
									return layout.Dimensions{}
								}
								srvURL := ""
								if c := v.app.Conn(); c != nil {
									srvURL = c.URL
								}
								if srvURL == "" {
									return layout.Dimensions{}
								}
								ci := v.app.Images.Get(srvURL+e.URL, func() { v.app.Window.Invalidate() })
								if ci == nil || !ci.ok {
									return layout.Dimensions{}
								}
								h := gtx.Dp(24)
								imgBounds := ci.img.Bounds()
								imgW := imgBounds.Dx()
								imgH := imgBounds.Dy()
								w := h
								if imgH > 0 {
									w = h * imgW / imgH
								}
								scaleX := float32(w) / float32(imgW)
								scaleY := float32(h) / float32(imgH)
								defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
								defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()
								ci.op.Add(gtx.Ops)
								paint.PaintOp{}.Add(gtx.Ops)
								return layout.Dimensions{Size: image.Pt(w, h)}
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, ":"+e.Name+":")
									lbl.Color = ColorAccent
									return lbl.Layout(gtx)
								})
							}),
						)
					})
				},
			)
		})
	})
}

func (v *MessageView) layoutUnicodeEmojiGrid(gtx layout.Context, catIdx int) layout.Dimensions {
	cat := UnicodeEmojiCategories[catIdx]

	// Calculate button offset for this category
	btnOffset := 0
	for i := 0; i < catIdx; i++ {
		btnOffset += len(UnicodeEmojiCategories[i].Emojis)
	}

	cellSize := gtx.Dp(36)
	cols := gtx.Constraints.Max.X / cellSize
	if cols < 1 {
		cols = 1
	}
	rows := (len(cat.Emojis) + cols - 1) / cols

	v.emojiList.Axis = layout.Vertical
	return material.List(v.app.Theme.Material, &v.emojiList).Layout(gtx, rows, func(gtx layout.Context, rowIdx int) layout.Dimensions {
		var items []layout.FlexChild
		for c := 0; c < cols; c++ {
			emojiIdx := rowIdx*cols + c
			if emojiIdx >= len(cat.Emojis) {
				break
			}
			emoji := cat.Emojis[emojiIdx]
			bIdx := btnOffset + emojiIdx
			if bIdx >= len(v.unicodeEmojiBtns) {
				break
			}
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.unicodeEmojiBtns[bIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := color.NRGBA{}
					if v.unicodeEmojiBtns[bIdx].Hovered() {
						bg = ColorHover
					}
					sz := image.Pt(cellSize, cellSize)
					gtx.Constraints.Min = sz
					gtx.Constraints.Max = sz
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							if bg.A > 0 {
								rr := gtx.Dp(4)
								paint.FillShape(gtx.Ops, bg, clip.RRect{
									Rect: image.Rect(0, 0, cellSize, cellSize),
									NE:   rr, NW: rr, SE: rr, SW: rr,
								}.Op(gtx.Ops))
							}
							return layout.Dimensions{Size: sz}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body1(v.app.Theme.Material, emoji)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							})
						},
					)
				})
			}))
		}
		return layout.Flex{}.Layout(gtx, items...)
	})
}


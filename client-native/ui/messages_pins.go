package ui

import (
	"image"
	"image/color"
	"strings"

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

type pinListAction struct {
	unpinBtn       widget.Clickable
	replyBtn       widget.Clickable
	copyBtn        widget.Clickable
	deleteBtn      widget.Clickable
	editBtn        widget.Clickable
	thumbUpBtn     widget.Clickable
	thumbDownBtn   widget.Clickable
	linkPreviewBtn widget.Clickable
	attBtns        [4]widget.Clickable
}

func (v *MessageView) layoutPinBar(gtx layout.Context) layout.Dimensions {
	// Bar with the latest pinned message
	latest := v.pinnedMsgs[0]
	author := v.app.ResolveUserName(latest.Author)

	var perms int64
	isOwn := false
	if conn := v.app.Conn(); conn != nil {
		perms = conn.MyPermissions
		isOwn = latest.UserID == conn.UserID
	}
	canPin := perms&(api.PermManageMessages|api.PermAdmin) != 0
	canDelete := isOwn || perms&(api.PermManageMessages|api.PermAdmin) != 0

	expanded := v.pinExpanded

	// Red outline only when pin hasn't been seen yet
	isNew := v.pinSeenID != latest.ID

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				if isNew {
					// Red outline all around
					borderColor := color.NRGBA{R: 200, G: 50, B: 50, A: 255}
					bw := gtx.Dp(2)
					paint.FillShape(gtx.Ops, borderColor, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					inner := image.Rect(bw, bw, bounds.Max.X-bw, bounds.Max.Y-bw)
					irr := rr - bw
					if irr < 0 {
						irr = 0
					}
					paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
						Rect: inner,
						NE:   irr, NW: irr, SE: irr, SW: irr,
					}.Op(gtx.Ops))
				} else {
					paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Compact row: pin icon + author + preview + arrow
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.pinBarBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									// Pin icon
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										pinClr := ColorTextDim
										if isNew {
											pinClr = color.NRGBA{R: 200, G: 50, B: 50, A: 255}
										}
										return layoutIcon(gtx, IconPin, 14, pinClr)
									}),
									// Author
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, author)
											if conn := v.app.Conn(); conn != nil {
												lbl.Color = v.app.GetUserRoleColor(conn, latest.UserID, author)
											} else {
												lbl.Color = UserColor(author)
											}
											lbl.Font.Weight = 600
											return lbl.Layout(gtx)
										})
									}),
									// Truncated preview (first line only)
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										if expanded || latest.Content == "" {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											preview := latest.Content
											if nl := strings.IndexByte(preview, '\n'); nl >= 0 {
												preview = preview[:nl]
											}
											if len(preview) > 120 {
												preview = preview[:120] + "..."
											}
											lbl := material.Caption(v.app.Theme.Material, preview)
											lbl.Color = ColorTextDim
											lbl.MaxLines = 1
											return lbl.Layout(gtx)
										})
									}),
									// Expand/collapse arrow
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											arrow := "▼"
											if expanded {
												arrow = "▲"
											}
											lbl := material.Caption(v.app.Theme.Material, arrow)
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						}),
						// Expanded content
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !expanded {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								var items []layout.FlexChild
								if latest.Content != "" {
									emojis := v.getEmojis()
									sURL := ""
									if c := v.app.Conn(); c != nil {
										sURL = c.URL
									}
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutMessageContent(gtx, v.app.Theme, latest.Content, emojis, &v.pinLinks, nil, nil, nil, &v.pinSels, v.app, sURL)
									}))
								}
								for j, att := range latest.Attachments {
									if j >= len(v.pinAttBtns) {
										break
									}
									a := att
									idx := j
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutPinAttachment(gtx, idx, a)
										})
									}))
								}
								if ytURL := findYouTubeURLInText(latest.Content); ytURL != "" {
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutYouTubeEmbed(gtx, v.app, latest.ID, ytURL)
										})
									}))
								}
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
							})
						}),
						// Action buttons (only when expanded)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !expanded {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallAction(gtx, &v.pinThumbUpBtn, "\U0001f44d", ColorTextDim)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallAction(gtx, &v.pinThumbDownBtn, "\U0001f44e", ColorTextDim)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallIconAction(gtx, &v.pinCopyBtn, IconCopy, ColorTextDim)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallIconAction(gtx, &v.pinReplyBtn, IconReply, ColorTextDim)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if !canPin {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallAction(gtx, &v.pinUnpinBtn, "Unpin", ColorAccent)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if !isOwn {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallIconAction(gtx, &v.pinEditBtn, IconEdit, ColorTextDim)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if !canDelete {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallIconAction(gtx, &v.pinDeleteBtn, IconDelete, ColorDanger)
										})
									}),
								)
							})
						}),
					)
				})
			},
		)
	})
}

func (v *MessageView) layoutPinAttachment(gtx layout.Context, idx int, att api.Attachment) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}

	// Inline image preview
	if strings.HasPrefix(att.MimeType, "image/") {
		url := conn.URL + att.URL
		ci := v.app.Images.Get(url, func() { v.app.Window.Invalidate() })
		if ci == nil {
			lbl := material.Caption(v.app.Theme.Material, "Loading "+att.Filename+"...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}
		if ci.ok {
			imgBounds := ci.img.Bounds()
			imgW := imgBounds.Dx()
			imgH := imgBounds.Dy()
			maxW := gtx.Dp(300)
			maxH := gtx.Dp(200)
			if imgW > maxW {
				imgH = imgH * maxW / imgW
				imgW = maxW
			}
			if imgH > maxH {
				imgW = imgW * maxH / imgH
				imgH = maxH
			}
			return v.pinAttBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				origW := float32(ci.img.Bounds().Dx())
				origH := float32(ci.img.Bounds().Dy())
				scaleX := float32(imgW) / origW
				scaleY := float32(imgH) / origH

				defer clip.Rect{Max: image.Pt(imgW, imgH)}.Push(gtx.Ops).Pop()
				aff := f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))
				op.Affine(aff).Add(gtx.Ops)
				ci.op.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(imgW, imgH)}
			})
		}
	}

	// File / video — clickable link
	btn := &v.pinAttBtns[idx]
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
			bg = ColorHover
		}
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					icon := IconDownload
					label := att.Filename + " (" + FormatBytes(att.Size) + ")"
					if isVideoMIME(att.MimeType) {
						icon = IconPlayArrow
						label = att.Filename + " — click to play"
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, icon, 14, ColorAccent)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, label)
								lbl.Color = ColorAccent
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	})
}

func (v *MessageView) layoutPinnedMessages(gtx layout.Context) layout.Dimensions {
	maxH := gtx.Dp(400)
	gtx.Constraints.Max.Y = maxH
	v.pinsList.Axis = layout.Vertical

	// Ensure enough slots
	for len(v.pinsListLinks) < len(v.pinnedMsgs) {
		v.pinsListLinks = append(v.pinsListLinks, MsgLinks{})
	}
	for len(v.pinsListSels) < len(v.pinnedMsgs) {
		v.pinsListSels = append(v.pinsListSels, nil)
	}
	for len(v.pinsListActs) < len(v.pinnedMsgs) {
		v.pinsListActs = append(v.pinsListActs, pinListAction{})
	}

	conn := v.app.Conn()
	sURL := ""
	var perms int64
	var myID string
	if conn != nil {
		sURL = conn.URL
		perms = conn.MyPermissions
		myID = conn.UserID
	}
	emojis := v.getEmojis()
	canPin := perms&(api.PermManageMessages|api.PermAdmin) != 0

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Skip the first message — it's in the pin bar
					listLen := len(v.pinnedMsgs) - 1
					if listLen < 0 {
						listLen = 0
					}
					return material.List(v.app.Theme.Material, &v.pinsList).Layout(gtx, listLen, func(gtx layout.Context, idx int) layout.Dimensions {
						realIdx := idx + 1
						msg := v.pinnedMsgs[realIdx]
						author := v.app.ResolveUserName(msg.Author)
						act := &v.pinsListActs[realIdx]
						isOwn := msg.UserID == myID
						canDel := isOwn || perms&(api.PermManageMessages|api.PermAdmin) != 0

						return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							var rows []layout.FlexChild
							// Author + time
							rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.End}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, author)
										if conn := v.app.Conn(); conn != nil {
											lbl.Color = v.app.GetUserRoleColor(conn, msg.UserID, author)
										} else {
											lbl.Color = UserColor(author)
										}
										lbl.Font.Weight = 600
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, FormatDateTime(msg.CreatedAt))
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									}),
								)
							}))
							// Content
							if msg.Content != "" {
								rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutMessageContent(gtx, v.app.Theme, msg.Content, emojis, &v.pinsListLinks[realIdx], nil, nil, nil, &v.pinsListSels[realIdx], v.app, sURL)
								}))
							}
							// Attachments (image preview + files)
							for j, att := range msg.Attachments {
								if j >= len(act.attBtns) {
									break
								}
								a := att
								ai := j
								rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.layoutPinsListAttachment(gtx, act, ai, a, sURL)
									})
								}))
							}
							// Link preview (OpenGraph)
							if msg.LinkPreview != nil && (msg.LinkPreview.Title != "" || msg.LinkPreview.Description != "") {
								lp := msg.LinkPreview
								rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return act.linkPreviewBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											maxW := gtx.Dp(420)
											if gtx.Constraints.Max.X > maxW {
												gtx.Constraints.Max.X = maxW
											}
											return layout.Background{}.Layout(gtx,
												func(gtx layout.Context) layout.Dimensions {
													bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
													rr := gtx.Dp(6)
													paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
													bw := gtx.Dp(3)
													paint.FillShape(gtx.Ops, ColorAccent, clip.RRect{Rect: image.Rect(0, 0, bw, bounds.Max.Y), NW: rr, SW: rr}.Op(gtx.Ops))
													return layout.Dimensions{Size: bounds.Max}
												},
												func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														var lpRows []layout.FlexChild
														if lp.SiteName != "" {
															lpRows = append(lpRows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																lbl := material.Caption(v.app.Theme.Material, lp.SiteName)
																lbl.Color = ColorTextDim
																return lbl.Layout(gtx)
															}))
														}
														if lp.Title != "" {
															lpRows = append(lpRows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																	lbl := material.Body2(v.app.Theme.Material, lp.Title)
																	lbl.Color = ColorAccent
																	lbl.Font.Weight = 600
																	lbl.MaxLines = 2
																	return lbl.Layout(gtx)
																})
															}))
														}
														if lp.Description != "" {
															lpRows = append(lpRows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																	lbl := material.Caption(v.app.Theme.Material, lp.Description)
																	lbl.Color = ColorText
																	lbl.MaxLines = 3
																	return lbl.Layout(gtx)
																})
															}))
														}
														if lp.ImageURL != "" {
															lpRows = append(lpRows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																ci := v.app.Images.Get(lp.ImageURL, func() { v.app.Window.Invalidate() })
																if ci == nil || ci.img == nil {
																	return layout.Dimensions{}
																}
																return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																	imgW := ci.img.Bounds().Dx()
																	imgH := ci.img.Bounds().Dy()
																	if imgW == 0 || imgH == 0 {
																		return layout.Dimensions{}
																	}
																	maxIW := gtx.Constraints.Max.X
																	maxIH := gtx.Dp(120)
																	scale := float32(maxIW) / float32(imgW)
																	if int(float32(imgH)*scale) > maxIH {
																		scale = float32(maxIH) / float32(imgH)
																	}
																	if scale > 1 {
																		scale = 1
																	}
																	w := int(float32(imgW) * scale)
																	h := int(float32(imgH) * scale)
																	rr := gtx.Dp(4)
																	rc := clip.RRect{Rect: image.Rect(0, 0, w, h), NE: rr, NW: rr, SE: rr, SW: rr}
																	stack := rc.Push(gtx.Ops)
																	imgOp := paint.NewImageOp(ci.img)
																	imgOp.Filter = paint.FilterLinear
																	imgOp.Add(gtx.Ops)
																	paint.PaintOp{}.Add(gtx.Ops)
																	stack.Pop()
																	return layout.Dimensions{Size: image.Pt(w, h)}
																})
															}))
														}
														return layout.Flex{Axis: layout.Vertical}.Layout(gtx, lpRows...)
													})
												},
											)
										})
									})
								}))
							}
							// YouTube embed
							if ytURL := findYouTubeURLInText(msg.Content); ytURL != "" {
								msgID := msg.ID
								rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutYouTubeEmbed(gtx, v.app, msgID, ytURL)
									})
								}))
							}
							// Action buttons
							rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									var btns []layout.FlexChild
									btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallAction(gtx, &act.thumbUpBtn, "\U0001f44d", ColorTextDim)
									}))
									btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallAction(gtx, &act.thumbDownBtn, "\U0001f44e", ColorTextDim)
										})
									}))
									btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallIconAction(gtx, &act.copyBtn, IconCopy, ColorTextDim)
										})
									}))
									btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallIconAction(gtx, &act.replyBtn, IconReply, ColorTextDim)
										})
									}))
									if canPin {
										btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.layoutSmallAction(gtx, &act.unpinBtn, "Unpin", ColorAccent)
											})
										}))
									}
									if isOwn {
										btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.layoutSmallIconAction(gtx, &act.editBtn, IconEdit, ColorTextDim)
											})
										}))
									}
									if canDel {
										btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.layoutSmallIconAction(gtx, &act.deleteBtn, IconDelete, ColorDanger)
											})
										}))
									}
									return layout.Flex{}.Layout(gtx, btns...)
								})
							}))
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
						})
					})
				})
			},
		)
	})
}

func (v *MessageView) layoutPinsListAttachment(gtx layout.Context, act *pinListAction, idx int, att api.Attachment, serverURL string) layout.Dimensions {
	// Inline image preview
	if strings.HasPrefix(att.MimeType, "image/") && serverURL != "" {
		url := serverURL + att.URL
		ci := v.app.Images.Get(url, func() { v.app.Window.Invalidate() })
		if ci == nil {
			lbl := material.Caption(v.app.Theme.Material, "Loading "+att.Filename+"...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}
		if ci.ok {
			imgBounds := ci.img.Bounds()
			imgW := imgBounds.Dx()
			imgH := imgBounds.Dy()
			maxW := gtx.Dp(250)
			maxH := gtx.Dp(150)
			if imgW > maxW {
				imgH = imgH * maxW / imgW
				imgW = maxW
			}
			if imgH > maxH {
				imgW = imgW * maxH / imgH
				imgH = maxH
			}
			return act.attBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				origW := float32(ci.img.Bounds().Dx())
				origH := float32(ci.img.Bounds().Dy())
				scaleX := float32(imgW) / origW
				scaleY := float32(imgH) / origH
				defer clip.Rect{Max: image.Pt(imgW, imgH)}.Push(gtx.Ops).Pop()
				aff := f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))
				op.Affine(aff).Add(gtx.Ops)
				ci.op.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(imgW, imgH)}
			})
		}
	}

	// File / video
	btn := &act.attBtns[idx]
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
			bg = ColorHover
		}
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					icon := IconDownload
					label := att.Filename + " (" + FormatBytes(att.Size) + ")"
					if isVideoMIME(att.MimeType) {
						icon = IconPlayArrow
						label = att.Filename + " — click to play"
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, icon, 14, ColorAccent)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, label)
								lbl.Color = ColorAccent
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	})
}

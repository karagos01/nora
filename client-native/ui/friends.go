package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type friendRequestBtns struct {
	accept  widget.Clickable
	decline widget.Clickable
}

type FriendListView struct {
	app            *App
	list           widget.List
	friendBtns     []widget.Clickable
	shareBtns      []widget.Clickable
	reqBtns        []friendRequestBtns
	sentCancelBtns []widget.Clickable

	// Add friend
	addFriendEditor widget.Editor
	addFriendBtn    widget.Clickable
	addFriendError  string
}

func NewFriendListView(a *App) *FriendListView {
	v := &FriendListView{app: a}
	v.list.Axis = layout.Vertical
	v.addFriendEditor.SingleLine = true
	return v
}

func (v *FriendListView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutColoredBg(gtx, ColorCard)
	}

	v.app.mu.RLock()
	friends := conn.Friends
	requests := conn.FriendRequests
	sentRequests := conn.SentFriendRequests
	myUserID := conn.UserID
	onlineUsers := conn.OnlineUsers
	// Build map of user → share access level + ID
	type friendShareInfo struct {
		access  shareAccess
		shareID string
	}
	friendShares := make(map[string]friendShareInfo)
	for _, s := range conn.MyShares {
		friendShares[s.OwnerID] = friendShareInfo{shareWrite, s.ID}
	}
	for _, s := range conn.SharedWithMe {
		acc := shareRead
		if s.CanWrite {
			acc = shareWrite
		}
		if prev, ok := friendShares[s.OwnerID]; !ok || acc > prev.access {
			friendShares[s.OwnerID] = friendShareInfo{acc, s.ID}
		}
	}
	v.app.mu.RUnlock()

	if len(v.friendBtns) < len(friends) {
		v.friendBtns = make([]widget.Clickable, len(friends)+10)
	}
	if len(v.shareBtns) < len(friends) {
		v.shareBtns = make([]widget.Clickable, len(friends)+10)
	}
	if len(v.reqBtns) < len(requests) {
		v.reqBtns = make([]friendRequestBtns, len(requests)+5)
	}
	if len(v.sentCancelBtns) < len(sentRequests) {
		v.sentCancelBtns = make([]widget.Clickable, len(sentRequests)+5)
	}

	// Handle request button clicks
	for i, req := range requests {
		if v.reqBtns[i].accept.Clicked(gtx) {
			reqID := req.ID
			go func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.AcceptFriendRequest(reqID); err != nil {
						log.Printf("AcceptFriendRequest error: %v", err)
					}
				}
			}()
		}
		if v.reqBtns[i].decline.Clicked(gtx) {
			reqID := req.ID
			go func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.DeclineFriendRequest(reqID); err != nil {
						log.Printf("DeclineFriendRequest error: %v", err)
					}
				}
			}()
		}
	}

	// Handle sent request cancel clicks
	for i, req := range sentRequests {
		if v.sentCancelBtns[i].Clicked(gtx) {
			reqID := req.ID
			go func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.DeclineFriendRequest(reqID); err != nil {
						log.Printf("CancelFriendRequest error: %v", err)
					}
				}
			}()
		}
	}

	// Handle friend clicks
	for i, f := range friends {
		if v.friendBtns[i].Clicked(gtx) && f.ID != myUserID {
			v.app.UserPopup.Show(f.ID, v.app.ResolveUserName(&f))
		}
	}

	// Handle add friend
	if v.addFriendBtn.Clicked(gtx) {
		username := v.addFriendEditor.Text()
		if username != "" {
			// Find user by username/display name in members
			var targetID string
			v.app.mu.RLock()
			for _, u := range conn.Users {
				if u.Username == username || u.DisplayName == username {
					targetID = u.ID
					break
				}
			}
			v.app.mu.RUnlock()

			if targetID == "" {
				v.addFriendError = "User not found"
			} else if targetID == myUserID {
				v.addFriendError = "Cannot add yourself"
			} else {
				v.addFriendError = ""
				go func() {
					if c := v.app.Conn(); c != nil {
						if err := c.Client.SendFriendRequest(targetID); err != nil {
							v.addFriendError = err.Error()
							v.app.Window.Invalidate()
						} else {
							v.addFriendEditor.SetText("")
							v.app.Window.Invalidate()
						}
					}
				}()
			}
		}
	}

	// Build layout items
	var items []layout.FlexChild

	// Add friend input
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								ed := material.Editor(v.app.Theme.Material, &v.addFriendEditor, "Add friend...")
								ed.Color = ColorText
								ed.HintColor = ColorTextDim
								return ed.Layout(gtx)
							})
						},
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.layoutSmallIconBtn(gtx, &v.addFriendBtn, IconPersonAdd, ColorAccent)
					})
				}),
			)
		})
	}))
	if v.addFriendError != "" {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, v.addFriendError)
				lbl.Color = ColorDanger
				return lbl.Layout(gtx)
			})
		}))
	}

	// Divider after add friend
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}))

	// Friend requests section
	if len(requests) > 0 {
		items = append(items,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("REQUESTS (%d)", len(requests)))
					lbl.Color = ColorAccent
					return lbl.Layout(gtx)
				})
			}),
		)

		for i, req := range requests {
			idx := i
			r := req
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutFriendRequest(gtx, idx, r)
			}))
		}

		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
				paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
				return layout.Dimensions{Size: size}
			})
		}))
	}

	// Sent requests section
	if len(sentRequests) > 0 {
		items = append(items,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("SENT (%d)", len(sentRequests)))
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
		)
		for i, req := range sentRequests {
			idx := i
			r := req
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutSentRequest(gtx, idx, r)
			}))
		}
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
				paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
				return layout.Dimensions{Size: size}
			})
		}))
	}

	// Friends header
	items = append(items,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "FRIENDS")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
	)

	// Friends list
	if len(friends) == 0 {
		items = append(items, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No friends yet")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		items = append(items, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(v.app.Theme.Material, &v.list).Layout(gtx, len(friends), func(gtx layout.Context, idx int) layout.Dimensions {
				f := friends[idx]
				online := onlineUsers[f.ID]
				fsi := friendShares[f.ID]

				// Handle share icon click
				if v.shareBtns[idx].Clicked(gtx) && fsi.access != shareNone && fsi.shareID != "" {
					v.app.Mode = ViewShares
					v.app.SharesView.selectShare(fsi.shareID)
				}

				return v.friendBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := color.NRGBA{}
					if v.friendBtns[idx].Hovered() {
						pointer.CursorPointer.Add(gtx.Ops)
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
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										size := gtx.Dp(8)
										clr := ColorOffline
										if online {
											clr = ColorOnline
										}
										paint.FillShape(gtx.Ops, clr, clip.Ellipse{
											Max: image.Pt(size, size),
										}.Op(gtx.Ops))
										return layout.Dimensions{Size: image.Pt(size, size)}
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											dn := v.app.ResolveUserName(&f)
											lbl := material.Body2(v.app.Theme.Material, dn)
											nameColor := UserColor(dn)
											if !online {
												nameColor = color.NRGBA{
													R: nameColor.R/2 + ColorOffline.R/2,
													G: nameColor.G/2 + ColorOffline.G/2,
													B: nameColor.B/2 + ColorOffline.B/2,
													A: 255,
												}
											}
											lbl.Color = nameColor
											return lbl.Layout(gtx)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if fsi.access == shareNone {
											return layout.Dimensions{}
										}
										clr := ColorTextDim // read-only = gray
										if fsi.access == shareWrite {
											clr = ColorOnline // write = green
										}
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.shareBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												if v.shareBtns[idx].Hovered() {
													pointer.CursorPointer.Add(gtx.Ops)
												}
												return layoutIcon(gtx, IconFolder, 14, clr)
											})
										})
									}),
								)
							})
						},
					)
				})
			})
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *FriendListView) layoutFriendRequest(gtx layout.Context, idx int, req api.FriendRequest) layout.Dimensions {
	username := v.app.ResolveUserName(req.FromUser)

	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Username
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, username)
							lbl.Color = UserColor(username)
							return lbl.Layout(gtx)
						}),
						// Accept / Decline buttons
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.reqBtns[idx].accept, "Accept", ColorOnline)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallBtn(gtx, &v.reqBtns[idx].decline, "Decline", ColorDanger)
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

func (v *FriendListView) layoutSmallIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, clr color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{R: clr.R / 4, G: clr.G / 4, B: clr.B / 4, A: 255}
		if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = color.NRGBA{R: clr.R / 3, G: clr.G / 3, B: clr.B / 3, A: 255}
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, icon, 16, clr)
				})
			},
		)
	})
}

func (v *FriendListView) layoutSmallBtn(gtx layout.Context, btn *widget.Clickable, text string, clr color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{R: clr.R / 4, G: clr.G / 4, B: clr.B / 4, A: 255}
		if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = color.NRGBA{R: clr.R / 3, G: clr.G / 3, B: clr.B / 3, A: 255}
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, text)
					lbl.Color = clr
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *FriendListView) layoutSentRequest(gtx layout.Context, idx int, req api.FriendRequest) layout.Dimensions {
	username := v.app.ResolveUserName(req.ToUser)

	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, username)
							lbl.Color = UserColor(username)
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.sentCancelBtns[idx], "Cancel", ColorDanger)
						}),
					)
				})
			},
		)
	})
}

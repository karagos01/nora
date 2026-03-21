package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"strings"

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
	addFriendEditor   widget.Editor
	addFriendBtn      widget.Clickable
	addFriendError    string
	autocompleteBtns  []widget.Clickable
	autocompleteUsers []api.User
}

func NewFriendListView(a *App) *FriendListView {
	v := &FriendListView{app: a}
	v.list.Axis = layout.Vertical
	v.addFriendEditor.SingleLine = true
	v.addFriendEditor.Submit = true
	return v
}

func (v *FriendListView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutColoredBg(gtx, ColorCard)
	}

	// Unified friends from contacts DB (cross-server)
	unifiedFriends := v.app.GetUnifiedFriends()

	// Requests from ALL servers
	v.app.mu.RLock()
	myUserID := conn.UserID
	type taggedRequest struct {
		api.FriendRequest
		ServerName string
		ServerIdx  int
	}
	var requests []taggedRequest
	var sentRequests []taggedRequest
	for srvIdx, s := range v.app.Servers {
		for _, r := range s.FriendRequests {
			requests = append(requests, taggedRequest{r, s.Name, srvIdx})
		}
		for _, r := range s.SentFriendRequests {
			sentRequests = append(sentRequests, taggedRequest{r, s.Name, srvIdx})
		}
	}
	v.app.mu.RUnlock()

	if len(v.friendBtns) < len(unifiedFriends) {
		v.friendBtns = make([]widget.Clickable, len(unifiedFriends)+10)
	}
	if len(v.shareBtns) < len(unifiedFriends) {
		v.shareBtns = make([]widget.Clickable, len(unifiedFriends)+10)
	}
	if len(v.reqBtns) < len(requests) {
		v.reqBtns = make([]friendRequestBtns, len(requests)+5)
	}
	if len(v.sentCancelBtns) < len(sentRequests) {
		v.sentCancelBtns = make([]widget.Clickable, len(sentRequests)+5)
	}

	// Handle request button clicks
	for i, req := range requests {
		if i >= len(v.reqBtns) {
			break
		}
		if v.reqBtns[i].accept.Clicked(gtx) {
			reqID := req.ID
			srvIdx := req.ServerIdx
			go func() {
				v.app.mu.RLock()
				if srvIdx >= 0 && srvIdx < len(v.app.Servers) {
					s := v.app.Servers[srvIdx]
					v.app.mu.RUnlock()
					if err := s.Client.AcceptFriendRequest(reqID); err != nil {
						log.Printf("AcceptFriendRequest error: %v", err)
					}
				} else {
					v.app.mu.RUnlock()
				}
			}()
		}
		if v.reqBtns[i].decline.Clicked(gtx) {
			reqID := req.ID
			srvIdx := req.ServerIdx
			go func() {
				v.app.mu.RLock()
				if srvIdx >= 0 && srvIdx < len(v.app.Servers) {
					s := v.app.Servers[srvIdx]
					v.app.mu.RUnlock()
					if err := s.Client.DeclineFriendRequest(reqID); err != nil {
						log.Printf("DeclineFriendRequest error: %v", err)
					}
				} else {
					v.app.mu.RUnlock()
				}
			}()
		}
	}

	// Handle sent request cancel clicks
	for i, req := range sentRequests {
		if i >= len(v.sentCancelBtns) {
			break
		}
		if v.sentCancelBtns[i].Clicked(gtx) {
			reqID := req.ID
			srvIdx := req.ServerIdx
			go func() {
				v.app.mu.RLock()
				if srvIdx >= 0 && srvIdx < len(v.app.Servers) {
					s := v.app.Servers[srvIdx]
					v.app.mu.RUnlock()
					if err := s.Client.DeclineFriendRequest(reqID); err != nil {
						log.Printf("CancelFriendRequest error: %v", err)
					}
				} else {
					v.app.mu.RUnlock()
				}
			}()
		}
	}

	// Handle friend clicks
	for i, uf := range unifiedFriends {
		if i < len(v.friendBtns) && v.friendBtns[i].Clicked(gtx) {
			// Find user on any server to get ID for popup
			v.app.mu.RLock()
			if user, _ := v.app.FindUserAcrossServers(uf.PublicKey); user != nil {
				v.app.mu.RUnlock()
				v.app.UserPopup.Show(user.ID, uf.Name)
			} else {
				v.app.mu.RUnlock()
			}
		}
	}

	// Handle autocomplete clicks
	for i := range v.autocompleteUsers {
		if i < len(v.autocompleteBtns) && v.autocompleteBtns[i].Clicked(gtx) {
			v.sendFriendRequestTo(v.autocompleteUsers[i].ID)
		}
	}

	// Handle add friend button / submit
	if v.addFriendBtn.Clicked(gtx) {
		v.doAddFriend()
	}
	for {
		ev, ok := v.addFriendEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.doAddFriend()
		}
	}

	// Build autocomplete list
	query := strings.ToLower(strings.TrimSpace(v.addFriendEditor.Text()))
	v.autocompleteUsers = nil
	if len(query) >= 2 && conn != nil {
		// Check if it looks like a public key (long hex string)
		isKey := len(query) > 20
		if !isKey {
			v.app.mu.RLock()
			seen := make(map[string]bool)
			for _, s := range v.app.Servers {
				for _, u := range s.Members {
					if u.ID == myUserID || seen[u.PublicKey] {
						continue
					}
					name := strings.ToLower(u.Username)
					disp := strings.ToLower(u.DisplayName)
					if strings.Contains(name, query) || strings.Contains(disp, query) {
						v.autocompleteUsers = append(v.autocompleteUsers, u)
						if u.PublicKey != "" {
							seen[u.PublicKey] = true
						}
						if len(v.autocompleteUsers) >= 8 {
							break
						}
					}
				}
				if len(v.autocompleteUsers) >= 8 {
					break
				}
			}
			v.app.mu.RUnlock()
		}
	}
	if len(v.autocompleteBtns) < len(v.autocompleteUsers) {
		v.autocompleteBtns = make([]widget.Clickable, len(v.autocompleteUsers)+5)
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
								hint := "Name, key, or key#server..."
							ed := material.Editor(v.app.Theme.Material, &v.addFriendEditor, hint)
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

	// Autocomplete dropdown
	if len(v.autocompleteUsers) > 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var acItems []layout.FlexChild
				for i, u := range v.autocompleteUsers {
					idx := i
					user := u
					acItems = append(acItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.autocompleteBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := color.NRGBA{}
							if v.autocompleteBtns[idx].Hovered() {
								bg = ColorHover
							}
							if bg.A > 0 {
								paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}.Op())
							}
							return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										dn := user.DisplayName
										if dn == "" {
											dn = user.Username
										}
										return layoutAvatar(gtx, v.app, dn, user.AvatarURL, 20)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											dn := user.DisplayName
											if dn == "" {
												dn = user.Username
											}
											lbl := material.Body2(v.app.Theme.Material, dn)
											lbl.Color = ColorText
											return lbl.Layout(gtx)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconPersonAdd, 14, ColorAccent)
										})
									}),
								)
							})
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, acItems...)
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
				return v.layoutFriendRequest(gtx, idx, r.FriendRequest)
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
				return v.layoutSentRequest(gtx, idx, r.FriendRequest)
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
	if len(unifiedFriends) == 0 {
		items = append(items, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No friends yet")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		items = append(items, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(v.app.Theme.Material, &v.list).Layout(gtx, len(unifiedFriends), func(gtx layout.Context, idx int) layout.Dimensions {
				uf := unifiedFriends[idx]

				return v.friendBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := color.NRGBA{}
					if v.friendBtns[idx].Hovered() {
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
										return layout.Stack{Alignment: layout.SE}.Layout(gtx,
											layout.Stacked(func(gtx layout.Context) layout.Dimensions {
												return layoutAvatar(gtx, v.app, uf.Name, uf.AvatarURL, 24)
											}),
											layout.Stacked(func(gtx layout.Context) layout.Dimensions {
												sz := gtx.Dp(8)
												clr := ColorOffline
												if uf.Online {
													clr = ColorOnline
												}
												paint.FillShape(gtx.Ops, clr, clip.Ellipse{
													Max: image.Pt(sz, sz),
												}.Op(gtx.Ops))
												return layout.Dimensions{Size: image.Pt(sz, sz)}
											}),
										)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(v.app.Theme.Material, uf.Name)
											nameColor := UserColor(uf.Name)
											if !uf.Online {
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

func (v *FriendListView) doAddFriend() {
	input := strings.TrimSpace(v.addFriendEditor.Text())
	if input == "" {
		return
	}

	// Format: key#server — cross-server contact sharing
	if strings.Contains(input, "#") && len(input) > 20 {
		parts := strings.SplitN(input, "#", 2)
		pubKey := strings.TrimSpace(parts[0])
		serverURL := strings.TrimSpace(parts[1])

		// Save to contacts DB
		if v.app.Contacts != nil {
			v.app.Contacts.EnsureContact(pubKey, "", serverURL, "")
			v.app.Contacts.SetFriend(pubKey, true)
		}

		// Try to send friend request on the target server
		v.addFriendError = ""
		go func() {
			sent := false
			v.app.mu.RLock()
			for _, s := range v.app.Servers {
				if s.URL == serverURL || strings.Contains(s.URL, serverURL) || strings.Contains(serverURL, s.URL) {
					// We're connected to this server — send request there
					v.app.mu.RUnlock()
					if err := s.Client.SendFriendRequestByKey(pubKey); err != nil {
						v.addFriendError = err.Error()
					} else {
						v.addFriendEditor.SetText("")
					}
					sent = true
					v.app.Window.Invalidate()
					return
				}
			}
			v.app.mu.RUnlock()
			if !sent {
				// Not connected to that server — just save contact locally
				v.addFriendEditor.SetText("")
				v.app.Window.Invalidate()
			}
		}()
		return
	}

	// Check if input looks like a public key (long hex/base64 string)
	if len(input) > 20 {
		v.addFriendError = ""
		conn := v.app.Conn()
		if conn == nil {
			return
		}
		go func() {
			if err := conn.Client.SendFriendRequestByKey(input); err != nil {
				v.addFriendError = err.Error()
			} else {
				v.addFriendEditor.SetText("")
			}
			v.app.Window.Invalidate()
		}()
		return
	}

	// Find by username/display name across ALL servers
	var targetID string
	var targetConn *ServerConnection
	v.app.mu.RLock()
	for _, s := range v.app.Servers {
		for _, u := range s.Users {
			if strings.EqualFold(u.Username, input) || strings.EqualFold(u.DisplayName, input) {
				targetID = u.ID
				targetConn = s
				break
			}
		}
		if targetID != "" {
			break
		}
	}
	myID := ""
	if conn := v.app.Conn(); conn != nil {
		myID = conn.UserID
	}
	v.app.mu.RUnlock()

	if targetID == "" {
		v.addFriendError = "User not found on any server"
	} else if targetID == myID {
		v.addFriendError = "Cannot add yourself"
	} else if targetConn != nil {
		v.sendFriendRequestOn(targetConn, targetID)
	}
}

func (v *FriendListView) sendFriendRequestTo(userID string) {
	v.sendFriendRequestOn(v.app.Conn(), userID)
}

func (v *FriendListView) sendFriendRequestOn(c *ServerConnection, userID string) {
	if c == nil {
		return
	}
	v.addFriendError = ""
	go func() {
		if c != nil {
			if err := c.Client.SendFriendRequest(userID); err != nil {
				v.addFriendError = err.Error()
			} else {
				v.addFriendEditor.SetText("")
			}
			v.app.Window.Invalidate()
		}
	}()
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

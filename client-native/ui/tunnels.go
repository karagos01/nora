package ui

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

// TunnelView displays and manages personal VPN tunnels
type TunnelView struct {
	app  *App
	list widget.List

	// Buttons per tunnel (accept/close)
	acceptBtns []widget.Clickable
	closeBtns  []widget.Clickable

	// New tunnel
	createBtn   widget.Clickable
	friendBtns  []widget.Clickable
	showCreate  bool
	friendList  widget.List
}

func NewTunnelView(a *App) *TunnelView {
	tv := &TunnelView{app: a}
	tv.list.Axis = layout.Vertical
	tv.friendList.Axis = layout.Vertical
	return tv
}

func (tv *TunnelView) Layout(gtx layout.Context) layout.Dimensions {
	a := tv.app
	conn := a.Conn()
	if conn == nil {
		return layoutCentered(gtx, a.Theme, "Not connected", ColorTextDim)
	}

	a.mu.RLock()
	tunnels := make([]api.Tunnel, len(conn.Tunnels))
	copy(tunnels, conn.Tunnels)
	userID := conn.UserID
	a.mu.RUnlock()

	// Ensure enough buttons
	if len(tv.acceptBtns) < len(tunnels)+1 {
		tv.acceptBtns = make([]widget.Clickable, len(tunnels)+10)
		tv.closeBtns = make([]widget.Clickable, len(tunnels)+10)
	}

	// Handle clicks
	for i, t := range tunnels {
		if i < len(tv.acceptBtns) && tv.acceptBtns[i].Clicked(gtx) {
			tunnel := t
			go func() {
				// Generate WG keypair for accept
				pubKey := fmt.Sprintf("tunnel-accept-%s", tunnel.ID[:8])
				resp, err := conn.Client.AcceptTunnel(tunnel.ID, pubKey)
				if err == nil {
					a.mu.Lock()
					for j, tt := range conn.Tunnels {
						if tt.ID == resp.Tunnel.ID {
							conn.Tunnels[j] = resp.Tunnel
							break
						}
					}
					a.mu.Unlock()
					a.Window.Invalidate()
				}
			}()
		}
		if i < len(tv.closeBtns) && tv.closeBtns[i].Clicked(gtx) {
			tunnelID := t.ID
			go func() {
				if conn.Client.CloseTunnel(tunnelID) == nil {
					a.mu.Lock()
					for j, tt := range conn.Tunnels {
						if tt.ID == tunnelID {
							conn.Tunnels = append(conn.Tunnels[:j], conn.Tunnels[j+1:]...)
							break
						}
					}
					a.mu.Unlock()
					a.Window.Invalidate()
				}
			}()
		}
	}

	// Create button klik
	if tv.createBtn.Clicked(gtx) {
		tv.showCreate = !tv.showCreate
	}

	// Friend list kliky (create tunnel)
	a.mu.RLock()
	friends := make([]api.User, len(conn.Friends))
	copy(friends, conn.Friends)
	a.mu.RUnlock()

	if len(tv.friendBtns) < len(friends)+1 {
		tv.friendBtns = make([]widget.Clickable, len(friends)+10)
	}

	for i, f := range friends {
		if i < len(tv.friendBtns) && tv.friendBtns[i].Clicked(gtx) {
			targetID := f.ID
			go func() {
				pubKey := fmt.Sprintf("tunnel-create-%s", targetID[:8])
				resp, err := conn.Client.CreateTunnel(targetID, pubKey)
				if err == nil {
					a.mu.Lock()
					conn.Tunnels = append(conn.Tunnels, resp.Tunnel)
					a.mu.Unlock()
					tv.showCreate = false
					a.Window.Invalidate()
				}
			}()
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.H6(a.Theme.Material, "VPN Tunnels")
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(a.Theme.Material, &tv.createBtn, "New Tunnel")
						btn.Background = ColorAccent
						btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						return btn.Layout(gtx)
					}),
				)
			})
		}),
		// Friend picker (for creating a tunnel)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !tv.showCreate {
				return layout.Dimensions{}
			}
			return tv.layoutFriendPicker(gtx, friends, tunnels, userID)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Tunnel list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(tunnels) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(a.Theme.Material, "No active tunnels. Create one to connect with a friend via VPN.")
					lbl.Color = ColorTextDim
					lbl.Alignment = 1 // center
					return lbl.Layout(gtx)
				})
			}
			return material.List(a.Theme.Material, &tv.list).Layout(gtx, len(tunnels), func(gtx layout.Context, i int) layout.Dimensions {
				return tv.layoutTunnelCard(gtx, tunnels[i], i, userID)
			})
		}),
	)
}

// layoutFriendPicker displays the friend list for selecting a tunnel target
func (tv *TunnelView) layoutFriendPicker(gtx layout.Context, friends []api.User, tunnels []api.Tunnel, myID string) layout.Dimensions {
	a := tv.app

	// Filter friends — exclude those with an existing tunnel
	var available []api.User
	var availIdxs []int
	for i, f := range friends {
		hasTunnel := false
		for _, t := range tunnels {
			if (t.CreatorID == myID && t.TargetID == f.ID) || (t.TargetID == myID && t.CreatorID == f.ID) {
				hasTunnel = true
				break
			}
		}
		if !hasTunnel {
			available = append(available, f)
			availIdxs = append(availIdxs, i)
		}
	}

	if len(available) == 0 {
		return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(a.Theme.Material, "No friends available for tunnel")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}

	maxH := gtx.Dp(200)
	gtx.Constraints.Max.Y = maxH

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(a.Theme.Material, "Select a friend:")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.List(a.Theme.Material, &tv.friendList).Layout(gtx, len(available), func(gtx layout.Context, i int) layout.Dimensions {
					idx := availIdxs[i]
					friend := available[i]
					return tv.friendBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						bg := color.NRGBA{A: 0}
						if tv.friendBtns[idx].Hovered() {
							bg = ColorHover
						}
						paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}.Op())
						return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconPerson, 18, ColorAccent)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(a.Theme.Material, friend.Username)
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					})
				})
			}),
		)
	})
}

// layoutTunnelCard renders a single tunnel card
func (tv *TunnelView) layoutTunnelCard(gtx layout.Context, t api.Tunnel, idx int, myID string) layout.Dimensions {
	a := tv.app

	// Determine peer name
	peerName := t.TargetName
	peerIP := t.TargetIP
	myIP := t.CreatorIP
	isCreator := t.CreatorID == myID
	if !isCreator {
		peerName = t.CreatorName
		peerIP = t.CreatorIP
		myIP = t.TargetIP
	}

	// Status color
	statusColor := ColorTextDim
	statusText := t.Status
	switch t.Status {
	case "pending":
		statusColor = color.NRGBA{R: 255, G: 180, B: 0, A: 255} // yellow
		if isCreator {
			statusText = "Waiting for acceptance"
		} else {
			statusText = "Incoming request"
		}
	case "active":
		statusColor = color.NRGBA{R: 80, G: 200, B: 120, A: 255} // green
		statusText = "Connected"
	}

	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Main row: icon + name + status
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconPerson, 24, ColorAccent)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body1(a.Theme.Material, peerName)
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									})
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
								}),
								// Status badge
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(a.Theme.Material, statusText)
									lbl.Color = statusColor
									return lbl.Layout(gtx)
								}),
							)
						}),
						// IP info (if active)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if t.Status != "active" {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(34)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(a.Theme.Material, fmt.Sprintf("Your IP: %s", myIP))
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(a.Theme.Material, fmt.Sprintf("Peer IP: %s", peerIP))
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
								)
							})
						}),
						// Buttons
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
									// Accept button (only for target on pending)
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if t.Status != "pending" || isCreator {
											return layout.Dimensions{}
										}
										btn := material.Button(a.Theme.Material, &tv.acceptBtns[idx], "Accept")
										btn.Background = color.NRGBA{R: 80, G: 200, B: 120, A: 255}
										btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
										return btn.Layout(gtx)
									}),
									// Close/Decline button
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										label := "Close"
										if t.Status == "pending" && !isCreator {
											label = "Decline"
										}
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											btn := material.Button(a.Theme.Material, &tv.closeBtns[idx], label)
											btn.Background = ColorDanger
											btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
											return btn.Layout(gtx)
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

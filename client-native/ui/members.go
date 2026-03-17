package ui

import (
	"image"
	"image/color"
	"sort"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type MemberView struct {
	app        *App
	list       widget.List
	memberBtns []widget.Clickable
}

func NewMemberView(a *App) *MemberView {
	v := &MemberView{app: a}
	v.list.Axis = layout.Vertical
	return v
}

func (v *MemberView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutColoredBg(gtx, ColorCard)
	}

	v.app.mu.RLock()
	type memberInfo struct {
		id         string
		username   string
		avatarURL  string
		online     bool
		isOwner    bool
		status     string
		statusText string
	}
	members := make([]memberInfo, len(conn.Members))
	for i, m := range conn.Members {
		displayName := v.app.ResolveUserName(&conn.Members[i])
		members[i] = memberInfo{
			id:         m.ID,
			username:   displayName,
			avatarURL:  m.AvatarURL,
			online:     conn.OnlineUsers[m.ID],
			isOwner:    m.IsOwner,
			status:     conn.UserStatuses[m.ID],
			statusText: conn.UserStatusText[m.ID],
		}
	}
	myUserID := conn.UserID
	v.app.mu.RUnlock()

	sort.Slice(members, func(i, j int) bool {
		if members[i].online != members[j].online {
			return members[i].online
		}
		return members[i].username < members[j].username
	})

	var onlineCount int
	for _, m := range members {
		if m.online {
			onlineCount++
		}
	}

	// Ensure enough click handlers
	if len(v.memberBtns) < len(members) {
		v.memberBtns = make([]widget.Clickable, len(members)+10)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "MEMBERS")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),

		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(v.app.Theme.Material, &v.list).Layout(gtx, len(members), func(gtx layout.Context, idx int) layout.Dimensions {
				m := members[idx]

				// Handle click — show user popup
				if v.memberBtns[idx].Clicked(gtx) && m.id != myUserID {
					v.app.UserPopup.Show(m.id, m.username)
				}

				var sectionHeader string
				if idx == 0 && m.online {
					sectionHeader = "Online"
				} else if idx == onlineCount && !m.online {
					sectionHeader = "Offline"
				}

				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if sectionHeader == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, sectionHeader)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.memberBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := color.NRGBA{}
							if v.memberBtns[idx].Hovered() && m.id != myUserID {
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
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											// Avatar (24px) with online indicator
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Stack{Alignment: layout.SE}.Layout(gtx,
													layout.Stacked(func(gtx layout.Context) layout.Dimensions {
														return layoutAvatar(gtx, v.app, m.username, m.avatarURL, 24)
													}),
													layout.Stacked(func(gtx layout.Context) layout.Dimensions {
														sz := gtx.Dp(8)
														clr := ColorOffline
														if m.online {
															switch m.status {
															case "away":
																clr = ColorStatusAway
															case "dnd":
																clr = ColorStatusDND
															default:
																clr = ColorOnline
															}
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
													return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															lbl := material.Body2(v.app.Theme.Material, m.username)
															nameColor := UserColor(m.username)
															if conn := v.app.Conn(); conn != nil {
																nameColor = v.app.GetUserRoleColor(conn, m.id, m.username)
															}
															if !m.online {
																nameColor = color.NRGBA{
																	R: nameColor.R/2 + ColorOffline.R/2,
																	G: nameColor.G/2 + ColorOffline.G/2,
																	B: nameColor.B/2 + ColorOffline.B/2,
																	A: 255,
																}
															}
															lbl.Color = nameColor
															return lbl.Layout(gtx)
														}),
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															if !m.isOwner {
																return layout.Dimensions{}
															}
															return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																lbl := material.Caption(v.app.Theme.Material, "owner")
																lbl.Color = ColorAccent
																return lbl.Layout(gtx)
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
					}),
				)
			})
		}),
	)
}


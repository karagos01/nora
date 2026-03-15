package ui

import (
	"fmt"
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

// notifItemKind rozlišuje typ notifikace.
type notifItemKind int

const (
	notifFriendRequest notifItemKind = iota
	notifUnreadChannel
	notifUnreadDM
	notifUnreadGroup
)

// notifItem je jedna položka v notification centeru.
type notifItem struct {
	Kind        notifItemKind
	ServerIdx   int
	ServerName  string
	ChannelID   string
	ChannelName string
	ConvID      string
	PeerName    string
	GroupID     string
	GroupName   string
	Count       int
	UserID      string // pro friend request
	Username    string // pro friend request
}

// NotificationCenter — agregovaný přehled nepřečtených zpráv a notifikací.
type NotificationCenter struct {
	app      *App
	list     widget.List
	items    []notifItem
	itemBtns []widget.Clickable
}

func NewNotificationCenter(a *App) *NotificationCenter {
	nc := &NotificationCenter{app: a}
	nc.list.Axis = layout.Vertical
	return nc
}

// totalUnread vrací celkový počet nepřečtených notifikací ze všech serverů.
func (nc *NotificationCenter) totalUnread() int {
	a := nc.app
	total := 0
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, conn := range a.Servers {
		for _, c := range conn.UnreadCount {
			if c > 0 {
				total += c
			}
		}
		for _, c := range conn.UnreadDMCount {
			if c > 0 {
				total += c
			}
		}
		for _, u := range conn.UnreadGroups {
			if u {
				total++
			}
		}
		total += len(conn.FriendRequests)
	}
	return total
}

// refresh znovu sestaví seznam notifikačních položek ze všech serverů.
func (nc *NotificationCenter) refresh() {
	nc.items = nc.items[:0]
	a := nc.app
	a.mu.RLock()
	defer a.mu.RUnlock()

	for srvIdx, conn := range a.Servers {
		// Friend requesty
		for _, fr := range conn.FriendRequests {
			username := ""
			if fr.FromUser != nil {
				username = fr.FromUser.Username
			}
			nc.items = append(nc.items, notifItem{
				Kind:       notifFriendRequest,
				ServerIdx:  srvIdx,
				ServerName: conn.Name,
				UserID:     fr.FromUserID,
				Username:   username,
			})
		}

		// Unread channels
		for chID, count := range conn.UnreadCount {
			if count <= 0 {
				continue
			}
			chName := ""
			for _, ch := range conn.Channels {
				if ch.ID == chID {
					chName = ch.Name
					break
				}
			}
			nc.items = append(nc.items, notifItem{
				Kind:        notifUnreadChannel,
				ServerIdx:   srvIdx,
				ServerName:  conn.Name,
				ChannelID:   chID,
				ChannelName: chName,
				Count:       count,
			})
		}

		// Unread DMs
		for convID, count := range conn.UnreadDMCount {
			if count <= 0 {
				continue
			}
			peerName := ""
			for _, conv := range conn.DMConversations {
				if conv.ID == convID {
					for _, p := range conv.Participants {
						if p.UserID != conn.UserID {
							for _, u := range conn.Users {
								if u.ID == p.UserID {
									peerName = u.Username
									break
								}
							}
							break
						}
					}
					break
				}
			}
			nc.items = append(nc.items, notifItem{
				Kind:       notifUnreadDM,
				ServerIdx:  srvIdx,
				ServerName: conn.Name,
				ConvID:     convID,
				PeerName:   peerName,
				Count:      count,
			})
		}

		// Unread groups
		for groupID, has := range conn.UnreadGroups {
			if !has {
				continue
			}
			groupName := ""
			for _, g := range conn.Groups {
				if g.ID == groupID {
					groupName = g.Name
					break
				}
			}
			nc.items = append(nc.items, notifItem{
				Kind:       notifUnreadGroup,
				ServerIdx:  srvIdx,
				ServerName: conn.Name,
				GroupID:    groupID,
				GroupName:  groupName,
				Count:      1,
			})
		}
	}

	// Seřadit: friend requesty nahoře, pak kanály, DM, groups
	sort.SliceStable(nc.items, func(i, j int) bool {
		return nc.items[i].Kind < nc.items[j].Kind
	})

	// Zajistit dostatek tlačítek
	if len(nc.itemBtns) < len(nc.items) {
		nc.itemBtns = make([]widget.Clickable, len(nc.items)+10)
	}
}

// LayoutSidebar renderuje seznam notifikací v sidebaru (300px panel).
func (nc *NotificationCenter) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	nc.refresh()

	// Zpracovat kliknutí na položky
	for i := range nc.items {
		if i < len(nc.itemBtns) && nc.itemBtns[i].Clicked(gtx) {
			nc.handleClick(i)
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return nc.layoutHeader(gtx)
		}),
		// Seznam položek
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())
			if len(nc.items) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(nc.app.Theme.Material, "No notifications")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}
			return material.List(nc.app.Theme.Material, &nc.list).Layout(gtx, len(nc.items), func(gtx layout.Context, i int) layout.Dimensions {
				return nc.layoutItem(gtx, i)
			})
		}),
	)
}

// LayoutMain renderuje hlavní oblast (placeholder text).
func (nc *NotificationCenter) LayoutMain(gtx layout.Context) layout.Dimensions {
	return layoutCentered(gtx, nc.app.Theme, "Select a notification from the sidebar", ColorTextDim)
}

// layoutHeader renderuje záhlaví notification centeru.
func (nc *NotificationCenter) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconNotifications, 22, ColorText)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(nc.app.Theme.Material, "Notifications")
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								})
							}),
						)
					})
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
			)
		},
	)
}

// layoutItem renderuje jednu notifikační položku.
func (nc *NotificationCenter) layoutItem(gtx layout.Context, idx int) layout.Dimensions {
	item := nc.items[idx]

	return nc.itemBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{A: 0}
		if nc.itemBtns[idx].Hovered() {
			bg = ColorHover
		}
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}.Op())

		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Ikona dle typu
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					icon, clr := nc.itemIconColor(item)
					return layoutIcon(gtx, icon, 20, clr)
				}),
				// Text
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(nc.app.Theme.Material, nc.itemTitle(item))
								lbl.Color = ColorText
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(nc.app.Theme.Material, nc.itemSubtitle(item))
								lbl.Color = ColorTextDim
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
						)
					})
				}),
				// Badge / count
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if item.Count <= 0 {
						return layout.Dimensions{}
					}
					return nc.layoutBadge(gtx, item.Count)
				}),
			)
		})
	})
}

// itemIconColor vrátí ikonu a barvu pro daný typ notifikace.
func (nc *NotificationCenter) itemIconColor(item notifItem) (*NIcon, color.NRGBA) {
	switch item.Kind {
	case notifFriendRequest:
		return IconPersonAdd, color.NRGBA{R: 80, G: 200, B: 120, A: 255}
	case notifUnreadChannel:
		return IconChat, ColorAccent
	case notifUnreadDM:
		return IconPerson, color.NRGBA{R: 100, G: 180, B: 255, A: 255}
	case notifUnreadGroup:
		return IconGroup, color.NRGBA{R: 200, G: 150, B: 255, A: 255}
	default:
		return IconNotifications, ColorTextDim
	}
}

// itemTitle vrátí hlavní text položky.
func (nc *NotificationCenter) itemTitle(item notifItem) string {
	switch item.Kind {
	case notifFriendRequest:
		if item.Username != "" {
			return item.Username
		}
		return "Friend request"
	case notifUnreadChannel:
		if item.ChannelName != "" {
			return "#" + item.ChannelName
		}
		return "Channel"
	case notifUnreadDM:
		if item.PeerName != "" {
			return item.PeerName
		}
		return "Direct message"
	case notifUnreadGroup:
		if item.GroupName != "" {
			return item.GroupName
		}
		return "Group"
	default:
		return "Notification"
	}
}

// itemSubtitle vrátí sekundární text (server name + kontext).
func (nc *NotificationCenter) itemSubtitle(item notifItem) string {
	switch item.Kind {
	case notifFriendRequest:
		return fmt.Sprintf("%s — Friend request", item.ServerName)
	case notifUnreadChannel:
		return fmt.Sprintf("%s — %d new messages", item.ServerName, item.Count)
	case notifUnreadDM:
		return fmt.Sprintf("%s — %d new messages", item.ServerName, item.Count)
	case notifUnreadGroup:
		return fmt.Sprintf("%s — New messages", item.ServerName)
	default:
		return item.ServerName
	}
}

// layoutBadge renderuje červený badge s počtem.
func (nc *NotificationCenter) layoutBadge(gtx layout.Context, count int) layout.Dimensions {
	text := fmt.Sprintf("%d", count)
	if count > 99 {
		text = "99+"
	}

	return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := bounds.Dy() / 2
				if rr < 1 {
					rr = gtx.Dp(8)
				}
				paint.FillShape(gtx.Ops, ColorDanger, clip.RRect{
					Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(nc.app.Theme.Material, text)
					lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

// handleClick zpracuje kliknutí na notifikační položku a přepne na odpovídající view.
func (nc *NotificationCenter) handleClick(idx int) {
	if idx < 0 || idx >= len(nc.items) {
		return
	}
	item := nc.items[idx]
	a := nc.app

	switch item.Kind {
	case notifFriendRequest:
		// Přepnout na Home (DM view s friend requests)
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
		}
		a.Mode = ViewDM
		a.mu.Unlock()

	case notifUnreadChannel:
		// Přepnout na server + kanál
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
			a.Mode = ViewChannels
		}
		a.mu.Unlock()
		a.SelectChannel(item.ChannelID, item.ChannelName)

	case notifUnreadDM:
		// Přepnout na DM konverzaci
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
		}
		a.mu.Unlock()
		a.SelectDM(item.ConvID)

	case notifUnreadGroup:
		// Přepnout na group
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
			conn := a.Servers[item.ServerIdx]
			conn.ActiveGroupID = item.GroupID
			a.Mode = ViewGroup
		}
		a.mu.Unlock()
	}

	a.Window.Invalidate()
}

package ui

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// notifItemKind distinguishes the notification type.
type notifItemKind int

const (
	notifAlert notifItemKind = iota // one-time alerts (accepted/rejected etc.)
	notifFriendRequest
	notifLFGApplication
	notifUnreadChannel
	notifUnreadDM
	notifUnreadGroup
)

// notifItem is a single item in the notification center.
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
	UserID      string // for friend request
	Username    string // for friend request
	ListingID   string // for LFG application
	LFGGameName string // for LFG application
	Message     string // for alerts
	AlertID     int    // stable ID for dismiss
}

// alertEntry is a one-time notification stored until dismissed.
type alertEntry struct {
	ID         int
	ServerName string
	Title      string
	Message    string
	CreatedAt  time.Time
}

// NotificationCenter — aggregated overview of unread messages and notifications.
type NotificationCenter struct {
	app        *App
	list       widget.List
	items      []notifItem
	itemBtns   []widget.Clickable
	alerts     []alertEntry
	nextAlertID int
}

// AddAlert adds a one-time alert to the notification center.
func (nc *NotificationCenter) AddAlert(serverName, title, message string) {
	nc.nextAlertID++
	nc.alerts = append(nc.alerts, alertEntry{
		ID:         nc.nextAlertID,
		ServerName: serverName,
		Title:      title,
		Message:    message,
		CreatedAt:  time.Now(),
	})
}

func NewNotificationCenter(a *App) *NotificationCenter {
	nc := &NotificationCenter{app: a}
	nc.list.Axis = layout.Vertical
	return nc
}

// totalUnread returns the total count of unread notifications from all servers.
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

		// LFG pending applications (for my listings + pending apps from WS, deduplicated)
		if a.LFGBoard != nil {
			seen := make(map[string]bool)
			for _, listing := range a.LFGBoard.listings {
				if listing.UserID == conn.UserID {
					seen[listing.ID] = true
					for _, app := range listing.Applications {
						if app.Status == "pending" {
							total++
						}
					}
				}
			}
			for listingID, apps := range a.LFGBoard.pendingApps {
				if seen[listingID] {
					continue
				}
				for _, app := range apps {
					if app.Status == "pending" {
						total++
					}
				}
			}
		}
	}
	total += len(nc.alerts)
	return total
}

// refresh rebuilds the list of notification items from all servers.
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

		// LFG applications (pending, for listings I own — from loaded listings + pendingApps)
		if a.LFGBoard != nil {
			seen := make(map[string]bool)
			for _, listing := range a.LFGBoard.listings {
				if listing.UserID != conn.UserID {
					continue
				}
				pendingCount := 0
				for _, app := range listing.Applications {
					if app.Status == "pending" {
						pendingCount++
					}
				}
				if pendingCount > 0 {
					nc.items = append(nc.items, notifItem{
						Kind:        notifLFGApplication,
						ServerIdx:   srvIdx,
						ServerName:  conn.Name,
						ListingID:   listing.ID,
						ChannelID:   listing.ChannelID,
						LFGGameName: listing.GameName,
						Count:       pendingCount,
					})
				}
				seen[listing.ID] = true
			}
			// Also check pendingApps (received while not on LFG page)
			for listingID, apps := range a.LFGBoard.pendingApps {
				if seen[listingID] {
					continue
				}
				pendingCount := 0
				for _, app := range apps {
					if app.Status == "pending" {
						pendingCount++
					}
				}
				if pendingCount > 0 {
					nc.items = append(nc.items, notifItem{
						Kind:        notifLFGApplication,
						ServerIdx:   srvIdx,
						ServerName:  conn.Name,
						ListingID:   listingID,
						LFGGameName: "LFG",
						Count:       pendingCount,
					})
				}
			}
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

	// One-time alerts
	for _, al := range nc.alerts {
		nc.items = append(nc.items, notifItem{
			Kind:       notifAlert,
			ServerName: al.ServerName,
			Message:    al.Message,
			GroupName:  al.Title, // reuse field for title
			AlertID:    al.ID,
			Count:      1,
		})
	}

	// Sort: alerts on top, then friend requests, then channels, DM, groups
	sort.SliceStable(nc.items, func(i, j int) bool {
		return nc.items[i].Kind < nc.items[j].Kind
	})

	// Ensure enough buttons
	if len(nc.itemBtns) < len(nc.items) {
		nc.itemBtns = make([]widget.Clickable, len(nc.items)+10)
	}
}

// LayoutSidebar renders the notification list in the sidebar (300px panel).
func (nc *NotificationCenter) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	nc.refresh()

	// Handle item clicks
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
		// Item list
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

// LayoutMain renders the main area (placeholder text).
func (nc *NotificationCenter) LayoutMain(gtx layout.Context) layout.Dimensions {
	return layoutCentered(gtx, nc.app.Theme, "Select a notification from the sidebar", ColorTextDim)
}

// layoutHeader renders the notification center header.
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

// layoutItem renders a single notification item.
func (nc *NotificationCenter) layoutItem(gtx layout.Context, idx int) layout.Dimensions {
	item := nc.items[idx]
	th := nc.app.Theme

	return nc.itemBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{A: 0}
		if nc.itemBtns[idx].Hovered() {
			bg = ColorHover
		}
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}.Op())

		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Start}.Layout(gtx,
				// Icon
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						icon, clr := nc.itemIconColor(item)
						return layoutIcon(gtx, icon, 22, clr)
					})
				}),
				// Content
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Type label + time
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										_, clr := nc.itemIconColor(item)
										lbl := material.Label(th.Material, th.Sp(11), nc.itemTypeLabel(item))
										lbl.Color = clr
										lbl.Font.Weight = font.Bold
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Label(th.Material, th.Sp(11), item.ServerName)
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										ts := nc.itemTime(item)
										if ts == "" {
											return layout.Dimensions{}
										}
										lbl := material.Label(th.Material, th.Sp(10), ts)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
								)
							}),
							// Title (bold)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(th.Material, th.Sp(14), nc.itemTitle(item))
									lbl.Color = ColorText
									lbl.Font.Weight = font.Bold
									return lbl.Layout(gtx)
								})
							}),
							// Description
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								desc := nc.itemDescription(item)
								if desc == "" {
									return layout.Dimensions{}
								}
								return layout.Inset{Top: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(th.Material, th.Sp(13), desc)
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								})
							}),
						)
					})
				}),
				// Badge / count
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if item.Count <= 1 {
						return layout.Dimensions{}
					}
					return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return nc.layoutBadge(gtx, item.Count)
					})
				}),
			)
		})
	})
}

// itemTypeLabel returns the type label for a notification.
func (nc *NotificationCenter) itemTypeLabel(item notifItem) string {
	switch item.Kind {
	case notifAlert:
		return "ALERT"
	case notifFriendRequest:
		return "FRIEND REQUEST"
	case notifLFGApplication:
		return "LFG APPLICATION"
	case notifUnreadChannel:
		return "CHANNEL"
	case notifUnreadDM:
		return "DIRECT MESSAGE"
	case notifUnreadGroup:
		return "GROUP"
	}
	return "NOTIFICATION"
}

// itemDescription returns a longer description for the notification.
func (nc *NotificationCenter) itemDescription(item notifItem) string {
	switch item.Kind {
	case notifAlert:
		return item.Message
	case notifFriendRequest:
		return "Wants to be your friend. Click to view."
	case notifLFGApplication:
		return fmt.Sprintf("%d pending — click to review applications", item.Count)
	case notifUnreadChannel:
		return fmt.Sprintf("%d unread messages", item.Count)
	case notifUnreadDM:
		return fmt.Sprintf("%d unread messages", item.Count)
	case notifUnreadGroup:
		return "New messages in group"
	}
	return ""
}

// itemTime returns a relative timestamp for the notification.
func (nc *NotificationCenter) itemTime(item notifItem) string {
	if item.Kind == notifAlert {
		for _, al := range nc.alerts {
			if al.ID == item.AlertID {
				return lfgTimeAgo(al.CreatedAt)
			}
		}
	}
	return ""
}

// itemIconColor returns the icon and color for the given notification type.
func (nc *NotificationCenter) itemIconColor(item notifItem) (*NIcon, color.NRGBA) {
	switch item.Kind {
	case notifAlert:
		return IconNotifications, color.NRGBA{R: 100, G: 180, B: 255, A: 255}
	case notifFriendRequest:
		return IconPersonAdd, color.NRGBA{R: 80, G: 200, B: 120, A: 255}
	case notifLFGApplication:
		return IconGroup, color.NRGBA{R: 255, G: 180, B: 50, A: 255}
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

// itemTitle returns the main text of the item.
func (nc *NotificationCenter) itemTitle(item notifItem) string {
	switch item.Kind {
	case notifAlert:
		return item.GroupName // title stored in GroupName
	case notifFriendRequest:
		if item.Username != "" {
			return item.Username
		}
		return "Friend request"
	case notifLFGApplication:
		return fmt.Sprintf("LFG: %s", item.LFGGameName)
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

// itemSubtitle returns the secondary text (server name + context).
func (nc *NotificationCenter) itemSubtitle(item notifItem) string {
	switch item.Kind {
	case notifAlert:
		return fmt.Sprintf("%s — %s", item.ServerName, item.Message)
	case notifFriendRequest:
		return fmt.Sprintf("%s — Friend request", item.ServerName)
	case notifLFGApplication:
		return fmt.Sprintf("%s — %d pending applications", item.ServerName, item.Count)
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

// layoutBadge renders a red badge with a count.
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
					lbl.Color = ColorWhite
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

// handleClick handles a click on a notification item and switches to the corresponding view.
func (nc *NotificationCenter) handleClick(idx int) {
	if idx < 0 || idx >= len(nc.items) {
		return
	}
	item := nc.items[idx]
	a := nc.app

	switch item.Kind {
	case notifAlert:
		// Dismiss alert by stable ID
		for i, al := range nc.alerts {
			if al.ID == item.AlertID {
				nc.alerts = append(nc.alerts[:i], nc.alerts[i+1:]...)
				break
			}
		}

	case notifFriendRequest:
		// Switch to Home (DM view with friend requests)
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
		}
		a.Mode = ViewDM
		a.mu.Unlock()

	case notifUnreadChannel:
		// Switch to server + channel
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
			a.Mode = ViewChannels
		}
		a.mu.Unlock()
		a.SelectChannel(item.ChannelID, item.ChannelName)

	case notifUnreadDM:
		// Switch to DM conversation
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
			a.Mode = ViewDM
		}
		a.mu.Unlock()
		a.SelectDM(item.ConvID)

	case notifUnreadGroup:
		// Switch to group
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
			conn := a.Servers[item.ServerIdx]
			conn.ActiveGroupID = item.GroupID
			a.Mode = ViewGroup
		}
		a.mu.Unlock()

	case notifLFGApplication:
		// Switch to LFG channel and expand applications
		a.mu.Lock()
		if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
			a.ActiveServer = item.ServerIdx
			a.Mode = ViewChannels
		}
		a.mu.Unlock()
		// Navigate to the LFG channel
		if item.ChannelID != "" {
			chName := ""
			if item.ServerIdx >= 0 && item.ServerIdx < len(a.Servers) {
				a.mu.RLock()
				for _, ch := range a.Servers[item.ServerIdx].Channels {
					if ch.ID == item.ChannelID {
						chName = ch.Name
						break
					}
				}
				a.mu.RUnlock()
			}
			a.SelectChannel(item.ChannelID, chName)
		}
		if a.LFGBoard != nil {
			a.LFGBoard.showAppsListing = item.ListingID
		}
	}

	a.Window.Invalidate()
}

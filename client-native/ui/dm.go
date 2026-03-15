package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/crypto"
	"nora-client/p2p"
	"nora-client/store"
)

type dmMsgAction struct {
	nameBtn   widget.Clickable
	avatarBtn widget.Clickable
	copyBtn   widget.Clickable
	replyBtn  widget.Clickable
	editBtn   widget.Clickable
	textSels  []widget.Selectable
}

type DMViewUI struct {
	app       *App
	convList  widget.List
	msgList   widget.List
	editor    widget.Editor
	convBtns  []widget.Clickable
	delBtns   []widget.Clickable
	msgDelBtns []widget.Clickable
	actions    []dmMsgAction

	// Reply
	replyToMsg     *api.DMPendingMessage // zpráva na kterou odpovídáme
	cancelReplyBtn widget.Clickable

	// Edit
	editingMsg    *api.DMPendingMessage // zpráva kterou editujeme
	cancelEditBtn widget.Clickable

	// Call
	callBtn widget.Clickable

	// Upload / P2P
	uploadBtn  widget.Clickable
	p2pBtns    []widget.Clickable            // per-message P2P download clickables
	p2pBarBtns map[string]*widget.Clickable  // progress bar clickables

	editorList widget.List

	// Typing
	lastTypingSent time.Time

	// Groups
	groupBtns      []widget.Clickable
	createGroupBtn widget.Clickable
	joinGroupBtn   widget.Clickable
	joinGroupEd    widget.Editor
	showJoinGroup  bool

	// Files / Tools
	filesBtn    widget.Clickable
	kanbanBtn   widget.Clickable
	calendarBtn widget.Clickable

	// Emoji picker
	emojiBtn         widget.Clickable
	showEmojis       bool
	emojiList        widget.List
	emojiClickBtns   []widget.Clickable
	unicodeEmojiBtns []widget.Clickable
	emojiCategoryIdx int
	emojiCatBtns     []widget.Clickable
}

func NewDMView(a *App) *DMViewUI {
	v := &DMViewUI{app: a}
	v.convList.Axis = layout.Vertical
	v.msgList.Axis = layout.Vertical
	v.msgList.ScrollToEnd = true
	v.editor.Submit = true
	v.editorList.Axis = layout.Vertical
	v.editorList.ScrollToEnd = true
	v.joinGroupEd.SingleLine = true
	v.joinGroupEd.Submit = true
	v.p2pBarBtns = make(map[string]*widget.Clickable)
	// Emoji picker
	total := 0
	for _, cat := range UnicodeEmojiCategories {
		total += len(cat.Emojis)
	}
	v.unicodeEmojiBtns = make([]widget.Clickable, total)
	v.emojiCatBtns = make([]widget.Clickable, len(UnicodeEmojiCategories)+1)
	return v
}

// peerName returns the peer's username for a DM conversation.
func (v *DMViewUI) peerID(conv api.DMConversation, myUserID string) string {
	for _, p := range conv.Participants {
		if p.UserID != myUserID {
			return p.UserID
		}
	}
	return ""
}

func (v *DMViewUI) peerName(conv api.DMConversation, userID string) string {
	conn := v.app.Conn()
	if conn == nil {
		return "Unknown"
	}

	for _, p := range conv.Participants {
		if p.UserID != userID {
			for _, u := range conn.Users {
				if u.ID == p.UserID {
					return v.app.ResolveUserName(&u)
				}
			}
			return "Unknown"
		}
	}
	return "Unknown"
}

func (v *DMViewUI) findUsername(userID string) string {
	conn := v.app.Conn()
	if conn == nil {
		return "?"
	}
	for _, u := range conn.Users {
		if u.ID == userID {
			return v.app.ResolveUserName(&u)
		}
	}
	return "?"
}

func (v *DMViewUI) layoutUnreadBadge(gtx layout.Context, count int) layout.Dimensions {
	size := gtx.Dp(18)
	paint.FillShape(gtx.Ops, ColorDanger, clip.RRect{
		Rect: image.Rect(0, 0, size, size),
		NE:   size / 2, NW: size / 2, SE: size / 2, SW: size / 2,
	}.Op(gtx.Ops))
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(size, size)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			text := fmt.Sprintf("%d", count)
			if count > 9 {
				text = "9+"
			}
			lbl := material.Caption(v.app.Theme.Material, text)
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return lbl.Layout(gtx)
		}),
	)
}

func (v *DMViewUI) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutColoredBg(gtx, ColorCard)
	}

	v.app.mu.RLock()
	allConvs := conn.DMConversations
	activeDM := conn.ActiveDMID
	userID := conn.UserID
	onlineUsers := conn.OnlineUsers
	groups := make([]api.Group, len(conn.Groups))
	copy(groups, conn.Groups)
	activeGroupID := conn.ActiveGroupID
	v.app.mu.RUnlock()

	// Cross-server blocklist: filtrovat konverzace s blokovanými uživateli
	var convs []api.DMConversation
	for _, conv := range allConvs {
		blocked := false
		for _, p := range conv.Participants {
			if p.UserID != userID && p.PublicKey != "" && v.app.IsBlockedKey(p.PublicKey) {
				blocked = true
				break
			}
		}
		if !blocked {
			convs = append(convs, conv)
		}
	}

	if len(v.convBtns) < len(convs) {
		v.convBtns = make([]widget.Clickable, len(convs)+10)
	}
	if len(v.delBtns) < len(convs) {
		v.delBtns = make([]widget.Clickable, len(convs)+10)
	}
	if len(v.groupBtns) < len(groups) {
		v.groupBtns = make([]widget.Clickable, len(groups)+10)
	}

	// Handle delete clicks — show confirm dialog
	for i, conv := range convs {
		if v.delBtns[i].Clicked(gtx) {
			convID := conv.ID
			peerName := v.peerName(conv, userID)
			v.app.ConfirmDlg.Show(
				"Delete Conversation",
				"Delete conversation with "+peerName+"? This will delete the history for both users.",
				func() { v.deleteDM(convID) },
			)
		}
	}

	// Handle group clicks
	for i, grp := range groups {
		if v.groupBtns[i].Clicked(gtx) {
			v.app.SelectGroup(grp.ID, grp.Name)
		}
	}

	// Handle files button
	if v.filesBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewShares
		v.app.mu.Unlock()
		v.app.SharesView.loadShareList()
	}
	if v.kanbanBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewKanban
		v.app.mu.Unlock()
		v.app.KanbanView.LoadBoards()
	}
	if v.calendarBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewCalendar
		v.app.mu.Unlock()
		v.app.CalendarView.LoadEvents()
	}

	// Handle create group
	if v.createGroupBtn.Clicked(gtx) {
		v.app.CreateGroupDlg.Show()
	}
	if v.joinGroupBtn.Clicked(gtx) {
		v.showJoinGroup = !v.showJoinGroup
		v.joinGroupEd.SetText("")
	}
	// Handle join group submit
	for {
		ev, ok := v.joinGroupEd.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			code := v.joinGroupEd.Text()
			if code != "" {
				v.showJoinGroup = false
				v.joinGroupEd.SetText("")
				go v.joinGroupByCode(code)
			}
		}
	}

	joinGroupExtra := 0
	if v.showJoinGroup {
		joinGroupExtra = 1
	}
	totalItems := len(convs) + 1 + joinGroupExtra + len(groups) + 1 // +1 for groups header, +1 for create btn

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(16), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(v.app.Theme.Material, "Direct Messages")
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),

		// Files button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutNavBtn(gtx, &v.filesBtn, IconFolder, "Files")
		}),
		// Kanban button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutNavBtn(gtx, &v.kanbanBtn, IconViewColumn, "Kanban")
		}),
		// Calendar button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutNavBtn(gtx, &v.calendarBtn, IconSchedule, "Calendar")
		}),

		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(v.app.Theme.Material, &v.convList).Layout(gtx, totalItems, func(gtx layout.Context, idx int) layout.Dimensions {
				// DM conversations
				if idx < len(convs) {
				conv := convs[idx]

				if v.convBtns[idx].Clicked(gtx) {
					v.app.SelectDM(conv.ID)
				}

				active := conv.ID == activeDM
				name := v.peerName(conv, userID)
				peerOnline := onlineUsers[v.peerID(conv, userID)]
				hovered := v.convBtns[idx].Hovered() || v.delBtns[idx].Hovered()

				bg := ColorCard
				if active {
					bg = ColorSelected
				} else if hovered {
					bg = ColorHover
				}

				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							// Clickable conversation area
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return v.convBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												size := gtx.Dp(32)
												clr := UserColor(name)
												rr := size / 2
												paint.FillShape(gtx.Ops, clr, clip.RRect{
													Rect: image.Rect(0, 0, size, size),
													NE:   rr, NW: rr, SE: rr, SW: rr,
												}.Op(gtx.Ops))
												return layout.Stack{Alignment: layout.Center}.Layout(gtx,
													layout.Stacked(func(gtx layout.Context) layout.Dimensions {
														return layout.Dimensions{Size: image.Pt(size, size)}
													}),
													layout.Stacked(func(gtx layout.Context) layout.Dimensions {
														initial := "?"
														if len(name) > 0 {
															initial = string([]rune(name)[0])
														}
														lbl := material.Caption(v.app.Theme.Material, initial)
														lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
														return lbl.Layout(gtx)
													}),
												)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													dotSize := gtx.Dp(8)
													clr := ColorOffline
													if peerOnline {
														clr = ColorOnline
													}
													paint.FillShape(gtx.Ops, clr, clip.Ellipse{
														Max: image.Pt(dotSize, dotSize),
													}.Op(gtx.Ops))
													return layout.Dimensions{Size: image.Pt(dotSize, dotSize)}
												})
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body2(v.app.Theme.Material, name)
													nameColor := UserColor(name)
													if !peerOnline {
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
												unread := conn.UnreadDMCount[conv.ID]
												if unread <= 0 {
													return layout.Dimensions{}
												}
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return v.layoutUnreadBadge(gtx, unread)
												})
											}),
										)
									})
								})
							}),
							// Delete button (visible on hover)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if !hovered {
									return layout.Dimensions{}
								}
								return v.delBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										delBg := color.NRGBA{}
										if v.delBtns[idx].Hovered() {
											delBg = color.NRGBA{R: 80, G: 30, B: 30, A: 255}
										}
										return layout.Background{}.Layout(gtx,
											func(gtx layout.Context) layout.Dimensions {
												bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
												if delBg.A > 0 {
													rr := gtx.Dp(4)
													paint.FillShape(gtx.Ops, delBg, clip.RRect{
														Rect: bounds,
														NE:   rr, NW: rr, SE: rr, SW: rr,
													}.Op(gtx.Ops))
												}
												return layout.Dimensions{Size: bounds.Max}
											},
											func(gtx layout.Context) layout.Dimensions {
												return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Caption(v.app.Theme.Material, "X")
													lbl.Color = ColorDanger
													return lbl.Layout(gtx)
												})
											},
										)
									})
								})
							}),
						)
					},
				)
				}

				// Groups header
				gIdx := idx - len(convs)
				if gIdx == 0 {
					return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, "GROUPS")
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.joinGroupBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, "Join")
										lbl.Color = ColorAccent
										return lbl.Layout(gtx)
									})
								})
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.createGroupBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, "+")
										lbl.Color = ColorAccent
										return lbl.Layout(gtx)
									})
								})
							}),
						)
					})
				}

				// Join group editor
				if v.showJoinGroup && gIdx == 1 {
					return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
								rr := gtx.Dp(6)
								paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
									Rect: bounds,
									NE:   rr, NW: rr, SE: rr, SW: rr,
								}.Op(gtx.Ops))
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									ed := material.Editor(v.app.Theme.Material, &v.joinGroupEd, "Invite code...")
									ed.Color = ColorText
									ed.HintColor = ColorTextDim
									return ed.Layout(gtx)
								})
							},
						)
					})
				}

				// Group items
				gi := gIdx - 1 - joinGroupExtra // offset by header + optional join editor
				if gi >= 0 && gi < len(groups) {
					grp := groups[gi]
					active := grp.ID == activeGroupID

					bg := ColorCard
					if active {
						bg = ColorSelected
					} else if v.groupBtns[gi].Hovered() {
						bg = ColorHover
					}

					return v.groupBtns[gi].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
								paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(v.app.Theme.Material, grp.Name)
											if active {
												lbl.Color = ColorText
											} else {
												lbl.Color = ColorTextDim
											}
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if !conn.UnreadGroups[grp.ID] {
												return layout.Dimensions{}
											}
											sz := gtx.Dp(8)
											paint.FillShape(gtx.Ops, ColorAccent, clip.RRect{
												Rect: image.Rect(0, 0, sz, sz),
												NE:   sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
											}.Op(gtx.Ops))
											return layout.Dimensions{Size: image.Pt(sz, sz)}
										}),
									)
								})
							},
						)
					})
				}

				return layout.Dimensions{}
			})
		}),
	)
}

func (v *DMViewUI) LayoutMessages(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutCentered(gtx, v.app.Theme, "Select a server", ColorTextDim)
	}

	v.app.mu.RLock()
	messages := conn.DMMessages
	activeDM := conn.ActiveDMID
	peerKey := conn.ActiveDMPeerKey
	secretKey := v.app.SecretKey
	userID := conn.UserID
	v.app.mu.RUnlock()

	// Ctrl+V: clipboard paste upload
	for {
		ev, ok := gtx.Event(key.Filter{Name: "V", Required: key.ModCtrl})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			go v.pasteClipboardImageDM()
		}
	}

	// Upload button click
	if v.uploadBtn.Clicked(gtx) {
		go v.pickFilesDM()
	}

	// Emoji picker toggle
	if v.emojiBtn.Clicked(gtx) {
		v.showEmojis = !v.showEmojis
		if v.showEmojis {
			v.emojiCategoryIdx = 1
		}
	}

	// Emoji clicks
	emojis := v.getEmojis()
	if len(v.emojiClickBtns) < len(emojis) {
		v.emojiClickBtns = make([]widget.Clickable, len(emojis)+10)
	}
	for i, e := range emojis {
		if v.emojiClickBtns[i].Clicked(gtx) {
			v.editor.Insert(":" + e.Name + ":")
			v.showEmojis = false
		}
	}

	// Unicode emoji clicks
	uniIdx := 0
	for _, cat := range UnicodeEmojiCategories {
		for _, emoji := range cat.Emojis {
			if uniIdx < len(v.unicodeEmojiBtns) && v.unicodeEmojiBtns[uniIdx].Clicked(gtx) {
				v.editor.Insert(emoji)
				v.showEmojis = false
			}
			uniIdx++
		}
	}

	// Emoji category tabs
	for i := range v.emojiCatBtns {
		if i < len(v.emojiCatBtns) && v.emojiCatBtns[i].Clicked(gtx) {
			v.emojiCategoryIdx = i
		}
	}

	for {
		ev, ok := v.editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			v.sendDM()
		case widget.ChangeEvent:
			v.sendDMTyping()
		}
	}

	if activeDM == "" {
		return layoutCentered(gtx, v.app.Theme, "Select a conversation", ColorTextDim)
	}

	// Get peer name and ID for header
	peerName := "Encrypted Conversation"
	dmPeerID := ""
	peerOnline := false
	v.app.mu.RLock()
	for _, conv := range conn.DMConversations {
		if conv.ID == activeDM {
			peerName = v.peerName(conv, userID)
			dmPeerID = v.peerID(conv, userID)
			peerOnline = conn.OnlineUsers[dmPeerID]
			break
		}
	}
	v.app.mu.RUnlock()

	// Handle call button
	if v.callBtn.Clicked(gtx) {
		if conn.Call != nil && dmPeerID != "" && peerOnline {
			conn.Call.StartCall(activeDM, dmPeerID)
			StartOutgoingRingLoop()
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(v.app.Theme.Material, peerName)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						// Call button — jen pokud peer online a žádný hovor neprobíhá
						callActive := conn.Call != nil && conn.Call.IsActive()
						canCall := peerOnline && !callActive && dmPeerID != ""
						btnColor := ColorAccent
						textColor := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						if !canCall {
							btnColor = ColorInput
							textColor = ColorTextDim
						}
						if v.callBtn.Hovered() && canCall {
							btnColor = ColorAccentHover
						}
						return v.callBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
									rr := gtx.Dp(6)
									paint.FillShape(gtx.Ops, btnColor, clip.RRect{
										Rect: bounds,
										NE:   rr, NW: rr, SE: rr, SW: rr,
									}.Op(gtx.Ops))
									return layout.Dimensions{Size: bounds.Max}
								},
								func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, "Call")
										lbl.Color = textColor
										return lbl.Layout(gtx)
									})
								},
							)
						})
					}),
				)
			})
		}),

		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		// P2P progress panel
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutDMP2PPanel(gtx)
		}),

		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(messages) == 0 {
				return layoutCentered(gtx, v.app.Theme, "No messages yet", ColorTextDim)
			}

			if len(v.msgDelBtns) < len(messages) {
				v.msgDelBtns = make([]widget.Clickable, len(messages)+20)
			}
			if len(v.p2pBtns) < len(messages) {
				v.p2pBtns = make([]widget.Clickable, len(messages)+20)
			}
			if len(v.actions) < len(messages) {
				v.actions = make([]dmMsgAction, len(messages)+20)
			}

			// Handle cancel reply/edit
			if v.cancelReplyBtn.Clicked(gtx) {
				v.replyToMsg = nil
			}
			if v.cancelEditBtn.Clicked(gtx) {
				v.editingMsg = nil
				v.editor.SetText("")
			}

			// Handle name/avatar clicks → UserPopup
			for i, msg := range messages {
				if v.actions[i].nameBtn.Clicked(gtx) || v.actions[i].avatarBtn.Clicked(gtx) {
					if msg.Author != nil {
						v.app.UserPopup.Show(msg.SenderID, v.app.ResolveUserName(msg.Author))
					} else {
						name := v.findUsername(msg.SenderID)
						v.app.UserPopup.Show(msg.SenderID, name)
					}
				}
				if v.actions[i].copyBtn.Clicked(gtx) {
					content := msg.DecryptedContent
					if content != "" {
						copyToClipboard(content)
					}
				}
				// Handle reply clicks
				if v.actions[i].replyBtn.Clicked(gtx) {
					msgCopy := msg
					v.replyToMsg = &msgCopy
					v.editingMsg = nil
				}
				// Handle edit clicks (only own messages)
				if v.actions[i].editBtn.Clicked(gtx) && msg.SenderID == userID {
					msgCopy := msg
					v.editingMsg = &msgCopy
					v.replyToMsg = nil
					v.editor.SetText(msg.DecryptedContent)
				}
			}

			// Handle delete clicks
			for i, msg := range messages {
				if v.msgDelBtns[i].Clicked(gtx) {
					msgID := msg.ID
					convID := msg.ConversationID
					v.app.ConfirmDlg.Show("Delete Message", "Delete this message for both users?", func() {
						v.deleteDMMessage(convID, msgID)
					})
				}
			}

			// Handle P2P download clicks
			for i, msg := range messages {
				if v.p2pBtns[i].Clicked(gtx) {
					// Dešifrovat obsah pro parsing
					content := msg.DecryptedContent
					if content == "" {
						content = msg.EncryptedContent
						if peerKey != "" && secretKey != "" {
							if dec, err := crypto.DecryptDM(secretKey, peerKey, msg.EncryptedContent); err == nil {
								content = dec
							}
						}
					}
					if info := parseP2PLink(content); info != nil && conn != nil && conn.P2P != nil {
						senderID := info.senderID
						transferID := info.transferID
						fileName := info.fileName
						go func() {
							savePath := saveFileDialog(fileName)
							if savePath == "" {
								return
							}
							conn.P2P.RequestDownload(senderID, transferID, savePath)
							v.app.Window.Invalidate()
						}()
					}
				}
			}

			return material.List(v.app.Theme.Material, &v.msgList).Layout(gtx, len(messages), func(gtx layout.Context, idx int) layout.Dimensions {
				msg := messages[idx]

				content := msg.DecryptedContent
				if content == "" {
					content = msg.EncryptedContent
					if peerKey != "" && secretKey != "" {
						decrypted, err := crypto.DecryptDM(secretKey, peerKey, msg.EncryptedContent)
						if err == nil {
							content = decrypted
						} else {
							content = "[decryption failed]"
						}
					}
				}

				// Resolve username and avatar from SenderID
				username := v.findUsername(msg.SenderID)
				avatarURL := ""
				if msg.Author != nil {
					username = v.app.ResolveUserName(msg.Author)
					avatarURL = msg.Author.AvatarURL
				}
				if avatarURL == "" {
					if u := v.app.FindUser(msg.SenderID); u != nil {
						avatarURL = u.AvatarURL
					}
				}

				// Message grouping
				grouped := false
				if idx > 0 {
					prev := messages[idx-1]
					if prev.SenderID == msg.SenderID &&
						msg.CreatedAt.Sub(prev.CreatedAt) < 5*time.Minute {
						grouped = true
					}
				}

				isMine := msg.SenderID == userID
				return v.layoutDMMessage(gtx, username, avatarURL, content, msg, messages, grouped, isMine, idx, &v.msgDelBtns[idx], &v.actions[idx])
			})
		}),

		// Typing indicator
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutDMTypingIndicator(gtx)
		}),

		// Emoji picker (above input)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showEmojis {
				return layout.Dimensions{}
			}
			return v.layoutDMEmojiPicker(gtx)
		}),

		// Reply/Edit preview bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.replyToMsg == nil && v.editingMsg == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(6)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									var label string
									if v.editingMsg != nil {
										label = "Editing message"
									} else if v.replyToMsg != nil {
										content := v.replyToMsg.DecryptedContent
										if len(content) > 80 {
											content = content[:80] + "..."
										}
										label = "Reply: " + content
									}
									lbl := material.Caption(v.app.Theme.Material, label)
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									cancelBtn := &v.cancelReplyBtn
									if v.editingMsg != nil {
										cancelBtn = &v.cancelEditBtn
									}
									return cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconClose, 16, ColorTextDim)
									})
								}),
							)
						})
					},
				)
			})
		}),

		// Input row (upload + emoji + editor)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(16), Bottom: unit.Dp(12), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.End}.Layout(gtx,
					// Upload button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutDMCircleIconBtn(gtx, v.app, &v.uploadBtn, IconUpload)
					}),
					// Editor
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
									rr := gtx.Dp(8)
									paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
										Rect: bounds,
										NE:   rr, NW: rr, SE: rr, SW: rr,
									}.Op(gtx.Ops))
									return layout.Dimensions{Size: bounds.Max}
								},
								func(gtx layout.Context) layout.Dimensions {
									maxH := gtx.Dp(200)
									if gtx.Constraints.Max.Y > maxH {
										gtx.Constraints.Max.Y = maxH
									}
									return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										ed := material.Editor(v.app.Theme.Material, &v.editor, "Encrypted message...")
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										if strings.Count(v.editor.Text(), "\n") >= 7 {
											lst := material.List(v.app.Theme.Material, &v.editorList)
											return lst.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
												return ed.Layout(gtx)
											})
										}
										return ed.Layout(gtx)
									})
								},
							)
						})
					}),
					// Emoji button (napravo od editoru)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutDMCircleIconBtn(gtx, v.app, &v.emojiBtn, IconEmoji)
						})
					}),
				)
			})
		}),
	)
}

func (v *DMViewUI) layoutDMMessage(gtx layout.Context, username, avatarURL, content string, msg api.DMPendingMessage, allMessages []api.DMPendingMessage, grouped, isMine bool, idx int, delBtn *widget.Clickable, act *dmMsgAction) layout.Dimensions {
	topPad := unit.Dp(8)
	if grouped && msg.ReplyToID == "" {
		topPad = unit.Dp(1)
	}

	serverURL := ""
	if c := v.app.Conn(); c != nil {
		serverURL = c.URL
	}

	hovered := delBtn.Hovered() || act.replyBtn.Hovered() || act.editBtn.Hovered() || act.copyBtn.Hovered()

	// P2P link detekce
	p2pInfo := parseP2PLink(content)

	msgID := msg.ID
	createdAt := msg.CreatedAt

	return layout.Inset{Top: topPad, Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx,
			// Avatar (clickable)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				size := gtx.Dp(32)
				if grouped && msg.ReplyToID == "" {
					return layout.Dimensions{Size: image.Pt(size, 0)}
				}
				return act.avatarBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutAvatar(gtx, v.app, username, avatarURL, 32)
				})
			}),

			// Content
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Reply indicator (shown before content for both grouped and non-grouped)
					replyIndicator := func(gtx layout.Context) layout.Dimensions {
						if msg.ReplyToID == "" {
							return layout.Dimensions{}
						}
						replyContent := ""
						replyAuthor := ""
						for _, m := range allMessages {
							if m.ID == msg.ReplyToID {
								replyContent = m.DecryptedContent
								if m.Author != nil {
									replyAuthor = v.app.ResolveUserName(m.Author)
								} else {
									replyAuthor = v.findUsername(m.SenderID)
								}
								break
							}
						}
						if replyContent == "" {
							replyContent = "(message not found)"
						}
						if len(replyContent) > 60 {
							replyContent = replyContent[:60] + "..."
						}
						return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconReply, 12, ColorTextDim)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									text := replyContent
									if replyAuthor != "" {
										text = replyAuthor + ": " + text
									}
									lbl := material.Caption(v.app.Theme.Material, text)
									lbl.Color = ColorTextDim
									lbl.TextSize = unit.Sp(11)
									return lbl.Layout(gtx)
								}),
							)
						})
					}

					if grouped && msg.ReplyToID == "" {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if p2pInfo != nil {
									return v.layoutDMP2PBlock(gtx, p2pInfo, idx, isMine)
								}
								emojis := v.getEmojis()
								return layoutMessageContent(gtx, v.app.Theme, content, emojis, nil, nil, nil, nil, &act.textSels, v.app, serverURL)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								ytURL := findYouTubeURLInText(content)
								if ytURL == "" {
									return layout.Dimensions{}
								}
								return layoutYouTubeEmbed(gtx, v.app, msgID, ytURL)
							}),
						)
					}

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Reply indicator
						layout.Rigid(replyIndicator),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								// Clickable name
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return act.nameBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(v.app.Theme.Material, username)
										lbl.Color = UserColor(username)
										if act.nameBtn.Hovered() {
											lbl.Color = ColorAccentHover
										}
										lbl.Font.Weight = 600
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, FormatDateTime(createdAt))
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								if p2pInfo != nil {
									return v.layoutDMP2PBlock(gtx, p2pInfo, idx, isMine)
								}
								emojis := v.getEmojis()
								return layoutMessageContent(gtx, v.app.Theme, content, emojis, nil, nil, nil, nil, &act.textSels, v.app, serverURL)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							ytURL := findYouTubeURLInText(content)
							if ytURL == "" {
								return layout.Dimensions{}
							}
							return layoutYouTubeEmbed(gtx, v.app, msgID, ytURL)
						}),
					)
				})
			}),

			// Action buttons on hover (copy + delete)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !hovered {
					return delBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(0, 0)}
					})
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					// Copy button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return act.copyBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconCopy, 14, ColorTextDim)
							})
						})
					}),
					// Reply button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return act.replyBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconReply, 14, ColorTextDim)
							})
						})
					}),
					// Edit button (only for own messages)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !isMine {
							return layout.Dimensions{}
						}
						return act.editBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconEdit, 14, ColorTextDim)
							})
						})
					}),
					// Delete button (only for own messages)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !isMine {
							return layout.Dimensions{}
						}
						return delBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconDelete, 14, ColorDanger)
							})
						})
					}),
				)
			}),
		)
	})
}

func (v *DMViewUI) getEmojis() []api.CustomEmoji {
	conn := v.app.Conn()
	if conn == nil {
		return nil
	}
	return conn.Emojis
}

func (v *DMViewUI) layoutDMEmojiPicker(gtx layout.Context) layout.Dimensions {
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
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutDMEmojiCategoryTabs(gtx, emojis)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Max.Y = maxH
							if v.emojiCategoryIdx == 0 {
								return v.layoutDMCustomEmojiList(gtx, emojis)
							}
							catIdx := v.emojiCategoryIdx - 1
							if catIdx >= len(UnicodeEmojiCategories) {
								catIdx = 0
							}
							return v.layoutDMUnicodeEmojiGrid(gtx, catIdx)
						}),
					)
				})
			},
		)
	})
}

func (v *DMViewUI) layoutDMEmojiCategoryTabs(gtx layout.Context, customEmojis []api.CustomEmoji) layout.Dimensions {
	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutDMEmojiTab(gtx, &v.emojiCatBtns[0], "Server", v.emojiCategoryIdx == 0)
	}))

	for i, cat := range UnicodeEmojiCategories {
		idx := i
		name := cat.Name
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutDMEmojiTab(gtx, &v.emojiCatBtns[idx+1], name, v.emojiCategoryIdx == idx+1)
		}))
	}

	return layout.Flex{}.Layout(gtx, items...)
}

func (v *DMViewUI) layoutDMEmojiTab(gtx layout.Context, btn *widget.Clickable, name string, active bool) layout.Dimensions {
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

func (v *DMViewUI) layoutDMCustomEmojiList(gtx layout.Context, emojis []api.CustomEmoji) layout.Dimensions {
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

func (v *DMViewUI) layoutDMUnicodeEmojiGrid(gtx layout.Context, catIdx int) layout.Dimensions {
	cat := UnicodeEmojiCategories[catIdx]

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

func (v *DMViewUI) joinGroupByCode(input string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	// Parse invite code — could be just code, or host:port/g/code link
	code := input
	if idx := strings.LastIndex(input, "/g/"); idx >= 0 {
		code = input[idx+3:]
	}

	// Find matching group invite
	for _, g := range conn.Groups {
		invites, err := conn.Client.GetGroupInvites(g.ID)
		if err != nil {
			continue
		}
		for _, inv := range invites {
			if inv.Code == code {
				group, err := conn.Client.JoinGroupByInvite(g.ID, code)
				if err != nil {
					log.Printf("JoinGroupByInvite: %v", err)
					return
				}
				v.app.mu.Lock()
				conn.Groups = append(conn.Groups, *group)
				v.app.mu.Unlock()
				v.app.Window.Invalidate()
				return
			}
		}
	}

	// If not found locally, the group might not be in our list yet.
	// Try the code directly against the server's group invites endpoint
	// This requires a server-side lookup — we'll do a best-effort approach
	log.Printf("Group invite code %q not found", code)
}

func (v *DMViewUI) deleteDM(convID string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	if err := conn.Client.DeleteDMConversation(convID); err != nil {
		log.Printf("DeleteDMConversation error: %v", err)
		return
	}

	// Remove locally
	v.app.mu.Lock()
	for i, conv := range conn.DMConversations {
		if conv.ID == convID {
			conn.DMConversations = append(conn.DMConversations[:i], conn.DMConversations[i+1:]...)
			break
		}
	}
	if conn.ActiveDMID == convID {
		conn.ActiveDMID = ""
		conn.ActiveDMPeerKey = ""
		conn.DMMessages = nil
	}
	v.app.mu.Unlock()

	// Delete local history
	if v.app.DMHistory != nil {
		v.app.DMHistory.DeleteConversation(convID)
		v.app.DMHistory.Save()
	}

	v.app.Window.Invalidate()
}

func (v *DMViewUI) deleteDMMessage(convID, msgID string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	// Find peer user ID
	var peerUserID string
	v.app.mu.RLock()
	for _, conv := range conn.DMConversations {
		if conv.ID == convID {
			for _, p := range conv.Participants {
				if p.UserID != conn.UserID {
					peerUserID = p.UserID
					break
				}
			}
			break
		}
	}
	v.app.mu.RUnlock()

	// Notify peer via WS
	if peerUserID != "" && conn.WS != nil {
		payload := map[string]string{
			"to":              peerUserID,
			"conversation_id": convID,
			"message_id":      msgID,
		}
		data, _ := json.Marshal(payload)
		conn.WS.Send(api.WSEvent{Type: "dm.message.delete", Payload: data})
	}

	// Remove locally
	v.app.mu.Lock()
	for i, m := range conn.DMMessages {
		if m.ID == msgID {
			conn.DMMessages = append(conn.DMMessages[:i], conn.DMMessages[i+1:]...)
			break
		}
	}
	v.app.mu.Unlock()

	if v.app.DMHistory != nil {
		v.app.DMHistory.DeleteMessage(msgID)
		v.app.DMHistory.Save()
	}

	v.app.Window.Invalidate()
}

func (v *DMViewUI) sendDM() {
	text := v.editor.Text()
	if text == "" {
		return
	}
	v.editor.SetText("")

	if v.editingMsg != nil {
		msg := v.editingMsg
		v.editingMsg = nil
		go v.editDMMessage(msg, text)
		return
	}

	replyToID := ""
	if v.replyToMsg != nil {
		replyToID = v.replyToMsg.ID
		v.replyToMsg = nil
	}
	go v.sendDMText(text, replyToID)
}

// sendDMText pošle libovolný text jako šifrovanou DM zprávu.
func (v *DMViewUI) sendDMText(text, replyToID string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	v.app.mu.RLock()
	convID := conn.ActiveDMID
	secretKey := v.app.SecretKey
	peerKey := conn.ActiveDMPeerKey
	v.app.mu.RUnlock()

	if convID == "" || peerKey == "" {
		return
	}

	encrypted, err := crypto.EncryptDM(secretKey, peerKey, text)
	if err != nil {
		log.Printf("DM encrypt error: %v", err)
		return
	}

	sent, err := conn.Client.SendDM(convID, encrypted, replyToID)
	if err != nil {
		log.Printf("DM send error: %v", err)
		v.app.Toasts.Error("Failed to send DM")
		return
	}

	// Save sent message to local history
	if v.app.DMHistory != nil && sent != nil {
		v.app.DMHistory.AddMessage(store.StoredDMMessage{
			ID:             sent.ID,
			ConversationID: sent.ConversationID,
			SenderID:       sent.SenderID,
			Content:        text,
			ReplyToID:      replyToID,
			CreatedAt:      sent.CreatedAt,
		})
		v.app.DMHistory.Save()
	}

	// Add to current view with decrypted content
	if sent != nil {
		sent.DecryptedContent = text
		sent.ReplyToID = replyToID
		v.app.mu.Lock()
		if conn.ActiveDMID == convID {
			conn.DMMessages = append(conn.DMMessages, *sent)
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}
}

// editDMMessage pošle edit přes WS relay a aktualizuje lokálně.
func (v *DMViewUI) editDMMessage(msg *api.DMPendingMessage, newText string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	v.app.mu.RLock()
	convID := conn.ActiveDMID
	secretKey := v.app.SecretKey
	peerKey := conn.ActiveDMPeerKey
	v.app.mu.RUnlock()

	if convID == "" || peerKey == "" {
		return
	}

	encrypted, err := crypto.EncryptDM(secretKey, peerKey, newText)
	if err != nil {
		log.Printf("DM encrypt error: %v", err)
		return
	}

	// Najít peer user ID
	var peerUserID string
	v.app.mu.RLock()
	for _, c := range conn.DMConversations {
		if c.ID == convID {
			for _, p := range c.Participants {
				if p.UserID != conn.UserID {
					peerUserID = p.UserID
					break
				}
			}
			break
		}
	}
	v.app.mu.RUnlock()

	// Poslat edit přes WS relay
	if peerUserID != "" && conn.WS != nil {
		conn.WS.SendJSON("dm.message.edit", map[string]string{
			"to":                peerUserID,
			"id":                msg.ID,
			"conversation_id":   convID,
			"encrypted_content": encrypted,
		})
	}

	// Aktualizovat lokálně
	v.app.mu.Lock()
	if conn.ActiveDMID == convID {
		for i, m := range conn.DMMessages {
			if m.ID == msg.ID {
				conn.DMMessages[i].DecryptedContent = newText
				conn.DMMessages[i].EncryptedContent = encrypted
				break
			}
		}
	}
	v.app.mu.Unlock()

	// Aktualizovat lokální historii
	if v.app.DMHistory != nil {
		v.app.DMHistory.UpdateMessage(convID, msg.ID, newText)
		v.app.DMHistory.Save()
	}

	v.app.Window.Invalidate()
}

func (v *DMViewUI) sendDMTyping() {
	now := time.Now()
	if now.Sub(v.lastTypingSent) < 3*time.Second {
		return
	}
	v.lastTypingSent = now

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	go func() {
		v.app.mu.RLock()
		convID := conn.ActiveDMID
		v.app.mu.RUnlock()
		if convID == "" {
			return
		}

		payload, _ := json.Marshal(map[string]string{
			"conversation_id": convID,
			"user_id":         conn.UserID,
		})
		conn.WS.Send(api.WSEvent{
			Type:    "typing.start",
			Payload: payload,
		})
	}()
}

func (v *DMViewUI) layoutDMTypingIndicator(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}

	v.app.mu.RLock()
	typers := conn.TypingDMUsers
	v.app.mu.RUnlock()

	if len(typers) == 0 {
		return layout.Dimensions{}
	}

	names := ""
	count := 0
	now := time.Now()
	for uid, t := range typers {
		if now.Sub(t) > 5*time.Second {
			continue
		}
		if uid == conn.UserID {
			continue
		}
		name := "?"
		for _, u := range conn.Users {
			if u.ID == uid {
				name = v.app.ResolveUserName(&u)
				break
			}
		}
		if count > 0 {
			names += ", "
		}
		names += name
		count++
	}

	if count == 0 {
		return layout.Dimensions{}
	}

	gtx.Execute(op.InvalidateCmd{At: time.Now().Add(5 * time.Second)})

	text := names + " is typing..."
	if count > 1 {
		text = names + " are typing..."
	}

	return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Caption(v.app.Theme.Material, text)
		lbl.Color = ColorTextDim
		return lbl.Layout(gtx)
	})
}

// --- P2P file sharing v DM ---

// layoutDMCircleIconBtn renderuje kulaté tlačítko s ikonou (standalone, bez MessageView).
func layoutDMCircleIconBtn(gtx layout.Context, app *App, btn *widget.Clickable, icon *NIcon) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(36)
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min
		bg := ColorInput
		if btn.Hovered() {
			bg = ColorHover
		}
		rr := size / 2
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layoutIcon(gtx, icon, 20, ColorText)
		})
	})
}

func (v *DMViewUI) layoutNavBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, label string) layout.Dimensions {
	bg := ColorCard
	if btn.Hovered() {
		bg = ColorHover
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(36))}.Op())
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, icon, 18, ColorAccent)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(v.app.Theme.Material, label)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	})
}

// pickFilesDM otevře dialog pro výběr souborů a nabídne upload/P2P.
func (v *DMViewUI) pickFilesDM() {
	paths := openMultiFileDialog()
	if len(paths) == 0 {
		return
	}

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	if conn.P2P != nil {
		v.app.ZipUploadDlg.Show(paths, v.startUploadsDM, v.startP2PSendDM)
	} else if len(paths) >= 2 {
		v.app.ZipUploadDlg.Show(paths, v.startUploadsDM)
	} else {
		v.startUploadsDM(paths)
		return
	}
	v.app.Window.Invalidate()
}

// startP2PSendDM registruje soubory v P2P manageru a pošle P2P link jako šifrovanou DM.
func (v *DMViewUI) startP2PSendDM(paths []string) {
	conn := v.app.Conn()
	if conn == nil || conn.P2P == nil {
		return
	}

	v.app.mu.RLock()
	userID := conn.UserID
	v.app.mu.RUnlock()

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		fileName := info.Name()
		fileSize := info.Size()

		isTemp := strings.HasPrefix(path, os.TempDir())
		var transferID string
		if isTemp {
			transferID = conn.P2P.RegisterTempFile(path, fileName, fileSize)
		} else {
			transferID = conn.P2P.RegisterFile(path, fileName, fileSize)
		}
		msgText := fmt.Sprintf("[P2P:%s:%s:%s:%d]", transferID, userID, fileName, fileSize)
		v.sendDMText(msgText, "")
	}
}

// startUploadsDM nahraje soubory na server a pošle URL jako šifrovanou DM.
func (v *DMViewUI) startUploadsDM(paths []string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		name := filepath.Base(path)

		go func(fname string, fileData []byte, origPath string) {
			att, err := conn.Client.UploadFile(fname, fileData)
			if err != nil {
				log.Printf("DM upload error: %v", err)
				// P2P fallback při size limitu
				errStr := err.Error()
				if conn.P2P != nil && (strings.Contains(errStr, "413") || strings.Contains(errStr, "too large") || strings.Contains(errStr, "file size")) {
					v.app.ConfirmDlg.Show("File too large", "File exceeds server limit. Share directly via P2P?", func() {
						v.startP2PSendDM([]string{origPath})
					})
					v.app.Window.Invalidate()
				}
				return
			}
			// Poslat URL souboru jako DM
			url := att.URL
			if !strings.HasPrefix(url, "http") {
				url = conn.URL + url
			}
			v.sendDMText(url, "")
		}(name, data, path)
	}
}

// layoutDMP2PBlock renderuje P2P odkaz ve zprávě.
func (v *DMViewUI) layoutDMP2PBlock(gtx layout.Context, info *p2pLinkInfo, idx int, isOwn bool) layout.Dimensions {
	conn := v.app.Conn()

	icon := IconArrowDown
	iconColor := ColorAccent
	suffix := " — click to save"
	textColor := ColorAccent
	clickable := true

	senderOffline := conn != nil && !isOwn && !conn.OnlineUsers[info.senderID]

	if conn != nil && conn.P2P != nil {
		if conn.P2P.IsUnavailable(info.transferID) {
			icon = IconCancel
			iconColor = ColorDanger
			suffix = " — unavailable"
			textColor = ColorTextDim
			clickable = false
		} else if v.dmP2PTransferActive(conn, info.transferID) {
			suffix = " — downloading..."
			clickable = false
		} else if conn.P2P.IsDownloaded(info.transferID) {
			iconColor = ColorSuccess
			suffix = ""
			textColor = ColorSuccess
		} else if isOwn {
			if conn.P2P.IsRegistered(info.transferID) {
				icon = IconUpload
				suffix = " — sharing"
			} else {
				icon = IconCancel
				iconColor = ColorDanger
				suffix = " — expired"
				textColor = ColorTextDim
			}
			clickable = false
		} else if senderOffline {
			icon = IconCancel
			iconColor = ColorDanger
			suffix = " — unavailable"
			textColor = ColorTextDim
			clickable = false
		}
	} else if isOwn {
		icon = IconCancel
		iconColor = ColorDanger
		suffix = " — expired"
		textColor = ColorTextDim
		clickable = false
	} else if senderOffline {
		icon = IconCancel
		iconColor = ColorDanger
		suffix = " — unavailable"
		textColor = ColorTextDim
		clickable = false
	}

	label := info.fileName + " (" + FormatBytes(info.fileSize) + ") · P2P" + suffix
	btn := &v.p2pBtns[idx]

	renderBlock := func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if clickable && btn.Hovered() {
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
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, icon, 16, iconColor)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, label)
								lbl.Color = textColor
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	}

	if clickable {
		return btn.Layout(gtx, renderBlock)
	}
	return renderBlock(gtx)
}

// dmP2PTransferActive vrátí true pokud je transfer v aktivním stavu.
func (v *DMViewUI) dmP2PTransferActive(conn *ServerConnection, transferID string) bool {
	for _, t := range conn.P2P.GetActiveTransfers() {
		if t.ID == transferID && (t.Status == p2p.StatusWaiting || t.Status == p2p.StatusConnecting || t.Status == p2p.StatusTransferring) {
			return true
		}
	}
	return false
}

// layoutDMP2PPanel zobrazí progress bary pro P2P příjem v DM.
func (v *DMViewUI) layoutDMP2PPanel(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil || conn.P2P == nil {
		return layout.Dimensions{}
	}

	transfers := conn.P2P.GetActiveTransfers()
	var visible []*p2p.Transfer
	for _, t := range transfers {
		if t.Direction != p2p.DirReceive {
			continue
		}
		switch t.Status {
		case p2p.StatusWaiting, p2p.StatusConnecting, p2p.StatusTransferring, p2p.StatusError:
			visible = append(visible, t)
		}
	}
	if len(visible) == 0 {
		return layout.Dimensions{}
	}

	sort.Slice(visible, func(i, j int) bool {
		return visible[i].ID < visible[j].ID
	})

	// Zpracovat kliky na bary
	for _, t := range visible {
		btn, ok := v.p2pBarBtns[t.ID]
		if !ok {
			continue
		}
		if btn.Clicked(gtx) {
			tid := t.ID
			peerID := t.PeerID
			savePath := t.SavePath()
			if t.Status == p2p.StatusError {
				errMsg := t.Error
				if strings.Contains(errMsg, "rejected") || strings.Contains(errMsg, "unavailable") {
					v.app.ConfirmDlg.ShowWithText("P2P Transfer", "File no longer available. The sender may have disconnected.", "OK", func() {
						conn.P2P.DismissTransfer(tid)
						v.app.Window.Invalidate()
					})
				} else {
					v.app.ConfirmDlg.ShowWithCancel("P2P Transfer", "Error: "+errMsg, "Retry", func() {
						conn.P2P.DismissTransfer(tid)
						conn.P2P.RequestDownload(peerID, tid, savePath)
						v.app.Window.Invalidate()
					}, func() {
						conn.P2P.DismissTransfer(tid)
						v.app.Window.Invalidate()
					})
				}
			} else {
				v.app.ConfirmDlg.ShowWithText("P2P Transfer", "Cancel download of "+t.FileName+"?", "Cancel", func() {
					conn.P2P.CancelTransfer(tid)
					v.app.Window.Invalidate()
				})
			}
		}
	}

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		items := make([]layout.FlexChild, 0, len(visible))
		for _, t := range visible {
			t := t
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutDMP2PBar(gtx, t)
			}))
		}
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceStart}.Layout(gtx, items...)
	})
}

// layoutDMP2PBar renderuje jednotlivý P2P progress bar.
func (v *DMViewUI) layoutDMP2PBar(gtx layout.Context, t *p2p.Transfer) layout.Dimensions {
	btn, ok := v.p2pBarBtns[t.ID]
	if !ok {
		btn = new(widget.Clickable)
		v.p2pBarBtns[t.ID] = btn
	}

	var statusText string
	barColor := ColorAccent

	switch t.Status {
	case p2p.StatusWaiting, p2p.StatusConnecting:
		statusText = t.FileName + " — connecting..."
	case p2p.StatusTransferring:
		if t.FileSize > 0 {
			pct := t.Progress * 100 / t.FileSize
			statusText = fmt.Sprintf("%s — %d%% (%s / %s)", t.FileName, pct, FormatBytes(t.Progress), FormatBytes(t.FileSize))
			if !t.StartTime.IsZero() {
				elapsed := time.Since(t.StartTime).Seconds()
				bytesThisSession := t.Progress - t.Offset
				if elapsed > 0.5 && bytesThisSession > 0 {
					speed := float64(bytesThisSession) / elapsed
					statusText += fmt.Sprintf(" · %s/s", FormatBytes(int64(speed)))
					remaining := float64(t.FileSize-t.Progress) / speed
					if remaining > 0 && remaining < 3600 {
						if remaining < 60 {
							statusText += fmt.Sprintf(" · ~%ds", int(remaining))
						} else {
							statusText += fmt.Sprintf(" · ~%dm%ds", int(remaining)/60, int(remaining)%60)
						}
					}
				}
			}
		} else {
			statusText = fmt.Sprintf("%s — %s", t.FileName, FormatBytes(t.Progress))
		}
	case p2p.StatusError:
		statusText = t.FileName + " — " + t.Error
		barColor = ColorDanger
	}

	renderBar := func(gtx layout.Context) layout.Dimensions {
		bg := ColorSidebar
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
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
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconArrowDown, 14, barColor)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, statusText)
											lbl.Color = barColor
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						},
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if t.Status != p2p.StatusTransferring || t.FileSize <= 0 {
						return layout.Dimensions{}
					}
					maxW := gtx.Constraints.Max.X
					h := gtx.Dp(3)
					paint.FillShape(gtx.Ops, ColorBg, clip.Rect(image.Rect(0, 0, maxW, h)).Op())
					pct := float32(t.Progress) / float32(t.FileSize)
					barW := int(pct * float32(maxW))
					if barW > 0 {
						paint.FillShape(gtx.Ops, barColor, clip.Rect(image.Rect(0, 0, barW, h)).Op())
					}
					return layout.Dimensions{Size: image.Pt(maxW, h)}
				}),
			)
		})
	}

	return btn.Layout(gtx, renderBar)
}

// pasteClipboardImageDM zkusí přečíst obrázek z clipboard a uploadnout ho jako DM.
func (v *DMViewUI) pasteClipboardImageDM() {
	data := readClipboardImage()
	if data == nil {
		return
	}

	tmpFile, err := os.CreateTemp("", "nora-paste-*.png")
	if err != nil {
		return
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return
	}
	tmpFile.Close()

	v.startUploadsDM([]string{tmpPath})

	go func() {
		time.Sleep(30 * time.Second)
		os.Remove(tmpPath)
	}()
}

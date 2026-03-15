package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/crypto"
	"nora-client/store"
)

// groupMsgAction drží klikací tlačítka pro attachmenty jedné group zprávy.
type groupMsgAction struct {
	attBtns [4]widget.Clickable
}

// groupPendingUpload sleduje stav nahrávaného souboru v group chatu.
type groupPendingUpload struct {
	filename string
	size     int64
	sent     int64
	status   int // 0=uploading, 1=done, 2=error
	err      string
	result   *api.UploadResult
}

type GroupViewUI struct {
	app    *App
	list   widget.List
	editor widget.Editor

	editorList widget.List

	inviteBtn     widget.Clickable
	inviteCopyBtn widget.Clickable
	leaveBtn      widget.Clickable
	deleteBtn     widget.Clickable
	membersBtn    widget.Clickable
	showMembers   bool
	inviteLink    string

	// Invite list
	showInvites    bool
	inviteListBtn  widget.Clickable
	invites        []api.GroupInvite
	inviteCopyBtns []widget.Clickable

	// Emoji picker
	emojiBtn         widget.Clickable
	showEmojis       bool
	emojiList        widget.List
	emojiClickBtns   []widget.Clickable
	unicodeEmojiBtns []widget.Clickable
	emojiCategoryIdx int
	emojiCatBtns     []widget.Clickable

	// Upload state
	uploadBtn      widget.Clickable
	pendingUploads []*groupPendingUpload
	uploadMu       sync.Mutex

	// Attachment actions per message
	actions []groupMsgAction
}

func NewGroupView(a *App) *GroupViewUI {
	v := &GroupViewUI{app: a}
	v.list.Axis = layout.Vertical
	v.list.ScrollToEnd = true
	v.editor.Submit = true
	v.editorList.Axis = layout.Vertical
	v.editorList.ScrollToEnd = true
	total := 0
	for _, cat := range UnicodeEmojiCategories {
		total += len(cat.Emojis)
	}
	v.unicodeEmojiBtns = make([]widget.Clickable, total)
	v.emojiCatBtns = make([]widget.Clickable, len(UnicodeEmojiCategories)+1)
	return v
}

func (v *GroupViewUI) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutCentered(gtx, v.app.Theme, "Select a server", ColorTextDim)
	}

	v.app.mu.RLock()
	groupID := conn.ActiveGroupID
	messages := conn.GroupMessages
	v.app.mu.RUnlock()

	if groupID == "" {
		return layoutCentered(gtx, v.app.Theme, "Select a group", ColorTextDim)
	}

	// Get group info
	groupName := "Group"
	isCreator := false
	var members []api.GroupMember
	v.app.mu.RLock()
	for _, g := range conn.Groups {
		if g.ID == groupID {
			groupName = g.Name
			isCreator = g.CreatorID == conn.UserID
			members = make([]api.GroupMember, len(g.Members))
			copy(members, g.Members)
			break
		}
	}
	onlineUsers := conn.OnlineUsers
	v.app.mu.RUnlock()

	// Handle action buttons
	if v.inviteBtn.Clicked(gtx) {
		go func() {
			inv, err := conn.Client.CreateGroupInvite(groupID, 0, 86400)
			if err != nil {
				log.Printf("CreateGroupInvite: %v", err)
				return
			}
			v.inviteLink = inv.Link
			v.app.Window.Invalidate()
		}()
	}
	if v.membersBtn.Clicked(gtx) {
		v.showMembers = !v.showMembers
	}
	if v.inviteListBtn.Clicked(gtx) {
		v.showInvites = !v.showInvites
		if v.showInvites {
			gid := groupID
			go func() {
				invs, err := conn.Client.GetGroupInvites(gid)
				if err != nil {
					log.Printf("GetGroupInvites: %v", err)
					return
				}
				v.invites = invs
				if len(v.inviteCopyBtns) < len(invs) {
					v.inviteCopyBtns = make([]widget.Clickable, len(invs)+5)
				}
				v.app.Window.Invalidate()
			}()
		}
	}
	// Handle invite copy clicks
	for i, inv := range v.invites {
		if i < len(v.inviteCopyBtns) && v.inviteCopyBtns[i].Clicked(gtx) {
			copyToClipboard(inv.Link)
		}
	}
	if v.inviteCopyBtn.Clicked(gtx) && v.inviteLink != "" {
		copyToClipboard(v.inviteLink)
	}
	if v.leaveBtn.Clicked(gtx) {
		gid := groupID
		v.app.ConfirmDlg.Show("Leave Group", "Leave this group? You will lose message history.", func() {
			go func() {
				if err := conn.Client.LeaveGroup(gid, conn.UserID); err != nil {
					log.Printf("LeaveGroup: %v", err)
					return
				}
				v.app.mu.Lock()
				for i, g := range conn.Groups {
					if g.ID == gid {
						conn.Groups = append(conn.Groups[:i], conn.Groups[i+1:]...)
						break
					}
				}
				conn.ActiveGroupID = ""
				conn.GroupMessages = nil
				v.app.Mode = ViewDM
				v.app.mu.Unlock()
				if v.app.GroupHistory != nil {
					v.app.GroupHistory.DeleteGroup(gid)
					v.app.GroupHistory.Save()
				}
				v.app.Window.Invalidate()
			}()
		})
	}
	if v.deleteBtn.Clicked(gtx) && isCreator {
		gid := groupID
		v.app.ConfirmDlg.Show("Delete Group", "Delete this group permanently?", func() {
			go func() {
				if err := conn.Client.DeleteGroup(gid); err != nil {
					log.Printf("DeleteGroup: %v", err)
					return
				}
				v.app.mu.Lock()
				for i, g := range conn.Groups {
					if g.ID == gid {
						conn.Groups = append(conn.Groups[:i], conn.Groups[i+1:]...)
						break
					}
				}
				conn.ActiveGroupID = ""
				conn.GroupMessages = nil
				v.app.Mode = ViewDM
				v.app.mu.Unlock()
				if v.app.GroupHistory != nil {
					v.app.GroupHistory.DeleteGroup(gid)
					v.app.GroupHistory.Save()
				}
				v.app.Window.Invalidate()
			}()
		})
	}

	// Emoji picker toggle
	if v.emojiBtn.Clicked(gtx) {
		v.showEmojis = !v.showEmojis
		if v.showEmojis {
			v.emojiCategoryIdx = 1
		}
	}

	// Emoji clicks
	customEmojis := v.getEmojis()
	if len(v.emojiClickBtns) < len(customEmojis) {
		v.emojiClickBtns = make([]widget.Clickable, len(customEmojis)+10)
	}
	for i, e := range customEmojis {
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

	// Handle upload button click
	if v.uploadBtn.Clicked(gtx) {
		go v.pickGroupFiles()
	}

	// Handle attachment clicks (save/open)
	for i, msg := range messages {
		if i >= len(v.actions) {
			break
		}
		for j, att := range msg.Attachments {
			if j >= len(v.actions[i].attBtns) {
				break
			}
			if v.actions[i].attBtns[j].Clicked(gtx) {
				if conn != nil {
					fileURL := conn.URL + att.URL
					fname := att.Filename
					tok := conn.Client.GetAccessToken()
					isImage := strings.HasPrefix(att.MimeType, "image/")
					if isImage {
						go openURL(fileURL)
					} else {
						v.app.SaveDlg.Show(fname, func(savePath string) {
							go v.downloadGroupFile(fileURL, savePath, fname, tok)
						})
					}
				}
			}
		}
	}

	// Handle editor events
	for {
		ev, ok := v.editor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.sendGroupMessage()
		}
	}

	// Zajistit dostatečný počet action widgetů
	if len(v.actions) < len(messages) {
		old := v.actions
		v.actions = make([]groupMsgAction, len(messages)+20)
		copy(v.actions, old)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(v.app.Theme.Material, groupName)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := fmt.Sprintf("Members (%d)", len(members))
						return v.layoutHeaderBtn(gtx, &v.membersBtn, label)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutHeaderBtn(gtx, &v.inviteBtn, "Invite")
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							label := "Links"
							if v.showInvites {
								label = "Hide Links"
							}
							return v.layoutHeaderBtn(gtx, &v.inviteListBtn, label)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutHeaderIconBtn(gtx, &v.leaveBtn, IconExit)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !isCreator {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutHeaderBtn(gtx, &v.deleteBtn, "Delete")
						})
					}),
				)
			})
		}),

		// Invite link bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.inviteLink == "" {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, "Invite: "+v.inviteLink)
									lbl.Color = ColorAccent
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return v.layoutHeaderIconBtn(gtx, &v.inviteCopyBtn, IconCopy)
								}),
							)
						})
					},
				)
			})
		}),

		// Invite list panel (collapsible)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showInvites || len(v.invites) == 0 {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var items []layout.FlexChild
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, "INVITE LINKS")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}))
				for i, inv := range v.invites {
					idx := i
					invite := inv
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									text := invite.Link
									if invite.MaxUses > 0 {
										text += fmt.Sprintf(" (%d/%d uses)", invite.Uses, invite.MaxUses)
									}
									lbl := material.Caption(v.app.Theme.Material, text)
									lbl.Color = ColorAccent
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if idx >= len(v.inviteCopyBtns) {
										return layout.Dimensions{}
									}
									return v.layoutHeaderIconBtn(gtx, &v.inviteCopyBtns[idx], IconCopy)
								}),
							)
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			})
		}),

		// Members panel (collapsible)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showMembers || len(members) == 0 {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var items []layout.FlexChild
				for _, m := range members {
					member := m
					online := onlineUsers[member.UserID]
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									dotSize := gtx.Dp(8)
									clr := ColorOffline
									if online {
										clr = ColorOnline
									}
									paint.FillShape(gtx.Ops, clr, clip.Ellipse{
										Max: image.Pt(dotSize, dotSize),
									}.Op(gtx.Ops))
									return layout.Dimensions{Size: image.Pt(dotSize, dotSize)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										name := member.Username
										lbl := material.Caption(v.app.Theme.Material, name)
										nameColor := UserColor(name)
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
							)
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			})
		}),

		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		// Messages
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(messages) == 0 {
				return layoutCentered(gtx, v.app.Theme, "No messages yet — E2E encrypted", ColorTextDim)
			}

			return material.List(v.app.Theme.Material, &v.list).Layout(gtx, len(messages), func(gtx layout.Context, idx int) layout.Dimensions {
				msg := messages[idx]

				content := msg.DecryptedContent
				if content == "" {
					content = "[encrypted]"
				}

				username := v.findUsername(msg.SenderID)
				if msg.Author != nil {
					username = v.app.ResolveUserName(msg.Author)
				}

				grouped := false
				if idx > 0 {
					prev := messages[idx-1]
					if prev.SenderID == msg.SenderID &&
						msg.CreatedAt.Sub(prev.CreatedAt) < 5*time.Minute {
						grouped = true
					}
				}

				return v.layoutGroupMessage(gtx, idx, msg, username, content, msg.CreatedAt, grouped)
			})
		}),

		// Emoji picker (above input)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showEmojis {
				return layout.Dimensions{}
			}
			return v.layoutGroupEmojiPicker(gtx)
		}),

		// Input row (upload + emoji + editor)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(16), Bottom: unit.Dp(12), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.End}.Layout(gtx,
					// Upload button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutDMCircleIconBtn(gtx, v.app, &v.uploadBtn, IconUpload)
					}),
					// Emoji button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutDMCircleIconBtn(gtx, v.app, &v.emojiBtn, IconEmoji)
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
										ed := material.Editor(v.app.Theme.Material, &v.editor, "Encrypted group message...")
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
				)
			})
		}),
	)
}

func (v *GroupViewUI) layoutGroupMessage(gtx layout.Context, msgIdx int, msg api.GroupMessage, username, content string, createdAt time.Time, grouped bool) layout.Dimensions {
	topPad := unit.Dp(8)
	if grouped {
		topPad = unit.Dp(1)
	}

	serverURL := ""
	if c := v.app.Conn(); c != nil {
		serverURL = c.URL
	}

	return layout.Inset{Top: topPad, Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				size := gtx.Dp(32)
				if grouped {
					return layout.Dimensions{Size: image.Pt(size, 0)}
				}

				clr := UserColor(username)
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
						if len(username) > 0 {
							initial = string([]rune(username)[0])
						}
						lbl := material.Caption(v.app.Theme.Material, initial)
						lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						return lbl.Layout(gtx)
					}),
				)
			}),

			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if grouped {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								emojis := v.getEmojis()
								return layoutMessageContent(gtx, v.app.Theme, content, emojis, nil, nil, nil, nil, nil, v.app, serverURL)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.layoutGroupAttachments(gtx, msgIdx, msg, serverURL)
							}),
						)
					}

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, username)
									lbl.Color = UserColor(username)
									lbl.Font.Weight = 600
									return lbl.Layout(gtx)
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
								emojis := v.getEmojis()
								return layoutMessageContent(gtx, v.app.Theme, content, emojis, nil, nil, nil, nil, nil, v.app, serverURL)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutGroupAttachments(gtx, msgIdx, msg, serverURL)
						}),
					)
				})
			}),
		)
	})
}

func (v *GroupViewUI) layoutHeaderIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
			bg = color.NRGBA{R: bg.R + 15, G: bg.G + 15, B: bg.B + 15, A: 255}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, icon, 16, ColorTextDim)
				})
			},
		)
	})
}

func (v *GroupViewUI) layoutHeaderBtn(gtx layout.Context, btn *widget.Clickable, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
			bg = color.NRGBA{R: bg.R + 15, G: bg.G + 15, B: bg.B + 15, A: 255}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, text)
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *GroupViewUI) getEmojis() []api.CustomEmoji {
	conn := v.app.Conn()
	if conn == nil {
		return nil
	}
	return conn.Emojis
}

func (v *GroupViewUI) findUsername(userID string) string {
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

func (v *GroupViewUI) sendGroupMessage() {
	text := v.editor.Text()
	if text == "" {
		return
	}
	v.editor.SetText("")

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	go func() {
		v.app.mu.RLock()
		groupID := conn.ActiveGroupID
		v.app.mu.RUnlock()

		if groupID == "" {
			return
		}

		// Get group key
		groupKey := ""
		if v.app.GroupHistory != nil {
			groupKey = v.app.GroupHistory.GetKey(groupID)
		}
		if groupKey == "" {
			log.Printf("No group key for %s — generating", groupID)
			var err error
			groupKey, err = crypto.GenerateGroupKey()
			if err != nil {
				log.Printf("GenerateGroupKey: %v", err)
				return
			}
			if v.app.GroupHistory != nil {
				v.app.GroupHistory.SetKey(groupID, groupKey)
				v.app.GroupHistory.Save()
			}
			// Distribute key to members
			v.distributeGroupKey(groupID, groupKey)
		}

		encrypted, err := crypto.EncryptGroupMessage(groupKey, text)
		if err != nil {
			log.Printf("EncryptGroupMessage: %v", err)
			return
		}

		if err := conn.Client.RelayGroupMessage(groupID, encrypted); err != nil {
			log.Printf("RelayGroupMessage: %v", err)
			return
		}

		// Save to local history
		now := time.Now()
		msgID := now.Format("20060102150405.999999")
		if v.app.GroupHistory != nil {
			v.app.GroupHistory.AddMessage(store.StoredGroupMessage{
				ID:        msgID,
				GroupID:   groupID,
				SenderID:  conn.UserID,
				Content:   text,
				CreatedAt: now,
			})

			// Counter-based key rotation (po N odeslaných zprávách)
			v.app.GroupHistory.IncrementCount(groupID)
			if v.app.GroupHistory.NeedsRotation(groupID) {
				log.Printf("Group key rotation triggered for %s (threshold reached)", groupID)
				go v.app.rotateGroupKey(conn, groupID)
			}

			v.app.GroupHistory.Save()
		}

		// Add to view
		v.app.mu.Lock()
		if conn.ActiveGroupID == groupID {
			conn.GroupMessages = append(conn.GroupMessages, api.GroupMessage{
				ID:               msgID,
				GroupID:          groupID,
				SenderID:         conn.UserID,
				DecryptedContent: text,
				CreatedAt:        now,
			})
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *GroupViewUI) distributeGroupKey(groupID, groupKey string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	// Get group members
	group, err := conn.Client.GetGroup(groupID)
	if err != nil {
		log.Printf("GetGroup for key distribution: %v", err)
		return
	}

	secretKey := v.app.SecretKey

	for _, member := range group.Members {
		if member.UserID == conn.UserID {
			continue
		}

		encKey, err := crypto.EncryptGroupKeyForMember(secretKey, member.PublicKey, groupKey)
		if err != nil {
			log.Printf("EncryptGroupKeyForMember %s: %v", member.UserID, err)
			continue
		}

		payload, _ := json.Marshal(map[string]string{
			"group_id":      groupID,
			"encrypted_key": encKey,
			"to":            member.UserID,
		})
		conn.WS.Send(api.WSEvent{
			Type:    "group.key",
			Payload: payload,
		})
	}
}

func (v *GroupViewUI) layoutGroupEmojiPicker(gtx layout.Context) layout.Dimensions {
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
							return v.layoutGroupEmojiCategoryTabs(gtx, emojis)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Max.Y = maxH
							if v.emojiCategoryIdx == 0 {
								return v.layoutGroupCustomEmojiList(gtx, emojis)
							}
							catIdx := v.emojiCategoryIdx - 1
							if catIdx >= len(UnicodeEmojiCategories) {
								catIdx = 0
							}
							return v.layoutGroupUnicodeEmojiGrid(gtx, catIdx)
						}),
					)
				})
			},
		)
	})
}

func (v *GroupViewUI) layoutGroupEmojiCategoryTabs(gtx layout.Context, customEmojis []api.CustomEmoji) layout.Dimensions {
	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutGroupEmojiTab(gtx, &v.emojiCatBtns[0], "Server", v.emojiCategoryIdx == 0)
	}))

	for i, cat := range UnicodeEmojiCategories {
		idx := i
		name := cat.Name
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutGroupEmojiTab(gtx, &v.emojiCatBtns[idx+1], name, v.emojiCategoryIdx == idx+1)
		}))
	}

	return layout.Flex{}.Layout(gtx, items...)
}

func (v *GroupViewUI) layoutGroupEmojiTab(gtx layout.Context, btn *widget.Clickable, name string, active bool) layout.Dimensions {
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

func (v *GroupViewUI) layoutGroupCustomEmojiList(gtx layout.Context, emojis []api.CustomEmoji) layout.Dimensions {
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

func (v *GroupViewUI) layoutGroupUnicodeEmojiGrid(gtx layout.Context, catIdx int) layout.Dimensions {
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

// pickGroupFiles otevře file dialog a nahraje vybrané soubory přes chunked upload.
func (v *GroupViewUI) pickGroupFiles() {
	paths := openMultiFileDialog()
	if len(paths) == 0 {
		return
	}

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("group: read file: %v", err)
			continue
		}
		name := filepath.Base(path)
		pu := &groupPendingUpload{
			filename: name,
			size:     int64(len(data)),
			status:   0,
		}

		v.uploadMu.Lock()
		v.pendingUploads = append(v.pendingUploads, pu)
		v.uploadMu.Unlock()
		v.app.Window.Invalidate()

		go func(pending *groupPendingUpload, fname string, fileData []byte) {
			result, err := conn.Client.UploadFileChunked(fname, fileData, func(sent, total int64) {
				pending.sent = sent
				v.app.Window.Invalidate()
			})

			v.uploadMu.Lock()
			if err != nil {
				pending.status = 2
				pending.err = err.Error()
				log.Printf("group: upload error: %v", err)
			} else {
				pending.status = 1
				pending.result = result
				pending.sent = pending.size
			}

			// Pokud jsou všechny hotové, odeslat zprávu
			allDone := true
			for _, u := range v.pendingUploads {
				if u.status == 0 {
					allDone = false
					break
				}
			}
			v.uploadMu.Unlock()

			if allDone {
				v.sendGroupMessageWithPendingUploads()
			}

			v.app.Window.Invalidate()
		}(pu, name, data)
	}
}

// sendGroupMessageWithPendingUploads odešle group zprávu se všemi nahranými soubory.
func (v *GroupViewUI) sendGroupMessageWithPendingUploads() {
	v.uploadMu.Lock()
	var attachments []api.GroupAttachmentPayload
	for _, u := range v.pendingUploads {
		if u.status == 1 && u.result != nil {
			attachments = append(attachments, api.GroupAttachmentPayload{
				Filename: u.result.Original,
				URL:      u.result.URL,
				Size:     u.result.Size,
				MimeType: u.result.MimeType,
			})
		}
	}
	v.pendingUploads = nil
	v.uploadMu.Unlock()

	if len(attachments) == 0 {
		return
	}

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	v.app.mu.RLock()
	groupID := conn.ActiveGroupID
	v.app.mu.RUnlock()

	if groupID == "" {
		return
	}

	// Získat group klíč
	groupKey := ""
	if v.app.GroupHistory != nil {
		groupKey = v.app.GroupHistory.GetKey(groupID)
	}
	if groupKey == "" {
		var err error
		groupKey, err = crypto.GenerateGroupKey()
		if err != nil {
			log.Printf("GenerateGroupKey: %v", err)
			return
		}
		if v.app.GroupHistory != nil {
			v.app.GroupHistory.SetKey(groupID, groupKey)
			v.app.GroupHistory.Save()
		}
		v.distributeGroupKey(groupID, groupKey)
	}

	// Popis příloh jako text zprávy
	var names []string
	for _, a := range attachments {
		names = append(names, a.Filename)
	}
	text := strings.Join(names, ", ")

	encrypted, err := crypto.EncryptGroupMessage(groupKey, text)
	if err != nil {
		log.Printf("EncryptGroupMessage: %v", err)
		return
	}

	if err := conn.Client.RelayGroupMessageWithAttachments(groupID, encrypted, attachments); err != nil {
		log.Printf("RelayGroupMessage: %v", err)
		return
	}

	// Uložit do lokální historie
	now := time.Now()
	msgID := now.Format("20060102150405.999999")

	var storedAtts []store.StoredGroupAttachment
	for _, a := range attachments {
		storedAtts = append(storedAtts, store.StoredGroupAttachment{
			Filename: a.Filename,
			URL:      a.URL,
			Size:     a.Size,
			MimeType: a.MimeType,
		})
	}

	if v.app.GroupHistory != nil {
		v.app.GroupHistory.AddMessage(store.StoredGroupMessage{
			ID:          msgID,
			GroupID:     groupID,
			SenderID:    conn.UserID,
			Content:     text,
			Attachments: storedAtts,
			CreatedAt:   now,
		})
		v.app.GroupHistory.IncrementCount(groupID)
		if v.app.GroupHistory.NeedsRotation(groupID) {
			log.Printf("Group key rotation triggered for %s (threshold reached)", groupID)
			go v.app.rotateGroupKey(conn, groupID)
		}
		v.app.GroupHistory.Save()
	}

	// Přidat do view
	var apiAtts []api.Attachment
	for _, a := range attachments {
		apiAtts = append(apiAtts, api.Attachment{
			Filename: a.Filename,
			URL:      a.URL,
			Size:     a.Size,
			MimeType: a.MimeType,
		})
	}

	v.app.mu.Lock()
	if conn.ActiveGroupID == groupID {
		conn.GroupMessages = append(conn.GroupMessages, api.GroupMessage{
			ID:               msgID,
			GroupID:          groupID,
			SenderID:         conn.UserID,
			DecryptedContent: text,
			Attachments:      apiAtts,
			CreatedAt:        now,
		})
	}
	v.app.mu.Unlock()
	v.app.Window.Invalidate()
}

// layoutGroupAttachments vykreslí přílohy group zprávy (inline obrázky, klikací soubory).
func (v *GroupViewUI) layoutGroupAttachments(gtx layout.Context, msgIdx int, msg api.GroupMessage, serverURL string) layout.Dimensions {
	if len(msg.Attachments) == 0 {
		return layout.Dimensions{}
	}

	var items []layout.FlexChild
	for j, att := range msg.Attachments {
		if j >= len(v.actions[msgIdx].attBtns) {
			break
		}
		a := att
		attIdx := j
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				// Inline image preview
				if strings.HasPrefix(a.MimeType, "image/") && serverURL != "" {
					return v.layoutGroupImagePreview(gtx, msgIdx, attIdx, a, serverURL)
				}
				// Klikací odkaz na soubor
				btn := &v.actions[msgIdx].attBtns[attIdx]
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
								sizeStr := FormatBytes(a.Size)
								lbl := material.Caption(v.app.Theme.Material, a.Filename+" ("+sizeStr+") — click to save")
								lbl.Color = ColorAccent
								return lbl.Layout(gtx)
							})
						},
					)
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

// layoutGroupImagePreview vykreslí inline náhled obrázku s možností kliknutí.
func (v *GroupViewUI) layoutGroupImagePreview(gtx layout.Context, msgIdx, attIdx int, att api.Attachment, serverURL string) layout.Dimensions {
	url := serverURL + att.URL
	ci := v.app.Images.Get(url, func() { v.app.Window.Invalidate() })

	btn := &v.actions[msgIdx].attBtns[attIdx]

	if ci == nil {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Loading "+att.Filename+"...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}

	if !ci.ok {
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+") — click to save")
				lbl.Color = ColorAccent
				return lbl.Layout(gtx)
			})
		})
	}

	// Spočítat zobrazovací rozměry (max 400x300, zachovat poměr stran)
	imgBounds := ci.img.Bounds()
	imgW := imgBounds.Dx()
	imgH := imgBounds.Dy()
	maxW := gtx.Dp(400)
	maxH := gtx.Dp(300)

	if imgW > maxW {
		imgH = imgH * maxW / imgW
		imgW = maxW
	}
	if imgH > maxH {
		imgW = imgW * maxH / imgH
		imgH = maxH
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				origW := float32(ci.img.Bounds().Dx())
				origH := float32(ci.img.Bounds().Dy())
				scaleX := float32(imgW) / origW
				scaleY := float32(imgH) / origH

				rr := gtx.Dp(6)
				defer clip.RRect{
					Rect: image.Rect(0, 0, imgW, imgH),
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Push(gtx.Ops).Pop()

				defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()
				ci.op.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)

				return layout.Dimensions{Size: image.Pt(imgW, imgH)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+")")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
		)
	})
}

// downloadGroupFile stáhne soubor z URL a uloží na disk.
func (v *GroupViewUI) downloadGroupFile(fileURL, savePath, filename, token string) {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		log.Printf("group: download request: %v", err)
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("group: download: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("group: download HTTP %d", resp.StatusCode)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("group: read response: %v", err)
		return
	}

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		log.Printf("group: write file: %v", err)
		return
	}
	log.Printf("group: saved %s (%d bytes)", savePath, len(data))
}

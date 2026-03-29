package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/store"
)

type UserPopup struct {
	app *App

	Visible   bool
	UserID    string
	Username  string
	PublicKey string

	sendMsgBtn      widget.Clickable
	addFriendBtn    widget.Clickable
	blockBtn        widget.Clickable
	timeoutBtn      widget.Clickable
	banBtn          widget.Clickable
	hideAllBtn      widget.Clickable
	deleteAllBtn    widget.Clickable
	overlayBtn      widget.Clickable // background overlay (click to close)
	cardBtn         widget.Clickable // card area (absorbs clicks, prevents closing)

	// Contact actions
	renameBtn  widget.Clickable
	notesBtn   widget.Clickable
	shareQRBtn widget.Clickable
	copyKeyBtn widget.Clickable

	// Admin profile
	profileBtn    widget.Clickable
	showProfile   bool
	profile       *api.UserProfile
	profileLoaded bool

	// Report
	reportBtn widget.Clickable

	// Role assignment
	roleBtns  []widget.Clickable
	userRoles map[string]bool // roleID → assigned
	rolesLoaded bool

	// Per-user voice volume
	userVolSlider Slider

	// Share access
	shareBtns    []widget.Clickable
	shareAccess  map[string]bool // shareID → has access
	sharesLoaded bool

	// Contact info (cached)
	serverNames []store.ServerName
	contactNote string
}

func NewUserPopup(a *App) *UserPopup {
	p := &UserPopup{app: a}
	p.userVolSlider.Min = 0
	p.userVolSlider.Max = 2.0
	p.userVolSlider.Value = 1.0
	return p
}

func (p *UserPopup) Show(userID, username string) {
	// Don't show popup for own user
	if conn := p.app.Conn(); conn != nil && userID == conn.UserID {
		return
	}
	p.Visible = true
	p.UserID = userID
	p.Username = username
	p.rolesLoaded = false
	p.showProfile = false
	p.profileLoaded = false
	p.profile = nil
	p.userRoles = nil
	p.sharesLoaded = false
	p.shareAccess = nil
	p.serverNames = nil
	p.contactNote = ""

	// Find public key
	p.PublicKey = ""
	if u := p.app.FindUser(userID); u != nil {
		p.PublicKey = u.PublicKey
	}

	// Load contact info
	if p.PublicKey != "" && p.app.Contacts != nil {
		ct := p.app.Contacts.GetContact(p.PublicKey)
		if ct != nil {
			p.contactNote = ct.Notes
		}
		p.serverNames = p.app.Contacts.GetServerNames(p.PublicKey)
	}

	// Load user's roles and share access in background
	go func() {
		conn := p.app.Conn()
		if conn == nil {
			return
		}
		roles, err := conn.Client.GetUserRoles(userID)
		if err != nil {
			log.Printf("GetUserRoles: %v", err)
			return
		}
		p.userRoles = make(map[string]bool)
		for _, r := range roles {
			p.userRoles[r.ID] = true
		}
		p.rolesLoaded = true

		// Load share access for my shares
		p.app.mu.RLock()
		myShares := make([]api.SharedDirectory, len(conn.MyShares))
		copy(myShares, conn.MyShares)
		p.app.mu.RUnlock()

		if len(myShares) > 0 {
			access := make(map[string]bool)
			for _, s := range myShares {
				perms, err := conn.Client.GetSharePermissions(s.ID)
				if err != nil {
					continue
				}
				for _, perm := range perms {
					if perm.GranteeID != nil && *perm.GranteeID == userID && perm.CanRead {
						access[s.ID] = true
					}
				}
			}
			p.shareAccess = access
			p.sharesLoaded = true
		}

		p.app.Window.Invalidate()
	}()
}

func (p *UserPopup) Hide() {
	p.Visible = false
	p.UserID = ""
	p.Username = ""
	p.PublicKey = ""
}

func (p *UserPopup) Layout(gtx layout.Context) layout.Dimensions {
	if !p.Visible {
		return layout.Dimensions{}
	}

	// Capture values before any Hide() can clear them
	userID := p.UserID
	isFriend := p.isFriend(userID)
	showAdmin := p.canModerate(userID)

	// Check if user is in the same voice channel as me
	inVoiceWithMe := false
	if conn := p.app.Conn(); conn != nil && conn.Voice != nil && conn.Voice.IsActive() {
		myChannel, _, _ := conn.Voice.GetState()
		p.app.mu.RLock()
		for _, uid := range conn.VoiceState[myChannel] {
			if uid == userID && userID != conn.UserID {
				inVoiceWithMe = true
				break
			}
		}
		p.app.mu.RUnlock()

		// Init slider from current value
		if inVoiceWithMe {
			conn.Voice.GetLevels() // ensure state is fresh
			p.app.mu.RLock()       // not needed for UserVolumes but safe
			p.app.mu.RUnlock()
			if vol, ok := conn.Voice.UserVolumes[userID]; ok {
				p.userVolSlider.Value = vol
			} else {
				p.userVolSlider.Value = 1.0
			}
		}
	}

	// Handle per-user volume slider
	if p.userVolSlider.Changed() && inVoiceWithMe {
		if conn := p.app.Conn(); conn != nil && conn.Voice != nil {
			conn.Voice.SetUserVolume(userID, p.userVolSlider.Value)
		}
	}

	// Gather roles
	var allRoles []api.Role
	if showAdmin && p.rolesLoaded {
		conn := p.app.Conn()
		if conn != nil {
			p.app.mu.RLock()
			allRoles = make([]api.Role, len(conn.Roles))
			copy(allRoles, conn.Roles)
			p.app.mu.RUnlock()
		}
	}
	if len(p.roleBtns) < len(allRoles) {
		p.roleBtns = make([]widget.Clickable, len(allRoles)+5)
	}

	// Handle role toggle clicks
	for i, role := range allRoles {
		if p.roleBtns[i].Clicked(gtx) {
			roleID := role.ID
			has := p.userRoles[roleID]
			go func() {
				conn := p.app.Conn()
				if conn == nil {
					return
				}
				if has {
					if err := conn.Client.RemoveRole(userID, roleID); err != nil {
						log.Printf("RemoveRole: %v", err)
						return
					}
					delete(p.userRoles, roleID)
				} else {
					if err := conn.Client.AssignRole(userID, roleID); err != nil {
						log.Printf("AssignRole: %v", err)
						return
					}
					p.userRoles[roleID] = true
				}
				p.app.Window.Invalidate()
			}()
		}
	}

	// Gather my shares
	var myShares []api.SharedDirectory
	if p.sharesLoaded {
		if conn := p.app.Conn(); conn != nil {
			p.app.mu.RLock()
			myShares = make([]api.SharedDirectory, len(conn.MyShares))
			copy(myShares, conn.MyShares)
			p.app.mu.RUnlock()
		}
	}
	// Don't show if clicking on myself or if I have no shares
	if conn := p.app.Conn(); conn != nil && userID == conn.UserID {
		myShares = nil
	}
	if len(p.shareBtns) < len(myShares) {
		p.shareBtns = make([]widget.Clickable, len(myShares)+5)
	}

	// Handle share access toggle clicks
	for i, share := range myShares {
		if p.shareBtns[i].Clicked(gtx) {
			shareID := share.ID
			hasAccess := p.shareAccess[shareID]
			go func() {
				conn := p.app.Conn()
				if conn == nil {
					return
				}
				uid := userID
				if hasAccess {
					// Find and delete the permission
					perms, err := conn.Client.GetSharePermissions(shareID)
					if err == nil {
						for _, perm := range perms {
							if perm.GranteeID != nil && *perm.GranteeID == uid {
								conn.Client.DeleteSharePermission(shareID, perm.ID)
								break
							}
						}
					}
					delete(p.shareAccess, shareID)
				} else {
					if _, err := conn.Client.SetSharePermission(shareID, &uid, true, false, false, false); err != nil {
						log.Printf("SetSharePermission error: %v", err)
					} else {
						if p.shareAccess == nil {
							p.shareAccess = make(map[string]bool)
						}
						p.shareAccess[shareID] = true
					}
				}
				p.app.Window.Invalidate()
			}()
		}
	}

	// Contact action clicks
	if p.renameBtn.Clicked(gtx) {
		pubKey := p.PublicKey
		p.Hide()
		if pubKey != "" && p.app.Contacts != nil {
			ct := p.app.Contacts.GetContact(pubKey)
			currentName := ""
			if ct != nil {
				currentName = ct.CustomName
			}
			p.app.InputDlg.Show("Rename Contact", "Custom name (empty to reset)", "Save", currentName, func(val string) {
				p.app.Contacts.SetCustomName(pubKey, val)
				p.app.Window.Invalidate()
			})
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.notesBtn.Clicked(gtx) {
		pubKey := p.PublicKey
		notes := p.contactNote
		p.Hide()
		if pubKey != "" && p.app.Contacts != nil {
			p.app.InputDlg.Show("Contact Notes", "Notes", "Save", notes, func(val string) {
				p.app.Contacts.SetNotes(pubKey, val)
				p.app.Window.Invalidate()
			})
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.shareQRBtn.Clicked(gtx) {
		pubKey := p.PublicKey
		uname := p.Username
		p.Hide()
		if pubKey != "" {
			p.app.QRDlg.Show("Contact: "+uname, "nora://contact/"+pubKey+"?name="+uname)
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.copyKeyBtn.Clicked(gtx) {
		if p.PublicKey != "" {
			// Build key#server format
			copyStr := p.PublicKey
			conn := p.app.Conn()
			if conn != nil && conn.URL != "" {
				copyStr = p.PublicKey + "#" + conn.URL
			}
			copyToClipboard(copyStr)
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Profile button (admin)
	if p.profileBtn.Clicked(gtx) && showAdmin {
		p.showProfile = !p.showProfile
		if p.showProfile && !p.profileLoaded {
			uid := userID
			go func() {
				if conn := p.app.Conn(); conn != nil {
					prof, err := conn.Client.GetUserProfile(uid)
					if err == nil {
						p.profile = prof
						p.profileLoaded = true
						p.app.Window.Invalidate()
					}
				}
			}()
		}
	}

	// Report button
	if p.reportBtn.Clicked(gtx) {
		uid := userID
		uname := p.Username
		p.Hide()
		p.app.InputDlg.Show("Report "+uname, "Reason (optional)", "Report", "", func(reason string) {
			go func() {
				if conn := p.app.Conn(); conn != nil {
					if err := conn.Client.ReportUser(uid, "", reason); err != nil {
						p.app.Toasts.Error("Report failed: " + err.Error())
					} else {
						p.app.Toasts.Info("User reported.")
					}
					p.app.Window.Invalidate()
				}
			}()
		})
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Handle clicks — action buttons first, overlay last
	if p.sendMsgBtn.Clicked(gtx) {
		p.Hide()
		go p.openDMFor(userID)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.addFriendBtn.Clicked(gtx) {
		p.Hide()
		if isFriend {
			go p.removeFriendFor(userID)
		} else {
			go p.addFriendFor(userID)
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.blockBtn.Clicked(gtx) {
		p.Hide()
		go p.blockUserFor(userID)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.timeoutBtn.Clicked(gtx) {
		uname := p.Username
		p.Hide()
		p.app.TimeoutDlg.Show(userID, uname)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.hideAllBtn.Clicked(gtx) {
		uname := p.Username
		p.Hide()
		p.app.ConfirmDlg.ShowWithText(
			"Hide Messages",
			"Hide all messages from "+uname+" in this channel?",
			"Hide All",
			func() {
				conn := p.app.Conn()
				if conn == nil {
					return
				}
				conn.Client.HideUserMessages(userID)
			},
		)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.deleteAllBtn.Clicked(gtx) {
		uname := p.Username
		p.Hide()
		p.app.ConfirmDlg.ShowWithText(
			"Delete Messages",
			"Permanently delete all messages from "+uname+"?",
			"Delete All",
			func() {
				conn := p.app.Conn()
				if conn == nil {
					return
				}
				conn.Client.DeleteUserMessages(userID)
			},
		)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if p.banBtn.Clicked(gtx) {
		uname := p.Username
		p.Hide()
		p.app.BanDlg.Show(userID, uname)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	// cardBtn clicks are consumed but ignored (prevents close)
	p.cardBtn.Clicked(gtx)
	// Overlay click = close
	if p.overlayBtn.Clicked(gtx) {
		p.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Use Stack to separate overlay (background) from popup card
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		// Layer 1: Full-screen overlay background (click outside card = close)
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return p.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 120}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		// Layer 2: Popup card (separate from overlay — clicks don't reach overlayBtn)
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(280)
			gtx.Constraints.Min.X = gtx.Dp(280)

			return p.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(12)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
							Rect: bounds,
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Username header with avatar, discriminant, key
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										avatarURL := ""
										if u := p.app.FindUser(userID); u != nil {
											avatarURL = u.AvatarURL
										}
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutAvatar(gtx, p.app, p.Username, avatarURL, 40)
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															lbl := material.H6(p.app.Theme.Material, p.Username)
															lbl.Color = UserColor(p.Username)
															return lbl.Layout(gtx)
														}),
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															if p.PublicKey == "" {
																return layout.Dimensions{}
															}
															disc := store.Discriminant(p.PublicKey)
															lbl := material.Caption(p.app.Theme.Material, "#"+disc)
															lbl.Color = ColorTextDim
															return lbl.Layout(gtx)
														}),
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															conn := p.app.Conn()
															if conn == nil {
																return layout.Dimensions{}
															}
															statusText := conn.UserStatusText[p.UserID]
															if statusText == "" {
																return layout.Dimensions{}
															}
															lbl := material.Caption(p.app.Theme.Material, statusText)
															lbl.Color = ColorTextDim
															return lbl.Layout(gtx)
														}),
													)
												})
											}),
										)
									})
								}),
								// Short key + copy button
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if p.PublicKey == "" {
										return layout.Dimensions{}
									}
									return layout.Inset{Bottom: unit.Dp(8), Left: unit.Dp(0)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return p.copyKeyBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											shortKey := ShortenKey(p.PublicKey)
											conn := p.app.Conn()
											label := shortKey
											if conn != nil && conn.Name != "" {
												label = shortKey + "#" + conn.Name
											}
											label += " [copy]"
											lbl := material.Caption(p.app.Theme.Material, label)
											lbl.Color = ColorTextDim
											if p.copyKeyBtn.Hovered() {
												lbl.Color = ColorAccent
											}
											return lbl.Layout(gtx)
										})
									})
								}),
								// Server names
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if len(p.serverNames) == 0 {
										return layout.Dimensions{}
									}
									return p.layoutServerNames(gtx)
								}),

								// Divider
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
									paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
									return layout.Dimensions{Size: size}
								}),

								// Send Message
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return p.layoutMenuItem(gtx, &p.sendMsgBtn, "Send Message")
								}),

								// Add/Remove Friend
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									label := "Add Friend"
									if isFriend {
										label = "Remove Friend"
									}
									return p.layoutMenuItem(gtx, &p.addFriendBtn, label)
								}),

								// Contact: Rename
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if p.PublicKey == "" {
										return layout.Dimensions{}
									}
									return p.layoutMenuItem(gtx, &p.renameBtn, "Rename Contact")
								}),
								// Contact: Notes
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if p.PublicKey == "" {
										return layout.Dimensions{}
									}
									label := "Notes"
									if p.contactNote != "" {
										label = "Notes: " + truncateStr(p.contactNote, 20)
									}
									return p.layoutMenuItem(gtx, &p.notesBtn, label)
								}),
								// Contact: Share QR
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if p.PublicKey == "" {
										return layout.Dimensions{}
									}
									return p.layoutMenuItem(gtx, &p.shareQRBtn, "Share Contact QR")
								}),

								// Profile (admin)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !showAdmin {
										return layout.Dimensions{}
									}
									label := "View Profile"
									if p.showProfile {
										label = "Hide Profile"
									}
									return p.layoutMenuItem(gtx, &p.profileBtn, label)
								}),
								// Profile details
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !p.showProfile || p.profile == nil {
										return layout.Dimensions{}
									}
									return p.layoutProfile(gtx)
								}),

								// Roles (owner only)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !showAdmin || !p.rolesLoaded || len(allRoles) == 0 {
										return layout.Dimensions{}
									}
									return p.layoutRolesSection(gtx, allRoles)
								}),

								// Hide All Messages (owner only)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !showAdmin {
										return layout.Dimensions{}
									}
									return p.layoutMenuItemDanger(gtx, &p.hideAllBtn, "Hide All Messages")
								}),
								// Delete All Messages (owner only)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !showAdmin {
										return layout.Dimensions{}
									}
									return p.layoutMenuItemDanger(gtx, &p.deleteAllBtn, "Delete All Messages")
								}),
								// Timeout (owner only)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !showAdmin {
										return layout.Dimensions{}
									}
									return p.layoutMenuItemDanger(gtx, &p.timeoutBtn, "Timeout")
								}),
								// Ban (owner only)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !showAdmin {
										return layout.Dimensions{}
									}
									return p.layoutMenuItemDanger(gtx, &p.banBtn, "Ban")
								}),

								// Per-user voice volume
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !inVoiceWithMe {
										return layout.Dimensions{}
									}
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(p.app.Theme.Material, "User Volume")
												lbl.Color = ColorTextDim
												return lbl.Layout(gtx)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
														layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
															return p.userVolSlider.Layout(gtx, 0)
														}),
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																pct := int(p.userVolSlider.Value / p.userVolSlider.Max * 100)
																lbl := material.Caption(p.app.Theme.Material, fmt.Sprintf("%d%%", pct))
																lbl.Color = ColorTextDim
																return lbl.Layout(gtx)
															})
														}),
													)
												})
											}),
										)
									})
								}),

								// Share Access
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if len(myShares) == 0 {
										return layout.Dimensions{}
									}
									return p.layoutSharesSection(gtx, myShares)
								}),

								// Report
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return p.layoutMenuItemDanger(gtx, &p.reportBtn, "Report")
								}),
								// Block
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return p.layoutMenuItemDanger(gtx, &p.blockBtn, "Block")
								}),
							)
						})
					},
				)
			})
		}),
	)
}

func (p *UserPopup) layoutMenuItem(gtx layout.Context, btn *widget.Clickable, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
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
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(p.app.Theme.Material, text)
					lbl.Color = ColorText
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (p *UserPopup) layoutMenuItemDanger(gtx layout.Context, btn *widget.Clickable, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
			bg = color.NRGBA{R: 80, G: 30, B: 30, A: 255}
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
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(p.app.Theme.Material, text)
					lbl.Color = ColorDanger
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (p *UserPopup) layoutRolesSection(gtx layout.Context, roles []api.Role) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(p.app.Theme.Material, "ROLES")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	for i, role := range roles {
		idx := i
		r := role
		has := p.userRoles[r.ID]
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.roleBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg := color.NRGBA{}
				if p.roleBtns[idx].Hovered() {
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
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							check := "[ ] "
							clr := ColorTextDim
							if has {
								check = "[x] "
								clr = ColorAccent
							}
							lbl := material.Body2(p.app.Theme.Material, check+r.Name)
							lbl.Color = clr
							return lbl.Layout(gtx)
						})
					},
				)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (p *UserPopup) openDMFor(userID string) {
	conn := p.app.Conn()
	if conn == nil || userID == "" {
		log.Printf("openDMFor: conn=%v userID=%q", conn, userID)
		return
	}

	// Check existing conversations
	p.app.mu.RLock()
	for _, conv := range conn.DMConversations {
		for _, part := range conv.Participants {
			if part.UserID == userID {
				p.app.mu.RUnlock()
				log.Printf("openDMFor: found existing conv %s", conv.ID)
				p.app.SelectDM(conv.ID)
				p.app.Window.Invalidate()
				return
			}
		}
	}
	p.app.mu.RUnlock()

	// Try to create DM on active server
	log.Printf("openDMFor: creating new DM with user %s", userID)
	conv, err := conn.Client.CreateDMConversation(userID)
	if err != nil {
		log.Printf("CreateDMConversation error: %v — trying cross-server", err)

		// User might be on a different server — find them and try that server
		p.app.mu.RLock()
		var peerPubKey string
		for _, s := range p.app.Servers {
			for _, u := range s.Users {
				if u.ID == userID {
					peerPubKey = u.PublicKey
					break
				}
			}
			if peerPubKey != "" {
				break
			}
		}
		p.app.mu.RUnlock()

		if peerPubKey != "" {
			// Try FindBestDMServer
			p.app.mu.RLock()
			srvIdx, convID, found := p.app.FindBestDMServer(peerPubKey)
			p.app.mu.RUnlock()

			if found && convID != "" {
				p.app.mu.Lock()
				p.app.ActiveServer = srvIdx
				p.app.mu.Unlock()
				p.app.SelectDM(convID)
				p.app.Window.Invalidate()
				return
			}

			if found && srvIdx >= 0 {
				// Server found but no conv — create on that server
				p.app.mu.RLock()
				targetConn := p.app.Servers[srvIdx]
				var targetUserID string
				for _, u := range targetConn.Users {
					if u.PublicKey == peerPubKey {
						targetUserID = u.ID
						break
					}
				}
				p.app.mu.RUnlock()

				if targetUserID != "" {
					conv2, err := targetConn.Client.CreateDMConversation(targetUserID)
					if err == nil {
						p.app.mu.Lock()
						p.app.ActiveServer = srvIdx
						targetConn.DMConversations = append(targetConn.DMConversations, *conv2)
						p.app.mu.Unlock()
						p.app.SelectDM(conv2.ID)
						p.app.Window.Invalidate()
						return
					}
				}
			}

			// Fallback: open relay conversation
			relayConvID := "relay:" + peerPubKey
			p.app.mu.Lock()
			conn.ActiveDMID = relayConvID
			conn.ActiveDMPeerKey = peerPubKey
			p.app.Mode = ViewDM
			p.app.mu.Unlock()
			p.app.Window.Invalidate()
			return
		}
		return
	}

	// Reload conversations from server to get full participant data (including PublicKey)
	convs, err := conn.Client.GetDMConversations()
	if err != nil {
		log.Printf("GetDMConversations error: %v", err)
		p.app.mu.Lock()
		conn.DMConversations = append(conn.DMConversations, *conv)
		p.app.mu.Unlock()
	} else {
		p.app.mu.Lock()
		conn.DMConversations = convs
		p.app.mu.Unlock()
	}

	p.app.SelectDM(conv.ID)
	p.app.Window.Invalidate()
}

func (p *UserPopup) isFriend(userID string) bool {
	conn := p.app.Conn()
	if conn == nil {
		return false
	}
	p.app.mu.RLock()
	defer p.app.mu.RUnlock()
	for _, f := range conn.Friends {
		if f.ID == userID {
			return true
		}
	}
	return false
}

func (p *UserPopup) addFriendFor(userID string) {
	conn := p.app.Conn()
	if conn == nil || userID == "" {
		return
	}
	if err := conn.Client.SendFriendRequest(userID); err != nil {
		log.Printf("SendFriendRequest error: %v", err)
	}
}

func (p *UserPopup) removeFriendFor(userID string) {
	conn := p.app.Conn()
	if conn == nil || userID == "" {
		return
	}
	if err := conn.Client.RemoveFriend(userID); err != nil {
		log.Printf("RemoveFriend error: %v", err)
		return
	}
	// Update local friends list
	p.app.mu.Lock()
	for i, f := range conn.Friends {
		if f.ID == userID {
			conn.Friends = append(conn.Friends[:i], conn.Friends[i+1:]...)
			break
		}
	}
	p.app.mu.Unlock()
	p.app.Window.Invalidate()
}

func (p *UserPopup) canModerate(targetID string) bool {
	conn := p.app.Conn()
	if conn == nil {
		return false
	}
	if conn.UserID == targetID {
		return false
	}
	p.app.mu.RLock()
	defer p.app.mu.RUnlock()
	currentIsOwner := false
	targetIsOwner := false
	for _, u := range conn.Users {
		if u.ID == conn.UserID && u.IsOwner {
			currentIsOwner = true
		}
		if u.ID == targetID && u.IsOwner {
			targetIsOwner = true
		}
	}
	return currentIsOwner && !targetIsOwner
}

func (p *UserPopup) blockUserFor(userID string) {
	conn := p.app.Conn()
	if conn == nil || userID == "" {
		return
	}
	if err := conn.Client.BlockUser(userID); err != nil {
		log.Printf("BlockUser error: %v", err)
	}
}

func (p *UserPopup) layoutProfile(gtx layout.Context) layout.Dimensions {
	prof := p.profile
	th := p.app.Theme

	var items []layout.FlexChild

	// Header
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		lbl := material.Caption(th.Material, "USER PROFILE")
		lbl.Color = ColorAccent
		return lbl.Layout(gtx)
	}))

	// Stats
	statsText := fmt.Sprintf("Messages: %d  |  Uploads: %d (%.1f MB)", prof.MessageCount, prof.UploadCount, prof.UploadSizeMB)
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(th.Material, statsText)
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		})
	}))

	// Invite chain
	if prof.InvitedByName != "" {
		text := "Invited by: " + prof.InvitedByName
		if prof.JoinedVia != "" {
			text += " (code: " + prof.JoinedVia + ")"
		}
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, text)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	}

	// Join date
	if prof.User != nil && !prof.User.CreatedAt.IsZero() {
		text := "Joined: " + prof.User.CreatedAt.Local().Format("2006-01-02 15:04")
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, text)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	}

	// Channel stats (top channels)
	if len(prof.ChannelStats) > 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, "TOP CHANNELS")
				lbl.Color = ColorAccent
				return lbl.Layout(gtx)
			})
		}))
		max := len(prof.ChannelStats)
		if max > 10 {
			max = 10
		}
		for i := 0; i < max; i++ {
			cs := prof.ChannelStats[i]
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(1), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(th.Material, fmt.Sprintf("#%s — %d msgs", cs.ChannelName, cs.MessageCount))
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}))
		}
	}

	// Reports
	if prof.ReportsReceived > 0 || prof.ReportsFiled > 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, "REPORTS")
				lbl.Color = ColorDanger
				return lbl.Layout(gtx)
			})
		}))
		text := fmt.Sprintf("Received: %d  |  Filed: %d", prof.ReportsReceived, prof.ReportsFiled)
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, text)
				clr := ColorTextDim
				if prof.ReportsReceived >= 3 {
					clr = ColorDanger
				}
				lbl.Color = clr
				return lbl.Layout(gtx)
			})
		}))
		// Recent reports
		for _, r := range prof.RecentReports {
			report := r
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					status := report.Status
					reason := report.Reason
					if reason == "" {
						reason = "(no reason)"
					}
					text := fmt.Sprintf("[%s] by %s: %s", status, report.ReporterName, reason)
					lbl := material.Caption(th.Material, text)
					lbl.Color = ColorTextDim
					lbl.MaxLines = 2
					return lbl.Layout(gtx)
				})
			}))
		}
	}

	// Bottom padding
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{}
		})
	}))

	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
	})
}

func (p *UserPopup) layoutSharesSection(gtx layout.Context, shares []api.SharedDirectory) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(p.app.Theme.Material, "SHARE ACCESS")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	for i, share := range shares {
		idx := i
		s := share
		hasAccess := p.shareAccess[s.ID]
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.shareBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg := color.NRGBA{}
				if p.shareBtns[idx].Hovered() {
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
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								// Checkbox indicator
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									sz := gtx.Dp(14)
									clr := ColorTextDim
									if hasAccess {
										clr = ColorAccent
									}
									paint.FillShape(gtx.Ops, clr, clip.RRect{
										Rect: image.Rect(0, 0, sz, sz),
										NE: sz / 4, NW: sz / 4, SE: sz / 4, SW: sz / 4,
									}.Op(gtx.Ops))
									if hasAccess {
										// Inner check mark
										inner := gtx.Dp(6)
										off := (sz - inner) / 2
										paint.FillShape(gtx.Ops, ColorText, clip.Rect{
											Min: image.Pt(off, off),
											Max: image.Pt(off+inner, off+inner),
										}.Op())
									}
									return layout.Dimensions{Size: image.Pt(sz, sz)}
								}),
								// Share name
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(p.app.Theme.Material, s.DisplayName)
										lbl.Color = ColorText
										lbl.MaxLines = 1
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					},
				)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (p *UserPopup) layoutServerNames(gtx layout.Context) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(p.app.Theme.Material, "KNOWN AS")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	for _, sn := range p.serverNames {
		sn := sn
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				name := sn.DisplayName
				if name == "" {
					name = "?"
				}
				srv := sn.ServerName
				if srv == "" {
					srv = sn.ServerURL
				}
				// Check online status on this server
				online := false
				p.app.mu.RLock()
				for _, s := range p.app.Servers {
					if s.URL == sn.ServerURL {
						for _, u := range s.Users {
							if u.PublicKey == sn.PublicKey {
								online = s.OnlineUsers[u.ID]
								break
							}
						}
						break
					}
				}
				p.app.mu.RUnlock()

				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(6)
						clr := ColorOffline
						if online {
							clr = ColorOnline
						}
						paint.FillShape(gtx.Ops, clr, clip.Ellipse{Max: image.Pt(sz, sz)}.Op(gtx.Ops))
						return layout.Dimensions{Size: image.Pt(sz, sz)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(p.app.Theme.Material, name+" on "+srv)
							lbl.Color = ColorTextDim
							lbl.MaxLines = 1
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		}))
	}
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{}
		})
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

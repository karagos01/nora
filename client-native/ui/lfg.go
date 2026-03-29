package ui

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"log"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type LFGBoardView struct {
	app *App

	listings   []api.LFGListing
	channelID  string
	listWidget widget.List

	// Create form
	showCreate     bool
	newBtn         widget.Clickable
	gameNameEd     widget.Editor
	contentEd      widget.Editor
	maxPlayersEd   widget.Editor
	submitBtn      widget.Clickable
	cancelBtn      widget.Clickable

	// Background clicks for editor focus
	gameNameBgClick   gesture.Click
	contentBgClick    gesture.Click
	maxPlayersBgClick gesture.Click
	searchBgClick     gesture.Click
	applyMsgBgClick   gesture.Click

	// Search/filter
	searchEd widget.Editor

	// Per-listing buttons
	deleteBtns map[string]*widget.Clickable
	authorBtns map[string]*widget.Clickable
	dmBtns     map[string]*widget.Clickable
	joinBtns   map[string]*widget.Clickable

	// Application popup
	showApplyPopup  string // listing ID or empty
	applyMessageEd  widget.Editor
	applySubmitBtn  widget.Clickable
	applyCancelBtn  widget.Clickable

	// Application management (author view)
	pendingApps     map[string][]api.LFGApplication // listingID → apps (when not viewing LFG)
	showAppsListing string                          // listing ID or empty
	appsBtn         map[string]*widget.Clickable // per-listing "view apps" button
	acceptBtns      map[string]*widget.Clickable // per-application accept
	rejectBtns      map[string]*widget.Clickable // per-application reject
}

func NewLFGBoardView(a *App) *LFGBoardView {
	v := &LFGBoardView{
		app:        a,
		deleteBtns: make(map[string]*widget.Clickable),
		authorBtns: make(map[string]*widget.Clickable),
		dmBtns:     make(map[string]*widget.Clickable),
		joinBtns:   make(map[string]*widget.Clickable),
		appsBtn:    make(map[string]*widget.Clickable),
		acceptBtns: make(map[string]*widget.Clickable),
		rejectBtns: make(map[string]*widget.Clickable),
	}
	v.listWidget.Axis = layout.Vertical
	v.gameNameEd.SingleLine = true
	v.gameNameEd.Submit = true
	v.gameNameEd.MaxLen = 30
	v.contentEd.SingleLine = false
	v.contentEd.MaxLen = 250
	v.applyMessageEd.SingleLine = false
	v.maxPlayersEd.SingleLine = true
	v.maxPlayersEd.Filter = "0123456789"
	v.searchEd.SingleLine = true
	return v
}

func (v *LFGBoardView) Load(channelID string) {
	v.channelID = channelID
	v.showCreate = false
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	go func() {
		listings, err := conn.Client.ListLFGListings(channelID)
		if err != nil {
			log.Printf("ListLFGListings: %v", err)
			return
		}
		v.app.mu.Lock()
		v.listings = listings
		// Merge any pending applications received while not viewing
		if v.pendingApps != nil {
			for i := range v.listings {
				if apps, ok := v.pendingApps[v.listings[i].ID]; ok {
					v.listings[i].Applications = apps
				}
			}
			v.pendingApps = nil
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *LFGBoardView) Layout(gtx layout.Context) layout.Dimensions {
	th := v.app.Theme

	// Handle new button
	if v.newBtn.Clicked(gtx) {
		v.showCreate = !v.showCreate
		if v.showCreate {
			v.gameNameEd.SetText("")
			v.contentEd.SetText("")
		}
	}

	// Handle submit
	if v.submitBtn.Clicked(gtx) {
		v.doCreate()
	}

	if v.cancelBtn.Clicked(gtx) {
		v.showCreate = false
	}

	// Handle delete buttons
	for id, btn := range v.deleteBtns {
		if btn.Clicked(gtx) {
			delID := id
			v.app.ConfirmDlg.Show("Delete LFG Post", "Delete this post? The group conversation will be kept.", func() {
				v.doDelete(delID)
			})
		}
	}

	// Handle apply popup buttons
	if v.applySubmitBtn.Clicked(gtx) && v.showApplyPopup != "" {
		listID := v.showApplyPopup
		msg := strings.TrimSpace(v.applyMessageEd.Text())
		chID := v.channelID
		v.showApplyPopup = ""
		go func() {
			if conn := v.app.Conn(); conn != nil {
				conn.Client.ApplyLFGListing(chID, listID, msg)
			}
		}()
	}
	if v.applyCancelBtn.Clicked(gtx) {
		v.showApplyPopup = ""
	}

	// Handle apps view button
	for id, btn := range v.appsBtn {
		if btn.Clicked(gtx) {
			if v.showAppsListing == id {
				v.showAppsListing = ""
			} else {
				v.showAppsListing = id
			}
		}
	}

	// Handle accept/reject buttons
	for key, btn := range v.acceptBtns {
		if btn.Clicked(gtx) {
			parts := strings.SplitN(key, "|", 2)
			if len(parts) == 2 {
				listID, userID := parts[0], parts[1]
				chID := v.channelID
				go func() {
					if conn := v.app.Conn(); conn != nil {
						conn.Client.AcceptLFGApplication(chID, listID, userID)
						// Reload listings
						v.Load(chID)
					}
				}()
			}
		}
	}
	for key, btn := range v.rejectBtns {
		if btn.Clicked(gtx) {
			parts := strings.SplitN(key, "|", 2)
			if len(parts) == 2 {
				listID, userID := parts[0], parts[1]
				chID := v.channelID
				go func() {
					if conn := v.app.Conn(); conn != nil {
						conn.Client.RejectLFGApplication(chID, listID, userID)
						v.Load(chID)
					}
				}()
			}
		}
	}

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutHeader(gtx, th)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutSearch(gtx, th)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !v.showCreate {
						return layout.Dimensions{}
					}
					return v.layoutCreateForm(gtx, th)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.layoutListings(gtx, th)
				}),
			)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if v.showApplyPopup == "" {
				return layout.Dimensions{}
			}
			return v.layoutApplyPopup(gtx, th)
		}),
	)
}

func (v *LFGBoardView) layoutHeader(gtx layout.Context, th *Theme) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					conn := v.app.Conn()
					name := "LFG Board"
					if conn != nil {
						name = conn.ActiveChannelName + " - Looking For Group"
					}
					lbl := material.Label(th.Material, th.Sp(18), name)
					lbl.Font.Weight = font.Bold
					lbl.Color = ColorText
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					label := "New Post"
					if v.showCreate {
						label = "Cancel"
					}
					btn := material.Button(th.Material, &v.newBtn, label)
					btn.Background = ColorAccent
					btn.Color = ColorWhite
					btn.CornerRadius = unit.Dp(4)
					btn.TextSize = th.Sp(13)
					btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}
					return btn.Layout(gtx)
				}),
			)
		},
	)
}

func (v *LFGBoardView) layoutSearch(gtx layout.Context, th *Theme) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th.Material, &v.searchEd, "Filter by game or player...")
		ed.Color = ColorText
		ed.HintColor = ColorTextDim
		ed.TextSize = th.Sp(14)
		return lfgEditorBox(gtx, ed, &v.searchBgClick)
	})
}

func (v *LFGBoardView) layoutCreateForm(gtx layout.Context, th *Theme) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(12)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
					rr := clip.RRect{Rect: image.Rect(0, 0, sz.X, sz.Y), SE: gtx.Dp(6), SW: gtx.Dp(6), NE: gtx.Dp(6), NW: gtx.Dp(6)}
					paint.FillShape(gtx.Ops, ColorCard, rr.Op(gtx.Ops))
					return layout.Dimensions{Size: sz}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return lfgFieldLabel(gtx, th, "Game Name", len([]rune(v.gameNameEd.Text())), 30)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										ed := material.Editor(th.Material, &v.gameNameEd, "e.g. Valorant, CS2, League...")
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										ed.TextSize = th.Sp(14)
										return lfgEditorBox(gtx, ed, &v.gameNameBgClick)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return lfgFieldLabel(gtx, th, "Description", len([]rune(v.contentEd.Text())), 250)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										ed := material.Editor(th.Material, &v.contentEd, "Looking for teammates to play...")
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										ed.TextSize = th.Sp(14)
										gtx.Constraints.Min.Y = gtx.Dp(60)
										return lfgEditorBox(gtx, ed, &v.contentBgClick)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(th.Material, th.Sp(13), "Max Players")
												lbl.Color = ColorTextDim
												return lbl.Layout(gtx)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													gtx.Constraints.Max.X = gtx.Dp(60)
													ed := material.Editor(th.Material, &v.maxPlayersEd, "0")
													ed.Color = ColorText
													ed.HintColor = ColorTextDim
													ed.TextSize = th.Sp(14)
													return lfgEditorBox(gtx, ed, &v.maxPlayersBgClick)
												})
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Label(th.Material, th.Sp(13), "(0 = unlimited)")
													lbl.Color = ColorTextDim
													return lbl.Layout(gtx)
												})
											}),
										)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											btn := material.Button(th.Material, &v.cancelBtn, "Cancel")
											btn.Background = ColorInput
											btn.Color = ColorText
											btn.CornerRadius = unit.Dp(4)
											btn.TextSize = th.Sp(13)
											btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}
											return btn.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											btn := material.Button(th.Material, &v.submitBtn, "Post")
											btn.Background = ColorAccent
											btn.Color = ColorWhite
											btn.CornerRadius = unit.Dp(4)
											btn.TextSize = th.Sp(13)
											btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}
											return btn.Layout(gtx)
										}),
									)
								}),
							)
						},
					)
				},
			)
		},
	)
}

func (v *LFGBoardView) layoutListings(gtx layout.Context, th *Theme) layout.Dimensions {
	v.app.mu.RLock()
	allListings := make([]api.LFGListing, len(v.listings))
	copy(allListings, v.listings)
	v.app.mu.RUnlock()

	// Filter by search query
	query := strings.ToLower(strings.TrimSpace(v.searchEd.Text()))
	var listings []api.LFGListing
	if query == "" {
		listings = allListings
	} else {
		for _, l := range allListings {
			authorName := ""
			if l.Author != nil {
				authorName = strings.ToLower(v.app.ResolveUserName(l.Author))
			}
			if strings.Contains(strings.ToLower(l.GameName), query) ||
				strings.Contains(strings.ToLower(l.Content), query) ||
				strings.Contains(authorName, query) {
				listings = append(listings, l)
			}
		}
	}

	if len(listings) == 0 {
		msg := "No LFG posts yet. Click 'New Post' to create one!"
		if query != "" {
			msg = "No posts match your search."
		}
		return layoutCentered(gtx, th, msg, ColorTextDim)
	}

	// Fill background to prevent artifacts from bleeding through
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return material.List(th.Material, &v.listWidget).Layout(gtx, len(listings), func(gtx layout.Context, i int) layout.Dimensions {
		listing := listings[i]
		return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(6)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutListingCard(gtx, th, listing)
					}),
					// Applications list rendered OUTSIDE the card
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						conn := v.app.Conn()
						isOwn := conn != nil && listing.UserID == conn.UserID
						if !isOwn || v.showAppsListing != listing.ID {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutApplications(gtx, th, listing)
						})
					}),
				)
			},
		)
	})
}

func (v *LFGBoardView) layoutListingCard(gtx layout.Context, th *Theme, listing api.LFGListing) layout.Dimensions {
	conn := v.app.Conn()
	isOwn := conn != nil && listing.UserID == conn.UserID

	// Ensure per-listing buttons exist
	if _, ok := v.deleteBtns[listing.ID]; !ok {
		v.deleteBtns[listing.ID] = &widget.Clickable{}
	}
	if _, ok := v.authorBtns[listing.ID]; !ok {
		v.authorBtns[listing.ID] = &widget.Clickable{}
	}
	if _, ok := v.dmBtns[listing.ID]; !ok {
		v.dmBtns[listing.ID] = &widget.Clickable{}
	}
	if _, ok := v.joinBtns[listing.ID]; !ok {
		v.joinBtns[listing.ID] = &widget.Clickable{}
	}

	// Handle author click — show user popup
	if v.authorBtns[listing.ID].Clicked(gtx) && listing.Author != nil {
		authorName := v.app.ResolveUserName(listing.Author)
		v.app.UserPopup.Show(listing.UserID, authorName)
	}
	// Handle DM click
	if v.dmBtns[listing.ID].Clicked(gtx) && listing.Author != nil && !isOwn {
		v.app.UserPopup.openDMFor(listing.UserID)
	}
	// Handle join/leave click
	if v.joinBtns[listing.ID].Clicked(gtx) && conn != nil {
		myID := conn.UserID
		joined := false
		for _, p := range listing.Participants {
			if p.ID == myID {
				joined = true
				break
			}
		}
		listID := listing.ID
		chID := listing.ChannelID
		if joined {
			go func() {
				conn.Client.LeaveLFGListing(chID, listID)
			}()
		} else if listing.MaxPlayers > 0 {
			// With limit → show application popup
			v.showApplyPopup = listID
			v.applyMessageEd.SetText("")
		} else {
			// No limit → direct join
			go func() {
				conn.Client.JoinLFGListing(chID, listID)
			}()
		}
	}

	accentColor := lfgGameColor(listing.GameName)
	authorName := "Unknown"
	authorAvatar := ""
	if listing.Author != nil {
		authorName = v.app.ResolveUserName(listing.Author)
		authorAvatar = listing.Author.AvatarURL
	}

	return layout.Stack{}.Layout(gtx,
		// Background — fills to match content size
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Min
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
				Rect: image.Rect(0, 0, sz.X, sz.Y),
				SE: rr, SW: rr, NE: rr, NW: rr,
			}.Op(gtx.Ops))
			// Left accent bar
			paint.FillShape(gtx.Ops, accentColor, clip.Rect{Max: image.Pt(gtx.Dp(3), sz.Y)}.Op())
			return layout.Dimensions{Size: sz}
		}),
		// Content
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Start}.Layout(gtx,
						// Avatar
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(2), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.authorBtns[listing.ID].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutAvatar(gtx, v.app, authorName, authorAvatar, 32)
								})
							})
						}),
						// Content area
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Row 1: author name + game tag + time + actions
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										// Author name (clickable)
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return v.authorBtns[listing.ID].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												nameColor := UserColor(authorName)
												if conn != nil {
													nameColor = v.app.GetUserRoleColor(conn, listing.UserID, authorName)
												}
												lbl := material.Label(th.Material, th.Sp(14), authorName)
												lbl.Font.Weight = font.Bold
												lbl.Color = nameColor
												return lbl.Layout(gtx)
											})
										}),
										// Game tag
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layout.Background{}.Layout(gtx,
													func(gtx layout.Context) layout.Dimensions {
														sz := image.Pt(gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
														rr := gtx.Dp(3)
														// Semi-transparent accent
														tagBg := accentColor
														tagBg.A = 40
														paint.FillShape(gtx.Ops, tagBg, clip.RRect{
															Rect: image.Rect(0, 0, sz.X, sz.Y),
															NE: rr, NW: rr, SE: rr, SW: rr,
														}.Op(gtx.Ops))
														return layout.Dimensions{Size: sz}
													},
													func(gtx layout.Context) layout.Dimensions {
														return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															lbl := material.Label(th.Material, th.Sp(14), listing.GameName)
															lbl.Color = accentColor
															return lbl.Layout(gtx)
														})
													},
												)
											})
										}),
										// Timestamp
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(th.Material, th.Sp(14), lfgTimeAgo(listing.CreatedAt))
												lbl.Color = ColorTextDim
												return lbl.Layout(gtx)
											})
										}),
										// Spacer
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
										}),
										// Expiry badge
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											remaining := time.Until(listing.ExpiresAt)
											var expText string
											expClr := ColorTextDim
											if remaining <= 0 {
												expText = "Expired"
												expClr = ColorDanger
											} else if remaining < time.Hour {
												expText = fmt.Sprintf("%dm left", int(remaining.Minutes()))
												expClr = ColorWarning
											} else if remaining < 24*time.Hour {
												expText = fmt.Sprintf("%dh left", int(remaining.Hours()))
											} else {
												expText = fmt.Sprintf("%dd left", int(remaining.Hours()/24))
											}
											lbl := material.Label(th.Material, th.Sp(13), expText)
											lbl.Color = expClr
											return lbl.Layout(gtx)
										}),
										// Delete (own only)
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if !isOwn {
												return layout.Dimensions{}
											}
											return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.deleteBtns[listing.ID].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layoutIcon(gtx, IconDelete, 14, ColorDanger)
												})
											})
										}),
									)
								}),
								// Row 2: content text
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(th.Material, th.Sp(14), listing.Content)
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									})
								}),
								// Row 3: join/leave + participants + DM
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										myID := ""
										if conn != nil {
											myID = conn.UserID
										}
										joined := false
										for _, p := range listing.Participants {
											if p.ID == myID {
												joined = true
												break
											}
										}
										nPart := len(listing.Participants)

										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											// Join/Leave button
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												label := "Join"
												btnBg := ColorAccent
												if listing.MaxPlayers > 0 && !joined && !isOwn {
													label = "Apply"
												}
												if joined {
													label = "Leave"
													btnBg = ColorInput
												}
												// Full check
												if !joined && listing.MaxPlayers > 0 && nPart >= listing.MaxPlayers {
													label = "Full"
													btnBg = ColorInput
												}
												btn := material.Button(th.Material, v.joinBtns[listing.ID], label)
												btn.Background = btnBg
												btn.Color = ColorWhite
												btn.CornerRadius = unit.Dp(3)
												btn.TextSize = th.Sp(13)
												btn.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}
												return btn.Layout(gtx)
											}),
											// Participant count
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													text := fmt.Sprintf("%d", nPart)
													if listing.MaxPlayers > 0 {
														text = fmt.Sprintf("%d/%d", nPart, listing.MaxPlayers)
													}
													text += " joined"
													lbl := material.Label(th.Material, th.Sp(13), text)
													clr := ColorTextDim
													if listing.MaxPlayers > 0 && nPart >= listing.MaxPlayers {
														clr = ColorWarning
													}
													lbl.Color = clr
													return lbl.Layout(gtx)
												})
											}),
											// Participant avatars (first 5)
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												if nPart == 0 {
													return layout.Dimensions{}
												}
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													max := nPart
													if max > 5 {
														max = 5
													}
													var avatarChildren []layout.FlexChild
													for pi := 0; pi < max; pi++ {
														name := listing.Participants[pi].DisplayName
														if name == "" {
															name = listing.Participants[pi].Username
														}
														avatar := listing.Participants[pi].AvatarURL
														avatarChildren = append(avatarChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																return layoutAvatar(gtx, v.app, name, avatar, 16)
															})
														}))
													}
													if nPart > 5 {
														avatarChildren = append(avatarChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															lbl := material.Label(th.Material, th.Sp(14), fmt.Sprintf("+%d", nPart-5))
															lbl.Color = ColorTextDim
															return lbl.Layout(gtx)
														}))
													}
													return layout.Flex{Alignment: layout.Middle}.Layout(gtx, avatarChildren...)
												})
											}),
											// DM button (for other users' listings)
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												if isOwn || listing.Author == nil {
													return layout.Dimensions{}
												}
												return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return v.dmBtns[listing.ID].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
															layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																return layoutIcon(gtx, IconChat, 12, ColorAccent)
															}),
															layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																return layout.Inset{Left: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																	lbl := material.Label(th.Material, th.Sp(14), "Message")
																	lbl.Color = ColorAccent
																	return lbl.Layout(gtx)
																})
															}),
														)
													})
												})
											}),
										)
									})
								}),
							)
						}),
						// Row 4: Applications button (author only)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !isOwn || listing.MaxPlayers == 0 || len(listing.Applications) == 0 {
								return layout.Dimensions{}
							}
							if _, ok := v.appsBtn[listing.ID]; !ok {
								v.appsBtn[listing.ID] = &widget.Clickable{}
							}
							pendingCount := 0
							for _, a := range listing.Applications {
								if a.Status == "pending" {
									pendingCount++
								}
							}
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								label := fmt.Sprintf("Applications (%d pending)", pendingCount)
								btn := material.Button(th.Material, v.appsBtn[listing.ID], label)
								btn.Background = ColorInput
								btn.Color = ColorText
								btn.CornerRadius = unit.Dp(3)
								btn.TextSize = th.Sp(13)
								btn.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}
								return btn.Layout(gtx)
							})
						}),
					)
				},
			)
		}),
	)
}

func (v *LFGBoardView) layoutApplications(gtx layout.Context, th *Theme, listing api.LFGListing) layout.Dimensions {
	// Filter pending apps
	var pending []api.LFGApplication
	for _, a := range listing.Applications {
		if a.Status == "pending" {
			pending = append(pending, a)
		}
	}
	if len(pending) == 0 {
		return layout.Dimensions{}
	}

	// Ensure buttons for all pending apps
	for _, a := range pending {
		k := a.ListingID + "|" + a.UserID
		if _, ok := v.acceptBtns[k]; !ok {
			v.acceptBtns[k] = &widget.Clickable{}
		}
		if _, ok := v.rejectBtns[k]; !ok {
			v.rejectBtns[k] = &widget.Clickable{}
		}
	}

	// Render each application sequentially using Flex
	var items []layout.FlexChild
	for idx := range pending {
		i := idx
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			a := pending[i]
			k := a.ListingID + "|" + a.UserID
			return v.layoutOneApplication(gtx, th, a, v.acceptBtns[k], v.rejectBtns[k])
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *LFGBoardView) layoutOneApplication(gtx layout.Context, th *Theme, a api.LFGApplication, acceptBtn, rejectBtn *widget.Clickable) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Min
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
					Rect: image.Rect(0, 0, sz.X, sz.Y),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			uname := "Unknown"
			avatar := ""
			if a.User != nil {
				uname = a.User.DisplayName
				if uname == "" {
					uname = a.User.Username
				}
				avatar = a.User.AvatarURL
			}
			resolvedName := uname
			if a.User != nil {
				resolvedName = v.app.ResolveUserName(a.User)
			}

			var items []layout.FlexChild
			// Row: avatar + name + buttons
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutAvatar(gtx, v.app, uname, avatar, 20)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(th.Material, th.Sp(13), resolvedName)
							lbl.Font.Weight = font.Bold
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th.Material, acceptBtn, "Accept")
						btn.Background = ColorOnline
						btn.Color = ColorWhite
						btn.CornerRadius = unit.Dp(3)
						btn.TextSize = th.Sp(13)
						btn.Inset = layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(8), Right: unit.Dp(8)}
						return btn.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							btn := material.Button(th.Material, rejectBtn, "Reject")
							btn.Background = ColorDanger
							btn.Color = ColorWhite
							btn.CornerRadius = unit.Dp(3)
							btn.TextSize = th.Sp(13)
							btn.Inset = layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(8), Right: unit.Dp(8)}
							return btn.Layout(gtx)
						})
					}),
				)
			}))
			// Message row
			if a.Message != "" {
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(th.Material, th.Sp(14), a.Message)
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
		})
			}),
		)
	})
}

func (v *LFGBoardView) layoutApplyPopup(gtx layout.Context, th *Theme) layout.Dimensions {
	// Semi-transparent backdrop
	paint.FillShape(gtx.Ops, color.NRGBA{0, 0, 0, 150}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(400)
		gtx.Constraints.Min.X = gtx.Dp(400)
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
					Rect: image.Rect(0, 0, sz.X, sz.Y),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(th.Material, th.Sp(16), "Apply to Join")
							lbl.Font.Weight = font.Bold
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(th.Material, th.Sp(14), "This listing has a player limit. Write a short message to the host.")
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.Y = gtx.Dp(60)
							ed := material.Editor(th.Material, &v.applyMessageEd, "Why should you be picked?")
							ed.Color = ColorText
							ed.HintColor = ColorTextDim
							ed.TextSize = th.Sp(14)
							return lfgEditorBox(gtx, ed, &v.applyMsgBgClick)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										btn := material.Button(th.Material, &v.applyCancelBtn, "Cancel")
										btn.Background = ColorInput
										btn.Color = ColorText
										btn.CornerRadius = unit.Dp(4)
										btn.TextSize = th.Sp(13)
										btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}
										return btn.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										btn := material.Button(th.Material, &v.applySubmitBtn, "Send Application")
										btn.Background = ColorAccent
										btn.Color = ColorWhite
										btn.CornerRadius = unit.Dp(4)
										btn.TextSize = th.Sp(13)
										btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}
										return btn.Layout(gtx)
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

func (v *LFGBoardView) doCreate() {
	gameName := strings.TrimSpace(v.gameNameEd.Text())
	content := strings.TrimSpace(v.contentEd.Text())
	if gameName == "" || content == "" {
		return
	}
	maxPlayers, _ := strconv.Atoi(strings.TrimSpace(v.maxPlayersEd.Text()))

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	chID := v.channelID
	go func() {
		listing, err := conn.Client.CreateLFGListing(chID, gameName, content, maxPlayers)
		if err != nil {
			log.Printf("CreateLFGListing: %v", err)
			return
		}
		v.app.mu.Lock()
		// Prepend (newest first)
		v.listings = append([]api.LFGListing{*listing}, v.listings...)
		v.showCreate = false
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *LFGBoardView) doDelete(listingID string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	chID := v.channelID
	go func() {
		if err := conn.Client.DeleteLFGListing(chID, listingID); err != nil {
			log.Printf("DeleteLFGListing: %v", err)
			return
		}
		v.app.mu.Lock()
		for i, l := range v.listings {
			if l.ID == listingID {
				v.listings = append(v.listings[:i], v.listings[i+1:]...)
				break
			}
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

// HandleWSCreate adds a listing from a WS event.
func (v *LFGBoardView) HandleWSCreate(listing api.LFGListing) {
	v.app.mu.Lock()
	defer v.app.mu.Unlock()
	for _, l := range v.listings {
		if l.ID == listing.ID {
			return
		}
	}
	v.listings = append([]api.LFGListing{listing}, v.listings...)
}

// HandleWSDelete removes a listing from a WS event.
func (v *LFGBoardView) HandleWSDelete(listingID string) {
	v.app.mu.Lock()
	defer v.app.mu.Unlock()
	for i, l := range v.listings {
		if l.ID == listingID {
			v.listings = append(v.listings[:i], v.listings[i+1:]...)
			break
		}
	}
	// Cleanup button maps
	delete(v.deleteBtns, listingID)
	delete(v.authorBtns, listingID)
	delete(v.dmBtns, listingID)
	delete(v.joinBtns, listingID)
	delete(v.appsBtn, listingID)
	// Cleanup accept/reject buttons (keyed as "listingID|userID")
	for k := range v.acceptBtns {
		if len(k) > len(listingID) && k[:len(listingID)+1] == listingID+"|" {
			delete(v.acceptBtns, k)
		}
	}
	for k := range v.rejectBtns {
		if len(k) > len(listingID) && k[:len(listingID)+1] == listingID+"|" {
			delete(v.rejectBtns, k)
		}
	}
	delete(v.pendingApps, listingID)
}

// HandleWSParticipants updates participants for a listing from a WS event.
func (v *LFGBoardView) HandleWSParticipants(listingID string, participants []api.User) {
	v.app.mu.Lock()
	defer v.app.mu.Unlock()
	for i := range v.listings {
		if v.listings[i].ID == listingID {
			v.listings[i].Participants = participants
			return
		}
	}
}

// HandleWSApplications updates applications for a listing from a WS event.
func (v *LFGBoardView) HandleWSApplications(listingID string, apps []api.LFGApplication) {
	v.app.mu.Lock()
	defer v.app.mu.Unlock()
	found := false
	for i := range v.listings {
		if v.listings[i].ID == listingID {
			v.listings[i].Applications = apps
			found = true
			break
		}
	}
	// If listing not loaded (author is on different page), store in pending map
	if !found {
		if v.pendingApps == nil {
			v.pendingApps = make(map[string][]api.LFGApplication)
		}
		v.pendingApps[listingID] = apps
	}
}

// lfgTimeAgo returns a human-readable relative time string.
func lfgTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

// lfgGameColor returns a deterministic accent color based on game name.
func lfgGameColor(name string) color.NRGBA {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	hue := h.Sum32() % 360
	r, g, b := hslToRGB(float64(hue), 0.6, 0.55)
	return color.NRGBA{R: r, G: g, B: b, A: 255}
}

// lfgEditorBox renders an editor inside a bordered box.
func lfgFieldLabel(gtx layout.Context, th *Theme, label string, current, max int) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(th.Material, th.Sp(14), label)
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			clr := ColorTextDim
			if current >= max {
				clr = ColorWarning
			}
			lbl := material.Label(th.Material, th.Sp(13), fmt.Sprintf("%d/%d", current, max))
			lbl.Color = clr
			return lbl.Layout(gtx)
		}),
	)
}

func lfgEditorBox(gtx layout.Context, ed material.EditorStyle, bgClick *gesture.Click) layout.Dimensions {
	// Process background clicks → focus editor
	if bgClick != nil {
		for {
			ev, ok := bgClick.Update(gtx.Source)
			if !ok {
				break
			}
			if ev.Kind == gesture.KindClick {
				gtx.Execute(key.FocusCmd{Tag: ed.Editor})
			}
		}
	}

	dims := layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(4)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
				Rect: image.Rect(0, 0, sz.X, sz.Y),
				SE: rr, SW: rr, NE: rr, NW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: sz}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ed.Layout(gtx)
			})
		},
	)

	// Register background click + text cursor on full box area
	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	if bgClick != nil {
		bgClick.Add(gtx.Ops)
	}
	return dims
}

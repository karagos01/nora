package ui

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"log"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/text"
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
	showCreate bool
	newBtn     widget.Clickable
	gameNameEd widget.Editor
	contentEd  widget.Editor
	submitBtn  widget.Clickable
	cancelBtn  widget.Clickable

	// Per-listing buttons
	deleteBtns map[string]*widget.Clickable
	authorBtns map[string]*widget.Clickable
	dmBtns     map[string]*widget.Clickable
}

func NewLFGBoardView(a *App) *LFGBoardView {
	v := &LFGBoardView{
		app:        a,
		deleteBtns: make(map[string]*widget.Clickable),
		authorBtns: make(map[string]*widget.Clickable),
		dmBtns:     make(map[string]*widget.Clickable),
	}
	v.listWidget.Axis = layout.Vertical
	v.gameNameEd.SingleLine = true
	v.gameNameEd.Submit = true
	v.contentEd.SingleLine = false
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
			v.doDelete(id)
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutHeader(gtx, th)
		}),
		// Create form
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showCreate {
				return layout.Dimensions{}
			}
			return v.layoutCreateForm(gtx, th)
		}),
		// Listings
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return v.layoutListings(gtx, th)
		}),
	)
}

func (v *LFGBoardView) layoutHeader(gtx layout.Context, th *Theme) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					conn := v.app.Conn()
					name := "LFG Board"
					if conn != nil {
						name = conn.ActiveChannelName + " - Looking For Group"
					}
					lbl := material.Label(th.Material, unit.Sp(18), name)
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
					btn.Color = color.NRGBA{255, 255, 255, 255}
					btn.CornerRadius = unit.Dp(4)
					btn.TextSize = unit.Sp(13)
					btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}
					return btn.Layout(gtx)
				}),
			)
		},
	)
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
									lbl := material.Label(th.Material, unit.Sp(13), "Game Name")
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										ed := material.Editor(th.Material, &v.gameNameEd, "e.g. Valorant, CS2, League...")
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										ed.TextSize = unit.Sp(14)
										return lfgEditorBox(gtx, ed)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(th.Material, unit.Sp(13), "Description")
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										ed := material.Editor(th.Material, &v.contentEd, "Looking for teammates to play...")
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										ed.TextSize = unit.Sp(14)
										gtx.Constraints.Min.Y = gtx.Dp(60)
										return lfgEditorBox(gtx, ed)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											btn := material.Button(th.Material, &v.cancelBtn, "Cancel")
											btn.Background = ColorInput
											btn.Color = ColorText
											btn.CornerRadius = unit.Dp(4)
											btn.TextSize = unit.Sp(13)
											btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}
											return btn.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											btn := material.Button(th.Material, &v.submitBtn, "Post")
											btn.Background = ColorAccent
											btn.Color = color.NRGBA{255, 255, 255, 255}
											btn.CornerRadius = unit.Dp(4)
											btn.TextSize = unit.Sp(13)
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
	listings := make([]api.LFGListing, len(v.listings))
	copy(listings, v.listings)
	v.app.mu.RUnlock()

	if len(listings) == 0 {
		return layoutCentered(gtx, th, "No LFG posts yet. Click 'New Post' to create one!", ColorTextDim)
	}

	return material.List(th.Material, &v.listWidget).Layout(gtx, len(listings), func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return v.layoutListingCard(gtx, th, listings[i])
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

	// Handle author click — show user popup
	if v.authorBtns[listing.ID].Clicked(gtx) && listing.Author != nil {
		authorName := v.app.ResolveUserName(listing.Author)
		v.app.UserPopup.Show(listing.UserID, authorName)
	}
	// Handle DM click
	if v.dmBtns[listing.ID].Clicked(gtx) && listing.Author != nil && !isOwn {
		v.app.UserPopup.openDMFor(listing.UserID)
	}

	accentColor := lfgGameColor(listing.GameName)

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := clip.RRect{Rect: image.Rect(0, 0, sz.X, sz.Y), SE: gtx.Dp(6), SW: gtx.Dp(6), NE: gtx.Dp(6), NW: gtx.Dp(6)}
			paint.FillShape(gtx.Ops, ColorCard, rr.Op(gtx.Ops))
			// Left accent bar
			bar := clip.Rect{Min: image.Pt(0, 0), Max: image.Pt(gtx.Dp(4), sz.Y)}.Op()
			paint.FillShape(gtx.Ops, accentColor, bar)
			return layout.Dimensions{Size: sz}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Game name + delete button row
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(th.Material, unit.Sp(15), listing.GameName)
									lbl.Font.Weight = font.Bold
									lbl.Color = accentColor
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !isOwn {
										return layout.Dimensions{}
									}
									btn := material.Button(th.Material, v.deleteBtns[listing.ID], "Delete")
									btn.Background = color.NRGBA{200, 50, 50, 255}
									btn.Color = color.NRGBA{255, 255, 255, 255}
									btn.CornerRadius = unit.Dp(3)
									btn.TextSize = unit.Sp(11)
									btn.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}
									return btn.Layout(gtx)
								}),
							)
						}),
						// Content
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(th.Material, unit.Sp(13), listing.Content)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							})
						}),
						// Footer: clickable author + DM button + expiry
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								// Clickable author name
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									authorName := "Unknown"
									if listing.Author != nil {
										authorName = v.app.ResolveUserName(listing.Author)
									}
									return v.authorBtns[listing.ID].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										if v.authorBtns[listing.ID].Hovered() {
											pointer.CursorPointer.Add(gtx.Ops)
										}
										nameColor := UserColor(authorName)
										if conn != nil {
											nameColor = v.app.GetUserRoleColor(conn, listing.UserID, authorName)
										}
										lbl := material.Label(th.Material, unit.Sp(11), authorName)
										lbl.Color = nameColor
										return lbl.Layout(gtx)
									})
								}),
								// Timestamp
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(th.Material, unit.Sp(11), " \u00b7 "+lfgTimeAgo(listing.CreatedAt))
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								// DM button (only for other users' listings)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if isOwn || listing.Author == nil {
										return layout.Dimensions{}
									}
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.dmBtns[listing.ID].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											if v.dmBtns[listing.ID].Hovered() {
												pointer.CursorPointer.Add(gtx.Ops)
											}
											clr := ColorAccent
											lbl := material.Label(th.Material, unit.Sp(11), "Message")
											lbl.Color = clr
											return lbl.Layout(gtx)
										})
									})
								}),
								// Expiry (right-aligned via Flexed)
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									remaining := time.Until(listing.ExpiresAt)
									var expText string
									if remaining <= 0 {
										expText = "Expired"
									} else {
										days := int(remaining.Hours() / 24)
										if days > 0 {
											expText = fmt.Sprintf("Expires in %dd", days)
										} else {
											hours := int(remaining.Hours())
											if hours > 0 {
												expText = fmt.Sprintf("Expires in %dh", hours)
											} else {
												expText = fmt.Sprintf("Expires in %dm", int(remaining.Minutes()))
											}
										}
									}
									lbl := material.Label(th.Material, unit.Sp(11), expText)
									lbl.Color = ColorTextDim
									lbl.Alignment = text.End
									return lbl.Layout(gtx)
								}),
							)
						}),
					)
				},
			)
		},
	)
}

func (v *LFGBoardView) doCreate() {
	gameName := strings.TrimSpace(v.gameNameEd.Text())
	content := strings.TrimSpace(v.contentEd.Text())
	if gameName == "" || content == "" {
		return
	}

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	chID := v.channelID
	go func() {
		listing, err := conn.Client.CreateLFGListing(chID, gameName, content)
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
			return
		}
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
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
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
func lfgEditorBox(gtx layout.Context, ed material.EditorStyle) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := clip.RRect{Rect: image.Rect(0, 0, sz.X, sz.Y), SE: gtx.Dp(4), SW: gtx.Dp(4), NE: gtx.Dp(4), NW: gtx.Dp(4)}
			paint.FillShape(gtx.Ops, ColorInput, rr.Op(gtx.Ops))
			return layout.Dimensions{Size: sz}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return ed.Layout(gtx)
				},
			)
		},
	)
}

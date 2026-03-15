package ui

import (
	"image"
	"log"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (v *MessageView) layoutSearchBar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconSearch, 16, ColorTextDim)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				ed := material.Editor(v.app.Theme.Material, &v.searchEditor, "Search (from:user has:image before:2025-01-01)")
				ed.Color = ColorText
				ed.HintColor = ColorTextDim
				return ed.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.searchLoading {
					lbl := material.Caption(v.app.Theme.Material, "Searching...")
					lbl.Color = ColorTextDim
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, lbl.Layout)
				}
				return layout.Dimensions{}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.searchCloseBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconClose, 16, ColorTextDim)
					})
				})
			}),
		)
	})
}

// layoutSearchResults renders search results instead of normal messages.
func (v *MessageView) layoutSearchResults(gtx layout.Context, userID string, myPerms int64, usernames map[string]bool, usernameToID map[string]string, myUsername string) layout.Dimensions {
	results := v.searchResults
	if len(results) == 0 && !v.searchLoading {
		return layoutCentered(gtx, v.app.Theme, "No results found", ColorTextDim)
	}
	if len(results) == 0 {
		return layout.Dimensions{}
	}

	if len(v.searchActions) < len(results) {
		v.searchActions = make([]msgAction, len(results)+20)
	}

	conn := v.app.Conn()

	// Count number of items (results + optional "Load more")
	hasMore := len(results) >= 50 && len(results) == v.searchOffset
	itemCount := len(results)
	if hasMore {
		itemCount++
	}

	return material.List(v.app.Theme.Material, &v.searchList).Layout(gtx, itemCount, func(gtx layout.Context, i int) layout.Dimensions {
		// "Load more" button na konci
		if i >= len(results) {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					btn := material.Button(v.app.Theme.Material, &v.searchMore, "Load more")
					btn.Background = ColorSidebar
					btn.Color = ColorText
					return btn.Layout(gtx)
				})
			})
		}

		msg := results[i]
		act := &v.searchActions[i]

		// Click on name/avatar → UserPopup
		if act.nameBtn.Clicked(gtx) || act.avatarBtn.Clicked(gtx) {
			if msg.Author != nil {
				v.app.UserPopup.Show(msg.Author.ID, msg.Author.Username)
			}
		}

		// Date/time and author
		authorName := "Unknown"
		authorColor := ColorText
		if msg.Author != nil {
			authorName = v.app.ResolveUserName(msg.Author)
			if conn := v.app.Conn(); conn != nil {
				authorColor = v.app.GetUserRoleColor(conn, msg.Author.ID, authorName)
			} else {
				authorColor = UserColor(authorName)
			}
		}
		timeStr := FormatDateTime(msg.CreatedAt)

		// Channel info
		channelLabel := ""
		if conn != nil {
			for _, ch := range conn.Channels {
				if ch.ID == msg.ChannelID {
					channelLabel = "#" + ch.Name
					break
				}
			}
		}

		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return act.nameBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, authorName)
								lbl.Color = authorColor
								lbl.Font.Weight = 700
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, timeStr)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if channelLabel == "" {
								return layout.Dimensions{}
							}
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, channelLabel)
								lbl.Color = ColorAccent
								return lbl.Layout(gtx)
							})
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(v.app.Theme.Material, msg.Content)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					})
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
						paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
						return layout.Dimensions{Size: size}
					})
				}),
			)
		})
	})
}

// doSearch performs a search on the server.
func (v *MessageView) doSearch(query string, offset int, more bool) {
	conn := v.app.Conn()
	if conn == nil {
		v.searchLoading = false
		return
	}
	msgs, err := conn.Client.SearchMessages(conn.ActiveChannelID, query, 50, offset)
	if err != nil {
		log.Printf("Search error: %v", err)
		v.searchLoading = false
		v.app.Window.Invalidate()
		return
	}
	if more {
		v.searchResults = append(v.searchResults, msgs...)
	} else {
		v.searchResults = msgs
	}
	v.searchOffset = offset + len(msgs)
	if len(v.searchActions) < len(v.searchResults) {
		v.searchActions = make([]msgAction, len(v.searchResults)+20)
	}
	v.searchLoading = false
	v.app.Window.Invalidate()
}


package ui

import (
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"nora-client/api"
)

func (v *MessageView) updateMentionState() {
	text := v.editor.Text()
	// Find last @ that's either at start or after a space
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		v.showMentions = false
		return
	}
	// @ must be at start or preceded by space
	if lastAt > 0 && text[lastAt-1] != ' ' {
		v.showMentions = false
		return
	}
	after := text[lastAt+1:]
	// No space after @ means still typing mention
	if strings.Contains(after, " ") {
		v.showMentions = false
		return
	}
	newQuery := strings.ToLower(after)
	if newQuery != v.mentionQuery {
		v.mentionSelIdx = 0
	}
	v.mentionQuery = newQuery
	v.showMentions = true
}

func (v *MessageView) getMentionCandidates() []api.User {
	conn := v.app.Conn()
	if conn == nil {
		return nil
	}
	v.app.mu.RLock()
	users := conn.Users
	v.app.mu.RUnlock()

	var result []api.User
	for _, u := range users {
		name := strings.ToLower(u.Username)
		dn := strings.ToLower(u.DisplayName)
		if v.mentionQuery == "" || strings.Contains(name, v.mentionQuery) || strings.Contains(dn, v.mentionQuery) {
			result = append(result, u)
			if len(result) >= 8 {
				break
			}
		}
	}
	return result
}

func (v *MessageView) insertMention(username string) {
	text := v.editor.Text()
	lastAt := strings.LastIndex(text, "@")
	if lastAt >= 0 {
		newText := text[:lastAt] + "@" + username + " "
		v.editor.SetText(newText)
		v.editor.SetCaret(len(newText), len(newText))
	}
	v.showMentions = false
}

func (v *MessageView) layoutMentionPopup(gtx layout.Context) layout.Dimensions {
	members := v.getMentionCandidates()
	if len(members) == 0 {
		v.showMentions = false
		return layout.Dimensions{}
	}
	// Clamp selection index
	if v.mentionSelIdx >= len(members) {
		v.mentionSelIdx = len(members) - 1
	}

	return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				var items []layout.FlexChild
				for i, u := range members {
					i := i
					user := u
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := &v.mentionBtns[i]
						bg := ColorCard
						if btn.Hovered() || i == v.mentionSelIdx {
							bg = ColorHover
						}
						return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
									paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
									return layout.Dimensions{Size: bounds.Max}
								},
								func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										name := v.app.ResolveUserName(&user)
										lbl := material.Body2(v.app.Theme.Material, "@"+name)
										if conn := v.app.Conn(); conn != nil {
											lbl.Color = v.app.GetUserRoleColor(conn, user.ID, name)
										} else {
											lbl.Color = UserColor(name)
										}
										return lbl.Layout(gtx)
									})
								},
							)
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			},
		)
	})
}

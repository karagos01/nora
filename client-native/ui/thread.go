package ui

import (
	"image"
	"log"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type ThreadView struct {
	app *App

	Visible  bool
	ParentID string
	Messages []api.Message // [0]=parent, [1..]=replies

	list     widget.List
	editor   widget.Editor
	sendBtn  widget.Clickable
	closeBtn widget.Clickable
	actions  []msgAction

	loading bool
}

func NewThreadView(a *App) *ThreadView {
	v := &ThreadView{app: a}
	v.list.Axis = layout.Vertical
	v.list.ScrollToEnd = true
	v.editor.Submit = true
	return v
}

// Open otevře thread pro danou zprávu.
func (v *ThreadView) Open(parentID string) {
	v.ParentID = parentID
	v.Visible = true
	v.Messages = nil
	v.actions = nil
	v.loading = true
	v.editor.SetText("")

	go func() {
		conn := v.app.Conn()
		if conn == nil {
			return
		}
		msgs, err := conn.Client.GetMessageThread(parentID)
		if err != nil {
			log.Printf("thread: failed to load: %v", err)
			v.loading = false
			v.app.Window.Invalidate()
			return
		}
		v.Messages = msgs
		v.actions = make([]msgAction, len(msgs)+10)
		v.loading = false
		v.app.Window.Invalidate()
	}()
}

// Close zavře thread view.
func (v *ThreadView) Close() {
	v.Visible = false
	v.ParentID = ""
	v.Messages = nil
	v.actions = nil
}

// AddReply přidá novou reply do threadu (z WS eventu).
func (v *ThreadView) AddReply(msg api.Message) {
	v.Messages = append(v.Messages, msg)
	if len(v.actions) < len(v.Messages) {
		v.actions = make([]msgAction, len(v.Messages)+10)
	}
}

func (v *ThreadView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Handle close button
	if v.closeBtn.Clicked(gtx) {
		v.Close()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Handle send
	for {
		ev, ok := v.editor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			v.sendReply()
		}
	}
	if v.sendBtn.Clicked(gtx) {
		v.sendReply()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutHeader(gtx)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Messages (parent + replies)
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if v.loading {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, "Loading...")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}
			if len(v.Messages) == 0 {
				return layout.Dimensions{}
			}
			return v.layoutMessages(gtx)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Composer
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutComposer(gtx)
		}),
	)
}

func (v *ThreadView) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, "Thread")
					lbl.Color = ColorText
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(v.app.Theme.Material, "X")
						lbl.Color = ColorTextDim
						if v.closeBtn.Hovered() {
							lbl.Color = ColorText
						}
						return layout.UniformInset(unit.Dp(4)).Layout(gtx, lbl.Layout)
					})
				}),
			)
		},
	)
}

func (v *ThreadView) layoutMessages(gtx layout.Context) layout.Dimensions {
	// Zajistit dostatek action slotů
	if len(v.actions) < len(v.Messages) {
		v.actions = make([]msgAction, len(v.Messages)+10)
	}

	return material.List(v.app.Theme.Material, &v.list).Layout(gtx, len(v.Messages), func(gtx layout.Context, idx int) layout.Dimensions {
		msg := v.Messages[idx]
		isParent := idx == 0

		username := v.app.ResolveUserName(msg.Author)
		avatarURL := ""
		if msg.Author != nil {
			avatarURL = msg.Author.AvatarURL
		}

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Parent divider (po parent zprávě)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !isParent {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				})
			}),
			// Message row
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{}.Layout(gtx,
							// Avatar (28px)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layoutAvatar(gtx, v.app, username, avatarURL, 28)
							}),
							// Content
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										// Name + time
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													nameColor := UserColor(username)
													if conn := v.app.Conn(); conn != nil {
														nameColor = v.app.GetUserRoleColor(conn, msg.UserID, username)
													}
													lbl := material.Caption(v.app.Theme.Material, username)
													lbl.Color = nameColor
													return lbl.Layout(gtx)
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Caption(v.app.Theme.Material, FormatTime(msg.CreatedAt))
														lbl.Color = ColorTextDim
														return lbl.Layout(gtx)
													})
												}),
											)
										}),
										// Content
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											conn := v.app.Conn()
											var emojis []api.CustomEmoji
											usernames := make(map[string]bool)
											usernameToID := make(map[string]string)
											if conn != nil {
												v.app.mu.RLock()
												emojis = conn.Emojis
												for _, u := range conn.Members {
													name := v.app.ResolveUserName(&u)
													usernames[name] = true
													usernameToID[name] = u.ID
												}
												v.app.mu.RUnlock()
											}
											serverURL := ""
											if conn != nil {
												serverURL = conn.URL
											}
											return layoutMessageContent(gtx, v.app.Theme, msg.Content, emojis, &v.actions[idx].links, &v.actions[idx].mentions, usernameToID, usernames, nil, v.app, serverURL)
										}),
										// Attachments
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if len(msg.Attachments) == 0 {
												return layout.Dimensions{}
											}
											return v.layoutAttachments(gtx, msg.Attachments)
										}),
									)
								})
							}),
						)
					},
				)
			}),
		)
	})
}

func (v *ThreadView) layoutAttachments(gtx layout.Context, atts []api.Attachment) layout.Dimensions {
	var items []layout.FlexChild
	for _, att := range atts {
		a := att
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, a.Filename)
				lbl.Color = ColorAccent
				return lbl.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *ThreadView) layoutComposer(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							ed := material.Editor(v.app.Theme.Material, &v.editor, "Reply in thread...")
							ed.Color = ColorText
							ed.HintColor = ColorTextDim
							return ed.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.sendBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, "Send")
									btnColor := ColorAccent
									if v.sendBtn.Hovered() {
										btnColor = ColorAccentHover
									}
									lbl.Color = btnColor
									return lbl.Layout(gtx)
								})
							})
						}),
					)
				})
			},
		)
	})
}

func (v *ThreadView) sendReply() {
	text := strings.TrimSpace(v.editor.Text())
	if text == "" || v.ParentID == "" {
		return
	}
	v.editor.SetText("")

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	channelID := ""
	if len(v.Messages) > 0 {
		channelID = v.Messages[0].ChannelID
	}
	if channelID == "" {
		return
	}

	parentID := v.ParentID
	go func() {
		_, err := conn.Client.SendMessage(channelID, text, parentID)
		if err != nil {
			log.Printf("thread: failed to send reply: %v", err)
		}
	}()
}

// handleLinkClicks zpracuje kliknutí na linky v thread zprávách.
func (v *ThreadView) handleLinkClicks(gtx layout.Context) {
	for i := range v.actions {
		if i >= len(v.Messages) {
			break
		}
		for j := 0; j < v.actions[i].links.N && j < 4; j++ {
			if v.actions[i].links.Btns[j].Clicked(gtx) {
				openURL(v.actions[i].links.URLs[j])
			}
		}
	}
}


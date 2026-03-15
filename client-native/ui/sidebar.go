package ui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/store"
)

type SidebarView struct {
	app            *App
	homeBtn        widget.Clickable
	notifBtn       widget.Clickable
	addBtn         widget.Clickable
	serverBtns     []widget.Clickable
	serverRightTags []bool // pointer event tags for right-click
}

func NewSidebarView(a *App) *SidebarView {
	return &SidebarView{app: a}
}

func (v *SidebarView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorSidebar, clip.Rect{Max: gtx.Constraints.Max}.Op())

	v.app.mu.RLock()
	serverCount := len(v.app.Servers)
	mode := v.app.Mode
	v.app.mu.RUnlock()

	if len(v.serverBtns) < serverCount {
		v.serverBtns = make([]widget.Clickable, serverCount+5)
	}
	if len(v.serverRightTags) < serverCount {
		v.serverRightTags = make([]bool, serverCount+5)
	}

	// Process right-click events on servers
	for i := 0; i < serverCount; i++ {
		for {
			ev, ok := gtx.Event(pointer.Filter{
				Target: &v.serverRightTags[i],
				Kinds:  pointer.Press,
			})
			if !ok {
				break
			}
			if pe, ok := ev.(pointer.Event); ok && pe.Buttons == pointer.ButtonSecondary {
				idx := i
				v.showServerNotifyMenu(idx, v.app.CursorX, v.app.CursorY)
			}
		}
	}

	var children []layout.FlexChild

	// Home button (DMs)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			isActive := mode == ViewHome || mode == ViewDM || mode == ViewGroup
			hasUnreadDM := false
			v.app.mu.RLock()
			for _, srv := range v.app.Servers {
				for _, c := range srv.UnreadDMCount {
					if c > 0 {
						hasUnreadDM = true
						break
					}
				}
				if hasUnreadDM {
					break
				}
				for _, u := range srv.UnreadGroups {
					if u {
						hasUnreadDM = true
						break
					}
				}
				if hasUnreadDM {
					break
				}
			}
			v.app.mu.RUnlock()
			return v.layoutIconBtnWithBadgeIcon(gtx, &v.homeBtn, IconChat, isActive, hasUnreadDM, func() {
				v.app.SwitchToHome()
			})
		})
	}))

	// Notifications button (bell)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			isActive := mode == ViewNotifications
			return v.layoutIconBtnWithBadgeIcon(gtx, &v.notifBtn, IconNotifications, isActive, v.app.NotifCenter.totalUnread() > 0, func() {
				v.app.mu.Lock()
				v.app.Mode = ViewNotifications
				v.app.mu.Unlock()
			})
		})
	}))

	// Divider
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X-gtx.Dp(24), gtx.Dp(2))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	}))

	// Connected servers
	for i := 0; i < serverCount; i++ {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				v.app.mu.RLock()
				name := v.app.Servers[i].Name
				icon := v.app.Servers[i].Icon
				activeIdx := v.app.ActiveServer
				hasUnread := false
				for _, c := range v.app.Servers[i].UnreadCount {
					if c > 0 {
						hasUnread = true
						break
					}
				}
				v.app.mu.RUnlock()

				isActive := mode == ViewChannels && activeIdx == i

				var dims layout.Dimensions
				if icon != nil {
					dims = v.layoutIconImgBtnWithBadge(gtx, &v.serverBtns[i], icon, isActive, hasUnread, func() {
						v.app.SwitchToServer(i)
					})
				} else {
					initial := "S"
					if len(name) > 0 {
						runes := []rune(name)
						initial = string(runes[0])
					}
					dims = v.layoutIconBtnWithBadge(gtx, &v.serverBtns[i], initial, isActive, hasUnread, func() {
						v.app.SwitchToServer(i)
					})
				}

				// Right-click area for notification menu (PassOp passes clicks down to Clickable)
				areaStack := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
				pr := pointer.PassOp{}.Push(gtx.Ops)
				event.Op(gtx.Ops, &v.serverRightTags[i])
				pr.Pop()
				areaStack.Pop()

				return dims
			})
		}))
	}

	// "+" button to add server
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutAddBtn(gtx)
		})
	}))

	// Spacer
	children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(0, 0)}
	}))

	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx, children...)
}

func (v *SidebarView) layoutIconBtn(gtx layout.Context, btn *widget.Clickable, text string, active bool, onClick func()) layout.Dimensions {
	if btn.Clicked(gtx) {
		onClick()
	}

	size := gtx.Dp(48)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		bg := ColorCard
		if active {
			bg = ColorAccent
		} else if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = ColorHover
		}

		rr := size / 4
		if active {
			rr = size / 6
		}

		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(v.app.Theme.Material, text)
			lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return lbl.Layout(gtx)
		})
	})
}

func (v *SidebarView) layoutIconBtnWithBadge(gtx layout.Context, btn *widget.Clickable, text string, active, hasUnread bool, onClick func()) layout.Dimensions {
	if !hasUnread {
		return v.layoutIconBtn(gtx, btn, text, active, onClick)
	}

	return layout.Stack{Alignment: layout.NE}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return v.layoutIconBtn(gtx, btn, text, active, onClick)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Dp(10)
			paint.FillShape(gtx.Ops, ColorDanger, clip.RRect{
				Rect: image.Rect(0, 0, sz, sz),
				NE:   sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: image.Pt(sz, sz)}
		}),
	)
}

func (v *SidebarView) layoutIconImgBtn(gtx layout.Context, btn *widget.Clickable, img image.Image, active bool, onClick func()) layout.Dimensions {
	if btn.Clicked(gtx) {
		onClick()
	}

	size := gtx.Dp(48)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		rr := size / 4
		if active {
			rr = size / 6
		}

		defer clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Push(gtx.Ops).Pop()

		imgBounds := img.Bounds()
		imgW := float32(imgBounds.Dx())
		imgH := float32(imgBounds.Dy())
		scaleX := float32(size) / imgW
		scaleY := float32(size) / imgH

		defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()

		imgOp := paint.NewImageOp(img)
		imgOp.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)

		return layout.Dimensions{Size: image.Pt(size, size)}
	})
}

func (v *SidebarView) layoutIconImgBtnWithBadge(gtx layout.Context, btn *widget.Clickable, img image.Image, active, hasUnread bool, onClick func()) layout.Dimensions {
	if !hasUnread {
		return v.layoutIconImgBtn(gtx, btn, img, active, onClick)
	}

	return layout.Stack{Alignment: layout.NE}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return v.layoutIconImgBtn(gtx, btn, img, active, onClick)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Dp(10)
			paint.FillShape(gtx.Ops, ColorDanger, clip.RRect{
				Rect: image.Rect(0, 0, sz, sz),
				NE:   sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: image.Pt(sz, sz)}
		}),
	)
}

func (v *SidebarView) layoutIconBtnIcon(gtx layout.Context, btn *widget.Clickable, icon *NIcon, active bool, onClick func()) layout.Dimensions {
	if btn.Clicked(gtx) {
		onClick()
	}

	size := gtx.Dp(48)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		bg := ColorCard
		if active {
			bg = ColorAccent
		} else if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = ColorHover
		}

		rr := size / 4
		if active {
			rr = size / 6
		}

		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layoutIcon(gtx, icon, 28, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		})
	})
}

func (v *SidebarView) layoutIconBtnWithBadgeIcon(gtx layout.Context, btn *widget.Clickable, icon *NIcon, active, hasUnread bool, onClick func()) layout.Dimensions {
	if !hasUnread {
		return v.layoutIconBtnIcon(gtx, btn, icon, active, onClick)
	}

	return layout.Stack{Alignment: layout.NE}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return v.layoutIconBtnIcon(gtx, btn, icon, active, onClick)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Dp(10)
			paint.FillShape(gtx.Ops, ColorDanger, clip.RRect{
				Rect: image.Rect(0, 0, sz, sz),
				NE:   sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: image.Pt(sz, sz)}
		}),
	)
}

func (v *SidebarView) layoutAddBtn(gtx layout.Context) layout.Dimensions {
	if v.addBtn.Clicked(gtx) {
		v.app.AddServerView.Reset()
		v.app.ShowAddServer = true
	}

	size := gtx.Dp(48)
	return v.addBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		rr := size / 2
		bg := color.NRGBA{R: 40, G: 120, B: 60, A: 255}
		if v.addBtn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = color.NRGBA{R: 50, G: 150, B: 75, A: 255}
		}
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layoutIcon(gtx, IconAdd, 24, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		})
	})
}

func (v *SidebarView) showServerNotifyMenu(serverIdx, x, y int) {
	a := v.app
	a.mu.RLock()
	if serverIdx < 0 || serverIdx >= len(a.Servers) {
		a.mu.RUnlock()
		return
	}
	conn := a.Servers[serverIdx]
	currentLevel := conn.NotifyLevel
	serverURL := conn.URL
	a.mu.RUnlock()

	allSelected := currentLevel != nil && *currentLevel == store.NotifyAll
	mentionsSelected := currentLevel != nil && *currentLevel == store.NotifyMentions
	mutedSelected := currentLevel != nil && *currentLevel == store.NotifyNothing
	defaultSelected := currentLevel == nil

	items := []ContextMenuItem{
		{Label: "Notifications", Children: []ContextMenuItem{
			{Label: "All messages", Selected: allSelected, Action: func() {
				lvl := store.NotifyAll
				a.mu.Lock()
				conn.NotifyLevel = &lvl
				a.mu.Unlock()
				go store.UpdateServerNotifyLevel(a.PublicKey, serverURL, &lvl)
			}},
			{Label: "Only @mentions", Selected: mentionsSelected, Action: func() {
				lvl := store.NotifyMentions
				a.mu.Lock()
				conn.NotifyLevel = &lvl
				a.mu.Unlock()
				go store.UpdateServerNotifyLevel(a.PublicKey, serverURL, &lvl)
			}},
			{Label: "Muted", Selected: mutedSelected, Action: func() {
				lvl := store.NotifyNothing
				a.mu.Lock()
				conn.NotifyLevel = &lvl
				a.mu.Unlock()
				go store.UpdateServerNotifyLevel(a.PublicKey, serverURL, &lvl)
			}},
			{IsSep: true},
			{Label: "Use global default", Selected: defaultSelected, Action: func() {
				a.mu.Lock()
				conn.NotifyLevel = nil
				a.mu.Unlock()
				go store.UpdateServerNotifyLevel(a.PublicKey, serverURL, nil)
			}},
		}},
	}

	a.ContextMenu.Show(x, y, "Server Settings", items)
}

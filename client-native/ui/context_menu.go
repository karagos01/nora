package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type ContextMenuItem struct {
	Label    string
	Selected bool
	Action   func()
	IsSep    bool
	Children []ContextMenuItem // submenu položky (rozbalí se klikem)
}

type ContextMenu struct {
	app       *App
	Visible   bool
	Title     string
	posX      int
	posY      int
	Items     []ContextMenuItem
	itemBtns  []widget.Clickable
	childBtns []widget.Clickable // sdílený pool pro children
	expanded  map[int]bool       // které top-level položky jsou rozbalené
	bgBtn     widget.Clickable
}

func NewContextMenu(a *App) *ContextMenu {
	return &ContextMenu{app: a, expanded: make(map[int]bool)}
}

func (m *ContextMenu) Show(x, y int, title string, items []ContextMenuItem) {
	m.Visible = true
	m.Title = title
	m.posX = x
	m.posY = y
	m.Items = items
	m.expanded = make(map[int]bool)

	totalItems := len(items)
	totalChildren := 0
	for _, item := range items {
		totalChildren += len(item.Children)
	}
	if len(m.itemBtns) < totalItems {
		m.itemBtns = make([]widget.Clickable, totalItems+5)
	}
	if len(m.childBtns) < totalChildren {
		m.childBtns = make([]widget.Clickable, totalChildren+10)
	}
}

func (m *ContextMenu) Hide() {
	m.Visible = false
	m.Items = nil
	m.expanded = make(map[int]bool)
}

func (m *ContextMenu) Layout(gtx layout.Context) layout.Dimensions {
	if !m.Visible {
		return layout.Dimensions{}
	}

	// Zpracovat kliky na items PŘED bgBtn
	itemClicked := false
	childIdx := 0
	for i, item := range m.Items {
		if item.IsSep {
			continue
		}
		if i < len(m.itemBtns) && m.itemBtns[i].Clicked(gtx) {
			itemClicked = true
			if len(item.Children) > 0 {
				m.expanded[i] = !m.expanded[i]
			} else if item.Action != nil {
				item.Action()
			}
		}
		if m.expanded[i] {
			for j, child := range item.Children {
				if !child.IsSep && childIdx < len(m.childBtns) && m.childBtns[childIdx].Clicked(gtx) {
					itemClicked = true
					if child.Action != nil {
						child.Action()
					}
					// Aktualizovat Selected flagy — kliknutý = true, ostatní = false
					for k := range m.Items[i].Children {
						m.Items[i].Children[k].Selected = (k == j)
					}
				}
				childIdx++
			}
		} else {
			childIdx += len(item.Children)
		}
	}

	// Klik na pozadí zavře menu
	if m.bgBtn.Clicked(gtx) && !itemClicked {
		m.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	menuWidth := gtx.Dp(200)
	itemHeight := gtx.Dp(32)
	headerHeight := gtx.Dp(28)
	sepHeight := gtx.Dp(9)

	// Spočítat výšku
	totalHeight := 0
	if m.Title != "" {
		totalHeight += headerHeight + sepHeight
	}
	for i, item := range m.Items {
		if item.IsSep {
			totalHeight += sepHeight
		} else {
			totalHeight += itemHeight
		}
		if m.expanded[i] {
			for _, child := range item.Children {
				if child.IsSep {
					totalHeight += sepHeight
				} else {
					totalHeight += itemHeight
				}
			}
		}
	}

	return m.bgBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = gtx.Constraints.Max

		paint.FillShape(gtx.Ops, color.NRGBA{A: 60}, clip.Rect{Max: gtx.Constraints.Max}.Op())

		x := m.posX
		y := m.posY
		if x+menuWidth > gtx.Constraints.Max.X {
			x = gtx.Constraints.Max.X - menuWidth
		}
		if y+totalHeight > gtx.Constraints.Max.Y {
			y = gtx.Constraints.Max.Y - totalHeight
		}
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}

		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()

		rr := gtx.Dp(6)
		cardRect := image.Rect(0, 0, menuWidth, totalHeight)
		paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
			Rect: cardRect,
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		borderColor := color.NRGBA{R: 60, G: 60, B: 60, A: 255}
		paint.FillShape(gtx.Ops, borderColor, clip.Stroke{
			Path:  clip.RRect{Rect: cardRect, NE: rr, NW: rr, SE: rr, SW: rr}.Path(gtx.Ops),
			Width: float32(gtx.Dp(1)),
		}.Op())

		yOff := 0

		// Title header
		if m.Title != "" {
			func() {
				defer op.Offset(image.Pt(0, yOff)).Push(gtx.Ops).Pop()
				hGtx := gtx
				hGtx.Constraints.Min = image.Pt(menuWidth, headerHeight)
				hGtx.Constraints.Max = hGtx.Constraints.Min
				layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(hGtx, func(gtx layout.Context) layout.Dimensions {
					return layout.W.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(m.app.Theme.Material, m.Title)
						lbl.Color = ColorTextDim
						lbl.Font.Weight = 600
						return lbl.Layout(gtx)
					})
				})
			}()
			yOff += headerHeight

			func() {
				defer op.Offset(image.Pt(0, yOff)).Push(gtx.Ops).Pop()
				lineY := gtx.Dp(4)
				defer op.Offset(image.Pt(gtx.Dp(8), lineY)).Push(gtx.Ops).Pop()
				lineSize := image.Pt(menuWidth-gtx.Dp(16), gtx.Dp(1))
				paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: lineSize}.Op())
			}()
			yOff += sepHeight
		}

		childIdx := 0
		for i, item := range m.Items {
			itemY := yOff
			if item.IsSep {
				func() {
					defer op.Offset(image.Pt(0, itemY)).Push(gtx.Ops).Pop()
					lineY := gtx.Dp(4)
					defer op.Offset(image.Pt(gtx.Dp(8), lineY)).Push(gtx.Ops).Pop()
					lineSize := image.Pt(menuWidth-gtx.Dp(16), gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: lineSize}.Op())
				}()
				yOff += sepHeight
				continue
			}

			hasChildren := len(item.Children) > 0
			isExpanded := m.expanded[i]

			func() {
				defer op.Offset(image.Pt(0, itemY)).Push(gtx.Ops).Pop()
				m.layoutMenuItem(gtx, &m.itemBtns[i], item.Label, item.Selected, hasChildren, isExpanded, menuWidth, itemHeight, unit.Dp(10))
			}()
			yOff += itemHeight

			if isExpanded {
				for _, child := range item.Children {
					cY := yOff
					if child.IsSep {
						func() {
							defer op.Offset(image.Pt(0, cY)).Push(gtx.Ops).Pop()
							lineY := gtx.Dp(4)
							defer op.Offset(image.Pt(gtx.Dp(16), lineY)).Push(gtx.Ops).Pop()
							lineSize := image.Pt(menuWidth-gtx.Dp(24), gtx.Dp(1))
							paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: lineSize}.Op())
						}()
						yOff += sepHeight
						childIdx++
						continue
					}

					func() {
						defer op.Offset(image.Pt(0, cY)).Push(gtx.Ops).Pop()
						m.layoutMenuItem(gtx, &m.childBtns[childIdx], child.Label, child.Selected, false, false, menuWidth, itemHeight, unit.Dp(22))
					}()
					yOff += itemHeight
					childIdx++
				}
			} else {
				childIdx += len(item.Children)
			}
		}

		return layout.Dimensions{Size: gtx.Constraints.Max}
	})
}

// layoutMenuItem renderuje jednu řádku menu — ikona + text, vertikálně vycentrované.
func (m *ContextMenu) layoutMenuItem(gtx layout.Context, btn *widget.Clickable, label string, selected, hasChildren, isExpanded bool, w, h int, leftInset unit.Dp) {
	itemGtx := gtx
	itemGtx.Constraints.Min = image.Pt(w, h)
	itemGtx.Constraints.Max = itemGtx.Constraints.Min

	btn.Layout(itemGtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
			bg = ColorHover
		}
		if bg.A > 0 {
			paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		}

		return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: leftInset, Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(16)
					if hasChildren {
						arrow := IconChevronRight
						if isExpanded {
							arrow = IconExpand
						}
						return layoutIcon(gtx, arrow, 14, ColorTextDim)
					}
					if selected {
						return layoutIcon(gtx, IconCheck, 14, ColorAccent)
					}
					return layout.Dimensions{Size: image.Pt(sz, sz)}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(m.app.Theme.Material, label)
						lbl.Color = ColorText
						if hasChildren {
							lbl.Font.Weight = 600
						}
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	})
}

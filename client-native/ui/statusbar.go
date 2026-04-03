package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// StatusItem represents a single progress task shown in the status bar.
type StatusItem struct {
	ID       string
	Label    string  // e.g. "Downloading photos"
	Detail   string  // e.g. "3 / 12 files"
	Progress float64 // 0.0 – 1.0
	Icon     *NIcon
	OnCancel func() // called when user confirms cancel; nil = no cancel button
}

// StatusBar shows active background operations above the user panel.
type StatusBar struct {
	mu         sync.Mutex
	items      []StatusItem
	cancelBtns []widget.Clickable
}

func NewStatusBar() *StatusBar {
	return &StatusBar{}
}

// Set adds or updates a status item by ID.
func (sb *StatusBar) Set(item StatusItem) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	for i, it := range sb.items {
		if it.ID == item.ID {
			sb.items[i] = item
			return
		}
	}
	sb.items = append(sb.items, item)
	sb.cancelBtns = append(sb.cancelBtns, widget.Clickable{})
}

// Remove removes a status item by ID.
func (sb *StatusBar) Remove(id string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	for i, it := range sb.items {
		if it.ID == id {
			sb.items = append(sb.items[:i], sb.items[i+1:]...)
			sb.cancelBtns = append(sb.cancelBtns[:i], sb.cancelBtns[i+1:]...)
			return
		}
	}
}

// Items returns a snapshot of current items.
func (sb *StatusBar) Items() []StatusItem {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	out := make([]StatusItem, len(sb.items))
	copy(out, sb.items)
	return out
}

// Layout renders the status bar. Returns zero dimensions when no items are active.
func (sb *StatusBar) Layout(gtx layout.Context, th *Theme) layout.Dimensions {
	sb.mu.Lock()
	items := make([]StatusItem, len(sb.items))
	copy(items, sb.items)
	// Process cancel clicks under lock
	for i := range sb.cancelBtns {
		if i < len(items) && sb.cancelBtns[i].Clicked(gtx) && items[i].OnCancel != nil {
			fn := items[i].OnCancel
			sb.mu.Unlock()
			fn()
			return layout.Dimensions{}
		}
	}
	sb.mu.Unlock()

	if len(items) == 0 {
		return layout.Dimensions{}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Items
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				list := layout.List{Axis: layout.Vertical}
				return list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
					sb.mu.Lock()
					var btn *widget.Clickable
					if i < len(sb.cancelBtns) {
						btn = &sb.cancelBtns[i]
					}
					sb.mu.Unlock()
					return sb.layoutItem(gtx, th, items[i], btn)
				})
			})
		}),
	)
}

func (sb *StatusBar) layoutItem(gtx layout.Context, th *Theme, item StatusItem, cancelBtn *widget.Clickable) layout.Dimensions {
	ringSize := gtx.Dp(24)
	ringWidth := float32(gtx.Dp(3))

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		// Progress ring + icon
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(ringSize, ringSize)
			gtx.Constraints.Min = sz
			gtx.Constraints.Max = sz

			cx := float32(ringSize) / 2
			cy := float32(ringSize) / 2
			r := cx - ringWidth/2

			// Background ring (dim)
			drawArc(gtx.Ops, cx, cy, r, ringWidth, 0, 2*math.Pi, color.NRGBA{R: 255, G: 255, B: 255, A: 30})

			// Progress arc
			if item.Progress > 0 {
				angle := item.Progress * 2 * math.Pi
				drawArc(gtx.Ops, cx, cy, r, ringWidth, -math.Pi/2, -math.Pi/2+angle, ColorAccent)
			}

			// Icon in center
			if item.Icon != nil {
				iconSz := gtx.Dp(14)
				off := (ringSize - iconSz) / 2
				defer op.Offset(image.Pt(off, off)).Push(gtx.Ops).Pop()
				layoutIcon(gtx, item.Icon, 14, ColorTextDim)
			}

			return layout.Dimensions{Size: sz}
		}),
		// Label + detail
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				pct := int(item.Progress * 100)
				text := item.Label
				if item.Detail != "" {
					text = fmt.Sprintf("%s — %s (%d%%)", item.Label, item.Detail, pct)
				} else {
					text = fmt.Sprintf("%s (%d%%)", item.Label, pct)
				}
				lbl := material.Caption(th.Material, text)
				lbl.Color = ColorTextDim
				lbl.MaxLines = 1
				return lbl.Layout(gtx)
			})
		}),
		// Cancel button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if item.OnCancel == nil || cancelBtn == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconClose, 16, ColorDanger)
				})
			})
		}),
	)
}

// drawArc draws a stroked arc segment.
func drawArc(ops *op.Ops, cx, cy, radius, width float32, startAngle, endAngle float64, clr color.NRGBA) {
	steps := 32
	span := endAngle - startAngle
	if span <= 0 {
		return
	}

	var p clip.Path
	p.Begin(ops)

	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		a := startAngle + t*span
		x := cx + radius*float32(math.Cos(a))
		y := cy + radius*float32(math.Sin(a))
		if i == 0 {
			p.MoveTo(f32.Pt(x, y))
		} else {
			p.LineTo(f32.Pt(x, y))
		}
	}

	paint.FillShape(ops, clr, clip.Stroke{
		Path:  p.End(),
		Width: width,
	}.Op())
}

// layoutStatusBar renders the status bar above the user panel.
func (a *App) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	if a.StatusBar == nil {
		return layout.Dimensions{}
	}
	return a.StatusBar.Layout(gtx, a.Theme)
}

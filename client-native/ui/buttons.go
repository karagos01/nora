package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Standard button styles for consistent UI across the app.
// Use these instead of ad-hoc button layouts.

const (
	btnRadiusStd  = 6 // standard border radius
	btnRadiusRound = 12 // rounded (sidebar)
)

// layoutActionBtn renders a standard action button (icon + text, colored background).
// Use for primary actions like "New Post", "Create", etc.
func layoutActionBtn(gtx layout.Context, th *Theme, btn *widget.Clickable, label string, bg color.NRGBA) layout.Dimensions {
	b := material.Button(th.Material, btn, label)
	b.Background = bg
	b.Color = ColorWhite
	b.CornerRadius = unit.Dp(btnRadiusStd)
	b.TextSize = th.Sp(13)
	b.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(14), Right: unit.Dp(14)}
	return b.Layout(gtx)
}

// layoutDangerBtn renders a danger action button (red background).
func layoutDangerBtn(gtx layout.Context, th *Theme, btn *widget.Clickable, label string) layout.Dimensions {
	return layoutActionBtn(gtx, th, btn, label, ColorDanger)
}

// layoutSecondaryBtn renders a secondary button (muted background).
func layoutSecondaryBtn(gtx layout.Context, th *Theme, btn *widget.Clickable, label string) layout.Dimensions {
	return layoutActionBtn(gtx, th, btn, label, ColorInput)
}

// layoutSmallActionBtn renders a compact action button (smaller padding).
func layoutSmallActionBtn(gtx layout.Context, th *Theme, btn *widget.Clickable, label string, bg color.NRGBA) layout.Dimensions {
	b := material.Button(th.Material, btn, label)
	b.Background = bg
	b.Color = ColorWhite
	b.CornerRadius = unit.Dp(4)
	b.TextSize = th.Sp(12)
	b.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}
	return b.Layout(gtx)
}

// layoutIconTextBtn renders a nav-style button with icon + label and background.
// Use for sidebar nav items (Files, Kanban, Calendar, etc.)
func layoutIconTextBtn(gtx layout.Context, th *Theme, btn *widget.Clickable, icon *NIcon, label string, active bool) layout.Dimensions {
	bg := ColorCard
	iconClr := ColorTextDim
	textClr := ColorTextDim
	if active {
		bg = ColorSelected
		iconClr = ColorAccent
		textClr = ColorText
	} else if btn.Hovered() {
		bg = ColorHover
		iconClr = ColorText
		textClr = ColorText
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(btnRadiusStd)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, icon, 16, iconClr)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th.Material, label)
								lbl.Color = textClr
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	})
}

// layoutTextLink renders a clickable text link (no background, accent color).
// Use for inline actions like "Join", "Create", "Message".
func layoutTextLink(gtx layout.Context, th *Theme, btn *widget.Clickable, label string, clr color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		c := clr
		if btn.Hovered() {
			c = ColorText
		}
		lbl := material.Body2(th.Material, label)
		lbl.Color = c
		return lbl.Layout(gtx)
	})
}

// layoutCompactIconBtn renders a small icon-only button with hover background.
// Use for header actions (settings gear, close X, etc.)
func layoutCompactIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, size unit.Dp, clr color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		c := clr
		if btn.Hovered() {
			bg = ColorHover
			c = ColorText
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				if bg.A == 0 {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, gtx.Constraints.Min.Y)}
				}
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, icon, size, c)
				})
			},
		)
	})
}

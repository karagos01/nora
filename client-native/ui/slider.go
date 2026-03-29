package ui

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// Slider is a horizontal slider widget for Gio.
type Slider struct {
	Value   float32
	Min     float32
	Max     float32
	drag    gesture.Drag
	changed bool
}

// Changed reports whether Value has changed since last call.
func (s *Slider) Changed() bool {
	c := s.changed
	s.changed = false
	return c
}

// Layout draws the slider at the given width.
func (s *Slider) Layout(gtx layout.Context, width unit.Dp) layout.Dimensions {
	if s.Max <= s.Min {
		s.Max = s.Min + 1
	}

	w := gtx.Dp(width)
	if w <= 0 {
		w = gtx.Constraints.Max.X
	}
	if w > gtx.Constraints.Max.X {
		w = gtx.Constraints.Max.X
	}

	trackH := gtx.Dp(4)
	handleR := gtx.Dp(6)
	totalH := handleR*2 + 2

	// Process drag events
	for {
		ev, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch ev.Kind {
		case pointer.Press, pointer.Drag:
			x := ev.Position.X
			ratio := x / float32(w)
			if ratio < 0 {
				ratio = 0
			}
			if ratio > 1 {
				ratio = 1
			}
			newVal := s.Min + ratio*(s.Max-s.Min)
			if newVal != s.Value {
				s.Value = newVal
				s.changed = true
			}
		}
	}

	if s.Value < s.Min {
		s.Value = s.Min
	}
	if s.Value > s.Max {
		s.Value = s.Max
	}

	ratio := (s.Value - s.Min) / (s.Max - s.Min)
	fillW := int(ratio * float32(w))

	size := image.Pt(w, totalH)

	// Register drag area
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	s.drag.Add(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)

	trackY := (totalH - trackH) / 2

	// Track background
	rr := trackH / 2
	paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
		Rect: image.Rect(0, trackY, w, trackY+trackH),
		NE:   rr, NW: rr, SE: rr, SW: rr,
	}.Op(gtx.Ops))

	// Fill (left of handle)
	if fillW > 0 {
		paint.FillShape(gtx.Ops, ColorAccent, clip.RRect{
			Rect: image.Rect(0, trackY, fillW, trackY+trackH),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
	}

	// Handle circle — use op.Offset to position it
	handleD := handleR * 2
	cx := fillW - handleR
	cy := totalH/2 - handleR
	stack := op.Offset(image.Pt(cx, cy)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, ColorWhite,
		clip.Ellipse{Max: image.Pt(handleD, handleD)}.Op(gtx.Ops))
	stack.Pop()

	return layout.Dimensions{Size: size}
}

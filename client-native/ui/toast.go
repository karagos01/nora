package ui

import (
	"image"
	"image/color"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

type ToastLevel int

const (
	ToastError   ToastLevel = iota
	ToastWarning
	ToastSuccess
	ToastInfo
)

type toast struct {
	Message   string
	Level     ToastLevel
	CreatedAt time.Time
	Duration  time.Duration
}

type ToastManager struct {
	app    *App
	mu     sync.Mutex
	toasts []toast
}

func NewToastManager(a *App) *ToastManager {
	return &ToastManager{app: a}
}

func (t *ToastManager) Show(msg string, level ToastLevel) {
	t.mu.Lock()
	t.toasts = append(t.toasts, toast{
		Message:   msg,
		Level:     level,
		CreatedAt: time.Now(),
		Duration:  4 * time.Second,
	})
	// Max 5 viditelných toastů
	if len(t.toasts) > 5 {
		t.toasts = t.toasts[len(t.toasts)-5:]
	}
	t.mu.Unlock()
	t.app.Window.Invalidate()
}

func (t *ToastManager) Error(msg string)   { t.Show(msg, ToastError) }
func (t *ToastManager) Warning(msg string) { t.Show(msg, ToastWarning) }
func (t *ToastManager) Success(msg string) { t.Show(msg, ToastSuccess) }
func (t *ToastManager) Info(msg string)    { t.Show(msg, ToastInfo) }

func (t *ToastManager) Layout(gtx layout.Context) layout.Dimensions {
	t.mu.Lock()
	now := time.Now()
	// Odfiltrovat expirované
	alive := t.toasts[:0]
	needInvalidate := false
	for _, toast := range t.toasts {
		if now.Sub(toast.CreatedAt) < toast.Duration {
			alive = append(alive, toast)
			needInvalidate = true
		}
	}
	t.toasts = alive
	toasts := make([]toast, len(alive))
	copy(toasts, alive)
	t.mu.Unlock()

	if len(toasts) == 0 {
		return layout.Dimensions{}
	}

	// Invalidovat pro animaci zmizení
	if needInvalidate {
		gtx.Execute(op.InvalidateCmd{At: time.Now().Add(50 * time.Millisecond)})
	}

	// Toasty v pravém dolním rohu
	return layout.S.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
				toastItems(gtx, t.app.Theme, toasts)...,
			)
		})
	})
}

func toastItems(gtx layout.Context, th *Theme, toasts []toast) []layout.FlexChild {
	items := make([]layout.FlexChild, len(toasts))
	now := time.Now()
	for i, t := range toasts {
		t := t
		items[i] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutToast(gtx, th, t, now)
			})
		})
	}
	return items
}

func layoutToast(gtx layout.Context, th *Theme, t toast, now time.Time) layout.Dimensions {
	elapsed := now.Sub(t.CreatedAt)
	remaining := t.Duration - elapsed

	// Fade out v posledních 500ms
	alpha := byte(255)
	if remaining < 500*time.Millisecond {
		alpha = byte(255 * remaining / (500 * time.Millisecond))
	}

	bg := toastBgColor(t.Level)
	bg.A = alpha
	fg := color.NRGBA{R: 255, G: 255, B: 255, A: alpha}

	maxW := gtx.Dp(360)
	if maxW > gtx.Constraints.Max.X {
		maxW = gtx.Constraints.Max.X
	}
	gtx.Constraints.Max.X = maxW

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body2(th.Material, t.Message)
		lbl.Color = fg
		return lbl.Layout(gtx)
	})
	call := macro.Stop()

	// Pozadí s rounded rect
	rr := gtx.Dp(6)
	rect := clip.RRect{
		Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
		NE:   rr, NW: rr, SE: rr, SW: rr,
	}.Push(gtx.Ops)
	paint.ColorOp{Color: bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()

	call.Add(gtx.Ops)
	return dims
}

func toastBgColor(level ToastLevel) color.NRGBA {
	switch level {
	case ToastError:
		return color.NRGBA{R: 180, G: 40, B: 40, A: 255}
	case ToastWarning:
		return color.NRGBA{R: 180, G: 130, B: 20, A: 255}
	case ToastSuccess:
		return color.NRGBA{R: 40, G: 150, B: 80, A: 255}
	default: // ToastInfo
		return color.NRGBA{R: 50, G: 50, B: 90, A: 255}
	}
}

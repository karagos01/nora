package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"

	"github.com/google/uuid"
)

// LiveWhiteboardView is an ephemeral whiteboard overlay for voice channels.
// Strokes live in server Hub memory only — no database persistence.
type LiveWhiteboardView struct {
	app     *App
	Visible bool

	ChannelID string
	StarterID string

	strokes     []api.WhiteboardStroke
	currentPath []f32.Point
	drawing     bool

	// Toolbar
	tool      string // "pen" | "eraser" | "rect" | "circle" | "line" | "arrow" | "text"
	colorIdx  int
	width     int
	closeBtn  widget.Clickable
	undoBtn   widget.Clickable
	clearBtn  widget.Clickable
	exportBtn widget.Clickable
	penBtn    widget.Clickable
	eraserBtn widget.Clickable
	colorBtns [16]widget.Clickable
	widthUp   widget.Clickable
	widthDown widget.Clickable

	// Shape tools
	shapeStart  f32.Point
	shapeEnd    f32.Point
	rectBtn     widget.Clickable
	circleBtn   widget.Clickable
	lineBtn     widget.Clickable
	arrowBtn    widget.Clickable
	textToolBtn widget.Clickable
	fillBtn     widget.Clickable
	fillShape   bool

	// Text tool
	showTextInput    bool
	textEditor       widget.Editor
	textPos          f32.Point
	textSizeIdx      int
	textSubmitBtn    widget.Clickable
	textCancelBtn    widget.Clickable
	textSizeCycleBtn widget.Clickable
	textNeedsFocus   bool

	// Pan & Zoom
	zoom       float32
	panX       float32
	panY       float32
	panning    bool
	panStart   f32.Point
	panStartOX float32
	panStartOY float32
	zoomInBtn  widget.Clickable
	zoomOutBtn widget.Clickable
	zoomResetBtn widget.Clickable

	// Color + opacity
	opacity       float32
	opacitySlider Slider
	hexEditor     widget.Editor
	customColor   color.NRGBA
	useCustom     bool

	// Drawing input
	canvasTag bool
}

func NewLiveWhiteboardView(a *App) *LiveWhiteboardView {
	lw := &LiveWhiteboardView{
		app:         a,
		tool:        "pen",
		width:       3,
		zoom:        1.0,
		opacity:     1.0,
		textSizeIdx: 1,
	}
	lw.textEditor.Submit = true
	lw.hexEditor.SingleLine = true
	lw.opacitySlider = Slider{Min: 0, Max: 1, Value: 1}
	return lw
}

// Start sends livewb.start and opens the view.
func (lw *LiveWhiteboardView) Start(channelID string) {
	conn := lw.app.Conn()
	if conn == nil {
		return
	}
	conn.WS.SendJSON("livewb.start", map[string]string{
		"channel_id": channelID,
	})
	lw.Open(channelID, conn.UserID)
}

// Open opens the live whiteboard view for viewing/drawing.
// Sends livewb.join to request the full stroke state from the server.
func (lw *LiveWhiteboardView) Open(channelID, starterID string) {
	lw.Visible = true
	lw.ChannelID = channelID
	lw.StarterID = starterID
	lw.strokes = nil
	lw.currentPath = nil
	lw.drawing = false
	lw.zoom = 1.0
	lw.panX = 0
	lw.panY = 0
	lw.showTextInput = false

	// Request existing strokes from server
	conn := lw.app.Conn()
	if conn != nil {
		conn.WS.SendJSON("livewb.join", map[string]string{
			"channel_id": channelID,
		})
	}
}

// Close hides the view. If we are the starter, sends livewb.stop.
func (lw *LiveWhiteboardView) Close() {
	conn := lw.app.Conn()
	if conn != nil && lw.StarterID == conn.UserID {
		conn.WS.SendJSON("livewb.stop", map[string]string{
			"channel_id": lw.ChannelID,
		})
	}
	lw.Visible = false
	lw.ChannelID = ""
	lw.StarterID = ""
	lw.strokes = nil
	lw.currentPath = nil
	lw.showTextInput = false
}

func (lw *LiveWhiteboardView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return lw.layoutCanvas(gtx)
}

func (lw *LiveWhiteboardView) layoutCanvas(gtx layout.Context) layout.Dimensions {
	conn := lw.app.Conn()

	// Toolbar clicks
	if lw.closeBtn.Clicked(gtx) {
		lw.Close()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if lw.undoBtn.Clicked(gtx) {
		if conn != nil {
			conn.WS.SendJSON("livewb.undo", map[string]string{
				"channel_id": lw.ChannelID,
			})
		}
	}
	if lw.clearBtn.Clicked(gtx) {
		if conn != nil {
			conn.WS.SendJSON("livewb.clear", map[string]string{
				"channel_id": lw.ChannelID,
			})
		}
	}
	if lw.exportBtn.Clicked(gtx) {
		strokes := make([]api.WhiteboardStroke, len(lw.strokes))
		copy(strokes, lw.strokes)
		canvasW := gtx.Constraints.Max.X
		canvasH := gtx.Constraints.Max.Y
		go func() {
			lw.exportPNG(strokes, canvasW, canvasH)
		}()
	}

	// Tool selection
	if lw.penBtn.Clicked(gtx) {
		lw.tool = "pen"
	}
	if lw.eraserBtn.Clicked(gtx) {
		lw.tool = "eraser"
	}
	if lw.rectBtn.Clicked(gtx) {
		lw.tool = "rect"
	}
	if lw.circleBtn.Clicked(gtx) {
		lw.tool = "circle"
	}
	if lw.lineBtn.Clicked(gtx) {
		lw.tool = "line"
	}
	if lw.arrowBtn.Clicked(gtx) {
		lw.tool = "arrow"
	}
	if lw.textToolBtn.Clicked(gtx) {
		lw.tool = "text"
	}
	if lw.fillBtn.Clicked(gtx) {
		lw.fillShape = !lw.fillShape
	}

	// Color selection
	for i := range lw.colorBtns {
		if lw.colorBtns[i].Clicked(gtx) {
			lw.colorIdx = i
			lw.useCustom = false
		}
	}

	// Width
	if lw.widthUp.Clicked(gtx) && lw.width < 20 {
		lw.width++
	}
	if lw.widthDown.Clicked(gtx) && lw.width > 1 {
		lw.width--
	}

	// Zoom buttons
	if lw.zoomInBtn.Clicked(gtx) {
		lw.zoom = min(lw.zoom*1.25, 4.0)
	}
	if lw.zoomOutBtn.Clicked(gtx) {
		lw.zoom = max(lw.zoom/1.25, 0.25)
	}
	if lw.zoomResetBtn.Clicked(gtx) {
		lw.zoom = 1.0
		lw.panX = 0
		lw.panY = 0
	}

	// Hex color input
	hexText := lw.hexEditor.Text()
	if isValidHexColor(hexText) {
		lw.customColor = parseHexColor(hexText)
		lw.useCustom = true
	}

	// Text input events
	if lw.showTextInput {
		if lw.textSubmitBtn.Clicked(gtx) {
			lw.submitText()
		}
		if lw.textCancelBtn.Clicked(gtx) {
			lw.showTextInput = false
			lw.textEditor.SetText("")
		}
		if lw.textSizeCycleBtn.Clicked(gtx) {
			lw.textSizeIdx = (lw.textSizeIdx + 1) % len(textSizes)
		}
		for {
			ev, ok := lw.textEditor.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				lw.submitText()
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.layoutToolbar(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return lw.layoutDrawArea(gtx)
		}),
	)
}

func (lw *LiveWhiteboardView) layoutToolbar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return lw.layoutToolbarRow1(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return lw.layoutToolbarRow2(gtx)
			}),
		)
	})
}

func (lw *LiveWhiteboardView) layoutToolbarRow1(gtx layout.Context) layout.Dimensions {
	conn := lw.app.Conn()
	isStarter := conn != nil && lw.StarterID == conn.UserID

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		// Title
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(lw.app.Theme.Material, "Live Whiteboard")
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		}),
		layout.Rigid(wbSpacer(12)),

		// Pen
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.penBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "pen" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Pen", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),

		// Eraser
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.eraserBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "eraser" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Eraser", clr)
			})
		}),
		layout.Rigid(wbSpacer(6)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(6)),

		// Shape tools
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.rectBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "rect" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Rect", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.circleBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "circle" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Circle", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.lineBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "line" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Line", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.arrowBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "arrow" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Arrow", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.textToolBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.tool == "text" {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Text", clr)
			})
		}),
		layout.Rigid(wbSpacer(6)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(6)),

		// Fill toggle
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.fillBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if lw.fillShape {
					clr = ColorAccent
				}
				return lw.layoutToolBtn(gtx, "Fill", clr)
			})
		}),
		layout.Rigid(wbSpacer(6)),

		// Width
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.widthDown.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIcon(gtx, IconArrowDown, 14, ColorTextDim)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(lw.app.Theme.Material, fmt.Sprintf("%dpx", lw.width))
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.widthUp.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIcon(gtx, IconArrowUp, 14, ColorTextDim)
			})
		}),

		// Spacer
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}),

		// Export PNG
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.exportBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return lw.layoutToolBtn(gtx, "Export", ColorTextDim)
			})
		}),
		layout.Rigid(wbSpacer(2)),

		// Undo
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.undoBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return lw.layoutToolBtn(gtx, "Undo", ColorTextDim)
			})
		}),
		layout.Rigid(wbSpacer(2)),

		// Clear
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.clearBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return lw.layoutToolBtn(gtx, "Clear", ColorDanger)
			})
		}),
		layout.Rigid(wbSpacer(4)),

		// Close (starter = stop, others = just hide)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := "Leave"
			if isStarter {
				label = "Stop"
			}
			return lw.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconClose, 18, ColorTextDim)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(lw.app.Theme.Material, label)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		}),
	)
}

func (lw *LiveWhiteboardView) layoutToolbarRow2(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		// Color swatches
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.layoutColorPalette(gtx)
		}),
		layout.Rigid(wbSpacer(6)),

		// Hex input
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(76)
			gtx.Constraints.Min.X = gtx.Dp(68)
			ed := material.Editor(lw.app.Theme.Material, &lw.hexEditor, "#hex")
			ed.Color = ColorText
			ed.HintColor = ColorTextDim
			ed.TextSize = unit.Sp(12)
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(4)
					bg := ColorInput
					if lw.useCustom {
						bg = withAlpha(ColorAccent, 40)
					}
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, ed.Layout)
				},
			)
		}),
		layout.Rigid(wbSpacer(8)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(8)),

		// Opacity
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(lw.app.Theme.Material, "Opacity:")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
		layout.Rigid(wbSpacer(4)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			dims := lw.opacitySlider.Layout(gtx, unit.Dp(90))
			lw.opacity = lw.opacitySlider.Value
			return dims
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(lw.app.Theme.Material, fmt.Sprintf("%.0f%%", lw.opacity*100))
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(wbSpacer(8)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(8)),

		// Zoom controls
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.zoomOutBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(lw.app.Theme.Material, " - ")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(lw.app.Theme.Material, fmt.Sprintf("%.0f%%", lw.zoom*100))
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.zoomInBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(lw.app.Theme.Material, " + ")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(wbSpacer(4)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.zoomResetBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(lw.app.Theme.Material, "Reset")
				clr := ColorTextDim
				if lw.zoomResetBtn.Hovered() {
					clr = ColorText
				}
				lbl.Color = clr
				return lbl.Layout(gtx)
			})
		}),
	)
}

func (lw *LiveWhiteboardView) layoutToolBtn(gtx layout.Context, label string, clr color.NRGBA) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Caption(lw.app.Theme.Material, label)
		lbl.Color = clr
		return lbl.Layout(gtx)
	})
}

func (lw *LiveWhiteboardView) layoutColorPalette(gtx layout.Context) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(lw.colorBtns)*2)
	for i := range lw.colorBtns {
		idx := i
		if i > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Spacer{Width: unit.Dp(2)}.Layout(gtx)
			}))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return lw.colorBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Dp(16)
				size := image.Pt(sz, sz)
				rr := gtx.Dp(3)
				clr := wbColors[idx]
				paint.FillShape(gtx.Ops, clr, clip.RRect{
					Rect: image.Rectangle{Max: size},
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				if idx == lw.colorIdx && !lw.useCustom {
					bw := gtx.Dp(2)
					paint.FillShape(gtx.Ops, ColorText, clip.Rect{Max: image.Pt(sz, bw)}.Op())
					paint.FillShape(gtx.Ops, ColorText, clip.Rect{Min: image.Pt(0, sz-bw), Max: image.Pt(sz, sz)}.Op())
					paint.FillShape(gtx.Ops, ColorText, clip.Rect{Max: image.Pt(bw, sz)}.Op())
					paint.FillShape(gtx.Ops, ColorText, clip.Rect{Min: image.Pt(sz-bw, 0), Max: image.Pt(sz, sz)}.Op())
				}
				return layout.Dimensions{Size: size}
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

func (lw *LiveWhiteboardView) layoutDrawArea(gtx layout.Context) layout.Dimensions {
	canvasSize := gtx.Constraints.Max

	// Background
	paint.FillShape(gtx.Ops, wbCanvasBg, clip.Rect{Max: canvasSize}.Op())

	// Handle pointer events
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &lw.canvasTag,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -10, Max: 10},
		})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}

		switch pe.Kind {
		case pointer.Scroll:
			if pe.Scroll.Y != 0 {
				oldZoom := lw.zoom
				lw.zoom *= 1 - pe.Scroll.Y*0.05
				if lw.zoom < 0.25 {
					lw.zoom = 0.25
				}
				if lw.zoom > 4.0 {
					lw.zoom = 4.0
				}
				ratio := lw.zoom / oldZoom
				lw.panX = pe.Position.X - (pe.Position.X-lw.panX)*ratio
				lw.panY = pe.Position.Y - (pe.Position.Y-lw.panY)*ratio
				lw.app.Window.Invalidate()
			}

		case pointer.Press:
			if lw.showTextInput {
				break
			}
			if pe.Buttons.Contain(pointer.ButtonSecondary) {
				lw.panning = true
				lw.panStart = pe.Position
				lw.panStartOX = lw.panX
				lw.panStartOY = lw.panY
			} else if lw.tool == "text" {
				lw.textPos = lw.screenToNorm(pe.Position, canvasSize)
				lw.showTextInput = true
				lw.textNeedsFocus = true
				lw.textEditor.SetText("")
			} else if isShapeTool(lw.tool) {
				lw.drawing = true
				lw.shapeStart = pe.Position
				lw.shapeEnd = pe.Position
			} else {
				lw.drawing = true
				lw.currentPath = []f32.Point{pe.Position}
			}
			lw.app.Window.Invalidate()

		case pointer.Drag:
			if lw.panning {
				lw.panX = lw.panStartOX + pe.Position.X - lw.panStart.X
				lw.panY = lw.panStartOY + pe.Position.Y - lw.panStart.Y
			} else if lw.drawing {
				if isShapeTool(lw.tool) {
					lw.shapeEnd = pe.Position
				} else {
					lw.currentPath = append(lw.currentPath, pe.Position)
				}
			}
			lw.app.Window.Invalidate()

		case pointer.Release, pointer.Cancel:
			if lw.panning {
				lw.panning = false
			} else if lw.drawing {
				if isShapeTool(lw.tool) {
					lw.finishShapeStroke(canvasSize)
				} else if len(lw.currentPath) > 1 {
					lw.finishStroke(canvasSize)
				}
				lw.drawing = false
				lw.currentPath = nil
			}
		}
	}

	// Render existing strokes
	for _, s := range lw.strokes {
		lw.renderStroke(gtx, s, canvasSize)
	}

	// Render current stroke (pen/eraser)
	if lw.drawing && len(lw.currentPath) > 1 && !isShapeTool(lw.tool) {
		clr := lw.getStrokeColor()
		if lw.tool == "eraser" {
			clr = wbCanvasBg
		}
		drawPath(gtx, lw.currentPath, clr, float32(lw.width))
	}

	// Render shape preview
	if lw.drawing && isShapeTool(lw.tool) {
		clr := lw.getStrokeColor()
		w := float32(lw.width)
		tool := lw.tool
		if lw.fillShape && (tool == "rect" || tool == "circle") {
			tool += "_fill"
		}
		switch tool {
		case "rect":
			drawRectOutline(gtx.Ops, lw.shapeStart, lw.shapeEnd, clr, w)
		case "rect_fill":
			drawFilledRect(gtx.Ops, lw.shapeStart, lw.shapeEnd, clr)
		case "circle":
			drawEllipseOutline(gtx.Ops, lw.shapeStart, lw.shapeEnd, clr, w)
		case "circle_fill":
			drawFilledEllipse(gtx.Ops, lw.shapeStart, lw.shapeEnd, clr)
		case "line":
			drawLine(gtx.Ops, lw.shapeStart, lw.shapeEnd, clr, w)
		case "arrow":
			drawArrowLine(gtx.Ops, lw.shapeStart, lw.shapeEnd, clr, w)
		}
	}

	// Register pointer input area
	st := clip.Rect{Max: canvasSize}.Push(gtx.Ops)
	event.Op(gtx.Ops, &lw.canvasTag)
	if lw.panning {
		pointer.CursorGrabbing.Add(gtx.Ops)
	} else {
		pointer.CursorCrosshair.Add(gtx.Ops)
	}
	st.Pop()

	// Text input overlay
	if lw.showTextInput {
		if lw.textNeedsFocus {
			gtx.Execute(key.FocusCmd{Tag: &lw.textEditor})
			lw.textNeedsFocus = false
		}
		lw.layoutTextInput(gtx, canvasSize)
	}

	return layout.Dimensions{Size: canvasSize}
}

func (lw *LiveWhiteboardView) layoutTextInput(gtx layout.Context, canvasSize image.Point) {
	pos := lw.normToScreen(lw.textPos.X, lw.textPos.Y, canvasSize)

	overlayW := gtx.Dp(220)
	overlayH := gtx.Dp(90)
	ox := int(pos.X)
	oy := int(pos.Y)
	if ox+overlayW > canvasSize.X {
		ox = canvasSize.X - overlayW
	}
	if oy+overlayH > canvasSize.Y {
		oy = canvasSize.Y - overlayH
	}
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}

	offStack := op.Offset(image.Pt(ox, oy)).Push(gtx.Ops)
	defer offStack.Pop()

	ogtx := gtx
	ogtx.Constraints = layout.Constraints{
		Min: image.Pt(gtx.Dp(200), 0),
		Max: image.Pt(gtx.Dp(220), gtx.Dp(120)),
	}

	layout.Background{}.Layout(ogtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
				Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			bw := gtx.Dp(1)
			borderClr := withAlpha(ColorAccent, 100)
			paint.FillShape(gtx.Ops, borderClr, clip.Rect{Max: image.Pt(bounds.Max.X, bw)}.Op())
			paint.FillShape(gtx.Ops, borderClr, clip.Rect{Min: image.Pt(0, bounds.Max.Y-bw), Max: bounds.Max}.Op())
			paint.FillShape(gtx.Ops, borderClr, clip.Rect{Max: image.Pt(bw, bounds.Max.Y)}.Op())
			paint.FillShape(gtx.Ops, borderClr, clip.Rect{Min: image.Pt(bounds.Max.X-bw, 0), Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(lw.app.Theme.Material, &lw.textEditor, "Type text...")
						ed.Color = ColorText
						ed.HintColor = ColorTextDim
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
								rr := gtx.Dp(4)
								paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
									Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
								}.Op(gtx.Ops))
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, ed.Layout)
							},
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return lw.textSubmitBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return lw.layoutToolBtn(gtx, "OK", ColorAccent)
									})
								}),
								layout.Rigid(wbSpacer(2)),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return lw.textCancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return lw.layoutToolBtn(gtx, "Cancel", ColorTextDim)
									})
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return lw.textSizeCycleBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(lw.app.Theme.Material, fmt.Sprintf("%dpt", textSizes[lw.textSizeIdx]))
										lbl.Color = ColorAccent
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					}),
				)
			})
		},
	)
}

// --- Rendering ---

func (lw *LiveWhiteboardView) renderStroke(gtx layout.Context, s api.WhiteboardStroke, canvasSize image.Point) {
	if s.Tool == "text" {
		lw.renderTextStroke(gtx, s, canvasSize)
		return
	}

	var points [][]float64
	if err := json.Unmarshal([]byte(s.PathData), &points); err != nil {
		return
	}

	clr := parseHexColor(s.Color)
	if s.Tool == "eraser" {
		clr = wbCanvasBg
	}
	w := float32(s.Width)

	switch s.Tool {
	case "rect", "rect_fill", "circle", "circle_fill", "line", "arrow":
		if len(points) < 2 || len(points[0]) < 2 || len(points[1]) < 2 {
			return
		}
		p0 := lw.normToScreen(float32(points[0][0]), float32(points[0][1]), canvasSize)
		p1 := lw.normToScreen(float32(points[1][0]), float32(points[1][1]), canvasSize)

		switch s.Tool {
		case "rect":
			drawRectOutline(gtx.Ops, p0, p1, clr, w)
		case "rect_fill":
			drawFilledRect(gtx.Ops, p0, p1, clr)
		case "circle":
			drawEllipseOutline(gtx.Ops, p0, p1, clr, w)
		case "circle_fill":
			drawFilledEllipse(gtx.Ops, p0, p1, clr)
		case "line":
			drawLine(gtx.Ops, p0, p1, clr, w)
		case "arrow":
			drawArrowLine(gtx.Ops, p0, p1, clr, w)
		}

	default:
		if len(points) < 2 {
			return
		}
		fPts := make([]f32.Point, len(points))
		for i, pt := range points {
			if len(pt) >= 2 {
				fPts[i] = lw.normToScreen(float32(pt[0]), float32(pt[1]), canvasSize)
			}
		}
		drawPath(gtx, fPts, clr, w)
	}
}

func (lw *LiveWhiteboardView) renderTextStroke(gtx layout.Context, s api.WhiteboardStroke, canvasSize image.Point) {
	var td struct {
		Text string  `json:"text"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
		Size int     `json:"size"`
	}
	if err := json.Unmarshal([]byte(s.PathData), &td); err != nil || td.Text == "" {
		return
	}

	clr := parseHexColor(s.Color)
	pos := lw.normToScreen(float32(td.X), float32(td.Y), canvasSize)

	fontSize := float32(td.Size) * lw.zoom
	if fontSize < 4 {
		fontSize = 4
	}
	if fontSize > 200 {
		fontSize = 200
	}

	stack := op.Offset(image.Pt(int(pos.X), int(pos.Y))).Push(gtx.Ops)
	lbl := material.Label(lw.app.Theme.Material, unit.Sp(fontSize), td.Text)
	lbl.Color = clr
	lbl.Layout(gtx)
	stack.Pop()
}

// --- Stroke submission (optimistic local + WS) ---

// addLocalStroke generates a UUID, appends the stroke to lw.strokes immediately,
// and sends the stroke to the server. The server broadcasts to other clients only.
func (lw *LiveWhiteboardView) addLocalStroke(pathData, hexColor, tool string, width int) {
	id := uuid.New().String()

	lw.strokes = append(lw.strokes, api.WhiteboardStroke{
		ID:       id,
		UserID:   lw.getUserID(),
		PathData: pathData,
		Color:    hexColor,
		Width:    width,
		Tool:     tool,
		Username: lw.app.Username,
	})

	conn := lw.app.Conn()
	if conn == nil {
		return
	}
	conn.WS.SendJSON("livewb.stroke", map[string]any{
		"channel_id": lw.ChannelID,
		"id":         id,
		"path_data":  pathData,
		"color":      hexColor,
		"width":      width,
		"tool":       tool,
		"username":   lw.app.Username,
	})
}

func (lw *LiveWhiteboardView) finishStroke(canvasSize image.Point) {
	if len(lw.currentPath) < 2 {
		return
	}

	points := make([][]float64, len(lw.currentPath))
	for i, p := range lw.currentPath {
		n := lw.screenToNorm(p, canvasSize)
		points[i] = []float64{float64(n.X), float64(n.Y)}
	}

	pathData, _ := json.Marshal(points)
	lw.addLocalStroke(string(pathData), lw.getStrokeHex(), lw.tool, lw.width)
}

func (lw *LiveWhiteboardView) finishShapeStroke(canvasSize image.Point) {
	dx := lw.shapeEnd.X - lw.shapeStart.X
	dy := lw.shapeEnd.Y - lw.shapeStart.Y
	if dx*dx+dy*dy < 4 {
		return
	}

	p0 := lw.screenToNorm(lw.shapeStart, canvasSize)
	p1 := lw.screenToNorm(lw.shapeEnd, canvasSize)

	points := [][]float64{
		{float64(p0.X), float64(p0.Y)},
		{float64(p1.X), float64(p1.Y)},
	}
	pathData, _ := json.Marshal(points)

	tool := lw.tool
	if lw.fillShape && (tool == "rect" || tool == "circle") {
		tool += "_fill"
	}
	lw.addLocalStroke(string(pathData), lw.getStrokeHex(), tool, lw.width)
}

func (lw *LiveWhiteboardView) submitText() {
	text := lw.textEditor.Text()
	if text == "" {
		lw.showTextInput = false
		return
	}

	textData := map[string]interface{}{
		"text": text,
		"x":    float64(lw.textPos.X),
		"y":    float64(lw.textPos.Y),
		"size": textSizes[lw.textSizeIdx],
	}
	pathData, _ := json.Marshal(textData)

	lw.addLocalStroke(string(pathData), lw.getStrokeHex(), "text", 0)

	lw.textEditor.SetText("")
	lw.showTextInput = false
}

// --- Color helpers ---

func (lw *LiveWhiteboardView) getStrokeColor() color.NRGBA {
	var c color.NRGBA
	if lw.useCustom {
		c = lw.customColor
	} else {
		c = wbColors[lw.colorIdx]
	}
	c.A = uint8(lw.opacity * 255)
	return c
}

func (lw *LiveWhiteboardView) getStrokeHex() string {
	var hexColor string
	if lw.useCustom {
		hexColor = fmt.Sprintf("#%02x%02x%02x", lw.customColor.R, lw.customColor.G, lw.customColor.B)
	} else {
		hexColor = wbColorHex[lw.colorIdx]
	}
	if lw.opacity < 0.99 {
		alpha := uint8(lw.opacity * 255)
		hexColor += fmt.Sprintf("%02x", alpha)
	}
	return hexColor
}

// --- Transform helpers ---

func (lw *LiveWhiteboardView) screenToNorm(p f32.Point, cs image.Point) f32.Point {
	return f32.Point{
		X: (p.X - lw.panX) / (lw.zoom * float32(cs.X)),
		Y: (p.Y - lw.panY) / (lw.zoom * float32(cs.Y)),
	}
}

func (lw *LiveWhiteboardView) normToScreen(nx, ny float32, cs image.Point) f32.Point {
	return f32.Point{
		X: nx*lw.zoom*float32(cs.X) + lw.panX,
		Y: ny*lw.zoom*float32(cs.Y) + lw.panY,
	}
}

// --- PNG Export ---

func (lw *LiveWhiteboardView) exportPNG(strokes []api.WhiteboardStroke, canvasW, canvasH int) {
	if canvasW < 100 {
		canvasW = 1920
	}
	if canvasH < 100 {
		canvasH = 1080
	}
	if canvasW > 4000 {
		canvasW = 4000
	}
	if canvasH > 4000 {
		canvasH = 4000
	}

	img := renderStrokesToImage(strokes, canvasW, canvasH)

	path := saveFileDialog("live-whiteboard.png")
	if path == "" {
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Printf("livewb: png encode: %v", err)
		return
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		log.Printf("livewb: write file: %v", err)
		return
	}
	log.Printf("livewb: exported to %s", path)
}

func (lw *LiveWhiteboardView) getUserID() string {
	conn := lw.app.Conn()
	if conn != nil {
		return conn.UserID
	}
	return ""
}


package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
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
)

var wbCanvasBg = color.NRGBA{R: 20, G: 20, B: 35, A: 255}

// WhiteboardView je fullscreen overlay pro kreslicí plátno.
type WhiteboardView struct {
	app     *App
	Visible bool

	// Board picker
	showPicker   bool
	boards       []api.Whiteboard
	boardBtns    []widget.Clickable
	newBoardBtn  widget.Clickable
	nameEditor   widget.Editor
	boardList    widget.List
	boardDelBtns []widget.Clickable

	// Active board
	BoardID   string
	BoardName string
	CreatorID string

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
	textPos          f32.Point // normalizovaná pozice
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

	// Barvy + opacity
	opacity       float32
	opacitySlider Slider
	hexEditor     widget.Editor
	customColor   color.NRGBA
	useCustom     bool

	// Drawing input
	canvasTag bool
}

var wbColors = [16]color.NRGBA{
	{R: 255, G: 255, B: 255, A: 255}, // white
	{R: 230, G: 60, B: 60, A: 255},   // red
	{R: 80, G: 200, B: 120, A: 255},  // green
	{R: 80, G: 120, B: 230, A: 255},  // blue
	{R: 240, G: 220, B: 40, A: 255},  // yellow
	{R: 240, G: 150, B: 40, A: 255},  // orange
	{R: 170, G: 80, B: 220, A: 255},  // purple
	{R: 60, G: 200, B: 220, A: 255},  // cyan
	{R: 0, G: 0, B: 0, A: 255},       // black
	{R: 240, G: 130, B: 170, A: 255}, // pink
	{R: 140, G: 90, B: 50, A: 255},   // brown
	{R: 130, G: 230, B: 50, A: 255},  // lime
	{R: 0, G: 150, B: 136, A: 255},   // teal
	{R: 30, G: 40, B: 120, A: 255},   // navy
	{R: 150, G: 150, B: 150, A: 255}, // gray
	{R: 220, G: 50, B: 200, A: 255},  // magenta
}

var wbColorHex = [16]string{
	"#ffffff", "#e63c3c", "#50c878", "#5078e6",
	"#f0dc28", "#f09628", "#aa50dc", "#3cc8dc",
	"#000000", "#f082aa", "#8c5a32", "#82e632",
	"#009688", "#1e2878", "#969696", "#dc32c8",
}

var textSizes = [5]int{12, 16, 20, 24, 32}

func NewWhiteboardView(a *App) *WhiteboardView {
	wv := &WhiteboardView{
		app:         a,
		tool:        "pen",
		width:       3,
		zoom:        1.0,
		opacity:     1.0,
		textSizeIdx: 1,
	}
	wv.boardList.Axis = layout.Vertical
	wv.nameEditor.SingleLine = true
	wv.nameEditor.Submit = true
	wv.textEditor.Submit = true
	wv.hexEditor.SingleLine = true
	wv.opacitySlider = Slider{Min: 0, Max: 1, Value: 1}
	return wv
}

// Open zobrazí whiteboard picker.
func (wv *WhiteboardView) Open() {
	wv.Visible = true
	wv.showPicker = true
	wv.BoardID = ""
	wv.strokes = nil
	wv.currentPath = nil
	wv.zoom = 1.0
	wv.panX = 0
	wv.panY = 0
	wv.showTextInput = false

	conn := wv.app.Conn()
	if conn == nil {
		return
	}
	go func() {
		boards, err := conn.Client.GetWhiteboards()
		if err != nil {
			log.Printf("whiteboard: list: %v", err)
			return
		}
		wv.app.mu.Lock()
		wv.boards = boards
		wv.app.mu.Unlock()
		wv.app.Window.Invalidate()
	}()
}

// OpenBoard otevře konkrétní board.
func (wv *WhiteboardView) OpenBoard(boardID, boardName, creatorID string) {
	wv.showPicker = false
	wv.BoardID = boardID
	wv.BoardName = boardName
	wv.CreatorID = creatorID
	wv.strokes = nil
	wv.currentPath = nil
	wv.zoom = 1.0
	wv.panX = 0
	wv.panY = 0
	wv.showTextInput = false

	conn := wv.app.Conn()
	if conn == nil {
		return
	}
	go func() {
		strokes, err := conn.Client.GetWhiteboardStrokes(boardID)
		if err != nil {
			log.Printf("whiteboard: get strokes: %v", err)
			return
		}
		wv.app.mu.Lock()
		wv.strokes = strokes
		wv.app.mu.Unlock()
		wv.app.Window.Invalidate()
	}()
}

func (wv *WhiteboardView) Close() {
	wv.Visible = false
	wv.BoardID = ""
	wv.strokes = nil
	wv.currentPath = nil
	wv.showPicker = false
	wv.showTextInput = false
}

func (wv *WhiteboardView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	if wv.showPicker {
		return wv.layoutPicker(gtx)
	}
	return wv.layoutCanvas(gtx)
}

// layoutPicker — board list
func (wv *WhiteboardView) layoutPicker(gtx layout.Context) layout.Dimensions {
	if wv.closeBtn.Clicked(gtx) {
		wv.Close()
	}

	for {
		ev, ok := wv.nameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			name := wv.nameEditor.Text()
			if name != "" {
				wv.nameEditor.SetText("")
				conn := wv.app.Conn()
				if conn != nil {
					go func() {
						wb, err := conn.Client.CreateWhiteboard(name)
						if err != nil {
							log.Printf("whiteboard: create: %v", err)
							return
						}
						wv.app.mu.Lock()
						wv.boards = append([]api.Whiteboard{*wb}, wv.boards...)
						wv.app.mu.Unlock()
						wv.app.Window.Invalidate()
					}()
				}
			}
		}
	}

	if wv.newBoardBtn.Clicked(gtx) {
		name := wv.nameEditor.Text()
		if name != "" {
			wv.nameEditor.SetText("")
			conn := wv.app.Conn()
			if conn != nil {
				go func() {
					wb, err := conn.Client.CreateWhiteboard(name)
					if err != nil {
						log.Printf("whiteboard: create: %v", err)
						return
					}
					wv.app.mu.Lock()
					wv.boards = append([]api.Whiteboard{*wb}, wv.boards...)
					wv.app.mu.Unlock()
					wv.app.Window.Invalidate()
				}()
			}
		}
	}

	for len(wv.boardBtns) < len(wv.boards) {
		wv.boardBtns = append(wv.boardBtns, widget.Clickable{})
	}
	for len(wv.boardDelBtns) < len(wv.boards) {
		wv.boardDelBtns = append(wv.boardDelBtns, widget.Clickable{})
	}
	conn := wv.app.Conn()
	for i, b := range wv.boards {
		if wv.boardBtns[i].Clicked(gtx) {
			wv.OpenBoard(b.ID, b.Name, b.CreatorID)
		}
		if wv.boardDelBtns[i].Clicked(gtx) {
			boardID := b.ID
			if conn != nil {
				go func() {
					if err := conn.Client.DeleteWhiteboard(boardID); err != nil {
						log.Printf("whiteboard: delete: %v", err)
						return
					}
					wv.app.mu.Lock()
					for j, wb := range wv.boards {
						if wb.ID == boardID {
							wv.boards = append(wv.boards[:j], wv.boards[j+1:]...)
							break
						}
					}
					wv.app.mu.Unlock()
					wv.app.Window.Invalidate()
				}()
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.H6(wv.app.Theme.Material, "Whiteboard")
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return wv.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconClose, 24, ColorTextDim)
						})
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(wv.app.Theme.Material, &wv.nameEditor, "New board name...")
						ed.Color = ColorText
						ed.HintColor = ColorTextDim
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
								rr := gtx.Dp(6)
								paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
									Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
								}.Op(gtx.Ops))
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, ed.Layout)
							},
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return wv.newBoardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconAdd, 28, ColorAccent)
							})
						})
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			count := len(wv.boards)
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return material.List(wv.app.Theme.Material, &wv.boardList).Layout(gtx, count, func(gtx layout.Context, idx int) layout.Dimensions {
					b := wv.boards[idx]
					return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return wv.boardBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := ColorCard
							if wv.boardBtns[idx].Hovered() {
								bg = ColorHover
							}
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
									rr := gtx.Dp(6)
									paint.FillShape(gtx.Ops, bg, clip.RRect{
										Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
									}.Op(gtx.Ops))
									return layout.Dimensions{Size: bounds.Max}
								},
								func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconEdit, 18, ColorAccent)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body1(wv.app.Theme.Material, b.Name)
													lbl.Color = ColorText
													return lbl.Layout(gtx)
												})
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												canDelete := false
												if conn != nil {
													canDelete = b.CreatorID == conn.UserID || wv.app.isAdmin(conn)
												}
												if !canDelete {
													return layout.Dimensions{}
												}
												return wv.boardDelBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													clr := ColorTextDim
													if wv.boardDelBtns[idx].Hovered() {
														clr = ColorDanger
													}
													return layoutIcon(gtx, IconDelete, 18, clr)
												})
											}),
										)
									})
								},
							)
						})
					})
				})
			})
		}),
	)
}

// layoutCanvas — drawing canvas
func (wv *WhiteboardView) layoutCanvas(gtx layout.Context) layout.Dimensions {
	// Toolbar clicks
	if wv.closeBtn.Clicked(gtx) {
		wv.showPicker = true
		wv.BoardID = ""
		wv.strokes = nil
		wv.showTextInput = false
	}
	if wv.undoBtn.Clicked(gtx) {
		conn := wv.app.Conn()
		if conn != nil {
			go func() {
				if err := conn.Client.UndoWhiteboardStroke(wv.BoardID); err != nil {
					log.Printf("whiteboard: undo: %v", err)
				}
			}()
		}
	}
	if wv.clearBtn.Clicked(gtx) {
		conn := wv.app.Conn()
		if conn != nil {
			go func() {
				if err := conn.Client.ClearWhiteboard(wv.BoardID); err != nil {
					log.Printf("whiteboard: clear: %v", err)
				}
			}()
		}
	}
	if wv.exportBtn.Clicked(gtx) {
		strokes := make([]api.WhiteboardStroke, len(wv.strokes))
		copy(strokes, wv.strokes)
		canvasW := gtx.Constraints.Max.X
		canvasH := gtx.Constraints.Max.Y
		go func() {
			wv.exportPNG(strokes, canvasW, canvasH)
		}()
	}

	// Tool selection
	if wv.penBtn.Clicked(gtx) {
		wv.tool = "pen"
	}
	if wv.eraserBtn.Clicked(gtx) {
		wv.tool = "eraser"
	}
	if wv.rectBtn.Clicked(gtx) {
		wv.tool = "rect"
	}
	if wv.circleBtn.Clicked(gtx) {
		wv.tool = "circle"
	}
	if wv.lineBtn.Clicked(gtx) {
		wv.tool = "line"
	}
	if wv.arrowBtn.Clicked(gtx) {
		wv.tool = "arrow"
	}
	if wv.textToolBtn.Clicked(gtx) {
		wv.tool = "text"
	}
	if wv.fillBtn.Clicked(gtx) {
		wv.fillShape = !wv.fillShape
	}

	// Color selection
	for i := range wv.colorBtns {
		if wv.colorBtns[i].Clicked(gtx) {
			wv.colorIdx = i
			wv.useCustom = false
		}
	}

	// Width
	if wv.widthUp.Clicked(gtx) && wv.width < 20 {
		wv.width++
	}
	if wv.widthDown.Clicked(gtx) && wv.width > 1 {
		wv.width--
	}

	// Zoom buttons
	if wv.zoomInBtn.Clicked(gtx) {
		wv.zoom = min(wv.zoom*1.25, 4.0)
	}
	if wv.zoomOutBtn.Clicked(gtx) {
		wv.zoom = max(wv.zoom/1.25, 0.25)
	}
	if wv.zoomResetBtn.Clicked(gtx) {
		wv.zoom = 1.0
		wv.panX = 0
		wv.panY = 0
	}

	// Hex color input
	hexText := wv.hexEditor.Text()
	if isValidHexColor(hexText) {
		wv.customColor = parseHexColor(hexText)
		wv.useCustom = true
	}

	// Text input events
	if wv.showTextInput {
		if wv.textSubmitBtn.Clicked(gtx) {
			wv.submitText()
		}
		if wv.textCancelBtn.Clicked(gtx) {
			wv.showTextInput = false
			wv.textEditor.SetText("")
		}
		if wv.textSizeCycleBtn.Clicked(gtx) {
			wv.textSizeIdx = (wv.textSizeIdx + 1) % len(textSizes)
		}
		for {
			ev, ok := wv.textEditor.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				wv.submitText()
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.layoutToolbar(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return wv.layoutDrawArea(gtx)
		}),
	)
}

func (wv *WhiteboardView) layoutToolbar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return wv.layoutToolbarRow1(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return wv.layoutToolbarRow2(gtx)
			}),
		)
	})
}

func (wv *WhiteboardView) layoutToolbarRow1(gtx layout.Context) layout.Dimensions {
	conn := wv.app.Conn()
	canClear := false
	if conn != nil {
		canClear = wv.CreatorID == conn.UserID || wv.app.isAdmin(conn)
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		// Board name
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(wv.app.Theme.Material, wv.BoardName)
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		}),
		layout.Rigid(wbSpacer(12)),

		// Pen
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.penBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "pen" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Pen", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),

		// Eraser
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.eraserBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "eraser" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Eraser", clr)
			})
		}),
		layout.Rigid(wbSpacer(6)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(6)),

		// Shape tools
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.rectBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "rect" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Rect", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.circleBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "circle" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Circle", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.lineBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "line" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Line", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.arrowBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "arrow" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Arrow", clr)
			})
		}),
		layout.Rigid(wbSpacer(2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.textToolBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.tool == "text" {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Text", clr)
			})
		}),
		layout.Rigid(wbSpacer(6)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(6)),

		// Fill toggle
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.fillBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if wv.fillShape {
					clr = ColorAccent
				}
				return wv.layoutToolBtn(gtx, "Fill", clr)
			})
		}),
		layout.Rigid(wbSpacer(6)),

		// Width
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.widthDown.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIcon(gtx, IconArrowDown, 14, ColorTextDim)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(wv.app.Theme.Material, fmt.Sprintf("%dpx", wv.width))
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.widthUp.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIcon(gtx, IconArrowUp, 14, ColorTextDim)
			})
		}),

		// Spacer
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}),

		// Export PNG
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.exportBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return wv.layoutToolBtn(gtx, "Export", ColorTextDim)
			})
		}),
		layout.Rigid(wbSpacer(2)),

		// Undo
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.undoBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return wv.layoutToolBtn(gtx, "Undo", ColorTextDim)
			})
		}),
		layout.Rigid(wbSpacer(2)),

		// Clear (jen owner/admin)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !canClear {
				return layout.Dimensions{}
			}
			return wv.clearBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return wv.layoutToolBtn(gtx, "Clear", ColorDanger)
			})
		}),
		layout.Rigid(wbSpacer(4)),

		// Close
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIcon(gtx, IconClose, 22, ColorTextDim)
			})
		}),
	)
}

func (wv *WhiteboardView) layoutToolbarRow2(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		// Color swatches (16)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.layoutColorPalette(gtx)
		}),
		layout.Rigid(wbSpacer(6)),

		// Hex input
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(76)
			gtx.Constraints.Min.X = gtx.Dp(68)
			ed := material.Editor(wv.app.Theme.Material, &wv.hexEditor, "#hex")
			ed.Color = ColorText
			ed.HintColor = ColorTextDim
			ed.TextSize = unit.Sp(12)
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(4)
					bg := ColorInput
					if wv.useCustom {
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
			lbl := material.Caption(wv.app.Theme.Material, "Opacity:")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
		layout.Rigid(wbSpacer(4)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			dims := wv.opacitySlider.Layout(gtx, unit.Dp(90))
			wv.opacity = wv.opacitySlider.Value
			return dims
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(wv.app.Theme.Material, fmt.Sprintf("%.0f%%", wv.opacity*100))
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(wbSpacer(8)),
		layout.Rigid(wbDivider),
		layout.Rigid(wbSpacer(8)),

		// Zoom controls
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.zoomOutBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(wv.app.Theme.Material, " - ")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(wv.app.Theme.Material, fmt.Sprintf("%.0f%%", wv.zoom*100))
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.zoomInBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(wv.app.Theme.Material, " + ")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Rigid(wbSpacer(4)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.zoomResetBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(wv.app.Theme.Material, "Reset")
				clr := ColorTextDim
				if wv.zoomResetBtn.Hovered() {
					clr = ColorText
				}
				lbl.Color = clr
				return lbl.Layout(gtx)
			})
		}),
	)
}

func (wv *WhiteboardView) layoutToolBtn(gtx layout.Context, label string, clr color.NRGBA) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Caption(wv.app.Theme.Material, label)
		lbl.Color = clr
		return lbl.Layout(gtx)
	})
}

func (wv *WhiteboardView) layoutColorPalette(gtx layout.Context) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(wv.colorBtns)*2)
	for i := range wv.colorBtns {
		idx := i
		if i > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Spacer{Width: unit.Dp(2)}.Layout(gtx)
			}))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wv.colorBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Dp(16)
				size := image.Pt(sz, sz)
				rr := gtx.Dp(3)
				clr := wbColors[idx]
				paint.FillShape(gtx.Ops, clr, clip.RRect{
					Rect: image.Rectangle{Max: size},
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				// Selected indicator
				if idx == wv.colorIdx && !wv.useCustom {
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

func (wv *WhiteboardView) layoutDrawArea(gtx layout.Context) layout.Dimensions {
	canvasSize := gtx.Constraints.Max

	// Background
	paint.FillShape(gtx.Ops, wbCanvasBg, clip.Rect{Max: canvasSize}.Op())

	// Zpracovat pointer eventy
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &wv.canvasTag,
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
				oldZoom := wv.zoom
				wv.zoom *= 1 - pe.Scroll.Y*0.05
				if wv.zoom < 0.25 {
					wv.zoom = 0.25
				}
				if wv.zoom > 4.0 {
					wv.zoom = 4.0
				}
				// Pivot kolem myši
				ratio := wv.zoom / oldZoom
				wv.panX = pe.Position.X - (pe.Position.X-wv.panX)*ratio
				wv.panY = pe.Position.Y - (pe.Position.Y-wv.panY)*ratio
				wv.app.Window.Invalidate()
			}

		case pointer.Press:
			if wv.showTextInput {
				break
			}
			if pe.Buttons.Contain(pointer.ButtonSecondary) {
				// Right-click → pan
				wv.panning = true
				wv.panStart = pe.Position
				wv.panStartOX = wv.panX
				wv.panStartOY = wv.panY
			} else if wv.tool == "text" {
				wv.textPos = wv.screenToNorm(pe.Position, canvasSize)
				wv.showTextInput = true
				wv.textNeedsFocus = true
				wv.textEditor.SetText("")
			} else if isShapeTool(wv.tool) {
				wv.drawing = true
				wv.shapeStart = pe.Position
				wv.shapeEnd = pe.Position
			} else {
				// pen / eraser
				wv.drawing = true
				wv.currentPath = []f32.Point{pe.Position}
			}
			wv.app.Window.Invalidate()

		case pointer.Drag:
			if wv.panning {
				wv.panX = wv.panStartOX + pe.Position.X - wv.panStart.X
				wv.panY = wv.panStartOY + pe.Position.Y - wv.panStart.Y
			} else if wv.drawing {
				if isShapeTool(wv.tool) {
					wv.shapeEnd = pe.Position
				} else {
					wv.currentPath = append(wv.currentPath, pe.Position)
				}
			}
			wv.app.Window.Invalidate()

		case pointer.Release, pointer.Cancel:
			if wv.panning {
				wv.panning = false
			} else if wv.drawing {
				if isShapeTool(wv.tool) {
					wv.finishShapeStroke(canvasSize)
				} else if len(wv.currentPath) > 1 {
					wv.finishStroke(canvasSize)
				}
				wv.drawing = false
				wv.currentPath = nil
			}
		}
	}

	// Render existující strokes
	for _, s := range wv.strokes {
		wv.renderStroke(gtx, s, canvasSize)
	}

	// Render aktuální tah (pen/eraser)
	if wv.drawing && len(wv.currentPath) > 1 && !isShapeTool(wv.tool) {
		clr := wv.getStrokeColor()
		if wv.tool == "eraser" {
			clr = wbCanvasBg
		}
		drawPath(gtx, wv.currentPath, clr, float32(wv.width))
	}

	// Render shape preview
	if wv.drawing && isShapeTool(wv.tool) {
		clr := wv.getStrokeColor()
		w := float32(wv.width)
		tool := wv.tool
		if wv.fillShape && (tool == "rect" || tool == "circle") {
			tool += "_fill"
		}
		switch tool {
		case "rect":
			drawRectOutline(gtx.Ops, wv.shapeStart, wv.shapeEnd, clr, w)
		case "rect_fill":
			drawFilledRect(gtx.Ops, wv.shapeStart, wv.shapeEnd, clr)
		case "circle":
			drawEllipseOutline(gtx.Ops, wv.shapeStart, wv.shapeEnd, clr, w)
		case "circle_fill":
			drawFilledEllipse(gtx.Ops, wv.shapeStart, wv.shapeEnd, clr)
		case "line":
			drawLine(gtx.Ops, wv.shapeStart, wv.shapeEnd, clr, w)
		case "arrow":
			drawArrowLine(gtx.Ops, wv.shapeStart, wv.shapeEnd, clr, w)
		}
	}

	// Register pointer input area
	st := clip.Rect{Max: canvasSize}.Push(gtx.Ops)
	event.Op(gtx.Ops, &wv.canvasTag)
	if wv.panning {
		pointer.CursorGrabbing.Add(gtx.Ops)
	} else {
		pointer.CursorCrosshair.Add(gtx.Ops)
	}
	st.Pop()

	// Text input overlay
	if wv.showTextInput {
		if wv.textNeedsFocus {
			gtx.Execute(key.FocusCmd{Tag: &wv.textEditor})
			wv.textNeedsFocus = false
		}
		wv.layoutTextInput(gtx, canvasSize)
	}

	return layout.Dimensions{Size: canvasSize}
}

func (wv *WhiteboardView) layoutTextInput(gtx layout.Context, canvasSize image.Point) {
	pos := wv.normToScreen(wv.textPos.X, wv.textPos.Y, canvasSize)

	// Clamp overlay pozice aby se vešla na canvas
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
			// Border
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
					// Editor
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(wv.app.Theme.Material, &wv.textEditor, "Type text...")
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
					// Buttons row
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return wv.textSubmitBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return wv.layoutToolBtn(gtx, "OK", ColorAccent)
									})
								}),
								layout.Rigid(wbSpacer(2)),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return wv.textCancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return wv.layoutToolBtn(gtx, "Cancel", ColorTextDim)
									})
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return wv.textSizeCycleBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(wv.app.Theme.Material, fmt.Sprintf("%dpt", textSizes[wv.textSizeIdx]))
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

func (wv *WhiteboardView) renderStroke(gtx layout.Context, s api.WhiteboardStroke, canvasSize image.Point) {
	if s.Tool == "text" {
		wv.renderTextStroke(gtx, s, canvasSize)
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
		p0 := wv.normToScreen(float32(points[0][0]), float32(points[0][1]), canvasSize)
		p1 := wv.normToScreen(float32(points[1][0]), float32(points[1][1]), canvasSize)

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
		// pen / eraser — séria bodů
		if len(points) < 2 {
			return
		}
		fPts := make([]f32.Point, len(points))
		for i, pt := range points {
			if len(pt) >= 2 {
				fPts[i] = wv.normToScreen(float32(pt[0]), float32(pt[1]), canvasSize)
			}
		}
		drawPath(gtx, fPts, clr, w)
	}
}

func (wv *WhiteboardView) renderTextStroke(gtx layout.Context, s api.WhiteboardStroke, canvasSize image.Point) {
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
	pos := wv.normToScreen(float32(td.X), float32(td.Y), canvasSize)

	fontSize := float32(td.Size) * wv.zoom
	if fontSize < 4 {
		fontSize = 4
	}

	stack := op.Offset(image.Pt(int(pos.X), int(pos.Y))).Push(gtx.Ops)
	lbl := material.Label(wv.app.Theme.Material, unit.Sp(fontSize), td.Text)
	lbl.Color = clr
	lbl.Layout(gtx)
	stack.Pop()
}

// --- Stroke submission ---

func (wv *WhiteboardView) finishStroke(canvasSize image.Point) {
	if len(wv.currentPath) < 2 {
		return
	}

	points := make([][]float64, len(wv.currentPath))
	for i, p := range wv.currentPath {
		n := wv.screenToNorm(p, canvasSize)
		points[i] = []float64{float64(n.X), float64(n.Y)}
	}

	pathData, _ := json.Marshal(points)
	hexColor := wv.getStrokeHex()
	tool := wv.tool
	width := wv.width

	conn := wv.app.Conn()
	if conn == nil {
		return
	}
	boardID := wv.BoardID

	go func() {
		stroke := api.WhiteboardStroke{
			PathData: string(pathData),
			Color:    hexColor,
			Width:    width,
			Tool:     tool,
		}
		_, err := conn.Client.AddWhiteboardStroke(boardID, stroke)
		if err != nil {
			log.Printf("whiteboard: add stroke: %v", err)
		}
	}()
}

func (wv *WhiteboardView) finishShapeStroke(canvasSize image.Point) {
	dx := wv.shapeEnd.X - wv.shapeStart.X
	dy := wv.shapeEnd.Y - wv.shapeStart.Y
	if dx*dx+dy*dy < 4 {
		return // příliš malý shape
	}

	p0 := wv.screenToNorm(wv.shapeStart, canvasSize)
	p1 := wv.screenToNorm(wv.shapeEnd, canvasSize)

	points := [][]float64{
		{float64(p0.X), float64(p0.Y)},
		{float64(p1.X), float64(p1.Y)},
	}
	pathData, _ := json.Marshal(points)

	hexColor := wv.getStrokeHex()
	tool := wv.tool
	if wv.fillShape && (tool == "rect" || tool == "circle") {
		tool += "_fill"
	}
	width := wv.width

	conn := wv.app.Conn()
	if conn == nil {
		return
	}
	boardID := wv.BoardID

	go func() {
		stroke := api.WhiteboardStroke{
			PathData: string(pathData),
			Color:    hexColor,
			Width:    width,
			Tool:     tool,
		}
		_, err := conn.Client.AddWhiteboardStroke(boardID, stroke)
		if err != nil {
			log.Printf("whiteboard: add stroke: %v", err)
		}
	}()
}

func (wv *WhiteboardView) submitText() {
	text := wv.textEditor.Text()
	if text == "" {
		wv.showTextInput = false
		return
	}

	textData := map[string]interface{}{
		"text": text,
		"x":    float64(wv.textPos.X),
		"y":    float64(wv.textPos.Y),
		"size": textSizes[wv.textSizeIdx],
	}
	pathData, _ := json.Marshal(textData)
	hexColor := wv.getStrokeHex()

	conn := wv.app.Conn()
	if conn == nil {
		return
	}
	boardID := wv.BoardID

	go func() {
		stroke := api.WhiteboardStroke{
			PathData: string(pathData),
			Color:    hexColor,
			Width:    0,
			Tool:     "text",
		}
		_, err := conn.Client.AddWhiteboardStroke(boardID, stroke)
		if err != nil {
			log.Printf("whiteboard: add stroke: %v", err)
		}
	}()

	wv.textEditor.SetText("")
	wv.showTextInput = false
}

// --- Color helpers ---

func (wv *WhiteboardView) getStrokeColor() color.NRGBA {
	var c color.NRGBA
	if wv.useCustom {
		c = wv.customColor
	} else {
		c = wbColors[wv.colorIdx]
	}
	c.A = uint8(wv.opacity * 255)
	return c
}

func (wv *WhiteboardView) getStrokeHex() string {
	var hexColor string
	if wv.useCustom {
		hexColor = fmt.Sprintf("#%02x%02x%02x", wv.customColor.R, wv.customColor.G, wv.customColor.B)
	} else {
		hexColor = wbColorHex[wv.colorIdx]
	}
	if wv.opacity < 0.99 {
		alpha := uint8(wv.opacity * 255)
		hexColor += fmt.Sprintf("%02x", alpha)
	}
	return hexColor
}

// --- Drawing primitives ---

func drawPath(gtx layout.Context, pts []f32.Point, clr color.NRGBA, width float32) {
	if len(pts) < 2 {
		return
	}
	for i := 1; i < len(pts); i++ {
		drawLine(gtx.Ops, pts[i-1], pts[i], clr, width)
	}
}

func drawLine(ops *op.Ops, p0, p1 f32.Point, clr color.NRGBA, width float32) {
	dx := p1.X - p0.X
	dy := p1.Y - p0.Y
	length := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if length < 0.5 {
		return
	}

	nx := -dy / length * width / 2
	ny := dx / length * width / 2

	var path clip.Path
	path.Begin(ops)
	path.MoveTo(f32.Point{X: p0.X + nx, Y: p0.Y + ny})
	path.LineTo(f32.Point{X: p1.X + nx, Y: p1.Y + ny})
	path.LineTo(f32.Point{X: p1.X - nx, Y: p1.Y - ny})
	path.LineTo(f32.Point{X: p0.X - nx, Y: p0.Y - ny})
	path.Close()

	st := clip.Outline{Path: path.End()}.Op().Push(ops)
	paint.ColorOp{Color: clr}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}

func drawRectOutline(ops *op.Ops, p0, p1 f32.Point, clr color.NRGBA, width float32) {
	tl := f32.Point{X: min(p0.X, p1.X), Y: min(p0.Y, p1.Y)}
	tr := f32.Point{X: max(p0.X, p1.X), Y: min(p0.Y, p1.Y)}
	br := f32.Point{X: max(p0.X, p1.X), Y: max(p0.Y, p1.Y)}
	bl := f32.Point{X: min(p0.X, p1.X), Y: max(p0.Y, p1.Y)}
	drawLine(ops, tl, tr, clr, width)
	drawLine(ops, tr, br, clr, width)
	drawLine(ops, br, bl, clr, width)
	drawLine(ops, bl, tl, clr, width)
}

func drawFilledRect(ops *op.Ops, p0, p1 f32.Point, clr color.NRGBA) {
	minX := int(min(p0.X, p1.X))
	minY := int(min(p0.Y, p1.Y))
	maxX := int(max(p0.X, p1.X))
	maxY := int(max(p0.Y, p1.Y))
	if maxX <= minX || maxY <= minY {
		return
	}
	st := clip.Rect{Min: image.Pt(minX, minY), Max: image.Pt(maxX, maxY)}.Push(ops)
	paint.ColorOp{Color: clr}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}

func drawEllipseOutline(ops *op.Ops, p0, p1 f32.Point, clr color.NRGBA, width float32) {
	cx := (p0.X + p1.X) / 2
	cy := (p0.Y + p1.Y) / 2
	rx := float32(math.Abs(float64(p1.X-p0.X))) / 2
	ry := float32(math.Abs(float64(p1.Y-p0.Y))) / 2
	if rx < 1 && ry < 1 {
		return
	}

	const segments = 48
	for i := 0; i < segments; i++ {
		a0 := float64(i) * 2 * math.Pi / segments
		a1 := float64(i+1) * 2 * math.Pi / segments
		pp0 := f32.Point{
			X: cx + rx*float32(math.Cos(a0)),
			Y: cy + ry*float32(math.Sin(a0)),
		}
		pp1 := f32.Point{
			X: cx + rx*float32(math.Cos(a1)),
			Y: cy + ry*float32(math.Sin(a1)),
		}
		drawLine(ops, pp0, pp1, clr, width)
	}
}

func drawFilledEllipse(ops *op.Ops, p0, p1 f32.Point, clr color.NRGBA) {
	minX := int(min(p0.X, p1.X))
	minY := int(min(p0.Y, p1.Y))
	maxX := int(max(p0.X, p1.X))
	maxY := int(max(p0.Y, p1.Y))
	if maxX <= minX || maxY <= minY {
		return
	}
	st := clip.Ellipse{Min: image.Pt(minX, minY), Max: image.Pt(maxX, maxY)}.Op(ops).Push(ops)
	paint.ColorOp{Color: clr}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}

func drawArrowLine(ops *op.Ops, p0, p1 f32.Point, clr color.NRGBA, width float32) {
	drawLine(ops, p0, p1, clr, width)

	dx := p1.X - p0.X
	dy := p1.Y - p0.Y
	length := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if length < 2 {
		return
	}

	// Jednotkový vektor směrem k hrotu
	ux := dx / length
	uy := dy / length
	// Perpendikular
	px := -uy
	py := ux

	headLen := width * 4
	if headLen < 10 {
		headLen = 10
	}
	headW := headLen * 0.6

	// Trojúhelník hrotu
	tip := p1
	left := f32.Point{X: p1.X - ux*headLen + px*headW/2, Y: p1.Y - uy*headLen + py*headW/2}
	right := f32.Point{X: p1.X - ux*headLen - px*headW/2, Y: p1.Y - uy*headLen - py*headW/2}

	var path clip.Path
	path.Begin(ops)
	path.MoveTo(tip)
	path.LineTo(left)
	path.LineTo(right)
	path.Close()

	st := clip.Outline{Path: path.End()}.Op().Push(ops)
	paint.ColorOp{Color: clr}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}

// --- Transform helpers ---

func (wv *WhiteboardView) screenToNorm(p f32.Point, cs image.Point) f32.Point {
	return f32.Point{
		X: (p.X - wv.panX) / (wv.zoom * float32(cs.X)),
		Y: (p.Y - wv.panY) / (wv.zoom * float32(cs.Y)),
	}
}

func (wv *WhiteboardView) normToScreen(nx, ny float32, cs image.Point) f32.Point {
	return f32.Point{
		X: nx*wv.zoom*float32(cs.X) + wv.panX,
		Y: ny*wv.zoom*float32(cs.Y) + wv.panY,
	}
}

// --- Utility ---

func isShapeTool(tool string) bool {
	return tool == "rect" || tool == "circle" || tool == "line" || tool == "arrow"
}

func isValidHexColor(s string) bool {
	if len(s) == 6 && s[0] != '#' {
		s = "#" + s
	}
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Toolbar helper: spacer
func wbSpacer(dp int) func(layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Spacer{Width: unit.Dp(dp)}.Layout(gtx)
	}
}

// Toolbar helper: vertical divider
func wbDivider(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Dp(1), gtx.Dp(20))
	paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
	return layout.Dimensions{Size: size}
}

// isAdmin kontroluje zda uživatel je owner nebo má ADMIN permission.
func (a *App) isAdmin(conn *ServerConnection) bool {
	if conn == nil {
		return false
	}
	for _, u := range conn.Users {
		if u.ID == conn.UserID {
			return u.IsOwner
		}
	}
	return conn.MyPermissions&512 != 0 // PermAdmin
}

// --- PNG Export ---

// exportPNG vykreslí strokes do image.RGBA a uloží jako PNG přes save dialog.
func (wv *WhiteboardView) exportPNG(strokes []api.WhiteboardStroke, canvasW, canvasH int) {
	if canvasW < 100 {
		canvasW = 1920
	}
	if canvasH < 100 {
		canvasH = 1080
	}

	img := renderStrokesToImage(strokes, canvasW, canvasH)

	path := saveFileDialog("whiteboard.png")
	if path == "" {
		return
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Printf("whiteboard: png encode: %v", err)
		return
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		log.Printf("whiteboard: write file: %v", err)
		return
	}
	log.Printf("whiteboard: exported to %s", path)
}

// renderStrokesToImage vykreslí všechny strokes do RGBA obrázku.
func renderStrokesToImage(strokes []api.WhiteboardStroke, width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	// Vyplnit pozadím (wbCanvasBg)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, wbCanvasBg)
		}
	}

	cs := image.Pt(width, height)

	for _, s := range strokes {
		if s.Tool == "text" {
			// Text strokes — renderovat jako jednoduché bloky bodů
			renderTextStrokeToImage(img, s, cs)
			continue
		}

		var points [][]float64
		if err := json.Unmarshal([]byte(s.PathData), &points); err != nil {
			continue
		}

		clr := parseHexColor(s.Color)
		if s.Tool == "eraser" {
			clr = wbCanvasBg
		}
		w := s.Width
		if w < 1 {
			w = 1
		}

		switch s.Tool {
		case "rect", "rect_fill", "circle", "circle_fill", "line", "arrow":
			if len(points) < 2 || len(points[0]) < 2 || len(points[1]) < 2 {
				continue
			}
			x0 := int(float64(points[0][0]) * float64(cs.X))
			y0 := int(float64(points[0][1]) * float64(cs.Y))
			x1 := int(float64(points[1][0]) * float64(cs.X))
			y1 := int(float64(points[1][1]) * float64(cs.Y))

			switch s.Tool {
			case "rect":
				imgDrawRectOutline(img, x0, y0, x1, y1, clr, w)
			case "rect_fill":
				imgDrawFilledRect(img, x0, y0, x1, y1, clr)
			case "circle":
				imgDrawEllipseOutline(img, x0, y0, x1, y1, clr, w)
			case "circle_fill":
				imgDrawFilledEllipse(img, x0, y0, x1, y1, clr)
			case "line":
				imgDrawLine(img, x0, y0, x1, y1, clr, w)
			case "arrow":
				imgDrawArrowLine(img, x0, y0, x1, y1, clr, w)
			}

		default:
			// pen / eraser — séria bodů
			if len(points) < 2 {
				continue
			}
			for i := 1; i < len(points); i++ {
				if len(points[i-1]) < 2 || len(points[i]) < 2 {
					continue
				}
				px0 := int(points[i-1][0] * float64(cs.X))
				py0 := int(points[i-1][1] * float64(cs.Y))
				px1 := int(points[i][0] * float64(cs.X))
				py1 := int(points[i][1] * float64(cs.Y))
				imgDrawLine(img, px0, py0, px1, py1, clr, w)
			}
		}
	}

	return img
}

// renderTextStrokeToImage renderuje text stroke jako obdélníkový blok barvy.
// Plný font rendering bez externích závislostí není možný, tak kreslíme marker.
func renderTextStrokeToImage(img *image.NRGBA, s api.WhiteboardStroke, cs image.Point) {
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
	px := int(td.X * float64(cs.X))
	py := int(td.Y * float64(cs.Y))
	sz := td.Size
	if sz < 8 {
		sz = 8
	}
	// Obdélníkový marker pro text (šířka úměrná délce textu)
	w := len(td.Text) * sz * 2 / 3
	h := sz + 4
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			imgSetPixelBlend(img, px+dx, py+dy, clr)
		}
	}
}

// --- Image kreslicí primitiva (Bresenham + thick lines) ---

// imgDrawLine kreslí čáru s danou šířkou do image.RGBA.
func imgDrawLine(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA, width int) {
	if width <= 1 {
		imgBresenham(img, x0, y0, x1, y1, clr)
		return
	}
	// Tlustá čára — kreslit kruhy (filled circles) podél Bresenhamovy čáry
	r := width / 2
	imgBresenhamThick(img, x0, y0, x1, y1, clr, r)
}

// imgBresenham kreslí 1px čáru (Bresenham algorithm).
func imgBresenham(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA) {
	dx := x1 - x0
	dy := y1 - y0
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}

	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}

	err := dx - dy
	for {
		imgSetPixelBlend(img, x0, y0, clr)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// imgBresenhamThick kreslí tlustou čáru — pro každý bod Bresenham vyplní kruh s poloměrem r.
func imgBresenhamThick(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA, r int) {
	dx := x1 - x0
	dy := y1 - y0
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}

	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}

	err := dx - dy
	for {
		imgFillCircle(img, x0, y0, r, clr)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// imgFillCircle vyplní kruh se středem (cx,cy) a poloměrem r.
func imgFillCircle(img *image.NRGBA, cx, cy, r int, clr color.NRGBA) {
	r2 := r * r
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r2 {
				imgSetPixelBlend(img, cx+dx, cy+dy, clr)
			}
		}
	}
}

// imgSetPixelBlend nastaví pixel s alpha blendingem.
func imgSetPixelBlend(img *image.NRGBA, x, y int, clr color.NRGBA) {
	bounds := img.Bounds()
	if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
		return
	}
	if clr.A == 255 {
		img.SetNRGBA(x, y, clr)
		return
	}
	if clr.A == 0 {
		return
	}
	// Alpha blending
	bg := img.NRGBAAt(x, y)
	a := float64(clr.A) / 255.0
	ia := 1.0 - a
	out := color.NRGBA{
		R: uint8(float64(clr.R)*a + float64(bg.R)*ia),
		G: uint8(float64(clr.G)*a + float64(bg.G)*ia),
		B: uint8(float64(clr.B)*a + float64(bg.B)*ia),
		A: 255,
	}
	img.SetNRGBA(x, y, out)
}

// imgDrawRectOutline kreslí obrys obdélníku.
func imgDrawRectOutline(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA, width int) {
	minX, maxX := x0, x1
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minY, maxY := y0, y1
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	imgDrawLine(img, minX, minY, maxX, minY, clr, width)
	imgDrawLine(img, maxX, minY, maxX, maxY, clr, width)
	imgDrawLine(img, maxX, maxY, minX, maxY, clr, width)
	imgDrawLine(img, minX, maxY, minX, minY, clr, width)
}

// imgDrawFilledRect kreslí vyplněný obdélník.
func imgDrawFilledRect(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA) {
	minX, maxX := x0, x1
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minY, maxY := y0, y1
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			imgSetPixelBlend(img, x, y, clr)
		}
	}
}

// imgDrawEllipseOutline kreslí obrys elipsy.
func imgDrawEllipseOutline(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA, width int) {
	cx := (x0 + x1) / 2
	cy := (y0 + y1) / 2
	rx := x1 - x0
	if rx < 0 {
		rx = -rx
	}
	rx /= 2
	ry := y1 - y0
	if ry < 0 {
		ry = -ry
	}
	ry /= 2
	if rx < 1 && ry < 1 {
		return
	}

	const segments = 64
	for i := 0; i < segments; i++ {
		a0 := float64(i) * 2 * math.Pi / segments
		a1 := float64(i+1) * 2 * math.Pi / segments
		px0 := cx + int(float64(rx)*math.Cos(a0))
		py0 := cy + int(float64(ry)*math.Sin(a0))
		px1 := cx + int(float64(rx)*math.Cos(a1))
		py1 := cy + int(float64(ry)*math.Sin(a1))
		imgDrawLine(img, px0, py0, px1, py1, clr, width)
	}
}

// imgDrawFilledEllipse kreslí vyplněnou elipsu.
func imgDrawFilledEllipse(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA) {
	cx := (x0 + x1) / 2
	cy := (y0 + y1) / 2
	rx := x1 - x0
	if rx < 0 {
		rx = -rx
	}
	rx /= 2
	ry := y1 - y0
	if ry < 0 {
		ry = -ry
	}
	ry /= 2
	if rx < 1 && ry < 1 {
		return
	}

	for dy := -ry; dy <= ry; dy++ {
		for dx := -rx; dx <= rx; dx++ {
			// Elipsa: (dx/rx)^2 + (dy/ry)^2 <= 1
			ex := float64(dx) / float64(rx)
			ey := float64(dy) / float64(ry)
			if ex*ex+ey*ey <= 1.0 {
				imgSetPixelBlend(img, cx+dx, cy+dy, clr)
			}
		}
	}
}

// imgDrawArrowLine kreslí čáru se šipkou na konci.
func imgDrawArrowLine(img *image.NRGBA, x0, y0, x1, y1 int, clr color.NRGBA, width int) {
	imgDrawLine(img, x0, y0, x1, y1, clr, width)

	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	length := math.Sqrt(dx*dx + dy*dy)
	if length < 2 {
		return
	}

	ux := dx / length
	uy := dy / length
	px := -uy
	py := ux

	headLen := float64(width) * 4
	if headLen < 10 {
		headLen = 10
	}
	headW := headLen * 0.6

	// Trojúhelník hrotu — vyplnit
	tipX, tipY := x1, y1
	lx := int(float64(x1) - ux*headLen + px*headW/2)
	ly := int(float64(y1) - uy*headLen + py*headW/2)
	rx := int(float64(x1) - ux*headLen - px*headW/2)
	ry := int(float64(y1) - uy*headLen - py*headW/2)

	imgFillTriangle(img, tipX, tipY, lx, ly, rx, ry, clr)
}

// imgFillTriangle vyplní trojúhelník (scanline).
func imgFillTriangle(img *image.NRGBA, x0, y0, x1, y1, x2, y2 int, clr color.NRGBA) {
	// Seřadit body dle Y
	if y0 > y1 {
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	if y0 > y2 {
		x0, y0, x2, y2 = x2, y2, x0, y0
	}
	if y1 > y2 {
		x1, y1, x2, y2 = x2, y2, x1, y1
	}

	for y := y0; y <= y2; y++ {
		var xa, xb float64
		if y < y1 {
			if y1-y0 > 0 {
				xa = float64(x0) + float64(x1-x0)*float64(y-y0)/float64(y1-y0)
			} else {
				xa = float64(x0)
			}
		} else {
			if y2-y1 > 0 {
				xa = float64(x1) + float64(x2-x1)*float64(y-y1)/float64(y2-y1)
			} else {
				xa = float64(x1)
			}
		}
		if y2-y0 > 0 {
			xb = float64(x0) + float64(x2-x0)*float64(y-y0)/float64(y2-y0)
		} else {
			xb = float64(x0)
		}
		if xa > xb {
			xa, xb = xb, xa
		}
		for x := int(xa); x <= int(xb); x++ {
			imgSetPixelBlend(img, x, y, clr)
		}
	}
}

package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"log"
	"sync"
	"sync/atomic"

	"nora-client/screen"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// StreamViewer displays a remote user's screen share stream.
type StreamViewer struct {
	app          *App
	Visible      bool
	StreamerID   string
	closeBtn     widget.Clickable
	currentFrame atomic.Pointer[image.NRGBA]

	// H.264 decoder
	decoder      *screen.Decoder
	decoderMu    sync.Mutex
	streamWidth  int
	streamHeight int
	useH264      bool
	decoderError string // chybová hláška pokud decoder selže (ffmpeg chybí)
}

func NewStreamViewer(a *App) *StreamViewer {
	return &StreamViewer{app: a}
}

// HandleMessage zpracovává příchozí DataChannel zprávu (H.264 nebo legacy JPEG).
func (sv *StreamViewer) HandleMessage(data []byte) {
	msgType, payload := screen.ParseMessage(data)

	switch msgType {
	case 0:
		// Legacy JPEG
		sv.decodeJPEG(payload)

	case screen.MsgMetadata:
		w, h, _, ok := screen.DecodeMetadata(payload)
		if !ok {
			return
		}
		sv.startDecoder(w, h)

	case screen.MsgH264Data:
		sv.decoderMu.Lock()
		dec := sv.decoder
		errStr := sv.decoderError
		sv.decoderMu.Unlock()

		if dec != nil {
			if err := dec.WriteData(payload); err != nil {
				log.Printf("stream viewer: decoder write: %v", err)
			}
		} else if errStr != "" {
			// Decoder selhal (ffmpeg chybí) — ignorovat H.264 data
		}
	}
}

// decodeJPEG dekóduje legacy JPEG frame.
func (sv *StreamViewer) decodeJPEG(data []byte) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	bounds := img.Bounds()
	nrgba := image.NewNRGBA(bounds)
	draw.Draw(nrgba, bounds, img, bounds.Min, draw.Src)
	sv.currentFrame.Store(nrgba)
	sv.app.Window.Invalidate()
}

// startDecoder spustí nový H.264 decoder (zavře předchozí pokud existuje).
func (sv *StreamViewer) startDecoder(w, h int) {
	sv.decoderMu.Lock()
	defer sv.decoderMu.Unlock()

	// Zavřít předchozí decoder
	if sv.decoder != nil {
		sv.decoder.Close()
		sv.decoder = nil
	}
	sv.decoderError = ""

	dec, err := screen.NewDecoder(w, h)
	if err != nil {
		log.Printf("stream viewer: decoder start failed (ffmpeg missing?): %v", err)
		sv.decoderError = "H.264 decoding requires ffmpeg"
		sv.app.Window.Invalidate()
		return
	}

	sv.decoder = dec
	sv.streamWidth = w
	sv.streamHeight = h
	sv.useH264 = true

	log.Printf("stream viewer: H.264 decoder started (%dx%d)", w, h)

	go sv.decoderReadLoop(dec)
}

// decoderReadLoop čte dekódované framy a ukládá je pro zobrazení.
func (sv *StreamViewer) decoderReadLoop(dec *screen.Decoder) {
	for frame := range dec.Frames() {
		sv.currentFrame.Store(frame)
		sv.app.Window.Invalidate()
	}
}

// StartWatching begins watching a streamer's screen share.
func (sv *StreamViewer) StartWatching(streamerID string) {
	sv.StreamerID = streamerID
	sv.Visible = true
	sv.currentFrame.Store(nil)
	sv.decoderMu.Lock()
	sv.decoderError = ""
	sv.decoderMu.Unlock()

	conn := sv.app.Conn()
	if conn != nil {
		conn.WS.SendJSON("screen.watch", map[string]any{
			"to":       streamerID,
			"watching": true,
		})
	}
}

// StopWatching stops watching and hides the viewer.
func (sv *StreamViewer) StopWatching() {
	if sv.StreamerID != "" {
		conn := sv.app.Conn()
		if conn != nil {
			conn.WS.SendJSON("screen.watch", map[string]any{
				"to":       sv.StreamerID,
				"watching": false,
			})
		}
	}
	sv.Visible = false
	sv.StreamerID = ""
	sv.currentFrame.Store(nil)

	// Cleanup decoder
	sv.decoderMu.Lock()
	if sv.decoder != nil {
		sv.decoder.Close()
		sv.decoder = nil
	}
	sv.useH264 = false
	sv.decoderMu.Unlock()
}

func (sv *StreamViewer) Layout(gtx layout.Context) layout.Dimensions {
	if sv.closeBtn.Clicked(gtx) {
		sv.StopWatching()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	streamerName := sv.StreamerID
	if u := sv.app.FindUser(sv.StreamerID); u != nil {
		streamerName = sv.app.ResolveUserName(u)
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							size := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
							paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: size}.Op())
							return layout.Dimensions{Size: size}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconMonitor, 18, ColorAccent)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(sv.app.Theme.Material, "Watching "+streamerName+"'s stream")
											lbl.Color = ColorText
											return lbl.Layout(gtx)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										size := gtx.Dp(28)
										return sv.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											gtx.Constraints.Min = image.Pt(size, size)
											gtx.Constraints.Max = gtx.Constraints.Min
											bg := color.NRGBA{A: 0}
											if sv.closeBtn.Hovered() {
												bg = withAlpha(ColorDanger, 60)
											}
											rr := size / 4
											paint.FillShape(gtx.Ops, bg, clip.RRect{
												Rect: image.Rect(0, 0, size, size),
												NE:   rr, NW: rr, SE: rr, SW: rr,
											}.Op(gtx.Ops))
											return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconClose, 18, ColorDanger)
											})
										})
									}),
								)
							})
						},
					)
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				// Stream image
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					// Chyba decoderu (ffmpeg chybí)
					sv.decoderMu.Lock()
					errStr := sv.decoderError
					sv.decoderMu.Unlock()
					if errStr != "" {
						return layoutCentered(gtx, sv.app.Theme, errStr, ColorDanger)
					}

					frame := sv.currentFrame.Load()
					if frame == nil {
						return layoutCentered(gtx, sv.app.Theme, "Waiting for stream...", ColorTextDim)
					}
					imgWidget := widget.Image{
						Src:      paint.NewImageOp(frame),
						Fit:      widget.Contain,
						Position: layout.Center,
					}
					return imgWidget.Layout(gtx)
				}),
			)
		},
	)
}

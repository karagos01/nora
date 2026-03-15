package ui

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/coder/websocket"
)

type GameConsoleView struct {
	app        *App
	Visible    bool
	ServerID   string
	ServerName string

	lines    []string
	logSel   widget.Selectable
	logList  widget.List
	cmdEd    widget.Editor
	sendBtn  widget.Clickable
	closeBtn widget.Clickable

	conn   *websocket.Conn
	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewGameConsoleView(a *App) *GameConsoleView {
	v := &GameConsoleView{app: a}
	v.logList.Axis = layout.Vertical
	v.cmdEd.SingleLine = true
	v.cmdEd.Submit = true
	return v
}

func (v *GameConsoleView) Open(serverID, serverName string, sc *ServerConnection) {
	v.Close()

	v.mu.Lock()
	v.Visible = true
	v.ServerID = serverID
	v.ServerName = serverName
	v.lines = nil
	v.mu.Unlock()

	// WS URL
	baseURL := sc.URL
	token := sc.Client.GetAccessToken()
	wsURL := fmt.Sprintf("%s/api/gameservers/%s/logs", baseURL, serverID)
	// http → ws
	if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}

	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel

	go v.connectAndStream(ctx, wsURL, token)
}

func (v *GameConsoleView) Close() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.mu.Lock()
	if v.conn != nil {
		v.conn.Close(websocket.StatusNormalClosure, "")
		v.conn = nil
	}
	v.Visible = false
	v.mu.Unlock()
}

func (v *GameConsoleView) connectAndStream(ctx context.Context, wsURL, token string) {
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + token},
		},
	})
	if err != nil {
		log.Printf("GameConsole WS dial error: %v", err)
		v.addLine("[connection error: " + err.Error() + "]")
		return
	}
	v.mu.Lock()
	v.conn = conn
	v.mu.Unlock()

	// Čtení zpráv
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				v.addLine("[disconnected]")
			}
			return
		}
		// Docker logs mohou obsahovat více řádků
		scanner := bufio.NewScanner(bytes.NewReader(msg))
		for scanner.Scan() {
			v.addLine(scanner.Text())
		}
	}
}

// stripANSI odstraní ANSI escape sekvence z textu (barvy, formátování)
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func (v *GameConsoleView) addLine(line string) {
	line = ansiRe.ReplaceAllString(line, "")
	v.mu.Lock()
	v.lines = append(v.lines, line)
	// Ring buffer: max 500 řádků
	if len(v.lines) > 500 {
		v.lines = v.lines[len(v.lines)-500:]
	}
	v.mu.Unlock()
	v.app.Window.Invalidate()
}

func (v *GameConsoleView) sendCommand(cmd string) {
	v.mu.Lock()
	conn := v.conn
	v.mu.Unlock()
	if conn == nil || cmd == "" {
		return
	}
	err := conn.Write(context.Background(), websocket.MessageText, []byte(cmd))
	if err != nil {
		log.Printf("GameConsole send error: %v", err)
	}
}

func (v *GameConsoleView) Layout(gtx layout.Context) layout.Dimensions {
	// Handle clicks
	if v.closeBtn.Clicked(gtx) {
		v.Close()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Handle send
	for {
		ev, ok := v.cmdEd.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			cmd := v.cmdEd.Text()
			v.cmdEd.SetText("")
			if cmd != "" {
				v.addLine("> " + cmd)
				go v.sendCommand(cmd)
			}
		}
	}
	if v.sendBtn.Clicked(gtx) {
		cmd := v.cmdEd.Text()
		v.cmdEd.SetText("")
		if cmd != "" {
			v.addLine("> " + cmd)
			go v.sendCommand(cmd)
		}
	}

	v.mu.Lock()
	lines := make([]string, len(v.lines))
	copy(lines, v.lines)
	title := "Console: " + v.ServerName
	v.mu.Unlock()

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
			paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body1(v.app.Theme.Material, title)
								lbl.Color = ColorText
								lbl.TextSize = unit.Sp(14)
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutSmallButton(gtx, v.app.Theme, "Close", ColorTextDim, v.closeBtn.Hovered())
								})
							}),
						)
					})
				}),
				// Log area
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
								rr := gtx.Dp(6)
								paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									if len(lines) == 0 {
										return layoutCentered(gtx, v.app.Theme, "Waiting for logs...", ColorTextDim)
									}
									logText := strings.Join(lines, "\n")
									v.logSel.SetText(logText)
									textMac := op.Record(gtx.Ops)
									paint.ColorOp{Color: ColorText}.Add(gtx.Ops)
									textCall := textMac.Stop()
									selMac := op.Record(gtx.Ops)
									paint.ColorOp{Color: ColorAccent}.Add(gtx.Ops)
									selCall := selMac.Stop()
									return material.List(v.app.Theme.Material, &v.logList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
										return v.logSel.Layout(gtx, v.app.Theme.Material.Shaper, font.Font{Typeface: v.app.Theme.Material.Face}, unit.Sp(12), textCall, selCall)
									})
								})
							},
						)
					})
				}),
				// Command input
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Background{}.Layout(gtx,
									func(gtx layout.Context) layout.Dimensions {
										bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
										rr := gtx.Dp(6)
										paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
										return layout.Dimensions{Size: bounds.Max}
									},
									func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											e := material.Editor(v.app.Theme.Material, &v.cmdEd, "Type command...")
											e.Color = ColorText
											e.HintColor = ColorTextDim
											e.TextSize = unit.Sp(13)
											return e.Layout(gtx)
										})
									},
								)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return v.sendBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutSmallButton(gtx, v.app.Theme, "Send", ColorAccent, v.sendBtn.Hovered())
									})
								})
							}),
						)
					})
				}),
			)
		},
	)
}


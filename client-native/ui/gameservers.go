package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/mount"
)

type FileExplorerState struct {
	Visible     bool
	ServerID    string
	ServerName  string
	CurrentPath string // "" = root
	Entries     []api.GameServerFileEntry
	Loading     bool
	Error       string

	list       widget.List
	backBtn    widget.Clickable
	closeBtn   widget.Clickable
	uploadBtn  widget.Clickable
	mkdirBtn   widget.Clickable
	refreshBtn widget.Clickable
	entryBtns  []widget.Clickable
	deleteBtns []widget.Clickable

	linkDirBtn widget.Clickable
	mountBtn   widget.Clickable
	unmountBtn widget.Clickable

	// Mkdir dialog
	showMkdir     bool
	mkdirEd       widget.Editor
	mkdirOkBtn    widget.Clickable
	mkdirCancelBtn widget.Clickable
}

type TextEditorState struct {
	Visible  bool
	ServerID string
	FilePath string
	Modified bool
	Error    string
	origText string

	editor   widget.Editor
	saveBtn  widget.Clickable
	closeBtn widget.Clickable

	// Syntax highlighting
	editMode         bool             // true = plain editor, false = highlighted view
	editBtn          widget.Clickable // toggle edit/view
	viewList         widget.List      // scrollable view mode
	lang             string           // chroma language name
	highlightedLines [][]coloredToken // cached per-line tokens
}

type GameServersView struct {
	app *App

	// Sidebar
	sideList    widget.List
	backBtn     widget.Clickable
	overviewBtn widget.Clickable

	// Main
	mainList widget.List
	newBtn   widget.Clickable

	// Per-instance buttons
	startBtns   []widget.Clickable
	stopBtns    []widget.Clickable
	restartBtns []widget.Clickable
	deleteBtns  []widget.Clickable
	consoleBtns []widget.Clickable
	filesBtns   []widget.Clickable

	// Error selectables (pro kopírování error messages)
	errorSels []widget.Selectable

	// Create dialog
	showCreate     bool
	createNameEd   widget.Editor
	createBtn      widget.Clickable
	cancelBtn      widget.Clickable
	presets        []api.GameServerPreset
	presetsLoaded  bool
	presetBtns     []widget.Clickable
	selectedPreset int

	// RCON
	rconEditors  []widget.Editor
	rconBtns     []widget.Clickable
	rconResponse map[string]string
	rconList     widget.List

	// Room (member tracking per server)
	gsMembers  map[string][]api.GameServerMember
	joinBtns   []widget.Clickable
	accessBtns []widget.Clickable

	// Stats cache
	statsCache map[string]*api.GameServerStats
	statsTime  map[string]time.Time
	statsMu    sync.Mutex

	// Console
	Console *GameConsoleView

	// File explorer + text editor
	fileExplorer *FileExplorerState
	textEditor   *TextEditorState
}

func NewGameServersView(a *App) *GameServersView {
	v := &GameServersView{
		app:          a,
		rconResponse: make(map[string]string),
		gsMembers:    make(map[string][]api.GameServerMember),
		statsCache:   make(map[string]*api.GameServerStats),
		statsTime:    make(map[string]time.Time),
		fileExplorer: &FileExplorerState{},
		textEditor:   &TextEditorState{},
	}
	v.sideList.Axis = layout.Vertical
	v.mainList.Axis = layout.Vertical
	v.rconList.Axis = layout.Vertical
	v.createNameEd.SingleLine = true
	v.createNameEd.Submit = true
	v.fileExplorer.list.Axis = layout.Vertical
	v.fileExplorer.mkdirEd.SingleLine = true
	v.textEditor.editor.SingleLine = false
	v.textEditor.editor.Submit = false
	v.textEditor.viewList.Axis = layout.Vertical
	v.Console = NewGameConsoleView(a)
	return v
}

// isGSMember zkontroluje zda uživatel je členem game server roomu
func (v *GameServersView) isGSMember(gsID, userID string) bool {
	for _, m := range v.gsMembers[gsID] {
		if m.UserID == userID {
			return true
		}
	}
	return false
}

// LoadMembers načte členy pro všechny game servery
func (v *GameServersView) LoadMembers(conn *ServerConnection) {
	v.app.mu.RLock()
	servers := make([]api.GameServerInstance, len(conn.GameServers))
	copy(servers, conn.GameServers)
	v.app.mu.RUnlock()

	for _, gs := range servers {
		gsID := gs.ID
		go func() {
			if members, err := conn.Client.GetGameServerMembers(gsID); err == nil {
				v.app.mu.Lock()
				if members == nil {
					members = []api.GameServerMember{}
				}
				v.gsMembers[gsID] = members
				v.app.mu.Unlock()
				v.app.Window.Invalidate()
			}
		}()
	}
}

func (v *GameServersView) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	if v.backBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewChannels
		v.app.mu.Unlock()
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
			paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconBack, 18, ColorTextDim)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body1(v.app.Theme.Material, "Game Servers")
										lbl.Color = ColorText
										lbl.TextSize = unit.Sp(14)
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.overviewBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := color.NRGBA{}
							if v.overviewBtn.Hovered() {
								bg = ColorHover
							}
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									if bg != (color.NRGBA{}) {
										bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
										paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
										return layout.Dimensions{Size: bounds.Max}
									}
									return layout.Dimensions{}
								},
								func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconStorage, 16, ColorAccent)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body2(v.app.Theme.Material, "Overview")
													lbl.Color = ColorText
													return lbl.Layout(gtx)
												})
											}),
										)
									})
								},
							)
						})
					})
				}),
			)
		},
	)
}

func (v *GameServersView) LayoutMain(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layoutCentered(gtx, v.app.Theme, "Not connected", ColorTextDim)
	}

	// Overlay priority: text editor > file explorer > console > server list

	// Text editor overlay
	if v.textEditor.Visible {
		return v.layoutTextEditor(gtx, conn)
	}

	// File explorer overlay
	if v.fileExplorer.Visible {
		return v.layoutFileExplorer(gtx, conn)
	}

	// Console overlay
	if v.Console.Visible {
		return v.Console.Layout(gtx)
	}

	// Handle clicks
	v.app.mu.RLock()
	servers := make([]api.GameServerInstance, len(conn.GameServers))
	copy(servers, conn.GameServers)
	v.app.mu.RUnlock()

	// Ensure button slices
	for len(v.startBtns) < len(servers) {
		v.startBtns = append(v.startBtns, widget.Clickable{})
		v.stopBtns = append(v.stopBtns, widget.Clickable{})
		v.restartBtns = append(v.restartBtns, widget.Clickable{})
		v.deleteBtns = append(v.deleteBtns, widget.Clickable{})
		v.consoleBtns = append(v.consoleBtns, widget.Clickable{})
		v.filesBtns = append(v.filesBtns, widget.Clickable{})
		v.errorSels = append(v.errorSels, widget.Selectable{})
		v.joinBtns = append(v.joinBtns, widget.Clickable{})
		v.accessBtns = append(v.accessBtns, widget.Clickable{})
		ed := widget.Editor{}
		ed.SingleLine = true
		ed.Submit = true
		v.rconEditors = append(v.rconEditors, ed)
		v.rconBtns = append(v.rconBtns, widget.Clickable{})
	}

	// Handle button clicks
	for i, gs := range servers {
		if i < len(v.startBtns) && v.startBtns[i].Clicked(gtx) {
			gsID := gs.ID
			go func() {
				if err := conn.Client.StartGameServer(gsID); err != nil {
					log.Printf("Start game server error: %v", err)
					v.app.Toasts.Error("Failed to start game server")
				}
			}()
		}
		if i < len(v.stopBtns) && v.stopBtns[i].Clicked(gtx) {
			gsID := gs.ID
			go func() {
				if err := conn.Client.StopGameServer(gsID); err != nil {
					log.Printf("Stop game server error: %v", err)
					v.app.Toasts.Error("Failed to stop game server")
				}
			}()
		}
		if i < len(v.restartBtns) && v.restartBtns[i].Clicked(gtx) {
			gsID := gs.ID
			go func() {
				if err := conn.Client.RestartGameServer(gsID); err != nil {
					log.Printf("Restart game server error: %v", err)
					v.app.Toasts.Error("Failed to restart game server")
				}
			}()
		}
		if i < len(v.deleteBtns) && v.deleteBtns[i].Clicked(gtx) {
			gsID := gs.ID
			gsName := gs.Name
			v.app.ConfirmDlg.Show("Delete Server", fmt.Sprintf("Delete game server '%s'? This will stop and remove the container and all data.", gsName), func() {
				go func() {
					if err := conn.Client.DeleteGameServer(gsID); err != nil {
						log.Printf("Delete game server error: %v", err)
						v.app.Toasts.Error("Failed to delete game server")
						return
					}
					if servers, err := conn.Client.GetGameServers(); err == nil {
						v.app.mu.Lock()
						conn.GameServers = servers
						v.app.mu.Unlock()
						v.app.Window.Invalidate()
					}
				}()
			})
		}
		if i < len(v.consoleBtns) && v.consoleBtns[i].Clicked(gtx) {
			v.Console.Open(gs.ID, gs.Name, conn)
		}
		if i < len(v.filesBtns) && v.filesBtns[i].Clicked(gtx) {
			v.openFileExplorer(gs.ID, gs.Name, conn)
		}
		// RCON send button
		if i < len(v.rconBtns) && v.rconBtns[i].Clicked(gtx) {
			v.submitRCON(gs.ID, i, conn)
		}
		// RCON editor submit (Enter)
		if i < len(v.rconEditors) {
			for {
				ev, ok := v.rconEditors[i].Update(gtx)
				if !ok {
					break
				}
				if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
					v.submitRCON(gs.ID, i, conn)
				}
			}
		}
		if i < len(v.joinBtns) && v.joinBtns[i].Clicked(gtx) {
			gsID := gs.ID
			isMember := v.isGSMember(gsID, conn.UserID)
			go func() {
				if isMember {
					conn.Client.LeaveGameServer(gsID)
				} else {
					conn.Client.JoinGameServer(gsID)
				}
			}()
		}
		if i < len(v.accessBtns) && v.accessBtns[i].Clicked(gtx) {
			gsID := gs.ID
			newMode := "room"
			if gs.AccessMode == "room" {
				newMode = "open"
			}
			go func() {
				conn.Client.SetGameServerAccess(gsID, newMode)
			}()
		}
	}

	// New button
	if v.newBtn.Clicked(gtx) {
		v.showCreate = true
		v.createNameEd.SetText("")
		v.selectedPreset = 0
		// Načti presety ze serveru
		if !v.presetsLoaded {
			go func() {
				if presets, err := conn.Client.GetGameServerPresets(); err == nil && len(presets) > 0 {
					v.presets = presets
					v.presetBtns = make([]widget.Clickable, len(presets))
					v.presetsLoaded = true
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	// Create dialog buttons
	if v.showCreate {
		if v.cancelBtn.Clicked(gtx) {
			v.showCreate = false
		}
		// Preset clicks — při výběru presetu auto-fill jméno pokud je prázdné
		for i := range v.presetBtns {
			if v.presetBtns[i].Clicked(gtx) {
				v.selectedPreset = i
				if strings.TrimSpace(v.createNameEd.Text()) == "" && i < len(v.presets) {
					v.createNameEd.SetText(capitalizeFirst(v.presets[i].Name))
				}
			}
		}
		// Potvrzení přes tlačítko Create nebo Enter v name poli
		submitCreate := v.createBtn.Clicked(gtx)
		for {
			ev, ok := v.createNameEd.Update(gtx)
			if !ok {
				break
			}
			if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
				submitCreate = true
			}
		}
		if submitCreate {
			name := strings.TrimSpace(v.createNameEd.Text())
			preset := "minecraft"
			if v.selectedPreset < len(v.presets) {
				preset = v.presets[v.selectedPreset].Name
			}
			if name == "" {
				name = capitalizeFirst(preset)
			}
			{
				v.showCreate = false
				go func() {
					_, err := conn.Client.CreateGameServer(name, preset)
					if err != nil {
						log.Printf("Create game server error: %v", err)
						v.app.Toasts.Error("Failed to create game server")
						return
					}
					// Refresh seznam serverů
					if servers, err := conn.Client.GetGameServers(); err == nil {
						v.app.mu.Lock()
						conn.GameServers = servers
						v.app.mu.Unlock()
						v.app.Window.Invalidate()
					}
				}()
			}
		}
	}

	// Fetch stats for running servers (cached for 5s)
	for _, gs := range servers {
		if gs.Status == "running" {
			v.statsMu.Lock()
			lastFetch, ok := v.statsTime[gs.ID]
			v.statsMu.Unlock()
			if !ok || time.Since(lastFetch) > 5*time.Second {
				gsID := gs.ID
				go func() {
					stats, err := conn.Client.GetGameServerStats(gsID)
					if err == nil {
						v.statsMu.Lock()
						v.statsCache[gsID] = stats
						v.statsTime[gsID] = time.Now()
						v.statsMu.Unlock()
						v.app.Window.Invalidate()
					}
				}()
				v.statsMu.Lock()
				v.statsTime[gs.ID] = time.Now()
				v.statsMu.Unlock()
			}
		}
	}

	// Create dialog overlay
	if v.showCreate {
		return v.layoutCreateDialog(gtx)
	}

	// Main layout
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
					return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(12), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								lbl := material.H6(v.app.Theme.Material, "Game Servers")
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.newBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutButton(gtx, v.app.Theme, "New", ColorAccent, v.newBtn.Hovered())
								})
							}),
						)
					})
				}),
				// Server list
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if len(servers) == 0 {
						return layoutCentered(gtx, v.app.Theme, "No game servers yet. Click 'New' to create one.", ColorTextDim)
					}
					return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.List(v.app.Theme.Material, &v.mainList).Layout(gtx, len(servers), func(gtx layout.Context, i int) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutServerCard(gtx, servers[i], i)
							})
						})
					})
				}),
			)
		},
	)
}

func (v *GameServersView) layoutServerCard(gtx layout.Context, gs api.GameServerInstance, idx int) layout.Dimensions {
	conn := v.app.Conn()
	var statusColor color.NRGBA
	statusText := gs.Status
	switch gs.Status {
	case "running":
		statusColor = ColorOnline
	case "starting":
		statusColor = ColorAccent
	case "error":
		statusColor = ColorDanger
	default:
		statusColor = ColorTextDim
	}

	// Zjistit zda uživatel je admin
	isAdmin := conn != nil && v.app.isAdmin(conn)

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
				Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					// Name + status + access mode
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body1(v.app.Theme.Material, gs.Name)
										lbl.Color = ColorText
										lbl.TextSize = unit.Sp(15)
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if gs.AccessMode != "room" {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconLock, 14, ColorAccent)
										})
									}),
								)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										size := gtx.Dp(8)
										bounds := image.Rect(0, 0, size, size)
										rr := size / 2
										paint.FillShape(gtx.Ops, statusColor, clip.RRect{
											Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
										}.Op(gtx.Ops))
										return layout.Dimensions{Size: bounds.Max}
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(v.app.Theme.Material, statusText)
											lbl.Color = statusColor
											return lbl.Layout(gtx)
										})
									}),
								)
							}),
						)
					}),
					// Stats (only for running)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if gs.Status != "running" {
							if gs.ErrorMsg != "" {
								return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									v.errorSels[idx].SetText(gs.ErrorMsg)
									textMac := op.Record(gtx.Ops)
									paint.ColorOp{Color: ColorDanger}.Add(gtx.Ops)
									textCall := textMac.Stop()
									selMac := op.Record(gtx.Ops)
									paint.ColorOp{Color: color.NRGBA{R: 255, G: 80, B: 80, A: 60}}.Add(gtx.Ops)
									selCall := selMac.Stop()
									return v.errorSels[idx].Layout(gtx, v.app.Theme.Material.Shaper, font.Font{Typeface: v.app.Theme.Material.Face}, unit.Sp(11), textCall, selCall)
								})
							}
							return layout.Dimensions{}
						}
						v.statsMu.Lock()
						stats := v.statsCache[gs.ID]
						v.statsMu.Unlock()
						if stats == nil {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							info := fmt.Sprintf("CPU %s  ·  %s / %s  ·  Net %s", stats.CPUPercent, stats.MemUsage, stats.MemLimit, stats.NetIO)
							lbl := material.Body2(v.app.Theme.Material, info)
							lbl.Color = ColorTextDim
							lbl.TextSize = unit.Sp(11)
							return lbl.Layout(gtx)
						})
					}),
					// Members (room mode or has members)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						members := v.gsMembers[gs.ID]
						if len(members) == 0 && gs.AccessMode != "room" {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							var items []layout.FlexChild
							memberCount := fmt.Sprintf("Members (%d)", len(members))
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, memberCount)
								lbl.Color = ColorTextDim
								lbl.TextSize = unit.Sp(11)
								return lbl.Layout(gtx)
							}))
							for _, m := range members {
								username := m.Username
								uid := m.UserID
								items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												// Online tečka
												dotColor := ColorTextDim
												if conn != nil && conn.OnlineUsers[uid] {
													dotColor = ColorOnline
												}
												size := gtx.Dp(6)
												bounds := image.Rect(0, 0, size, size)
												rr := size / 2
												paint.FillShape(gtx.Ops, dotColor, clip.RRect{
													Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
												}.Op(gtx.Ops))
												return layout.Dimensions{Size: bounds.Max}
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body2(v.app.Theme.Material, username)
													lbl.Color = ColorText
													lbl.TextSize = unit.Sp(11)
													return lbl.Layout(gtx)
												})
											}),
										)
									})
								}))
							}
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
						})
					}),
					// Buttons
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							var btns []layout.FlexChild

							// Files button — vždy viditelný
							btns = append(btns,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return v.filesBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutSmallButton(gtx, v.app.Theme, "Files", ColorAccent, v.filesBtns[idx].Hovered())
									})
								}),
							)

							// Join/Leave — vždy viditelný
							isMember := conn != nil && v.isGSMember(gs.ID, conn.UserID)
							btns = append(btns,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										label := "Join"
										clr := ColorOnline
										if isMember {
											label = "Leave"
											clr = ColorDanger
										}
										return v.joinBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutSmallButton(gtx, v.app.Theme, label, clr, v.joinBtns[idx].Hovered())
										})
									})
								}),
							)

							// Lock/Unlock — jen admin
							if isAdmin {
								btns = append(btns,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											label := "Lock"
											clr := ColorAccent
											if gs.AccessMode == "room" {
												label = "Unlock"
												clr = ColorTextDim
											}
											return v.accessBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, label, clr, v.accessBtns[idx].Hovered())
											})
										})
									}),
								)
							}

							switch gs.Status {
							case "running":
								btns = append(btns,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.stopBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Stop", ColorDanger, v.stopBtns[idx].Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.restartBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Restart", ColorAccent, v.restartBtns[idx].Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.consoleBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Console", ColorTextDim, v.consoleBtns[idx].Hovered())
											})
										})
									}),
								)
							case "stopped", "error":
								btns = append(btns,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.startBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Start", ColorOnline, v.startBtns[idx].Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.deleteBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Delete", ColorDanger, v.deleteBtns[idx].Hovered())
											})
										})
									}),
								)
							case "starting":
								btns = append(btns,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(v.app.Theme.Material, "Starting...")
											lbl.Color = ColorAccent
											lbl.TextSize = unit.Sp(12)
											return lbl.Layout(gtx)
										})
									}),
								)
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx, btns...)
						})
					}),
					// RCON sekce (jen pro running servery)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if gs.Status != "running" || !isAdmin {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Label
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, "RCON Command")
									lbl.Color = ColorTextDim
									lbl.TextSize = unit.Sp(11)
									return lbl.Layout(gtx)
								}),
								// Input + Send
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return layout.Background{}.Layout(gtx,
													func(gtx layout.Context) layout.Dimensions {
														bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
														rr := gtx.Dp(4)
														paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
														return layout.Dimensions{Size: bounds.Max}
													},
													func(gtx layout.Context) layout.Dimensions {
														return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															e := material.Editor(v.app.Theme.Material, &v.rconEditors[idx], "e.g. list, say hello")
															e.Color = ColorText
															e.HintColor = ColorTextDim
															e.TextSize = unit.Sp(12)
															e.Font.Typeface = "Go Mono"
															return e.Layout(gtx)
														})
													},
												)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return v.rconBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														return layoutSmallButton(gtx, v.app.Theme, "Send", ColorAccent, v.rconBtns[idx].Hovered())
													})
												})
											}),
										)
									})
								}),
								// Response
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									resp := v.rconResponse[gs.ID]
									if resp == "" {
										return layout.Dimensions{}
									}
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Background{}.Layout(gtx,
											func(gtx layout.Context) layout.Dimensions {
												bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
												rr := gtx.Dp(4)
												paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
												return layout.Dimensions{Size: bounds.Max}
											},
											func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													// Maximální výška pro response
													gtx.Constraints.Max.Y = gtx.Dp(120)
													lbl := material.Body2(v.app.Theme.Material, resp)
													lbl.Color = ColorText
													lbl.TextSize = unit.Sp(11)
													lbl.Font.Typeface = "Go Mono"
													return lbl.Layout(gtx)
												})
											},
										)
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

// submitRCON odešle RCON příkaz na game server
func (v *GameServersView) submitRCON(gsID string, idx int, conn *ServerConnection) {
	if idx >= len(v.rconEditors) {
		return
	}
	cmd := strings.TrimSpace(v.rconEditors[idx].Text())
	if cmd == "" {
		return
	}
	v.rconEditors[idx].SetText("")
	v.rconResponse[gsID] = "Sending..."
	go func() {
		resp, err := conn.Client.GameServerRCON(gsID, cmd)
		v.app.mu.Lock()
		if err != nil {
			v.rconResponse[gsID] = "Error: " + err.Error()
		} else if resp == "" {
			v.rconResponse[gsID] = "(empty response)"
		} else {
			v.rconResponse[gsID] = resp
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *GameServersView) layoutCreateDialog(gtx layout.Context) layout.Dimensions {
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
			paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(400)
			gtx.Constraints.Min.X = gtx.Dp(400)
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(12)
					paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
						Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Title
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.H6(v.app.Theme.Material, "Create Game Server")
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							}),
							// Name
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return v.layoutField(gtx, "Name", &v.createNameEd, "My Server")
								})
							}),
							// Preset label
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, "Preset")
									lbl.Color = ColorTextDim
									lbl.TextSize = unit.Sp(12)
									return lbl.Layout(gtx)
								})
							}),
							// Preset buttons
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if len(v.presets) == 0 {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(v.app.Theme.Material, "Loading...")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}
								return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return v.layoutPresetGrid(gtx)
								})
							}),
							// Buttons
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Spacing: layout.SpaceEnd, Alignment: layout.Middle}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Dimensions{}
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return v.cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Cancel", ColorTextDim, v.cancelBtn.Hovered())
											})
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.createBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layoutButton(gtx, v.app.Theme, "Create", ColorAccent, v.createBtn.Hovered())
												})
											})
										}),
									)
								})
							}),
						)
					})
				},
			)
		}),
	)
}

func (v *GameServersView) layoutPresetGrid(gtx layout.Context) layout.Dimensions {
	// Preset tlačítka ve wrapping flow layout (3 na řádek)
	const perRow = 3
	var rows []layout.FlexChild
	for rowStart := 0; rowStart < len(v.presets); rowStart += perRow {
		start := rowStart
		end := start + perRow
		if end > len(v.presets) {
			end = len(v.presets)
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var cols []layout.FlexChild
			for i := start; i < end; i++ {
				idx := i
				cols = append(cols, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.presetBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							selected := idx == v.selectedPreset
							name := capitalizeFirst(v.presets[idx].Name)
							return v.layoutPresetBtn(gtx, name, selected, v.presetBtns[idx].Hovered())
						})
					})
				}))
			}
			// Padding pro neúplný řádek
			for i := end - start; i < perRow; i++ {
				cols = append(cols, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{}
				}))
			}
			return layout.Flex{}.Layout(gtx, cols...)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (v *GameServersView) layoutPresetBtn(gtx layout.Context, name string, selected, hovered bool) layout.Dimensions {
	bgColor := ColorInput
	if selected {
		bgColor = ColorAccent
	} else if hovered {
		bgColor = color.NRGBA{R: 70, G: 70, B: 80, A: 255}
	}
	textColor := ColorTextDim
	if selected {
		textColor = ColorText
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, bgColor, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, name)
				lbl.Color = textColor
				lbl.TextSize = unit.Sp(12)
				lbl.Alignment = text.Middle
				return lbl.Layout(gtx)
			})
		},
	)
}

// --- File Explorer ---

func (v *GameServersView) openFileExplorer(serverID, serverName string, conn *ServerConnection) {
	fe := v.fileExplorer
	fe.Visible = true
	fe.ServerID = serverID
	fe.ServerName = serverName
	fe.CurrentPath = ""
	fe.Error = ""
	fe.Loading = true
	go v.loadFileExplorerEntries(conn)
}

func (v *GameServersView) loadFileExplorerEntries(conn *ServerConnection) {
	fe := v.fileExplorer
	entries, err := conn.Client.ListGameServerFiles(fe.ServerID, fe.CurrentPath)
	v.app.mu.Lock()
	if err != nil {
		fe.Error = err.Error()
		fe.Entries = nil
	} else {
		fe.Error = ""
		fe.Entries = entries
	}
	fe.Loading = false
	v.app.mu.Unlock()
	v.app.Window.Invalidate()
}

func (v *GameServersView) layoutFileExplorer(gtx layout.Context, conn *ServerConnection) layout.Dimensions {
	fe := v.fileExplorer

	// Handle close
	if fe.closeBtn.Clicked(gtx) {
		fe.Visible = false
		return layout.Dimensions{}
	}

	// Handle back
	if fe.backBtn.Clicked(gtx) && fe.CurrentPath != "" {
		fe.CurrentPath = filepath.Dir(fe.CurrentPath)
		if fe.CurrentPath == "." {
			fe.CurrentPath = ""
		}
		fe.Loading = true
		go v.loadFileExplorerEntries(conn)
	}

	// Handle refresh
	if fe.refreshBtn.Clicked(gtx) {
		fe.Loading = true
		go v.loadFileExplorerEntries(conn)
	}

	// Handle link directory — načte rekurzivně soubory a odešle jako attachmenty
	if fe.linkDirBtn.Clicked(gtx) {
		serverID := fe.ServerID
		currentPath := fe.CurrentPath
		go func() {
			entries, err := conn.Client.ListGameServerFilesRecursive(serverID, currentPath)
			if err != nil {
				log.Printf("ListFilesRecursive error: %v", err)
				return
			}
			if len(entries) == 0 {
				return
			}
			var results []api.UploadResult
			for _, e := range entries {
				dlURL := conn.Client.GameServerFileDownloadURL(serverID, filepath.Join(currentPath, e.RelPath))
				results = append(results, api.UploadResult{
					Filename: e.RelPath,
					Original: e.RelPath,
					URL:      dlURL,
					Size:     e.Size,
				})
			}
			// Odeslat přímo do aktivního kanálu bez upload dialogu
			v.app.mu.Lock()
			v.app.Mode = ViewChannels
			v.app.mu.Unlock()
			chID := conn.ActiveChannelID
			if chID == "" {
				return
			}
			if _, err := conn.Client.SendMessageWithAttachments(chID, "", "", results); err != nil {
				log.Printf("SendMessageWithAttachments error: %v", err)
			}
		}()
	}

	// Handle mount
	if fe.mountBtn.Clicked(gtx) {
		serverID := fe.ServerID
		serverName := fe.ServerName
		go func() {
			gsfs := mount.NewGameServerFS(serverID, serverName, conn.URL, conn.Client, conn.Mounts.Cache())
			info, err := conn.Mounts.MountFS("gs-"+serverID, serverName, gsfs)
			if err != nil {
				log.Printf("Mount game server error: %v", err)
			} else {
				log.Printf("Mounted game server %s at %s", serverName, info.Path)
			}
			v.app.Window.Invalidate()
		}()
	}

	// Handle unmount
	if fe.unmountBtn.Clicked(gtx) {
		serverID := fe.ServerID
		go func() {
			if err := conn.Mounts.Unmount("gs-" + serverID); err != nil {
				log.Printf("Unmount game server error: %v", err)
			}
			v.app.Window.Invalidate()
		}()
	}

	// Handle mkdir
	if fe.mkdirBtn.Clicked(gtx) {
		fe.showMkdir = true
		fe.mkdirEd.SetText("")
	}
	if fe.showMkdir {
		if fe.mkdirCancelBtn.Clicked(gtx) {
			fe.showMkdir = false
		}
		if fe.mkdirOkBtn.Clicked(gtx) {
			name := fe.mkdirEd.Text()
			if name != "" {
				fe.showMkdir = false
				path := name
				if fe.CurrentPath != "" {
					path = fe.CurrentPath + "/" + name
				}
				go func() {
					if err := conn.Client.MkdirGameServer(fe.ServerID, path); err != nil {
						log.Printf("Mkdir error: %v", err)
					}
					v.loadFileExplorerEntries(conn)
				}()
			}
		}
	}

	// Handle upload
	if fe.uploadBtn.Clicked(gtx) {
		go func() {
			filePath := openFileDialog()
			if filePath == "" {
				return
			}
			data, err := readFileBytes(filePath)
			if err != nil {
				log.Printf("Read file error: %v", err)
				return
			}
			filename := filepath.Base(filePath)
			uploadPath := fe.CurrentPath
			if uploadPath == "" {
				uploadPath = "."
			}
			if err := conn.Client.UploadGameServerFile(fe.ServerID, uploadPath, filename, data); err != nil {
				log.Printf("Upload error: %v", err)
			}
			v.loadFileExplorerEntries(conn)
		}()
	}

	// Ensure entry button slices
	for len(fe.entryBtns) < len(fe.Entries) {
		fe.entryBtns = append(fe.entryBtns, widget.Clickable{})
		fe.deleteBtns = append(fe.deleteBtns, widget.Clickable{})
	}

	// Handle entry clicks
	for i, entry := range fe.Entries {
		if i < len(fe.deleteBtns) && fe.deleteBtns[i].Clicked(gtx) {
			entryName := entry.Name
			path := entryName
			if fe.CurrentPath != "" {
				path = fe.CurrentPath + "/" + entryName
			}
			v.app.ConfirmDlg.Show("Delete", fmt.Sprintf("Delete '%s'?", entryName), func() {
				go func() {
					if err := conn.Client.DeleteGameServerFile(fe.ServerID, path); err != nil {
						log.Printf("Delete error: %v", err)
					}
					v.loadFileExplorerEntries(conn)
				}()
			})
		}
		if i < len(fe.entryBtns) && fe.entryBtns[i].Clicked(gtx) {
			if entry.IsDir {
				// Navigate into directory
				if fe.CurrentPath == "" {
					fe.CurrentPath = entry.Name
				} else {
					fe.CurrentPath = fe.CurrentPath + "/" + entry.Name
				}
				fe.Loading = true
				go v.loadFileExplorerEntries(conn)
			} else if isTextFile(entry.Name) {
				// Open in text editor
				path := entry.Name
				if fe.CurrentPath != "" {
					path = fe.CurrentPath + "/" + entry.Name
				}
				v.openTextEditor(fe.ServerID, path, conn)
			}
		}
	}

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
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									// Back button
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if fe.CurrentPath == "" {
											return layout.Dimensions{}
										}
										return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return fe.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconBack, 18, ColorTextDim)
											})
										})
									}),
									// Breadcrumb
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										path := fe.ServerName
										if fe.CurrentPath != "" {
											path += " / " + strings.ReplaceAll(fe.CurrentPath, "/", " / ")
										}
										lbl := material.Body1(v.app.Theme.Material, path)
										lbl.Color = ColorText
										lbl.TextSize = unit.Sp(14)
										return lbl.Layout(gtx)
									}),
								)
							}),
							// Action buttons
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return fe.uploadBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutSmallButton(gtx, v.app.Theme, "Upload", ColorAccent, fe.uploadBtn.Hovered())
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return fe.mkdirBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "New Folder", ColorAccent, fe.mkdirBtn.Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return fe.linkDirBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Link Dir", ColorAccent, fe.linkDirBtn.Hovered())
											})
										})
									}),
									// Mount / Unmount
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										mounted := conn.Mounts.IsMounted("gs-" + fe.ServerID)
										if mounted {
											info := conn.Mounts.GetMountInfo("gs-" + fe.ServerID)
											return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														return fe.unmountBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															return layoutSmallButton(gtx, v.app.Theme, "Unmount", ColorDanger, fe.unmountBtn.Hovered())
														})
													})
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													if info == nil {
														return layout.Dimensions{}
													}
													return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Body2(v.app.Theme.Material, info.Path)
														lbl.Color = ColorTextDim
														lbl.TextSize = unit.Sp(11)
														return lbl.Layout(gtx)
													})
												}),
											)
										}
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return fe.mountBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Mount", ColorAccent, fe.mountBtn.Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return fe.refreshBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Refresh", ColorTextDim, fe.refreshBtn.Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return fe.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Close", ColorTextDim, fe.closeBtn.Hovered())
											})
										})
									}),
								)
							}),
						)
					})
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				// Mkdir dialog
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !fe.showMkdir {
						return layout.Dimensions{}
					}
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, "Folder name:")
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									gtx.Constraints.Max.X = gtx.Dp(200)
									return layout.Background{}.Layout(gtx,
										func(gtx layout.Context) layout.Dimensions {
											bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
											rr := gtx.Dp(4)
											paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
											return layout.Dimensions{Size: bounds.Max}
										},
										func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												e := material.Editor(v.app.Theme.Material, &fe.mkdirEd, "new-folder")
												e.Color = ColorText
												e.HintColor = ColorTextDim
												e.TextSize = unit.Sp(13)
												return e.Layout(gtx)
											})
										},
									)
								})
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return fe.mkdirOkBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutSmallButton(gtx, v.app.Theme, "OK", ColorAccent, fe.mkdirOkBtn.Hovered())
									})
								})
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return fe.mkdirCancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutSmallButton(gtx, v.app.Theme, "Cancel", ColorTextDim, fe.mkdirCancelBtn.Hovered())
									})
								})
							}),
						)
					})
				}),
				// Error
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if fe.Error == "" {
						return layout.Dimensions{}
					}
					return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(v.app.Theme.Material, fe.Error)
						lbl.Color = ColorDanger
						lbl.TextSize = unit.Sp(12)
						return lbl.Layout(gtx)
					})
				}),
				// File list
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if fe.Loading {
						return layoutCentered(gtx, v.app.Theme, "Loading...", ColorTextDim)
					}
					if len(fe.Entries) == 0 {
						msg := fmt.Sprintf("Empty directory (server=%s, path=%q)", fe.ServerID, fe.CurrentPath)
						return layoutCentered(gtx, v.app.Theme, msg, ColorTextDim)
					}
					return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.List(v.app.Theme.Material, &fe.list).Layout(gtx, len(fe.Entries), func(gtx layout.Context, i int) layout.Dimensions {
							entry := fe.Entries[i]
							return v.layoutFileEntry(gtx, entry, i)
						})
					})
				}),
			)
		},
	)
}

func (v *GameServersView) layoutFileEntry(gtx layout.Context, entry api.GameServerFileEntry, idx int) layout.Dimensions {
	fe := v.fileExplorer
	hovered := fe.entryBtns[idx].Hovered()

	return fe.entryBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Změříme obsah přes op.Record, pak nakreslíme pozadí + obsah
		macro := op.Record(gtx.Ops)
		dims := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
				// Icon + name
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							icon := IconFile
							clr := ColorTextDim
							if entry.IsDir {
								icon = IconFolder
								clr = ColorAccent
							}
							return layoutIcon(gtx, icon, 16, clr)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, entry.Name)
								lbl.Color = ColorText
								lbl.TextSize = unit.Sp(13)
								return lbl.Layout(gtx)
							})
						}),
					)
				}),
				// Size + delete
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if entry.IsDir {
								return layout.Dimensions{}
							}
							lbl := material.Body2(v.app.Theme.Material, formatFileSize(entry.Size))
							lbl.Color = ColorTextDim
							lbl.TextSize = unit.Sp(11)
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return fe.deleteBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconDelete, 14, ColorDanger)
								})
							})
						}),
					)
				}),
			)
		})
		call := macro.Stop()

		// Nakreslíme pozadí (hover)
		if hovered {
			rr := gtx.Dp(4)
			paint.FillShape(gtx.Ops, ColorHover, clip.RRect{
				Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
		}
		call.Add(gtx.Ops)
		return dims
	})
}

// --- Text Editor ---

func (v *GameServersView) openTextEditor(serverID, path string, conn *ServerConnection) {
	te := v.textEditor
	te.Visible = true
	te.ServerID = serverID
	te.FilePath = path
	te.Modified = false
	te.Error = ""
	te.editMode = false
	te.lang = extToLang(path)
	te.highlightedLines = nil
	te.editor.SetText("Loading...")

	go func() {
		content, err := conn.Client.ReadGameServerFile(serverID, path)
		v.app.mu.Lock()
		if err != nil {
			te.Error = err.Error()
			te.editor.SetText("")
		} else {
			te.editor.SetText(content)
			te.origText = content
			te.buildHighlight()
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *GameServersView) layoutTextEditor(gtx layout.Context, conn *ServerConnection) layout.Dimensions {
	te := v.textEditor

	// Detect modification (jen v edit mode)
	if te.editMode {
		for {
			_, ok := te.editor.Update(gtx)
			if !ok {
				break
			}
			te.Modified = te.editor.Text() != te.origText
		}
	}

	// Handle edit/view toggle
	if te.editBtn.Clicked(gtx) {
		te.editMode = !te.editMode
		if !te.editMode {
			// Přepnout na view mode — rebuild highlight z aktuálního textu
			te.buildHighlight()
		}
	}

	// Handle save
	if te.saveBtn.Clicked(gtx) && te.editMode && te.Modified {
		content := te.editor.Text()
		serverID := te.ServerID
		path := te.FilePath
		go func() {
			err := conn.Client.WriteGameServerFile(serverID, path, content)
			v.app.mu.Lock()
			if err != nil {
				te.Error = err.Error()
			} else {
				te.Error = ""
				te.Modified = false
				te.origText = content
				te.editMode = false
				te.buildHighlight()
			}
			v.app.mu.Unlock()
			v.app.Window.Invalidate()
		}()
	}

	// Handle close
	if te.closeBtn.Clicked(gtx) {
		if te.Modified {
			v.app.ConfirmDlg.Show("Unsaved Changes", "Discard unsaved changes?", func() {
				te.Visible = false
				v.app.Window.Invalidate()
			})
		} else {
			te.Visible = false
		}
	}

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
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										title := te.FilePath
										if te.Modified {
											title += " *"
										}
										lbl := material.Body1(v.app.Theme.Material, title)
										lbl.Color = ColorText
										lbl.TextSize = unit.Sp(14)
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if te.lang == "" {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, strings.ToUpper(te.lang))
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									}),
								)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									// Edit/View toggle
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return te.editBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											label := "Edit"
											if te.editMode {
												label = "View"
											}
											return layoutSmallButton(gtx, v.app.Theme, label, ColorAccent, te.editBtn.Hovered())
										})
									}),
									// Save (jen v edit mode a pokud modified)
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return te.saveBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												clr := ColorAccent
												if !te.editMode || !te.Modified {
													clr = ColorTextDim
												}
												return layoutSmallButton(gtx, v.app.Theme, "Save", clr, te.saveBtn.Hovered())
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return te.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutSmallButton(gtx, v.app.Theme, "Close", ColorTextDim, te.closeBtn.Hovered())
											})
										})
									}),
								)
							}),
						)
					})
				}),
				// Error
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if te.Error == "" {
						return layout.Dimensions{}
					}
					return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(v.app.Theme.Material, te.Error)
						lbl.Color = ColorDanger
						lbl.TextSize = unit.Sp(12)
						return lbl.Layout(gtx)
					})
				}),
				// Divider
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				// Editor / View
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if te.editMode {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							e := material.Editor(v.app.Theme.Material, &te.editor, "")
							e.Color = ColorText
							e.HintColor = ColorTextDim
							e.TextSize = unit.Sp(13)
							return e.Layout(gtx)
						})
					}
					// View mode — syntax highlighted s čísly řádků
					return v.layoutTextEditorView(gtx)
				}),
			)
		},
	)
}

// --- Helpers ---

func (v *GameServersView) layoutField(gtx layout.Context, label string, ed *widget.Editor, hint string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(v.app.Theme.Material, label)
			lbl.Color = ColorTextDim
			lbl.TextSize = unit.Sp(12)
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(6)
						paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							e := material.Editor(v.app.Theme.Material, ed, hint)
							e.Color = ColorText
							e.HintColor = ColorTextDim
							e.TextSize = unit.Sp(14)
							return e.Layout(gtx)
						})
					},
				)
			})
		}),
	)
}

func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func isTextFile(name string) bool {
	textExts := map[string]bool{
		".toml": true, ".txt": true, ".cfg": true, ".conf": true,
		".properties": true, ".yml": true, ".yaml": true, ".json": true,
		".xml": true, ".log": true, ".sh": true, ".bat": true,
		".md": true, ".ini": true, ".env": true, ".csv": true,
	}
	return textExts[strings.ToLower(filepath.Ext(name))]
}

// extToLang mapuje příponu souboru na chroma language name.
func extToLang(name string) string {
	m := map[string]string{
		".go": "go", ".py": "python", ".js": "javascript", ".ts": "typescript",
		".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
		".xml": "xml", ".html": "html", ".css": "css",
		".sh": "bash", ".bat": "batchfile", ".ps1": "powershell",
		".md": "markdown", ".ini": "ini", ".cfg": "ini",
		".properties": "properties", ".env": "bash",
		".conf": "nginx", ".csv": "", ".txt": "", ".log": "",
	}
	return m[strings.ToLower(filepath.Ext(name))]
}

// buildHighlight tokenizuje obsah editoru a cachuje per-line tokeny.
func (te *TextEditorState) buildHighlight() {
	text := te.editor.Text()
	if text == "" {
		te.highlightedLines = nil
		return
	}
	// Tokenizovat pomocí chroma
	if te.lang != "" {
		tokens := tokenizeCode(text, te.lang)
		if tokens != nil {
			te.highlightedLines = splitTokensIntoLines(tokens)
			return
		}
	}
	// Fallback: plain text s ColorText
	lines := strings.Split(text, "\n")
	te.highlightedLines = make([][]coloredToken, len(lines))
	for i, ln := range lines {
		if ln != "" {
			te.highlightedLines[i] = []coloredToken{{text: ln, color: ColorText}}
		}
	}
}

// splitTokensIntoLines rozdělí flat token slice na per-line slices.
func splitTokensIntoLines(tokens []coloredToken) [][]coloredToken {
	var lines [][]coloredToken
	var current []coloredToken
	for _, tok := range tokens {
		parts := strings.Split(tok.text, "\n")
		for i, part := range parts {
			if i > 0 {
				lines = append(lines, current)
				current = nil
			}
			if part != "" {
				current = append(current, coloredToken{text: part, color: tok.color})
			}
		}
	}
	lines = append(lines, current)
	return lines
}

// layoutTextEditorView renderuje syntax highlighted view s čísly řádků.
func (v *GameServersView) layoutTextEditorView(gtx layout.Context) layout.Dimensions {
	te := v.textEditor
	lines := te.highlightedLines
	if lines == nil {
		return layout.Dimensions{}
	}

	numDigits := len(fmt.Sprint(len(lines)))

	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.List(v.app.Theme.Material, &te.viewList).Layout(gtx, len(lines), func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					// Číslo řádku
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						num := fmt.Sprintf("%*d", numDigits, i+1)
						lbl := material.Body2(v.app.Theme.Material, num)
						lbl.Color = ColorTextDim
						lbl.Font.Typeface = "Go Mono"
						lbl.TextSize = unit.Sp(13)
						return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, lbl.Layout)
					}),
					// Tokeny řádku
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						toks := lines[i]
						if len(toks) == 0 {
							lbl := material.Body2(v.app.Theme.Material, " ")
							lbl.Font.Typeface = "Go Mono"
							lbl.TextSize = unit.Sp(13)
							return lbl.Layout(gtx)
						}
						var items []layout.FlexChild
						for _, t := range toks {
							tok := t
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, tok.text)
								lbl.Color = tok.color
								lbl.Font.Typeface = "Go Mono"
								lbl.TextSize = unit.Sp(13)
								return lbl.Layout(gtx)
							}))
						}
						return layout.Flex{}.Layout(gtx, items...)
					}),
				)
			})
		})
	})
}

// layoutButton a layoutSmallButton — sdílené tlačítko helpery

func layoutButton(gtx layout.Context, th *Theme, text string, clr color.NRGBA, hovered bool) layout.Dimensions {
	bg := clr
	if hovered {
		bg.A = 220
	}

	// Nejdřív změř obsah
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body2(th.Material, text)
		lbl.Color = ColorText
		lbl.TextSize = unit.Sp(13)
		return lbl.Layout(gtx)
	})
	call := macro.Stop()

	// Pozadí na velikost obsahu
	rr := gtx.Dp(6)
	paint.FillShape(gtx.Ops, bg, clip.RRect{
		Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
		NE: rr, NW: rr, SE: rr, SW: rr,
	}.Op(gtx.Ops))

	call.Add(gtx.Ops)
	return dims
}

func layoutSmallButton(gtx layout.Context, th *Theme, text string, clr color.NRGBA, hovered bool) layout.Dimensions {
	bg := ColorInput
	if hovered {
		bg = ColorHover
	}

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body2(th.Material, text)
		lbl.Color = clr
		lbl.TextSize = unit.Sp(12)
		return lbl.Layout(gtx)
	})
	call := macro.Stop()

	rr := gtx.Dp(4)
	paint.FillShape(gtx.Ops, bg, clip.RRect{
		Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
		NE: rr, NW: rr, SE: rr, SW: rr,
	}.Op(gtx.Ops))

	call.Add(gtx.Ops)
	return dims
}

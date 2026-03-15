package ui

import (
	"context"
	"image"
	"image/color"
	"log"
	"sync"
	"time"

	"gioui.org/app"
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
	"nora-client/store"
)

type ViewMode int

const (
	ViewLogin ViewMode = iota
	ViewHome
	ViewChannels
	ViewDM
	ViewGroup
	ViewSettings
	ViewUserSettings
	ViewShares
	ViewLibrary
	ViewGameServers
	ViewKanban
	ViewCalendar
	ViewNotifications
)

type App struct {
	Window *app.Window
	Theme  *Theme

	Mode   ViewMode
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// Identity (unlocked after login)
	SecretKey string
	PublicKey string
	Username  string

	// Servers
	Servers      []*ServerConnection
	ActiveServer int // index into Servers, -1 = none

	// UI components
	Login          *LoginView
	Sidebar        *SidebarView
	ChanView       *ChannelView
	MsgView        *MessageView
	DMView         *DMViewUI
	MemberView     *MemberView
	FriendList     *FriendListView
	GroupView      *GroupViewUI
	AddServerView  *AddServerView
	ShowAddServer  bool
	UpdateBar      *UpdateBar
	UserPopup      *UserPopup
	ConfirmDlg     *ConfirmDialog
	TimeoutDlg     *TimeoutDialog
	BanDlg         *BanDialog
	CreateDlg      *CreateDialog
	ChannelEditDlg *ChannelEditDialog
	CatEditDlg     *CategoryEditDialog
	CreateGroupDlg *CreateGroupDialog
	InputDlg       *InputDialog
	Settings       *SettingsView
	LANHelper      *LANHelper
	VoiceCtrl      *VoiceControls
	CallOverlay    *CallOverlay
	UploadDlg      *UploadDialog
	SaveDlg        *SaveDialog
	ZipUploadDlg   *ZipUploadDialog
	ZipExtractDlg  *ZipExtractDialog
	P2POfferDlg    *P2POfferDialog
	StreamViewer   *StreamViewer
	VideoPlayer    *VideoPlayerUI
	SharesView     *SharesView
	Library        *LibraryView
	GameServers    *GameServersView
	LinkFileDlg    *LinkFileDialog
	LobbyJoinDlg   *LobbyJoinDialog
	ThreadView     *ThreadView
	WhiteboardView *WhiteboardView
	KanbanView     *KanbanView
	KanbanDlg      *CardEditDialog
	CalendarView   *CalendarView
	CalendarDlg    *EventEditDialog
	NotifCenter *NotificationCenter
	NotifMgr    *NotifManager
	TunnelView  *TunnelView

	// Context menu (right-click)
	ContextMenu *ContextMenu

	// File drop (OS drag-and-drop → upload)
	DroppedFiles        chan []string
	fileDropInitialized bool
	useTransferDrop     bool
	showDropOverlay     bool // visual indication during drag-over (Wayland)

	// Global notification level
	GlobalNotifyLevel store.NotifyLevel

	// Sound settings
	NotifVolume    float64 // 0.0-1.0 (default 1.0)
	CustomNotifSnd string  // path to custom notification sound
	CustomDMSnd    string  // path to custom DM sound

	// Video settings
	YouTubeQuality int // preferred height (360, 480, 720), 0 = auto

	// User panel (bottom of channel sidebar)
	userSettingsBtn widget.Clickable
	logoutBtn       widget.Clickable

	// Image cache
	Images *ImageCache

	// DM history (local persistence)
	DMHistory    *store.DMHistory
	GroupHistory *store.GroupHistory
	Bookmarks    *store.BookmarkStore

	// Contacts DB (per-identity SQLite)
	Contacts *store.ContactsDB

	// QR dialog
	QRDlg *QRDialog

	// Search view
	SearchView *SearchView

	// Toast notifications
	Toasts *ToastManager

	// Deep link (from os.Args)
	PendingDeepLink string

	// Deep link invite — queued for user confirmation
	PendingDeepLinkInvite *DeepLinkInvite

	// Cursor tracking (window-space, for context menu position)
	cursorTag bool
	dropTag   bool
	CursorX   int
	CursorY   int


	// Version (set via ldflags)
	Version string
}

func NewApp(w *app.Window, version string) *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		Window:       w,
		Theme:        NewNORATheme(),
		Mode:         ViewLogin,
		ctx:          ctx,
		cancel:       cancel,
		ActiveServer: -1,
		Version:      version,
	}
	a.Login = NewLoginView(a)
	a.Sidebar = NewSidebarView(a)
	a.ChanView = NewChannelView(a)
	a.MsgView = NewMessageView(a)
	a.DMView = NewDMView(a)
	a.MemberView = NewMemberView(a)
	a.FriendList = NewFriendListView(a)
	a.GroupView = NewGroupView(a)
	a.AddServerView = NewAddServerView(a)
	a.UpdateBar = NewUpdateBar(a)
	a.UserPopup = NewUserPopup(a)
	a.ConfirmDlg = NewConfirmDialog(a)
	a.TimeoutDlg = NewTimeoutDialog(a)
	a.BanDlg = NewBanDialog(a)
	a.CreateDlg = NewCreateDialog(a)
	a.ChannelEditDlg = NewChannelEditDialog(a)
	a.CatEditDlg = NewCategoryEditDialog(a)
	a.CreateGroupDlg = NewCreateGroupDialog(a)
	a.InputDlg = NewInputDialog(a)
	a.Settings = NewSettingsView(a)
	a.LANHelper = NewLANHelper(a)
	a.VoiceCtrl = NewVoiceControls(a)
	a.CallOverlay = NewCallOverlay(a)
	a.UploadDlg = NewUploadDialog(a)
	a.SaveDlg = NewSaveDialog(a)
	a.ZipUploadDlg = NewZipUploadDialog(a)
	a.ZipExtractDlg = NewZipExtractDialog(a)
	a.P2POfferDlg = NewP2POfferDialog(a)
	a.StreamViewer = NewStreamViewer(a)
	a.VideoPlayer = NewVideoPlayerUI(a)
	a.SharesView = NewSharesView(a)
	a.Library = NewLibraryView(a)
	a.GameServers = NewGameServersView(a)
	a.LinkFileDlg = NewLinkFileDialog(a)
	a.LobbyJoinDlg = NewLobbyJoinDialog(a)
	a.ThreadView = NewThreadView(a)
	a.WhiteboardView = NewWhiteboardView(a)
	a.KanbanView = NewKanbanView(a)
	a.KanbanDlg = NewCardEditDialog(a)
	a.CalendarView = NewCalendarView(a)
	a.CalendarDlg = NewEventEditDialog(a)
	a.NotifCenter = NewNotificationCenter(a)
	a.NotifMgr = NewNotifManager(a)
	a.TunnelView = NewTunnelView(a)
	a.DroppedFiles = make(chan []string, 4)
	a.Images = NewImageCache()
	a.ContextMenu = NewContextMenu(a)
	a.QRDlg = NewQRDialog(a)
	a.SearchView = NewSearchView(a)
	a.Toasts = NewToastManager(a)
	return a
}

// Conn returns the active ServerConnection or nil.
func (a *App) Conn() *ServerConnection {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ActiveServer >= 0 && a.ActiveServer < len(a.Servers) {
		return a.Servers[a.ActiveServer]
	}
	return nil
}

func (a *App) runAutoCleanup(publicKey string) {
	maxCache, maxDays := store.GetStorageSettings(publicKey)
	if maxCache == 0 {
		maxCache = 2 << 30 // 2 GB default
	}

	if freed, _ := store.CleanupCache(maxCache); freed > 0 {
		log.Printf("Auto-cleanup: freed %s from cache", FormatBytes(freed))
	}

	if maxDays > 0 {
		maxAge := time.Duration(maxDays) * 24 * time.Hour
		if a.DMHistory != nil {
			if n := a.DMHistory.DeleteOlderThan(maxAge); n > 0 {
				a.DMHistory.Save()
			}
		}
		if a.GroupHistory != nil {
			if n := a.GroupHistory.DeleteOlderThan(maxAge); n > 0 {
				a.GroupHistory.Save()
			}
		}
	}
}

func (a *App) Layout(gtx layout.Context) layout.Dimensions {
	// Poll OS-level file drops (X11 XDND)
	for {
		select {
		case raw := <-app.FileDropChan:
			a.HandleDroppedFiles(parseDroppedURIList(raw))
		default:
			goto doneFileDrop
		}
	}
doneFileDrop:

	// Cursor tracking — process events from previous frame
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &a.cursorTag,
			Kinds:  pointer.Move | pointer.Press | pointer.Drag,
		})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			a.CursorX = int(pe.Position.X)
			a.CursorY = int(pe.Position.Y)
			// Record user input for focus tracking (desktop notifications)
			if a.NotifMgr != nil {
				a.NotifMgr.RecordInput()
			}
		}
	}

	// Pre-record cursor tracking ops (will be added on top of Z-order)
	cursorMacro := op.Record(gtx.Ops)
	st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	pr := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &a.cursorTag)
	pr.Pop()
	st.Pop()
	cursorOps := cursorMacro.Stop()

	// Keyboard shortcuts (global)
	a.handleKeyboardShortcuts(gtx)

	var dims layout.Dimensions
	switch a.Mode {
	case ViewLogin:
		dims = a.Login.Layout(gtx)
	case ViewHome, ViewChannels, ViewDM, ViewGroup, ViewSettings, ViewUserSettings, ViewShares, ViewLibrary, ViewGameServers, ViewKanban, ViewCalendar, ViewNotifications:
		// Call overlay is visible from other views too (incoming call)
		hasCallOverlay := false
		if conn := a.Conn(); conn != nil && conn.Call != nil && conn.Call.IsActive() && a.Mode != ViewDM {
			hasCallOverlay = true
		}
		if a.ShowAddServer || a.UserPopup.Visible || a.ConfirmDlg.Visible || a.TimeoutDlg.Visible || a.BanDlg.Visible || a.CreateDlg.Visible || a.CreateGroupDlg.Visible || a.ChannelEditDlg.Visible || a.CatEditDlg.Visible || a.UploadDlg.Visible || a.SaveDlg.Visible || a.ZipUploadDlg.Visible || a.ZipExtractDlg.Visible || a.P2POfferDlg.Visible || a.ContextMenu.Visible || a.LinkFileDlg.Visible || a.LobbyJoinDlg.visible || a.QRDlg.Visible || a.KanbanDlg.Visible || a.CalendarDlg.Visible || hasCallOverlay {
			dims = a.layoutWithOverlay(gtx)
		} else {
			dims = a.layoutMain(gtx)
		}
	}

	// Drop zone overlay (drag-over indication)
	if a.showDropOverlay {
		dropMacro := op.Record(gtx.Ops)
		a.layoutDropOverlay(gtx)
		dropOps := dropMacro.Stop()
		dropOps.Add(gtx.Ops)
	}

	// Toast overlay (above main content)
	if a.Toasts != nil {
		toastMacro := op.Record(gtx.Ops)
		toastGtx := gtx
		toastGtx.Constraints.Min = image.Point{}
		a.Toasts.Layout(toastGtx)
		toastOps := toastMacro.Stop()
		toastOps.Add(gtx.Ops)
	}

	// Cursor tracker on top of Z-order (PassOp → events pass down to content)
	cursorOps.Add(gtx.Ops)
	return dims
}

func (a *App) layoutWithOverlay(gtx layout.Context) layout.Dimensions {
	a.layoutMain(gtx)
	// Call overlay (visible from non-DM views)
	if conn := a.Conn(); conn != nil && conn.Call != nil && conn.Call.IsActive() && a.Mode != ViewDM {
		return a.CallOverlay.Layout(gtx, conn.Call)
	}
	if a.ConfirmDlg.Visible {
		return a.ConfirmDlg.Layout(gtx)
	}
	if a.TimeoutDlg.Visible {
		return a.TimeoutDlg.Layout(gtx)
	}
	if a.BanDlg.Visible {
		return a.BanDlg.Layout(gtx)
	}
	if a.CreateDlg.Visible {
		return a.CreateDlg.Layout(gtx)
	}
	if a.CreateGroupDlg.Visible {
		return a.CreateGroupDlg.Layout(gtx)
	}
	if a.ChannelEditDlg.Visible {
		return a.ChannelEditDlg.Layout(gtx)
	}
	if a.CatEditDlg.Visible {
		return a.CatEditDlg.Layout(gtx)
	}
	if a.UploadDlg.Visible {
		return a.UploadDlg.Layout(gtx)
	}
	if a.SaveDlg.Visible {
		return a.SaveDlg.Layout(gtx)
	}
	if a.ZipUploadDlg.Visible {
		return a.ZipUploadDlg.Layout(gtx)
	}
	if a.ZipExtractDlg.Visible {
		return a.ZipExtractDlg.Layout(gtx)
	}
	if a.P2POfferDlg.Visible {
		return a.P2POfferDlg.Layout(gtx)
	}
	if a.LinkFileDlg.Visible {
		return a.LinkFileDlg.Layout(gtx)
	}
	if a.LobbyJoinDlg.visible {
		return a.LobbyJoinDlg.Layout(gtx)
	}
	if a.KanbanDlg.Visible {
		return a.KanbanDlg.Layout(gtx)
	}
	if a.CalendarDlg.Visible {
		return a.CalendarDlg.Layout(gtx)
	}
	if a.QRDlg.Visible {
		return a.QRDlg.Layout(gtx)
	}
	if a.InputDlg.Visible {
		return a.InputDlg.Layout(gtx)
	}
	if a.ContextMenu.Visible {
		return a.ContextMenu.Layout(gtx)
	}
	if a.ShowAddServer {
		return a.AddServerView.Layout(gtx)
	}
	return a.UserPopup.Layout(gtx)
}

func (a *App) layoutMain(gtx layout.Context) layout.Dimensions {
	conn := a.Conn()

	// Show confirmation dialog for pending deep link invite
	if a.PendingDeepLinkInvite != nil && !a.ConfirmDlg.Visible {
		inv := a.PendingDeepLinkInvite
		a.PendingDeepLinkInvite = nil
		server := inv.Server
		code := inv.Code
		a.ConfirmDlg.ShowConfirm("Join Server",
			"Connect to server "+server+" with invite code?",
			func() {
				a.ConnectServer(server, "", code)
			},
		)
	}

	// User panel click handlers
	if a.userSettingsBtn.Clicked(gtx) {
		a.mu.Lock()
		a.Mode = ViewUserSettings
		a.mu.Unlock()
		a.Settings.activeCategory = settingsCatProfile
	}
	if a.logoutBtn.Clicked(gtx) {
		a.ConfirmDlg.ShowConfirm("Logout", "Log out and return to login screen?", func() {
			for len(a.Servers) > 0 {
				a.DisconnectServer(0)
			}
			a.mu.Lock()
			a.SecretKey = ""
			a.PublicKey = ""
			a.Username = ""
			a.Mode = ViewLogin
			a.DMHistory = nil
			a.GroupHistory = nil
			if a.Bookmarks != nil {
				a.Bookmarks.Save()
				a.Bookmarks = nil
			}
			if a.Contacts != nil {
				a.Contacts.Close()
				a.Contacts = nil
			}
			a.mu.Unlock()
			a.Window.Invalidate()
		})
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Update bar (top)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.UpdateBar.Layout(gtx)
		}),
		// Main content
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				// Server sidebar (64px)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(64)
					gtx.Constraints.Max.X = gtx.Dp(64)
					return a.Sidebar.Layout(gtx)
				}),
				// Channel sidebar (300px) + voice controls + user panel
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(300)
					gtx.Constraints.Max.X = gtx.Dp(300)
					if conn == nil {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layoutColoredBg(gtx, ColorCard)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.layoutUserPanel(gtx)
							}),
						)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							if a.Mode == ViewNotifications {
								return a.NotifCenter.LayoutSidebar(gtx)
							}
							if a.Mode == ViewCalendar {
								return a.CalendarView.LayoutSidebar(gtx)
							}
							if a.Mode == ViewKanban {
								return a.KanbanView.LayoutSidebar(gtx)
							}
							if a.Mode == ViewGameServers {
								return a.GameServers.LayoutSidebar(gtx)
							}
							if a.Mode == ViewLibrary {
								return a.Library.LayoutSidebar(gtx)
							}
							if a.Mode == ViewShares {
								return a.SharesView.LayoutSidebar(gtx)
							}
							if a.Mode == ViewSettings || a.Mode == ViewUserSettings {
								return a.Settings.LayoutSidebar(gtx)
							}
							if a.Mode == ViewDM || a.Mode == ViewGroup {
								return a.DMView.LayoutSidebar(gtx)
							}
							return a.ChanView.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.VoiceCtrl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.layoutUserPanel(gtx)
						}),
					)
				}),
				// Main area (messages / settings / group / stream viewer / whiteboard)
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					// Whiteboard overlay
					if a.WhiteboardView != nil && a.WhiteboardView.Visible {
						return a.WhiteboardView.Layout(gtx)
					}
					// VideoPlayer / StreamViewer overlay — must be in Stack with CallOverlay
					// so call buttons (mute/hangup) remain visible
					hasFullOverlay := (a.VideoPlayer != nil && a.VideoPlayer.Visible) || (a.StreamViewer != nil && a.StreamViewer.Visible)
					if hasFullOverlay {
						hasCall := conn != nil && conn.Call != nil && conn.Call.IsActive()
						if hasCall {
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									if a.VideoPlayer != nil && a.VideoPlayer.Visible {
										return a.VideoPlayer.Layout(gtx)
									}
									return a.StreamViewer.Layout(gtx)
								}),
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									return a.CallOverlay.Layout(gtx, conn.Call)
								}),
							)
						}
						if a.VideoPlayer != nil && a.VideoPlayer.Visible {
							return a.VideoPlayer.Layout(gtx)
						}
						return a.StreamViewer.Layout(gtx)
					}
					if conn == nil && a.Mode != ViewUserSettings {
						if a.Mode == ViewHome && a.Contacts != nil {
							return a.SearchView.Layout(gtx)
						}
						return layoutCentered(gtx, a.Theme, "Add a server with the + button on the left", ColorTextDim)
					}
					if a.Mode == ViewHome && a.Contacts != nil {
						return a.SearchView.Layout(gtx)
					}
					if a.Mode == ViewNotifications {
						return a.NotifCenter.LayoutMain(gtx)
					}
					if a.Mode == ViewCalendar {
						return a.CalendarView.LayoutMain(gtx)
					}
					if a.Mode == ViewKanban {
						return a.KanbanView.LayoutMain(gtx)
					}
					if a.Mode == ViewGameServers {
						return a.GameServers.LayoutMain(gtx)
					}
					if a.Mode == ViewLibrary {
						return a.Library.LayoutMain(gtx)
					}
					if a.Mode == ViewShares {
						return a.SharesView.LayoutMain(gtx)
					}
					if a.Mode == ViewSettings || a.Mode == ViewUserSettings {
						return a.Settings.Layout(gtx)
					}
					if a.Mode == ViewGroup {
						return a.GroupView.Layout(gtx)
					}
					if a.Mode == ViewDM {
						// Call overlay over DM message area
						if conn != nil && conn.Call != nil && conn.Call.IsActive() {
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									return a.DMView.LayoutMessages(gtx)
								}),
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									return a.CallOverlay.Layout(gtx, conn.Call)
								}),
							)
						}
						return a.DMView.LayoutMessages(gtx)
					}
					return a.MsgView.Layout(gtx)
				}),
				// Right panel — Members/Friends or Thread
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if conn == nil || a.Mode == ViewSettings || a.Mode == ViewUserSettings || a.Mode == ViewShares || a.Mode == ViewLibrary || a.Mode == ViewGameServers || a.Mode == ViewKanban || a.Mode == ViewCalendar || a.Mode == ViewNotifications {
						return layout.Dimensions{}
					}
					// Thread view replaces member list
					if a.Mode == ViewChannels && a.ThreadView.Visible {
						gtx.Constraints.Min.X = gtx.Dp(320)
						gtx.Constraints.Max.X = gtx.Dp(320)
						return a.ThreadView.Layout(gtx)
					}
					gtx.Constraints.Min.X = gtx.Dp(200)
					gtx.Constraints.Max.X = gtx.Dp(200)
					if a.Mode == ViewDM || a.Mode == ViewGroup {
						return a.FriendList.Layout(gtx)
					}
					if a.Mode == ViewChannels {
						return a.MemberView.Layout(gtx)
					}
					return layout.Dimensions{}
				}),
			)
		}),
	)
}

// layoutUserPanel renders the horizontal user bar at the bottom of the channel sidebar (like Discord)
func (a *App) layoutUserPanel(gtx layout.Context) layout.Dimensions {
	a.mu.RLock()
	username := a.Username
	mode := a.Mode
	a.mu.RUnlock()

	// Avatar URL
	avatarURL := ""
	conn := a.Conn()
	if conn != nil {
		a.mu.RLock()
		for _, u := range conn.Users {
			if u.ID == conn.UserID {
				avatarURL = u.AvatarURL
				break
			}
		}
		a.mu.RUnlock()
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Divider on top
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
					paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
					return layout.Dimensions{Size: size}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							// Avatar
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layoutAvatar(gtx, a, username, avatarURL, 28)
							}),
							// Username
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(a.Theme.Material, username)
									lbl.Color = ColorText
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								})
							}),
							// Settings gear
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return a.layoutUserPanelIconBtn(gtx, &a.userSettingsBtn, IconSettings, mode == ViewUserSettings)
								})
							}),
							// Logout
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return a.layoutUserPanelIconBtn(gtx, &a.logoutBtn, IconExit, false)
								})
							}),
						)
					})
				}),
			)
		},
	)
}

func (a *App) layoutUserPanelIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, active bool) layout.Dimensions {
	size := gtx.Dp(28)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		bg := color.NRGBA{A: 0}
		if active {
			bg = ColorAccent
		} else if btn.Hovered() {
			bg = ColorHover
		}

		rr := size / 4
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			fg := ColorTextDim
			if active {
				fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			}
			return layoutIcon(gtx, icon, 18, fg)
		})
	})
}

func (a *App) SwitchToServer(idx int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if idx >= 0 && idx < len(a.Servers) {
		a.ActiveServer = idx
		a.Mode = ViewChannels
	}
}

func (a *App) SwitchToHome() {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Keep first server connection active for DMs
	if len(a.Servers) > 0 && a.ActiveServer < 0 {
		a.ActiveServer = 0
	}
	a.Mode = ViewDM
}

func (a *App) DisconnectServer(idx int) {
	a.mu.Lock()
	if idx < 0 || idx >= len(a.Servers) {
		a.mu.Unlock()
		return
	}
	conn := a.Servers[idx]
	if conn.Call != nil && conn.Call.IsActive() {
		conn.Call.HangupCall()
	}
	if conn.Voice != nil {
		conn.Voice.Destroy()
	}
	if conn.P2P != nil {
		conn.P2P.Cleanup()
	}
	if conn.Swarm != nil {
		conn.Swarm.Close()
	}
	if conn.Mounts != nil {
		conn.Mounts.UnmountAll()
	}
	conn.Cancel()
	if conn.WS != nil {
		conn.WS.Close()
	}
	a.Servers = append(a.Servers[:idx], a.Servers[idx+1:]...)
	if a.ActiveServer >= len(a.Servers) {
		a.ActiveServer = len(a.Servers) - 1
	}
	if len(a.Servers) == 0 {
		a.ActiveServer = -1
		a.Mode = ViewHome
	} else {
		a.Mode = ViewDM
	}
	a.mu.Unlock()
}

func (a *App) FindUser(id string) *api.User {
	conn := a.Conn()
	if conn == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for i := range conn.Users {
		if conn.Users[i].ID == id {
			return &conn.Users[i]
		}
	}
	return nil
}

// IsBlockedKey returns true if the public key is blocked in the client blocklist (cross-server).
func (a *App) IsBlockedKey(publicKey string) bool {
	if publicKey == "" || a.Contacts == nil {
		return false
	}
	return a.Contacts.IsBlocked(publicKey)
}

func (a *App) Destroy() {
	for _, s := range a.Servers {
		if s.Call != nil && s.Call.IsActive() {
			s.Call.HangupCall()
		}
		if s.Voice != nil {
			s.Voice.Destroy()
		}
		if s.P2P != nil {
			s.P2P.Cleanup()
		}
		if s.Swarm != nil {
			s.Swarm.Close()
		}
		if s.Mounts != nil {
			s.Mounts.UnmountAll()
		}
		if s.WS != nil {
			s.WS.Close()
		}
		s.Cancel()
	}
	if a.VideoPlayer != nil {
		a.VideoPlayer.Destroy()
	}
	if a.Contacts != nil {
		a.Contacts.Close()
	}
	a.cancel()
}

// handleKeyboardShortcuts handles global keyboard shortcuts.
func (a *App) handleKeyboardShortcuts(gtx layout.Context) {
	if a.Mode == ViewLogin {
		return
	}

	// Register filters for keyboard shortcuts
	for {
		ev, ok := gtx.Event(
			// Escape — close dialogs/overlays
			key.Filter{Name: key.NameEscape},
			// Ctrl+1..9 — switch server
			key.Filter{Name: "1", Required: key.ModCtrl},
			key.Filter{Name: "2", Required: key.ModCtrl},
			key.Filter{Name: "3", Required: key.ModCtrl},
			key.Filter{Name: "4", Required: key.ModCtrl},
			key.Filter{Name: "5", Required: key.ModCtrl},
			key.Filter{Name: "6", Required: key.ModCtrl},
			key.Filter{Name: "7", Required: key.ModCtrl},
			key.Filter{Name: "8", Required: key.ModCtrl},
			key.Filter{Name: "9", Required: key.ModCtrl},
		)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		// Record keyboard input for focus tracking (desktop notifications)
		if a.NotifMgr != nil {
			a.NotifMgr.RecordInput()
		}

		switch {
		case e.Name == key.NameEscape:
			a.handleEscapeKey()

		case e.Modifiers.Contain(key.ModCtrl) && e.Name >= "1" && e.Name <= "9":
			idx := int(e.Name[0] - '1')
			a.mu.RLock()
			n := len(a.Servers)
			a.mu.RUnlock()
			if idx < n {
				a.SwitchToServer(idx)
			}
		}
	}
}

// handleEscapeKey closes the topmost dialog/overlay.
func (a *App) handleEscapeKey() {
	// Close dialogs in priority order (topmost first)
	switch {
	case a.ContextMenu.Visible:
		a.ContextMenu.Visible = false
	case a.UserPopup.Visible:
		a.UserPopup.Hide()
	case a.ConfirmDlg.Visible:
		a.ConfirmDlg.Visible = false
	case a.TimeoutDlg.Visible:
		a.TimeoutDlg.Visible = false
	case a.BanDlg.Visible:
		a.BanDlg.Visible = false
	case a.InputDlg.Visible:
		a.InputDlg.Visible = false
	case a.CreateDlg.Visible:
		a.CreateDlg.Visible = false
	case a.CreateGroupDlg.Visible:
		a.CreateGroupDlg.Visible = false
	case a.ChannelEditDlg.Visible:
		a.ChannelEditDlg.Visible = false
	case a.UploadDlg.Visible:
		a.UploadDlg.Visible = false
	case a.SaveDlg.Visible:
		a.SaveDlg.Visible = false
	case a.ZipUploadDlg.Visible:
		a.ZipUploadDlg.Visible = false
	case a.ZipExtractDlg.Visible:
		a.ZipExtractDlg.Visible = false
	case a.P2POfferDlg.Visible:
		a.P2POfferDlg.Visible = false
	case a.LinkFileDlg.Visible:
		a.LinkFileDlg.Visible = false
	case a.QRDlg.Visible:
		a.QRDlg.Visible = false
	case a.KanbanDlg.Visible:
		a.KanbanDlg.Visible = false
	case a.CalendarDlg.Visible:
		a.CalendarDlg.Visible = false
	case a.ShowAddServer:
		a.ShowAddServer = false
	case a.VideoPlayer != nil && a.VideoPlayer.Visible:
		a.VideoPlayer.Close()
	case a.StreamViewer != nil && a.StreamViewer.Visible:
		a.StreamViewer.Visible = false
	case a.WhiteboardView != nil && a.WhiteboardView.Visible:
		a.WhiteboardView.Visible = false
	case a.ThreadView != nil && a.ThreadView.Visible:
		a.ThreadView.Visible = false
	case a.Mode == ViewSettings || a.Mode == ViewUserSettings:
		a.mu.Lock()
		a.Mode = ViewChannels
		a.mu.Unlock()
	}
}

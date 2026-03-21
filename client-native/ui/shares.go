package ui

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/mount"
	"nora-client/p2p"
	"nora-client/store"
)

type SharesView struct {
	app *App

	// Sidebar
	sideList       widget.List
	myShareBtns    []widget.Clickable
	sharedBtns     []widget.Clickable
	addShareBtn    widget.Clickable
	backBtn        widget.Clickable

	// Main area
	mainList       widget.List
	fileBtns       []widget.Clickable
	downloadBtns   []widget.Clickable
	parentBtn      widget.Clickable
	deleteShareBtn widget.Clickable
	refreshBtn     widget.Clickable

	// Server picker for new shares
	showServerPicker  bool
	pendingShareDir   string // local path waiting for server selection
	pendingShareHash  string
	pendingShareName  string
	serverCheckboxes  []widget.Bool
	serverPickerOkBtn     widget.Clickable
	serverPickerCancelBtn widget.Clickable

	// Permission editor
	memberSearchEd  widget.Editor // search filter for member picker
	permsBtn        widget.Clickable
	permList        widget.List
	permEditBtns    []widget.Clickable
	permDelBtns     []widget.Clickable
	addPermBtns     []widget.Clickable
	globalReadCheck  widget.Bool
	globalWriteCheck widget.Bool
	globalDeleteCheck widget.Bool
	globalBlockedCheck widget.Bool
	saveGlobalBtn   widget.Clickable
	// Per-user permission being edited
	editPermIdx     int // -1 = none, index do Permissions
	editReadCheck   widget.Bool
	editWriteCheck  widget.Bool
	editDeleteCheck widget.Bool
	editBlockedCheck widget.Bool
	savePermBtn     widget.Clickable
	cancelPermBtn   widget.Clickable

	// Server info
	showServerInfo   bool
	serverInfoBtn    widget.Clickable
	serverToggleBtns []widget.Clickable // per-server toggle buttons

	// Limits editor
	limitsBtn           widget.Clickable
	ShowLimits          bool
	limitFileSizeEditor widget.Editor
	limitQuotaEditor    widget.Editor
	limitFilesEditor    widget.Editor
	limitExpiryEditor   widget.Editor
	saveLimitsBtn       widget.Clickable
	shareStatSize       int64
	shareStatFiles      int

	// P2P shared files
	p2pBtns      []widget.Clickable // unshare buttons

	// Mount buttons (SHARED WITH ME)
	mountBtns   []widget.Clickable
	unmountBtns []widget.Clickable

	// Set local path (owner shares)
	setPathBtn widget.Clickable

	// Swarm download
	swarmBtns    []widget.Clickable
	swarmCounts  map[string]int // fileID → seeder count

	// Shared file explorer widget
	fileWidget    *FileExplorerWidget
	deleteSelBtn  widget.Clickable
	clearSelBtn   widget.Clickable
	sortedFiles   []FileExplorerEntry
	// State
	ActiveShareID string
	BrowsePath    string // current path in browsed directory
	ShowPerms     bool   // show permission editor
	Files         []api.SharedFileEntry
	Permissions   []api.SharePermission
}

func NewSharesView(a *App) *SharesView {
	v := &SharesView{app: a}
	v.sideList.Axis = layout.Vertical
	v.mainList.Axis = layout.Vertical
	v.permList.Axis = layout.Vertical
	v.BrowsePath = "/"
	v.editPermIdx = -1
	v.memberSearchEd.SingleLine = true
	v.fileWidget = NewFileExplorerWidget(a)
	return v
}

// LayoutSidebar — left panel with the list of shared directories
func (v *SharesView) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutColoredBg(gtx, ColorCard)
	}

	v.app.mu.RLock()
	myShares := conn.MyShares
	sharedWithMe := conn.SharedWithMe
	v.app.mu.RUnlock()

	// P2P registered files
	var p2pFiles []p2p.RegisteredFile
	if conn.P2P != nil {
		p2pFiles = conn.P2P.GetRegisteredFiles()
	}

	if len(v.myShareBtns) < len(myShares)+1 {
		v.myShareBtns = make([]widget.Clickable, len(myShares)+10)
	}
	if len(v.sharedBtns) < len(sharedWithMe)+1 {
		v.sharedBtns = make([]widget.Clickable, len(sharedWithMe)+10)
	}
	if len(v.p2pBtns) < len(p2pFiles)+1 {
		v.p2pBtns = make([]widget.Clickable, len(p2pFiles)+10)
	}
	if len(v.mountBtns) < len(sharedWithMe)+1 {
		v.mountBtns = make([]widget.Clickable, len(sharedWithMe)+10)
	}
	if len(v.unmountBtns) < len(sharedWithMe)+1 {
		v.unmountBtns = make([]widget.Clickable, len(sharedWithMe)+10)
	}

	// Klik handlery
	for i, s := range myShares {
		if v.myShareBtns[i].Clicked(gtx) {
			v.selectShare(s.ID)
		}
	}
	for i, s := range sharedWithMe {
		if v.sharedBtns[i].Clicked(gtx) {
			v.selectShare(s.ID)
		}
		if i < len(v.mountBtns) && v.mountBtns[i].Clicked(gtx) {
			shareID := s.ID
			shareName := s.DisplayName
			canWrite := s.CanWrite
			go func() {
				if conn.Mounts != nil {
					info, err := conn.Mounts.Mount(shareID, shareName, canWrite)
					if err != nil {
						log.Printf("Mount error: %v", err)
					} else {
						log.Printf("Mounted %s at %s", shareName, info.Path)
						v.persistMountedShares(conn)
					}
					v.app.Window.Invalidate()
				}
			}()
		}
		if i < len(v.unmountBtns) && v.unmountBtns[i].Clicked(gtx) {
			shareID := s.ID
			go func() {
				if conn.Mounts != nil {
					if err := conn.Mounts.Unmount(shareID); err != nil {
						log.Printf("Unmount error: %v", err)
					}
					v.persistMountedShares(conn)
					v.app.Window.Invalidate()
				}
			}()
		}
	}
	for i, f := range p2pFiles {
		if v.p2pBtns[i].Clicked(gtx) {
			tid := f.TransferID
			fileName := f.FileName
			v.app.ConfirmDlg.Show("Stop Sharing", fmt.Sprintf("Stop sharing \"%s\"?", fileName), func() {
				removed := conn.P2P.UnregisterFile(tid)
				if removed != nil && removed.IsTemp {
					filePath := removed.FilePath
					v.app.ConfirmDlg.Show("Delete ZIP", fmt.Sprintf("Delete temporary file \"%s\"?", removed.FileName), func() {
						os.Remove(filePath)
					})
				}
				v.app.Window.Invalidate()
			})
		}
	}
	if v.addShareBtn.Clicked(gtx) {
		v.addShare()
	}
	if v.backBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewDM
		v.app.mu.Unlock()
	}

	var children []layout.FlexChild

	// Header: "Files" + Back button
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconBack, 20, ColorTextDim)
					})
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.H6(v.app.Theme.Material, "Files")
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Divider
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutDivider(gtx)
	}))

	// "My Shared Folders" sekce
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, "MY SHARED FOLDERS")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.addShareBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconAdd, 18, ColorTextDim)
					})
				}),
			)
		})
	}))

	// My shares list
	for i, s := range myShares {
		i, s := i, s
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			active := v.ActiveShareID == s.ID
			return v.layoutShareItem(gtx, &v.myShareBtns[i], s.DisplayName, "", s.IsActive, active)
		}))
	}

	if len(myShares) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "No shared folders")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	}

	// Divider
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutDivider(gtx)
		})
	}))

	// "Shared With Me" sekce
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "SHARED WITH ME")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))

	for i, s := range sharedWithMe {
		i, s := i, s
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			active := v.ActiveShareID == s.ID
			isMounted := conn.Mounts != nil && conn.Mounts.IsMounted(s.ID)
			var mountBtn, unmountBtn *widget.Clickable
			if i < len(v.mountBtns) {
				mountBtn = &v.mountBtns[i]
			}
			if i < len(v.unmountBtns) {
				unmountBtn = &v.unmountBtns[i]
			}
			return v.layoutShareItemWithMount(gtx, &v.sharedBtns[i], s.DisplayName, s.OwnerName, s.IsActive, active, isMounted, mountBtn, unmountBtn)
		}))
	}

	if len(sharedWithMe) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "No folders shared with you")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	}

	// Divider
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutDivider(gtx)
		})
	}))

	// "P2P SHARED FILES" sekce
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "P2P SHARED FILES")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))

	for i, f := range p2pFiles {
		i, f := i, f
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutP2PFileItem(gtx, &v.p2pBtns[i], f)
		}))
	}

	if len(p2pFiles) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "No P2P shared files")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// LayoutMain — main area with file browsing
func (v *SharesView) LayoutMain(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Server picker overlay
	if v.showServerPicker {
		return v.layoutServerPicker(gtx)
	}

	conn := v.app.Conn()
	if conn == nil {
		return layoutCentered(gtx, v.app.Theme, "Not connected", ColorTextDim)
	}

	if v.ActiveShareID == "" {
		return layoutCentered(gtx, v.app.Theme, "Select a shared folder from the left panel", ColorTextDim)
	}

	// Find active share
	v.app.mu.RLock()
	var activeShare *api.SharedDirectory
	for i := range conn.MyShares {
		if conn.MyShares[i].ID == v.ActiveShareID {
			activeShare = &conn.MyShares[i]
			break
		}
	}
	if activeShare == nil {
		for i := range conn.SharedWithMe {
			if conn.SharedWithMe[i].ID == v.ActiveShareID {
				activeShare = &conn.SharedWithMe[i]
				break
			}
		}
	}
	v.app.mu.RUnlock()

	if activeShare == nil {
		return layoutCentered(gtx, v.app.Theme, "Share not found", ColorTextDim)
	}

	isOwner := activeShare.OwnerID == conn.UserID

	// Click handlery
	if v.parentBtn.Clicked(gtx) {
		if v.BrowsePath != "/" {
			v.BrowsePath = filepath.Dir(v.BrowsePath)
			if v.BrowsePath == "." {
				v.BrowsePath = "/"
			}
			v.loadFiles()
		}
	}
	if v.deleteShareBtn.Clicked(gtx) && isOwner {
		shareID := v.ActiveShareID
		v.app.ConfirmDlg.Show("Stop Sharing", fmt.Sprintf("Stop sharing \"%s\"? The directory will no longer be accessible to others.", activeShare.DisplayName), func() {
			go func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.DeleteShare(shareID); err != nil {
						log.Printf("DeleteShare error: %v", err)
					}
					v.app.mu.Lock()
					delete(conn.SharePaths, shareID)
					v.app.mu.Unlock()
					v.persistSharePaths()
				}
			}()
			v.ActiveShareID = ""
		})
	}
	if v.serverInfoBtn.Clicked(gtx) && isOwner {
		v.showServerInfo = !v.showServerInfo
		if v.showServerInfo {
			v.ShowLimits = false
			v.ShowPerms = false
		}
	}
	if v.limitsBtn.Clicked(gtx) && isOwner {
		v.ShowLimits = !v.ShowLimits
		if v.ShowLimits {
			v.ShowPerms = false
			v.showServerInfo = false
			v.loadLimits(activeShare)
		}
	}
	if v.permsBtn.Clicked(gtx) && isOwner {
		v.ShowPerms = !v.ShowPerms
		if v.ShowPerms {
			v.ShowLimits = false
			v.showServerInfo = false
			v.loadPermissions()
		}
		v.editPermIdx = -1
	}
	if v.setPathBtn.Clicked(gtx) && isOwner {
		shareID := v.ActiveShareID
		go func() {
			dir, err := pickDirectory()
			if err != nil || dir == "" {
				return
			}
			conn := v.app.Conn()
			if conn == nil {
				return
			}
			v.app.mu.Lock()
			conn.SharePaths[shareID] = dir
			v.app.mu.Unlock()
			v.persistSharePaths()
			log.Printf("SharePaths: set %s → %s", shareID, dir)
			v.syncShareFiles(shareID, dir)
			v.loadFiles()
			v.app.Window.Invalidate()
		}()
	}
	if v.saveLimitsBtn.Clicked(gtx) && isOwner {
		v.saveLimits()
	}
	if v.refreshBtn.Clicked(gtx) {
		if isOwner {
			v.syncLocalFiles()
		}
		if v.ShowLimits {
			v.loadLimits(activeShare)
		} else if v.ShowPerms {
			v.loadPermissions()
		} else {
			v.fileWidget.ClearSelection()
			v.loadFiles()
		}
	}

	// FileExplorerWidget event handling
	fw := v.fileWidget
	fw.HandleColumnClick(gtx)
	fw.HandleRenameEvents(gtx)

	// Handle rename completion
	canWrite := isOwner || activeShare.CanWrite
	if fw.RenameOK && canWrite {
		fw.RenameOK = false
		newName := fw.RenameName
		oldName := fw.RenameOrigName
		if oldName != "" && newName != oldName {
			shareID := v.ActiveShareID
			browsePath := v.BrowsePath
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.RenameShareFile(shareID, browsePath, oldName, newName); err != nil {
						log.Printf("RenameShareFile error: %v", err)
					}
					v.loadFiles()
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	// Handle batch delete selected
	if v.deleteSelBtn.Clicked(gtx) && canWrite {
		count := fw.SelectedCount()
		if count > 0 {
			indices := fw.SelectedIndices()
			type delItem struct {
				relPath  string
				fileName string
			}
			var items []delItem
			for _, idx := range indices {
				if idx < len(v.sortedFiles) {
					items = append(items, delItem{relPath: v.BrowsePath, fileName: v.sortedFiles[idx].Name})
				}
			}
			v.app.ConfirmDlg.Show("Delete Selected", fmt.Sprintf("Delete %d items?", count), func() {
				shareID := v.ActiveShareID
				go func() {
					if c := v.app.Conn(); c != nil {
						for _, item := range items {
							if err := c.Client.DeleteShareFile(shareID, item.relPath, item.fileName); err != nil {
								log.Printf("DeleteShareFile error: %v", err)
							}
						}
						fw.ClearSelection()
						v.loadFiles()
						v.app.Window.Invalidate()
					}
				}()
			})
		}
	}
	if v.clearSelBtn.Clicked(gtx) {
		fw.ClearSelection()
	}

	// Convert Files → sortedFiles
	v.sortedFiles = make([]FileExplorerEntry, len(v.Files))
	for i, f := range v.Files {
		var modTime time.Time
		if f.ModifiedAt != nil {
			modTime = *f.ModifiedAt
		}
		v.sortedFiles[i] = FileExplorerEntry{
			Name:    f.FileName,
			IsDir:   f.IsDir,
			Size:    f.FileSize,
			ModTime: modTime,
			ID:      f.ID,
		}
	}
	fw.SortEntries(v.sortedFiles)

	// Handle sorted file clicks
	if len(v.fileBtns) < len(v.sortedFiles)+1 {
		v.fileBtns = make([]widget.Clickable, len(v.sortedFiles)+10)
	}
	if len(v.downloadBtns) < len(v.sortedFiles)+1 {
		v.downloadBtns = make([]widget.Clickable, len(v.sortedFiles)+10)
	}
	if len(v.swarmBtns) < len(v.sortedFiles)+1 {
		v.swarmBtns = make([]widget.Clickable, len(v.sortedFiles)+10)
	}

	for i, se := range v.sortedFiles {
		// Find the original api.SharedFileEntry by ID
		if i < len(v.fileBtns) && v.fileBtns[i].Clicked(gtx) {
			if se.IsDir {
				if strings.Contains(se.Name, "..") || strings.ContainsAny(se.Name, "/\\") {
					continue
				}
				if v.BrowsePath == "/" {
					v.BrowsePath = "/" + se.Name
				} else {
					v.BrowsePath = v.BrowsePath + "/" + se.Name
				}
				fw.ClearSelection()
				fw.CancelRename()
				v.loadFiles()
			}
		}
		if i < len(v.downloadBtns) && v.downloadBtns[i].Clicked(gtx) && !se.IsDir {
			if origFile := v.findOrigFileByID(se.ID); origFile != nil {
				v.requestDownload(*origFile)
			}
		}
		if i < len(v.swarmBtns) && v.swarmBtns[i].Clicked(gtx) && !se.IsDir {
			if origFile := v.findOrigFileByID(se.ID); origFile != nil {
				v.requestSwarmDownload(*origFile)
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutHeader(gtx, activeShare, isOwner)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Breadcrumb path
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.BrowsePath == "/" {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.parentBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconBack, 18, ColorAccent)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, v.BrowsePath)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		}),
		// Selection bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return fw.LayoutDeleteSelectedBar(gtx, &v.deleteSelBtn, &v.clearSelBtn)
		}),
		// File list or Permission editor or Limits editor
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if v.showServerInfo {
				return v.layoutServerInfo(gtx, activeShare)
			}
			if v.ShowLimits {
				return v.layoutLimits(gtx, activeShare)
			}
			if v.ShowPerms {
				return v.layoutPermissions(gtx)
			}
			return v.layoutFileList(gtx)
		}),
	)
}

func (v *SharesView) layoutHeader(gtx layout.Context, share *api.SharedDirectory, isOwner bool) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			// Ikona
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutIcon(gtx, IconFolder, 24, ColorAccent)
			}),
			// Name
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.H6(v.app.Theme.Material, share.DisplayName)
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							owner := "You"
							if !isOwner {
								owner = share.OwnerName
							}
							status := "offline"
							if share.IsActive {
								status = "online"
							}
							conn := v.app.Conn()
							serverName := ""
							if conn != nil {
								serverName = conn.Name
								if serverName == "" {
									serverName = conn.URL
								}
							}
							text := fmt.Sprintf("Owner: %s · %s · Server: %s", owner, status, serverName)
							if conn != nil {
								if isOwner {
									v.app.mu.RLock()
									lp, hasLP := conn.SharePaths[share.ID]
									v.app.mu.RUnlock()
									if hasLP {
										text += fmt.Sprintf(" · Path: %s", lp)
									} else {
										text += " · No local path set!"
									}
								}
								if conn.Mounts != nil {
									if info := conn.Mounts.GetMountInfo(share.ID); info != nil {
										text += fmt.Sprintf(" · Mounted: %s", info.Path)
									}
								}
							}
							lbl := material.Caption(v.app.Theme.Material, text)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
					)
				})
			}),
			// Set Path (owner — local path missing)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !isOwner {
					return layout.Dimensions{}
				}
				conn := v.app.Conn()
				if conn == nil {
					return layout.Dimensions{}
				}
				v.app.mu.RLock()
				_, hasPath := conn.SharePaths[share.ID]
				v.app.mu.RUnlock()
				clr := ColorWarning
				if hasPath {
					clr = ColorOnline
				}
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.setPathBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconFolder, 20, clr)
					})
				})
			}),
			// Limits (owner only)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !isOwner {
					return layout.Dimensions{}
				}
				clr := ColorTextDim
				if v.ShowLimits {
					clr = ColorAccent
				}
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.limitsBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconStorage, 20, clr)
					})
				})
			}),
			// Permissions (owner only)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !isOwner {
					return layout.Dimensions{}
				}
				clr := ColorTextDim
				if v.ShowPerms {
					clr = ColorAccent
				}
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.permsBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconSettings, 20, clr)
					})
				})
			}),
			// Server info (owner only — shows which servers share this directory)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !isOwner {
					return layout.Dimensions{}
				}
				clr := ColorTextDim
				if v.showServerInfo {
					clr = ColorAccent
				}
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.serverInfoBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconMonitor, 20, clr)
					})
				})
			}),
			// Refresh
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.refreshBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconRefresh, 20, ColorTextDim)
					})
				})
			}),
			// Delete (owner only)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !isOwner {
					return layout.Dimensions{}
				}
				return v.deleteShareBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconCancel, 20, ColorDanger)
				})
			}),
		)
	})
}

func (v *SharesView) loadLimits(share *api.SharedDirectory) {
	v.limitFileSizeEditor.SetText(strconv.Itoa(share.MaxFileSizeMB))
	v.limitQuotaEditor.SetText(strconv.Itoa(share.StorageQuotaMB))
	v.limitFilesEditor.SetText(strconv.Itoa(share.MaxFilesCount))
	v.limitExpiryEditor.SetText("0")

	shareID := v.ActiveShareID
	go func() {
		conn := v.app.Conn()
		if conn == nil {
			return
		}
		totalSize, filesCount, err := conn.Client.GetShareStats(shareID)
		if err != nil {
			log.Printf("GetShareStats error: %v", err)
			return
		}
		v.app.mu.Lock()
		v.shareStatSize = totalSize
		v.shareStatFiles = filesCount
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *SharesView) saveLimits() {
	shareID := v.ActiveShareID
	maxFileSize, _ := strconv.Atoi(v.limitFileSizeEditor.Text())
	quota, _ := strconv.Atoi(v.limitQuotaEditor.Text())
	maxFiles, _ := strconv.Atoi(v.limitFilesEditor.Text())
	expiryHours, _ := strconv.Atoi(v.limitExpiryEditor.Text())

	go func() {
		conn := v.app.Conn()
		if conn == nil {
			return
		}
		body := map[string]interface{}{
			"max_file_size_mb":  maxFileSize,
			"storage_quota_mb":  quota,
			"max_files_count":   maxFiles,
			"expiry_hours":      expiryHours,
		}
		_, err := conn.Client.UpdateShare(shareID, body)
		if err != nil {
			log.Printf("UpdateShare (limits) error: %v", err)
			return
		}
		v.loadShareList()
		v.app.Window.Invalidate()
	}()
}

func (v *SharesView) layoutLimits(gtx layout.Context, share *api.SharedDirectory) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Header
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.H6(v.app.Theme.Material, "Share Limits")
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, "Set 0 for unlimited. Limits are enforced on sync and upload.")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
			// Usage stats
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				v.app.mu.RLock()
				sz := v.shareStatSize
				fc := v.shareStatFiles
				v.app.mu.RUnlock()
				text := fmt.Sprintf("Current usage: %d files, %s", fc, FormatBytes(sz))
				return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, text)
					lbl.Color = ColorAccent
					return lbl.Layout(gtx)
				})
			}),
			// Expiration info
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if share.ExpiresAt == nil {
					return layout.Dimensions{}
				}
				text := fmt.Sprintf("Expires: %s", share.ExpiresAt.Local().Format("2006-01-02 15:04"))
				if time.Now().After(*share.ExpiresAt) {
					text += " (EXPIRED)"
				}
				return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, text)
					if time.Now().After(*share.ExpiresAt) {
						lbl.Color = ColorDanger
					} else {
						lbl.Color = ColorWarning
					}
					return lbl.Layout(gtx)
				})
			}),
			// Max File Size
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutLimitField(gtx, "Max File Size (MB)", &v.limitFileSizeEditor)
			}),
			// Storage Quota
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutLimitField(gtx, "Storage Quota (MB)", &v.limitQuotaEditor)
			}),
			// Max Files
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutLimitField(gtx, "Max Files Count", &v.limitFilesEditor)
			}),
			// Expiry Hours
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutLimitField(gtx, "Expiry (hours from now, 0 = none)", &v.limitExpiryEditor)
			}),
			// Save button
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					btn := material.Button(v.app.Theme.Material, &v.saveLimitsBtn, "Save Limits")
					btn.Background = ColorAccent
					btn.Color = ColorText
					return btn.Layout(gtx)
				})
			}),
		)
	})
}

func (v *SharesView) layoutLimitField(gtx layout.Context, label string, editor *widget.Editor) layout.Dimensions {
	editor.SingleLine = true
	editor.Filter = "0123456789"
	return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, label)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(200)
					ed := material.Editor(v.app.Theme.Material, editor, "0")
					ed.Color = ColorText
					ed.HintColor = ColorTextDim
					return layoutEditorBg(gtx, ed)
				})
			}),
		)
	})
}

func layoutEditorBg(gtx layout.Context, ed material.EditorStyle) layout.Dimensions {
	bgClr := color.NRGBA{R: 30, G: 30, B: 40, A: 255}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			r := gtx.Dp(4)
			paint.FillShape(gtx.Ops, bgClr, clip.RRect{
				Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y),
				NE: r, NW: r, SE: r, SW: r,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, ed.Layout)
		}),
	)
}

func (v *SharesView) loadPermissions() {
	shareID := v.ActiveShareID
	go func() {
		conn := v.app.Conn()
		if conn == nil {
			return
		}
		perms, err := conn.Client.GetSharePermissions(shareID)
		if err != nil {
			log.Printf("GetSharePermissions error: %v", err)
			return
		}
		v.app.mu.Lock()
		v.Permissions = perms
		// Find global settings and set checkboxes
		for _, p := range perms {
			if p.GranteeID == nil {
				v.globalReadCheck.Value = p.CanRead
				v.globalWriteCheck.Value = p.CanWrite
				v.globalDeleteCheck.Value = p.CanDelete
				v.globalBlockedCheck.Value = p.IsBlocked
				break
			}
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *SharesView) layoutPermissions(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}

	// Click handlery
	// Write implikuje Read
	if v.globalWriteCheck.Value && !v.globalReadCheck.Value {
		v.globalReadCheck.Value = true
	}
	if v.saveGlobalBtn.Clicked(gtx) {
		shareID := v.ActiveShareID
		canRead := v.globalReadCheck.Value
		canWrite := v.globalWriteCheck.Value
		isBlocked := v.globalBlockedCheck.Value
		go func() {
			c := v.app.Conn()
			if c == nil {
				return
			}
			if _, err := c.Client.SetSharePermission(shareID, nil, canRead, canWrite, canWrite, isBlocked); err != nil {
				log.Printf("SetSharePermission (global) error: %v", err)
			}
			v.loadPermissions()
		}()
	}

	// Delete per-user permission
	for i := range v.permDelBtns {
		if i < len(v.Permissions) && v.permDelBtns[i].Clicked(gtx) {
			perm := v.Permissions[i]
			if perm.GranteeID == nil {
				continue // don't allow deleting global
			}
			permID := perm.ID
			shareID := v.ActiveShareID
			go func() {
				c := v.app.Conn()
				if c == nil {
					return
				}
				if err := c.Client.DeleteSharePermission(shareID, permID); err != nil {
					log.Printf("DeleteSharePermission error: %v", err)
				}
				v.loadPermissions()
			}()
		}
	}

	// Add permission for a server member (member picker click handlers)
	// Handled v layoutAddMembersSection

	// Write implikuje Read (per-user)
	if v.editWriteCheck.Value && !v.editReadCheck.Value {
		v.editReadCheck.Value = true
	}
	// Save edited per-user permission
	if v.savePermBtn.Clicked(gtx) && v.editPermIdx >= 0 && v.editPermIdx < len(v.Permissions) {
		perm := v.Permissions[v.editPermIdx]
		if perm.GranteeID != nil {
			shareID := v.ActiveShareID
			granteeID := *perm.GranteeID
			canRead := v.editReadCheck.Value
			canWrite := v.editWriteCheck.Value
			isBlocked := v.editBlockedCheck.Value
			go func() {
				c := v.app.Conn()
				if c == nil {
					return
				}
				gid := granteeID
				if _, err := c.Client.SetSharePermission(shareID, &gid, canRead, canWrite, canWrite, isBlocked); err != nil {
					log.Printf("SetSharePermission error: %v", err)
				}
				v.loadPermissions()
			}()
		}
		v.editPermIdx = -1
	}
	if v.cancelPermBtn.Clicked(gtx) {
		v.editPermIdx = -1
	}

	// Split permissions into global and per-user
	var globalPerm *api.SharePermission
	var userPerms []api.SharePermission
	for i := range v.Permissions {
		if v.Permissions[i].GranteeID == nil {
			globalPerm = &v.Permissions[i]
		} else {
			userPerms = append(userPerms, v.Permissions[i])
		}
	}

	// Ensure enough buttons
	if len(v.permEditBtns) < len(v.Permissions)+1 {
		v.permEditBtns = make([]widget.Clickable, len(v.Permissions)+10)
	}
	if len(v.permDelBtns) < len(v.Permissions)+1 {
		v.permDelBtns = make([]widget.Clickable, len(v.Permissions)+10)
	}

	type permListItem struct {
		kind string // "global_header", "global_row", "divider", "user_header", "user_row", "add_row", "edit_row"
		idx  int
	}
	var items []permListItem
	items = append(items, permListItem{kind: "global_header"})
	items = append(items, permListItem{kind: "global_row"})
	items = append(items, permListItem{kind: "divider"})
	items = append(items, permListItem{kind: "user_header"})
	for i := range userPerms {
		if v.editPermIdx >= 0 && v.editPermIdx < len(v.Permissions) && v.Permissions[v.editPermIdx].ID == userPerms[i].ID {
			items = append(items, permListItem{kind: "edit_row", idx: i})
		} else {
			items = append(items, permListItem{kind: "user_row", idx: i})
		}
	}
	// Build a list of members without per-user permissions (for member picker)
	hasPermSet := make(map[string]bool)
	for _, p := range userPerms {
		if p.GranteeID != nil {
			hasPermSet[*p.GranteeID] = true
		}
	}
	searchQuery := strings.ToLower(strings.TrimSpace(v.memberSearchEd.Text()))
	var availableMembers []api.User
	if conn != nil {
		v.app.mu.RLock()
		for _, m := range conn.Members {
			if m.ID != conn.UserID && !hasPermSet[m.ID] {
				if searchQuery != "" {
					name := strings.ToLower(v.app.ResolveUserName(&m))
					if !strings.Contains(name, searchQuery) && !strings.Contains(strings.ToLower(m.Username), searchQuery) {
						continue
					}
				}
				availableMembers = append(availableMembers, m)
			}
		}
		v.app.mu.RUnlock()
	}
	if len(v.addPermBtns) < len(availableMembers)+1 {
		v.addPermBtns = make([]widget.Clickable, len(availableMembers)+10)
	}

	// Handle member picker clicks
	for i, m := range availableMembers {
		if i < len(v.addPermBtns) && v.addPermBtns[i].Clicked(gtx) {
			memberID := m.ID
			shareID := v.ActiveShareID
			go func() {
				c := v.app.Conn()
				if c == nil {
					return
				}
				uid := memberID
				if _, err := c.Client.SetSharePermission(shareID, &uid, true, false, false, false); err != nil {
					log.Printf("SetSharePermission error: %v", err)
				}
				v.loadPermissions()
			}()
		}
	}

	// Always show the add user section when there are members without perms (or search is active)
	showAddSection := len(availableMembers) > 0 || searchQuery != ""
	if conn != nil && !showAddSection {
		// Check if there are ANY members without perms (before search filter)
		v.app.mu.RLock()
		for _, m := range conn.Members {
			if m.ID != conn.UserID && !hasPermSet[m.ID] {
				showAddSection = true
				break
			}
		}
		v.app.mu.RUnlock()
	}
	if showAddSection {
		items = append(items, permListItem{kind: "divider"})
		items = append(items, permListItem{kind: "add_header"})
		items = append(items, permListItem{kind: "add_search"})
		for i := range availableMembers {
			items = append(items, permListItem{kind: "add_member", idx: i})
		}
	}

	return material.List(v.app.Theme.Material, &v.permList).Layout(gtx, len(items), func(gtx layout.Context, idx int) layout.Dimensions {
		item := items[idx]
		switch item.kind {
		case "global_header":
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(v.app.Theme.Material, "Default Permissions")
				lbl.Color = ColorText
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			})
		case "global_row":
			return v.layoutGlobalPerms(gtx, globalPerm)
		case "divider":
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, v.layoutDivider)
		case "user_header":
			return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(v.app.Theme.Material, "User Permissions")
				lbl.Color = ColorText
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			})
		case "user_row":
			perm := userPerms[item.idx]
			origIdx := 0
			for j := range v.Permissions {
				if v.Permissions[j].ID == perm.ID {
					origIdx = j
					break
				}
			}
			return v.layoutUserPermRow(gtx, perm, origIdx)
		case "edit_row":
			return v.layoutEditPermRow(gtx)
		case "add_header":
			return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(v.app.Theme.Material, "Add User")
				lbl.Color = ColorText
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			})
		case "add_search":
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(4)
						paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
							Rect: image.Rect(0, 0, sz.X, sz.Y),
							NE: rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							ed := material.Editor(v.app.Theme.Material, &v.memberSearchEd, "Search users...")
							ed.Color = ColorText
							ed.HintColor = ColorTextDim
							ed.TextSize = unit.Sp(13)
							return ed.Layout(gtx)
						})
					},
				)
			})
		case "add_member":
			m := availableMembers[item.idx]
			return v.layoutMemberPickerRow(gtx, &v.addPermBtns[item.idx], m)
		}
		return layout.Dimensions{}
	})
}

func (v *SharesView) layoutGlobalPerms(gtx layout.Context, perm *api.SharePermission) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "These permissions apply to all users by default")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							cb := material.CheckBox(v.app.Theme.Material, &v.globalReadCheck, "Read")
							cb.Color = ColorText
							cb.IconColor = ColorAccent
							return cb.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								cb := material.CheckBox(v.app.Theme.Material, &v.globalWriteCheck, "Write + Delete")
								cb.Color = ColorText
								cb.IconColor = ColorAccent
								return cb.Layout(gtx)
							})
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Dimensions{}
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							btn := material.Button(v.app.Theme.Material, &v.saveGlobalBtn, "Save")
							btn.Background = ColorAccent
							btn.Color = ColorText
							btn.TextSize = unit.Sp(13)
							btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
							return btn.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (v *SharesView) layoutUserPermRow(gtx layout.Context, perm api.SharePermission, origIdx int) layout.Dimensions {
	bg := ColorCard
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(40))}.Op())
		return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Username
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					name := perm.GranteeName
					if name == "" && perm.GranteeID != nil {
						name = (*perm.GranteeID)[:8] + "..."
					}
					lbl := material.Body2(v.app.Theme.Material, name)
					lbl.Color = ColorText
					gtx.Constraints.Min.X = gtx.Dp(120)
					return lbl.Layout(gtx)
				}),
				// Permissions badges
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						v.layoutPermBadge(gtx, "R", perm.CanRead, ColorAccent),
						v.layoutPermBadge(gtx, "W+D", perm.CanWrite, color.NRGBA{R: 230, G: 180, B: 40, A: 255}),
						v.layoutPermBadge(gtx, "Blocked", perm.IsBlocked, color.NRGBA{R: 200, G: 60, B: 60, A: 255}),
					)
				}),
				// Edit button
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					btn := &widget.Clickable{}
					if origIdx < len(v.permEditBtns) {
						btn = &v.permEditBtns[origIdx]
					}
					if btn.Clicked(gtx) {
						v.editPermIdx = origIdx
						v.editReadCheck.Value = perm.CanRead
						v.editWriteCheck.Value = perm.CanWrite
						v.editDeleteCheck.Value = perm.CanDelete
						v.editBlockedCheck.Value = perm.IsBlocked
					}
					return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconEdit, 16, ColorTextDim)
					})
				}),
			)
		})
	})
}

func (v *SharesView) layoutPermBadge(gtx layout.Context, label string, active bool, clr color.NRGBA) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		if !active {
			return layout.Dimensions{}
		}
		return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			dims := layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, label)
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
			call := macro.Stop()
			r := gtx.Dp(4)
			paint.FillShape(gtx.Ops, clr, clip.RRect{
				Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
				NE: r, NW: r, SE: r, SW: r,
			}.Op(gtx.Ops))
			call.Add(gtx.Ops)
			return dims
		})
	})
}

func (v *SharesView) layoutEditPermRow(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bgClr := color.NRGBA{R: 40, G: 40, B: 55, A: 255}
		paint.FillShape(gtx.Ops, bgClr, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Checkboxy
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							cb := material.CheckBox(v.app.Theme.Material, &v.editReadCheck, "Read")
							cb.Color = ColorText
							cb.IconColor = ColorAccent
							return cb.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								cb := material.CheckBox(v.app.Theme.Material, &v.editWriteCheck, "Write + Delete")
								cb.Color = ColorText
								cb.IconColor = ColorAccent
								return cb.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								cb := material.CheckBox(v.app.Theme.Material, &v.editBlockedCheck, "Blocked")
								cb.Color = ColorText
								cb.IconColor = ColorDanger
								return cb.Layout(gtx)
							})
						}),
					)
				}),
				// Save/Cancel/Delete buttons
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								btn := material.Button(v.app.Theme.Material, &v.savePermBtn, "Save")
								btn.Background = ColorAccent
								btn.Color = ColorText
								btn.TextSize = unit.Sp(13)
								btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
								return btn.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									btn := material.Button(v.app.Theme.Material, &v.cancelPermBtn, "Cancel")
									btn.Background = ColorHover
									btn.Color = ColorText
									btn.TextSize = unit.Sp(13)
									btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
									return btn.Layout(gtx)
								})
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{}
							}),
							// Delete permission
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if v.editPermIdx < 0 || v.editPermIdx >= len(v.Permissions) {
									return layout.Dimensions{}
								}
								perm := v.Permissions[v.editPermIdx]
								if perm.GranteeID == nil {
									return layout.Dimensions{}
								}
								permID := perm.ID
								shareID := v.ActiveShareID
								delBtn := &widget.Clickable{}
								if v.editPermIdx < len(v.permDelBtns) {
									delBtn = &v.permDelBtns[v.editPermIdx]
								}
								if delBtn.Clicked(gtx) {
									go func() {
										c := v.app.Conn()
										if c == nil {
											return
										}
										if err := c.Client.DeleteSharePermission(shareID, permID); err != nil {
											log.Printf("DeleteSharePermission error: %v", err)
										}
										v.editPermIdx = -1
										v.loadPermissions()
									}()
								}
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									btn := material.Button(v.app.Theme.Material, delBtn, "Remove")
									btn.Background = ColorDanger
									btn.Color = ColorText
									btn.TextSize = unit.Sp(13)
									btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
									return btn.Layout(gtx)
								})
							}),
						)
					})
				}),
			)
		})
	})
}

func (v *SharesView) layoutMemberPickerRow(gtx layout.Context, btn *widget.Clickable, m api.User) layout.Dimensions {
	bg := ColorCard
	if btn.Hovered() {
		bg = ColorHover
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(36))}.Op())
		return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(24), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconAdd, 16, ColorAccent)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(v.app.Theme.Material, m.Username)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	})
}

func (v *SharesView) layoutFileList(gtx layout.Context) layout.Dimensions {
	fw := v.fileWidget
	sorted := v.sortedFiles

	if len(sorted) == 0 {
		return layoutCentered(gtx, v.app.Theme, "No files in this folder", ColorTextDim)
	}

	conn := v.app.Conn()
	swarmEnabled := conn != nil && conn.SwarmSharingEnabled

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Column headers
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return fw.LayoutColumnHeaders(gtx)
		}),
		// File list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(v.app.Theme.Material, &v.mainList).Layout(gtx, len(sorted), func(gtx layout.Context, i int) layout.Dimensions {
				entry := sorted[i]
				selected := fw.Selected[i]
				hovered := i < len(v.fileBtns) && v.fileBtns[i].Hovered()

				// Build extra widgets: swarm badge, swarm button, download button
				var extras []layout.Widget

				// Seed count badge
				seedCount := 0
				if swarmEnabled && !entry.IsDir && v.swarmCounts != nil {
					seedCount = v.swarmCounts[entry.ID]
				}
				if seedCount > 0 {
					count := seedCount
					extras = append(extras, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("%d seeds", count))
						lbl.Color = ColorOnline
						return lbl.Layout(gtx)
					})
					// Swarm button
					if i < len(v.swarmBtns) {
						btn := &v.swarmBtns[i]
						extras = append(extras, func(gtx layout.Context) layout.Dimensions {
							return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutSmallButton(gtx, v.app.Theme, "Swarm", ColorOnline, btn.Hovered())
							})
						})
					}
				}

				// Download button (files only)
				if !entry.IsDir && i < len(v.downloadBtns) {
					btn := &v.downloadBtns[i]
					extras = append(extras, func(gtx layout.Context) layout.Dimensions {
						return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconDownload, 16, ColorAccent)
						})
					})
				}

				return v.fileBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return fw.LayoutEntryRow(gtx, entry, i, selected, hovered, extras...)
				})
			})
		}),
	)
}

// findOrigFileByID looks up the original SharedFileEntry by ID.
func (v *SharesView) findOrigFileByID(id string) *api.SharedFileEntry {
	for i := range v.Files {
		if v.Files[i].ID == id {
			return &v.Files[i]
		}
	}
	return nil
}

func (v *SharesView) layoutShareItem(gtx layout.Context, btn *widget.Clickable, name, owner string, online, active bool) layout.Dimensions {
	bg := color.NRGBA{A: 0}
	if active {
		bg = ColorHover
	} else if btn.Hovered() {
		bg = color.NRGBA{R: 255, G: 255, B: 255, A: 15}
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(40))}.Op())

		return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Folder icon
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconFolder, 18, ColorAccent)
				}),
				// Name + owner
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						if owner != "" {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, name)
									lbl.Color = ColorText
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, owner)
									lbl.Color = ColorTextDim
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								}),
							)
						}
						lbl := material.Body2(v.app.Theme.Material, name)
						lbl.Color = ColorText
						lbl.MaxLines = 1
						return lbl.Layout(gtx)
					})
				}),
				// Online indicator
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(8)
					clr := color.NRGBA{R: 100, G: 100, B: 100, A: 255}
					if online {
						clr = color.NRGBA{R: 60, G: 180, B: 75, A: 255}
					}
					paint.FillShape(gtx.Ops, clr, clip.RRect{
						Rect: image.Rect(0, 0, sz, sz),
						NE: sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: image.Pt(sz, sz)}
				}),
			)
		})
	})
}

func (v *SharesView) layoutShareItemWithMount(gtx layout.Context, btn *widget.Clickable, name, owner string, online, active, mounted bool, mountBtn, unmountBtn *widget.Clickable) layout.Dimensions {
	bg := color.NRGBA{A: 0}
	if active {
		bg = ColorHover
	} else if btn.Hovered() {
		bg = color.NRGBA{R: 255, G: 255, B: 255, A: 15}
	}

	return layout.Inset{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(50))}.Op())

		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Main row (clickable)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconFolder, 18, ColorAccent)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(v.app.Theme.Material, name)
											lbl.Color = ColorText
											lbl.MaxLines = 1
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if owner == "" {
												return layout.Dimensions{}
											}
											lbl := material.Caption(v.app.Theme.Material, owner)
											lbl.Color = ColorTextDim
											lbl.MaxLines = 1
											return lbl.Layout(gtx)
										}),
									)
								})
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								sz := gtx.Dp(8)
								clr := color.NRGBA{R: 100, G: 100, B: 100, A: 255}
								if online {
									clr = color.NRGBA{R: 60, G: 180, B: 75, A: 255}
								}
								paint.FillShape(gtx.Ops, clr, clip.RRect{
									Rect: image.Rect(0, 0, sz, sz),
									NE: sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
								}.Op(gtx.Ops))
								return layout.Dimensions{Size: image.Pt(sz, sz)}
							}),
						)
					})
				}),
				// Mount/Unmount button
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(26), Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						if mounted && unmountBtn != nil {
							return unmountBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconClose, 12, ColorDanger)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, "Unmount")
											lbl.Color = ColorDanger
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						}
						if mountBtn != nil {
							return mountBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconFolder, 12, ColorAccent)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, "Mount")
											lbl.Color = ColorAccent
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						}
						return layout.Dimensions{}
					})
				}),
			)
		})
	})
}

func (v *SharesView) layoutP2PFileItem(gtx layout.Context, stopBtn *widget.Clickable, f p2p.RegisteredFile) layout.Dimensions {
	bg := ColorCard
	if stopBtn.Hovered() {
		bg = ColorHover
	}

	return layout.UniformInset(unit.Dp(0)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(44))}.Op())

		return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconFile, 18, ColorTextDim)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, f.FileName)
								lbl.Color = ColorText
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, FormatBytes(f.FileSize))
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
						)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return stopBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconClose, 16, ColorDanger)
					})
				}),
			)
		})
	})
}

func (v *SharesView) layoutDivider(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
	paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
	return layout.Dimensions{Size: size}
}

func (v *SharesView) layoutServerInfo(gtx layout.Context, share *api.SharedDirectory) layout.Dimensions {
	if share == nil {
		return layout.Dimensions{}
	}
	th := v.app.Theme

	v.app.mu.RLock()
	servers := make([]*ServerConnection, len(v.app.Servers))
	copy(servers, v.app.Servers)
	v.app.mu.RUnlock()

	type serverStatus struct {
		idx      int
		conn     *ServerConnection
		name     string
		isShared bool
		shareID  string
	}
	var statuses []serverStatus
	for i, s := range servers {
		name := s.Name
		if name == "" {
			name = s.URL
		}
		found := false
		foundID := ""
		for _, ms := range s.MyShares {
			if ms.PathHash == share.PathHash {
				found = true
				foundID = ms.ID
				break
			}
		}
		statuses = append(statuses, serverStatus{idx: i, conn: s, name: name, isShared: found, shareID: foundID})
	}

	// Ensure toggle buttons
	if len(v.serverToggleBtns) < len(statuses) {
		v.serverToggleBtns = make([]widget.Clickable, len(statuses)+5)
	}

	// Handle toggle clicks
	for i, st := range statuses {
		if v.serverToggleBtns[i].Clicked(gtx) {
			s := st
			pathHash := share.PathHash
			displayName := share.DisplayName
			if s.isShared {
				// Confirm before disabling
				sid := s.shareID
				sconn := s.conn
				sname := s.name
				v.app.ConfirmDlg.Show("Stop Sharing", fmt.Sprintf("Stop sharing \"%s\" on server \"%s\"?", displayName, sname), func() {
					go func() {
						if err := sconn.Client.DeleteShare(sid); err != nil {
							log.Printf("DeleteShare on server %s: %v", sname, err)
						}
						v.app.mu.Lock()
						delete(sconn.SharePaths, sid)
						v.app.mu.Unlock()
						v.persistSharePaths()
						v.loadShareList()
						v.app.Window.Invalidate()
					}()
				})
			} else {
				// Enable — no confirm needed
				sconn := s.conn
				go func() {
					var localPath string
					v.app.mu.RLock()
					for _, srv := range v.app.Servers {
						for _, ms := range srv.MyShares {
							if ms.PathHash == pathHash {
								if lp, ok := srv.SharePaths[ms.ID]; ok {
									localPath = lp
									break
								}
							}
						}
						if localPath != "" {
							break
						}
					}
					v.app.mu.RUnlock()
					if localPath == "" {
						log.Printf("Cannot find local path for share %s", displayName)
						return
					}
					v.createShareOnServer(sconn, localPath, pathHash, displayName)
				}()
			}
		}
	}

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var items []layout.FlexChild

		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th.Material, "Shared on Servers")
			lbl.Color = ColorText
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th.Material, "Click a server to enable or disable sharing on it.")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))

		for i, st := range statuses {
			s := st
			btnIdx := i
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							clr := ColorDanger
							if s.isShared {
								clr = ColorOnline
							}
							return layoutIcon(gtx, IconMonitor, 18, clr)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th.Material, s.name)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := "Enable"
							btnBg := ColorAccent
							if s.isShared {
								label = "Disable"
								btnBg = ColorDanger
							}
							btn := material.Button(th.Material, &v.serverToggleBtns[btnIdx], label)
							btn.Background = btnBg
							btn.Color = color.NRGBA{255, 255, 255, 255}
							btn.CornerRadius = unit.Dp(3)
							btn.TextSize = unit.Sp(12)
							btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}
							return btn.Layout(gtx)
						}),
					)
				})
			}))
		}

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
	})
}

func (v *SharesView) layoutServerPicker(gtx layout.Context) layout.Dimensions {
	th := v.app.Theme

	// Handle OK button
	if v.serverPickerOkBtn.Clicked(gtx) {
		v.confirmServerPicker()
	}

	v.app.mu.RLock()
	servers := make([]*ServerConnection, len(v.app.Servers))
	copy(servers, v.app.Servers)
	v.app.mu.RUnlock()

	// Ensure checkboxes match server count
	if len(v.serverCheckboxes) < len(servers) {
		old := v.serverCheckboxes
		v.serverCheckboxes = make([]widget.Bool, len(servers))
		copy(v.serverCheckboxes, old)
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(380)
		gtx.Constraints.Min.X = gtx.Dp(380)
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
					Rect: image.Rect(0, 0, sz.X, sz.Y),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var items []layout.FlexChild

					// Title
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(th.Material, "Share \""+v.pendingShareName+"\" on servers:")
						lbl.Color = ColorText
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}))
					items = append(items, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout))

					// Server checkboxes
					for i, s := range servers {
						idx := i
						name := s.Name
						if name == "" {
							name = s.URL
						}
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								cb := material.CheckBox(th.Material, &v.serverCheckboxes[idx], name)
								cb.Color = ColorText
								cb.IconColor = ColorAccent
								cb.Size = unit.Dp(18)
								cb.TextSize = unit.Sp(14)
								return cb.Layout(gtx)
							})
						}))
					}

					items = append(items, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout))

					// OK + Cancel buttons
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if v.serverPickerCancelBtn.Clicked(gtx) {
									v.showServerPicker = false
								}
								btn := material.Button(th.Material, &v.serverPickerCancelBtn, "Cancel")
								btn.Background = ColorInput
								btn.Color = ColorText
								btn.CornerRadius = unit.Dp(4)
								btn.TextSize = unit.Sp(13)
								btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}
								return btn.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								btn := material.Button(th.Material, &v.serverPickerOkBtn, "Share")
								btn.Background = ColorAccent
								btn.Color = color.NRGBA{255, 255, 255, 255}
								btn.CornerRadius = unit.Dp(4)
								btn.TextSize = unit.Sp(13)
								btn.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}
								return btn.Layout(gtx)
							}),
						)
					}))

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
				})
			},
		)
	})
}

// --- Akce ---

func (v *SharesView) selectShare(shareID string) {
	v.ActiveShareID = shareID
	v.BrowsePath = "/"
	v.ShowPerms = false
	v.ShowLimits = false
	v.loadFiles()
}

func (v *SharesView) loadFiles() {
	shareID := v.ActiveShareID
	path := v.BrowsePath
	go func() {
		conn := v.app.Conn()
		if conn == nil {
			return
		}
		files, err := conn.Client.GetShareFiles(shareID, path)
		if err != nil {
			log.Printf("GetShareFiles error: %v", err)
			return
		}
		v.app.mu.Lock()
		v.Files = files
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
	v.loadSwarmCounts()
}

func (v *SharesView) addShare() {
	go func() {
		// Open native dialog for directory selection
		dir, err := pickDirectory()
		if err != nil || dir == "" {
			return
		}

		// Hash the path (server does not know the actual path)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(dir)))

		// Display name = last path component
		name := filepath.Base(dir)
		if name == "." || name == "/" {
			name = "Shared Folder"
		}

		// If connected to multiple servers, show server picker
		v.app.mu.RLock()
		numServers := len(v.app.Servers)
		v.app.mu.RUnlock()

		if numServers > 1 {
			v.pendingShareDir = dir
			v.pendingShareHash = hash
			v.pendingShareName = name
			v.app.mu.RLock()
			v.serverCheckboxes = make([]widget.Bool, numServers)
			// Pre-check the active server
			for i := range v.serverCheckboxes {
				v.serverCheckboxes[i].Value = i == v.app.ActiveServer
			}
			v.app.mu.RUnlock()
			v.showServerPicker = true
			v.app.Window.Invalidate()
			return
		}

		// Single server — create immediately
		v.createShareOnServer(v.app.Conn(), dir, hash, name)
	}()
}

func (v *SharesView) createShareOnServer(conn *ServerConnection, dir, hash, name string) {
	if conn == nil {
		return
	}
	newDir, err := conn.Client.CreateShare(hash, name)
	if err != nil {
		log.Printf("CreateShare error: %v", err)
		return
	}

	v.app.mu.Lock()
	conn.SharePaths[newDir.ID] = dir
	v.app.mu.Unlock()
	v.persistSharePaths()

	v.syncShareFiles(newDir.ID, dir)
	v.loadShareList()
	v.app.Window.Invalidate()
}

func (v *SharesView) confirmServerPicker() {
	dir := v.pendingShareDir
	hash := v.pendingShareHash
	name := v.pendingShareName
	v.showServerPicker = false

	v.app.mu.RLock()
	servers := make([]*ServerConnection, len(v.app.Servers))
	copy(servers, v.app.Servers)
	selected := make([]bool, len(v.serverCheckboxes))
	for i := range v.serverCheckboxes {
		selected[i] = v.serverCheckboxes[i].Value
	}
	v.app.mu.RUnlock()

	go func() {
		for i, s := range servers {
			if selected[i] {
				v.createShareOnServer(s, dir, hash, name)
			}
		}
	}()
}

func (v *SharesView) syncLocalFiles() {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	shareID := v.ActiveShareID
	v.app.mu.RLock()
	localPath, ok := conn.SharePaths[shareID]
	v.app.mu.RUnlock()

	if !ok || localPath == "" {
		return
	}

	go func() {
		v.syncShareFiles(shareID, localPath)
		v.loadFiles()
	}()
}

func (v *SharesView) syncShareFiles(shareID, localPath string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	var files []map[string]interface{}
	err := filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip symlinks (security — we do not want to serve /etc/shadow etc.)
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		rel, _ := filepath.Rel(localPath, path)
		if rel == "." {
			return nil
		}

		// Path traversal protection
		if strings.Contains(rel, "..") {
			return nil
		}

		parent := filepath.Dir(rel)
		if parent == "." {
			parent = "/"
		} else {
			parent = "/" + filepath.ToSlash(parent)
		}

		files = append(files, map[string]interface{}{
			"relative_path": parent,
			"file_name":     info.Name(),
			"file_size":     info.Size(),
			"is_dir":        info.IsDir(),
			"file_hash":     "",
		})

		return nil
	})
	if err != nil {
		log.Printf("Walk error: %v", err)
		return
	}

	if err := conn.Client.SyncShareFiles(shareID, files); err != nil {
		log.Printf("SyncShareFiles error: %v", err)
	}
}

func (v *SharesView) loadShareList() {
	conn := v.app.Conn()
	if conn == nil {
		return
	}

	go func() {
		resp, err := conn.Client.GetShares()
		if err != nil {
			log.Printf("GetShares error: %v", err)
			return
		}
		v.app.mu.Lock()
		if resp.Own != nil {
			conn.MyShares = resp.Own
		} else {
			conn.MyShares = []api.SharedDirectory{}
		}
		if resp.Accessible != nil {
			conn.SharedWithMe = resp.Accessible
		} else {
			conn.SharedWithMe = []api.SharedDirectory{}
		}
		// Aktualizovat canWrite na mountech
		if conn.Mounts != nil {
			for _, s := range conn.SharedWithMe {
				conn.Mounts.UpdateCanWrite(s.ID, s.CanWrite)
			}
		}
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *SharesView) requestDownload(f api.SharedFileEntry) {
	shareID := v.ActiveShareID
	browsePath := v.BrowsePath

	go func() {
		conn := v.app.Conn()
		if conn == nil {
			return
		}

		// Find ownerID
		v.app.mu.RLock()
		var ownerID string
		for _, s := range conn.MyShares {
			if s.ID == shareID {
				ownerID = s.OwnerID
				break
			}
		}
		for _, s := range conn.SharedWithMe {
			if s.ID == shareID {
				ownerID = s.OwnerID
				break
			}
		}
		v.app.mu.RUnlock()

		resp, err := conn.Client.RequestTransfer(shareID, f.ID)
		if err != nil {
			log.Printf("RequestTransfer error: %v", err)
			return
		}

		transferID, _ := resp["transfer_id"].(string)
		if transferID == "" {
			log.Printf("RequestTransfer: no transfer_id in response")
			return
		}

		// Prepare save path to cache
		cache := mount.NewCache()
		if err := cache.EnsureDir(conn.URL, shareID, browsePath); err != nil {
			log.Printf("EnsureDir error: %v", err)
			return
		}
		savePath := cache.FilePath(conn.URL, shareID, browsePath, f.FileName)

		log.Printf("Download: transfer_id=%s, owner=%s, save=%s", transferID, ownerID, savePath)

		// Small delay — owner must process transfer.request and register the file
		time.Sleep(500 * time.Millisecond)

		// Start P2P download (sends file.request to owner)
		if conn.P2P != nil && ownerID != "" {
			conn.P2P.RequestDownload(ownerID, transferID, savePath)
		}
	}()
}

func (v *SharesView) requestSwarmDownload(f api.SharedFileEntry) {
	shareID := v.ActiveShareID
	browsePath := v.BrowsePath

	// C21 fix: check for empty shareID
	if shareID == "" {
		return
	}

	go func() {
		conn := v.app.Conn()
		if conn == nil || conn.Swarm == nil {
			return
		}

		resp, err := conn.Client.SwarmRequest(shareID, f.ID)
		if err != nil {
			log.Printf("SwarmRequest error: %v", err)
			return
		}

		if resp.TransferID == "" || len(resp.Sources) == 0 {
			log.Printf("SwarmRequest: no transfer_id or sources")
			return
		}

		// Prepare save path to cache
		cache := mount.NewCache()
		if err := cache.EnsureDir(conn.URL, shareID, browsePath); err != nil {
			log.Printf("EnsureDir error: %v", err)
			return
		}
		savePath := cache.FilePath(conn.URL, shareID, browsePath, f.FileName)

		// C13 fix: path traversal protection — savePath must be inside cache dir
		cacheBase := cache.Dir()
		cleanSave := filepath.Clean(savePath)
		cleanBase := filepath.Clean(cacheBase)
		if !strings.HasPrefix(cleanSave, cleanBase+string(filepath.Separator)) && cleanSave != cleanBase {
			log.Printf("Swarm: path traversal detected: %s not under %s", cleanSave, cleanBase)
			return
		}

		// Extrahovat source user IDs
		var sourceIDs []string
		for _, s := range resp.Sources {
			if s.Online {
				sourceIDs = append(sourceIDs, s.UserID)
			}
		}

		if len(sourceIDs) == 0 {
			log.Printf("SwarmRequest: no online sources")
			return
		}

		log.Printf("Swarm download: transfer=%s, sources=%d, pieces=%d, save=%s",
			resp.TransferID, len(sourceIDs), resp.TotalPieces, savePath)

		if err := conn.Swarm.StartDownload(resp.TransferID, shareID, f.ID, f.FileName, savePath,
			resp.FileSize, resp.PieceSize, resp.TotalPieces, sourceIDs); err != nil {
			log.Printf("Swarm StartDownload error: %v", err)
			return
		}

		// C6 fix: after completion register as seeder; max 5min timeout
		transferID := resp.TransferID
		go func() {
			for i := 0; i < 150; i++ { // max 5 minut (150 × 2s)
				time.Sleep(2 * time.Second)
				if conn.Swarm.IsDownloaded(transferID) {
					conn.Swarm.RegisterSeedFile(shareID, f.ID, savePath, f.FileName, resp.FileSize)
					conn.Client.SwarmAddSeed(shareID, f.ID)
					log.Printf("Swarm: auto-seeding %s", f.FileName)
					v.loadSwarmCounts()
					v.app.Window.Invalidate()
					return
				}
				if conn.Swarm.IsFailed(transferID) {
					log.Printf("Swarm: download failed: %s", f.FileName)
					return
				}
				dl := conn.Swarm.GetDownload(transferID)
				if dl == nil {
					return
				}
			}
			log.Printf("Swarm: auto-seed timeout for %s", f.FileName)
		}()
	}()
}

func (v *SharesView) loadSwarmCounts() {
	shareID := v.ActiveShareID
	conn := v.app.Conn()
	if conn == nil || !conn.SwarmSharingEnabled {
		return
	}
	go func() {
		counts, err := conn.Client.SwarmCounts(shareID)
		if err != nil {
			log.Printf("SwarmCounts error: %v", err)
			return
		}
		v.app.mu.Lock()
		v.swarmCounts = counts
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

// persistSharePaths saves SharePaths to identities.json
func (v *SharesView) persistSharePaths() {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	paths := make(map[string]string, len(conn.SharePaths))
	for k, val := range conn.SharePaths {
		paths[k] = val
	}
	store.UpdateSharePaths(v.app.PublicKey, conn.URL, paths)
}

// persistMountedShares saves currently mounted shares to identities.json
func (v *SharesView) persistMountedShares(conn *ServerConnection) {
	if conn == nil || conn.Mounts == nil {
		return
	}
	mounts := conn.Mounts.GetAllMounts()
	mounted := make(map[string]store.MountedShareInfo, len(mounts))
	for _, m := range mounts {
		mounted[m.ShareID] = store.MountedShareInfo{
			DisplayName: m.ShareName,
			DriveLetter: m.DriveLetter,
			Port:        m.Port,
			CanWrite:    m.CanWrite,
		}
	}
	store.UpdateMountedShares(v.app.PublicKey, conn.URL, mounted)
}


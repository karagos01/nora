package ui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type FileSource int

const (
	SourceAll FileSource = iota
	SourceAttachments
	SourceStorage
	SourceShares
	SourceP2P
)

type FileTypeFilter int

const (
	TypeAll FileTypeFilter = iota
	TypeImages
	TypeVideo
	TypeAudio
	TypeDocuments
)

type LibraryItem struct {
	FileName   string
	FileSize   int64
	MimeType   string
	URL        string
	Source     FileSource
	SourceName string
	UploadedBy string
	CreatedAt  time.Time
}

type LibraryView struct {
	app *App

	// Sidebar
	sideList    widget.List
	backBtn     widget.Clickable
	sourceBtns  [5]widget.Clickable
	typeBtns    [5]widget.Clickable
	channelBtns []widget.Clickable
	userBtns    []widget.Clickable

	// Main
	mainList     widget.List
	searchEd     widget.Editor
	downloadBtns []widget.Clickable

	// State
	ActiveSource  FileSource
	ActiveType    FileTypeFilter
	ActiveChannel string
	ActiveUser    string
	Items         []LibraryItem
	FilteredItems []LibraryItem
	Loading       bool
	TotalSize     int64

	mu sync.Mutex
}

func NewLibraryView(a *App) *LibraryView {
	v := &LibraryView{app: a}
	v.sideList.Axis = layout.Vertical
	v.mainList.Axis = layout.Vertical
	v.searchEd.SingleLine = true
	return v
}

func (v *LibraryView) Open() {
	v.ActiveSource = SourceAll
	v.ActiveType = TypeAll
	v.ActiveChannel = ""
	v.ActiveUser = ""
	v.searchEd.SetText("")
	v.loadFiles()
}

func (v *LibraryView) loadFiles() {
	v.mu.Lock()
	v.Loading = true
	v.mu.Unlock()
	v.app.Window.Invalidate()

	go func() {
		conn := v.app.Conn()
		if conn == nil {
			v.mu.Lock()
			v.Loading = false
			v.Items = nil
			v.FilteredItems = nil
			v.mu.Unlock()
			v.app.Window.Invalidate()
			return
		}

		var items []LibraryItem
		var wg sync.WaitGroup
		var mu sync.Mutex

		source := v.ActiveSource

		// Attachments
		if source == SourceAll || source == SourceAttachments {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mimeType := v.mimeTypeForFilter()
				gallery, err := conn.Client.GetGallery(mimeType, v.ActiveChannel, v.ActiveUser, "", 100)
				if err != nil {
					log.Printf("Library: GetGallery: %v", err)
					return
				}
				mu.Lock()
				for _, g := range gallery {
					items = append(items, LibraryItem{
						FileName:   g.Filename,
						FileSize:   g.Size,
						MimeType:   g.MimeType,
						URL:        g.URL,
						Source:     SourceAttachments,
						SourceName: "#" + g.ChannelName,
						UploadedBy: g.Username,
						CreatedAt:  g.CreatedAt,
					})
				}
				mu.Unlock()
			}()
		}

		// Storage
		if source == SourceAll || source == SourceStorage {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v.loadStorageRecursive(conn, nil, &items, &mu)
			}()
		}

		// Shares
		if source == SourceAll || source == SourceShares {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v.app.mu.RLock()
				allShares := make([]api.SharedDirectory, 0, len(conn.MyShares)+len(conn.SharedWithMe))
				allShares = append(allShares, conn.MyShares...)
				allShares = append(allShares, conn.SharedWithMe...)
				v.app.mu.RUnlock()

				for _, share := range allShares {
					files, err := conn.Client.GetShareFiles(share.ID, "")
					if err != nil {
						log.Printf("Library: GetShareFiles(%s): %v", share.ID, err)
						continue
					}
					mu.Lock()
					for _, f := range files {
						if f.IsDir {
							continue
						}
						items = append(items, LibraryItem{
							FileName:   f.FileName,
							FileSize:   f.FileSize,
							MimeType:   guessMimeFromName(f.FileName),
							Source:     SourceShares,
							SourceName: share.DisplayName,
							UploadedBy: share.OwnerName,
							CreatedAt:  f.CachedAt,
						})
					}
					mu.Unlock()
				}
			}()
		}

		// P2P
		if source == SourceAll || source == SourceP2P {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if conn.P2P == nil {
					return
				}
				p2pFiles := conn.P2P.GetRegisteredFiles()
				mu.Lock()
				for _, f := range p2pFiles {
					items = append(items, LibraryItem{
						FileName:   f.FileName,
						FileSize:   f.FileSize,
						MimeType:   guessMimeFromName(f.FileName),
						Source:     SourceP2P,
						SourceName: "P2P",
						UploadedBy: "You",
					})
				}
				mu.Unlock()
			}()
		}

		wg.Wait()

		// Seřadit od nejnovějšího
		sort.Slice(items, func(i, j int) bool {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})

		v.mu.Lock()
		v.Items = items
		v.Loading = false
		v.mu.Unlock()

		v.applyFilter()
		v.app.Window.Invalidate()
	}()
}

func (v *LibraryView) loadStorageRecursive(conn *ServerConnection, folderID *string, items *[]LibraryItem, mu *sync.Mutex) {
	files, err := conn.Client.ListStorageFiles(folderID)
	if err != nil {
		log.Printf("Library: ListStorageFiles: %v", err)
		return
	}
	mu.Lock()
	for _, f := range files {
		*items = append(*items, LibraryItem{
			FileName:   f.Name,
			FileSize:   f.Size,
			MimeType:   f.MimeType,
			URL:        f.URL,
			Source:     SourceStorage,
			SourceName: "Storage",
			UploadedBy: f.Username,
			CreatedAt:  f.CreatedAt,
		})
	}
	mu.Unlock()

	folders, err := conn.Client.ListStorageFolders(folderID)
	if err != nil {
		log.Printf("Library: ListStorageFolders: %v", err)
		return
	}
	for _, folder := range folders {
		fid := folder.ID
		v.loadStorageRecursive(conn, &fid, items, mu)
	}
}

func (v *LibraryView) mimeTypeForFilter() string {
	switch v.ActiveType {
	case TypeImages:
		return "image"
	case TypeVideo:
		return "video"
	case TypeAudio:
		return "audio"
	}
	return ""
}

func (v *LibraryView) applyFilter() {
	v.mu.Lock()
	defer v.mu.Unlock()

	query := strings.ToLower(v.searchEd.Text())
	var filtered []LibraryItem
	var totalSize int64

	for _, item := range v.Items {
		// Text search
		if query != "" && !strings.Contains(strings.ToLower(item.FileName), query) {
			continue
		}
		// Type filter (lokálně pro non-attachment zdroje)
		if v.ActiveType != TypeAll && !v.matchesType(item) {
			continue
		}
		filtered = append(filtered, item)
		totalSize += item.FileSize
	}

	v.FilteredItems = filtered
	v.TotalSize = totalSize
}

func (v *LibraryView) matchesType(item LibraryItem) bool {
	mime := strings.ToLower(item.MimeType)
	switch v.ActiveType {
	case TypeImages:
		return strings.HasPrefix(mime, "image/")
	case TypeVideo:
		return strings.HasPrefix(mime, "video/")
	case TypeAudio:
		return strings.HasPrefix(mime, "audio/")
	case TypeDocuments:
		return strings.HasPrefix(mime, "application/pdf") ||
			strings.HasPrefix(mime, "application/msword") ||
			strings.HasPrefix(mime, "application/vnd.") ||
			strings.HasPrefix(mime, "text/")
	}
	return true
}

func guessMimeFromName(name string) string {
	name = strings.ToLower(name)
	switch {
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".gif"):
		return "image/gif"
	case strings.HasSuffix(name, ".webp"):
		return "image/webp"
	case strings.HasSuffix(name, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(name, ".webm"):
		return "video/webm"
	case strings.HasSuffix(name, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(name, ".ogg"):
		return "audio/ogg"
	case strings.HasSuffix(name, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(name, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(name, ".zip"):
		return "application/zip"
	case strings.HasSuffix(name, ".txt"):
		return "text/plain"
	}
	return "application/octet-stream"
}

// LayoutSidebar — levý panel (240px, vyplňuje channel sidebar slot)
func (v *LibraryView) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()

	// Back button
	if v.backBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewChannels
		v.app.mu.Unlock()
	}

	// Source clicks
	for i := 0; i < 5; i++ {
		if v.sourceBtns[i].Clicked(gtx) {
			v.ActiveSource = FileSource(i)
			v.ActiveChannel = ""
			v.ActiveUser = ""
			v.loadFiles()
		}
	}

	// Type clicks
	for i := 0; i < 5; i++ {
		if v.typeBtns[i].Clicked(gtx) {
			v.ActiveType = FileTypeFilter(i)
			v.applyFilter()
			v.app.Window.Invalidate()
		}
	}

	// Channel clicks
	var channels []api.Channel
	if conn != nil {
		v.app.mu.RLock()
		channels = conn.Channels
		v.app.mu.RUnlock()
	}
	if len(v.channelBtns) < len(channels)+1 {
		v.channelBtns = make([]widget.Clickable, len(channels)+1)
	}
	// "All channels" click
	if v.channelBtns[0].Clicked(gtx) {
		v.ActiveChannel = ""
		if v.ActiveSource == SourceAttachments {
			v.loadFiles()
		}
	}
	for i, ch := range channels {
		if ch.Type == "voice" {
			continue
		}
		if v.channelBtns[i+1].Clicked(gtx) {
			v.ActiveChannel = ch.ID
			if v.ActiveSource == SourceAll || v.ActiveSource == SourceAttachments {
				v.ActiveSource = SourceAttachments
				v.loadFiles()
			}
		}
	}

	// User clicks
	var users []api.User
	if conn != nil {
		v.app.mu.RLock()
		users = conn.Users
		v.app.mu.RUnlock()
	}
	if len(v.userBtns) < len(users)+1 {
		v.userBtns = make([]widget.Clickable, len(users)+1)
	}
	if v.userBtns[0].Clicked(gtx) {
		v.ActiveUser = ""
		if v.ActiveSource == SourceAttachments || v.ActiveSource == SourceStorage {
			v.loadFiles()
		}
	}
	for i, u := range users {
		if v.userBtns[i+1].Clicked(gtx) {
			v.ActiveUser = u.ID
			if v.ActiveSource == SourceAll || v.ActiveSource == SourceAttachments {
				v.ActiveSource = SourceAttachments
				v.loadFiles()
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconBack, 20, ColorTextDim)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconFolder, 20, ColorAccent)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body1(v.app.Theme.Material, "Files")
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									})
								}),
							)
						})
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

		// Scrollable sidebar content
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(v.app.Theme.Material, &v.sideList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
				var items []layout.FlexChild

				// SOURCE section
				items = append(items, v.sidebarSection("SOURCE")...)
				sourceLabels := []string{"All Files", "Attachments", "Server Storage", "Shared Folders", "P2P Files"}
				for i, label := range sourceLabels {
					i, label := i, label
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSidebarItem(gtx, &v.sourceBtns[i], label, FileSource(i) == v.ActiveSource)
					}))
				}

				// TYPE section
				items = append(items, v.sidebarSection("FILE TYPE")...)
				typeLabels := []string{"All Types", "Images", "Video", "Audio", "Documents"}
				for i, label := range typeLabels {
					i, label := i, label
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSidebarItem(gtx, &v.typeBtns[i], label, FileTypeFilter(i) == v.ActiveType)
					}))
				}

				// CHANNEL section (jen pro Attachments)
				if v.ActiveSource == SourceAll || v.ActiveSource == SourceAttachments {
					items = append(items, v.sidebarSection("CHANNEL")...)
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSidebarItem(gtx, &v.channelBtns[0], "All Channels", v.ActiveChannel == "")
					}))
					for i, ch := range channels {
						if ch.Type == "voice" {
							continue
						}
						i, ch := i, ch
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSidebarItem(gtx, &v.channelBtns[i+1], "# "+ch.Name, v.ActiveChannel == ch.ID)
						}))
					}
				}

				// UPLOADED BY section (pro Attachments/Storage)
				if v.ActiveSource == SourceAll || v.ActiveSource == SourceAttachments || v.ActiveSource == SourceStorage {
					items = append(items, v.sidebarSection("UPLOADED BY")...)
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSidebarItem(gtx, &v.userBtns[0], "All Users", v.ActiveUser == "")
					}))
					for i, u := range users {
						i, u := i, u
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSidebarItem(gtx, &v.userBtns[i+1], v.app.ResolveUserName(&u), v.ActiveUser == u.ID)
						}))
					}
				}

				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			})
		}),
	)
}

func (v *LibraryView) sidebarSection(title string) []layout.FlexChild {
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, title)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
	}
}

func (v *LibraryView) layoutSidebarItem(gtx layout.Context, btn *widget.Clickable, label string, active bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if active {
			bg = ColorSelected
		} else if btn.Hovered() {
			bg = ColorHover
		}

		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				if bg.A > 0 {
					paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(16), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					textColor := ColorTextDim
					if active {
						textColor = ColorText
					}
					lbl := material.Body2(v.app.Theme.Material, label)
					lbl.Color = textColor
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

// LayoutMain — hlavní oblast (zprávy/soubory)
func (v *LibraryView) LayoutMain(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Live filter
	if _, ok := v.searchEd.Update(gtx); ok {
		v.applyFilter()
	}

	v.mu.Lock()
	loading := v.Loading
	filtered := v.FilteredItems
	totalSize := v.TotalSize
	v.mu.Unlock()

	// Zajistit dostatek tlačítek
	if len(v.downloadBtns) < len(filtered) {
		v.downloadBtns = make([]widget.Clickable, len(filtered)+20)
	}

	// Download clicks
	for i := range filtered {
		if i < len(v.downloadBtns) && v.downloadBtns[i].Clicked(gtx) {
			item := filtered[i]
			if item.URL != "" {
				conn := v.app.Conn()
				if conn != nil {
					fileURL := conn.URL + item.URL
					fname := item.FileName
					tok := conn.Client.GetAccessToken()
					v.app.SaveDlg.Show(fname, func(savePath string) {
						go v.downloadFile(fileURL, savePath, tok)
					})
				}
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Search bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(6)
						paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
							Rect: bounds,
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconSearch, 16, ColorTextDim)
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										ed := material.Editor(v.app.Theme.Material, &v.searchEd, "Filter files...")
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										return ed.Layout(gtx)
									})
								}),
							)
						})
					},
				)
			})
		}),

		// Stats bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var text string
				if loading {
					text = "Loading..."
				} else {
					text = fmt.Sprintf("%d files", len(filtered))
					if totalSize > 0 {
						text += " \u00b7 " + formatFileSize(totalSize)
					}
				}
				lbl := material.Caption(v.app.Theme.Material, text)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),

		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		// File list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if loading {
				return layoutCentered(gtx, v.app.Theme, "Loading files...", ColorTextDim)
			}
			if len(filtered) == 0 {
				return layoutCentered(gtx, v.app.Theme, "No files found", ColorTextDim)
			}
			return material.List(v.app.Theme.Material, &v.mainList).Layout(gtx, len(filtered), func(gtx layout.Context, idx int) layout.Dimensions {
				return v.layoutFileItem(gtx, idx, filtered[idx])
			})
		}),
	)
}

func (v *LibraryView) layoutFileItem(gtx layout.Context, idx int, item LibraryItem) layout.Dimensions {
	hovered := idx < len(v.downloadBtns) && v.downloadBtns[idx].Hovered()

	bg := ColorBg
	if hovered {
		bg = ColorHover
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					// Řádek 1: filename, source, size
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							// File icon
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								icon := iconForMime(item.MimeType)
								return layoutIcon(gtx, icon, 18, ColorAccent)
							}),
							// Filename
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, item.FileName)
									lbl.Color = ColorText
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								})
							}),
							// Source tag
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, item.SourceName)
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								})
							}),
							// Size
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, formatFileSize(item.FileSize))
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								})
							}),
						)
					}),
					// Řádek 2: uploader, date, download
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(26)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								// Uploader
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									parts := []string{}
									if item.UploadedBy != "" {
										parts = append(parts, item.UploadedBy)
									}
									if !item.CreatedAt.IsZero() {
										parts = append(parts, item.CreatedAt.Format("2006-01-02 15:04"))
									}
									lbl := material.Caption(v.app.Theme.Material, strings.Join(parts, " \u00b7 "))
									lbl.Color = withAlpha(ColorTextDim, 160)
									return lbl.Layout(gtx)
								}),
								// Download button
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if item.URL == "" {
										return layout.Dimensions{}
									}
									if idx >= len(v.downloadBtns) {
										return layout.Dimensions{}
									}
									return v.downloadBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										clr := ColorTextDim
										if v.downloadBtns[idx].Hovered() {
											clr = ColorAccent
										}
										return layoutIcon(gtx, IconDownload, 16, clr)
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

func iconForMime(mime string) *NIcon {
	mime = strings.ToLower(mime)
	if strings.HasPrefix(mime, "image/") {
		return IconImage
	}
	if strings.HasPrefix(mime, "video/") {
		return IconVideocam
	}
	if strings.HasPrefix(mime, "audio/") {
		return IconVolumeUp
	}
	return IconFile
}

func (v *LibraryView) downloadFile(fileURL, savePath, token string) {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		log.Printf("library download: %v", err)
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("library download: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("library download: HTTP %d", resp.StatusCode)
		return
	}

	f, err := os.Create(savePath)
	if err != nil {
		log.Printf("library download: %v", err)
		return
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		log.Printf("library download: %v", err)
	}
}

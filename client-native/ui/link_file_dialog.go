package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type LinkFileDialog struct {
	app     *App
	Visible bool

	onSelect func(results []api.UploadResult) // callback after selection

	folders     []api.StorageFolder
	files       []api.StorageFile
	parentStack []*string // navigation history (nil = root)
	selected    map[string]bool

	list       widget.List
	folderBtns []widget.Clickable
	fileBtns   []widget.Clickable
	backBtn    widget.Clickable
	attachBtn  widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable

	loading bool
	err     string
}

func NewLinkFileDialog(a *App) *LinkFileDialog {
	d := &LinkFileDialog{
		app:      a,
		selected: make(map[string]bool),
	}
	d.list.Axis = layout.Vertical
	return d
}

func (d *LinkFileDialog) Show(callback func(results []api.UploadResult)) {
	d.Visible = true
	d.onSelect = callback
	d.parentStack = nil
	d.selected = make(map[string]bool)
	d.err = ""
	d.loadFolder(nil)
}

func (d *LinkFileDialog) Hide() {
	d.Visible = false
	d.onSelect = nil
}

func (d *LinkFileDialog) loadFolder(parentID *string) {
	d.loading = true
	d.err = ""
	conn := d.app.Conn()
	if conn == nil {
		d.loading = false
		d.err = "No server connection"
		return
	}
	go func() {
		folders, ferr := conn.Client.ListStorageFolders(parentID)
		files, fierr := conn.Client.ListStorageFiles(parentID)
		d.loading = false
		if ferr != nil {
			d.err = ferr.Error()
		} else if fierr != nil {
			d.err = fierr.Error()
		} else {
			d.folders = folders
			d.files = files
			d.folderBtns = make([]widget.Clickable, len(folders))
			d.fileBtns = make([]widget.Clickable, len(files))
		}
		d.app.Window.Invalidate()
	}()
}

func (d *LinkFileDialog) currentParentID() *string {
	if len(d.parentStack) == 0 {
		return nil
	}
	return d.parentStack[len(d.parentStack)-1]
}

func (d *LinkFileDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	// Click on overlay -> close
	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
	}
	// Click on card -> no action (capture to prevent closing)
	d.cardBtn.Clicked(gtx)

	// Back
	if d.backBtn.Clicked(gtx) && len(d.parentStack) > 0 {
		d.parentStack = d.parentStack[:len(d.parentStack)-1]
		d.loadFolder(d.currentParentID())
	}

	// Folder clicks
	for i := range d.folderBtns {
		if d.folderBtns[i].Clicked(gtx) && i < len(d.folders) {
			fid := d.folders[i].ID
			d.parentStack = append(d.parentStack, &fid)
			d.loadFolder(&fid)
		}
	}

	// File clicks (toggle select)
	for i := range d.fileBtns {
		if d.fileBtns[i].Clicked(gtx) && i < len(d.files) {
			fid := d.files[i].ID
			if d.selected[fid] {
				delete(d.selected, fid)
			} else {
				d.selected[fid] = true
			}
		}
	}

	// Cancel
	if d.cancelBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{}
	}

	// Attach
	if d.attachBtn.Clicked(gtx) {
		var results []api.UploadResult
		for _, f := range d.files {
			if d.selected[f.ID] {
				results = append(results, api.UploadResult{
					Filename: f.Name,
					Original: f.Name,
					URL:      f.URL,
					MimeType: f.MimeType,
					Size:     f.Size,
				})
			}
		}
		if len(results) > 0 && d.onSelect != nil {
			d.onSelect(results)
		}
		d.Hide()
		return layout.Dimensions{}
	}

	// Overlay dark background
	return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			maxW := gtx.Dp(500)
			maxH := gtx.Dp(500)
			if gtx.Constraints.Max.X < maxW {
				maxW = gtx.Constraints.Max.X - gtx.Dp(40)
			}
			if gtx.Constraints.Max.Y < maxH {
				maxH = gtx.Constraints.Max.Y - gtx.Dp(40)
			}
			gtx.Constraints.Min = image.Pt(maxW, 0)
			gtx.Constraints.Max = image.Pt(maxW, maxH)

			return d.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				rr := gtx.Dp(8)
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
				paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))

				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					// Header
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if len(d.parentStack) > 0 {
										return d.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconBack, 18, ColorTextDim)
											})
										})
									}
									return layout.Dimensions{}
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, "Link File from Storage")
									lbl.Color = ColorText
									return lbl.Layout(gtx)
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
					// Content
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if d.loading {
							return layoutCentered(gtx, d.app.Theme, "Loading...", ColorTextDim)
						}
						if d.err != "" {
							return layoutCentered(gtx, d.app.Theme, d.err, ColorDanger)
						}
						if len(d.folders) == 0 && len(d.files) == 0 {
							return layoutCentered(gtx, d.app.Theme, "Empty folder", ColorTextDim)
						}

						totalItems := len(d.folders) + len(d.files)
						return material.List(d.app.Theme.Material, &d.list).Layout(gtx, totalItems, func(gtx layout.Context, idx int) layout.Dimensions {
							if idx < len(d.folders) {
								return d.layoutFolderItem(gtx, idx)
							}
							return d.layoutFileItem(gtx, idx-len(d.folders))
						})
					}),
					// Divider
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
						paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
						return layout.Dimensions{Size: size}
					}),
					// Footer
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							selCount := len(d.selected)
							return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									if selCount == 0 {
										lbl := material.Body2(d.app.Theme.Material, "Select files to attach")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}
									lbl := material.Body2(d.app.Theme.Material, pluralize(selCount, "file", "files")+" selected")
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(d.app.Theme.Material, "Cancel")
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if selCount == 0 {
										return layout.Dimensions{}
									}
									return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.attachBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutButton(gtx, d.app.Theme, "Attach", ColorAccent, d.attachBtn.Hovered())
										})
									})
								}),
							)
						})
					}),
				)
			})
		})
	})
}

func (d *LinkFileDialog) layoutFolderItem(gtx layout.Context, idx int) layout.Dimensions {
	if idx >= len(d.folders) || idx >= len(d.folderBtns) {
		return layout.Dimensions{}
	}
	f := d.folders[idx]
	bg := color.NRGBA{}
	if d.folderBtns[idx].Hovered() {
		bg = ColorHover
	}
	return d.folderBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconFolder, 18, ColorAccent)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(d.app.Theme.Material, f.Name)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	})
}

func (d *LinkFileDialog) layoutFileItem(gtx layout.Context, idx int) layout.Dimensions {
	if idx >= len(d.files) || idx >= len(d.fileBtns) {
		return layout.Dimensions{}
	}
	f := d.files[idx]
	isSelected := d.selected[f.ID]
	bg := color.NRGBA{}
	if isSelected {
		bg = color.NRGBA{R: 88, G: 101, B: 242, A: 40}
	} else if d.fileBtns[idx].Hovered() {
		bg = ColorHover
	}
	return d.fileBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if isSelected {
								return layoutIcon(gtx, IconCheck, 18, ColorAccent)
							}
							return layoutIcon(gtx, IconFile, 18, ColorTextDim)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(d.app.Theme.Material, f.Name)
								lbl.Color = ColorText
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(d.app.Theme.Material, FormatBytes(f.Size))
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	})
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// logLinkFileError logs an error in LinkFileDialog
func logLinkFileError(msg string, err error) {
	if err != nil {
		log.Printf("LinkFileDialog: %s: %v", msg, err)
	}
}

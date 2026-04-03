package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const maxRecentDirs = 6
const maxPathHints = 8

type dirEntry struct {
	name  string
	isDir bool
	size  int64
}

// SaveDialog is a custom Gio save-file dialog with file browsing.
type SaveDialog struct {
	app     *App
	Visible bool

	currentDir string
	entries    []dirEntry
	entryBtns  []widget.Clickable

	// Recent directories
	recentDirs []string
	recentBtns [maxRecentDirs]widget.Clickable

	// Editable path + autocomplete
	pathEditor   widget.Editor
	pathHints    []string // full paths
	pathHintBtns [maxPathHints]widget.Clickable
	selectedHint int  // -1 = none
	showHints    bool // show dropdown
	lastPathText string

	filenameEditor widget.Editor
	fileList       widget.List

	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
	saveBtn    widget.Clickable
	cancelBtn  widget.Clickable
	upBtn      widget.Clickable
	closeBtn   widget.Clickable

	onSave     func(string)
	needsFocus bool
	folderMode bool // when true, pick a directory (no filename field)
}

func NewSaveDialog(a *App) *SaveDialog {
	d := &SaveDialog{app: a}
	d.fileList.Axis = layout.Vertical
	d.filenameEditor.Submit = true
	d.filenameEditor.SingleLine = true
	d.pathEditor.Submit = true
	d.pathEditor.SingleLine = true
	d.selectedHint = -1
	return d
}

func (d *SaveDialog) Show(defaultName string, onSave func(string)) {
	d.folderMode = false
	d.showCommon(onSave)
	d.filenameEditor.SetText(defaultName)
	d.filenameEditor.SetCaret(len(defaultName), 0)
}

// ShowFolderPick opens the dialog in folder-pick mode (no filename field).
// The callback receives the selected directory path.
func (d *SaveDialog) ShowFolderPick(onPick func(string)) {
	d.folderMode = true
	d.showCommon(onPick)
}

func (d *SaveDialog) showCommon(onSave func(string)) {
	d.Visible = true
	d.onSave = onSave
	d.needsFocus = true

	// Load directory history
	d.recentDirs = loadRecentDirs()

	// Default directory: last used, or ~/Downloads, or ~
	home, _ := os.UserHomeDir()
	if len(d.recentDirs) > 0 {
		if info, err := os.Stat(d.recentDirs[0]); err == nil && info.IsDir() {
			d.currentDir = d.recentDirs[0]
		} else {
			d.currentDir = defaultDownloadDir(home)
		}
	} else {
		d.currentDir = defaultDownloadDir(home)
	}

	d.pathEditor.SetText(d.currentDir)
	d.lastPathText = d.currentDir
	d.showHints = false
	d.selectedHint = -1
	d.pathHints = nil
	d.readDir()
}

func defaultDownloadDir(home string) string {
	dl := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dl); err == nil && info.IsDir() {
		return dl
	}
	return home
}

func (d *SaveDialog) Hide() {
	d.Visible = false
	d.onSave = nil
	d.entries = nil
	d.showHints = false
	d.pathHints = nil
}

func (d *SaveDialog) readDir() {
	dirEntries, err := os.ReadDir(d.currentDir)
	if err != nil {
		d.entries = nil
		return
	}

	var dirs, files []dirEntry
	for _, e := range dirEntries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		de := dirEntry{name: e.Name(), isDir: e.IsDir(), size: size}
		if e.IsDir() {
			dirs = append(dirs, de)
		} else {
			files = append(files, de)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].name) < strings.ToLower(files[j].name) })

	d.entries = append(dirs, files...)

	d.fileList.Position.First = 0
	d.fileList.Position.Offset = 0
}

func (d *SaveDialog) navigateTo(dir string) {
	d.currentDir = dir
	d.pathEditor.SetText(dir)
	d.lastPathText = dir
	d.showHints = false
	d.selectedHint = -1
	d.pathHints = nil
	d.readDir()
}

func (d *SaveDialog) doSave() {
	if d.folderMode {
		if d.onSave == nil {
			return
		}
		addRecentDir(d.currentDir)
		fn := d.onSave
		dir := d.currentDir
		d.Hide()
		fn(dir)
		return
	}

	name := strings.TrimSpace(d.filenameEditor.Text())
	if name == "" || d.onSave == nil {
		return
	}
	fullPath := filepath.Join(d.currentDir, name)

	// File already exists — confirm overwrite
	if _, err := os.Stat(fullPath); err == nil {
		saveFn := d.onSave
		dir := d.currentDir
		d.app.ConfirmDlg.ShowWithCancel(
			"File exists",
			fmt.Sprintf("\"%s\" already exists. Overwrite?", name),
			"Overwrite",
			func() {
				addRecentDir(dir)
				d.Hide()
				saveFn(fullPath)
				d.app.Window.Invalidate()
			},
			nil,
		)
		return
	}

	addRecentDir(d.currentDir)

	fn := d.onSave
	d.Hide()
	fn(fullPath)
}

// expandPath replaces ~ with the home directory.
func expandPath(text string) string {
	if strings.HasPrefix(text, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, text[2:])
	}
	if text == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	return text
}

// computePathHints computes autocomplete suggestions for the current text in the path editor.
func (d *SaveDialog) computePathHints() {
	text := strings.TrimSpace(d.pathEditor.Text())
	text = expandPath(text)

	d.pathHints = nil
	d.selectedHint = -1

	// If text ends with "/" and it's an existing directory — show subdirectories
	if strings.HasSuffix(text, "/") || strings.HasSuffix(text, string(os.PathSeparator)) {
		text = strings.TrimRight(text, "/"+string(os.PathSeparator))
		if info, err := os.Stat(text); err == nil && info.IsDir() {
			d.listSubdirs(text, "")
		}
		d.showHints = len(d.pathHints) > 0
		return
	}

	// Otherwise: parent + partial match
	parent := filepath.Dir(text)
	partial := filepath.Base(text)
	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		d.listSubdirs(parent, partial)
	}
	d.showHints = len(d.pathHints) > 0
}

// listSubdirs populates pathHints with subdirectories of parent matching the prefix.
func (d *SaveDialog) listSubdirs(parent, prefix string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	lowerPrefix := strings.ToLower(prefix)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if prefix == "" || strings.HasPrefix(strings.ToLower(e.Name()), lowerPrefix) {
			d.pathHints = append(d.pathHints, filepath.Join(parent, e.Name()))
			if len(d.pathHints) >= maxPathHints {
				break
			}
		}
	}
}

// applyHint navigates to the selected hint and updates the editor.
func (d *SaveDialog) applyHint(path string) {
	d.navigateTo(path)
	// Append "/" for further completion
	d.pathEditor.SetText(path + "/")
	d.lastPathText = path + "/"
	text := d.pathEditor.Text()
	d.pathEditor.SetCaret(len(text), len(text))
	d.computePathHints()
}

func (d *SaveDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	// Focus the appropriate editor on open
	if d.needsFocus {
		d.needsFocus = false
		if d.folderMode {
			gtx.Execute(key.FocusCmd{Tag: &d.pathEditor})
		} else {
			gtx.Execute(key.FocusCmd{Tag: &d.filenameEditor})
		}
	}

	// Detect text change in path editor — recompute hints
	currentPathText := d.pathEditor.Text()
	if currentPathText != d.lastPathText {
		d.lastPathText = currentPathText
		d.computePathHints()
	}

	// Handle path editor submit (Enter)
	for {
		ev, ok := d.pathEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			if d.showHints && d.selectedHint >= 0 && d.selectedHint < len(d.pathHints) {
				d.applyHint(d.pathHints[d.selectedHint])
			} else {
				text := expandPath(strings.TrimSpace(d.pathEditor.Text()))
				text = strings.TrimRight(text, "/"+string(os.PathSeparator))
				if info, err := os.Stat(text); err == nil && info.IsDir() {
					d.navigateTo(text)
				}
			}
		}
	}

	// Handle filename editor submit (Enter → save)
	for {
		ev, ok := d.filenameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			d.doSave()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}

	// Arrow keys + Tab + Escape for path editor hints
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &d.pathEditor, Name: key.NameDownArrow},
			key.Filter{Focus: &d.pathEditor, Name: key.NameUpArrow},
			key.Filter{Focus: &d.pathEditor, Name: key.NameTab},
			key.Filter{Focus: &d.pathEditor, Name: key.NameEscape},
		)
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			switch ke.Name {
			case key.NameDownArrow:
				if d.showHints && d.selectedHint < len(d.pathHints)-1 {
					d.selectedHint++
				}
			case key.NameUpArrow:
				if d.selectedHint > 0 {
					d.selectedHint--
				} else if d.selectedHint == 0 {
					d.selectedHint = -1
				}
			case key.NameTab:
				if d.showHints && len(d.pathHints) > 0 {
					idx := d.selectedHint
					if idx < 0 {
						idx = 0
					}
					d.applyHint(d.pathHints[idx])
				}
			case key.NameEscape:
				if d.showHints {
					d.showHints = false
					d.pathEditor.SetText(d.currentDir)
					d.lastPathText = d.currentDir
				}
			}
		}
	}

	// Handle button clicks
	if d.closeBtn.Clicked(gtx) || d.cancelBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.saveBtn.Clicked(gtx) {
		d.doSave()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.upBtn.Clicked(gtx) {
		parent := filepath.Dir(d.currentDir)
		if parent != d.currentDir {
			d.navigateTo(parent)
		}
	}

	// Handle recent directory clicks
	for i := range d.recentDirs {
		if i >= maxRecentDirs {
			break
		}
		if d.recentBtns[i].Clicked(gtx) {
			d.navigateTo(d.recentDirs[i])
		}
	}

	// Handle hint clicks
	for i := range d.pathHints {
		if i >= maxPathHints {
			break
		}
		if d.pathHintBtns[i].Clicked(gtx) {
			d.applyHint(d.pathHints[i])
		}
	}

	// Handle entry clicks
	if len(d.entryBtns) < len(d.entries) {
		d.entryBtns = make([]widget.Clickable, len(d.entries)+10)
	}
	for i, e := range d.entries {
		if d.entryBtns[i].Clicked(gtx) {
			if e.isDir {
				d.navigateTo(filepath.Join(d.currentDir, e.name))
			} else {
				d.filenameEditor.SetText(e.name)
			}
		}
	}

	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		// Backdrop
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		// Card
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			cardW := gtx.Dp(500)
			if cardW > gtx.Constraints.Max.X-gtx.Dp(40) {
				cardW = gtx.Constraints.Max.X - gtx.Dp(40)
			}
			maxH := gtx.Constraints.Max.Y * 70 / 100
			gtx.Constraints.Max.X = cardW
			gtx.Constraints.Min.X = cardW
			gtx.Constraints.Max.Y = maxH

			return d.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(12)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
							Rect: bounds,
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Header
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutHeader(gtx)
								}),
								// Recent directories
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutRecentDirs(gtx)
								}),
								// Path editor + Up button
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutPathBar(gtx)
								}),
								// Path hints dropdown
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutPathHints(gtx)
								}),
								// File list
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.layoutFileList(gtx)
									})
								}),
								// Filename editor (hidden in folder mode)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if d.folderMode {
										return layout.Dimensions{}
									}
									return d.layoutFilenameEditor(gtx)
								}),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutButtons(gtx)
								}),
							)
						})
					},
				)
			})
		}),
	)
}

func (d *SaveDialog) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(d.app.Theme.Material, "Save file")
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				clr := ColorTextDim
				if d.closeBtn.Hovered() {
					clr = ColorText
				}
				lbl := material.Body1(d.app.Theme.Material, "X")
				lbl.Color = clr
				return lbl.Layout(gtx)
			})
		}),
	)
}

func (d *SaveDialog) layoutRecentDirs(gtx layout.Context) layout.Dimensions {
	if len(d.recentDirs) == 0 {
		return layout.Dimensions{}
	}

	home, _ := os.UserHomeDir()

	return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(d.app.Theme.Material, "Recent")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var children []layout.FlexChild
					for i, dir := range d.recentDirs {
						if i >= maxRecentDirs {
							break
						}
						idx := i
						dirPath := dir

						displayPath := dirPath
						if strings.HasPrefix(displayPath, home) {
							displayPath = "~" + displayPath[len(home):]
						}
						parts := strings.Split(displayPath, string(os.PathSeparator))
						if len(parts) > 2 {
							displayPath = ".../" + strings.Join(parts[len(parts)-2:], "/")
						}

						isCurrent := dirPath == d.currentDir

						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.recentBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									bg := ColorInput
									fg := ColorTextDim
									if isCurrent {
										bg = ColorAccentDim
										fg = ColorText
									} else if d.recentBtns[idx].Hovered() {
										bg = ColorHover
										fg = ColorText
									}
									return layout.Background{}.Layout(gtx,
										func(gtx layout.Context) layout.Dimensions {
											bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
											rr := gtx.Dp(4)
											paint.FillShape(gtx.Ops, bg, clip.RRect{
												Rect: bounds,
												NE:   rr, NW: rr, SE: rr, SW: rr,
											}.Op(gtx.Ops))
											return layout.Dimensions{Size: bounds.Max}
										},
										func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(d.app.Theme.Material, displayPath)
												lbl.Color = fg
												return lbl.Layout(gtx)
											})
										},
									)
								})
							})
						}))
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
				})
			}),
		)
	})
}

func (d *SaveDialog) layoutPathBar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			// Up button
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return d.upBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Dp(28)
					bg := color.NRGBA{A: 0}
					if d.upBtn.Hovered() {
						bg = ColorHover
					}
					rr := size / 4
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: image.Rect(0, 0, size, size),
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = image.Pt(size, size)
						gtx.Constraints.Max = gtx.Constraints.Min
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body1(d.app.Theme.Material, "..")
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					})
				})
			}),
			// Editable path
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								ed := material.Editor(d.app.Theme.Material, &d.pathEditor, "/path/to/directory")
								ed.Color = ColorText
								ed.HintColor = ColorTextDim
								return ed.Layout(gtx)
							})
						},
					)
				})
			}),
		)
	})
}

func (d *SaveDialog) layoutPathHints(gtx layout.Context) layout.Dimensions {
	if !d.showHints || len(d.pathHints) == 0 {
		return layout.Dimensions{}
	}

	home, _ := os.UserHomeDir()

	return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				var items []layout.FlexChild
				for i, hint := range d.pathHints {
					if i >= maxPathHints {
						break
					}
					idx := i
					hintPath := hint

					// Display shortened
					display := hintPath
					if strings.HasPrefix(display, home) {
						display = "~" + display[len(home):]
					}

					isSelected := idx == d.selectedHint

					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return d.pathHintBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := color.NRGBA{A: 0}
							fg := ColorText
							if isSelected {
								bg = ColorAccentDim
								fg = ColorText
							} else if d.pathHintBtns[idx].Hovered() {
								bg = ColorHover
							}
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
									paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
									return layout.Dimensions{Size: bounds.Max}
								},
								func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(d.app.Theme.Material, display+"/")
										lbl.Color = fg
										lbl.MaxLines = 1
										return lbl.Layout(gtx)
									})
								},
							)
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			},
		)
	})
}

func (d *SaveDialog) layoutFileList(gtx layout.Context) layout.Dimensions {
	if len(d.entries) == 0 {
		lbl := material.Body2(d.app.Theme.Material, "Empty directory")
		lbl.Color = ColorTextDim
		return lbl.Layout(gtx)
	}

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
			return material.List(d.app.Theme.Material, &d.fileList).Layout(gtx, len(d.entries), func(gtx layout.Context, idx int) layout.Dimensions {
				e := d.entries[idx]
				return d.entryBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := color.NRGBA{A: 0}
					if d.entryBtns[idx].Hovered() {
						bg = ColorHover
					}
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
							paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								if e.isDir {
									lbl := material.Body2(d.app.Theme.Material, e.name+"/")
									lbl.Color = ColorAccent
									lbl.Font.Weight = 600
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								}
								return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(d.app.Theme.Material, e.name)
										lbl.Color = ColorText
										lbl.MaxLines = 1
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(d.app.Theme.Material, FormatBytes(e.size))
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						},
					)
				})
			})
		},
	)
}

func (d *SaveDialog) layoutFilenameEditor(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(d.app.Theme.Material, "File name")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								ed := material.Editor(d.app.Theme.Material, &d.filenameEditor, "filename")
								ed.Color = ColorText
								ed.HintColor = ColorTextDim
								return ed.Layout(gtx)
							})
						},
					)
				})
			}),
		)
	})
}

func (d *SaveDialog) layoutButtons(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Spacing: layout.SpaceStart, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutDialogBtn(gtx, d.app.Theme, &d.cancelBtn, "Cancel", ColorInput, ColorText)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				label := "Save"
				if d.folderMode {
					label = "Select"
				}
				return layoutDialogBtn(gtx, d.app.Theme, &d.saveBtn, label, ColorAccent, ColorWhite)
			})
		}),
	)
}


// --- Persistence: ~/.nora/save_dirs.json ---

func recentDirsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nora", "save_dirs.json")
}

func loadRecentDirs() []string {
	data, err := os.ReadFile(recentDirsPath())
	if err != nil {
		return nil
	}
	var dirs []string
	if err := json.Unmarshal(data, &dirs); err != nil {
		return nil
	}
	var valid []string
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			valid = append(valid, dir)
		}
	}
	return valid
}

func saveRecentDirs(dirs []string) {
	data, err := json.Marshal(dirs)
	if err != nil {
		return
	}
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".nora"), 0700)
	os.WriteFile(recentDirsPath(), data, 0644)
}

func addRecentDir(dir string) {
	dirs := loadRecentDirs()

	var filtered []string
	for _, d := range dirs {
		if d != dir {
			filtered = append(filtered, d)
		}
	}

	result := append([]string{dir}, filtered...)
	if len(result) > maxRecentDirs {
		result = result[:maxRecentDirs]
	}

	saveRecentDirs(result)
}

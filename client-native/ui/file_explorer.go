package ui

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// FileExplorerEntry is a unified entry type for both game server files and shared files.
type FileExplorerEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	ID      string // for shared files
}

// SortColumn identifies which column is used for sorting.
type SortColumn int

const (
	SortByName SortColumn = iota
	SortBySize
	SortByDate
)

// FileExplorerWidget provides shared sort, multi-select, inline rename, and column
// header functionality for file explorer views.
type FileExplorerWidget struct {
	app *App

	// Sort
	SortCol SortColumn
	SortAsc bool

	// Column header clickables
	nameColBtn widget.Clickable
	sizeColBtn widget.Clickable
	dateColBtn widget.Clickable

	// Multi-select
	Selected     map[int]bool
	LastClickIdx int

	// Inline rename
	RenameIdx      int // -1 = not renaming
	RenameOrigName string // original name captured at rename start
	renameEditor   widget.Editor
	RenameOK       bool // set when rename is confirmed
	RenameName     string

	// Right-click detection tags per row
	rightClickTags []bool
}

// NewFileExplorerWidget creates a new FileExplorerWidget.
func NewFileExplorerWidget(app *App) *FileExplorerWidget {
	w := &FileExplorerWidget{
		app:          app,
		SortCol:      SortByName,
		SortAsc:      true,
		Selected:     make(map[int]bool),
		LastClickIdx: -1,
		RenameIdx:    -1,
	}
	w.renameEditor.SingleLine = true
	w.renameEditor.Submit = true
	return w
}

// SortEntries sorts the entries in place: directories first, then by the selected column.
func (w *FileExplorerWidget) SortEntries(entries []FileExplorerEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		// Directories always first
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		var less bool
		switch w.SortCol {
		case SortBySize:
			less = a.Size < b.Size
		case SortByDate:
			less = a.ModTime.Before(b.ModTime)
		default: // SortByName
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if !w.SortAsc {
			less = !less
		}
		return less
	})
}

// HandleColumnClick processes column header clicks and toggles sort direction.
func (w *FileExplorerWidget) HandleColumnClick(gtx layout.Context) {
	if w.nameColBtn.Clicked(gtx) {
		if w.SortCol == SortByName {
			w.SortAsc = !w.SortAsc
		} else {
			w.SortCol = SortByName
			w.SortAsc = true
		}
	}
	if w.sizeColBtn.Clicked(gtx) {
		if w.SortCol == SortBySize {
			w.SortAsc = !w.SortAsc
		} else {
			w.SortCol = SortBySize
			w.SortAsc = true
		}
	}
	if w.dateColBtn.Clicked(gtx) {
		if w.SortCol == SortByDate {
			w.SortAsc = !w.SortAsc
		} else {
			w.SortCol = SortByDate
			w.SortAsc = false // newest first by default
		}
	}
}

// HandleRenameEvents processes rename editor submit/cancel events.
func (w *FileExplorerWidget) HandleRenameEvents(gtx layout.Context) {
	if w.RenameIdx < 0 {
		return
	}
	for {
		ev, ok := w.renameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			name := strings.TrimSpace(w.renameEditor.Text())
			if name != "" {
				w.RenameOK = true
				w.RenameName = name
			}
			w.RenameIdx = -1
		}
	}
	// Escape cancels rename
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if _, isPress := ev.(key.Event); isPress {
			w.RenameIdx = -1
		}
	}
}

// StartRename begins inline rename for the given entry.
func (w *FileExplorerWidget) StartRename(idx int, currentName string) {
	w.RenameIdx = idx
	w.RenameOrigName = currentName
	w.RenameOK = false
	w.RenameName = ""
	w.renameEditor.SetText(currentName)
	// Select the name part (before extension)
	dot := strings.LastIndex(currentName, ".")
	if dot > 0 {
		w.renameEditor.SetCaret(dot, 0)
	} else {
		w.renameEditor.SetCaret(len(currentName), 0)
	}
}

// CancelRename cancels any in-progress rename.
func (w *FileExplorerWidget) CancelRename() {
	w.RenameIdx = -1
	w.RenameOK = false
}

// HandleClick processes a click on a row with modifier keys for multi-select.
// Returns true if this was a multi-select action (caller should not navigate).
func (w *FileExplorerWidget) HandleClick(idx int, ctrl, shift bool) bool {
	if shift && w.LastClickIdx >= 0 {
		// Range select
		lo, hi := w.LastClickIdx, idx
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			w.Selected[i] = true
		}
		return true
	}
	if ctrl {
		// Toggle single entry
		if w.Selected[idx] {
			delete(w.Selected, idx)
		} else {
			w.Selected[idx] = true
		}
		w.LastClickIdx = idx
		return true
	}
	// Plain click — clear selection
	w.ClearSelection()
	w.LastClickIdx = idx
	return false
}

// ClearSelection removes all selections.
func (w *FileExplorerWidget) ClearSelection() {
	w.Selected = make(map[int]bool)
}

// SelectedCount returns the number of selected entries.
func (w *FileExplorerWidget) SelectedCount() int {
	return len(w.Selected)
}

// SelectedIndices returns sorted list of selected indices.
func (w *FileExplorerWidget) SelectedIndices() []int {
	indices := make([]int, 0, len(w.Selected))
	for idx := range w.Selected {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	return indices
}

// LayoutColumnHeaders renders the sortable column headers.
func (w *FileExplorerWidget) LayoutColumnHeaders(gtx layout.Context) layout.Dimensions {
	th := w.app.Theme

	sortArrow := func(col SortColumn) string {
		if w.SortCol != col {
			return ""
		}
		if w.SortAsc {
			return " \u25B2" // ▲
		}
		return " \u25BC" // ▼
	}

	headerBg := color.NRGBA{R: 30, G: 30, B: 40, A: 255}

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			// Checkbox column spacer (when multi-select is active)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(gtx.Dp(28), 0)}
			}),
			// Name column (flex)
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return w.nameColBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(th.Material, "Name"+sortArrow(SortByName))
						lbl.Color = ColorTextDim
						if w.SortCol == SortByName {
							lbl.Color = ColorText
						}
						return lbl.Layout(gtx)
					})
				})
			}),
			// Size column (80dp)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(80)
				return w.sizeColBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(th.Material, "Size"+sortArrow(SortBySize))
						lbl.Color = ColorTextDim
						if w.SortCol == SortBySize {
							lbl.Color = ColorText
						}
						return lbl.Layout(gtx)
					})
				})
			}),
			// Modified column (130dp)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(130)
				return w.dateColBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(th.Material, "Modified"+sortArrow(SortByDate))
						lbl.Color = ColorTextDim
						if w.SortCol == SortByDate {
							lbl.Color = ColorText
						}
						return lbl.Layout(gtx)
					})
				})
			}),
			// Action column spacer
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(gtx.Dp(40), 0)}
			}),
		)
	})
	call := macro.Stop()

	// Background
	paint.FillShape(gtx.Ops, headerBg, clip.Rect{Max: image.Pt(dims.Size.X, dims.Size.Y)}.Op())
	call.Add(gtx.Ops)
	return dims
}

// LayoutEntryRow renders a single file entry row.
// extraWidgets are laid out after the date column (e.g., delete/download buttons).
func (w *FileExplorerWidget) LayoutEntryRow(gtx layout.Context, entry FileExplorerEntry, idx int, selected bool, hovered bool, extraWidgets ...layout.Widget) layout.Dimensions {
	th := w.app.Theme

	isRenaming := w.RenameIdx == idx

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			// Checkbox area
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				size := gtx.Dp(28)
				if selected {
					return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconCheck, 16, ColorAccent)
					})
				}
				return layout.Dimensions{Size: image.Pt(size, 0)}
			}),
			// Icon
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				icon := IconFile
				clr := ColorTextDim
				if entry.IsDir {
					icon = IconFolder
					clr = ColorAccent
				}
				return layoutIcon(gtx, icon, 16, clr)
			}),
			// Name (flex) — or rename editor
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if isRenaming {
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
								rr := gtx.Dp(3)
								paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									e := material.Editor(th.Material, &w.renameEditor, "")
									e.Color = ColorText
									e.TextSize = th.Sp(13)
									return e.Layout(gtx)
								})
							},
						)
					}
					lbl := material.Body2(th.Material, entry.Name)
					lbl.Color = ColorText
					lbl.TextSize = th.Sp(13)
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				})
			}),
			// Size (80dp)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(80)
				if entry.IsDir {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
				}
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(th.Material, FormatBytes(entry.Size))
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
			// Modified (130dp)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(130)
				if entry.ModTime.IsZero() {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
				}
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(th.Material, formatModTime(entry.ModTime))
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
			// Extra widgets (delete, download, etc.)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(extraWidgets) == 0 {
					return layout.Dimensions{Size: image.Pt(gtx.Dp(40), 0)}
				}
				var items []layout.FlexChild
				for _, w := range extraWidgets {
					w := w
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, w)
					}))
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
			}),
		)
	})
	call := macro.Stop()

	// Background — selected or hovered
	if selected {
		rr := gtx.Dp(4)
		paint.FillShape(gtx.Ops, ColorSelected, clip.RRect{
			Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
	} else if hovered {
		rr := gtx.Dp(4)
		paint.FillShape(gtx.Ops, ColorHover, clip.RRect{
			Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
	}

	call.Add(gtx.Ops)
	return dims
}

// LayoutDeleteSelectedBar renders the "Delete N selected" bar.
func (w *FileExplorerWidget) LayoutDeleteSelectedBar(gtx layout.Context, deleteBtn *widget.Clickable, clearBtn *widget.Clickable) layout.Dimensions {
	count := w.SelectedCount()
	if count == 0 {
		return layout.Dimensions{}
	}

	th := w.app.Theme
	bg := color.NRGBA{R: 40, G: 30, B: 30, A: 255}

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th.Material, fmt.Sprintf("%d selected", count))
				lbl.Color = ColorText
				lbl.TextSize = th.Sp(12)
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return deleteBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutSmallButton(gtx, th, "Delete Selected", ColorDanger, deleteBtn.Hovered())
					})
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return clearBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutSmallButton(gtx, th, "Clear", ColorTextDim, clearBtn.Hovered())
					})
				})
			}),
		)
	})
	call := macro.Stop()

	paint.FillShape(gtx.Ops, bg, clip.Rect{Max: dims.Size}.Op())
	call.Add(gtx.Ops)
	return dims
}

// formatModTime formats a modification time for display.
// Today: "15:04", this year: "2 Jan 15:04", older: "2 Jan 2006"
func formatModTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())

	if t.After(today) {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("2 Jan 15:04")
	}
	return t.Format("2 Jan 2006")
}

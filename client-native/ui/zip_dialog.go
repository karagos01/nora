package ui

import (
	"archive/zip"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// --- ZipUploadDialog ---
// Shown after file selection: offers upload/ZIP/P2P variants.

type ZipUploadDialog struct {
	app     *App
	Visible bool

	paths    []string
	onResult func(paths []string)
	onP2P    func(paths []string) // P2P send callback

	nameEditor widget.Editor
	needsFocus bool
	zipBtn     widget.Clickable
	indivBtn   widget.Clickable
	p2pBtn     widget.Clickable
	p2pZipBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable

	fileList widget.List
}

func NewZipUploadDialog(a *App) *ZipUploadDialog {
	d := &ZipUploadDialog{app: a}
	d.fileList.Axis = layout.Vertical
	d.nameEditor.SingleLine = true
	d.nameEditor.Submit = true
	return d
}

func (d *ZipUploadDialog) Show(paths []string, onResult func([]string), onP2P ...func([]string)) {
	d.Visible = true
	d.paths = paths
	d.onResult = onResult
	d.onP2P = nil
	if len(onP2P) > 0 {
		d.onP2P = onP2P[0]
	}
	baseName := fmt.Sprintf("nora-upload-%d", time.Now().UnixMilli())
	d.nameEditor.SetText(baseName)
	d.nameEditor.SetCaret(len(baseName), 0)
	d.needsFocus = true
}

func (d *ZipUploadDialog) Hide() {
	d.Visible = false
	d.paths = nil
	d.onResult = nil
	d.onP2P = nil
}

func (d *ZipUploadDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	if d.needsFocus {
		d.needsFocus = false
		gtx.Execute(key.FocusCmd{Tag: &d.nameEditor})
	}

	for {
		ev, ok := d.nameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			d.zipBtn.Click()
		}
	}

	if d.zipBtn.Clicked(gtx) {
		paths := d.paths
		fn := d.onResult
		baseName := strings.TrimSpace(d.nameEditor.Text())
		if baseName == "" {
			baseName = fmt.Sprintf("nora-upload-%d", time.Now().UnixMilli())
		}
		zipName := baseName + ".zip"
		d.Hide()
		if fn != nil {
			go func() {
				zipPath, err := createZipFromPaths(paths, zipName)
				if err != nil {
					fn(paths)
				} else {
					fn([]string{zipPath})
				}
				d.app.Window.Invalidate()
			}()
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.indivBtn.Clicked(gtx) {
		paths := d.paths
		fn := d.onResult
		d.Hide()
		if fn != nil {
			fn(paths)
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.p2pBtn.Clicked(gtx) {
		paths := d.paths
		fn := d.onP2P
		d.Hide()
		if fn != nil {
			fn(paths)
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.p2pZipBtn.Clicked(gtx) {
		paths := d.paths
		fn := d.onP2P
		baseName := strings.TrimSpace(d.nameEditor.Text())
		if baseName == "" {
			baseName = fmt.Sprintf("nora-upload-%d", time.Now().UnixMilli())
		}
		zipName := baseName + ".zip"
		d.Hide()
		if fn != nil {
			go func() {
				zipPath, err := createZipFromPaths(paths, zipName)
				if err != nil {
					fn(paths)
				} else {
					fn([]string{zipPath})
				}
				d.app.Window.Invalidate()
			}()
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			cardW := gtx.Dp(400)
			if cardW > gtx.Constraints.Max.X-gtx.Dp(40) {
				cardW = gtx.Constraints.Max.X - gtx.Dp(40)
			}
			maxH := gtx.Constraints.Max.Y * 60 / 100
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
						return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									title := "Upload files"
									if d.onP2P != nil {
										title = "Send files"
									}
									lbl := material.H6(d.app.Theme.Material, title)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										text := fmt.Sprintf("%d files selected", len(d.paths))
										if len(d.paths) == 1 {
											text = "1 file selected"
										}
										lbl := material.Body2(d.app.Theme.Material, text)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										maxListH := gtx.Dp(150)
										gtx.Constraints.Max.Y = maxListH
										return d.layoutFileList(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										var total int64
										for _, p := range d.paths {
											if info, err := os.Stat(p); err == nil {
												total += info.Size()
											}
										}
										text := fmt.Sprintf("Total: %s", FormatBytes(total))
										lbl := material.Caption(d.app.Theme.Material, text)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "ZIP file name")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
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
														return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															ed := material.Editor(d.app.Theme.Material, &d.nameEditor, "archive")
															ed.Color = ColorText
															ed.HintColor = ColorTextDim
															return ed.Layout(gtx)
														})
													},
												)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body2(d.app.Theme.Material, ".zip")
													lbl.Color = ColorTextDim
													return lbl.Layout(gtx)
												})
											}),
										)
									})
								}),
								// Upload to server — two buttons 50/50
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									gap := gtx.Dp(8)
									half := (gtx.Constraints.Max.X - gap) / 2
									return layout.Flex{}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											gtx.Constraints.Min.X = half
											gtx.Constraints.Max.X = half
											indivLabel := "Upload individually"
											if len(d.paths) == 1 {
												indivLabel = "Upload"
											}
											return d.layoutEqualBtn(gtx, &d.indivBtn, indivLabel, ColorInput, ColorText)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Dimensions{Size: image.Pt(gap, 0)}
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											gtx.Constraints.Min.X = half
											gtx.Constraints.Max.X = half
											return d.layoutEqualBtn(gtx, &d.zipBtn, "Upload as ZIP", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
										}),
									)
								}),
								// P2P — two buttons 50/50
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if d.onP2P == nil {
										return layout.Dimensions{}
									}
									return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										gap := gtx.Dp(8)
										half := (gtx.Constraints.Max.X - gap) / 2
										return layout.Flex{}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												gtx.Constraints.Min.X = half
												gtx.Constraints.Max.X = half
												return d.layoutEqualBtn(gtx, &d.p2pBtn, "Share P2P", ColorInput, ColorAccent)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Dimensions{Size: image.Pt(gap, 0)}
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												gtx.Constraints.Min.X = half
												gtx.Constraints.Max.X = half
												return d.layoutEqualBtn(gtx, &d.p2pZipBtn, "P2P as ZIP", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
											}),
										)
									})
								}),
							)
						})
					},
				)
			})
		}),
	)
}

func (d *ZipUploadDialog) layoutFileList(gtx layout.Context) layout.Dimensions {
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
			return material.List(d.app.Theme.Material, &d.fileList).Layout(gtx, len(d.paths), func(gtx layout.Context, idx int) layout.Dimensions {
				p := d.paths[idx]
				name := filepath.Base(p)
				var sizeStr string
				if info, err := os.Stat(p); err == nil {
					sizeStr = FormatBytes(info.Size())
				}
				return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(d.app.Theme.Material, name)
							lbl.Color = ColorText
							lbl.MaxLines = 1
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(d.app.Theme.Material, sizeStr)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			})
		},
	)
}

// layoutEqualBtn — button with centered text, fills the full width of constraints.
func (d *ZipUploadDialog) layoutEqualBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hoverBg := bg
		if btn.Hovered() {
			hoverBg = color.NRGBA{
				R: min8(bg.R + 20),
				G: min8(bg.G + 20),
				B: min8(bg.B + 20),
				A: 255,
			}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(d.app.Theme.Material, text)
						lbl.Color = fg
						return lbl.Layout(gtx)
					})
				})
			},
		)
	})
}

// --- ZipExtractDialog ---
// Shown IMMEDIATELY when a .zip file download starts.
// User pre-selects a choice, which is applied after the download completes.
// Cannot be dismissed by clicking outside — user must choose an action.

const (
	zipChoiceExtractDelete = 1
	zipChoiceExtractOnly   = 2
	zipChoiceKeep          = 3
)

type ZipExtractDialog struct {
	app     *App
	Visible bool

	zipPath    string
	filename   string
	choiceCh   chan int // choice sent after button click
	downloaded bool    // true when download completed

	extractDeleteBtn widget.Clickable
	extractBtn       widget.Clickable
	keepBtn          widget.Clickable
	overlayBtn       widget.Clickable // dark background only, doesn't handle click
	cardBtn          widget.Clickable
}

func NewZipExtractDialog(a *App) *ZipExtractDialog {
	return &ZipExtractDialog{app: a}
}

func (d *ZipExtractDialog) Show(zipPath string) {
	d.Visible = true
	d.zipPath = zipPath
	d.filename = filepath.Base(zipPath)
	d.downloaded = false
	d.choiceCh = make(chan int, 1)
}

// MarkDownloaded signals that the download is complete. Updates the dialog text.
func (d *ZipExtractDialog) MarkDownloaded() {
	d.downloaded = true
	d.app.Window.Invalidate()
}

// WaitChoice blocks until the user selects an action. Returns zipChoiceExtractDelete/ExtractOnly/Keep.
func (d *ZipExtractDialog) WaitChoice() int {
	if d.choiceCh == nil {
		return zipChoiceKeep
	}
	return <-d.choiceCh
}

// Cancel hides the dialog without waiting for a choice (for download errors).
func (d *ZipExtractDialog) Cancel() {
	d.Visible = false
	d.choiceCh = nil
	d.app.Window.Invalidate()
}

func (d *ZipExtractDialog) sendChoice(choice int) {
	d.Visible = false
	if d.choiceCh != nil {
		select {
		case d.choiceCh <- choice:
		default:
		}
	}
}

func (d *ZipExtractDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	if d.extractDeleteBtn.Clicked(gtx) {
		d.sendChoice(zipChoiceExtractDelete)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.extractBtn.Clicked(gtx) {
		d.sendChoice(zipChoiceExtractOnly)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.keepBtn.Clicked(gtx) {
		d.sendChoice(zipChoiceKeep)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	d.cardBtn.Clicked(gtx)
	d.overlayBtn.Clicked(gtx)

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			cardW := gtx.Dp(380)
			if cardW > gtx.Constraints.Max.X-gtx.Dp(40) {
				cardW = gtx.Constraints.Max.X - gtx.Dp(40)
			}
			gtx.Constraints.Max.X = cardW
			gtx.Constraints.Min.X = cardW

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
						return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									title := "Downloading ZIP..."
									if d.downloaded {
										title = "Download complete"
									}
									lbl := material.H6(d.app.Theme.Material, title)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										text := fmt.Sprintf("What to do with %s after download?", d.filename)
										if d.downloaded {
											text = fmt.Sprintf("%s ready. Extract files?", d.filename)
										}
										lbl := material.Body2(d.app.Theme.Material, text)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return d.layoutFullWidthBtn(gtx, &d.extractDeleteBtn, "Extract & delete ZIP", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return d.layoutFullWidthBtn(gtx, &d.extractBtn, "Extract only", ColorInput, ColorText)
											})
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return d.layoutFullWidthBtn(gtx, &d.keepBtn, "Keep ZIP", ColorInput, ColorText)
											})
										}),
									)
								}),
							)
						})
					},
				)
			})
		}),
	)
}

func (d *ZipExtractDialog) layoutFullWidthBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hoverBg := bg
		if btn.Hovered() {
			hoverBg = color.NRGBA{
				R: min8(bg.R + 20),
				G: min8(bg.G + 20),
				B: min8(bg.B + 20),
				A: 255,
			}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, hoverBg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(d.app.Theme.Material, text)
						lbl.Color = fg
						return lbl.Layout(gtx)
					})
				})
			},
		)
	})
}

// --- Overwrite check ---

func zipConflicts(zipPath, destDir string) []string {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil
	}
	defer r.Close()

	var conflicts []string
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || strings.HasPrefix(name, string(os.PathSeparator)) {
			continue
		}
		target := filepath.Join(destDir, name)
		rel, err := filepath.Rel(destDir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if _, err := os.Stat(target); err == nil {
			conflicts = append(conflicts, name)
		}
	}
	return conflicts
}

func formatConflictMessage(conflicts []string) string {
	if len(conflicts) == 1 {
		return fmt.Sprintf("\"%s\" already exists. Overwrite?", conflicts[0])
	}
	if len(conflicts) <= 5 {
		return fmt.Sprintf("%d files already exist:\n%s\n\nOverwrite?", len(conflicts), strings.Join(conflicts, ", "))
	}
	shown := strings.Join(conflicts[:5], ", ")
	return fmt.Sprintf("%d files already exist:\n%s and %d more\n\nOverwrite?", len(conflicts), shown, len(conflicts)-5)
}

// --- Utility functions ---

func createZipFromPaths(paths []string, zipName string) (string, error) {
	zipPath := filepath.Join(os.TempDir(), zipName)

	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			continue
		}
		header.Name = filepath.Base(p)
		header.Method = zip.Deflate

		writer, err := w.CreateHeader(header)
		if err != nil {
			continue
		}

		src, err := os.Open(p)
		if err != nil {
			continue
		}
		io.Copy(writer, src)
		src.Close()
	}

	return zipPath, nil
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || strings.HasPrefix(name, string(os.PathSeparator)) {
			continue
		}
		target := filepath.Join(destDir, name)

		rel, err := filepath.Rel(destDir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(target), 0755)

		dst, err := os.Create(target)
		if err != nil {
			continue
		}

		src, err := f.Open()
		if err != nil {
			dst.Close()
			continue
		}

		io.Copy(dst, io.LimitReader(src, 1<<30))
		src.Close()
		dst.Close()
	}

	return nil
}

package ui

import (
	"image"
	"image/color"
	"log"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// CategoryEditDialog is a popup dialog for editing category name and color.
type CategoryEditDialog struct {
	app     *App
	Visible bool

	catID    string
	nameEd   widget.Editor
	colorBtns [20]widget.Clickable
	selectedColor int // index into categoryColorPresets, -1 = custom

	confirmBtn widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
}

func NewCategoryEditDialog(a *App) *CategoryEditDialog {
	d := &CategoryEditDialog{app: a}
	d.nameEd.SingleLine = true
	d.nameEd.Submit = true
	return d
}

func (d *CategoryEditDialog) Show(catID, name, hexColor string) {
	d.Visible = true
	d.catID = catID
	d.nameEd.SetText(name)
	d.selectedColor = -1
	for i, preset := range categoryColorPresets {
		if strings.EqualFold(hexColor, preset) {
			d.selectedColor = i
			break
		}
	}
}

func (d *CategoryEditDialog) Hide() {
	d.Visible = false
}

func (d *CategoryEditDialog) submit() {
	name := d.nameEd.Text()
	if name == "" {
		return
	}
	clr := ""
	if d.selectedColor >= 0 && d.selectedColor < len(categoryColorPresets) {
		clr = categoryColorPresets[d.selectedColor]
	}
	if clr == "" {
		clr = "#555555"
	}
	catID := d.catID
	d.Hide()
	go func() {
		if c := d.app.Conn(); c != nil {
			if err := c.Client.UpdateCategory(catID, name, clr); err != nil {
				log.Printf("UpdateCategory: %v", err)
				return
			}
			d.app.Window.Invalidate()
		}
	}()
}

func (d *CategoryEditDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	// Color swatch clicks
	for i := range d.colorBtns {
		if d.colorBtns[i].Clicked(gtx) {
			d.selectedColor = i
		}
	}

	if d.confirmBtn.Clicked(gtx) {
		d.submit()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if d.cancelBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	d.cardBtn.Clicked(gtx)
	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// Enter to submit
	for {
		ev, ok := d.nameEd.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			d.submit()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		// Overlay
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		// Card
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(360)
			gtx.Constraints.Min.X = gtx.Dp(360)

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
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, "Edit Category")
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								// Name field
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutField(gtx, d.app.Theme, "Category name", &d.nameEd)
									})
								}),
								// Color label
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "Color")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Color grid
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutColorGrid(gtx)
								}),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutDialogBtn(gtx, d.app.Theme, &d.cancelBtn, "Cancel", ColorInput, ColorText)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layoutDialogBtn(gtx, d.app.Theme, &d.confirmBtn, "Save", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
												})
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

func (d *CategoryEditDialog) layoutColorGrid(gtx layout.Context) layout.Dimensions {
	const colsPerRow = 10
	var rows []layout.FlexChild
	for start := 0; start < len(categoryColorPresets); start += colsPerRow {
		rowStart := start
		rowEnd := start + colsPerRow
		if rowEnd > len(categoryColorPresets) {
			rowEnd = len(categoryColorPresets)
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var items []layout.FlexChild
			for i := rowStart; i < rowEnd; i++ {
				idx := i
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return d.layoutColorSwatch(gtx, idx)
					})
				}))
			}
			return layout.Flex{}.Layout(gtx, items...)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (d *CategoryEditDialog) layoutColorSwatch(gtx layout.Context, idx int) layout.Dimensions {
	selected := d.selectedColor == idx
	clr := parseHexColor(categoryColorPresets[idx])

	return d.colorBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(26)
		outerSize := size
		if selected {
			outerSize = size + gtx.Dp(4)
		}

		return layout.Stack{Alignment: layout.Center}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				if selected {
					sz := image.Pt(outerSize, outerSize)
					paint.FillShape(gtx.Ops, ColorText, clip.Ellipse{Max: sz}.Op(gtx.Ops))
					return layout.Dimensions{Size: sz}
				}
				return layout.Dimensions{Size: image.Pt(outerSize, outerSize)}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(size, size)
				paint.FillShape(gtx.Ops, clr, clip.Ellipse{Max: sz}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz}
			}),
		)
	})
}


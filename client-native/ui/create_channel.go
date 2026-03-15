package ui

import (
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

type catPickerItem struct {
	id      string
	name    string
	color   string
	isChild bool
}

var categoryColorPresets = []string{
	"#555555", // grey (default)
	"#7c5cbf", // purple (accent)
	"#3498db", // blue
	"#2ecc71", // green
	"#f1c40f", // yellow
	"#e67e22", // orange
	"#e74c3c", // red
	"#e91e63", // pink
}

type CreateDialog struct {
	app     *App
	Visible bool

	// Mode: 0 = channel, 1 = category
	mode int

	nameEditor widget.Editor
	createBtn  widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable

	// Mode toggle
	channelModeBtn  widget.Clickable
	categoryModeBtn widget.Clickable

	// Channel fields
	textTypeBtn  widget.Clickable
	voiceTypeBtn widget.Clickable
	lobbyTypeBtn widget.Clickable
	lanTypeBtn   widget.Clickable
	channelType  string // "text", "voice", "lobby" or "lan"
	catBtns      []widget.Clickable
	selectedCat  *string // nil = no category

	// Category fields
	colorBtns        [8]widget.Clickable
	selectedColor    int
	childCatBtns     []widget.Clickable
	selectedChildren map[string]bool // cat IDs to move inside new root
}

func NewCreateDialog(a *App) *CreateDialog {
	d := &CreateDialog{
		app:         a,
		channelType: "text",
	}
	d.nameEditor.SingleLine = true
	d.nameEditor.Submit = true
	return d
}

func (d *CreateDialog) Show() {
	d.Visible = true
	d.mode = 0
	d.nameEditor.SetText("")
	d.channelType = "text"
	d.selectedCat = nil
	d.selectedColor = 0
	d.selectedChildren = nil
}

func (d *CreateDialog) Hide() {
	d.Visible = false
}

func (d *CreateDialog) submit() {
	name := d.nameEditor.Text()
	if name == "" {
		return
	}
	d.Hide()
	if d.mode == 0 {
		chType := d.channelType
		catID := d.selectedCat
		go func() {
			if c := d.app.Conn(); c != nil {
				_, err := c.Client.CreateChannel(name, chType, catID)
				if err != nil {
					log.Printf("CreateChannel: %v", err)
					return
				}
				// Kanál se přidá přes WS event channel.create
				d.app.Window.Invalidate()
			}
		}()
	} else {
		clr := categoryColorPresets[d.selectedColor]
		// Zkopírovat vybrané children
		children := make(map[string]bool)
		for k, v := range d.selectedChildren {
			children[k] = v
		}
		go func() {
			if c := d.app.Conn(); c != nil {
				cat, err := c.Client.CreateCategory(name, clr, nil)
				if err != nil {
					log.Printf("CreateCategory: %v", err)
					return
				}
				// Přesunout vybrané kategorie dovnitř nové root
				for childID := range children {
					if err := c.Client.SetCategoryParent(childID, cat.ID); err != nil {
						log.Printf("SetCategoryParent: %v", err)
					}
				}
				d.app.Window.Invalidate()
			}
		}()
	}
}

func (d *CreateDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	conn := d.app.Conn()

	// Get categories for channel mode
	var cats []api.ChannelCategory
	if conn != nil {
		d.app.mu.RLock()
		cats = make([]api.ChannelCategory, len(conn.Categories))
		copy(cats, conn.Categories)
		d.app.mu.RUnlock()
	}
	// Flat list přiřaditelných kategorií pro channel mode:
	// všechny kategorie (root i child)
	var assignableCats []catPickerItem
	for _, cat := range cats {
		assignableCats = append(assignableCats, catPickerItem{id: cat.ID, name: cat.Name, color: cat.Color})
		for _, child := range cat.Children {
			assignableCats = append(assignableCats, catPickerItem{
				id: child.ID, name: cat.Name + " / " + child.Name, color: child.Color, isChild: true,
			})
		}
	}

	if len(d.catBtns) < len(assignableCats)+1 {
		d.catBtns = make([]widget.Clickable, len(assignableCats)+5)
	}
	if len(d.childCatBtns) < len(cats)+1 {
		d.childCatBtns = make([]widget.Clickable, len(cats)+5)
	}

	// Handle mode toggle
	if d.channelModeBtn.Clicked(gtx) {
		d.mode = 0
		d.nameEditor.SetText("")
	}
	if d.categoryModeBtn.Clicked(gtx) {
		d.mode = 1
		d.nameEditor.SetText("")
	}

	// Channel type toggle
	if d.textTypeBtn.Clicked(gtx) {
		d.channelType = "text"
	}
	if d.voiceTypeBtn.Clicked(gtx) {
		d.channelType = "voice"
	}
	if d.lobbyTypeBtn.Clicked(gtx) {
		d.channelType = "lobby"
	}
	if d.lanTypeBtn.Clicked(gtx) {
		d.channelType = "lan"
	}

	// Category selection (channel mode) — assignable cats
	for i := range assignableCats {
		if d.catBtns[i].Clicked(gtx) {
			id := assignableCats[i].id
			if d.selectedCat != nil && *d.selectedCat == id {
				d.selectedCat = nil
			} else {
				d.selectedCat = &id
			}
		}
	}

	// Children category selection (category mode) — multi-select toggle
	for i := range cats {
		if d.childCatBtns[i].Clicked(gtx) {
			id := cats[i].ID
			if d.selectedChildren == nil {
				d.selectedChildren = make(map[string]bool)
			}
			if d.selectedChildren[id] {
				delete(d.selectedChildren, id)
			} else {
				d.selectedChildren[id] = true
			}
		}
	}

	// Color selection (category mode)
	for i := range d.colorBtns {
		if d.colorBtns[i].Clicked(gtx) {
			d.selectedColor = i
		}
	}

	// Enter to confirm
	for {
		ev, ok := d.nameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			d.submit()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
	}

	// Handle create / cancel
	if d.createBtn.Clicked(gtx) {
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

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, color.NRGBA{A: 140}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(340)
			gtx.Constraints.Min.X = gtx.Dp(340)

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
							return d.layoutContent(gtx, cats, assignableCats)
						})
					},
				)
			})
		}),
	)
}

func (d *CreateDialog) layoutContent(gtx layout.Context, cats []api.ChannelCategory, assignableCats []catPickerItem) layout.Dimensions {
	var items []layout.FlexChild

	// Title
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		lbl := material.H6(d.app.Theme.Material, "Create")
		lbl.Color = ColorText
		return lbl.Layout(gtx)
	}))

	// Mode toggle: Channel / Category
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return d.layoutToggleBtn(gtx, &d.channelModeBtn, "Channel", d.mode == 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(gtx.Dp(4), 0)}
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return d.layoutToggleBtn(gtx, &d.categoryModeBtn, "Category", d.mode == 1)
				}),
			)
		})
	}))

	// Name input
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		hint := "Channel name"
		if d.mode == 1 {
			hint = "Category name"
		}
		return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return d.layoutEditor(gtx, &d.nameEditor, hint)
		})
	}))

	if d.mode == 0 {
		// Channel: type toggle
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(d.app.Theme.Material, "Type:")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutTypeBtn(gtx, &d.textTypeBtn, "Text", d.channelType == "text")
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutTypeBtn(gtx, &d.voiceTypeBtn, "Voice", d.channelType == "voice")
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutTypeBtn(gtx, &d.lobbyTypeBtn, "Lobby", d.channelType == "lobby")
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutTypeBtn(gtx, &d.lanTypeBtn, "LAN", d.channelType == "lan")
						})
					}),
				)
			})
		}))

		// Channel: category selection (hierarchický — standalone + child kategorie)
		if len(assignableCats) > 0 {
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, "Category (optional):")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}))
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var catItems []layout.FlexChild
					for i, ac := range assignableCats {
						idx := i
						c := ac
						selected := d.selectedCat != nil && *d.selectedCat == c.id
						catItems = append(catItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutCatBtn(gtx, &d.catBtns[idx], c.name, parseHexColor(c.color), selected)
							})
						}))
					}
					return layout.Flex{Alignment: layout.Start}.Layout(gtx, catItems...)
				})
			}))
		}
	} else {
		// Category: color selection
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(d.app.Theme.Material, "Color:")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutColorGrid(gtx)
			})
		}))

		// Include existing categories (optional) — přesun dovnitř nové root
		if len(cats) > 0 {
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, "Move inside (optional):")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}))
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var catItems []layout.FlexChild
					for i, cat := range cats {
						idx := i
						c := cat
						selected := d.selectedChildren[c.ID]
						catItems = append(catItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutCatBtn(gtx, &d.childCatBtns[idx], c.Name, parseHexColor(c.Color), selected)
							})
						}))
					}
					return layout.Flex{Alignment: layout.Start}.Layout(gtx, catItems...)
				})
			}))
		}
	}

	// Buttons
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return d.layoutBtn(gtx, &d.cancelBtn, "Cancel", ColorInput, ColorText)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return d.layoutBtn(gtx, &d.createBtn, "Create", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
					})
				}),
			)
		})
	}))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (d *CreateDialog) layoutColorGrid(gtx layout.Context) layout.Dimensions {
	var items []layout.FlexChild
	for i := range categoryColorPresets {
		idx := i
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutColorSwatch(gtx, idx)
			})
		}))
	}
	return layout.Flex{}.Layout(gtx, items...)
}

func (d *CreateDialog) layoutColorSwatch(gtx layout.Context, idx int) layout.Dimensions {
	selected := d.selectedColor == idx
	clr := parseHexColor(categoryColorPresets[idx])

	return d.colorBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(28)
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

func (d *CreateDialog) layoutToggleBtn(gtx layout.Context, btn *widget.Clickable, text string, active bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		fg := ColorTextDim
		if active {
			bg = ColorAccent
			fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		} else if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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

func (d *CreateDialog) layoutEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(d.app.Theme.Material, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}

func (d *CreateDialog) layoutTypeBtn(gtx layout.Context, btn *widget.Clickable, text string, active bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if active {
			bg = ColorAccent
		} else if btn.Hovered() {
			bg = ColorHover
		}
		fg := ColorTextDim
		if active {
			fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
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
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, text)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (d *CreateDialog) layoutCatBtn(gtx layout.Context, btn *widget.Clickable, name string, catColor color.NRGBA, selected bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if selected {
			bg = ColorSelected
		} else if btn.Hovered() {
			bg = ColorHover
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
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							size := image.Pt(gtx.Dp(3), gtx.Dp(12))
							paint.FillShape(gtx.Ops, catColor, clip.Rect{Max: size}.Op())
							return layout.Dimensions{Size: size}
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(d.app.Theme.Material, name)
							if selected {
								lbl.Color = ColorText
							} else {
								lbl.Color = ColorTextDim
							}
							return lbl.Layout(gtx)
						})
					}),
				)
			},
		)
	})
}

func (d *CreateDialog) layoutBtn(gtx layout.Context, btn *widget.Clickable, text string, bg, fg color.NRGBA) layout.Dimensions {
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
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(d.app.Theme.Material, text)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

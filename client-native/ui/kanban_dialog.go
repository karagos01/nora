package ui

import (
	"image"
	"image/color"
	"log"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

// CardEditDialog is an overlay dialog for editing a kanban card.
type CardEditDialog struct {
	app     *App
	Visible bool

	Card    *api.KanbanCard
	Board   *api.KanbanBoard
	BoardID string

	titleEd   widget.Editor
	descEd    widget.Editor
	saveBtn   widget.Clickable
	deleteBtn widget.Clickable
	closeBtn  widget.Clickable

	// Move dropdown
	moveExpanded bool
	moveBtn      widget.Clickable
	moveBtns     []widget.Clickable

	// Color picker
	colorBtns [8]widget.Clickable
	colorIdx  int

	// Assign
	assignExpanded bool
	assignBtn      widget.Clickable
	assignBtns     []widget.Clickable

	// Due date
	dueDateEd       widget.Editor
	dueTodayBtn     widget.Clickable
	dueTomorrowBtn  widget.Clickable
	dueWeekBtn      widget.Clickable
	dueClearBtn     widget.Clickable
}

var cardColors = [8]struct {
	hex   string
	color color.NRGBA
}{
	{"", color.NRGBA{R: 80, G: 80, B: 90, A: 255}},         // none/default
	{"#5865f2", color.NRGBA{R: 88, G: 101, B: 242, A: 255}}, // blue
	{"#57f287", color.NRGBA{R: 87, G: 242, B: 135, A: 255}}, // green
	{"#fee75c", color.NRGBA{R: 254, G: 231, B: 92, A: 255}}, // yellow
	{"#ed4245", color.NRGBA{R: 237, G: 66, B: 69, A: 255}},  // red
	{"#eb459e", color.NRGBA{R: 235, G: 69, B: 158, A: 255}}, // pink
	{"#f47b67", color.NRGBA{R: 244, G: 123, B: 103, A: 255}}, // orange
	{"#9b59b6", color.NRGBA{R: 155, G: 89, B: 182, A: 255}}, // purple
}

func NewCardEditDialog(a *App) *CardEditDialog {
	d := &CardEditDialog{app: a}
	d.titleEd.SingleLine = true
	d.titleEd.Submit = true
	d.descEd.SingleLine = false
	d.dueDateEd.SingleLine = true
	d.dueDateEd.Submit = true
	return d
}

func (d *CardEditDialog) Open(card api.KanbanCard, board *api.KanbanBoard) {
	d.Card = &card
	d.Board = board
	d.BoardID = board.ID
	d.Visible = true
	d.titleEd.SetText(card.Title)
	d.descEd.SetText(card.Description)
	d.moveExpanded = false
	d.assignExpanded = false

	// Find color index
	d.colorIdx = 0
	for i, c := range cardColors {
		if c.hex == card.Color {
			d.colorIdx = i
			break
		}
	}

	// Set due date editor
	if card.DueDate != nil {
		d.dueDateEd.SetText(card.DueDate.Format("2006-01-02"))
	} else {
		d.dueDateEd.SetText("")
	}
}

func (d *CardEditDialog) Close() {
	d.Visible = false
	d.Card = nil
	d.Board = nil
}

func (d *CardEditDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible || d.Card == nil {
		return layout.Dimensions{}
	}

	// Click handlers
	if d.closeBtn.Clicked(gtx) {
		d.Close()
		return layout.Dimensions{}
	}
	if d.saveBtn.Clicked(gtx) {
		d.save()
		return layout.Dimensions{}
	}
	if d.deleteBtn.Clicked(gtx) {
		cardID := d.Card.ID
		d.Close()
		d.app.ConfirmDlg.ShowConfirm("Delete Card", "Delete this card permanently?", func() {
			d.deleteCard(cardID)
		})
		return layout.Dimensions{}
	}

	// Move dropdown
	if d.moveBtn.Clicked(gtx) {
		d.moveExpanded = !d.moveExpanded
		d.assignExpanded = false
	}
	if d.moveExpanded && d.Board != nil {
		cols := d.Board.Columns
		if len(d.moveBtns) < len(cols) {
			d.moveBtns = make([]widget.Clickable, len(cols))
		}
		for i := range cols {
			if d.moveBtns[i].Clicked(gtx) {
				d.moveCard(cols[i].ID)
				d.moveExpanded = false
			}
		}
	}

	// Assign dropdown
	if d.assignBtn.Clicked(gtx) {
		d.assignExpanded = !d.assignExpanded
		d.moveExpanded = false
	}

	// Color buttons
	for i := range d.colorBtns {
		if d.colorBtns[i].Clicked(gtx) {
			d.colorIdx = i
		}
	}

	// Scrim (dark background)
	paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Dialog box (centered, 420px wide)
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		maxW := gtx.Dp(420)
		if gtx.Constraints.Max.X < maxW {
			maxW = gtx.Constraints.Max.X - gtx.Dp(32)
		}
		gtx.Constraints.Min.X = maxW
		gtx.Constraints.Max.X = maxW

		rr := gtx.Dp(12)
		bg := ColorCard

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Header
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.H6(d.app.Theme.Material, "Edit Card")
									lbl.Color = ColorText
									lbl.Font.Weight = font.Bold
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconClose, 22, ColorTextDim)
									})
								}),
							)
						}),
						// Title
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(d.app.Theme.Material, "TITLE")
								lbl.Color = ColorTextDim
								lbl.Font.Weight = font.Bold
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								ed := material.Editor(d.app.Theme.Material, &d.titleEd, "Card title...")
								ed.Color = ColorText
								ed.HintColor = ColorTextDim
								return ed.Layout(gtx)
							})
						}),
						// Description
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(d.app.Theme.Material, "DESCRIPTION")
								lbl.Color = ColorTextDim
								lbl.Font.Weight = font.Bold
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.Y = gtx.Dp(60)
								ed := material.Editor(d.app.Theme.Material, &d.descEd, "Description...")
								ed.Color = ColorText
								ed.HintColor = ColorTextDim
								return ed.Layout(gtx)
							})
						}),
						// Color picker
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "COLOR")
										lbl.Color = ColorTextDim
										lbl.Font.Weight = font.Bold
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return d.layoutColorPicker(gtx)
										})
									}),
								)
							})
						}),
						// Due date
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutDueDateEditor(gtx)
							})
						}),
						// Move to column
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutMoveDropdown(gtx)
							})
						}),
						// Assign
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutAssignDropdown(gtx)
							})
						}),
						// Buttons row
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									// Delete
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.deleteBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layoutIcon(gtx, IconDelete, 18, ColorDanger)
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Body2(d.app.Theme.Material, "Delete")
														lbl.Color = ColorDanger
														return lbl.Layout(gtx)
													})
												}),
											)
										})
									}),
									// Spacer
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Dimensions{}
									}),
									// Save
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.saveBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											rr := gtx.Dp(6)
											sz := image.Pt(gtx.Dp(80), gtx.Dp(32))
											paint.FillShape(gtx.Ops, ColorAccent, clip.RRect{
												Rect: image.Rect(0, 0, sz.X, sz.Y),
												NE: rr, NW: rr, SE: rr, SW: rr,
											}.Op(gtx.Ops))
											return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												gtx.Constraints.Min = sz
												return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body2(d.app.Theme.Material, "Save")
													lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
													lbl.Font.Weight = font.Bold
													return lbl.Layout(gtx)
												})
											})
										})
									}),
								)
							})
						}),
					)
				})
			}),
		)
	})
}

func (d *CardEditDialog) layoutColorPicker(gtx layout.Context) layout.Dimensions {
	var children []layout.FlexChild
	for i := range cardColors {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.colorBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(24)
					rr := sz / 2
					c := cardColors[i].color
					paint.FillShape(gtx.Ops, c, clip.RRect{
						Rect: image.Rect(0, 0, sz, sz),
						NE: rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					// Checkmark for selected color
					if i == d.colorIdx {
						return layout.Stack{Alignment: layout.Center}.Layout(gtx,
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{Size: image.Pt(sz, sz)}
							}),
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconCheck, 14, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
							}),
						)
					}
					return layout.Dimensions{Size: image.Pt(sz, sz)}
				})
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

// layoutDueDateEditor renders the due date section — text input (YYYY-MM-DD) + preset buttons.
func (d *CardEditDialog) layoutDueDateEditor(gtx layout.Context) layout.Dimensions {
	// Preset buttons — date setting
	if d.dueTodayBtn.Clicked(gtx) {
		d.dueDateEd.SetText(time.Now().Format("2006-01-02"))
	}
	if d.dueTomorrowBtn.Clicked(gtx) {
		d.dueDateEd.SetText(time.Now().AddDate(0, 0, 1).Format("2006-01-02"))
	}
	if d.dueWeekBtn.Clicked(gtx) {
		d.dueDateEd.SetText(time.Now().AddDate(0, 0, 7).Format("2006-01-02"))
	}
	if d.dueClearBtn.Clicked(gtx) {
		d.dueDateEd.SetText("")
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Label
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(d.app.Theme.Material, "DUE DATE")
			lbl.Color = ColorTextDim
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		}),
		// Text input
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				ed := material.Editor(d.app.Theme.Material, &d.dueDateEd, "YYYY-MM-DD")
				ed.Color = ColorText
				ed.HintColor = ColorTextDim
				return ed.Layout(gtx)
			})
		}),
		// Preset buttons
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return d.layoutDuePresetBtn(gtx, &d.dueTodayBtn, "Today")
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutDuePresetBtn(gtx, &d.dueTomorrowBtn, "Tomorrow")
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutDuePresetBtn(gtx, &d.dueWeekBtn, "+1 Week")
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return d.layoutDuePresetBtn(gtx, &d.dueClearBtn, "Clear")
						})
					}),
				)
			})
		}),
	)
}

// layoutDuePresetBtn renders a single preset button for due date.
func (d *CardEditDialog) layoutDuePresetBtn(gtx layout.Context, btn *widget.Clickable, label string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := gtx.Dp(4)
		bg := color.NRGBA{R: 55, G: 58, B: 68, A: 255}
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, gtx.Constraints.Min.Y)}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, label)
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
		)
	})
}

func (d *CardEditDialog) layoutMoveDropdown(gtx layout.Context) layout.Dimensions {
	if d.Board == nil {
		return layout.Dimensions{}
	}

	// Determine current column
	currentCol := ""
	for _, col := range d.Board.Columns {
		if col.ID == d.Card.ColumnID {
			currentCol = col.Title
			break
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.moveBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(d.app.Theme.Material, "MOVE TO")
						lbl.Color = ColorTextDim
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(d.app.Theme.Material, currentCol)
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						icon := IconExpand
						if d.moveExpanded {
							icon = IconCollapse
						}
						return layoutIcon(gtx, icon, 18, ColorTextDim)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !d.moveExpanded {
				return layout.Dimensions{}
			}
			cols := d.Board.Columns
			if len(d.moveBtns) < len(cols) {
				d.moveBtns = make([]widget.Clickable, len(cols))
			}
			var items []layout.FlexChild
			for i, col := range cols {
				i, col := i, col
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					isCurrentCol := col.ID == d.Card.ColumnID
					return d.moveBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						bg := color.NRGBA{A: 0}
						if d.moveBtns[i].Hovered() {
							bg = ColorHover
						}
						rr := gtx.Dp(4)
						paint.FillShape(gtx.Ops, bg, clip.RRect{
							Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Dp(28)),
							NE: rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if isCurrentCol {
										return layoutIcon(gtx, IconCheck, 14, ColorAccent)
									}
									return layout.Dimensions{Size: image.Pt(gtx.Dp(14), 0)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(d.app.Theme.Material, col.Title)
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
		}),
	)
}

func (d *CardEditDialog) layoutAssignDropdown(gtx layout.Context) layout.Dimensions {
	assignedName := "None"
	if d.Card.AssignedUser != nil {
		assignedName = d.Card.AssignedUser.DisplayName
		if assignedName == "" {
			assignedName = d.Card.AssignedUser.Username
		}
	}

	conn := d.app.Conn()
	var members []api.User
	if conn != nil {
		d.app.mu.RLock()
		members = make([]api.User, len(conn.Members))
		copy(members, conn.Members)
		d.app.mu.RUnlock()
	}

	if d.assignExpanded {
		if len(d.assignBtns) < len(members)+1 {
			d.assignBtns = make([]widget.Clickable, len(members)+1)
		}
		// Unassign button
		if d.assignBtns[0].Clicked(gtx) {
			d.Card.AssignedTo = nil
			d.Card.AssignedUser = nil
			d.assignExpanded = false
		}
		for i, m := range members {
			if d.assignBtns[i+1].Clicked(gtx) {
				uid := m.ID
				d.Card.AssignedTo = &uid
				d.Card.AssignedUser = &members[i]
				d.assignExpanded = false
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.assignBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(d.app.Theme.Material, "ASSIGNED TO")
						lbl.Color = ColorTextDim
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(d.app.Theme.Material, assignedName)
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						icon := IconExpand
						if d.assignExpanded {
							icon = IconCollapse
						}
						return layoutIcon(gtx, icon, 18, ColorTextDim)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !d.assignExpanded {
				return layout.Dimensions{}
			}
			var items []layout.FlexChild
			// "None" option
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return d.assignBtns[0].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := color.NRGBA{A: 0}
					if d.assignBtns[0].Hovered() {
						bg = ColorHover
					}
					rr := gtx.Dp(4)
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Dp(28)),
						NE: rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(d.app.Theme.Material, "None")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				})
			}))
			for i, m := range members {
				i, m := i, m
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return d.assignBtns[i+1].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						bg := color.NRGBA{A: 0}
						if d.assignBtns[i+1].Hovered() {
							bg = ColorHover
						}
						rr := gtx.Dp(4)
						paint.FillShape(gtx.Ops, bg, clip.RRect{
							Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Dp(28)),
							NE: rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							name := m.DisplayName
							if name == "" {
								name = m.Username
							}
							isAssigned := d.Card.AssignedTo != nil && *d.Card.AssignedTo == m.ID
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if isAssigned {
										return layoutIcon(gtx, IconCheck, 14, ColorAccent)
									}
									return layout.Dimensions{Size: image.Pt(gtx.Dp(14), 0)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(d.app.Theme.Material, name)
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
		}),
	)
}

func (d *CardEditDialog) save() {
	if d.Card == nil {
		return
	}
	conn := d.app.Conn()
	if conn == nil {
		return
	}

	cardID := d.Card.ID
	updates := map[string]interface{}{
		"title":       d.titleEd.Text(),
		"description": d.descEd.Text(),
		"color":       cardColors[d.colorIdx].hex,
	}
	if d.Card.AssignedTo != nil {
		updates["assigned_to"] = *d.Card.AssignedTo
	} else {
		updates["clear_assign"] = true
	}

	// Due date — parse from editor or clear
	dueTxt := strings.TrimSpace(d.dueDateEd.Text())
	if dueTxt == "" {
		updates["clear_due"] = true
	} else {
		// Send as RFC3339 for server (add T00:00:00Z)
		if _, err := time.Parse("2006-01-02", dueTxt); err == nil {
			updates["due_date"] = dueTxt + "T00:00:00Z"
		}
	}

	d.Close()

	go func() {
		_, err := conn.Client.UpdateKanbanCard(cardID, updates)
		if err != nil {
			log.Printf("kanban: update card: %v", err)
			return
		}
		if d.app.KanbanView.ActiveBoard != nil {
			d.app.KanbanView.LoadBoard(d.app.KanbanView.ActiveBoard.ID)
		}
	}()
}

func (d *CardEditDialog) deleteCard(cardID string) {
	conn := d.app.Conn()
	if conn == nil {
		return
	}
	go func() {
		if err := conn.Client.DeleteKanbanCard(cardID); err != nil {
			log.Printf("kanban: delete card: %v", err)
			return
		}
		if d.app.KanbanView.ActiveBoard != nil {
			d.app.KanbanView.LoadBoard(d.app.KanbanView.ActiveBoard.ID)
		}
	}()
}

func (d *CardEditDialog) moveCard(targetColID string) {
	if d.Card == nil {
		return
	}
	conn := d.app.Conn()
	if conn == nil {
		return
	}
	cardID := d.Card.ID
	go func() {
		if err := conn.Client.MoveKanbanCard(cardID, targetColID, 0); err != nil {
			log.Printf("kanban: move card: %v", err)
			return
		}
		if d.app.KanbanView.ActiveBoard != nil {
			d.app.KanbanView.LoadBoard(d.app.KanbanView.ActiveBoard.ID)
		}
		d.app.Window.Invalidate()
	}()
}

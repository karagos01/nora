package ui

import (
	"encoding/json"
	"fmt"
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

type KanbanView struct {
	app *App

	// Board list (sidebar picker)
	boards      []api.KanbanBoard
	boardBtns   []widget.Clickable
	boardDelBtns []widget.Clickable
	newBoardBtn widget.Clickable
	newBoardEd  widget.Editor
	showCreate  bool
	boardList   widget.List

	// Active board
	ActiveBoard *api.KanbanBoard

	// Column management
	addColBtn widget.Clickable
	addColEd  widget.Editor
	showAddCol bool

	// Card creation (inline per column)
	addCardCol string // column ID where a card is being added
	addCardEd  widget.Editor
	addCardBtns map[string]*widget.Clickable

	// Card buttons (klik → edit dialog)
	cardBtns map[string]*widget.Clickable

	// Column delete buttons
	colDelBtns map[string]*widget.Clickable

	// Horizontal scroll for columns
	colList widget.List

	// Per-column vertical card lists
	cardLists map[string]*widget.List

	// Back button
	backBtn widget.Clickable

	// Loading state
	loading bool
}

func NewKanbanView(a *App) *KanbanView {
	v := &KanbanView{
		app:         a,
		addCardBtns: make(map[string]*widget.Clickable),
		cardBtns:    make(map[string]*widget.Clickable),
		colDelBtns:  make(map[string]*widget.Clickable),
		cardLists:   make(map[string]*widget.List),
	}
	v.boardList.Axis = layout.Vertical
	v.colList.Axis = layout.Horizontal
	v.newBoardEd.SingleLine = true
	v.newBoardEd.Submit = true
	v.addColEd.SingleLine = true
	v.addColEd.Submit = true
	v.addCardEd.SingleLine = true
	v.addCardEd.Submit = true
	return v
}

// LoadBoards loads the list of boards from the server.
func (v *KanbanView) LoadBoards() {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	v.loading = true
	go func() {
		boards, err := conn.Client.ListKanbanBoards()
		if err != nil {
			log.Printf("kanban: list boards: %v", err)
			v.loading = false
			v.app.Window.Invalidate()
			return
		}
		v.boards = boards
		v.loading = false
		v.app.Window.Invalidate()
	}()
}

// LoadBoard loads board detail (columns + cards).
func (v *KanbanView) LoadBoard(id string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	v.loading = true
	go func() {
		board, err := conn.Client.GetKanbanBoard(id)
		if err != nil {
			log.Printf("kanban: get board %s: %v", id, err)
			v.loading = false
			v.app.Window.Invalidate()
			return
		}
		v.ActiveBoard = board
		v.loading = false
		v.app.Window.Invalidate()
	}()
}

func (v *KanbanView) getCardBtn(id string) *widget.Clickable {
	if btn, ok := v.cardBtns[id]; ok {
		return btn
	}
	btn := &widget.Clickable{}
	v.cardBtns[id] = btn
	return btn
}

func (v *KanbanView) getAddCardBtn(colID string) *widget.Clickable {
	if btn, ok := v.addCardBtns[colID]; ok {
		return btn
	}
	btn := &widget.Clickable{}
	v.addCardBtns[colID] = btn
	return btn
}

func (v *KanbanView) getColDelBtn(colID string) *widget.Clickable {
	if btn, ok := v.colDelBtns[colID]; ok {
		return btn
	}
	btn := &widget.Clickable{}
	v.colDelBtns[colID] = btn
	return btn
}

func (v *KanbanView) getCardList(colID string) *widget.List {
	if l, ok := v.cardLists[colID]; ok {
		return l
	}
	l := &widget.List{}
	l.Axis = layout.Vertical
	v.cardLists[colID] = l
	return l
}

// LayoutSidebar renders the left panel (board picker).
func (v *KanbanView) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	if v.backBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewChannels
		v.app.mu.Unlock()
	}

	// Submit handler for new board
	for {
		e, ok := v.newBoardEd.Update(gtx)
		if !ok {
			break
		}
		if sub, ok := e.(widget.SubmitEvent); ok {
			name := strings.TrimSpace(sub.Text)
			if name != "" {
				v.createBoard(name)
				v.newBoardEd.SetText("")
				v.showCreate = false
			}
		}
	}

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
			paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: bounds.Max}.Op())
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconBack, 20, ColorTextDim)
								})
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body1(v.app.Theme.Material, "Kanban Boards")
									lbl.Color = ColorText
									lbl.Font.Weight = font.Bold
									return lbl.Layout(gtx)
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
				// Board list
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.layoutBoardList(gtx)
				}),
				// New board button / input
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutNewBoardArea(gtx)
				}),
			)
		},
	)
}

func (v *KanbanView) layoutBoardList(gtx layout.Context) layout.Dimensions {
	count := len(v.boards)
	if len(v.boardBtns) < count {
		v.boardBtns = make([]widget.Clickable, count+5)
	}
	if len(v.boardDelBtns) < count {
		v.boardDelBtns = make([]widget.Clickable, count+5)
	}

	for i := 0; i < count; i++ {
		if v.boardBtns[i].Clicked(gtx) {
			board := v.boards[i]
			v.LoadBoard(board.ID)
		}
		if v.boardDelBtns[i].Clicked(gtx) {
			boardID := v.boards[i].ID
			v.app.ConfirmDlg.ShowConfirm("Delete Board", "Delete this board and all its cards?", func() {
				v.deleteBoard(boardID)
			})
		}
	}

	return material.List(v.app.Theme.Material, &v.boardList).Layout(gtx, count, func(gtx layout.Context, i int) layout.Dimensions {
		board := v.boards[i]
		isActive := v.ActiveBoard != nil && v.ActiveBoard.ID == board.ID

		return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bg := color.NRGBA{A: 0}
			if isActive {
				bg = ColorHover
			}

			return v.boardBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if v.boardBtns[i].Hovered() && !isActive {
					bg = color.NRGBA{R: 50, G: 50, B: 60, A: 255}
				}

				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Dp(36)),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))

				return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(4), Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconViewColumn, 16, ColorTextDim)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, board.Name)
								lbl.Color = ColorText
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.boardDelBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconDelete, 14, ColorTextDim)
							})
						}),
					)
				})
			})
		})
	})
}

func (v *KanbanView) layoutNewBoardArea(gtx layout.Context) layout.Dimensions {
	if v.newBoardBtn.Clicked(gtx) {
		v.showCreate = !v.showCreate
	}

	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if !v.showCreate {
			return v.newBoardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutIcon(gtx, IconAdd, 18, ColorAccent)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, "New Board")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		}

		ed := material.Editor(v.app.Theme.Material, &v.newBoardEd, "Board name...")
		ed.Color = ColorText
		ed.HintColor = ColorTextDim
		return ed.Layout(gtx)
	})
}

// LayoutMain renders the main area (board with columns and cards).
func (v *KanbanView) LayoutMain(gtx layout.Context) layout.Dimensions {
	// Submit handler for inline card addition
	for {
		e, ok := v.addCardEd.Update(gtx)
		if !ok {
			break
		}
		if sub, ok := e.(widget.SubmitEvent); ok {
			title := strings.TrimSpace(sub.Text)
			if title != "" && v.addCardCol != "" {
				v.createCard(v.addCardCol, title)
				v.addCardEd.SetText("")
				v.addCardCol = ""
			}
		}
	}

	// Submit handler for new column
	for {
		e, ok := v.addColEd.Update(gtx)
		if !ok {
			break
		}
		if sub, ok := e.(widget.SubmitEvent); ok {
			title := strings.TrimSpace(sub.Text)
			if title != "" && v.ActiveBoard != nil {
				v.createColumn(title)
				v.addColEd.SetText("")
				v.showAddCol = false
			}
		}
	}

	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	if v.ActiveBoard == nil {
		if v.loading {
			return layoutCentered(gtx, v.app.Theme, "Loading...", ColorTextDim)
		}
		return layoutCentered(gtx, v.app.Theme, "Select a board from the sidebar", ColorTextDim)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutBoardHeader(gtx)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),
		// Columns (horizontal scroll)
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return v.layoutColumns(gtx)
		}),
	)
}

func (v *KanbanView) layoutBoardHeader(gtx layout.Context) layout.Dimensions {
	if v.addColBtn.Clicked(gtx) {
		v.showAddCol = !v.showAddCol
	}

	return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				lbl := material.H6(v.app.Theme.Material, v.ActiveBoard.Name)
				lbl.Color = ColorText
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.showAddCol {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Max.X = gtx.Dp(160)
						ed := material.Editor(v.app.Theme.Material, &v.addColEd, "Column name...")
						ed.Color = ColorText
						ed.HintColor = ColorTextDim
						return ed.Layout(gtx)
					})
				}
				return v.addColBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconAdd, 18, ColorAccent)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, "Add Column")
								lbl.Color = ColorAccent
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			}),
		)
	})
}

func (v *KanbanView) layoutColumns(gtx layout.Context) layout.Dimensions {
	if v.ActiveBoard == nil {
		return layout.Dimensions{}
	}
	columns := v.ActiveBoard.Columns
	count := len(columns)

	return material.List(v.app.Theme.Material, &v.colList).Layout(gtx, count, func(gtx layout.Context, i int) layout.Dimensions {
		col := columns[i]
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(4), Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutColumn(gtx, col)
		})
	})
}

func (v *KanbanView) layoutColumn(gtx layout.Context, col api.KanbanColumn) layout.Dimensions {
	colWidth := gtx.Dp(260)
	gtx.Constraints.Min.X = colWidth
	gtx.Constraints.Max.X = colWidth

	// Barva sloupce
	colColor := parseHexColor(col.Color)

	// Column delete button
	delBtn := v.getColDelBtn(col.ID)
	if delBtn.Clicked(gtx) {
		colID := col.ID
		v.app.ConfirmDlg.ShowConfirm("Delete Column", "Delete this column and all its cards?", func() {
			v.deleteColumn(colID)
		})
	}

	// Handle add card button for this column
	addBtn := v.getAddCardBtn(col.ID)
	if addBtn.Clicked(gtx) {
		if v.addCardCol == col.ID {
			v.addCardCol = ""
		} else {
			v.addCardCol = col.ID
			v.addCardEd.SetText("")
		}
	}

	rr := gtx.Dp(8)
	bgColor := color.NRGBA{R: 30, G: 32, B: 40, A: 255}

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, bgColor, clip.RRect{
				Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y),
				NE: rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Max.Y)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Column header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							// Color strip
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								sz := image.Pt(gtx.Dp(4), gtx.Dp(16))
								paint.FillShape(gtx.Ops, colColor, clip.RRect{
									Rect: image.Rect(0, 0, sz.X, sz.Y),
									NE: 2, NW: 2, SE: 2, SW: 2,
								}.Op(gtx.Ops))
								return layout.Dimensions{Size: sz}
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body1(v.app.Theme.Material, col.Title)
									lbl.Color = ColorText
									lbl.Font.Weight = font.Bold
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								})
							}),
							// Card count
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								count := len(col.Cards)
								lbl := material.Caption(v.app.Theme.Material, strings.Repeat(" ", 0)+string(rune('0'+count%10)))
								if count >= 10 {
									lbl = material.Caption(v.app.Theme.Material, strings.TrimSpace(strings.Replace(material.Caption(v.app.Theme.Material, "").Text, "", "", -1)))
								}
								// Simpler: just display the count
								lbl = material.Caption(v.app.Theme.Material, formatCount(len(col.Cards)))
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
							// Delete column
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return delBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconDelete, 14, ColorTextDim)
									})
								})
							}),
						)
					})
				}),
				// Cards
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.layoutCards(gtx, col)
				}),
				// Add card area
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutAddCard(gtx, col.ID, addBtn)
				}),
			)
		}),
	)
}

func (v *KanbanView) layoutCards(gtx layout.Context, col api.KanbanColumn) layout.Dimensions {
	cards := col.Cards
	count := len(cards)
	list := v.getCardList(col.ID)

	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.List(v.app.Theme.Material, list).Layout(gtx, count, func(gtx layout.Context, i int) layout.Dimensions {
			card := cards[i]
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutCard(gtx, card)
			})
		})
	})
}

func (v *KanbanView) layoutCard(gtx layout.Context, card api.KanbanCard) layout.Dimensions {
	btn := v.getCardBtn(card.ID)
	if btn.Clicked(gtx) {
		v.app.KanbanDlg.Open(card, v.ActiveBoard)
	}

	cardColor := parseHexColor(card.Color)
	hasColor := card.Color != ""

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := gtx.Dp(6)
		bg := color.NRGBA{R: 45, G: 48, B: 58, A: 255}
		if btn.Hovered() {
			bg = color.NRGBA{R: 55, G: 58, B: 68, A: 255}
		}

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y),
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					// Color strip on the left (if card has a color)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !hasColor {
							return layout.Dimensions{}
						}
						sz := image.Pt(gtx.Dp(4), gtx.Constraints.Min.Y)
						if sz.Y < gtx.Dp(40) {
							sz.Y = gtx.Dp(40)
						}
						paint.FillShape(gtx.Ops, cardColor, clip.RRect{
							Rect: image.Rect(0, 0, sz.X, sz.Y),
							NW: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz}
					}),
					// Obsah karty
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, card.Title)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								// Assigned user (if any)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if card.AssignedUser == nil {
										return layout.Dimensions{}
									}
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										name := card.AssignedUser.DisplayName
										if name == "" {
											name = card.AssignedUser.Username
										}
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconPerson, 12, ColorTextDim)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Caption(v.app.Theme.Material, name)
													lbl.Color = ColorTextDim
													return lbl.Layout(gtx)
												})
											}),
										)
									})
								}),
								// Due date (if set) — color coded: overdue=red, today=orange, future=gray
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if card.DueDate == nil {
										return layout.Dimensions{}
									}
									now := time.Now()
									today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
									due := time.Date(card.DueDate.Year(), card.DueDate.Month(), card.DueDate.Day(), 0, 0, 0, 0, now.Location())

									var dueText string
									var dueColor color.NRGBA
									if due.Before(today) {
										dueText = "Overdue!"
										dueColor = ColorDanger
									} else if due.Equal(today) {
										dueText = "Due: Today"
										dueColor = ColorWarning
									} else {
										dueText = fmt.Sprintf("Due: %s", card.DueDate.Format("Jan 2"))
										dueColor = ColorTextDim
									}
									return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconSchedule, 12, dueColor)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Caption(v.app.Theme.Material, dueText)
													lbl.Color = dueColor
													return lbl.Layout(gtx)
												})
											}),
										)
									})
								}),
							)
						})
					}),
				)
			}),
		)
	})
}

func (v *KanbanView) layoutAddCard(gtx layout.Context, colID string, addBtn *widget.Clickable) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if v.addCardCol == colID {
			ed := material.Editor(v.app.Theme.Material, &v.addCardEd, "Card title...")
			ed.Color = ColorText
			ed.HintColor = ColorTextDim
			return ed.Layout(gtx)
		}

		return addBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, IconAdd, 16, ColorTextDim)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "Add Card")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	})
}

// --- API operace ---

func (v *KanbanView) createBoard(name string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	go func() {
		board, err := conn.Client.CreateKanbanBoard(name, "")
		if err != nil {
			log.Printf("kanban: create board: %v", err)
			return
		}
		v.boards = append(v.boards, *board)
		v.LoadBoard(board.ID)
		v.app.Window.Invalidate()
	}()
}

func (v *KanbanView) deleteBoard(id string) {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	go func() {
		if err := conn.Client.DeleteKanbanBoard(id); err != nil {
			log.Printf("kanban: delete board: %v", err)
			return
		}
		for i, b := range v.boards {
			if b.ID == id {
				v.boards = append(v.boards[:i], v.boards[i+1:]...)
				break
			}
		}
		if v.ActiveBoard != nil && v.ActiveBoard.ID == id {
			v.ActiveBoard = nil
		}
		v.app.Window.Invalidate()
	}()
}

func (v *KanbanView) createColumn(title string) {
	conn := v.app.Conn()
	if conn == nil || v.ActiveBoard == nil {
		return
	}
	boardID := v.ActiveBoard.ID
	go func() {
		_, err := conn.Client.CreateKanbanColumn(boardID, title, "#555555")
		if err != nil {
			log.Printf("kanban: create column: %v", err)
			return
		}
		v.LoadBoard(boardID)
	}()
}

func (v *KanbanView) deleteColumn(colID string) {
	conn := v.app.Conn()
	if conn == nil || v.ActiveBoard == nil {
		return
	}
	boardID := v.ActiveBoard.ID
	go func() {
		if err := conn.Client.DeleteKanbanColumn(boardID, colID); err != nil {
			log.Printf("kanban: delete column: %v", err)
			return
		}
		v.LoadBoard(boardID)
	}()
}

func (v *KanbanView) createCard(colID, title string) {
	conn := v.app.Conn()
	if conn == nil || v.ActiveBoard == nil {
		return
	}
	boardID := v.ActiveBoard.ID
	go func() {
		_, err := conn.Client.CreateKanbanCard(boardID, colID, title)
		if err != nil {
			log.Printf("kanban: create card: %v", err)
			return
		}
		v.LoadBoard(boardID)
	}()
}

// HandleWSEvent processes kanban WS events and updates local state.
func (v *KanbanView) HandleWSEvent(evType string, payload json.RawMessage) {
	switch evType {
	case "kanban.board_create":
		var board api.KanbanBoard
		if json.Unmarshal(payload, &board) == nil {
			// Check for duplicate (createBoard already added it locally)
			dup := false
			for _, b := range v.boards {
				if b.ID == board.ID {
					dup = true
					break
				}
			}
			if !dup {
				v.boards = append(v.boards, board)
			}
		}

	case "kanban.board_delete":
		var data struct{ ID string `json:"id"` }
		if json.Unmarshal(payload, &data) == nil {
			for i, b := range v.boards {
				if b.ID == data.ID {
					v.boards = append(v.boards[:i], v.boards[i+1:]...)
					break
				}
			}
			if v.ActiveBoard != nil && v.ActiveBoard.ID == data.ID {
				v.ActiveBoard = nil
			}
		}

	case "kanban.column_create", "kanban.column_update", "kanban.column_delete",
		"kanban.card_create", "kanban.card_update", "kanban.card_move", "kanban.card_delete":
		// For column/card events — reload active board
		var data struct {
			BoardID string `json:"board_id"`
		}
		if json.Unmarshal(payload, &data) == nil {
			if v.ActiveBoard != nil && v.ActiveBoard.ID == data.BoardID {
				v.LoadBoard(data.BoardID)
			}
		}
	}
	v.app.Window.Invalidate()
}

// --- Helpers ---

func formatCount(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

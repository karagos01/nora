package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"strconv"
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

var eventColors = [8]string{
	"#3498db", // blue
	"#2ecc71", // green
	"#e74c3c", // red
	"#f39c12", // orange
	"#9b59b6", // purple
	"#1abc9c", // teal
	"#e67e22", // dark orange
	"#34495e", // gray
}

var reminderLabels = [4]string{"None", "15 min", "1 hour", "1 day"}
var reminderMinutes = [4]int{0, 15, 60, 1440}

var recurrenceLabels = [5]string{"None", "Daily", "Weekly", "Monthly", "Yearly"}
var recurrenceValues = [5]string{"", "daily", "weekly", "monthly", "yearly"}

type EventEditDialog struct {
	app     *App
	Visible bool
	Event   *api.Event // nil = create mode
	IsNew   bool

	titleEd    widget.Editor
	descEd     widget.Editor
	locationEd widget.Editor

	// Date — 7 day buttons (today + 6 days)
	dayBtns  [7]widget.Clickable
	dayIndex int

	// Time
	hourEd    widget.Editor
	minEd     widget.Editor
	endHourEd widget.Editor
	endMinEd  widget.Editor

	// All day
	allDayBtn widget.Clickable
	allDay    bool

	// Barvy
	colorBtns [8]widget.Clickable
	colorIdx  int

	// Reminder
	reminderBtns [4]widget.Clickable
	reminderIdx  int

	// Repeat (recurrence)
	repeatBtns [5]widget.Clickable
	repeatIdx  int

	saveBtn   widget.Clickable
	deleteBtn widget.Clickable
	closeBtn  widget.Clickable
}

func NewEventEditDialog(a *App) *EventEditDialog {
	d := &EventEditDialog{app: a}
	d.titleEd.SingleLine = true
	d.titleEd.Submit = true
	d.descEd.SingleLine = false
	d.locationEd.SingleLine = true
	d.locationEd.Submit = true
	d.hourEd.SingleLine = true
	d.hourEd.Submit = true
	d.minEd.SingleLine = true
	d.minEd.Submit = true
	d.endHourEd.SingleLine = true
	d.endHourEd.Submit = true
	d.endMinEd.SingleLine = true
	d.endMinEd.Submit = true
	return d
}

// ShowCreate opens the dialog for creating a new event.
func (d *EventEditDialog) ShowCreate() {
	d.IsNew = true
	d.Event = nil
	d.Visible = true
	d.titleEd.SetText("")
	d.descEd.SetText("")
	d.locationEd.SetText("")
	d.dayIndex = 0
	now := time.Now()
	// Default time: next full hour
	nextHour := now.Add(time.Hour).Truncate(time.Hour)
	d.hourEd.SetText(fmt.Sprintf("%d", nextHour.Hour()))
	d.minEd.SetText("00")
	d.endHourEd.SetText("")
	d.endMinEd.SetText("")
	d.allDay = false
	d.colorIdx = 0
	d.reminderIdx = 1 // 15 min default
	d.repeatIdx = 0   // None
}

// ShowEdit opens the dialog for editing an existing event.
func (d *EventEditDialog) ShowEdit(event *api.Event) {
	d.IsNew = false
	d.Event = event
	d.Visible = true
	d.titleEd.SetText(event.Title)
	d.descEd.SetText(event.Description)
	d.locationEd.SetText(event.Location)
	d.allDay = event.AllDay

	eLocal := event.StartsAt.Local()
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	eventDay := time.Date(eLocal.Year(), eLocal.Month(), eLocal.Day(), 0, 0, 0, 0, now.Location())
	dayDiff := int(eventDay.Sub(todayStart).Hours() / 24)
	if dayDiff < 0 {
		dayDiff = 0
	}
	if dayDiff > 6 {
		dayDiff = 6
	}
	d.dayIndex = dayDiff

	d.hourEd.SetText(fmt.Sprintf("%d", eLocal.Hour()))
	d.minEd.SetText(fmt.Sprintf("%02d", eLocal.Minute()))

	if event.EndsAt != nil {
		endLocal := event.EndsAt.Local()
		d.endHourEd.SetText(fmt.Sprintf("%d", endLocal.Hour()))
		d.endMinEd.SetText(fmt.Sprintf("%02d", endLocal.Minute()))
	} else {
		d.endHourEd.SetText("")
		d.endMinEd.SetText("")
	}

	// Find color
	d.colorIdx = 0
	for i, c := range eventColors {
		if c == event.Color {
			d.colorIdx = i
			break
		}
	}

	d.reminderIdx = 0 // None (current state unknown)

	// Find recurrence rule
	d.repeatIdx = 0
	for i, v := range recurrenceValues {
		if v == event.RecurrenceRule {
			d.repeatIdx = i
			break
		}
	}
}

func (d *EventEditDialog) Hide() {
	d.Visible = false
	d.Event = nil
}

func (d *EventEditDialog) Layout(gtx layout.Context) layout.Dimensions {
	th := d.app.Theme.Material

	// Handle clicks
	if d.closeBtn.Clicked(gtx) {
		d.Hide()
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	if d.saveBtn.Clicked(gtx) {
		d.doSave()
	}

	if d.deleteBtn.Clicked(gtx) && d.Event != nil {
		eventID := d.Event.ID
		d.Hide()
		go func() {
			conn := d.app.Conn()
			if conn == nil {
				return
			}
			if err := conn.Client.DeleteEvent(eventID); err != nil {
				log.Printf("calendar: delete event: %v", err)
			}
		}()
	}

	if d.allDayBtn.Clicked(gtx) {
		d.allDay = !d.allDay
	}

	for i := range d.dayBtns {
		if d.dayBtns[i].Clicked(gtx) {
			d.dayIndex = i
		}
	}
	for i := range d.colorBtns {
		if d.colorBtns[i].Clicked(gtx) {
			d.colorIdx = i
		}
	}
	for i := range d.reminderBtns {
		if d.reminderBtns[i].Clicked(gtx) {
			d.reminderIdx = i
		}
	}
	for i := range d.repeatBtns {
		if d.repeatBtns[i].Clicked(gtx) {
			d.repeatIdx = i
		}
	}

	// Overlay background (scrim)
	paint.FillShape(gtx.Ops, color.NRGBA{A: 180},
		clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Dialog box (max 460px, centered)
	dialogW := gtx.Dp(460)
	if dialogW > gtx.Constraints.Max.X-gtx.Dp(32) {
		dialogW = gtx.Constraints.Max.X - gtx.Dp(32)
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = dialogW
		gtx.Constraints.Max.X = dialogW
		maxH := gtx.Constraints.Max.Y - gtx.Dp(60)
		if maxH > gtx.Dp(620) {
			maxH = gtx.Dp(620)
		}
		gtx.Constraints.Max.Y = maxH

		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
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
						// Header
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									title := "New Event"
									if !d.IsNew {
										title = "Edit Event"
									}
									lbl := material.H6(th, title)
									lbl.Color = ColorText
									lbl.Font.Weight = font.Bold
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconClose, 24, ColorTextDim)
									})
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

						// Title
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Title")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutEditor(gtx, th, &d.titleEd, "Event title")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

						// Description
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Description")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutEditor(gtx, th, &d.descEd, "Optional description")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

						// Location
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Location")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutEditor(gtx, th, &d.locationEd, "Optional location")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

						// Day selector (7 buttons)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Date")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutDayPicker(gtx, th)
							})
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

						// Time + All day
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								// Start time
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(th, "Start time")
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if d.allDay {
												lbl := material.Caption(th, "All day")
												lbl.Color = ColorTextDim
												return lbl.Layout(gtx)
											}
											return d.layoutTimePair(gtx, th, &d.hourEd, &d.minEd)
										}),
									)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
								// End time
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if d.allDay {
										return layout.Dimensions{}
									}
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(th, "End time (optional)")
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return d.layoutTimePair(gtx, th, &d.endHourEd, &d.endMinEd)
										}),
									)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
								// All day toggle
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.allDayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										bg := ColorInput
										fg := ColorTextDim
										if d.allDay {
											bg = ColorAccent
											fg = ColorWhite
										}
										return layoutPillBtn(gtx, th, "All day", bg, fg)
									})
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

						// Barva
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Color")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutColorPicker(gtx)
							})
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

						// Reminder
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Reminder")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutReminderPicker(gtx, th)
							})
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

						// Repeat
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(th, "Repeat")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return d.layoutRepeatPicker(gtx, th)
							})
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),

						// Action buttons
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.saveBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										label := "Create"
										if !d.IsNew {
											label = "Save"
										}
										return layoutPillBtn(gtx, th, label, ColorAccent, ColorWhite)
									})
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if d.IsNew {
										return layout.Dimensions{}
									}
									return d.deleteBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layoutPillBtn(gtx, th, "Delete", ColorDanger, ColorWhite)
									})
								}),
							)
						}),
					)
				})
			},
		)
	})
}

func (d *EventEditDialog) layoutEditor(gtx layout.Context, th *material.Theme, ed *widget.Editor, hint string) layout.Dimensions {
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
				e := material.Editor(th, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}

func (d *EventEditDialog) layoutTimePair(gtx layout.Context, th *material.Theme, hourEd, minEd *widget.Editor) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(40)
			gtx.Constraints.Min.X = gtx.Dp(40)
			return d.layoutTimeBox(gtx, th, hourEd, "HH")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, ":")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(40)
			gtx.Constraints.Min.X = gtx.Dp(40)
			return d.layoutTimeBox(gtx, th, minEd, "MM")
		}),
	)
}

func (d *EventEditDialog) layoutTimeBox(gtx layout.Context, th *material.Theme, ed *widget.Editor, hint string) layout.Dimensions {
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
			return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(th, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}

func (d *EventEditDialog) layoutDayPicker(gtx layout.Context, th *material.Theme) layout.Dimensions {
	now := time.Now()
	var children []layout.FlexChild
	for i := 0; i < 7; i++ {
		i := i
		day := now.AddDate(0, 0, i)
		label := day.Format("Mon 2")
		if i == 0 {
			label = "Today"
		} else if i == 1 {
			label = "Tmrw"
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			bg := ColorInput
			fg := ColorTextDim
			if d.dayIndex == i {
				bg = ColorAccent
				fg = ColorWhite
			}
			return d.dayBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutPillBtn(gtx, th, label, bg, fg)
			})
		}))
		if i < 6 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
	}
	return layout.Flex{}.Layout(gtx, children...)
}

func (d *EventEditDialog) layoutColorPicker(gtx layout.Context) layout.Dimensions {
	sz := gtx.Dp(24)
	gap := gtx.Dp(6)
	var children []layout.FlexChild
	for i := 0; i < 8; i++ {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.colorBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					c := parseHexColor(eventColors[i])
					rr := sz / 2
					paint.FillShape(gtx.Ops, c, clip.RRect{
						Rect: image.Rect(0, 0, sz, sz),
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					// Selected — white border
					if d.colorIdx == i {
						borderW := gtx.Dp(2)
						paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 200}, clip.Stroke{
							Path: clip.RRect{
								Rect: image.Rect(0, 0, sz, sz),
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Path(gtx.Ops),
							Width: float32(borderW),
						}.Op())
					}
					return layout.Dimensions{Size: image.Pt(sz+gap, sz)}
				})
			})
		}))
	}
	return layout.Flex{}.Layout(gtx, children...)
}

func (d *EventEditDialog) layoutReminderPicker(gtx layout.Context, th *material.Theme) layout.Dimensions {
	var children []layout.FlexChild
	for i := 0; i < 4; i++ {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			bg := ColorInput
			fg := ColorTextDim
			if d.reminderIdx == i {
				bg = ColorAccent
				fg = ColorWhite
			}
			return d.reminderBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutPillBtn(gtx, th, reminderLabels[i], bg, fg)
			})
		}))
		if i < 3 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
	}
	return layout.Flex{}.Layout(gtx, children...)
}

func (d *EventEditDialog) layoutRepeatPicker(gtx layout.Context, th *material.Theme) layout.Dimensions {
	var children []layout.FlexChild
	for i := 0; i < 5; i++ {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			bg := ColorInput
			fg := ColorTextDim
			if d.repeatIdx == i {
				bg = ColorAccent
				fg = ColorWhite
			}
			return d.repeatBtns[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutPillBtn(gtx, th, recurrenceLabels[i], bg, fg)
			})
		}))
		if i < 4 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
	}
	return layout.Flex{}.Layout(gtx, children...)
}

func (d *EventEditDialog) doSave() {
	title := strings.TrimSpace(d.titleEd.Text())
	if title == "" {
		return
	}

	desc := d.descEd.Text()
	location := strings.TrimSpace(d.locationEd.Text())
	clr := eventColors[d.colorIdx]

	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day = day.AddDate(0, 0, d.dayIndex)

	h, _ := strconv.Atoi(strings.TrimSpace(d.hourEd.Text()))
	m, _ := strconv.Atoi(strings.TrimSpace(d.minEd.Text()))
	if h < 0 {
		h = 0
	}
	if h > 23 {
		h = 23
	}
	if m < 0 {
		m = 0
	}
	if m > 59 {
		m = 59
	}

	startsAt := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, now.Location())
	startsStr := startsAt.UTC().Format(time.RFC3339)

	var endsStr *string
	if !d.allDay {
		eh, _ := strconv.Atoi(strings.TrimSpace(d.endHourEd.Text()))
		em, _ := strconv.Atoi(strings.TrimSpace(d.endMinEd.Text()))
		if d.endHourEd.Text() != "" || d.endMinEd.Text() != "" {
			if eh < 0 {
				eh = 0
			}
			if eh > 23 {
				eh = 23
			}
			if em < 0 {
				em = 0
			}
			if em > 59 {
				em = 59
			}
			endsAt := time.Date(day.Year(), day.Month(), day.Day(), eh, em, 0, 0, now.Location())
			s := endsAt.UTC().Format(time.RFC3339)
			endsStr = &s
		}
	}

	reminderMin := reminderMinutes[d.reminderIdx]
	recRule := recurrenceValues[d.repeatIdx]

	eventRef := d.Event
	isNew := d.IsNew
	d.Hide()

	go func() {
		conn := d.app.Conn()
		if conn == nil {
			return
		}

		if isNew {
			evt, err := conn.Client.CreateEvent(title, desc, location, clr, startsStr, endsStr, d.allDay, recRule)
			if err != nil {
				log.Printf("calendar: create event: %v", err)
				return
			}
			// Set reminder
			if reminderMin > 0 {
				conn.Client.SetEventReminder(evt.ID, reminderMin)
			}
		} else if eventRef != nil {
			updates := map[string]interface{}{
				"title":           title,
				"description":     desc,
				"location":        location,
				"color":           clr,
				"starts_at":       startsStr,
				"all_day":         d.allDay,
				"recurrence_rule": recRule,
			}
			if endsStr != nil {
				updates["ends_at"] = *endsStr
			} else {
				updates["clear_ends_at"] = true
			}
			if _, err := conn.Client.UpdateEvent(eventRef.ID, updates); err != nil {
				log.Printf("calendar: update event: %v", err)
				return
			}
			// Aktualizovat reminder
			if reminderMin > 0 {
				conn.Client.SetEventReminder(eventRef.ID, reminderMin)
			} else {
				conn.Client.RemoveEventReminder(eventRef.ID)
			}
		}
	}()
}

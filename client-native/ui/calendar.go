package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"sort"
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

type CalendarView struct {
	app *App

	// Eventy
	events    []api.Event
	eventBtns []widget.Clickable
	eventList widget.List

	// Měsíční navigace
	viewMonth time.Time
	prevBtn   widget.Clickable
	nextBtn   widget.Clickable

	// Vybraný den (0 = vše)
	selectedDay int
	dayBtns     [42]widget.Clickable // max 6 řádků * 7 dní

	// New event
	newBtn widget.Clickable

	// Back
	backBtn widget.Clickable

	loading bool
}

func NewCalendarView(a *App) *CalendarView {
	v := &CalendarView{
		app:       a,
		viewMonth: time.Now(),
	}
	v.eventList.Axis = layout.Vertical
	return v
}

// LoadEvents načte eventy pro aktuální měsíc (+/- 1 měsíc buffer).
func (v *CalendarView) LoadEvents() {
	conn := v.app.Conn()
	if conn == nil {
		return
	}
	v.loading = true
	from := time.Date(v.viewMonth.Year(), v.viewMonth.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	to := time.Date(v.viewMonth.Year(), v.viewMonth.Month()+1, 0, 23, 59, 59, 0, time.UTC).AddDate(0, 1, 0)
	fromStr := from.Format(time.RFC3339)
	toStr := to.Format(time.RFC3339)
	go func() {
		events, err := conn.Client.ListEvents(fromStr, toStr)
		if err != nil {
			log.Printf("calendar: list events: %v", err)
			v.loading = false
			v.app.Window.Invalidate()
			return
		}
		if events == nil {
			events = []api.Event{}
		}
		sort.Slice(events, func(i, j int) bool {
			return events[i].StartsAt.Before(events[j].StartsAt)
		})
		v.events = events
		if len(v.eventBtns) < len(events) {
			v.eventBtns = make([]widget.Clickable, len(events)+5)
		}
		v.loading = false
		v.app.Window.Invalidate()
	}()
}

// HandleWSEvent zpracuje WS eventy pro kalendář.
func (v *CalendarView) HandleWSEvent(evType string, payload json.RawMessage) {
	switch evType {
	case "calendar.event_create":
		var event api.Event
		if json.Unmarshal(payload, &event) == nil {
			v.events = append(v.events, event)
			sort.Slice(v.events, func(i, j int) bool {
				return v.events[i].StartsAt.Before(v.events[j].StartsAt)
			})
			if len(v.eventBtns) < len(v.events) {
				v.eventBtns = make([]widget.Clickable, len(v.events)+5)
			}
		}
	case "calendar.event_update":
		var event api.Event
		if json.Unmarshal(payload, &event) == nil {
			for i := range v.events {
				if v.events[i].ID == event.ID {
					v.events[i] = event
					break
				}
			}
			sort.Slice(v.events, func(i, j int) bool {
				return v.events[i].StartsAt.Before(v.events[j].StartsAt)
			})
		}
	case "calendar.event_delete":
		var data struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(payload, &data) == nil {
			for i := range v.events {
				if v.events[i].ID == data.ID {
					v.events = append(v.events[:i], v.events[i+1:]...)
					break
				}
			}
		}
	}
}

// LayoutSidebar renderuje mini kalendář v levém panelu.
func (v *CalendarView) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	th := v.app.Theme.Material
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Zpracovat navigaci
	if v.prevBtn.Clicked(gtx) {
		v.viewMonth = v.viewMonth.AddDate(0, -1, 0)
		v.selectedDay = 0
		v.LoadEvents()
	}
	if v.nextBtn.Clicked(gtx) {
		v.viewMonth = v.viewMonth.AddDate(0, 1, 0)
		v.selectedDay = 0
		v.LoadEvents()
	}

	// Back
	if v.backBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewChannels
		v.app.mu.Unlock()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Back + header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconBack, 20, ColorTextDim)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(th, "Back")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
					)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.H6(th, "Events")
				lbl.Color = ColorText
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			})
		}),

		// Měsíční navigace
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.prevBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconBack, 20, ColorTextDim)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(th, v.viewMonth.Format("January 2006"))
						lbl.Color = ColorText
						lbl.Alignment = 1 // center
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.nextBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, IconChevronRight, 20, ColorTextDim)
						})
					}),
				)
			})
		}),

		// Mini kalendář grid
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutMiniCalendar(gtx, th)
			})
		}),

		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				size := image.Pt(gtx.Constraints.Max.X-gtx.Dp(24), gtx.Dp(1))
				paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
				return layout.Dimensions{Size: size}
			})
		}),

		// Upcoming events (mini list)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th, "Upcoming")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutUpcoming(gtx, th)
			})
		}),
	)
}

// layoutMiniCalendar renderuje měsíční grid (Mo Tu We Th Fr Sa Su).
func (v *CalendarView) layoutMiniCalendar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	now := time.Now()
	today := now.Day()
	todayMonth := now.Month()
	todayYear := now.Year()

	year := v.viewMonth.Year()
	month := v.viewMonth.Month()
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	// Pondělí = 0, Neděle = 6
	weekday := int(first.Weekday())
	if weekday == 0 {
		weekday = 6
	} else {
		weekday--
	}
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	// Zjistit které dny mají eventy
	eventDays := make(map[int]bool)
	for _, e := range v.events {
		eLocal := e.StartsAt.Local()
		if eLocal.Month() == month && eLocal.Year() == year {
			eventDays[eLocal.Day()] = true
		}
	}

	cellW := (gtx.Constraints.Max.X) / 7
	cellH := gtx.Dp(24)

	var children []layout.FlexChild

	// Header: Mo Tu We Th Fr Sa Su
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		dayNames := [7]string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
		return layout.Flex{}.Layout(gtx, func() []layout.FlexChild {
			var cells []layout.FlexChild
			for _, name := range dayNames {
				name := name
				cells = append(cells, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = cellW
					gtx.Constraints.Max.X = cellW
					lbl := material.Caption(th, name)
					lbl.Color = ColorTextDim
					lbl.Alignment = 1
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return lbl.Layout(gtx)
					})
				}))
			}
			return cells
		}()...)
	}))

	// Dny v gridu (max 6 řádků)
	day := 1
	for row := 0; row < 6; row++ {
		if day > daysInMonth {
			break
		}
		row := row
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var cells []layout.FlexChild
			for col := 0; col < 7; col++ {
				cellIdx := row*7 + col
				if cellIdx < weekday || day > daysInMonth {
					cells = append(cells, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(cellW, cellH)}
					}))
					continue
				}
				d := day
				day++
				cells = append(cells, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Pt(cellW, cellH)
					gtx.Constraints.Max = gtx.Constraints.Min

					isToday := d == today && month == todayMonth && year == todayYear
					isSelected := v.selectedDay == d
					hasEvent := eventDays[d]

					btnIdx := cellIdx
					if btnIdx < len(v.dayBtns) {
						if v.dayBtns[btnIdx].Clicked(gtx) {
							if v.selectedDay == d {
								v.selectedDay = 0 // deselect
							} else {
								v.selectedDay = d
							}
						}

						return v.dayBtns[btnIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutDayCell(gtx, th, d, isToday, isSelected, hasEvent, cellW, cellH)
						})
					}
					return v.layoutDayCell(gtx, th, d, isToday, isSelected, hasEvent, cellW, cellH)
				}))
			}
			return layout.Flex{}.Layout(gtx, cells...)
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (v *CalendarView) layoutDayCell(gtx layout.Context, th *material.Theme, day int, isToday, isSelected, hasEvent bool, w, h int) layout.Dimensions {
	// Pozadí
	if isSelected {
		rr := h / 2
		paint.FillShape(gtx.Ops, ColorAccent, clip.RRect{
			Rect: image.Rect(0, 0, w, h),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
	} else if isToday {
		rr := h / 2
		paint.FillShape(gtx.Ops, color.NRGBA{R: 60, G: 60, B: 70, A: 255}, clip.RRect{
			Rect: image.Rect(0, 0, w, h),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
	}

	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(th, fmt.Sprintf("%d", day))
			if hasEvent {
				lbl.Font.Weight = font.Bold
			}
			if isSelected || isToday {
				lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				lbl.Color = ColorText
			}
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return lbl.Layout(gtx)
			})
		}),
		// Event indikátor (malá tečka dole)
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if !hasEvent || isSelected {
				return layout.Dimensions{Size: image.Pt(w, h)}
			}
			dotSize := gtx.Dp(4)
			x := (w - dotSize) / 2
			y := h - dotSize - gtx.Dp(1)
			defer clip.RRect{
				Rect: image.Rect(x, y, x+dotSize, y+dotSize),
				NE:   dotSize / 2, NW: dotSize / 2, SE: dotSize / 2, SW: dotSize / 2,
			}.Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, ColorAccent)
			return layout.Dimensions{Size: image.Pt(w, h)}
		}),
	)
}

// layoutUpcoming renderuje nejbližší eventy v sidebaru.
func (v *CalendarView) layoutUpcoming(gtx layout.Context, th *material.Theme) layout.Dimensions {
	now := time.Now()
	var upcoming []api.Event
	for _, e := range v.events {
		if e.StartsAt.After(now.Add(-24 * time.Hour)) {
			upcoming = append(upcoming, e)
		}
		if len(upcoming) >= 5 {
			break
		}
	}

	if len(upcoming) == 0 {
		lbl := material.Caption(th, "No upcoming events")
		lbl.Color = ColorTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return lbl.Layout(gtx)
		})
	}

	var children []layout.FlexChild
	for _, e := range upcoming {
		e := e
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					// Barevná tečka
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(8)
						c := parseHexColor(e.Color)
						paint.FillShape(gtx.Ops, c, clip.RRect{
							Rect: image.Rect(0, 0, sz, sz),
							NE:   sz / 2, NW: sz / 2, SE: sz / 2, SW: sz / 2,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: image.Pt(sz, sz)}
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								title := e.Title
								if len(title) > 20 {
									title = title[:20] + "..."
								}
								lbl := material.Caption(th, title)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								timeStr := formatEventTime(e, now)
								lbl := material.Caption(th, timeStr)
								lbl.Color = ColorTextDim
								lbl.TextSize = unit.Sp(10)
								return lbl.Layout(gtx)
							}),
						)
					}),
				)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// LayoutMain renderuje hlavní oblast s chronologickým seznamem eventů.
func (v *CalendarView) LayoutMain(gtx layout.Context) layout.Dimensions {
	th := v.app.Theme.Material
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// New event button
	if v.newBtn.Clicked(gtx) {
		v.app.CalendarDlg.ShowCreate()
	}

	// Filtrovat eventy podle vybraného dne
	filtered := v.getFilteredEvents()

	// Kliknutí na event
	for i := range filtered {
		if i < len(v.eventBtns) && v.eventBtns[i].Clicked(gtx) {
			evt := filtered[i]
			v.app.CalendarDlg.ShowEdit(&evt)
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						title := "Events"
						if v.selectedDay > 0 {
							title = fmt.Sprintf("Events — %s %d", v.viewMonth.Format("January"), v.selectedDay)
						}
						lbl := material.H6(th, title)
						lbl.Color = ColorText
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.newBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutPillBtn(gtx, th, "+ New Event", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
						})
					}),
				)
			})
		}),

		// Event list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if v.loading {
				return layoutCentered(gtx, v.app.Theme, "Loading...", ColorTextDim)
			}
			if len(filtered) == 0 {
				msg := "No events"
				if v.selectedDay > 0 {
					msg = "No events on this day"
				}
				return layoutCentered(gtx, v.app.Theme, msg, ColorTextDim)
			}

			if len(v.eventBtns) < len(filtered) {
				v.eventBtns = make([]widget.Clickable, len(filtered)+5)
			}

			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lst := material.List(th, &v.eventList)
				return lst.Layout(gtx, len(filtered), func(gtx layout.Context, idx int) layout.Dimensions {
					return v.layoutEventCard(gtx, th, filtered[idx], idx)
				})
			})
		}),
	)
}

func (v *CalendarView) getFilteredEvents() []api.Event {
	if v.selectedDay == 0 {
		// Vrátit eventy z aktuálního měsíce
		var filtered []api.Event
		for _, e := range v.events {
			eLocal := e.StartsAt.Local()
			if eLocal.Month() == v.viewMonth.Month() && eLocal.Year() == v.viewMonth.Year() {
				filtered = append(filtered, e)
			}
		}
		return filtered
	}
	// Filtrovat na konkrétní den
	var filtered []api.Event
	for _, e := range v.events {
		eLocal := e.StartsAt.Local()
		if eLocal.Day() == v.selectedDay && eLocal.Month() == v.viewMonth.Month() && eLocal.Year() == v.viewMonth.Year() {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// layoutEventCard renderuje jednu event kartu.
func (v *CalendarView) layoutEventCard(gtx layout.Context, th *material.Theme, event api.Event, idx int) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if idx >= len(v.eventBtns) {
			return layout.Dimensions{}
		}
		return v.eventBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(8)
					paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							// Barevný pásek vlevo
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								w := gtx.Dp(4)
								h := gtx.Dp(40)
								c := parseHexColor(event.Color)
								rr := w / 2
								paint.FillShape(gtx.Ops, c, clip.RRect{
									Rect: image.Rect(0, 0, w, h),
									NE:   rr, NW: rr, SE: rr, SW: rr,
								}.Op(gtx.Ops))
								return layout.Dimensions{Size: image.Pt(w, h)}
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							// Obsah
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										// Název + repeat ikona
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Body1(th, event.Title)
												lbl.Color = ColorText
												lbl.Font.Weight = font.Bold
												return lbl.Layout(gtx)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												if event.RecurrenceRule == "" {
													return layout.Dimensions{}
												}
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layoutIcon(gtx, IconRepeat, 14, ColorTextDim)
												})
											}),
										)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										timeStr := formatEventTimeRange(event)
										if event.RecurrenceRule != "" {
											timeStr += " (" + event.RecurrenceRule + ")"
										}
										lbl := material.Caption(th, timeStr)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if event.Location == "" {
											return layout.Dimensions{}
										}
										lbl := material.Caption(th, event.Location)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
								)
							}),
							// Datum (pravá strana)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								dayStr := event.StartsAt.Local().Format("Jan 2")
								lbl := material.Caption(th, dayStr)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
						)
					})
				},
			)
		})
	})
}

func formatEventTime(e api.Event, now time.Time) string {
	eLocal := e.StartsAt.Local()
	if e.AllDay {
		if eLocal.Day() == now.Day() && eLocal.Month() == now.Month() {
			return "All day"
		}
		return eLocal.Format("Jan 2") + " all day"
	}
	if eLocal.Day() == now.Day() && eLocal.Month() == now.Month() && eLocal.Year() == now.Year() {
		return eLocal.Format("15:04")
	}
	if eLocal.Day() == now.Day()+1 && eLocal.Month() == now.Month() {
		return "tomorrow " + eLocal.Format("15:04")
	}
	return eLocal.Format("Jan 2 15:04")
}

func formatEventTimeRange(e api.Event) string {
	eLocal := e.StartsAt.Local()
	if e.AllDay {
		return "All day"
	}
	start := eLocal.Format("15:04")
	if e.EndsAt != nil {
		end := e.EndsAt.Local().Format("15:04")
		return start + " - " + end
	}
	return start
}

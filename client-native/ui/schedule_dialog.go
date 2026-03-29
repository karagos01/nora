package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

type ScheduleBuilder struct {
	app *App

	// Preset buttons
	presetBtns [4]widget.Clickable

	// Custom time
	dayIndex   int // 0=today, 1=tomorrow, 2=day after
	dayBtns    [3]widget.Clickable
	hourEditor widget.Editor
	minEditor  widget.Editor

	// Resulting time
	selectedTime time.Time
	hasSelection bool

	// Action buttons
	scheduleBtn widget.Clickable
	cancelBtn   widget.Clickable

	// List of scheduled messages
	scheduled    []api.ScheduledMessage
	loaded       bool
	loading      bool
	cancelBtns   []widget.Clickable
	listScroll   widget.List
}

var presetLabels = [4]string{"In 30 min", "In 1 hour", "In 3 hours", "Tomorrow 9:00"}

func NewScheduleBuilder(app *App) *ScheduleBuilder {
	sb := &ScheduleBuilder{
		app: app,
	}
	sb.hourEditor.SingleLine = true
	sb.hourEditor.Submit = true
	sb.minEditor.SingleLine = true
	sb.minEditor.Submit = true
	sb.listScroll.Axis = layout.Vertical
	return sb
}

func (sb *ScheduleBuilder) Reset() {
	sb.hasSelection = false
	sb.selectedTime = time.Time{}
	sb.dayIndex = 0
	sb.hourEditor.SetText("")
	sb.minEditor.SetText("")
	sb.loaded = false
	sb.loading = false
}

func (sb *ScheduleBuilder) LoadScheduled() {
	if sb.loading {
		return
	}
	sb.loading = true
	go func() {
		conn := sb.app.Conn()
		if conn == nil {
			sb.loading = false
			return
		}
		msgs, err := conn.Client.ListScheduledMessages()
		if err != nil {
			log.Printf("ListScheduledMessages error: %v", err)
			sb.loading = false
			sb.loaded = true
			return
		}
		sb.scheduled = msgs
		if len(sb.cancelBtns) < len(msgs) {
			sb.cancelBtns = make([]widget.Clickable, len(msgs)+5)
		}
		sb.loading = false
		sb.loaded = true
		sb.app.Window.Invalidate()
	}()
}

func (sb *ScheduleBuilder) getSelectedTime() time.Time {
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day = day.AddDate(0, 0, sb.dayIndex)

	h, _ := strconv.Atoi(strings.TrimSpace(sb.hourEditor.Text()))
	m, _ := strconv.Atoi(strings.TrimSpace(sb.minEditor.Text()))
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

	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, now.Location())
}

func (sb *ScheduleBuilder) Layout(gtx layout.Context) layout.Dimensions {
	th := sb.app.Theme.Material

	// Handle preset clicks
	now := time.Now()
	for i := range sb.presetBtns {
		if sb.presetBtns[i].Clicked(gtx) {
			switch i {
			case 0: // 30 min
				sb.selectedTime = now.Add(30 * time.Minute)
			case 1: // 1 hour
				sb.selectedTime = now.Add(1 * time.Hour)
			case 2: // 3 hours
				sb.selectedTime = now.Add(3 * time.Hour)
			case 3: // tomorrow 9:00
				tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
				sb.selectedTime = tomorrow
			}
			sb.hasSelection = true
			sb.hourEditor.SetText(fmt.Sprintf("%d", sb.selectedTime.Hour()))
			sb.minEditor.SetText(fmt.Sprintf("%02d", sb.selectedTime.Minute()))
			// Set dayIndex
			selDay := time.Date(sb.selectedTime.Year(), sb.selectedTime.Month(), sb.selectedTime.Day(), 0, 0, 0, 0, now.Location())
			todayDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			sb.dayIndex = int(selDay.Sub(todayDay).Hours() / 24)
			if sb.dayIndex < 0 {
				sb.dayIndex = 0
			}
			if sb.dayIndex > 2 {
				sb.dayIndex = 2
			}
		}
	}

	// Handle day clicks
	for i := range sb.dayBtns {
		if sb.dayBtns[i].Clicked(gtx) {
			sb.dayIndex = i
			sb.selectedTime = sb.getSelectedTime()
			sb.hasSelection = sb.hourEditor.Text() != "" || sb.minEditor.Text() != ""
		}
	}

	// Update selected time from editors
	if sb.hourEditor.Text() != "" || sb.minEditor.Text() != "" {
		sb.selectedTime = sb.getSelectedTime()
		sb.hasSelection = true
	}

	// Handle cancel scheduled message clicks
	for i := range sb.scheduled {
		if i < len(sb.cancelBtns) && sb.cancelBtns[i].Clicked(gtx) {
			msgID := sb.scheduled[i].ID
			go func() {
				conn := sb.app.Conn()
				if conn == nil {
					return
				}
				if err := conn.Client.DeleteScheduledMessage(msgID); err != nil {
					log.Printf("DeleteScheduledMessage error: %v", err)
				}
				sb.loaded = false
				sb.LoadScheduled()
			}()
		}
	}

	// Lazy load
	if !sb.loaded {
		sb.LoadScheduled()
	}

	panelBg := ColorCard

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			paint.FillShape(gtx.Ops, panelBg, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					// Header
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(th, "Schedule Message")
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Preset buttons
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutPresetBtn(gtx, th, 0)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutPresetBtn(gtx, th, 1)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutPresetBtn(gtx, th, 2)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutPresetBtn(gtx, th, 3)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Custom time: day selector + HH:MM
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						dayLabels := sb.getDayLabels()
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutDayBtn(gtx, th, 0, dayLabels[0])
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutDayBtn(gtx, th, 1, dayLabels[1])
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.layoutDayBtn(gtx, th, 2, dayLabels[2])
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							// HH
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Max.X = gtx.Dp(40)
								gtx.Constraints.Min.X = gtx.Dp(40)
								return sb.layoutTimeEditor(gtx, th, &sb.hourEditor, "HH")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body1(th, ":")
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
							// MM
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Max.X = gtx.Dp(40)
								gtx.Constraints.Min.X = gtx.Dp(40)
								return sb.layoutTimeEditor(gtx, th, &sb.minEditor, "MM")
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Text preview
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !sb.hasSelection {
							return layout.Dimensions{}
						}
						txt := fmt.Sprintf("Will be sent: %s", sb.selectedTime.Format("Mon Jan 2, 15:04"))
						lbl := material.Caption(th, txt)
						lbl.Color = ColorAccent
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Schedule / Cancel
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.scheduleBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutPillBtn(gtx, th, "Schedule", ColorAccent, ColorWhite)
								})
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return sb.cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutPillBtn(gtx, th, "Cancel", ColorInput, ColorTextDim)
								})
							}),
						)
					}),

					// Scheduled messages list
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if len(sb.scheduled) == 0 {
							return layout.Dimensions{}
						}
						return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(th, fmt.Sprintf("Scheduled (%d)", len(sb.scheduled)))
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									maxH := gtx.Dp(150)
									if gtx.Constraints.Max.Y > maxH {
										gtx.Constraints.Max.Y = maxH
									}
									lst := material.List(th, &sb.listScroll)
									return lst.Layout(gtx, len(sb.scheduled), func(gtx layout.Context, idx int) layout.Dimensions {
										return sb.layoutScheduledItem(gtx, th, idx)
									})
								}),
							)
						})
					}),
				)
			})
		},
	)
}

func (sb *ScheduleBuilder) getDayLabels() [3]string {
	now := time.Now()
	return [3]string{
		"Today",
		now.AddDate(0, 0, 1).Format("Mon"),
		now.AddDate(0, 0, 2).Format("Mon"),
	}
}

func (sb *ScheduleBuilder) layoutPresetBtn(gtx layout.Context, th *material.Theme, idx int) layout.Dimensions {
	return sb.presetBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layoutPillBtn(gtx, th, presetLabels[idx], ColorInput, ColorText)
	})
}

func (sb *ScheduleBuilder) layoutDayBtn(gtx layout.Context, th *material.Theme, idx int, label string) layout.Dimensions {
	bg := ColorInput
	fg := ColorTextDim
	if sb.dayIndex == idx {
		bg = ColorAccent
		fg = ColorWhite
	}
	return sb.dayBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layoutPillBtn(gtx, th, label, bg, fg)
	})
}

func (sb *ScheduleBuilder) layoutTimeEditor(gtx layout.Context, th *material.Theme, ed *widget.Editor, hint string) layout.Dimensions {
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

func (sb *ScheduleBuilder) layoutScheduledItem(gtx layout.Context, th *material.Theme, idx int) layout.Dimensions {
	msg := sb.scheduled[idx]
	content := msg.Content
	if len(content) > 40 {
		content = content[:40] + "..."
	}
	channelName := msg.ChannelName
	if channelName == "" {
		channelName = "unknown"
	}

	return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				bg := color.NRGBA{R: 40, G: 40, B: 45, A: 255}
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							txt := fmt.Sprintf("#%s: \"%s\" — %s", channelName, content, msg.ScheduledAt.Local().Format("Mon 15:04"))
							lbl := material.Caption(th, txt)
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if idx >= len(sb.cancelBtns) {
								return layout.Dimensions{}
							}
							return sb.cancelBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutPillBtn(gtx, th, "Cancel", color.NRGBA{R: 180, G: 60, B: 60, A: 255}, ColorWhite)
							})
						}),
					)
				})
			},
		)
	})
}

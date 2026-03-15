package ui

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type PollBuilder struct {
	app            *App
	questionEditor widget.Editor
	optionEditors  [10]widget.Editor
	optionCount    int
	pollType       int // 0=simple, 1=multi, 2=anonymous
	typeBtns       [3]widget.Clickable
	addOptionBtn   widget.Clickable
	removeOptionBtns [10]widget.Clickable
	createBtn      widget.Clickable
	cancelBtn      widget.Clickable
	// Expiration (hours, 0 = no expiration)
	expiresEditor  widget.Editor
	expiresBtns    [4]widget.Clickable // 1h, 6h, 24h, none
}

func NewPollBuilder(app *App) *PollBuilder {
	pb := &PollBuilder{
		app:         app,
		optionCount: 2,
	}
	pb.questionEditor.SingleLine = true
	pb.questionEditor.Submit = true
	pb.expiresEditor.SingleLine = true
	pb.expiresEditor.Submit = true
	for i := range pb.optionEditors {
		pb.optionEditors[i].SingleLine = true
		pb.optionEditors[i].Submit = true
	}
	return pb
}

func (pb *PollBuilder) Reset() {
	pb.questionEditor.SetText("")
	for i := range pb.optionEditors {
		pb.optionEditors[i].SetText("")
	}
	pb.optionCount = 2
	pb.pollType = 0
	pb.expiresEditor.SetText("")
}

var expiresLabels = [4]string{"1h", "6h", "24h", "None"}
var expiresValues = [4]string{"1", "6", "24", ""}

var pollTypeLabels = [3]string{"Single choice", "Multiple choice", "Anonymous"}
var pollTypeValues = [3]string{"simple", "multi", "anonymous"}

func (pb *PollBuilder) Layout(gtx layout.Context) layout.Dimensions {
	th := pb.app.Theme.Material

	// Handle clicks
	for i := range pb.typeBtns {
		if pb.typeBtns[i].Clicked(gtx) {
			pb.pollType = i
		}
	}
	if pb.addOptionBtn.Clicked(gtx) && pb.optionCount < 10 {
		pb.optionCount++
	}
	for i := range pb.expiresBtns {
		if pb.expiresBtns[i].Clicked(gtx) {
			pb.expiresEditor.SetText(expiresValues[i])
		}
	}
	for i := 0; i < pb.optionCount; i++ {
		if pb.removeOptionBtns[i].Clicked(gtx) && pb.optionCount > 2 {
			// Shift editors down
			for j := i; j < pb.optionCount-1; j++ {
				pb.optionEditors[j].SetText(pb.optionEditors[j+1].Text())
			}
			pb.optionEditors[pb.optionCount-1].SetText("")
			pb.optionCount--
		}
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
						lbl := material.Body1(th, "Create Poll")
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Question editor
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return pb.layoutEditorField(gtx, th, &pb.questionEditor, "Question")
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Type selector
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutTypeBtn(gtx, th, 0)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutTypeBtn(gtx, th, 1)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutTypeBtn(gtx, th, 2)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Options
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						var children []layout.FlexChild
						for i := 0; i < pb.optionCount; i++ {
							idx := i
							children = append(children,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return pb.layoutEditorField(gtx, th, &pb.optionEditors[idx], "Option "+string(rune('1'+idx)))
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if pb.optionCount <= 2 {
												return layout.Dimensions{}
											}
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return pb.removeOptionBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Body2(th, "x")
													lbl.Color = ColorTextDim
													return lbl.Layout(gtx)
												})
											})
										}),
									)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
							)
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
					}),

					// Add option button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if pb.optionCount >= 10 {
							return layout.Dimensions{}
						}
						return pb.addOptionBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(th, "+ Add option")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Expiration selector
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(th, "Expires:")
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutExpiresBtn(gtx, th, 0)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutExpiresBtn(gtx, th, 1)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutExpiresBtn(gtx, th, 2)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.layoutExpiresBtn(gtx, th, 3)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Create / Cancel buttons
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.createBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutPillBtn(gtx, th, "Create", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
								})
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return pb.cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutPillBtn(gtx, th, "Cancel", ColorInput, ColorTextDim)
								})
							}),
						)
					}),
				)
			})
		},
	)
}

func (pb *PollBuilder) layoutEditorField(gtx layout.Context, th *material.Theme, ed *widget.Editor, hint string) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE:   rr, SW: rr,
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

func (pb *PollBuilder) layoutTypeBtn(gtx layout.Context, th *material.Theme, idx int) layout.Dimensions {
	bg := ColorInput
	txtCol := ColorTextDim
	if pb.pollType == idx {
		bg = ColorAccent
		txtCol = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return pb.typeBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layoutPillBtn(gtx, th, pollTypeLabels[idx], bg, txtCol)
	})
}

func layoutPillBtn(gtx layout.Context, th *material.Theme, text string, bg, fg color.NRGBA) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(12)
			paint.FillShape(gtx.Ops, bg, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th, text)
				lbl.Color = fg
				return lbl.Layout(gtx)
			})
		},
	)
}

// GetPollType returns the string type of the poll.
func (pb *PollBuilder) GetPollType() string {
	return pollTypeValues[pb.pollType]
}

// GetQuestion returns the question text.
func (pb *PollBuilder) GetQuestion() string {
	return pb.questionEditor.Text()
}

// GetOptions returns the labels of all options.
func (pb *PollBuilder) GetOptions() []string {
	opts := make([]string, 0, pb.optionCount)
	for i := 0; i < pb.optionCount; i++ {
		opts = append(opts, pb.optionEditors[i].Text())
	}
	return opts
}

// IsValid returns true if the poll is ready to be sent.
func (pb *PollBuilder) IsValid() bool {
	if pb.questionEditor.Text() == "" {
		return false
	}
	for i := 0; i < pb.optionCount; i++ {
		if pb.optionEditors[i].Text() == "" {
			return false
		}
	}
	return true
}

// GetExpiresAt returns the poll expiration time, or nil if no expiration.
func (pb *PollBuilder) GetExpiresAt() *time.Time {
	txt := pb.expiresEditor.Text()
	if txt == "" {
		return nil
	}
	hours, err := strconv.Atoi(txt)
	if err != nil || hours <= 0 {
		return nil
	}
	t := time.Now().UTC().Add(time.Duration(hours) * time.Hour)
	return &t
}

func (pb *PollBuilder) layoutExpiresBtn(gtx layout.Context, th *material.Theme, idx int) layout.Dimensions {
	current := pb.expiresEditor.Text()
	active := current == expiresValues[idx]
	bg := ColorInput
	txtCol := ColorTextDim
	if active {
		bg = ColorAccent
		txtCol = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return pb.expiresBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layoutPillBtn(gtx, th, expiresLabels[idx], bg, txtCol)
	})
}

// FormatExpiration returns a human-readable description of the expiration.
func FormatExpiration(expiresAt *time.Time) string {
	if expiresAt == nil {
		return ""
	}
	remaining := time.Until(*expiresAt)
	if remaining <= 0 {
		return "Expired"
	}
	if remaining < time.Hour {
		return fmt.Sprintf("%dm left", int(remaining.Minutes()))
	}
	if remaining < 24*time.Hour {
		return fmt.Sprintf("%dh left", int(remaining.Hours()))
	}
	return fmt.Sprintf("%dd left", int(remaining.Hours()/24))
}

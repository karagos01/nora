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
)

type CreateGroupDialog struct {
	app     *App
	Visible bool

	nameEditor widget.Editor
	createBtn  widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable
}

func NewCreateGroupDialog(a *App) *CreateGroupDialog {
	d := &CreateGroupDialog{app: a}
	d.nameEditor.SingleLine = true
	d.nameEditor.Submit = true
	return d
}

func (d *CreateGroupDialog) Show() {
	d.Visible = true
	d.nameEditor.SetText("")
}

func (d *CreateGroupDialog) Hide() {
	d.Visible = false
}

func (d *CreateGroupDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	submit := d.createBtn.Clicked(gtx)
	for {
		ev, ok := d.nameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submit = true
		}
	}
	if submit {
		name := d.nameEditor.Text()
		if name != "" {
			d.Hide()
			go func() {
				if c := d.app.Conn(); c != nil {
					group, err := c.Client.CreateGroup(name)
					if err != nil {
						log.Printf("CreateGroup: %v", err)
						return
					}
					d.app.mu.Lock()
					c.Groups = append(c.Groups, *group)
					d.app.mu.Unlock()
					d.app.Window.Invalidate()
				}
			}()
		}
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
			gtx.Constraints.Max.X = gtx.Dp(320)
			gtx.Constraints.Min.X = gtx.Dp(320)

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
									lbl := material.H6(d.app.Theme.Material, "Create Group")
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "E2E encrypted group chat")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Name input
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.layoutEditor(gtx, &d.nameEditor, "Group name")
									})
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
													return layoutDialogBtn(gtx, d.app.Theme, &d.createBtn, "Create", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
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

func (d *CreateGroupDialog) layoutEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
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


package ui

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// layoutFormattingToolbar renders B/I/S/Code buttons above the editor.
func (v *MessageView) layoutFormattingToolbar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if v.fmtBoldBtn.Clicked(gtx) {
			v.wrapEditorSelection("**", "**")
		}
		if v.fmtItalicBtn.Clicked(gtx) {
			v.wrapEditorSelection("*", "*")
		}
		if v.fmtStrikeBtn.Clicked(gtx) {
			v.wrapEditorSelection("~~", "~~")
		}
		if v.fmtCodeBtn.Clicked(gtx) {
			v.wrapEditorSelection("`", "`")
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutFmtBtn(gtx, &v.fmtBoldBtn, "B", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutFmtBtn(gtx, &v.fmtItalicBtn, "I", false)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutFmtBtn(gtx, &v.fmtStrikeBtn, "S", false)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutFmtBtn(gtx, &v.fmtCodeBtn, "</>", false)
			}),
		)
	})
}

// layoutFmtBtn renders a single formatting button.
func (v *MessageView) layoutFmtBtn(gtx layout.Context, btn *widget.Clickable, label string, bold bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				if bg.A > 0 {
					rr := gtx.Dp(4)
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, label)
					lbl.Color = ColorTextDim
					if btn.Hovered() {
						lbl.Color = ColorText
					}
					if bold {
						lbl.Font.Weight = 700
					}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

// wrapEditorSelection — wrap vybraný text prefixem/suffixem, nebo vlož markers na kurzor
func (v *MessageView) wrapEditorSelection(prefix, suffix string) {
	start, end := v.editor.Selection()
	if start > end {
		start, end = end, start
	}
	txt := v.editor.Text()
	runes := []rune(txt)

	if start == end {
		// Žádný výběr — vložit markers a kurzor mezi ně
		insert := prefix + suffix
		v.editor.SetText(string(runes[:start]) + insert + string(runes[start:]))
		// Nastavit kurzor mezi prefix a suffix
		newPos := start + len([]rune(prefix))
		v.editor.SetCaret(newPos, newPos)
	} else {
		// Wrap vybraný text
		selected := string(runes[start:end])
		wrapped := prefix + selected + suffix
		v.editor.SetText(string(runes[:start]) + wrapped + string(runes[end:]))
		// Vybrat celý wrapped text
		newEnd := start + len([]rune(wrapped))
		v.editor.SetCaret(newEnd, newEnd)
	}
}

// layoutEditedLabel vykreslí klikatelný "(edited)" label.
func (v *MessageView) layoutEditedLabel(gtx layout.Context, idx int) layout.Dimensions {
	return v.actions[idx].editHistoryBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Caption(v.app.Theme.Material, " (edited)")
		lbl.Color = ColorTextDim
		return lbl.Layout(gtx)
	})
}

// layoutEditHistoryOverlay vykreslí overlay s historií editací zprávy.
func (v *MessageView) layoutEditHistoryOverlay(gtx layout.Context) layout.Dimensions {
	if !v.showEditHistory {
		return layout.Dimensions{}
	}

	// Zavřít overlay klikem na tlačítko
	if v.editHistoryClose.Clicked(gtx) {
		v.showEditHistory = false
		v.editHistory = nil
		v.editHistoryMsgID = ""
	}

	// Poloprůhledné pozadí
	paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Panel uprostřed
	panelW := gtx.Constraints.Max.X * 2 / 3
	if panelW < gtx.Dp(unit.Dp(400)) {
		panelW = gtx.Constraints.Max.X - gtx.Dp(unit.Dp(40))
	}
	panelH := gtx.Constraints.Max.Y * 2 / 3
	if panelH < gtx.Dp(unit.Dp(300)) {
		panelH = gtx.Constraints.Max.Y - gtx.Dp(unit.Dp(40))
	}

	offsetX := (gtx.Constraints.Max.X - panelW) / 2
	offsetY := (gtx.Constraints.Max.Y - panelH) / 2

	defer op.Offset(image.Pt(offsetX, offsetY)).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: image.Pt(panelW, panelH)}.Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: image.Pt(panelW, panelH)}.Op())

	gtx.Constraints.Max = image.Pt(panelW, panelH)
	gtx.Constraints.Min = gtx.Constraints.Max

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.H6(v.app.Theme.Material, "Edit History")
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(v.app.Theme.Material, &v.editHistoryClose, "Close")
						btn.Background = ColorAccent
						btn.Color = ColorText
						return btn.Layout(gtx)
					}),
				)
			})
		}),
		// Seznam editací
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(v.editHistory) == 0 {
				return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, "Loading...")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}

			return material.List(v.app.Theme.Material, &v.editHistoryList).Layout(gtx, len(v.editHistory), func(gtx layout.Context, i int) layout.Dimensions {
				edit := v.editHistory[i]
				version := len(v.editHistory) - i
				ts := edit.EditedAt.Local().Format("15:04 02.01.2006")

				return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							header := fmt.Sprintf("Version %d (%s)", version, ts)
							lbl := material.Caption(v.app.Theme.Material, header)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, edit.OldContent)
								lbl.Color = ColorText
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			})
		}),
	)
}

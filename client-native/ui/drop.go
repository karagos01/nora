package ui

import (
	"image"
	"image/color"
	"log"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// HandleDroppedFiles zpracuje soubory přetažené z OS file manageru do okna.
// Routuje do příslušného upload flow dle aktivního ViewMode.
func (a *App) HandleDroppedFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	log.Printf("drop: HandleDroppedFiles called with %d files, mode=%d", len(paths), a.Mode)
	conn := a.Conn()
	if conn == nil {
		return
	}

	switch a.Mode {
	case ViewChannels:
		if conn.P2P != nil {
			a.ZipUploadDlg.Show(paths, a.MsgView.startUploads, a.MsgView.startP2PSend)
		} else if len(paths) >= 2 {
			a.ZipUploadDlg.Show(paths, a.MsgView.startUploads)
		} else {
			a.MsgView.startUploads(paths)
			return
		}
	case ViewDM:
		if conn.P2P != nil {
			a.ZipUploadDlg.Show(paths, a.DMView.startUploadsDM, a.DMView.startP2PSendDM)
		} else if len(paths) >= 2 {
			a.ZipUploadDlg.Show(paths, a.DMView.startUploadsDM)
		} else {
			a.DMView.startUploadsDM(paths)
			return
		}
	default:
		return
	}
	a.Window.Invalidate()
}

// layoutDropOverlay zobrazí poloprůhledný overlay s ikonou a textem při drag-over.
func (a *App) layoutDropOverlay(gtx layout.Context) layout.Dimensions {
	// Poloprůhledné pozadí
	paint.FillShape(gtx.Ops, color.NRGBA{R: 30, G: 30, B: 40, A: 200}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Ohraničení (border box s přerušovanou čárou — zjednodušeno jako plný rámeček)
	margin := gtx.Dp(24)
	inner := image.Rect(margin, margin, gtx.Constraints.Max.X-margin, gtx.Constraints.Max.Y-margin)
	border := gtx.Dp(2)

	// Horní + dolní okraj
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: inner.Min, Max: image.Pt(inner.Max.X, inner.Min.Y+border)}.Op())
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: image.Pt(inner.Min.X, inner.Max.Y-border), Max: inner.Max}.Op())
	// Levý + pravý okraj
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: inner.Min, Max: image.Pt(inner.Min.X+border, inner.Max.Y)}.Op())
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: image.Pt(inner.Max.X-border, inner.Min.Y), Max: inner.Max}.Op())

	// Text uprostřed
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.H5(a.Theme.Material, "Drop files to upload")
				lbl.Color = ColorText
				lbl.Alignment = 1 // center
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(a.Theme.Material, "Release to start upload")
				lbl.Color = ColorTextDim
				lbl.Alignment = 1
				return lbl.Layout(gtx)
			}),
		)
	})
}

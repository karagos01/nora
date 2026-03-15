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

// HandleDroppedFiles processes files dragged from the OS file manager into the window.
// Routes to the appropriate upload flow based on the active ViewMode.
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

// layoutDropOverlay displays a semi-transparent overlay with an icon and text during drag-over.
func (a *App) layoutDropOverlay(gtx layout.Context) layout.Dimensions {
	// Semi-transparent background
	paint.FillShape(gtx.Ops, color.NRGBA{R: 30, G: 30, B: 40, A: 200}, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Border (dashed border box — simplified as a solid frame)
	margin := gtx.Dp(24)
	inner := image.Rect(margin, margin, gtx.Constraints.Max.X-margin, gtx.Constraints.Max.Y-margin)
	border := gtx.Dp(2)

	// Top + bottom edge
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: inner.Min, Max: image.Pt(inner.Max.X, inner.Min.Y+border)}.Op())
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: image.Pt(inner.Min.X, inner.Max.Y-border), Max: inner.Max}.Op())
	// Left + right edge
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: inner.Min, Max: image.Pt(inner.Min.X+border, inner.Max.Y)}.Op())
	paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Min: image.Pt(inner.Max.X-border, inner.Min.Y), Max: inner.Max}.Op())

	// Centered text
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

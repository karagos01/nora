package ui

import (
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/p2p"
)

// p2pLinkInfo — parsed P2P link from a message.
type p2pLinkInfo struct {
	transferID string
	senderID   string
	fileName   string
	fileSize   int64
}

// parseP2PLink parses the format [P2P:transferID:senderUserID:fileName:fileSize].
func parseP2PLink(text string) *p2pLinkInfo {
	if !strings.HasPrefix(text, "[P2P:") || !strings.HasSuffix(text, "]") {
		return nil
	}
	inner := text[5 : len(text)-1] // between [P2P: and ]
	parts := strings.SplitN(inner, ":", 4)
	if len(parts) != 4 {
		return nil
	}
	var size int64
	fmt.Sscanf(parts[3], "%d", &size)
	return &p2pLinkInfo{
		transferID: parts[0],
		senderID:   parts[1],
		fileName:   parts[2],
		fileSize:   size,
	}
}

// layoutP2PPanel displays individual progress bars for P2P reception.
func (v *MessageView) layoutP2PPanel(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil || conn.P2P == nil {
		return layout.Dimensions{}
	}

	transfers := conn.P2P.GetActiveTransfers()
	// Filter DirReceive in states Waiting/Connecting/Transferring/Error
	var visible []*p2p.Transfer
	for _, t := range transfers {
		if t.Direction != p2p.DirReceive {
			continue
		}
		switch t.Status {
		case p2p.StatusWaiting, p2p.StatusConnecting, p2p.StatusTransferring, p2p.StatusError:
			visible = append(visible, t)
		}
	}
	if len(visible) == 0 {
		return layout.Dimensions{}
	}

	// Stable sort by ID (map returns in random order)
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].ID < visible[j].ID
	})

	// Process clicks on bars
	for _, t := range visible {
		btn, ok := v.p2pBarBtns[t.ID]
		if !ok {
			continue
		}
		if btn.Clicked(gtx) {
			tid := t.ID
			peerID := t.PeerID
			savePath := t.SavePath()
			if t.Status == p2p.StatusError {
				errMsg := t.Error
				if strings.Contains(errMsg, "rejected") || strings.Contains(errMsg, "unavailable") {
					v.app.ConfirmDlg.ShowWithText("P2P Transfer", "File no longer available. The sender may have disconnected.", "OK", func() {
						conn.P2P.DismissTransfer(tid)
						v.app.Window.Invalidate()
					})
				} else {
					v.app.ConfirmDlg.ShowWithCancel("P2P Transfer", "Error: "+errMsg, "Retry", func() {
						conn.P2P.DismissTransfer(tid)
						conn.P2P.RequestDownload(peerID, tid, savePath)
						v.app.Window.Invalidate()
					}, func() {
						conn.P2P.DismissTransfer(tid)
						v.app.Window.Invalidate()
					})
				}
			} else {
				v.app.ConfirmDlg.ShowWithText("P2P Transfer", "Cancel download of "+t.FileName+"?", "Cancel", func() {
					conn.P2P.CancelTransfer(tid)
					v.app.Window.Invalidate()
				})
			}
		}
	}

	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		items := make([]layout.FlexChild, 0, len(visible))
		for _, t := range visible {
			t := t
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutP2PBar(gtx, t)
			}))
		}
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceStart}.Layout(gtx, items...)
	})
}

// layoutP2PBar renders an individual P2P progress bar.
func (v *MessageView) layoutP2PBar(gtx layout.Context, t *p2p.Transfer) layout.Dimensions {
	// Ensure clickable for this transfer
	btn, ok := v.p2pBarBtns[t.ID]
	if !ok {
		btn = new(widget.Clickable)
		v.p2pBarBtns[t.ID] = btn
	}

	var statusText string
	barColor := ColorAccent

	switch t.Status {
	case p2p.StatusWaiting, p2p.StatusConnecting:
		statusText = t.FileName + " \u2014 connecting..."
	case p2p.StatusTransferring:
		if t.FileSize > 0 {
			pct := t.Progress * 100 / t.FileSize
			statusText = fmt.Sprintf("%s \u2014 %d%% (%s / %s)", t.FileName, pct, FormatBytes(t.Progress), FormatBytes(t.FileSize))
			if !t.StartTime.IsZero() {
				elapsed := time.Since(t.StartTime).Seconds()
				bytesThisSession := t.Progress - t.Offset
				if elapsed > 0.5 && bytesThisSession > 0 {
					speed := float64(bytesThisSession) / elapsed
					statusText += fmt.Sprintf(" \u00b7 %s/s", FormatBytes(int64(speed)))
					remaining := float64(t.FileSize-t.Progress) / speed
					if remaining > 0 && remaining < 3600 {
						if remaining < 60 {
							statusText += fmt.Sprintf(" \u00b7 ~%ds", int(remaining))
						} else {
							statusText += fmt.Sprintf(" \u00b7 ~%dm%ds", int(remaining)/60, int(remaining)%60)
						}
					}
				}
			}
		} else {
			statusText = fmt.Sprintf("%s \u2014 %s", t.FileName, FormatBytes(t.Progress))
		}
	case p2p.StatusError:
		statusText = t.FileName + " \u2014 " + t.Error
		barColor = ColorDanger
	}

	renderBar := func(gtx layout.Context) layout.Dimensions {
		bg := ColorSidebar
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Text + background
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, bg, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconArrowDown, 14, barColor)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, statusText)
											lbl.Color = barColor
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						},
					)
				}),
				// Progress bar (only when transferring)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if t.Status != p2p.StatusTransferring || t.FileSize <= 0 {
						return layout.Dimensions{}
					}
					maxW := gtx.Constraints.Max.X
					h := gtx.Dp(3)
					paint.FillShape(gtx.Ops, ColorBg, clip.Rect(image.Rect(0, 0, maxW, h)).Op())
					pct := float32(t.Progress) / float32(t.FileSize)
					barW := int(pct * float32(maxW))
					if barW > 0 {
						paint.FillShape(gtx.Ops, barColor, clip.Rect(image.Rect(0, 0, barW, h)).Op())
					}
					return layout.Dimensions{Size: image.Pt(maxW, h)}
				}),
			)
		})
	}

	return btn.Layout(gtx, renderBar)
}


// layoutP2PBlock renders a P2P link in the style of regular attachments (single-line).
func (v *MessageView) layoutP2PBlock(gtx layout.Context, info *p2pLinkInfo, idx int, isOwn bool) layout.Dimensions {
	conn := v.app.Conn()

	// Determine state: unavailable > active transfer > downloaded > own > available
	icon := IconArrowDown
	iconColor := ColorAccent
	suffix := " \u2014 click to save"
	textColor := ColorAccent
	clickable := true

	senderOffline := conn != nil && !isOwn && !conn.OnlineUsers[info.senderID]

	if conn != nil && conn.P2P != nil {
		if conn.P2P.IsUnavailable(info.transferID) {
			icon = IconCancel
			iconColor = ColorDanger
			suffix = " \u2014 unavailable"
			textColor = ColorTextDim
			clickable = false
		} else if v.p2pTransferActive(conn, info.transferID) {
			suffix = " \u2014 downloading..."
			clickable = false
		} else if conn.P2P.IsDownloaded(info.transferID) {
			iconColor = ColorSuccess
			suffix = ""
			textColor = ColorSuccess
		} else if isOwn {
			if conn.P2P.IsRegistered(info.transferID) {
				icon = IconUpload
				suffix = " \u2014 sharing"
			} else {
				icon = IconCancel
				iconColor = ColorDanger
				suffix = " \u2014 expired"
				textColor = ColorTextDim
			}
			clickable = false
		} else if senderOffline {
			icon = IconCancel
			iconColor = ColorDanger
			suffix = " \u2014 unavailable"
			textColor = ColorTextDim
			clickable = false
		}
	} else if isOwn {
		icon = IconCancel
		iconColor = ColorDanger
		suffix = " \u2014 expired"
		textColor = ColorTextDim
		clickable = false
	} else if senderOffline {
		icon = IconCancel
		iconColor = ColorDanger
		suffix = " \u2014 unavailable"
		textColor = ColorTextDim
		clickable = false
	}

	label := info.fileName + " (" + FormatBytes(info.fileSize) + ") \u00b7 P2P" + suffix
	btn := &v.actions[idx].p2pBtn

	renderBlock := func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if clickable && btn.Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, bg, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutIcon(gtx, icon, 16, iconColor)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, label)
								lbl.Color = textColor
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			},
		)
	}

	if clickable {
		return btn.Layout(gtx, renderBlock)
	}
	return renderBlock(gtx)
}

// p2pTransferActive returns true if the transfer is in an active state (waiting/connecting/transferring).
func (v *MessageView) p2pTransferActive(conn *ServerConnection, transferID string) bool {
	for _, t := range conn.P2P.GetActiveTransfers() {
		if t.ID == transferID && (t.Status == p2p.StatusWaiting || t.Status == p2p.StatusConnecting || t.Status == p2p.StatusTransferring) {
			return true
		}
	}
	return false
}

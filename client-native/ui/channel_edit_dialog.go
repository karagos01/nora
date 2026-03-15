package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
)

// permOption describes a single permission for UI checkboxes
type permOption struct {
	Label string
	Bit   int64
}

// Supported permission bits for channel overrides
var channelPermOptions = []permOption{
	{"Send Messages", api.PermSendMessages},
	{"Read", api.PermRead},
	{"Manage Messages", api.PermManageMessages},
	{"Upload", api.PermUpload},
}

type ChannelEditDialog struct {
	app     *App
	Visible bool

	channelID string
	nameEd    widget.Editor
	topicEd   widget.Editor

	// Category selection
	catBtns     []widget.Clickable
	selectedCat int // index into categories, -1 = none

	// Slow mode selection
	slowModeBtns    [6]widget.Clickable
	selectedSlowIdx int // index into slowModeOptions

	// Permission overrides
	permOverrides    []api.ChannelPermOverride
	permDeleteBtns   []widget.Clickable
	permLoaded       bool
	addOverrideBtn   widget.Clickable
	showAddOverride  bool
	addTargetTypeIdx int // 0 = role, 1 = user
	addTypeBtns      [2]widget.Clickable
	addTargetEd      widget.Editor
	addAllowChecks   [4]widget.Bool
	addDenyChecks    [4]widget.Bool
	addSaveBtn       widget.Clickable
	addCancelBtn     widget.Clickable

	confirmBtn widget.Clickable
	cancelBtn  widget.Clickable
	overlayBtn widget.Clickable
	cardBtn    widget.Clickable

	scrollList widget.List
}

var slowModeOptions = []struct {
	Label   string
	Seconds int
}{
	{"Off", 0},
	{"5s", 5},
	{"10s", 10},
	{"30s", 30},
	{"1m", 60},
	{"5m", 300},
}

func NewChannelEditDialog(a *App) *ChannelEditDialog {
	d := &ChannelEditDialog{app: a, selectedCat: -1}
	d.nameEd.SingleLine = true
	d.topicEd.SingleLine = true
	d.addTargetEd.SingleLine = true
	d.scrollList.Axis = layout.Vertical
	return d
}

func (d *ChannelEditDialog) Show(ch api.Channel) {
	d.Visible = true
	d.channelID = ch.ID
	d.nameEd.SetText(ch.Name)
	d.topicEd.SetText(ch.Topic)
	d.selectedCat = -1
	d.selectedSlowIdx = 0
	d.permOverrides = nil
	d.permDeleteBtns = nil
	d.permLoaded = false
	d.showAddOverride = false
	d.addTargetEd.SetText("")
	d.addTargetTypeIdx = 0
	for i := range d.addAllowChecks {
		d.addAllowChecks[i].Value = false
	}
	for i := range d.addDenyChecks {
		d.addDenyChecks[i].Value = false
	}

	conn := d.app.Conn()
	if conn != nil && ch.CategoryID != nil {
		for i, cat := range conn.Categories {
			if cat.ID == *ch.CategoryID {
				d.selectedCat = i
				break
			}
		}
	}
	for i, opt := range slowModeOptions {
		if opt.Seconds == ch.SlowModeSeconds {
			d.selectedSlowIdx = i
			break
		}
	}

	// Load permission overrides asynchronously
	go d.loadOverrides(ch.ID)
}

func (d *ChannelEditDialog) loadOverrides(channelID string) {
	conn := d.app.Conn()
	if conn == nil {
		return
	}
	overrides, err := conn.Client.GetChannelPermissions(channelID)
	if err != nil {
		log.Printf("GetChannelPermissions: %v", err)
		overrides = []api.ChannelPermOverride{}
	}
	d.app.mu.Lock()
	d.permOverrides = overrides
	d.permDeleteBtns = make([]widget.Clickable, len(overrides))
	d.permLoaded = true
	d.app.mu.Unlock()
	d.app.Window.Invalidate()
}

func (d *ChannelEditDialog) Hide() {
	d.Visible = false
}

func (d *ChannelEditDialog) Layout(gtx layout.Context) layout.Dimensions {
	if !d.Visible {
		return layout.Dimensions{}
	}

	conn := d.app.Conn()
	if conn == nil {
		d.Hide()
		return layout.Dimensions{}
	}

	d.app.mu.RLock()
	categories := conn.Categories
	d.app.mu.RUnlock()

	if len(d.catBtns) < len(categories)+1 {
		d.catBtns = make([]widget.Clickable, len(categories)+2)
	}

	if d.overlayBtn.Clicked(gtx) {
		d.Hide()
	}
	if d.cancelBtn.Clicked(gtx) {
		d.Hide()
	}

	// Category button clicks
	if d.catBtns[0].Clicked(gtx) {
		d.selectedCat = -1 // None
	}
	for i := range categories {
		if d.catBtns[i+1].Clicked(gtx) {
			d.selectedCat = i
		}
	}

	// Slow mode button clicks
	for i := range slowModeOptions {
		if d.slowModeBtns[i].Clicked(gtx) {
			d.selectedSlowIdx = i
		}
	}

	// Permission override delete clicks
	for i := range d.permDeleteBtns {
		if d.permDeleteBtns[i].Clicked(gtx) {
			if i < len(d.permOverrides) {
				ov := d.permOverrides[i]
				go d.deleteOverride(conn, ov.ChannelID, ov.TargetType, ov.TargetID)
			}
		}
	}

	// Add override
	if d.addOverrideBtn.Clicked(gtx) {
		d.showAddOverride = true
	}
	for i := range d.addTypeBtns {
		if d.addTypeBtns[i].Clicked(gtx) {
			d.addTargetTypeIdx = i
		}
	}
	if d.addCancelBtn.Clicked(gtx) {
		d.showAddOverride = false
	}
	if d.addSaveBtn.Clicked(gtx) {
		d.saveOverride(conn)
	}

	if d.confirmBtn.Clicked(gtx) {
		d.save(conn, categories)
	}

	return d.overlayBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, color.NRGBA{A: 180}, clip.Rect{Max: gtx.Constraints.Max}.Op())
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return d.cardBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(440)
				gtx.Constraints.Max.X = gtx.Dp(440)
				gtx.Constraints.Max.Y = gtx.Dp(600)
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(12)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return material.List(d.app.Theme.Material, &d.scrollList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									// Title
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.H6(d.app.Theme.Material, "Edit Channel")
										lbl.Color = ColorText
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),

									// Name
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "NAME")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.layoutEditor(gtx, &d.nameEd, "Channel name")
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

									// Topic
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "TOPIC")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.layoutEditor(gtx, &d.topicEd, "Channel topic (optional)")
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

									// Category
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "CATEGORY")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.layoutCategorySelect(gtx, categories)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

									// Slow mode
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(d.app.Theme.Material, "SLOW MODE")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.layoutSlowModeSelect(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),

									// Permission Overrides
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return d.layoutPermissionOverrides(gtx, conn)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),

									// Buttons
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutDialogBtn(gtx, d.app.Theme, &d.cancelBtn, "Cancel", ColorInput, ColorText)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layoutDialogBtn(gtx, d.app.Theme, &d.confirmBtn, "Save", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
											}),
										)
									}),
								)
							})
						})
					},
				)
			})
		})
	})
}

// layoutPermissionOverrides renders the permission overrides section
func (d *ChannelEditDialog) layoutPermissionOverrides(gtx layout.Context, conn *ServerConnection) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, "PERMISSION OVERRIDES")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutDialogBtn(gtx, d.app.Theme, &d.addOverrideBtn, "+ Add", ColorInput, ColorText)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		// Override list
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !d.permLoaded {
				lbl := material.Caption(d.app.Theme.Material, "Loading...")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}
			if len(d.permOverrides) == 0 && !d.showAddOverride {
				lbl := material.Caption(d.app.Theme.Material, "No overrides set")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}
			var items []layout.FlexChild
			for i, ov := range d.permOverrides {
				idx := i
				override := ov
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return d.layoutOverrideRow(gtx, idx, override, conn)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
		}),
		// Add override form
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !d.showAddOverride {
				return layout.Dimensions{}
			}
			return d.layoutAddOverrideForm(gtx)
		}),
	)
}

// layoutOverrideRow renders a single override row
func (d *ChannelEditDialog) layoutOverrideRow(gtx layout.Context, idx int, ov api.ChannelPermOverride, conn *ServerConnection) layout.Dimensions {
	// Resolve target name
	targetName := ov.TargetID
	if ov.TargetType == "role" {
		d.app.mu.RLock()
		for _, r := range conn.Roles {
			if r.ID == ov.TargetID {
				targetName = r.Name
				break
			}
		}
		d.app.mu.RUnlock()
	} else {
		d.app.mu.RLock()
		for _, u := range conn.Users {
			if u.ID == ov.TargetID {
				targetName = u.Username
				break
			}
		}
		d.app.mu.RUnlock()
	}

	typeLabel := "Role"
	if ov.TargetType == "user" {
		typeLabel = "User"
	}

	allowStr := permBitsToString(ov.Allow)
	denyStr := permBitsToString(ov.Deny)

	desc := fmt.Sprintf("[%s] %s", typeLabel, targetName)
	if allowStr != "" {
		desc += fmt.Sprintf(" | Allow: %s", allowStr)
	}
	if denyStr != "" {
		desc += fmt.Sprintf(" | Deny: %s", denyStr)
	}

	return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(d.app.Theme.Material, desc)
				lbl.Color = ColorText
				lbl.MaxLines = 2
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if idx >= len(d.permDeleteBtns) {
					return layout.Dimensions{}
				}
				return layoutDialogBtn(gtx, d.app.Theme, &d.permDeleteBtns[idx], "X", color.NRGBA{R: 200, G: 60, B: 60, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}),
		)
	})
}

// layoutAddOverrideForm renders the form for adding a new override
func (d *ChannelEditDialog) layoutAddOverrideForm(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Target type selection
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(d.app.Theme.Material, "TARGET TYPE")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							typeLabels := [2]string{"Role", "User"}
							var items []layout.FlexChild
							for i, label := range typeLabels {
								idx := i
								lbl := label
								items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return d.layoutCatOption(gtx, &d.addTypeBtns[idx], lbl, d.addTargetTypeIdx == idx)
									})
								}))
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

						// Target ID
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							hint := "Role ID"
							if d.addTargetTypeIdx == 1 {
								hint = "User ID"
							}
							lbl := material.Caption(d.app.Theme.Material, "TARGET ID")
							lbl.Color = ColorTextDim
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return d.layoutEditor(gtx, &d.addTargetEd, hint)
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

						// Allow checkboxes
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(d.app.Theme.Material, "ALLOW")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutPermCheckboxes(gtx, d.addAllowChecks[:])
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),

						// Deny checkboxes
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(d.app.Theme.Material, "DENY")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return d.layoutPermCheckboxes(gtx, d.addDenyChecks[:])
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

						// Save / Cancel
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutDialogBtn(gtx, d.app.Theme, &d.addCancelBtn, "Cancel", ColorInput, ColorText)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutDialogBtn(gtx, d.app.Theme, &d.addSaveBtn, "Add", ColorAccent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
								}),
							)
						}),
					)
				})
			},
		)
	})
}

// layoutPermCheckboxes renders a row of permission checkboxes
func (d *ChannelEditDialog) layoutPermCheckboxes(gtx layout.Context, checks []widget.Bool) layout.Dimensions {
	var items []layout.FlexChild
	for i, opt := range channelPermOptions {
		idx := i
		label := opt.Label
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cb := material.CheckBox(d.app.Theme.Material, &checks[idx], label)
				cb.Color = ColorText
				cb.Size = unit.Dp(16)
				return cb.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
}

func (d *ChannelEditDialog) saveOverride(conn *ServerConnection) {
	targetType := "role"
	if d.addTargetTypeIdx == 1 {
		targetType = "user"
	}
	targetID := d.addTargetEd.Text()
	if targetID == "" {
		return
	}

	var allow, deny int64
	for i, opt := range channelPermOptions {
		if d.addAllowChecks[i].Value {
			allow |= opt.Bit
		}
		if d.addDenyChecks[i].Value {
			deny |= opt.Bit
		}
	}

	chID := d.channelID
	d.showAddOverride = false

	go func() {
		ov := api.ChannelPermOverride{
			ChannelID:  chID,
			TargetType: targetType,
			TargetID:   targetID,
			Allow:      allow,
			Deny:       deny,
		}
		if err := conn.Client.SetChannelPermission(ov); err != nil {
			log.Printf("SetChannelPermission: %v", err)
			return
		}
		// Reload overrides
		d.loadOverrides(chID)
	}()
}

func (d *ChannelEditDialog) deleteOverride(conn *ServerConnection, channelID, targetType, targetID string) {
	if err := conn.Client.DeleteChannelPermission(channelID, targetType, targetID); err != nil {
		log.Printf("DeleteChannelPermission: %v", err)
		return
	}
	d.loadOverrides(channelID)
}

func (d *ChannelEditDialog) layoutCategorySelect(gtx layout.Context, categories []api.ChannelCategory) layout.Dimensions {
	var items []layout.FlexChild

	// "None" option
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return d.layoutCatOption(gtx, &d.catBtns[0], "None", d.selectedCat == -1)
	}))

	for i, cat := range categories {
		idx := i
		name := cat.Name
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutCatOption(gtx, &d.catBtns[idx+1], name, d.selectedCat == idx)
			})
		}))
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
}

func (d *ChannelEditDialog) layoutCatOption(gtx layout.Context, btn *widget.Clickable, text string, selected bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if selected {
			bg = ColorAccent
		} else if btn.Hovered() {
			bg = ColorHover
		}
		fg := ColorTextDim
		if selected {
			fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(4)
				paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
				return layout.Dimensions{Size: sz.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(d.app.Theme.Material, text)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (d *ChannelEditDialog) layoutSlowModeSelect(gtx layout.Context) layout.Dimensions {
	var items []layout.FlexChild
	for i, opt := range slowModeOptions {
		idx := i
		label := opt.Label
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.layoutCatOption(gtx, &d.slowModeBtns[idx], label, d.selectedSlowIdx == idx)
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
}

func (d *ChannelEditDialog) save(conn *ServerConnection, categories []api.ChannelCategory) {
	name := d.nameEd.Text()
	topic := d.topicEd.Text()
	chID := d.channelID

	var catID *string
	if d.selectedCat >= 0 && d.selectedCat < len(categories) {
		id := categories[d.selectedCat].ID
		catID = &id
	}

	slowSec := slowModeOptions[d.selectedSlowIdx].Seconds
	slowMode := &slowSec

	d.Hide()

	go func() {
		if err := conn.Client.UpdateChannel(chID, name, topic, catID, slowMode); err != nil {
			log.Printf("UpdateChannel: %v", err)
			return
		}
		d.app.mu.Lock()
		for i, ch := range conn.Channels {
			if ch.ID == chID {
				conn.Channels[i].Name = name
				conn.Channels[i].Topic = topic
				conn.Channels[i].CategoryID = catID
				conn.Channels[i].SlowModeSeconds = slowSec
				if conn.ActiveChannelID == chID {
					conn.ActiveChannelName = name
				}
				break
			}
		}
		d.app.mu.Unlock()
		d.app.Window.Invalidate()
	}()
}

func (d *ChannelEditDialog) layoutEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(8)
			paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
			return layout.Dimensions{Size: sz.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(d.app.Theme.Material, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}


// permBitsToString converts a bitmask to a human-readable string
func permBitsToString(bits int64) string {
	if bits == 0 {
		return ""
	}
	var parts []string
	for _, opt := range channelPermOptions {
		if bits&opt.Bit != 0 {
			parts = append(parts, opt.Label)
		}
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

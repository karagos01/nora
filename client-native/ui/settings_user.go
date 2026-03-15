package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/store"
)

func (v *SettingsView) layoutProfileSection(gtx layout.Context, blocks []blockInfo) layout.Dimensions {
	var items []layout.FlexChild

	// Identity
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Profile")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Username")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body2(v.app.Theme.Material, v.app.Username)
		lbl.Color = ColorText
		return lbl.Layout(gtx)
	}))

	// Avatar
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Avatar")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		var avatarURL string
		if conn := v.app.Conn(); conn != nil {
			v.app.mu.RLock()
			for _, u := range conn.Users {
				if u.ID == conn.UserID {
					avatarURL = u.AvatarURL
					break
				}
			}
			v.app.mu.RUnlock()
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutAvatar(gtx, v.app, v.app.Username, avatarURL, 48)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return v.layoutAccentBtn(gtx, &v.avatarPickBtn, "Choose File")
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if v.avatarFilePath == "" {
										return layout.Dimensions{}
									}
									return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										text := "Upload"
										if v.avatarUploading {
											text = "..."
										}
										return v.layoutAccentBtn(gtx, &v.avatarUploadBtn, text)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if avatarURL == "" {
										return layout.Dimensions{}
									}
									return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.avatarDeleteBtn, "Remove", ColorDanger)
									})
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if v.avatarFilePath == "" && v.avatarError == "" {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								text := filepath.Base(v.avatarFilePath)
								clr := ColorTextDim
								if v.avatarError != "" {
									text = v.avatarError
									clr = ColorDanger
								}
								lbl := material.Caption(v.app.Theme.Material, text)
								lbl.Color = clr
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			}),
		)
	}))

	// Display name
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Display Name")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return v.layoutEditor(gtx, &v.displayNameEditor, "Enter display name...")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					text := "Save"
					if v.displayNameSaving {
						text = "..."
					}
					return v.layoutSmallBtn(gtx, &v.saveDisplayNameBtn, text, ColorAccent)
				})
			}),
		)
	}))
	if v.displayNameError != "" {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, v.displayNameError)
				lbl.Color = ColorDanger
				return lbl.Layout(gtx)
			})
		}))
	}
	if v.displayNameSuccess {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Display name updated")
				lbl.Color = ColorOnline
				return lbl.Layout(gtx)
			})
		}))
	}

	// Custom Status
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Status")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutStatusButtons(gtx)
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.layoutEditor(gtx, &v.statusTextEditor, "Custom status text...")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						text := "Save"
						if v.statusSaving {
							text = "..."
						}
						return v.layoutSmallBtn(gtx, &v.saveStatusBtn, text, ColorAccent)
					})
				}),
			)
		})
	}))

	// Public Key
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Public Key")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				pk := v.app.PublicKey
				if len(pk) > 32 {
					pk = pk[:16] + "..." + pk[len(pk)-16:]
				}
				lbl := material.Body2(v.app.Theme.Material, pk)
				lbl.Color = ColorAccent
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutSmallIconBtn(gtx, &v.copyPubKey, IconCopy, ColorTextDim)
				})
			}),
		)
	}))

	// Register nora:// protocol
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutSection(gtx, "Protocol")
		})
	}))
	if v.registerProtocolBtn.Clicked(gtx) {
		go func() {
			if err := RegisterURLScheme(); err != nil {
				log.Printf("RegisterURLScheme: %v", err)
			} else {
				log.Printf("nora:// protocol registered successfully")
			}
		}()
	}
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.registerProtocolBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(6)
						bg := ColorInput
						if v.registerProtocolBtn.Hovered() {
							bg = ColorHover
						}
						paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, "Register nora:// protocol")
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					},
				)
			})
		})
	}))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutPasswordSection(gtx layout.Context) layout.Dimensions {
	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Change Password")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutEditor(gtx, &v.oldPwEditor, "Current password")
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutEditor(gtx, &v.newPwEditor, "New password")
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.layoutEditor(gtx, &v.confirmPwEditor, "Confirm new password")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.layoutSmallBtn(gtx, &v.changePwBtn, "Change", ColorAccent)
					})
				}),
			)
		})
	}))
	if v.pwChangeError != "" {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, v.pwChangeError)
				lbl.Color = ColorDanger
				return lbl.Layout(gtx)
			})
		}))
	}
	if v.pwChangeOk {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Password changed successfully")
				lbl.Color = ColorOnline
				return lbl.Layout(gtx)
			})
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutVoiceSection(gtx layout.Context) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Voice Settings")
	}))
	items = append(items, v.layoutVoiceSettings(gtx)...)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutBlockedSection(gtx layout.Context, blocks []blockInfo) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Blocked Users")
	}))
	if len(blocks) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No blocked users")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		for i, b := range blocks {
			idx := i
			block := b
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, block.Username)
							lbl.Color = UserColor(block.Username)
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.unblockBtns[idx], "Unblock", ColorDanger)
						}),
					)
				})
			}))
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutNotificationsSection(gtx layout.Context) layout.Dimensions {
	a := v.app

	// Handle clicks
	if v.notifyAllBtn.Clicked(gtx) {
		a.GlobalNotifyLevel = store.NotifyAll
		go store.UpdateGlobalNotifyLevel(a.PublicKey, store.NotifyAll)
	}
	if v.notifyMentionsBtn.Clicked(gtx) {
		a.GlobalNotifyLevel = store.NotifyMentions
		go store.UpdateGlobalNotifyLevel(a.PublicKey, store.NotifyMentions)
	}
	if v.notifyNothingBtn.Clicked(gtx) {
		a.GlobalNotifyLevel = store.NotifyNothing
		go store.UpdateGlobalNotifyLevel(a.PublicKey, store.NotifyNothing)
	}

	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Notifications")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Default notification level for all servers and channels.")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))

	current := a.GlobalNotifyLevel

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutNotifyRadio(gtx, &v.notifyAllBtn, "All messages", "Play sound for every message.", current == store.NotifyAll)
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutNotifyRadio(gtx, &v.notifyMentionsBtn, "Only @mentions", "Only play sound when you are mentioned.", current == store.NotifyMentions)
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutNotifyRadio(gtx, &v.notifyNothingBtn, "Muted", "No notification sounds.", current == store.NotifyNothing)
	}))

	// --- Sound Settings ---
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(v.app.Theme.Material, "Sound Settings")
			lbl.Color = ColorText
			return lbl.Layout(gtx)
		})
	}))

	// Volume slider
	if v.notifVolumeSlider.Changed() {
		a.NotifVolume = float64(v.notifVolumeSlider.Value)
		SetSoundSettings(a.NotifVolume, a.CustomNotifSnd, a.CustomDMSnd)
		go store.UpdateSoundSettings(a.PublicKey, a.NotifVolume, a.CustomNotifSnd, a.CustomDMSnd)
		if time.Since(v.lastPreviewTime) > 400*time.Millisecond {
			v.lastPreviewTime = time.Now()
			PlayNotifPreview()
		}
	}
	// Sync slider value from app
	v.notifVolumeSlider.Value = float32(a.NotifVolume)

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(130)
					lbl := material.Caption(v.app.Theme.Material, "Sound Volume")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.notifVolumeSlider.Layout(gtx, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						pct := int(v.notifVolumeSlider.Value * 100)
						lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("%d%%", pct))
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Notification Sound row
	if v.notifUploadBtn.Clicked(gtx) {
		go func() {
			path := openFileDialog()
			if path == "" {
				return
			}
			dst := filepath.Join(store.SoundsDir(), "notification"+filepath.Ext(path))
			if err := copyFile(path, dst); err != nil {
				log.Printf("copy notification sound: %v", err)
				return
			}
			a.CustomNotifSnd = dst
			SetSoundSettings(a.NotifVolume, a.CustomNotifSnd, a.CustomDMSnd)
			store.UpdateSoundSettings(a.PublicKey, a.NotifVolume, a.CustomNotifSnd, a.CustomDMSnd)
			a.Window.Invalidate()
		}()
	}
	if v.notifResetBtn.Clicked(gtx) {
		a.CustomNotifSnd = ""
		SetSoundSettings(a.NotifVolume, "", a.CustomDMSnd)
		go store.UpdateSoundSettings(a.PublicKey, a.NotifVolume, "", a.CustomDMSnd)
	}

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSoundRow(gtx, "Notification Sound", a.CustomNotifSnd, &v.notifUploadBtn, &v.notifResetBtn)
	}))

	// DM Sound row
	if v.dmUploadBtn.Clicked(gtx) {
		go func() {
			path := openFileDialog()
			if path == "" {
				return
			}
			dst := filepath.Join(store.SoundsDir(), "dm"+filepath.Ext(path))
			if err := copyFile(path, dst); err != nil {
				log.Printf("copy DM sound: %v", err)
				return
			}
			a.CustomDMSnd = dst
			SetSoundSettings(a.NotifVolume, a.CustomNotifSnd, a.CustomDMSnd)
			store.UpdateSoundSettings(a.PublicKey, a.NotifVolume, a.CustomNotifSnd, a.CustomDMSnd)
			a.Window.Invalidate()
		}()
	}
	if v.dmResetBtn.Clicked(gtx) {
		a.CustomDMSnd = ""
		SetSoundSettings(a.NotifVolume, a.CustomNotifSnd, "")
		go store.UpdateSoundSettings(a.PublicKey, a.NotifVolume, a.CustomNotifSnd, "")
	}

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSoundRow(gtx, "DM Sound", a.CustomDMSnd, &v.dmUploadBtn, &v.dmResetBtn)
	}))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

// layoutSoundRow renders a sound row (label, state, upload, reset).
func (v *SettingsView) layoutSoundRow(gtx layout.Context, label, customPath string, uploadBtn, resetBtn *widget.Clickable) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(130)
				lbl := material.Caption(v.app.Theme.Material, label)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				text := "Default"
				if customPath != "" {
					text = filepath.Base(customPath)
				}
				lbl := material.Caption(v.app.Theme.Material, text)
				if customPath != "" {
					lbl.Color = ColorAccent
				} else {
					lbl.Color = ColorTextDim
				}
				return lbl.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if customPath != "" {
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.layoutSmallBtn(gtx, resetBtn, "Reset", ColorDanger)
					})
				}
				return layout.Dimensions{}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutSmallBtn(gtx, uploadBtn, "Upload", ColorAccent)
				})
			}),
		)
	})
}

func (v *SettingsView) layoutNotifyRadio(gtx layout.Context, btn *widget.Clickable, label, desc string, selected bool) layout.Dimensions {
	return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bg := color.NRGBA{}
			if btn.Hovered() {
				bg = ColorHover
			}
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
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
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							// Radio circle
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								sz := gtx.Dp(18)
								borderColor := ColorTextDim
								if selected {
									borderColor = ColorAccent
								}
								// Outer circle
								paint.FillShape(gtx.Ops, borderColor, clip.Ellipse{Max: image.Pt(sz, sz)}.Op(gtx.Ops))
								// Inner (bg)
								innerOff := gtx.Dp(2)
								innerSz := sz - 2*innerOff
								defer clip.Ellipse{Min: image.Pt(innerOff, innerOff), Max: image.Pt(innerOff+innerSz, innerOff+innerSz)}.Op(gtx.Ops).Push(gtx.Ops).Pop()
								if selected {
									paint.FillShape(gtx.Ops, ColorAccent, clip.Ellipse{Min: image.Pt(innerOff, innerOff), Max: image.Pt(innerOff+innerSz, innerOff+innerSz)}.Op(gtx.Ops))
									// White dot center
									dotOff := gtx.Dp(5)
									dotSz := sz - 2*dotOff
									paint.FillShape(gtx.Ops, ColorCard, clip.Ellipse{Min: image.Pt(dotOff, dotOff), Max: image.Pt(dotOff+dotSz, dotOff+dotSz)}.Op(gtx.Ops))
								} else {
									paint.FillShape(gtx.Ops, ColorCard, clip.Ellipse{Min: image.Pt(innerOff, innerOff), Max: image.Pt(innerOff+innerSz, innerOff+innerSz)}.Op(gtx.Ops))
								}
								return layout.Dimensions{Size: image.Pt(sz, sz)}
							}),
							// Label + description
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(v.app.Theme.Material, label)
											lbl.Color = ColorText
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, desc)
											lbl.Color = ColorTextDim
											return lbl.Layout(gtx)
										}),
									)
								})
							}),
						)
					})
				},
			)
		})
	})
}

func (v *SettingsView) layoutStorageSection(gtx layout.Context) layout.Dimensions {
	a := v.app

	// Auto-scan on first display
	if !v.storageScanned && !v.storageScanning {
		v.storageScanning = true
		go func() {
			u := store.ScanDiskUsage()
			v.storageUsage = u
			v.storageScanned = true
			v.storageScanning = false

			// Load saved settings
			maxCache, maxDays := store.GetStorageSettings(a.PublicKey)
			if maxCache > 0 {
				v.storageCacheSlider.Value = float32(maxCache) / float32(1<<30) // bytes → GB
			}
			if maxDays > 0 {
				v.storageHistoryEditor.SetText(strconv.Itoa(maxDays))
			}

			a.Window.Invalidate()
		}()
	}

	// Rescan click
	if v.storageRescanBtn.Clicked(gtx) {
		v.storageScanned = false
	}

	// Save click
	if v.storageSaveBtn.Clicked(gtx) {
		maxCache := int64(v.storageCacheSlider.Value * float32(1<<30))
		maxDays := 0
		if t := strings.TrimSpace(v.storageHistoryEditor.Text()); t != "" {
			if d, err := strconv.Atoi(t); err == nil && d >= 0 {
				maxDays = d
			}
		}
		go store.UpdateStorageSettings(a.PublicKey, maxCache, maxDays)
	}

	// Clear cache click
	if v.clearCacheBtn.Clicked(gtx) {
		a.ConfirmDlg.ShowConfirm("Clear Cache", fmt.Sprintf("Delete all cached files (%s)?", FormatBytes(v.storageUsage.Cache)), func() {
			go func() {
				store.CleanupCacheAll()
				v.storageScanned = false
				a.Window.Invalidate()
			}()
		})
	}

	// Clear history click
	if v.clearHistoryBtn.Clicked(gtx) {
		a.ConfirmDlg.ShowConfirm("Clear Message History", "Delete all local DM and group message history?\nEncryption keys will be preserved.", func() {
			go func() {
				if a.DMHistory != nil {
					a.DMHistory.DeleteOlderThan(0)
					a.DMHistory.Save()
				}
				if a.GroupHistory != nil {
					a.GroupHistory.DeleteOlderThan(0)
					a.GroupHistory.Save()
				}
				v.storageScanned = false
				a.Window.Invalidate()
			}()
		})
	}

	var items []layout.FlexChild

	// Header
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Storage")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Local disk usage for cached files, message history and sounds.")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))

	if v.storageScanning {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Scanning...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}))
	} else if v.storageScanned {
		u := v.storageUsage

		dmCount := 0
		if a.DMHistory != nil {
			dmCount = a.DMHistory.MessageCount()
		}
		groupCount := 0
		if a.GroupHistory != nil {
			groupCount = a.GroupHistory.MessageCount()
		}

		type row struct {
			label string
			value string
			bold  bool
		}
		rows := []row{
			{"DM History", fmt.Sprintf("%s (%d messages)", FormatBytes(u.DMHistory), dmCount), false},
			{"Group History", fmt.Sprintf("%s (%d messages)", FormatBytes(u.GroupHistory), groupCount), false},
			{"Cache", FormatBytes(u.Cache), false},
			{"Sounds", FormatBytes(u.Sounds), false},
			{"Other", FormatBytes(u.Other), false},
			{"Total", FormatBytes(u.Total), true},
		}

		for _, r := range rows {
			r := r
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Dp(130)
							lbl := material.Caption(v.app.Theme.Material, r.label)
							lbl.Color = ColorTextDim
							if r.bold {
								lbl.Font.Weight = 600
								lbl.Color = ColorText
							}
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, r.value)
							lbl.Color = ColorText
							if r.bold {
								lbl.Font.Weight = 600
							}
							return lbl.Layout(gtx)
						}),
					)
				})
			}))
		}
	}

	// Separator
	items = append(items, v.layoutDivider())

	// Limits
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(v.app.Theme.Material, "Limits")
			lbl.Color = ColorText
			lbl.Font.Weight = 600
			return lbl.Layout(gtx)
		})
	}))

	// Cache limit slider
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(130)
					lbl := material.Caption(v.app.Theme.Material, "Cache Limit")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.storageCacheSlider.Layout(gtx, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("%.1f GB", v.storageCacheSlider.Value))
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// History days editor
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(130)
					lbl := material.Caption(v.app.Theme.Material, "History Max Age")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(60)
					gtx.Constraints.Min.X = gtx.Dp(60)
					return v.layoutEditor(gtx, &v.storageHistoryEditor, "0")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "days (0 = unlimited)")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Save button
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutAccentBtn(gtx, &v.storageSaveBtn, "Save")
		})
	}))

	// Separator
	items = append(items, v.layoutDivider())

	// Cleanup
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(v.app.Theme.Material, "Cleanup")
			lbl.Color = ColorText
			lbl.Font.Weight = 600
			return lbl.Layout(gtx)
		})
	}))

	// Clear cache
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutSmallBtn(gtx, &v.clearCacheBtn, "Clear Cache", ColorDanger)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						txt := ""
						if v.storageScanned {
							txt = FormatBytes(v.storageUsage.Cache)
						}
						lbl := material.Caption(v.app.Theme.Material, txt)
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Clear history
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutSmallBtn(gtx, &v.clearHistoryBtn, "Clear Message History", ColorDanger)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "Encryption keys will be preserved")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Rescan
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutSmallBtn(gtx, &v.storageRescanBtn, "Rescan", ColorAccent)
		})
	}))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutAppearanceSection(gtx layout.Context) layout.Dimensions {
	a := v.app

	// Handle slider change
	if v.fontScaleSlider.Changed() {
		scale := v.fontScaleSlider.Value
		a.Theme.ApplyFontScale(scale)
		go store.UpdateFontScale(a.PublicKey, float64(scale))
	}
	// Sync slider
	s := a.Theme.FontScale
	if s == 0 {
		s = 1.0
	}
	v.fontScaleSlider.Value = s

	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Appearance")
	}))

	// Font size slider
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(130)
					lbl := material.Caption(a.Theme.Material, "Text Size")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.fontScaleSlider.Layout(gtx, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						pct := int(v.fontScaleSlider.Value * 100)
						lbl := material.Caption(a.Theme.Material, fmt.Sprintf("%d%%", pct))
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Compact mode toggle
	if v.compactModeBtn.Clicked(gtx) {
		a.Theme.CompactMode = !a.Theme.CompactMode
		go store.UpdateCompactMode(a.PublicKey, a.Theme.CompactMode)
	}
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.compactModeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(18)
						clr := ColorTextDim
						if a.Theme.CompactMode {
							clr = ColorAccent
						}
						rr := sz / 4
						paint.FillShape(gtx.Ops, clr, clip.RRect{
							Rect: image.Rect(0, 0, sz, sz),
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						if a.Theme.CompactMode {
							inner := gtx.Dp(8)
							off := (sz - inner) / 2
							paint.FillShape(gtx.Ops, ColorText, clip.Rect{
								Min: image.Pt(off, off),
								Max: image.Pt(off+inner, off+inner),
							}.Op())
						}
						return layout.Dimensions{Size: image.Pt(sz, sz)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(a.Theme.Material, "Compact Mode")
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(a.Theme.Material, "(IRC-style messages)")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		})
	}))

	// Preview
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(a.Theme.Material, "Preview:")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								return layoutColoredBg(gtx, ColorInput)
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body1(a.Theme.Material, "Alice")
											lbl.Color = UserColor("Alice")
											lbl.Font.Weight = 700
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Body2(a.Theme.Material, "This is how your messages will look at this text size.")
											lbl.Color = ColorText
											return lbl.Layout(gtx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(a.Theme.Material, "12:34 — caption text")
												lbl.Color = ColorTextDim
												return lbl.Layout(gtx)
											})
										}),
									)
								})
							},
						)
					})
				}),
			)
		})
	}))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutPinboardSection(gtx layout.Context) layout.Dimensions {
	if !v.pinboardLoaded {
		conn := v.app.Conn()
		if conn != nil && v.app.Bookmarks != nil {
			v.pinboardBookmarks = v.app.Bookmarks.GetAll(conn.URL)
		} else {
			v.pinboardBookmarks = nil
		}
		v.pinboardDelBtns = make([]widget.Clickable, len(v.pinboardBookmarks))
		v.pinboardJumpBtns = make([]widget.Clickable, len(v.pinboardBookmarks))
		v.pinboardLoaded = true
	}

	// Handle clicks
	for i := range v.pinboardBookmarks {
		if i < len(v.pinboardDelBtns) && v.pinboardDelBtns[i].Clicked(gtx) {
			bk := v.pinboardBookmarks[i]
			v.app.Bookmarks.Remove(bk.ID, bk.ServerURL)
			go v.app.Bookmarks.Save()
			v.pinboardLoaded = false
		}
		if i < len(v.pinboardJumpBtns) && v.pinboardJumpBtns[i].Clicked(gtx) {
			bk := v.pinboardBookmarks[i]
			v.app.SelectChannel(bk.ChannelID, bk.ChannelName)
		}
	}

	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Pinboard")
	}))

	if len(v.pinboardBookmarks) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No bookmarks yet. Use the bookmark icon on messages to save them here.")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		countText := fmt.Sprintf("%d bookmark", len(v.pinboardBookmarks))
		if len(v.pinboardBookmarks) != 1 {
			countText += "s"
		}
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, countText)
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))

		for i, bk := range v.pinboardBookmarks {
			idx := i
			bookmark := bk
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutPinboardItem(gtx, idx, bookmark)
			}))
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutPinboardItem(gtx layout.Context, idx int, bk store.StoredBookmark) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := gtx.Dp(6)
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
					Rect: bounds,
					NE: rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Row 1: #channel — author — date
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, "#"+bk.ChannelName)
									lbl.Color = ColorAccent
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, bk.AuthorName)
										lbl.Color = UserColor(bk.AuthorName)
										return lbl.Layout(gtx)
									})
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, FormatDateTime(bk.CreatedAt))
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
							)
						}),
						// Row 2: content snippet
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							content := bk.Content
							if len(content) > 100 {
								content = content[:100] + "..."
							}
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(v.app.Theme.Material, content)
								lbl.Color = ColorText
								lbl.MaxLines = 2
								return lbl.Layout(gtx)
							})
						}),
						// Row 3: note (if exists)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if bk.Note == "" {
								return layout.Dimensions{}
							}
							return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, "Note: "+bk.Note)
								lbl.Color = ColorTextDim
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							})
						}),
						// Row 4: actions
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.pinboardJumpBtns[idx], "Jump", ColorAccent)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutSmallBtn(gtx, &v.pinboardDelBtns[idx], "Remove", ColorDanger)
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
}

// layoutVoiceSettings returns FlexChild items for voice settings section.
// layoutVoiceSettings returns FlexChild items for voice settings section.
func (v *SettingsView) layoutVoiceSettings(gtx layout.Context) []layout.FlexChild {
	var items []layout.FlexChild

	// Input device list
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Input Device")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	if len(v.voiceInputDevices) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "No devices found")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}))
	} else {
		for i, dev := range v.voiceInputDevices {
			idx := i
			d := dev
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					selected := v.voiceSelectedInput == d.ID || (v.voiceSelectedInput == "" && d.IsDefault)
					return v.layoutDeviceBtn(gtx, &v.voiceInputBtns[idx], d.Name, selected)
				})
			}))
		}
	}

	// Output device list
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Output Device")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	if len(v.voiceOutputDevices) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "No devices found")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}))
	} else {
		for i, dev := range v.voiceOutputDevices {
			idx := i
			d := dev
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					selected := v.voiceSelectedOutput == d.ID || (v.voiceSelectedOutput == "" && d.IsDefault)
					return v.layoutDeviceBtn(gtx, &v.voiceOutputBtns[idx], d.Name, selected)
				})
			}))
		}
	}

	// Volume sliders
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(100)
					lbl := material.Caption(v.app.Theme.Material, "Mic Volume")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.voiceMicSlider.Layout(gtx, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						pct := int(v.voiceMicSlider.Value / v.voiceMicSlider.Max * 100)
						lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("%d%%", pct))
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(100)
					lbl := material.Caption(v.app.Theme.Material, "Speaker Volume")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return v.voiceSpeakerSlider.Layout(gtx, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						pct := int(v.voiceSpeakerSlider.Value / v.voiceSpeakerSlider.Max * 100)
						lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("%d%%", pct))
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Noise suppression toggle
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		nsEnabled := false
		if c := v.app.Conn(); c != nil && c.Voice != nil {
			nsEnabled = c.Voice.IsNoiseSuppressionEnabled()
		}
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.voiceNoiseSupprBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(18)
						clr := ColorTextDim
						if nsEnabled {
							clr = ColorAccent
						}
						rr := sz / 4
						paint.FillShape(gtx.Ops, clr, clip.RRect{
							Rect: image.Rect(0, 0, sz, sz),
							NE:   rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						if nsEnabled {
							inner := gtx.Dp(8)
							off := (sz - inner) / 2
							paint.FillShape(gtx.Ops, ColorText, clip.Rect{
								Min: image.Pt(off, off),
								Max: image.Pt(off+inner, off+inner),
							}.Op())
						}
						return layout.Dimensions{Size: image.Pt(sz, sz)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, "Noise Suppression")
							lbl.Color = ColorText
							return lbl.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, "(noise gate)")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
				)
			})
		})
	}))

	// Refresh button
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.layoutSmallBtn(gtx, &v.voiceRefreshBtn, "Refresh Devices", ColorAccent)
		})
	}))

	return items
}

func (v *SettingsView) layoutDeviceBtn(gtx layout.Context, btn *widget.Clickable, name string, selected bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		fg := ColorTextDim
		if selected {
			bg = withAlpha(ColorAccent, 40)
			fg = ColorAccent
		} else if btn.Hovered() {
			bg = ColorHover
			fg = ColorText
		}
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
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, name)
					lbl.Color = fg
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *SettingsView) layoutStatusButtons(gtx layout.Context) layout.Dimensions {
	type statusBtn struct {
		btn   *widget.Clickable
		label string
		clr   color.NRGBA
		val   string
	}
	buttons := []statusBtn{
		{&v.statusOnlineBtn, "Online", ColorOnline, ""},
		{&v.statusAwayBtn, "Away", ColorStatusAway, "away"},
		{&v.statusDNDBtn, "DND", ColorStatusDND, "dnd"},
	}

	var currentStatus string
	if conn := v.app.Conn(); conn != nil {
		v.app.mu.RLock()
		currentStatus = conn.UserStatuses[conn.UserID]
		v.app.mu.RUnlock()
	}

	var items []layout.FlexChild
	for _, b := range buttons {
		btn := b
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				selected := currentStatus == btn.val
				return btn.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := ColorInput
					if selected {
						bg = btn.clr
					} else if btn.btn.Hovered() {
						bg = ColorHover
					}
					fg := ColorTextDim
					if selected {
						fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					}
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, btn.label)
								lbl.Color = fg
								return lbl.Layout(gtx)
							})
						},
					)
				})
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
}

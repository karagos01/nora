package ui

import (
	"fmt"
	"image"
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

	"nora-client/api"
)

func (v *SettingsView) doSaveServerSettings() {
	name := v.nameEditor.Text()
	desc := v.descEditor.Text()
	uploadSizeStr := v.uploadSizeEditor.Text()
	gsEnabled := v.gameServersToggle
	swarmEnabled := v.swarmSharingToggle
	go func() {
		if c := v.app.Conn(); c != nil {
			payload := map[string]interface{}{
				"server_name":            name,
				"server_description":     desc,
				"game_servers_enabled":   gsEnabled,
				"swarm_sharing_enabled":  swarmEnabled,
			}
			if mb, err := strconv.Atoi(strings.TrimSpace(uploadSizeStr)); err == nil && mb >= 1 {
				payload["max_upload_size_mb"] = mb
			}
			if err := c.Client.UpdateServerSettings(payload); err != nil {
				log.Printf("UpdateServerSettings: %v", err)
				return
			}
			c.GameServersEnabled = gsEnabled
			v.gameServersOriginal = gsEnabled
			c.SwarmSharingEnabled = swarmEnabled
			v.swarmSharingOriginal = swarmEnabled
			v.origName = name
			v.origDesc = desc
			v.origUploadSize = strings.TrimSpace(uploadSizeStr)
			v.app.mu.Lock()
			c.Name = name
			v.app.mu.Unlock()
			v.app.Window.Invalidate()
		}
	}()
}

// hasServerChanges returns true if there are unsaved changes in server settings.
func (v *SettingsView) hasServerChanges() bool {
	if v.nameEditor.Text() != v.origName {
		return true
	}
	if v.descEditor.Text() != v.origDesc {
		return true
	}
	if strings.TrimSpace(v.uploadSizeEditor.Text()) != v.origUploadSize {
		return true
	}
	if v.gameServersToggle != v.gameServersOriginal {
		return true
	}
	if v.swarmSharingToggle != v.swarmSharingOriginal {
		return true
	}
	return false
}

func (v *SettingsView) layoutServerSection(gtx layout.Context, conn *ServerConnection, isOwner bool) layout.Dimensions {
	if !isOwner {
		return layout.Dimensions{}
	}
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Server Settings")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Server Name")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutEditor(gtx, &v.nameEditor, "Server name")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Description")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutEditor(gtx, &v.descEditor, "Server description")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Max Upload Size (MB)")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutEditor(gtx, &v.uploadSizeEditor, "10")
	}))
	// Game Servers toggle
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.gameServersBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				check := "[ ]"
				if v.gameServersToggle {
					check = "[x]"
				}
				lbl := material.Body2(v.app.Theme.Material, check+" Enable Game Servers")
				if v.gameServersToggle {
					lbl.Color = ColorAccent
				} else {
					lbl.Color = ColorTextDim
				}
				return lbl.Layout(gtx)
			})
		})
	}))
	// Docker status + install (only if enabling game servers and Docker is not available)
	if v.gameServersToggle && !v.dockerAvailable {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "Docker is not installed on the server. It is required to run game servers.")
						lbl.Color = ColorWarning
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.dockerInstalling {
							lbl := material.Caption(v.app.Theme.Material, "Installing Docker...")
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}
						if v.dockerInstallError != "" {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, "Install failed: "+v.dockerInstallError)
									lbl.Color = ColorDanger
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.installDockerBtn, "Retry Install Docker", ColorAccent)
									})
								}),
							)
						}
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.installDockerBtn, "Install Docker", ColorAccent)
						})
					}),
				)
			})
		}))
	}
	// Swarm Sharing toggle
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.swarmSharingBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				check := "[ ]"
				if v.swarmSharingToggle {
					check = "[x]"
				}
				lbl := material.Body2(v.app.Theme.Material, check+" Enable Swarm Sharing")
				if v.swarmSharingToggle {
					lbl.Color = ColorAccent
				} else {
					lbl.Color = ColorTextDim
				}
				return lbl.Layout(gtx)
			})
		})
	}))

	// Server Icon
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Server Icon")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutAccentBtn(gtx, &v.iconPickBtn, "Choose File")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.iconFilePath == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					name := filepath.Base(v.iconFilePath)
					lbl := material.Caption(v.app.Theme.Material, name)
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if v.iconFilePath == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutAccentBtn(gtx, &v.iconUploadBtn, "Upload")
				})
			}),
		)
	}))
	if conn != nil && conn.IconURL != "" {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutSmallBtn(gtx, &v.iconDeleteBtn, "Remove Icon", ColorDanger)
			})
		}))
	}
	if v.iconError != "" {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, v.iconError)
				lbl.Color = ColorDanger
				return lbl.Layout(gtx)
			})
		}))
	}

	// Save button — only if there are unsaved changes
	if v.hasServerChanges() {
		gsChanged := v.gameServersToggle != v.gameServersOriginal
		saveLabel := "Save"
		if gsChanged {
			saveLabel = "Save & Restart"
		}
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutAccentBtn(gtx, &v.saveSettingsBtn, saveLabel)
			})
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutRolesSection(gtx layout.Context, conn *ServerConnection, roles []api.Role, isOwner bool) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Roles")
	}))
	if len(roles) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No roles")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		for i, role := range roles {
			idx := i
			r := role
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutRoleItem(gtx, idx, r, len(roles))
			}))
		}
	}
	if isOwner {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return v.layoutEditor(gtx, &v.newRoleEditor, "New role name")
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutAccentBtn(gtx, &v.createRoleBtn, "Add")
						})
					}),
				)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutInvitesSection(gtx layout.Context, conn *ServerConnection, invites []api.Invite) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return v.layoutSection(gtx, "Invites")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutAccentBtn(gtx, &v.createInviteBtn, "Create")
			}),
		)
	}))
	if len(invites) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No active invites")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		for i, inv := range invites {
			idx := i
			invite := inv
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return v.layoutInviteRow(gtx, idx, invite)
			}))
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutEmojisSection(gtx layout.Context, conn *ServerConnection, emojis []api.CustomEmoji) layout.Dimensions {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Server Emojis")
	}))
	if len(emojis) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No custom emojis")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		for i, emoji := range emojis {
			idx := i
			e := emoji
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(v.app.Theme.Material, ":"+e.Name+":")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, FormatBytes(e.Size))
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.emojiDelBtns[idx], "Delete", ColorDanger)
						}),
					)
				})
			}))
		}
	}
	// Upload form
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return v.layoutEditor(gtx, &v.emojiNameEditor, "Emoji name")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutAccentBtn(gtx, &v.emojiPickBtn, "File...")
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								label := "Upload"
								if v.emojiUploading {
									label = "..."
								}
								return v.layoutAccentBtn(gtx, &v.emojiUploadBtn, label)
							})
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if v.emojiFilePath == "" && v.emojiError == "" {
						return layout.Dimensions{}
					}
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						if v.emojiError != "" {
							lbl := material.Caption(v.app.Theme.Material, v.emojiError)
							lbl.Color = ColorDanger
							return lbl.Layout(gtx)
						}
						lbl := material.Caption(v.app.Theme.Material, "Selected: "+filepath.Base(v.emojiFilePath))
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutBansSection(gtx layout.Context) layout.Dimensions {
	// Sub-tab clicks
	tabNames := []string{"Bans", "Device Bans", "Invite Chain", "Quarantine", "Approvals", "Reports"}
	for i := range v.bansSubBtns {
		if v.bansSubBtns[i].Clicked(gtx) {
			v.bansSubTab = i
			// Lazy load data for new tab
			switch i {
			case 1:
				v.deviceBansLoaded = false
			case 2:
				v.inviteChainLoaded = false
			case 3:
				v.quarantineLoaded = false
			case 4:
				v.approvalsLoaded = false
			case 5:
				v.reportsLoaded = false
			}
		}
	}

	// Lazy-load data based on active tab
	v.lazyLoadBansSubdata()

	var items []layout.FlexChild

	// Tab bar
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx, func() []layout.FlexChild {
				var tabs []layout.FlexChild
				for i, name := range tabNames {
					idx := i
					n := name
					tabs = append(tabs, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							selected := v.bansSubTab == idx
							bg := ColorInput
							fg := ColorTextDim
							if selected {
								bg = ColorAccent
								fg = ColorWhite
							}
							return v.bansSubBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Background{}.Layout(gtx,
									func(gtx layout.Context) layout.Dimensions {
										bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
										rr := gtx.Dp(4)
										paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: bounds, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
										return layout.Dimensions{Size: bounds.Max}
									},
									func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, n)
											lbl.Color = fg
											return lbl.Layout(gtx)
										})
									},
								)
							})
						})
					}))
				}
				return tabs
			}()...)
		})
	}))

	// Content based on active tab
	switch v.bansSubTab {
	case 0:
		items = append(items, v.layoutBansList(gtx)...)
	case 1:
		items = append(items, v.layoutDeviceBansList(gtx)...)
	case 2:
		items = append(items, v.layoutInviteChainList(gtx)...)
	case 3:
		items = append(items, v.layoutQuarantineList(gtx)...)
	case 4:
		items = append(items, v.layoutApprovalsList(gtx)...)
	case 5:
		items = append(items, v.layoutReportsList(gtx)...)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) lazyLoadBansSubdata() {
	c := v.app.Conn()
	if c == nil {
		return
	}
	switch v.bansSubTab {
	case 1:
		if !v.deviceBansLoaded {
			v.deviceBansLoaded = true
			go func() {
				bans, err := c.Client.GetDeviceBans()
				if err != nil {
					log.Printf("GetDeviceBans: %v", err)
					return
				}
				v.deviceBans = bans
				if len(v.unbanDeviceBtns) < len(bans) {
					v.unbanDeviceBtns = make([]widget.Clickable, len(bans)+5)
				}
				v.app.Window.Invalidate()
			}()
		}
	case 2:
		if !v.inviteChainLoaded {
			v.inviteChainLoaded = true
			go func() {
				chain, err := c.Client.GetInviteChain()
				if err != nil {
					log.Printf("GetInviteChain: %v", err)
					return
				}
				v.inviteChain = chain
				v.app.Window.Invalidate()
			}()
		}
	case 3:
		if !v.quarantineLoaded {
			v.quarantineLoaded = true
			go func() {
				entries, err := c.Client.GetQuarantine()
				if err != nil {
					log.Printf("GetQuarantine: %v", err)
					return
				}
				v.quarantineEntries = entries
				if len(v.quarantineAppBtns) < len(entries) {
					v.quarantineAppBtns = make([]widget.Clickable, len(entries)+5)
					v.quarantineDelBtns = make([]widget.Clickable, len(entries)+5)
				}
				v.app.Window.Invalidate()
			}()
		}
	case 4:
		if !v.approvalsLoaded {
			v.approvalsLoaded = true
			go func() {
				approvals, err := c.Client.GetPendingApprovals()
				if err != nil {
					log.Printf("GetPendingApprovals: %v", err)
					return
				}
				v.pendingApprovals = approvals
				if len(v.approveUserBtns) < len(approvals) {
					v.approveUserBtns = make([]widget.Clickable, len(approvals)+5)
					v.rejectUserBtns = make([]widget.Clickable, len(approvals)+5)
				}
				v.app.Window.Invalidate()
			}()
		}
	case 5:
		if !v.reportsLoaded {
			v.reportsLoaded = true
			go func() {
				reports, err := c.Client.GetReports()
				if err != nil {
					log.Printf("GetReports: %v", err)
					return
				}
				v.pendingReports = reports
				if len(v.reviewBtns) < len(reports) {
					v.reviewBtns = make([]widget.Clickable, len(reports)+5)
					v.dismissBtns = make([]widget.Clickable, len(reports)+5)
				}
				v.app.Window.Invalidate()
			}()
		}
	}
}

func (v *SettingsView) layoutBansList(gtx layout.Context) []layout.FlexChild {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Banned Users")
	}))
	if len(v.bans) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No banned users")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		for i, b := range v.bans {
			idx := i
			ban := b
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									text := ban.Username
									if ban.Reason != "" {
										text += " (" + ban.Reason + ")"
									}
									// Expiration
									if ban.ExpiresAt != nil {
										remaining := time.Until(*ban.ExpiresAt)
										if remaining > 0 {
											text += fmt.Sprintf(" [%s left]", formatBanDuration(remaining))
										} else {
											text += " [expired]"
										}
									} else {
										text += " [permanent]"
									}
									lbl := material.Body2(v.app.Theme.Material, text)
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return v.layoutSmallBtn(gtx, &v.unbanBtns[idx], "Unban", ColorAccent)
								}),
							)
						}),
						// Invited by + device count
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							var info string
							if ban.InvitedBy != "" {
								info += "Invited by: " + ban.InvitedBy
							}
							if ban.DeviceCount > 0 {
								if info != "" {
									info += "  |  "
								}
								info += fmt.Sprintf("Devices: %d", ban.DeviceCount)
							}
							if info == "" {
								return layout.Dimensions{}
							}
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, info)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
					)
				})
			}))
		}
	}
	return items
}

func (v *SettingsView) layoutDeviceBansList(gtx layout.Context) []layout.FlexChild {
	// Handle unban device clicks
	for i, b := range v.deviceBans {
		if i < len(v.unbanDeviceBtns) && v.unbanDeviceBtns[i].Clicked(gtx) {
			banID := b.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.DeleteDeviceBan(banID); err != nil {
						log.Printf("DeleteDeviceBan: %v", err)
						return
					}
					bans, _ := c.Client.GetDeviceBans()
					v.deviceBans = bans
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Device Bans")
	}))
	if len(v.deviceBans) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No device bans")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		if len(v.unbanDeviceBtns) < len(v.deviceBans) {
			v.unbanDeviceBtns = make([]widget.Clickable, len(v.deviceBans)+5)
		}
		for i, b := range v.deviceBans {
			idx := i
			ban := b
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text := "Device"
							if ban.RelatedUsername != "" {
								text += " (" + ban.RelatedUsername + ")"
							}
							if ban.Reason != "" {
								text += " - " + ban.Reason
							}
							lbl := material.Body2(v.app.Theme.Material, text)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.unbanDeviceBtns[idx], "Unban", ColorAccent)
						}),
					)
				})
			}))
		}
	}
	return items
}

func (v *SettingsView) layoutInviteChainList(gtx layout.Context) []layout.FlexChild {
	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Invite Chain")
	}))
	if len(v.inviteChain) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No invite chain data")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		// Display tree
		for _, node := range v.inviteChain {
			items = append(items, v.layoutInviteChainNode(node, 0)...)
		}
	}
	return items
}

func (v *SettingsView) layoutInviteChainNode(node api.InviteChainNode, depth int) []layout.FlexChild {
	var items []layout.FlexChild
	n := node
	d := depth
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(float32(d * 16))}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			text := n.Username
			if n.IsBanned {
				text += " [BANNED]"
			}
			fg := ColorTextDim
			if n.IsBanned {
				fg = ColorDanger
			}
			lbl := material.Body2(v.app.Theme.Material, text)
			lbl.Color = fg
			return lbl.Layout(gtx)
		})
	}))
	for _, child := range node.Children {
		items = append(items, v.layoutInviteChainNode(child, depth+1)...)
	}
	return items
}

func (v *SettingsView) layoutQuarantineList(gtx layout.Context) []layout.FlexChild {
	// Handle clicks
	for i, e := range v.quarantineEntries {
		if i < len(v.quarantineAppBtns) && v.quarantineAppBtns[i].Clicked(gtx) {
			userID := e.UserID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.ApproveQuarantine(userID); err != nil {
						log.Printf("ApproveQuarantine: %v", err)
						return
					}
					entries, _ := c.Client.GetQuarantine()
					v.quarantineEntries = entries
					v.app.Window.Invalidate()
				}
			}()
		}
		if i < len(v.quarantineDelBtns) && v.quarantineDelBtns[i].Clicked(gtx) {
			userID := e.UserID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.RemoveQuarantine(userID); err != nil {
						log.Printf("RemoveQuarantine: %v", err)
						return
					}
					entries, _ := c.Client.GetQuarantine()
					v.quarantineEntries = entries
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Quarantine")
	}))
	if len(v.quarantineEntries) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No users in quarantine")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		if len(v.quarantineAppBtns) < len(v.quarantineEntries) {
			v.quarantineAppBtns = make([]widget.Clickable, len(v.quarantineEntries)+5)
			v.quarantineDelBtns = make([]widget.Clickable, len(v.quarantineEntries)+5)
		}
		for i, e := range v.quarantineEntries {
			idx := i
			entry := e
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text := entry.Username
							if entry.EndsAt != nil {
								remaining := time.Until(*entry.EndsAt)
								if remaining > 0 {
									text += fmt.Sprintf(" [%s left]", formatBanDuration(remaining))
								} else {
									text += " [ended]"
								}
							} else {
								text += " [manual]"
							}
							lbl := material.Body2(v.app.Theme.Material, text)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.quarantineAppBtns[idx], "Approve", ColorAccent)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutSmallBtn(gtx, &v.quarantineDelBtns[idx], "Remove", ColorDanger)
							})
						}),
					)
				})
			}))
		}
	}
	return items
}

func (v *SettingsView) layoutApprovalsList(gtx layout.Context) []layout.FlexChild {
	// Handle clicks
	for i, a := range v.pendingApprovals {
		if i < len(v.approveUserBtns) && v.approveUserBtns[i].Clicked(gtx) {
			userID := a.UserID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.ApproveUser(userID); err != nil {
						log.Printf("ApproveUser: %v", err)
						return
					}
					approvals, _ := c.Client.GetPendingApprovals()
					v.pendingApprovals = approvals
					v.app.Window.Invalidate()
				}
			}()
		}
		if i < len(v.rejectUserBtns) && v.rejectUserBtns[i].Clicked(gtx) {
			userID := a.UserID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.RejectUser(userID); err != nil {
						log.Printf("RejectUser: %v", err)
						return
					}
					approvals, _ := c.Client.GetPendingApprovals()
					v.pendingApprovals = approvals
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	var items []layout.FlexChild
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Pending Approvals")
	}))
	if len(v.pendingApprovals) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No pending approvals")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
	} else {
		if len(v.approveUserBtns) < len(v.pendingApprovals) {
			v.approveUserBtns = make([]widget.Clickable, len(v.pendingApprovals)+5)
			v.rejectUserBtns = make([]widget.Clickable, len(v.pendingApprovals)+5)
		}
		for i, a := range v.pendingApprovals {
			idx := i
			approval := a
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text := approval.RequestedUsername
							if approval.InviterUsername != "" {
								text += " (invited by " + approval.InviterUsername + ")"
							}
							lbl := material.Body2(v.app.Theme.Material, text)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.approveUserBtns[idx], "Approve", ColorAccent)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutSmallBtn(gtx, &v.rejectUserBtns[idx], "Reject", ColorDanger)
							})
						}),
					)
				})
			}))
		}
	}
	return items
}

func formatBanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (v *SettingsView) layoutDisconnectSection(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSection(gtx, "Disconnect")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutDangerBtn(gtx, &v.disconnectBtn, "Disconnect from Server")
			})
		}),
	)
}

func (v *SettingsView) layoutInviteRow(gtx layout.Context, idx int, invite api.Invite) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, invite.Link)
									lbl.Color = ColorAccent
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									usesText := fmt.Sprintf("%d uses", invite.Uses)
									if invite.MaxUses > 0 {
										usesText = fmt.Sprintf("%d / %d uses", invite.Uses, invite.MaxUses)
									}
									lbl := material.Caption(v.app.Theme.Material, usesText)
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutSmallBtn(gtx, &v.inviteQRBtns[idx], "QR", ColorAccent)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutSmallIconBtn(gtx, &v.inviteCopyBtns[idx], IconCopy, ColorAccent)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallBtn(gtx, &v.inviteDelBtns[idx], "Delete", ColorDanger)
						}),
					)
				})
			},
		)
	})
}

func (v *SettingsView) reloadInvites(conn *ServerConnection) {
	invites, err := conn.Client.GetInvites()
	if err == nil {
		v.app.mu.Lock()
		conn.Invites = invites
		v.app.mu.Unlock()
	}
	v.app.Window.Invalidate()
}

func (v *SettingsView) permissionDefs() []permDef {
	return []permDef{
		{"Send Messages", api.PermSendMessages},
		{"Read", api.PermRead},
		{"Manage Messages", api.PermManageMessages},
		{"Manage Channels", api.PermManageChannels},
		{"Manage Roles", api.PermManageRoles},
		{"Manage Invites", api.PermManageInvites},
		{"Kick", api.PermKick},
		{"Ban", api.PermBan},
		{"Upload", api.PermUpload},
		{"Admin", api.PermAdmin},
	}
}

func (v *SettingsView) layoutRoleItem(gtx layout.Context, idx int, role api.Role, roleCount int) layout.Dimensions {
	expanded := v.expandedRole == idx

	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var items []layout.FlexChild

					// Header row (name + delete)
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.roleBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									arrow := "> "
									if expanded {
										arrow = "v "
									}
									lbl := material.Body2(v.app.Theme.Material, arrow+role.Name)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if idx == 0 || role.ID == "everyone" {
										return layout.Dimensions{}
									}
									return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.roleUpBtns[idx], "Up", ColorAccent)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if idx >= roleCount-1 || role.ID == "everyone" {
										return layout.Dimensions{}
									}
									return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.roleDownBtns[idx], "Down", ColorAccent)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("pos: %d", role.Position))
									lbl.Color = ColorTextDim
									return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return v.layoutSmallBtn(gtx, &v.roleDelBtns[idx], "Delete", ColorDanger)
									})
								}),
							)
						})
					}))

					if expanded {
						// Name editor
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutEditor(gtx, &v.roleNameEditors[idx], "Role name")
							})
						}))

						// Color editor (hex, e.g. #ff0000)
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return v.layoutEditor(gtx, &v.roleColorEditors[idx], "Color (#ff0000)")
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										// Color preview — small square
										hexStr := v.roleColorEditors[idx].Text()
										if hexStr == "" {
											hexStr = role.Color
										}
										clr, ok := ParseHexColor(hexStr)
										if !ok || hexStr == "" {
											return layout.Dimensions{}
										}
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											sz := gtx.Dp(20)
											rr := gtx.Dp(4)
											paint.FillShape(gtx.Ops, clr, clip.RRect{
												Rect: image.Rect(0, 0, sz, sz),
												NE:   rr, NW: rr, SE: rr, SW: rr,
											}.Op(gtx.Ops))
											return layout.Dimensions{Size: image.Pt(sz, sz)}
										})
									}),
								)
							})
						}))

						// Permission checkboxes
						perms := v.permissionDefs()
						for p, perm := range perms {
							pi := p
							pd := perm
							has := role.Permissions&pd.value != 0
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return v.rolePermBtns[idx][pi].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												check := "[ ] "
												if has {
													check = "[x] "
												}
												lbl := material.Body2(v.app.Theme.Material, check+pd.name)
												if has {
													lbl.Color = ColorAccent
												} else {
													lbl.Color = ColorTextDim
												}
												return lbl.Layout(gtx)
											}),
										)
									})
								})
							}))
						}

						// Save button
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.layoutAccentBtn(gtx, &v.roleSaveBtns[idx], "Save")
							})
						}))
					}

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
				})
			},
		)
	})
}

func (v *SettingsView) reloadRoles(conn *ServerConnection) {
	roles, err := conn.Client.GetRoles()
	if err == nil {
		v.app.mu.Lock()
		conn.Roles = roles
		v.app.mu.Unlock()
	}
	v.app.Window.Invalidate()
}

func (v *SettingsView) hasServerDiskChanges() bool {
	return strings.TrimSpace(v.serverDiskMaxMBEditor.Text()) != v.serverDiskOrigMaxMB ||
		strings.TrimSpace(v.serverDiskHistoryEditor.Text()) != v.serverDiskOrigHistory
}

func (v *SettingsView) hasAutomodChanges() bool {
	if v.automodToggle != v.automodOriginal {
		return true
	}
	if v.automodWordEditor.Text() != v.automodOrigWords {
		return true
	}
	if strings.TrimSpace(v.automodSpamMaxEditor.Text()) != v.automodOrigSpamMax {
		return true
	}
	if strings.TrimSpace(v.automodSpamWindowEditor.Text()) != v.automodOrigSpamWindow {
		return true
	}
	if strings.TrimSpace(v.automodSpamTimeoutEditor.Text()) != v.automodOrigSpamTimeout {
		return true
	}
	return false
}

func (v *SettingsView) layoutAutomodSection(gtx layout.Context) layout.Dimensions {
	// Toggle click
	if v.automodBtn.Clicked(gtx) {
		v.automodToggle = !v.automodToggle
	}

	// Save
	if v.automodSaveBtn.Clicked(gtx) {
		v.doSaveAutomod()
	}

	var items []layout.FlexChild

	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Auto-Moderation")
	}))

	if !v.automodLoaded {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(v.app.Theme.Material, "Loading...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
	}

	// Enable toggle
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return v.automodBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				check := "[ ]"
				if v.automodToggle {
					check = "[x]"
				}
				lbl := material.Body2(v.app.Theme.Material, check+" Enable Auto-Moderation")
				if v.automodToggle {
					lbl.Color = ColorAccent
				} else {
					lbl.Color = ColorTextDim
				}
				return lbl.Layout(gtx)
			})
		})
	}))

	if v.automodToggle {
		// Word Filter section
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutSection(gtx, "Word Filter")
			})
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Blocked words (one per line)")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			v.automodWordEditor.Submit = false
			gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(150))
			return v.layoutEditor(gtx, &v.automodWordEditor, "Enter words...")
		}))

		// Spam Detection section
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutSection(gtx, "Spam Detection")
			})
		}))

		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Max messages in window")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutEditor(gtx, &v.automodSpamMaxEditor, "5")
		}))

		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Time window (seconds)")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutEditor(gtx, &v.automodSpamWindowEditor, "10")
		}))

		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Timeout duration (seconds)")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutEditor(gtx, &v.automodSpamTimeoutEditor, "300")
		}))
	}

	// Save button
	if v.hasAutomodChanges() {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutAccentBtn(gtx, &v.automodSaveBtn, "Save")
			})
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) doSaveAutomod() {
	enabled := v.automodToggle
	wordsText := v.automodWordEditor.Text()
	spamMaxStr := strings.TrimSpace(v.automodSpamMaxEditor.Text())
	spamWindowStr := strings.TrimSpace(v.automodSpamWindowEditor.Text())
	spamTimeoutStr := strings.TrimSpace(v.automodSpamTimeoutEditor.Text())

	// Split words by lines, filter out empty ones
	var words []string
	for _, line := range strings.Split(wordsText, "\n") {
		w := strings.TrimSpace(line)
		if w != "" {
			words = append(words, w)
		}
	}

	go func() {
		c := v.app.Conn()
		if c == nil {
			return
		}
		payload := map[string]interface{}{
			"automod_enabled":     enabled,
			"automod_word_filter": words,
		}
		if n, err := strconv.Atoi(spamMaxStr); err == nil && n >= 2 {
			payload["automod_spam_max_messages"] = n
		}
		if n, err := strconv.Atoi(spamWindowStr); err == nil && n >= 5 {
			payload["automod_spam_window_seconds"] = n
		}
		if n, err := strconv.Atoi(spamTimeoutStr); err == nil && n >= 60 {
			payload["automod_spam_timeout_seconds"] = n
		}
		if err := c.Client.UpdateServerSettings(payload); err != nil {
			log.Printf("UpdateServerSettings (automod): %v", err)
			return
		}
		v.automodOriginal = enabled
		v.automodOrigWords = wordsText
		v.automodOrigSpamMax = spamMaxStr
		v.automodOrigSpamWindow = spamWindowStr
		v.automodOrigSpamTimeout = spamTimeoutStr
		c.AutoModEnabled = enabled
		v.app.Window.Invalidate()
	}()
}

func (v *SettingsView) layoutServerDiskSection(gtx layout.Context) layout.Dimensions {
	a := v.app

	// Auto-scan on first display
	if !v.serverDiskScanned && !v.serverDiskScanning {
		v.serverDiskScanning = true
		go func() {
			conn := a.Conn()
			if conn == nil {
				v.serverDiskScanning = false
				return
			}
			info, err := conn.Client.GetServerStorage()
			if err != nil {
				log.Printf("GetServerStorage: %v", err)
				v.serverDiskScanning = false
				a.Window.Invalidate()
				return
			}
			v.serverDiskInfo = info
			v.serverDiskScanned = true
			v.serverDiskScanning = false

			mb := strconv.Itoa(info.MaxMB)
			hl := strconv.Itoa(info.ChannelHistoryLimit)
			v.serverDiskMaxMBEditor.SetText(mb)
			v.serverDiskHistoryEditor.SetText(hl)
			v.serverDiskOrigMaxMB = mb
			v.serverDiskOrigHistory = hl

			a.Window.Invalidate()
		}()
	}

	// Rescan click
	if v.serverDiskRescanBtn.Clicked(gtx) {
		v.serverDiskScanned = false
	}

	// Save click
	if v.serverDiskSaveBtn.Clicked(gtx) {
		mbStr := strings.TrimSpace(v.serverDiskMaxMBEditor.Text())
		hlStr := strings.TrimSpace(v.serverDiskHistoryEditor.Text())
		go func() {
			conn := a.Conn()
			if conn == nil {
				return
			}
			var maxMB *int
			var histLimit *int
			if mb, err := strconv.Atoi(mbStr); err == nil && mb >= 0 {
				maxMB = &mb
			}
			if hl, err := strconv.Atoi(hlStr); err == nil && hl >= 0 {
				histLimit = &hl
			}
			if err := conn.Client.UpdateServerStorage(maxMB, histLimit); err != nil {
				log.Printf("UpdateServerStorage: %v", err)
				return
			}
			v.serverDiskOrigMaxMB = mbStr
			v.serverDiskOrigHistory = hlStr
			// Rescan after saving
			v.serverDiskScanned = false
			a.Window.Invalidate()
		}()
	}

	// Trim History click
	if v.serverDiskTrimBtn.Clicked(gtx) {
		a.ConfirmDlg.ShowConfirm("Trim Channel History", "Trim messages exceeding the channel history limit on the server?\nThis cannot be undone.", func() {
			hlStr := strings.TrimSpace(v.serverDiskHistoryEditor.Text())
			go func() {
				conn := a.Conn()
				if conn == nil {
					return
				}
				hl := 0
				if n, err := strconv.Atoi(hlStr); err == nil && n > 0 {
					hl = n
				}
				if hl <= 0 {
					return
				}
				if err := conn.Client.UpdateServerStorage(nil, &hl); err != nil {
					log.Printf("TrimHistory: %v", err)
					return
				}
				v.serverDiskScanned = false
				a.Window.Invalidate()
			}()
		})
	}

	var items []layout.FlexChild

	// Header
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.layoutSection(gtx, "Disk Usage")
	}))
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Server disk usage breakdown.")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}))

	if v.serverDiskScanning {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Scanning...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}))
	} else if v.serverDiskScanned && v.serverDiskInfo != nil {
		info := v.serverDiskInfo

		type row struct {
			label string
			value string
			bold  bool
		}
		rows := []row{
			{"Database", FormatBytes(info.DBBytes), false},
			{"Attachments", FormatBytes(info.AttachmentsBytes), false},
			{"Emojis", FormatBytes(info.EmojisBytes), false},
			{"Avatars", FormatBytes(info.AvatarsBytes), false},
			{"Server Icon", FormatBytes(info.IconBytes), false},
			{"Uploads Total", FormatBytes(info.UploadsBytes), true},
			{"Total Files", strconv.Itoa(info.TotalFiles), false},
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

	// Storage Limit editor
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(130)
					lbl := material.Caption(v.app.Theme.Material, "Storage Limit")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(80)
					gtx.Constraints.Min.X = gtx.Dp(80)
					return v.layoutEditor(gtx, &v.serverDiskMaxMBEditor, "0")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "MB (0 = unlimited)")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Channel History Limit editor
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(130)
					lbl := material.Caption(v.app.Theme.Material, "History Limit")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(80)
					gtx.Constraints.Min.X = gtx.Dp(80)
					return v.layoutEditor(gtx, &v.serverDiskHistoryEditor, "0")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "messages per channel (0 = unlimited)")
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	}))

	// Save button (only if there are changes)
	if v.hasServerDiskChanges() {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutAccentBtn(gtx, &v.serverDiskSaveBtn, "Save")
			})
		}))
	}

	// Separator
	items = append(items, v.layoutDivider())

	// Maintenance
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(v.app.Theme.Material, "Maintenance")
			lbl.Color = ColorText
			lbl.Font.Weight = 600
			return lbl.Layout(gtx)
		})
	}))

	// Trim History
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutSmallBtn(gtx, &v.serverDiskTrimBtn, "Trim History", ColorDanger)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, "Delete messages exceeding the history limit")
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
			return v.layoutSmallBtn(gtx, &v.serverDiskRescanBtn, "Rescan", ColorAccent)
		})
	}))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

func (v *SettingsView) layoutBackupSection(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}
	th := v.app.Theme.Material

	// Load backup info if not yet loaded
	if !v.backupInfoLoaded && !v.backupInfoLoading {
		v.backupInfoLoading = true
		go func() {
			info, err := conn.Client.BackupInfo()
			if err != nil {
				v.backupError = err.Error()
			} else {
				v.backupInfo = info
			}
			v.backupInfoLoaded = true
			v.backupInfoLoading = false
			v.app.Window.Invalidate()
		}()
	}

	// Click handling
	if v.backupBtn.Clicked(gtx) {
		v.backupError = ""
		v.backupSuccess = ""
		go func() {
			savePath := saveFileDialog("nora-backup.db")
			if savePath == "" {
				return
			}
			if err := conn.Client.BackupDownload(savePath); err != nil {
				v.backupError = err.Error()
			} else {
				v.backupSuccess = "Backup saved to " + savePath
			}
			v.app.Window.Invalidate()
		}()
	}
	if v.restoreBtn.Clicked(gtx) {
		v.backupError = ""
		v.backupSuccess = ""
		go func() {
			filePath := openFileDialog()
			if filePath == "" {
				return
			}
			v.restoreFilePath = filePath
			v.app.ConfirmDlg.ShowWithText("Restore Database", "This will replace the entire database. The server should be restarted after restore. Continue?", "Restore", func() {
				go func() {
					if err := conn.Client.RestoreDatabase(v.restoreFilePath); err != nil {
						v.backupError = err.Error()
					} else {
						v.backupSuccess = "Database restored. Please restart the server."
					}
					v.app.Window.Invalidate()
				}()
			})
			v.app.Window.Invalidate()
		}()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutSection(gtx, "Database Backup")
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

		// Info
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.backupInfo == nil {
				if v.backupInfoLoading {
					lbl := material.Caption(th, "Loading...")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				}
				return layout.Dimensions{}
			}
			info := v.backupInfo
			sizeMB := float64(info.DatabaseSize) / 1024 / 1024
			text := fmt.Sprintf("Size: %.1f MB | Users: %d | Messages: %d | Channels: %d", sizeMB, info.Users, info.Messages, info.Channels)
			lbl := material.Caption(th, text)
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

		// Buttons
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.backupBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutPillBtn(gtx, th, "Create Backup", ColorAccent, ColorWhite)
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.restoreBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutPillBtn(gtx, th, "Restore Backup", ColorDanger, ColorWhite)
					})
				}),
			)
		}),

		// Error/success messages
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.backupError != "" {
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(th, v.backupError)
					lbl.Color = ColorDanger
					return lbl.Layout(gtx)
				})
			}
			if v.backupSuccess != "" {
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(th, v.backupSuccess)
					lbl.Color = ColorOnline
					return lbl.Layout(gtx)
				})
			}
			return layout.Dimensions{}
		}),
	)
}

func (v *SettingsView) layoutReportsList(gtx layout.Context) []layout.FlexChild {
	var items []layout.FlexChild

	// Handle review/dismiss clicks
	for i, r := range v.pendingReports {
		if i < len(v.reviewBtns) && v.reviewBtns[i].Clicked(gtx) {
			rid := r.ID
			go func() {
				if conn := v.app.Conn(); conn != nil {
					conn.Client.ReviewReport(rid, "reviewed")
					v.reportsLoaded = false
					v.app.Window.Invalidate()
				}
			}()
		}
		if i < len(v.dismissBtns) && v.dismissBtns[i].Clicked(gtx) {
			rid := r.ID
			go func() {
				if conn := v.app.Conn(); conn != nil {
					conn.Client.ReviewReport(rid, "dismissed")
					v.reportsLoaded = false
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	if len(v.pendingReports) == 0 {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(v.app.Theme.Material, "No pending reports")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			})
		}))
		return items
	}

	for i, r := range v.pendingReports {
		idx := i
		report := r
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Constraints.Min
						rr := gtx.Dp(6)
						paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
							Rect: image.Rect(0, 0, sz.X, sz.Y),
							NE: rr, NW: rr, SE: rr, SW: rr,
						}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz}
					}),
					layout.Stacked(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Reporter → Target
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									text := report.ReporterName + " reported " + report.TargetName
									lbl := material.Body2(v.app.Theme.Material, text)
									lbl.Color = ColorText
									return lbl.Layout(gtx)
								}),
								// Reason
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if report.Reason == "" {
										return layout.Dimensions{}
									}
									return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, "Reason: "+report.Reason)
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Time
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, report.CreatedAt.Local().Format("2006-01-02 15:04"))
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									})
								}),
								// Buttons
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												btn := material.Button(v.app.Theme.Material, &v.reviewBtns[idx], "Take Action")
												btn.Background = ColorDanger
												btn.Color = ColorWhite
												btn.CornerRadius = unit.Dp(4)
												btn.TextSize = v.app.Theme.Sp(12)
												btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}
												return btn.Layout(gtx)
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													btn := material.Button(v.app.Theme.Material, &v.dismissBtns[idx], "Dismiss")
													btn.Background = ColorInput
													btn.Color = ColorText
													btn.CornerRadius = unit.Dp(4)
													btn.TextSize = v.app.Theme.Sp(12)
													btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}
													return btn.Layout(gtx)
												})
											}),
										)
									})
								}),
							)
						})
					}),
				)
			})
		}))
	}

	return items
}

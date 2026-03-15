package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/crypto"
	"nora-client/store"
)

// Kategorie settings sidebar
const (
	settingsCatProfile  = 0
	settingsCatPassword = 1
	settingsCatVoice    = 2
	settingsCatBlocked  = 3
	settingsCatServer   = 4
	settingsCatRoles    = 5
	settingsCatInvites  = 6
	settingsCatEmojis   = 7
	settingsCatBans     = 8
	settingsCatDisconnect = 9
	settingsCatNotify    = 10
	settingsCatStorage   = 11
	settingsCatServerDisk = 12
	settingsCatAppearance = 13
	settingsCatAutomod   = 14
	settingsCatPinboard  = 15
	settingsCatBackup    = 16
	settingsCatTunnels   = 17
)

type SettingsView struct {
	app      *App
	list     widget.List
	activeCategory int
	catBtns  [18]widget.Clickable

	// Blocked users
	unblockBtns []widget.Clickable

	// Invites
	createInviteBtn widget.Clickable
	inviteDelBtns   []widget.Clickable
	inviteCopyBtns  []widget.Clickable
	inviteQRBtns    []widget.Clickable

	// Server settings (owner only)
	nameEditor        widget.Editor
	descEditor        widget.Editor
	uploadSizeEditor  widget.Editor
	saveSettingsBtn   widget.Clickable
	settingsLoaded    bool
	// Originální hodnoty pro detekci změn
	origName       string
	origDesc       string
	origUploadSize string

	// Roles
	roleBtns        []widget.Clickable
	roleDelBtns     []widget.Clickable
	roleUpBtns      []widget.Clickable
	roleDownBtns    []widget.Clickable
	rolePermBtns    [][]widget.Clickable // [roleIdx][permIdx]
	roleSaveBtns    []widget.Clickable
	roleNameEditors  []widget.Editor
	roleColorEditors []widget.Editor
	createRoleBtn   widget.Clickable
	newRoleEditor   widget.Editor
	expandedRole    int // index of expanded role, -1 = none

	// Display name
	displayNameEditor   widget.Editor
	saveDisplayNameBtn  widget.Clickable
	displayNameLoaded   bool
	displayNameSaving   bool
	displayNameError    string
	displayNameSuccess  bool

	// Custom status
	statusOnlineBtn  widget.Clickable
	statusAwayBtn    widget.Clickable
	statusDNDBtn     widget.Clickable
	statusTextEditor widget.Editor
	saveStatusBtn    widget.Clickable
	statusSaving     bool
	statusLoaded     bool

	// Copy buttons
	copyPubKey widget.Clickable

	// Emojis
	emojiNameEditor widget.Editor
	emojiPickBtn    widget.Clickable
	emojiUploadBtn  widget.Clickable
	emojiDelBtns    []widget.Clickable
	emojiFilePath   string
	emojiUploading  bool
	emojiError      string

	// Bans
	bansLoaded bool
	bans       []api.Ban
	unbanBtns  []widget.Clickable

	// Bans subsections
	bansSubTab int // 0=bans, 1=device bans, 2=invite chain, 3=quarantine, 4=approvals
	bansSubBtns [5]widget.Clickable

	// Device bans
	deviceBansLoaded bool
	deviceBans       []api.DeviceBan
	unbanDeviceBtns  []widget.Clickable

	// Invite chain
	inviteChainLoaded bool
	inviteChain       []api.InviteChainNode

	// Quarantine
	quarantineLoaded  bool
	quarantineEntries []api.QuarantineEntry
	quarantineAppBtns []widget.Clickable
	quarantineDelBtns []widget.Clickable

	// Approvals
	approvalsLoaded bool
	pendingApprovals []api.PendingApproval
	approveUserBtns  []widget.Clickable
	rejectUserBtns   []widget.Clickable

	// Password change
	oldPwEditor    widget.Editor
	newPwEditor    widget.Editor
	confirmPwEditor widget.Editor
	changePwBtn    widget.Clickable
	pwChangeError  string
	pwChangeOk     bool

	// Avatar
	avatarPickBtn   widget.Clickable
	avatarUploadBtn widget.Clickable
	avatarDeleteBtn widget.Clickable
	avatarFilePath  string
	avatarError     string
	avatarUploading bool

	// Game servers toggle (owner)
	gameServersToggle    bool
	gameServersOriginal  bool // hodnota z serveru při načtení
	gameServersBtn       widget.Clickable

	// Swarm sharing toggle (owner)
	swarmSharingToggle    bool
	swarmSharingOriginal  bool
	swarmSharingBtn       widget.Clickable
	installDockerBtn     widget.Clickable
	dockerAvailable      bool
	dockerChecked        bool
	dockerInstalling     bool
	dockerInstallError   string

	// Auto-moderation (owner only)
	automodLoaded           bool
	automodToggle           bool
	automodOriginal         bool
	automodBtn              widget.Clickable
	automodWordEditor       widget.Editor
	automodOrigWords        string
	automodSpamMaxEditor    widget.Editor
	automodOrigSpamMax      string
	automodSpamWindowEditor widget.Editor
	automodOrigSpamWindow   string
	automodSpamTimeoutEditor widget.Editor
	automodOrigSpamTimeout  string
	automodSaveBtn          widget.Clickable

	// Server icon
	iconPickBtn   widget.Clickable
	iconUploadBtn widget.Clickable
	iconDeleteBtn widget.Clickable
	iconFilePath  string
	iconError     string

	// Disconnect
	disconnectBtn widget.Clickable

	// Notification settings
	notifyAllBtn      widget.Clickable
	notifyMentionsBtn widget.Clickable
	notifyNothingBtn  widget.Clickable

	// Sound settings
	notifVolumeSlider Slider
	notifUploadBtn    widget.Clickable
	dmUploadBtn       widget.Clickable
	notifResetBtn     widget.Clickable
	dmResetBtn        widget.Clickable
	lastPreviewTime   time.Time

	// Voice settings
	voiceMicSlider         Slider
	voiceSpeakerSlider     Slider
	voiceInputBtns         []widget.Clickable
	voiceOutputBtns        []widget.Clickable
	voiceRefreshBtn        widget.Clickable
	voiceNoiseSupprBtn     widget.Clickable
	voiceInputDevices      []voiceDeviceInfo
	voiceOutputDevices     []voiceDeviceInfo
	voiceDevicesLoaded     bool
	voiceSelectedInput     string
	voiceSelectedOutput    string

	// Storage
	storageScanned       bool
	storageUsage         store.DiskUsage
	storageScanning      bool
	storageCacheSlider   Slider
	storageHistoryEditor widget.Editor
	storageSaveBtn       widget.Clickable
	clearCacheBtn        widget.Clickable
	clearHistoryBtn      widget.Clickable
	storageRescanBtn     widget.Clickable

	// Server disk management (owner)
	serverDiskInfo            *api.ServerStorageInfo
	serverDiskScanned         bool
	serverDiskScanning        bool
	serverDiskMaxMBEditor     widget.Editor
	serverDiskHistoryEditor   widget.Editor
	serverDiskSaveBtn         widget.Clickable
	serverDiskRescanBtn       widget.Clickable
	serverDiskTrimBtn         widget.Clickable
	serverDiskOrigMaxMB       string
	serverDiskOrigHistory     string

	// Protocol registration
	registerProtocolBtn widget.Clickable

	// Appearance — font scale + compact mode
	fontScaleSlider Slider
	compactModeBtn  widget.Clickable

	// Pinboard
	pinboardBookmarks []store.StoredBookmark
	pinboardLoaded    bool
	pinboardDelBtns   []widget.Clickable
	pinboardJumpBtns  []widget.Clickable

	// Backup/Restore
	backupInfo       *api.BackupInfo
	backupInfoLoaded bool
	backupInfoLoading bool
	backupBtn        widget.Clickable
	restoreBtn       widget.Clickable
	backupError      string
	backupSuccess    string
	restoreFilePath  string
}

type voiceDeviceInfo struct {
	ID        string
	Name      string
	IsDefault bool
}

type catItem struct {
	idx   int
	label string
	sep   bool
}

type blockInfo struct{ ID, Username string }

func NewSettingsView(a *App) *SettingsView {
	v := &SettingsView{app: a, expandedRole: -1}
	v.list.Axis = layout.Vertical
	v.oldPwEditor.SingleLine = true
	v.oldPwEditor.Mask = '*'
	v.newPwEditor.SingleLine = true
	v.newPwEditor.Mask = '*'
	v.confirmPwEditor.SingleLine = true
	v.confirmPwEditor.Mask = '*'
	v.voiceMicSlider.Min = 0
	v.voiceMicSlider.Max = 2.0
	v.voiceMicSlider.Value = 1.0
	v.voiceSpeakerSlider.Min = 0
	v.voiceSpeakerSlider.Max = 2.0
	v.voiceSpeakerSlider.Value = 1.0
	v.notifVolumeSlider.Min = 0
	v.notifVolumeSlider.Max = 1.0
	v.notifVolumeSlider.Value = 1.0
	v.fontScaleSlider.Min = 0.7
	v.fontScaleSlider.Max = 1.6
	v.fontScaleSlider.Value = 1.0
	v.storageCacheSlider.Min = 0
	v.storageCacheSlider.Max = 10
	v.storageCacheSlider.Value = 2 // default 2 GB
	v.storageHistoryEditor.SingleLine = true
	v.serverDiskMaxMBEditor.SingleLine = true
	v.serverDiskHistoryEditor.SingleLine = true
	return v
}

func (v *SettingsView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()

	// Gather data under lock
	var blocks []blockInfo
	var invites []api.Invite
	var roles []api.Role
	var emojis []api.CustomEmoji
	if conn != nil {
		v.app.mu.RLock()
		for _, u := range conn.BlockedUsers {
			blocks = append(blocks, blockInfo{u.ID, u.Username})
		}
		invites = make([]api.Invite, len(conn.Invites))
		copy(invites, conn.Invites)
		roles = make([]api.Role, len(conn.Roles))
		copy(roles, conn.Roles)
		emojis = make([]api.CustomEmoji, len(conn.Emojis))
		copy(emojis, conn.Emojis)
		v.app.mu.RUnlock()
	}

	// Ensure button slices
	if len(v.unblockBtns) < len(blocks) {
		v.unblockBtns = make([]widget.Clickable, len(blocks)+5)
	}
	if len(v.inviteDelBtns) < len(invites) {
		v.inviteDelBtns = make([]widget.Clickable, len(invites)+5)
		v.inviteCopyBtns = make([]widget.Clickable, len(invites)+5)
		v.inviteQRBtns = make([]widget.Clickable, len(invites)+5)
	}
	if len(v.emojiDelBtns) < len(emojis) {
		v.emojiDelBtns = make([]widget.Clickable, len(emojis)+10)
	}
	if len(v.roleBtns) < len(roles) {
		v.roleBtns = make([]widget.Clickable, len(roles)+5)
		v.roleDelBtns = make([]widget.Clickable, len(roles)+5)
		v.roleUpBtns = make([]widget.Clickable, len(roles)+5)
		v.roleDownBtns = make([]widget.Clickable, len(roles)+5)
		v.roleSaveBtns = make([]widget.Clickable, len(roles)+5)
		v.roleNameEditors = make([]widget.Editor, len(roles)+5)
		v.roleColorEditors = make([]widget.Editor, len(roles)+5)
		v.rolePermBtns = make([][]widget.Clickable, len(roles)+5)
		for i := range v.rolePermBtns {
			v.rolePermBtns[i] = make([]widget.Clickable, 10)
		}
	}

	// Lazy-load bans for owner
	if v.isOwner() && !v.bansLoaded {
		v.bansLoaded = true
		go func() {
			if c := v.app.Conn(); c != nil {
				bans, err := c.Client.GetBans()
				if err != nil {
					log.Printf("GetBans: %v", err)
					return
				}
				v.bans = bans
				if len(v.unbanBtns) < len(bans) {
					v.unbanBtns = make([]widget.Clickable, len(bans)+5)
				}
				v.app.Window.Invalidate()
			}
		}()
	}
	if len(v.unbanBtns) < len(v.bans) {
		v.unbanBtns = make([]widget.Clickable, len(v.bans)+5)
	}

	// Handle unban clicks
	for i, b := range v.bans {
		if i < len(v.unbanBtns) && v.unbanBtns[i].Clicked(gtx) {
			userID := b.UserID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.UnbanUser(userID); err != nil {
						log.Printf("UnbanUser: %v", err)
						return
					}
					// Reload bans
					bans, _ := c.Client.GetBans()
					v.bans = bans
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	// Handle unblock clicks
	for i, b := range blocks {
		if v.unblockBtns[i].Clicked(gtx) {
			userID := b.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.UnblockUser(userID); err != nil {
						log.Printf("UnblockUser: %v", err)
						return
					}
					v.app.loadBlocks(c)
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	// Init display name editor
	if conn != nil && !v.displayNameLoaded {
		v.displayNameLoaded = true
		v.displayNameEditor.SingleLine = true
		// Find current user's display name
		v.app.mu.RLock()
		for _, u := range conn.Users {
			if u.ID == conn.UserID {
				v.displayNameEditor.SetText(u.DisplayName)
				break
			}
		}
		v.app.mu.RUnlock()
	}

	// Handle save display name
	if v.saveDisplayNameBtn.Clicked(gtx) && !v.displayNameSaving {
		newName := v.displayNameEditor.Text()
		v.displayNameSaving = true
		v.displayNameError = ""
		v.displayNameSuccess = false
		go func() {
			if c := v.app.Conn(); c != nil {
				if err := c.Client.UpdateDisplayName(newName); err != nil {
					v.displayNameError = err.Error()
				} else {
					v.displayNameSuccess = true
					// Reload users to pick up the change
					users, err := c.Client.GetUsers()
					if err == nil {
						v.app.mu.Lock()
						c.Users = users
						c.Members = users
						v.app.mu.Unlock()
					}
				}
				v.displayNameSaving = false
				v.app.Window.Invalidate()
			}
		}()
	}

	// Init status editor
	if conn != nil && !v.statusLoaded {
		v.statusLoaded = true
		v.statusTextEditor.SingleLine = true
		v.app.mu.RLock()
		for _, u := range conn.Users {
			if u.ID == conn.UserID {
				v.statusTextEditor.SetText(u.StatusText)
				break
			}
		}
		v.app.mu.RUnlock()
	}

	// Handle status button clicks
	saveStatus := func(status string) {
		if v.statusSaving {
			return
		}
		v.statusSaving = true
		statusText := v.statusTextEditor.Text()
		go func() {
			if c := v.app.Conn(); c != nil {
				c.Client.UpdateStatus(status, statusText)
				v.statusSaving = false
				v.app.Window.Invalidate()
			}
		}()
	}
	if v.statusOnlineBtn.Clicked(gtx) {
		saveStatus("")
	}
	if v.statusAwayBtn.Clicked(gtx) {
		saveStatus("away")
	}
	if v.statusDNDBtn.Clicked(gtx) {
		saveStatus("dnd")
	}
	if v.saveStatusBtn.Clicked(gtx) {
		// Uložit jen text, nechat status jak je
		if !v.statusSaving {
			v.statusSaving = true
			statusText := v.statusTextEditor.Text()
			go func() {
				if c := v.app.Conn(); c != nil {
					// Získat aktuální status
					var currentStatus string
					v.app.mu.RLock()
					currentStatus = c.UserStatuses[c.UserID]
					v.app.mu.RUnlock()
					c.Client.UpdateStatus(currentStatus, statusText)
					v.statusSaving = false
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	// Handle password change
	if v.changePwBtn.Clicked(gtx) {
		oldPw := v.oldPwEditor.Text()
		newPw := v.newPwEditor.Text()
		confirmPw := v.confirmPwEditor.Text()
		v.pwChangeError = ""
		v.pwChangeOk = false

		if oldPw == "" || newPw == "" {
			v.pwChangeError = "All fields are required"
		} else if newPw != confirmPw {
			v.pwChangeError = "New passwords don't match"
		} else if len(newPw) < 4 {
			v.pwChangeError = "Password too short (min 4 chars)"
		} else {
			// Verify old password by trying to decrypt
			id, _ := store.FindIdentity(v.app.PublicKey)
			if id == nil {
				v.pwChangeError = "Identity not found"
			} else {
				_, err := crypto.DecryptKey(id.Encrypted, oldPw)
				if err != nil {
					v.pwChangeError = "Wrong current password"
				} else {
					// Re-encrypt with new password
					encrypted, err := crypto.EncryptKey(v.app.SecretKey, newPw)
					if err != nil {
						v.pwChangeError = "Encryption failed: " + err.Error()
					} else {
						id.Encrypted = encrypted
						if err := store.SaveOrUpdateIdentity(*id); err != nil {
							v.pwChangeError = "Save failed: " + err.Error()
						} else {
							v.pwChangeOk = true
							v.oldPwEditor.SetText("")
							v.newPwEditor.SetText("")
							v.confirmPwEditor.SetText("")
						}
					}
				}
			}
		}
	}

	// Handle copy public key
	if v.copyPubKey.Clicked(gtx) {
		copyToClipboard(v.app.PublicKey)
	}

	// Handle create invite
	if v.createInviteBtn.Clicked(gtx) {
		go func() {
			if c := v.app.Conn(); c != nil {
				if _, err := c.Client.CreateInvite(10, 86400); err != nil {
					log.Printf("CreateInvite: %v", err)
					return
				}
				v.reloadInvites(c)
			}
		}()
	}

	// Handle invite clicks
	for i, inv := range invites {
		if v.inviteDelBtns[i].Clicked(gtx) {
			id := inv.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.DeleteInvite(id); err != nil {
						log.Printf("DeleteInvite: %v", err)
						return
					}
					v.reloadInvites(c)
				}
			}()
		}
		if v.inviteCopyBtns[i].Clicked(gtx) {
			copyToClipboard(inv.Link)
		}
		if v.inviteQRBtns[i].Clicked(gtx) {
			// Sestavit nora:// invite link
			host := ""
			if conn := v.app.Conn(); conn != nil {
				host = conn.URL
				if strings.HasPrefix(host, "http://") {
					host = host[7:]
				} else if strings.HasPrefix(host, "https://") {
					host = host[8:]
				}
			}
			code := inv.Code
			v.app.QRDlg.Show("Invite QR", "nora://invite/"+host+"/"+code)
		}
	}

	// Handle emoji file pick
	if v.emojiPickBtn.Clicked(gtx) {
		go func() {
			path := openFileDialog()
			if path != "" {
				v.emojiFilePath = path
				v.emojiError = ""
				v.app.Window.Invalidate()
			}
		}()
	}

	// Handle emoji upload
	if v.emojiUploadBtn.Clicked(gtx) && !v.emojiUploading {
		name := v.emojiNameEditor.Text()
		fpath := v.emojiFilePath
		if name != "" && fpath != "" {
			v.emojiUploading = true
			go func() {
				defer func() {
					v.emojiUploading = false
					v.app.Window.Invalidate()
				}()
				data, err := os.ReadFile(fpath)
				if err != nil {
					v.emojiError = "Failed to read file"
					return
				}
				if c := v.app.Conn(); c != nil {
					_, err := c.Client.CreateEmoji(name, filepath.Base(fpath), data)
					if err != nil {
						v.emojiError = err.Error()
						return
					}
					v.emojiNameEditor.SetText("")
					v.emojiFilePath = ""
					v.emojiError = ""
				}
			}()
		}
	}

	// Handle emoji delete clicks
	for i, emoji := range emojis {
		if v.emojiDelBtns[i].Clicked(gtx) {
			emojiID := emoji.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.DeleteEmoji(emojiID); err != nil {
						log.Printf("DeleteEmoji: %v", err)
					}
				}
			}()
		}
	}

	// Handle role expand/collapse
	for i := range roles {
		if v.roleBtns[i].Clicked(gtx) {
			if v.expandedRole == i {
				v.expandedRole = -1
			} else {
				v.expandedRole = i
				v.roleNameEditors[i].SetText(roles[i].Name)
				v.roleColorEditors[i].SetText(roles[i].Color)
			}
		}
	}

	// Handle role delete clicks
	for i, role := range roles {
		if v.roleDelBtns[i].Clicked(gtx) {
			roleID := role.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.DeleteRole(roleID); err != nil {
						log.Printf("DeleteRole: %v", err)
						return
					}
					v.reloadRoles(c)
				}
			}()
		}
	}

	// Handle role save clicks
	for i, role := range roles {
		if v.roleSaveBtns[i].Clicked(gtx) {
			roleID := role.ID
			newName := v.roleNameEditors[i].Text()
			newColor := v.roleColorEditors[i].Text()
			perms := role.Permissions
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.UpdateRole(roleID, newName, perms, newColor); err != nil {
						log.Printf("UpdateRole: %v", err)
						return
					}
					v.reloadRoles(c)
				}
			}()
		}
	}

	// Handle role reorder (UP/DOWN)
	for i, role := range roles {
		if v.roleUpBtns[i].Clicked(gtx) && i > 0 {
			id1, id2 := role.ID, roles[i-1].ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.SwapRolePositions(id1, id2); err != nil {
						log.Printf("SwapRolePositions: %v", err)
						return
					}
					v.reloadRoles(c)
				}
			}()
		}
		if v.roleDownBtns[i].Clicked(gtx) && i < len(roles)-1 {
			id1, id2 := role.ID, roles[i+1].ID
			go func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.SwapRolePositions(id1, id2); err != nil {
						log.Printf("SwapRolePositions: %v", err)
						return
					}
					v.reloadRoles(c)
				}
			}()
		}
	}

	// Handle create role
	if v.createRoleBtn.Clicked(gtx) {
		roleName := v.newRoleEditor.Text()
		if roleName != "" {
			go func() {
				if c := v.app.Conn(); c != nil {
					if _, err := c.Client.CreateRole(roleName, 0); err != nil {
						log.Printf("CreateRole: %v", err)
						return
					}
					v.newRoleEditor.SetText("")
					v.reloadRoles(c)
				}
			}()
		}
	}

	// Handle permission toggle clicks
	permDefs := v.permissionDefs()
	for i, role := range roles {
		for p := range permDefs {
			if v.rolePermBtns[i][p].Clicked(gtx) {
				perm := permDefs[p].value
				newPerms := role.Permissions ^ perm // toggle
				roleID := role.ID
				roleName := role.Name
				roleColor := role.Color
				go func() {
					if c := v.app.Conn(); c != nil {
						if err := c.Client.UpdateRole(roleID, roleName, newPerms, roleColor); err != nil {
							log.Printf("UpdateRole perm: %v", err)
							return
						}
						v.reloadRoles(c)
					}
				}()
			}
		}
	}

	// Owner check
	isOwner := v.isOwner()

	// Load server settings for owner (once)
	if isOwner && !v.settingsLoaded && conn != nil {
		v.settingsLoaded = true
		go func() {
			settings, err := conn.Client.GetServerSettings()
			if err != nil {
				log.Printf("GetServerSettings: %v", err)
				return
			}
			if name, ok := settings["server_name"].(string); ok {
				v.nameEditor.SetText(name)
				v.origName = name
			}
			if desc, ok := settings["server_description"].(string); ok {
				v.descEditor.SetText(desc)
				v.origDesc = desc
			}
			if mb, ok := settings["max_upload_size_mb"].(float64); ok {
				s := fmt.Sprintf("%d", int(mb))
				v.uploadSizeEditor.SetText(s)
				v.origUploadSize = s
			}
			if enabled, ok := settings["game_servers_enabled"].(bool); ok {
				v.gameServersToggle = enabled
				v.gameServersOriginal = enabled
			}
			if enabled, ok := settings["swarm_sharing_enabled"].(bool); ok {
				v.swarmSharingToggle = enabled
				v.swarmSharingOriginal = enabled
			}
			// Auto-moderation
			if enabled, ok := settings["automod_enabled"].(bool); ok {
				v.automodToggle = enabled
				v.automodOriginal = enabled
			}
			if words, ok := settings["automod_word_filter"].([]interface{}); ok {
				var lines []string
				for _, w := range words {
					if s, ok := w.(string); ok {
						lines = append(lines, s)
					}
				}
				joined := strings.Join(lines, "\n")
				v.automodWordEditor.SetText(joined)
				v.automodOrigWords = joined
			}
			if n, ok := settings["automod_spam_max_messages"].(float64); ok {
				s := fmt.Sprintf("%d", int(n))
				v.automodSpamMaxEditor.SetText(s)
				v.automodOrigSpamMax = s
			}
			if n, ok := settings["automod_spam_window_seconds"].(float64); ok {
				s := fmt.Sprintf("%d", int(n))
				v.automodSpamWindowEditor.SetText(s)
				v.automodOrigSpamWindow = s
			}
			if n, ok := settings["automod_spam_timeout_seconds"].(float64); ok {
				s := fmt.Sprintf("%d", int(n))
				v.automodSpamTimeoutEditor.SetText(s)
				v.automodOrigSpamTimeout = s
			}
			v.automodLoaded = true
			v.app.Window.Invalidate()
		}()
	}

	// Načíst Docker status (jen pokud je game servers zapnuté/přepínáno na zapnuté)
	if isOwner && !v.dockerChecked && conn != nil && v.gameServersToggle {
		v.dockerChecked = true
		go func() {
			status, err := conn.Client.GetDockerStatus()
			if err != nil {
				return
			}
			if avail, ok := status["available"].(bool); ok {
				v.dockerAvailable = avail
			}
			if installing, ok := status["installing"].(bool); ok {
				v.dockerInstalling = installing
			}
			v.app.Window.Invalidate()
		}()
	}

	// Handle voice settings
	if v.voiceRefreshBtn.Clicked(gtx) {
		v.voiceDevicesLoaded = false
	}
	if !v.voiceDevicesLoaded {
		v.voiceDevicesLoaded = true
		go func() {
			if c := v.app.Conn(); c != nil && c.Voice != nil {
				inputs, outputs, err := c.Voice.EnumDevices()
				if err != nil {
					log.Printf("EnumDevices: %v", err)
					return
				}
				v.voiceInputDevices = nil
				for _, d := range inputs {
					v.voiceInputDevices = append(v.voiceInputDevices, voiceDeviceInfo{
						ID: d.ID, Name: d.Name, IsDefault: d.IsDefault,
					})
				}
				v.voiceOutputDevices = nil
				for _, d := range outputs {
					v.voiceOutputDevices = append(v.voiceOutputDevices, voiceDeviceInfo{
						ID: d.ID, Name: d.Name, IsDefault: d.IsDefault,
					})
				}
				if len(v.voiceInputBtns) < len(v.voiceInputDevices) {
					v.voiceInputBtns = make([]widget.Clickable, len(v.voiceInputDevices)+5)
				}
				if len(v.voiceOutputBtns) < len(v.voiceOutputDevices) {
					v.voiceOutputBtns = make([]widget.Clickable, len(v.voiceOutputDevices)+5)
				}
				v.app.Window.Invalidate()
			}
		}()
	}
	// Handle voice input device clicks
	for i := range v.voiceInputDevices {
		if i < len(v.voiceInputBtns) && v.voiceInputBtns[i].Clicked(gtx) {
			v.voiceSelectedInput = v.voiceInputDevices[i].ID
			if c := v.app.Conn(); c != nil && c.Voice != nil {
				c.Voice.SetInputDevice(v.voiceSelectedInput)
			}
		}
	}
	// Handle voice output device clicks
	for i := range v.voiceOutputDevices {
		if i < len(v.voiceOutputBtns) && v.voiceOutputBtns[i].Clicked(gtx) {
			v.voiceSelectedOutput = v.voiceOutputDevices[i].ID
			if c := v.app.Conn(); c != nil && c.Voice != nil {
				c.Voice.SetOutputDevice(v.voiceSelectedOutput)
			}
		}
	}
	// Handle voice slider changes
	if v.voiceMicSlider.Changed() {
		if c := v.app.Conn(); c != nil && c.Voice != nil {
			c.Voice.SetMicVolume(v.voiceMicSlider.Value)
		}
	}
	if v.voiceSpeakerSlider.Changed() {
		if c := v.app.Conn(); c != nil && c.Voice != nil {
			c.Voice.SetSpeakerVolume(v.voiceSpeakerSlider.Value)
		}
	}
	// Handle noise suppression toggle
	if v.voiceNoiseSupprBtn.Clicked(gtx) {
		if c := v.app.Conn(); c != nil && c.Voice != nil {
			enabled := c.Voice.IsNoiseSuppressionEnabled()
			c.Voice.SetNoiseSuppression(!enabled)
		}
	}

	// Handle disconnect
	if v.disconnectBtn.Clicked(gtx) && conn != nil {
		idx := v.app.ActiveServer
		v.app.ConfirmDlg.Show("Disconnect", "Disconnect from this server?", func() {
			v.app.DisconnectServer(idx)
			v.app.Window.Invalidate()
		})
	}

	// Handle avatar pick
	if v.avatarPickBtn.Clicked(gtx) {
		go func() {
			path := openFileDialog()
			if path != "" {
				v.avatarFilePath = path
				v.avatarError = ""
				v.app.Window.Invalidate()
			}
		}()
	}
	// Handle avatar upload
	if v.avatarUploadBtn.Clicked(gtx) && v.avatarFilePath != "" && !v.avatarUploading {
		path := v.avatarFilePath
		v.avatarUploading = true
		go func() {
			defer func() {
				v.avatarUploading = false
				v.app.Window.Invalidate()
			}()
			data, err := os.ReadFile(path)
			if err != nil {
				v.avatarError = err.Error()
				return
			}
			if len(data) > 512*1024 {
				v.avatarError = "Max 512KB"
				return
			}
			if c := v.app.Conn(); c != nil {
				_, err := c.Client.UploadAvatar(filepath.Base(path), data)
				if err != nil {
					v.avatarError = err.Error()
					return
				}
				v.avatarFilePath = ""
				v.avatarError = ""
			}
		}()
	}
	// Handle avatar delete
	if v.avatarDeleteBtn.Clicked(gtx) {
		go func() {
			if c := v.app.Conn(); c != nil {
				if _, err := c.Client.DeleteAvatar(); err != nil {
					log.Printf("DeleteAvatar: %v", err)
				}
			}
		}()
	}

	// Handle icon pick
	if v.iconPickBtn.Clicked(gtx) {
		go func() {
			path := openFileDialog()
			if path != "" {
				v.iconFilePath = path
				v.iconError = ""
				v.app.Window.Invalidate()
			}
		}()
	}
	// Handle icon upload
	if v.iconUploadBtn.Clicked(gtx) && v.iconFilePath != "" {
		path := v.iconFilePath
		go func() {
			data, err := os.ReadFile(path)
			if err != nil {
				v.iconError = err.Error()
				v.app.Window.Invalidate()
				return
			}
			if len(data) > 512*1024 {
				v.iconError = "Max 512KB"
				v.app.Window.Invalidate()
				return
			}
			if c := v.app.Conn(); c != nil {
				iconURL, err := c.Client.UploadServerIcon(filepath.Base(path), data)
				if err != nil {
					v.iconError = err.Error()
					v.app.Window.Invalidate()
					return
				}
				v.iconFilePath = ""
				v.iconError = ""
				c.IconURL = iconURL
				v.app.downloadServerIcon(c, c.URL+iconURL)
				v.app.Window.Invalidate()
			}
		}()
	}
	// Handle icon delete
	if v.iconDeleteBtn.Clicked(gtx) {
		go func() {
			if c := v.app.Conn(); c != nil {
				if err := c.Client.DeleteServerIcon(); err != nil {
					log.Printf("DeleteServerIcon: %v", err)
					return
				}
				c.IconURL = ""
				c.Icon = nil
				v.app.Window.Invalidate()
			}
		}()
	}

	if v.gameServersBtn.Clicked(gtx) {
		v.gameServersToggle = !v.gameServersToggle
		// Při zapnutí zkontrolovat Docker
		if v.gameServersToggle && !v.dockerChecked {
			v.dockerChecked = true
			go func() {
				if c := v.app.Conn(); c != nil {
					status, err := c.Client.GetDockerStatus()
					if err != nil {
						return
					}
					if avail, ok := status["available"].(bool); ok {
						v.dockerAvailable = avail
					}
					v.app.Window.Invalidate()
				}
			}()
		}
	}

	if v.swarmSharingBtn.Clicked(gtx) {
		v.swarmSharingToggle = !v.swarmSharingToggle
	}

	// Install Docker
	if v.installDockerBtn.Clicked(gtx) && !v.dockerInstalling {
		v.dockerInstalling = true
		v.dockerInstallError = ""
		go func() {
			if c := v.app.Conn(); c != nil {
				if err := c.Client.InstallDocker(); err != nil {
					v.dockerInstallError = err.Error()
					v.dockerInstalling = false
					v.app.Window.Invalidate()
					return
				}
				// Pollovat stav instalace
				for {
					status, err := c.Client.GetDockerStatus()
					if err != nil {
						break
					}
					installing, _ := status["installing"].(bool)
					if !installing {
						if avail, ok := status["available"].(bool); ok {
							v.dockerAvailable = avail
						}
						if installErr, ok := status["install_error"].(string); ok && installErr != "" {
							v.dockerInstallError = installErr
						}
						v.dockerInstalling = false
						v.app.Window.Invalidate()
						break
					}
					v.app.Window.Invalidate()
					// Počkat 3 sekundy před dalším pollem
					select {
					case <-v.app.ctx.Done():
						return
					case <-time.After(3 * time.Second):
					}
				}
			}
		}()
	}

	if v.saveSettingsBtn.Clicked(gtx) {
		v.doSaveServerSettings()
	}

	// Content only (sidebar + click handling je v LayoutSidebar, volaný v channel sloupci)
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return material.List(v.app.Theme.Material, &v.list).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			switch v.activeCategory {
			case settingsCatProfile:
				return v.layoutProfileSection(gtx, blocks)
			case settingsCatPassword:
				return v.layoutPasswordSection(gtx)
			case settingsCatVoice:
				return v.layoutVoiceSection(gtx)
			case settingsCatBlocked:
				return v.layoutBlockedSection(gtx, blocks)
			case settingsCatServer:
				return v.layoutServerSection(gtx, conn, isOwner)
			case settingsCatRoles:
				return v.layoutRolesSection(gtx, conn, roles, isOwner)
			case settingsCatInvites:
				return v.layoutInvitesSection(gtx, conn, invites)
			case settingsCatEmojis:
				return v.layoutEmojisSection(gtx, conn, emojis)
			case settingsCatBans:
				return v.layoutBansSection(gtx)
			case settingsCatAppearance:
				return v.layoutAppearanceSection(gtx)
			case settingsCatNotify:
				return v.layoutNotificationsSection(gtx)
			case settingsCatStorage:
				return v.layoutStorageSection(gtx)
			case settingsCatAutomod:
				return v.layoutAutomodSection(gtx)
			case settingsCatServerDisk:
				return v.layoutServerDiskSection(gtx)
			case settingsCatPinboard:
				return v.layoutPinboardSection(gtx)
			case settingsCatBackup:
				return v.layoutBackupSection(gtx)
			case settingsCatTunnels:
				return v.app.TunnelView.Layout(gtx)
			case settingsCatDisconnect:
				return v.layoutDisconnectSection(gtx)
			}
			return layout.Dimensions{}
		})
	})
}

func (v *SettingsView) layoutSidebarItem(gtx layout.Context, btn *widget.Clickable, label string, active, isDanger bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if active {
			bg = ColorAccentDim
		} else if btn.Hovered() {
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
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, label)
					if isDanger {
						lbl.Color = ColorDanger
					} else if active {
						lbl.Color = ColorText
					} else {
						lbl.Color = ColorTextDim
					}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

// LayoutSidebar renderuje settings kategorie v levém 240px sloupci (jako kanály).
// Musí být volaný PŘED Layout(), protože btn.Layout() konzumuje click eventy.
func (v *SettingsView) LayoutSidebar(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	isOwner := v.isOwner()
	isServerMode := v.app.Mode == ViewSettings

	// Sestavit kategorie podle mode a permissions
	var cats []catItem
	if isServerMode {
		if isOwner {
			cats = append(cats, catItem{settingsCatServer, "Server", false})
		}
		if isOwner || (conn != nil && conn.MyPermissions&(api.PermManageRoles|api.PermAdmin) != 0) {
			cats = append(cats, catItem{settingsCatRoles, "Roles", false})
		}
		if isOwner || (conn != nil && conn.MyPermissions&(api.PermManageInvites|api.PermAdmin) != 0) {
			cats = append(cats, catItem{settingsCatInvites, "Invites", false})
		}
		if isOwner || (conn != nil && conn.MyPermissions&(api.PermManageEmojis|api.PermAdmin) != 0) {
			cats = append(cats, catItem{settingsCatEmojis, "Emojis", false})
		}
		if isOwner {
			cats = append(cats, catItem{settingsCatBans, "Bans", false})
		}
		if isOwner {
			cats = append(cats, catItem{settingsCatAutomod, "Auto-Mod", false})
		}
		if isOwner {
			cats = append(cats, catItem{settingsCatServerDisk, "Disk", false})
		}
		if isOwner {
			cats = append(cats, catItem{settingsCatBackup, "Backup", false})
		}
		cats = append(cats, catItem{settingsCatTunnels, "VPN Tunnels", false})
		cats = append(cats, catItem{-1, "", true})
		cats = append(cats, catItem{settingsCatDisconnect, "Disconnect", false})
	} else {
		cats = append(cats, catItem{settingsCatProfile, "Profile", false})
		cats = append(cats, catItem{settingsCatPassword, "Password", false})
		cats = append(cats, catItem{settingsCatAppearance, "Appearance", false})
		cats = append(cats, catItem{settingsCatVoice, "Voice", false})
		cats = append(cats, catItem{settingsCatNotify, "Notifications", false})
		cats = append(cats, catItem{settingsCatStorage, "Storage", false})
		cats = append(cats, catItem{settingsCatBlocked, "Blocked Users", false})
		cats = append(cats, catItem{settingsCatPinboard, "Pinboard", false})
	}

	// Click handling — MUSÍ být před btn.Layout() (Gio konzumuje eventy v Layout)
	for _, c := range cats {
		if c.sep {
			continue
		}
		if v.catBtns[c.idx].Clicked(gtx) {
			if c.idx != v.activeCategory && v.activeCategory == settingsCatServer && v.hasServerChanges() {
				// Neuložené změny — potvrdit odchod
				target := c.idx
				dlg := v.app.ConfirmDlg
				dlg.CancelText = "Discard"
				dlg.ShowWithCancel("Unsaved Changes", "You have unsaved changes. Save before leaving?",
					"Save", func() {
						// Save
						v.doSaveServerSettings()
						v.activeCategory = target
						v.list.Position.First = 0
						v.list.Position.Offset = 0
						v.app.Window.Invalidate()
					}, func() {
						// Discard
						v.nameEditor.SetText(v.origName)
						v.descEditor.SetText(v.origDesc)
						v.uploadSizeEditor.SetText(v.origUploadSize)
						v.gameServersToggle = v.gameServersOriginal
						v.swarmSharingToggle = v.swarmSharingOriginal
						v.activeCategory = target
						v.list.Position.First = 0
						v.list.Position.Offset = 0
						v.app.Window.Invalidate()
					})
				dlg.confirmColor = ColorAccent
			} else {
				if c.idx == settingsCatServerDisk && c.idx != v.activeCategory {
					v.serverDiskScanned = false
				}
				if c.idx == settingsCatPinboard && c.idx != v.activeCategory {
					v.pinboardLoaded = false
				}
				if c.idx == settingsCatBackup && c.idx != v.activeCategory {
					v.backupInfoLoaded = false
				}
				v.activeCategory = c.idx
				v.list.Position.First = 0
				v.list.Position.Offset = 0
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				headerText := "User Settings"
				if isServerMode {
					headerText = "Server Settings"
				}
				lbl := material.Body1(v.app.Theme.Material, headerText)
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: sz}.Op())
			return layout.Dimensions{Size: sz}
		}),
		// Category items
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var items []layout.FlexChild
				for _, c := range cats {
					cc := c
					if cc.sep {
						items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
								paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: sz}.Op())
								return layout.Dimensions{Size: sz}
							})
						}))
						continue
					}
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSidebarItem(gtx, &v.catBtns[cc.idx], cc.label, v.activeCategory == cc.idx, cc.idx == settingsCatDisconnect)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
			})
		}),
	)
}

func (v *SettingsView) layoutSection(gtx layout.Context, title string) layout.Dimensions {
	lbl := material.Body1(v.app.Theme.Material, title)
	lbl.Color = ColorText
	lbl.Font.Weight = 600
	return lbl.Layout(gtx)
}

func (v *SettingsView) layoutDivider() layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		})
	})
}

func (v *SettingsView) layoutSmallIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, icon, 16, fg)
				})
			},
		)
	})
}

func (v *SettingsView) layoutSmallBtn(gtx layout.Context, btn *widget.Clickable, text string, fg color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorInput
		if btn.Hovered() {
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, text)
					lbl.Color = fg
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *SettingsView) layoutAccentBtn(gtx layout.Context, btn *widget.Clickable, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorAccent
		if btn.Hovered() {
			bg = ColorAccentHover
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
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, text)
					lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *SettingsView) layoutDangerBtn(gtx layout.Context, btn *widget.Clickable, text string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ColorDanger
		if btn.Hovered() {
			bg = color.NRGBA{R: min8(bg.R + 30), G: bg.G, B: bg.B, A: 255}
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
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(v.app.Theme.Material, text)
					lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *SettingsView) layoutEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
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
				e := material.Editor(v.app.Theme.Material, ed, hint)
				e.Color = ColorText
				e.HintColor = ColorTextDim
				return e.Layout(gtx)
			})
		},
	)
}

func (v *SettingsView) isOwner() bool {
	conn := v.app.Conn()
	if conn == nil {
		return false
	}
	v.app.mu.RLock()
	defer v.app.mu.RUnlock()
	for _, u := range conn.Users {
		if u.ID == conn.UserID {
			return u.IsOwner
		}
	}
	return false
}

type permDef struct {
	name  string
	value int64
}

func (a *App) loadBlocks(conn *ServerConnection) {
	blocks, err := conn.Client.GetBlocks()
	if err == nil {
		a.mu.Lock()
		conn.BlockedUsers = blocks
		a.mu.Unlock()
	}
}

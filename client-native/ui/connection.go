package ui

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nora-client/api"
	"nora-client/crypto"
	"nora-client/device"
	"nora-client/mount"
	"nora-client/p2p"
	"nora-client/store"
	"nora-client/voice"
)

// ServerConnection holds the state of one connected server.
type ServerConnection struct {
	URL         string
	Name        string
	Description string
	Client *api.Client
	WS     *api.WSClient
	UserID string
	Cancel context.CancelFunc
	Ctx    context.Context

	Users      []api.User
	Channels   []api.Channel
	Categories []api.ChannelCategory
	Members    []api.User
	Messages   []api.Message

	Friends        []api.User
	FriendRequests     []api.FriendRequest // incoming pending requests
	SentFriendRequests []api.FriendRequest // sent outgoing requests
	BlockedUsers   []api.User
	Invites        []api.Invite
	Roles          []api.Role
	Emojis         []api.CustomEmoji

	DMConversations []api.DMConversation
	DMMessages      []api.DMPendingMessage
	ActiveDMID      string
	ActiveDMPeerKey string

	ActiveChannelID   string
	ActiveChannelName string

	// Groups
	Groups              []api.Group
	GroupMessages       []api.GroupMessage // current group messages
	ActiveGroupID       string
	PendingGroupMsgs    []api.GroupMessage // messages waiting for key

	OnlineUsers    map[string]bool
	UserStatuses   map[string]string    // userID → status (away/dnd)
	UserStatusText map[string]string    // userID → status text
	TypingUsers    map[string]time.Time // userID → last typing time (active channel, legacy)
	TypingDMUsers  map[string]time.Time // userID → last DM typing time
	ChannelTyping  map[string]map[string]time.Time // channelID → userID → lastTypingTime
	UnreadCount    map[string]int       // channelID → unread count
	UnreadDMCount  map[string]int       // conversationID → unread count
	UnreadGroups   map[string]bool      // groupID → has unread

	// Voice state: channelID → list of userIDs in that voice channel
	VoiceState map[string][]string
	// Voice manager (WebRTC + audio)
	Voice *voice.Manager
	// Call manager (1:1 DM hovory)
	Call *voice.CallManager
	// P2P file transfer manager
	P2P *p2p.Manager

	// LAN Party
	LANParties []api.LANParty
	LANMembers map[string][]api.LANPartyMember // partyID → members

	// Screen sharing: userID → channelID
	ScreenSharers map[string]string

	// Live whiteboards: channelID → starterID
	LiveWhiteboards map[string]string

	// File sharing
	MyShares     []api.SharedDirectory
	SharedWithMe []api.SharedDirectory
	SharePaths   map[string]string // shareID → local path (owner only)
	Mounts       *mount.MountManager

	// Game servers
	GameServers        []api.GameServerInstance
	GameServersEnabled bool

	// Swarm sharing
	Swarm               *p2p.SwarmManager
	SwarmSharingEnabled bool

	// Auto-moderation
	AutoModEnabled bool

	// Notification overrides (z identity store)
	NotifyLevel   *store.NotifyLevel
	ChannelNotify map[string]store.NotifyLevel

	// Mapping userID → set of role IDs (for role colors and permissions)
	UserRolesMap map[string]map[string]bool

	// Cached permissions (bitwise OR of all user roles)
	MyPermissions int64

	// Server info
	IconURL string
	StunURL string
	Icon    image.Image

	// VPN Tunnels
	Tunnels []api.Tunnel

	// Lobby voice channels
	LastVoiceError string // last voice error (e.g. "Wrong password")
}

// Unlock unlocks or creates an identity.
// passwordVerifyPlaintext is a known string encrypted with the password for wrong password detection.
const passwordVerifyPlaintext = "NORA"

func (a *App) Unlock(username, password string) error {
	ids, _ := store.LoadIdentities()

	// If identities exist, just unlock — never create a new one
	if len(ids) > 0 && password != "" {
		for i, id := range ids {
			// Quick check via PasswordVerify (if it exists)
			if id.PasswordVerify != "" {
				plain, err := crypto.DecryptKey(id.PasswordVerify, password)
				if err != nil || plain != passwordVerifyPlaintext {
					continue // wrong password for this identity
				}
			}

			secret, err := crypto.DecryptKey(id.Encrypted, password)
			if err != nil {
				continue
			}

			a.PublicKey = id.PublicKey
			a.SecretKey = secret
			a.Username = id.Username
			a.Mode = ViewHome
			a.GlobalNotifyLevel = id.NotifyLevel

			// Migrate old sound settings to new maps
			if id.MigrateSoundSettings() {
				_ = store.SaveIdentities(ids)
			}
			if id.SoundVolumes == nil {
				id.SoundVolumes = make(map[string]float64)
			}
			if id.CustomSounds == nil {
				id.CustomSounds = make(map[string]string)
			}
			a.SoundVolumes = id.SoundVolumes
			a.CustomSounds = id.CustomSounds
			SetAllSoundSettings(id.SoundVolumes, id.CustomSounds)

			if id.VideoVolume > 0 {
				a.VideoPlayer.SetVolume(id.VideoVolume)
			}
			a.YouTubeQuality = id.YouTubeQuality

			fs := id.FontScale
			if fs == 0 {
				fs = 1.0
			}
			a.Theme.ApplyFontScale(float32(fs))
			a.Theme.CompactMode = id.CompactMode

			a.DMHistory = store.NewDMHistory(id.PublicKey, secret)
			a.GroupHistory = store.NewGroupHistory(id.PublicKey, secret)
			a.Bookmarks = store.NewBookmarkStore(id.PublicKey)

			if cdb, err := store.OpenContacts(id.PublicKey); err == nil {
				a.Contacts = cdb
			} else {
				log.Printf("contacts DB open: %v", err)
			}

			// Migration: add PasswordVerify for old identities
			if id.PasswordVerify == "" {
				if verify, err := crypto.EncryptKey(passwordVerifyPlaintext, password); err == nil {
					ids[i].PasswordVerify = verify
					_ = store.SaveIdentities(ids)
				}
			}

			go a.runAutoCleanup(id.PublicKey)
			go a.reconnectSavedServers(id.Servers)
			go a.handlePendingDeepLink()

			return nil
		}
		return fmt.Errorf("wrong password")
	}

	// No identities — create a new one
	return a.createIdentity(username, password)
}

// CreateNewIdentity explicitly creates a new identity (called from login UI in create mode).
func (a *App) CreateNewIdentity(username, password string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}
	if password == "" {
		return fmt.Errorf("password is required")
	}
	return a.createIdentity(username, password)
}

func (a *App) createIdentity(username, password string) error {
	kp, err := crypto.GenerateKeypair()
	if err != nil {
		return err
	}

	if password != "" {
		encrypted, err := crypto.EncryptKey(kp.SecretKey, password)
		if err != nil {
			return fmt.Errorf("key encryption: %w", err)
		}
		verify, err := crypto.EncryptKey(passwordVerifyPlaintext, password)
		if err != nil {
			return fmt.Errorf("verify encryption: %w", err)
		}
		if err := store.SaveOrUpdateIdentity(store.StoredIdentity{
			PublicKey:      kp.PublicKey,
			Username:       username,
			Encrypted:      encrypted,
			PasswordVerify: verify,
		}); err != nil {
			return err
		}
	}

	a.PublicKey = kp.PublicKey
	a.SecretKey = kp.SecretKey
	a.Username = username
	a.Mode = ViewHome
	a.DMHistory = store.NewDMHistory(kp.PublicKey, kp.SecretKey)
	a.GroupHistory = store.NewGroupHistory(kp.PublicKey, kp.SecretKey)
	a.Bookmarks = store.NewBookmarkStore(kp.PublicKey)
	if cdb, err := store.OpenContacts(kp.PublicKey); err == nil {
		a.Contacts = cdb
	} else {
		log.Printf("contacts DB open: %v", err)
	}
	return nil
}

// reconnectSavedServers reconnects to all previously saved servers.
func (a *App) reconnectSavedServers(servers []store.StoredServer) {
	for _, s := range servers {
		if s.URL == "" || s.RefreshToken == "" {
			continue
		}
		if err := a.reconnectServer(s); err != nil {
			log.Printf("auto-reconnect %s: %v", s.URL, err)
		}
	}
	a.Window.Invalidate()
}

// reconnectServer reconnects to a single server using its refresh token.
func (a *App) reconnectServer(s store.StoredServer) error {
	client := api.NewClient(s.URL)
	client.SetTokens("", s.RefreshToken)

	refreshResp, err := client.Refresh()
	if err != nil {
		// Refresh token expired — try challenge-response
		challengeResp, err := client.Challenge(a.PublicKey, "", "", device.GetDeviceID(), device.GetHardwareHash())
		if err != nil {
			return fmt.Errorf("challenge: %w", err)
		}
		sig, err := crypto.Sign(a.SecretKey, challengeResp.Nonce)
		if err != nil {
			return fmt.Errorf("sign: %w", err)
		}
		verifyResp, err := client.Verify(a.PublicKey, challengeResp.Nonce, sig)
		if err != nil {
			return fmt.Errorf("verify: %w", err)
		}
		client.SetTokens(verifyResp.AccessToken, verifyResp.RefreshToken)
		store.UpdateServerToken(a.PublicKey, s.URL, verifyResp.RefreshToken)

		return a.setupServerConnection(s.URL, s.Name, "", "", "", false, false, false, client, verifyResp.User.ID)
	}

	client.SetTokens(refreshResp.AccessToken, refreshResp.RefreshToken)
	store.UpdateServerToken(a.PublicKey, s.URL, refreshResp.RefreshToken)

	// Get user ID from server
	info, _ := client.GetServerInfo()
	users, _ := client.GetUsers()
	userID := ""
	for _, u := range users {
		if u.PublicKey == a.PublicKey {
			userID = u.ID
			break
		}
	}

	name := s.Name
	description := ""
	if info != nil && info.Name != "" {
		name = info.Name
		description = info.Description
	}

	iconURL := ""
	stunURL := ""
	gsEnabled := false
	swarmEnabled := false
	autoMod := false
	if info != nil {
		iconURL = info.IconURL
		stunURL = info.StunURL
		gsEnabled = info.GameServersEnabled
		swarmEnabled = info.SwarmSharingEnabled
		autoMod = info.AutoModEnabled
	}

	return a.setupServerConnection(s.URL, name, description, iconURL, stunURL, gsEnabled, swarmEnabled, autoMod, client, userID)
}

// setupServerConnection creates the connection, loads data, and adds it to the server list.
func (a *App) setupServerConnection(serverURL, name, description, iconURL, stunURL string, gsEnabled, swarmEnabled, autoModEnabled bool, client *api.Client, userID string) error {
	if name == "" {
		name = serverURL
	}

	sctx, scancel := context.WithCancel(a.ctx)
	conn := &ServerConnection{
		URL:         serverURL,
		Name:        name,
		Description: description,
		Client:      client,
		UserID:      userID,
		Ctx:         sctx,
		Cancel:      scancel,
		OnlineUsers:    make(map[string]bool),
		UserStatuses:   make(map[string]string),
		UserStatusText: make(map[string]string),
		TypingUsers:   make(map[string]time.Time),
		TypingDMUsers: make(map[string]time.Time),
		ChannelTyping: make(map[string]map[string]time.Time),
		UnreadCount:   make(map[string]int),
		UnreadDMCount: make(map[string]int),
		UnreadGroups:  make(map[string]bool),
		VoiceState:     make(map[string][]string),
		LANMembers:     make(map[string][]api.LANPartyMember),
		ScreenSharers:   make(map[string]string),
		LiveWhiteboards: make(map[string]string),
		SharePaths:      make(map[string]string),
	}

	// Load persisted SharePaths from identity
	if saved := store.GetSharePaths(a.PublicKey, serverURL); saved != nil {
		for k, v := range saved {
			conn.SharePaths[k] = v
		}
	}

	// Load notify settings from identity
	srvNotify, chNotify := store.GetServerNotifySettings(a.PublicKey, serverURL)
	conn.NotifyLevel = srvNotify
	if chNotify != nil {
		conn.ChannelNotify = chNotify
	} else {
		conn.ChannelNotify = make(map[string]store.NotifyLevel)
	}

	// Auto-refresh callback — save new refresh token
	pubKey := a.PublicKey
	client.OnTokenRefresh = func(_, refresh string) {
		store.UpdateServerToken(pubKey, serverURL, refresh)
	}

	conn.IconURL = iconURL
	conn.StunURL = stunURL
	conn.GameServersEnabled = gsEnabled

	conn.SwarmSharingEnabled = swarmEnabled
	conn.AutoModEnabled = autoModEnabled
	if iconURL != "" {
		go a.downloadServerIcon(conn, serverURL+iconURL)
	}

	a.loadServerData(conn)

	ws := api.NewWSClient(serverURL, client.GetAccessToken(), func() {
		a.Window.Invalidate()
	})
	if err := ws.Connect(sctx); err != nil {
		log.Printf("WS connect error: %v", err)
		a.Toasts.Error("WebSocket connection failed")
	}
	ws.SetOnReconnect(func() {
		a.loadServerData(conn)
	})
	conn.WS = ws

	// Initialize voice manager with WS send callback
	sendWS := func(eventType string, payload any) error {
		return conn.WS.SendJSON(eventType, payload)
	}
	invalidate := func() {
		a.Window.Invalidate()
	}
	conn.Voice = voice.NewManager(conn.UserID, conn.StunURL, sendWS, invalidate)

	// Screen share frame callback
	conn.Voice.OnScreenFrame = func(from string, data []byte) {
		if a.StreamViewer != nil && a.StreamViewer.Visible && a.StreamViewer.StreamerID == from {
			a.StreamViewer.HandleMessage(data)
		}
	}

	// Initialize call manager (mutual exclusion with voice)
	conn.Call = voice.NewCallManager(conn.StunURL, sendWS, invalidate, func() {
		conn.Voice.Leave()
	}, func() {
		StopCallRingLoop()
	})

	// Initialize P2P file transfer manager
	conn.P2P = p2p.NewManager(conn.UserID, conn.StunURL, sendWS, invalidate)
	conn.P2P.SetOnOffer(func(t *p2p.Transfer) {
		// Find sender's username
		senderName := t.PeerID
		a.mu.RLock()
		for _, u := range conn.Users {
			if u.ID == t.PeerID {
				senderName = u.Username
				break
			}
		}
		a.mu.RUnlock()
		a.P2POfferDlg.Show(conn, t, senderName)
		a.Window.Invalidate()
	})
	conn.P2P.SetOnZipStart(func(savePath string) {
		a.ZipExtractDlg.Show(savePath)
		a.Window.Invalidate()
	})
	conn.P2P.SetOnZipDone(func(savePath string) {
		a.ZipExtractDlg.MarkDownloaded()
		a.Window.Invalidate()
		go func() {
			choice := a.ZipExtractDlg.WaitChoice()
			dir := filepath.Dir(savePath)
			if choice == zipChoiceExtractDelete || choice == zipChoiceExtractOnly {
				doExtract := true
				conflicts := zipConflicts(savePath, dir)
				if len(conflicts) > 0 {
					ch := make(chan bool, 1)
					a.ConfirmDlg.ShowWithCancel(
						"Overwrite files",
						formatConflictMessage(conflicts),
						"Overwrite",
						func() { ch <- true },
						func() { ch <- false },
					)
					a.Window.Invalidate()
					doExtract = <-ch
				}
				if doExtract {
					extractZip(savePath, dir)
					if choice == zipChoiceExtractDelete {
						removeWithRetry(savePath)
					}
				}
			}
		}()
	})

	// Initialize swarm manager
	conn.Swarm = p2p.NewSwarmManager(conn.UserID, conn.StunURL, sendWS, invalidate)

	// Initialize MountManager for shared directories
	conn.Mounts = mount.NewMountManager(serverURL, client, func(shareID, fileID, savePath string) error {
		if conn.P2P == nil {
			return fmt.Errorf("P2P not available")
		}
		// Find ownerID for this share
		a.mu.RLock()
		var ownerID string
		for _, s := range conn.SharedWithMe {
			if s.ID == shareID {
				ownerID = s.OwnerID
				break
			}
		}
		a.mu.RUnlock()
		if ownerID == "" {
			return fmt.Errorf("owner not found for share %s", shareID)
		}

		// Call RequestTransfer API
		resp, err := conn.Client.RequestTransfer(shareID, fileID)
		if err != nil {
			return fmt.Errorf("request transfer: %w", err)
		}
		transferID, _ := resp["transfer_id"].(string)
		if transferID == "" {
			return fmt.Errorf("no transfer_id in response")
		}

		// Wait for registration at the owner
		time.Sleep(500 * time.Millisecond)

		// Start P2P download
		conn.P2P.RequestDownload(ownerID, transferID, savePath)

		// Wait for completion (polling)
		for i := 0; i < 600; i++ { // max 10 minutes
			time.Sleep(time.Second)
			if conn.P2P.IsDownloaded(transferID) {
				return nil
			}
			if conn.P2P.IsUnavailable(transferID) {
				return fmt.Errorf("transfer unavailable")
			}
		}
		return fmt.Errorf("transfer timeout")
	}, func(shareID, fileName, relativePath, stagedPath string, fileSize int64) error {
		// Upload: call RequestUpload API → get uploadID → register staged file → poll
		if conn.P2P == nil {
			return fmt.Errorf("P2P not available")
		}

		resp, err := conn.Client.RequestUpload(shareID, fileName, fileSize, relativePath)
		if err != nil {
			return fmt.Errorf("request upload: %w", err)
		}
		uploadID, _ := resp["upload_id"].(string)
		if uploadID == "" {
			return fmt.Errorf("no upload_id in response")
		}

		// Register staged file in P2P — owner sends file.request and HandleRequest auto-responds
		conn.P2P.RegisterFileForShare(uploadID, stagedPath, fileName, fileSize)

		// Wait for completion (owner downloads the file via P2P)
		for i := 0; i < 600; i++ { // max 10 minutes
			time.Sleep(time.Second)
			if conn.P2P.IsTransferSent(uploadID) {
				return nil
			}
			if conn.P2P.IsUnavailable(uploadID) {
				return fmt.Errorf("upload unavailable")
			}
		}
		return fmt.Errorf("upload timeout")
	}, func(shareID, relativePath, fileName string) error {
		// Delete: zavolat DeleteShareFile API
		return conn.Client.DeleteShareFile(shareID, relativePath, fileName)
	})

	go a.handleWSEvents(conn)

	a.mu.Lock()
	a.Servers = append(a.Servers, conn)
	a.mu.Unlock()

	// Auto-remount previously mounted shares
	if mounted := store.GetMountedShares(a.PublicKey, serverURL); len(mounted) > 0 {
		go func() {
			for shareID, msi := range mounted {
				if conn.Mounts == nil {
					continue
				}
				// Determine current canWrite from server data (not from saved state)
				canWrite := msi.CanWrite
				for _, s := range conn.SharedWithMe {
					if s.ID == shareID {
						canWrite = s.CanWrite
						break
					}
				}
				info, err := conn.Mounts.MountPreferred(shareID, msi.DisplayName, msi.DriveLetter, msi.Port, canWrite)
				if err != nil {
					log.Printf("Auto-remount %s: %v", msi.DisplayName, err)
				} else {
					log.Printf("Auto-remounted %s at %s (port=%d, canWrite=%v)", msi.DisplayName, info.Path, info.Port, canWrite)
				}
			}
			// Persist to save potentially new port/letter
			if v := a.SharesView; v != nil {
				v.persistMountedShares(conn)
			}
			a.Window.Invalidate()
		}()
	}

	return nil
}

// ConnectServer connects to a server and adds it to the list.
// username is optional — empty string means existing user (no registration).
// inviteCode is optional — used for servers with closed registration.
func (a *App) ConnectServer(serverURL string, username string, inviteCode ...string) error {
	if !strings.HasPrefix(serverURL, "http") {
		serverURL = "http://" + serverURL
	}
	serverURL = strings.TrimRight(serverURL, "/")
	// Strip scheme to check for an explicit port (works for both http:// and https://)
	after := serverURL
	after = strings.TrimPrefix(after, "https://")
	after = strings.TrimPrefix(after, "http://")
	if !strings.Contains(after, ":") {
		serverURL += ":9021"
	}

	// Already connected?
	a.mu.RLock()
	for i, s := range a.Servers {
		if s.URL == serverURL {
			a.mu.RUnlock()
			a.mu.Lock()
			a.ActiveServer = i
			a.Mode = ViewChannels
			a.mu.Unlock()
			return nil
		}
	}
	a.mu.RUnlock()

	// Warn about unencrypted HTTP connections
	if strings.HasPrefix(serverURL, "http://") {
		go func() {
			a.Toasts.Warning("Connecting over unencrypted HTTP. Traffic may be intercepted.")
			a.Window.Invalidate()
		}()
	}

	client := api.NewClient(serverURL)

	invite := ""
	if len(inviteCode) > 0 {
		invite = inviteCode[0]
	}

	challengeResp, err := client.Challenge(a.PublicKey, username, invite, device.GetDeviceID(), device.GetHardwareHash())
	if err != nil {
		return fmt.Errorf("challenge: %w", err)
	}

	sig, err := crypto.Sign(a.SecretKey, challengeResp.Nonce)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	verifyResp, err := client.Verify(a.PublicKey, challengeResp.Nonce, sig)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	client.SetTokens(verifyResp.AccessToken, verifyResp.RefreshToken)
	store.UpdateServerToken(a.PublicKey, serverURL, verifyResp.RefreshToken)

	info, _ := client.GetServerInfo()
	name := serverURL
	addDescription := ""
	if info != nil && info.Name != "" {
		name = info.Name
		addDescription = info.Description
	}

	// Save server name to store
	store.UpdateServerName(a.PublicKey, serverURL, name)

	addIconURL := ""
	addStunURL := ""
	addGSEnabled := false
	addSwarmEnabled := false
	addAutoMod := false
	if info != nil {
		addIconURL = info.IconURL
		addStunURL = info.StunURL
		addGSEnabled = info.GameServersEnabled
		addSwarmEnabled = info.SwarmSharingEnabled
		addAutoMod = info.AutoModEnabled
	}

	if err := a.setupServerConnection(serverURL, name, addDescription, addIconURL, addStunURL, addGSEnabled, addSwarmEnabled, addAutoMod, client, verifyResp.User.ID); err != nil {
		return err
	}

	a.mu.Lock()
	a.ActiveServer = len(a.Servers) - 1
	a.Mode = ViewChannels
	a.ShowAddServer = false
	a.mu.Unlock()

	return nil
}

func (a *App) loadServerData(conn *ServerConnection) {
	channels, err := conn.Client.GetChannels()
	if err == nil {
		conn.Channels = channels
		sort.Slice(conn.Channels, func(i, j int) bool {
			return conn.Channels[i].Position < conn.Channels[j].Position
		})
		if conn.ActiveChannelID == "" {
			for _, ch := range channels {
				if ch.Type == "" || ch.Type == "text" {
					conn.ActiveChannelID = ch.ID
					conn.ActiveChannelName = ch.Name
					break
				}
			}
		}
	}

	cats, err := conn.Client.GetCategories()
	if err == nil {
		conn.Categories = cats
	}

	users, err := conn.Client.GetUsers()
	if err == nil {
		conn.Users = users
		conn.Members = users
		// Auto-create contacts
		if a.Contacts != nil {
			for _, u := range users {
				if u.PublicKey != "" {
					name := u.DisplayName
					if name == "" {
						name = u.Username
					}
					a.Contacts.EnsureContact(u.PublicKey, name, conn.URL, conn.Name)
				}
			}
		}
	}

	convs, err := conn.Client.GetDMConversations()
	if err == nil {
		conn.DMConversations = convs
		// Set unread indicators for conversations with pending messages
		totalPending := 0
		for _, c := range convs {
			if c.PendingCount > 0 {
				conn.UnreadDMCount[c.ID] = c.PendingCount
				totalPending += c.PendingCount
			}
		}
		// Notify about pending DMs received while offline
		if totalPending > 0 {
			msg := fmt.Sprintf("You have %d unread message(s)", totalPending)
			a.Toasts.Info(msg)
			go func() {
				PlayDMSound()
				if a.NotifMgr != nil {
					title := conn.Name + " - Direct Messages"
					a.NotifMgr.NotifyForced(title, msg)
				}
			}()
		}
	}

	friends, err := conn.Client.GetFriends()
	if err == nil {
		conn.Friends = friends
	}

	freqs, err := conn.Client.GetFriendRequests()
	if err == nil {
		conn.FriendRequests = freqs.Incoming
		conn.SentFriendRequests = freqs.Sent
	}

	blocks, err := conn.Client.GetBlocks()
	if err == nil {
		conn.BlockedUsers = blocks
		// Sync to client-side cross-server blocklist
		if a.Contacts != nil {
			for _, u := range blocks {
				if u.PublicKey != "" {
					a.Contacts.SetBlocked(u.PublicKey, true)
				}
			}
		}
	}

	invites, errInv := conn.Client.GetInvites()
	if errInv == nil {
		conn.Invites = invites
	}

	roles, errRoles := conn.Client.GetRoles()
	if errRoles == nil {
		conn.Roles = roles
	}

	// Load role mapping for all users (for role colors)
	conn.UserRolesMap = make(map[string]map[string]bool)
	for _, u := range conn.Members {
		userRoles, err := conn.Client.GetUserRoles(u.ID)
		if err == nil && len(userRoles) > 0 {
			roleIDs := make(map[string]bool, len(userRoles))
			for _, r := range userRoles {
				roleIDs[r.ID] = true
			}
			conn.UserRolesMap[u.ID] = roleIDs
		}
	}

	// Calculate permissions of the current user
	myRoleIDs := conn.UserRolesMap[conn.UserID]
	var perms int64
	for _, r := range roles {
		if myRoleIDs[r.ID] {
			perms |= r.Permissions
		}
	}
	// Owner has full access
	for _, u := range conn.Users {
		if u.ID == conn.UserID && u.IsOwner {
			perms = 0x7FFFFFFFFFFFFFFF
			break
		}
	}
	conn.MyPermissions = perms

	groups, errGroups := conn.Client.GetGroups()
	if errGroups == nil {
		conn.Groups = groups
	}

	emojis, errEmojis := conn.Client.GetEmojis()
	if errEmojis == nil {
		conn.Emojis = emojis
	}

	voiceResp, errVoice := conn.Client.GetVoiceState()
	if errVoice == nil && voiceResp != nil {
		conn.VoiceState = voiceResp.Channels
		if voiceResp.ScreenSharers != nil {
			conn.ScreenSharers = voiceResp.ScreenSharers
		}
		if voiceResp.LiveWhiteboards != nil {
			conn.LiveWhiteboards = voiceResp.LiveWhiteboards
		}
	}

	lanResp, errLAN := conn.Client.GetLANParties()
	if errLAN == nil && lanResp != nil {
		conn.LANParties = lanResp.Parties
		conn.LANMembers = lanResp.Members
	}

	// VPN Tunnels (silent failure — server may not have WG enabled)
	if tunnels, err := conn.Client.GetTunnels(); err == nil {
		conn.Tunnels = tunnels
	}

	// Game servers (silent failure — server may not have the feature enabled)
	if gsInstances, err := conn.Client.GetGameServers(); err == nil {
		conn.GameServers = gsInstances
	}

	// Load game server members in the background
	if a.GameServers != nil {
		a.GameServers.LoadMembers(conn)
	}

	sharesResp, errShares := conn.Client.GetShares()
	if errShares == nil && sharesResp != nil {
		if sharesResp.Own != nil {
			conn.MyShares = sharesResp.Own
		}
		if sharesResp.Accessible != nil {
			conn.SharedWithMe = sharesResp.Accessible
		}
		// Update canWrite on existing mounts according to current permissions
		if conn.Mounts != nil {
			for _, s := range conn.SharedWithMe {
				conn.Mounts.UpdateCanWrite(s.ID, s.CanWrite)
			}
		}
	}

	if conn.ActiveChannelID != "" {
		msgs, err := conn.Client.GetMessages(conn.ActiveChannelID, "", 50)
		if err == nil {
			reverseMessages(msgs)
			conn.Messages = msgs
		}
		pins, err := conn.Client.GetPinnedMessages(conn.ActiveChannelID)
		if err == nil {
			a.MsgView.pinnedMsgs = pins
		}
		a.MsgView.pinSeenID = store.GetPinSeenID(a.PublicKey, conn.URL, conn.ActiveChannelID)
	}
}

func reverseMessages(msgs []api.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func reverseDMMessages(msgs []api.DMPendingMessage) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func (a *App) downloadServerIcon(conn *ServerConnection, url string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("download server icon: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		log.Printf("decode server icon: %v", err)
		return
	}
	a.mu.Lock()
	conn.Icon = img
	a.mu.Unlock()
	a.Window.Invalidate()
}

func (a *App) SelectChannel(id, name string) {
	conn := a.Conn()
	if conn == nil {
		return
	}

	// Close whiteboard overlay when switching channels
	if a.WhiteboardView != nil && a.WhiteboardView.Visible {
		a.WhiteboardView.Visible = false
	}

	a.mu.Lock()
	conn.ActiveChannelID = id
	conn.ActiveChannelName = name
	delete(conn.UnreadCount, id)
	// Clear DM viewed state so notifications work
	conn.ActiveDMID = ""
	conn.ActiveDMPeerKey = ""
	a.Mode = ViewChannels

	a.mu.Unlock()

	// Check if this is an LFG channel — load board instead of messages
	isLFG := false
	for _, ch := range conn.Channels {
		if ch.ID == id && ch.Type == "lfg" {
			isLFG = true
			break
		}
	}

	if isLFG {
		a.LFGBoard.Load(id)
		return
	}

	a.MsgView.ResetScrollState()

	go func() {
		msgs, err := conn.Client.GetMessages(id, "", 50)
		if err != nil {
			log.Printf("GetMessages error: %v", err)
			return
		}
		reverseMessages(msgs)
		a.mu.Lock()
		conn.Messages = msgs
		a.mu.Unlock()
		a.Window.Invalidate()
	}()

	// Load pinSeenID from saved state
	a.MsgView.pinSeenID = store.GetPinSeenID(a.PublicKey, conn.URL, id)

	// Load pinned messages for display in the pin bar
	go func() {
		pins, err := conn.Client.GetPinnedMessages(id)
		if err == nil {
			a.MsgView.pinnedMsgs = pins
			a.Window.Invalidate()
		}
	}()
}

func (a *App) SelectGroup(groupID, groupName string) {
	conn := a.Conn()
	if conn == nil {
		return
	}

	a.mu.Lock()
	conn.ActiveGroupID = groupID
	// Clear DM viewed state so notifications work
	conn.ActiveDMID = ""
	conn.ActiveDMPeerKey = ""
	a.Mode = ViewGroup
	delete(conn.UnreadGroups, groupID)
	a.mu.Unlock()

	// Load messages from local history
	if a.GroupHistory != nil {
		stored := a.GroupHistory.GetMessages(groupID)
		msgs := make([]api.GroupMessage, len(stored))
		for i, m := range stored {
			gm := api.GroupMessage{
				ID:               m.ID,
				GroupID:          m.GroupID,
				SenderID:         m.SenderID,
				DecryptedContent: m.Content,
				CreatedAt:        m.CreatedAt,
			}
			for _, sa := range m.Attachments {
				gm.Attachments = append(gm.Attachments, api.Attachment{
					Filename: sa.Filename,
					URL:      sa.URL,
					Size:     sa.Size,
					MimeType: sa.MimeType,
				})
			}
			msgs[i] = gm
		}
		a.mu.Lock()
		conn.GroupMessages = msgs
		a.mu.Unlock()
	}
	a.Window.Invalidate()
}

func (a *App) SelectDM(convID string) {
	conn := a.Conn()
	if conn == nil {
		return
	}

	a.mu.Lock()
	conn.ActiveDMID = convID
	a.Mode = ViewDM
	delete(conn.UnreadDMCount, convID)
	conn.TypingDMUsers = make(map[string]time.Time)

	var peerKey string
	for _, conv := range conn.DMConversations {
		if conv.ID == convID {
			for _, p := range conv.Participants {
				if p.UserID != conn.UserID {
					peerKey = p.PublicKey
					conn.ActiveDMPeerKey = peerKey
					break
				}
			}
			break
		}
	}
	secretKey := a.SecretKey

	// Load local history first (instant display)
	if a.DMHistory != nil {
		stored := a.DMHistory.GetMessages(convID)
		pending := make([]api.DMPendingMessage, len(stored))
		for i, m := range stored {
			pending[i] = api.DMPendingMessage{
				ID:               m.ID,
				ConversationID:   m.ConversationID,
				SenderID:         m.SenderID,
				DecryptedContent: m.Content,
				CreatedAt:        m.CreatedAt,
			}
		}
		conn.DMMessages = pending
	}
	a.mu.Unlock()

	// Fetch pending messages from server, decrypt, merge with history
	go func() {
		msgs, err := conn.Client.GetDMPending(convID)
		if err != nil {
			log.Printf("GetDMPending error: %v", err)
			a.Window.Invalidate()
			return
		}
		reverseDMMessages(msgs)

		// Decrypt and save new pending messages to local history
		if a.DMHistory != nil && peerKey != "" && secretKey != "" {
			for _, msg := range msgs {
				decrypted, err := crypto.DecryptDM(secretKey, peerKey, msg.EncryptedContent)
				if err == nil {
					a.DMHistory.AddMessage(store.StoredDMMessage{
						ID:             msg.ID,
						ConversationID: msg.ConversationID,
						SenderID:       msg.SenderID,
						Content:        decrypted,
						CreatedAt:      msg.CreatedAt,
					})
				}
			}
			a.DMHistory.Save()

			// Rebuild message list from full history
			stored := a.DMHistory.GetMessages(convID)
			pending := make([]api.DMPendingMessage, len(stored))
			for i, m := range stored {
				pending[i] = api.DMPendingMessage{
					ID:               m.ID,
					ConversationID:   m.ConversationID,
					SenderID:         m.SenderID,
					DecryptedContent: m.Content,
					CreatedAt:        m.CreatedAt,
				}
			}
			a.mu.Lock()
			conn.DMMessages = pending
			a.mu.Unlock()
		} else {
			a.mu.Lock()
			conn.DMMessages = msgs
			a.mu.Unlock()
		}
		a.Window.Invalidate()
	}()
}

// handlePendingDeepLink processes a deep link passed from the command line.
func (a *App) handlePendingDeepLink() {
	link := a.PendingDeepLink
	if link == "" {
		return
	}
	a.PendingDeepLink = ""

	dl := ParseDeepLink(link)
	if dl == nil {
		return
	}

	switch dl.Type {
	case "contact":
		if dl.Key != "" && a.Contacts != nil {
			name := dl.Name
			if name == "" {
				name = ShortenKey(dl.Key)
			}
			a.Contacts.EnsureContact(dl.Key, name, "", "")
			log.Printf("Deep link: added contact %s (%s)", name, ShortenKey(dl.Key))
		}
	case "invite":
		if dl.Server != "" && dl.Code != "" {
			log.Printf("Deep link: invite for %s code=%s (queued for confirmation)", dl.Server, dl.Code)
			// Queue for confirmation — don't auto-connect to untrusted servers
			a.PendingDeepLinkInvite = &DeepLinkInvite{Server: dl.Server, Code: dl.Code}
			a.Window.Invalidate()
		}
	}
}

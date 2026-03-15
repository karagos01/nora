package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/config"
	"nora/database"
	"nora/database/queries"
	"nora/gameserver"
	"nora/handlers"
	"nora/middleware"
	"nora/models"
	"nora/moderation"
	"nora/util"
	"path/filepath"
	"nora/wg"
	"nora/ws"
	"os"
	"os/signal"
	"syscall"
	"strconv"
	"time"

	"github.com/google/uuid"
)

func main() {
	// Structured JSON logger on stderr
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfgPath := "nora.toml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Auth.JWTSecret == "" || cfg.Auth.JWTSecret == "CHANGE-ME-to-random-64-char-string" {
		log.Fatal("Please set a proper jwt_secret in nora.toml")
	}

	db, err := database.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	if err := db.SeedDefaults(); err != nil {
		log.Fatalf("Failed to seed defaults: %v", err)
	}

	// Load runtime settings from DB (override config from toml)
	settingsQ := &queries.SettingsQueries{DB: db.DB}
	if v := settingsQ.Get("server_name", ""); v != "" {
		cfg.Server.Name = v
	}
	if v := settingsQ.Get("server_description", ""); v != "" {
		cfg.Server.Description = v
	}
	if v := settingsQ.Get("max_upload_size_mb", ""); v != "" {
		if mb, err := strconv.Atoi(v); err == nil {
			cfg.Uploads.MaxSizeMB = mb
		}
	}
	if v := settingsQ.Get("open_registration", ""); v != "" {
		cfg.Registration.Open = v == "true"
	}
	if v := settingsQ.Get("storage_max_mb", ""); v != "" {
		if mb, err := strconv.Atoi(v); err == nil {
			cfg.Uploads.StorageMaxMB = mb
		}
	}
	if v := settingsQ.Get("channel_history_limit", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Uploads.ChannelHistoryLimit = n
		}
	}
	serverIconURL := settingsQ.Get("server_icon_url", "")

	jwtSvc := auth.NewJWTService(cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL.Duration)
	hub := ws.NewHub()
	go hub.Run()

	// LAN kick callback is set after deps creation (below)

	// Cleanup old pending DM messages (>30 days) every hour
	dmQueries := &queries.DMQueries{DB: db.DB}
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := dmQueries.CleanupOldPending(30); err == nil && n > 0 {
				slog.Info("cleaned up old pending DM messages", "count", n)
			}
		}
	}()

	rl := middleware.NewEndpointRateLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)

	storageQ := &queries.StorageQueries{DB: db.DB}

	uploadSessions := handlers.NewUploadSessionStore()
	uploadLimiter := handlers.NewUploadRateLimiter(30) // max 30 uploads/minute/user

	// WireGuard LAN Party manager
	var wgManager *wg.Manager
	if cfg.LAN.Enabled {
		var err error
		wgManager, err = wg.NewManager(cfg.LAN)
		if err != nil {
			slog.Warn("WireGuard manager initialization failed", "error", err)
		} else {
			// Server-level keypair — load or generate
			privKey := settingsQ.Get("wg_private_key", "")
			pubKey := settingsQ.Get("wg_public_key", "")
			if privKey == "" || pubKey == "" {
				privKey, pubKey, err = wg.GenerateKeypair()
				if err != nil {
					slog.Warn("WG keypair generation failed", "error", err)
				} else {
					settingsQ.Set("wg_private_key", privKey)
					settingsQ.Set("wg_public_key", pubKey)
					slog.Info("generated new WireGuard server keypair")
				}
			}
			wgManager.SetKeys(privKey, pubKey)
			slog.Info("WireGuard LAN manager initialized", "interface", cfg.LAN.Interface, "port", cfg.LAN.Port)
		}
	}

	// Game server manager — always initialize (file operations don't need Docker)
	gsEnabled := cfg.GameServers.Enabled
	if v := settingsQ.Get("game_servers_enabled", ""); v != "" {
		gsEnabled = v == "true"
	}
	// Swarm sharing
	swarmEnabled := false
	if v := settingsQ.Get("swarm_sharing_enabled", ""); v == "true" {
		swarmEnabled = true
	}

	gsPresetsDir := filepath.Join(filepath.Dir(cfg.GameServers.DataDir), "gameserver-presets")
	gsManager := gameserver.NewManager(cfg.GameServers.DataDir, gsPresetsDir)
	if !gsManager.DockerAvailable() {
		slog.Warn("Docker is not available, game server start/stop will not work")
	}
	slog.Info("game server manager ready", "data_dir", cfg.GameServers.DataDir, "enabled", gsEnabled)

	// Auto-moderation
	autoMod := moderation.New()
	if v := settingsQ.Get("automod_enabled", ""); v == "true" {
		autoMod.Enabled = true
	}
	if v := settingsQ.Get("automod_word_filter", ""); v != "" {
		var words []string
		if json.Unmarshal([]byte(v), &words) == nil {
			autoMod.SetWordFilter(words)
		}
	}
	if v := settingsQ.Get("automod_spam_max_messages", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 {
			autoMod.SpamMaxMessages = n
		}
	}
	if v := settingsQ.Get("automod_spam_window_seconds", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 {
			autoMod.SpamWindowSeconds = n
		}
	}
	if v := settingsQ.Get("automod_spam_timeout_seconds", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 60 {
			autoMod.SpamTimeoutSeconds = n
		}
	}
	// Find owner ID for auto-timeout issued_by
	usersQ := &queries.UserQueries{DB: db.DB}
	allUsers, _ := usersQ.List()
	for _, u := range allUsers {
		if u.IsOwner {
			autoMod.OwnerID = u.ID
			break
		}
	}

	deps := &handlers.Deps{
		DB:            db,
		DBPath:        cfg.Database.Path,
		Hub:           hub,
		JWTService:    jwtSvc,
		RefreshTTL:    cfg.Auth.RefreshTokenTTL.Duration,
		ChallengeTTL:  cfg.Auth.ChallengeTTL.Duration,
		OpenReg:       cfg.Registration.Open,
		UploadsDir:    cfg.Uploads.Dir,
		MaxUploadSize: int64(cfg.Uploads.MaxSizeMB) * 1024 * 1024,
		AllowedTypes:  cfg.Uploads.AllowedTypes,
		ServerName:          cfg.Server.Name,
		ServerDesc:          cfg.Server.Description,
		ServerIconURL:       serverIconURL,
		SourceURL:           cfg.Server.SourceURL,
		StunURL:             cfg.Server.StunURL,
		StorageMaxMB:        cfg.Uploads.StorageMaxMB,
		ChannelHistoryLimit: cfg.Uploads.ChannelHistoryLimit,
		Uploads:             uploadSessions,
		UploadLimiter:       uploadLimiter,
		Users:         &queries.UserQueries{DB: db.DB},
		Channels:      &queries.ChannelQueries{DB: db.DB},
		Messages:      &queries.MessageQueries{DB: db.DB},
		Roles:         &queries.RoleQueries{DB: db.DB},
		Invites:       &queries.InviteQueries{DB: db.DB},
		RefreshTokens: &queries.RefreshTokenQueries{DB: db.DB},
		DMs:           &queries.DMQueries{DB: db.DB},
		Bans:          &queries.BanQueries{DB: db.DB},
		Attachments:   &queries.AttachmentQueries{DB: db.DB},
		AuthChallenges: &queries.AuthChallengeQueries{DB: db.DB},
		Timeouts:       &queries.TimeoutQueries{DB: db.DB},
		Friends:        &queries.FriendQueries{DB: db.DB},
		Settings:       settingsQ,
		FriendRequests: &queries.FriendRequestQueries{DB: db.DB},
		Blocks:         &queries.BlockQueries{DB: db.DB},
		Groups:         &queries.GroupQueries{DB: db.DB},
		Emojis:         &queries.EmojiQueries{DB: db.DB},
		Categories:     &queries.CategoryQueries{DB: db.DB},
		Reactions:      &queries.ReactionQueries{DB: db.DB},
		IPBans:         &queries.IPBanQueries{DB: db.DB},
		Storage:        storageQ,
		AuditLog:       &queries.AuditLogQueries{DB: db.DB},
		LinkPreviews: &queries.LinkPreviewQueries{DB: db.DB},
		Polls:       &queries.PollQueries{DB: db.DB},
		Webhooks:    &queries.WebhookQueries{DB: db.DB},
		GalleryQ:    &queries.GalleryQueries{DB: db.DB},
		FileStorage: &queries.FileStorageQueries{DB: db.DB},
		Shares:      &queries.ShareQueries{DB: db.DB},
		WG:             wgManager,
		LAN:            &queries.LANQueries{DB: db.DB},
		Whiteboards:        &queries.WhiteboardQueries{DB: db.DB},
		GameServersEnabled: gsEnabled,
		GameServerQ:        &queries.GameServerQueries{DB: db.DB},
		GameServerMgr:      gsManager,
		SwarmSharingEnabled: swarmEnabled,
		SwarmSeeds:          &queries.SwarmQueries{DB: db.DB},
		Scheduled:           &queries.ScheduledMessageQueries{DB: db.DB},
		KanbanQ:             &queries.KanbanQueries{DB: db.DB},
		CalendarQ:           &queries.CalendarQueries{DB: db.DB},
		Tunnels:             &queries.TunnelQueries{DB: db.DB},
		ChannelPermQ:        &queries.ChannelPermQueries{DB: db.DB},
		AutoMod:             autoMod,
		DeviceBans:          &queries.DeviceBanQueries{DB: db.DB},
		UserDevices:         &queries.UserDeviceQueries{DB: db.DB},
		InviteChain:         &queries.InviteChainQueries{DB: db.DB},
		Quarantine:          &queries.QuarantineQueries{DB: db.DB},
		Approvals:           &queries.ApprovalQueries{DB: db.DB},
		SecurityCfg:         cfg.Security,
		RegMode:             config.ResolveRegMode(cfg.Registration),
	}

	// User status callback for presence init batch
	hub.SetUserStatusFn(func(userID string) (string, string) {
		u, err := deps.Users.GetByID(userID)
		if err != nil {
			return "", ""
		}
		return u.Status, u.StatusText
	})

	// LAN auto-kick: after 5min offline remove user from all LAN parties
	hub.SetLANKickCallback(func(userID string) {
		deps.KickUserFromAllParties(userID)
	})

	// Lobby voice channels callbacks
	hub.SetLobbyCallbacks(
		// createFn: creates sub-channel in DB
		func(lobbyID, name string) (string, error) {
			lobby, err := deps.Channels.GetByID(lobbyID)
			if err != nil {
				return "", err
			}
			id, _ := uuid.NewV7()
			ch := &models.Channel{
				ID:         id.String(),
				Name:       name,
				Type:       "voice",
				ParentID:   &lobbyID,
				CategoryID: lobby.CategoryID,
				Position:   lobby.Position,
			}
			if err := deps.Channels.Create(ch); err != nil {
				return "", err
			}
			ch, _ = deps.Channels.GetByID(ch.ID)
			msg, _ := ws.NewEvent(ws.EventChannelCreate, ch)
			hub.Broadcast(msg)
			return ch.ID, nil
		},
		// deleteFn: deletes sub-channel from DB + broadcast
		func(channelID string) {
			deps.Channels.Delete(channelID)
			msg, _ := ws.NewEvent(ws.EventChannelDelete, map[string]string{"id": channelID})
			hub.Broadcast(msg)
		},
		// isLobbyFn: checks if channel is type "lobby"
		func(channelID string) bool {
			ch, err := deps.Channels.GetByID(channelID)
			return err == nil && ch.Type == "lobby"
		},
		// renameFn: renames sub-channel in DB
		func(channelID, name string) error {
			ch, err := deps.Channels.GetByID(channelID)
			if err != nil {
				return err
			}
			ch.Name = name
			return deps.Channels.Update(ch)
		},
	)

	// WireGuard startup — create interface and add all peers from all active parties
	if wgManager != nil && wgManager.PublicKey() != "" {
		allPeers, err := deps.LAN.GetAllActivePeers()
		if err == nil {
			peers := make([]wg.PeerInfo, len(allPeers))
			for i, m := range allPeers {
				peers[i] = wg.PeerInfo{PublicKey: m.PublicKey, AssignedIP: m.AssignedIP}
			}
			privKey := settingsQ.Get("wg_private_key", "")
			if err := wgManager.RecoverInterface(privKey, peers); err != nil {
				slog.Warn("WireGuard startup failed", "error", err)
			}
		}
	}

	// LAN startup: start kick timers for all LAN party members
	// Those who connect within 5 minutes will have their timer cancelled. Those who don't will be kicked.
	if lanUserIDs, err := deps.LAN.GetAllActiveMemberUserIDs(); err == nil && len(lanUserIDs) > 0 {
		for _, uid := range lanUserIDs {
			hub.StartLANKickTimer(uid)
		}
		slog.Info("LAN: started kick timers for party members", "count", len(lanUserIDs))
	}

	// Game server firewall: restore iptables rules for running room servers
	if gsEnabled {
		if roomServers, err := deps.GameServerQ.GetRunningRoomServers(); err == nil && len(roomServers) > 0 {
			for _, gs := range roomServers {
				gs := gs
				deps.RefreshGameServerFirewall(&gs)
			}
			slog.Info("game servers: restored firewall for room servers", "count", len(roomServers))
		}
	}

	// OnConnect callback — IP refresh for firewall
	hub.SetOnConnect(func(userID string, r *http.Request) {
		ip := util.GetClientIP(r)
		u, err := deps.Users.GetByID(userID)
		if err != nil || u.LastIP == ip {
			return
		}
		deps.Users.UpdateLastIP(userID, ip)
		if !gsEnabled {
			return
		}
		servers, _ := deps.GameServerQ.GetRunningRoomServers()
		for _, gs := range servers {
			gs := gs
			if deps.GameServerQ.IsMember(gs.ID, userID) {
				deps.RefreshGameServerFirewall(&gs)
			}
		}
	})

	// Storage auto-cleanup goroutine (every hour)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if deps.ChannelHistoryLimit > 0 {
				deps.TrimAllChannels()
			}
			if deps.StorageMaxMB > 0 {
				storageCleanupBySize(deps)
			}
		}
	}()

	// Ban expiry cleanup (every hour)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, _ := deps.Bans.DeleteExpired(); n > 0 {
				slog.Info("cleaned up expired bans", "count", n)
			}
			if n, _ := deps.DeviceBans.DeleteExpired(); n > 0 {
				slog.Info("cleaned up expired device bans", "count", n)
			}
		}
	}()

	// Scheduled messages dispatcher (every 30s)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			deps.DispatchScheduledMessages()
		}
	}()

	// Event reminder dispatcher (every 60s)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			deps.DispatchEventReminders()
		}
	}()

	apiRouter := handlers.NewRouter(deps)

	var handler http.Handler = apiRouter
	handler = rl.Middleware(handler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("server shutting down...")
		if wgManager != nil {
			wgManager.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	slog.Info("NORA server starting", "addr", addr, "server_name", cfg.Server.Name)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}

	slog.Info("server stopped")
}

func storageCleanupBySize(deps *handlers.Deps) {
	maxBytes := int64(deps.StorageMaxMB) * 1024 * 1024

	// Calculate current uploads size
	var totalBytes int64
	entries, err := os.ReadDir(deps.UploadsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			totalBytes += info.Size()
		}
	}

	if totalBytes <= maxBytes {
		return
	}

	// Delete oldest attachments until we fit within the limit
	paths, err := deps.Storage.OldestAttachmentPaths(100)
	if err != nil || len(paths) == 0 {
		return
	}

	deleted, err := deps.Storage.DeleteMessagesWithAttachments(paths)
	if err != nil {
		slog.Error("storage cleanup: message deletion failed", "error", err)
		return
	}

	handlers.DeleteAttachmentFiles(deps.UploadsDir, paths, deps.Attachments)
	if deleted > 0 {
		slog.Info("storage cleanup: deleted messages with attachments", "deleted", deleted)
	}
}

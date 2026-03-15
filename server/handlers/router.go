package handlers

import (
	"net/http"
	"nora/auth"
	"nora/config"
	"nora/database"
	"nora/database/queries"
	"nora/gameserver"
	"nora/middleware"
	"nora/moderation"
	"nora/wg"
	"nora/ws"
	"sync"
	"time"
)

type Deps struct {
	DB            *database.DB
	DBPath        string
	Hub           *ws.Hub
	JWTService    *auth.JWTService
	RefreshTTL    time.Duration
	ChallengeTTL  time.Duration
	OpenReg       bool
	UploadsDir    string
	MaxUploadSize int64
	AllowedTypes  []string
	ServerName    string
	ServerDesc    string
	ServerIconURL string
	SourceURL     string
	StunURL       string

	StorageMaxMB        int
	ChannelHistoryLimit int
	Uploads             *UploadSessionStore
	UploadLimiter       *UploadRateLimiter

	Users          *queries.UserQueries
	Channels       *queries.ChannelQueries
	Messages       *queries.MessageQueries
	Roles          *queries.RoleQueries
	Invites        *queries.InviteQueries
	RefreshTokens  *queries.RefreshTokenQueries
	DMs            *queries.DMQueries
	Bans           *queries.BanQueries
	Attachments    *queries.AttachmentQueries
	AuthChallenges *queries.AuthChallengeQueries
	Timeouts       *queries.TimeoutQueries
	Friends        *queries.FriendQueries
	Settings       *queries.SettingsQueries
	FriendRequests *queries.FriendRequestQueries
	Blocks         *queries.BlockQueries
	Groups         *queries.GroupQueries
	Emojis         *queries.EmojiQueries
	Categories     *queries.CategoryQueries
	Reactions      *queries.ReactionQueries
	IPBans         *queries.IPBanQueries
	Storage        *queries.StorageQueries
	AuditLog       *queries.AuditLogQueries

	LinkPreviews *queries.LinkPreviewQueries
	Polls       *queries.PollQueries
	Webhooks    *queries.WebhookQueries
	GalleryQ    *queries.GalleryQueries
	FileStorage *queries.FileStorageQueries
	Shares      *queries.ShareQueries

	Whiteboards *queries.WhiteboardQueries

	WG  *wg.Manager
	LAN *queries.LANQueries

	GameServersEnabled bool
	GameServerQ        *queries.GameServerQueries
	GameServerMgr      *gameserver.Manager

	SwarmSharingEnabled bool
	SwarmSeeds          *queries.SwarmQueries

	Scheduled *queries.ScheduledMessageQueries

	KanbanQ   *queries.KanbanQueries
	CalendarQ *queries.CalendarQueries

	AutoMod *moderation.AutoMod

	// VPN Tunnels
	Tunnels *queries.TunnelQueries

	// Channel permission overrides
	ChannelPermQ *queries.ChannelPermQueries

	// Ban system
	DeviceBans  *queries.DeviceBanQueries
	UserDevices *queries.UserDeviceQueries
	InviteChain *queries.InviteChainQueries
	Quarantine  *queries.QuarantineQueries
	Approvals   *queries.ApprovalQueries
	SecurityCfg config.SecurityConfig
	RegMode     string

	// SettingsMu protects the fields ServerName, ServerDesc, ServerIconURL, MaxUploadSize,
	// OpenReg, GameServersEnabled, SwarmSharingEnabled against data races.
	SettingsMu sync.RWMutex
}

func NewRouter(d *Deps) http.Handler {
	mux := http.NewServeMux()

	authMW := auth.Middleware(d.JWTService, d.Bans.IsBanned)

	// Health
	mux.HandleFunc("GET /api/health", d.Health)

	// Server info
	mux.HandleFunc("GET /api/server", d.ServerInfo)

	// AGPL compliance
	mux.HandleFunc("GET /api/source", d.SourceDownload)
	mux.HandleFunc("GET /api/source/info", d.SourceInfo)

	// Auth (public) — challenge-response
	mux.HandleFunc("POST /api/auth/challenge", d.Challenge)
	mux.HandleFunc("POST /api/auth/verify", d.Verify)
	mux.HandleFunc("POST /api/auth/refresh", d.Refresh)

	// WebSocket
	mux.HandleFunc("GET /api/ws", ws.UpgradeHandler(d.Hub, d.JWTService, d.Bans.IsBanned))

	// Uploads (public read)
	mux.HandleFunc("GET /api/uploads/", d.ServeUpload)

	// Webhook send (public — token in URL)
	mux.HandleFunc("POST /api/webhooks/{id}/{token}", d.WebhookSend)

	// Game server log stream (WS, auth via query token)
	mux.HandleFunc("GET /api/gameservers/{id}/logs", d.GameServerLogs)

	// Protected endpoints
	protected := http.NewServeMux()

	// Auth (protected)
	protected.HandleFunc("POST /api/auth/logout", d.Logout)

	// Users
	protected.HandleFunc("GET /api/users", d.ListUsers)
	protected.HandleFunc("GET /api/users/{id}", d.GetUser)
	protected.HandleFunc("PATCH /api/users/me", d.UpdateMe)
	protected.HandleFunc("POST /api/users/me/avatar", d.UploadAvatar)
	protected.HandleFunc("DELETE /api/users/me/avatar", d.DeleteAvatar)

	// Channels
	protected.HandleFunc("GET /api/channels", d.ListChannels)
	protected.HandleFunc("POST /api/channels", d.CreateChannel)
	protected.HandleFunc("POST /api/channels/reorder", d.ReorderChannels)
	protected.HandleFunc("GET /api/channels/{id}", d.GetChannel)
	protected.HandleFunc("PATCH /api/channels/{id}", d.UpdateChannel)
	protected.HandleFunc("DELETE /api/channels/{id}", d.DeleteChannel)

	// Channel permission overrides
	protected.HandleFunc("GET /api/channels/{id}/permissions", d.ListChannelPermOverrides)
	protected.HandleFunc("PUT /api/channels/{id}/permissions", d.SetChannelPermOverride)
	protected.HandleFunc("DELETE /api/channels/{id}/permissions/{targetType}/{targetId}", d.DeleteChannelPermOverride)

	// Messages
	protected.HandleFunc("GET /api/channels/{id}/messages", d.ListMessages)
	protected.HandleFunc("POST /api/channels/{id}/messages", d.CreateMessage)
	protected.HandleFunc("PATCH /api/messages/{id}", d.UpdateMessage)
	protected.HandleFunc("DELETE /api/messages/{id}", d.DeleteMessage)
	protected.HandleFunc("PUT /api/messages/{id}/reactions", d.ToggleReaction)
	protected.HandleFunc("PUT /api/messages/{id}/pin", d.PinMessage)
	protected.HandleFunc("GET /api/channels/{id}/pins", d.ListPinnedMessages)
	protected.HandleFunc("GET /api/channels/{id}/messages/search", d.SearchMessages)
	protected.HandleFunc("GET /api/messages/{id}/thread", d.GetMessageThread)
	protected.HandleFunc("GET /api/messages/{id}/history", d.GetMessageEditHistory)

	// Role
	protected.HandleFunc("GET /api/roles", d.ListRoles)
	protected.HandleFunc("POST /api/roles", d.CreateRole)
	protected.HandleFunc("POST /api/roles/swap", d.SwapRolePositions)
	protected.HandleFunc("PATCH /api/roles/{id}", d.UpdateRole)
	protected.HandleFunc("DELETE /api/roles/{id}", d.DeleteRole)
	protected.HandleFunc("GET /api/users/{userId}/roles", d.GetUserRoles)
	protected.HandleFunc("PUT /api/users/{userId}/roles/{roleId}", d.AssignRole)
	protected.HandleFunc("DELETE /api/users/{userId}/roles/{roleId}", d.RemoveRole)

	// Invite
	protected.HandleFunc("GET /api/invites", d.ListInvites)
	protected.HandleFunc("POST /api/invites", d.CreateInvite)
	protected.HandleFunc("DELETE /api/invites/{id}", d.DeleteInvite)

	// Bans & Kick
	protected.HandleFunc("GET /api/bans", d.ListBans)
	protected.HandleFunc("POST /api/bans", d.CreateBan)
	protected.HandleFunc("DELETE /api/bans/{userId}", d.DeleteBan)
	protected.HandleFunc("GET /api/bans/ips", d.ListIPBans)
	protected.HandleFunc("DELETE /api/bans/ips/{ip}", d.DeleteIPBan)
	protected.HandleFunc("POST /api/users/{id}/timeout", d.KickUser)

	// Server settings (owner only)
	protected.HandleFunc("GET /api/server/settings", d.GetSettings)
	protected.HandleFunc("PATCH /api/server/settings", d.UpdateSettings)
	protected.HandleFunc("POST /api/server/icon", d.UploadServerIcon)
	protected.HandleFunc("DELETE /api/server/icon", d.DeleteServerIcon)

	// Storage management (owner only)
	protected.HandleFunc("GET /api/server/storage", d.GetStorageInfo)
	protected.HandleFunc("PATCH /api/server/storage", d.UpdateStorageSettings)

	// Friends
	protected.HandleFunc("GET /api/friends", d.ListFriends)
	protected.HandleFunc("DELETE /api/friends/{userId}", d.RemoveFriend)

	// Friend requests
	protected.HandleFunc("GET /api/friends/requests", d.ListFriendRequests)
	protected.HandleFunc("POST /api/friends/requests", d.SendFriendRequest)
	protected.HandleFunc("POST /api/friends/requests/{id}/accept", d.AcceptFriendRequest)
	protected.HandleFunc("POST /api/friends/requests/{id}/decline", d.DeclineFriendRequest)

	// Blocks
	protected.HandleFunc("GET /api/blocks", d.ListBlocks)
	protected.HandleFunc("POST /api/blocks", d.BlockUser)
	protected.HandleFunc("DELETE /api/blocks/{userId}", d.UnblockUser)

	// DM (E2E encrypted relay)
	protected.HandleFunc("GET /api/dm", d.ListDMConversations)
	protected.HandleFunc("POST /api/dm", d.CreateDMConversation)
	protected.HandleFunc("DELETE /api/dm/{id}", d.DeleteDMConversation)
	protected.HandleFunc("GET /api/dm/{id}/pending", d.GetDMPending)
	protected.HandleFunc("POST /api/dm/{id}/messages", d.CreateDMMessage)

	// Ephemeral Groups (E2E encrypted, server does not store messages)
	protected.HandleFunc("GET /api/groups", d.ListGroups)
	protected.HandleFunc("POST /api/groups", d.CreateGroup)
	protected.HandleFunc("GET /api/groups/{id}", d.GetGroup)
	protected.HandleFunc("DELETE /api/groups/{id}", d.DeleteGroup)
	protected.HandleFunc("POST /api/groups/{id}/members", d.JoinGroup)
	protected.HandleFunc("DELETE /api/groups/{id}/members/{userId}", d.LeaveGroup)
	protected.HandleFunc("POST /api/groups/{id}/messages", d.RelayGroupMessage)
	protected.HandleFunc("POST /api/groups/{id}/invites", d.CreateGroupInvite)
	protected.HandleFunc("GET /api/groups/{id}/invites", d.ListGroupInvites)

	// Emoji
	protected.HandleFunc("GET /api/emojis", d.ListEmojis)
	protected.HandleFunc("POST /api/emojis", d.CreateEmoji)
	protected.HandleFunc("DELETE /api/emojis/{id}", d.DeleteEmoji)

	// Channel categories
	protected.HandleFunc("GET /api/categories", d.ListCategories)
	protected.HandleFunc("POST /api/categories", d.CreateCategory)
	protected.HandleFunc("POST /api/categories/reorder", d.ReorderCategories)
	protected.HandleFunc("PATCH /api/categories/{id}", d.UpdateCategory)
	protected.HandleFunc("DELETE /api/categories/{id}", d.DeleteCategory)

	// Message hiding & bulk operations
	protected.HandleFunc("PUT /api/messages/{id}/hide", d.HideMessage)
	protected.HandleFunc("PUT /api/users/{id}/messages/hide", d.HideUserMessages)
	protected.HandleFunc("DELETE /api/users/{id}/messages", d.DeleteUserMessages)

	// Audit log / Activity
	protected.HandleFunc("GET /api/users/{id}/activity", d.ListUserActivity)
	protected.HandleFunc("GET /api/users/{id}/messages", d.ListUserMessages)

	// Voice state
	protected.HandleFunc("GET /api/voice/state", d.VoiceState)
	protected.HandleFunc("POST /api/voice/move", d.VoiceMove)

	// File uploads
	protected.HandleFunc("POST /api/upload", d.Upload)
	protected.HandleFunc("POST /api/upload/init", d.InitUpload)
	protected.HandleFunc("PATCH /api/upload/{id}", d.UploadChunk)
	protected.HandleFunc("HEAD /api/upload/{id}", d.UploadStatus)

	// Scheduled messages
	protected.HandleFunc("POST /api/channels/{id}/messages/schedule", d.CreateScheduledMessage)
	protected.HandleFunc("GET /api/scheduled-messages", d.ListScheduledMessages)
	protected.HandleFunc("DELETE /api/scheduled-messages/{id}", d.DeleteScheduledMessage)

	// Polls
	protected.HandleFunc("PUT /api/polls/{id}/vote", d.VotePoll)

	// Webhooks
	protected.HandleFunc("GET /api/webhooks", d.ListWebhooks)
	protected.HandleFunc("POST /api/webhooks", d.CreateWebhook)
	protected.HandleFunc("PATCH /api/webhooks/{id}", d.UpdateWebhook)
	protected.HandleFunc("DELETE /api/webhooks/{id}", d.DeleteWebhook)

	// Media gallery
	protected.HandleFunc("GET /api/gallery", d.Gallery)

	// File storage
	protected.HandleFunc("GET /api/storage/folders", d.ListStorageFolders)
	protected.HandleFunc("POST /api/storage/folders", d.CreateStorageFolder)
	protected.HandleFunc("PATCH /api/storage/folders/{id}", d.RenameStorageFolder)
	protected.HandleFunc("DELETE /api/storage/folders/{id}", d.DeleteStorageFolder)
	protected.HandleFunc("GET /api/storage/files", d.ListStorageFiles)
	protected.HandleFunc("POST /api/storage/files", d.UploadStorageFile)
	protected.HandleFunc("PATCH /api/storage/files/{id}", d.RenameStorageFile)
	protected.HandleFunc("DELETE /api/storage/files/{id}", d.DeleteStorageFile)

	// File sharing
	protected.HandleFunc("GET /api/shares", d.ListShares)
	protected.HandleFunc("POST /api/shares", d.CreateShare)
	protected.HandleFunc("GET /api/shares/{id}", d.GetShare)
	protected.HandleFunc("PATCH /api/shares/{id}", d.UpdateShare)
	protected.HandleFunc("DELETE /api/shares/{id}", d.DeleteShare)
	protected.HandleFunc("GET /api/shares/{id}/stats", d.GetShareStats)
	protected.HandleFunc("GET /api/shares/{id}/permissions", d.ListSharePermissions)
	protected.HandleFunc("POST /api/shares/{id}/permissions", d.AddSharePermission)
	protected.HandleFunc("PATCH /api/shares/{id}/permissions/{pid}", d.UpdateSharePermission)
	protected.HandleFunc("DELETE /api/shares/{id}/permissions/{pid}", d.DeleteSharePermission)
	protected.HandleFunc("GET /api/shares/{id}/files", d.ListShareFiles)
	protected.HandleFunc("POST /api/shares/{id}/files/sync", d.SyncShareFiles)
	protected.HandleFunc("POST /api/shares/{id}/transfer/request", d.TransferRequest)
	protected.HandleFunc("POST /api/shares/{id}/upload/request", d.UploadRequest)
	protected.HandleFunc("DELETE /api/shares/{id}/files", d.DeleteShareFile)

	// Swarm P2P sharing
	protected.HandleFunc("POST /api/shares/{id}/swarm/seed", d.SwarmAddSeed)
	protected.HandleFunc("DELETE /api/shares/{id}/swarm/seed/{fileId}", d.SwarmRemoveSeed)
	protected.HandleFunc("GET /api/shares/{id}/swarm/sources/{fileId}", d.SwarmSources)
	protected.HandleFunc("GET /api/shares/{id}/swarm/counts", d.SwarmCounts)
	protected.HandleFunc("POST /api/shares/{id}/swarm/request", d.SwarmRequest)

	// Game servers
	protected.HandleFunc("GET /api/gameservers/presets", d.GetGameServerPresets)
	protected.HandleFunc("GET /api/gameservers", d.GetGameServers)
	protected.HandleFunc("POST /api/gameservers", d.CreateGameServer)
	protected.HandleFunc("DELETE /api/gameservers/{id}", d.DeleteGameServer)
	protected.HandleFunc("POST /api/gameservers/{id}/start", d.StartGameServer)
	protected.HandleFunc("POST /api/gameservers/{id}/stop", d.StopGameServer)
	protected.HandleFunc("POST /api/gameservers/{id}/restart", d.RestartGameServer)
	protected.HandleFunc("GET /api/gameservers/{id}/stats", d.GameServerStats)
	protected.HandleFunc("GET /api/gameservers/{id}/files", d.GameServerFiles)
	protected.HandleFunc("GET /api/gameservers/{id}/files/content", d.GameServerFileContent)
	protected.HandleFunc("PUT /api/gameservers/{id}/files/content", d.GameServerFileWrite)
	protected.HandleFunc("POST /api/gameservers/{id}/files/upload", d.GameServerFileUpload)
	protected.HandleFunc("DELETE /api/gameservers/{id}/files", d.GameServerFileDelete)
	protected.HandleFunc("POST /api/gameservers/{id}/files/mkdir", d.GameServerMkdir)
	protected.HandleFunc("GET /api/gameservers/{id}/files/download", d.GameServerFileDownload)
	protected.HandleFunc("GET /api/gameservers/{id}/files/recursive", d.GameServerListRecursive)
	protected.HandleFunc("POST /api/gameservers/{id}/join", d.JoinGameServer)
	protected.HandleFunc("POST /api/gameservers/{id}/leave", d.LeaveGameServer)
	protected.HandleFunc("GET /api/gameservers/{id}/members", d.GetGameServerMembers)
	protected.HandleFunc("PUT /api/gameservers/{id}/access", d.SetGameServerAccess)
	protected.HandleFunc("POST /api/gameservers/{id}/rcon", d.RCONCommand)
	protected.HandleFunc("GET /api/gameservers/docker-status", d.DockerStatus)
	protected.HandleFunc("POST /api/gameservers/install-docker", d.InstallDocker)

	// Whiteboard
	protected.HandleFunc("GET /api/whiteboards", d.ListWhiteboards)
	protected.HandleFunc("POST /api/whiteboards", d.CreateWhiteboard)
	protected.HandleFunc("DELETE /api/whiteboards/{id}", d.DeleteWhiteboard)
	protected.HandleFunc("GET /api/whiteboards/{id}/strokes", d.GetWhiteboardStrokes)
	protected.HandleFunc("POST /api/whiteboards/{id}/strokes", d.AddWhiteboardStroke)
	protected.HandleFunc("POST /api/whiteboards/{id}/undo", d.UndoWhiteboardStroke)
	protected.HandleFunc("POST /api/whiteboards/{id}/clear", d.ClearWhiteboard)

	// Kanban board
	protected.HandleFunc("GET /api/kanban", d.ListKanbanBoards)
	protected.HandleFunc("POST /api/kanban", d.CreateKanbanBoard)
	protected.HandleFunc("GET /api/kanban/{id}", d.GetKanbanBoard)
	protected.HandleFunc("DELETE /api/kanban/{id}", d.DeleteKanbanBoard)
	protected.HandleFunc("POST /api/kanban/{id}/columns", d.CreateKanbanColumn)
	protected.HandleFunc("PATCH /api/kanban/{id}/columns/{colId}", d.UpdateKanbanColumn)
	protected.HandleFunc("POST /api/kanban/{id}/columns/reorder", d.ReorderKanbanColumns)
	protected.HandleFunc("DELETE /api/kanban/{id}/columns/{colId}", d.DeleteKanbanColumn)
	protected.HandleFunc("POST /api/kanban/{id}/cards", d.CreateKanbanCard)
	protected.HandleFunc("PATCH /api/kanban/cards/{cardId}", d.UpdateKanbanCard)
	protected.HandleFunc("POST /api/kanban/cards/{cardId}/move", d.MoveKanbanCard)
	protected.HandleFunc("DELETE /api/kanban/cards/{cardId}", d.DeleteKanbanCard)

	// Calendar events
	protected.HandleFunc("GET /api/events", d.ListEvents)
	protected.HandleFunc("POST /api/events", d.CreateEvent)
	protected.HandleFunc("GET /api/events/{id}", d.GetEvent)
	protected.HandleFunc("PATCH /api/events/{id}", d.UpdateEvent)
	protected.HandleFunc("DELETE /api/events/{id}", d.DeleteEvent)
	protected.HandleFunc("POST /api/events/{id}/remind", d.SetEventReminder)
	protected.HandleFunc("DELETE /api/events/{id}/remind", d.RemoveEventReminder)

	// Device management & bans
	protected.HandleFunc("GET /api/devices", d.ListDevices)
	protected.HandleFunc("GET /api/devices/{userId}", d.ListUserDevices)
	protected.HandleFunc("GET /api/bans/devices", d.ListDeviceBans)
	protected.HandleFunc("POST /api/bans/devices", d.CreateDeviceBan)
	protected.HandleFunc("DELETE /api/bans/devices/{id}", d.DeleteDeviceBan)

	// Invite chain
	protected.HandleFunc("GET /api/invite-chain", d.GetInviteChainTree)
	protected.HandleFunc("GET /api/invite-chain/{userId}", d.GetInviteChainUser)

	// Quarantine
	protected.HandleFunc("GET /api/quarantine", d.ListQuarantine)
	protected.HandleFunc("POST /api/quarantine/{userId}/approve", d.ApproveQuarantine)
	protected.HandleFunc("DELETE /api/quarantine/{userId}", d.RemoveQuarantine)

	// Backup/Restore (owner only)
	protected.HandleFunc("GET /api/admin/backup", d.BackupDatabase)
	protected.HandleFunc("POST /api/admin/restore", d.RestoreDatabase)
	protected.HandleFunc("GET /api/admin/backup/info", d.BackupInfo)

	// Approvals
	protected.HandleFunc("GET /api/approvals", d.ListPendingApprovals)
	protected.HandleFunc("POST /api/approvals/{userId}/approve", d.ApproveUser)
	protected.HandleFunc("POST /api/approvals/{userId}/reject", d.RejectUser)

	// LAN Party (only if WG manager is enabled)
	if d.WG != nil {
		protected.HandleFunc("GET /api/lan", d.GetLANParties)
		protected.HandleFunc("POST /api/lan", d.CreateLANParty)
		protected.HandleFunc("DELETE /api/lan/{id}", d.DeleteLANParty)
		protected.HandleFunc("POST /api/lan/{id}/join", d.JoinLANParty)
		protected.HandleFunc("DELETE /api/lan/{id}/leave", d.LeaveLANParty)

		// VPN Tunnels
		protected.HandleFunc("GET /api/tunnels", d.GetTunnels)
		protected.HandleFunc("POST /api/tunnels", d.CreateTunnel)
		protected.HandleFunc("POST /api/tunnels/{id}/accept", d.AcceptTunnel)
		protected.HandleFunc("POST /api/tunnels/{id}/close", d.CloseTunnel)
	}

	mux.Handle("/api/", authMW(protected))

	// Root — JSON info (no SPA)
	mux.HandleFunc("GET /{$}", d.ServerInfo)

	var handler http.Handler = mux
	handler = middleware.Logging(handler)
	handler = middleware.CORS(handler)

	return handler
}

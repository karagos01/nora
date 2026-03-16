package api

import (
	"encoding/json"
	"time"
)

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	PublicKey   string    `json:"public_key"`
	AvatarURL   string    `json:"avatar_url"`
	IsOwner     bool      `json:"is_owner"`
	Status      string    `json:"status,omitempty"`
	StatusText  string    `json:"status_text,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Channel struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Topic           string    `json:"topic"`
	Type            string    `json:"type"`
	Position        int       `json:"position"`
	CategoryID      *string   `json:"category_id"`
	ParentID        *string   `json:"parent_id,omitempty"`
	SlowModeSeconds int       `json:"slow_mode_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

type ChannelCategory struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Color    string            `json:"color"`
	Position int               `json:"position"`
	ParentID *string           `json:"parent_id,omitempty"`
	Children []ChannelCategory `json:"children,omitempty"`
}

type Message struct {
	ID          string       `json:"id"`
	ChannelID   string       `json:"channel_id"`
	UserID      string       `json:"user_id"`
	Content     string       `json:"content"`
	ReplyToID   *string      `json:"reply_to_id,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   *time.Time   `json:"updated_at,omitempty"`
	Author      *User        `json:"author,omitempty"`
	ReplyTo     *Message     `json:"reply_to,omitempty"`
	IsPinned    bool         `json:"is_pinned"`
	IsHidden    bool         `json:"is_hidden"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`
	Poll        *Poll        `json:"poll,omitempty"`
	LinkPreview *LinkPreview `json:"link_preview,omitempty"`
	ReplyCount  int          `json:"reply_count,omitempty"`
}

type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
}

type Poll struct {
	ID        string       `json:"id"`
	MessageID string       `json:"message_id"`
	Question  string       `json:"question"`
	PollType  string       `json:"poll_type"`
	Options   []PollOption `json:"options"`
	ExpiresAt *time.Time   `json:"expires_at,omitempty"`
}

type PollOption struct {
	ID       string   `json:"id"`
	PollID   string   `json:"poll_id"`
	Label    string   `json:"label"`
	Position int      `json:"position"`
	Count    int      `json:"count"`
	UserIDs  []string `json:"user_ids,omitempty"`
}

type Reaction struct {
	MessageID string   `json:"message_id"`
	Emoji     string   `json:"emoji"`
	Count     int      `json:"count"`
	UserIDs   []string `json:"user_ids"`
}

type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

type DMConversation struct {
	ID           string          `json:"id"`
	Participants []DMParticipant `json:"participants,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	PendingCount int             `json:"pending_count"`
}

type DMParticipant struct {
	ConversationID string `json:"conversation_id"`
	UserID         string `json:"user_id"`
	PublicKey      string `json:"public_key"`
}

type DMPendingMessage struct {
	ID               string    `json:"id"`
	ConversationID   string    `json:"conversation_id"`
	SenderID         string    `json:"sender_id"`
	EncryptedContent string    `json:"encrypted_content"`
	ReplyToID        string    `json:"reply_to_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Author           *User     `json:"author,omitempty"`
	DecryptedContent string    `json:"-"` // locally decrypted, not from JSON
}

type FriendRequest struct {
	ID         string `json:"id"`
	FromUserID string `json:"from_user_id"`
	ToUserID   string `json:"to_user_id"`
	FromUser   *User  `json:"from_user,omitempty"`
	ToUser     *User  `json:"to_user,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type FriendRequestList struct {
	Incoming []FriendRequest `json:"incoming"`
	Sent     []FriendRequest `json:"sent"`
}

type ServerInfo struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	IconURL             string `json:"icon_url,omitempty"`
	StunURL             string `json:"stun_url,omitempty"`
	UserCount           int    `json:"user_count"`
	Version             string `json:"version,omitempty"`
	GameServersEnabled  bool   `json:"game_servers_enabled"`
	SwarmSharingEnabled bool   `json:"swarm_sharing_enabled"`
	AutoModEnabled      bool   `json:"automod_enabled"`
	RegistrationMode    string `json:"registration_mode,omitempty"`
}

// Swarm P2P sharing
type SwarmSeeder struct {
	UserID string `json:"user_id"`
	Online bool   `json:"online"`
}

type SwarmSourcesResponse struct {
	Seeders []SwarmSeeder `json:"seeders"`
	Total   int           `json:"total"`
	Online  int           `json:"online"`
}

type SwarmRequestResponse struct {
	TransferID  string        `json:"transfer_id"`
	Sources     []SwarmSeeder `json:"sources"`
	PieceSize   int           `json:"piece_size"`
	TotalPieces int           `json:"total_pieces"`
	FileSize    int64         `json:"file_size"`
	FileName    string        `json:"file_name"`
}

type ScheduledMessage struct {
	ID          string    `json:"id"`
	ChannelID   string    `json:"channel_id"`
	UserID      string    `json:"user_id"`
	Content     string    `json:"content"`
	ReplyToID   *string   `json:"reply_to_id,omitempty"`
	ScheduledAt time.Time `json:"scheduled_at"`
	CreatedAt   time.Time `json:"created_at"`
	ChannelName string    `json:"channel_name,omitempty"`
}

type ChallengeResponse struct {
	Nonce string `json:"nonce"`
}

type VerifyResponse struct {
	AccessToken     string `json:"access_token"`
	RefreshToken    string `json:"refresh_token"`
	User            User   `json:"user"`
	PendingApproval bool   `json:"pending_approval,omitempty"`
}

type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Permissions int64     `json:"permissions"`
	Position    int       `json:"position"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
}

type Invite struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	Link            string     `json:"link"`
	CreatorID       string     `json:"creator_id"`
	CreatorUsername  string     `json:"creator_username"`
	MaxUses         int        `json:"max_uses"`
	Uses            int        `json:"uses"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type Ban struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Reason      string     `json:"reason"`
	BannedBy    string     `json:"banned_by"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	InvitedBy   string     `json:"invited_by,omitempty"`
	DeviceCount int        `json:"device_count"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Timeout struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Reason    string    `json:"reason"`
	IssuedBy  string    `json:"issued_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Permission bitmask constants
const (
	PermSendMessages  int64 = 1
	PermRead          int64 = 2
	PermManageMessages int64 = 4
	PermManageChannels int64 = 8
	PermManageRoles   int64 = 16
	PermManageInvites int64 = 32
	PermKick          int64 = 64
	PermBan           int64 = 128
	PermUpload        int64 = 256
	PermAdmin         int64 = 512
	PermManageEmojis    int64 = 1024
	PermApproveMembers int64 = 4096
)

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatorID string    `json:"creator_id"`
	CreatedAt time.Time `json:"created_at"`
	Members   []GroupMember `json:"members,omitempty"`
}

type GroupMember struct {
	GroupID   string    `json:"group_id"`
	UserID    string    `json:"user_id"`
	PublicKey string    `json:"public_key"`
	Username  string    `json:"username,omitempty"`
	JoinedAt  time.Time `json:"joined_at"`
}

type GroupInvite struct {
	ID        string     `json:"id"`
	GroupID   string     `json:"group_id"`
	Code      string     `json:"code"`
	CreatorID string     `json:"creator_id"`
	MaxUses   int        `json:"max_uses"`
	Uses      int        `json:"uses"`
	Link      string     `json:"link"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type GroupMessage struct {
	ID               string       `json:"id"`
	GroupID          string       `json:"group_id"`
	SenderID         string       `json:"sender_id"`
	EncryptedContent string       `json:"encrypted_content"`
	CreatedAt        time.Time    `json:"created_at"`
	Author           *User        `json:"author,omitempty"`
	Attachments      []Attachment `json:"attachments,omitempty"`
	DecryptedContent string       `json:"-"` // locally decrypted
}

type CustomEmoji struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	UploaderID string    `json:"uploader_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type LANParty struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatorID string    `json:"creator_id"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type LANPartyMember struct {
	ID         string    `json:"id"`
	PartyID    string    `json:"party_id"`
	UserID     string    `json:"user_id"`
	PublicKey  string    `json:"public_key"`
	AssignedIP string    `json:"assigned_ip"`
	JoinedAt   time.Time `json:"joined_at"`
	Username   string    `json:"username,omitempty"`
}

type LANPartiesResponse struct {
	Parties []LANParty                `json:"parties"`
	Members map[string][]LANPartyMember `json:"members"`
}

type JoinLANResponse struct {
	Member   LANPartyMember `json:"member"`
	WGConfig *WGConfig      `json:"wg_config,omitempty"`
}

type WGConfig struct {
	ServerPublicKey string `json:"server_public_key"`
	ServerEndpoint  string `json:"server_endpoint"`
	AssignedIP      string `json:"assigned_ip"`
	AllowedIPs      string `json:"allowed_ips"`
}

// File sharing

type SharedDirectory struct {
	ID             string     `json:"id"`
	OwnerID        string     `json:"owner_id"`
	PathHash       string     `json:"path_hash"`
	DisplayName    string     `json:"display_name"`
	IsActive       bool       `json:"is_active"`
	MaxFileSizeMB  int        `json:"max_file_size_mb"`
	StorageQuotaMB int        `json:"storage_quota_mb"`
	MaxFilesCount  int        `json:"max_files_count"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	OwnerName      string     `json:"owner_name,omitempty"`
	CanWrite       bool       `json:"can_write,omitempty"`
	CanDelete      bool       `json:"can_delete,omitempty"`
}

type SharePermission struct {
	ID          string    `json:"id"`
	DirectoryID string    `json:"directory_id"`
	GranteeID   *string   `json:"grantee_id"`
	CanRead     bool      `json:"can_read"`
	CanWrite    bool      `json:"can_write"`
	CanDelete   bool      `json:"can_delete"`
	IsBlocked   bool      `json:"is_blocked"`
	GrantedAt   time.Time `json:"granted_at"`
	GranteeName string    `json:"grantee_name,omitempty"`
}

type SharedFileEntry struct {
	ID           string     `json:"id"`
	DirectoryID  string     `json:"directory_id"`
	RelativePath string     `json:"relative_path"`
	FileName     string     `json:"file_name"`
	FileSize     int64      `json:"file_size"`
	IsDir        bool       `json:"is_dir"`
	FileHash     string     `json:"file_hash,omitempty"`
	ModifiedAt   *time.Time `json:"modified_at,omitempty"`
	CachedAt     time.Time  `json:"cached_at"`
}

type ShareListResponse struct {
	Own        []SharedDirectory `json:"own"`
	Accessible []SharedDirectory `json:"accessible"`
}

type ShareDetailResponse struct {
	Directory   SharedDirectory   `json:"directory"`
	Permissions []SharePermission `json:"permissions"`
}

// Gallery — aggregated attachments from messages
type GalleryItem struct {
	ID          string    `json:"id"`
	MessageID   string    `json:"message_id"`
	Filename    string    `json:"filename"`
	MimeType    string    `json:"mime_type"`
	Size        int64     `json:"size"`
	URL         string    `json:"url"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	CreatedAt   time.Time `json:"created_at"`
}

// File storage — folders and files on the server
type StorageFolder struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  *string   `json:"parent_id"`
	CreatorID string    `json:"creator_id"`
	CreatedAt time.Time `json:"created_at"`
}

type StorageFile struct {
	ID         string    `json:"id"`
	FolderID   *string   `json:"folder_id"`
	Name       string    `json:"name"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	UploaderID string    `json:"uploader_id"`
	Username   string    `json:"username,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ServerStorageInfo struct {
	DBBytes             int64 `json:"db_bytes"`
	UploadsBytes        int64 `json:"uploads_bytes"`
	AttachmentsBytes    int64 `json:"attachments_bytes"`
	EmojisBytes         int64 `json:"emojis_bytes"`
	IconBytes           int64 `json:"icon_bytes"`
	AvatarsBytes        int64 `json:"avatars_bytes"`
	TotalFiles          int   `json:"total_files"`
	MaxMB               int   `json:"max_mb"`
	ChannelHistoryLimit int   `json:"channel_history_limit"`
}

type WSEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Whiteboard

type Whiteboard struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ChannelID *string   `json:"channel_id,omitempty"`
	CreatorID string    `json:"creator_id"`
	CreatedAt time.Time `json:"created_at"`
}

type WhiteboardStroke struct {
	ID           string `json:"id"`
	WhiteboardID string `json:"whiteboard_id"`
	UserID       string `json:"user_id"`
	PathData     string `json:"path_data"`
	Color        string `json:"color"`
	Width        int    `json:"width"`
	Tool         string `json:"tool"`
	Username     string `json:"username,omitempty"`
}

// Game servers

type GameServerInstance struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	ContainerID string    `json:"container_id,omitempty"`
	CreatorID   string    `json:"creator_id"`
	AccessMode  string    `json:"access_mode"`
	CreatedAt   time.Time `json:"created_at"`
	ErrorMsg    string    `json:"error_msg,omitempty"`
}

type GameServerMember struct {
	GameServerID string    `json:"game_server_id"`
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	JoinedAt     time.Time `json:"joined_at"`
}

type GameServerPreset struct {
	Name string `json:"name"`
}

// VPN Tunnel — personal WireGuard tunnel
type Tunnel struct {
	ID              string    `json:"id"`
	CreatorID       string    `json:"creator_id"`
	TargetID        string    `json:"target_id"`
	Status          string    `json:"status"`
	CreatorWGPubKey string    `json:"creator_wg_pubkey,omitempty"`
	TargetWGPubKey  string    `json:"target_wg_pubkey,omitempty"`
	CreatorIP       string    `json:"creator_ip,omitempty"`
	TargetIP        string    `json:"target_ip,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	CreatorName     string    `json:"creator_name,omitempty"`
	TargetName      string    `json:"target_name,omitempty"`
}

type GameServerStats struct {
	CPUPercent string `json:"cpu_percent"`
	MemUsage   string `json:"mem_usage"`
	MemLimit   string `json:"mem_limit"`
	NetIO      string `json:"net_io"`
	Uptime     string `json:"uptime"`
}

type GameServerFileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type GameServerRecursiveEntry struct {
	RelPath string    `json:"rel_path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// Kanban board

type KanbanBoard struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	CreatorID   string         `json:"creator_id"`
	Columns     []KanbanColumn `json:"columns,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type KanbanColumn struct {
	ID        string       `json:"id"`
	BoardID   string       `json:"board_id"`
	Title     string       `json:"title"`
	Position  int          `json:"position"`
	Color     string       `json:"color"`
	Cards     []KanbanCard `json:"cards,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

type KanbanCard struct {
	ID           string     `json:"id"`
	ColumnID     string     `json:"column_id"`
	BoardID      string     `json:"board_id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Position     int        `json:"position"`
	AssignedTo   *string    `json:"assigned_to,omitempty"`
	AssignedUser *User      `json:"assigned_user,omitempty"`
	CreatedBy    string     `json:"created_by"`
	Author       *User      `json:"author,omitempty"`
	Color        string     `json:"color"`
	DueDate      *time.Time `json:"due_date,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

// Ban system — device bans, invite chain, quarantine, approvals

type DeviceBan struct {
	ID              string     `json:"id"`
	DeviceID        string     `json:"device_id"`
	HardwareHash    string     `json:"hardware_hash"`
	RelatedUserID   string     `json:"related_user_id"`
	RelatedUsername string     `json:"related_username,omitempty"`
	BannedBy        string     `json:"banned_by"`
	Reason          string     `json:"reason"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type UserDevice struct {
	UserID       string    `json:"user_id"`
	DeviceID     string    `json:"device_id"`
	HardwareHash string    `json:"hardware_hash"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Username     string    `json:"username,omitempty"`
}

type InviteChainNode struct {
	UserID   string            `json:"user_id"`
	Username string            `json:"username"`
	IsBanned bool              `json:"is_banned"`
	JoinedAt time.Time         `json:"joined_at"`
	Children []InviteChainNode `json:"children,omitempty"`
}

type QuarantineEntry struct {
	UserID     string     `json:"user_id"`
	Username   string     `json:"username,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	ApprovedBy *string    `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}

type PendingApproval struct {
	UserID            string    `json:"user_id"`
	RequestedUsername string    `json:"requested_username"`
	InvitedByID       string    `json:"invited_by_id"`
	InviterUsername    string    `json:"inviter_username,omitempty"`
	RequestedAt       time.Time `json:"requested_at"`
	Status            string    `json:"status"`
}

// Channel permission overrides
type ChannelPermOverride struct {
	ChannelID  string `json:"channel_id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Allow      int64  `json:"allow"`
	Deny       int64  `json:"deny"`
}

// Message edit history

type MessageEdit struct {
	ID         int       `json:"id"`
	MessageID  string    `json:"message_id"`
	OldContent string    `json:"old_content"`
	EditedAt   time.Time `json:"edited_at"`
	EditedBy   string    `json:"edited_by"`
}

// Calendar events

type Event struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	CreatorID      string     `json:"creator_id"`
	Creator        *User      `json:"creator,omitempty"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
	Location       string     `json:"location"`
	Color          string     `json:"color"`
	AllDay         bool       `json:"all_day"`
	RecurrenceRule string     `json:"recurrence_rule"`
	CreatedAt      time.Time  `json:"created_at"`
}

type EventReminder struct {
	ID       string    `json:"id"`
	EventID  string    `json:"event_id"`
	UserID   string    `json:"user_id"`
	RemindAt time.Time `json:"remind_at"`
	Reminded bool      `json:"reminded"`
}

type BackupInfo struct {
	DatabaseSize int64 `json:"database_size"`
	Users        int   `json:"users"`
	Messages     int   `json:"messages"`
	Channels     int   `json:"channels"`
}

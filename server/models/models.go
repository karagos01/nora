package models

import "time"

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	PublicKey   string    `json:"public_key"`
	AvatarURL   string    `json:"avatar_url"`
	IsOwner     bool      `json:"is_owner"`
	Status      string    `json:"status,omitempty"`
	StatusText  string    `json:"status_text,omitempty"`
	LastIP      string    `json:"-"`
	InvitedBy   string    `json:"invited_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type AuthChallenge struct {
	PublicKey    string    `json:"public_key"`
	Nonce        string    `json:"nonce"`
	ExpiresAt    time.Time `json:"expires_at"`
	Username     string    `json:"username,omitempty"`
	InvitedBy    string    `json:"invited_by,omitempty"`
	DeviceID     string    `json:"device_id,omitempty"`
	HardwareHash string    `json:"hardware_hash,omitempty"`
}

type ChannelCategory struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Color     string            `json:"color"`
	Position  int               `json:"position"`
	ParentID  *string           `json:"parent_id,omitempty"`
	Children  []ChannelCategory `json:"children,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
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

type Message struct {
	ID               string       `json:"id"`
	ChannelID        string       `json:"channel_id"`
	UserID           string       `json:"user_id"`
	Content          string       `json:"content"`
	ReplyToID        *string      `json:"reply_to_id,omitempty"`
	IdempotencyKey   string       `json:"-"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        *time.Time   `json:"updated_at,omitempty"`
	Author           *User        `json:"author,omitempty"`
	ReplyTo          *Message     `json:"reply_to,omitempty"`
	IsPinned         bool         `json:"is_pinned"`
	PinnedBy         *string      `json:"pinned_by,omitempty"`
	IsHidden         bool         `json:"is_hidden"`
	HiddenBy         *string      `json:"hidden_by,omitempty"`
	HiddenByPosition int          `json:"hidden_by_position"`
	ChannelName      string       `json:"channel_name,omitempty"`
	Attachments      []Attachment `json:"attachments,omitempty"`
	Reactions        []Reaction   `json:"reactions,omitempty"`
	Poll             *Poll        `json:"poll,omitempty"`
	LinkPreview      *LinkPreview `json:"link_preview,omitempty"`
	ReplyCount       int          `json:"reply_count,omitempty"`
}

type LinkPreview struct {
	ID          string `json:"id"`
	MessageID   string `json:"message_id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
}

type Attachment struct {
	ID          string `json:"id"`
	MessageID   string `json:"message_id"`
	Filepath    string `json:"-"`
	Filename    string `json:"filename"`
	MimeType    string `json:"mime_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
	ContentHash string `json:"-"`
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
	CreatorID       string     `json:"creator_id"`
	CreatorUsername  string     `json:"creator_username,omitempty"`
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
	IP          string     `json:"ip,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	InvitedBy   string     `json:"invited_by,omitempty"`
	DeviceCount int        `json:"device_count"`
	CreatedAt   time.Time  `json:"created_at"`
}

type IPBan struct {
	IP             string    `json:"ip"`
	Reason         string    `json:"reason"`
	BannedBy       string    `json:"banned_by"`
	RelatedUserID  string    `json:"related_user_id"`
	RelatedUsername string    `json:"related_username,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Timeout struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Reason    string    `json:"reason"`
	IssuedBy  string    `json:"issued_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type RefreshToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type DMConversation struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
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
}

type FriendRequest struct {
	ID         string    `json:"id"`
	FromUserID string    `json:"from_user_id"`
	ToUserID   string    `json:"to_user_id"`
	CreatedAt  time.Time `json:"created_at"`
	FromUser   *User     `json:"from_user,omitempty"`
	ToUser     *User     `json:"to_user,omitempty"`
}

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatorID string    `json:"creator_id"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupMember struct {
	GroupID   string    `json:"group_id"`
	UserID    string    `json:"user_id"`
	PublicKey string    `json:"public_key"`
	JoinedAt  time.Time `json:"joined_at"`
}

type GroupInvite struct {
	ID        string     `json:"id"`
	GroupID   string     `json:"group_id"`
	Code      string     `json:"code"`
	CreatorID string     `json:"creator_id"`
	MaxUses   int        `json:"max_uses"`
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Message edit history
type MessageEdit struct {
	ID         int       `json:"id"`
	MessageID  string    `json:"message_id"`
	OldContent string    `json:"old_content"`
	EditedAt   time.Time `json:"edited_at"`
	EditedBy   string    `json:"edited_by"`
}

// Permission bitmasks
const (
	PermSendMessages   int64 = 1
	PermRead           int64 = 2
	PermManageMessages int64 = 4
	PermManageChannels int64 = 8
	PermManageRoles    int64 = 16
	PermManageInvites  int64 = 32
	PermKick           int64 = 64
	PermBan            int64 = 128
	PermUpload         int64 = 256
	PermAdmin          int64 = 512
	PermManageEmojis   int64 = 1024
	PermViewActivity   int64 = 2048
	PermApproveMembers int64 = 4096
)

type AuditLogEntry struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
	Username   string    `json:"username,omitempty"`
	AvatarURL  string    `json:"avatar_url,omitempty"`
}

type Reaction struct {
	MessageID string   `json:"message_id"`
	Emoji     string   `json:"emoji"`
	Count     int      `json:"count"`
	UserIDs   []string `json:"user_ids"`
}

type Poll struct {
	ID        string       `json:"id"`
	MessageID string       `json:"message_id"`
	Question  string       `json:"question"`
	PollType  string       `json:"poll_type"`
	Options   []PollOption `json:"options"`
	CreatedAt time.Time    `json:"created_at"`
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

type Emoji struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Filepath   string    `json:"-"`
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

// Webhook for bot/integration API
type Webhook struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Name      string    `json:"name"`
	Token     string    `json:"token,omitempty"`
	AvatarURL string    `json:"avatar_url"`
	CreatorID string    `json:"creator_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Gallery — attachment with message context
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

// File storage — folders and files
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
	Filepath   string    `json:"-"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	UploaderID string    `json:"uploader_id"`
	Username   string    `json:"username,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
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

// Game Server Manager

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

// VPN Tunnel — personal WireGuard tunnel between two users
type Tunnel struct {
	ID              string    `json:"id"`
	CreatorID       string    `json:"creator_id"`
	TargetID        string    `json:"target_id"`
	Status          string    `json:"status"` // pending, active, closed
	CreatorWGPubKey string    `json:"creator_wg_pubkey,omitempty"`
	TargetWGPubKey  string    `json:"target_wg_pubkey,omitempty"`
	CreatorIP       string    `json:"creator_ip,omitempty"`
	TargetIP        string    `json:"target_ip,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	CreatorName     string    `json:"creator_name,omitempty"`
	TargetName      string    `json:"target_name,omitempty"`
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
	ID           string    `json:"id"`
	WhiteboardID string    `json:"whiteboard_id"`
	UserID       string    `json:"user_id"`
	PathData     string    `json:"path_data"`
	Color        string    `json:"color"`
	Width        int       `json:"width"`
	Tool         string    `json:"tool"`
	CreatedAt    time.Time `json:"created_at"`
	Username     string    `json:"username,omitempty"`
}

// Shared directories
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

// Ban system — device bans, invite chain, quarantine, approvals

type DeviceBan struct {
	ID              string     `json:"id"`
	DeviceID        string     `json:"device_id"`
	HardwareHash    string     `json:"hardware_hash"`
	RelatedUserID   string     `json:"related_user_id"`
	BannedBy        string     `json:"banned_by"`
	Reason          string     `json:"reason"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	RelatedUsername string     `json:"related_username,omitempty"`
}

type UserDevice struct {
	UserID       string    `json:"user_id"`
	DeviceID     string    `json:"device_id"`
	HardwareHash string    `json:"hardware_hash"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Username     string    `json:"username,omitempty"`
}

type InviteChainEntry struct {
	UserID          string    `json:"user_id"`
	InvitedByID     string    `json:"invited_by_id"`
	InviteCode      string    `json:"invite_code"`
	JoinedAt        time.Time `json:"joined_at"`
	Username        string    `json:"username"`
	InviterUsername  string    `json:"inviter_username"`
	InvitedCount    int       `json:"invited_count"`
	BannedCount     int       `json:"banned_count"`
	IsBanned        bool      `json:"is_banned"`
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
	StartedAt  time.Time  `json:"started_at"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	ApprovedBy *string    `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	Username   string     `json:"username,omitempty"`
}

type PendingApproval struct {
	UserID            string     `json:"user_id"`
	RequestedUsername string     `json:"requested_username"`
	InvitedByID       string     `json:"invited_by_id"`
	RequestedAt       time.Time  `json:"requested_at"`
	ReviewedBy        *string    `json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	Status            string     `json:"status"`
	InviterUsername    string     `json:"inviter_username,omitempty"`
}

// Channel permission overrides (per-channel allow/deny for roles or users)
type ChannelPermOverride struct {
	ChannelID  string `json:"channel_id"`
	TargetType string `json:"target_type"` // "role" or "user"
	TargetID   string `json:"target_id"`
	Allow      int64  `json:"allow"`
	Deny       int64  `json:"deny"`
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

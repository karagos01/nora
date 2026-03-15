package ws

import "encoding/json"

type EventType string

const (
	EventMessageNew     EventType = "message.new"
	EventMessageEdit    EventType = "message.edit"
	EventMessageDelete  EventType = "message.delete"
	EventTypingStart    EventType = "typing.start"
	EventPresenceUpdate EventType = "presence.update"
	EventChannelCreate  EventType = "channel.create"
	EventChannelUpdate  EventType = "channel.update"
	EventChannelDelete  EventType = "channel.delete"
	EventDMMessage      EventType = "dm.message"
	EventMemberJoin     EventType = "member.join"
	EventMemberLeave    EventType = "member.leave"
	EventFriendAdd      EventType = "friend.add"
	EventFriendRemove   EventType = "friend.remove"
	EventFileOffer      EventType = "file.offer"
	EventFileAccept     EventType = "file.accept"
	EventFileReject     EventType = "file.reject"
	EventFileChunk      EventType = "file.chunk"
	EventFileAck        EventType = "file.ack"
	EventFileComplete   EventType = "file.complete"
	EventFileCancel     EventType = "file.cancel"
	EventFileResume     EventType = "file.resume"
	EventFileIce        EventType = "file.ice"
	EventFileRequest    EventType = "file.request"
	EventServerUpdate          EventType = "server.update"
	EventFriendRequest         EventType = "friend.request"
	EventFriendRequestAccept   EventType = "friend.request.accept"
	EventFriendRequestDecline  EventType = "friend.request.decline"
	EventBlockAdd              EventType = "block.add"
	EventBlockRemove           EventType = "block.remove"
	EventGroupMessage          EventType = "group.message"
	EventGroupKey              EventType = "group.key"
	EventGroupMemberJoin       EventType = "group.member.join"
	EventGroupMemberLeave      EventType = "group.member.leave"
	EventGroupCreate           EventType = "group.create"
	EventGroupDelete           EventType = "group.delete"

	// Voice channel
	EventVoiceJoin   EventType = "voice.join"
	EventVoiceLeave  EventType = "voice.leave"
	EventVoiceState  EventType = "voice.state"
	EventVoiceMove   EventType = "voice.move"
	EventVoiceMute   EventType = "voice.mute"
	EventVoiceOffer  EventType = "voice.offer"
	EventVoiceAnswer EventType = "voice.answer"
	EventVoiceIce    EventType = "voice.ice"

	// Pin
	EventMessagePin   EventType = "message.pin"
	EventMessageUnpin EventType = "message.unpin"

	// Reactions
	EventReactionAdd    EventType = "reaction.add"
	EventReactionRemove EventType = "reaction.remove"

	// Message hiding & bulk delete
	EventMessageHide       EventType = "message.hide"
	EventMessagesHide      EventType = "messages.hide"
	EventMessagesBulkDelete EventType = "messages.bulk_delete"

	// Emoji
	EventEmojiCreate EventType = "emoji.create"
	EventEmojiDelete EventType = "emoji.delete"

	// Channel categories
	EventCategoryCreate EventType = "category.create"
	EventCategoryUpdate EventType = "category.update"
	EventCategoryDelete EventType = "category.delete"

	// Member update (avatar, display_name, etc.)
	EventMemberUpdate EventType = "member.update"

	// LAN Party
	EventLANCreate EventType = "lan.create"
	EventLANJoin   EventType = "lan.join"
	EventLANLeave  EventType = "lan.leave"
	EventLANDelete EventType = "lan.delete"

	// DM management
	EventDMDelete        EventType = "dm.delete"
	EventDMMessageDelete EventType = "dm.message.delete"
	EventDMMessageEdit   EventType = "dm.message.edit"

	// Screen sharing
	EventScreenShare EventType = "screen.share"
	EventScreenWatch EventType = "screen.watch"

	// Webhooks
	EventWebhookCreate EventType = "webhook.create"
	EventWebhookDelete EventType = "webhook.delete"

	// File storage
	EventStorageFileCreate   EventType = "storage.file.create"
	EventStorageFileDelete   EventType = "storage.file.delete"
	EventStorageFolderCreate EventType = "storage.folder.create"
	EventStorageFolderDelete EventType = "storage.folder.delete"

	// File sharing
	EventShareRegistered       EventType = "share.registered"
	EventShareUnregistered     EventType = "share.unregistered"
	EventShareUpdated          EventType = "share.updated"
	EventShareOwnerOnline      EventType = "share.owner_online"
	EventShareOwnerOffline     EventType = "share.owner_offline"
	EventShareFilesChanged     EventType = "share.files_changed"
	EventSharePermissionChanged EventType = "share.permission_changed"
	EventFileDeleted           EventType = "file.deleted"
	EventUploadRequest         EventType = "upload.request"
	EventTransferRequest       EventType = "transfer.request"
	EventTransferReady         EventType = "transfer.ready"
	EventTransferProgress      EventType = "transfer.progress"
	EventTransferComplete      EventType = "transfer.complete"
	EventTransferError         EventType = "transfer.error"

	// DM call
	EventCallRing    EventType = "call.ring"
	EventCallAccept  EventType = "call.accept"
	EventCallDecline EventType = "call.decline"
	EventCallHangup  EventType = "call.hangup"
	EventCallOffer   EventType = "call.offer"
	EventCallAnswer  EventType = "call.answer"
	EventCallIce     EventType = "call.ice"

	// Link previews
	EventMessageLinkPreview EventType = "message.linkpreview"

	// Polls
	EventPollVote EventType = "poll.vote"

	// Game servers
	EventGameServerCreate EventType = "gameserver.create"
	EventGameServerStart  EventType = "gameserver.start"
	EventGameServerStop   EventType = "gameserver.stop"
	EventGameServerDelete EventType = "gameserver.delete"
	EventGameServerStatus EventType = "gameserver.status"
	EventGameServerJoin   EventType = "gameserver.join"
	EventGameServerLeave  EventType = "gameserver.leave"

	// Whiteboard
	EventWhiteboardStroke EventType = "whiteboard.stroke"
	EventWhiteboardUndo   EventType = "whiteboard.undo"
	EventWhiteboardClear  EventType = "whiteboard.clear"

	// Swarm P2P
	EventSwarmPieceRequest  EventType = "swarm.piece_request"
	EventSwarmPieceOffer    EventType = "swarm.piece_offer"
	EventSwarmPieceAccept   EventType = "swarm.piece_accept"
	EventSwarmPieceIce      EventType = "swarm.piece_ice"
	EventSwarmPieceComplete EventType = "swarm.piece_complete"
	EventSwarmDownloadNotify EventType = "swarm.download_notify"
	EventSwarmSeedAdded      EventType = "swarm.seed_added"
	EventSwarmSeedRemoved    EventType = "swarm.seed_removed"

	// Lobby voice channels
	EventLobbyRename   EventType = "lobby.rename"
	EventLobbyPassword EventType = "lobby.password"
	EventVoiceError    EventType = "voice.error"

	// Ban system
	EventMemberTimeout    EventType = "member.timeout"
	EventBanCreated       EventType = "ban.created"
	EventBanExpired       EventType = "ban.expired"
	EventDeviceSuspicious EventType = "device.suspicious"
	EventQuarantineEnded  EventType = "quarantine.ended"
	EventApprovalPending  EventType = "approval.pending"
	EventApprovalResolved EventType = "approval.resolved"

	// Calendar events
	EventCalendarCreate   EventType = "calendar.event_create"
	EventCalendarUpdate   EventType = "calendar.event_update"
	EventCalendarDelete   EventType = "calendar.event_delete"
	EventCalendarReminder EventType = "calendar.reminder"

	// Channel typing
	EventChannelTyping EventType = "channel.typing"

	// Kanban board
	EventKanbanBoardCreate  EventType = "kanban.board_create"
	EventKanbanBoardDelete  EventType = "kanban.board_delete"
	EventKanbanColumnCreate EventType = "kanban.column_create"
	EventKanbanColumnUpdate EventType = "kanban.column_update"
	EventKanbanColumnDelete EventType = "kanban.column_delete"
	EventKanbanCardCreate   EventType = "kanban.card_create"
	EventKanbanCardUpdate   EventType = "kanban.card_update"
	EventKanbanCardMove     EventType = "kanban.card_move"
	EventKanbanCardDelete   EventType = "kanban.card_delete"

	// VPN Tunnels
	EventTunnelRequest EventType = "tunnel.request"
	EventTunnelAccept  EventType = "tunnel.accept"
	EventTunnelClose   EventType = "tunnel.close"
)

type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type IncomingEvent struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type TypingPayload struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
}

func NewEvent(typ EventType, payload any) ([]byte, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Event{Type: typ, Payload: p})
}

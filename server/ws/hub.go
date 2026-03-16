package ws

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// DynamicChannelInfo holds runtime state for lobby sub-channels (not persisted to DB)
type DynamicChannelInfo struct {
	ManagerID string // userID of the first user (manager)
	Password  string // optional password
	ParentID  string // lobby channel ID
	Name      string // sub-channel name
}

// LiveWhiteboard holds ephemeral whiteboard state for a voice channel (not persisted)
type LiveWhiteboard struct {
	StarterID string
	ChannelID string
	Strokes   []LiveWhiteboardStroke
}

// LiveWhiteboardStroke represents a single stroke in a live whiteboard
type LiveWhiteboardStroke struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	PathData string `json:"path_data"`
	Color    string `json:"color"`
	Width    int    `json:"width"`
	Tool     string `json:"tool"`
	Username string `json:"username"`
}

type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte

	// Voice state: channelID -> set of userIDs
	voiceState map[string]map[string]bool
	// Reverse lookup: userID -> channelID
	voiceUser map[string]string

	// Screen share state: userID -> channelID
	screenSharers map[string]string

	// Lobby voice channels: dynamic sub-channels
	dynamicChannels map[string]*DynamicChannelInfo // channelID -> info
	lobbyCounters   map[string]int                 // lobbyID -> next room number

	// Live whiteboards: channelID -> whiteboard (in-memory cache, persisted to DB)
	liveWhiteboards map[string]*LiveWhiteboard

	// Live whiteboard DB callbacks (set from main.go)
	liveWBCreateFn func(channelID, starterID string) error
	liveWBDeleteFn func(channelID string) error
	liveWBStrokeFn func(stroke LiveWhiteboardStroke, channelID string) error
	liveWBUndoFn   func(strokeID string) error
	liveWBClearFn  func(channelID string) error

	// Lobby callbacks (set from main.go)
	lobbyCreateFn func(lobbyID, name string) (channelID string, err error)
	lobbyDeleteFn func(channelID string)
	isLobbyFn     func(channelID string) bool
	lobbyRenameFn func(channelID, name string) error

	// User status callback — returns status and status_text from DB
	userStatusFn func(userID string) (status, statusText string)

	// LAN kick callback — called after 5min offline, removes user from all LAN parties
	lanKickCallback func(userID string)
	// Active kick timers (userID -> timer), protected by lanMu
	lanMu         sync.Mutex
	lanKickTimers map[string]*time.Timer

	// OnConnect callback — called after client connects (for IP refresh)
	onConnectFn func(userID string, r *http.Request)
}

func NewHub() *Hub {
	return &Hub{
		clients:         make(map[*Client]bool),
		register:        make(chan *Client),
		unregister:      make(chan *Client),
		broadcast:       make(chan []byte, 256),
		voiceState:      make(map[string]map[string]bool),
		voiceUser:       make(map[string]string),
		screenSharers:   make(map[string]string),
		dynamicChannels: make(map[string]*DynamicChannelInfo),
		lobbyCounters:   make(map[string]int),
		liveWhiteboards: make(map[string]*LiveWhiteboard),
		lanKickTimers:   make(map[string]*time.Timer),
	}
}

// SetUserStatusFn sets the callback for reading user status from DB
func (h *Hub) SetUserStatusFn(fn func(userID string) (string, string)) {
	h.userStatusFn = fn
}

// SetOnConnect sets the callback called after a client connects
func (h *Hub) SetOnConnect(fn func(userID string, r *http.Request)) {
	h.onConnectFn = fn
}

// GetUserStatuses returns the statuses map for online users
func (h *Hub) GetUserStatuses(onlineIDs []string) map[string]map[string]string {
	if h.userStatusFn == nil {
		return nil
	}
	statuses := make(map[string]map[string]string)
	for _, uid := range onlineIDs {
		status, statusText := h.userStatusFn(uid)
		if status != "" || statusText != "" {
			statuses[uid] = map[string]string{"status": status, "status_text": statusText}
		}
	}
	if len(statuses) == 0 {
		return nil
	}
	return statuses
}

// SetLANKickCallback sets the callback called after 5min offline to kick from LAN parties
func (h *Hub) SetLANKickCallback(cb func(userID string)) {
	h.lanKickCallback = cb
}

// OnlineUserIDs returns a list of unique user IDs currently connected via WS
func (h *Hub) OnlineUserIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	seen := make(map[string]bool)
	var ids []string
	for client := range h.clients {
		if !seen[client.UserID] {
			seen[client.UserID] = true
			ids = append(ids, client.UserID)
		}
	}
	return ids
}

// IsUserOnline checks whether the user has at least one WS connection (thread-safe)
func (h *Hub) IsUserOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isUserOnline(userID)
}

// isUserOnline checks whether the user has at least one WS connection (must be called with h.mu.RLock)
func (h *Hub) isUserOnline(userID string) bool {
	for client := range h.clients {
		if client.UserID == userID {
			return true
		}
	}
	return false
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			wasOnline := h.isUserOnline(client.UserID)
			h.clients[client] = true
			h.mu.Unlock()
			slog.Info("ws: client connected", "user_id", client.UserID, "total", len(h.clients))

			// Broadcast presence online if this is the user's first session
			if !wasOnline {
				h.cancelLANKickTimer(client.UserID)

				msg, _ := NewEvent(EventPresenceUpdate, map[string]any{
					"user_id": client.UserID,
					"status":  "online",
				})
				h.Broadcast(msg)
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			stillOnline := h.isUserOnline(client.UserID)

			// Voice cleanup: if the user has no other session, remove from voice channel
			var voiceLeftChannel string
			var voiceRemaining []string
			if !stillOnline {
				voiceLeftChannel = h.voiceUser[client.UserID]
				if voiceLeftChannel != "" {
					h.voiceLeaveUnsafe(client.UserID)
					voiceRemaining = h.voiceUsersUnsafe(voiceLeftChannel)
				}
			}
			h.mu.Unlock()
			slog.Info("ws: client disconnected", "user_id", client.UserID, "total", len(h.clients))

			// Broadcast voice state update if the user left a voice channel
			if voiceLeftChannel != "" {
				msg, _ := NewEvent(EventVoiceState, map[string]any{
					"channel_id": voiceLeftChannel,
					"users":      voiceRemaining,
					"left":       client.UserID,
				})
				h.Broadcast(msg)

				// Cleanup dynamic lobby sub-channel if empty
				h.checkDynamicCleanup(voiceLeftChannel)

				// Cleanup live whiteboard if voice channel is now empty
				h.checkLiveWBCleanup(voiceLeftChannel)
			}

			// Broadcast presence offline if this was the last session
			if !stillOnline {
				msg, _ := NewEvent(EventPresenceUpdate, map[string]any{
					"user_id": client.UserID,
					"status":  "offline",
				})
				h.Broadcast(msg)

				// Start 5min LAN kick timer
				if h.lanKickCallback != nil {
					userID := client.UserID
					h.StartLANKickTimer(userID)
				}
			}

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
					client.dropped = 0
				default:
					client.dropped++
					if client.dropped >= 10 {
						slog.Warn("ws: dropping slow client", "user_id", client.UserID, "dropped", client.dropped)
						go func(c *Client) {
							h.unregister <- c
						}(client)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(msg []byte) {
	h.broadcast <- msg
}

// BroadcastExcluding sends a message to all connected clients except the excluded user IDs.
func (h *Hub) BroadcastExcluding(msg []byte, excludedUserIDs map[string]bool) {
	if len(excludedUserIDs) == 0 {
		h.Broadcast(msg)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if excludedUserIDs[client.UserID] {
			continue
		}
		select {
		case client.send <- msg:
			client.dropped = 0
		default:
			client.dropped++
			if client.dropped >= 10 {
				slog.Warn("ws: dropping slow client", "user_id", client.UserID, "dropped", client.dropped)
				go func(c *Client) {
					h.unregister <- c
				}(client)
			}
		}
	}
}

// BroadcastToUser sends a message to a specific user (all their sessions)
func (h *Hub) BroadcastToUser(userID string, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client.UserID == userID {
			select {
			case client.send <- msg:
			default:
			}
		}
	}
}

// DisconnectUser disconnects all sessions of the given user
func (h *Hub) DisconnectUser(userID string) {
	h.mu.RLock()
	var targets []*Client
	for client := range h.clients {
		if client.UserID == userID {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		h.unregister <- c
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// VoiceJoin adds a user to a voice channel (auto-leave from previous), returns list of users in the channel
func (h *Hub) VoiceJoin(channelID, userID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Auto-leave from previous channel
	h.voiceLeaveUnsafe(userID)

	if h.voiceState[channelID] == nil {
		h.voiceState[channelID] = make(map[string]bool)
	}
	h.voiceState[channelID][userID] = true
	h.voiceUser[userID] = channelID

	return h.voiceUsersUnsafe(channelID)
}

// VoiceLeave removes a user from the voice channel, returns channelID and remaining users
func (h *Hub) VoiceLeave(userID string) (string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	channelID := h.voiceUser[userID]
	if channelID == "" {
		return "", nil
	}
	h.voiceLeaveUnsafe(userID)
	return channelID, h.voiceUsersUnsafe(channelID)
}

// VoiceUsers returns the list of users in a channel
func (h *Hub) VoiceUsers(channelID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.voiceUsersUnsafe(channelID)
}

// AllVoiceState returns the complete voice state for the REST endpoint
func (h *Hub) AllVoiceState() map[string][]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string][]string)
	for chID, users := range h.voiceState {
		if len(users) > 0 {
			var ids []string
			for uid := range users {
				ids = append(ids, uid)
			}
			result[chID] = ids
		}
	}
	return result
}

// VoiceChannelOf returns the channelID where the user is, or ""
func (h *Hub) VoiceChannelOf(userID string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.voiceUser[userID]
}

func (h *Hub) voiceLeaveUnsafe(userID string) {
	channelID := h.voiceUser[userID]
	if channelID == "" {
		return
	}
	delete(h.voiceUser, userID)
	delete(h.screenSharers, userID)
	if h.voiceState[channelID] != nil {
		delete(h.voiceState[channelID], userID)
		if len(h.voiceState[channelID]) == 0 {
			delete(h.voiceState, channelID)
		}
	}
}

// SetScreenShare sets the screen share state for a user.
func (h *Hub) SetScreenShare(userID, channelID string, sharing bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sharing {
		h.screenSharers[userID] = channelID
	} else {
		delete(h.screenSharers, userID)
	}
}

// ScreenSharers returns a map of userID -> channelID for all active screen sharers.
func (h *Hub) ScreenSharers() map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]string, len(h.screenSharers))
	for uid, chID := range h.screenSharers {
		result[uid] = chID
	}
	return result
}

func (h *Hub) voiceUsersUnsafe(channelID string) []string {
	users := h.voiceState[channelID]
	if len(users) == 0 {
		return nil
	}
	var ids []string
	for uid := range users {
		ids = append(ids, uid)
	}
	return ids
}

// RegisterSync adds a client synchronously and returns the list of online users (including the new one)
func (h *Hub) RegisterSync(client *Client) []string {
	h.mu.Lock()
	wasOnline := h.isUserOnline(client.UserID)
	h.clients[client] = true
	h.mu.Unlock()
	slog.Info("ws: client connected", "user_id", client.UserID, "total", len(h.clients))

	if !wasOnline {
		h.cancelLANKickTimer(client.UserID)

		msg, _ := NewEvent(EventPresenceUpdate, map[string]any{
			"user_id": client.UserID,
			"status":  "online",
		})
		h.Broadcast(msg)
	}

	return h.OnlineUserIDs()
}

// StartLANKickTimer starts a 5min timer to kick a user from LAN parties
func (h *Hub) StartLANKickTimer(userID string) {
	h.lanMu.Lock()
	defer h.lanMu.Unlock()

	// Cancel existing timer
	if t, ok := h.lanKickTimers[userID]; ok {
		t.Stop()
	}

	h.lanKickTimers[userID] = time.AfterFunc(5*time.Minute, func() {
		if !h.IsUserOnline(userID) {
			slog.Info("lan: kicking offline user from all parties (5min timeout)", "user_id", userID)
			h.lanKickCallback(userID)
		}
		h.lanMu.Lock()
		delete(h.lanKickTimers, userID)
		h.lanMu.Unlock()
	})
	slog.Debug("lan: started 5min kick timer", "user_id", userID)
}

// cancelLANKickTimer cancels the kick timer for a user (reconnect)
func (h *Hub) cancelLANKickTimer(userID string) {
	h.lanMu.Lock()
	defer h.lanMu.Unlock()

	if t, ok := h.lanKickTimers[userID]; ok {
		t.Stop()
		delete(h.lanKickTimers, userID)
		slog.Debug("lan: cancelled kick timer (reconnect)", "user_id", userID)
	}
}

// SetLobbyCallbacks sets callbacks for lobby voice channels
func (h *Hub) SetLobbyCallbacks(createFn func(lobbyID, name string) (string, error), deleteFn func(channelID string), isLobbyFn func(channelID string) bool, renameFn func(channelID, name string) error) {
	h.lobbyCreateFn = createFn
	h.lobbyDeleteFn = deleteFn
	h.isLobbyFn = isLobbyFn
	h.lobbyRenameFn = renameFn
}

// GetDynamic returns info about a dynamic sub-channel (nil if it doesn't exist)
func (h *Hub) GetDynamic(channelID string) *DynamicChannelInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.dynamicChannels[channelID]
}

// IsDynamic checks whether a channel is a dynamic lobby sub-channel
func (h *Hub) IsDynamic(channelID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.dynamicChannels[channelID] != nil
}

// SpawnFromLobby creates a new dynamic sub-channel from a lobby
func (h *Hub) SpawnFromLobby(lobbyID, userID, name, password string) (string, error) {
	if h.lobbyCreateFn == nil {
		return "", nil
	}

	h.mu.Lock()
	h.lobbyCounters[lobbyID]++
	counter := h.lobbyCounters[lobbyID]
	h.mu.Unlock()

	if name == "" {
		name = "Room " + time.Now().Format("15:04")
	}
	_ = counter

	channelID, err := h.lobbyCreateFn(lobbyID, name)
	if err != nil {
		return "", err
	}

	h.mu.Lock()
	h.dynamicChannels[channelID] = &DynamicChannelInfo{
		ManagerID: userID,
		Password:  password,
		ParentID:  lobbyID,
		Name:      name,
	}
	h.mu.Unlock()

	slog.Info("lobby: sub-channel created", "channel_id", channelID, "lobby_id", lobbyID, "manager_id", userID)
	return channelID, nil
}

// SetDynamicPassword sets the password for a dynamic sub-channel (manager only)
func (h *Hub) SetDynamicPassword(channelID, userID, password string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	info := h.dynamicChannels[channelID]
	if info == nil || info.ManagerID != userID {
		return false
	}
	info.Password = password
	return true
}

// CheckDynamicPassword verifies the password for a dynamic sub-channel
func (h *Hub) CheckDynamicPassword(channelID, password string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	info := h.dynamicChannels[channelID]
	if info == nil {
		return true // not dynamic -> no password
	}
	if info.Password == "" {
		return true // no password
	}
	return subtle.ConstantTimeCompare([]byte(info.Password), []byte(password)) == 1
}

// checkDynamicCleanup checks whether a dynamic sub-channel is empty and deletes it
func (h *Hub) checkDynamicCleanup(channelID string) {
	h.mu.Lock()
	info := h.dynamicChannels[channelID]
	if info == nil {
		h.mu.Unlock()
		return
	}
	// Check if anyone is still in the channel
	users := h.voiceState[channelID]
	if len(users) > 0 {
		h.mu.Unlock()
		return
	}
	// Empty — delete
	delete(h.dynamicChannels, channelID)
	h.mu.Unlock()

	slog.Info("lobby: cleanup of empty sub-channel", "channel_id", channelID)
	if h.lobbyDeleteFn != nil {
		h.lobbyDeleteFn(channelID)
	}
}

// checkLiveWBCleanup removes the live whiteboard if the voice channel is empty and broadcasts stop.
func (h *Hub) checkLiveWBCleanup(channelID string) {
	h.mu.RLock()
	wb := h.liveWhiteboards[channelID]
	if wb == nil {
		h.mu.RUnlock()
		return
	}
	users := h.voiceState[channelID]
	if len(users) > 0 {
		h.mu.RUnlock()
		return
	}
	h.mu.RUnlock()

	// Voice channel is empty — delete WB (with DB cleanup)
	h.LiveWBDelete(channelID)

	slog.Info("livewb: cleanup (voice channel empty)", "channel_id", channelID)
	msg, _ := NewEvent(EventLiveWBStop, map[string]string{
		"channel_id": channelID,
	})
	h.Broadcast(msg)
}

// SetLiveWBCallbacks sets the DB persistence callbacks for live whiteboards.
func (h *Hub) SetLiveWBCallbacks(
	createFn func(channelID, starterID string) error,
	deleteFn func(channelID string) error,
	strokeFn func(stroke LiveWhiteboardStroke, channelID string) error,
	undoFn func(strokeID string) error,
	clearFn func(channelID string) error,
) {
	h.liveWBCreateFn = createFn
	h.liveWBDeleteFn = deleteFn
	h.liveWBStrokeFn = strokeFn
	h.liveWBUndoFn = undoFn
	h.liveWBClearFn = clearFn
}

// LoadLiveWB loads a live whiteboard session into memory (called on server startup).
func (h *Hub) LoadLiveWB(channelID, starterID string, strokes []LiveWhiteboardStroke) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveWhiteboards[channelID] = &LiveWhiteboard{
		StarterID: starterID,
		ChannelID: channelID,
		Strokes:   strokes,
	}
}

// LiveWBStart creates a new live whiteboard for a voice channel.
func (h *Hub) LiveWBStart(channelID, userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.liveWhiteboards[channelID] != nil {
		return false // already active
	}
	h.liveWhiteboards[channelID] = &LiveWhiteboard{
		StarterID: userID,
		ChannelID: channelID,
	}
	if h.liveWBCreateFn != nil {
		go h.liveWBCreateFn(channelID, userID)
	}
	return true
}

// LiveWBStop removes the live whiteboard (starter only). Returns true if stopped.
func (h *Hub) LiveWBStop(channelID, userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wb := h.liveWhiteboards[channelID]
	if wb == nil {
		return false
	}
	if wb.StarterID != userID {
		return false
	}
	delete(h.liveWhiteboards, channelID)
	if h.liveWBDeleteFn != nil {
		go h.liveWBDeleteFn(channelID)
	}
	return true
}

// LiveWBGet returns the live whiteboard for a channel (nil if none).
func (h *Hub) LiveWBGet(channelID string) *LiveWhiteboard {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.liveWhiteboards[channelID]
}

// LiveWBAddStroke adds a stroke to the live whiteboard. Returns false if no active WB.
func (h *Hub) LiveWBAddStroke(channelID string, stroke LiveWhiteboardStroke) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wb := h.liveWhiteboards[channelID]
	if wb == nil {
		return false
	}
	wb.Strokes = append(wb.Strokes, stroke)
	if h.liveWBStrokeFn != nil {
		s := stroke // copy for goroutine
		go h.liveWBStrokeFn(s, channelID)
	}
	return true
}

// LiveWBUndo removes the last stroke by the given user. Returns the stroke ID or "".
func (h *Hub) LiveWBUndo(channelID, userID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	wb := h.liveWhiteboards[channelID]
	if wb == nil {
		return ""
	}
	for i := len(wb.Strokes) - 1; i >= 0; i-- {
		if wb.Strokes[i].UserID == userID {
			id := wb.Strokes[i].ID
			wb.Strokes = append(wb.Strokes[:i], wb.Strokes[i+1:]...)
			if h.liveWBUndoFn != nil {
				go h.liveWBUndoFn(id)
			}
			return id
		}
	}
	return ""
}

// LiveWBClear removes all strokes from the live whiteboard.
func (h *Hub) LiveWBClear(channelID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wb := h.liveWhiteboards[channelID]
	if wb == nil {
		return false
	}
	wb.Strokes = nil
	if h.liveWBClearFn != nil {
		go h.liveWBClearFn(channelID)
	}
	return true
}

// LiveWBDelete removes a live whiteboard (no starter check — used for cleanup).
func (h *Hub) LiveWBDelete(channelID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.liveWhiteboards, channelID)
	if h.liveWBDeleteFn != nil {
		go h.liveWBDeleteFn(channelID)
	}
}

// LiveWBSnapshot returns a copy of the starter ID and all strokes for a live whiteboard.
func (h *Hub) LiveWBSnapshot(channelID string) (string, []LiveWhiteboardStroke, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	wb := h.liveWhiteboards[channelID]
	if wb == nil {
		return "", nil, false
	}
	strokes := make([]LiveWhiteboardStroke, len(wb.Strokes))
	copy(strokes, wb.Strokes)
	return wb.StarterID, strokes, true
}

// LiveWBState returns a map of channelID -> starterID for all active live whiteboards.
func (h *Hub) LiveWBState() map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.liveWhiteboards) == 0 {
		return nil
	}
	result := make(map[string]string, len(h.liveWhiteboards))
	for chID, wb := range h.liveWhiteboards {
		result[chID] = wb.StarterID
	}
	return result
}

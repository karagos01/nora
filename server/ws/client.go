package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 54 * time.Second
	maxMessageSize = 131072
)

type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	UserID  string
	dropped int // number of dropped messages (slow client detection)
}

const sendBufSize = 64

func NewClient(hub *Hub, conn *websocket.Conn, userID string) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, sendBufSize),
		UserID: userID,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	c.conn.SetReadLimit(maxMessageSize)

	for {
		_, msg, err := c.conn.Read(context.Background())
		if err != nil {
			break
		}

		var event IncomingEvent
		if err := json.Unmarshal(msg, &event); err != nil {
			continue
		}

		c.handleEvent(event)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), writeWait)
			err := c.conn.Write(ctx, websocket.MessageText, message)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), writeWait)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) handleEvent(event IncomingEvent) {
	switch event.Type {
	case EventTypingStart:
		var payload TypingPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return
		}
		payload.UserID = c.UserID
		msg, err := NewEvent(EventTypingStart, payload)
		if err != nil {
			return
		}
		c.hub.Broadcast(msg)

	case EventChannelTyping:
		// Channel typing — broadcast to all clients (client filters by channel_id)
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
			return
		}
		msg, err := NewEvent(EventChannelTyping, map[string]string{
			"channel_id": payload.ChannelID,
			"user_id":    c.UserID,
		})
		if err != nil {
			return
		}
		// Broadcast to all except the sender
		c.hub.mu.RLock()
		for client := range c.hub.clients {
			if client.UserID != c.UserID {
				select {
				case client.send <- msg:
				default:
				}
			}
		}
		c.hub.mu.RUnlock()

	case EventFileOffer, EventFileAccept, EventFileReject, EventFileChunk, EventFileAck, EventFileComplete, EventFileCancel, EventFileResume, EventFileIce, EventFileRequest:
		c.relayFileEvent(event)

	case EventGroupMessage, EventGroupKey:
		c.relayGroupEvent(event)

	case EventVoiceJoin:
		c.handleVoiceJoin(event)
	case EventVoiceLeave:
		c.handleVoiceLeave()
	case EventVoiceMute:
		c.handleVoiceMute(event)
	case EventVoiceOffer, EventVoiceAnswer, EventVoiceIce, EventScreenWatch:
		c.relayFileEvent(event)
	case EventDMMessageDelete, EventDMMessageEdit:
		c.relayFileEvent(event)
	case EventCallRing, EventCallAccept, EventCallDecline, EventCallHangup, EventCallOffer, EventCallAnswer, EventCallIce:
		c.relayFileEvent(event)
	case EventTransferReady, EventTransferProgress, EventTransferComplete, EventTransferError:
		c.relayFileEvent(event)

	case EventSwarmPieceRequest, EventSwarmPieceOffer, EventSwarmPieceAccept, EventSwarmPieceIce, EventSwarmPieceComplete:
		c.relayFileEvent(event)

	case EventScreenShare:
		c.handleScreenShare(event)

	case EventLiveWBStart:
		c.handleLiveWBStart(event)
	case EventLiveWBStop:
		c.handleLiveWBStop(event)
	case EventLiveWBStroke:
		c.handleLiveWBStroke(event)
	case EventLiveWBUndo:
		c.handleLiveWBUndo(event)
	case EventLiveWBClear:
		c.handleLiveWBClear(event)
	case EventLiveWBJoin:
		c.handleLiveWBJoin(event)

	case EventLobbyRename:
		c.handleLobbyRename(event)
	case EventLobbyPassword:
		c.handleLobbyPassword(event)

	default:
		slog.Warn("ws: unknown event type", "type", event.Type)
	}
}

func (c *Client) handleVoiceJoin(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
		Password  string `json:"password"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}

	targetChannelID := payload.ChannelID

	// Check if channel is a lobby -> spawn a new sub-channel
	if c.hub.isLobbyFn != nil && c.hub.isLobbyFn(payload.ChannelID) {
		newID, err := c.hub.SpawnFromLobby(payload.ChannelID, c.UserID, payload.Name, payload.Password)
		if err != nil {
			slog.Error("lobby: error creating room", "error", err)
			c.sendError("voice.error", "Failed to create lobby room")
			return
		}
		targetChannelID = newID

		// Broadcast channel.create for the new sub-channel
		if c.hub.lobbyCreateFn != nil {
			// Channel data is broadcast from lobbyCreateFn callback (in main.go)
			// But we need broadcast — we delegate to the callback code in main
		}
	}

	// Verify password for dynamic sub-channel
	if c.hub.IsDynamic(targetChannelID) {
		if !c.hub.CheckDynamicPassword(targetChannelID, payload.Password) {
			c.sendError("voice.error", "Wrong password")
			return
		}
	}

	users := c.hub.VoiceJoin(targetChannelID, c.UserID)
	msg, _ := NewEvent(EventVoiceState, map[string]any{
		"channel_id": targetChannelID,
		"users":      users,
		"joined":     c.UserID,
	})
	c.hub.Broadcast(msg)

	// Send live whiteboard state to the joining client (late joiner)
	if starterID, strokes, ok := c.hub.LiveWBSnapshot(targetChannelID); ok {
		stateMsg, _ := NewEvent(EventLiveWBState, map[string]any{
			"channel_id": targetChannelID,
			"starter_id": starterID,
			"strokes":    strokes,
		})
		select {
		case c.send <- stateMsg:
		default:
		}
	}
}

func (c *Client) handleVoiceLeave() {
	channelID, remaining := c.hub.VoiceLeave(c.UserID)
	if channelID == "" {
		return
	}
	msg, _ := NewEvent(EventVoiceState, map[string]any{
		"channel_id": channelID,
		"users":      remaining,
		"left":       c.UserID,
	})
	c.hub.Broadcast(msg)

	// Cleanup dynamic lobby sub-channel if empty
	c.hub.checkDynamicCleanup(channelID)

	// Cleanup live whiteboard if voice channel is now empty
	c.hub.checkLiveWBCleanup(channelID)
}

func (c *Client) handleLobbyRename(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" || payload.Name == "" {
		return
	}

	c.hub.mu.RLock()
	info := c.hub.dynamicChannels[payload.ChannelID]
	c.hub.mu.RUnlock()

	if info == nil || info.ManagerID != c.UserID {
		c.sendError("voice.error", "Not the room manager")
		return
	}

	// Rename in DB via callback
	if c.hub.lobbyRenameFn != nil {
		if err := c.hub.lobbyRenameFn(payload.ChannelID, payload.Name); err != nil {
			slog.Error("lobby: rename failed", "channel_id", payload.ChannelID, "error", err)
			return
		}
	}

	c.hub.mu.Lock()
	if di := c.hub.dynamicChannels[payload.ChannelID]; di != nil {
		di.Name = payload.Name
	}
	c.hub.mu.Unlock()

	// Broadcast channel.update
	msg, _ := NewEvent(EventChannelUpdate, map[string]any{
		"id":   payload.ChannelID,
		"name": payload.Name,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) handleLobbyPassword(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
		Password  string `json:"password"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}

	if !c.hub.SetDynamicPassword(payload.ChannelID, c.UserID, payload.Password) {
		c.sendError("voice.error", "Not the room manager")
	}
}

// sendError sends an error event back to the client
func (c *Client) sendError(eventType, message string) {
	msg, err := NewEvent(EventType(eventType), map[string]string{"error": message})
	if err != nil {
		return
	}
	select {
	case c.send <- msg:
	default:
	}
}

func (c *Client) handleVoiceMute(event IncomingEvent) {
	var payload struct {
		Muted    bool `json:"muted"`
		Deafened bool `json:"deafened"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return
	}
	msg, _ := NewEvent(EventVoiceMute, map[string]any{
		"user_id":  c.UserID,
		"muted":    payload.Muted,
		"deafened": payload.Deafened,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) relayFileEvent(event IncomingEvent) {
	var target struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(event.Payload, &target); err != nil || target.To == "" {
		return
	}

	// Add "from" to the payload (anti-spoof)
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return
	}
	payload["from"] = c.UserID

	p, err := json.Marshal(payload)
	if err != nil {
		return
	}

	msg, err := json.Marshal(Event{Type: event.Type, Payload: p})
	if err != nil {
		return
	}

	c.hub.BroadcastToUser(target.To, msg)
}

func (c *Client) handleScreenShare(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
		Sharing   bool   `json:"sharing"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}

	// Track screen share state in the hub
	c.hub.SetScreenShare(c.UserID, payload.ChannelID, payload.Sharing)

	msg, _ := NewEvent(EventScreenShare, map[string]any{
		"channel_id": payload.ChannelID,
		"user_id":    c.UserID,
		"sharing":    payload.Sharing,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) handleLiveWBStart(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}
	if c.hub.VoiceChannelOf(c.UserID) != payload.ChannelID {
		c.sendError("livewb.error", "Not in this voice channel")
		return
	}
	if !c.hub.LiveWBStart(payload.ChannelID, c.UserID) {
		c.sendError("livewb.error", "Whiteboard already active")
		return
	}
	msg, _ := NewEvent(EventLiveWBStart, map[string]any{
		"channel_id": payload.ChannelID,
		"starter_id": c.UserID,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) handleLiveWBStop(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}
	if !c.hub.LiveWBStop(payload.ChannelID, c.UserID) {
		return
	}
	msg, _ := NewEvent(EventLiveWBStop, map[string]string{
		"channel_id": payload.ChannelID,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) handleLiveWBStroke(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
		ID        string `json:"id"`
		PathData  string `json:"path_data"`
		Color     string `json:"color"`
		Width     int    `json:"width"`
		Tool      string `json:"tool"`
		Username  string `json:"username"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}
	if c.hub.VoiceChannelOf(c.UserID) != payload.ChannelID {
		return
	}
	strokeID := payload.ID
	if strokeID == "" {
		strokeID = uuid.New().String()
	}
	stroke := LiveWhiteboardStroke{
		ID:       strokeID,
		UserID:   c.UserID,
		PathData: payload.PathData,
		Color:    payload.Color,
		Width:    payload.Width,
		Tool:     payload.Tool,
		Username: payload.Username,
	}
	if !c.hub.LiveWBAddStroke(payload.ChannelID, stroke) {
		return
	}
	msg, _ := NewEvent(EventLiveWBStroke, map[string]any{
		"channel_id": payload.ChannelID,
		"stroke":     stroke,
	})
	c.hub.BroadcastExcluding(msg, map[string]bool{c.UserID: true})
}

func (c *Client) handleLiveWBUndo(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}
	if c.hub.VoiceChannelOf(c.UserID) != payload.ChannelID {
		return
	}
	strokeID := c.hub.LiveWBUndo(payload.ChannelID, c.UserID)
	if strokeID == "" {
		return
	}
	msg, _ := NewEvent(EventLiveWBUndo, map[string]any{
		"channel_id": payload.ChannelID,
		"stroke_id":  strokeID,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) handleLiveWBClear(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}
	if c.hub.VoiceChannelOf(c.UserID) != payload.ChannelID {
		return
	}
	if !c.hub.LiveWBClear(payload.ChannelID) {
		return
	}
	msg, _ := NewEvent(EventLiveWBClear, map[string]any{
		"channel_id": payload.ChannelID,
	})
	c.hub.Broadcast(msg)
}

func (c *Client) handleLiveWBJoin(event IncomingEvent) {
	var payload struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.ChannelID == "" {
		return
	}
	if c.hub.VoiceChannelOf(c.UserID) != payload.ChannelID {
		return
	}
	starterID, strokes, ok := c.hub.LiveWBSnapshot(payload.ChannelID)
	if !ok {
		return
	}
	msg, _ := NewEvent(EventLiveWBState, map[string]any{
		"channel_id": payload.ChannelID,
		"starter_id": starterID,
		"strokes":    strokes,
	})
	select {
	case c.send <- msg:
	default:
	}
}

func (c *Client) relayGroupEvent(event IncomingEvent) {
	var parsed struct {
		To []string `json:"to"`
	}
	if err := json.Unmarshal(event.Payload, &parsed); err != nil || len(parsed.To) == 0 {
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return
	}
	payload["from"] = c.UserID

	p, err := json.Marshal(payload)
	if err != nil {
		return
	}

	msg, err := json.Marshal(Event{Type: event.Type, Payload: p})
	if err != nil {
		return
	}

	for _, userID := range parsed.To {
		c.hub.BroadcastToUser(userID, msg)
	}
}

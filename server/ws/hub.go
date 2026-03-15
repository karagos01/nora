package ws

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// DynamicChannelInfo drží runtime state pro lobby sub-kanály (neukládá se do DB)
type DynamicChannelInfo struct {
	ManagerID string // userID prvního uživatele (správce)
	Password  string // volitelné heslo
	ParentID  string // lobby channel ID
	Name      string // název sub-kanálu
}

type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte

	// Voice state: channelID -> set of userIDs
	voiceState map[string]map[string]bool
	// Reverzní lookup: userID -> channelID
	voiceUser map[string]string

	// Screen share state: userID -> channelID
	screenSharers map[string]string

	// Lobby voice channels: dynamické sub-kanály
	dynamicChannels map[string]*DynamicChannelInfo // channelID → info
	lobbyCounters   map[string]int                 // lobbyID → next room number

	// Lobby callbacks (set z main.go)
	lobbyCreateFn func(lobbyID, name string) (channelID string, err error)
	lobbyDeleteFn func(channelID string)
	isLobbyFn     func(channelID string) bool
	lobbyRenameFn func(channelID, name string) error

	// User status callback — vrátí status a status_text z DB
	userStatusFn func(userID string) (status, statusText string)

	// LAN kick callback — zavolá se po 5min offline, odebere usera ze všech LAN parties
	lanKickCallback func(userID string)
	// Aktivní kick timery (userID → timer), chráněno lanMu
	lanMu         sync.Mutex
	lanKickTimers map[string]*time.Timer

	// OnConnect callback — zavolá se po připojení klienta (pro IP refresh)
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
		lanKickTimers:   make(map[string]*time.Timer),
	}
}

// SetUserStatusFn nastaví callback pro čtení statusu uživatele z DB
func (h *Hub) SetUserStatusFn(fn func(userID string) (string, string)) {
	h.userStatusFn = fn
}

// SetOnConnect nastaví callback volaný po připojení klienta
func (h *Hub) SetOnConnect(fn func(userID string, r *http.Request)) {
	h.onConnectFn = fn
}

// GetUserStatuses vrátí statuses mapu pro online uživatele
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

// SetLANKickCallback nastaví callback volaný po 5min offline pro kick z LAN parties
func (h *Hub) SetLANKickCallback(cb func(userID string)) {
	h.lanKickCallback = cb
}

// OnlineUserIDs vrací seznam unikátních uživatelských ID aktuálně připojených přes WS
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

// IsUserOnline zjistí zda má uživatel aspoň jedno WS spojení (thread-safe)
func (h *Hub) IsUserOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isUserOnline(userID)
}

// isUserOnline zjistí zda má uživatel aspoň jedno WS spojení (musí se volat s h.mu.RLock)
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
			slog.Info("ws: klient připojen", "user_id", client.UserID, "total", len(h.clients))

			// Broadcast presence online pokud to je první session uživatele
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

			// Voice cleanup: pokud uživatel nemá jinou session, odeber z voice kanálu
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
			slog.Info("ws: klient odpojen", "user_id", client.UserID, "total", len(h.clients))

			// Broadcast voice state update pokud uživatel opustil voice kanál
			if voiceLeftChannel != "" {
				msg, _ := NewEvent(EventVoiceState, map[string]any{
					"channel_id": voiceLeftChannel,
					"users":      voiceRemaining,
					"left":       client.UserID,
				})
				h.Broadcast(msg)

				// Cleanup dynamického lobby sub-kanálu pokud prázdný
				h.checkDynamicCleanup(voiceLeftChannel)
			}

			// Broadcast presence offline pokud to byla poslední session
			if !stillOnline {
				msg, _ := NewEvent(EventPresenceUpdate, map[string]any{
					"user_id": client.UserID,
					"status":  "offline",
				})
				h.Broadcast(msg)

				// Spustit 5min LAN kick timer
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
				default:
					go func(c *Client) {
						h.unregister <- c
					}(client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(msg []byte) {
	h.broadcast <- msg
}

// BroadcastToUser posílá zprávu konkrétnímu uživateli (všechny jeho sessions)
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

// DisconnectUser odpojí všechny sessions daného uživatele
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

// VoiceJoin přidá uživatele do voice kanálu (auto-leave z předchozího), vrátí seznam uživatelů v kanálu
func (h *Hub) VoiceJoin(channelID, userID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Auto-leave z předchozího kanálu
	h.voiceLeaveUnsafe(userID)

	if h.voiceState[channelID] == nil {
		h.voiceState[channelID] = make(map[string]bool)
	}
	h.voiceState[channelID][userID] = true
	h.voiceUser[userID] = channelID

	return h.voiceUsersUnsafe(channelID)
}

// VoiceLeave odebere uživatele z voice kanálu, vrátí channelID a zbývající uživatele
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

// VoiceUsers vrátí seznam uživatelů v kanálu
func (h *Hub) VoiceUsers(channelID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.voiceUsersUnsafe(channelID)
}

// AllVoiceState vrátí kompletní voice state pro REST endpoint
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

// VoiceChannelOf vrátí channelID kde je uživatel, nebo ""
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

// SetScreenShare nastaví screen share stav pro uživatele.
func (h *Hub) SetScreenShare(userID, channelID string, sharing bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sharing {
		h.screenSharers[userID] = channelID
	} else {
		delete(h.screenSharers, userID)
	}
}

// ScreenSharers vrátí mapu userID → channelID pro všechny aktivní screen sharery.
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

// RegisterSync přidá klienta synchronně a vrátí seznam online uživatelů (včetně nového)
func (h *Hub) RegisterSync(client *Client) []string {
	h.mu.Lock()
	wasOnline := h.isUserOnline(client.UserID)
	h.clients[client] = true
	h.mu.Unlock()
	slog.Info("ws: klient připojen", "user_id", client.UserID, "total", len(h.clients))

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

// StartLANKickTimer spustí 5min timer pro kick uživatele z LAN parties
func (h *Hub) StartLANKickTimer(userID string) {
	h.lanMu.Lock()
	defer h.lanMu.Unlock()

	// Zrušit existující timer
	if t, ok := h.lanKickTimers[userID]; ok {
		t.Stop()
	}

	h.lanKickTimers[userID] = time.AfterFunc(5*time.Minute, func() {
		if !h.IsUserOnline(userID) {
			slog.Info("lan: kick offline uživatele ze všech parties (5min timeout)", "user_id", userID)
			h.lanKickCallback(userID)
		}
		h.lanMu.Lock()
		delete(h.lanKickTimers, userID)
		h.lanMu.Unlock()
	})
	slog.Debug("lan: spuštěn 5min kick timer", "user_id", userID)
}

// cancelLANKickTimer zruší kick timer pro uživatele (reconnect)
func (h *Hub) cancelLANKickTimer(userID string) {
	h.lanMu.Lock()
	defer h.lanMu.Unlock()

	if t, ok := h.lanKickTimers[userID]; ok {
		t.Stop()
		delete(h.lanKickTimers, userID)
		slog.Debug("lan: zrušen kick timer (reconnect)", "user_id", userID)
	}
}

// SetLobbyCallbacks nastaví callbacky pro lobby voice kanály
func (h *Hub) SetLobbyCallbacks(createFn func(lobbyID, name string) (string, error), deleteFn func(channelID string), isLobbyFn func(channelID string) bool, renameFn func(channelID, name string) error) {
	h.lobbyCreateFn = createFn
	h.lobbyDeleteFn = deleteFn
	h.isLobbyFn = isLobbyFn
	h.lobbyRenameFn = renameFn
}

// GetDynamic vrátí info o dynamickém sub-kanálu (nil pokud neexistuje)
func (h *Hub) GetDynamic(channelID string) *DynamicChannelInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.dynamicChannels[channelID]
}

// IsDynamic zjistí zda kanál je dynamický lobby sub-kanál
func (h *Hub) IsDynamic(channelID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.dynamicChannels[channelID] != nil
}

// SpawnFromLobby vytvoří nový dynamický sub-kanál z lobby
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

	slog.Info("lobby: vytvořen sub-kanál", "channel_id", channelID, "lobby_id", lobbyID, "manager_id", userID)
	return channelID, nil
}

// SetDynamicPassword nastaví heslo pro dynamický sub-kanál (jen manager)
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

// CheckDynamicPassword ověří heslo pro dynamický sub-kanál
func (h *Hub) CheckDynamicPassword(channelID, password string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	info := h.dynamicChannels[channelID]
	if info == nil {
		return true // není dynamický → bez hesla
	}
	if info.Password == "" {
		return true // bez hesla
	}
	return subtle.ConstantTimeCompare([]byte(info.Password), []byte(password)) == 1
}

// checkDynamicCleanup zkontroluje zda dynamický sub-kanál zůstal prázdný a smaže ho
func (h *Hub) checkDynamicCleanup(channelID string) {
	h.mu.Lock()
	info := h.dynamicChannels[channelID]
	if info == nil {
		h.mu.Unlock()
		return
	}
	// Zjistit zda v kanálu ještě někdo je
	users := h.voiceState[channelID]
	if len(users) > 0 {
		h.mu.Unlock()
		return
	}
	// Prázdný — smazat
	delete(h.dynamicChannels, channelID)
	h.mu.Unlock()

	slog.Info("lobby: cleanup prázdného sub-kanálu", "channel_id", channelID)
	if h.lobbyDeleteFn != nil {
		h.lobbyDeleteFn(channelID)
	}
}

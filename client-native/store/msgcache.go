package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CachedAttachment je příloha uložená v message cache.
type CachedAttachment struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// CachedMessage je channel zpráva uložená v lokální cache.
type CachedMessage struct {
	ID          string             `json:"id"`
	ChannelID   string             `json:"channel_id"`
	UserID      string             `json:"user_id"`
	Username    string             `json:"username"`
	Content     string             `json:"content"`
	ReplyToID   string             `json:"reply_to_id,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	IsPinned    bool               `json:"is_pinned,omitempty"`
	IsHidden    bool               `json:"is_hidden,omitempty"`
	Attachments []CachedAttachment `json:"attachments,omitempty"`
}

const maxCachedPerChannel = 200

// MessageCache ukládá channel zprávy lokálně pro rychlejší načítání.
type MessageCache struct {
	mu       sync.Mutex
	filePath string
	messages map[string][]CachedMessage // channelID → messages (sorted by time)
	dirty    bool
}

// NewMessageCache vytvoří message cache pro daný server a identitu.
func NewMessageCache(publicKey, serverURL string) *MessageCache {
	short := publicKey
	if len(short) > 16 {
		short = short[:16]
	}
	h := sha256.Sum256([]byte(serverURL))
	serverHash := fmt.Sprintf("%x", h[:8])

	mc := &MessageCache{
		filePath: filepath.Join(noraDir(), fmt.Sprintf("msgcache_%s_%s.json", short, serverHash)),
		messages: make(map[string][]CachedMessage),
	}
	mc.load()
	return mc
}

func (mc *MessageCache) load() {
	data, err := os.ReadFile(mc.filePath)
	if err != nil {
		return
	}
	json.Unmarshal(data, &mc.messages)
}

// Save uloží cache na disk.
func (mc *MessageCache) Save() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if !mc.dirty {
		return
	}
	ensureDir()
	data, err := json.Marshal(mc.messages)
	if err != nil {
		return
	}
	os.WriteFile(mc.filePath, data, 0600)
	mc.dirty = false
}

// GetMessages vrátí cached zprávy pro kanál.
func (mc *MessageCache) GetMessages(channelID string) []CachedMessage {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	msgs := mc.messages[channelID]
	result := make([]CachedMessage, len(msgs))
	copy(result, msgs)
	return result
}

// SetMessages nastaví zprávy pro kanál (nahradí existující cache).
func (mc *MessageCache) SetMessages(channelID string, msgs []CachedMessage) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(msgs) > maxCachedPerChannel {
		msgs = msgs[len(msgs)-maxCachedPerChannel:]
	}
	mc.messages[channelID] = msgs
	mc.dirty = true
}

// AddMessage přidá zprávu do cache (deduplikace dle ID).
func (mc *MessageCache) AddMessage(msg CachedMessage) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	msgs := mc.messages[msg.ChannelID]
	for _, m := range msgs {
		if m.ID == msg.ID {
			return
		}
	}
	mc.messages[msg.ChannelID] = append(msgs, msg)
	// Trim pokud přesahuje limit
	if len(mc.messages[msg.ChannelID]) > maxCachedPerChannel {
		mc.messages[msg.ChannelID] = mc.messages[msg.ChannelID][len(mc.messages[msg.ChannelID])-maxCachedPerChannel:]
	}
	mc.dirty = true
}

// UpdateMessage aktualizuje obsah zprávy v cache.
func (mc *MessageCache) UpdateMessage(msgID, content string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for chID, msgs := range mc.messages {
		for i, m := range msgs {
			if m.ID == msgID {
				mc.messages[chID][i].Content = content
				mc.dirty = true
				return
			}
		}
	}
}

// DeleteMessage smaže zprávu z cache.
func (mc *MessageCache) DeleteMessage(msgID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for chID, msgs := range mc.messages {
		for i, m := range msgs {
			if m.ID == msgID {
				mc.messages[chID] = append(msgs[:i], msgs[i+1:]...)
				mc.dirty = true
				return
			}
		}
	}
}

// UpdatePin aktualizuje pin stav zprávy.
func (mc *MessageCache) UpdatePin(msgID string, pinned bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for chID, msgs := range mc.messages {
		for i, m := range msgs {
			if m.ID == msgID {
				mc.messages[chID][i].IsPinned = pinned
				mc.dirty = true
				return
			}
		}
	}
}

// UpdateHidden aktualizuje hidden stav zprávy.
func (mc *MessageCache) UpdateHidden(msgID string, hidden bool, content string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for chID, msgs := range mc.messages {
		for i, m := range msgs {
			if m.ID == msgID {
				mc.messages[chID][i].IsHidden = hidden
				mc.messages[chID][i].Content = content
				mc.dirty = true
				return
			}
		}
	}
}

// DeleteByUser smaže všechny zprávy od daného uživatele.
func (mc *MessageCache) DeleteByUser(userID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for chID, msgs := range mc.messages {
		var kept []CachedMessage
		for _, m := range msgs {
			if m.UserID != userID {
				kept = append(kept, m)
			}
		}
		mc.messages[chID] = kept
	}
	mc.dirty = true
}

// HideByUser skryje všechny zprávy od daného uživatele.
func (mc *MessageCache) HideByUser(userID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for chID, msgs := range mc.messages {
		for i, m := range msgs {
			if m.UserID == userID {
				mc.messages[chID][i].IsHidden = true
			}
		}
	}
	mc.dirty = true
}

// MessageCount vrátí celkový počet cached zpráv.
func (mc *MessageCache) MessageCount() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	count := 0
	for _, msgs := range mc.messages {
		count += len(msgs)
	}
	return count
}

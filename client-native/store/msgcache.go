package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CachedAttachment is an attachment stored in the message cache.
type CachedAttachment struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// CachedMessage is a channel message stored in the local cache.
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

// MessageCache stores channel messages locally for faster loading.
type MessageCache struct {
	mu       sync.Mutex
	filePath string
	messages map[string][]CachedMessage // channelID → messages (sorted by time)
	dirty    bool
}

// NewMessageCache creates a message cache for the given server and identity.
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

// Save writes the cache to disk.
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

// GetMessages returns cached messages for a channel, sorted by time.
func (mc *MessageCache) GetMessages(channelID string) []CachedMessage {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	msgs := mc.messages[channelID]
	result := make([]CachedMessage, len(msgs))
	copy(result, msgs)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// SetMessages sets messages for a channel (replaces existing cache).
func (mc *MessageCache) SetMessages(channelID string, msgs []CachedMessage) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(msgs) > maxCachedPerChannel {
		msgs = msgs[len(msgs)-maxCachedPerChannel:]
	}
	mc.messages[channelID] = msgs
	mc.dirty = true
}

// AddMessage adds a message to the cache (deduplication by ID).
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
	// Trim if exceeds limit
	if len(mc.messages[msg.ChannelID]) > maxCachedPerChannel {
		mc.messages[msg.ChannelID] = mc.messages[msg.ChannelID][len(mc.messages[msg.ChannelID])-maxCachedPerChannel:]
	}
	mc.dirty = true
}

// UpdateMessage updates message content in the cache.
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

// DeleteMessage removes a message from the cache.
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

// UpdatePin updates the pin state of a message.
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

// UpdateHidden updates the hidden state of a message.
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

// DeleteByUser removes all messages from the given user.
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

// HideByUser hides all messages from the given user.
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

// MessageCount returns the total number of cached messages.
func (mc *MessageCache) MessageCount() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	count := 0
	for _, msgs := range mc.messages {
		count += len(msgs)
	}
	return count
}

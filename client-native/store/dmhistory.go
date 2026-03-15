package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StoredDMMessage is a decrypted DM message stored locally.
type StoredDMMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	SenderID       string    `json:"sender_id"`
	Content        string    `json:"content"` // decrypted plaintext
	ReplyToID      string    `json:"reply_to_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// DMHistory manages local DM message persistence per identity.
type DMHistory struct {
	mu        sync.Mutex
	publicKey string
	messages  map[string][]StoredDMMessage // convID → messages (sorted by time)
	dirty     bool
}

func NewDMHistory(publicKey string) *DMHistory {
	h := &DMHistory{
		publicKey: publicKey,
		messages:  make(map[string][]StoredDMMessage),
	}
	h.load()
	return h
}

func (h *DMHistory) historyPath() string {
	// Use first 16 chars of public key as filename to avoid collisions
	short := h.publicKey
	if len(short) > 16 {
		short = short[:16]
	}
	return filepath.Join(noraDir(), "dm_history_"+short+".json")
}

func (h *DMHistory) load() {
	data, err := os.ReadFile(h.historyPath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &h.messages)
}

func (h *DMHistory) Save() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty {
		return
	}
	ensureDir()
	data, err := json.Marshal(h.messages)
	if err != nil {
		return
	}
	os.WriteFile(h.historyPath(), data, 0600)
	h.dirty = false
}

// AddMessage adds a decrypted message to the local history.
// Deduplicates by message ID.
func (h *DMHistory) AddMessage(msg StoredDMMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	msgs := h.messages[msg.ConversationID]
	for _, m := range msgs {
		if m.ID == msg.ID {
			return // already stored
		}
	}

	h.messages[msg.ConversationID] = append(msgs, msg)
	h.dirty = true
}

// UpdateMessage updates the content of a message in local history.
func (h *DMHistory) UpdateMessage(convID, msgID, newContent string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	msgs := h.messages[convID]
	for i, m := range msgs {
		if m.ID == msgID {
			msgs[i].Content = newContent
			h.dirty = true
			return
		}
	}
}

// DeleteMessage removes a single message by ID from the local history.
func (h *DMHistory) DeleteMessage(msgID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for convID, msgs := range h.messages {
		for i, m := range msgs {
			if m.ID == msgID {
				h.messages[convID] = append(msgs[:i], msgs[i+1:]...)
				h.dirty = true
				return
			}
		}
	}
}

// DeleteConversation removes all stored messages for a conversation.
func (h *DMHistory) DeleteConversation(convID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.messages[convID]; ok {
		delete(h.messages, convID)
		h.dirty = true
	}
}

// GetMessages returns all stored messages for a conversation, sorted by time.
func (h *DMHistory) GetMessages(convID string) []StoredDMMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	msgs := h.messages[convID]
	result := make([]StoredDMMessage, len(msgs))
	copy(result, msgs)
	return result
}

// DeleteOlderThan deletes messages older than maxAge. Returns the number of deleted messages.
func (h *DMHistory) DeleteOlderThan(maxAge time.Duration) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	deleted := 0
	for convID, msgs := range h.messages {
		var kept []StoredDMMessage
		for _, m := range msgs {
			if m.CreatedAt.Before(cutoff) {
				deleted++
			} else {
				kept = append(kept, m)
			}
		}
		if len(kept) == 0 {
			delete(h.messages, convID)
		} else {
			h.messages[convID] = kept
		}
	}
	if deleted > 0 {
		h.dirty = true
	}
	return deleted
}

// MessageCount returns the total number of messages across all conversations.
func (h *DMHistory) MessageCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	count := 0
	for _, msgs := range h.messages {
		count += len(msgs)
	}
	return count
}

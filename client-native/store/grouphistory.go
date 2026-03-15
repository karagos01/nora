package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StoredGroupAttachment is an attachment stored in the local history.
type StoredGroupAttachment struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// StoredGroupMessage is a decrypted group message stored locally.
type StoredGroupMessage struct {
	ID          string                  `json:"id"`
	GroupID     string                  `json:"group_id"`
	SenderID    string                  `json:"sender_id"`
	Content     string                  `json:"content"` // decrypted plaintext
	Attachments []StoredGroupAttachment `json:"attachments,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
}

const keyRotationThreshold = 500 // key rotation after N sent messages

// GroupHistory manages local group message and key persistence per identity.
type GroupHistory struct {
	mu         sync.Mutex
	publicKey  string
	messages   map[string][]StoredGroupMessage // groupID → messages
	keys       map[string]string               // groupID → current group key (hex)
	oldKeys    map[string][]string             // groupID → old keys (for decrypting history)
	msgCount   map[string]int                  // groupID → number of messages sent with current key
	dirty      bool
}

type groupHistoryData struct {
	Messages map[string][]StoredGroupMessage `json:"messages"`
	Keys     map[string]string               `json:"keys"`
	OldKeys  map[string][]string             `json:"old_keys,omitempty"`
	MsgCount map[string]int                  `json:"msg_count,omitempty"`
}

func NewGroupHistory(publicKey string) *GroupHistory {
	h := &GroupHistory{
		publicKey: publicKey,
		messages:  make(map[string][]StoredGroupMessage),
		keys:      make(map[string]string),
		oldKeys:   make(map[string][]string),
		msgCount:  make(map[string]int),
	}
	h.load()
	return h
}

func (h *GroupHistory) historyPath() string {
	short := h.publicKey
	if len(short) > 16 {
		short = short[:16]
	}
	return filepath.Join(noraDir(), "group_history_"+short+".json")
}

func (h *GroupHistory) load() {
	data, err := os.ReadFile(h.historyPath())
	if err != nil {
		return
	}
	var d groupHistoryData
	if json.Unmarshal(data, &d) == nil {
		if d.Messages != nil {
			h.messages = d.Messages
		}
		if d.Keys != nil {
			h.keys = d.Keys
		}
		if d.OldKeys != nil {
			h.oldKeys = d.OldKeys
		}
		if d.MsgCount != nil {
			h.msgCount = d.MsgCount
		}
	}
}

func (h *GroupHistory) Save() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty {
		return
	}
	ensureDir()
	data, err := json.Marshal(groupHistoryData{
		Messages: h.messages,
		Keys:     h.keys,
		OldKeys:  h.oldKeys,
		MsgCount: h.msgCount,
	})
	if err != nil {
		return
	}
	os.WriteFile(h.historyPath(), data, 0600)
	h.dirty = false
}

// AddMessage adds a decrypted message to local history. Deduplicates by ID.
func (h *GroupHistory) AddMessage(msg StoredGroupMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	msgs := h.messages[msg.GroupID]
	for _, m := range msgs {
		if m.ID == msg.ID {
			return
		}
	}

	h.messages[msg.GroupID] = append(msgs, msg)
	h.dirty = true
}

// GetMessages returns all stored messages for a group.
func (h *GroupHistory) GetMessages(groupID string) []StoredGroupMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	msgs := h.messages[groupID]
	result := make([]StoredGroupMessage, len(msgs))
	copy(result, msgs)
	return result
}

// DeleteGroup removes all stored messages, keys and counters for a group.
func (h *GroupHistory) DeleteGroup(groupID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.messages, groupID)
	delete(h.keys, groupID)
	delete(h.oldKeys, groupID)
	delete(h.msgCount, groupID)
	h.dirty = true
}

// SetKey stores the group key (hex).
func (h *GroupHistory) SetKey(groupID, keyHex string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.keys[groupID] = keyHex
	h.dirty = true
}

// GetKey returns the stored group key (hex), or "" if not found.
func (h *GroupHistory) GetKey(groupID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.keys[groupID]
}

// GetAllKeys returns the current key + all old keys (for fallback decryption).
func (h *GroupHistory) GetAllKeys(groupID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var keys []string
	if k := h.keys[groupID]; k != "" {
		keys = append(keys, k)
	}
	keys = append(keys, h.oldKeys[groupID]...)
	return keys
}

// IncrementCount increments the sent message counter and returns the new value.
func (h *GroupHistory) IncrementCount(groupID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgCount[groupID]++
	h.dirty = true
	return h.msgCount[groupID]
}

// NeedsRotation returns true if it's time for key rotation.
func (h *GroupHistory) NeedsRotation(groupID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.msgCount[groupID] >= keyRotationThreshold
}

// DeleteOlderThan deletes messages older than maxAge, but PRESERVES keys, oldKeys and msgCount.
func (h *GroupHistory) DeleteOlderThan(maxAge time.Duration) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	deleted := 0
	for groupID, msgs := range h.messages {
		var kept []StoredGroupMessage
		for _, m := range msgs {
			if m.CreatedAt.Before(cutoff) {
				deleted++
			} else {
				kept = append(kept, m)
			}
		}
		if len(kept) == 0 {
			h.messages[groupID] = nil // don't delete keys!
		} else {
			h.messages[groupID] = kept
		}
	}
	if deleted > 0 {
		h.dirty = true
	}
	return deleted
}

// MessageCount returns the total number of messages across all groups.
func (h *GroupHistory) MessageCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	count := 0
	for _, msgs := range h.messages {
		count += len(msgs)
	}
	return count
}

// RotateKey moves the current key to history, sets a new one and resets the counter.
func (h *GroupHistory) RotateKey(groupID, newKeyHex string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.keys[groupID]; old != "" {
		// Max 10 old keys
		history := h.oldKeys[groupID]
		history = append([]string{old}, history...)
		if len(history) > 10 {
			history = history[:10]
		}
		h.oldKeys[groupID] = history
	}
	h.keys[groupID] = newKeyHex
	h.msgCount[groupID] = 0
	h.dirty = true
}

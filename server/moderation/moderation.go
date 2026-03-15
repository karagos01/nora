package moderation

import (
	"strings"
	"sync"
	"time"
)

// AutoMod provides word filter and spam detection for channel messages.
type AutoMod struct {
	Mu              sync.RWMutex
	Enabled         bool
	WordFilter      []string
	wordFilterLower []string

	SpamMaxMessages    int // max messages in window (default 5)
	SpamWindowSeconds  int // window length in seconds (default 10)
	SpamTimeoutSeconds int // timeout on spam in seconds (default 300)

	OwnerID string // for issued_by on auto-timeout

	spamTracker map[string][]time.Time // userID → timestamps
}

// New creates an AutoMod with defaults and starts a cleanup goroutine.
func New() *AutoMod {
	am := &AutoMod{
		SpamMaxMessages:    5,
		SpamWindowSeconds:  10,
		SpamTimeoutSeconds: 300,
		spamTracker:        make(map[string][]time.Time),
	}
	go am.cleanup()
	return am
}

// SetWordFilter sets the word filter and lowercase cache.
func (am *AutoMod) SetWordFilter(words []string) {
	am.Mu.Lock()
	defer am.Mu.Unlock()
	am.WordFilter = words
	am.wordFilterLower = make([]string, len(words))
	for i, w := range words {
		am.wordFilterLower[i] = strings.ToLower(w)
	}
}

// CheckContent checks a message against the word filter.
// Returns (blocked, matchedWord).
func (am *AutoMod) CheckContent(content string) (bool, string) {
	am.Mu.RLock()
	defer am.Mu.RUnlock()
	if len(am.wordFilterLower) == 0 {
		return false, ""
	}
	lower := strings.ToLower(content)
	for i, w := range am.wordFilterLower {
		if w != "" && strings.Contains(lower, w) {
			return true, am.WordFilter[i]
		}
	}
	return false, ""
}

// TrackMessage records a user's message and returns true if it is spam.
func (am *AutoMod) TrackMessage(userID string) bool {
	am.Mu.Lock()
	defer am.Mu.Unlock()

	now := time.Now()
	window := time.Duration(am.SpamWindowSeconds) * time.Second
	cutoff := now.Add(-window)

	// Filter out old entries
	timestamps := am.spamTracker[userID]
	filtered := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	am.spamTracker[userID] = filtered

	return len(filtered) > am.SpamMaxMessages
}

// cleanup deletes old entries from the spam tracker every minute.
func (am *AutoMod) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		am.Mu.Lock()
		now := time.Now()
		window := time.Duration(am.SpamWindowSeconds) * time.Second
		cutoff := now.Add(-window)
		for uid, timestamps := range am.spamTracker {
			filtered := timestamps[:0]
			for _, t := range timestamps {
				if t.After(cutoff) {
					filtered = append(filtered, t)
				}
			}
			if len(filtered) == 0 {
				delete(am.spamTracker, uid)
			} else {
				am.spamTracker[uid] = filtered
			}
		}
		am.Mu.Unlock()
	}
}

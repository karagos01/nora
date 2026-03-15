package moderation

import (
	"strings"
	"sync"
	"time"
)

// AutoMod poskytuje word filter a spam detekci pro channel zprávy.
type AutoMod struct {
	Mu              sync.RWMutex
	Enabled         bool
	WordFilter      []string
	wordFilterLower []string

	SpamMaxMessages    int // max zpráv v okně (default 5)
	SpamWindowSeconds  int // délka okna v sekundách (default 10)
	SpamTimeoutSeconds int // timeout při spamu v sekundách (default 300)

	OwnerID string // pro issued_by při auto-timeout

	spamTracker map[string][]time.Time // userID → timestamps
}

// New vytvoří AutoMod s defaulty a spustí cleanup goroutinu.
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

// SetWordFilter nastaví word filter a lowercase cache.
func (am *AutoMod) SetWordFilter(words []string) {
	am.Mu.Lock()
	defer am.Mu.Unlock()
	am.WordFilter = words
	am.wordFilterLower = make([]string, len(words))
	for i, w := range words {
		am.wordFilterLower[i] = strings.ToLower(w)
	}
}

// CheckContent kontroluje zprávu proti word filtru.
// Vrací (blocked, matchedWord).
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

// TrackMessage zaznamená zprávu uživatele a vrátí true pokud je spam.
func (am *AutoMod) TrackMessage(userID string) bool {
	am.Mu.Lock()
	defer am.Mu.Unlock()

	now := time.Now()
	window := time.Duration(am.SpamWindowSeconds) * time.Second
	cutoff := now.Add(-window)

	// Filtrovat staré záznamy
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

// cleanup každou minutu maže staré záznamy ze spam trackeru.
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

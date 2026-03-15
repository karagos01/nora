package ui

import (
	"log"
	"sync"
	"time"
)

// notifCooldown je minimální interval mezi notifikacemi ze stejného zdroje.
const notifCooldown = 5 * time.Second

// inputIdleThreshold — pokud proběhl uživatelský input v posledních 2 sekundách,
// okno se považuje za aktivní a notifikace se neposílají.
const inputIdleThreshold = 2 * time.Second

// NotifManager spravuje desktop notifikace s cooldownem a focus trackingem.
type NotifManager struct {
	app     *App
	enabled bool

	// Cooldown per source (zamezit spamu)
	lastNotif map[string]time.Time
	mu        sync.Mutex

	// Focus tracking — čas posledního uživatelského inputu (klik, klávesa).
	// Gio nemá spolehlivý "window focus" event, takže sledujeme input aktivitu.
	lastInputTime time.Time
}

// NewNotifManager vytvoří nový NotifManager.
func NewNotifManager(a *App) *NotifManager {
	return &NotifManager{
		app:       a,
		enabled:   true,
		lastNotif: make(map[string]time.Time),
	}
}

// RecordInput zaznamená, že proběhl uživatelský input (klik, klávesa, scroll).
// Volat z main event loop při zpracování FrameEvent.
func (nm *NotifManager) RecordInput() {
	nm.mu.Lock()
	nm.lastInputTime = time.Now()
	nm.mu.Unlock()
}

// isWindowActive vrátí true pokud proběhl uživatelský input v posledních 2 sekundách.
func (nm *NotifManager) isWindowActive() bool {
	nm.mu.Lock()
	active := time.Since(nm.lastInputTime) < inputIdleThreshold
	nm.mu.Unlock()
	return active
}

// shouldNotify ověří: enabled + okno není aktivní + cooldown (5s per source).
func (nm *NotifManager) shouldNotify(source string) bool {
	if !nm.enabled {
		return false
	}
	if nm.isWindowActive() {
		return false
	}

	nm.mu.Lock()
	defer nm.mu.Unlock()

	if last, ok := nm.lastNotif[source]; ok {
		if time.Since(last) < notifCooldown {
			return false
		}
	}
	nm.lastNotif[source] = time.Now()
	return true
}

// NotifyMessage odešle desktop notifikaci pro novou zprávu (channel/DM/group).
func (nm *NotifManager) NotifyMessage(serverName, channelOrUser, content, senderName string) {
	source := serverName + ":" + channelOrUser
	if !nm.shouldNotify(source) {
		return
	}

	title := channelOrUser
	if serverName != "" {
		title = serverName + " - " + channelOrUser
	}
	body := senderName + ": " + truncateMsg(content, 150)

	go func() {
		if err := sendDesktopNotification(title, body, ""); err != nil {
			log.Printf("desktop notif error: %v", err)
		}
	}()
}

// NotifyFriendRequest odešle desktop notifikaci pro friend request.
func (nm *NotifManager) NotifyFriendRequest(serverName, fromUser string) {
	source := "friend:" + fromUser
	if !nm.shouldNotify(source) {
		return
	}

	title := "Friend Request"
	if serverName != "" {
		title = serverName + " - Friend Request"
	}
	body := fromUser + " sent you a friend request"

	go func() {
		if err := sendDesktopNotification(title, body, ""); err != nil {
			log.Printf("desktop notif error: %v", err)
		}
	}()
}

// NotifyCalendarReminder odešle desktop notifikaci pro calendar reminder.
func (nm *NotifManager) NotifyCalendarReminder(serverName, eventTitle string) {
	source := "calendar:" + eventTitle
	if !nm.shouldNotify(source) {
		return
	}

	title := "Event Reminder"
	if serverName != "" {
		title = serverName + " - Event Reminder"
	}
	body := eventTitle

	go func() {
		if err := sendDesktopNotification(title, body, ""); err != nil {
			log.Printf("desktop notif error: %v", err)
		}
	}()
}

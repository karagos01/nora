package ui

import (
	"log"
	"sync"
	"time"
)

// notifCooldown is the minimum interval between notifications from the same source.
const notifCooldown = 5 * time.Second

// inputIdleThreshold — if user input occurred in the last 2 seconds,
// the window is considered active and notifications are not sent.
const inputIdleThreshold = 2 * time.Second

// NotifManager spravuje desktop notifikace s cooldownem a focus trackingem.
type NotifManager struct {
	app     *App
	enabled bool

	// Cooldown per source (zamezit spamu)
	lastNotif map[string]time.Time
	mu        sync.Mutex

	// Focus tracking — time of last user input (click, keystroke).
	// Gio does not have a reliable "window focus" event, so we track input activity.
	lastInputTime time.Time
}

// NewNotifManager creates a new NotifManager.
func NewNotifManager(a *App) *NotifManager {
	return &NotifManager{
		app:       a,
		enabled:   true,
		lastNotif: make(map[string]time.Time),
	}
}

// RecordInput records that user input occurred (click, keystroke, scroll).
// Call from the main event loop when processing FrameEvent.
func (nm *NotifManager) RecordInput() {
	nm.mu.Lock()
	nm.lastInputTime = time.Now()
	nm.mu.Unlock()
}

// isWindowActive returns true if user input occurred in the last 2 seconds.
func (nm *NotifManager) isWindowActive() bool {
	nm.mu.Lock()
	active := time.Since(nm.lastInputTime) < inputIdleThreshold
	nm.mu.Unlock()
	return active
}

// shouldNotify verifies: enabled + window not active + cooldown (5s per source).
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

// NotifyMessage sends a desktop notification for a new message (channel/DM/group).
func (nm *NotifManager) NotifyMessage(serverName, channelOrUser, content, senderName string) {
	source := serverName + ":" + channelOrUser
	if !nm.shouldNotify(source) {
		return
	}

	title := channelOrUser
	if serverName != "" {
		title = serverName + " - " + channelOrUser
	}
	body := content
	if senderName != "" {
		body = senderName + ": " + truncateMsg(content, 150)
	}

	go func() {
		if err := sendDesktopNotification(title, body, ""); err != nil {
			log.Printf("desktop notif error: %v", err)
		}
	}()
}

// NotifyForced sends a desktop notification bypassing the window-active check.
// Used for login-time pending message notifications where the window is always active.
func (nm *NotifManager) NotifyForced(title, body string) {
	if !nm.enabled {
		return
	}
	go func() {
		if err := sendDesktopNotification(title, body, ""); err != nil {
			log.Printf("desktop notif error: %v", err)
		}
	}()
}

// NotifyFriendRequest sends a desktop notification for a friend request.
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

// NotifyCalendarReminder sends a desktop notification for a calendar reminder.
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

package ui

import (
	"nora-client/api"
	"nora-client/store"
)

// ResolveUserName returns the display name for an api.User.
// Priority: custom_name > auto_name (no collision) > auto_name#discriminant > key.
func (a *App) ResolveUserName(u *api.User) string {
	if u == nil {
		return "?"
	}

	pubKey := u.PublicKey
	if pubKey == "" || a.Contacts == nil {
		return DisplayNameOf(u)
	}

	ct := a.Contacts.GetContact(pubKey)
	if ct == nil {
		return DisplayNameOf(u)
	}

	// 1. Custom name (nickname)
	if ct.CustomName != "" {
		return ct.CustomName
	}

	// 2. Auto name — check for collision
	name := ct.AutoName
	if name == "" {
		name = DisplayNameOf(u)
	}

	if a.Contacts.HasNameCollision(name) {
		return name + "#" + ct.Discriminant
	}

	return name
}

// ResolveNameByKey returns the display name for a public key (without api.User).
func (a *App) ResolveNameByKey(publicKey string) string {
	if publicKey == "" || a.Contacts == nil {
		return "?"
	}

	ct := a.Contacts.GetContact(publicKey)
	if ct == nil {
		return ShortenKey(publicKey)
	}

	if ct.CustomName != "" {
		return ct.CustomName
	}

	name := ct.AutoName
	if name == "" {
		return ShortenKey(publicKey)
	}

	if a.Contacts.HasNameCollision(name) {
		return name + "#" + ct.Discriminant
	}

	return name
}

// ResolveNameByID looks up a user in conn.Users and then resolves the name via contacts.
// The caller MUST hold a.mu (at least RLock).
func (a *App) ResolveNameByID(conn *ServerConnection, userID string) string {
	if conn == nil {
		return "?"
	}
	for i := range conn.Users {
		if conn.Users[i].ID == userID {
			return a.ResolveUserName(&conn.Users[i])
		}
	}
	return "Unknown"
}

// ShortenKey shortens a key to the first 8 + last 4 characters.
func ShortenKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// FormatDiscriminant returns name#disc for display.
func FormatDiscriminant(name, publicKey string) string {
	return name + "#" + store.Discriminant(publicKey)
}

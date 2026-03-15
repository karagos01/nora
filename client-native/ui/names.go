package ui

import (
	"nora-client/api"
	"nora-client/store"
)

// ResolveUserName vrátí zobrazované jméno pro api.User.
// Priorita: custom_name > auto_name (bez kolize) > auto_name#discriminant > klíč.
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

	// 1. Custom name (přezdívka)
	if ct.CustomName != "" {
		return ct.CustomName
	}

	// 2. Auto name — zkontrolovat kolizi
	name := ct.AutoName
	if name == "" {
		name = DisplayNameOf(u)
	}

	if a.Contacts.HasNameCollision(name) {
		return name + "#" + ct.Discriminant
	}

	return name
}

// ResolveNameByKey vrátí zobrazované jméno pro public key (bez api.User).
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

// ResolveNameByID hledá uživatele v conn.Users a pak řeší jméno přes kontakty.
// Volající MUSÍ držet a.mu (alespoň RLock).
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

// ShortenKey zkrátí klíč na prvních 8 + posledních 4 znaků.
func ShortenKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// FormatDiscriminant vrátí jméno#disc pro zobrazení.
func FormatDiscriminant(name, publicKey string) string {
	return name + "#" + store.Discriminant(publicKey)
}

package store

import (
	"database/sql"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Contact struct {
	PublicKey       string
	CustomName      string // user-set nickname (empty = not set)
	AutoName        string // name from first encounter
	Discriminant    string // first 4 hex chars of public key
	FirstSeenServer string
	FirstSeenAt     time.Time
	Notes           string
	Blocked         bool // cross-server block (client-side blocklist)
	IsFriend        bool // cross-server friend flag
}

type ServerName struct {
	PublicKey    string
	ServerURL   string
	ServerName  string
	DisplayName string
	UpdatedAt   time.Time
}

type ContactsDB struct {
	db *sql.DB
}

// OpenContacts opens the SQLite contacts DB for the given identity.
func OpenContacts(publicKey string) (*ContactsDB, error) {
	if err := os.MkdirAll(noraDir(), 0700); err != nil {
		return nil, err
	}
	// Decode hex key for prefix
	prefix := publicKey
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	dbPath := filepath.Join(noraDir(), "contacts_"+prefix+".db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// WAL mode + schema
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS contacts (
			public_key TEXT PRIMARY KEY,
			custom_name TEXT DEFAULT '',
			auto_name TEXT NOT NULL DEFAULT '',
			discriminant TEXT NOT NULL DEFAULT '',
			first_seen_server TEXT DEFAULT '',
			first_seen_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			notes TEXT DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS contact_server_names (
			public_key TEXT NOT NULL,
			server_url TEXT NOT NULL,
			server_name TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (public_key, server_url),
			FOREIGN KEY (public_key) REFERENCES contacts(public_key)
		);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Migration: add blocked + is_friend columns
	db.Exec("ALTER TABLE contacts ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE contacts ADD COLUMN is_friend INTEGER NOT NULL DEFAULT 0")

	return &ContactsDB{db: db}, nil
}

func (c *ContactsDB) Close() {
	if c != nil && c.db != nil {
		c.db.Close()
	}
}

// Discriminant returns the first 4 hex characters of a public key.
func Discriminant(publicKey string) string {
	// Public key is hex-encoded (64 chars for ed25519)
	clean := strings.TrimSpace(publicKey)
	if len(clean) >= 4 {
		// Verify they are valid hex characters
		if _, err := hex.DecodeString(clean[:4]); err == nil {
			return strings.ToLower(clean[:4])
		}
	}
	return "0000"
}

// EnsureContact creates a contact if it doesn't exist, updates server name.
func (c *ContactsDB) EnsureContact(pubKey, name, serverURL, serverName string) {
	if c == nil || c.db == nil || pubKey == "" {
		return
	}
	disc := Discriminant(pubKey)

	_, err := c.db.Exec(`
		INSERT INTO contacts (public_key, auto_name, discriminant, first_seen_server)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(public_key) DO NOTHING
	`, pubKey, name, disc, serverURL)
	if err != nil {
		log.Printf("contacts: EnsureContact: %v", err)
	}

	if serverURL != "" {
		c.UpdateServerName(pubKey, serverURL, serverName, name)
	}
}

// UpdateServerName updates the user's name on the given server.
func (c *ContactsDB) UpdateServerName(pubKey, serverURL, srvName, displayName string) {
	if c == nil || c.db == nil || pubKey == "" {
		return
	}
	_, err := c.db.Exec(`
		INSERT INTO contact_server_names (public_key, server_url, server_name, display_name, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(public_key, server_url) DO UPDATE SET
			server_name = excluded.server_name,
			display_name = excluded.display_name,
			updated_at = excluded.updated_at
	`, pubKey, serverURL, srvName, displayName, time.Now().UTC())
	if err != nil {
		log.Printf("contacts: UpdateServerName: %v", err)
	}
}

// GetContact returns the contact for the given public key.
func (c *ContactsDB) GetContact(pubKey string) *Contact {
	if c == nil || c.db == nil {
		return nil
	}
	row := c.db.QueryRow(`
		SELECT public_key, custom_name, auto_name, discriminant, first_seen_server, first_seen_at, notes, blocked, is_friend
		FROM contacts WHERE public_key = ?
	`, pubKey)

	var ct Contact
	var firstSeen string
	var blocked, isFriend int
	err := row.Scan(&ct.PublicKey, &ct.CustomName, &ct.AutoName, &ct.Discriminant, &ct.FirstSeenServer, &firstSeen, &ct.Notes, &blocked, &isFriend)
	if err != nil {
		return nil
	}
	ct.FirstSeenAt, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
	ct.Blocked = blocked != 0
	ct.IsFriend = isFriend != 0
	return &ct
}

// SetCustomName sets the user's custom name (nickname).
func (c *ContactsDB) SetCustomName(pubKey, name string) error {
	if c == nil || c.db == nil {
		return nil
	}
	_, err := c.db.Exec("UPDATE contacts SET custom_name = ? WHERE public_key = ?", name, pubKey)
	return err
}

// SetNotes sets the note for a contact.
func (c *ContactsDB) SetNotes(pubKey, notes string) error {
	if c == nil || c.db == nil {
		return nil
	}
	_, err := c.db.Exec("UPDATE contacts SET notes = ? WHERE public_key = ?", notes, pubKey)
	return err
}

// GetServerNames returns all names of the user across different servers.
func (c *ContactsDB) GetServerNames(pubKey string) []ServerName {
	if c == nil || c.db == nil {
		return nil
	}
	rows, err := c.db.Query(`
		SELECT public_key, server_url, server_name, display_name, updated_at
		FROM contact_server_names WHERE public_key = ?
	`, pubKey)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []ServerName
	for rows.Next() {
		var sn ServerName
		var updatedAt string
		if err := rows.Scan(&sn.PublicKey, &sn.ServerURL, &sn.ServerName, &sn.DisplayName, &updatedAt); err == nil {
			sn.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
			result = append(result, sn)
		}
	}
	return result
}

// AllContacts returns all contacts.
func (c *ContactsDB) AllContacts() []Contact {
	if c == nil || c.db == nil {
		return nil
	}
	rows, err := c.db.Query(`
		SELECT public_key, custom_name, auto_name, discriminant, first_seen_server, first_seen_at, notes, blocked, is_friend
		FROM contacts ORDER BY auto_name
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []Contact
	for rows.Next() {
		var ct Contact
		var firstSeen string
		var blocked, isFriend int
		if err := rows.Scan(&ct.PublicKey, &ct.CustomName, &ct.AutoName, &ct.Discriminant, &ct.FirstSeenServer, &firstSeen, &ct.Notes, &blocked, &isFriend); err == nil {
			ct.FirstSeenAt, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
			ct.Blocked = blocked != 0
			ct.IsFriend = isFriend != 0
			result = append(result, ct)
		}
	}
	return result
}

// Search searches contacts by name (LIKE search).
func (c *ContactsDB) Search(query string) []Contact {
	if c == nil || c.db == nil || query == "" {
		return nil
	}
	like := "%" + query + "%"
	rows, err := c.db.Query(`
		SELECT public_key, custom_name, auto_name, discriminant, first_seen_server, first_seen_at, notes, blocked, is_friend
		FROM contacts
		WHERE custom_name LIKE ? OR auto_name LIKE ? OR public_key LIKE ? OR discriminant LIKE ?
		ORDER BY auto_name LIMIT 50
	`, like, like, like, like)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []Contact
	for rows.Next() {
		var ct Contact
		var firstSeen string
		var blocked, isFriend int
		if err := rows.Scan(&ct.PublicKey, &ct.CustomName, &ct.AutoName, &ct.Discriminant, &ct.FirstSeenServer, &firstSeen, &ct.Notes, &blocked, &isFriend); err == nil {
			ct.FirstSeenAt, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
			ct.Blocked = blocked != 0
			ct.IsFriend = isFriend != 0
			result = append(result, ct)
		}
	}
	return result
}

// SetBlocked sets a cross-server block for a contact identified by public key.
func (c *ContactsDB) SetBlocked(pubKey string, blocked bool) error {
	if c == nil || c.db == nil || pubKey == "" {
		return nil
	}
	val := 0
	if blocked {
		val = 1
	}
	// If the contact doesn't exist, create it
	c.EnsureContact(pubKey, "", "", "")
	_, err := c.db.Exec("UPDATE contacts SET blocked = ? WHERE public_key = ?", val, pubKey)
	return err
}

// IsBlocked returns true if the contact is blocked (cross-server).
func (c *ContactsDB) IsBlocked(pubKey string) bool {
	if c == nil || c.db == nil || pubKey == "" {
		return false
	}
	var blocked int
	err := c.db.QueryRow("SELECT blocked FROM contacts WHERE public_key = ?", pubKey).Scan(&blocked)
	if err != nil {
		return false
	}
	return blocked != 0
}

// SetFriend sets the cross-server friend flag for a contact.
func (c *ContactsDB) SetFriend(pubKey string, isFriend bool) {
	if c == nil || c.db == nil || pubKey == "" {
		return
	}
	val := 0
	if isFriend {
		val = 1
	}
	c.EnsureContact(pubKey, "", "", "")
	c.db.Exec("UPDATE contacts SET is_friend = ? WHERE public_key = ?", val, pubKey)
}

// GetFriends returns all contacts marked as friends.
func (c *ContactsDB) GetFriends() []Contact {
	if c == nil || c.db == nil {
		return nil
	}
	rows, err := c.db.Query(`
		SELECT public_key, custom_name, auto_name, discriminant, first_seen_server, first_seen_at, notes, blocked, is_friend
		FROM contacts WHERE is_friend = 1 ORDER BY auto_name
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []Contact
	for rows.Next() {
		var ct Contact
		var firstSeen string
		var blocked, isFriend int
		if err := rows.Scan(&ct.PublicKey, &ct.CustomName, &ct.AutoName, &ct.Discriminant, &ct.FirstSeenServer, &firstSeen, &ct.Notes, &blocked, &isFriend); err == nil {
			ct.FirstSeenAt, _ = time.Parse("2006-01-02 15:04:05", firstSeen)
			ct.Blocked = blocked != 0
			ct.IsFriend = isFriend != 0
			result = append(result, ct)
		}
	}
	return result
}

// HasNameCollision checks whether two or more contacts share the same auto_name.
func (c *ContactsDB) HasNameCollision(name string) bool {
	if c == nil || c.db == nil || name == "" {
		return false
	}
	var count int
	c.db.QueryRow("SELECT COUNT(*) FROM contacts WHERE auto_name = ?", name).Scan(&count)
	return count > 1
}

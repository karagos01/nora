package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", p, err)
		}
	}

	return &DB{db}, nil
}

func (db *DB) Migrate() error {
	// Migrace: drop staré plaintext DM tabulky
	db.Exec("DROP TABLE IF EXISTS dm_messages")
	db.Exec("DROP INDEX IF EXISTS idx_dm_messages_conv_created")

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migrace: přidat type sloupec do channels
	db.Exec("ALTER TABLE channels ADD COLUMN type TEXT NOT NULL DEFAULT 'text'")

	// Migrace: přidat category_id sloupec do channels
	db.Exec("ALTER TABLE channels ADD COLUMN category_id TEXT REFERENCES channel_categories(id) ON DELETE SET NULL")

	// Migrace: přidat avatar_url sloupec do users
	db.Exec("ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''")

	// Migrace: přidat reply_to_id sloupec do messages
	db.Exec("ALTER TABLE messages ADD COLUMN reply_to_id TEXT REFERENCES messages(id) ON DELETE SET NULL")

	// Migrace: přidat last_ip a invited_by sloupce do users
	db.Exec("ALTER TABLE users ADD COLUMN last_ip TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE users ADD COLUMN invited_by TEXT NOT NULL DEFAULT ''")

	// Migrace: přidat pin sloupce do messages
	db.Exec("ALTER TABLE messages ADD COLUMN is_pinned INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE messages ADD COLUMN pinned_by TEXT")

	// Migrace: přidat hidden sloupce do messages
	db.Exec("ALTER TABLE messages ADD COLUMN is_hidden INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE messages ADD COLUMN hidden_by TEXT")
	db.Exec("ALTER TABLE messages ADD COLUMN hidden_by_position INTEGER NOT NULL DEFAULT 0")

	// Migrace: multi LAN party — odebrat per-party WG sloupce (nyní server-level)
	db.Exec("ALTER TABLE lan_parties DROP COLUMN server_private_key")
	db.Exec("ALTER TABLE lan_parties DROP COLUMN server_public_key")
	db.Exec("ALTER TABLE lan_parties DROP COLUMN subnet")
	db.Exec("ALTER TABLE lan_parties DROP COLUMN next_ip")

	// Migrace: share limity
	db.Exec("ALTER TABLE shared_directories ADD COLUMN max_file_size_mb INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE shared_directories ADD COLUMN storage_quota_mb INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE shared_directories ADD COLUMN max_files_count INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE shared_directories ADD COLUMN expires_at DATETIME")

	// Migrace: auth_challenges — pending registration data
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN username TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN invited_by TEXT NOT NULL DEFAULT ''")

	// Migrace: zjednodušení game_servers — drop nepotřebné sloupce (config-on-disk)
	db.Exec("ALTER TABLE game_servers DROP COLUMN def_id")
	db.Exec("ALTER TABLE game_servers DROP COLUMN env")
	db.Exec("ALTER TABLE game_servers DROP COLUMN host_port")
	db.Exec("ALTER TABLE game_servers DROP COLUMN memory_mb")

	// Migrace: content hash pro deduplikaci uploadů
	db.Exec("ALTER TABLE attachments ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''")

	// Migrace: lobby voice kanály — parent_id pro sub-kanály
	db.Exec("ALTER TABLE channels ADD COLUMN parent_id TEXT REFERENCES channels(id) ON DELETE CASCADE")

	// Migrace: slow mode — cooldown na zprávy per kanál
	db.Exec("ALTER TABLE channels ADD COLUMN slow_mode_seconds INTEGER NOT NULL DEFAULT 0")

	// Migrace: custom status — user status + text
	db.Exec("ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE users ADD COLUMN status_text TEXT NOT NULL DEFAULT ''")

	// Migrace: ban expirace
	db.Exec("ALTER TABLE bans ADD COLUMN expires_at DATETIME")

	// Migrace: auth challenges — device info
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN device_id TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN hardware_hash TEXT NOT NULL DEFAULT ''")

	// Migrace: hierarchické kategorie — parent_id pro sub-kategorie
	db.Exec("ALTER TABLE channel_categories ADD COLUMN parent_id TEXT REFERENCES channel_categories(id) ON DELETE CASCADE")

	// Migrace: reply_to_id pro DM pending zprávy
	db.Exec("ALTER TABLE dm_pending ADD COLUMN reply_to_id TEXT NOT NULL DEFAULT ''")

	// Migrace: barva role
	db.Exec("ALTER TABLE roles ADD COLUMN color TEXT NOT NULL DEFAULT ''")

	// Migrace: game server access control (room pattern)
	db.Exec("ALTER TABLE game_servers ADD COLUMN access_mode TEXT NOT NULL DEFAULT 'open'")
	db.Exec(`CREATE TABLE IF NOT EXISTS game_server_members (
		game_server_id TEXT NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at DATETIME DEFAULT (datetime('now')),
		PRIMARY KEY(game_server_id, user_id)
	)`)

	// Migrace: personální WireGuard tunely
	db.Exec(`CREATE TABLE IF NOT EXISTS tunnels (
		id TEXT PRIMARY KEY,
		creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		status TEXT NOT NULL DEFAULT 'pending',
		creator_wg_pubkey TEXT NOT NULL DEFAULT '',
		target_wg_pubkey TEXT NOT NULL DEFAULT '',
		creator_ip TEXT NOT NULL DEFAULT '',
		target_ip TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT (datetime('now'))
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tunnels_creator ON tunnels(creator_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tunnels_target ON tunnels(target_id)`)

	// Migrace: poll expiration
	db.Exec("ALTER TABLE polls ADD COLUMN expires_at DATETIME")

	// Migrace: recurring events — recurrence_rule sloupec
	db.Exec("ALTER TABLE events ADD COLUMN recurrence_rule TEXT NOT NULL DEFAULT ''")

	// Migrace: historie editací zpráv
	db.Exec(`CREATE TABLE IF NOT EXISTS message_edits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		old_content TEXT NOT NULL,
		edited_at DATETIME DEFAULT (datetime('now')),
		edited_by TEXT NOT NULL REFERENCES users(id)
	)`)

	return nil
}

func (db *DB) SeedDefaults() error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM roles").Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		_, err = db.Exec(`INSERT INTO roles (id, name, permissions, position) VALUES
			('everyone', 'everyone', 259, 999)`)
		if err != nil {
			return err
		}
	}

	err = db.QueryRow("SELECT COUNT(*) FROM channels").Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		_, err = db.Exec(`INSERT INTO channels (id, name, topic, position) VALUES
			(?, 'general', 'General discussion', 0)`,
			generateID())
		if err != nil {
			return err
		}
	}

	return nil
}

// Reopen zavře stávající spojení a otevře novou databázi.
func (db *DB) Reopen(path string) (*DB, error) {
	db.Close()
	newDB, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := newDB.Migrate(); err != nil {
		newDB.Close()
		return nil, err
	}
	return newDB, nil
}

func generateID() string {
	id, _ := newUUID()
	return id
}

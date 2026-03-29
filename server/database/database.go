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
	// Migration: drop old plaintext DM tables
	db.Exec("DROP TABLE IF EXISTS dm_messages")
	db.Exec("DROP INDEX IF EXISTS idx_dm_messages_conv_created")

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add type column to channels
	db.Exec("ALTER TABLE channels ADD COLUMN type TEXT NOT NULL DEFAULT 'text'")

	// Migration: add category_id column to channels
	db.Exec("ALTER TABLE channels ADD COLUMN category_id TEXT REFERENCES channel_categories(id) ON DELETE SET NULL")

	// Migration: add avatar_url column to users
	db.Exec("ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT ''")

	// Migration: add reply_to_id column to messages
	db.Exec("ALTER TABLE messages ADD COLUMN reply_to_id TEXT REFERENCES messages(id) ON DELETE SET NULL")

	// Migration: add last_ip and invited_by columns to users
	db.Exec("ALTER TABLE users ADD COLUMN last_ip TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE users ADD COLUMN invited_by TEXT NOT NULL DEFAULT ''")

	// Migration: add pin columns to messages
	db.Exec("ALTER TABLE messages ADD COLUMN is_pinned INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE messages ADD COLUMN pinned_by TEXT")

	// Migration: add hidden columns to messages
	db.Exec("ALTER TABLE messages ADD COLUMN is_hidden INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE messages ADD COLUMN hidden_by TEXT")
	db.Exec("ALTER TABLE messages ADD COLUMN hidden_by_position INTEGER NOT NULL DEFAULT 0")

	// Migration: multi LAN party — remove per-party WG columns (now server-level)
	db.Exec("ALTER TABLE lan_parties DROP COLUMN server_private_key")
	db.Exec("ALTER TABLE lan_parties DROP COLUMN server_public_key")
	db.Exec("ALTER TABLE lan_parties DROP COLUMN subnet")
	db.Exec("ALTER TABLE lan_parties DROP COLUMN next_ip")

	// Migration: share limits
	db.Exec("ALTER TABLE shared_directories ADD COLUMN max_file_size_mb INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE shared_directories ADD COLUMN storage_quota_mb INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE shared_directories ADD COLUMN max_files_count INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE shared_directories ADD COLUMN expires_at DATETIME")

	// Migration: auth_challenges — pending registration data
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN username TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN invited_by TEXT NOT NULL DEFAULT ''")

	// Migration: simplify game_servers — drop unnecessary columns (config-on-disk)
	db.Exec("ALTER TABLE game_servers DROP COLUMN def_id")
	db.Exec("ALTER TABLE game_servers DROP COLUMN env")
	db.Exec("ALTER TABLE game_servers DROP COLUMN host_port")
	db.Exec("ALTER TABLE game_servers DROP COLUMN memory_mb")

	// Migration: content hash for upload deduplication
	db.Exec("ALTER TABLE attachments ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''")

	// Migration: lobby voice channels — parent_id for sub-channels
	db.Exec("ALTER TABLE channels ADD COLUMN parent_id TEXT REFERENCES channels(id) ON DELETE CASCADE")

	// Migration: slow mode — message cooldown per channel
	db.Exec("ALTER TABLE channels ADD COLUMN slow_mode_seconds INTEGER NOT NULL DEFAULT 0")

	// Migration: custom status — user status + text
	db.Exec("ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE users ADD COLUMN status_text TEXT NOT NULL DEFAULT ''")

	// Migration: ban expiration
	db.Exec("ALTER TABLE bans ADD COLUMN expires_at DATETIME")

	// Migration: auth challenges — device info
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN device_id TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE auth_challenges ADD COLUMN hardware_hash TEXT NOT NULL DEFAULT ''")

	// Migration: hierarchical categories — parent_id for sub-categories
	db.Exec("ALTER TABLE channel_categories ADD COLUMN parent_id TEXT REFERENCES channel_categories(id) ON DELETE CASCADE")

	// Migration: reply_to_id for DM pending messages
	db.Exec("ALTER TABLE dm_pending ADD COLUMN reply_to_id TEXT NOT NULL DEFAULT ''")

	// Migration: role color
	db.Exec("ALTER TABLE roles ADD COLUMN color TEXT NOT NULL DEFAULT ''")

	// Migration: game server access control (room pattern)
	db.Exec("ALTER TABLE game_servers ADD COLUMN access_mode TEXT NOT NULL DEFAULT 'open'")
	db.Exec(`CREATE TABLE IF NOT EXISTS game_server_members (
		game_server_id TEXT NOT NULL REFERENCES game_servers(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at DATETIME DEFAULT (datetime('now')),
		PRIMARY KEY(game_server_id, user_id)
	)`)

	// Migration: personal WireGuard tunnels
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

	// Migration: poll expiration
	db.Exec("ALTER TABLE polls ADD COLUMN expires_at DATETIME")

	// Migration: recurring events — recurrence_rule column
	db.Exec("ALTER TABLE events ADD COLUMN recurrence_rule TEXT NOT NULL DEFAULT ''")

	// Migration: message edit history
	db.Exec(`CREATE TABLE IF NOT EXISTS message_edits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		old_content TEXT NOT NULL,
		edited_at DATETIME DEFAULT (datetime('now')),
		edited_by TEXT NOT NULL REFERENCES users(id)
	)`)

	// Migration: FTS5 full-text search for messages
	db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content, content='messages', content_rowid='rowid')`)
	// Triggers to keep FTS index in sync
	db.Exec(`CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE OF content ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
		INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
	END`)
	// Backfill existing messages into FTS index (no-op if already populated)
	db.Exec(`INSERT OR IGNORE INTO messages_fts(rowid, content) SELECT rowid, content FROM messages`)

	// Migration: LFG (Looking For Group) listings
	db.Exec(`CREATE TABLE IF NOT EXISTS lfg_listings (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		game_name TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		expires_at DATETIME NOT NULL
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_lfg_channel ON lfg_listings(channel_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_lfg_expires ON lfg_listings(expires_at)`)

	// Migration: LFG max_players + participants + applications + group
	db.Exec("ALTER TABLE lfg_listings ADD COLUMN max_players INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE lfg_listings ADD COLUMN group_id TEXT NOT NULL DEFAULT ''")
	db.Exec(`CREATE TABLE IF NOT EXISTS lfg_participants (
		listing_id TEXT NOT NULL REFERENCES lfg_listings(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		joined_at DATETIME NOT NULL DEFAULT (datetime('now')),
		PRIMARY KEY (listing_id, user_id)
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS lfg_applications (
		listing_id TEXT NOT NULL REFERENCES lfg_listings(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		message TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		PRIMARY KEY (listing_id, user_id)
	)`)

	// Migration: user reports
	db.Exec(`CREATE TABLE IF NOT EXISTS reports (
		id TEXT PRIMARY KEY,
		reporter_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_message_id TEXT DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		reviewed_by TEXT DEFAULT '',
		reviewed_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_reports_target ON reports(target_user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status)`)

	// Migration: channel read state (per-user last read message tracking)
	db.Exec(`CREATE TABLE IF NOT EXISTS channel_read_state (
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		last_read_id TEXT NOT NULL DEFAULT '',
		last_read_at DATETIME NOT NULL DEFAULT (datetime('now')),
		PRIMARY KEY (user_id, channel_id)
	)`)

	// Migration: idempotency key for message deduplication
	db.Exec("ALTER TABLE messages ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT ''")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_idempotency ON messages(user_id, idempotency_key) WHERE idempotency_key != ''")

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

// Reopen closes the existing connection and opens a new database.
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

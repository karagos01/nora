package database

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL DEFAULT '',
	public_key TEXT NOT NULL UNIQUE,
	is_owner INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS auth_challenges (
	public_key TEXT PRIMARY KEY,
	nonce TEXT NOT NULL,
	expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS roles (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	permissions INTEGER NOT NULL DEFAULT 0,
	position INTEGER NOT NULL DEFAULT 0,
	color TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS user_roles (
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS channel_categories (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	color TEXT NOT NULL DEFAULT '#555555',
	position INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS channels (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	topic TEXT NOT NULL DEFAULT '',
	position INTEGER NOT NULL DEFAULT 0,
	category_id TEXT REFERENCES channel_categories(id) ON DELETE SET NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	content TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at);

CREATE TABLE IF NOT EXISTS attachments (
	id TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	filepath TEXT NOT NULL,
	filename TEXT NOT NULL,
	mime_type TEXT NOT NULL,
	size INTEGER NOT NULL,
	content_hash TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS invites (
	id TEXT PRIMARY KEY,
	code TEXT NOT NULL UNIQUE,
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	max_uses INTEGER NOT NULL DEFAULT 0,
	uses INTEGER NOT NULL DEFAULT 0,
	expires_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS bans (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	reason TEXT NOT NULL DEFAULT '',
	banned_by TEXT NOT NULL REFERENCES users(id),
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_bans_user ON bans(user_id);

CREATE TABLE IF NOT EXISTS timeouts (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	reason TEXT NOT NULL DEFAULT '',
	issued_by TEXT NOT NULL REFERENCES users(id),
	expires_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_timeouts_user ON timeouts(user_id);

CREATE TABLE IF NOT EXISTS refresh_tokens (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	expires_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);

CREATE TABLE IF NOT EXISTS dm_conversations (
	id TEXT PRIMARY KEY,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS dm_participants (
	conversation_id TEXT NOT NULL REFERENCES dm_conversations(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	public_key TEXT NOT NULL,
	PRIMARY KEY (conversation_id, user_id)
);

CREATE TABLE IF NOT EXISTS dm_pending (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES dm_conversations(id) ON DELETE CASCADE,
	sender_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	encrypted_content TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_dm_pending_conv ON dm_pending(conversation_id);

CREATE TABLE IF NOT EXISTS friends (
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	friend_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (user_id, friend_id)
);

CREATE TABLE IF NOT EXISTS server_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS friend_requests (
	id TEXT PRIMARY KEY,
	from_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	to_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_friend_requests_pair ON friend_requests(from_user_id, to_user_id);

CREATE TABLE IF NOT EXISTS blocks (
	blocker_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (blocker_id, blocked_id)
);

CREATE TABLE IF NOT EXISTS groups (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS group_members (
	group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	public_key TEXT NOT NULL,
	joined_at DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (group_id, user_id)
);

CREATE TABLE IF NOT EXISTS group_invites (
	id TEXT PRIMARY KEY,
	group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	code TEXT NOT NULL UNIQUE,
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	max_uses INTEGER NOT NULL DEFAULT 0,
	uses INTEGER NOT NULL DEFAULT 0,
	expires_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS emojis (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	filepath TEXT NOT NULL,
	mime_type TEXT NOT NULL,
	size INTEGER NOT NULL,
	uploader_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS banned_ips (
	ip TEXT PRIMARY KEY,
	reason TEXT NOT NULL DEFAULT '',
	banned_by TEXT NOT NULL,
	related_user_id TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS reactions (
	message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	emoji TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (message_id, user_id, emoji)
);
CREATE INDEX IF NOT EXISTS idx_reactions_message ON reactions(message_id);

CREATE TABLE IF NOT EXISTS lan_parties (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS lan_party_members (
	id TEXT PRIMARY KEY,
	party_id TEXT NOT NULL REFERENCES lan_parties(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	public_key TEXT NOT NULL,
	assigned_ip TEXT NOT NULL,
	joined_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(party_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_lan_party_members_party ON lan_party_members(party_id);

CREATE TABLE IF NOT EXISTS webhooks (
	id TEXT PRIMARY KEY,
	channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	token TEXT NOT NULL UNIQUE,
	avatar_url TEXT NOT NULL DEFAULT '',
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS storage_folders (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	parent_id TEXT REFERENCES storage_folders(id) ON DELETE CASCADE,
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS storage_files (
	id TEXT PRIMARY KEY,
	folder_id TEXT REFERENCES storage_folders(id) ON DELETE SET NULL,
	name TEXT NOT NULL,
	filepath TEXT NOT NULL,
	mime_type TEXT NOT NULL,
	size INTEGER NOT NULL,
	uploader_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS audit_log (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	action TEXT NOT NULL,
	target_type TEXT NOT NULL,
	target_id TEXT NOT NULL DEFAULT '',
	details TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_audit_log_user_created ON audit_log(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at);

CREATE TABLE IF NOT EXISTS shared_directories (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	path_hash TEXT NOT NULL,
	display_name TEXT NOT NULL,
	is_active INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(owner_id, path_hash)
);

CREATE TABLE IF NOT EXISTS share_permissions (
	id TEXT PRIMARY KEY,
	directory_id TEXT NOT NULL REFERENCES shared_directories(id) ON DELETE CASCADE,
	grantee_id TEXT,
	can_read INTEGER NOT NULL DEFAULT 0,
	can_write INTEGER NOT NULL DEFAULT 0,
	can_delete INTEGER NOT NULL DEFAULT 0,
	is_blocked INTEGER NOT NULL DEFAULT 0,
	granted_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(directory_id, grantee_id)
);

CREATE TABLE IF NOT EXISTS shared_file_cache (
	id TEXT PRIMARY KEY,
	directory_id TEXT NOT NULL REFERENCES shared_directories(id) ON DELETE CASCADE,
	relative_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	file_size INTEGER NOT NULL DEFAULT 0,
	is_dir INTEGER NOT NULL DEFAULT 0,
	file_hash TEXT NOT NULL DEFAULT '',
	modified_at DATETIME,
	cached_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_shared_file_cache_dir ON shared_file_cache(directory_id);
CREATE INDEX IF NOT EXISTS idx_shared_file_cache_dir_path ON shared_file_cache(directory_id, relative_path);

CREATE TABLE IF NOT EXISTS game_servers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'stopped',
	container_id TEXT NOT NULL DEFAULT '',
	creator_id TEXT NOT NULL REFERENCES users(id),
	created_at DATETIME DEFAULT (datetime('now')),
	error_msg TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS whiteboards (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE,
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS whiteboard_strokes (
	id TEXT PRIMARY KEY,
	whiteboard_id TEXT NOT NULL REFERENCES whiteboards(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	path_data TEXT NOT NULL,
	color TEXT NOT NULL DEFAULT '#ffffff',
	width INTEGER NOT NULL DEFAULT 2,
	tool TEXT NOT NULL DEFAULT 'pen',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_wb_strokes_board ON whiteboard_strokes(whiteboard_id, created_at);

CREATE TABLE IF NOT EXISTS live_whiteboards (
	channel_id TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
	starter_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS live_wb_strokes (
	id TEXT PRIMARY KEY,
	channel_id TEXT NOT NULL REFERENCES live_whiteboards(channel_id) ON DELETE CASCADE,
	user_id TEXT NOT NULL,
	path_data TEXT NOT NULL,
	color TEXT NOT NULL DEFAULT '#ffffff',
	width INTEGER NOT NULL DEFAULT 2,
	tool TEXT NOT NULL DEFAULT 'pen',
	username TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_live_wb_strokes_channel ON live_wb_strokes(channel_id, created_at);

CREATE TABLE IF NOT EXISTS link_previews (
	id TEXT PRIMARY KEY,
	message_id TEXT NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
	url TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	image_url TEXT NOT NULL DEFAULT '',
	site_name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS polls (
	id TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	question TEXT NOT NULL,
	poll_type TEXT NOT NULL DEFAULT 'simple',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_polls_message ON polls(message_id);

CREATE TABLE IF NOT EXISTS poll_options (
	id TEXT PRIMARY KEY,
	poll_id TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
	label TEXT NOT NULL,
	position INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_poll_options_poll ON poll_options(poll_id);

CREATE TABLE IF NOT EXISTS poll_votes (
	poll_id TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
	option_id TEXT NOT NULL REFERENCES poll_options(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (poll_id, option_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_poll_votes_poll ON poll_votes(poll_id);

CREATE TABLE IF NOT EXISTS swarm_seeds (
	id TEXT PRIMARY KEY,
	file_cache_id TEXT NOT NULL REFERENCES shared_file_cache(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(file_cache_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_swarm_seeds_file ON swarm_seeds(file_cache_id);

CREATE TABLE IF NOT EXISTS scheduled_messages (
	id TEXT PRIMARY KEY,
	channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	content TEXT NOT NULL,
	reply_to_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
	scheduled_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_scheduled_messages_time ON scheduled_messages(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_messages_user ON scheduled_messages(user_id);

CREATE TABLE IF NOT EXISTS device_bans (
	id TEXT PRIMARY KEY,
	device_id TEXT NOT NULL DEFAULT '',
	hardware_hash TEXT NOT NULL DEFAULT '',
	related_user_id TEXT NOT NULL DEFAULT '',
	banned_by TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	expires_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_device_bans_device ON device_bans(device_id);
CREATE INDEX IF NOT EXISTS idx_device_bans_hash ON device_bans(hardware_hash);

CREATE TABLE IF NOT EXISTS user_devices (
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	device_id TEXT NOT NULL,
	hardware_hash TEXT NOT NULL DEFAULT '',
	first_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
	last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (user_id, device_id)
);

CREATE TABLE IF NOT EXISTS invite_chain (
	user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	invited_by_id TEXT NOT NULL DEFAULT '',
	invite_code TEXT NOT NULL DEFAULT '',
	joined_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS quarantine (
	user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	started_at DATETIME NOT NULL DEFAULT (datetime('now')),
	ends_at DATETIME,
	approved_by TEXT,
	approved_at DATETIME
);

CREATE TABLE IF NOT EXISTS pending_approvals (
	user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	requested_username TEXT NOT NULL,
	invited_by_id TEXT NOT NULL DEFAULT '',
	requested_at DATETIME NOT NULL DEFAULT (datetime('now')),
	reviewed_by TEXT,
	reviewed_at DATETIME,
	status TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE IF NOT EXISTS kanban_boards (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS kanban_columns (
	id TEXT PRIMARY KEY,
	board_id TEXT NOT NULL REFERENCES kanban_boards(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	position INTEGER NOT NULL DEFAULT 0,
	color TEXT NOT NULL DEFAULT '#555555',
	created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kanban_columns_board ON kanban_columns(board_id, position);

CREATE TABLE IF NOT EXISTS kanban_cards (
	id TEXT PRIMARY KEY,
	column_id TEXT NOT NULL REFERENCES kanban_columns(id) ON DELETE CASCADE,
	board_id TEXT NOT NULL REFERENCES kanban_boards(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	position INTEGER NOT NULL DEFAULT 0,
	assigned_to TEXT REFERENCES users(id) ON DELETE SET NULL,
	created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	color TEXT NOT NULL DEFAULT '',
	due_date DATETIME,
	created_at DATETIME NOT NULL,
	updated_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_kanban_cards_column ON kanban_cards(column_id, position);

CREATE TABLE IF NOT EXISTS channel_permission_overrides (
	channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	target_type TEXT NOT NULL CHECK(target_type IN ('role', 'user')),
	target_id TEXT NOT NULL,
	allow INTEGER NOT NULL DEFAULT 0,
	deny INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(channel_id, target_type, target_id)
);

CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	creator_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	starts_at DATETIME NOT NULL,
	ends_at DATETIME,
	location TEXT NOT NULL DEFAULT '',
	color TEXT NOT NULL DEFAULT '#3498db',
	all_day INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_time ON events(starts_at);

CREATE TABLE IF NOT EXISTS event_reminders (
	id TEXT PRIMARY KEY,
	event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	remind_at DATETIME NOT NULL,
	reminded INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_event_reminders_time ON event_reminders(remind_at, reminded);
`

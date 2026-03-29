package queries

import (
	"database/sql"
	"time"
)

type ReadStateQueries struct {
	DB *sql.DB
}

// ChannelUnread represents unread count for a channel.
type ChannelUnread struct {
	ChannelID   string `json:"channel_id"`
	UnreadCount int    `json:"unread_count"`
}

// MarkRead sets the last read message ID for a user in a channel.
func (q *ReadStateQueries) MarkRead(userID, channelID, messageID string) error {
	_, err := q.DB.Exec(`
		INSERT INTO channel_read_state (user_id, channel_id, last_read_id, last_read_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, channel_id) DO UPDATE SET
			last_read_id = excluded.last_read_id,
			last_read_at = excluded.last_read_at`,
		userID, channelID, messageID, time.Now().UTC(),
	)
	return err
}

// GetUnreadCounts returns unread message counts for all channels a user has access to.
// Note: message ID comparison (m.id > rs.last_read_id) relies on UUIDv7 lexicographic ordering.
func (q *ReadStateQueries) GetUnreadCounts(userID string) ([]ChannelUnread, error) {
	rows, err := q.DB.Query(`
		SELECT c.id, COUNT(m.id) as unread
		FROM channels c
		LEFT JOIN channel_read_state rs ON rs.channel_id = c.id AND rs.user_id = ?
		LEFT JOIN messages m ON m.channel_id = c.id AND (
			rs.last_read_id IS NULL OR rs.last_read_id = '' OR m.id > rs.last_read_id
		)
		WHERE m.id IS NOT NULL
		GROUP BY c.id
		HAVING unread > 0`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ChannelUnread
	for rows.Next() {
		var cu ChannelUnread
		if err := rows.Scan(&cu.ChannelID, &cu.UnreadCount); err == nil {
			result = append(result, cu)
		}
	}
	return result, rows.Err()
}

// GetLastReadID returns the last read message ID for a specific channel.
func (q *ReadStateQueries) GetLastReadID(userID, channelID string) string {
	var lastRead string
	q.DB.QueryRow(`SELECT last_read_id FROM channel_read_state WHERE user_id = ? AND channel_id = ?`,
		userID, channelID).Scan(&lastRead)
	return lastRead
}

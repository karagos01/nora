package queries

import "database/sql"

type StorageQueries struct {
	DB *sql.DB
}

// TrimChannelHistory deletes messages exceeding keepCount in a given channel.
// Returns filepaths of attachments from deleted messages (for disk cleanup).
func (q *StorageQueries) TrimChannelHistory(channelID string, keepCount int) ([]string, error) {
	// Find filepaths of attachments for messages that will be deleted (skip pinned messages)
	rows, err := q.DB.Query(
		`SELECT a.filepath FROM attachments a
		 JOIN messages m ON m.id = a.message_id
		 WHERE m.channel_id = ? AND m.is_pinned = 0 AND m.id NOT IN (
			SELECT id FROM messages WHERE channel_id = ? ORDER BY created_at DESC LIMIT ?
		 )`,
		channelID, channelID, keepCount,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Delete messages (CASCADE deletes attachments from DB), skip pinned messages
	_, err = q.DB.Exec(
		`DELETE FROM messages WHERE channel_id = ? AND is_pinned = 0 AND id NOT IN (
			SELECT id FROM messages WHERE channel_id = ? ORDER BY created_at DESC LIMIT ?
		)`,
		channelID, channelID, keepCount,
	)
	return paths, err
}

// OldestAttachmentPaths returns filepaths of the oldest attachments (for size-based cleanup).
// Returns pairs (message_id, filepath) sorted from oldest message.
func (q *StorageQueries) OldestAttachmentPaths(limit int) ([]string, error) {
	rows, err := q.DB.Query(
		`SELECT a.filepath FROM attachments a
		 JOIN messages m ON m.id = a.message_id
		 ORDER BY m.created_at ASC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// DeleteMessagesWithAttachments deletes messages whose attachments match the given filepaths.
// Returns the number of deleted messages.
func (q *StorageQueries) DeleteMessagesWithAttachments(filepaths []string) (int64, error) {
	if len(filepaths) == 0 {
		return 0, nil
	}

	query := `DELETE FROM messages WHERE id IN (SELECT message_id FROM attachments WHERE filepath IN (`
	args := make([]any, len(filepaths))
	for i, p := range filepaths {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = p
	}
	query += "))"

	res, err := q.DB.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AllChannelIDs returns IDs of all channels.
func (q *StorageQueries) AllChannelIDs() ([]string, error) {
	rows, err := q.DB.Query(`SELECT id FROM channels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AttachmentsDiskUsage returns the total size of all attachments in DB (in bytes).
func (q *StorageQueries) AttachmentsDiskUsage() (int64, error) {
	var total sql.NullInt64
	err := q.DB.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM attachments`).Scan(&total)
	if total.Valid {
		return total.Int64, err
	}
	return 0, err
}

// TotalAttachmentCount returns the number of attachments in DB.
func (q *StorageQueries) TotalAttachmentCount() (int, error) {
	var count int
	err := q.DB.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&count)
	return count, err
}

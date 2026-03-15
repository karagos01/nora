package queries

import "database/sql"

type StorageQueries struct {
	DB *sql.DB
}

// TrimChannelHistory smaže zprávy nad keepCount v daném kanálu.
// Vrátí filepaths příloh smazaných zpráv (pro smazání z disku).
func (q *StorageQueries) TrimChannelHistory(channelID string, keepCount int) ([]string, error) {
	// Najít filepaths příloh zpráv, které budou smazány (pinned zprávy přeskočit)
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

	// Smazat zprávy (CASCADE smaže attachments z DB), pinned zprávy přeskočit
	_, err = q.DB.Exec(
		`DELETE FROM messages WHERE channel_id = ? AND is_pinned = 0 AND id NOT IN (
			SELECT id FROM messages WHERE channel_id = ? ORDER BY created_at DESC LIMIT ?
		)`,
		channelID, channelID, keepCount,
	)
	return paths, err
}

// OldestAttachmentPaths vrátí filepaths nejstarších příloh (pro size-based cleanup).
// Vrací páry (message_id, filepath) seřazené od nejstarší zprávy.
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

// DeleteMessagesWithAttachments smaže zprávy jejichž přílohy odpovídají daným filepaths.
// Vrací počet smazaných zpráv.
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

// AllChannelIDs vrátí ID všech kanálů.
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

// AttachmentsDiskUsage vrátí celkovou velikost všech příloh v DB (v bajtech).
func (q *StorageQueries) AttachmentsDiskUsage() (int64, error) {
	var total sql.NullInt64
	err := q.DB.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM attachments`).Scan(&total)
	if total.Valid {
		return total.Int64, err
	}
	return 0, err
}

// TotalAttachmentCount vrátí počet příloh v DB.
func (q *StorageQueries) TotalAttachmentCount() (int, error) {
	var count int
	err := q.DB.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&count)
	return count, err
}

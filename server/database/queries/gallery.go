package queries

import (
	"database/sql"
	"nora/models"
	"strings"
)

type GalleryQueries struct {
	DB *sql.DB
}

// ListMedia vrátí přílohy s kontextem zprávy, filtrované podle typu, kanálu, uživatele
func (q *GalleryQueries) ListMedia(mimePrefix, channelID, userID, before string, limit int) ([]models.GalleryItem, error) {
	query := `SELECT a.id, a.message_id, a.filepath, a.filename, a.mime_type, a.size,
		m.channel_id, COALESCE(c.name, ''), m.user_id, COALESCE(u.username, ''), m.created_at
		FROM attachments a
		JOIN messages m ON m.id = a.message_id
		LEFT JOIN channels c ON c.id = m.channel_id
		LEFT JOIN users u ON u.id = m.user_id
		WHERE 1=1`

	var args []any

	if mimePrefix != "" {
		query += " AND a.mime_type LIKE ?"
		args = append(args, mimePrefix+"%")
	}
	if channelID != "" {
		query += " AND m.channel_id = ?"
		args = append(args, channelID)
	}
	if userID != "" {
		query += " AND m.user_id = ?"
		args = append(args, userID)
	}
	if before != "" {
		query += " AND m.created_at < (SELECT created_at FROM messages WHERE id = ?)"
		args = append(args, before)
	}

	query += " ORDER BY m.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := q.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.GalleryItem
	for rows.Next() {
		var item models.GalleryItem
		var filepath string
		if err := rows.Scan(&item.ID, &item.MessageID, &filepath, &item.Filename, &item.MimeType, &item.Size,
			&item.ChannelID, &item.ChannelName, &item.UserID, &item.Username, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.URL = "/api/uploads/" + filepath
		result = append(result, item)
	}
	return result, rows.Err()
}

// Search hledá přílohy podle názvu souboru
func (q *GalleryQueries) Search(term string, limit int) ([]models.GalleryItem, error) {
	query := `SELECT a.id, a.message_id, a.filepath, a.filename, a.mime_type, a.size,
		m.channel_id, COALESCE(c.name, ''), m.user_id, COALESCE(u.username, ''), m.created_at
		FROM attachments a
		JOIN messages m ON m.id = a.message_id
		LEFT JOIN channels c ON c.id = m.channel_id
		LEFT JOIN users u ON u.id = m.user_id
		WHERE LOWER(a.filename) LIKE ?
		ORDER BY m.created_at DESC LIMIT ?`

	rows, err := q.DB.Query(query, "%"+strings.ToLower(term)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.GalleryItem
	for rows.Next() {
		var item models.GalleryItem
		var filepath string
		if err := rows.Scan(&item.ID, &item.MessageID, &filepath, &item.Filename, &item.MimeType, &item.Size,
			&item.ChannelID, &item.ChannelName, &item.UserID, &item.Username, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.URL = "/api/uploads/" + filepath
		result = append(result, item)
	}
	return result, rows.Err()
}

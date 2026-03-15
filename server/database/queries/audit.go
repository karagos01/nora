package queries

import (
	"database/sql"
	"nora/models"
)

type AuditLogQueries struct {
	DB *sql.DB
}

func (q *AuditLogQueries) Create(entry *models.AuditLogEntry) error {
	_, err := q.DB.Exec(
		`INSERT INTO audit_log (id, user_id, action, target_type, target_id, details) VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.UserID, entry.Action, entry.TargetType, entry.TargetID, entry.Details,
	)
	return err
}

func (q *AuditLogQueries) ListByUser(userID, before string, limit int) ([]models.AuditLogEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `SELECT a.id, a.user_id, a.action, a.target_type, a.target_id, a.details, a.created_at,
	                  u.username, u.avatar_url
	           FROM audit_log a
	           JOIN users u ON u.id = a.user_id
	           WHERE a.user_id = ?`

	var rows *sql.Rows
	var err error

	if before == "" {
		rows, err = q.DB.Query(query+" ORDER BY a.created_at DESC LIMIT ?", userID, limit)
	} else {
		rows, err = q.DB.Query(
			query+" AND a.created_at < (SELECT created_at FROM audit_log WHERE id = ?) ORDER BY a.created_at DESC LIMIT ?",
			userID, before, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.AuditLogEntry
	for rows.Next() {
		var e models.AuditLogEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.CreatedAt, &e.Username, &e.AvatarURL); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListUserMessages vrací zprávy uživatele ve všech kanálech.
func (q *AuditLogQueries) ListUserMessages(db *sql.DB, userID, before string, limit int) ([]models.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `SELECT m.id, m.channel_id, m.user_id, m.content, m.created_at, m.updated_at,
	                  m.is_hidden, m.hidden_by, m.hidden_by_position,
	                  u.id, u.username, u.display_name, u.public_key, u.avatar_url,
	                  c.name
	           FROM messages m
	           JOIN users u ON u.id = m.user_id
	           JOIN channels c ON c.id = m.channel_id
	           WHERE m.user_id = ?`

	var rows *sql.Rows
	var err error

	if before == "" {
		rows, err = db.Query(query+" ORDER BY m.created_at DESC LIMIT ?", userID, limit)
	} else {
		rows, err = db.Query(
			query+" AND m.created_at < (SELECT created_at FROM messages WHERE id = ?) ORDER BY m.created_at DESC LIMIT ?",
			userID, before, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		var author models.User
		var updatedAt sql.NullTime
		var hiddenBy sql.NullString
		var channelName string
		if err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.CreatedAt, &updatedAt,
			&msg.IsHidden, &hiddenBy, &msg.HiddenByPosition,
			&author.ID, &author.Username, &author.DisplayName, &author.PublicKey, &author.AvatarURL,
			&channelName,
		); err != nil {
			return nil, err
		}
		if updatedAt.Valid {
			msg.UpdatedAt = &updatedAt.Time
		}
		if hiddenBy.Valid {
			msg.HiddenBy = &hiddenBy.String
		}
		msg.ChannelName = channelName
		msg.Author = &author
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

package queries

import (
	"database/sql"
	"nora/models"
)

type AttachmentQueries struct {
	DB *sql.DB
}

func (q *AttachmentQueries) Create(att *models.Attachment) error {
	_, err := q.DB.Exec(
		`INSERT INTO attachments (id, message_id, filepath, filename, mime_type, size, content_hash) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		att.ID, att.MessageID, att.Filepath, att.Filename, att.MimeType, att.Size, att.ContentHash,
	)
	return err
}

func (q *AttachmentQueries) ListByMessageID(msgID string) ([]models.Attachment, error) {
	rows, err := q.DB.Query(
		`SELECT id, message_id, filepath, filename, mime_type, size, content_hash FROM attachments WHERE message_id = ?`,
		msgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filepath, &a.Filename, &a.MimeType, &a.Size, &a.ContentHash); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (q *AttachmentQueries) ListByMessageIDs(ids []string) (map[string][]models.Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build query with IN(?)
	query := `SELECT id, message_id, filepath, filename, mime_type, size, content_hash FROM attachments WHERE message_id IN (`
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := q.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]models.Attachment)
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filepath, &a.Filename, &a.MimeType, &a.Size, &a.ContentHash); err != nil {
			return nil, err
		}
		result[a.MessageID] = append(result[a.MessageID], a)
	}
	return result, rows.Err()
}

// ListByUserMessages returns all attachments for messages of a given user.
func (q *AttachmentQueries) ListByUserMessages(userID string) ([]models.Attachment, error) {
	rows, err := q.DB.Query(
		`SELECT a.id, a.message_id, a.filepath, a.filename, a.mime_type, a.size, a.content_hash
		 FROM attachments a JOIN messages m ON a.message_id = m.id WHERE m.user_id = ?`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filepath, &a.Filename, &a.MimeType, &a.Size, &a.ContentHash); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// FindByContentHash finds the filepath of an existing file with the same content hash.
func (q *AttachmentQueries) FindByContentHash(hash string) (string, error) {
	var filepath string
	err := q.DB.QueryRow(
		`SELECT filepath FROM attachments WHERE content_hash = ? LIMIT 1`, hash,
	).Scan(&filepath)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return filepath, err
}

// CountByFilepath counts how many attachments point to the same file.
func (q *AttachmentQueries) CountByFilepath(filepath string) (int, error) {
	var count int
	err := q.DB.QueryRow(
		`SELECT COUNT(*) FROM attachments WHERE filepath = ?`, filepath,
	).Scan(&count)
	return count, err
}

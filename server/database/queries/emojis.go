package queries

import (
	"database/sql"
	"nora/models"
)

type EmojiQueries struct {
	DB *sql.DB
}

func (q *EmojiQueries) Create(emoji *models.Emoji) error {
	_, err := q.DB.Exec(
		`INSERT INTO emojis (id, name, filepath, mime_type, size, uploader_id) VALUES (?, ?, ?, ?, ?, ?)`,
		emoji.ID, emoji.Name, emoji.Filepath, emoji.MimeType, emoji.Size, emoji.UploaderID,
	)
	return err
}

func (q *EmojiQueries) GetByID(id string) (*models.Emoji, error) {
	var e models.Emoji
	err := q.DB.QueryRow(
		`SELECT id, name, filepath, mime_type, size, uploader_id, created_at FROM emojis WHERE id = ?`, id,
	).Scan(&e.ID, &e.Name, &e.Filepath, &e.MimeType, &e.Size, &e.UploaderID, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (q *EmojiQueries) List() ([]models.Emoji, error) {
	rows, err := q.DB.Query(`SELECT id, name, filepath, mime_type, size, uploader_id, created_at FROM emojis ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emojis []models.Emoji
	for rows.Next() {
		var e models.Emoji
		if err := rows.Scan(&e.ID, &e.Name, &e.Filepath, &e.MimeType, &e.Size, &e.UploaderID, &e.CreatedAt); err != nil {
			return nil, err
		}
		emojis = append(emojis, e)
	}
	return emojis, rows.Err()
}

func (q *EmojiQueries) Delete(id string) error {
	_, err := q.DB.Exec(`DELETE FROM emojis WHERE id = ?`, id)
	return err
}

func (q *EmojiQueries) NameExists(name string) (bool, error) {
	var count int
	err := q.DB.QueryRow(`SELECT COUNT(*) FROM emojis WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

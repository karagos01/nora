package queries

import (
	"database/sql"
	"nora/models"
)

type WebhookQueries struct {
	DB *sql.DB
}

func (q *WebhookQueries) Create(w *models.Webhook) error {
	_, err := q.DB.Exec(
		`INSERT INTO webhooks (id, channel_id, name, token, avatar_url, creator_id) VALUES (?, ?, ?, ?, ?, ?)`,
		w.ID, w.ChannelID, w.Name, w.Token, w.AvatarURL, w.CreatorID,
	)
	return err
}

func (q *WebhookQueries) List() ([]models.Webhook, error) {
	rows, err := q.DB.Query(`SELECT id, channel_id, name, token, avatar_url, creator_id, created_at FROM webhooks ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Webhook
	for rows.Next() {
		var w models.Webhook
		if err := rows.Scan(&w.ID, &w.ChannelID, &w.Name, &w.Token, &w.AvatarURL, &w.CreatorID, &w.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (q *WebhookQueries) GetByID(id string) (*models.Webhook, error) {
	var w models.Webhook
	err := q.DB.QueryRow(
		`SELECT id, channel_id, name, token, avatar_url, creator_id, created_at FROM webhooks WHERE id = ?`, id,
	).Scan(&w.ID, &w.ChannelID, &w.Name, &w.Token, &w.AvatarURL, &w.CreatorID, &w.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (q *WebhookQueries) GetByToken(token string) (*models.Webhook, error) {
	var w models.Webhook
	err := q.DB.QueryRow(
		`SELECT id, channel_id, name, token, avatar_url, creator_id, created_at FROM webhooks WHERE token = ?`, token,
	).Scan(&w.ID, &w.ChannelID, &w.Name, &w.Token, &w.AvatarURL, &w.CreatorID, &w.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (q *WebhookQueries) Delete(id string) error {
	_, err := q.DB.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

func (q *WebhookQueries) Update(id, name, avatarURL string) error {
	_, err := q.DB.Exec(`UPDATE webhooks SET name = ?, avatar_url = ? WHERE id = ?`, name, avatarURL, id)
	return err
}

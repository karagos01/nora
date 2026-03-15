package queries

import (
	"database/sql"
	"nora/models"
	"time"
)

type ScheduledMessageQueries struct {
	DB *sql.DB
}

func (q *ScheduledMessageQueries) Create(msg *models.ScheduledMessage) error {
	_, err := q.DB.Exec(
		`INSERT INTO scheduled_messages (id, channel_id, user_id, content, reply_to_id, scheduled_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ChannelID, msg.UserID, msg.Content, msg.ReplyToID, msg.ScheduledAt, msg.CreatedAt,
	)
	return err
}

func (q *ScheduledMessageQueries) Delete(id, userID string) error {
	res, err := q.DB.Exec(`DELETE FROM scheduled_messages WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (q *ScheduledMessageQueries) DeleteByID(id string) error {
	_, err := q.DB.Exec(`DELETE FROM scheduled_messages WHERE id = ?`, id)
	return err
}

func (q *ScheduledMessageQueries) ListByUser(userID string) ([]models.ScheduledMessage, error) {
	rows, err := q.DB.Query(
		`SELECT s.id, s.channel_id, s.user_id, s.content, s.reply_to_id, s.scheduled_at, s.created_at, COALESCE(c.name, '')
		 FROM scheduled_messages s
		 LEFT JOIN channels c ON c.id = s.channel_id
		 WHERE s.user_id = ?
		 ORDER BY s.scheduled_at ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []models.ScheduledMessage
	for rows.Next() {
		var m models.ScheduledMessage
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Content, &m.ReplyToID, &m.ScheduledAt, &m.CreatedAt, &m.ChannelName); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (q *ScheduledMessageQueries) ListDue(now time.Time) ([]models.ScheduledMessage, error) {
	rows, err := q.DB.Query(
		`SELECT id, channel_id, user_id, content, reply_to_id, scheduled_at, created_at
		 FROM scheduled_messages
		 WHERE scheduled_at <= ?
		 ORDER BY scheduled_at ASC`,
		now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []models.ScheduledMessage
	for rows.Next() {
		var m models.ScheduledMessage
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Content, &m.ReplyToID, &m.ScheduledAt, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (q *ScheduledMessageQueries) DeleteByUserID(userID string) error {
	_, err := q.DB.Exec(`DELETE FROM scheduled_messages WHERE user_id = ?`, userID)
	return err
}

func (q *ScheduledMessageQueries) CountByUser(userID string) (int, error) {
	var n int
	err := q.DB.QueryRow(`SELECT COUNT(*) FROM scheduled_messages WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

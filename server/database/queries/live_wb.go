package queries

import (
	"database/sql"
	"time"
)

// LiveWBStroke represents a stroke in a live whiteboard session.
type LiveWBStroke struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	UserID    string    `json:"user_id"`
	PathData  string    `json:"path_data"`
	Color     string    `json:"color"`
	Width     int       `json:"width"`
	Tool      string    `json:"tool"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// LiveWBSession represents an active live whiteboard session.
type LiveWBSession struct {
	ChannelID string    `json:"channel_id"`
	StarterID string    `json:"starter_id"`
	CreatedAt time.Time `json:"created_at"`
}

type LiveWBQueries struct {
	DB *sql.DB
}

func (q *LiveWBQueries) CreateSession(channelID, starterID string) error {
	_, err := q.DB.Exec(
		`INSERT OR REPLACE INTO live_whiteboards (channel_id, starter_id, created_at) VALUES (?, ?, ?)`,
		channelID, starterID, time.Now().UTC(),
	)
	return err
}

func (q *LiveWBQueries) DeleteSession(channelID string) error {
	_, err := q.DB.Exec(`DELETE FROM live_whiteboards WHERE channel_id = ?`, channelID)
	return err
}

func (q *LiveWBQueries) GetSession(channelID string) (*LiveWBSession, error) {
	var s LiveWBSession
	err := q.DB.QueryRow(
		`SELECT channel_id, starter_id, created_at FROM live_whiteboards WHERE channel_id = ?`, channelID,
	).Scan(&s.ChannelID, &s.StarterID, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (q *LiveWBQueries) GetAllSessions() ([]LiveWBSession, error) {
	rows, err := q.DB.Query(`SELECT channel_id, starter_id, created_at FROM live_whiteboards`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LiveWBSession
	for rows.Next() {
		var s LiveWBSession
		if err := rows.Scan(&s.ChannelID, &s.StarterID, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (q *LiveWBQueries) AddStroke(s *LiveWBStroke) error {
	_, err := q.DB.Exec(
		`INSERT INTO live_wb_strokes (id, channel_id, user_id, path_data, color, width, tool, username, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.ChannelID, s.UserID, s.PathData, s.Color, s.Width, s.Tool, s.Username, s.CreatedAt,
	)
	return err
}

func (q *LiveWBQueries) GetStrokes(channelID string) ([]LiveWBStroke, error) {
	rows, err := q.DB.Query(
		`SELECT id, channel_id, user_id, path_data, color, width, tool, username, created_at
		FROM live_wb_strokes WHERE channel_id = ? ORDER BY created_at`, channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LiveWBStroke
	for rows.Next() {
		var s LiveWBStroke
		if err := rows.Scan(&s.ID, &s.ChannelID, &s.UserID, &s.PathData, &s.Color, &s.Width, &s.Tool, &s.Username, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (q *LiveWBQueries) DeleteStroke(id string) error {
	_, err := q.DB.Exec(`DELETE FROM live_wb_strokes WHERE id = ?`, id)
	return err
}

func (q *LiveWBQueries) DeleteLastStrokeByUser(channelID, userID string) (string, error) {
	var strokeID string
	err := q.DB.QueryRow(
		`SELECT id FROM live_wb_strokes WHERE channel_id = ? AND user_id = ? ORDER BY created_at DESC LIMIT 1`,
		channelID, userID,
	).Scan(&strokeID)
	if err != nil {
		return "", err
	}
	_, err = q.DB.Exec(`DELETE FROM live_wb_strokes WHERE id = ?`, strokeID)
	return strokeID, err
}

func (q *LiveWBQueries) ClearStrokes(channelID string) error {
	_, err := q.DB.Exec(`DELETE FROM live_wb_strokes WHERE channel_id = ?`, channelID)
	return err
}

// DeleteStaleSessions deletes sessions older than the given duration and their strokes (CASCADE).
func (q *LiveWBQueries) DeleteStaleSessions(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := q.DB.Exec(`DELETE FROM live_whiteboards WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

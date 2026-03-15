package queries

import (
	"database/sql"
	"nora/models"
)

type WhiteboardQueries struct {
	DB *sql.DB
}

func (q *WhiteboardQueries) Create(wb *models.Whiteboard) error {
	_, err := q.DB.Exec(
		`INSERT INTO whiteboards (id, name, channel_id, creator_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		wb.ID, wb.Name, wb.ChannelID, wb.CreatorID, wb.CreatedAt,
	)
	return err
}

func (q *WhiteboardQueries) GetByID(id string) (*models.Whiteboard, error) {
	var wb models.Whiteboard
	err := q.DB.QueryRow(
		`SELECT id, name, channel_id, creator_id, created_at FROM whiteboards WHERE id = ?`, id,
	).Scan(&wb.ID, &wb.Name, &wb.ChannelID, &wb.CreatorID, &wb.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &wb, nil
}

func (q *WhiteboardQueries) List() ([]models.Whiteboard, error) {
	rows, err := q.DB.Query(`SELECT id, name, channel_id, creator_id, created_at FROM whiteboards ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Whiteboard
	for rows.Next() {
		var wb models.Whiteboard
		if err := rows.Scan(&wb.ID, &wb.Name, &wb.ChannelID, &wb.CreatorID, &wb.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, wb)
	}
	return result, rows.Err()
}

func (q *WhiteboardQueries) Delete(id string) error {
	_, err := q.DB.Exec(`DELETE FROM whiteboards WHERE id = ?`, id)
	return err
}

func (q *WhiteboardQueries) AddStroke(s *models.WhiteboardStroke) error {
	_, err := q.DB.Exec(
		`INSERT INTO whiteboard_strokes (id, whiteboard_id, user_id, path_data, color, width, tool, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.WhiteboardID, s.UserID, s.PathData, s.Color, s.Width, s.Tool, s.CreatedAt,
	)
	return err
}

func (q *WhiteboardQueries) GetStrokes(boardID string) ([]models.WhiteboardStroke, error) {
	rows, err := q.DB.Query(
		`SELECT s.id, s.whiteboard_id, s.user_id, s.path_data, s.color, s.width, s.tool, s.created_at, COALESCE(u.username, '')
		FROM whiteboard_strokes s LEFT JOIN users u ON s.user_id = u.id
		WHERE s.whiteboard_id = ? ORDER BY s.created_at`, boardID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.WhiteboardStroke
	for rows.Next() {
		var s models.WhiteboardStroke
		if err := rows.Scan(&s.ID, &s.WhiteboardID, &s.UserID, &s.PathData, &s.Color, &s.Width, &s.Tool, &s.CreatedAt, &s.Username); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (q *WhiteboardQueries) DeleteStroke(id, userID string) error {
	_, err := q.DB.Exec(`DELETE FROM whiteboard_strokes WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// DeleteLastStrokeByUser deletes the last stroke of a given user on a board and returns its ID.
func (q *WhiteboardQueries) DeleteLastStrokeByUser(boardID, userID string) (string, error) {
	var strokeID string
	err := q.DB.QueryRow(
		`SELECT id FROM whiteboard_strokes WHERE whiteboard_id = ? AND user_id = ? ORDER BY created_at DESC LIMIT 1`,
		boardID, userID,
	).Scan(&strokeID)
	if err != nil {
		return "", err
	}
	_, err = q.DB.Exec(`DELETE FROM whiteboard_strokes WHERE id = ?`, strokeID)
	return strokeID, err
}

func (q *WhiteboardQueries) ClearStrokes(boardID string) error {
	_, err := q.DB.Exec(`DELETE FROM whiteboard_strokes WHERE whiteboard_id = ?`, boardID)
	return err
}

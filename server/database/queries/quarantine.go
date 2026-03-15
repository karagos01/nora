package queries

import (
	"database/sql"
	"nora/models"
	"time"
)

type QuarantineQueries struct {
	DB *sql.DB
}

func (q *QuarantineQueries) Create(userID string, endsAt *time.Time) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO quarantine (user_id, started_at, ends_at)
		 VALUES (?, datetime('now'), ?)`,
		userID, endsAt,
	)
	return err
}

func (q *QuarantineQueries) IsInQuarantine(userID string) bool {
	var count int
	q.DB.QueryRow(
		`SELECT COUNT(*) FROM quarantine
		 WHERE user_id = ? AND approved_by IS NULL
		 AND (ends_at IS NULL OR ends_at > datetime('now'))`,
		userID,
	).Scan(&count)
	return count > 0
}

func (q *QuarantineQueries) GetByUser(userID string) (*models.QuarantineEntry, error) {
	e := &models.QuarantineEntry{}
	err := q.DB.QueryRow(
		`SELECT q.user_id, q.started_at, q.ends_at, q.approved_by, q.approved_at,
		        COALESCE(u.username, '')
		 FROM quarantine q LEFT JOIN users u ON q.user_id = u.id
		 WHERE q.user_id = ?`, userID,
	).Scan(&e.UserID, &e.StartedAt, &e.EndsAt, &e.ApprovedBy, &e.ApprovedAt, &e.Username)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (q *QuarantineQueries) List() ([]models.QuarantineEntry, error) {
	rows, err := q.DB.Query(
		`SELECT q.user_id, q.started_at, q.ends_at, q.approved_by, q.approved_at,
		        COALESCE(u.username, '')
		 FROM quarantine q LEFT JOIN users u ON q.user_id = u.id
		 WHERE q.approved_by IS NULL AND (q.ends_at IS NULL OR q.ends_at > datetime('now'))
		 ORDER BY q.started_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.QuarantineEntry
	for rows.Next() {
		var e models.QuarantineEntry
		if err := rows.Scan(&e.UserID, &e.StartedAt, &e.EndsAt, &e.ApprovedBy, &e.ApprovedAt, &e.Username); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (q *QuarantineQueries) Approve(userID, approvedBy string) error {
	_, err := q.DB.Exec(
		`UPDATE quarantine SET approved_by = ?, approved_at = datetime('now') WHERE user_id = ?`,
		approvedBy, userID,
	)
	return err
}

func (q *QuarantineQueries) Delete(userID string) error {
	_, err := q.DB.Exec("DELETE FROM quarantine WHERE user_id = ?", userID)
	return err
}

package queries

import (
	"database/sql"
	"nora/models"
)

type TimeoutQueries struct {
	DB *sql.DB
}

func (q *TimeoutQueries) Create(t *models.Timeout) error {
	_, err := q.DB.Exec(
		`INSERT OR REPLACE INTO timeouts (id, user_id, reason, issued_by, expires_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Reason, t.IssuedBy, t.ExpiresAt,
	)
	return err
}

func (q *TimeoutQueries) GetActive(userID string) (*models.Timeout, error) {
	var t models.Timeout
	err := q.DB.QueryRow(
		`SELECT id, user_id, reason, issued_by, expires_at, created_at FROM timeouts WHERE user_id = ? AND expires_at > datetime('now')`,
		userID,
	).Scan(&t.ID, &t.UserID, &t.Reason, &t.IssuedBy, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (q *TimeoutQueries) Delete(userID string) error {
	_, err := q.DB.Exec("DELETE FROM timeouts WHERE user_id = ?", userID)
	return err
}

func (q *TimeoutQueries) CleanExpired() error {
	_, err := q.DB.Exec("DELETE FROM timeouts WHERE expires_at <= datetime('now')")
	return err
}

func (q *TimeoutQueries) List() ([]models.Timeout, error) {
	rows, err := q.DB.Query(
		`SELECT id, user_id, reason, issued_by, expires_at, created_at FROM timeouts WHERE expires_at > datetime('now') ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var timeouts []models.Timeout
	for rows.Next() {
		var t models.Timeout
		if err := rows.Scan(&t.ID, &t.UserID, &t.Reason, &t.IssuedBy, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		timeouts = append(timeouts, t)
	}
	return timeouts, nil
}

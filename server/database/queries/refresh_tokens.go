package queries

import (
	"database/sql"
	"nora/models"
)

type RefreshTokenQueries struct {
	DB *sql.DB
}

func (q *RefreshTokenQueries) Create(rt *models.RefreshToken) error {
	_, err := q.DB.Exec(
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		rt.ID, rt.UserID, rt.TokenHash, rt.ExpiresAt,
	)
	return err
}

func (q *RefreshTokenQueries) GetByHash(hash string) (*models.RefreshToken, error) {
	rt := &models.RefreshToken{}
	err := q.DB.QueryRow(
		`SELECT id, user_id, token_hash, expires_at, created_at
		 FROM refresh_tokens WHERE token_hash = ?`, hash,
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

func (q *RefreshTokenQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM refresh_tokens WHERE id = ?", id)
	return err
}

func (q *RefreshTokenQueries) DeleteByUserID(userID string) error {
	_, err := q.DB.Exec("DELETE FROM refresh_tokens WHERE user_id = ?", userID)
	return err
}

func (q *RefreshTokenQueries) DeleteExpired() (int64, error) {
	result, err := q.DB.Exec("DELETE FROM refresh_tokens WHERE expires_at < datetime('now')")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (q *RefreshTokenQueries) CountByUserID(userID string) (int, error) {
	var count int
	err := q.DB.QueryRow(
		"SELECT COUNT(*) FROM refresh_tokens WHERE user_id = ? AND expires_at > datetime('now')",
		userID,
	).Scan(&count)
	return count, err
}


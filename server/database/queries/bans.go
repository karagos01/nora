package queries

import (
	"database/sql"
	"nora/models"
)

type BanQueries struct {
	DB *sql.DB
}

func (q *BanQueries) Create(ban *models.Ban) error {
	_, err := q.DB.Exec(
		`INSERT INTO bans (id, user_id, reason, banned_by, expires_at) VALUES (?, ?, ?, ?, ?)`,
		ban.ID, ban.UserID, ban.Reason, ban.BannedBy, ban.ExpiresAt,
	)
	return err
}

func (q *BanQueries) GetByUserID(userID string) (*models.Ban, error) {
	b := &models.Ban{}
	err := q.DB.QueryRow(
		`SELECT id, user_id, reason, banned_by, created_at, expires_at FROM bans WHERE user_id = ?`, userID,
	).Scan(&b.ID, &b.UserID, &b.Reason, &b.BannedBy, &b.CreatedAt, &b.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (q *BanQueries) List() ([]models.Ban, error) {
	rows, err := q.DB.Query(
		`SELECT b.id, b.user_id, COALESCE(u.username, ''), COALESCE(u.display_name, ''),
		        b.reason, b.banned_by, b.created_at, COALESCE(u.last_ip, ''),
		        b.expires_at, COALESCE(u.invited_by, ''),
		        (SELECT COUNT(*) FROM user_devices ud WHERE ud.user_id = b.user_id)
		 FROM bans b LEFT JOIN users u ON b.user_id = u.id
		 ORDER BY b.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bans []models.Ban
	for rows.Next() {
		var b models.Ban
		if err := rows.Scan(&b.ID, &b.UserID, &b.Username, &b.DisplayName,
			&b.Reason, &b.BannedBy, &b.CreatedAt, &b.IP,
			&b.ExpiresAt, &b.InvitedBy, &b.DeviceCount); err != nil {
			return nil, err
		}
		bans = append(bans, b)
	}
	return bans, rows.Err()
}

func (q *BanQueries) IsBanned(userID string) bool {
	var count int
	q.DB.QueryRow(
		`SELECT COUNT(*) FROM bans WHERE user_id = ?
		 AND (expires_at IS NULL OR expires_at > datetime('now'))`,
		userID,
	).Scan(&count)
	return count > 0
}

func (q *BanQueries) Delete(userID string) error {
	_, err := q.DB.Exec("DELETE FROM bans WHERE user_id = ?", userID)
	return err
}

func (q *BanQueries) DeleteExpired() (int64, error) {
	result, err := q.DB.Exec("DELETE FROM bans WHERE expires_at IS NOT NULL AND expires_at < datetime('now')")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

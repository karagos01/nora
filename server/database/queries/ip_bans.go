package queries

import (
	"database/sql"
	"nora/models"
)

type IPBanQueries struct {
	DB *sql.DB
}

func (q *IPBanQueries) Create(ip, reason, bannedBy, relatedUserID string) error {
	_, err := q.DB.Exec(
		`INSERT OR REPLACE INTO banned_ips (ip, reason, banned_by, related_user_id) VALUES (?, ?, ?, ?)`,
		ip, reason, bannedBy, relatedUserID,
	)
	return err
}

func (q *IPBanQueries) IsBanned(ip string) bool {
	var count int
	q.DB.QueryRow("SELECT COUNT(*) FROM banned_ips WHERE ip = ?", ip).Scan(&count)
	return count > 0
}

func (q *IPBanQueries) List() ([]models.IPBan, error) {
	rows, err := q.DB.Query(
		`SELECT b.ip, b.reason, b.banned_by, b.related_user_id,
		        COALESCE(u.username, ''), b.created_at
		 FROM banned_ips b LEFT JOIN users u ON b.related_user_id = u.id
		 ORDER BY b.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bans []models.IPBan
	for rows.Next() {
		var b models.IPBan
		if err := rows.Scan(&b.IP, &b.Reason, &b.BannedBy, &b.RelatedUserID, &b.RelatedUsername, &b.CreatedAt); err != nil {
			return nil, err
		}
		bans = append(bans, b)
	}
	return bans, rows.Err()
}

func (q *IPBanQueries) Delete(ip string) error {
	_, err := q.DB.Exec("DELETE FROM banned_ips WHERE ip = ?", ip)
	return err
}

func (q *IPBanQueries) DeleteByRelatedUser(userID string) error {
	_, err := q.DB.Exec("DELETE FROM banned_ips WHERE related_user_id = ?", userID)
	return err
}

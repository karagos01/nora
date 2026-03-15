package queries

import (
	"database/sql"
	"nora/models"
)

type DeviceBanQueries struct {
	DB *sql.DB
}

func (q *DeviceBanQueries) Create(ban *models.DeviceBan) error {
	_, err := q.DB.Exec(
		`INSERT INTO device_bans (id, device_id, hardware_hash, related_user_id, banned_by, reason, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		ban.ID, ban.DeviceID, ban.HardwareHash, ban.RelatedUserID, ban.BannedBy, ban.Reason, ban.ExpiresAt,
	)
	return err
}

func (q *DeviceBanQueries) IsDeviceOrHardwareBanned(deviceID, hardwareHash string) bool {
	var count int
	q.DB.QueryRow(
		`SELECT COUNT(*) FROM device_bans
		 WHERE (device_id = ? OR hardware_hash = ?)
		 AND (expires_at IS NULL OR expires_at > datetime('now'))`,
		deviceID, hardwareHash,
	).Scan(&count)
	return count > 0
}

func (q *DeviceBanQueries) List() ([]models.DeviceBan, error) {
	rows, err := q.DB.Query(
		`SELECT db.id, db.device_id, db.hardware_hash, db.related_user_id,
		        db.banned_by, db.reason, db.expires_at, db.created_at,
		        COALESCE(u.username, '')
		 FROM device_bans db LEFT JOIN users u ON db.related_user_id = u.id
		 ORDER BY db.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bans []models.DeviceBan
	for rows.Next() {
		var b models.DeviceBan
		if err := rows.Scan(&b.ID, &b.DeviceID, &b.HardwareHash, &b.RelatedUserID,
			&b.BannedBy, &b.Reason, &b.ExpiresAt, &b.CreatedAt, &b.RelatedUsername); err != nil {
			return nil, err
		}
		bans = append(bans, b)
	}
	return bans, rows.Err()
}

func (q *DeviceBanQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM device_bans WHERE id = ?", id)
	return err
}

func (q *DeviceBanQueries) DeleteByRelatedUser(userID string) error {
	_, err := q.DB.Exec("DELETE FROM device_bans WHERE related_user_id = ?", userID)
	return err
}

func (q *DeviceBanQueries) DeleteExpired() (int64, error) {
	result, err := q.DB.Exec("DELETE FROM device_bans WHERE expires_at IS NOT NULL AND expires_at < datetime('now')")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

package queries

import (
	"database/sql"
	"nora/models"
)

type UserDeviceQueries struct {
	DB *sql.DB
}

func (q *UserDeviceQueries) Upsert(userID, deviceID, hardwareHash string) error {
	_, err := q.DB.Exec(
		`INSERT INTO user_devices (user_id, device_id, hardware_hash, first_seen_at, last_seen_at)
		 VALUES (?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(user_id, device_id) DO UPDATE SET
		   hardware_hash = excluded.hardware_hash,
		   last_seen_at = datetime('now')`,
		userID, deviceID, hardwareHash,
	)
	return err
}

func (q *UserDeviceQueries) GetByUser(userID string) ([]models.UserDevice, error) {
	rows, err := q.DB.Query(
		`SELECT user_id, device_id, hardware_hash, first_seen_at, last_seen_at
		 FROM user_devices WHERE user_id = ? ORDER BY last_seen_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []models.UserDevice
	for rows.Next() {
		var d models.UserDevice
		if err := rows.Scan(&d.UserID, &d.DeviceID, &d.HardwareHash, &d.FirstSeenAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (q *UserDeviceQueries) GetByDeviceID(deviceID string) ([]models.UserDevice, error) {
	rows, err := q.DB.Query(
		`SELECT ud.user_id, ud.device_id, ud.hardware_hash, ud.first_seen_at, ud.last_seen_at,
		        COALESCE(u.username, '')
		 FROM user_devices ud LEFT JOIN users u ON ud.user_id = u.id
		 WHERE ud.device_id = ? ORDER BY ud.last_seen_at DESC`, deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []models.UserDevice
	for rows.Next() {
		var d models.UserDevice
		if err := rows.Scan(&d.UserID, &d.DeviceID, &d.HardwareHash, &d.FirstSeenAt, &d.LastSeenAt, &d.Username); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (q *UserDeviceQueries) GetByHardwareHash(hash string) ([]models.UserDevice, error) {
	rows, err := q.DB.Query(
		`SELECT ud.user_id, ud.device_id, ud.hardware_hash, ud.first_seen_at, ud.last_seen_at,
		        COALESCE(u.username, '')
		 FROM user_devices ud LEFT JOIN users u ON ud.user_id = u.id
		 WHERE ud.hardware_hash = ? ORDER BY ud.last_seen_at DESC`, hash,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []models.UserDevice
	for rows.Next() {
		var d models.UserDevice
		if err := rows.Scan(&d.UserID, &d.DeviceID, &d.HardwareHash, &d.FirstSeenAt, &d.LastSeenAt, &d.Username); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (q *UserDeviceQueries) CountByUser(userID string) (int, error) {
	var count int
	err := q.DB.QueryRow("SELECT COUNT(*) FROM user_devices WHERE user_id = ?", userID).Scan(&count)
	return count, err
}

func (q *UserDeviceQueries) ListAll() ([]models.UserDevice, error) {
	rows, err := q.DB.Query(
		`SELECT ud.user_id, ud.device_id, ud.hardware_hash, ud.first_seen_at, ud.last_seen_at,
		        COALESCE(u.username, '')
		 FROM user_devices ud LEFT JOIN users u ON ud.user_id = u.id
		 ORDER BY ud.last_seen_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []models.UserDevice
	for rows.Next() {
		var d models.UserDevice
		if err := rows.Scan(&d.UserID, &d.DeviceID, &d.HardwareHash, &d.FirstSeenAt, &d.LastSeenAt, &d.Username); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

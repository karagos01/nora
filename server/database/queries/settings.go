package queries

import "database/sql"

type SettingsQueries struct {
	DB *sql.DB
}

func (q *SettingsQueries) Get(key, defaultValue string) string {
	var value string
	err := q.DB.QueryRow(`SELECT value FROM server_settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return defaultValue
	}
	return value
}

func (q *SettingsQueries) Set(key, value string) error {
	_, err := q.DB.Exec(
		`INSERT INTO server_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

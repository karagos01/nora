package queries

import (
	"database/sql"
	"nora/models"
)

type AuthChallengeQueries struct {
	DB *sql.DB
}

func (q *AuthChallengeQueries) Create(publicKey, nonce, expiresAt, username, invitedBy, deviceID, hardwareHash string) error {
	_, err := q.DB.Exec(
		`INSERT OR REPLACE INTO auth_challenges (public_key, nonce, expires_at, username, invited_by, device_id, hardware_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		publicKey, nonce, expiresAt, username, invitedBy, deviceID, hardwareHash,
	)
	return err
}

func (q *AuthChallengeQueries) GetByPublicKey(publicKey string) (*models.AuthChallenge, error) {
	c := &models.AuthChallenge{}
	err := q.DB.QueryRow(
		`SELECT public_key, nonce, expires_at, username, invited_by, device_id, hardware_hash
		 FROM auth_challenges WHERE public_key = ?`,
		publicKey,
	).Scan(&c.PublicKey, &c.Nonce, &c.ExpiresAt, &c.Username, &c.InvitedBy, &c.DeviceID, &c.HardwareHash)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (q *AuthChallengeQueries) Delete(publicKey string) error {
	_, err := q.DB.Exec("DELETE FROM auth_challenges WHERE public_key = ?", publicKey)
	return err
}

func (q *AuthChallengeQueries) CleanExpired() (int64, error) {
	result, err := q.DB.Exec("DELETE FROM auth_challenges WHERE expires_at < datetime('now')")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

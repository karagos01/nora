package queries

import (
	"database/sql"
	"nora/models"
)

type UserQueries struct {
	DB *sql.DB
}

func (q *UserQueries) Create(user *models.User) error {
	_, err := q.DB.Exec(
		`INSERT INTO users (id, username, display_name, public_key, is_owner, avatar_url, last_ip, invited_by, status, status_text)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.DisplayName, user.PublicKey, user.IsOwner, user.AvatarURL, user.LastIP, user.InvitedBy, user.Status, user.StatusText,
	)
	return err
}

func (q *UserQueries) GetByID(id string) (*models.User, error) {
	u := &models.User{}
	err := q.DB.QueryRow(
		`SELECT id, username, display_name, public_key, is_owner, created_at, avatar_url, last_ip, invited_by, status, status_text
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL, &u.LastIP, &u.InvitedBy, &u.Status, &u.StatusText)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (q *UserQueries) GetByUsername(username string) (*models.User, error) {
	u := &models.User{}
	err := q.DB.QueryRow(
		`SELECT id, username, display_name, public_key, is_owner, created_at, avatar_url, last_ip, invited_by, status, status_text
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL, &u.LastIP, &u.InvitedBy, &u.Status, &u.StatusText)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (q *UserQueries) GetByPublicKey(key string) (*models.User, error) {
	u := &models.User{}
	err := q.DB.QueryRow(
		`SELECT id, username, display_name, public_key, is_owner, created_at, avatar_url, last_ip, invited_by, status, status_text
		 FROM users WHERE public_key = ?`, key,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL, &u.LastIP, &u.InvitedBy, &u.Status, &u.StatusText)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (q *UserQueries) List() ([]models.User, error) {
	rows, err := q.DB.Query(
		`SELECT id, username, display_name, public_key, is_owner, created_at, avatar_url, last_ip, invited_by, status, status_text
		 FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL, &u.LastIP, &u.InvitedBy, &u.Status, &u.StatusText); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (q *UserQueries) GetOwner() (*models.User, error) {
	var u models.User
	err := q.DB.QueryRow(
		`SELECT id, username, display_name, public_key, is_owner, created_at, avatar_url, last_ip, invited_by, status, status_text
		 FROM users WHERE is_owner = 1 LIMIT 1`,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL, &u.LastIP, &u.InvitedBy, &u.Status, &u.StatusText)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (q *UserQueries) Count() (int, error) {
	var count int
	err := q.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (q *UserQueries) IsBanned(userID string) (bool, error) {
	var count int
	err := q.DB.QueryRow("SELECT COUNT(*) FROM bans WHERE user_id = ?", userID).Scan(&count)
	return count > 0, err
}

func (q *UserQueries) UpdateDisplayName(id, displayName string) error {
	_, err := q.DB.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, id)
	return err
}

func (q *UserQueries) UpdateLastIP(id, ip string) error {
	_, err := q.DB.Exec("UPDATE users SET last_ip = ? WHERE id = ?", ip, id)
	return err
}

func (q *UserQueries) UpdateStatus(id, status, statusText string) error {
	_, err := q.DB.Exec("UPDATE users SET status = ?, status_text = ? WHERE id = ?", status, statusText, id)
	return err
}

func (q *UserQueries) UpdateAvatarURL(id, url string) error {
	_, err := q.DB.Exec("UPDATE users SET avatar_url = ? WHERE id = ?", url, id)
	return err
}

func (q *UserQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

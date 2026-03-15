package queries

import (
	"database/sql"
	"nora/models"
)

type FriendQueries struct {
	DB *sql.DB
}

func (q *FriendQueries) Add(userID, friendID string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT OR IGNORE INTO friends (user_id, friend_id) VALUES (?, ?)`, userID, friendID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO friends (user_id, friend_id) VALUES (?, ?)`, friendID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (q *FriendQueries) Remove(userID, friendID string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM friends WHERE user_id = ? AND friend_id = ?`, userID, friendID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM friends WHERE user_id = ? AND friend_id = ?`, friendID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (q *FriendQueries) List(userID string) ([]models.User, error) {
	rows, err := q.DB.Query(
		`SELECT u.id, u.username, u.display_name, u.public_key, u.is_owner, u.created_at, u.avatar_url
		 FROM friends f JOIN users u ON f.friend_id = u.id
		 WHERE f.user_id = ?
		 ORDER BY u.username`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (q *FriendQueries) AreFriends(userID1, userID2 string) (bool, error) {
	var count int
	err := q.DB.QueryRow(`SELECT COUNT(*) FROM friends WHERE user_id = ? AND friend_id = ?`, userID1, userID2).Scan(&count)
	return count > 0, err
}

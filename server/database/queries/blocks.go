package queries

import (
	"database/sql"
	"nora/models"
)

type BlockQueries struct {
	DB *sql.DB
}

func (q *BlockQueries) Add(blockerID, blockedID string) error {
	_, err := q.DB.Exec(`INSERT OR IGNORE INTO blocks (blocker_id, blocked_id) VALUES (?, ?)`, blockerID, blockedID)
	return err
}

func (q *BlockQueries) Remove(blockerID, blockedID string) error {
	_, err := q.DB.Exec(`DELETE FROM blocks WHERE blocker_id = ? AND blocked_id = ?`, blockerID, blockedID)
	return err
}

func (q *BlockQueries) List(blockerID string) ([]models.User, error) {
	rows, err := q.DB.Query(
		`SELECT u.id, u.username, u.display_name, u.public_key, u.is_owner, u.created_at, u.avatar_url
		 FROM blocks b JOIN users u ON b.blocked_id = u.id
		 WHERE b.blocker_id = ?
		 ORDER BY b.created_at DESC`, blockerID,
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

func (q *BlockQueries) IsBlocked(blockerID, blockedID string) (bool, error) {
	var count int
	err := q.DB.QueryRow(`SELECT COUNT(*) FROM blocks WHERE blocker_id = ? AND blocked_id = ?`, blockerID, blockedID).Scan(&count)
	return count > 0, err
}

func (q *BlockQueries) EitherBlocked(userID1, userID2 string) (bool, error) {
	var count int
	err := q.DB.QueryRow(
		`SELECT COUNT(*) FROM blocks WHERE (blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)`,
		userID1, userID2, userID2, userID1,
	).Scan(&count)
	return count > 0, err
}

package queries

import (
	"database/sql"
	"nora/models"

	"github.com/google/uuid"
)

type FriendRequestQueries struct {
	DB *sql.DB
}

func (q *FriendRequestQueries) Create(fromUserID, toUserID string) (*models.FriendRequest, error) {
	id, _ := uuid.NewV7()
	req := &models.FriendRequest{
		ID:         id.String(),
		FromUserID: fromUserID,
		ToUserID:   toUserID,
	}
	err := q.DB.QueryRow(
		`INSERT INTO friend_requests (id, from_user_id, to_user_id) VALUES (?, ?, ?) RETURNING created_at`,
		req.ID, req.FromUserID, req.ToUserID,
	).Scan(&req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (q *FriendRequestQueries) ListPendingForUser(userID string) ([]models.FriendRequest, error) {
	rows, err := q.DB.Query(
		`SELECT fr.id, fr.from_user_id, fr.to_user_id, fr.created_at,
		        u.id, u.username, u.display_name, u.public_key, u.is_owner, u.created_at, u.avatar_url
		 FROM friend_requests fr
		 JOIN users u ON fr.from_user_id = u.id
		 WHERE fr.to_user_id = ?
		 ORDER BY fr.created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []models.FriendRequest
	for rows.Next() {
		var fr models.FriendRequest
		var u models.User
		if err := rows.Scan(&fr.ID, &fr.FromUserID, &fr.ToUserID, &fr.CreatedAt,
			&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL); err != nil {
			return nil, err
		}
		fr.FromUser = &u
		reqs = append(reqs, fr)
	}
	return reqs, rows.Err()
}

func (q *FriendRequestQueries) ListSentByUser(userID string) ([]models.FriendRequest, error) {
	rows, err := q.DB.Query(
		`SELECT fr.id, fr.from_user_id, fr.to_user_id, fr.created_at,
		        u.id, u.username, u.display_name, u.public_key, u.is_owner, u.created_at, u.avatar_url
		 FROM friend_requests fr
		 JOIN users u ON fr.to_user_id = u.id
		 WHERE fr.from_user_id = ?
		 ORDER BY fr.created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []models.FriendRequest
	for rows.Next() {
		var fr models.FriendRequest
		var u models.User
		if err := rows.Scan(&fr.ID, &fr.FromUserID, &fr.ToUserID, &fr.CreatedAt,
			&u.ID, &u.Username, &u.DisplayName, &u.PublicKey, &u.IsOwner, &u.CreatedAt, &u.AvatarURL); err != nil {
			return nil, err
		}
		fr.ToUser = &u
		reqs = append(reqs, fr)
	}
	return reqs, rows.Err()
}

func (q *FriendRequestQueries) GetByID(id string) (*models.FriendRequest, error) {
	var fr models.FriendRequest
	err := q.DB.QueryRow(
		`SELECT id, from_user_id, to_user_id, created_at FROM friend_requests WHERE id = ?`, id,
	).Scan(&fr.ID, &fr.FromUserID, &fr.ToUserID, &fr.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &fr, nil
}

func (q *FriendRequestQueries) Exists(userID1, userID2 string) (bool, error) {
	var count int
	err := q.DB.QueryRow(
		`SELECT COUNT(*) FROM friend_requests WHERE (from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)`,
		userID1, userID2, userID2, userID1,
	).Scan(&count)
	return count > 0, err
}

func (q *FriendRequestQueries) Delete(id string) error {
	_, err := q.DB.Exec(`DELETE FROM friend_requests WHERE id = ?`, id)
	return err
}

func (q *FriendRequestQueries) DeleteBetween(userID1, userID2 string) error {
	_, err := q.DB.Exec(
		`DELETE FROM friend_requests WHERE (from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)`,
		userID1, userID2, userID2, userID1,
	)
	return err
}

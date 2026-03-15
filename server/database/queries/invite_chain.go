package queries

import (
	"database/sql"
	"nora/models"
)

type InviteChainQueries struct {
	DB *sql.DB
}

func (q *InviteChainQueries) Create(userID, invitedByID, inviteCode string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO invite_chain (user_id, invited_by_id, invite_code, joined_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		userID, invitedByID, inviteCode,
	)
	return err
}

func (q *InviteChainQueries) GetByUser(userID string) (*models.InviteChainEntry, error) {
	e := &models.InviteChainEntry{}
	err := q.DB.QueryRow(
		`SELECT ic.user_id, ic.invited_by_id, ic.invite_code, ic.joined_at,
		        COALESCE(u.username, ''), COALESCE(inv.username, '')
		 FROM invite_chain ic
		 LEFT JOIN users u ON ic.user_id = u.id
		 LEFT JOIN users inv ON ic.invited_by_id = inv.id
		 WHERE ic.user_id = ?`, userID,
	).Scan(&e.UserID, &e.InvitedByID, &e.InviteCode, &e.JoinedAt, &e.Username, &e.InviterUsername)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (q *InviteChainQueries) GetAll() ([]models.InviteChainEntry, error) {
	rows, err := q.DB.Query(
		`SELECT ic.user_id, ic.invited_by_id, ic.invite_code, ic.joined_at,
		        COALESCE(u.username, ''), COALESCE(inv.username, ''),
		        (SELECT COUNT(*) FROM invite_chain ic2 WHERE ic2.invited_by_id = ic.user_id),
		        (SELECT COUNT(*) FROM invite_chain ic3 INNER JOIN bans b ON ic3.user_id = b.user_id WHERE ic3.invited_by_id = ic.user_id),
		        CASE WHEN EXISTS(SELECT 1 FROM bans WHERE user_id = ic.user_id) THEN 1 ELSE 0 END
		 FROM invite_chain ic
		 LEFT JOIN users u ON ic.user_id = u.id
		 LEFT JOIN users inv ON ic.invited_by_id = inv.id
		 ORDER BY ic.joined_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.InviteChainEntry
	for rows.Next() {
		var e models.InviteChainEntry
		if err := rows.Scan(&e.UserID, &e.InvitedByID, &e.InviteCode, &e.JoinedAt,
			&e.Username, &e.InviterUsername, &e.InvitedCount, &e.BannedCount, &e.IsBanned); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (q *InviteChainQueries) CountInvitedByUser(userID string) (int, error) {
	var count int
	err := q.DB.QueryRow("SELECT COUNT(*) FROM invite_chain WHERE invited_by_id = ?", userID).Scan(&count)
	return count, err
}

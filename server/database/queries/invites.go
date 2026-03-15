package queries

import (
	"database/sql"
	"nora/models"
)

type InviteQueries struct {
	DB *sql.DB
}

func (q *InviteQueries) Create(inv *models.Invite) error {
	_, err := q.DB.Exec(
		`INSERT INTO invites (id, code, creator_id, max_uses, expires_at) VALUES (?, ?, ?, ?, ?)`,
		inv.ID, inv.Code, inv.CreatorID, inv.MaxUses, inv.ExpiresAt,
	)
	return err
}

func (q *InviteQueries) GetByCode(code string) (*models.Invite, error) {
	inv := &models.Invite{}
	var expiresAt sql.NullTime
	err := q.DB.QueryRow(
		`SELECT id, code, creator_id, max_uses, uses, expires_at, created_at
		 FROM invites WHERE code = ?`, code,
	).Scan(&inv.ID, &inv.Code, &inv.CreatorID, &inv.MaxUses, &inv.Uses, &expiresAt, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		inv.ExpiresAt = &expiresAt.Time
	}
	return inv, nil
}

// IncrementUses atomically increments uses only if max_uses is not reached.
// Returns true if the increment was applied, false if the invite is fully used.
func (q *InviteQueries) IncrementUses(id string) (bool, error) {
	res, err := q.DB.Exec(
		"UPDATE invites SET uses = uses + 1 WHERE id = ? AND (max_uses = 0 OR uses < max_uses)",
		id,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (q *InviteQueries) List() ([]models.Invite, error) {
	rows, err := q.DB.Query(
		`SELECT i.id, i.code, i.creator_id, COALESCE(u.username, ''), i.max_uses, i.uses, i.expires_at, i.created_at
		 FROM invites i LEFT JOIN users u ON i.creator_id = u.id
		 ORDER BY i.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []models.Invite
	for rows.Next() {
		var inv models.Invite
		var expiresAt sql.NullTime
		if err := rows.Scan(&inv.ID, &inv.Code, &inv.CreatorID, &inv.CreatorUsername, &inv.MaxUses, &inv.Uses, &expiresAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			inv.ExpiresAt = &expiresAt.Time
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

func (q *InviteQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM invites WHERE id = ?", id)
	return err
}

func (q *InviteQueries) DeleteByCreatorID(creatorID string) error {
	_, err := q.DB.Exec("DELETE FROM invites WHERE creator_id = ?", creatorID)
	return err
}

func (q *InviteQueries) CountByCreator(creatorID string) (int, error) {
	var count int
	err := q.DB.QueryRow("SELECT COUNT(*) FROM invites WHERE creator_id = ?", creatorID).Scan(&count)
	return count, err
}

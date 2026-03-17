package queries

import (
	"database/sql"
	"nora/models"
)

type GroupQueries struct {
	DB *sql.DB
}

func (q *GroupQueries) Create(g *models.Group) error {
	_, err := q.DB.Exec(
		"INSERT INTO groups (id, name, creator_id) VALUES (?, ?, ?)",
		g.ID, g.Name, g.CreatorID,
	)
	return err
}

func (q *GroupQueries) GetByID(id string) (*models.Group, error) {
	g := &models.Group{}
	err := q.DB.QueryRow(
		"SELECT id, name, creator_id, created_at FROM groups WHERE id = ?", id,
	).Scan(&g.ID, &g.Name, &g.CreatorID, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (q *GroupQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM groups WHERE id = ?", id)
	return err
}

func (q *GroupQueries) ListForUser(userID string) ([]models.Group, error) {
	rows, err := q.DB.Query(
		`SELECT g.id, g.name, g.creator_id, g.created_at FROM groups g
		 JOIN group_members gm ON gm.group_id = g.id
		 WHERE gm.user_id = ?
		 ORDER BY g.created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []models.Group
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatorID, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (q *GroupQueries) AddMember(m *models.GroupMember) error {
	_, err := q.DB.Exec(
		"INSERT INTO group_members (group_id, user_id, public_key) VALUES (?, ?, ?)",
		m.GroupID, m.UserID, m.PublicKey,
	)
	return err
}

func (q *GroupQueries) RemoveMember(groupID, userID string) error {
	_, err := q.DB.Exec(
		"DELETE FROM group_members WHERE group_id = ? AND user_id = ?",
		groupID, userID,
	)
	return err
}

func (q *GroupQueries) GetMembers(groupID string) ([]models.GroupMember, error) {
	rows, err := q.DB.Query(
		"SELECT group_id, user_id, public_key, joined_at FROM group_members WHERE group_id = ?",
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.GroupMember
	for rows.Next() {
		var m models.GroupMember
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.PublicKey, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (q *GroupQueries) IsMember(groupID, userID string) (bool, error) {
	var count int
	err := q.DB.QueryRow(
		"SELECT COUNT(*) FROM group_members WHERE group_id = ? AND user_id = ?",
		groupID, userID,
	).Scan(&count)
	return count > 0, err
}

func (q *GroupQueries) CreateInvite(inv *models.GroupInvite) error {
	_, err := q.DB.Exec(
		"INSERT INTO group_invites (id, group_id, code, creator_id, max_uses, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		inv.ID, inv.GroupID, inv.Code, inv.CreatorID, inv.MaxUses, inv.ExpiresAt,
	)
	return err
}

func (q *GroupQueries) GetInviteByCode(code string) (*models.GroupInvite, error) {
	inv := &models.GroupInvite{}
	var expiresAt sql.NullTime
	err := q.DB.QueryRow(
		`SELECT id, group_id, code, creator_id, max_uses, uses, expires_at, created_at
		 FROM group_invites WHERE code = ?`, code,
	).Scan(&inv.ID, &inv.GroupID, &inv.Code, &inv.CreatorID, &inv.MaxUses, &inv.Uses, &expiresAt, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		inv.ExpiresAt = &expiresAt.Time
	}
	return inv, nil
}

func (q *GroupQueries) IncrementInviteUses(id string) error {
	_, err := q.DB.Exec("UPDATE group_invites SET uses = uses + 1 WHERE id = ?", id)
	return err
}

func (q *GroupQueries) ListInvites(groupID string) ([]models.GroupInvite, error) {
	rows, err := q.DB.Query(
		`SELECT id, group_id, code, creator_id, max_uses, uses, expires_at, created_at
		 FROM group_invites WHERE group_id = ? ORDER BY created_at DESC`, groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []models.GroupInvite
	for rows.Next() {
		var inv models.GroupInvite
		var expiresAt sql.NullTime
		if err := rows.Scan(&inv.ID, &inv.GroupID, &inv.Code, &inv.CreatorID, &inv.MaxUses, &inv.Uses, &expiresAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			inv.ExpiresAt = &expiresAt.Time
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

func (q *GroupQueries) DeleteInvite(id string) error {
	_, err := q.DB.Exec("DELETE FROM group_invites WHERE id = ?", id)
	return err
}

// DeleteAllInvites removes all invites for a group.
func (q *GroupQueries) DeleteAllInvites(groupID string) error {
	_, err := q.DB.Exec("DELETE FROM group_invites WHERE group_id = ?", groupID)
	return err
}

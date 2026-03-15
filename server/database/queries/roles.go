package queries

import (
	"database/sql"
	"nora/models"
)

type RoleQueries struct {
	DB *sql.DB
}

func (q *RoleQueries) Create(role *models.Role) error {
	// Automatically set position before everyone (999) — lower number = higher rank
	var maxPos sql.NullInt64
	q.DB.QueryRow(`SELECT MAX(position) FROM roles WHERE position < (SELECT position FROM roles WHERE id = 'everyone')`).Scan(&maxPos)
	if maxPos.Valid {
		role.Position = int(maxPos.Int64) + 1
	} else {
		role.Position = 1
	}

	_, err := q.DB.Exec(
		`INSERT INTO roles (id, name, permissions, position, color) VALUES (?, ?, ?, ?, ?)`,
		role.ID, role.Name, role.Permissions, role.Position, role.Color,
	)
	return err
}

func (q *RoleQueries) GetByID(id string) (*models.Role, error) {
	r := &models.Role{}
	err := q.DB.QueryRow(
		`SELECT id, name, permissions, position, color, created_at FROM roles WHERE id = ?`, id,
	).Scan(&r.ID, &r.Name, &r.Permissions, &r.Position, &r.Color, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (q *RoleQueries) List() ([]models.Role, error) {
	rows, err := q.DB.Query(
		`SELECT id, name, permissions, position, color, created_at FROM roles ORDER BY position`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []models.Role
	for rows.Next() {
		var r models.Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Permissions, &r.Position, &r.Color, &r.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

func (q *RoleQueries) Update(role *models.Role) error {
	_, err := q.DB.Exec(
		`UPDATE roles SET name = ?, permissions = ?, position = ?, color = ? WHERE id = ?`,
		role.Name, role.Permissions, role.Position, role.Color, role.ID,
	)
	return err
}

func (q *RoleQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM roles WHERE id = ?", id)
	return err
}

func (q *RoleQueries) AssignToUser(userID, roleID string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`,
		userID, roleID,
	)
	return err
}

func (q *RoleQueries) RemoveFromUser(userID, roleID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM user_roles WHERE user_id = ? AND role_id = ?`,
		userID, roleID,
	)
	return err
}

func (q *RoleQueries) GetUserPermissions(userID string) (int64, error) {
	// everyone role permissions + position
	var everyonePerms int64
	var everyonePos int
	q.DB.QueryRow(`SELECT COALESCE(permissions, 0), position FROM roles WHERE id = 'everyone'`).Scan(&everyonePerms, &everyonePos)

	// Bitwise OR across all assigned roles (SUM doesn't work for bitmasks)
	rows, err := q.DB.Query(
		`SELECT r.permissions, r.position FROM roles r
		 JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = ?`, userID,
	)
	if err != nil {
		return everyonePerms, nil
	}
	defer rows.Close()

	var perms int64
	hasBelowEveryone := false
	for rows.Next() {
		var p int64
		var pos int
		if err := rows.Scan(&p, &pos); err != nil {
			continue
		}
		perms |= p
		if pos > everyonePos {
			hasBelowEveryone = true
		}
	}
	// If user has a role below everyone, everyone perms are not added
	if hasBelowEveryone {
		return perms, nil
	}
	return everyonePerms | perms, nil
}

func (q *RoleQueries) GetUserRoles(userID string) ([]models.Role, error) {
	rows, err := q.DB.Query(
		`SELECT r.id, r.name, r.permissions, r.position, r.color, r.created_at
		 FROM roles r
		 JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = ?
		 ORDER BY r.position`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []models.Role
	for rows.Next() {
		var r models.Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Permissions, &r.Position, &r.Color, &r.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GetHighestPosition returns the lowest position number from the user's roles (= their highest rank).
// Owner returns -1 (above everything). User without roles returns MaxInt.
func (q *RoleQueries) GetHighestPosition(userID string, isOwner bool) (int, error) {
	if isOwner {
		return -1, nil
	}

	var pos sql.NullInt64
	err := q.DB.QueryRow(
		`SELECT MIN(r.position) FROM roles r
		 JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.user_id = ?`, userID,
	).Scan(&pos)
	if err != nil {
		return 1<<31 - 1, err
	}
	if !pos.Valid {
		return 1<<31 - 1, nil
	}
	return int(pos.Int64), nil
}

// SwapPositions swaps the position of two roles in a transaction.
// If one of the roles is "everyone", it doesn't swap — it moves the other role across the boundary.
func (q *RoleQueries) SwapPositions(roleID1, roleID2 string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Special case: moving across the everyone boundary
	if roleID1 == "everyone" || roleID2 == "everyone" {
		otherID := roleID1
		if roleID1 == "everyone" {
			otherID = roleID2
		}
		var otherPos, everyonePos int
		if err := tx.QueryRow(`SELECT position FROM roles WHERE id = ?`, otherID).Scan(&otherPos); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT position FROM roles WHERE id = 'everyone'`).Scan(&everyonePos); err != nil {
			return err
		}

		var newPos int
		if otherPos < everyonePos {
			// Move below everyone
			var maxBelow sql.NullInt64
			tx.QueryRow(`SELECT MAX(position) FROM roles WHERE position > ?`, everyonePos).Scan(&maxBelow)
			if maxBelow.Valid {
				newPos = int(maxBelow.Int64) + 1
			} else {
				newPos = everyonePos + 1
			}
		} else {
			// Move above everyone
			var maxAbove sql.NullInt64
			tx.QueryRow(`SELECT MAX(position) FROM roles WHERE position < ?`, everyonePos).Scan(&maxAbove)
			if maxAbove.Valid {
				newPos = int(maxAbove.Int64) + 1
			} else {
				newPos = 1
			}
		}
		if _, err := tx.Exec(`UPDATE roles SET position = ? WHERE id = ?`, newPos, otherID); err != nil {
			return err
		}
		return tx.Commit()
	}

	// Normal swap
	var pos1, pos2 int
	if err := tx.QueryRow(`SELECT position FROM roles WHERE id = ?`, roleID1).Scan(&pos1); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT position FROM roles WHERE id = ?`, roleID2).Scan(&pos2); err != nil {
		return err
	}

	if _, err := tx.Exec(`UPDATE roles SET position = ? WHERE id = ?`, pos2, roleID1); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE roles SET position = ? WHERE id = ?`, pos1, roleID2); err != nil {
		return err
	}

	return tx.Commit()
}

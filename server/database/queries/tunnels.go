package queries

import (
	"database/sql"
	"nora/models"
)

type TunnelQueries struct {
	DB *sql.DB
}

// Create creates a new tunnel request
func (q *TunnelQueries) Create(t *models.Tunnel) error {
	_, err := q.DB.Exec(
		`INSERT INTO tunnels (id, creator_id, target_id, status, creator_wg_pubkey, creator_ip)
		 VALUES (?, ?, ?, 'pending', ?, ?)`,
		t.ID, t.CreatorID, t.TargetID, t.CreatorWGPubKey, t.CreatorIP,
	)
	return err
}

// GetByID returns a tunnel with enriched username
func (q *TunnelQueries) GetByID(id string) (*models.Tunnel, error) {
	t := &models.Tunnel{}
	err := q.DB.QueryRow(
		`SELECT t.id, t.creator_id, t.target_id, t.status,
		        t.creator_wg_pubkey, t.target_wg_pubkey,
		        t.creator_ip, t.target_ip, t.created_at,
		        COALESCE(uc.username, ''), COALESCE(ut.username, '')
		 FROM tunnels t
		 LEFT JOIN users uc ON uc.id = t.creator_id
		 LEFT JOIN users ut ON ut.id = t.target_id
		 WHERE t.id = ?`, id,
	).Scan(&t.ID, &t.CreatorID, &t.TargetID, &t.Status,
		&t.CreatorWGPubKey, &t.TargetWGPubKey,
		&t.CreatorIP, &t.TargetIP, &t.CreatedAt,
		&t.CreatorName, &t.TargetName)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// GetByUser returns all tunnels for a user (as creator or target)
func (q *TunnelQueries) GetByUser(userID string) ([]models.Tunnel, error) {
	rows, err := q.DB.Query(
		`SELECT t.id, t.creator_id, t.target_id, t.status,
		        t.creator_wg_pubkey, t.target_wg_pubkey,
		        t.creator_ip, t.target_ip, t.created_at,
		        COALESCE(uc.username, ''), COALESCE(ut.username, '')
		 FROM tunnels t
		 LEFT JOIN users uc ON uc.id = t.creator_id
		 LEFT JOIN users ut ON ut.id = t.target_id
		 WHERE (t.creator_id = ? OR t.target_id = ?) AND t.status != 'closed'
		 ORDER BY t.created_at DESC`, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tunnels []models.Tunnel
	for rows.Next() {
		var t models.Tunnel
		if err := rows.Scan(&t.ID, &t.CreatorID, &t.TargetID, &t.Status,
			&t.CreatorWGPubKey, &t.TargetWGPubKey,
			&t.CreatorIP, &t.TargetIP, &t.CreatedAt,
			&t.CreatorName, &t.TargetName); err != nil {
			return nil, err
		}
		tunnels = append(tunnels, t)
	}
	return tunnels, rows.Err()
}

// Accept updates a tunnel to active with target WG pubkey and IP
func (q *TunnelQueries) Accept(id, targetWGPubKey, targetIP string) error {
	_, err := q.DB.Exec(
		`UPDATE tunnels SET status = 'active', target_wg_pubkey = ?, target_ip = ?
		 WHERE id = ? AND status = 'pending'`,
		targetWGPubKey, targetIP, id,
	)
	return err
}

// Close closes a tunnel
func (q *TunnelQueries) Close(id string) error {
	_, err := q.DB.Exec(`UPDATE tunnels SET status = 'closed' WHERE id = ?`, id)
	return err
}

// HasActiveTunnel checks if an active/pending tunnel exists between two users
func (q *TunnelQueries) HasActiveTunnel(userA, userB string) bool {
	var count int
	q.DB.QueryRow(
		`SELECT COUNT(*) FROM tunnels
		 WHERE status != 'closed'
		 AND ((creator_id = ? AND target_id = ?) OR (creator_id = ? AND target_id = ?))`,
		userA, userB, userB, userA,
	).Scan(&count)
	return count > 0
}

// GetActiveTunnelsForUser returns active tunnels where the user is creator or target
func (q *TunnelQueries) GetActiveTunnelsForUser(userID string) ([]models.Tunnel, error) {
	rows, err := q.DB.Query(
		`SELECT t.id, t.creator_id, t.target_id, t.status,
		        t.creator_wg_pubkey, t.target_wg_pubkey,
		        t.creator_ip, t.target_ip, t.created_at,
		        COALESCE(uc.username, ''), COALESCE(ut.username, '')
		 FROM tunnels t
		 LEFT JOIN users uc ON uc.id = t.creator_id
		 LEFT JOIN users ut ON ut.id = t.target_id
		 WHERE (t.creator_id = ? OR t.target_id = ?) AND t.status = 'active'`,
		userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tunnels []models.Tunnel
	for rows.Next() {
		var t models.Tunnel
		if err := rows.Scan(&t.ID, &t.CreatorID, &t.TargetID, &t.Status,
			&t.CreatorWGPubKey, &t.TargetWGPubKey,
			&t.CreatorIP, &t.TargetIP, &t.CreatedAt,
			&t.CreatorName, &t.TargetName); err != nil {
			return nil, err
		}
		tunnels = append(tunnels, t)
	}
	return tunnels, rows.Err()
}

// Delete removes a tunnel (for cleanup)
func (q *TunnelQueries) Delete(id string) error {
	_, err := q.DB.Exec(`DELETE FROM tunnels WHERE id = ?`, id)
	return err
}

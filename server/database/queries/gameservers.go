package queries

import (
	"database/sql"
	"nora/models"
)

type GameServerQueries struct {
	DB *sql.DB
}

func (q *GameServerQueries) Create(gs *models.GameServerInstance) error {
	_, err := q.DB.Exec(
		`INSERT INTO game_servers (id, name, status, container_id, creator_id, error_msg, access_mode)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		gs.ID, gs.Name, gs.Status, gs.ContainerID, gs.CreatorID, gs.ErrorMsg, gs.AccessMode,
	)
	return err
}

func (q *GameServerQueries) GetAll() ([]models.GameServerInstance, error) {
	rows, err := q.DB.Query(
		`SELECT id, name, status, container_id, creator_id, created_at, error_msg, access_mode
		 FROM game_servers ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []models.GameServerInstance
	for rows.Next() {
		var gs models.GameServerInstance
		if err := rows.Scan(&gs.ID, &gs.Name, &gs.Status, &gs.ContainerID, &gs.CreatorID, &gs.CreatedAt, &gs.ErrorMsg, &gs.AccessMode); err != nil {
			return nil, err
		}
		servers = append(servers, gs)
	}
	return servers, rows.Err()
}

func (q *GameServerQueries) GetByID(id string) (*models.GameServerInstance, error) {
	gs := &models.GameServerInstance{}
	err := q.DB.QueryRow(
		`SELECT id, name, status, container_id, creator_id, created_at, error_msg, access_mode
		 FROM game_servers WHERE id = ?`, id,
	).Scan(&gs.ID, &gs.Name, &gs.Status, &gs.ContainerID, &gs.CreatorID, &gs.CreatedAt, &gs.ErrorMsg, &gs.AccessMode)
	if err != nil {
		return nil, err
	}
	return gs, nil
}

func (q *GameServerQueries) UpdateStatus(id, status, containerID, errorMsg string) error {
	_, err := q.DB.Exec(
		`UPDATE game_servers SET status = ?, container_id = ?, error_msg = ? WHERE id = ?`,
		status, containerID, errorMsg, id,
	)
	return err
}

func (q *GameServerQueries) UpdateAccessMode(id, mode string) error {
	_, err := q.DB.Exec(`UPDATE game_servers SET access_mode = ? WHERE id = ?`, mode, id)
	return err
}

func (q *GameServerQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM game_servers WHERE id = ?", id)
	return err
}

// JoinMember adds a user to a game server room
func (q *GameServerQueries) JoinMember(gsID, userID string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO game_server_members (game_server_id, user_id) VALUES (?, ?)`,
		gsID, userID,
	)
	return err
}

// LeaveMember removes a user from a game server room
func (q *GameServerQueries) LeaveMember(gsID, userID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM game_server_members WHERE game_server_id = ? AND user_id = ?`,
		gsID, userID,
	)
	return err
}

// IsMember returns true if the user is a member of the room
func (q *GameServerQueries) IsMember(gsID, userID string) bool {
	var count int
	q.DB.QueryRow(
		`SELECT COUNT(*) FROM game_server_members WHERE game_server_id = ? AND user_id = ?`,
		gsID, userID,
	).Scan(&count)
	return count > 0
}

// GetMembers returns the list of game server room members
func (q *GameServerQueries) GetMembers(gsID string) ([]models.GameServerMember, error) {
	rows, err := q.DB.Query(
		`SELECT m.game_server_id, m.user_id, u.username, m.joined_at
		 FROM game_server_members m JOIN users u ON m.user_id = u.id
		 WHERE m.game_server_id = ? ORDER BY m.joined_at`, gsID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.GameServerMember
	for rows.Next() {
		var m models.GameServerMember
		if err := rows.Scan(&m.GameServerID, &m.UserID, &m.Username, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetMemberIPs returns IP addresses of room members (for iptables allowlist)
func (q *GameServerQueries) GetMemberIPs(gsID string) ([]string, error) {
	rows, err := q.DB.Query(
		`SELECT DISTINCT u.last_ip FROM game_server_members m
		 JOIN users u ON m.user_id = u.id
		 WHERE m.game_server_id = ? AND u.last_ip != ''`, gsID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, rows.Err()
}

// RemoveAllMembers deletes all members from a room
func (q *GameServerQueries) RemoveAllMembers(gsID string) error {
	_, err := q.DB.Exec(`DELETE FROM game_server_members WHERE game_server_id = ?`, gsID)
	return err
}

// GetRunningRoomServers returns running servers with access_mode="room"
func (q *GameServerQueries) GetRunningRoomServers() ([]models.GameServerInstance, error) {
	rows, err := q.DB.Query(
		`SELECT id, name, status, container_id, creator_id, created_at, error_msg, access_mode
		 FROM game_servers WHERE status = 'running' AND access_mode = 'room'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []models.GameServerInstance
	for rows.Next() {
		var gs models.GameServerInstance
		if err := rows.Scan(&gs.ID, &gs.Name, &gs.Status, &gs.ContainerID, &gs.CreatorID, &gs.CreatedAt, &gs.ErrorMsg, &gs.AccessMode); err != nil {
			return nil, err
		}
		servers = append(servers, gs)
	}
	return servers, rows.Err()
}

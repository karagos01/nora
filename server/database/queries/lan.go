package queries

import (
	"database/sql"
	"fmt"
	"nora/models"
)

type LANQueries struct {
	DB *sql.DB
}

func (q *LANQueries) CreateParty(p *models.LANParty) error {
	_, err := q.DB.Exec(
		`INSERT INTO lan_parties (id, name, creator_id, active)
		 VALUES (?, ?, ?, 1)`,
		p.ID, p.Name, p.CreatorID,
	)
	return err
}

func (q *LANQueries) GetParty(id string) (*models.LANParty, error) {
	p := &models.LANParty{}
	var active int
	err := q.DB.QueryRow(
		`SELECT id, name, creator_id, active, created_at
		 FROM lan_parties WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.CreatorID, &active, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.Active = active == 1
	return p, nil
}

func (q *LANQueries) GetActiveParties() ([]models.LANParty, error) {
	rows, err := q.DB.Query(
		`SELECT id, name, creator_id, active, created_at
		 FROM lan_parties WHERE active = 1 ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var parties []models.LANParty
	for rows.Next() {
		var p models.LANParty
		var active int
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatorID, &active, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Active = active == 1
		parties = append(parties, p)
	}
	return parties, rows.Err()
}

func (q *LANQueries) DeactivateParty(id string) error {
	_, err := q.DB.Exec("UPDATE lan_parties SET active = 0 WHERE id = ?", id)
	return err
}

func (q *LANQueries) AddMember(m *models.LANPartyMember) error {
	_, err := q.DB.Exec(
		`INSERT INTO lan_party_members (id, party_id, user_id, public_key, assigned_ip)
		 VALUES (?, ?, ?, ?, ?)`,
		m.ID, m.PartyID, m.UserID, m.PublicKey, m.AssignedIP,
	)
	return err
}

func (q *LANQueries) RemoveMember(partyID, userID string) error {
	_, err := q.DB.Exec(
		"DELETE FROM lan_party_members WHERE party_id = ? AND user_id = ?",
		partyID, userID,
	)
	return err
}

func (q *LANQueries) GetMembers(partyID string) ([]models.LANPartyMember, error) {
	rows, err := q.DB.Query(
		`SELECT m.id, m.party_id, m.user_id, m.public_key, m.assigned_ip, m.joined_at, u.username
		 FROM lan_party_members m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.party_id = ?
		 ORDER BY m.joined_at`, partyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.LANPartyMember
	for rows.Next() {
		var m models.LANPartyMember
		if err := rows.Scan(&m.ID, &m.PartyID, &m.UserID, &m.PublicKey, &m.AssignedIP, &m.JoinedAt, &m.Username); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (q *LANQueries) GetMemberByUser(partyID, userID string) (*models.LANPartyMember, error) {
	m := &models.LANPartyMember{}
	err := q.DB.QueryRow(
		`SELECT m.id, m.party_id, m.user_id, m.public_key, m.assigned_ip, m.joined_at, u.username
		 FROM lan_party_members m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.party_id = ? AND m.user_id = ?`, partyID, userID,
	).Scan(&m.ID, &m.PartyID, &m.UserID, &m.PublicKey, &m.AssignedIP, &m.JoinedAt, &m.Username)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetUserPeer finds an existing WG peer of the user (any active party)
func (q *LANQueries) GetUserPeer(userID string) (*models.LANPartyMember, error) {
	m := &models.LANPartyMember{}
	err := q.DB.QueryRow(
		`SELECT m.id, m.party_id, m.user_id, m.public_key, m.assigned_ip, m.joined_at, u.username
		 FROM lan_party_members m
		 JOIN users u ON u.id = m.user_id
		 JOIN lan_parties p ON p.id = m.party_id
		 WHERE m.user_id = ? AND p.active = 1
		 LIMIT 1`, userID,
	).Scan(&m.ID, &m.PartyID, &m.UserID, &m.PublicKey, &m.AssignedIP, &m.JoinedAt, &m.Username)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetAllActivePeers returns all peers from all active parties (for WG recovery)
func (q *LANQueries) GetAllActivePeers() ([]models.LANPartyMember, error) {
	rows, err := q.DB.Query(
		`SELECT DISTINCT m.public_key, m.assigned_ip
		 FROM lan_party_members m
		 JOIN lan_parties p ON p.id = m.party_id
		 WHERE p.active = 1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.LANPartyMember
	for rows.Next() {
		var m models.LANPartyMember
		if err := rows.Scan(&m.PublicKey, &m.AssignedIP); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// IsUserInOtherParty checks if the user is a member of another active party
func (q *LANQueries) IsUserInOtherParty(partyID, userID string) (bool, error) {
	var count int
	err := q.DB.QueryRow(
		`SELECT COUNT(*) FROM lan_party_members m
		 JOIN lan_parties p ON p.id = m.party_id
		 WHERE m.user_id = ? AND m.party_id != ? AND p.active = 1`,
		userID, partyID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetNextIPSimple returns the global next IP
func (q *LANQueries) GetNextIPSimple() (int, error) {
	rows, err := q.DB.Query(
		`SELECT m.assigned_ip FROM lan_party_members m
		 JOIN lan_parties p ON p.id = m.party_id
		 WHERE p.active = 1`,
	)
	if err != nil {
		return 2, err
	}
	defer rows.Close()

	maxHost := 1
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			continue
		}
		var a, b, c, d int
		fmt.Sscanf(ip, "%d.%d.%d.%d", &a, &b, &c, &d)
		if d > maxHost {
			maxHost = d
		}
	}
	return maxHost + 1, rows.Err()
}

// GetAllActiveMemberUserIDs returns unique user IDs from all active LAN parties
func (q *LANQueries) GetAllActiveMemberUserIDs() ([]string, error) {
	rows, err := q.DB.Query(
		`SELECT DISTINCT m.user_id FROM lan_party_members m
		 JOIN lan_parties p ON p.id = m.party_id
		 WHERE p.active = 1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q *LANQueries) FormatIP(subnet string, hostPart int) string {
	var a, b, c int
	fmt.Sscanf(subnet, "%d.%d.%d.", &a, &b, &c)
	return fmt.Sprintf("%d.%d.%d.%d", a, b, c, hostPart)
}

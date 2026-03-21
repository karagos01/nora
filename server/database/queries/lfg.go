package queries

import (
	"database/sql"
	"fmt"
	"nora/models"
	"time"
)

type LFGQueries struct {
	DB *sql.DB
}

// ListByChannel returns active (non-expired) listings for a channel, newest first.
func (q *LFGQueries) ListByChannel(channelID string) ([]models.LFGListing, error) {
	rows, err := q.DB.Query(`
		SELECT l.id, l.user_id, l.channel_id, l.group_id, l.game_name, l.content, l.max_players, l.created_at, l.expires_at,
		       u.id, u.username, u.display_name, u.avatar_url
		FROM lfg_listings l
		JOIN users u ON u.id = l.user_id
		WHERE l.channel_id = ? AND l.expires_at > datetime('now')
		ORDER BY l.created_at DESC`,
		channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var listings []models.LFGListing
	for rows.Next() {
		var l models.LFGListing
		l.Author = &models.User{}
		if err := rows.Scan(
			&l.ID, &l.UserID, &l.ChannelID, &l.GroupID, &l.GameName, &l.Content, &l.MaxPlayers, &l.CreatedAt, &l.ExpiresAt,
			&l.Author.ID, &l.Author.Username, &l.Author.DisplayName, &l.Author.AvatarURL,
		); err != nil {
			return nil, err
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load participants + applications for all listings
	for i := range listings {
		participants, _ := q.GetParticipants(listings[i].ID)
		listings[i].Participants = participants
		apps, _ := q.GetApplications(listings[i].ID)
		listings[i].Applications = apps
	}
	return listings, nil
}

// Create inserts a new LFG listing.
func (q *LFGQueries) Create(listing *models.LFGListing) error {
	_, err := q.DB.Exec(
		`INSERT INTO lfg_listings (id, user_id, channel_id, group_id, game_name, content, max_players, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		listing.ID, listing.UserID, listing.ChannelID, listing.GroupID, listing.GameName, listing.Content,
		listing.MaxPlayers, listing.CreatedAt, listing.ExpiresAt,
	)
	return err
}

// Delete removes a listing owned by the given user.
func (q *LFGQueries) Delete(id, userID string) error {
	res, err := q.DB.Exec(`DELETE FROM lfg_listings WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteByID removes a listing by ID regardless of author (admin delete).
func (q *LFGQueries) DeleteByID(id string) error {
	_, err := q.DB.Exec(`DELETE FROM lfg_listings WHERE id = ?`, id)
	return err
}

// GetByID returns a single listing by ID.
func (q *LFGQueries) GetByID(id string) (*models.LFGListing, error) {
	var l models.LFGListing
	err := q.DB.QueryRow(
		`SELECT id, user_id, channel_id, group_id, game_name, content, max_players, created_at, expires_at FROM lfg_listings WHERE id = ?`,
		id,
	).Scan(&l.ID, &l.UserID, &l.ChannelID, &l.GroupID, &l.GameName, &l.Content, &l.MaxPlayers, &l.CreatedAt, &l.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Join adds a participant to a listing atomically (checks capacity).
func (q *LFGQueries) Join(listingID, userID string, maxPlayers int) error {
	if maxPlayers > 0 {
		// Atomic check-and-insert within a single statement
		res, err := q.DB.Exec(`
			INSERT OR IGNORE INTO lfg_participants (listing_id, user_id, joined_at)
			SELECT ?, ?, ?
			WHERE (SELECT COUNT(*) FROM lfg_participants WHERE listing_id = ?) < ?`,
			listingID, userID, time.Now().UTC(), listingID, maxPlayers,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("listing full or already joined")
		}
		return nil
	}
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO lfg_participants (listing_id, user_id, joined_at) VALUES (?, ?, ?)`,
		listingID, userID, time.Now().UTC(),
	)
	return err
}

// Leave removes a participant from a listing.
func (q *LFGQueries) Leave(listingID, userID string) error {
	_, err := q.DB.Exec(`DELETE FROM lfg_participants WHERE listing_id = ? AND user_id = ?`, listingID, userID)
	return err
}

// GetParticipants returns users who joined a listing.
func (q *LFGQueries) GetParticipants(listingID string) ([]models.User, error) {
	rows, err := q.DB.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url
		FROM lfg_participants p
		JOIN users u ON u.id = p.user_id
		WHERE p.listing_id = ?
		ORDER BY p.joined_at`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ParticipantCount returns the number of participants.
func (q *LFGQueries) ParticipantCount(listingID string) int {
	var n int
	q.DB.QueryRow(`SELECT COUNT(*) FROM lfg_participants WHERE listing_id = ?`, listingID).Scan(&n)
	return n
}

// Apply creates a pending application.
func (q *LFGQueries) Apply(listingID, userID, message string) error {
	_, err := q.DB.Exec(
		`INSERT OR REPLACE INTO lfg_applications (listing_id, user_id, message, status, created_at) VALUES (?, ?, ?, 'pending', ?)`,
		listingID, userID, message, time.Now().UTC(),
	)
	return err
}

// SetApplicationStatus atomically updates an application status. Returns error if not pending.
func (q *LFGQueries) SetApplicationStatus(listingID, userID, status string) error {
	res, err := q.DB.Exec(
		`UPDATE lfg_applications SET status = ? WHERE listing_id = ? AND user_id = ? AND status = 'pending'`,
		status, listingID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("application not pending")
	}
	return nil
}

// GetApplications returns all applications for a listing with user info.
func (q *LFGQueries) GetApplications(listingID string) ([]models.LFGApplication, error) {
	rows, err := q.DB.Query(`
		SELECT a.listing_id, a.user_id, a.message, a.status, a.created_at,
		       u.id, u.username, u.display_name, u.avatar_url
		FROM lfg_applications a
		JOIN users u ON u.id = a.user_id
		WHERE a.listing_id = ?
		ORDER BY a.created_at`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var apps []models.LFGApplication
	for rows.Next() {
		var a models.LFGApplication
		var u models.User
		if err := rows.Scan(&a.ListingID, &a.UserID, &a.Message, &a.Status, &a.CreatedAt,
			&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL); err != nil {
			return nil, err
		}
		a.User = &u
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

// CleanupExpired deletes all expired listings. Groups are kept alive. Returns listing count deleted.
func (q *LFGQueries) CleanupExpired() (int64, error) {
	res, err := q.DB.Exec(`DELETE FROM lfg_listings WHERE expires_at <= datetime('now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

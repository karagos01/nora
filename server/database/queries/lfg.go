package queries

import (
	"database/sql"
	"nora/models"
)

type LFGQueries struct {
	DB *sql.DB
}

// ListByChannel returns active (non-expired) listings for a channel, newest first.
func (q *LFGQueries) ListByChannel(channelID string) ([]models.LFGListing, error) {
	rows, err := q.DB.Query(`
		SELECT l.id, l.user_id, l.channel_id, l.game_name, l.content, l.created_at, l.expires_at,
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
			&l.ID, &l.UserID, &l.ChannelID, &l.GameName, &l.Content, &l.CreatedAt, &l.ExpiresAt,
			&l.Author.ID, &l.Author.Username, &l.Author.DisplayName, &l.Author.AvatarURL,
		); err != nil {
			return nil, err
		}
		listings = append(listings, l)
	}
	return listings, rows.Err()
}

// Create inserts a new LFG listing.
func (q *LFGQueries) Create(listing *models.LFGListing) error {
	_, err := q.DB.Exec(
		`INSERT INTO lfg_listings (id, user_id, channel_id, game_name, content, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		listing.ID, listing.UserID, listing.ChannelID, listing.GameName, listing.Content,
		listing.CreatedAt, listing.ExpiresAt,
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
		`SELECT id, user_id, channel_id, game_name, content, created_at, expires_at FROM lfg_listings WHERE id = ?`,
		id,
	).Scan(&l.ID, &l.UserID, &l.ChannelID, &l.GameName, &l.Content, &l.CreatedAt, &l.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// CleanupExpired deletes all expired listings and returns the count.
func (q *LFGQueries) CleanupExpired() (int64, error) {
	res, err := q.DB.Exec(`DELETE FROM lfg_listings WHERE expires_at <= datetime('now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

package queries

import "database/sql"

type ProfileQueries struct {
	DB *sql.DB
}

type UserStats struct {
	MessageCount int
	UploadCount  int
	UploadSizeMB float64
	ChannelStats []ProfileChannelStat
}

type ProfileChannelStat struct {
	ChannelID    string
	ChannelName  string
	MessageCount int
}

func (q *ProfileQueries) GetUserStats(userID string) (*UserStats, error) {
	stats := &UserStats{}

	q.DB.QueryRow("SELECT COUNT(*) FROM messages WHERE user_id = ?", userID).Scan(&stats.MessageCount)

	var uploadSize int64
	q.DB.QueryRow("SELECT COUNT(*), COALESCE(SUM(size), 0) FROM uploads WHERE user_id = ?", userID).Scan(&stats.UploadCount, &uploadSize)
	stats.UploadSizeMB = float64(uploadSize) / (1024 * 1024)

	rows, err := q.DB.Query(`
		SELECT m.channel_id, c.name, COUNT(*) as cnt
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE m.user_id = ?
		GROUP BY m.channel_id
		ORDER BY cnt DESC
		LIMIT 20`, userID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cs ProfileChannelStat
			if rows.Scan(&cs.ChannelID, &cs.ChannelName, &cs.MessageCount) == nil {
				stats.ChannelStats = append(stats.ChannelStats, cs)
			}
		}
	}

	return stats, nil
}

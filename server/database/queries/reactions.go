package queries

import (
	"database/sql"
	"nora/models"
	"strings"
)

type ReactionQueries struct {
	DB *sql.DB
}

func (q *ReactionQueries) Add(messageID, userID, emoji string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO reactions (message_id, user_id, emoji) VALUES (?, ?, ?)`,
		messageID, userID, emoji,
	)
	return err
}

func (q *ReactionQueries) Remove(messageID, userID, emoji string) error {
	_, err := q.DB.Exec(
		`DELETE FROM reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`,
		messageID, userID, emoji,
	)
	return err
}

func (q *ReactionQueries) Exists(messageID, userID, emoji string) (bool, error) {
	var n int
	err := q.DB.QueryRow(
		`SELECT 1 FROM reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`,
		messageID, userID, emoji,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (q *ReactionQueries) ListByMessageIDs(ids []string) (map[string][]models.Reaction, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `SELECT message_id, emoji, GROUP_CONCAT(user_id), COUNT(*) FROM reactions WHERE message_id IN (` +
		strings.Join(placeholders, ",") + `) GROUP BY message_id, emoji`

	rows, err := q.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]models.Reaction)
	for rows.Next() {
		var r models.Reaction
		var userIDsStr string
		if err := rows.Scan(&r.MessageID, &r.Emoji, &userIDsStr, &r.Count); err != nil {
			return nil, err
		}
		r.UserIDs = strings.Split(userIDsStr, ",")
		result[r.MessageID] = append(result[r.MessageID], r)
	}
	return result, rows.Err()
}

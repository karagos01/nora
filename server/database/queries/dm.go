package queries

import (
	"database/sql"
	"fmt"
	"nora/models"
)

type DMQueries struct {
	DB *sql.DB
}

func (q *DMQueries) CreateConversation(conv *models.DMConversation, participants []models.DMParticipant) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT INTO dm_conversations (id) VALUES (?)", conv.ID)
	if err != nil {
		return err
	}

	for _, p := range participants {
		_, err = tx.Exec(
			"INSERT INTO dm_participants (conversation_id, user_id, public_key) VALUES (?, ?, ?)",
			conv.ID, p.UserID, p.PublicKey,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (q *DMQueries) FindConversation(userID1, userID2 string) (*models.DMConversation, error) {
	conv := &models.DMConversation{}
	err := q.DB.QueryRow(
		`SELECT dc.id, dc.created_at FROM dm_conversations dc
		 WHERE dc.id IN (
			SELECT dp1.conversation_id FROM dm_participants dp1
			JOIN dm_participants dp2 ON dp1.conversation_id = dp2.conversation_id
			WHERE dp1.user_id = ? AND dp2.user_id = ?
		 )`, userID1, userID2,
	).Scan(&conv.ID, &conv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return conv, nil
}

func (q *DMQueries) ListConversations(userID string) ([]models.DMConversation, error) {
	rows, err := q.DB.Query(
		`SELECT dc.id, dc.created_at FROM dm_conversations dc
		 JOIN dm_participants dp ON dp.conversation_id = dc.id
		 WHERE dp.user_id = ?
		 ORDER BY dc.created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []models.DMConversation
	for rows.Next() {
		var c models.DMConversation
		if err := rows.Scan(&c.ID, &c.CreatedAt); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (q *DMQueries) GetParticipants(conversationID string) ([]models.DMParticipant, error) {
	rows, err := q.DB.Query(
		`SELECT conversation_id, user_id, public_key FROM dm_participants WHERE conversation_id = ?`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var parts []models.DMParticipant
	for rows.Next() {
		var p models.DMParticipant
		if err := rows.Scan(&p.ConversationID, &p.UserID, &p.PublicKey); err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

func (q *DMQueries) IsParticipant(conversationID, userID string) (bool, error) {
	var count int
	err := q.DB.QueryRow(
		"SELECT COUNT(*) FROM dm_participants WHERE conversation_id = ? AND user_id = ?",
		conversationID, userID,
	).Scan(&count)
	return count > 0, err
}

func (q *DMQueries) CreatePending(msg *models.DMPendingMessage) error {
	_, err := q.DB.Exec(
		`INSERT INTO dm_pending (id, conversation_id, sender_id, encrypted_content, reply_to_id) VALUES (?, ?, ?, ?, ?)`,
		msg.ID, msg.ConversationID, msg.SenderID, msg.EncryptedContent, msg.ReplyToID,
	)
	return err
}

func (q *DMQueries) ListPending(conversationID string) ([]models.DMPendingMessage, error) {
	rows, err := q.DB.Query(
		`SELECT m.id, m.conversation_id, m.sender_id, m.encrypted_content, m.reply_to_id, m.created_at,
		        u.id, u.username, u.display_name, u.public_key, u.avatar_url
		 FROM dm_pending m
		 JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id = ?
		 ORDER BY m.created_at ASC`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.DMPendingMessage
	for rows.Next() {
		var msg models.DMPendingMessage
		var author models.User
		if err := rows.Scan(
			&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.EncryptedContent, &msg.ReplyToID, &msg.CreatedAt,
			&author.ID, &author.Username, &author.DisplayName, &author.PublicKey, &author.AvatarURL,
		); err != nil {
			return nil, err
		}
		msg.Author = &author
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// PendingCount returns the number of pending messages in a conversation for a given recipient
// (messages sent by someone else that the recipient hasn't fetched yet).
func (q *DMQueries) PendingCount(conversationID, recipientID string) int {
	var count int
	q.DB.QueryRow(
		`SELECT COUNT(*) FROM dm_pending WHERE conversation_id = ? AND sender_id != ?`,
		conversationID, recipientID,
	).Scan(&count)
	return count
}

func (q *DMQueries) DeletePending(conversationID, recipientID string) error {
	// Delete pending messages in conversation that were not sent by the recipient (= those the recipient received)
	_, err := q.DB.Exec(
		`DELETE FROM dm_pending WHERE conversation_id = ? AND sender_id != ?`,
		conversationID, recipientID,
	)
	return err
}

func (q *DMQueries) DeleteConversation(id string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM dm_pending WHERE conversation_id = ?", id)
	tx.Exec("DELETE FROM dm_participants WHERE conversation_id = ?", id)
	_, err = tx.Exec("DELETE FROM dm_conversations WHERE id = ?", id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (q *DMQueries) CleanupOldPending(maxAgeDays int) (int64, error) {
	result, err := q.DB.Exec(
		`DELETE FROM dm_pending WHERE created_at < datetime('now', ? || ' days')`,
		fmt.Sprintf("-%d", maxAgeDays),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

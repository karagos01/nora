package queries

import (
	"database/sql"
	"fmt"
	"nora/models"
	"strings"
	"time"
)

// SearchFilters obsahuje rozšířené filtry pro vyhledávání zpráv.
type SearchFilters struct {
	FromUsername string // from:username — filtr podle autora
	HasImage    bool   // has:image — zprávy s obrázkovými přílohami
	HasLink     bool   // has:link — zprávy obsahující URL
	HasFile     bool   // has:file — zprávy s jakoukoliv přílohou
	Before      string // before:YYYY-MM-DD — zprávy před datem
	After       string // after:YYYY-MM-DD — zprávy po datu
}

type MessageQueries struct {
	DB *sql.DB
}

func (q *MessageQueries) Create(msg *models.Message) error {
	_, err := q.DB.Exec(
		`INSERT INTO messages (id, channel_id, user_id, content, reply_to_id) VALUES (?, ?, ?, ?, ?)`,
		msg.ID, msg.ChannelID, msg.UserID, msg.Content, msg.ReplyToID,
	)
	return err
}

func (q *MessageQueries) GetByID(id string) (*models.Message, error) {
	msg := &models.Message{}
	msg.Author = &models.User{}
	var updatedAt sql.NullTime
	var replyToID, pinnedBy, hiddenBy sql.NullString
	err := q.DB.QueryRow(
		`SELECT m.id, m.channel_id, m.user_id, m.content, m.created_at, m.updated_at, m.reply_to_id,
		        m.is_pinned, m.pinned_by, m.is_hidden, m.hidden_by, m.hidden_by_position,
		        u.id, u.username, u.display_name, u.public_key, u.avatar_url
		 FROM messages m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.id = ?`, id,
	).Scan(&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.CreatedAt, &updatedAt, &replyToID,
		&msg.IsPinned, &pinnedBy, &msg.IsHidden, &hiddenBy, &msg.HiddenByPosition,
		&msg.Author.ID, &msg.Author.Username, &msg.Author.DisplayName, &msg.Author.PublicKey, &msg.Author.AvatarURL)
	if err != nil {
		return nil, err
	}
	if updatedAt.Valid {
		msg.UpdatedAt = &updatedAt.Time
	}
	if replyToID.Valid {
		msg.ReplyToID = &replyToID.String
	}
	if pinnedBy.Valid {
		msg.PinnedBy = &pinnedBy.String
	}
	if hiddenBy.Valid {
		msg.HiddenBy = &hiddenBy.String
	}
	return msg, nil
}

// ListByChannel vrací zprávy s cursor-based paginací.
// before = ID zprávy, od které čteme dozadu (starší)
// limit = max počet zpráv
// callerPosition = position (rank) volajícího; skryté zprávy se vrátí vždy, ale obsah se vyčistí pokud callerPosition >= hidden_by_position
func (q *MessageQueries) ListByChannel(channelID, before string, limit, callerPosition int) ([]models.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `SELECT m.id, m.channel_id, m.user_id, m.content, m.created_at, m.updated_at, m.reply_to_id,
		        m.is_pinned, m.pinned_by, m.is_hidden, m.hidden_by, m.hidden_by_position,
		        u.id, u.username, u.display_name, u.public_key, u.avatar_url,
		        rm.id, rm.content, ru.id, ru.username, ru.display_name, ru.avatar_url
		 FROM messages m
		 JOIN users u ON u.id = m.user_id
		 LEFT JOIN messages rm ON rm.id = m.reply_to_id
		 LEFT JOIN users ru ON ru.id = rm.user_id
		 WHERE m.channel_id = ?`

	var rows *sql.Rows
	var err error

	if before == "" {
		rows, err = q.DB.Query(
			query+" ORDER BY m.created_at DESC LIMIT ?",
			channelID, limit,
		)
	} else {
		rows, err = q.DB.Query(
			query+" AND m.created_at < (SELECT created_at FROM messages WHERE id = ?) ORDER BY m.created_at DESC LIMIT ?",
			channelID, before, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		var author models.User
		var updatedAt sql.NullTime
		var replyToID, pinnedBy, hiddenBy, rmID, rmContent, ruID, ruUsername, ruDisplayName, ruAvatarURL sql.NullString
		if err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.CreatedAt, &updatedAt, &replyToID,
			&msg.IsPinned, &pinnedBy, &msg.IsHidden, &hiddenBy, &msg.HiddenByPosition,
			&author.ID, &author.Username, &author.DisplayName, &author.PublicKey, &author.AvatarURL,
			&rmID, &rmContent, &ruID, &ruUsername, &ruDisplayName, &ruAvatarURL,
		); err != nil {
			return nil, err
		}
		if updatedAt.Valid {
			msg.UpdatedAt = &updatedAt.Time
		}
		if replyToID.Valid {
			msg.ReplyToID = &replyToID.String
		}
		if pinnedBy.Valid {
			msg.PinnedBy = &pinnedBy.String
		}
		if hiddenBy.Valid {
			msg.HiddenBy = &hiddenBy.String
		}
		if rmID.Valid {
			msg.ReplyTo = &models.Message{
				ID:      rmID.String,
				Content: rmContent.String,
				Author: &models.User{
					ID:          ruID.String,
					Username:    ruUsername.String,
					DisplayName: ruDisplayName.String,
					AvatarURL:   ruAvatarURL.String,
				},
			}
		}
		msg.Author = &author
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stripHiddenContent(messages, callerPosition)
	return messages, nil
}

func (q *MessageQueries) Update(id, content string) error {
	_, err := q.DB.Exec(
		`UPDATE messages SET content = ?, updated_at = datetime('now') WHERE id = ?`,
		content, id,
	)
	return err
}

func (q *MessageQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM messages WHERE id = ?", id)
	return err
}

func (q *MessageQueries) SetPinned(id string, pinned bool, pinnedByID string) error {
	var pinnedBy *string
	if pinned {
		pinnedBy = &pinnedByID
	}
	_, err := q.DB.Exec(
		`UPDATE messages SET is_pinned = ?, pinned_by = ? WHERE id = ?`,
		pinned, pinnedBy, id,
	)
	return err
}

func (q *MessageQueries) ListPinned(channelID string) ([]models.Message, error) {
	rows, err := q.DB.Query(
		`SELECT m.id, m.channel_id, m.user_id, m.content, m.created_at, m.updated_at, m.reply_to_id,
		        m.is_pinned, m.pinned_by, m.is_hidden, m.hidden_by, m.hidden_by_position,
		        u.id, u.username, u.display_name, u.public_key, u.avatar_url
		 FROM messages m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.channel_id = ? AND m.is_pinned = 1
		 ORDER BY m.created_at DESC`, channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		var author models.User
		var updatedAt sql.NullTime
		var replyToID, pinnedBy, hiddenBy sql.NullString
		if err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.CreatedAt, &updatedAt, &replyToID,
			&msg.IsPinned, &pinnedBy, &msg.IsHidden, &hiddenBy, &msg.HiddenByPosition,
			&author.ID, &author.Username, &author.DisplayName, &author.PublicKey, &author.AvatarURL,
		); err != nil {
			return nil, err
		}
		if updatedAt.Valid {
			msg.UpdatedAt = &updatedAt.Time
		}
		if replyToID.Valid {
			msg.ReplyToID = &replyToID.String
		}
		if pinnedBy.Valid {
			msg.PinnedBy = &pinnedBy.String
		}
		if hiddenBy.Valid {
			msg.HiddenBy = &hiddenBy.String
		}
		msg.Author = &author
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// Search hledá zprávy v kanálu pomocí LIKE s offset paginací a rozšířenými filtry.
func (q *MessageQueries) Search(channelID, query string, limit, offset, callerPosition int, filters *SearchFilters) ([]models.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Sestavení dynamického SQL dotazu s filtry
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "m.channel_id = ?")
	args = append(args, channelID)

	conditions = append(conditions, "(m.is_hidden = 0 OR ? < m.hidden_by_position)")
	args = append(args, callerPosition)

	// Textové vyhledávání (pokud zůstal text po parsování filtrů)
	if query != "" {
		conditions = append(conditions, "m.content LIKE '%' || ? || '%'")
		args = append(args, query)
	}

	// Rozšířené filtry
	needAttachmentJoin := false
	if filters != nil {
		// from:username — filtr podle autora
		if filters.FromUsername != "" {
			conditions = append(conditions, "LOWER(u.username) = LOWER(?)")
			args = append(args, filters.FromUsername)
		}

		// has:image — zprávy s obrázkovými přílohami
		if filters.HasImage {
			needAttachmentJoin = true
			conditions = append(conditions, "(a.mime_type LIKE 'image/%')")
		}

		// has:file — zprávy s jakoukoliv přílohou
		if filters.HasFile {
			needAttachmentJoin = true
			conditions = append(conditions, "a.id IS NOT NULL")
		}

		// has:link — zprávy obsahující URL
		if filters.HasLink {
			conditions = append(conditions, "(m.content LIKE '%http://%' OR m.content LIKE '%https://%')")
		}

		// before:YYYY-MM-DD — zprávy před datem
		if filters.Before != "" {
			conditions = append(conditions, "m.created_at < ?")
			args = append(args, filters.Before+"T00:00:00Z")
		}

		// after:YYYY-MM-DD — zprávy po datu
		if filters.After != "" {
			conditions = append(conditions, "m.created_at >= ?")
			args = append(args, filters.After+"T00:00:00Z")
		}
	}

	// Sestavení JOIN klauzulí
	joins := `JOIN users u ON u.id = m.user_id
		 LEFT JOIN messages rm ON rm.id = m.reply_to_id
		 LEFT JOIN users ru ON ru.id = rm.user_id`
	if needAttachmentJoin {
		joins += "\n\t\t LEFT JOIN attachments a ON a.message_id = m.id"
	}

	// DISTINCT aby se zpráva neopakovala při více přílohách
	distinct := ""
	if needAttachmentJoin {
		distinct = "DISTINCT "
	}

	stmt := fmt.Sprintf(
		`SELECT %sm.id, m.channel_id, m.user_id, m.content, m.created_at, m.updated_at, m.reply_to_id,
		        m.is_pinned, m.pinned_by, m.is_hidden, m.hidden_by, m.hidden_by_position,
		        u.id, u.username, u.display_name, u.public_key, u.avatar_url,
		        rm.id, rm.content, ru.id, ru.username, ru.display_name, ru.avatar_url
		 FROM messages m
		 %s
		 WHERE %s
		 ORDER BY m.created_at DESC
		 LIMIT ? OFFSET ?`,
		distinct, joins, strings.Join(conditions, " AND "),
	)
	args = append(args, limit, offset)

	rows, err := q.DB.Query(stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		var author models.User
		var updatedAt sql.NullTime
		var replyToID, pinnedBy, hiddenBy, rmID, rmContent, ruID, ruUsername, ruDisplayName, ruAvatarURL sql.NullString
		if err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.CreatedAt, &updatedAt, &replyToID,
			&msg.IsPinned, &pinnedBy, &msg.IsHidden, &hiddenBy, &msg.HiddenByPosition,
			&author.ID, &author.Username, &author.DisplayName, &author.PublicKey, &author.AvatarURL,
			&rmID, &rmContent, &ruID, &ruUsername, &ruDisplayName, &ruAvatarURL,
		); err != nil {
			return nil, err
		}
		if updatedAt.Valid {
			msg.UpdatedAt = &updatedAt.Time
		}
		if replyToID.Valid {
			msg.ReplyToID = &replyToID.String
		}
		if pinnedBy.Valid {
			msg.PinnedBy = &pinnedBy.String
		}
		if hiddenBy.Valid {
			msg.HiddenBy = &hiddenBy.String
		}
		if rmID.Valid {
			msg.ReplyTo = &models.Message{
				ID:      rmID.String,
				Content: rmContent.String,
				Author: &models.User{
					ID:          ruID.String,
					Username:    ruUsername.String,
					DisplayName: ruDisplayName.String,
					AvatarURL:   ruAvatarURL.String,
				},
			}
		}
		msg.Author = &author
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (q *MessageQueries) GetLastMessageTime(channelID, userID string) (time.Time, error) {
	var t time.Time
	err := q.DB.QueryRow(
		`SELECT created_at FROM messages WHERE channel_id = ? AND user_id = ? ORDER BY created_at DESC LIMIT 1`,
		channelID, userID,
	).Scan(&t)
	return t, err
}

// ListReplies vrátí odpovědi na zprávu (thread).
func (q *MessageQueries) ListReplies(parentID string, callerPosition int) ([]models.Message, error) {
	rows, err := q.DB.Query(
		`SELECT m.id, m.channel_id, m.user_id, m.content, m.created_at, m.updated_at, m.reply_to_id,
		        m.is_pinned, m.pinned_by, m.is_hidden, m.hidden_by, m.hidden_by_position,
		        u.id, u.username, u.display_name, u.public_key, u.avatar_url
		 FROM messages m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.reply_to_id = ?
		 ORDER BY m.created_at ASC`,
		parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var msg models.Message
		var author models.User
		var updatedAt sql.NullTime
		var replyToID, pinnedBy, hiddenBy sql.NullString
		if err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &msg.Content, &msg.CreatedAt, &updatedAt, &replyToID,
			&msg.IsPinned, &pinnedBy, &msg.IsHidden, &hiddenBy, &msg.HiddenByPosition,
			&author.ID, &author.Username, &author.DisplayName, &author.PublicKey, &author.AvatarURL,
		); err != nil {
			return nil, err
		}
		if updatedAt.Valid {
			msg.UpdatedAt = &updatedAt.Time
		}
		if replyToID.Valid {
			msg.ReplyToID = &replyToID.String
		}
		if pinnedBy.Valid {
			msg.PinnedBy = &pinnedBy.String
		}
		if hiddenBy.Valid {
			msg.HiddenBy = &hiddenBy.String
		}
		msg.Author = &author
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stripHiddenContent(messages, callerPosition)
	return messages, nil
}

// BatchCountReplies spočítá odpovědi pro dané zprávy.
func (q *MessageQueries) BatchCountReplies(ids []string) (map[string]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `SELECT reply_to_id, COUNT(*) FROM messages WHERE reply_to_id IN (` + strings.Join(placeholders, ",") + `) GROUP BY reply_to_id`
	rows, err := q.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var parentID string
		var count int
		if err := rows.Scan(&parentID, &count); err != nil {
			return nil, err
		}
		result[parentID] = count
	}
	return result, rows.Err()
}

// SaveEditHistory uloží starý obsah zprávy do historie editací.
func (q *MessageQueries) SaveEditHistory(messageID, oldContent, editorID string) error {
	_, err := q.DB.Exec(
		`INSERT INTO message_edits (message_id, old_content, edited_by) VALUES (?, ?, ?)`,
		messageID, oldContent, editorID,
	)
	return err
}

// GetEditHistory vrátí historii editací zprávy (nejnovější první).
func (q *MessageQueries) GetEditHistory(messageID string) ([]models.MessageEdit, error) {
	rows, err := q.DB.Query(
		`SELECT id, message_id, old_content, edited_at, edited_by FROM message_edits WHERE message_id = ? ORDER BY edited_at DESC`,
		messageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edits []models.MessageEdit
	for rows.Next() {
		var e models.MessageEdit
		if err := rows.Scan(&e.ID, &e.MessageID, &e.OldContent, &e.EditedAt, &e.EditedBy); err != nil {
			return nil, err
		}
		edits = append(edits, e)
	}
	return edits, rows.Err()
}

func (q *MessageQueries) GetOwnerID(id string) (string, error) {
	var userID string
	err := q.DB.QueryRow("SELECT user_id FROM messages WHERE id = ?", id).Scan(&userID)
	return userID, err
}

func (q *MessageQueries) SetHidden(id string, hidden bool, hiddenBy string, hiddenByPosition int) error {
	var hb *string
	var pos int
	if hidden {
		hb = &hiddenBy
		pos = hiddenByPosition
	}
	_, err := q.DB.Exec(
		`UPDATE messages SET is_hidden = ?, hidden_by = ?, hidden_by_position = ? WHERE id = ?`,
		hidden, hb, pos, id,
	)
	return err
}

func (q *MessageQueries) HideByUserID(userID, hiddenBy string, hiddenByPosition int) error {
	_, err := q.DB.Exec(
		`UPDATE messages SET is_hidden = 1, hidden_by = ?, hidden_by_position = ? WHERE user_id = ?`,
		hiddenBy, hiddenByPosition, userID,
	)
	return err
}

func (q *MessageQueries) DeleteByUserID(userID string) error {
	_, err := q.DB.Exec("DELETE FROM messages WHERE user_id = ?", userID)
	return err
}

// stripHiddenContent vyčistí obsah skrytých zpráv pro uživatele bez oprávnění.
// Zpráva zůstane ve výpisu (uživatel vidí placeholder), ale obsah a přílohy se odstraní.
func stripHiddenContent(messages []models.Message, callerPosition int) {
	for i := range messages {
		if messages[i].IsHidden && callerPosition >= messages[i].HiddenByPosition {
			messages[i].Content = ""
			messages[i].Attachments = nil
			messages[i].Poll = nil
			messages[i].LinkPreview = nil
		}
	}
}

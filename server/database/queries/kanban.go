package queries

import (
	"database/sql"
	"nora/models"
	"time"
)

type KanbanQueries struct {
	DB *sql.DB
}

// Boards

func (q *KanbanQueries) CreateBoard(b *models.KanbanBoard) error {
	_, err := q.DB.Exec(
		`INSERT INTO kanban_boards (id, name, description, creator_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		b.ID, b.Name, b.Description, b.CreatorID, b.CreatedAt,
	)
	return err
}

func (q *KanbanQueries) ListBoards() ([]models.KanbanBoard, error) {
	rows, err := q.DB.Query(`SELECT id, name, description, creator_id, created_at FROM kanban_boards ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var boards []models.KanbanBoard
	for rows.Next() {
		var b models.KanbanBoard
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.CreatorID, &b.CreatedAt); err != nil {
			continue
		}
		boards = append(boards, b)
	}
	return boards, nil
}

func (q *KanbanQueries) GetBoard(id string) (*models.KanbanBoard, error) {
	var b models.KanbanBoard
	err := q.DB.QueryRow(
		`SELECT id, name, description, creator_id, created_at FROM kanban_boards WHERE id = ?`, id,
	).Scan(&b.ID, &b.Name, &b.Description, &b.CreatorID, &b.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Load columns
	colRows, err := q.DB.Query(
		`SELECT id, board_id, title, position, color, created_at FROM kanban_columns WHERE board_id = ? ORDER BY position`, id,
	)
	if err != nil {
		return &b, nil
	}
	defer colRows.Close()

	for colRows.Next() {
		var c models.KanbanColumn
		if err := colRows.Scan(&c.ID, &c.BoardID, &c.Title, &c.Position, &c.Color, &c.CreatedAt); err != nil {
			continue
		}
		b.Columns = append(b.Columns, c)
	}

	// Load cards for all columns + JOIN on users
	cardRows, err := q.DB.Query(`
		SELECT c.id, c.column_id, c.board_id, c.title, c.description, c.position,
		       c.assigned_to, c.created_by, c.color, c.due_date, c.created_at, c.updated_at,
		       au.id, au.username, au.display_name, au.avatar_url,
		       cu.id, cu.username, cu.display_name, cu.avatar_url
		FROM kanban_cards c
		LEFT JOIN users au ON c.assigned_to = au.id
		LEFT JOIN users cu ON c.created_by = cu.id
		WHERE c.board_id = ?
		ORDER BY c.position`, id,
	)
	if err != nil {
		return &b, nil
	}
	defer cardRows.Close()

	cardsByColumn := make(map[string][]models.KanbanCard)
	for cardRows.Next() {
		var card models.KanbanCard
		var assignedTo, auID, auUser, auDisplay, auAvatar sql.NullString
		var cuID, cuUser, cuDisplay, cuAvatar sql.NullString
		var dueDate sql.NullTime
		var updatedAt sql.NullTime

		if err := cardRows.Scan(
			&card.ID, &card.ColumnID, &card.BoardID, &card.Title, &card.Description, &card.Position,
			&assignedTo, &card.CreatedBy, &card.Color, &dueDate, &card.CreatedAt, &updatedAt,
			&auID, &auUser, &auDisplay, &auAvatar,
			&cuID, &cuUser, &cuDisplay, &cuAvatar,
		); err != nil {
			continue
		}

		if assignedTo.Valid {
			card.AssignedTo = &assignedTo.String
		}
		if dueDate.Valid {
			card.DueDate = &dueDate.Time
		}
		if updatedAt.Valid {
			card.UpdatedAt = &updatedAt.Time
		}
		if auID.Valid {
			card.AssignedUser = &models.User{
				ID:          auID.String,
				Username:    auUser.String,
				DisplayName: auDisplay.String,
				AvatarURL:   auAvatar.String,
			}
		}
		if cuID.Valid {
			card.Author = &models.User{
				ID:          cuID.String,
				Username:    cuUser.String,
				DisplayName: cuDisplay.String,
				AvatarURL:   cuAvatar.String,
			}
		}

		cardsByColumn[card.ColumnID] = append(cardsByColumn[card.ColumnID], card)
	}

	for i := range b.Columns {
		b.Columns[i].Cards = cardsByColumn[b.Columns[i].ID]
		if b.Columns[i].Cards == nil {
			b.Columns[i].Cards = []models.KanbanCard{}
		}
	}

	return &b, nil
}

func (q *KanbanQueries) DeleteBoard(id string) error {
	_, err := q.DB.Exec(`DELETE FROM kanban_boards WHERE id = ?`, id)
	return err
}

// Columns

func (q *KanbanQueries) CreateColumn(c *models.KanbanColumn) error {
	_, err := q.DB.Exec(
		`INSERT INTO kanban_columns (id, board_id, title, position, color, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.BoardID, c.Title, c.Position, c.Color, c.CreatedAt,
	)
	return err
}

func (q *KanbanQueries) UpdateColumn(id, title, color string) error {
	_, err := q.DB.Exec(`UPDATE kanban_columns SET title = ?, color = ? WHERE id = ?`, title, color, id)
	return err
}

func (q *KanbanQueries) ReorderColumns(boardID string, ids []string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE kanban_columns SET position = ? WHERE id = ? AND board_id = ?`, i, id, boardID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (q *KanbanQueries) DeleteColumn(id string) error {
	_, err := q.DB.Exec(`DELETE FROM kanban_columns WHERE id = ?`, id)
	return err
}

func (q *KanbanQueries) GetColumn(id string) (*models.KanbanColumn, error) {
	var c models.KanbanColumn
	err := q.DB.QueryRow(
		`SELECT id, board_id, title, position, color, created_at FROM kanban_columns WHERE id = ?`, id,
	).Scan(&c.ID, &c.BoardID, &c.Title, &c.Position, &c.Color, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (q *KanbanQueries) GetMaxColumnPosition(boardID string) int {
	var pos sql.NullInt64
	q.DB.QueryRow(`SELECT MAX(position) FROM kanban_columns WHERE board_id = ?`, boardID).Scan(&pos)
	if pos.Valid {
		return int(pos.Int64)
	}
	return -1
}

// Cards

func (q *KanbanQueries) CreateCard(c *models.KanbanCard) error {
	_, err := q.DB.Exec(
		`INSERT INTO kanban_cards (id, column_id, board_id, title, description, position, assigned_to, created_by, color, due_date, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ColumnID, c.BoardID, c.Title, c.Description, c.Position, c.AssignedTo, c.CreatedBy, c.Color, c.DueDate, c.CreatedAt,
	)
	return err
}

func (q *KanbanQueries) UpdateCard(c *models.KanbanCard) error {
	_, err := q.DB.Exec(
		`UPDATE kanban_cards SET title = ?, description = ?, assigned_to = ?, color = ?, due_date = ?, updated_at = ? WHERE id = ?`,
		c.Title, c.Description, c.AssignedTo, c.Color, c.DueDate, c.UpdatedAt, c.ID,
	)
	return err
}

func (q *KanbanQueries) MoveCard(cardID, targetColumnID string, position int) error {
	now := time.Now().UTC()
	_, err := q.DB.Exec(
		`UPDATE kanban_cards SET column_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		targetColumnID, position, now, cardID,
	)
	return err
}

func (q *KanbanQueries) DeleteCard(id string) error {
	_, err := q.DB.Exec(`DELETE FROM kanban_cards WHERE id = ?`, id)
	return err
}

func (q *KanbanQueries) GetCard(id string) (*models.KanbanCard, error) {
	var c models.KanbanCard
	var assignedTo sql.NullString
	var dueDate sql.NullTime
	var updatedAt sql.NullTime
	err := q.DB.QueryRow(
		`SELECT id, column_id, board_id, title, description, position, assigned_to, created_by, color, due_date, created_at, updated_at FROM kanban_cards WHERE id = ?`, id,
	).Scan(&c.ID, &c.ColumnID, &c.BoardID, &c.Title, &c.Description, &c.Position, &assignedTo, &c.CreatedBy, &c.Color, &dueDate, &c.CreatedAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if assignedTo.Valid {
		c.AssignedTo = &assignedTo.String
	}
	if dueDate.Valid {
		c.DueDate = &dueDate.Time
	}
	if updatedAt.Valid {
		c.UpdatedAt = &updatedAt.Time
	}
	return &c, nil
}

func (q *KanbanQueries) GetMaxCardPosition(columnID string) int {
	var pos sql.NullInt64
	q.DB.QueryRow(`SELECT MAX(position) FROM kanban_cards WHERE column_id = ?`, columnID).Scan(&pos)
	if pos.Valid {
		return int(pos.Int64)
	}
	return -1
}

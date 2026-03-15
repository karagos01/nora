package queries

import (
	"database/sql"
	"nora/models"
)

type ChannelQueries struct {
	DB *sql.DB
}

func (q *ChannelQueries) Create(ch *models.Channel) error {
	_, err := q.DB.Exec(
		`INSERT INTO channels (id, name, topic, type, position, category_id, parent_id, slow_mode_seconds) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.Name, ch.Topic, ch.Type, ch.Position, ch.CategoryID, ch.ParentID, ch.SlowModeSeconds,
	)
	return err
}

func (q *ChannelQueries) GetByID(id string) (*models.Channel, error) {
	ch := &models.Channel{}
	var categoryID, parentID sql.NullString
	err := q.DB.QueryRow(
		`SELECT id, name, topic, type, position, category_id, parent_id, slow_mode_seconds, created_at FROM channels WHERE id = ?`, id,
	).Scan(&ch.ID, &ch.Name, &ch.Topic, &ch.Type, &ch.Position, &categoryID, &parentID, &ch.SlowModeSeconds, &ch.CreatedAt)
	if err != nil {
		return nil, err
	}
	if categoryID.Valid {
		ch.CategoryID = &categoryID.String
	}
	if parentID.Valid {
		ch.ParentID = &parentID.String
	}
	return ch, nil
}

func (q *ChannelQueries) List() ([]models.Channel, error) {
	rows, err := q.DB.Query(
		`SELECT id, name, topic, type, position, category_id, parent_id, slow_mode_seconds, created_at FROM channels ORDER BY position, name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		var ch models.Channel
		var categoryID, parentID sql.NullString
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Topic, &ch.Type, &ch.Position, &categoryID, &parentID, &ch.SlowModeSeconds, &ch.CreatedAt); err != nil {
			return nil, err
		}
		if categoryID.Valid {
			ch.CategoryID = &categoryID.String
		}
		if parentID.Valid {
			ch.ParentID = &parentID.String
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (q *ChannelQueries) Update(ch *models.Channel) error {
	_, err := q.DB.Exec(
		`UPDATE channels SET name = ?, topic = ?, type = ?, position = ?, category_id = ?, parent_id = ?, slow_mode_seconds = ? WHERE id = ?`,
		ch.Name, ch.Topic, ch.Type, ch.Position, ch.CategoryID, ch.ParentID, ch.SlowModeSeconds, ch.ID,
	)
	return err
}

func (q *ChannelQueries) Delete(id string) error {
	_, err := q.DB.Exec("DELETE FROM channels WHERE id = ?", id)
	return err
}

func (q *ChannelQueries) Reorder(ids []string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE channels SET position = ? WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, id := range ids {
		if _, err := stmt.Exec(i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (q *ChannelQueries) NextPosition() (int, error) {
	var pos sql.NullInt64
	err := q.DB.QueryRow("SELECT MAX(position) FROM channels").Scan(&pos)
	if err != nil {
		return 0, err
	}
	if pos.Valid {
		return int(pos.Int64) + 1, nil
	}
	return 0, nil
}

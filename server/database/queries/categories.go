package queries

import (
	"database/sql"
	"nora/models"
)

type CategoryQueries struct {
	DB *sql.DB
}

func (q *CategoryQueries) Create(cat *models.ChannelCategory) error {
	_, err := q.DB.Exec(
		`INSERT INTO channel_categories (id, name, color, position, parent_id) VALUES (?, ?, ?, ?, ?)`,
		cat.ID, cat.Name, cat.Color, cat.Position, cat.ParentID,
	)
	return err
}

func (q *CategoryQueries) GetByID(id string) (*models.ChannelCategory, error) {
	cat := &models.ChannelCategory{}
	err := q.DB.QueryRow(
		`SELECT id, name, color, position, parent_id, created_at FROM channel_categories WHERE id = ?`, id,
	).Scan(&cat.ID, &cat.Name, &cat.Color, &cat.Position, &cat.ParentID, &cat.CreatedAt)
	if err != nil {
		return nil, err
	}
	return cat, nil
}

func (q *CategoryQueries) List() ([]models.ChannelCategory, error) {
	rows, err := q.DB.Query(
		`SELECT id, name, color, position, parent_id, created_at FROM channel_categories ORDER BY position, name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []models.ChannelCategory
	for rows.Next() {
		var cat models.ChannelCategory
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.Color, &cat.Position, &cat.ParentID, &cat.CreatedAt); err != nil {
			return nil, err
		}
		all = append(all, cat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Stavba hierarchie: top-level kategorie s Children polem
	catByID := make(map[string]*models.ChannelCategory, len(all))
	var topLevel []models.ChannelCategory
	for i := range all {
		catByID[all[i].ID] = &all[i]
	}
	// Přiřadit children k rodičům
	for i := range all {
		if all[i].ParentID != nil && *all[i].ParentID != "" {
			if parent, ok := catByID[*all[i].ParentID]; ok {
				parent.Children = append(parent.Children, all[i])
			}
		}
	}
	// Vrátit jen top-level (bez parenta)
	for i := range all {
		if all[i].ParentID == nil || *all[i].ParentID == "" {
			// Kopírovat children z catByID (pointer → aktualizovaná data)
			cat := *catByID[all[i].ID]
			topLevel = append(topLevel, cat)
		}
	}
	return topLevel, nil
}

func (q *CategoryQueries) Update(cat *models.ChannelCategory) error {
	_, err := q.DB.Exec(
		`UPDATE channel_categories SET name = ?, color = ?, position = ?, parent_id = ? WHERE id = ?`,
		cat.Name, cat.Color, cat.Position, cat.ParentID, cat.ID,
	)
	return err
}

func (q *CategoryQueries) Delete(id string) error {
	// Children se smažou CASCADE, kanály v kategorii se stanou uncategorized (ON DELETE SET NULL)
	_, err := q.DB.Exec("DELETE FROM channel_categories WHERE id = ?", id)
	return err
}

func (q *CategoryQueries) NextPosition() (int, error) {
	var pos sql.NullInt64
	err := q.DB.QueryRow("SELECT MAX(position) FROM channel_categories").Scan(&pos)
	if err != nil {
		return 0, err
	}
	if pos.Valid {
		return int(pos.Int64) + 1, nil
	}
	return 0, nil
}

func (q *CategoryQueries) Reorder(ids []string) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE channel_categories SET position = ? WHERE id = ?")
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

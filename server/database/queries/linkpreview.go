package queries

import (
	"database/sql"
	"nora/models"
)

type LinkPreviewQueries struct {
	DB *sql.DB
}

func (q *LinkPreviewQueries) Create(lp *models.LinkPreview) error {
	_, err := q.DB.Exec(
		`INSERT OR REPLACE INTO link_previews (id, message_id, url, title, description, image_url, site_name) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		lp.ID, lp.MessageID, lp.URL, lp.Title, lp.Description, lp.ImageURL, lp.SiteName,
	)
	return err
}

func (q *LinkPreviewQueries) GetByMessageIDs(ids []string) (map[string]*models.LinkPreview, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	query := `SELECT id, message_id, url, title, description, image_url, site_name FROM link_previews WHERE message_id IN (`
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := q.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*models.LinkPreview)
	for rows.Next() {
		var lp models.LinkPreview
		if err := rows.Scan(&lp.ID, &lp.MessageID, &lp.URL, &lp.Title, &lp.Description, &lp.ImageURL, &lp.SiteName); err != nil {
			return nil, err
		}
		result[lp.MessageID] = &lp
	}
	return result, rows.Err()
}

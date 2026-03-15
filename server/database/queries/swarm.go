package queries

import "database/sql"

type SwarmQueries struct {
	DB *sql.DB
}

func (q *SwarmQueries) AddSeed(id, fileCacheID, userID string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO swarm_seeds (id, file_cache_id, user_id) VALUES (?, ?, ?)`,
		id, fileCacheID, userID,
	)
	return err
}

func (q *SwarmQueries) RemoveSeed(fileCacheID, userID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM swarm_seeds WHERE file_cache_id = ? AND user_id = ?`,
		fileCacheID, userID,
	)
	return err
}

func (q *SwarmQueries) ListSeeders(fileCacheID string) ([]string, error) {
	rows, err := q.DB.Query(
		`SELECT user_id FROM swarm_seeds WHERE file_cache_id = ?`, fileCacheID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListFileSeederCounts vrátí počet seederů pro každý soubor v daném share
func (q *SwarmQueries) ListFileSeederCounts(directoryID string) (map[string]int, error) {
	rows, err := q.DB.Query(
		`SELECT sfc.id, COUNT(ss.id)
		 FROM shared_file_cache sfc
		 LEFT JOIN swarm_seeds ss ON ss.file_cache_id = sfc.id
		 WHERE sfc.directory_id = ? AND sfc.is_dir = 0
		 GROUP BY sfc.id
		 HAVING COUNT(ss.id) > 0`, directoryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var fileID string
		var count int
		if err := rows.Scan(&fileID, &count); err != nil {
			return nil, err
		}
		counts[fileID] = count
	}
	return counts, rows.Err()
}

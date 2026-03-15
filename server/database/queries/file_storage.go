package queries

import (
	"database/sql"
	"nora/models"
)

type FileStorageQueries struct {
	DB *sql.DB
}

// Folders

func (q *FileStorageQueries) CreateFolder(f *models.StorageFolder) error {
	_, err := q.DB.Exec(
		`INSERT INTO storage_folders (id, name, parent_id, creator_id) VALUES (?, ?, ?, ?)`,
		f.ID, f.Name, f.ParentID, f.CreatorID,
	)
	return err
}

func (q *FileStorageQueries) ListFolders(parentID *string) ([]models.StorageFolder, error) {
	var rows *sql.Rows
	var err error
	if parentID == nil {
		rows, err = q.DB.Query(
			`SELECT id, name, parent_id, creator_id, created_at FROM storage_folders WHERE parent_id IS NULL ORDER BY name`,
		)
	} else {
		rows, err = q.DB.Query(
			`SELECT id, name, parent_id, creator_id, created_at FROM storage_folders WHERE parent_id = ? ORDER BY name`,
			*parentID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.StorageFolder
	for rows.Next() {
		var f models.StorageFolder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatorID, &f.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (q *FileStorageQueries) GetFolder(id string) (*models.StorageFolder, error) {
	var f models.StorageFolder
	err := q.DB.QueryRow(
		`SELECT id, name, parent_id, creator_id, created_at FROM storage_folders WHERE id = ?`, id,
	).Scan(&f.ID, &f.Name, &f.ParentID, &f.CreatorID, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (q *FileStorageQueries) RenameFolder(id, name string) error {
	_, err := q.DB.Exec(`UPDATE storage_folders SET name = ? WHERE id = ?`, name, id)
	return err
}

func (q *FileStorageQueries) DeleteFolder(id string) error {
	_, err := q.DB.Exec(`DELETE FROM storage_folders WHERE id = ?`, id)
	return err
}

// Files

func (q *FileStorageQueries) CreateFile(f *models.StorageFile) error {
	_, err := q.DB.Exec(
		`INSERT INTO storage_files (id, folder_id, name, filepath, mime_type, size, uploader_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.FolderID, f.Name, f.Filepath, f.MimeType, f.Size, f.UploaderID,
	)
	return err
}

func (q *FileStorageQueries) ListFiles(folderID *string) ([]models.StorageFile, error) {
	var rows *sql.Rows
	var err error
	if folderID == nil {
		rows, err = q.DB.Query(
			`SELECT f.id, f.folder_id, f.name, f.filepath, f.mime_type, f.size, f.uploader_id, COALESCE(u.username, ''), f.created_at
			FROM storage_files f LEFT JOIN users u ON u.id = f.uploader_id
			WHERE f.folder_id IS NULL ORDER BY f.name`,
		)
	} else {
		rows, err = q.DB.Query(
			`SELECT f.id, f.folder_id, f.name, f.filepath, f.mime_type, f.size, f.uploader_id, COALESCE(u.username, ''), f.created_at
			FROM storage_files f LEFT JOIN users u ON u.id = f.uploader_id
			WHERE f.folder_id = ? ORDER BY f.name`,
			*folderID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.StorageFile
	for rows.Next() {
		var f models.StorageFile
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Filepath, &f.MimeType, &f.Size, &f.UploaderID, &f.Username, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.URL = "/api/uploads/" + f.Filepath
		result = append(result, f)
	}
	return result, rows.Err()
}

func (q *FileStorageQueries) GetFile(id string) (*models.StorageFile, error) {
	var f models.StorageFile
	err := q.DB.QueryRow(
		`SELECT f.id, f.folder_id, f.name, f.filepath, f.mime_type, f.size, f.uploader_id, COALESCE(u.username, ''), f.created_at
		FROM storage_files f LEFT JOIN users u ON u.id = f.uploader_id WHERE f.id = ?`, id,
	).Scan(&f.ID, &f.FolderID, &f.Name, &f.Filepath, &f.MimeType, &f.Size, &f.UploaderID, &f.Username, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	f.URL = "/api/uploads/" + f.Filepath
	return &f, nil
}

func (q *FileStorageQueries) RenameFile(id, name string) error {
	_, err := q.DB.Exec(`UPDATE storage_files SET name = ? WHERE id = ?`, name, id)
	return err
}

func (q *FileStorageQueries) MoveFile(id string, folderID *string) error {
	_, err := q.DB.Exec(`UPDATE storage_files SET folder_id = ? WHERE id = ?`, folderID, id)
	return err
}

func (q *FileStorageQueries) DeleteFile(id string) (string, error) {
	var filepath string
	err := q.DB.QueryRow(`SELECT filepath FROM storage_files WHERE id = ?`, id).Scan(&filepath)
	if err != nil {
		return "", err
	}
	_, err = q.DB.Exec(`DELETE FROM storage_files WHERE id = ?`, id)
	return filepath, err
}

// GetFilesInFolder returns filepaths of files in a folder (for deleting files from disk when folder is deleted)
func (q *FileStorageQueries) GetFilesInFolder(folderID string) ([]string, error) {
	rows, err := q.DB.Query(`SELECT filepath FROM storage_files WHERE folder_id = ?`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, err
		}
		result = append(result, fp)
	}
	return result, rows.Err()
}

// TotalStorageSize returns the total size of files in storage
func (q *FileStorageQueries) TotalStorageSize() (int64, error) {
	var total int64
	err := q.DB.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM storage_files`).Scan(&total)
	return total, err
}

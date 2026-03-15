package queries

import (
	"database/sql"
	"nora/models"
)

type ShareQueries struct {
	DB *sql.DB
}

// --- Shared directories ---

func (q *ShareQueries) CreateDirectory(d *models.SharedDirectory) error {
	_, err := q.DB.Exec(
		`INSERT INTO shared_directories (id, owner_id, path_hash, display_name, is_active, max_file_size_mb, storage_quota_mb, max_files_count, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.OwnerID, d.PathHash, d.DisplayName, d.IsActive,
		d.MaxFileSizeMB, d.StorageQuotaMB, d.MaxFilesCount, d.ExpiresAt,
	)
	return err
}

func (q *ShareQueries) GetDirectory(id string) (*models.SharedDirectory, error) {
	d := &models.SharedDirectory{}
	var active int
	err := q.DB.QueryRow(
		`SELECT sd.id, sd.owner_id, sd.path_hash, sd.display_name, sd.is_active,
		        sd.max_file_size_mb, sd.storage_quota_mb, sd.max_files_count, sd.expires_at,
		        sd.created_at, COALESCE(u.username, '') as owner_name
		 FROM shared_directories sd
		 LEFT JOIN users u ON u.id = sd.owner_id
		 WHERE sd.id = ?`, id,
	).Scan(&d.ID, &d.OwnerID, &d.PathHash, &d.DisplayName, &active,
		&d.MaxFileSizeMB, &d.StorageQuotaMB, &d.MaxFilesCount, &d.ExpiresAt,
		&d.CreatedAt, &d.OwnerName)
	if err != nil {
		return nil, err
	}
	d.IsActive = active != 0
	return d, nil
}

func (q *ShareQueries) DeleteDirectory(id string) error {
	_, err := q.DB.Exec("DELETE FROM shared_directories WHERE id = ?", id)
	return err
}

func (q *ShareQueries) UpdateDirectory(d *models.SharedDirectory) error {
	active := 0
	if d.IsActive {
		active = 1
	}
	_, err := q.DB.Exec(
		`UPDATE shared_directories SET display_name = ?, is_active = ?,
		 max_file_size_mb = ?, storage_quota_mb = ?, max_files_count = ?, expires_at = ?
		 WHERE id = ?`,
		d.DisplayName, active,
		d.MaxFileSizeMB, d.StorageQuotaMB, d.MaxFilesCount, d.ExpiresAt,
		d.ID,
	)
	return err
}

func (q *ShareQueries) SetActive(id string, active bool) error {
	a := 0
	if active {
		a = 1
	}
	_, err := q.DB.Exec("UPDATE shared_directories SET is_active = ? WHERE id = ?", a, id)
	return err
}

func (q *ShareQueries) SetAllInactive(ownerID string) error {
	_, err := q.DB.Exec("UPDATE shared_directories SET is_active = 0 WHERE owner_id = ?", ownerID)
	return err
}

func (q *ShareQueries) SetAllActive(ownerID string) error {
	_, err := q.DB.Exec("UPDATE shared_directories SET is_active = 1 WHERE owner_id = ?", ownerID)
	return err
}

// ListByOwner — directories owned by the user
func (q *ShareQueries) ListByOwner(ownerID string) ([]models.SharedDirectory, error) {
	rows, err := q.DB.Query(
		`SELECT sd.id, sd.owner_id, sd.path_hash, sd.display_name, sd.is_active,
		        sd.max_file_size_mb, sd.storage_quota_mb, sd.max_files_count, sd.expires_at,
		        sd.created_at, COALESCE(u.username, '') as owner_name
		 FROM shared_directories sd
		 LEFT JOIN users u ON u.id = sd.owner_id
		 WHERE sd.owner_id = ?
		 ORDER BY sd.created_at DESC`, ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []models.SharedDirectory
	for rows.Next() {
		var d models.SharedDirectory
		var active int
		if err := rows.Scan(&d.ID, &d.OwnerID, &d.PathHash, &d.DisplayName, &active,
			&d.MaxFileSizeMB, &d.StorageQuotaMB, &d.MaxFilesCount, &d.ExpiresAt,
			&d.CreatedAt, &d.OwnerName); err != nil {
			return nil, err
		}
		d.IsActive = active != 0
		dirs = append(dirs, d)
	}
	return dirs, rows.Err()
}

// ListAccessible — directories where the user has access (global read OR per-user read, not blocked)
func (q *ShareQueries) ListAccessible(userID string) ([]models.SharedDirectory, error) {
	rows, err := q.DB.Query(
		`SELECT DISTINCT sd.id, sd.owner_id, sd.path_hash, sd.display_name, sd.is_active,
		        sd.max_file_size_mb, sd.storage_quota_mb, sd.max_files_count, sd.expires_at,
		        sd.created_at, COALESCE(u.username, '') as owner_name
		 FROM shared_directories sd
		 LEFT JOIN users u ON u.id = sd.owner_id
		 LEFT JOIN share_permissions sp_global ON sp_global.directory_id = sd.id AND sp_global.grantee_id IS NULL
		 LEFT JOIN share_permissions sp_user ON sp_user.directory_id = sd.id AND sp_user.grantee_id = ?
		 WHERE sd.owner_id != ?
		   AND (
		     -- Per-user override exists and is not blocked and has read
		     (sp_user.id IS NOT NULL AND sp_user.is_blocked = 0 AND sp_user.can_read = 1)
		     OR
		     -- Global read and no per-user block exists
		     (sp_global.id IS NOT NULL AND sp_global.can_read = 1
		      AND (sp_user.id IS NULL OR (sp_user.is_blocked = 0 AND sp_user.can_read = 1))
		     )
		   )
		 ORDER BY sd.created_at DESC`, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []models.SharedDirectory
	for rows.Next() {
		var d models.SharedDirectory
		var active int
		if err := rows.Scan(&d.ID, &d.OwnerID, &d.PathHash, &d.DisplayName, &active,
			&d.MaxFileSizeMB, &d.StorageQuotaMB, &d.MaxFilesCount, &d.ExpiresAt,
			&d.CreatedAt, &d.OwnerName); err != nil {
			return nil, err
		}
		d.IsActive = active != 0
		dirs = append(dirs, d)
	}
	return dirs, rows.Err()
}

// --- Permissions ---

func (q *ShareQueries) SetPermission(p *models.SharePermission) error {
	_, err := q.DB.Exec(
		`INSERT INTO share_permissions (id, directory_id, grantee_id, can_read, can_write, can_delete, is_blocked)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(directory_id, grantee_id) DO UPDATE SET
		   can_read = excluded.can_read,
		   can_write = excluded.can_write,
		   can_delete = excluded.can_delete,
		   is_blocked = excluded.is_blocked,
		   granted_at = datetime('now')`,
		p.ID, p.DirectoryID, p.GranteeID, p.CanRead, p.CanWrite, p.CanDelete, p.IsBlocked,
	)
	return err
}

func (q *ShareQueries) GetPermission(id string) (*models.SharePermission, error) {
	p := &models.SharePermission{}
	var canRead, canWrite, canDelete, isBlocked int
	err := q.DB.QueryRow(
		`SELECT id, directory_id, grantee_id, can_read, can_write, can_delete, is_blocked, granted_at
		 FROM share_permissions WHERE id = ?`, id,
	).Scan(&p.ID, &p.DirectoryID, &p.GranteeID, &canRead, &canWrite, &canDelete, &isBlocked, &p.GrantedAt)
	if err != nil {
		return nil, err
	}
	p.CanRead = canRead != 0
	p.CanWrite = canWrite != 0
	p.CanDelete = canDelete != 0
	p.IsBlocked = isBlocked != 0
	return p, nil
}

func (q *ShareQueries) DeletePermission(id string) error {
	_, err := q.DB.Exec("DELETE FROM share_permissions WHERE id = ?", id)
	return err
}

func (q *ShareQueries) ListPermissions(directoryID string) ([]models.SharePermission, error) {
	rows, err := q.DB.Query(
		`SELECT sp.id, sp.directory_id, sp.grantee_id, sp.can_read, sp.can_write, sp.can_delete, sp.is_blocked, sp.granted_at,
		        COALESCE(u.username, '') as grantee_name
		 FROM share_permissions sp
		 LEFT JOIN users u ON u.id = sp.grantee_id
		 WHERE sp.directory_id = ?
		 ORDER BY sp.grantee_id IS NULL DESC, sp.granted_at`, directoryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []models.SharePermission
	for rows.Next() {
		var p models.SharePermission
		var canRead, canWrite, canDelete, isBlocked int
		if err := rows.Scan(&p.ID, &p.DirectoryID, &p.GranteeID, &canRead, &canWrite, &canDelete, &isBlocked, &p.GrantedAt, &p.GranteeName); err != nil {
			return nil, err
		}
		p.CanRead = canRead != 0
		p.CanWrite = canWrite != 0
		p.CanDelete = canDelete != 0
		p.IsBlocked = isBlocked != 0
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// GetEffectivePermission — returns effective permission for a user (per-user overrides global)
func (q *ShareQueries) GetEffectivePermission(directoryID, userID string) (*models.SharePermission, error) {
	// Try per-user
	p := &models.SharePermission{}
	var canRead, canWrite, canDelete, isBlocked int
	err := q.DB.QueryRow(
		`SELECT id, directory_id, grantee_id, can_read, can_write, can_delete, is_blocked, granted_at
		 FROM share_permissions WHERE directory_id = ? AND grantee_id = ?`,
		directoryID, userID,
	).Scan(&p.ID, &p.DirectoryID, &p.GranteeID, &canRead, &canWrite, &canDelete, &isBlocked, &p.GrantedAt)
	if err == nil {
		p.CanRead = canRead != 0
		p.CanWrite = canWrite != 0
		p.CanDelete = canDelete != 0
		p.IsBlocked = isBlocked != 0
		return p, nil
	}

	// Fallback to global
	err = q.DB.QueryRow(
		`SELECT id, directory_id, grantee_id, can_read, can_write, can_delete, is_blocked, granted_at
		 FROM share_permissions WHERE directory_id = ? AND grantee_id IS NULL`,
		directoryID,
	).Scan(&p.ID, &p.DirectoryID, &p.GranteeID, &canRead, &canWrite, &canDelete, &isBlocked, &p.GrantedAt)
	if err != nil {
		return nil, err
	}
	p.CanRead = canRead != 0
	p.CanWrite = canWrite != 0
	p.CanDelete = canDelete != 0
	p.IsBlocked = isBlocked != 0
	return p, nil
}

// GetShareStats — returns total size and file count in a share (excluding directories)
func (q *ShareQueries) GetShareStats(directoryID string) (totalSize int64, filesCount int, err error) {
	err = q.DB.QueryRow(
		`SELECT COALESCE(SUM(file_size), 0), COUNT(*)
		 FROM shared_file_cache
		 WHERE directory_id = ? AND is_dir = 0`, directoryID,
	).Scan(&totalSize, &filesCount)
	return
}

// --- File cache ---

func (q *ShareQueries) ReplaceFileCache(directoryID string, files []models.SharedFileEntry) error {
	tx, err := q.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM shared_file_cache WHERE directory_id = ?", directoryID)
	if err != nil {
		return err
	}

	for _, f := range files {
		isDir := 0
		if f.IsDir {
			isDir = 1
		}
		_, err = tx.Exec(
			`INSERT INTO shared_file_cache (id, directory_id, relative_path, file_name, file_size, is_dir, file_hash, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			f.ID, directoryID, f.RelativePath, f.FileName, f.FileSize, isDir, f.FileHash, f.ModifiedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (q *ShareQueries) ListFiles(directoryID, path string) ([]models.SharedFileEntry, error) {
	rows, err := q.DB.Query(
		`SELECT id, directory_id, relative_path, file_name, file_size, is_dir, file_hash, modified_at, cached_at
		 FROM shared_file_cache
		 WHERE directory_id = ? AND relative_path = ?
		 ORDER BY is_dir DESC, file_name`, directoryID, path,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []models.SharedFileEntry
	for rows.Next() {
		var f models.SharedFileEntry
		var isDir int
		if err := rows.Scan(&f.ID, &f.DirectoryID, &f.RelativePath, &f.FileName, &f.FileSize, &isDir, &f.FileHash, &f.ModifiedAt, &f.CachedAt); err != nil {
			return nil, err
		}
		f.IsDir = isDir != 0
		files = append(files, f)
	}
	return files, rows.Err()
}

func (q *ShareQueries) GetFile(id string) (*models.SharedFileEntry, error) {
	f := &models.SharedFileEntry{}
	var isDir int
	err := q.DB.QueryRow(
		`SELECT id, directory_id, relative_path, file_name, file_size, is_dir, file_hash, modified_at, cached_at
		 FROM shared_file_cache WHERE id = ?`, id,
	).Scan(&f.ID, &f.DirectoryID, &f.RelativePath, &f.FileName, &f.FileSize, &isDir, &f.FileHash, &f.ModifiedAt, &f.CachedAt)
	if err != nil {
		return nil, err
	}
	f.IsDir = isDir != 0
	return f, nil
}

func (q *ShareQueries) DeleteFileEntry(directoryID, relativePath, fileName string) error {
	res, err := q.DB.Exec(
		"DELETE FROM shared_file_cache WHERE directory_id = ? AND relative_path = ? AND file_name = ?",
		directoryID, relativePath, fileName,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (q *ShareQueries) ClearFileCache(directoryID string) error {
	_, err := q.DB.Exec("DELETE FROM shared_file_cache WHERE directory_id = ?", directoryID)
	return err
}

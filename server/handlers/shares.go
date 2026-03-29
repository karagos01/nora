package handlers

import (
	"encoding/hex"
	"log"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxFilesPerSync   = 50000
	maxSharesPerUser  = 50
	maxDisplayNameLen = 128
	pathHashLen       = 64
)

// --- Request types ---

type createShareRequest struct {
	PathHash    string `json:"path_hash"`
	DisplayName string `json:"display_name"`
}

type updateShareRequest struct {
	DisplayName    string `json:"display_name"`
	IsActive       *bool  `json:"is_active"`
	MaxFileSizeMB  *int   `json:"max_file_size_mb"`
	StorageQuotaMB *int   `json:"storage_quota_mb"`
	MaxFilesCount  *int   `json:"max_files_count"`
	ExpiryHours    *int   `json:"expiry_hours"`
}

type setPermissionRequest struct {
	GranteeID *string `json:"grantee_id"`
	CanRead   bool    `json:"can_read"`
	CanWrite  bool    `json:"can_write"`
	CanDelete bool    `json:"can_delete"`
	IsBlocked bool    `json:"is_blocked"`
}

type updatePermissionRequest struct {
	CanRead   *bool `json:"can_read"`
	CanWrite  *bool `json:"can_write"`
	CanDelete *bool `json:"can_delete"`
	IsBlocked *bool `json:"is_blocked"`
}

type syncFilesRequest struct {
	Files []syncFileEntry `json:"files"`
}

type syncFileEntry struct {
	RelativePath string `json:"relative_path"`
	FileName     string `json:"file_name"`
	FileSize     int64  `json:"file_size"`
	IsDir        bool   `json:"is_dir"`
	FileHash     string `json:"file_hash"`
	ModifiedAt   string `json:"modified_at"`
}

// --- Shared directories ---

func (d *Deps) ListShares(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	own, err := d.Shares.ListByOwner(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list shares")
		return
	}
	if own == nil {
		own = []models.SharedDirectory{}
	}

	// Lazy expiration check — deactivate expired shares
	now := time.Now().UTC()
	for i := range own {
		if own[i].ExpiresAt != nil && now.After(*own[i].ExpiresAt) && own[i].IsActive {
			own[i].IsActive = false
			d.Shares.UpdateDirectory(&own[i])
		}
	}

	accessible, err := d.Shares.ListAccessible(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list accessible shares")
		return
	}
	if accessible == nil {
		accessible = []models.SharedDirectory{}
	}

	// Add effective permissions to each accessible share
	for i := range accessible {
		perm, err := d.Shares.GetEffectivePermission(accessible[i].ID, user.ID)
		if err == nil {
			accessible[i].CanWrite = perm.CanWrite
			accessible[i].CanDelete = perm.CanDelete
		}
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"own":        own,
		"accessible": accessible,
	})
}

func (d *Deps) CreateShare(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var req createShareRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PathHash == "" || req.DisplayName == "" {
		util.Error(w, http.StatusBadRequest, "path_hash and display_name are required")
		return
	}
	if len(req.DisplayName) > maxDisplayNameLen {
		util.Error(w, http.StatusBadRequest, "display_name too long")
		return
	}

	// Validate path_hash format (hex SHA-256)
	if len(req.PathHash) != pathHashLen {
		util.Error(w, http.StatusBadRequest, "path_hash must be 64 hex characters")
		return
	}
	if _, err := hex.DecodeString(req.PathHash); err != nil {
		util.Error(w, http.StatusBadRequest, "path_hash must be valid hex")
		return
	}

	// Limit shares per user
	existing, _ := d.Shares.ListByOwner(user.ID)
	if len(existing) >= maxSharesPerUser {
		util.Error(w, http.StatusBadRequest, "share limit reached")
		return
	}

	id, _ := uuid.NewV7()
	dir := &models.SharedDirectory{
		ID:          id.String(),
		OwnerID:     user.ID,
		PathHash:    req.PathHash,
		DisplayName: req.DisplayName,
		IsActive:    true,
	}

	if err := d.Shares.CreateDirectory(dir); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create share")
		return
	}

	// Create global permission (default: read)
	permID, _ := uuid.NewV7()
	globalPerm := &models.SharePermission{
		ID:          permID.String(),
		DirectoryID: dir.ID,
		GranteeID:   nil,
		CanRead:     true,
	}
	d.Shares.SetPermission(globalPerm)

	dir, _ = d.Shares.GetDirectory(dir.ID)

	// Broadcast
	event, _ := ws.NewEvent(ws.EventShareRegistered, dir)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, dir)
}

func (d *Deps) GetShare(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Owner always sees, others must have permission
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	result := map[string]any{
		"directory": dir,
	}

	// Only owner can see permissions
	if dir.OwnerID == user.ID {
		perms, _ := d.Shares.ListPermissions(shareID)
		if perms == nil {
			perms = []models.SharePermission{}
		}
		result["permissions"] = perms
	}

	util.JSON(w, http.StatusOK, result)
}

func (d *Deps) UpdateShare(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can update")
		return
	}

	var req updateShareRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DisplayName != "" {
		if len(req.DisplayName) > maxDisplayNameLen {
			util.Error(w, http.StatusBadRequest, "display_name too long")
			return
		}
		dir.DisplayName = req.DisplayName
	}
	if req.IsActive != nil {
		dir.IsActive = *req.IsActive
	}
	if req.MaxFileSizeMB != nil {
		dir.MaxFileSizeMB = *req.MaxFileSizeMB
	}
	if req.StorageQuotaMB != nil {
		dir.StorageQuotaMB = *req.StorageQuotaMB
	}
	if req.MaxFilesCount != nil {
		dir.MaxFilesCount = *req.MaxFilesCount
	}
	if req.ExpiryHours != nil {
		if *req.ExpiryHours > 0 {
			t := time.Now().UTC().Add(time.Duration(*req.ExpiryHours) * time.Hour)
			dir.ExpiresAt = &t
		} else {
			dir.ExpiresAt = nil
		}
	}

	if err := d.Shares.UpdateDirectory(dir); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update share")
		return
	}

	dir, _ = d.Shares.GetDirectory(shareID)
	event, _ := ws.NewEvent(ws.EventShareUpdated, dir)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, dir)
}

func (d *Deps) DeleteShare(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can delete")
		return
	}

	d.Shares.DeleteDirectory(shareID)

	event, _ := ws.NewEvent(ws.EventShareUnregistered, map[string]string{
		"id":       shareID,
		"owner_id": user.ID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Permissions ---

func (d *Deps) AddSharePermission(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can manage permissions")
		return
	}

	var req setPermissionRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Write implies Read + Delete
	canWrite := req.CanWrite
	canRead := req.CanRead
	if canWrite {
		canRead = true
	}

	id, _ := uuid.NewV7()
	perm := &models.SharePermission{
		ID:          id.String(),
		DirectoryID: shareID,
		GranteeID:   req.GranteeID,
		CanRead:     canRead,
		CanWrite:    canWrite,
		CanDelete:   canWrite, // Delete = Write
		IsBlocked:   req.IsBlocked,
	}

	if err := d.Shares.SetPermission(perm); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to set permission")
		return
	}

	perms, _ := d.Shares.ListPermissions(shareID)
	if perms == nil {
		perms = []models.SharePermission{}
	}

	// Permission change notification — broadcast to all (to refresh accessible list),
	// but without sensitive data (only directory_id)
	event, _ := ws.NewEvent(ws.EventSharePermissionChanged, map[string]string{
		"directory_id": shareID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, perms)
}

func (d *Deps) ListSharePermissions(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can view permissions")
		return
	}

	perms, err := d.Shares.ListPermissions(shareID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list permissions")
		return
	}
	if perms == nil {
		perms = []models.SharePermission{}
	}

	util.JSON(w, http.StatusOK, perms)
}

func (d *Deps) UpdateSharePermission(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")
	permID := r.PathValue("pid")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can manage permissions")
		return
	}

	existing, err := d.Shares.GetPermission(permID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "permission not found")
		return
	}
	if existing.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "permission does not belong to this share")
		return
	}

	var req updatePermissionRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CanRead != nil {
		existing.CanRead = *req.CanRead
	}
	if req.CanWrite != nil {
		existing.CanWrite = *req.CanWrite
		existing.CanDelete = *req.CanWrite // Delete = Write
	}
	if req.IsBlocked != nil {
		existing.IsBlocked = *req.IsBlocked
	}
	// Write implies Read
	if existing.CanWrite {
		existing.CanRead = true
	}

	if err := d.Shares.SetPermission(existing); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update permission")
		return
	}

	event, _ := ws.NewEvent(ws.EventSharePermissionChanged, map[string]string{
		"directory_id": shareID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, existing)
}

func (d *Deps) DeleteSharePermission(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")
	permID := r.PathValue("pid")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can manage permissions")
		return
	}

	existing, err := d.Shares.GetPermission(permID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "permission not found")
		return
	}
	if existing.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "permission does not belong to this share")
		return
	}

	d.Shares.DeletePermission(permID)

	event, _ := ws.NewEvent(ws.EventSharePermissionChanged, map[string]string{
		"directory_id": shareID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- File listing ---

func (d *Deps) ListShareFiles(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")
	qpath := r.URL.Query().Get("path")
	if qpath == "" {
		qpath = "/"
	}
	if !isValidSharePath(qpath) {
		util.Error(w, http.StatusBadRequest, "invalid path")
		return
	}

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Access: owner always, others must have read
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	files, err := d.Shares.ListFiles(shareID, qpath)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	if files == nil {
		files = []models.SharedFileEntry{}
	}

	util.JSON(w, http.StatusOK, files)
}

// SyncShareFiles — client sends current file listing, server replaces cache
func (d *Deps) SyncShareFiles(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can sync files")
		return
	}

	// Check expiration
	if d.checkShareExpired(dir) {
		util.Error(w, http.StatusGone, "share has expired")
		return
	}

	// Limit request body (100MB max — large shares)
	r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024)

	var req syncFilesRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		log.Printf("SyncShareFiles decode error: %v (shareID=%s)", err, shareID)
		util.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Limit number of files (anti-DoS)
	if len(req.Files) > maxFilesPerSync {
		util.Error(w, http.StatusBadRequest, "too many files")
		return
	}

	var entries []models.SharedFileEntry
	var totalSize int64
	var fileCount int
	for _, f := range req.Files {
		// Validace path traversal
		if !isValidSharePath(f.RelativePath) || !isValidFileName(f.FileName) {
			continue
		}
		if f.FileSize < 0 {
			continue
		}

		if !f.IsDir {
			// Check max file size
			if dir.MaxFileSizeMB > 0 && f.FileSize > int64(dir.MaxFileSizeMB)*1024*1024 {
				continue // skip file exceeding limit
			}
			fileCount++
			totalSize += f.FileSize
		}

		id, _ := uuid.NewV7()
		entries = append(entries, models.SharedFileEntry{
			ID:           id.String(),
			DirectoryID:  shareID,
			RelativePath: f.RelativePath,
			FileName:     f.FileName,
			FileSize:     f.FileSize,
			IsDir:        f.IsDir,
			FileHash:     f.FileHash,
		})
	}

	// Check max files count
	if dir.MaxFilesCount > 0 && fileCount > dir.MaxFilesCount {
		util.Error(w, http.StatusBadRequest, "too many files for this share's limit")
		return
	}

	// Check storage quota
	if dir.StorageQuotaMB > 0 && totalSize > int64(dir.StorageQuotaMB)*1024*1024 {
		util.Error(w, http.StatusBadRequest, "storage quota exceeded")
		return
	}

	if err := d.Shares.ReplaceFileCache(shareID, entries); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to sync files")
		return
	}

	// Broadcast file change
	event, _ := ws.NewEvent(ws.EventShareFilesChanged, map[string]string{
		"directory_id": shareID,
		"owner_id":     user.ID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"count":  len(entries),
	})
}

// TransferRequest — file download request
func (d *Deps) TransferRequest(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Verify access
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanRead || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no access")
			return
		}
	}

	var req struct {
		FileID string `json:"file_id"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.FileID == "" {
		util.Error(w, http.StatusBadRequest, "file_id is required")
		return
	}

	file, err := d.Shares.GetFile(req.FileID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "file not found")
		return
	}

	// IDOR protection: file must belong to the requested share
	if file.DirectoryID != shareID {
		util.Error(w, http.StatusBadRequest, "file does not belong to this share")
		return
	}

	// Send transfer request to owner via WS
	transferID, _ := uuid.NewV7()
	event, _ := ws.NewEvent(ws.EventTransferRequest, map[string]any{
		"transfer_id":   transferID.String(),
		"directory_id":  shareID,
		"file_id":       file.ID,
		"file_name":     file.FileName,
		"file_size":     file.FileSize,
		"file_hash":     file.FileHash,
		"relative_path": file.RelativePath,
		"requester_id":  user.ID,
	})
	d.Hub.BroadcastToUser(dir.OwnerID, event)

	util.JSON(w, http.StatusOK, map[string]any{
		"transfer_id":  transferID.String(),
		"owner_online": d.Hub.IsUserOnline(dir.OwnerID),
	})
}

// UploadRequest — file upload request to share (uploader → owner)
func (d *Deps) UploadRequest(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Check expiration
	if d.checkShareExpired(dir) {
		util.Error(w, http.StatusGone, "share has expired")
		return
	}

	// Verify write permission
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanWrite || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no write access")
			return
		}
	}

	var req struct {
		FileName     string `json:"file_name"`
		FileSize     int64  `json:"file_size"`
		RelativePath string `json:"relative_path"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.FileName == "" {
		util.Error(w, http.StatusBadRequest, "file_name is required")
		return
	}
	if !isValidFileName(req.FileName) {
		util.Error(w, http.StatusBadRequest, "invalid file_name")
		return
	}
	if req.RelativePath == "" {
		req.RelativePath = "/"
	}
	if !isValidSharePath(req.RelativePath) {
		util.Error(w, http.StatusBadRequest, "invalid relative_path")
		return
	}

	// Check max file size
	if dir.MaxFileSizeMB > 0 && req.FileSize > int64(dir.MaxFileSizeMB)*1024*1024 {
		util.Error(w, http.StatusRequestEntityTooLarge, "file exceeds max file size limit")
		return
	}

	// Check stats (files count + storage quota)
	if dir.MaxFilesCount > 0 || dir.StorageQuotaMB > 0 {
		totalSize, filesCount, err := d.Shares.GetShareStats(shareID)
		if err == nil {
			if dir.MaxFilesCount > 0 && filesCount+1 > dir.MaxFilesCount {
				util.Error(w, http.StatusBadRequest, "max files count exceeded")
				return
			}
			if dir.StorageQuotaMB > 0 && totalSize+req.FileSize > int64(dir.StorageQuotaMB)*1024*1024 {
				util.Error(w, http.StatusBadRequest, "storage quota exceeded")
				return
			}
		}
	}

	uploadID, _ := uuid.NewV7()
	event, _ := ws.NewEvent(ws.EventUploadRequest, map[string]any{
		"upload_id":     uploadID.String(),
		"directory_id":  shareID,
		"file_name":     req.FileName,
		"file_size":     req.FileSize,
		"relative_path": req.RelativePath,
		"uploader_id":   user.ID,
	})
	d.Hub.BroadcastToUser(dir.OwnerID, event)

	util.JSON(w, http.StatusOK, map[string]any{
		"upload_id":    uploadID.String(),
		"owner_online": d.Hub.IsUserOnline(dir.OwnerID),
	})
}

// DeleteShareFile — deletes a file record from cache and notifies owner for physical deletion
func (d *Deps) DeleteShareFile(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Verify write permission (owner always has access)
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanWrite || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no write access")
			return
		}
	}

	var req struct {
		RelativePath string `json:"relative_path"`
		FileName     string `json:"file_name"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.FileName == "" {
		util.Error(w, http.StatusBadRequest, "file_name is required")
		return
	}
	if !isValidFileName(req.FileName) {
		util.Error(w, http.StatusBadRequest, "invalid file_name")
		return
	}
	if req.RelativePath == "" {
		req.RelativePath = "/"
	}
	if !isValidSharePath(req.RelativePath) {
		util.Error(w, http.StatusBadRequest, "invalid relative_path")
		return
	}

	// Delete record from DB
	if err := d.Shares.DeleteFileEntry(shareID, req.RelativePath, req.FileName); err != nil {
		util.Error(w, http.StatusNotFound, "file not found in cache")
		return
	}

	// Notify owner via WS — so they physically delete the file from disk
	event, _ := ws.NewEvent(ws.EventFileDeleted, map[string]any{
		"directory_id":  shareID,
		"relative_path": req.RelativePath,
		"file_name":     req.FileName,
		"deleted_by":    user.ID,
	})
	d.Hub.BroadcastToUser(dir.OwnerID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RenameShareFile renames a file entry in the shared file cache
func (d *Deps) RenameShareFile(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}

	// Verify write permission (owner always has access)
	if dir.OwnerID != user.ID {
		perm, err := d.Shares.GetEffectivePermission(shareID, user.ID)
		if err != nil || !perm.CanWrite || perm.IsBlocked {
			util.Error(w, http.StatusForbidden, "no write access")
			return
		}
	}

	var req struct {
		RelativePath string `json:"relative_path"`
		OldName      string `json:"old_name"`
		NewName      string `json:"new_name"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.OldName == "" || req.NewName == "" {
		util.Error(w, http.StatusBadRequest, "old_name and new_name are required")
		return
	}
	if !isValidFileName(req.OldName) || !isValidFileName(req.NewName) {
		util.Error(w, http.StatusBadRequest, "invalid file name")
		return
	}
	if req.RelativePath == "" {
		req.RelativePath = "/"
	}
	if !isValidSharePath(req.RelativePath) {
		util.Error(w, http.StatusBadRequest, "invalid relative_path")
		return
	}

	if err := d.Shares.RenameFileEntry(shareID, req.RelativePath, req.OldName, req.NewName); err != nil {
		util.Error(w, http.StatusNotFound, "file not found in cache")
		return
	}

	// Notify owner via WS — so they can rename the file on disk
	event, _ := ws.NewEvent(ws.EventFileRenamed, map[string]any{
		"directory_id":  shareID,
		"relative_path": req.RelativePath,
		"old_name":      req.OldName,
		"new_name":      req.NewName,
		"renamed_by":    user.ID,
	})
	d.Hub.BroadcastToUser(dir.OwnerID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetShareStats — returns total size and file count (owner only)
func (d *Deps) GetShareStats(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	shareID := r.PathValue("id")

	dir, err := d.Shares.GetDirectory(shareID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if dir.OwnerID != user.ID {
		util.Error(w, http.StatusForbidden, "only owner can view stats")
		return
	}

	totalSize, filesCount, err := d.Shares.GetShareStats(shareID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"total_size":  totalSize,
		"files_count": filesCount,
	})
}

// checkShareExpired checks share expiration and deactivates it, returns true if expired
func (d *Deps) checkShareExpired(dir *models.SharedDirectory) bool {
	if dir.ExpiresAt != nil && time.Now().UTC().After(*dir.ExpiresAt) {
		dir.IsActive = false
		d.Shares.UpdateDirectory(dir)
		return true
	}
	return false
}

// --- Validation ---

// isValidSharePath checks that the relative path is safe
func isValidSharePath(p string) bool {
	if p == "" {
		return false
	}
	// Must start with /
	if !strings.HasPrefix(p, "/") {
		return false
	}
	// Must not contain path traversal
	cleaned := path.Clean(p)
	if strings.Contains(cleaned, "..") {
		return false
	}
	return true
}

// isValidFileName checks that the filename does not contain path separators
func isValidFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if len(name) > 255 {
		return false
	}
	return true
}

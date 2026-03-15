package handlers

import (
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ListStorageFolders — GET /api/storage/folders?parent_id=...
func (d *Deps) ListStorageFolders(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var parentID *string
	if pid := r.URL.Query().Get("parent_id"); pid != "" {
		parentID = &pid
	}

	folders, err := d.FileStorage.ListFolders(parentID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list folders")
		return
	}
	if folders == nil {
		folders = []models.StorageFolder{}
	}

	util.JSON(w, http.StatusOK, folders)
}

// CreateStorageFolder — POST /api/storage/folders
func (d *Deps) CreateStorageFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 100 {
		util.Error(w, http.StatusBadRequest, "name must be 1-100 characters")
		return
	}

	// Ověřit parent existuje
	if req.ParentID != nil {
		if _, err := d.FileStorage.GetFolder(*req.ParentID); err != nil {
			util.Error(w, http.StatusNotFound, "parent folder not found")
			return
		}
	}

	id, _ := uuid.NewV7()
	folder := &models.StorageFolder{
		ID:        id.String(),
		Name:      req.Name,
		ParentID:  req.ParentID,
		CreatorID: user.ID,
		CreatedAt: time.Now().UTC(),
	}

	if err := d.FileStorage.CreateFolder(folder); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create folder")
		return
	}

	event, _ := ws.NewEvent(ws.EventStorageFolderCreate, folder)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, folder)
}

// RenameStorageFolder — PATCH /api/storage/folders/{id}
func (d *Deps) RenameStorageFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	folderID := r.PathValue("id")
	var req struct {
		Name string `json:"name"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 100 {
		util.Error(w, http.StatusBadRequest, "name must be 1-100 characters")
		return
	}

	if err := d.FileStorage.RenameFolder(folderID, req.Name); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to rename folder")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteStorageFolder — DELETE /api/storage/folders/{id}
func (d *Deps) DeleteStorageFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	folderID := r.PathValue("id")

	// Smazat soubory z disku
	absUploads, _ := filepath.Abs(d.UploadsDir)
	filepaths, _ := d.FileStorage.GetFilesInFolder(folderID)
	for _, fp := range filepaths {
		if strings.Contains(fp, "..") {
			continue
		}
		p := filepath.Join(d.UploadsDir, fp)
		absP, err := filepath.Abs(p)
		if err != nil || !strings.HasPrefix(absP, absUploads+string(filepath.Separator)) {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Error("smazání storage souboru selhalo", "path", p, "error", err)
		}
	}

	if err := d.FileStorage.DeleteFolder(folderID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}

	event, _ := ws.NewEvent(ws.EventStorageFolderDelete, map[string]string{"id": folderID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListStorageFiles — GET /api/storage/files?folder_id=...
func (d *Deps) ListStorageFiles(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var folderID *string
	if fid := r.URL.Query().Get("folder_id"); fid != "" {
		folderID = &fid
	}

	files, err := d.FileStorage.ListFiles(folderID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	if files == nil {
		files = []models.StorageFile{}
	}

	util.JSON(w, http.StatusOK, files)
}

// UploadStorageFile — POST /api/storage/files
func (d *Deps) UploadStorageFile(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, d.MaxUploadSize)
	if err := r.ParseMultipartForm(d.MaxUploadSize); err != nil {
		util.Error(w, http.StatusBadRequest, "file too large")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		util.Error(w, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()

	// Přečíst prvních 512 bytů pro MIME detekci
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mimeType := http.DetectContentType(buf[:n])
	file.Seek(0, 0)

	// Validovat MIME typ
	if !d.isAllowedType(mimeType) {
		util.Error(w, http.StatusBadRequest, "file type not allowed")
		return
	}

	// Uložit soubor
	fileID, _ := uuid.NewV7()
	ext := filepath.Ext(header.Filename)
	diskName := fileID.String() + ext
	dst := filepath.Join(d.UploadsDir, diskName)

	out, err := os.Create(dst)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer out.Close()

	written, err := out.ReadFrom(file)
	if err != nil {
		os.Remove(dst)
		util.Error(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	var folderID *string
	if fid := r.FormValue("folder_id"); fid != "" {
		folderID = &fid
	}

	sf := &models.StorageFile{
		ID:         fileID.String(),
		FolderID:   folderID,
		Name:       header.Filename,
		Filepath:   diskName,
		MimeType:   mimeType,
		Size:       written,
		UploaderID: user.ID,
		Username:   user.Username,
		URL:        "/api/uploads/" + diskName,
		CreatedAt:  time.Now().UTC(),
	}

	if err := d.FileStorage.CreateFile(sf); err != nil {
		os.Remove(dst)
		util.Error(w, http.StatusInternalServerError, "failed to save file record")
		return
	}

	event, _ := ws.NewEvent(ws.EventStorageFileCreate, sf)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, sf)
}

// RenameStorageFile — PATCH /api/storage/files/{id}
func (d *Deps) RenameStorageFile(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	fileID := r.PathValue("id")
	var req struct {
		Name     string  `json:"name,omitempty"`
		FolderID *string `json:"folder_id,omitempty"`
		Move     bool    `json:"move,omitempty"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Move {
		if err := d.FileStorage.MoveFile(fileID, req.FolderID); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to move file")
			return
		}
	} else if req.Name != "" {
		if err := d.FileStorage.RenameFile(fileID, req.Name); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to rename file")
			return
		}
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteStorageFile — DELETE /api/storage/files/{id}
func (d *Deps) DeleteStorageFile(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	fileID := r.PathValue("id")
	fp, err := d.FileStorage.DeleteFile(fileID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "file not found")
		return
	}

	// Smazat z disku (path traversal ochrana)
	if !strings.Contains(fp, "..") {
		p := filepath.Join(d.UploadsDir, fp)
		absP, absErr := filepath.Abs(p)
		absUploads, _ := filepath.Abs(d.UploadsDir)
		if absErr == nil && strings.HasPrefix(absP, absUploads+string(filepath.Separator)) {
			if err := os.Remove(absP); err != nil && !os.IsNotExist(err) {
				slog.Error("smazání storage souboru selhalo", "path", absP, "error", err)
			}
		}
	}

	event, _ := ws.NewEvent(ws.EventStorageFileDelete, map[string]string{"id": fileID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// isAllowedType kontroluje MIME typ proti whitelist
func (d *Deps) isAllowedType(mimeType string) bool {
	for _, allowed := range d.AllowedTypes {
		if mimeType == allowed {
			return true
		}
		// Prefix matching (image/ → image/png, image/jpeg...)
		if len(allowed) > 0 && allowed[len(allowed)-1] == '/' {
			if len(mimeType) > len(allowed) && mimeType[:len(allowed)] == allowed {
				return true
			}
		}
	}
	return false
}

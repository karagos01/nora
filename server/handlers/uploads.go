package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sanitizeFilename odstraní nebezpečné znaky z filename pro Content-Disposition header.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' || r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "." || name == ".." || name == "" {
		return ""
	}
	return name
}

// --- Per-user upload rate limiter ---

type UploadRateLimiter struct {
	mu    sync.Mutex
	count map[string]int       // userID → počet uploadů v aktuálním okně
	reset map[string]time.Time // userID → reset time
	limit int                  // max uploadů za minutu
}

func NewUploadRateLimiter(limit int) *UploadRateLimiter {
	return &UploadRateLimiter{
		count: make(map[string]int),
		reset: make(map[string]time.Time),
		limit: limit,
	}
}

func (u *UploadRateLimiter) Allow(userID string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	if u.reset[userID].Before(now) {
		u.count[userID] = 0
		u.reset[userID] = now.Add(time.Minute)
	}
	if u.count[userID] >= u.limit {
		return false
	}
	u.count[userID]++
	return true
}

// --- Chunked upload ---

type UploadSession struct {
	ID        string
	UserID    string
	Filename  string
	Size      int64
	Offset    int64
	TempPath  string
	ExpiresAt time.Time
	mu        sync.Mutex
}

type UploadSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*UploadSession
}

func NewUploadSessionStore() *UploadSessionStore {
	s := &UploadSessionStore{sessions: make(map[string]*UploadSession)}
	go s.cleanupLoop()
	return s
}

func (s *UploadSessionStore) Get(id string) *UploadSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

func (s *UploadSessionStore) Put(sess *UploadSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

func (s *UploadSessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *UploadSessionStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, sess := range s.sessions {
			if now.After(sess.ExpiresAt) {
				os.Remove(sess.TempPath)
				delete(s.sessions, id)
				slog.Debug("vyčištěna expirovaná upload session", "session_id", id)
			}
		}
		s.mu.Unlock()
	}
}

func (d *Deps) Upload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.checkQuarantine(user.ID, "upload"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	if d.UploadLimiter != nil && !d.UploadLimiter.Allow(user.ID) {
		util.Error(w, http.StatusTooManyRequests, "upload rate limit exceeded")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, d.MaxUploadSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		util.Error(w, http.StatusBadRequest, "failed to read file")
		return
	}
	defer file.Close()

	// Detekce MIME type (pro audit log)
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mimeType := http.DetectContentType(buf[:n])
	file.Seek(0, io.SeekStart)

	// UUID filename
	ext := filepath.Ext(header.Filename)
	id, _ := uuid.NewV7()
	filename := id.String() + ext

	os.MkdirAll(d.UploadsDir, 0755)
	dst, err := os.Create(filepath.Join(d.UploadsDir, filename))
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()

	size, err := io.Copy(dst, file)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	// Spočítat SHA-256 obsahu souboru
	dst.Seek(0, io.SeekStart)
	h := sha256.New()
	io.Copy(h, dst)
	contentHash := hex.EncodeToString(h.Sum(nil))

	// Deduplikace: pokud soubor se stejným hashem už existuje, smazat nový a vrátit existující
	if existing, err := d.Attachments.FindByContentHash(contentHash); err == nil && existing != "" {
		newPath := filepath.Join(d.UploadsDir, filename)
		os.Remove(newPath)
		filename = existing
	}

	d.logAudit(user.ID, "upload", "file", filename, map[string]any{"original": header.Filename, "size": size, "mime_type": mimeType})

	util.JSON(w, http.StatusCreated, map[string]any{
		"filename":     filename,
		"original":     header.Filename,
		"mime_type":    mimeType,
		"size":         size,
		"url":          "/api/uploads/" + filename,
		"content_hash": contentHash,
	})
}

// InitUpload zahájí chunked upload session
func (d *Deps) InitUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermUpload); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.checkQuarantine(user.ID, "upload"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	if d.UploadLimiter != nil && !d.UploadLimiter.Allow(user.ID) {
		util.Error(w, http.StatusTooManyRequests, "upload rate limit exceeded")
		return
	}

	var body struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	if err := util.DecodeJSON(r, &body); err != nil || body.Filename == "" || body.Size <= 0 {
		util.Error(w, http.StatusBadRequest, "filename and size required")
		return
	}

	if body.Size > d.MaxUploadSize {
		util.Error(w, http.StatusBadRequest, fmt.Sprintf("file too large (max %d bytes)", d.MaxUploadSize))
		return
	}

	id, _ := uuid.NewV7()
	tmpDir := filepath.Join(d.UploadsDir, "tmp")
	os.MkdirAll(tmpDir, 0755)
	tmpPath := filepath.Join(tmpDir, id.String()+".part")

	f, err := os.Create(tmpPath)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	f.Close()

	sess := &UploadSession{
		ID:        id.String(),
		UserID:    user.ID,
		Filename:  body.Filename,
		Size:      body.Size,
		Offset:    0,
		TempPath:  tmpPath,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	d.Uploads.Put(sess)

	util.JSON(w, http.StatusOK, map[string]any{
		"upload_id":  sess.ID,
		"chunk_size": 262144, // 256KB
	})
}

// UploadChunk přijme chunk dat pro chunked upload
func (d *Deps) UploadChunk(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	uploadID := r.PathValue("id")
	sess := d.Uploads.Get(uploadID)
	if sess == nil {
		util.Error(w, http.StatusNotFound, "upload session not found")
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.UserID != user.ID {
		util.Error(w, http.StatusForbidden, "not your upload")
		return
	}

	// Kontrola offsetu z headeru
	offsetHeader := r.Header.Get("Upload-Offset")
	if offsetHeader == "" {
		util.Error(w, http.StatusBadRequest, "Upload-Offset header required")
		return
	}
	clientOffset, err := strconv.ParseInt(offsetHeader, 10, 64)
	if err != nil || clientOffset != sess.Offset {
		util.Error(w, http.StatusConflict, fmt.Sprintf("offset mismatch: expected %d", sess.Offset))
		return
	}

	// Omezit velikost chunku (256KB + margin)
	r.Body = http.MaxBytesReader(w, r.Body, 300*1024)

	f, err := os.OpenFile(sess.TempPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to open temp file")
		return
	}
	defer f.Close()

	n, err := io.Copy(f, r.Body)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to write chunk")
		return
	}

	sess.Offset += n
	sess.ExpiresAt = time.Now().Add(30 * time.Minute)

	// Auto-complete když offset == size
	if sess.Offset >= sess.Size {
		d.completeUpload(w, sess)
		return
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"offset": sess.Offset,
	})
}

// UploadStatus vrátí aktuální stav upload session (pro resume)
func (d *Deps) UploadStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	uploadID := r.PathValue("id")
	sess := d.Uploads.Get(uploadID)
	if sess == nil {
		util.Error(w, http.StatusNotFound, "upload session not found")
		return
	}

	if sess.UserID != user.ID {
		util.Error(w, http.StatusForbidden, "not your upload")
		return
	}

	w.Header().Set("Upload-Offset", strconv.FormatInt(sess.Offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(sess.Size, 10))
	w.WriteHeader(http.StatusNoContent)
}

// completeUpload dokončí upload — MIME check, přesun souboru, cleanup session
func (d *Deps) completeUpload(w http.ResponseWriter, sess *UploadSession) {
	// MIME detekce z prvních 512B temp souboru
	f, err := os.Open(sess.TempPath)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to read temp file")
		return
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	f.Close()

	mimeType := http.DetectContentType(buf[:n])

	// Přesun do finální lokace
	ext := filepath.Ext(sess.Filename)
	finalID, _ := uuid.NewV7()
	filename := finalID.String() + ext
	finalPath := filepath.Join(d.UploadsDir, filename)

	if err := os.Rename(sess.TempPath, finalPath); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to finalize file")
		return
	}

	// Spočítat SHA-256 finálního souboru
	ff, err := os.Open(finalPath)
	var contentHash string
	if err == nil {
		h := sha256.New()
		io.Copy(h, ff)
		ff.Close()
		contentHash = hex.EncodeToString(h.Sum(nil))
	}

	// Deduplikace: pokud soubor se stejným hashem už existuje, smazat nový a vrátit existující
	if contentHash != "" {
		if existing, err := d.Attachments.FindByContentHash(contentHash); err == nil && existing != "" {
			os.Remove(finalPath)
			filename = existing
		}
	}

	d.Uploads.Delete(sess.ID)

	d.logAudit(sess.UserID, "upload", "file", filename, map[string]any{"original": sess.Filename, "size": sess.Size, "mime_type": mimeType})

	util.JSON(w, http.StatusCreated, map[string]any{
		"filename":     filename,
		"original":     sess.Filename,
		"url":          "/api/uploads/" + filename,
		"mime_type":    mimeType,
		"size":         sess.Size,
		"content_hash": contentHash,
	})
}

func (d *Deps) ServeUpload(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/api/uploads/")
	if filename == "" || strings.Contains(filename, "..") {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(d.UploadsDir, filename)
	absPath, err := filepath.Abs(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absUploads, _ := filepath.Abs(d.UploadsDir)
	if !strings.HasPrefix(absPath, absUploads+string(filepath.Separator)) && absPath != absUploads {
		http.NotFound(w, r)
		return
	}

	// Vypnout write deadline pro stahování souborů (videa, zipy).
	// Globální WriteTimeout (15s) nestačí pro pomalé klienty streamující video.
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Podpora stažení s originálním názvem (sanitizace proti header injection)
	if name := r.URL.Query().Get("name"); name != "" {
		safe := sanitizeFilename(name)
		if safe != "" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safe))
		}
	}

	http.ServeFile(w, r, path)
}

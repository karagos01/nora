package handlers

import (
	"io"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var emojiNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{2,32}$`)

func (d *Deps) ListEmojis(w http.ResponseWriter, r *http.Request) {
	emojis, err := d.Emojis.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list emojis")
		return
	}
	for i := range emojis {
		emojis[i].URL = "/api/uploads/" + emojis[i].Filepath
	}
	if emojis == nil {
		emojis = []models.Emoji{}
	}
	util.JSON(w, http.StatusOK, emojis)
}

func (d *Deps) CreateEmoji(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageEmojis); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Max 256KB
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)

	name := strings.TrimSpace(r.FormValue("name"))
	if !emojiNameRe.MatchString(name) {
		util.Error(w, http.StatusBadRequest, "invalid emoji name: only a-z, A-Z, 0-9, _ allowed, 2-32 chars")
		return
	}

	exists, err := d.Emojis.NameExists(name)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "database error")
		return
	}
	if exists {
		util.Error(w, http.StatusConflict, "emoji name already exists")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		util.Error(w, http.StatusBadRequest, "failed to read file")
		return
	}
	defer file.Close()

	// MIME detekce
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mimeType := http.DetectContentType(buf[:n])
	file.Seek(0, io.SeekStart)

	if !strings.HasPrefix(mimeType, "image/") {
		util.Error(w, http.StatusBadRequest, "only image files are allowed")
		return
	}

	// UUID filename
	ext := filepath.Ext(header.Filename)
	id, _ := uuid.NewV7()
	filename := "emoji_" + id.String() + ext

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

	emoji := &models.Emoji{
		ID:         id.String(),
		Name:       name,
		Filepath:   filename,
		MimeType:   mimeType,
		Size:       size,
		UploaderID: user.ID,
	}

	if err := d.Emojis.Create(emoji); err != nil {
		os.Remove(filepath.Join(d.UploadsDir, filename))
		util.Error(w, http.StatusInternalServerError, "failed to save emoji")
		return
	}

	emoji.URL = "/api/uploads/" + filename

	// Broadcast
	data, err := ws.NewEvent(ws.EventEmojiCreate, emoji)
	if err == nil {
		d.Hub.Broadcast(data)
	}

	d.logAudit(user.ID, "emoji.create", "emoji", emoji.ID, map[string]string{"name": name})

	util.JSON(w, http.StatusCreated, emoji)
}

func (d *Deps) DeleteEmoji(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageEmojis); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	emoji, err := d.Emojis.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "emoji not found")
		return
	}

	// Smazat soubor
	os.Remove(filepath.Join(d.UploadsDir, emoji.Filepath))

	if err := d.Emojis.Delete(id); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete emoji")
		return
	}

	// Broadcast
	data, err := ws.NewEvent(ws.EventEmojiDelete, map[string]string{"id": id})
	if err == nil {
		d.Hub.Broadcast(data)
	}

	d.logAudit(user.ID, "emoji.delete", "emoji", id, map[string]string{"name": emoji.Name})

	util.JSON(w, http.StatusOK, map[string]string{"id": id})
}

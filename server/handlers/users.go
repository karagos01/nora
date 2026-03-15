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
	"strings"
	"time"

	"github.com/google/uuid"
)

func (d *Deps) ListUsers(w http.ResponseWriter, r *http.Request) {
	pubKey := r.URL.Query().Get("public_key")
	if pubKey != "" {
		user, err := d.Users.GetByPublicKey(pubKey)
		if err != nil {
			util.Error(w, http.StatusNotFound, "user not found")
			return
		}
		util.JSON(w, http.StatusOK, []models.User{*user})
		return
	}

	users, err := d.Users.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	if users == nil {
		users = []models.User{}
	}
	util.JSON(w, http.StatusOK, users)
}

func (d *Deps) GetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, err := d.Users.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}
	util.JSON(w, http.StatusOK, user)
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Status      *string `json:"status,omitempty"`
	StatusText  *string `json:"status_text,omitempty"`
}

func (d *Deps) UpdateMe(w http.ResponseWriter, r *http.Request) {
	u := auth.GetUser(r)

	var req updateMeRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DisplayName != nil {
		if len(*req.DisplayName) > util.MaxDisplayName {
			util.Error(w, http.StatusBadRequest, "display name too long")
			return
		}
		if err := d.Users.UpdateDisplayName(u.ID, *req.DisplayName); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to update")
			return
		}
	}

	if req.Status != nil || req.StatusText != nil {
		status := ""
		statusText := ""
		if req.Status != nil {
			s := *req.Status
			if s != "" && s != "online" && s != "away" && s != "dnd" {
				util.Error(w, http.StatusBadRequest, "invalid status (online, away, dnd or empty)")
				return
			}
			status = s
		}
		if req.StatusText != nil {
			if len(*req.StatusText) > 128 {
				util.Error(w, http.StatusBadRequest, "status text too long (max 128)")
				return
			}
			statusText = *req.StatusText
		}
		// Načíst existující status pokud jen jeden parametr
		existing, _ := d.Users.GetByID(u.ID)
		if existing != nil {
			if req.Status == nil {
				status = existing.Status
			}
			if req.StatusText == nil {
				statusText = existing.StatusText
			}
		}
		if err := d.Users.UpdateStatus(u.ID, status, statusText); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to update status")
			return
		}
	}

	user, _ := d.Users.GetByID(u.ID)

	// Broadcast member.update
	data, err := ws.NewEvent(ws.EventMemberUpdate, user)
	if err == nil {
		d.Hub.Broadcast(data)
	}

	util.JSON(w, http.StatusOK, user)
}

type timeoutRequest struct {
	Duration int    `json:"duration"` // sekundy
	Reason   string `json:"reason"`
}

func (d *Deps) KickUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermKick); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	targetID := r.PathValue("id")
	target, err := d.Users.GetByID(targetID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if target.IsOwner {
		util.Error(w, http.StatusBadRequest, "cannot timeout server owner")
		return
	}

	// Hierarchická kontrola
	if err := d.canActOn(user, targetID); err != nil {
		util.Error(w, http.StatusForbidden, "cannot timeout a user with higher or equal rank")
		return
	}

	var req timeoutRequest
	if err := util.DecodeJSON(r, &req); err != nil || req.Duration <= 0 {
		req.Duration = 3600 // default 1h
	}

	id, _ := uuid.NewV7()
	timeout := &models.Timeout{
		ID:        id.String(),
		UserID:    targetID,
		Reason:    req.Reason,
		IssuedBy:  user.ID,
		ExpiresAt: time.Now().Add(time.Duration(req.Duration) * time.Second),
	}

	if err := d.Timeouts.Create(timeout); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create timeout")
		return
	}

	// Invalidovat refresh tokeny — vynutí re-auth
	d.RefreshTokens.DeleteByUserID(targetID)

	// Broadcast odpojení
	timeoutEvt, _ := ws.NewEvent(ws.EventMemberTimeout, map[string]string{"id": targetID})
	d.Hub.Broadcast(timeoutEvt)

	d.logAudit(user.ID, "member.timeout", "user", targetID, map[string]any{"reason": req.Reason, "duration": req.Duration, "username": target.Username})

	util.JSON(w, http.StatusCreated, timeout)
}

func (d *Deps) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	// Max 512KB
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)

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

	// Smazat předchozí avatar soubor
	entries, _ := os.ReadDir(d.UploadsDir)
	prefix := "avatar_" + user.ID + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			os.Remove(filepath.Join(d.UploadsDir, e.Name()))
		}
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
		ext = ".png"
	}
	filename := "avatar_" + user.ID + ext

	os.MkdirAll(d.UploadsDir, 0755)
	dst, err := os.Create(filepath.Join(d.UploadsDir, filename))
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	avatarURL := "/api/uploads/" + filename
	if err := d.Users.UpdateAvatarURL(user.ID, avatarURL); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update avatar")
		return
	}

	updated, _ := d.Users.GetByID(user.ID)

	// Broadcast member.update
	data, err := ws.NewEvent(ws.EventMemberUpdate, updated)
	if err == nil {
		d.Hub.Broadcast(data)
	}

	util.JSON(w, http.StatusOK, updated)
}

func (d *Deps) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	// Smazat avatar soubor
	entries, _ := os.ReadDir(d.UploadsDir)
	prefix := "avatar_" + user.ID + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			os.Remove(filepath.Join(d.UploadsDir, e.Name()))
		}
	}

	if err := d.Users.UpdateAvatarURL(user.ID, ""); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update avatar")
		return
	}

	updated, _ := d.Users.GetByID(user.ID)

	// Broadcast member.update
	data, err := ws.NewEvent(ws.EventMemberUpdate, updated)
	if err == nil {
		d.Hub.Broadcast(data)
	}

	util.JSON(w, http.StatusOK, updated)
}

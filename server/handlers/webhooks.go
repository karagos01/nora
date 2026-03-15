package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

// ListWebhooks — GET /api/webhooks
func (d *Deps) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	hooks, err := d.Webhooks.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}
	if hooks == nil {
		hooks = []models.Webhook{}
	}

	util.JSON(w, http.StatusOK, hooks)
}

// CreateWebhook — POST /api/webhooks
func (d *Deps) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 80 {
		util.Error(w, http.StatusBadRequest, "name must be 1-80 characters")
		return
	}

	// Ověřit že kanál existuje
	if _, err := d.Channels.GetByID(req.ChannelID); err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	// Generovat token (32 random bytes → 64 hex znaků)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	token := hex.EncodeToString(tokenBytes)

	id, _ := uuid.NewV7()
	hook := &models.Webhook{
		ID:        id.String(),
		ChannelID: req.ChannelID,
		Name:      req.Name,
		Token:     token,
		CreatorID: user.ID,
		CreatedAt: time.Now().UTC(),
	}

	if err := d.Webhooks.Create(hook); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	event, _ := ws.NewEvent(ws.EventWebhookCreate, hook)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, hook)
}

// DeleteWebhook — DELETE /api/webhooks/{id}
func (d *Deps) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	hookID := r.PathValue("id")
	if err := d.Webhooks.Delete(hookID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}

	event, _ := ws.NewEvent(ws.EventWebhookDelete, map[string]string{"id": hookID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UpdateWebhook — PATCH /api/webhooks/{id}
func (d *Deps) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	hookID := r.PathValue("id")
	var req struct {
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 80 {
		util.Error(w, http.StatusBadRequest, "name must be 1-80 characters")
		return
	}

	if err := d.Webhooks.Update(hookID, req.Name, req.AvatarURL); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update webhook")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// WebhookSend — POST /api/webhooks/{id}/{token} (VEŘEJNÝ, bez autentizace)
func (d *Deps) WebhookSend(w http.ResponseWriter, r *http.Request) {
	hookID := r.PathValue("id")
	token := r.PathValue("token")

	hook, err := d.Webhooks.GetByID(hookID)
	if err != nil || subtle.ConstantTimeCompare([]byte(hook.Token), []byte(token)) != 1 {
		util.Error(w, http.StatusNotFound, "webhook not found")
		return
	}

	var req struct {
		Content  string `json:"content"`
		Username string `json:"username,omitempty"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Content == "" || len(req.Content) > 4000 {
		util.Error(w, http.StatusBadRequest, "content must be 1-4000 characters")
		return
	}

	// Ověřit že kanál stále existuje
	if _, err := d.Channels.GetByID(hook.ChannelID); err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	// Vytvořit zprávu pod jménem webhooku
	msgID, _ := uuid.NewV7()
	msg := &models.Message{
		ID:        msgID.String(),
		ChannelID: hook.ChannelID,
		UserID:    hook.CreatorID,
		Content:   req.Content,
		CreatedAt: time.Now().UTC(),
	}

	if err := d.Messages.Create(msg); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	// Webhook zpráva — použít jméno webhooku nebo override
	displayName := hook.Name
	if req.Username != "" {
		displayName = req.Username
	}

	msg.Author = &models.User{
		ID:          hook.CreatorID,
		Username:    displayName,
		DisplayName: "[webhook]",
		AvatarURL:   hook.AvatarURL,
	}

	event, _ := ws.NewEvent(ws.EventMessageNew, msg)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, map[string]string{"id": msg.ID})
}

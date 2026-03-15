package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
)

// ListChannelPermOverrides vrátí všechny permission overrides pro daný kanál.
// Vyžaduje PermManageChannels.
func (d *Deps) ListChannelPermOverrides(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Ověřit že kanál existuje
	if _, err := d.Channels.GetByID(channelID); err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	overrides, err := d.ChannelPermQ.GetForChannel(channelID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list overrides")
		return
	}
	if overrides == nil {
		overrides = []models.ChannelPermOverride{}
	}

	util.JSON(w, http.StatusOK, overrides)
}

// SetChannelPermOverride nastaví (nebo aktualizuje) permission override pro kanál.
// Vyžaduje PermManageChannels.
func (d *Deps) SetChannelPermOverride(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Ověřit že kanál existuje
	if _, err := d.Channels.GetByID(channelID); err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	var req models.ChannelPermOverride
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validace
	if req.TargetType != "role" && req.TargetType != "user" {
		util.Error(w, http.StatusBadRequest, "target_type must be 'role' or 'user'")
		return
	}
	if req.TargetID == "" {
		util.Error(w, http.StatusBadRequest, "target_id required")
		return
	}
	if req.Allow < 0 || req.Deny < 0 {
		util.Error(w, http.StatusBadRequest, "allow and deny must be >= 0")
		return
	}

	// Ověřit existenci targetu
	if req.TargetType == "role" {
		if _, err := d.Roles.GetByID(req.TargetID); err != nil {
			util.Error(w, http.StatusBadRequest, "role not found")
			return
		}
	} else {
		if _, err := d.Users.GetByID(req.TargetID); err != nil {
			util.Error(w, http.StatusBadRequest, "user not found")
			return
		}
	}

	req.ChannelID = channelID

	if err := d.ChannelPermQ.Set(req); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to set override")
		return
	}

	util.JSON(w, http.StatusOK, req)
}

// DeleteChannelPermOverride smaže permission override pro kanál.
// Vyžaduje PermManageChannels.
func (d *Deps) DeleteChannelPermOverride(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")
	targetType := r.PathValue("targetType")
	targetID := r.PathValue("targetId")

	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if targetType != "role" && targetType != "user" {
		util.Error(w, http.StatusBadRequest, "invalid target_type")
		return
	}

	if err := d.ChannelPermQ.Delete(channelID, targetType, targetID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete override")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

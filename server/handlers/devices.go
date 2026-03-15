package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"

	"github.com/google/uuid"
)

func (d *Deps) ListDevices(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	devices, err := d.UserDevices.ListAll()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	if devices == nil {
		devices = []models.UserDevice{}
	}
	util.JSON(w, http.StatusOK, devices)
}

func (d *Deps) ListUserDevices(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	devices, err := d.UserDevices.GetByUser(userID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list user devices")
		return
	}
	if devices == nil {
		devices = []models.UserDevice{}
	}
	util.JSON(w, http.StatusOK, devices)
}

func (d *Deps) ListDeviceBans(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	bans, err := d.DeviceBans.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list device bans")
		return
	}
	if bans == nil {
		bans = []models.DeviceBan{}
	}
	util.JSON(w, http.StatusOK, bans)
}

type createDeviceBanRequest struct {
	DeviceID     string `json:"device_id"`
	HardwareHash string `json:"hardware_hash"`
	Reason       string `json:"reason"`
}

func (d *Deps) CreateDeviceBan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req createDeviceBanRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DeviceID == "" && req.HardwareHash == "" {
		util.Error(w, http.StatusBadRequest, "device_id or hardware_hash required")
		return
	}

	id, _ := uuid.NewV7()
	ban := &models.DeviceBan{
		ID:           id.String(),
		DeviceID:     req.DeviceID,
		HardwareHash: req.HardwareHash,
		BannedBy:     user.ID,
		Reason:       req.Reason,
	}

	if err := d.DeviceBans.Create(ban); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create device ban")
		return
	}

	util.JSON(w, http.StatusCreated, ban)
}

func (d *Deps) DeleteDeviceBan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	if err := d.DeviceBans.Delete(id); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete device ban")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

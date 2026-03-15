package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

type createBanRequest struct {
	UserID         string `json:"user_id"`
	Reason         string `json:"reason"`
	BanIP          bool   `json:"ban_ip"`
	BanDevice      bool   `json:"ban_device"`
	RevokeInvites  bool   `json:"revoke_invites"`
	Duration       int    `json:"duration"` // sekundy, 0 = permanent
	DeleteMessages bool   `json:"delete_messages"`
}

func (d *Deps) ListBans(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	bans, err := d.Bans.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list bans")
		return
	}
	if bans == nil {
		bans = []models.Ban{}
	}
	util.JSON(w, http.StatusOK, bans)
}

func (d *Deps) CreateBan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req createBanRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.UserID == "" {
		util.Error(w, http.StatusBadRequest, "user_id is required")
		return
	}

	// Nelze banovat ownera
	target, err := d.Users.GetByID(req.UserID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}
	if target.IsOwner {
		util.Error(w, http.StatusBadRequest, "cannot ban server owner")
		return
	}

	// Hierarchická kontrola
	if err := d.canActOn(user, req.UserID); err != nil {
		util.Error(w, http.StatusForbidden, "cannot ban a user with higher or equal rank")
		return
	}

	// Vypočítat expires_at
	var expiresAt *time.Time
	if req.Duration > 0 {
		t := time.Now().Add(time.Duration(req.Duration) * time.Second)
		expiresAt = &t
	}

	id, _ := uuid.NewV7()
	ban := &models.Ban{
		ID:        id.String(),
		UserID:    req.UserID,
		Reason:    req.Reason,
		BannedBy:  user.ID,
		ExpiresAt: expiresAt,
	}

	if err := d.Bans.Create(ban); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create ban")
		return
	}

	// IP ban pokud požadováno
	if req.BanIP && target.LastIP != "" {
		d.IPBans.Create(target.LastIP, req.Reason, user.ID, req.UserID)
	}

	// Device ban — zabanovat všechna zařízení uživatele
	if req.BanDevice && d.UserDevices != nil && d.DeviceBans != nil {
		devices, _ := d.UserDevices.GetByUser(req.UserID)
		for _, dev := range devices {
			devBanID, _ := uuid.NewV7()
			d.DeviceBans.Create(&models.DeviceBan{
				ID:            devBanID.String(),
				DeviceID:      dev.DeviceID,
				HardwareHash:  dev.HardwareHash,
				RelatedUserID: req.UserID,
				BannedBy:      user.ID,
				Reason:        req.Reason,
				ExpiresAt:     expiresAt,
			})
		}
	}

	// Smazat invite kódy (podmíněně)
	if req.RevokeInvites {
		d.Invites.DeleteByCreatorID(req.UserID)
	}

	// Smazat zprávy
	if req.DeleteMessages {
		d.Messages.DeleteByUserID(req.UserID)
	}

	// Smazat scheduled messages (vždy — zabanovaný uživatel je nemá posílat)
	d.Scheduled.DeleteByUserID(req.UserID)

	// Invalidovat refresh tokeny
	d.RefreshTokens.DeleteByUserID(req.UserID)

	// Broadcast member.leave a odpojit uživatele z WS
	event, _ := ws.NewEvent(ws.EventMemberLeave, map[string]string{"id": req.UserID})
	d.Hub.Broadcast(event)
	d.Hub.DisconnectUser(req.UserID)

	// Broadcast ban.created pro adminy
	banEvent, _ := ws.NewEvent(ws.EventBanCreated, map[string]string{
		"user_id":  req.UserID,
		"username": target.Username,
		"reason":   req.Reason,
	})
	d.Hub.Broadcast(banEvent)

	d.logAudit(user.ID, "member.ban", "user", req.UserID, map[string]string{"reason": req.Reason, "username": target.Username})

	util.JSON(w, http.StatusCreated, ban)
}

func (d *Deps) DeleteBan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")

	// Smazat i IP bany spojené s tímto uživatelem
	d.IPBans.DeleteByRelatedUser(userID)

	// Smazat device bany pro tohoto uživatele
	if d.DeviceBans != nil {
		d.DeviceBans.DeleteByRelatedUser(userID)
	}

	if err := d.Bans.Delete(userID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove ban")
		return
	}

	d.logAudit(user.ID, "member.unban", "user", userID, nil)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) ListIPBans(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	bans, err := d.IPBans.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list IP bans")
		return
	}
	if bans == nil {
		bans = []models.IPBan{}
	}
	util.JSON(w, http.StatusOK, bans)
}

func (d *Deps) DeleteIPBan(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	ip := r.PathValue("ip")
	if err := d.IPBans.Delete(ip); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove IP ban")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

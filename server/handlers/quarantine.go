package handlers

import (
	"fmt"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
)

func (d *Deps) ListQuarantine(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	entries, err := d.Quarantine.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list quarantine")
		return
	}
	if entries == nil {
		entries = []models.QuarantineEntry{}
	}
	util.JSON(w, http.StatusOK, entries)
}

func (d *Deps) ApproveQuarantine(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	if err := d.Quarantine.Approve(userID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to approve quarantine")
		return
	}

	event, _ := ws.NewEvent(ws.EventQuarantineEnded, map[string]string{"user_id": userID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) RemoveQuarantine(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	if err := d.Quarantine.Delete(userID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove quarantine")
		return
	}

	event, _ := ws.NewEvent(ws.EventQuarantineEnded, map[string]string{"user_id": userID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// checkQuarantine checks whether the user is in quarantine and has a restriction on the given action.
func (d *Deps) checkQuarantine(userID, action string) error {
	if d.Quarantine == nil || !d.SecurityCfg.Quarantine.Enabled {
		return nil
	}
	if !d.Quarantine.IsInQuarantine(userID) {
		return nil
	}

	switch action {
	case "send_messages":
		if d.SecurityCfg.Quarantine.RestrictSendMessages {
			return fmt.Errorf("you are in quarantine and cannot send messages yet")
		}
	case "upload":
		if d.SecurityCfg.Quarantine.RestrictUpload {
			return fmt.Errorf("you are in quarantine and cannot upload files yet")
		}
	case "invite":
		if d.SecurityCfg.Quarantine.RestrictInvite {
			return fmt.Errorf("you are in quarantine and cannot create invites yet")
		}
	case "dm":
		if d.SecurityCfg.Quarantine.RestrictDM {
			return fmt.Errorf("you are in quarantine and cannot send DMs yet")
		}
	}
	return nil
}

package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"
)

func (d *Deps) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	// PermBan or PermApproveMembers
	err1 := d.requirePermission(user, models.PermBan)
	err2 := d.requirePermission(user, models.PermApproveMembers)
	if err1 != nil && err2 != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	approvals, err := d.Approvals.ListPending()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list approvals")
		return
	}
	if approvals == nil {
		approvals = []models.PendingApproval{}
	}
	util.JSON(w, http.StatusOK, approvals)
}

func (d *Deps) ApproveUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	err1 := d.requirePermission(user, models.PermBan)
	err2 := d.requirePermission(user, models.PermApproveMembers)
	if err1 != nil && err2 != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	if err := d.Approvals.Approve(userID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to approve user")
		return
	}

	// If quarantine enabled — create quarantine
	if d.SecurityCfg.Quarantine.Enabled {
		var endsAt *time.Time
		if d.SecurityCfg.Quarantine.DurationDays > 0 {
			t := time.Now().Add(time.Duration(d.SecurityCfg.Quarantine.DurationDays) * 24 * time.Hour)
			endsAt = &t
		}
		d.Quarantine.Create(userID, endsAt)
	}

	// Broadcast member.join
	target, err := d.Users.GetByID(userID)
	if err == nil {
		joinMsg, _ := ws.NewEvent(ws.EventMemberJoin, target)
		d.Hub.Broadcast(joinMsg)
	}

	// Broadcast approval.resolved
	event, _ := ws.NewEvent(ws.EventApprovalResolved, map[string]string{
		"user_id": userID,
		"status":  "approved",
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) RejectUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	err1 := d.requirePermission(user, models.PermBan)
	err2 := d.requirePermission(user, models.PermApproveMembers)
	if err1 != nil && err2 != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	if err := d.Approvals.Reject(userID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to reject user")
		return
	}

	// Delete user
	d.Users.Delete(userID)
	d.RefreshTokens.DeleteByUserID(userID)

	// Broadcast approval.resolved
	event, _ := ws.NewEvent(ws.EventApprovalResolved, map[string]string{
		"user_id": userID,
		"status":  "rejected",
	})
	d.Hub.Broadcast(event)

	// Disconnect from WS
	d.Hub.DisconnectUser(userID)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

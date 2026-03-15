package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
)

type blockUserRequest struct {
	UserID string `json:"user_id"`
}

func (d *Deps) ListBlocks(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	blocked, err := d.Blocks.List(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list blocks")
		return
	}
	if blocked == nil {
		blocked = []models.User{}
	}
	util.JSON(w, http.StatusOK, blocked)
}

func (d *Deps) BlockUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var req blockUserRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.UserID == "" {
		util.Error(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.UserID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot block yourself")
		return
	}

	target, err := d.Users.GetByID(req.UserID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}

	if err := d.Blocks.Add(user.ID, req.UserID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to block user")
		return
	}

	// Odebrat přátelství pokud existuje
	areFriends, _ := d.Friends.AreFriends(user.ID, req.UserID)
	if areFriends {
		d.Friends.Remove(user.ID, req.UserID)

		payload1 := map[string]string{"user_id": req.UserID}
		ev1, _ := ws.NewEvent(ws.EventFriendRemove, payload1)
		d.Hub.BroadcastToUser(user.ID, ev1)

		payload2 := map[string]string{"user_id": user.ID}
		ev2, _ := ws.NewEvent(ws.EventFriendRemove, payload2)
		d.Hub.BroadcastToUser(req.UserID, ev2)
	}

	// Smazat pending friend requesty mezi nimi
	d.FriendRequests.DeleteBetween(user.ID, req.UserID)

	// WS block.add jen blockérovi
	event, _ := ws.NewEvent(ws.EventBlockAdd, target)
	d.Hub.BroadcastToUser(user.ID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) UnblockUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	blockedID := r.PathValue("userId")

	if err := d.Blocks.Remove(user.ID, blockedID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to unblock user")
		return
	}

	event, _ := ws.NewEvent(ws.EventBlockRemove, map[string]string{"user_id": blockedID})
	d.Hub.BroadcastToUser(user.ID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

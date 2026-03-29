package handlers

import (
	"net/http"
	"nora/auth"
	"strings"
	"nora/models"
	"nora/util"
	"nora/ws"
)

type sendFriendRequestReq struct {
	UserID    string `json:"user_id"`
	PublicKey string `json:"public_key"`
}

func (d *Deps) ListFriends(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	friends, err := d.Friends.List(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list friends")
		return
	}
	if friends == nil {
		friends = []models.User{}
	}
	util.JSON(w, http.StatusOK, friends)
}

func (d *Deps) RemoveFriend(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	friendID := r.PathValue("userId")

	if err := d.Friends.Remove(user.ID, friendID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove friend")
		return
	}

	payload := map[string]string{"user_id": friendID}
	event, _ := ws.NewEvent(ws.EventFriendRemove, payload)
	d.Hub.BroadcastToUser(user.ID, event)

	payload2 := map[string]string{"user_id": user.ID}
	event2, _ := ws.NewEvent(ws.EventFriendRemove, payload2)
	d.Hub.BroadcastToUser(friendID, event2)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) SendFriendRequest(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var req sendFriendRequestReq
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve user by ID or public key (strip #server suffix if present)
	if req.PublicKey != "" {
		if idx := strings.Index(req.PublicKey, "#"); idx > 0 {
			req.PublicKey = req.PublicKey[:idx]
		}
	}
	if req.UserID == "" && req.PublicKey != "" {
		u, err := d.Users.GetByPublicKey(req.PublicKey)
		if err != nil {
			util.Error(w, http.StatusNotFound, "user not found")
			return
		}
		req.UserID = u.ID
	}
	if req.UserID == "" {
		util.Error(w, http.StatusBadRequest, "user_id or public_key is required")
		return
	}
	if req.UserID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot send request to yourself")
		return
	}

	// Verify that the target user exists
	target, err := d.Users.GetByID(req.UserID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}

	// Block check
	blocked, _ := d.Blocks.EitherBlocked(user.ID, req.UserID)
	if blocked {
		util.Error(w, http.StatusForbidden, "this user is blocked")
		return
	}

	// Check if already friends
	areFriends, err := d.Friends.AreFriends(user.ID, req.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to check friendship")
		return
	}
	if areFriends {
		util.Error(w, http.StatusConflict, "already friends")
		return
	}

	// Check for existing request (both directions)
	exists, err := d.FriendRequests.Exists(user.ID, req.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to check existing request")
		return
	}
	if exists {
		util.Error(w, http.StatusConflict, "friend request already exists")
		return
	}

	// Create request
	fr, err := d.FriendRequests.Create(user.ID, req.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create friend request")
		return
	}

	// Add from_user info
	caller, _ := d.Users.GetByID(user.ID)
	fr.FromUser = caller
	fr.ToUser = target

	// WS notification to target user
	event, _ := ws.NewEvent(ws.EventFriendRequest, fr)
	d.Hub.BroadcastToUser(req.UserID, event)

	util.JSON(w, http.StatusCreated, fr)
}

func (d *Deps) ListFriendRequests(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	incoming, err := d.FriendRequests.ListPendingForUser(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list incoming requests")
		return
	}
	if incoming == nil {
		incoming = []models.FriendRequest{}
	}

	sent, err := d.FriendRequests.ListSentByUser(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list sent requests")
		return
	}
	if sent == nil {
		sent = []models.FriendRequest{}
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"incoming": incoming,
		"sent":     sent,
	})
}

func (d *Deps) AcceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	reqID := r.PathValue("id")

	fr, err := d.FriendRequests.GetByID(reqID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "friend request not found")
		return
	}

	// Only to_user can accept
	if fr.ToUserID != user.ID {
		util.Error(w, http.StatusForbidden, "only the recipient can accept")
		return
	}

	// Create friendship
	if err := d.Friends.Add(fr.FromUserID, fr.ToUserID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to add friend")
		return
	}

	// Delete request
	d.FriendRequests.Delete(reqID)

	// WS to both — friend.add
	fromUser, _ := d.Users.GetByID(fr.FromUserID)
	toUser, _ := d.Users.GetByID(fr.ToUserID)

	if fromUser != nil {
		event, _ := ws.NewEvent(ws.EventFriendAdd, toUser)
		d.Hub.BroadcastToUser(fr.FromUserID, event)
	}
	if toUser != nil {
		event, _ := ws.NewEvent(ws.EventFriendAdd, fromUser)
		d.Hub.BroadcastToUser(fr.ToUserID, event)
	}

	// WS notification about acceptance
	acceptEvent, _ := ws.NewEvent(ws.EventFriendRequestAccept, map[string]string{"id": reqID})
	d.Hub.BroadcastToUser(fr.FromUserID, acceptEvent)
	d.Hub.BroadcastToUser(fr.ToUserID, acceptEvent)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) DeclineFriendRequest(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	reqID := r.PathValue("id")

	fr, err := d.FriendRequests.GetByID(reqID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "friend request not found")
		return
	}

	// from or to user can decline
	if fr.FromUserID != user.ID && fr.ToUserID != user.ID {
		util.Error(w, http.StatusForbidden, "not your friend request")
		return
	}

	d.FriendRequests.Delete(reqID)

	// WS notification to both
	event, _ := ws.NewEvent(ws.EventFriendRequestDecline, map[string]string{"id": reqID})
	d.Hub.BroadcastToUser(fr.FromUserID, event)
	d.Hub.BroadcastToUser(fr.ToUserID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

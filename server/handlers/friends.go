package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
)

type sendFriendRequestReq struct {
	UserID string `json:"user_id"`
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

	if req.UserID == "" {
		util.Error(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.UserID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot send request to yourself")
		return
	}

	// Ověřit že cílový uživatel existuje
	target, err := d.Users.GetByID(req.UserID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}

	// Block kontrola
	blocked, _ := d.Blocks.EitherBlocked(user.ID, req.UserID)
	if blocked {
		util.Error(w, http.StatusForbidden, "this user is blocked")
		return
	}

	// Kontrola zda už jsou přátelé
	areFriends, err := d.Friends.AreFriends(user.ID, req.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to check friendship")
		return
	}
	if areFriends {
		util.Error(w, http.StatusConflict, "already friends")
		return
	}

	// Kontrola existujícího requestu (oba směry)
	exists, err := d.FriendRequests.Exists(user.ID, req.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to check existing request")
		return
	}
	if exists {
		util.Error(w, http.StatusConflict, "friend request already exists")
		return
	}

	// Vytvořit request
	fr, err := d.FriendRequests.Create(user.ID, req.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create friend request")
		return
	}

	// Přidat from_user info
	caller, _ := d.Users.GetByID(user.ID)
	fr.FromUser = caller
	fr.ToUser = target

	// WS notifikace cílovému uživateli
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

	// Jen to_user může přijmout
	if fr.ToUserID != user.ID {
		util.Error(w, http.StatusForbidden, "only the recipient can accept")
		return
	}

	// Vytvořit přátelství
	if err := d.Friends.Add(fr.FromUserID, fr.ToUserID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to add friend")
		return
	}

	// Smazat request
	d.FriendRequests.Delete(reqID)

	// WS oběma - friend.add
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

	// WS notifikace o acceptu
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

	// from nebo to user může odmítnout
	if fr.FromUserID != user.ID && fr.ToUserID != user.ID {
		util.Error(w, http.StatusForbidden, "not your friend request")
		return
	}

	d.FriendRequests.Delete(reqID)

	// WS notifikace oběma
	event, _ := ws.NewEvent(ws.EventFriendRequestDecline, map[string]string{"id": reqID})
	d.Hub.BroadcastToUser(fr.FromUserID, event)
	d.Hub.BroadcastToUser(fr.ToUserID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

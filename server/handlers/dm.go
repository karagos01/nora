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

type createDMRequest struct {
	UserID string `json:"user_id"`
}

type createDMMessageRequest struct {
	EncryptedContent string `json:"encrypted_content"`
	ReplyToID        string `json:"reply_to_id,omitempty"`
}

func (d *Deps) ListDMConversations(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	convs, err := d.DMs.ListConversations(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list conversations")
		return
	}
	if convs == nil {
		convs = []models.DMConversation{}
	}

	type convWithParticipants struct {
		models.DMConversation
		Participants []models.DMParticipant `json:"participants"`
		PendingCount int                    `json:"pending_count"`
	}

	var result []convWithParticipants
	for _, c := range convs {
		parts, _ := d.DMs.GetParticipants(c.ID)
		result = append(result, convWithParticipants{
			DMConversation: c,
			Participants:   parts,
			PendingCount:   d.DMs.PendingCount(c.ID, user.ID),
		})
	}

	util.JSON(w, http.StatusOK, result)
}

func (d *Deps) CreateDMConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	if err := d.checkQuarantine(user.ID, "dm"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	var req createDMRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.UserID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot DM yourself")
		return
	}

	// Existing conversation?
	existing, err := d.DMs.FindConversation(user.ID, req.UserID)
	if err == nil {
		util.JSON(w, http.StatusOK, existing)
		return
	}

	// Block check
	blocked, _ := d.Blocks.EitherBlocked(user.ID, req.UserID)
	if blocked {
		util.Error(w, http.StatusForbidden, "this user is blocked")
		return
	}

	target, err := d.Users.GetByID(req.UserID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}
	me, _ := d.Users.GetByID(user.ID)

	id, _ := uuid.NewV7()
	conv := &models.DMConversation{ID: id.String()}
	participants := []models.DMParticipant{
		{ConversationID: conv.ID, UserID: me.ID, PublicKey: me.PublicKey},
		{ConversationID: conv.ID, UserID: target.ID, PublicKey: target.PublicKey},
	}

	if err := d.DMs.CreateConversation(conv, participants); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}

	util.JSON(w, http.StatusCreated, conv)
}

const maxEncryptedContentLen = 16000 // hex-encoded encrypted content

func (d *Deps) CreateDMMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	convID := r.PathValue("id")

	if err := d.checkQuarantine(user.ID, "dm"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	ok, _ := d.DMs.IsParticipant(convID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a participant")
		return
	}

	// Block check — find the other party
	blockParts, _ := d.DMs.GetParticipants(convID)
	for _, p := range blockParts {
		if p.UserID != user.ID {
			blocked, _ := d.Blocks.EitherBlocked(user.ID, p.UserID)
			if blocked {
				util.Error(w, http.StatusForbidden, "this user is blocked")
				return
			}
			break
		}
	}

	var req createDMMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.EncryptedContent) == 0 {
		util.Error(w, http.StatusBadRequest, "encrypted_content cannot be empty")
		return
	}
	if len(req.EncryptedContent) > maxEncryptedContentLen {
		util.Error(w, http.StatusBadRequest, "encrypted_content too long")
		return
	}

	id, _ := uuid.NewV7()
	msg := &models.DMPendingMessage{
		ID:               id.String(),
		ConversationID:   convID,
		SenderID:         user.ID,
		EncryptedContent: req.EncryptedContent,
		ReplyToID:        req.ReplyToID,
		CreatedAt:        time.Now().UTC(),
	}

	author, _ := d.Users.GetByID(user.ID)
	if author != nil {
		msg.Author = author
	}

	// Broadcast WS event to all participants
	parts, _ := d.DMs.GetParticipants(convID)
	event, _ := ws.NewEvent(ws.EventDMMessage, msg)
	for _, p := range parts {
		d.Hub.BroadcastToUser(p.UserID, event)
	}

	// Store as pending for offline recipients
	for _, p := range parts {
		if p.UserID != user.ID && !d.Hub.IsUserOnline(p.UserID) {
			d.DMs.CreatePending(msg)
			break
		}
	}

	util.JSON(w, http.StatusCreated, msg)
}

func (d *Deps) DeleteDMConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	convID := r.PathValue("id")

	ok, _ := d.DMs.IsParticipant(convID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a participant")
		return
	}

	// Get participants before deletion
	parts, _ := d.DMs.GetParticipants(convID)

	if err := d.DMs.DeleteConversation(convID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete conversation")
		return
	}

	// Broadcast dm.delete to both parties
	event, _ := ws.NewEvent(ws.EventDMDelete, map[string]string{
		"conversation_id": convID,
	})
	for _, p := range parts {
		d.Hub.BroadcastToUser(p.UserID, event)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) GetDMPending(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	convID := r.PathValue("id")

	ok, _ := d.DMs.IsParticipant(convID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a participant")
		return
	}

	messages, err := d.DMs.ListPending(convID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list pending messages")
		return
	}
	if messages == nil {
		messages = []models.DMPendingMessage{}
	}

	// Delete received pending messages (those sent by someone else)
	d.DMs.DeletePending(convID, user.ID)

	util.JSON(w, http.StatusOK, messages)
}

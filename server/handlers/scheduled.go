package handlers

import (
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

type scheduleMessageRequest struct {
	Content     string    `json:"content"`
	ReplyToID   string    `json:"reply_to_id,omitempty"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

func (d *Deps) CreateScheduledMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	// Verify channel
	_, err := d.Channels.GetByID(channelID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req scheduleMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if msg := util.ValidateMessageContent(req.Content); msg != "" {
		util.Error(w, http.StatusBadRequest, msg)
		return
	}

	// Validate scheduled_at — min +1 min, max 7 days
	now := time.Now().UTC()
	if req.ScheduledAt.Before(now.Add(1 * time.Minute)) {
		util.Error(w, http.StatusBadRequest, "scheduled_at must be at least 1 minute in the future")
		return
	}
	if req.ScheduledAt.After(now.Add(7 * 24 * time.Hour)) {
		util.Error(w, http.StatusBadRequest, "scheduled_at must be within 7 days")
		return
	}

	// Limit 25 per user
	count, err := d.Scheduled.CountByUser(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to check limit")
		return
	}
	if count >= 25 {
		util.Error(w, http.StatusBadRequest, "maximum 25 scheduled messages per user")
		return
	}

	// Reply validation
	var replyToID *string
	if req.ReplyToID != "" {
		replyMsg, err := d.Messages.GetByID(req.ReplyToID)
		if err != nil || replyMsg.ChannelID != channelID {
			util.Error(w, http.StatusBadRequest, "invalid reply_to_id")
			return
		}
		replyToID = &req.ReplyToID
	}

	id, _ := uuid.NewV7()
	msg := &models.ScheduledMessage{
		ID:          id.String(),
		ChannelID:   channelID,
		UserID:      user.ID,
		Content:     req.Content,
		ReplyToID:   replyToID,
		ScheduledAt: req.ScheduledAt.UTC(),
		CreatedAt:   now,
	}

	if err := d.Scheduled.Create(msg); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to schedule message")
		return
	}

	util.JSON(w, http.StatusCreated, msg)
}

func (d *Deps) ListScheduledMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	msgs, err := d.Scheduled.ListByUser(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list scheduled messages")
		return
	}
	if msgs == nil {
		msgs = []models.ScheduledMessage{}
	}

	util.JSON(w, http.StatusOK, msgs)
}

func (d *Deps) DeleteScheduledMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	id := r.PathValue("id")

	if err := d.Scheduled.Delete(id, user.ID); err != nil {
		util.Error(w, http.StatusNotFound, "scheduled message not found")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DispatchScheduledMessages sends scheduled messages that have reached their scheduled_at time.
// Called from a ticker goroutine in main.go.
func (d *Deps) DispatchScheduledMessages() {
	due, err := d.Scheduled.ListDue(time.Now().UTC())
	if err != nil {
		slog.Error("scheduled: listing due messages failed", "error", err)
		return
	}

	for _, sm := range due {
		// Verify the channel still exists
		_, chErr := d.Channels.GetByID(sm.ChannelID)
		if chErr != nil {
			d.Scheduled.DeleteByID(sm.ID)
			continue
		}

		// Verify the user is not banned
		banned := d.Bans.IsBanned(sm.UserID)
		if banned {
			d.Scheduled.DeleteByID(sm.ID)
			continue
		}

		// Verify the user does not have a timeout
		if t, _ := d.Timeouts.GetActive(sm.UserID); t != nil {
			// Timeout is temporary — do not send, but do not delete either (will be sent after expiration)
			continue
		}

		// Verify the user is not in quarantine
		if err := d.checkQuarantine(sm.UserID, "send_messages"); err != nil {
			continue
		}

		// Create message
		msgID, _ := uuid.NewV7()
		msg := &models.Message{
			ID:        msgID.String(),
			ChannelID: sm.ChannelID,
			UserID:    sm.UserID,
			Content:   sm.Content,
			ReplyToID: sm.ReplyToID,
			CreatedAt: time.Now().UTC(),
		}

		if err := d.Messages.Create(msg); err != nil {
			slog.Error("scheduled: message creation failed", "scheduled_id", sm.ID, "error", err)
			continue
		}

		// Load author for broadcast
		author, _ := d.Users.GetByID(sm.UserID)
		if author != nil {
			msg.Author = author
		}

		// Reply data
		if msg.ReplyToID != nil {
			if replyMsg, err := d.Messages.GetByID(*msg.ReplyToID); err == nil {
				msg.ReplyTo = &models.Message{
					ID:      replyMsg.ID,
					Content: replyMsg.Content,
					Author:  replyMsg.Author,
				}
			}
		}

		event, _ := ws.NewEvent(ws.EventMessageNew, msg)
		d.Hub.Broadcast(event)

		// Link preview
		go d.fetchAndStoreLinkPreview(msg.ID, msg.Content)

		// Delete from scheduled
		d.Scheduled.DeleteByID(sm.ID)
	}
}

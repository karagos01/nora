package handlers

import (
	"net/http"
	"nora/auth"
	"nora/database/queries"
	"nora/models"
	"nora/util"
)

// MarkChannelRead marks a channel as read up to the given message ID.
func (d *Deps) MarkChannelRead(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	if err := d.requireChannelPermission(user, channelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		MessageID string `json:"message_id"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.MessageID == "" {
		util.Error(w, http.StatusBadRequest, "message_id required")
		return
	}

	if err := d.ReadState.MarkRead(user.ID, channelID, req.MessageID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetUnreadCounts returns unread message counts for all channels.
func (d *Deps) GetUnreadCounts(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	counts, err := d.ReadState.GetUnreadCounts(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get unread counts")
		return
	}
	if counts == nil {
		counts = make([]queries.ChannelUnread, 0)
	}

	util.JSON(w, http.StatusOK, counts)
}

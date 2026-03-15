package handlers

import (
	"net/http"
	"nora/auth"
	"nora/util"
	"nora/ws"
)

type toggleReactionRequest struct {
	Emoji string `json:"emoji"`
}

func (d *Deps) ToggleReaction(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	messageID := r.PathValue("id")

	var req toggleReactionRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Emoji == "" || len(req.Emoji) > 64 {
		util.Error(w, http.StatusBadRequest, "invalid emoji")
		return
	}

	// Verify the message exists
	if _, err := d.Messages.GetOwnerID(messageID); err != nil {
		util.Error(w, http.StatusNotFound, "message not found")
		return
	}

	exists, err := d.Reactions.Exists(messageID, user.ID, req.Emoji)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to check reaction")
		return
	}

	payload := map[string]string{
		"message_id": messageID,
		"user_id":    user.ID,
		"emoji":      req.Emoji,
	}

	if exists {
		if err := d.Reactions.Remove(messageID, user.ID, req.Emoji); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to remove reaction")
			return
		}
		event, _ := ws.NewEvent(ws.EventReactionRemove, payload)
		d.Hub.Broadcast(event)
	} else {
		// 👍/👎 are mutually exclusive — remove the opposite if it exists
		opposites := map[string]string{"👍": "👎", "👎": "👍"}
		if opp, ok := opposites[req.Emoji]; ok {
			if has, _ := d.Reactions.Exists(messageID, user.ID, opp); has {
				d.Reactions.Remove(messageID, user.ID, opp)
				removePayload := map[string]string{
					"message_id": messageID,
					"user_id":    user.ID,
					"emoji":      opp,
				}
				ev, _ := ws.NewEvent(ws.EventReactionRemove, removePayload)
				d.Hub.Broadcast(ev)
			}
		}

		if err := d.Reactions.Add(messageID, user.ID, req.Emoji); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to add reaction")
			return
		}
		event, _ := ws.NewEvent(ws.EventReactionAdd, payload)
		d.Hub.Broadcast(event)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

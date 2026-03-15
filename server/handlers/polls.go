package handlers

import (
	"net/http"
	"nora/auth"
	"nora/util"
	"nora/ws"
	"time"
)

type votePollRequest struct {
	OptionID string `json:"option_id"`
}

func (d *Deps) VotePoll(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	pollID := r.PathValue("id")

	var req votePollRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.OptionID == "" {
		util.Error(w, http.StatusBadRequest, "option_id required")
		return
	}

	// Verify the poll exists
	pollType, err := d.Polls.GetPollType(pollID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "poll not found")
		return
	}

	// Verify the option belongs to the poll
	belongs, err := d.Polls.OptionBelongsToPoll(pollID, req.OptionID)
	if err != nil || !belongs {
		util.Error(w, http.StatusBadRequest, "option does not belong to this poll")
		return
	}

	// Check expiration — load poll for expires_at
	poll, err := d.Polls.GetByID(pollID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "poll not found")
		return
	}
	if poll.ExpiresAt != nil && poll.ExpiresAt.Before(time.Now()) {
		util.Error(w, http.StatusForbidden, "poll has expired")
		return
	}

	switch pollType {
	case "simple", "anonymous":
		// Max 1 vote — if user already voted, toggle
		voted, _ := d.Polls.HasVoted(pollID, req.OptionID, user.ID)
		if voted {
			// Unvote (toggle off)
			d.Polls.Unvote(pollID, req.OptionID, user.ID)
		} else {
			// Remove previous vote (if exists) and add new one
			d.Polls.UnvoteAll(pollID, user.ID)
			d.Polls.Vote(pollID, req.OptionID, user.ID)
		}

	case "multi":
		// Toggle — vote if not voted, unvote if already voted
		voted, _ := d.Polls.HasVoted(pollID, req.OptionID, user.ID)
		if voted {
			d.Polls.Unvote(pollID, req.OptionID, user.ID)
		} else {
			d.Polls.Vote(pollID, req.OptionID, user.ID)
		}

	default:
		util.Error(w, http.StatusBadRequest, "unknown poll type")
		return
	}

	// Load updated poll
	poll, err = d.Polls.GetByID(pollID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to load poll")
		return
	}

	// WS broadcast
	event, _ := ws.NewEvent(ws.EventPollVote, poll)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, poll)
}

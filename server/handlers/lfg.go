package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"strings"
	"time"

	"github.com/google/uuid"
)

type createLFGRequest struct {
	GameName string `json:"game_name"`
	Content  string `json:"content"`
}

func (d *Deps) ListLFGListings(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	if err := d.requireChannelPermission(user, channelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	listings, err := d.LFGQ.ListByChannel(channelID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list LFG listings")
		return
	}
	if listings == nil {
		listings = []models.LFGListing{}
	}
	util.JSON(w, http.StatusOK, listings)
}

func (d *Deps) CreateLFGListing(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")

	if err := d.requireChannelPermission(user, channelID, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req createLFGRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.GameName = strings.TrimSpace(req.GameName)
	req.Content = strings.TrimSpace(req.Content)

	if req.GameName == "" || len(req.GameName) > 100 {
		util.Error(w, http.StatusBadRequest, "game_name is required (max 100 chars)")
		return
	}
	if req.Content == "" || len(req.Content) > 2000 {
		util.Error(w, http.StatusBadRequest, "content is required (max 2000 chars)")
		return
	}

	now := time.Now().UTC()
	id, _ := uuid.NewV7()
	listing := &models.LFGListing{
		ID:        id.String(),
		UserID:    user.ID,
		ChannelID: channelID,
		GameName:  req.GameName,
		Content:   req.Content,
		CreatedAt: now,
		ExpiresAt: now.Add(7 * 24 * time.Hour),
	}

	if err := d.LFGQ.Create(listing); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create LFG listing")
		return
	}

	// Attach author info
	u, _ := d.Users.GetByID(user.ID)
	if u != nil {
		listing.Author = u
	}

	msg, _ := ws.NewEvent(ws.EventLFGCreate, listing)
	d.broadcastToChannelReaders(channelID, msg)

	util.JSON(w, http.StatusCreated, listing)
}

func (d *Deps) DeleteLFGListing(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")
	listingID := r.PathValue("listingId")

	listing, err := d.LFGQ.GetByID(listingID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "listing not found")
		return
	}

	// Author can always delete their own; otherwise need MANAGE_MESSAGES
	if listing.UserID != user.ID {
		if err := d.requireChannelPermission(user, channelID, models.PermManageMessages); err != nil {
			util.Error(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		if err := d.LFGQ.DeleteByID(listingID); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to delete LFG listing")
			return
		}
	} else {
		if err := d.LFGQ.Delete(listingID, user.ID); err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to delete LFG listing")
			return
		}
	}

	msg, _ := ws.NewEvent(ws.EventLFGDelete, map[string]string{
		"id":         listingID,
		"channel_id": channelID,
	})
	d.broadcastToChannelReaders(channelID, msg)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

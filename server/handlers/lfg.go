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
	GameName   string `json:"game_name"`
	Content    string `json:"content"`
	MaxPlayers int    `json:"max_players"`
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
	// Only show applications to listing authors
	for i := range listings {
		if listings[i].UserID != user.ID {
			listings[i].Applications = nil
		}
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

	if req.GameName == "" || len([]rune(req.GameName)) > 30 {
		util.Error(w, http.StatusBadRequest, "game_name is required (max 30 chars)")
		return
	}
	if req.Content == "" || len([]rune(req.Content)) > 250 {
		util.Error(w, http.StatusBadRequest, "content is required (max 250 chars)")
		return
	}
	if req.MaxPlayers < 0 || req.MaxPlayers > 100 {
		req.MaxPlayers = 0
	}

	// Auto-create a group for this LFG listing
	groupID, _ := uuid.NewV7()
	group := &models.Group{
		ID:        groupID.String(),
		Name:      "LFG: " + req.GameName,
		CreatorID: user.ID,
		CreatedAt: time.Now().UTC(),
	}
	if err := d.Groups.Create(group); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create group")
		return
	}
	// Add creator as group member
	fullUser, _ := d.Users.GetByID(user.ID)
	if fullUser != nil {
		d.Groups.AddMember(&models.GroupMember{GroupID: group.ID, UserID: user.ID, PublicKey: fullUser.PublicKey})
	}

	now := time.Now().UTC()
	id, _ := uuid.NewV7()
	listing := &models.LFGListing{
		ID:         id.String(),
		UserID:     user.ID,
		ChannelID:  channelID,
		GroupID:    group.ID,
		GameName:   req.GameName,
		Content:    req.Content,
		MaxPlayers: req.MaxPlayers,
		CreatedAt:  now,
		ExpiresAt:  now.Add(7 * 24 * time.Hour),
	}

	if err := d.LFGQ.Create(listing); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create LFG listing")
		return
	}

	// Auto-join creator as participant
	d.LFGQ.Join(listing.ID, user.ID, 0)

	// Attach author info + participants
	u, _ := d.Users.GetByID(user.ID)
	if u != nil {
		listing.Author = u
		listing.Participants = []models.User{*u}
	}

	msg, _ := ws.NewEvent(ws.EventLFGCreate, listing)
	d.broadcastToChannelReaders(channelID, msg)

	// Notify group creation to creator
	groupMsg, _ := ws.NewEvent(ws.EventGroupCreate, group)
	d.Hub.BroadcastToUser(user.ID, groupMsg)

	util.JSON(w, http.StatusCreated, listing)
}

// JoinLFGListing — direct join (no limit) or auto-apply (with limit).
func (d *Deps) JoinLFGListing(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")
	listingID := r.PathValue("listingId")

	if err := d.requireChannelPermission(user, channelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	listing, err := d.LFGQ.GetByID(listingID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "listing not found")
		return
	}
	if listing.ChannelID != channelID {
		util.Error(w, http.StatusBadRequest, "listing does not belong to this channel")
		return
	}

	// No limit → direct join + add to group
	if listing.MaxPlayers == 0 {
		d.LFGQ.Join(listingID, user.ID, 0)

		// Add to group
		if listing.GroupID != "" {
			joiner, _ := d.Users.GetByID(user.ID)
			pubKey := ""
			if joiner != nil {
				pubKey = joiner.PublicKey
			}
			d.Groups.AddMember(&models.GroupMember{GroupID: listing.GroupID, UserID: user.ID, PublicKey: pubKey})
			// Notify group member join
			joinPayload := map[string]string{
				"group_id":   listing.GroupID,
				"user_id":    user.ID,
				"public_key": pubKey,
				"username":   user.Username,
			}
			joinMsg, _ := ws.NewEvent(ws.EventGroupMemberJoin, joinPayload)
			if members, err := d.Groups.GetMembers(listing.GroupID); err == nil {
				for _, m := range members {
					d.Hub.BroadcastToUser(m.UserID, joinMsg)
				}
			}
		}

		participants, _ := d.LFGQ.GetParticipants(listingID)
		payload := map[string]interface{}{
			"listing_id":   listingID,
			"channel_id":   channelID,
			"user_id":      user.ID,
			"participants": participants,
		}
		msg, _ := ws.NewEvent(ws.EventLFGJoin, payload)
		d.broadcastToChannelReaders(channelID, msg)

		util.JSON(w, http.StatusOK, payload)
		return
	}

	// With limit → this endpoint shouldn't be called directly; use /apply instead
	util.Error(w, http.StatusBadRequest, "this listing requires an application (use /apply)")
}

// ApplyLFGListing — submit an application with a message.
func (d *Deps) ApplyLFGListing(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")
	listingID := r.PathValue("listingId")

	if err := d.requireChannelPermission(user, channelID, models.PermRead); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	listing, err := d.LFGQ.GetByID(listingID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "listing not found")
		return
	}

	// Can't apply to your own listing
	if listing.UserID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot apply to your own listing")
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	util.DecodeJSON(r, &req)
	req.Message = strings.TrimSpace(req.Message)
	if len(req.Message) > 500 {
		req.Message = req.Message[:500]
	}

	if err := d.LFGQ.Apply(listingID, user.ID, req.Message); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to apply")
		return
	}

	// Get updated applications
	apps, _ := d.LFGQ.GetApplications(listingID)

	// Notify listing author
	u, _ := d.Users.GetByID(user.ID)
	payload := map[string]interface{}{
		"listing_id":   listingID,
		"channel_id":   channelID,
		"user_id":      user.ID,
		"applications": apps,
	}
	msg, _ := ws.NewEvent("lfg.apply", payload)
	d.Hub.BroadcastToUser(listing.UserID, msg)

	// Notify applicant
	resp := map[string]interface{}{
		"listing_id": listingID,
		"status":     "pending",
	}
	if u != nil {
		resp["user"] = u
	}
	util.JSON(w, http.StatusOK, resp)
}

// AcceptLFGApplication — accept an application → add user to group + participants.
func (d *Deps) AcceptLFGApplication(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	listingID := r.PathValue("listingId")
	applicantID := r.PathValue("userId")

	listing, err := d.LFGQ.GetByID(listingID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "listing not found")
		return
	}
	if listing.UserID != user.ID {
		util.Error(w, http.StatusForbidden, "only listing author can accept applications")
		return
	}

	// Atomically update application status (rejects if not pending)
	if err := d.LFGQ.SetApplicationStatus(listingID, applicantID, "accepted"); err != nil {
		util.Error(w, http.StatusBadRequest, "application is not pending or already processed")
		return
	}

	// Add to participants
	if err := d.LFGQ.Join(listingID, applicantID, listing.MaxPlayers); err != nil {
		util.Error(w, http.StatusConflict, "listing is full")
		return
	}

	// Add to group
	if listing.GroupID != "" {
		applicant, _ := d.Users.GetByID(applicantID)
		if applicant != nil {
			d.Groups.AddMember(&models.GroupMember{GroupID: listing.GroupID, UserID: applicantID, PublicKey: applicant.PublicKey})

			// Send group.create to the accepted user so they see the group
			group, _ := d.Groups.GetByID(listing.GroupID)
			if group != nil {
				createMsg, _ := ws.NewEvent(ws.EventGroupCreate, group)
				d.Hub.BroadcastToUser(applicantID, createMsg)
			}

			// Notify existing members about new join
			joinPayload := map[string]string{
				"group_id":   listing.GroupID,
				"user_id":    applicantID,
				"public_key": applicant.PublicKey,
				"username":   applicant.Username,
			}
			joinMsg, _ := ws.NewEvent(ws.EventGroupMemberJoin, joinPayload)
			if members, err := d.Groups.GetMembers(listing.GroupID); err == nil {
				for _, m := range members {
					d.Hub.BroadcastToUser(m.UserID, joinMsg)
				}
			}
		}
	}

	participants, _ := d.LFGQ.GetParticipants(listingID)
	apps, _ := d.LFGQ.GetApplications(listingID)

	// Broadcast participant update
	partPayload := map[string]interface{}{
		"listing_id":   listingID,
		"channel_id":   listing.ChannelID,
		"user_id":      applicantID,
		"participants": participants,
	}
	partMsg, _ := ws.NewEvent(ws.EventLFGJoin, partPayload)
	d.broadcastToChannelReaders(listing.ChannelID, partMsg)

	// Notify applicant of acceptance
	acceptPayload := map[string]interface{}{
		"listing_id": listingID,
		"status":     "accepted",
		"group_id":   listing.GroupID,
	}
	acceptMsg, _ := ws.NewEvent("lfg.accepted", acceptPayload)
	d.Hub.BroadcastToUser(applicantID, acceptMsg)

	util.JSON(w, http.StatusOK, map[string]interface{}{
		"participants": participants,
		"applications": apps,
	})
}

// RejectLFGApplication — reject an application.
func (d *Deps) RejectLFGApplication(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	listingID := r.PathValue("listingId")
	applicantID := r.PathValue("userId")

	listing, err := d.LFGQ.GetByID(listingID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "listing not found")
		return
	}
	if listing.UserID != user.ID {
		util.Error(w, http.StatusForbidden, "only listing author can reject applications")
		return
	}

	d.LFGQ.SetApplicationStatus(listingID, applicantID, "rejected")

	apps, _ := d.LFGQ.GetApplications(listingID)

	// Notify applicant
	rejectPayload := map[string]interface{}{
		"listing_id": listingID,
		"status":     "rejected",
	}
	rejectMsg, _ := ws.NewEvent("lfg.rejected", rejectPayload)
	d.Hub.BroadcastToUser(applicantID, rejectMsg)

	util.JSON(w, http.StatusOK, map[string]interface{}{
		"applications": apps,
	})
}

func (d *Deps) LeaveLFGListing(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	channelID := r.PathValue("id")
	listingID := r.PathValue("listingId")

	listing, err := d.LFGQ.GetByID(listingID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "listing not found")
		return
	}

	// Author cannot leave their own listing
	if listing.UserID == user.ID {
		util.Error(w, http.StatusBadRequest, "listing author cannot leave — delete the listing instead")
		return
	}

	d.LFGQ.Leave(listingID, user.ID)

	// Remove from group
	if listing.GroupID != "" {
		d.Groups.RemoveMember(listing.GroupID, user.ID)
		leavePayload := map[string]string{
			"group_id": listing.GroupID,
			"user_id":  user.ID,
		}
		leaveMsg, _ := ws.NewEvent(ws.EventGroupMemberLeave, leavePayload)
		if members, err := d.Groups.GetMembers(listing.GroupID); err == nil {
			for _, m := range members {
				d.Hub.BroadcastToUser(m.UserID, leaveMsg)
			}
		}
	}

	participants, _ := d.LFGQ.GetParticipants(listingID)

	payload := map[string]interface{}{
		"listing_id":   listingID,
		"channel_id":   channelID,
		"user_id":      user.ID,
		"participants": participants,
	}
	msg, _ := ws.NewEvent(ws.EventLFGLeave, payload)
	d.broadcastToChannelReaders(channelID, msg)

	util.JSON(w, http.StatusOK, payload)
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
	if listing.ChannelID != channelID {
		util.Error(w, http.StatusBadRequest, "listing does not belong to this channel")
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

	// Also delete the associated group (notify BEFORE delete)
	if listing.GroupID != "" {
		delMsg, _ := ws.NewEvent(ws.EventGroupDelete, map[string]string{"group_id": listing.GroupID})
		if members, err := d.Groups.GetMembers(listing.GroupID); err == nil {
			for _, m := range members {
				d.Hub.BroadcastToUser(m.UserID, delMsg)
			}
		}
		d.Groups.Delete(listing.GroupID)
	}

	msg, _ := ws.NewEvent(ws.EventLFGDelete, map[string]string{
		"id":         listingID,
		"channel_id": channelID,
	})
	d.broadcastToChannelReaders(channelID, msg)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

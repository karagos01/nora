package handlers

import (
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"

	"github.com/google/uuid"
)

type createChannelRequest struct {
	Name            string  `json:"name"`
	Topic           string  `json:"topic"`
	Type            string  `json:"type"`
	CategoryID      *string `json:"category_id"`
	SlowModeSeconds *int    `json:"slow_mode_seconds,omitempty"`
}

type updateChannelRequest struct {
	Name            *string `json:"name,omitempty"`
	Topic           *string `json:"topic,omitempty"`
	Position        *int    `json:"position,omitempty"`
	CategoryID      *string `json:"category_id"`
	SlowModeSeconds *int    `json:"slow_mode_seconds,omitempty"`
}

func (d *Deps) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := d.Channels.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	if channels == nil {
		channels = []models.Channel{}
	}
	util.JSON(w, http.StatusOK, channels)
}

func (d *Deps) GetChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, err := d.Channels.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	util.JSON(w, http.StatusOK, ch)
}

func (d *Deps) CreateChannel(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req createChannelRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if msg := util.ValidateChannelName(req.Name); msg != "" {
		util.Error(w, http.StatusBadRequest, msg)
		return
	}

	chType := req.Type
	if chType == "" {
		chType = "text"
	}
	if chType != "text" && chType != "voice" && chType != "lobby" && chType != "lan" {
		util.Error(w, http.StatusBadRequest, "type must be 'text', 'voice', 'lobby' or 'lan'")
		return
	}

	// Validate category existence
	if req.CategoryID != nil && *req.CategoryID != "" {
		if _, err := d.Categories.GetByID(*req.CategoryID); err != nil {
			util.Error(w, http.StatusBadRequest, "category not found")
			return
		}
	}

	slowMode := 0
	if req.SlowModeSeconds != nil && *req.SlowModeSeconds >= 0 {
		slowMode = *req.SlowModeSeconds
	}

	pos, _ := d.Channels.NextPosition()
	id, _ := uuid.NewV7()
	ch := &models.Channel{
		ID:              id.String(),
		Name:            req.Name,
		Topic:           req.Topic,
		Type:            chType,
		Position:        pos,
		CategoryID:      req.CategoryID,
		SlowModeSeconds: slowMode,
	}

	if err := d.Channels.Create(ch); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// For LAN channel, automatically create lan_party record
	if ch.Type == "lan" && d.LAN != nil {
		party := &models.LANParty{
			ID:        ch.ID,
			Name:      ch.Name,
			CreatorID: user.ID,
			Active:    true,
		}
		if err := d.LAN.CreateParty(party); err != nil {
			slog.Error("failed to create LAN party for channel", "channel_id", ch.ID, "error", err)
		}
	}

	ch, _ = d.Channels.GetByID(ch.ID)
	msg, _ := ws.NewEvent(ws.EventChannelCreate, ch)
	d.Hub.Broadcast(msg)

	d.logAudit(user.ID, "channel.create", "channel", ch.ID, map[string]string{"name": ch.Name, "type": ch.Type})

	util.JSON(w, http.StatusCreated, ch)
}

func (d *Deps) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	ch, err := d.Channels.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "channel not found")
		return
	}

	var req updateChannelRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		if msg := util.ValidateChannelName(*req.Name); msg != "" {
			util.Error(w, http.StatusBadRequest, msg)
			return
		}
		ch.Name = *req.Name
	}
	if req.Topic != nil {
		ch.Topic = *req.Topic
	}
	if req.Position != nil {
		ch.Position = *req.Position
	}
	if req.CategoryID != nil {
		if *req.CategoryID == "" {
			ch.CategoryID = nil
		} else {
			if _, err := d.Categories.GetByID(*req.CategoryID); err != nil {
				util.Error(w, http.StatusBadRequest, "category not found")
				return
			}
			ch.CategoryID = req.CategoryID
		}
	}
	if req.SlowModeSeconds != nil && *req.SlowModeSeconds >= 0 {
		ch.SlowModeSeconds = *req.SlowModeSeconds
	}

	if err := d.Channels.Update(ch); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update channel")
		return
	}

	msg, _ := ws.NewEvent(ws.EventChannelUpdate, ch)
	d.Hub.Broadcast(msg)

	d.logAudit(user.ID, "channel.update", "channel", ch.ID, map[string]string{"name": ch.Name})

	util.JSON(w, http.StatusOK, ch)
}

type reorderChannelsRequest struct {
	ChannelIDs []string `json:"channel_ids"`
}

func (d *Deps) ReorderChannels(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req reorderChannelsRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.ChannelIDs) == 0 {
		util.Error(w, http.StatusBadRequest, "channel_ids required")
		return
	}

	// Validation: all IDs must exist
	for _, id := range req.ChannelIDs {
		if _, err := d.Channels.GetByID(id); err != nil {
			util.Error(w, http.StatusBadRequest, "channel not found: "+id)
			return
		}
	}

	if err := d.Channels.Reorder(req.ChannelIDs); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to reorder channels")
		return
	}

	// Broadcast channel.update for each changed channel
	for _, id := range req.ChannelIDs {
		ch, err := d.Channels.GetByID(id)
		if err != nil {
			continue
		}
		msg, _ := ws.NewEvent(ws.EventChannelUpdate, ch)
		d.Hub.Broadcast(msg)
	}

	channels, _ := d.Channels.List()
	if channels == nil {
		channels = []models.Channel{}
	}
	util.JSON(w, http.StatusOK, channels)
}

func (d *Deps) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageChannels); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")

	// Load channel name before deletion
	ch, _ := d.Channels.GetByID(id)
	chName := ""
	if ch != nil {
		chName = ch.Name
	}

	// For LAN channel, clean up LAN party + WG peers
	if ch != nil && ch.Type == "lan" && d.LAN != nil {
		members, _ := d.LAN.GetMembers(ch.ID)
		d.LAN.DeactivateParty(ch.ID)
		if d.WG != nil && members != nil {
			for _, m := range members {
				inOther, _ := d.LAN.IsUserInOtherParty(ch.ID, m.UserID)
				if !inOther {
					d.WG.RemovePeer(m.PublicKey)
				}
			}
		}
		lanEvt, _ := ws.NewEvent(ws.EventLANDelete, map[string]string{"party_id": ch.ID})
		d.Hub.Broadcast(lanEvt)
	}

	if err := d.Channels.Delete(id); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}

	msg, _ := ws.NewEvent(ws.EventChannelDelete, map[string]string{"id": id})
	d.Hub.Broadcast(msg)

	d.logAudit(user.ID, "channel.delete", "channel", id, map[string]string{"name": chName})

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
)

// VoiceState vrátí aktuální voice state (kdo je v jakém voice kanálu) + screen sharers
func (d *Deps) VoiceState(w http.ResponseWriter, r *http.Request) {
	state := d.Hub.AllVoiceState()
	if state == nil {
		state = make(map[string][]string)
	}
	sharers := d.Hub.ScreenSharers()
	util.JSON(w, http.StatusOK, map[string]any{
		"channels":       state,
		"screen_sharers": sharers,
	})
}

// VoiceMove přesune uživatele do jiného voice kanálu (vyžaduje KICK permission + hierarchii)
func (d *Deps) VoiceMove(w http.ResponseWriter, r *http.Request) {
	actor := auth.GetUser(r)

	if err := d.requirePermission(actor, models.PermKick); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		UserID    string `json:"user_id"`
		ChannelID string `json:"channel_id"`
	}
	if err := util.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.ChannelID == "" {
		util.Error(w, http.StatusBadRequest, "user_id and channel_id required")
		return
	}

	// Kontrola hierarchie
	if err := d.canActOn(actor, req.UserID); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	// Ověřit že cílový kanál je voice
	ch, err := d.Channels.GetByID(req.ChannelID)
	if err != nil || ch.Type != "voice" {
		util.Error(w, http.StatusBadRequest, "target must be a voice channel")
		return
	}

	// Přesunout uživatele — VoiceJoin automaticky leave ze starého kanálu
	// Nejdřív zjistíme starý kanál pro broadcast leave
	oldChannelID, oldRemaining := d.Hub.VoiceLeave(req.UserID)
	if oldChannelID != "" {
		leaveMsg, _ := ws.NewEvent(ws.EventVoiceState, map[string]any{
			"channel_id": oldChannelID,
			"users":      oldRemaining,
			"left":       req.UserID,
		})
		d.Hub.Broadcast(leaveMsg)
	}

	// Join do nového kanálu
	newUsers := d.Hub.VoiceJoin(req.ChannelID, req.UserID)
	joinMsg, _ := ws.NewEvent(ws.EventVoiceState, map[string]any{
		"channel_id": req.ChannelID,
		"users":      newUsers,
		"joined":     req.UserID,
	})
	d.Hub.Broadcast(joinMsg)

	// Notifikace přesunutému uživateli
	moveMsg, _ := ws.NewEvent(ws.EventVoiceMove, map[string]any{
		"channel_id": req.ChannelID,
		"moved_by":   actor.ID,
	})
	d.Hub.BroadcastToUser(req.UserID, moveMsg)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

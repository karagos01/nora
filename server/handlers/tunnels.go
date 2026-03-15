package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"

	"github.com/google/uuid"
)

type createTunnelRequest struct {
	TargetID string `json:"target_id"`
	WGPubKey string `json:"wg_pubkey"`
}

type acceptTunnelRequest struct {
	WGPubKey string `json:"wg_pubkey"`
}

// GetTunnels returns the user's tunnels (pending + active)
func (d *Deps) GetTunnels(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	tunnels, err := d.Tunnels.GetByUser(user.ID)
	if err != nil {
		tunnels = []models.Tunnel{}
	}
	if tunnels == nil {
		tunnels = []models.Tunnel{}
	}
	util.JSON(w, http.StatusOK, tunnels)
}

// CreateTunnel creates a new tunnel request to another user
func (d *Deps) CreateTunnel(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var req createTunnelRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TargetID == "" || req.WGPubKey == "" {
		util.Error(w, http.StatusBadRequest, "target_id and wg_pubkey are required")
		return
	}

	if req.TargetID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot create tunnel to yourself")
		return
	}

	// Verify the target user exists
	target, err := d.Users.GetByID(req.TargetID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "target user not found")
		return
	}

	// Check if an active tunnel already exists
	if d.Tunnels.HasActiveTunnel(user.ID, req.TargetID) {
		util.Error(w, http.StatusConflict, "tunnel already exists between these users")
		return
	}

	// Allocate IP from the LAN pool
	nextIP, err := d.LAN.GetNextIPSimple()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to allocate IP")
		return
	}
	if nextIP > 254 {
		util.Error(w, http.StatusConflict, "no IPs available")
		return
	}
	creatorIP := d.LAN.FormatIP(d.WG.Subnet(), nextIP)

	id, _ := uuid.NewV7()
	tunnel := &models.Tunnel{
		ID:              id.String(),
		CreatorID:       user.ID,
		TargetID:        req.TargetID,
		Status:          "pending",
		CreatorWGPubKey: req.WGPubKey,
		CreatorIP:       creatorIP,
		CreatorName:     user.Username,
		TargetName:      target.Username,
	}

	if err := d.Tunnels.Create(tunnel); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create tunnel")
		return
	}

	// Notify the target user
	event, _ := ws.NewEvent(ws.EventTunnelRequest, tunnel)
	d.Hub.BroadcastToUser(req.TargetID, event)

	// WG config for the creator
	resp := map[string]any{
		"tunnel": tunnel,
		"wg_config": map[string]string{
			"server_public_key": d.WG.PublicKey(),
			"server_endpoint":   d.WG.Endpoint(),
			"assigned_ip":       creatorIP + "/24",
			"allowed_ips":       d.WG.Subnet(),
		},
	}
	util.JSON(w, http.StatusCreated, resp)
}

// AcceptTunnel accepts a tunnel request
func (d *Deps) AcceptTunnel(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	tunnelID := r.PathValue("id")

	tunnel, err := d.Tunnels.GetByID(tunnelID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if tunnel.TargetID != user.ID {
		util.Error(w, http.StatusForbidden, "only the target user can accept")
		return
	}

	if tunnel.Status != "pending" {
		util.Error(w, http.StatusBadRequest, "tunnel is not pending")
		return
	}

	var req acceptTunnelRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.WGPubKey == "" {
		util.Error(w, http.StatusBadRequest, "wg_pubkey is required")
		return
	}

	// Allocate IP for the target
	nextIP, err := d.LAN.GetNextIPSimple()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to allocate IP")
		return
	}
	if nextIP > 254 {
		util.Error(w, http.StatusConflict, "no IPs available")
		return
	}
	targetIP := d.LAN.FormatIP(d.WG.Subnet(), nextIP)

	if err := d.Tunnels.Accept(tunnelID, req.WGPubKey, targetIP); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to accept tunnel")
		return
	}

	// Add WG peers for both users
	if err := d.WG.AddPeer(tunnel.CreatorWGPubKey, tunnel.CreatorIP); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to add creator WG peer")
		return
	}
	if err := d.WG.AddPeer(req.WGPubKey, targetIP); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to add target WG peer")
		return
	}

	// Reload tunnel with current data
	tunnel, _ = d.Tunnels.GetByID(tunnelID)

	// Notify the creator
	event, _ := ws.NewEvent(ws.EventTunnelAccept, tunnel)
	d.Hub.BroadcastToUser(tunnel.CreatorID, event)

	// WG config for the target
	resp := map[string]any{
		"tunnel": tunnel,
		"wg_config": map[string]string{
			"server_public_key": d.WG.PublicKey(),
			"server_endpoint":   d.WG.Endpoint(),
			"assigned_ip":       targetIP + "/24",
			"allowed_ips":       d.WG.Subnet(),
		},
	}
	util.JSON(w, http.StatusOK, resp)
}

// CloseTunnel closes a tunnel (either side can close)
func (d *Deps) CloseTunnel(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	tunnelID := r.PathValue("id")

	tunnel, err := d.Tunnels.GetByID(tunnelID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if tunnel.CreatorID != user.ID && tunnel.TargetID != user.ID {
		util.Error(w, http.StatusForbidden, "not a participant")
		return
	}

	if tunnel.Status == "closed" {
		util.Error(w, http.StatusBadRequest, "tunnel already closed")
		return
	}

	// Remove WG peers if it was active
	if tunnel.Status == "active" {
		if tunnel.CreatorWGPubKey != "" {
			d.WG.RemovePeer(tunnel.CreatorWGPubKey)
		}
		if tunnel.TargetWGPubKey != "" {
			d.WG.RemovePeer(tunnel.TargetWGPubKey)
		}
	}

	d.Tunnels.Close(tunnelID)

	// Notify the other side
	otherUserID := tunnel.TargetID
	if user.ID == tunnel.TargetID {
		otherUserID = tunnel.CreatorID
	}
	event, _ := ws.NewEvent(ws.EventTunnelClose, map[string]string{
		"tunnel_id": tunnelID,
		"closed_by": user.ID,
	})
	d.Hub.BroadcastToUser(otherUserID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

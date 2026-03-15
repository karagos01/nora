package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"

	"github.com/google/uuid"
)

type createLANPartyRequest struct {
	Name string `json:"name"`
}

type joinLANPartyRequest struct {
	PublicKey string `json:"public_key"`
}

// GetLANParties returns all active LAN parties + members
func (d *Deps) GetLANParties(w http.ResponseWriter, r *http.Request) {
	parties, err := d.LAN.GetActiveParties()
	if err != nil {
		parties = []models.LANParty{}
	}
	if parties == nil {
		parties = []models.LANParty{}
	}

	membersMap := map[string][]models.LANPartyMember{}
	for _, p := range parties {
		members, _ := d.LAN.GetMembers(p.ID)
		if members == nil {
			members = []models.LANPartyMember{}
		}
		membersMap[p.ID] = members
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"parties": parties,
		"members": membersMap,
	})
}

// CreateLANParty creates a new LAN party
func (d *Deps) CreateLANParty(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	if err := d.requirePermission(user, models.PermAdmin); err != nil {
		util.Error(w, http.StatusForbidden, "only owner or admin can create LAN party")
		return
	}

	if d.WG == nil {
		util.Error(w, http.StatusServiceUnavailable, "LAN feature is not enabled on this server")
		return
	}

	var req createLANPartyRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 64 {
		util.Error(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}

	id, _ := uuid.NewV7()
	party := &models.LANParty{
		ID:        id.String(),
		Name:      req.Name,
		CreatorID: user.ID,
		Active:    true,
	}

	if err := d.LAN.CreateParty(party); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create party")
		return
	}

	party, _ = d.LAN.GetParty(party.ID)

	event, _ := ws.NewEvent(ws.EventLANCreate, party)
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusCreated, party)
}

// DeleteLANParty ends the party and removes WG peers that are not in another party
func (d *Deps) DeleteLANParty(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	partyID := r.PathValue("id")

	party, err := d.LAN.GetParty(partyID)
	if err != nil || !party.Active {
		util.Error(w, http.StatusNotFound, "party not found")
		return
	}

	if party.CreatorID != user.ID && !user.IsOwner {
		util.Error(w, http.StatusForbidden, "only the creator or owner can delete the party")
		return
	}

	// Get members before deactivation
	members, _ := d.LAN.GetMembers(party.ID)

	d.LAN.DeactivateParty(party.ID)

	// Remove WG peers only for users not in another active party
	if d.WG != nil && members != nil {
		for _, m := range members {
			inOther, _ := d.LAN.IsUserInOtherParty(party.ID, m.UserID)
			if !inOther {
				d.WG.RemovePeer(m.PublicKey)
			}
		}
	}

	event, _ := ws.NewEvent(ws.EventLANDelete, map[string]string{"party_id": party.ID})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// JoinLANParty adds a user to the LAN party
func (d *Deps) JoinLANParty(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	partyID := r.PathValue("id")

	if d.WG == nil {
		util.Error(w, http.StatusServiceUnavailable, "LAN feature is not enabled on this server")
		return
	}

	party, err := d.LAN.GetParty(partyID)
	if err != nil || !party.Active {
		util.Error(w, http.StatusNotFound, "party not found")
		return
	}

	// Already a member of this party?
	existing, err := d.LAN.GetMemberByUser(party.ID, user.ID)
	if err == nil && existing != nil {
		util.Error(w, http.StatusConflict, "already a member")
		return
	}

	var req joinLANPartyRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PublicKey == "" {
		util.Error(w, http.StatusBadRequest, "public_key is required")
		return
	}

	// Try to find an existing WG peer for the user (from another party)
	existingPeer, _ := d.LAN.GetUserPeer(user.ID)

	var assignedIP string
	var isNewPeer bool

	if existingPeer != nil {
		// User already has an IP from another party — reuse
		assignedIP = existingPeer.AssignedIP
		isNewPeer = false
	} else {
		// New peer — allocate IP
		nextIP, err := d.LAN.GetNextIPSimple()
		if err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to get next IP")
			return
		}
		if nextIP > 254 {
			util.Error(w, http.StatusConflict, "LAN is full (max 253 members)")
			return
		}
		assignedIP = d.LAN.FormatIP(d.WG.Subnet(), nextIP)
		isNewPeer = true
	}

	id, _ := uuid.NewV7()
	me, _ := d.Users.GetByID(user.ID)
	member := &models.LANPartyMember{
		ID:         id.String(),
		PartyID:    party.ID,
		UserID:     user.ID,
		PublicKey:  req.PublicKey,
		AssignedIP: assignedIP,
		Username:   me.Username,
	}

	if err := d.LAN.AddMember(member); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to join party")
		return
	}

	// Add WG peer only if it's new
	if isNewPeer {
		if err := d.WG.AddPeer(req.PublicKey, assignedIP); err != nil {
			d.LAN.RemoveMember(party.ID, user.ID)
			util.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to add WireGuard peer: %v", err))
			return
		}
	}

	event, _ := ws.NewEvent(ws.EventLANJoin, member)
	d.Hub.Broadcast(event)

	// Return wg_config only for a new peer — existing peer already has a tunnel
	resp := map[string]any{"member": member}
	if isNewPeer {
		resp["wg_config"] = map[string]string{
			"server_public_key": d.WG.PublicKey(),
			"server_endpoint":   d.WG.Endpoint(),
			"assigned_ip":       assignedIP + "/24",
			"allowed_ips":       d.WG.Subnet(),
		}
	}
	util.JSON(w, http.StatusOK, resp)
}

// KickUserFromAllParties removes a user from all active LAN parties + WG peer
func (d *Deps) KickUserFromAllParties(userID string) {
	parties, err := d.LAN.GetActiveParties()
	if err != nil {
		return
	}
	for _, party := range parties {
		member, err := d.LAN.GetMemberByUser(party.ID, userID)
		if err != nil || member == nil {
			continue
		}

		d.LAN.RemoveMember(party.ID, userID)

		// Remove WG peer only if user is not in another active party
		if d.WG != nil {
			inOther, _ := d.LAN.IsUserInOtherParty(party.ID, userID)
			if !inOther {
				d.WG.RemovePeer(member.PublicKey)
			}
		}

		event, _ := ws.NewEvent(ws.EventLANLeave, map[string]string{
			"party_id": party.ID,
			"user_id":  userID,
		})
		d.Hub.Broadcast(event)
	}
}

// LeaveLANParty removes a user from the LAN party
func (d *Deps) LeaveLANParty(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	partyID := r.PathValue("id")

	party, err := d.LAN.GetParty(partyID)
	if err != nil || !party.Active {
		util.Error(w, http.StatusNotFound, "party not found")
		return
	}

	member, err := d.LAN.GetMemberByUser(party.ID, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			util.Error(w, http.StatusNotFound, "not a member")
			return
		}
		util.Error(w, http.StatusInternalServerError, "failed to check membership")
		return
	}

	d.LAN.RemoveMember(party.ID, user.ID)

	// Remove WG peer only if user is not in another active party
	if d.WG != nil {
		inOther, _ := d.LAN.IsUserInOtherParty(party.ID, user.ID)
		if !inOther {
			d.WG.RemovePeer(member.PublicKey)
		}
	}

	event, _ := ws.NewEvent(ws.EventLANLeave, map[string]string{
		"party_id": party.ID,
		"user_id":  user.ID,
	})
	d.Hub.Broadcast(event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

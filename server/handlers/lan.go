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

// GetLANParties vrátí všechny aktivní LAN party + členy
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

// CreateLANParty vytvoří novou LAN party
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

// DeleteLANParty ukončí party a odebere WG peery kteří nejsou v jiné party
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

	// Získej členy před deaktivací
	members, _ := d.LAN.GetMembers(party.ID)

	d.LAN.DeactivateParty(party.ID)

	// Odeber WG peery jen pro users co nejsou v jiné aktivní party
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

// JoinLANParty přidá uživatele do LAN party
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

	// Už je členem této party?
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

	// Zkus najít existující WG peer uživatele (z jiné party)
	existingPeer, _ := d.LAN.GetUserPeer(user.ID)

	var assignedIP string
	var isNewPeer bool

	if existingPeer != nil {
		// User už má IP z jiné party — reuse
		assignedIP = existingPeer.AssignedIP
		isNewPeer = false
	} else {
		// Nový peer — přiděl IP
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

	// Přidej WG peer jen pokud je nový
	if isNewPeer {
		if err := d.WG.AddPeer(req.PublicKey, assignedIP); err != nil {
			d.LAN.RemoveMember(party.ID, user.ID)
			util.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to add WireGuard peer: %v", err))
			return
		}
	}

	event, _ := ws.NewEvent(ws.EventLANJoin, member)
	d.Hub.Broadcast(event)

	// wg_config vrátit jen pro nového peera — existující peer už má tunel
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

// KickUserFromAllParties odebere uživatele ze všech aktivních LAN parties + WG peer
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

		// Odeber WG peer jen pokud user není v jiné aktivní party
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

// LeaveLANParty odebere uživatele z LAN party
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

	// Odeber WG peer jen pokud user není v jiné aktivní party
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

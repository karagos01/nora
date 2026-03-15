package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"time"

	"github.com/google/uuid"
)

type createInviteRequest struct {
	MaxUses   int    `json:"max_uses"`
	ExpiresIn int    `json:"expires_in"` // sekundy, 0 = nikdy
}

func (d *Deps) ListInvites(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageInvites); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	invites, err := d.Invites.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list invites")
		return
	}
	if invites == nil {
		invites = []models.Invite{}
	}

	host := r.Host
	result := make([]map[string]any, len(invites))
	for i, inv := range invites {
		result[i] = map[string]any{
			"id":               inv.ID,
			"code":             inv.Code,
			"link":             host + "/" + inv.Code,
			"creator_id":       inv.CreatorID,
			"creator_username": inv.CreatorUsername,
			"max_uses":         inv.MaxUses,
			"uses":             inv.Uses,
			"expires_at":       inv.ExpiresAt,
			"created_at":       inv.CreatedAt,
		}
	}
	util.JSON(w, http.StatusOK, result)
}

func (d *Deps) CreateInvite(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageInvites); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.checkQuarantine(user.ID, "invite"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	// Max invites per user limit
	if d.SecurityCfg.Invites.MaxInvitesPerUser > 0 && d.InviteChain != nil {
		count, _ := d.Invites.CountByCreator(user.ID)
		if count >= d.SecurityCfg.Invites.MaxInvitesPerUser {
			util.Error(w, http.StatusForbidden, "invite limit reached")
			return
		}
	}

	var req createInviteRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	code := generateInviteCode()
	id, _ := uuid.NewV7()
	inv := &models.Invite{
		ID:        id.String(),
		Code:      code,
		CreatorID: user.ID,
		MaxUses:   req.MaxUses,
	}

	if req.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
		inv.ExpiresAt = &exp
	}

	if err := d.Invites.Create(inv); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create invite")
		return
	}

	inv, _ = d.Invites.GetByCode(code)

	d.logAudit(user.ID, "invite.create", "invite", inv.ID, map[string]any{"code": code, "max_uses": req.MaxUses})

	// Sestavit invite link: host:port/kod
	host := r.Host // obsahuje host:port
	link := host + "/" + code

	util.JSON(w, http.StatusCreated, map[string]any{
		"id":         inv.ID,
		"code":       inv.Code,
		"link":       link,
		"creator_id": inv.CreatorID,
		"max_uses":   inv.MaxUses,
		"uses":       inv.Uses,
		"expires_at": inv.ExpiresAt,
		"created_at": inv.CreatedAt,
	})
}

func (d *Deps) DeleteInvite(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageInvites); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	if err := d.Invites.Delete(id); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete invite")
		return
	}

	d.logAudit(user.ID, "invite.delete", "invite", id, nil)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func generateInviteCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

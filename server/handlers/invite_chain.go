package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
)

func (d *Deps) GetInviteChainTree(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	entries, err := d.InviteChain.GetAll()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get invite chain")
		return
	}

	// Sestavit stromovou strukturu
	nodeMap := make(map[string]*models.InviteChainNode)
	for _, e := range entries {
		nodeMap[e.UserID] = &models.InviteChainNode{
			UserID:   e.UserID,
			Username: e.Username,
			IsBanned: e.IsBanned,
			JoinedAt: e.JoinedAt,
		}
	}

	var roots []models.InviteChainNode
	for _, e := range entries {
		node := nodeMap[e.UserID]
		if e.InvitedByID == "" {
			roots = append(roots, *node)
		} else if parent, ok := nodeMap[e.InvitedByID]; ok {
			parent.Children = append(parent.Children, *node)
		} else {
			roots = append(roots, *node)
		}
	}

	// Aktualizovat children v roots (kvůli kopírování)
	var result []models.InviteChainNode
	for _, e := range entries {
		if e.InvitedByID == "" {
			result = append(result, *nodeMap[e.UserID])
		} else if _, ok := nodeMap[e.InvitedByID]; !ok {
			result = append(result, *nodeMap[e.UserID])
		}
	}

	if result == nil {
		result = []models.InviteChainNode{}
	}
	util.JSON(w, http.StatusOK, result)
}

func (d *Deps) GetInviteChainUser(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	entry, err := d.InviteChain.GetByUser(userID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "not found in invite chain")
		return
	}

	util.JSON(w, http.StatusOK, entry)
}

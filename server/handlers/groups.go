package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

type createGroupRequest struct {
	Name string `json:"name"`
}

type joinGroupRequest struct {
	InviteCode string `json:"invite_code"`
}

type createGroupInviteRequest struct {
	MaxUses   int `json:"max_uses"`
	ExpiresIn int `json:"expires_in"`
}

type relayGroupMessageRequest struct {
	EncryptedContent string                       `json:"encrypted_content"`
	Attachments      []relayGroupMessageAttachment `json:"attachments,omitempty"`
}

type relayGroupMessageAttachment struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

func (d *Deps) ListGroups(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	groups, err := d.Groups.ListForUser(user.ID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	if groups == nil {
		groups = []models.Group{}
	}

	type groupWithMembers struct {
		models.Group
		Members []models.GroupMember `json:"members"`
	}

	var result []groupWithMembers
	for _, g := range groups {
		members, _ := d.Groups.GetMembers(g.ID)
		if members == nil {
			members = []models.GroupMember{}
		}
		result = append(result, groupWithMembers{
			Group:   g,
			Members: members,
		})
	}

	util.JSON(w, http.StatusOK, result)
}

func (d *Deps) CreateGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	// Quarantine enforcement
	if err := d.checkQuarantine(user.ID, "create_group"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	var req createGroupRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || len(req.Name) > 64 {
		util.Error(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}

	id, _ := uuid.NewV7()
	g := &models.Group{
		ID:        id.String(),
		Name:      req.Name,
		CreatorID: user.ID,
	}

	if err := d.Groups.Create(g); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create group")
		return
	}

	me, _ := d.Users.GetByID(user.ID)
	member := &models.GroupMember{
		GroupID:   g.ID,
		UserID:    user.ID,
		PublicKey: me.PublicKey,
	}
	d.Groups.AddMember(member)

	g, _ = d.Groups.GetByID(g.ID)

	util.JSON(w, http.StatusCreated, g)
}

func (d *Deps) GetGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")

	ok, _ := d.Groups.IsMember(groupID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a member")
		return
	}

	g, err := d.Groups.GetByID(groupID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "group not found")
		return
	}

	members, _ := d.Groups.GetMembers(groupID)
	if members == nil {
		members = []models.GroupMember{}
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"id":         g.ID,
		"name":       g.Name,
		"creator_id": g.CreatorID,
		"created_at": g.CreatedAt,
		"members":    members,
	})
}

func (d *Deps) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")

	g, err := d.Groups.GetByID(groupID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "group not found")
		return
	}

	if g.CreatorID != user.ID {
		util.Error(w, http.StatusForbidden, "only the creator can delete the group")
		return
	}

	// Broadcast delete event to all members
	members, _ := d.Groups.GetMembers(groupID)
	event, _ := ws.NewEvent(ws.EventGroupDelete, map[string]string{"group_id": groupID})
	for _, m := range members {
		d.Hub.BroadcastToUser(m.UserID, event)
	}

	d.Groups.Delete(groupID)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) JoinGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")

	var req joinGroupRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify invite code
	inv, err := d.Groups.GetInviteByCode(req.InviteCode)
	if err != nil {
		util.Error(w, http.StatusNotFound, "invalid invite code")
		return
	}
	if inv.GroupID != groupID {
		util.Error(w, http.StatusBadRequest, "invite does not match this group")
		return
	}
	if inv.MaxUses > 0 && inv.Uses >= inv.MaxUses {
		util.Error(w, http.StatusGone, "invite has been used up")
		return
	}
	if inv.ExpiresAt != nil && inv.ExpiresAt.Before(time.Now()) {
		util.Error(w, http.StatusGone, "invite has expired")
		return
	}

	// Already a member?
	already, _ := d.Groups.IsMember(groupID, user.ID)
	if already {
		util.Error(w, http.StatusConflict, "already a member")
		return
	}

	me, _ := d.Users.GetByID(user.ID)
	member := &models.GroupMember{
		GroupID:   groupID,
		UserID:    user.ID,
		PublicKey: me.PublicKey,
	}

	if err := d.Groups.AddMember(member); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to join group")
		return
	}

	d.Groups.IncrementInviteUses(inv.ID)

	// Broadcast join event to all members
	members, _ := d.Groups.GetMembers(groupID)
	event, _ := ws.NewEvent(ws.EventGroupMemberJoin, map[string]any{
		"group_id":   groupID,
		"user_id":    user.ID,
		"public_key": me.PublicKey,
		"username":   me.Username,
	})
	for _, m := range members {
		d.Hub.BroadcastToUser(m.UserID, event)
	}

	util.JSON(w, http.StatusOK, member)
}

func (d *Deps) LeaveGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")
	targetID := r.PathValue("userId")

	// Can only leave yourself, or creator can kick
	g, err := d.Groups.GetByID(groupID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "group not found")
		return
	}

	if targetID != user.ID && g.CreatorID != user.ID {
		util.Error(w, http.StatusForbidden, "cannot remove other members")
		return
	}

	d.Groups.RemoveMember(groupID, targetID)

	// Broadcast leave event
	members, _ := d.Groups.GetMembers(groupID)
	event, _ := ws.NewEvent(ws.EventGroupMemberLeave, map[string]string{
		"group_id": groupID,
		"user_id":  targetID,
	})
	for _, m := range members {
		d.Hub.BroadcastToUser(m.UserID, event)
	}
	// Also notify the user who left
	d.Hub.BroadcastToUser(targetID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) RelayGroupMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")

	ok, _ := d.Groups.IsMember(groupID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a member")
		return
	}

	// Quarantine enforcement
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	var req relayGroupMessageRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.EncryptedContent) == 0 || len(req.EncryptedContent) > 32000 {
		util.Error(w, http.StatusBadRequest, "invalid encrypted_content length")
		return
	}

	author, _ := d.Users.GetByID(user.ID)

	id, _ := uuid.NewV7()
	msg := map[string]any{
		"id":                id.String(),
		"group_id":          groupID,
		"sender_id":         user.ID,
		"encrypted_content": req.EncryptedContent,
		"created_at":        time.Now(),
		"author":            author,
	}
	if len(req.Attachments) > 0 {
		msg["attachments"] = req.Attachments
	}

	// Broadcast to all members — server does not store anything
	members, _ := d.Groups.GetMembers(groupID)
	event, _ := ws.NewEvent(ws.EventGroupMessage, msg)
	for _, m := range members {
		d.Hub.BroadcastToUser(m.UserID, event)
	}

	util.JSON(w, http.StatusCreated, msg)
}

func (d *Deps) CreateGroupInvite(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")

	ok, _ := d.Groups.IsMember(groupID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a member")
		return
	}

	// Quarantine enforcement
	if err := d.checkQuarantine(user.ID, "create_invite"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	var req createGroupInviteRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	b := make([]byte, 16)
	rand.Read(b)
	code := hex.EncodeToString(b)

	id, _ := uuid.NewV7()
	inv := &models.GroupInvite{
		ID:        id.String(),
		GroupID:   groupID,
		Code:      code,
		CreatorID: user.ID,
		MaxUses:   req.MaxUses,
	}

	if req.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
		inv.ExpiresAt = &exp
	}

	if err := d.Groups.CreateInvite(inv); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create invite")
		return
	}

	inv, _ = d.Groups.GetInviteByCode(code)

	host := r.Host
	link := host + "/g/" + code

	util.JSON(w, http.StatusCreated, map[string]any{
		"id":         inv.ID,
		"group_id":   inv.GroupID,
		"code":       inv.Code,
		"link":       link,
		"creator_id": inv.CreatorID,
		"max_uses":   inv.MaxUses,
		"uses":       inv.Uses,
		"expires_at": inv.ExpiresAt,
		"created_at": inv.CreatedAt,
	})
}

func (d *Deps) ListGroupInvites(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	groupID := r.PathValue("id")

	ok, _ := d.Groups.IsMember(groupID, user.ID)
	if !ok {
		util.Error(w, http.StatusForbidden, "not a member")
		return
	}

	invites, err := d.Groups.ListInvites(groupID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list invites")
		return
	}
	if invites == nil {
		invites = []models.GroupInvite{}
	}

	host := r.Host
	result := make([]map[string]any, len(invites))
	for i, inv := range invites {
		result[i] = map[string]any{
			"id":         inv.ID,
			"group_id":   inv.GroupID,
			"code":       inv.Code,
			"link":       host + "/g/" + inv.Code,
			"creator_id": inv.CreatorID,
			"max_uses":   inv.MaxUses,
			"uses":       inv.Uses,
			"expires_at": inv.ExpiresAt,
			"created_at": inv.CreatedAt,
		}
	}

	util.JSON(w, http.StatusOK, result)
}

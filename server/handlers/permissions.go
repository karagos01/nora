package handlers

import (
	"errors"
	"nora/auth"
	"nora/models"
)

func (d *Deps) requirePermission(user *auth.ContextUser, perm int64) error {
	if user.IsOwner {
		return nil
	}

	perms, err := d.Roles.GetUserPermissions(user.ID)
	if err != nil {
		return err
	}

	if perms&models.PermAdmin != 0 {
		return nil
	}

	if perms&perm == 0 {
		return errors.New("insufficient permissions")
	}
	return nil
}

// canActOn checks role hierarchy — actor must have higher rank (lower position) than target.
func (d *Deps) canActOn(actor *auth.ContextUser, targetUserID string) error {
	if actor.IsOwner {
		return nil
	}

	target, err := d.Users.GetByID(targetUserID)
	if err != nil {
		return errors.New("target user not found")
	}
	if target.IsOwner {
		return errors.New("cannot act on server owner")
	}

	actorPos, err := d.Roles.GetHighestPosition(actor.ID, actor.IsOwner)
	if err != nil {
		return err
	}

	targetPos, err := d.Roles.GetHighestPosition(targetUserID, false)
	if err != nil {
		return err
	}

	if actorPos >= targetPos {
		return errors.New("insufficient role hierarchy")
	}
	return nil
}

// requireChannelPermission checks per-channel permissions (base role perms + channel overrides).
func (d *Deps) requireChannelPermission(user *auth.ContextUser, channelID string, perm int64) error {
	if user.IsOwner {
		return nil
	}

	perms := d.GetChannelPermissions(user.ID, channelID, user.IsOwner)

	if perms&models.PermAdmin != 0 {
		return nil
	}

	if perms&perm == 0 {
		return errors.New("insufficient permissions")
	}
	return nil
}

// GetChannelPermissions returns effective permissions for a user in a given channel.
// Procedure: base = global role perms, then apply channel overrides: result = (base | allow) & ^deny
func (d *Deps) GetChannelPermissions(userID, channelID string, isOwner bool) int64 {
	if isOwner {
		// Owner has everything
		return int64(^uint64(0) >> 1)
	}

	base, _ := d.Roles.GetUserPermissions(userID)

	// Admin bypass — channel overrides do not apply to admins
	if base&models.PermAdmin != 0 {
		return base
	}

	// Get role IDs of the user (including everyone)
	userRoles, err := d.Roles.GetUserRoles(userID)
	if err != nil {
		return base
	}

	roleIDs := make([]string, 0, len(userRoles)+1)
	hasEveryone := false
	for _, r := range userRoles {
		roleIDs = append(roleIDs, r.ID)
		if r.ID == "everyone" {
			hasEveryone = true
		}
	}
	if !hasEveryone {
		roleIDs = append(roleIDs, "everyone")
	}

	allow, deny, err := d.ChannelPermQ.GetForChannelAndUser(channelID, userID, roleIDs)
	if err != nil {
		return base
	}

	if allow == 0 && deny == 0 {
		return base
	}

	return (base | allow) & ^deny
}

// broadcastToChannelReaders sends a WS event only to users who have READ permission on the channel.
// For channels without permission overrides (common case), this is equivalent to Broadcast.
func (d *Deps) broadcastToChannelReaders(channelID string, msg []byte) {
	onlineIDs := d.Hub.OnlineUserIDs()
	if len(onlineIDs) == 0 {
		return
	}

	excluded := make(map[string]bool)
	for _, uid := range onlineIDs {
		perms := d.GetChannelPermissions(uid, channelID, false)
		if perms&models.PermAdmin == 0 && perms&models.PermRead == 0 {
			excluded[uid] = true
		}
	}

	d.Hub.BroadcastExcluding(msg, excluded)
}

// canActOnRole checks whether the actor can manipulate the given role (must have higher rank).
func (d *Deps) canActOnRole(actor *auth.ContextUser, rolePosition int) error {
	if actor.IsOwner {
		return nil
	}

	actorPos, err := d.Roles.GetHighestPosition(actor.ID, actor.IsOwner)
	if err != nil {
		return err
	}

	if actorPos >= rolePosition {
		return errors.New("insufficient role hierarchy")
	}
	return nil
}

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

// canActOn kontroluje hierarchii rolí — actor musí mít vyšší rank (nižší position) než target.
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

// requireChannelPermission kontroluje per-channel permissions (base role perms + channel overrides).
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

// GetChannelPermissions vrátí efektivní permissions pro uživatele v daném kanálu.
// Postup: base = globální role perms, pak aplikuj channel overrides: result = (base | allow) & ^deny
func (d *Deps) GetChannelPermissions(userID, channelID string, isOwner bool) int64 {
	if isOwner {
		// Owner má vše
		return int64(^uint64(0) >> 1)
	}

	base, _ := d.Roles.GetUserPermissions(userID)

	// Admin bypass — channel overrides se neaplikují na adminy
	if base&models.PermAdmin != 0 {
		return base
	}

	// Získat role IDs uživatele (včetně everyone)
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

// canActOnRole kontroluje zda actor může manipulovat s danou rolí (musí mít vyšší rank).
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

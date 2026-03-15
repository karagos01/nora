package queries

import (
	"database/sql"
	"nora/models"
	"strings"
)

// ChannelPermQueries manages per-channel permission overrides.
type ChannelPermQueries struct {
	DB *sql.DB
}

// Set creates or updates an override for a given channel and target (role/user).
func (q *ChannelPermQueries) Set(o models.ChannelPermOverride) error {
	_, err := q.DB.Exec(
		`INSERT INTO channel_permission_overrides (channel_id, target_type, target_id, allow, deny)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(channel_id, target_type, target_id)
		 DO UPDATE SET allow = excluded.allow, deny = excluded.deny`,
		o.ChannelID, o.TargetType, o.TargetID, o.Allow, o.Deny,
	)
	return err
}

// Delete removes an override for a given channel and target.
func (q *ChannelPermQueries) Delete(channelID, targetType, targetID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM channel_permission_overrides WHERE channel_id = ? AND target_type = ? AND target_id = ?`,
		channelID, targetType, targetID,
	)
	return err
}

// GetForChannel returns all overrides for a given channel.
func (q *ChannelPermQueries) GetForChannel(channelID string) ([]models.ChannelPermOverride, error) {
	rows, err := q.DB.Query(
		`SELECT channel_id, target_type, target_id, allow, deny
		 FROM channel_permission_overrides WHERE channel_id = ?`,
		channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overrides []models.ChannelPermOverride
	for rows.Next() {
		var o models.ChannelPermOverride
		if err := rows.Scan(&o.ChannelID, &o.TargetType, &o.TargetID, &o.Allow, &o.Deny); err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// GetForChannelAndUser computes effective allow/deny for a user in a channel.
// First, role overrides are applied (OR across allow, OR across deny),
// then user override overwrites (if it exists).
func (q *ChannelPermQueries) GetForChannelAndUser(channelID, userID string, roleIDs []string) (allow, deny int64, err error) {
	// Special case: user has no roles and no user override
	if len(roleIDs) == 0 {
		// Only user override
		err = q.DB.QueryRow(
			`SELECT allow, deny FROM channel_permission_overrides
			 WHERE channel_id = ? AND target_type = 'user' AND target_id = ?`,
			channelID, userID,
		).Scan(&allow, &deny)
		if err == sql.ErrNoRows {
			return 0, 0, nil
		}
		return allow, deny, err
	}

	// Everyone role is always present — include it in roleIDs if not already there
	// (caller from permissions.go provides the complete list)

	// Role overrides: OR across all roles
	placeholders := make([]string, len(roleIDs))
	args := make([]any, 0, len(roleIDs)+1)
	args = append(args, channelID)
	for i, rid := range roleIDs {
		placeholders[i] = "?"
		args = append(args, rid)
	}

	rows, err := q.DB.Query(
		`SELECT allow, deny FROM channel_permission_overrides
		 WHERE channel_id = ? AND target_type = 'role' AND target_id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var roleAllow, roleDeny int64
	for rows.Next() {
		var a, d int64
		if err := rows.Scan(&a, &d); err != nil {
			return 0, 0, err
		}
		roleAllow |= a
		roleDeny |= d
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	// User override (overwrites role overrides)
	var userAllow, userDeny int64
	err = q.DB.QueryRow(
		`SELECT allow, deny FROM channel_permission_overrides
		 WHERE channel_id = ? AND target_type = 'user' AND target_id = ?`,
		channelID, userID,
	).Scan(&userAllow, &userDeny)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}

	// Final: role overrides + user override
	allow = roleAllow | userAllow
	deny = roleDeny | userDeny

	return allow, deny, nil
}

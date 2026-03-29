package handlers

import (
	"log"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
)

type UserProfileResponse struct {
	User             *models.User   `json:"user"`
	MessageCount     int            `json:"message_count"`
	UploadCount      int            `json:"upload_count"`
	UploadSizeMB     float64        `json:"upload_size_mb"`
	JoinedVia        string         `json:"joined_via,omitempty"`
	InvitedBy        string         `json:"invited_by,omitempty"`
	InvitedByName    string         `json:"invited_by_name,omitempty"`
	ChannelStats     []ChannelStat  `json:"channel_stats,omitempty"`
	ReportsReceived  int            `json:"reports_received"`
	ReportsFiled     int            `json:"reports_filed"`
	RecentReports    []models.Report `json:"recent_reports,omitempty"`
}

type ChannelStat struct {
	ChannelID    string `json:"channel_id"`
	ChannelName  string `json:"channel_name"`
	MessageCount int    `json:"message_count"`
}

// GetUserProfile returns admin-visible profile info for a user.
func (d *Deps) GetUserProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	targetID := r.PathValue("id")

	// Require VIEW_ACTIVITY permission
	if err := d.requirePermission(user, models.PermViewActivity); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	target, err := d.Users.GetByID(targetID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "user not found")
		return
	}

	resp := UserProfileResponse{
		User: target,
	}

	// Stats from profile queries
	stats, err := d.ProfileQ.GetUserStats(targetID)
	if err != nil {
		log.Printf("GetUserStats(%s): %v", targetID, err)
	}
	if stats != nil {
		resp.MessageCount = stats.MessageCount
		resp.UploadCount = stats.UploadCount
		resp.UploadSizeMB = stats.UploadSizeMB
		resp.ChannelStats = make([]ChannelStat, len(stats.ChannelStats))
		for i, cs := range stats.ChannelStats {
			resp.ChannelStats[i] = ChannelStat{
				ChannelID:    cs.ChannelID,
				ChannelName:  cs.ChannelName,
				MessageCount: cs.MessageCount,
			}
		}
	}

	// Invite chain info
	if d.InviteChain != nil {
		entry, err := d.InviteChain.GetByUser(targetID)
		if err == nil && entry != nil {
			resp.JoinedVia = entry.InviteCode
			resp.InvitedBy = entry.InvitedByID
			resp.InvitedByName = entry.InviterUsername
			if resp.InvitedByName == "" && entry.InvitedByID != "" {
				inviter, err := d.Users.GetByID(entry.InvitedByID)
				if err == nil && inviter != nil {
					resp.InvitedByName = inviter.Username
				}
			}
		}
	}

	// Report stats
	if d.Reports != nil {
		resp.ReportsReceived, resp.ReportsFiled = d.Reports.CountByUser(targetID)
		resp.RecentReports, _ = d.Reports.ListByTarget(targetID, 10)
	}

	util.JSON(w, http.StatusOK, resp)
}

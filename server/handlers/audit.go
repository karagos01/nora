package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// logAudit logs an action to the audit_log. Fire-and-forget.
func (d *Deps) logAudit(userID, action, targetType, targetID string, details any) {
	go func() {
		detailsJSON := "{}"
		if details != nil {
			if b, err := json.Marshal(details); err == nil {
				detailsJSON = string(b)
			}
		}

		id, _ := uuid.NewV7()
		entry := &models.AuditLogEntry{
			ID:         id.String(),
			UserID:     userID,
			Action:     action,
			TargetType: targetType,
			TargetID:   targetID,
			Details:    detailsJSON,
		}
		if err := d.AuditLog.Create(entry); err != nil {
			slog.Error("failed to write audit log", "action", action, "error", err)
		}
	}()
}

func (d *Deps) ListUserActivity(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	targetID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermViewActivity); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.canActOn(user, targetID); err != nil {
		util.Error(w, http.StatusForbidden, "cannot view activity of higher-ranked users")
		return
	}

	before := r.URL.Query().Get("before")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}

	entries, err := d.AuditLog.ListByUser(targetID, before, limit)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list activity")
		return
	}
	if entries == nil {
		entries = []models.AuditLogEntry{}
	}

	util.JSON(w, http.StatusOK, entries)
}

func (d *Deps) ListUserMessages(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	targetID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermViewActivity); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if err := d.canActOn(user, targetID); err != nil {
		util.Error(w, http.StatusForbidden, "cannot view messages of higher-ranked users")
		return
	}

	before := r.URL.Query().Get("before")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}

	messages, err := d.AuditLog.ListUserMessages(d.DB.DB, targetID, before, limit)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	if messages == nil {
		messages = []models.Message{}
	}

	// Load attachments
	if len(messages) > 0 {
		ids := make([]string, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		attMap, _ := d.Attachments.ListByMessageIDs(ids)
		for i := range messages {
			if atts, ok := attMap[messages[i].ID]; ok {
				for j := range atts {
					if strings.HasPrefix(atts[j].Filepath, "/api/") {
						atts[j].URL = atts[j].Filepath
					} else {
						atts[j].URL = "/api/uploads/" + atts[j].Filepath
					}
				}
				messages[i].Attachments = atts
			}
		}
	}

	util.JSON(w, http.StatusOK, messages)
}

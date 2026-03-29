package handlers

import (
	"log"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	autoQuarantineReportCount = 3  // reports needed to auto-quarantine
	autoQuarantineWindowHours = 24 // time window for counting reports
)

type reportRequest struct {
	TargetUserID    string `json:"target_user_id"`
	TargetMessageID string `json:"target_message_id"`
	Reason          string `json:"reason"`
}

// CreateReport creates a new user/message report.
func (d *Deps) CreateReport(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	var req reportRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Reason = strings.TrimSpace(req.Reason)
	if req.TargetUserID == "" {
		util.Error(w, http.StatusBadRequest, "target_user_id required")
		return
	}
	if req.TargetUserID == user.ID {
		util.Error(w, http.StatusBadRequest, "cannot report yourself")
		return
	}
	if len(req.Reason) > 500 {
		util.Error(w, http.StatusBadRequest, "reason must be 500 chars or less")
		return
	}

	// Prevent duplicate reports
	if d.Reports.HasReported(user.ID, req.TargetUserID) {
		util.Error(w, http.StatusConflict, "you already reported this user")
		return
	}

	id, _ := uuid.NewV7()
	report := &models.Report{
		ID:              id.String(),
		ReporterID:      user.ID,
		TargetUserID:    req.TargetUserID,
		TargetMessageID: req.TargetMessageID,
		Reason:          req.Reason,
		CreatedAt:       time.Now().UTC(),
	}

	if err := d.Reports.Create(report); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create report")
		return
	}

	// Check auto-quarantine threshold
	recentCount := d.Reports.CountRecentByTarget(req.TargetUserID, autoQuarantineWindowHours)
	if recentCount >= autoQuarantineReportCount {
		// Auto-quarantine + hide messages
		if !d.Quarantine.IsInQuarantine(req.TargetUserID) {
			d.Quarantine.Create(req.TargetUserID, nil) // indefinite until admin reviews
			log.Printf("Auto-quarantined user %s after %d reports in %d hours",
				req.TargetUserID, recentCount, autoQuarantineWindowHours)

			// Notify admins via WS
			event, _ := ws.NewEvent("report.auto_quarantine", map[string]interface{}{
				"user_id":      req.TargetUserID,
				"report_count": recentCount,
			})
			d.Hub.Broadcast(event)
		}
	}

	util.JSON(w, http.StatusCreated, map[string]string{
		"id":     report.ID,
		"status": "reported",
	})
}

// ListReports returns all pending reports (admin).
func (d *Deps) ListReports(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	reports, err := d.Reports.ListPending()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list reports")
		return
	}
	if reports == nil {
		reports = []models.Report{}
	}
	util.JSON(w, http.StatusOK, reports)
}

// ReviewReport marks a report as reviewed or dismissed.
func (d *Deps) ReviewReport(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	reportID := r.PathValue("id")

	if err := d.requirePermission(user, models.PermBan); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Status string `json:"status"` // "reviewed" or "dismissed"
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "reviewed" && req.Status != "dismissed" {
		util.Error(w, http.StatusBadRequest, "status must be 'reviewed' or 'dismissed'")
		return
	}

	if err := d.Reports.Review(reportID, user.ID, req.Status); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to review report")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

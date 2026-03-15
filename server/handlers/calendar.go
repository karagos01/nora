package handlers

import (
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

// ListEvents — GET /api/events?from=&to=
// For recurring events, generates virtual instances within the given range.
func (d *Deps) ListEvents(w http.ResponseWriter, r *http.Request) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	var from, to time.Time
	var err error

	if fromStr != "" {
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			util.Error(w, http.StatusBadRequest, "invalid from date")
			return
		}
	} else {
		from = time.Now().UTC().AddDate(0, -1, 0)
	}

	if toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			util.Error(w, http.StatusBadRequest, "invalid to date")
			return
		}
	} else {
		to = time.Now().UTC().AddDate(0, 3, 0)
	}

	events, err := d.CalendarQ.ListEvents(from, to)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list events")
		return
	}

	// Expand recurring events into virtual instances
	var result []models.Event
	for _, e := range events {
		if e.RecurrenceRule == "" {
			// Non-recurring event — add only if within range
			if !e.StartsAt.Before(from) && !e.StartsAt.After(to) {
				result = append(result, e)
			}
			continue
		}
		// Generate virtual instances for recurring event
		instances := expandRecurringEvent(e, from, to)
		result = append(result, instances...)
	}

	if result == nil {
		result = []models.Event{}
	}
	util.JSON(w, http.StatusOK, result)
}

// expandRecurringEvent generates instances of a recurring event within the given range.
// Returns virtual copies with the same ID but shifted starts_at/ends_at.
func expandRecurringEvent(e models.Event, from, to time.Time) []models.Event {
	var instances []models.Event

	// Event duration (for shifting ends_at)
	var duration time.Duration
	if e.EndsAt != nil {
		duration = e.EndsAt.Sub(e.StartsAt)
	}

	// Generate instances from starts_at forward
	// Limit: max 365 instances (protection against infinite loop)
	current := e.StartsAt
	for i := 0; i < 365; i++ {
		if current.After(to) {
			break
		}
		if !current.Before(from) {
			instance := e
			instance.StartsAt = current
			if e.EndsAt != nil {
				end := current.Add(duration)
				instance.EndsAt = &end
			}
			instances = append(instances, instance)
		}
		current = advanceByRule(current, e.RecurrenceRule)
	}

	return instances
}

// advanceByRule advances the time by one interval according to the rule.
func advanceByRule(t time.Time, rule string) time.Time {
	switch rule {
	case "daily":
		return t.AddDate(0, 0, 1)
	case "weekly":
		return t.AddDate(0, 0, 7)
	case "monthly":
		return t.AddDate(0, 1, 0)
	case "yearly":
		return t.AddDate(1, 0, 0)
	default:
		// Unknown rule — advance by 100 years (effectively end the loop)
		return t.AddDate(100, 0, 0)
	}
}

// CreateEvent — POST /api/events
func (d *Deps) CreateEvent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	// Quarantine check
	if err := d.checkQuarantine(user.ID, "send_messages"); err != nil {
		util.Error(w, http.StatusForbidden, err.Error())
		return
	}

	// Verify PermSendMessages
	if err := d.requirePermission(user, models.PermSendMessages); err != nil {
		util.Error(w, http.StatusForbidden, "no permission")
		return
	}

	var req struct {
		Title          string  `json:"title"`
		Description    string  `json:"description"`
		StartsAt       string  `json:"starts_at"`
		EndsAt         *string `json:"ends_at,omitempty"`
		Location       string  `json:"location"`
		Color          string  `json:"color"`
		AllDay         bool    `json:"all_day"`
		RecurrenceRule string  `json:"recurrence_rule"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" || len(req.Title) > 200 {
		util.Error(w, http.StatusBadRequest, "title must be 1-200 characters")
		return
	}

	// Validate recurrence_rule
	if req.RecurrenceRule != "" && req.RecurrenceRule != "daily" && req.RecurrenceRule != "weekly" && req.RecurrenceRule != "monthly" && req.RecurrenceRule != "yearly" {
		util.Error(w, http.StatusBadRequest, "recurrence_rule must be empty, daily, weekly, monthly or yearly")
		return
	}

	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid starts_at date")
		return
	}

	var endsAt *time.Time
	if req.EndsAt != nil && *req.EndsAt != "" {
		t, err := time.Parse(time.RFC3339, *req.EndsAt)
		if err != nil {
			util.Error(w, http.StatusBadRequest, "invalid ends_at date")
			return
		}
		endsAt = &t
	}

	if req.Color == "" {
		req.Color = "#3498db"
	}

	now := time.Now().UTC()
	eventID, _ := uuid.NewV7()
	event := &models.Event{
		ID:             eventID.String(),
		Title:          req.Title,
		Description:    req.Description,
		CreatorID:      user.ID,
		StartsAt:       startsAt,
		EndsAt:         endsAt,
		Location:       req.Location,
		Color:          req.Color,
		AllDay:         req.AllDay,
		RecurrenceRule: req.RecurrenceRule,
		CreatedAt:      now,
	}

	if err := d.CalendarQ.CreateEvent(event); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create event")
		return
	}

	// Load with creator JOIN
	full, err := d.CalendarQ.GetEvent(event.ID)
	if err != nil {
		full = event
	}

	msg, _ := ws.NewEvent(ws.EventCalendarCreate, full)
	d.Hub.Broadcast(msg)

	util.JSON(w, http.StatusCreated, full)
}

// GetEvent — GET /api/events/{id}
func (d *Deps) GetEvent(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")

	event, err := d.CalendarQ.GetEvent(eventID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "event not found")
		return
	}

	util.JSON(w, http.StatusOK, event)
}

// UpdateEvent — PATCH /api/events/{id}
func (d *Deps) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	eventID := r.PathValue("id")

	event, err := d.CalendarQ.GetEvent(eventID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "event not found")
		return
	}

	// Only creator or admin
	if event.CreatorID != user.ID {
		if err := d.requirePermission(user, models.PermManageChannels); err != nil {
			util.Error(w, http.StatusForbidden, "only creator or admin can edit event")
			return
		}
	}

	var req struct {
		Title          *string `json:"title,omitempty"`
		Description    *string `json:"description,omitempty"`
		StartsAt       *string `json:"starts_at,omitempty"`
		EndsAt         *string `json:"ends_at,omitempty"`
		ClearEndsAt    bool    `json:"clear_ends_at,omitempty"`
		Location       *string `json:"location,omitempty"`
		Color          *string `json:"color,omitempty"`
		AllDay         *bool   `json:"all_day,omitempty"`
		RecurrenceRule *string `json:"recurrence_rule,omitempty"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title != nil {
		if *req.Title == "" || len(*req.Title) > 200 {
			util.Error(w, http.StatusBadRequest, "title must be 1-200 characters")
			return
		}
		event.Title = *req.Title
	}
	if req.Description != nil {
		event.Description = *req.Description
	}
	if req.StartsAt != nil {
		t, err := time.Parse(time.RFC3339, *req.StartsAt)
		if err != nil {
			util.Error(w, http.StatusBadRequest, "invalid starts_at date")
			return
		}
		event.StartsAt = t
	}
	if req.EndsAt != nil && *req.EndsAt != "" {
		t, err := time.Parse(time.RFC3339, *req.EndsAt)
		if err != nil {
			util.Error(w, http.StatusBadRequest, "invalid ends_at date")
			return
		}
		event.EndsAt = &t
	}
	if req.ClearEndsAt {
		event.EndsAt = nil
	}
	if req.Location != nil {
		event.Location = *req.Location
	}
	if req.Color != nil {
		event.Color = *req.Color
	}
	if req.AllDay != nil {
		event.AllDay = *req.AllDay
	}
	if req.RecurrenceRule != nil {
		// Validate recurrence_rule
		rule := *req.RecurrenceRule
		if rule != "" && rule != "daily" && rule != "weekly" && rule != "monthly" && rule != "yearly" {
			util.Error(w, http.StatusBadRequest, "recurrence_rule must be empty, daily, weekly, monthly or yearly")
			return
		}
		event.RecurrenceRule = rule
	}

	if err := d.CalendarQ.UpdateEvent(event); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update event")
		return
	}

	msg, _ := ws.NewEvent(ws.EventCalendarUpdate, event)
	d.Hub.Broadcast(msg)

	util.JSON(w, http.StatusOK, event)
}

// DeleteEvent — DELETE /api/events/{id}
func (d *Deps) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	eventID := r.PathValue("id")

	event, err := d.CalendarQ.GetEvent(eventID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "event not found")
		return
	}

	// Only creator or admin
	if event.CreatorID != user.ID {
		if err := d.requirePermission(user, models.PermManageChannels); err != nil {
			util.Error(w, http.StatusForbidden, "only creator or admin can delete event")
			return
		}
	}

	if err := d.CalendarQ.DeleteEvent(eventID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete event")
		return
	}

	msg, _ := ws.NewEvent(ws.EventCalendarDelete, map[string]string{"id": eventID})
	d.Hub.Broadcast(msg)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetEventReminder — POST /api/events/{id}/remind
func (d *Deps) SetEventReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	eventID := r.PathValue("id")

	event, err := d.CalendarQ.GetEvent(eventID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "event not found")
		return
	}

	var req struct {
		MinutesBefore int `json:"minutes_before"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.MinutesBefore <= 0 {
		util.Error(w, http.StatusBadRequest, "minutes_before must be positive")
		return
	}

	remindAt := event.StartsAt.Add(-time.Duration(req.MinutesBefore) * time.Minute)

	// Delete existing reminder for this user
	d.CalendarQ.DeleteReminder(eventID, user.ID)

	reminderID, _ := uuid.NewV7()
	reminder := &models.EventReminder{
		ID:       reminderID.String(),
		EventID:  eventID,
		UserID:   user.ID,
		RemindAt: remindAt,
	}

	if err := d.CalendarQ.CreateReminder(reminder); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create reminder")
		return
	}

	util.JSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"remind_at": remindAt,
	})
}

// RemoveEventReminder — DELETE /api/events/{id}/remind
func (d *Deps) RemoveEventReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	eventID := r.PathValue("id")

	if err := d.CalendarQ.DeleteReminder(eventID, user.ID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove reminder")
		return
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DispatchEventReminders is called from the ticker in main.go.
// For recurring events: after dispatch, creates a new reminder for the next instance.
func (d *Deps) DispatchEventReminders() {
	now := time.Now().UTC()
	due, err := d.CalendarQ.ListDueReminders(now)
	if err != nil {
		slog.Error("calendar: failed to list due reminders", "error", err)
		return
	}

	for _, dr := range due {
		payload := map[string]interface{}{
			"event_id":  dr.EventID,
			"title":     dr.EventTitle,
			"starts_at": dr.StartsAt,
		}

		msg, _ := ws.NewEvent(ws.EventCalendarReminder, payload)
		d.Hub.BroadcastToUser(dr.UserID, msg)

		d.CalendarQ.MarkReminded(dr.ID)

		// For recurring events, create reminder for next instance
		event, err := d.CalendarQ.GetEvent(dr.EventID)
		if err != nil || event.RecurrenceRule == "" {
			continue
		}

		// Calculate reminder offset (how many minutes before start)
		reminderOffset := dr.StartsAt.Sub(dr.RemindAt)
		if reminderOffset < 0 {
			reminderOffset = 15 * time.Minute
		}

		// Find next instance after current starts_at
		nextStart := advanceByRule(dr.StartsAt, event.RecurrenceRule)

		nextRemindAt := nextStart.Add(-reminderOffset)
		if nextRemindAt.Before(now) {
			// Next reminder is in the past, skip
			continue
		}

		nextID, _ := uuid.NewV7()
		nextReminder := &models.EventReminder{
			ID:       nextID.String(),
			EventID:  dr.EventID,
			UserID:   dr.UserID,
			RemindAt: nextRemindAt,
		}
		if err := d.CalendarQ.CreateReminder(nextReminder); err != nil {
			slog.Error("calendar: failed to create recurring reminder", "error", err)
		}
	}
}

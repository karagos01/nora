package queries

import (
	"database/sql"
	"nora/models"
	"time"
)

type CalendarQueries struct {
	DB *sql.DB
}

// CreateEvent vloží nový event do DB.
func (q *CalendarQueries) CreateEvent(e *models.Event) error {
	_, err := q.DB.Exec(
		`INSERT INTO events (id, title, description, creator_id, starts_at, ends_at, location, color, all_day, recurrence_rule, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Title, e.Description, e.CreatorID, e.StartsAt, e.EndsAt, e.Location, e.Color, e.AllDay, e.RecurrenceRule, e.CreatedAt,
	)
	return err
}

// UpdateEvent aktualizuje existující event.
func (q *CalendarQueries) UpdateEvent(e *models.Event) error {
	_, err := q.DB.Exec(
		`UPDATE events SET title = ?, description = ?, starts_at = ?, ends_at = ?, location = ?, color = ?, all_day = ?, recurrence_rule = ? WHERE id = ?`,
		e.Title, e.Description, e.StartsAt, e.EndsAt, e.Location, e.Color, e.AllDay, e.RecurrenceRule, e.ID,
	)
	return err
}

// DeleteEvent smaže event (kaskádově smaže i remindery).
func (q *CalendarQueries) DeleteEvent(id string) error {
	_, err := q.DB.Exec(`DELETE FROM events WHERE id = ?`, id)
	return err
}

// GetEvent vrátí event s creator JOINem.
func (q *CalendarQueries) GetEvent(id string) (*models.Event, error) {
	var e models.Event
	var endsAt sql.NullTime
	var allDay int
	var cuID, cuUser, cuDisplay, cuAvatar sql.NullString

	err := q.DB.QueryRow(`
		SELECT e.id, e.title, e.description, e.creator_id, e.starts_at, e.ends_at,
		       e.location, e.color, e.all_day, e.recurrence_rule, e.created_at,
		       u.id, u.username, u.display_name, u.avatar_url
		FROM events e
		LEFT JOIN users u ON e.creator_id = u.id
		WHERE e.id = ?`, id,
	).Scan(
		&e.ID, &e.Title, &e.Description, &e.CreatorID, &e.StartsAt, &endsAt,
		&e.Location, &e.Color, &allDay, &e.RecurrenceRule, &e.CreatedAt,
		&cuID, &cuUser, &cuDisplay, &cuAvatar,
	)
	if err != nil {
		return nil, err
	}

	if endsAt.Valid {
		e.EndsAt = &endsAt.Time
	}
	e.AllDay = allDay != 0
	if cuID.Valid {
		e.Creator = &models.User{
			ID:          cuID.String,
			Username:    cuUser.String,
			DisplayName: cuDisplay.String,
			AvatarURL:   cuAvatar.String,
		}
	}

	return &e, nil
}

// ListEvents vrátí eventy v daném časovém rozsahu (bez recurring expanze — tu dělá handler).
func (q *CalendarQueries) ListEvents(from, to time.Time) ([]models.Event, error) {
	rows, err := q.DB.Query(`
		SELECT e.id, e.title, e.description, e.creator_id, e.starts_at, e.ends_at,
		       e.location, e.color, e.all_day, e.recurrence_rule, e.created_at,
		       u.id, u.username, u.display_name, u.avatar_url
		FROM events e
		LEFT JOIN users u ON e.creator_id = u.id
		WHERE (e.starts_at >= ? AND e.starts_at <= ?) OR e.recurrence_rule != ''
		ORDER BY e.starts_at`, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var e models.Event
		var endsAt sql.NullTime
		var allDay int
		var cuID, cuUser, cuDisplay, cuAvatar sql.NullString

		if err := rows.Scan(
			&e.ID, &e.Title, &e.Description, &e.CreatorID, &e.StartsAt, &endsAt,
			&e.Location, &e.Color, &allDay, &e.RecurrenceRule, &e.CreatedAt,
			&cuID, &cuUser, &cuDisplay, &cuAvatar,
		); err != nil {
			continue
		}

		if endsAt.Valid {
			e.EndsAt = &endsAt.Time
		}
		e.AllDay = allDay != 0
		if cuID.Valid {
			e.Creator = &models.User{
				ID:          cuID.String,
				Username:    cuUser.String,
				DisplayName: cuDisplay.String,
				AvatarURL:   cuAvatar.String,
			}
		}

		events = append(events, e)
	}
	return events, nil
}

// GetRecurringEvents vrátí eventy s neprázdným recurrence_rule.
func (q *CalendarQueries) GetRecurringEvents() ([]models.Event, error) {
	rows, err := q.DB.Query(`
		SELECT e.id, e.title, e.description, e.creator_id, e.starts_at, e.ends_at,
		       e.location, e.color, e.all_day, e.recurrence_rule, e.created_at,
		       u.id, u.username, u.display_name, u.avatar_url
		FROM events e
		LEFT JOIN users u ON e.creator_id = u.id
		WHERE e.recurrence_rule != ''
		ORDER BY e.starts_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var e models.Event
		var endsAt sql.NullTime
		var allDay int
		var cuID, cuUser, cuDisplay, cuAvatar sql.NullString

		if err := rows.Scan(
			&e.ID, &e.Title, &e.Description, &e.CreatorID, &e.StartsAt, &endsAt,
			&e.Location, &e.Color, &allDay, &e.RecurrenceRule, &e.CreatedAt,
			&cuID, &cuUser, &cuDisplay, &cuAvatar,
		); err != nil {
			continue
		}

		if endsAt.Valid {
			e.EndsAt = &endsAt.Time
		}
		e.AllDay = allDay != 0
		if cuID.Valid {
			e.Creator = &models.User{
				ID:          cuID.String,
				Username:    cuUser.String,
				DisplayName: cuDisplay.String,
				AvatarURL:   cuAvatar.String,
			}
		}

		events = append(events, e)
	}
	return events, nil
}

// CreateReminder vytvoří reminder pro uživatele.
func (q *CalendarQueries) CreateReminder(r *models.EventReminder) error {
	_, err := q.DB.Exec(
		`INSERT INTO event_reminders (id, event_id, user_id, remind_at, reminded, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
		r.ID, r.EventID, r.UserID, r.RemindAt, time.Now().UTC(),
	)
	return err
}

// DeleteReminder smaže reminder pro daného uživatele a event.
func (q *CalendarQueries) DeleteReminder(eventID, userID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM event_reminders WHERE event_id = ? AND user_id = ?`, eventID, userID,
	)
	return err
}

// GetUserReminder vrátí reminder uživatele pro daný event (nebo nil).
func (q *CalendarQueries) GetUserReminder(eventID, userID string) (*models.EventReminder, error) {
	var r models.EventReminder
	var reminded int
	err := q.DB.QueryRow(
		`SELECT id, event_id, user_id, remind_at, reminded FROM event_reminders WHERE event_id = ? AND user_id = ?`,
		eventID, userID,
	).Scan(&r.ID, &r.EventID, &r.UserID, &r.RemindAt, &reminded)
	if err != nil {
		return nil, err
	}
	r.Reminded = reminded != 0
	return &r, nil
}

// DueReminder obsahuje reminder + název eventu pro notifikaci.
type DueReminder struct {
	models.EventReminder
	EventTitle string
	StartsAt   time.Time
}

// ListDueReminders vrátí remindery které jsou splatné a ještě nebyly odeslány.
func (q *CalendarQueries) ListDueReminders(now time.Time) ([]DueReminder, error) {
	rows, err := q.DB.Query(`
		SELECT r.id, r.event_id, r.user_id, r.remind_at, r.reminded, e.title, e.starts_at
		FROM event_reminders r
		JOIN events e ON r.event_id = e.id
		WHERE r.remind_at <= ? AND r.reminded = 0`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reminders []DueReminder
	for rows.Next() {
		var dr DueReminder
		var reminded int
		if err := rows.Scan(&dr.ID, &dr.EventID, &dr.UserID, &dr.RemindAt, &reminded, &dr.EventTitle, &dr.StartsAt); err != nil {
			continue
		}
		dr.Reminded = reminded != 0
		reminders = append(reminders, dr)
	}
	return reminders, nil
}

// MarkReminded označí reminder jako odeslaný.
func (q *CalendarQueries) MarkReminded(id string) error {
	_, err := q.DB.Exec(`UPDATE event_reminders SET reminded = 1 WHERE id = ?`, id)
	return err
}

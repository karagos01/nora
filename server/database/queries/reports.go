package queries

import (
	"database/sql"
	"nora/models"
	"time"
)

type ReportQueries struct {
	DB *sql.DB
}

func (q *ReportQueries) Create(r *models.Report) error {
	_, err := q.DB.Exec(
		`INSERT INTO reports (id, reporter_id, target_user_id, target_message_id, reason, status, created_at)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
		r.ID, r.ReporterID, r.TargetUserID, r.TargetMessageID, r.Reason, r.CreatedAt,
	)
	return err
}

// ListPending returns all pending reports with usernames.
func (q *ReportQueries) ListPending() ([]models.Report, error) {
	rows, err := q.DB.Query(`
		SELECT r.id, r.reporter_id, r.target_user_id, r.target_message_id, r.reason, r.status, r.created_at,
		       COALESCE(reporter.username, ''), COALESCE(target.username, '')
		FROM reports r
		LEFT JOIN users reporter ON reporter.id = r.reporter_id
		LEFT JOIN users target ON target.id = r.target_user_id
		WHERE r.status = 'pending'
		ORDER BY r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Report
	for rows.Next() {
		var r models.Report
		var createdAt string
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.TargetUserID, &r.TargetMessageID, &r.Reason, &r.Status, &createdAt,
			&r.ReporterName, &r.TargetName); err == nil {
			r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
			result = append(result, r)
		}
	}
	return result, rows.Err()
}

// Review marks a report as reviewed.
func (q *ReportQueries) Review(id, reviewerID, status string) error {
	_, err := q.DB.Exec(
		`UPDATE reports SET status = ?, reviewed_by = ?, reviewed_at = ? WHERE id = ?`,
		status, reviewerID, time.Now().UTC(), id,
	)
	return err
}

// CountRecentByTarget returns how many pending reports a user has received in the last N hours.
func (q *ReportQueries) CountRecentByTarget(targetUserID string, hours int) int {
	var count int
	q.DB.QueryRow(`
		SELECT COUNT(*) FROM reports
		WHERE target_user_id = ? AND status = 'pending'
		AND created_at > datetime('now', '-' || ? || ' hours')`,
		targetUserID, hours,
	).Scan(&count)
	return count
}

// CountByUser returns (received, filed) report counts for a user.
func (q *ReportQueries) CountByUser(userID string) (received, filed int) {
	q.DB.QueryRow("SELECT COUNT(*) FROM reports WHERE target_user_id = ?", userID).Scan(&received)
	q.DB.QueryRow("SELECT COUNT(*) FROM reports WHERE reporter_id = ?", userID).Scan(&filed)
	return
}

// ListByTarget returns recent reports against a user.
func (q *ReportQueries) ListByTarget(userID string, limit int) ([]models.Report, error) {
	rows, err := q.DB.Query(`
		SELECT r.id, r.reporter_id, r.target_user_id, r.target_message_id, r.reason, r.status, r.created_at,
		       COALESCE(reporter.username, ''), COALESCE(target.username, '')
		FROM reports r
		LEFT JOIN users reporter ON reporter.id = r.reporter_id
		LEFT JOIN users target ON target.id = r.target_user_id
		WHERE r.target_user_id = ?
		ORDER BY r.created_at DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Report
	for rows.Next() {
		var r models.Report
		var createdAt string
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.TargetUserID, &r.TargetMessageID, &r.Reason, &r.Status, &createdAt,
			&r.ReporterName, &r.TargetName); err == nil {
			r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
			result = append(result, r)
		}
	}
	return result, rows.Err()
}

// HasReported checks if a user already reported a target (prevents spam reports).
func (q *ReportQueries) HasReported(reporterID, targetUserID string) bool {
	var count int
	q.DB.QueryRow(`
		SELECT COUNT(*) FROM reports
		WHERE reporter_id = ? AND target_user_id = ? AND status = 'pending'`,
		reporterID, targetUserID,
	).Scan(&count)
	return count > 0
}

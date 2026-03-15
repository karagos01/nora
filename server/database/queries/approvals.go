package queries

import (
	"database/sql"
	"nora/models"
)

type ApprovalQueries struct {
	DB *sql.DB
}

func (q *ApprovalQueries) Create(userID, username, invitedByID string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO pending_approvals (user_id, requested_username, invited_by_id, requested_at, status)
		 VALUES (?, ?, ?, datetime('now'), 'pending')`,
		userID, username, invitedByID,
	)
	return err
}

func (q *ApprovalQueries) IsPending(userID string) bool {
	var count int
	q.DB.QueryRow("SELECT COUNT(*) FROM pending_approvals WHERE user_id = ? AND status = 'pending'", userID).Scan(&count)
	return count > 0
}

func (q *ApprovalQueries) ListPending() ([]models.PendingApproval, error) {
	rows, err := q.DB.Query(
		`SELECT pa.user_id, pa.requested_username, pa.invited_by_id,
		        pa.requested_at, pa.reviewed_by, pa.reviewed_at, pa.status,
		        COALESCE(inv.username, '')
		 FROM pending_approvals pa
		 LEFT JOIN users inv ON pa.invited_by_id = inv.id
		 WHERE pa.status = 'pending'
		 ORDER BY pa.requested_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []models.PendingApproval
	for rows.Next() {
		var a models.PendingApproval
		if err := rows.Scan(&a.UserID, &a.RequestedUsername, &a.InvitedByID,
			&a.RequestedAt, &a.ReviewedBy, &a.ReviewedAt, &a.Status, &a.InviterUsername); err != nil {
			return nil, err
		}
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}

func (q *ApprovalQueries) Approve(userID, reviewerID string) error {
	_, err := q.DB.Exec(
		`UPDATE pending_approvals SET status = 'approved', reviewed_by = ?, reviewed_at = datetime('now')
		 WHERE user_id = ? AND status = 'pending'`,
		reviewerID, userID,
	)
	return err
}

func (q *ApprovalQueries) Reject(userID, reviewerID string) error {
	_, err := q.DB.Exec(
		`UPDATE pending_approvals SET status = 'rejected', reviewed_by = ?, reviewed_at = datetime('now')
		 WHERE user_id = ? AND status = 'pending'`,
		reviewerID, userID,
	)
	return err
}

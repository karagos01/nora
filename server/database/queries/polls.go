package queries

import (
	"database/sql"
	"nora/models"
	"strings"
)

type PollQueries struct {
	DB *sql.DB
}

func (q *PollQueries) Create(poll *models.Poll) error {
	_, err := q.DB.Exec(
		`INSERT INTO polls (id, message_id, question, poll_type, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		poll.ID, poll.MessageID, poll.Question, poll.PollType, poll.CreatedAt, poll.ExpiresAt,
	)
	return err
}

func (q *PollQueries) CreateOption(opt *models.PollOption) error {
	_, err := q.DB.Exec(
		`INSERT INTO poll_options (id, poll_id, label, position) VALUES (?, ?, ?, ?)`,
		opt.ID, opt.PollID, opt.Label, opt.Position,
	)
	return err
}

func (q *PollQueries) Vote(pollID, optionID, userID string) error {
	_, err := q.DB.Exec(
		`INSERT OR IGNORE INTO poll_votes (poll_id, option_id, user_id) VALUES (?, ?, ?)`,
		pollID, optionID, userID,
	)
	return err
}

func (q *PollQueries) Unvote(pollID, optionID, userID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM poll_votes WHERE poll_id = ? AND option_id = ? AND user_id = ?`,
		pollID, optionID, userID,
	)
	return err
}

func (q *PollQueries) UnvoteAll(pollID, userID string) error {
	_, err := q.DB.Exec(
		`DELETE FROM poll_votes WHERE poll_id = ? AND user_id = ?`,
		pollID, userID,
	)
	return err
}

func (q *PollQueries) HasVoted(pollID, optionID, userID string) (bool, error) {
	var n int
	err := q.DB.QueryRow(
		`SELECT 1 FROM poll_votes WHERE poll_id = ? AND option_id = ? AND user_id = ?`,
		pollID, optionID, userID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (q *PollQueries) CountUserVotes(pollID, userID string) (int, error) {
	var n int
	err := q.DB.QueryRow(
		`SELECT COUNT(*) FROM poll_votes WHERE poll_id = ? AND user_id = ?`,
		pollID, userID,
	).Scan(&n)
	return n, err
}

func (q *PollQueries) GetPollType(pollID string) (string, error) {
	var t string
	err := q.DB.QueryRow(`SELECT poll_type FROM polls WHERE id = ?`, pollID).Scan(&t)
	return t, err
}

func (q *PollQueries) GetByID(pollID string) (*models.Poll, error) {
	var poll models.Poll
	err := q.DB.QueryRow(
		`SELECT id, message_id, question, poll_type, created_at, expires_at FROM polls WHERE id = ?`,
		pollID,
	).Scan(&poll.ID, &poll.MessageID, &poll.Question, &poll.PollType, &poll.CreatedAt, &poll.ExpiresAt)
	if err != nil {
		return nil, err
	}

	// Načíst options
	rows, err := q.DB.Query(
		`SELECT id, poll_id, label, position FROM poll_options WHERE poll_id = ? ORDER BY position`,
		pollID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var opt models.PollOption
		if err := rows.Scan(&opt.ID, &opt.PollID, &opt.Label, &opt.Position); err != nil {
			return nil, err
		}
		poll.Options = append(poll.Options, opt)
	}

	// Načíst hlasy
	voteRows, err := q.DB.Query(
		`SELECT option_id, GROUP_CONCAT(user_id), COUNT(*) FROM poll_votes WHERE poll_id = ? GROUP BY option_id`,
		pollID,
	)
	if err != nil {
		return nil, err
	}
	defer voteRows.Close()

	voteCounts := make(map[string]int)
	voteUsers := make(map[string][]string)
	for voteRows.Next() {
		var optID, userIDs string
		var count int
		if err := voteRows.Scan(&optID, &userIDs, &count); err != nil {
			return nil, err
		}
		voteCounts[optID] = count
		voteUsers[optID] = strings.Split(userIDs, ",")
	}

	for i := range poll.Options {
		poll.Options[i].Count = voteCounts[poll.Options[i].ID]
		if poll.PollType != "anonymous" {
			poll.Options[i].UserIDs = voteUsers[poll.Options[i].ID]
		}
	}

	return &poll, nil
}

func (q *PollQueries) GetByMessageID(messageID string) (*models.Poll, error) {
	var pollID string
	err := q.DB.QueryRow(`SELECT id FROM polls WHERE message_id = ?`, messageID).Scan(&pollID)
	if err != nil {
		return nil, err
	}
	return q.GetByID(pollID)
}

func (q *PollQueries) GetByMessageIDs(ids []string) (map[string]*models.Poll, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `SELECT id, message_id, question, poll_type, created_at, expires_at FROM polls WHERE message_id IN (` +
		strings.Join(placeholders, ",") + `)`

	rows, err := q.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	polls := make(map[string]*models.Poll)
	var pollIDs []string
	for rows.Next() {
		var p models.Poll
		if err := rows.Scan(&p.ID, &p.MessageID, &p.Question, &p.PollType, &p.CreatedAt, &p.ExpiresAt); err != nil {
			return nil, err
		}
		polls[p.MessageID] = &p
		pollIDs = append(pollIDs, p.ID)
	}

	if len(pollIDs) == 0 {
		return polls, nil
	}

	// Batch load options
	optPlaceholders := make([]string, len(pollIDs))
	optArgs := make([]any, len(pollIDs))
	for i, id := range pollIDs {
		optPlaceholders[i] = "?"
		optArgs[i] = id
	}

	optRows, err := q.DB.Query(
		`SELECT id, poll_id, label, position FROM poll_options WHERE poll_id IN (`+
			strings.Join(optPlaceholders, ",")+`) ORDER BY position`,
		optArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer optRows.Close()

	optionsByPoll := make(map[string][]models.PollOption)
	for optRows.Next() {
		var opt models.PollOption
		if err := optRows.Scan(&opt.ID, &opt.PollID, &opt.Label, &opt.Position); err != nil {
			return nil, err
		}
		optionsByPoll[opt.PollID] = append(optionsByPoll[opt.PollID], opt)
	}

	// Batch load votes
	voteRows, err := q.DB.Query(
		`SELECT poll_id, option_id, GROUP_CONCAT(user_id), COUNT(*) FROM poll_votes WHERE poll_id IN (`+
			strings.Join(optPlaceholders, ",")+`) GROUP BY poll_id, option_id`,
		optArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer voteRows.Close()

	type voteInfo struct {
		count   int
		userIDs []string
	}
	// key: pollID+optionID
	votes := make(map[string]voteInfo)
	for voteRows.Next() {
		var pollID, optID, userIDs string
		var count int
		if err := voteRows.Scan(&pollID, &optID, &userIDs, &count); err != nil {
			return nil, err
		}
		votes[pollID+"|"+optID] = voteInfo{count: count, userIDs: strings.Split(userIDs, ",")}
	}

	// Sestavit finální poll objekty
	for _, p := range polls {
		p.Options = optionsByPoll[p.ID]
		for i := range p.Options {
			key := p.ID + "|" + p.Options[i].ID
			if v, ok := votes[key]; ok {
				p.Options[i].Count = v.count
				if p.PollType != "anonymous" {
					p.Options[i].UserIDs = v.userIDs
				}
			}
		}
	}

	return polls, nil
}

func (q *PollQueries) OptionBelongsToPoll(pollID, optionID string) (bool, error) {
	var n int
	err := q.DB.QueryRow(
		`SELECT 1 FROM poll_options WHERE id = ? AND poll_id = ?`,
		optionID, pollID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

package db

import "time"

// Retry mirrors the call_retries table.
type Retry struct {
	ID            int64  `json:"id"`
	LeadID        int64  `json:"lead_id"`
	CampaignID    int64  `json:"campaign_id"`
	OrgID         int64  `json:"org_id"`
	Attempts      int    `json:"attempts"`
	MaxAttempts   int    `json:"max_attempts"`
	Status        string `json:"status"` // pending, completed, exhausted, cancelled
	NextAttemptAt string `json:"next_attempt_at"`
	CreatedAt     string `json:"created_at"`
}

// CreateRetry inserts a new retry record. nextAttempt is when to retry.
func (d *DB) CreateRetry(leadID, campaignID, orgID int64, maxAttempts int, nextAttempt time.Time) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO call_retries (lead_id, campaign_id, org_id, attempts, max_attempts, status, next_attempt_at)
		VALUES (?,?,?,0,?,'pending',?)`,
		leadID, nullInt64(campaignID), orgID, maxAttempts, nextAttempt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetPendingRetries returns retries that are due and still pending.
func (d *DB) GetPendingRetries() ([]Retry, error) {
	rows, err := d.pool.Query(`
		SELECT id, lead_id, COALESCE(campaign_id,0), org_id,
		COALESCE(attempts,0), COALESCE(max_attempts,3),
		COALESCE(status,'pending'),
		DATE_FORMAT(next_attempt_at,'%Y-%m-%d %H:%i:%s'),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_retries
		WHERE status='pending' AND next_attempt_at <= NOW()
		ORDER BY next_attempt_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRetries(rows)
}

// GetRetriesByCampaign returns all retries for a campaign.
func (d *DB) GetRetriesByCampaign(campaignID int64) ([]Retry, error) {
	rows, err := d.pool.Query(`
		SELECT id, lead_id, COALESCE(campaign_id,0), org_id,
		COALESCE(attempts,0), COALESCE(max_attempts,3),
		COALESCE(status,'pending'),
		DATE_FORMAT(next_attempt_at,'%Y-%m-%d %H:%i:%s'),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_retries WHERE campaign_id=? ORDER BY id DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRetries(rows)
}

// UpdateRetryStatus updates status for a retry (completed/exhausted/cancelled).
func (d *DB) UpdateRetryStatus(id int64, status string) error {
	_, err := d.pool.Exec(
		`UPDATE call_retries SET status=? WHERE id=?`, status, id)
	return err
}

// IncrRetryAttempt increments attempts and schedules the next retry with exponential backoff.
// Returns true if exhausted (attempts >= max_attempts).
func (d *DB) IncrRetryAttempt(id int64) (exhausted bool, err error) {
	// Fetch current state
	var attempts, maxAttempts int
	if err = d.pool.QueryRow(
		`SELECT COALESCE(attempts,0), COALESCE(max_attempts,3) FROM call_retries WHERE id=?`, id,
	).Scan(&attempts, &maxAttempts); err != nil {
		return false, err
	}
	attempts++
	if attempts >= maxAttempts {
		_, err = d.pool.Exec(
			`UPDATE call_retries SET attempts=?, status='exhausted' WHERE id=?`, attempts, id)
		return true, err
	}
	// Exponential backoff: 30m, 1h, 2h, ...
	backoff := time.Duration(30<<uint(attempts-1)) * time.Minute
	next := time.Now().Add(backoff)
	_, err = d.pool.Exec(
		`UPDATE call_retries SET attempts=?, next_attempt_at=? WHERE id=?`,
		attempts, next, id)
	return false, err
}

// HasPendingOrExhaustedRetry returns true if a lead already has an active retry in the campaign.
func (d *DB) HasPendingOrExhaustedRetry(leadID, campaignID int64) (bool, error) {
	var count int
	err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM call_retries
		WHERE lead_id=? AND campaign_id=? AND status IN ('pending','exhausted')`,
		leadID, nullInt64(campaignID),
	).Scan(&count)
	return count > 0, err
}

func scanRetries(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Retry, error) {
	var list []Retry
	for rows.Next() {
		var r Retry
		if err := rows.Scan(&r.ID, &r.LeadID, &r.CampaignID, &r.OrgID,
			&r.Attempts, &r.MaxAttempts, &r.Status,
			&r.NextAttemptAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

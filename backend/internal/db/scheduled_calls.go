package db

import "time"

// ScheduledCall mirrors the scheduled_calls table.
type ScheduledCall struct {
	ID          int64  `json:"id"`
	OrgID       int64  `json:"org_id"`
	LeadID      int64  `json:"lead_id"`
	CampaignID  int64  `json:"campaign_id"`
	ScheduledAt string `json:"scheduled_at"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	CreatedAt   string `json:"created_at"`
}

// CreateScheduledCall inserts a new scheduled call.
func (d *DB) CreateScheduledCall(orgID, leadID, campaignID int64, scheduledAt time.Time, notes string) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO scheduled_calls (org_id, lead_id, campaign_id, scheduled_at, status, notes)
		VALUES (?,?,?,?,'pending',?)`,
		orgID, leadID, nullInt64(campaignID), scheduledAt, nullString(notes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetScheduledCallsByOrg returns all scheduled calls for an org ordered by scheduled_at ASC.
func (d *DB) GetScheduledCallsByOrg(orgID int64) ([]ScheduledCall, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, lead_id, COALESCE(campaign_id,0),
		DATE_FORMAT(scheduled_at,'%Y-%m-%d %H:%i:%s'),
		COALESCE(status,'pending'), COALESCE(notes,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM scheduled_calls WHERE org_id=? ORDER BY scheduled_at ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledCalls(rows)
}

// GetPendingScheduledCalls returns all pending calls whose scheduled_at has passed.
func (d *DB) GetPendingScheduledCalls() ([]ScheduledCall, error) {
	rows, err := d.pool.Query(`
		SELECT id, org_id, lead_id, COALESCE(campaign_id,0),
		DATE_FORMAT(scheduled_at,'%Y-%m-%d %H:%i:%s'),
		COALESCE(status,'pending'), COALESCE(notes,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM scheduled_calls
		WHERE status='pending' AND scheduled_at <= NOW()
		ORDER BY scheduled_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledCalls(rows)
}

// UpdateScheduledCallStatus sets the status column (e.g., "completed", "failed", "cancelled").
func (d *DB) UpdateScheduledCallStatus(id int64, status string) error {
	_, err := d.pool.Exec(`UPDATE scheduled_calls SET status=? WHERE id=?`, status, id)
	return err
}

// CancelScheduledCall marks a pending call as cancelled. Returns true if updated.
func (d *DB) CancelScheduledCall(orgID, id int64) (bool, error) {
	res, err := d.pool.Exec(
		`UPDATE scheduled_calls SET status='cancelled'
		 WHERE id=? AND org_id=? AND status='pending'`, id, orgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func scanScheduledCalls(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]ScheduledCall, error) {
	var list []ScheduledCall
	for rows.Next() {
		var sc ScheduledCall
		if err := rows.Scan(&sc.ID, &sc.OrgID, &sc.LeadID, &sc.CampaignID,
			&sc.ScheduledAt, &sc.Status, &sc.Notes, &sc.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, sc)
	}
	return list, rows.Err()
}

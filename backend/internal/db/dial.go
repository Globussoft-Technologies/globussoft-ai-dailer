package db

import (
	"database/sql"
	"errors"
)

// CallLog mirrors the call_logs table (one row per dial attempt).
type CallLog struct {
	ID           int64  `json:"id"`
	LeadID       int64  `json:"lead_id"`
	CampaignID   int64  `json:"campaign_id"`
	OrgID        int64  `json:"org_id"`
	CallSid      string `json:"call_sid"`
	Phone        string `json:"phone"`
	Provider     string `json:"provider"`
	Status       string `json:"status"`
	RecordingURL string `json:"recording_url"`
	CreatedAt    string `json:"created_at"`
}

// SaveCallLog inserts a call attempt record. Returns the new row ID.
func (d *DB) SaveCallLog(leadID, campaignID, orgID int64, callSid, provider, phone, status string) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO call_logs (lead_id, campaign_id, org_id, call_sid, provider, phone, status)
		VALUES (?,?,?,?,?,?,?)`,
		leadID, nullInt64(campaignID), orgID, nullString(callSid), provider, phone, status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCallLogStatus updates the status column for a given call_sid.
func (d *DB) UpdateCallLogStatus(callSid, status string) error {
	_, err := d.pool.Exec(
		`UPDATE call_logs SET status=? WHERE call_sid=?`, status, callSid)
	return err
}

// UpdateCallLogRecordingURL saves the recording URL for a given call_sid.
func (d *DB) UpdateCallLogRecordingURL(callSid, url string) error {
	_, err := d.pool.Exec(
		`UPDATE call_logs SET recording_url=? WHERE call_sid=?`, nullString(url), callSid)
	return err
}

// GetCallLogByCallSid fetches the most recent call_log row for a call_sid.
func (d *DB) GetCallLogByCallSid(callSid string) (*CallLog, error) {
	row := d.pool.QueryRow(`
		SELECT id, lead_id, COALESCE(campaign_id,0), org_id,
		COALESCE(call_sid,''), COALESCE(phone,''), COALESCE(provider,''),
		COALESCE(status,''), COALESCE(recording_url,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_logs WHERE call_sid=? ORDER BY id DESC LIMIT 1`, callSid)
	cl := &CallLog{}
	err := row.Scan(&cl.ID, &cl.LeadID, &cl.CampaignID, &cl.OrgID,
		&cl.CallSid, &cl.Phone, &cl.Provider, &cl.Status, &cl.RecordingURL, &cl.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cl, err
}

// GetLastDialMeta returns the most recent call_log row across all leads/campaigns.
func (d *DB) GetLastDialMeta() (*CallLog, error) {
	row := d.pool.QueryRow(`
		SELECT id, lead_id, COALESCE(campaign_id,0), org_id,
		COALESCE(call_sid,''), COALESCE(phone,''), COALESCE(provider,''),
		COALESCE(status,''), COALESCE(recording_url,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_logs ORDER BY id DESC LIMIT 1`)
	cl := &CallLog{}
	err := row.Scan(&cl.ID, &cl.LeadID, &cl.CampaignID, &cl.OrgID,
		&cl.CallSid, &cl.Phone, &cl.Provider, &cl.Status, &cl.RecordingURL, &cl.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cl, err
}

// GetFailedLeadsInCampaign returns leads in the campaign whose status starts with "Call Failed".
func (d *DB) GetFailedLeadsInCampaign(campaignID int64) ([]Lead, error) {
	rows, err := d.pool.Query(`
		SELECT `+leadColsL+`
		FROM leads l
		JOIN campaign_leads cl ON l.id=cl.lead_id
		WHERE cl.campaign_id=? AND l.status LIKE 'Call Failed%'
		ORDER BY l.id DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *l)
	}
	return list, rows.Err()
}

// IncrLeadDialAttempts increments the dial_attempts counter for a lead.
func (d *DB) IncrLeadDialAttempts(leadID int64) error {
	_, err := d.pool.Exec(
		`UPDATE leads SET dial_attempts=COALESCE(dial_attempts,0)+1 WHERE id=?`, leadID)
	return err
}

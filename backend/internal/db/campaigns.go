package db

import (
	"database/sql"
	"errors"
)

// Campaign mirrors the campaigns table (joined with products.name).
// Stats is populated by list endpoints (LEFT JOIN on campaign_leads) and left
// nil by single-campaign fetches that don't need it.
type Campaign struct {
	ID          int64          `json:"id"`
	OrgID       int64          `json:"org_id"`
	ProductID   int64          `json:"product_id"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	TTSProvider string         `json:"tts_provider"`
	TTSVoiceID  string         `json:"tts_voice_id"`
	TTSLanguage string         `json:"tts_language"`
	LeadSource  string         `json:"lead_source"`
	Channel     string         `json:"channel"`
	ProductName string         `json:"product_name"`
	CreatedAt   string         `json:"created_at"`
	Stats       *CampaignStats `json:"stats,omitempty"`
}

const campaignCols = `c.id, c.org_id, c.product_id, c.name,
	COALESCE(c.status,'active'), COALESCE(c.tts_provider,''), COALESCE(c.tts_voice_id,''),
	COALESCE(c.tts_language,''), COALESCE(c.lead_source,''),
	COALESCE(c.channel,'voice'),
	COALESCE(p.name,''), DATE_FORMAT(c.created_at,'%Y-%m-%d %H:%i:%s')`

func scanCampaign(row interface{ Scan(...any) error }) (*Campaign, error) {
	c := &Campaign{}
	err := row.Scan(&c.ID, &c.OrgID, &c.ProductID, &c.Name, &c.Status,
		&c.TTSProvider, &c.TTSVoiceID, &c.TTSLanguage, &c.LeadSource,
		&c.Channel, &c.ProductName, &c.CreatedAt)
	return c, err
}

// GetCampaignsByOrg returns all campaigns for an org ordered newest first.
// Stats (total/called/qualified/appointments) are computed in the same query
// via a LEFT JOIN on campaign_leads so the list endpoint stays single-round-trip.
func (d *DB) GetCampaignsByOrg(orgID int64) ([]Campaign, error) {
	const statsSub = `
		SELECT
			cl.campaign_id,
			COUNT(*) AS total,
			SUM(CASE WHEN COALESCE(l.status,'new') != 'new' THEN 1 ELSE 0 END) AS called,
			SUM(CASE WHEN l.status IN ('Warm','Summarized','Closed') THEN 1 ELSE 0 END) AS qualified,
			SUM(CASE WHEN l.status IN ('Summarized','Closed') THEN 1 ELSE 0 END) AS appointments
		FROM campaign_leads cl
		JOIN leads l ON l.id = cl.lead_id
		GROUP BY cl.campaign_id`

	rows, err := d.pool.Query(
		`SELECT `+campaignCols+`,
			COALESCE(s.total,0), COALESCE(s.called,0),
			COALESCE(s.qualified,0), COALESCE(s.appointments,0)
		FROM campaigns c
		JOIN products p ON c.product_id = p.id
		LEFT JOIN (`+statsSub+`) s ON s.campaign_id = c.id
		WHERE c.org_id=?
		ORDER BY c.created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Campaign
	for rows.Next() {
		c := Campaign{}
		stats := CampaignStats{}
		if err := rows.Scan(&c.ID, &c.OrgID, &c.ProductID, &c.Name, &c.Status,
			&c.TTSProvider, &c.TTSVoiceID, &c.TTSLanguage, &c.LeadSource,
			&c.Channel, &c.ProductName, &c.CreatedAt,
			&stats.Total, &stats.Called, &stats.Qualified, &stats.Appointments,
		); err != nil {
			return nil, err
		}
		c.Stats = &stats
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetCampaignByID fetches one campaign. Returns nil when not found.
func (d *DB) GetCampaignByID(id int64) (*Campaign, error) {
	row := d.pool.QueryRow(
		`SELECT `+campaignCols+` FROM campaigns c JOIN products p ON c.product_id=p.id WHERE c.id=?`, id)
	c, err := scanCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// CreateCampaign inserts a new campaign. Returns the new ID.
func (d *DB) CreateCampaign(orgID, productID int64, name, leadSource, channel string) (int64, error) {
	if channel == "" {
		channel = "voice"
	}
	res, err := d.pool.Exec(
		`INSERT INTO campaigns (org_id, product_id, name, lead_source, channel) VALUES (?,?,?,?,?)`,
		orgID, productID, name, nullString(leadSource), channel)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCampaign updates mutable campaign fields. Pass zero/empty to skip a field.
func (d *DB) UpdateCampaign(id int64, name, status, leadSource, channel string, productID int64) error {
	if name != "" {
		if _, err := d.pool.Exec(`UPDATE campaigns SET name=? WHERE id=?`, name, id); err != nil {
			return err
		}
	}
	if status != "" {
		if _, err := d.pool.Exec(`UPDATE campaigns SET status=? WHERE id=?`, status, id); err != nil {
			return err
		}
	}
	if leadSource != "" {
		if _, err := d.pool.Exec(`UPDATE campaigns SET lead_source=? WHERE id=?`, nullString(leadSource), id); err != nil {
			return err
		}
	}
	if productID != 0 {
		if _, err := d.pool.Exec(`UPDATE campaigns SET product_id=? WHERE id=?`, productID, id); err != nil {
			return err
		}
	}
	if channel != "" {
		if _, err := d.pool.Exec(`UPDATE campaigns SET channel=? WHERE id=?`, channel, id); err != nil {
			return err
		}
	}
	return nil
}

// GetCampaignNewLeads returns all leads in a campaign with status='new'.
func (d *DB) GetCampaignNewLeads(campaignID int64) ([]Lead, error) {
	rows, err := d.pool.Query(`
		SELECT `+leadColsL+`
		FROM leads l
		JOIN campaign_leads cl ON l.id=cl.lead_id
		WHERE cl.campaign_id=? AND COALESCE(l.status,'new')='new'
		ORDER BY l.id ASC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Lead
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *lead)
	}
	return list, rows.Err()
}

// DeleteCampaign deletes a campaign. Returns true if deleted.
func (d *DB) DeleteCampaign(id int64) (bool, error) {
	res, err := d.pool.Exec(`DELETE FROM campaigns WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AddLeadsToCampaign bulk-inserts campaign_leads (IGNORE duplicates). Returns added count.
func (d *DB) AddLeadsToCampaign(campaignID int64, leadIDs []int64) (int, error) {
	var added int
	for _, lid := range leadIDs {
		res, err := d.pool.Exec(
			`INSERT IGNORE INTO campaign_leads (campaign_id, lead_id) VALUES (?,?)`, campaignID, lid)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	return added, nil
}

// RemoveLeadFromCampaign removes one lead from a campaign. Returns true if removed.
func (d *DB) RemoveLeadFromCampaign(campaignID, leadID int64) (bool, error) {
	res, err := d.pool.Exec(
		`DELETE FROM campaign_leads WHERE campaign_id=? AND lead_id=?`, campaignID, leadID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CampaignLead is a Lead with per-campaign call stats.
type CampaignLead struct {
	Lead
	TranscriptCount int64 `json:"transcript_count"`
	RecordingCount  int64 `json:"recording_count"`
	DialAttempts    int64 `json:"dial_attempts"`
}

// GetCampaignLeads returns all leads in a campaign with call stats.
func (d *DB) GetCampaignLeads(campaignID int64) ([]CampaignLead, error) {
	rows, err := d.pool.Query(`
		SELECT `+leadColsL+`,
			(SELECT COUNT(*) FROM call_transcripts ct
			 WHERE ct.lead_id=l.id AND ct.campaign_id=? AND ct.call_duration_s>5) AS transcript_count,
			(SELECT COUNT(*) FROM call_transcripts ct
			 WHERE ct.lead_id=l.id AND ct.campaign_id=? AND ct.recording_url IS NOT NULL AND ct.recording_url!='') AS recording_count,
			(SELECT COUNT(*) FROM call_transcripts ct
			 WHERE ct.lead_id=l.id AND ct.campaign_id=?) AS dial_attempts
		FROM leads l
		JOIN campaign_leads cl ON l.id=cl.lead_id
		WHERE cl.campaign_id=?
		ORDER BY l.id DESC`, campaignID, campaignID, campaignID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CampaignLead
	for rows.Next() {
		cl, err := scanCampaignLead(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *cl)
	}
	return list, rows.Err()
}

func scanCampaignLead(row interface{ Scan(...any) error }) (*CampaignLead, error) {
	cl := &CampaignLead{}
	var orgIDInt sql.NullInt64
	var followUpNote, interest, extID, crmProvider sql.NullString
	err := row.Scan(
		&cl.ID, &orgIDInt, &cl.FirstName, &cl.LastName, &cl.Phone,
		&cl.Source, &cl.Status, &followUpNote, &interest, &extID, &crmProvider,
		&cl.CreatedAt,
		&cl.TranscriptCount, &cl.RecordingCount, &cl.DialAttempts,
	)
	if err != nil {
		return nil, err
	}
	if orgIDInt.Valid {
		cl.OrgID = orgIDInt.Int64
	}
	cl.FollowUpNote = followUpNote.String
	cl.Interest = interest.String
	cl.ExternalID = extID.String
	cl.CRMProvider = crmProvider.String
	return cl, nil
}

// CampaignStats holds aggregate campaign metrics.
type CampaignStats struct {
	Total        int64 `json:"total"`
	Called       int64 `json:"called"`
	Qualified    int64 `json:"qualified"`
	Appointments int64 `json:"appointments"`
}

// OrgDashboardSummary is the org-wide top-of-page card row for /crm. Visible
// to all authenticated roles (Admin / Agent / Viewer) without exposing full
// campaign objects — that's how non-Admins see meaningful numbers even
// though /api/campaigns itself is admin-gated.
type OrgDashboardSummary struct {
	Campaigns    int64 `json:"campaigns"`
	TotalLeads   int64 `json:"total_leads"`
	Called       int64 `json:"called"`
	Qualified    int64 `json:"qualified"`
	Appointments int64 `json:"appointments"`
}

// GetOrgDashboardSummary returns the 5 dashboard numbers (active-campaign
// count + aggregated lead status counts) for one org. Status filters mirror
// GetCampaignStats so per-campaign and org-wide totals stay consistent.
func (d *DB) GetOrgDashboardSummary(orgID int64) (OrgDashboardSummary, error) {
	var s OrgDashboardSummary
	if err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM campaigns WHERE org_id=? AND status='active'`, orgID,
	).Scan(&s.Campaigns); err != nil {
		return s, err
	}
	err := d.pool.QueryRow(`
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN COALESCE(l.status,'new') != 'new' THEN 1 ELSE 0 END), 0) AS called,
			COALESCE(SUM(CASE WHEN l.status IN ('Warm','Summarized','Closed') THEN 1 ELSE 0 END), 0) AS qualified,
			COALESCE(SUM(CASE WHEN l.status IN ('Summarized','Closed') THEN 1 ELSE 0 END), 0) AS appointments
		FROM campaign_leads cl
		JOIN leads l ON l.id = cl.lead_id
		JOIN campaigns c ON c.id = cl.campaign_id
		WHERE c.org_id=?`, orgID,
	).Scan(&s.TotalLeads, &s.Called, &s.Qualified, &s.Appointments)
	return s, err
}

// GetCampaignStats returns 4 aggregate metrics for a campaign.
func (d *DB) GetCampaignStats(campaignID int64) (CampaignStats, error) {
	var s CampaignStats
	if err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM campaign_leads WHERE campaign_id=?`, campaignID,
	).Scan(&s.Total); err != nil {
		return s, err
	}
	if err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM leads l JOIN campaign_leads cl ON l.id=cl.lead_id
		WHERE cl.campaign_id=? AND l.status NOT IN ('new')`, campaignID,
	).Scan(&s.Called); err != nil {
		return s, err
	}
	if err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM leads l JOIN campaign_leads cl ON l.id=cl.lead_id
		WHERE cl.campaign_id=? AND l.status IN ('Warm','Summarized','Closed')`, campaignID,
	).Scan(&s.Qualified); err != nil {
		return s, err
	}
	err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM leads l JOIN campaign_leads cl ON l.id=cl.lead_id
		WHERE cl.campaign_id=? AND l.status IN ('Summarized','Closed')`, campaignID,
	).Scan(&s.Appointments)
	return s, err
}

// CallLogEntry is one row of the campaign call log (Exotel-style).
type CallLogEntry struct {
	ID           int64   `json:"id"`
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	Phone        string  `json:"phone"`
	Source       string  `json:"source"`
	LeadStatus   string  `json:"lead_status"`
	Duration     float64 `json:"call_duration_s"`
	RecordingURL string  `json:"recording_url"`
	CreatedAt    string  `json:"created_at"`
	Outcome      string  `json:"outcome"`
}

// GetCampaignCallLog returns the call log for all leads in a campaign.
//
// Authoritative filter is ct.campaign_id — every transcript carries the
// campaign it was placed for. We deliberately do NOT join campaign_leads:
// Sim Web Call and the quick-dial paths produce a transcript without
// inserting a campaign_leads row, and the prior INNER JOIN dropped those
// rows on the floor (call counted in Live Activity / Analytics but
// invisible in Call Log — see issue #65 ["Call Log only shows 1 row …"]).
func (d *DB) GetCampaignCallLog(campaignID int64) ([]CallLogEntry, error) {
	rows, err := d.pool.Query(`
		SELECT
			ct.id,
			l.first_name, COALESCE(l.last_name,''), l.phone, COALESCE(l.source,''),
			COALESCE(l.status,''), COALESCE(ct.call_duration_s,0), COALESCE(ct.recording_url,''),
			DATE_FORMAT(ct.created_at,'%Y-%m-%d %H:%i:%s'),
			CASE
				WHEN ct.call_duration_s>30 AND l.status IN ('Summarized','Closed') THEN 'Completed'
				WHEN ct.call_duration_s>5 THEN 'Connected'
				WHEN l.status LIKE 'Call Failed (busy)%' THEN 'Busy'
				WHEN l.status LIKE 'Call Failed (failed)%' THEN 'Failed'
				WHEN l.status LIKE 'DND%' THEN 'DND Blocked'
				ELSE 'No Answer'
			END AS outcome
		FROM call_transcripts ct
		JOIN leads l ON ct.lead_id=l.id
		WHERE ct.campaign_id=?
		ORDER BY ct.created_at DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CallLogEntry
	for rows.Next() {
		var e CallLogEntry
		if err := rows.Scan(&e.ID, &e.FirstName, &e.LastName, &e.Phone, &e.Source,
			&e.LeadStatus, &e.Duration, &e.RecordingURL, &e.CreatedAt, &e.Outcome); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

// GetCampaignVoiceSettings returns TTS settings, falling back to org defaults.
func (d *DB) GetCampaignVoiceSettings(campaignID int64) (VoiceSettings, error) {
	var orgID int64
	var provider, voiceID, lang sql.NullString
	err := d.pool.QueryRow(
		`SELECT COALESCE(tts_provider,''), COALESCE(tts_voice_id,''), COALESCE(tts_language,''), org_id
		FROM campaigns WHERE id=?`, campaignID,
	).Scan(&provider, &voiceID, &lang, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return VoiceSettings{}, nil
	}
	if err != nil {
		return VoiceSettings{}, err
	}
	if provider.String != "" && voiceID.String != "" {
		return VoiceSettings{
			TTSProvider: provider.String,
			TTSVoiceID:  voiceID.String,
			TTSLanguage: coalesceStr(lang.String, "hi"),
		}, nil
	}
	return d.GetOrganizationVoiceSettings(orgID)
}

// SaveCampaignVoiceSettings updates the tts_* columns on a campaign.
func (d *DB) SaveCampaignVoiceSettings(campaignID int64, vs VoiceSettings) error {
	_, err := d.pool.Exec(
		`UPDATE campaigns SET tts_provider=?, tts_voice_id=?, tts_language=? WHERE id=?`,
		nullString(vs.TTSProvider), nullString(vs.TTSVoiceID), nullString(vs.TTSLanguage), campaignID)
	return err
}

func coalesceStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

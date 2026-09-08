package db

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// EnsureCampaignsTable creates the campaigns table if it doesn't exist and adds
// any columns that may be missing on legacy schemas.
func (d *DB) EnsureCampaignsTable() error {
	_, err := d.pool.Exec(`
		CREATE TABLE IF NOT EXISTS campaigns (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			org_id BIGINT NOT NULL,
			product_id BIGINT DEFAULT NULL,
			name VARCHAR(255) NOT NULL,
			status VARCHAR(50) DEFAULT 'active',
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
			tts_provider VARCHAR(50) DEFAULT NULL,
			tts_voice_id VARCHAR(255) DEFAULT NULL,
			tts_language VARCHAR(10) DEFAULT NULL,
			max_call_duration_seconds INT DEFAULT NULL,
			lead_source VARCHAR(100) DEFAULT NULL,
			channel VARCHAR(20) NOT NULL DEFAULT 'voice',
			exotel_account_id BIGINT DEFAULT NULL,
			INDEX idx_org_id (org_id),
			INDEX idx_product_id (product_id),
			FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE,
			FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	columns := []struct{ name, def string }{
		{"tts_provider", "VARCHAR(50) DEFAULT NULL"},
		{"tts_voice_id", "VARCHAR(255) DEFAULT NULL"},
		{"tts_language", "VARCHAR(10) DEFAULT NULL"},
		{"max_call_duration_seconds", "INT DEFAULT NULL"},
		{"lead_source", "VARCHAR(100) DEFAULT NULL"},
		{"channel", "VARCHAR(20) NOT NULL DEFAULT 'voice'"},
		{"exotel_account_id", "BIGINT DEFAULT NULL"},
	}
	for _, col := range columns {
		_, alterErr := d.pool.Exec(fmt.Sprintf("ALTER TABLE campaigns ADD COLUMN %s %s", col.name, col.def))
		if alterErr != nil && !strings.Contains(alterErr.Error(), "Duplicate column name") {
			return alterErr
		}
	}
	return nil
}

// Campaign mirrors the campaigns table (joined with products.name).
// Stats is populated by list endpoints (LEFT JOIN on campaign_leads) and left
// nil by single-campaign fetches that don't need it.
type Campaign struct {
	ID              int64          `json:"id"`
	OrgID           int64          `json:"org_id"`
	ProductID       int64          `json:"product_id"`
	Name            string         `json:"name"`
	Status          string         `json:"status"`
	TTSProvider     string         `json:"tts_provider"`
	TTSVoiceID      string         `json:"tts_voice_id"`
	TTSLanguage     string         `json:"tts_language"`
	LeadSource      string         `json:"lead_source"`
	Channel         string         `json:"channel"`
	ExotelAccountID int64          `json:"exotel_account_id"`
	ProductName     string         `json:"product_name"`
	CreatedAt       string         `json:"created_at"`
	Stats           *CampaignStats `json:"stats,omitempty"`
}

// COALESCE on product_id because campaigns can legitimately have a NULL
// product_id (the LEFT JOIN to products surfaces these rows now). Scanning
// NULL into a Go int64 fails with "converting NULL to int64 is unsupported",
// so we collapse NULL → 0 here. The frontend treats product_id=0 as "unset"
// the same way it would treat a missing FK.
const campaignCols = `c.id, c.org_id, COALESCE(c.product_id,0), c.name,
	COALESCE(c.status,'active'), COALESCE(c.tts_provider,''), COALESCE(c.tts_voice_id,''),
	COALESCE(c.tts_language,''), COALESCE(c.lead_source,''),
	COALESCE(c.channel,'voice'),
	COALESCE(c.exotel_account_id,0),
	COALESCE(p.name,''), DATE_FORMAT(c.created_at,'%Y-%m-%d %H:%i:%s')`

func scanCampaign(row interface{ Scan(...any) error }) (*Campaign, error) {
	c := &Campaign{}
	err := row.Scan(&c.ID, &c.OrgID, &c.ProductID, &c.Name, &c.Status,
		&c.TTSProvider, &c.TTSVoiceID, &c.TTSLanguage, &c.LeadSource,
		&c.Channel, &c.ExotelAccountID, &c.ProductName, &c.CreatedAt)
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

	// LEFT JOIN to products so campaigns whose product_id is NULL or points
	// at a deleted product still appear in the listing. INNER JOIN here used
	// to silently drop them, while the dashboard summary's COUNT(*) over
	// campaigns counted them — leading to "metric says 3 but only 2 cards
	// render" reports.
	rows, err := d.pool.Query(
		`SELECT `+campaignCols+`,
			COALESCE(s.total,0), COALESCE(s.called,0),
			COALESCE(s.qualified,0), COALESCE(s.appointments,0)
		FROM campaigns c
		LEFT JOIN products p ON c.product_id = p.id
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
			&c.Channel, &c.ExotelAccountID, &c.ProductName, &c.CreatedAt,
			&stats.Total, &stats.Called, &stats.Qualified, &stats.Appointments,
		); err != nil {
			return nil, err
		}
		c.Stats = &stats
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetCampaignsByIDs returns campaigns for a specific set of IDs, ordered
// newest first. Used by RBAC-scoped campaign listing for Agents/Team Leaders.
func (d *DB) GetCampaignsByIDs(ids []int64) ([]Campaign, error) {
	if len(ids) == 0 {
		return []Campaign{}, nil
	}
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

	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := d.pool.Query(
		`SELECT `+campaignCols+`,
			COALESCE(s.total,0), COALESCE(s.called,0),
			COALESCE(s.qualified,0), COALESCE(s.appointments,0)
		FROM campaigns c
		LEFT JOIN products p ON c.product_id = p.id
		LEFT JOIN (`+statsSub+`) s ON s.campaign_id = c.id
		WHERE c.id IN (`+placeholders+`)
		ORDER BY c.created_at DESC`, args...)
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
			&c.Channel, &c.ExotelAccountID, &c.ProductName, &c.CreatedAt,
			&stats.Total, &stats.Called, &stats.Qualified, &stats.Appointments,
		); err != nil {
			return nil, err
		}
		c.Stats = &stats
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetAllCampaigns is the super-admin variant of GetCampaignsByOrg: it returns
// campaigns across every org, newest first.
func (d *DB) GetAllCampaigns() ([]Campaign, error) {
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
		`SELECT ` + campaignCols + `,
			COALESCE(s.total,0), COALESCE(s.called,0),
			COALESCE(s.qualified,0), COALESCE(s.appointments,0)
		FROM campaigns c
		LEFT JOIN products p ON c.product_id = p.id
		LEFT JOIN (` + statsSub + `) s ON s.campaign_id = c.id
		ORDER BY c.created_at DESC`)
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
			&c.Channel, &c.ExotelAccountID, &c.ProductName, &c.CreatedAt,
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
// LEFT JOIN to products mirrors GetCampaignsByOrg — a campaign with a NULL
// or deleted product_id is still a valid row to fetch (e.g. the user is
// opening it to fix the broken product link).
func (d *DB) GetCampaignByID(id int64) (*Campaign, error) {
	row := d.pool.QueryRow(
		`SELECT `+campaignCols+` FROM campaigns c LEFT JOIN products p ON c.product_id=p.id WHERE c.id=?`, id)
	c, err := scanCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// CreateCampaign inserts a new campaign. Returns the new ID.
func (d *DB) CreateCampaign(orgID, productID int64, name, leadSource, channel string, exotelAccountID int64) (int64, error) {
	if channel == "" {
		channel = "voice"
	}
	res, err := d.pool.Exec(
		`INSERT INTO campaigns (org_id, product_id, name, lead_source, channel, exotel_account_id) VALUES (?,?,?,?,?,?)`,
		orgID, productID, name, nullString(leadSource), channel, nullInt64(exotelAccountID))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetCampaignExotelAccount updates which org-level Exotel account a campaign uses.
// Pass 0 to unlink (fall back to inline per-campaign credentials).
func (d *DB) SetCampaignExotelAccount(campaignID, accountID int64) error {
	_, err := d.pool.Exec(`UPDATE campaigns SET exotel_account_id=? WHERE id=?`,
		nullInt64(accountID), campaignID)
	return err
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

// GetActiveCampaignForLeadPhone returns the most recent whatsapp campaign that
// contains a lead matching the given phone number, within the given org. Used
// by the WA agent to pick a campaign-level product prompt instead of the
// channel-wide default. Returns nil when no matching campaign exists.
func (d *DB) GetActiveCampaignForLeadPhone(orgID int64, phone string) (*Campaign, error) {
	// Phone may be stored as 10 digits ("7795740488") while the inbound webhook
	// normalises it to 12 digits with country code ("917795740488"). Match on
	// the last 10 digits so both formats resolve to the same lead.
	suffix := phone
	if len(suffix) > 10 {
		suffix = suffix[len(suffix)-10:]
	}
	row := d.pool.QueryRow(`
		SELECT `+campaignCols+`
		FROM campaigns c
		LEFT JOIN products p ON c.product_id = p.id
		JOIN campaign_leads cl ON cl.campaign_id = c.id
		JOIN leads l ON l.id = cl.lead_id
		WHERE c.org_id = ? AND c.channel = 'whatsapp'
		  AND RIGHT(l.phone, 10) = ?
		ORDER BY cl.id DESC
		LIMIT 1`, orgID, suffix)
	c, err := scanCampaign(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
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

// GetCampaignLeadCount returns the number of leads currently linked to a campaign.
func (d *DB) GetCampaignLeadCount(campaignID int64) (int64, error) {
	var n int64
	err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM campaign_leads WHERE campaign_id=?`, campaignID,
	).Scan(&n)
	return n, err
}

// GetCampaignLeadIDs returns the set of lead IDs already linked to a campaign.
func (d *DB) GetCampaignLeadIDs(campaignID int64) (map[int64]bool, error) {
	rows, err := d.pool.Query(
		`SELECT lead_id FROM campaign_leads WHERE campaign_id=?`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// AddLeadsToCampaign bulk-inserts campaign_leads (IGNORE duplicates). Returns added count.
func (d *DB) AddLeadsToCampaign(campaignID int64, leadIDs []int64) (int, error) {
	if len(leadIDs) == 0 {
		return 0, nil
	}
	const batchSize = 1000
	var added int
	for i := 0; i < len(leadIDs); i += batchSize {
		end := i + batchSize
		if end > len(leadIDs) {
			end = len(leadIDs)
		}
		batch := leadIDs[i:end]
		placeholders := strings.Repeat("(?,?),", len(batch)-1) + "(?,?)"
		q := `INSERT IGNORE INTO campaign_leads (campaign_id, lead_id) VALUES ` + placeholders
		args := make([]any, 0, len(batch)*2)
		for _, lid := range batch {
			args = append(args, campaignID, lid)
		}
		res, err := d.pool.Exec(q, args...)
		if err != nil {
			return added, err
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
	TranscriptCount         int64  `json:"transcript_count"`
	RecordingCount          int64  `json:"recording_count"`
	DialAttempts            int64  `json:"dial_attempts"`
	NextScheduledAt         string `json:"next_scheduled_at,omitempty"`
	HasPendingScheduledCall bool   `json:"has_pending_scheduled_call"`
	ScheduledCallID         int64  `json:"scheduled_call_id"`
	ScheduledCallMode       string `json:"scheduled_call_mode,omitempty"`
	ScheduledCallNotes      string `json:"scheduled_call_notes,omitempty"`
}

// GetCampaignLeads returns all leads in a campaign with call stats.
func (d *DB) GetCampaignLeads(campaignID int64) ([]CampaignLead, error) {
	return d.GetCampaignLeadsFiltered(campaignID, nil)
}

// GetCampaignLeadsFiltered returns campaign leads optionally filtered by
// executive IDs. An empty or nil slice returns all leads.
// Deprecated: use GetCampaignLeadsPaginated for large campaigns.
func (d *DB) GetCampaignLeadsFiltered(campaignID int64, execIDs []int64) ([]CampaignLead, error) {
	return d.GetCampaignLeadsPaginated(CampaignLeadsFilter{CampaignID: campaignID, ExecIDs: execIDs}, 0, 0)
}

// CampaignLeadsFilter holds optional filters for campaign lead listings.
type CampaignLeadsFilter struct {
	CampaignID    int64
	ExecIDs       []int64
	Search        string
	ScheduledFrom string // ISO datetime or empty
	ScheduledTo   string // ISO datetime or empty
}

// GetCampaignLeadsPaginated returns one page of campaign leads with call stats.
// Use limit=0 to return all matching leads (backward compatibility).
func (d *DB) GetCampaignLeadsPaginated(filter CampaignLeadsFilter, limit, offset int64) ([]CampaignLead, error) {
	search := "%" + filter.Search + "%"
	q := `SELECT l.id, l.org_id, l.first_name, COALESCE(l.last_name,''), l.phone,
		COALESCE(l.source,''), COALESCE(l.status,'new'),
		COALESCE(l.follow_up_note,''), COALESCE(DATE_FORMAT(l.follow_up_at, '%Y-%m-%dT%H:%i:%sZ'),''),
		COALESCE(l.interest,''), COALESCE(l.company,''), COALESCE(l.external_id,''), COALESCE(l.crm_provider,''),
		COALESCE(cl2.executive_id,0),
		DATE_FORMAT(l.created_at, '%Y-%m-%dT%H:%i:%sZ'),
		COALESCE(ct.transcript_count, 0) AS transcript_count,
		COALESCE(ct.recording_count, 0) AS recording_count,
		COALESCE(ct.dial_attempts, 0) AS dial_attempts,
		DATE_FORMAT(pc.scheduled_at, '%Y-%m-%dT%H:%i:%sZ') AS next_scheduled_at,
		pc.scheduled_at IS NOT NULL AS has_pending_scheduled_call,
		COALESCE(pc.id, 0) AS scheduled_call_id,
		COALESCE(pc.mode, '') AS scheduled_call_mode,
		COALESCE(pc.notes, '') AS scheduled_call_notes
	 FROM campaign_leads cl2
	 JOIN leads l ON l.id = cl2.lead_id
	 LEFT JOIN (
		SELECT lead_id,
			COUNT(*) AS dial_attempts,
			COUNT(*) AS transcript_count,
			SUM(CASE WHEN recording_url IS NOT NULL AND recording_url != '' THEN 1 ELSE 0 END) AS recording_count
		FROM call_transcripts
		GROUP BY lead_id
	 ) ct ON ct.lead_id = l.id
	 LEFT JOIN (
		SELECT sc1.lead_id, sc1.id, sc1.scheduled_at, COALESCE(sc1.mode, 'manual') AS mode, COALESCE(sc1.notes, '') AS notes
		FROM scheduled_calls sc1
		JOIN (
			SELECT lead_id, MIN(scheduled_at) AS scheduled_at
			FROM scheduled_calls
			WHERE campaign_id = ? AND status = 'pending'
			  AND scheduled_at >= COALESCE(NULLIF(?, ''), scheduled_at)
			  AND scheduled_at <= COALESCE(NULLIF(?, ''), scheduled_at)
			GROUP BY lead_id
		) picked ON picked.lead_id = sc1.lead_id AND picked.scheduled_at = sc1.scheduled_at
		WHERE sc1.campaign_id = ? AND sc1.status = 'pending'
	 ) pc ON pc.lead_id = l.id
	 WHERE cl2.campaign_id = ?
	   AND (l.first_name LIKE ? OR l.last_name LIKE ? OR l.phone LIKE ? OR l.company LIKE ? OR l.source LIKE ?)`
	args := []any{filter.CampaignID, filter.ScheduledFrom, filter.ScheduledTo, filter.CampaignID, filter.CampaignID, search, search, search, search, search}
	if len(filter.ExecIDs) > 0 {
		placeholders := strings.Repeat("?,", len(filter.ExecIDs)-1) + "?"
		q += ` AND COALESCE(cl2.executive_id,0) IN (` + placeholders + `)`
		for _, id := range filter.ExecIDs {
			args = append(args, id)
		}
	}
	if filter.ScheduledFrom != "" || filter.ScheduledTo != "" {
		q += ` AND pc.scheduled_at IS NOT NULL`
	}
	q += ` ORDER BY l.created_at DESC, l.id DESC`
	if limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := d.pool.Query(q, args...)
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

// CountCampaignLeads returns the total number of leads in a campaign matching
// the provided filter.
func (d *DB) CountCampaignLeads(filter CampaignLeadsFilter) (int64, error) {
	search := "%" + filter.Search + "%"
	q := `SELECT COUNT(*) FROM campaign_leads cl
	 JOIN leads l ON l.id = cl.lead_id
	 LEFT JOIN (
		SELECT lead_id, MIN(scheduled_at) AS scheduled_at
		FROM scheduled_calls
		WHERE campaign_id = ? AND status = 'pending'
		  AND scheduled_at >= COALESCE(NULLIF(?, ''), scheduled_at)
		  AND scheduled_at <= COALESCE(NULLIF(?, ''), scheduled_at)
		GROUP BY lead_id
	 ) pc ON pc.lead_id = l.id
	 WHERE cl.campaign_id = ?
	   AND (l.first_name LIKE ? OR l.last_name LIKE ? OR l.phone LIKE ? OR l.company LIKE ? OR l.source LIKE ?)`
	args := []any{filter.CampaignID, filter.ScheduledFrom, filter.ScheduledTo, filter.CampaignID, search, search, search, search, search}
	if len(filter.ExecIDs) > 0 {
		placeholders := strings.Repeat("?,", len(filter.ExecIDs)-1) + "?"
		q += ` AND COALESCE(cl.executive_id,0) IN (` + placeholders + `)`
		for _, id := range filter.ExecIDs {
			args = append(args, id)
		}
	}
	if filter.ScheduledFrom != "" || filter.ScheduledTo != "" {
		q += ` AND pc.scheduled_at IS NOT NULL`
	}
	var n int64
	err := d.pool.QueryRow(q, args...).Scan(&n)
	return n, err
}

func campaignExecFilterClause(execIDs []int64, applyExecFilter bool) (string, []any) {
	if !applyExecFilter || len(execIDs) == 0 {
		return "", nil
	}
	placeholders := strings.Repeat("?,", len(execIDs)-1) + "?"
	args := make([]any, 0, len(execIDs))
	for _, id := range execIDs {
		args = append(args, id)
	}
	return `COALESCE(cl.executive_id,0) IN (` + placeholders + `)`, args
}

// CampaignLeadMatchesAccess reports whether a campaign-lead row exists in the
// org and matches the optional executive restriction.
func (d *DB) CampaignLeadMatchesAccess(orgID, campaignID, leadID int64, execIDs []int64, applyExecFilter bool) (bool, error) {
	q := `
		SELECT COUNT(*)
		FROM campaign_leads cl
		JOIN campaigns c ON c.id=cl.campaign_id
		WHERE c.org_id=? AND cl.campaign_id=? AND cl.lead_id=?`
	args := []any{orgID, campaignID, leadID}
	if c, a := campaignExecFilterClause(execIDs, applyExecFilter); c != "" {
		q += ` AND ` + c
		args = append(args, a...)
	}
	var n int64
	if err := d.pool.QueryRow(q, args...).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func scanCampaignLead(row interface{ Scan(...any) error }) (*CampaignLead, error) {
	cl := &CampaignLead{}
	var orgIDInt sql.NullInt64
	var executiveID sql.NullInt64
	var followUpNote, followUpAt, interest, company, extID, crmProvider sql.NullString
	var nextScheduled sql.NullString
	var hasPending sql.NullBool
	var scheduledCallID sql.NullInt64
	var scheduledCallMode, scheduledCallNotes sql.NullString
	err := row.Scan(
		&cl.ID, &orgIDInt, &cl.FirstName, &cl.LastName, &cl.Phone,
		&cl.Source, &cl.Status, &followUpNote, &followUpAt, &interest, &company, &extID, &crmProvider,
		&executiveID, &cl.CreatedAt,
		&cl.TranscriptCount, &cl.RecordingCount, &cl.DialAttempts,
		&nextScheduled, &hasPending, &scheduledCallID, &scheduledCallMode, &scheduledCallNotes,
	)
	if err != nil {
		return nil, err
	}
	if orgIDInt.Valid {
		cl.OrgID = orgIDInt.Int64
	}
	if executiveID.Valid {
		cl.ExecutiveID = executiveID.Int64
	}
	cl.FollowUpNote = followUpNote.String
	cl.FollowUpAt = followUpAt.String
	cl.Interest = interest.String
	cl.Company = company.String
	cl.ExternalID = extID.String
	cl.CRMProvider = crmProvider.String
	if nextScheduled.Valid {
		cl.NextScheduledAt = nextScheduled.String
	}
	if hasPending.Valid {
		cl.HasPendingScheduledCall = hasPending.Bool
	}
	if scheduledCallID.Valid {
		cl.ScheduledCallID = scheduledCallID.Int64
	}
	cl.ScheduledCallMode = scheduledCallMode.String
	cl.ScheduledCallNotes = scheduledCallNotes.String
	return cl, nil
}

// CampaignStats holds aggregate campaign metrics.
type CampaignStats struct {
	Total        int64 `json:"total"`
	Called       int64 `json:"called"`
	Qualified    int64 `json:"qualified"`
	Appointments int64 `json:"appointments"`
}

// CallOutcomeStats breaks down call results for a campaign.
type CallOutcomeStats struct {
	Total      int64 `json:"total"`
	Connected  int64 `json:"connected"`
	Completed  int64 `json:"completed"`
	Unanswered int64 `json:"unanswered"`
	Busy       int64 `json:"busy"`
	Failed     int64 `json:"failed"`
}

// GetCampaignCallOutcomeStats returns the count of calls by outcome for a campaign.
// Connected/completed calls are derived from transcript rows because that is
// where duration lives. Failed terminal outcomes are derived from call_logs too:
// providers can report busy/no-answer without ever producing a transcript row,
// and the live activity panel is fed by those call_log status callbacks.
// When applyExecFilter is true, only calls whose lead's executive_id is in execIDs are counted.
func (d *DB) GetCampaignCallOutcomeStats(campaignID int64, execIDs []int64, applyExecFilter bool) (CallOutcomeStats, error) {
	var s CallOutcomeStats
	q := `
		SELECT outcome, COUNT(*)
		FROM (
			SELECT
				CASE
					WHEN ct.call_duration_s>30 AND l.status IN ('Summarized','Closed') THEN 'completed'
					WHEN ct.call_duration_s>5 THEN 'connected'
					ELSE 'unanswered'
				END AS outcome
			FROM call_transcripts ct
			LEFT JOIN leads l ON ct.lead_id=l.id
			LEFT JOIN campaign_leads cl ON cl.campaign_id=ct.campaign_id AND cl.lead_id=ct.lead_id
			WHERE ct.campaign_id=?`
	args := []any{campaignID}
	if c, a := campaignExecFilterClause(execIDs, applyExecFilter); c != "" {
		q += ` AND ` + c
		args = append(args, a...)
	}
	q += `
			UNION ALL
			SELECT
				CASE
					WHEN cl.status='busy' THEN 'busy'
					WHEN cl.status IN ('no-answer','no_answer') THEN 'unanswered'
					WHEN cl.status IN ('failed','cancelled') THEN 'failed'
					ELSE ''
				END AS outcome
			FROM call_logs cl
			LEFT JOIN leads l ON cl.lead_id=l.id
			LEFT JOIN campaign_leads cxl ON cxl.campaign_id=cl.campaign_id AND cxl.lead_id=cl.lead_id
			WHERE cl.campaign_id=?
			  AND cl.status IN ('busy','no-answer','no_answer','failed','cancelled')`
	args = append(args, campaignID)
	if c, a := campaignExecFilterClause(execIDs, applyExecFilter); c != "" {
		q += ` AND ` + strings.ReplaceAll(c, "cl.executive_id", "cxl.executive_id")
		args = append(args, a...)
	}
	q += `
		) outcomes
		WHERE outcome != ''
		GROUP BY outcome`
	rows, err := d.pool.Query(q, args...)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var count int64
		if err := rows.Scan(&outcome, &count); err != nil {
			return s, err
		}
		s.Total += count
		switch outcome {
		case "completed":
			s.Completed += count
		case "connected":
			s.Connected += count
		case "busy":
			s.Busy += count
		case "failed":
			s.Failed += count
		default:
			s.Unanswered += count
		}
	}
	return s, rows.Err()
}

// GetCampaignCallOutcomeStatsForUser returns call outcomes for one campaign
// attributed to the user who placed each call.
func (d *DB) GetCampaignCallOutcomeStatsForUser(campaignID, userID int64) (CallOutcomeStats, error) {
	var s CallOutcomeStats
	rows, err := d.pool.Query(`
		SELECT outcome, COUNT(*)
		FROM (
			SELECT
				CASE
					WHEN JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.outcome'))='completed' THEN 'completed'
					WHEN JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.outcome'))='connected' THEN 'connected'
					WHEN JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.outcome')) IN ('unanswered','no_answer')
						AND cl.status IN ('completed','answered','connected') THEN 'connected'
					WHEN EXISTS (
						SELECT 1
						FROM call_transcripts ct
						WHERE ct.org_id = aa.org_id
						  AND ct.lead_id = COALESCE(cl.lead_id, aa.lead_id)
						  AND COALESCE(ct.campaign_id, 0) = COALESCE(cl.campaign_id, aa.campaign_id, 0)
						  AND ct.call_duration_s > 5
						  AND ABS(TIMESTAMPDIFF(SECOND, aa.created_at, ct.created_at)) <= 14400
					) THEN 'connected'
					WHEN JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.outcome'))='busy' OR cl.status='busy' THEN 'busy'
					WHEN JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.outcome')) IN ('failed','cancelled')
						OR cl.status IN ('failed','cancelled') THEN 'failed'
					WHEN JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.outcome')) IN ('unanswered','no_answer')
						OR cl.status IN ('no-answer','no_answer') THEN 'unanswered'
					ELSE ''
				END AS outcome
			FROM agent_activities aa
			LEFT JOIN call_logs cl ON cl.org_id=aa.org_id
				AND cl.call_sid = JSON_UNQUOTE(JSON_EXTRACT(aa.metadata,'$.call_sid')) COLLATE utf8mb4_0900_ai_ci
			WHERE aa.campaign_id=?
				AND aa.user_id=?
				AND aa.activity_type='call'
			UNION ALL
			SELECT
				CASE
					WHEN cl.status IN ('busy') THEN 'busy'
					WHEN cl.status IN ('failed','cancelled') THEN 'failed'
					WHEN cl.status IN ('no-answer','no_answer','unanswered') THEN 'unanswered'
					ELSE ''
				END AS outcome
			FROM call_logs cl
			JOIN users u ON u.id=?
			LEFT JOIN executives e ON e.org_id=cl.org_id AND LOWER(e.email)=LOWER(u.email)
			WHERE cl.campaign_id=?
				AND cl.status IN ('no-answer','no_answer','unanswered','busy','failed','cancelled')
				AND (
					EXISTS (
						SELECT 1
						FROM campaign_user_assignments cua
						WHERE cua.campaign_id=cl.campaign_id AND cua.user_id=?
					)
					OR (e.id IS NOT NULL AND EXISTS (
						SELECT 1
						FROM campaign_leads campaign_lead
						WHERE campaign_lead.campaign_id=cl.campaign_id
						  AND campaign_lead.lead_id=cl.lead_id
						  AND campaign_lead.executive_id=e.id
					))
				)
				AND NOT EXISTS (
					SELECT 1
					FROM agent_activities aa2
					WHERE aa2.org_id=cl.org_id
						AND aa2.activity_type='call'
						AND JSON_UNQUOTE(JSON_EXTRACT(aa2.metadata,'$.call_sid')) COLLATE utf8mb4_0900_ai_ci = cl.call_sid
				)
		) outcomes
		WHERE outcome != ''
		GROUP BY outcome`, campaignID, userID, userID, campaignID, userID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome string
		var count int64
		if err := rows.Scan(&outcome, &count); err != nil {
			return s, err
		}
		s.Total += count
		switch outcome {
		case "completed":
			s.Completed += count
		case "connected":
			s.Connected += count
		case "busy":
			s.Busy += count
		case "failed":
			s.Failed += count
		default:
			s.Unanswered += count
		}
	}
	return s, rows.Err()
}

// OrgDashboardSummary is the org-wide top-of-page card row for /crm. Visible
// to all authenticated roles (Admin / Agent / Executive) without exposing full
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
			COUNT(DISTINCT l.phone) AS total,
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

// GetAllDashboardSummary returns the same 5 dashboard numbers summed across
// every organization. Used for the global super-admin dashboard view.
func (d *DB) GetAllDashboardSummary() (OrgDashboardSummary, error) {
	var s OrgDashboardSummary
	if err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM campaigns WHERE status='active'`,
	).Scan(&s.Campaigns); err != nil {
		return s, err
	}
	err := d.pool.QueryRow(`
		SELECT
			COUNT(DISTINCT CONCAT(l.org_id, ':', l.phone)) AS total,
			COALESCE(SUM(CASE WHEN COALESCE(l.status,'new') != 'new' THEN 1 ELSE 0 END), 0) AS called,
			COALESCE(SUM(CASE WHEN l.status IN ('Warm','Summarized','Closed') THEN 1 ELSE 0 END), 0) AS qualified,
			COALESCE(SUM(CASE WHEN l.status IN ('Summarized','Closed') THEN 1 ELSE 0 END), 0) AS appointments
		FROM campaign_leads cl
		JOIN leads l ON l.id = cl.lead_id
		JOIN campaigns c ON c.id = cl.campaign_id`,
	).Scan(&s.TotalLeads, &s.Called, &s.Qualified, &s.Appointments)
	return s, err
}

// GetDashboardSummaryForCampaigns returns dashboard totals scoped to a set of
// visible campaigns and, optionally, a subset of visible executive IDs. This
// keeps the CRM dashboard in sync with RBAC-filtered campaign and lead access.
func (d *DB) GetDashboardSummaryForCampaigns(orgID int64, campaignIDs, execIDs []int64, applyExecFilter bool) (OrgDashboardSummary, error) {
	var s OrgDashboardSummary
	if len(campaignIDs) == 0 {
		return s, nil
	}

	campaignPlaceholders := strings.Repeat("?,", len(campaignIDs)-1) + "?"
	campaignArgs := make([]any, 0, 1+len(campaignIDs))
	campaignArgs = append(campaignArgs, orgID)
	for _, id := range campaignIDs {
		campaignArgs = append(campaignArgs, id)
	}

	if err := d.pool.QueryRow(
		`SELECT COUNT(*) FROM campaigns
		  WHERE org_id=? AND status='active' AND id IN (`+campaignPlaceholders+`)`,
		campaignArgs...,
	).Scan(&s.Campaigns); err != nil {
		return s, err
	}

	leadQuery := `
		SELECT
			COUNT(DISTINCT l.phone) AS total,
			COALESCE(SUM(CASE WHEN COALESCE(l.status,'new') != 'new' THEN 1 ELSE 0 END), 0) AS called,
			COALESCE(SUM(CASE WHEN l.status IN ('Warm','Summarized','Closed') THEN 1 ELSE 0 END), 0) AS qualified,
			COALESCE(SUM(CASE WHEN l.status IN ('Summarized','Closed') THEN 1 ELSE 0 END), 0) AS appointments
		FROM campaign_leads cl
		JOIN leads l ON l.id = cl.lead_id
		JOIN campaigns c ON c.id = cl.campaign_id
		WHERE c.org_id=? AND c.id IN (` + campaignPlaceholders + `)`
	leadArgs := append([]any{}, campaignArgs...)
	if clause, args := campaignExecFilterClause(execIDs, applyExecFilter); clause != "" {
		leadQuery += ` AND ` + clause
		leadArgs = append(leadArgs, args...)
	}

	err := d.pool.QueryRow(leadQuery, leadArgs...).Scan(&s.TotalLeads, &s.Called, &s.Qualified, &s.Appointments)
	return s, err
}

// GetDashboardSummaryForCampaignsByUser returns visible campaign/lead totals
// but attributes call counts to the user who actually placed the calls.
func (d *DB) GetDashboardSummaryForCampaignsByUser(orgID int64, campaignIDs, execIDs []int64, applyExecFilter bool, userID int64) (OrgDashboardSummary, error) {
	s, err := d.GetDashboardSummaryForCampaigns(orgID, campaignIDs, execIDs, applyExecFilter)
	if err != nil || len(campaignIDs) == 0 || userID <= 0 {
		return s, err
	}
	campaignPlaceholders := strings.Repeat("?,", len(campaignIDs)-1) + "?"
	args := make([]any, 0, 2+len(campaignIDs))
	args = append(args, orgID, userID)
	for _, id := range campaignIDs {
		args = append(args, id)
	}
	err = d.pool.QueryRow(`
		SELECT COUNT(DISTINCT aa.lead_id)
		FROM agent_activities aa
		WHERE aa.org_id=?
			AND aa.user_id=?
			AND aa.activity_type='call'
			AND aa.campaign_id IN (`+campaignPlaceholders+`)`, args...).Scan(&s.Called)
	return s, err
}

// GetCampaignStats returns 4 aggregate metrics for a campaign.
// When applyExecFilter is true, only leads whose executive_id is in execIDs are counted.
func (d *DB) GetCampaignStats(campaignID int64, execIDs []int64, applyExecFilter bool) (CampaignStats, error) {
	var s CampaignStats

	totalQ := `SELECT COUNT(*) FROM campaign_leads cl JOIN leads l ON l.id=cl.lead_id WHERE cl.campaign_id=?`
	totalArgs := []any{campaignID}
	if c, a := campaignExecFilterClause(execIDs, applyExecFilter); c != "" {
		totalQ += ` AND ` + c
		totalArgs = append(totalArgs, a...)
	}
	if err := d.pool.QueryRow(totalQ, totalArgs...).Scan(&s.Total); err != nil {
		return s, err
	}

	statusQ := `SELECT COUNT(*) FROM leads l JOIN campaign_leads cl ON l.id=cl.lead_id WHERE cl.campaign_id=?`
	statusArgs := []any{campaignID}
	if c, a := campaignExecFilterClause(execIDs, applyExecFilter); c != "" {
		statusQ += ` AND ` + c
		statusArgs = append(statusArgs, a...)
	}

	if err := d.pool.QueryRow(statusQ+` AND l.status NOT IN ('new')`, statusArgs...).Scan(&s.Called); err != nil {
		return s, err
	}
	if err := d.pool.QueryRow(statusQ+` AND l.status IN ('Warm','Summarized','Closed')`, append([]any{}, statusArgs...)).Scan(&s.Qualified); err != nil {
		return s, err
	}
	err := d.pool.QueryRow(statusQ+` AND l.status IN ('Summarized','Closed')`, append([]any{}, statusArgs...)).Scan(&s.Appointments)
	return s, err
}

// GetCampaignStatsForUser returns campaign lead totals scoped by normal lead
// visibility, while counting called leads only for the user who placed calls.
func (d *DB) GetCampaignStatsForUser(campaignID, userID int64, execIDs []int64, applyExecFilter bool) (CampaignStats, error) {
	s, err := d.GetCampaignStats(campaignID, execIDs, applyExecFilter)
	if err != nil || userID <= 0 {
		return s, err
	}
	err = d.pool.QueryRow(`
		SELECT COUNT(DISTINCT aa.lead_id)
		FROM agent_activities aa
		WHERE aa.campaign_id=?
			AND aa.user_id=?
			AND aa.activity_type='call'`, campaignID, userID).Scan(&s.Called)
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
// invisible in Call Log).
//
// LEFT JOIN to leads, not INNER. Sim Web Call and ad-hoc dials commonly
// produce transcripts with lead_id=NULL (no enrolled lead row), and the
// previous INNER JOIN silently dropped them — exact symptom in issue #72.
// Empty first_name/phone fallback keeps the response shape unchanged.
// GetCampaignCallLog returns the full call history for leads in a campaign.
// If execIDs is non-empty, only calls whose lead has one of the given
// executive_id values (or is unassigned if 0 is included) are returned.
func (d *DB) GetCampaignCallLog(campaignID int64, execIDs []int64) ([]CallLogEntry, error) {
	q := `
		SELECT
			ct.id,
			COALESCE(l.first_name,''), COALESCE(l.last_name,''),
			COALESCE(l.phone,''), COALESCE(l.source,''),
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
		LEFT JOIN leads l ON ct.lead_id=l.id
		LEFT JOIN campaign_leads cl ON cl.campaign_id=ct.campaign_id AND cl.lead_id=ct.lead_id
		WHERE ct.campaign_id=?`
	args := []any{campaignID}
	if c, a := campaignExecFilterClause(execIDs, len(execIDs) > 0); c != "" {
		q += ` AND ` + c
		args = append(args, a...)
	}
	q += ` ORDER BY ct.created_at DESC`
	rows, err := d.pool.Query(q, args...)
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

// GetCampaignCallLogForUser returns the campaign call log attributed to a
// single dashboard user. Manual/configured-account calls are matched through
// nearby transcript rows when the provider call log is incomplete.
func (d *DB) GetCampaignCallLogForUser(campaignID, userID int64) ([]CallLogEntry, error) {
	q := `
		SELECT DISTINCT
			ct.id,
			COALESCE(l.first_name,''), COALESCE(l.last_name,''),
			COALESCE(l.phone,''), COALESCE(l.source,''),
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
		JOIN agent_activities aa ON aa.org_id=ct.org_id
			AND aa.user_id=?
			AND aa.campaign_id=ct.campaign_id
			AND aa.activity_type='call'
			AND aa.lead_id=ct.lead_id
			AND ABS(TIMESTAMPDIFF(SECOND, aa.created_at, ct.created_at)) <= 14400
		LEFT JOIN leads l ON ct.lead_id=l.id
		WHERE ct.campaign_id=?
		ORDER BY ct.created_at DESC`
	rows, err := d.pool.Query(q, userID, campaignID)
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

// RecordingExportRow is one row for the recordings CSV export.
type RecordingExportRow struct {
	Name              string
	Phone             string
	LeadStatus        string
	CallType          string
	CreatedAt         string
	Duration          float64
	Outcome           string
	FollowUpNote      string
	RecordingFilename string
	RecordingURL      string
}

// GetCampaignRecordingsExport returns call transcript rows that have a
// recording URL, enriched with lead name, phone, follow-up note, and a
// derived call type (AI Dial / Manual). If execIDs is non-empty, only
// recordings for leads with one of the given executive_id values are returned.
func (d *DB) GetCampaignRecordingsExport(campaignID int64, execIDs []int64) ([]RecordingExportRow, error) {
	q := `
		SELECT
			TRIM(CONCAT(COALESCE(l.first_name,''), ' ', COALESCE(l.last_name,''))) AS name,
			COALESCE(l.phone,''),
			COALESCE(l.status, ''),
			COALESCE(ct.tts_language,''),
			COALESCE(ct.call_duration_s, 0),
			COALESCE(ct.recording_url, ''),
			DATE_FORMAT(ct.created_at,'%Y-%m-%d %H:%i:%s'),
			COALESCE(l.follow_up_note,''),
			CASE
				WHEN ct.call_duration_s>30 AND l.status IN ('Summarized','Closed') THEN 'Completed'
				WHEN ct.call_duration_s>5 THEN 'Connected'
				WHEN l.status LIKE 'Call Failed (busy)%' THEN 'Busy'
				WHEN l.status LIKE 'Call Failed (failed)%' THEN 'Failed'
				WHEN l.status LIKE 'DND%' THEN 'DND Blocked'
				ELSE 'No Answer'
			END AS outcome
		FROM call_transcripts ct
		LEFT JOIN leads l ON ct.lead_id = l.id
		LEFT JOIN campaign_leads cl ON cl.campaign_id=ct.campaign_id AND cl.lead_id=ct.lead_id
		WHERE ct.campaign_id = ? AND ct.recording_url IS NOT NULL AND ct.recording_url != ''`
	args := []any{campaignID}
	if c, a := campaignExecFilterClause(execIDs, len(execIDs) > 0); c != "" {
		q += ` AND ` + c
		args = append(args, a...)
	}
	q += ` ORDER BY ct.created_at DESC`
	rows, err := d.pool.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []RecordingExportRow
	for rows.Next() {
		var e RecordingExportRow
		var ttsLang string
		if err := rows.Scan(&e.Name, &e.Phone, &e.LeadStatus, &ttsLang, &e.Duration, &e.RecordingURL,
			&e.CreatedAt, &e.FollowUpNote, &e.Outcome); err != nil {
			return nil, err
		}
		if ttsLang != "" {
			e.CallType = "AI Dial"
		} else {
			e.CallType = "Manual / Bridge"
		}
		// Extract filename from URL
		parts := strings.Split(e.RecordingURL, "/")
		e.RecordingFilename = parts[len(parts)-1]
		list = append(list, e)
	}
	return list, rows.Err()
}

// GetCampaignVoiceSettings returns TTS settings, falling back to org defaults.
func (d *DB) GetCampaignVoiceSettings(campaignID int64) (VoiceSettings, error) {
	// Direct-dial paths can call this with campaignID=0 (no campaign context),
	// in which case there's no row to read — fall straight through to the
	// platform default. Without this the caller (dial.Initiator) ends up with
	// an all-empty VoiceSettings, writes empty strings to the Redis pending
	// call, and the WS handler then never starts STT (which it gates on
	// `sess.Language != ""` post-Redis hydration). The phone audibly rings,
	// the user says hello, no transcripts get recorded. Returning the
	// platform default keeps the call functional.
	if campaignID <= 0 {
		return VoiceSettings{
			TTSProvider:            DefaultTTSProvider,
			TTSVoiceID:             DefaultVoiceIDFor(DefaultTTSProvider),
			TTSLanguage:            DefaultTTSLanguage,
			MaxCallDurationSeconds: 0,
		}, nil
	}
	var orgID int64
	var provider, voiceID, lang sql.NullString
	var maxDuration sql.NullInt64
	err := d.pool.QueryRow(
		`SELECT COALESCE(tts_provider,''), COALESCE(tts_voice_id,''), COALESCE(tts_language,''), max_call_duration_seconds, org_id
			FROM campaigns WHERE id=?`, campaignID,
	).Scan(&provider, &voiceID, &lang, &maxDuration, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		// Same reasoning as the campaignID<=0 branch: a missing campaign row
		// must still produce a usable language so STT can start. Without this
		// fallback the dial succeeds but the transcript comes back empty.
		return VoiceSettings{
			TTSProvider:            DefaultTTSProvider,
			TTSVoiceID:             DefaultVoiceIDFor(DefaultTTSProvider),
			TTSLanguage:            DefaultTTSLanguage,
			MaxCallDurationSeconds: 0,
		}, nil
	}
	if err != nil {
		return VoiceSettings{}, err
	}
	base, _ := d.GetOrganizationVoiceSettings(orgID)
	if provider.String != "" && voiceID.String != "" {
		base.TTSProvider = provider.String
		base.TTSVoiceID = voiceID.String
		base.TTSLanguage = coalesceStr(lang.String, DefaultTTSLanguage)
	}
	if maxDuration.Valid {
		base.MaxCallDurationSeconds = clampCallDurationSeconds(int(maxDuration.Int64))
	}
	return base, nil
}

// ListCampaignLeadIDs returns the IDs of all leads assigned to a campaign.
func (d *DB) ListCampaignLeadIDs(campaignID int64) ([]int64, error) {
	rows, err := d.pool.Query(
		`SELECT lead_id FROM campaign_leads WHERE campaign_id=?`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SaveCampaignVoiceSettings updates the tts_* columns on a campaign.
func (d *DB) SaveCampaignVoiceSettings(campaignID int64, vs VoiceSettings) error {
	_, err := d.pool.Exec(
		`UPDATE campaigns SET tts_provider=?, tts_voice_id=?, tts_language=?, max_call_duration_seconds=? WHERE id=?`,
		nullString(vs.TTSProvider), nullString(vs.TTSVoiceID), nullString(vs.TTSLanguage),
		nullInt(vs.MaxCallDurationSeconds), campaignID)
	return err
}

func coalesceStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ExotelCreds holds per-campaign telephony credentials.
// Field mapping:
//
//	Exotel: APIKey=API Key, APIToken=API Token, AccountSID, CallerID=Caller ID, AppID=App ID
//	Twilio: APIKey=Auth Token, APIToken=API Key SID, APISecret=API Secret, AccountSID, CallerID=From Phone
//	Tata: APIKey=Bearer/API Token, CallerID=DID/Caller ID, AppID=Agent Number, Subdomain=Click-to-Call endpoint override
type ExotelCreds struct {
	AccountID  int64  `json:"-"`
	Provider   string // "exotel", "twilio", "tata"/"smartflo"/"tata_tele"; empty means exotel
	APIKey     string `json:"exotel_api_key"`
	APIToken   string `json:"exotel_api_token"`
	APISecret  string // Twilio only
	AccountSID string `json:"exotel_account_sid"`
	CallerID   string `json:"exotel_caller_id"`
	AppID      string `json:"exotel_app_id"`
	AppType    string `json:"exotel_app_type"`
	Direction  string `json:"direction"`
	Region     string `json:"exotel_region"`    // Exotel region: in, us, sg, etc.
	Subdomain  string `json:"exotel_subdomain"` // Exotel account subdomain override
}

// IsSet returns true when the minimum required fields for the provider are set.
func (e ExotelCreds) IsSet() bool {
	if e.Provider == "twilio" {
		return e.AccountSID != "" && e.APIKey != "" && e.CallerID != ""
	}
	if e.Provider == "tata" || e.Provider == "smartflo" || e.Provider == "tata_tele" {
		if e.Direction == "inbound" {
			return e.APIKey != "" && e.CallerID != ""
		}
		return e.APIKey != "" && e.CallerID != "" && e.AppID != ""
	}
	// exotel: AppID is required for voice app routing
	return e.APIKey != "" && e.APIToken != "" && e.AccountSID != "" && e.CallerID != "" && e.AppID != ""
}

// GetCampaignExotelCreds returns the Exotel credentials for a campaign.
// It ONLY uses the org_exotel_accounts row linked via exotel_account_id.
// Inline per-campaign columns and platform env-var defaults are intentionally
// ignored so calls are routed strictly through the account selected in the UI.
func (d *DB) GetCampaignExotelCreds(campaignID int64) (ExotelCreds, error) {
	var c ExotelCreds
	var accountID sql.NullInt64
	err := d.pool.QueryRow(
		`SELECT COALESCE(exotel_account_id,0) FROM campaigns WHERE id=?`, campaignID,
	).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if accountID.Valid && accountID.Int64 > 0 {
		var orgID int64
		_ = d.pool.QueryRow(`SELECT org_id FROM campaigns WHERE id=?`, campaignID).Scan(&orgID)
		if linked, lerr := d.GetOrgExotelAccountCreds(accountID.Int64, orgID); lerr == nil {
			return linked, nil
		}
	}
	return c, nil
}

// SaveCampaignExotelCreds persists Exotel credentials on a campaign.
// Pass empty strings to clear individual fields.
func (d *DB) SaveCampaignExotelCreds(campaignID int64, creds ExotelCreds) error {
	_, err := d.pool.Exec(
		`UPDATE campaigns
		 SET exotel_api_key=?, exotel_api_token=?, exotel_account_sid=?,
		     exotel_caller_id=?, exotel_app_id=?
		 WHERE id=?`,
		nullString(creds.APIKey), nullString(creds.APIToken),
		nullString(creds.AccountSID), nullString(creds.CallerID),
		nullString(creds.AppID), campaignID)
	return err
}

// ExportCampaignLeads writes leads in the campaign as CSV to w.
// Rows are streamed from the database so memory stays flat even for 100k+ leads.
// If execIDs is non-empty, only leads with one of the given executive_id
// values (or unassigned if 0 is included) are exported.
func (d *DB) ExportCampaignLeads(campaignID int64, execIDs []int64, w io.Writer) error {
	wr := csv.NewWriter(w)
	header := []string{
		"Lead ID", "First Name", "Last Name", "Phone", "Source",
		"Status", "Follow-up Note", "Dial Attempts", "Recordings", "Next Scheduled At",
	}
	if err := wr.Write(header); err != nil {
		return err
	}
	wr.Flush()
	if err := wr.Error(); err != nil {
		return err
	}

	q := `
		SELECT
			l.id,
			COALESCE(l.first_name, ''),
			COALESCE(l.last_name, ''),
			l.phone,
			COALESCE(l.source, ''),
			COALESCE(l.status, 'new'),
			COALESCE(l.follow_up_note, ''),
			COALESCE(ct.dial_attempts, 0),
			COALESCE(ct.recording_count, 0),
			COALESCE(DATE_FORMAT(pc.scheduled_at, '%Y-%m-%d %H:%i:%s'), '')
		FROM campaign_leads cl
		JOIN leads l ON l.id = cl.lead_id
		LEFT JOIN (
			SELECT lead_id,
				COUNT(*) AS dial_attempts,
				SUM(CASE WHEN recording_url IS NOT NULL AND recording_url != '' THEN 1 ELSE 0 END) AS recording_count
			FROM call_transcripts
			WHERE campaign_id = ?
			GROUP BY lead_id
		) ct ON ct.lead_id = l.id
		LEFT JOIN (
			SELECT lead_id, MIN(scheduled_at) AS scheduled_at
			FROM scheduled_calls
			WHERE campaign_id = ? AND status = 'pending'
			GROUP BY lead_id
		) pc ON pc.lead_id = l.id
		WHERE cl.campaign_id = ?`
	args := []any{campaignID, campaignID, campaignID}
	if len(execIDs) > 0 {
		if c, a := campaignExecFilterClause(execIDs, true); c != "" {
			q += ` AND ` + c
			args = append(args, a...)
		}
	}
	q += ` ORDER BY l.created_at DESC, l.id DESC`
	rows, err := d.pool.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	const flushEvery = 100
	rowCount := 0
	for rows.Next() {
		var id, dialAttempts, recordingCount int64
		var firstName, lastName, phone, source, status, followUp, scheduledAt string
		if err := rows.Scan(&id, &firstName, &lastName, &phone, &source, &status, &followUp,
			&dialAttempts, &recordingCount, &scheduledAt); err != nil {
			return err
		}
		if err := wr.Write([]string{
			strconv.FormatInt(id, 10),
			firstName,
			lastName,
			phone,
			source,
			status,
			followUp,
			strconv.FormatInt(dialAttempts, 10),
			strconv.FormatInt(recordingCount, 10),
			scheduledAt,
		}); err != nil {
			return err
		}
		rowCount++
		if rowCount%flushEvery == 0 {
			wr.Flush()
			if err := wr.Error(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	wr.Flush()
	return wr.Error()
}

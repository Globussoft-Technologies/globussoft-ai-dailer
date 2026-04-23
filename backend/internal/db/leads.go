package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Lead mirrors the leads table.
type Lead struct {
	ID           int64   `json:"id"`
	OrgID        int64   `json:"org_id"`
	FirstName    string  `json:"first_name"`
	LastName     string  `json:"last_name"`
	Phone        string  `json:"phone"`
	Source       string  `json:"source"`
	Status       string  `json:"status"`
	FollowUpNote string  `json:"follow_up_note"`
	Interest     string  `json:"interest"`
	ExternalID   string  `json:"external_id"`
	CRMProvider  string  `json:"crm_provider"`
	CreatedAt    string  `json:"created_at"`
}

func scanLead(row interface{ Scan(...any) error }) (*Lead, error) {
	l := &Lead{}
	var orgID, followUpNote, interest, extID, crmProvider sql.NullString
	var orgIDInt sql.NullInt64
	err := row.Scan(
		&l.ID, &orgIDInt, &l.FirstName, &l.LastName, &l.Phone,
		&l.Source, &l.Status, &followUpNote, &interest, &extID, &crmProvider,
		&l.CreatedAt,
	)
	_ = orgID
	if err != nil {
		return nil, err
	}
	if orgIDInt.Valid {
		l.OrgID = orgIDInt.Int64
	}
	l.FollowUpNote = followUpNote.String
	l.Interest = interest.String
	l.ExternalID = extID.String
	l.CRMProvider = crmProvider.String
	return l, nil
}

const leadCols = `id, org_id, first_name, COALESCE(last_name,''), phone,
	COALESCE(source,''), COALESCE(status,'new'), COALESCE(follow_up_note,''),
	COALESCE(interest,''), COALESCE(external_id,''), COALESCE(crm_provider,''),
	DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s')`

// leadColsL is leadCols prefixed with table alias "l" for use in JOIN queries.
const leadColsL = `l.id, l.org_id, l.first_name, COALESCE(l.last_name,''), l.phone,
	COALESCE(l.source,''), COALESCE(l.status,'new'), COALESCE(l.follow_up_note,''),
	COALESCE(l.interest,''), COALESCE(l.external_id,''), COALESCE(l.crm_provider,''),
	DATE_FORMAT(l.created_at, '%Y-%m-%d %H:%i:%s')`

// GetAllLeads returns all leads for the given org (or all orgs if orgID == 0).
func (d *DB) GetAllLeads(orgID int64) ([]Lead, error) {
	q := `SELECT ` + leadCols + ` FROM leads`
	var args []any
	if orgID != 0 {
		q += ` WHERE org_id = ?`
		args = append(args, orgID)
	}
	q += ` ORDER BY created_at DESC`
	return queryLeads(d.pool, q, args...)
}

// SearchLeads full-text searches by name/phone in the given org.
func (d *DB) SearchLeads(query string, orgID int64) ([]Lead, error) {
	like := "%" + query + "%"
	q := `SELECT ` + leadCols + ` FROM leads WHERE (first_name LIKE ? OR last_name LIKE ? OR phone LIKE ?)`
	args := []any{like, like, like}
	if orgID != 0 {
		q += ` AND org_id = ?`
		args = append(args, orgID)
	}
	q += ` ORDER BY created_at DESC LIMIT 100`
	return queryLeads(d.pool, q, args...)
}

// GetLeadByID fetches one lead. Returns nil when not found.
func (d *DB) GetLeadByID(id int64) (*Lead, error) {
	row := d.pool.QueryRow(`SELECT `+leadCols+` FROM leads WHERE id = ?`, id)
	l, err := scanLead(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

// CreateLead inserts a new lead. Returns the new ID.
func (d *DB) CreateLead(firstName, lastName, phone, source, interest string, orgID int64) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO leads (org_id, first_name, last_name, phone, source, interest)
		 VALUES (?,?,?,?,?,?)`,
		nullInt64(orgID), firstName, lastName, phone, source, nullString(interest),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateLead updates mutable lead fields. Returns true if a row was changed.
func (d *DB) UpdateLead(id int64, firstName, lastName, phone, source, interest string, orgID int64) (bool, error) {
	res, err := d.pool.Exec(
		`UPDATE leads SET first_name=?, last_name=?, phone=?, source=?, interest=?
		 WHERE id=? AND (org_id=? OR org_id IS NULL)`,
		firstName, lastName, phone, source, nullString(interest), id, orgID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteLead deletes a lead scoped to the given org. Returns true if deleted.
func (d *DB) DeleteLead(id, orgID int64) (bool, error) {
	res, err := d.pool.Exec(
		`DELETE FROM leads WHERE id=? AND (org_id=? OR org_id IS NULL)`, id, orgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateLeadStatus sets the status column.
func (d *DB) UpdateLeadStatus(id int64, status string) error {
	_, err := d.pool.Exec(`UPDATE leads SET status=? WHERE id=?`, status, id)
	return err
}

// UpdateLeadNote sets the follow_up_note column.
func (d *DB) UpdateLeadNote(id int64, note string) error {
	_, err := d.pool.Exec(`UPDATE leads SET follow_up_note=? WHERE id=?`, note, id)
	return err
}

// BulkCreateLeads inserts multiple leads, skipping duplicates. Returns (imported, errors).
func (d *DB) BulkCreateLeads(rows []LeadImportRow, orgID int64) (int, []string) {
	var imported int
	var errs []string
	for i, r := range rows {
		_, err := d.CreateLead(r.FirstName, r.LastName, r.Phone, r.Source, "", orgID)
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "Duplicate") || strings.Contains(msg, "1062") {
				msg = "duplicate phone"
			}
			errs = append(errs, fmt.Sprintf("Row %d: %s", i+2, msg[:min(len(msg), 50)]))
		} else {
			imported++
		}
	}
	return imported, errs
}

// LeadImportRow holds one CSV row for bulk import.
type LeadImportRow struct {
	FirstName string
	LastName  string
	Phone     string
	Source    string
}

// Document mirrors the documents table.
type Document struct {
	ID         int64  `json:"id"`
	LeadID     int64  `json:"lead_id"`
	FileName   string `json:"file_name"`
	FileURL    string `json:"file_url"`
	UploadedAt string `json:"uploaded_at"`
}

// GetDocumentsByLead returns all documents for a lead.
func (d *DB) GetDocumentsByLead(leadID int64) ([]Document, error) {
	rows, err := d.pool.Query(
		`SELECT id, lead_id, file_name, file_url, DATE_FORMAT(uploaded_at,'%Y-%m-%d %H:%i:%s')
		 FROM documents WHERE lead_id=? ORDER BY uploaded_at DESC`, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []Document
	for rows.Next() {
		var doc Document
		if err := rows.Scan(&doc.ID, &doc.LeadID, &doc.FileName, &doc.FileURL, &doc.UploadedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

// CreateDocument inserts a document record.
func (d *DB) CreateDocument(leadID int64, fileName, fileURL string) error {
	_, err := d.pool.Exec(
		`INSERT INTO documents (lead_id, file_name, file_url) VALUES (?,?,?)`,
		leadID, fileName, fileURL)
	return err
}

// Transcript mirrors call_transcripts.
type Transcript struct {
	ID            int64   `json:"id"`
	LeadID        int64   `json:"lead_id"`
	CampaignID    int64   `json:"campaign_id"`
	Transcript    string  `json:"transcript"`
	RecordingURL  string  `json:"recording_url"`
	CallDurationS float64 `json:"call_duration_s"`
	CreatedAt     string  `json:"created_at"`
}

// GetTranscriptsByLead returns all transcripts for a lead.
func (d *DB) GetTranscriptsByLead(leadID int64) ([]Transcript, error) {
	rows, err := d.pool.Query(
		`SELECT id, COALESCE(lead_id,0), COALESCE(campaign_id,0),
		        COALESCE(transcript,'[]'), COALESCE(recording_url,''),
		        COALESCE(call_duration_s,0),
		        DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		 FROM call_transcripts WHERE lead_id=? ORDER BY created_at DESC`, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Transcript
	for rows.Next() {
		var t Transcript
		if err := rows.Scan(&t.ID, &t.LeadID, &t.CampaignID, &t.Transcript, &t.RecordingURL, &t.CallDurationS, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// GetRecentCallTimeline returns the most recent call transcripts for an org (across all leads).
func (d *DB) GetRecentCallTimeline(orgID int64, limit int) ([]Transcript, error) {
	rows, err := d.pool.Query(`
		SELECT ct.id, COALESCE(ct.lead_id,0), COALESCE(ct.campaign_id,0),
		COALESCE(ct.transcript,'[]'), COALESCE(ct.recording_url,''),
		COALESCE(ct.call_duration_s,0),
		DATE_FORMAT(ct.created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_transcripts ct
		JOIN leads l ON ct.lead_id=l.id
		WHERE l.org_id=?
		ORDER BY ct.created_at DESC LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Transcript
	for rows.Next() {
		var t Transcript
		if err := rows.Scan(&t.ID, &t.LeadID, &t.CampaignID, &t.Transcript,
			&t.RecordingURL, &t.CallDurationS, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// SaveCallTranscript inserts a call transcript row and returns the new ID.
// transcriptJSON should be a JSON array of {role,text} objects.
func (d *DB) SaveCallTranscript(leadID, campaignID int64, transcriptJSON, recordingURL string, durationS float32) (int64, error) {
	res, err := d.pool.Exec(
		`INSERT INTO call_transcripts (lead_id, campaign_id, transcript, recording_url, call_duration_s)
		 VALUES (?,?,?,?,?)`,
		nullInt64(leadID), nullInt64(campaignID), transcriptJSON, nullString(recordingURL), durationS)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCallTranscriptRecording updates the recording URL on an existing transcript.
func (d *DB) UpdateCallTranscriptRecording(transcriptID int64, recordingURL string) error {
	_, err := d.pool.Exec(`UPDATE call_transcripts SET recording_url=? WHERE id=?`, recordingURL, transcriptID)
	return err
}

// helpers

func queryLeads(pool *sql.DB, q string, args ...any) ([]Lead, error) {
	rows, err := pool.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var leads []Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		leads = append(leads, *l)
	}
	return leads, rows.Err()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ensure time import is used
var _ = time.Now

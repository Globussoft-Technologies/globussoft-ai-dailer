package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// WAChannelConfig is a row from wa_channel_config.
type WAChannelConfig struct {
	ID               int64             `json:"id"`
	OrgID            int64             `json:"org_id"`
	Provider         string            `json:"provider"`
	PhoneNumber      string            `json:"phone_number"`
	Credentials      map[string]string `json:"credentials"`
	DefaultProductID int64             `json:"default_product_id"`
	IsActive         bool              `json:"is_active"`
	AutoReplyEnabled bool              `json:"auto_reply_enabled"`
	WelcomeTemplate  string            `json:"welcome_template"`
}

// WABlastJob is a row from wa_blast_jobs.
type WABlastJob struct {
	ID         int64    `json:"id"`
	CampaignID int64    `json:"campaign_id"`
	OrgID      int64    `json:"org_id"`
	Status     string   `json:"status"`
	Total      int      `json:"total"`
	Sent       int      `json:"sent"`
	Failed     int      `json:"failed"`
	Errors     []string `json:"errors"`
}

// GetActiveWAConfig returns the first active wa_channel_config for an org.
// Returns nil when none is configured.
func (d *DB) GetActiveWAConfig(orgID int64) (*WAChannelConfig, error) {
	row := d.pool.QueryRow(`
		SELECT id, org_id, provider, phone_number,
		       COALESCE(credentials,'{}'),
		       COALESCE(default_product_id,0),
		       COALESCE(is_active,0), COALESCE(auto_reply_enabled,0),
		       COALESCE(welcome_template,'')
		FROM wa_channel_config
		WHERE org_id=? AND is_active=1
		ORDER BY id ASC
		LIMIT 1`, orgID)

	cfg := &WAChannelConfig{}
	var credsJSON string
	err := row.Scan(
		&cfg.ID, &cfg.OrgID, &cfg.Provider, &cfg.PhoneNumber,
		&credsJSON, &cfg.DefaultProductID,
		&cfg.IsActive, &cfg.AutoReplyEnabled,
		&cfg.WelcomeTemplate,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(credsJSON), &cfg.Credentials); err != nil {
		cfg.Credentials = map[string]string{}
	}
	return cfg, nil
}

// SaveWAConversationMessage inserts an outbound AI message into wa_conversations.
// Returns the new message ID.
func (d *DB) SaveWAConversationMessage(orgID, configID, leadID int64, phone, name, content string) (int64, error) {
	var leadIDVal interface{} = nil
	if leadID > 0 {
		leadIDVal = leadID
	}
	res, err := d.pool.Exec(`
		INSERT INTO wa_conversations
		  (org_id, channel_config_id, lead_id, contact_phone, contact_name,
		   direction, message_type, content, is_ai_generated, ai_model, status)
		VALUES (?,?,?,?,?, 'outbound','text',?, 1,'gemini','sent')`,
		orgID, configID, leadIDVal, phone, name, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// HasRecentOutbound checks whether an outbound WA message was sent to this
// phone number in the last 24 hours (prevents duplicate blast contacts).
func (d *DB) HasRecentOutbound(orgID int64, phone string) (bool, error) {
	var count int
	err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM wa_conversations
		WHERE org_id=? AND contact_phone=? AND direction='outbound'
		  AND created_at >= NOW() - INTERVAL 24 HOUR`,
		orgID, phone,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateBlastJob inserts a new wa_blast_jobs row and returns its ID.
func (d *DB) CreateBlastJob(campaignID, orgID int64, total int) (int64, error) {
	res, err := d.pool.Exec(`
		INSERT INTO wa_blast_jobs (campaign_id, org_id, status, total, sent, failed)
		VALUES (?,?,'running',?,0,0)`,
		campaignID, orgID, total)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// HasRunningBlastJob returns true if there is already a running blast for this campaign.
func (d *DB) HasRunningBlastJob(campaignID int64) (bool, error) {
	var count int
	err := d.pool.QueryRow(`
		SELECT COUNT(*) FROM wa_blast_jobs
		WHERE campaign_id=? AND status='running'`, campaignID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// IncrBlastJobSent atomically increments the sent counter.
func (d *DB) IncrBlastJobSent(jobID int64) error {
	_, err := d.pool.Exec(`UPDATE wa_blast_jobs SET sent=sent+1 WHERE id=?`, jobID)
	return err
}

// IncrBlastJobFailed atomically increments the failed counter and appends errMsg.
func (d *DB) IncrBlastJobFailed(jobID int64, errMsg string) error {
	// Append the error to the JSON array stored in errors_json.
	_, err := d.pool.Exec(`
		UPDATE wa_blast_jobs
		SET failed=failed+1,
		    errors_json=JSON_ARRAY_APPEND(COALESCE(errors_json,'[]'), '$', ?)
		WHERE id=?`, errMsg, jobID)
	return err
}

// FinishBlastJob marks a job as done.
func (d *DB) FinishBlastJob(jobID int64) error {
	_, err := d.pool.Exec(`UPDATE wa_blast_jobs SET status='done' WHERE id=?`, jobID)
	return err
}

// GetBlastJob fetches one blast job by ID.
func (d *DB) GetBlastJob(jobID int64) (*WABlastJob, error) {
	var errorsJSON sql.NullString
	job := &WABlastJob{}
	err := d.pool.QueryRow(`
		SELECT id, campaign_id, org_id, status, total, sent, failed, errors_json
		FROM wa_blast_jobs WHERE id=?`, jobID).Scan(
		&job.ID, &job.CampaignID, &job.OrgID, &job.Status,
		&job.Total, &job.Sent, &job.Failed, &errorsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if errorsJSON.Valid && errorsJSON.String != "" && errorsJSON.String != "null" {
		_ = json.Unmarshal([]byte(errorsJSON.String), &job.Errors)
	}
	if job.Errors == nil {
		job.Errors = []string{}
	}
	return job, nil
}

// GetProductName fetches a product's name by ID (used for greeting text).
func (d *DB) GetProductName(productID int64) (string, error) {
	var name string
	err := d.pool.QueryRow(`SELECT name FROM products WHERE id=?`, productID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// ─── Phone normalization ─────────────────────────────────────────────────────

// NormalizePhone ensures E.164 format for WhatsApp (e.g. "+919876543210").
// defaultCC is the 2-digit country code string without "+", e.g. "91".
// Returns (normalized, ok). ok=false means the number is too short to use.
func NormalizePhone(phone, defaultCC string) (string, bool) {
	// Strip whitespace, dashes, parentheses
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, phone)

	if cleaned == "" || len(cleaned) < 7 {
		return "", false
	}
	if strings.HasPrefix(cleaned, "+") {
		return cleaned, true
	}
	// If it's 10 digits (common Indian mobile), prepend country code
	if len(cleaned) == 10 && defaultCC != "" {
		return "+" + defaultCC + cleaned, true
	}
	// If it's already 12 digits and starts with country code digits, prepend +
	if len(cleaned) >= 10 && defaultCC != "" && strings.HasPrefix(cleaned, defaultCC) {
		return "+" + cleaned, true
	}
	// Generic: prepend + and country code
	if defaultCC != "" {
		return "+" + defaultCC + cleaned, true
	}
	return "+" + cleaned, true
}

// countryCodeFromPhone extracts the leading digits to guess country code
// from the business phone number (e.g. "+919876543210" → "91").
func CountryCodeFromPhone(businessPhone string) string {
	cleaned := strings.TrimPrefix(businessPhone, "+")
	if len(cleaned) >= 2 {
		// Common 2-digit prefixes: 91 (IN), 1 (US), 44 (UK), 971 (UAE)
		// Heuristic: if starts with 91 and is 12 digits total → "91"
		if len(cleaned) == 12 && strings.HasPrefix(cleaned, "91") {
			return "91"
		}
		if len(cleaned) == 11 && strings.HasPrefix(cleaned, "1") {
			return "1"
		}
	}
	return "" // unknown — NormalizePhone will prepend + only
}

// BuildGreeting produces a personalised opening message for blast.
// Uses welcome_template name as-is when provided; otherwise returns free-form text.
func BuildGreeting(leadName, productName string) string {
	firstName := strings.Fields(leadName)[0]
	if productName == "" {
		return fmt.Sprintf("Hi %s, I wanted to reach out to you. How can I help?", firstName)
	}
	return fmt.Sprintf(
		"Hi %s, I'm reaching out regarding %s. How can I help you today?",
		firstName, productName,
	)
}

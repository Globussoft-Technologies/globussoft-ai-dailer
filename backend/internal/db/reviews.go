package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

type schemaExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// EnsureCallReviewColumns applies the schema needed for cross-call memory.
// It mirrors the standalone migration so normal deployments do not depend on
// a separate manual migration step.
func (d *DB) EnsureCallReviewColumns() error {
	return ensureCallReviewColumns(d.pool)
}

func ensureCallReviewColumns(exec schemaExecutor) error {
	if _, err := exec.Exec(`ALTER TABLE call_reviews ADD COLUMN lead_id INT DEFAULT NULL`); err != nil && !isMySQLError(err, 1060) {
		return fmt.Errorf("add call_reviews.lead_id: %w", err)
	}

	if _, err := exec.Exec(`
		UPDATE call_reviews cr
		JOIN call_transcripts ct ON cr.transcript_id = ct.id
		SET cr.lead_id = ct.lead_id
		WHERE cr.lead_id IS NULL AND ct.lead_id IS NOT NULL`); err != nil {
		return fmt.Errorf("backfill call_reviews.lead_id: %w", err)
	}

	if _, err := exec.Exec(`ALTER TABLE call_reviews ADD INDEX idx_call_reviews_lead_id (lead_id)`); err != nil && !isMySQLError(err, 1061) {
		return fmt.Errorf("add call_reviews lead index: %w", err)
	}
	return nil
}

func isMySQLError(err error, number uint16) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == number
}

// CallReview mirrors the call_reviews table.
type CallReview struct {
	ID                          int64   `json:"id"`
	TranscriptID                int64   `json:"transcript_id"`
	OrgID                       int64   `json:"org_id"`
	LeadID                      int64   `json:"lead_id"`
	QualityScore                float64 `json:"quality_score"`
	Sentiment                   string  `json:"sentiment"`
	AppointmentBooked           bool    `json:"appointment_booked"`
	FailureReason               string  `json:"failure_reason"`
	WhatWentWell                string  `json:"what_went_well"`
	WhatWentWrong               string  `json:"what_went_wrong"`
	Summary                     string  `json:"summary"`
	Insights                    string  `json:"insights"`
	PromptImprovementSuggestion string  `json:"prompt_improvement_suggestion"`
	CreatedAt                   string  `json:"created_at"`
}

// CallReviewWithLead enriches a call_reviews row with lead name info and
// surfaces both the legacy (sentiment/insights) and the newer
// (customer_sentiment/what_went_well/...) column names. The Insights tab in
// the frontend reads the newer field names; older Go-written rows only have
// the legacy columns populated, so the SQL COALESCEs keep both shapes
// addressable. Issue #75.
type CallReviewWithLead struct {
	ID                          int64  `json:"id"`
	TranscriptID                int64  `json:"transcript_id"`
	OrgID                       int64  `json:"org_id"`
	LeadID                      int64  `json:"lead_id"`
	FirstName                   string `json:"first_name"`
	LastName                    string `json:"last_name"`
	QualityScore                int    `json:"quality_score"`
	AppointmentBooked           bool   `json:"appointment_booked"`
	CustomerSentiment           string `json:"customer_sentiment"`
	FailureReason               string `json:"failure_reason"`
	WhatWentWell                string `json:"what_went_well"`
	WhatWentWrong               string `json:"what_went_wrong"`
	PromptImprovementSuggestion string `json:"prompt_improvement_suggestion"`
	CreatedAt                   string `json:"created_at"`
}

// GetCallReviewsByCampaign returns reviews for transcripts in a campaign,
// joined to leads for first_name/last_name and COALESCEd across the legacy
// (sentiment/insights/summary) and current (customer_sentiment/what_went_*) column
// pairs so the Call Insights tab renders for both old and new rows. Issue #75.
func (d *DB) GetCallReviewsByCampaign(campaignID int64, execIDs []int64, applyExecFilter bool) ([]CallReviewWithLead, error) {
	q := `
		SELECT r.id, r.transcript_id, COALESCE(r.org_id,0),
		       COALESCE(r.lead_id, t.lead_id, 0),
		       COALESCE(l.first_name,''), COALESCE(l.last_name,''),
		       COALESCE(r.quality_score,0),
		       COALESCE(r.appointment_booked,0),
		       COALESCE(NULLIF(r.sentiment,''), 'neutral'),
		       COALESCE(r.failure_reason,''),
		       COALESCE(NULLIF(r.what_went_well,''), NULLIF(r.summary,''), ''),
		       COALESCE(r.what_went_wrong,''),
		       COALESCE(NULLIF(r.prompt_improvement_suggestion,''), NULLIF(r.insights,''), ''),
		       DATE_FORMAT(r.created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		LEFT JOIN leads l ON l.id = COALESCE(r.lead_id, t.lead_id)
		WHERE t.campaign_id=?`
	args := []any{campaignID}
	if c, a := execFilterClause(execIDs, applyExecFilter); c != "" {
		q += ` AND ` + c
		args = append(args, a...)
	}
	q += ` ORDER BY r.id DESC`
	rows, err := d.pool.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CallReviewWithLead
	for rows.Next() {
		var v CallReviewWithLead
		var apptBooked int
		if err := rows.Scan(&v.ID, &v.TranscriptID, &v.OrgID, &v.LeadID,
			&v.FirstName, &v.LastName, &v.QualityScore, &apptBooked,
			&v.CustomerSentiment, &v.FailureReason, &v.WhatWentWell,
			&v.WhatWentWrong, &v.PromptImprovementSuggestion, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.AppointmentBooked = apptBooked == 1
		list = append(list, v)
	}
	return list, rows.Err()
}

// CampaignCallInsights is the aggregate payload for the Call Insights tab.
// Issue #75 — was a 404 because this endpoint never existed; the tab was
// silently falling back to the per-call review list and rendering an empty
// state.
type CampaignCallInsights struct {
	TotalReviews       int64              `json:"total_reviews"`
	AvgQualityScore    float64            `json:"avg_quality_score"`
	AppointmentRate    float64            `json:"appointment_rate"`
	SentimentBreakdown map[string]int64   `json:"sentiment_breakdown"`
	TopImprovements    []ImprovementCount `json:"top_improvements"`
	TopFailureReasons  []FailureReason    `json:"top_failure_reasons"`
}

// ImprovementCount counts how many times the same prompt-improvement
// suggestion appears across reviews in a campaign.
type ImprovementCount struct {
	Suggestion string `json:"suggestion"`
	Count      int64  `json:"count"`
}

// GetCampaignCallInsights aggregates call_reviews rows for a campaign into
// the shape the Insights tab renders. Sentiment/improvement/failure columns
// COALESCE legacy and current schema names so old rows still contribute.
func (d *DB) GetCampaignCallInsights(campaignID int64, execIDs []int64, applyExecFilter bool) (*CampaignCallInsights, error) {
	out := &CampaignCallInsights{
		SentimentBreakdown: map[string]int64{},
		TopImprovements:    []ImprovementCount{},
		TopFailureReasons:  []FailureReason{},
	}

	filterJoin := ""
	filterWhere := ""
	filterArgs := []any{}
	if c, a := execFilterClause(execIDs, applyExecFilter); c != "" {
		filterJoin = "LEFT JOIN leads l ON l.id = COALESCE(r.lead_id, t.lead_id)"
		filterWhere = " AND " + c
		filterArgs = append(filterArgs, a...)
	}

	// Summary tile: total / avg score / appointment rate (as a percentage 0-100,
	// matching the frontend's `appointment_rate > 30` threshold check).
	err := d.pool.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(AVG(NULLIF(r.quality_score,0)),0),
		       CASE WHEN COUNT(*)=0 THEN 0
		            ELSE 100.0 * SUM(CASE WHEN r.appointment_booked=1 THEN 1 ELSE 0 END) / COUNT(*)
		       END
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		`+filterJoin+`
		WHERE t.campaign_id=?`+filterWhere, append([]any{campaignID}, filterArgs...)...).
		Scan(&out.TotalReviews, &out.AvgQualityScore, &out.AppointmentRate)
	if err != nil {
		return nil, err
	}

	sRows, err := d.pool.Query(`
		SELECT COALESCE(NULLIF(r.sentiment,''), 'neutral') AS s,
		       COUNT(*)
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		`+filterJoin+`
		WHERE t.campaign_id=?`+filterWhere+`
		GROUP BY s`, append([]any{campaignID}, filterArgs...)...)
	if err == nil {
		defer sRows.Close()
		for sRows.Next() {
			var label string
			var n int64
			if err := sRows.Scan(&label, &n); err == nil {
				out.SentimentBreakdown[label] = n
			}
		}
	}

	iRows, err := d.pool.Query(`
		SELECT COALESCE(NULLIF(r.prompt_improvement_suggestion,''), NULLIF(r.insights,'')) AS s,
		       COUNT(*) AS cnt
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		`+filterJoin+`
		WHERE t.campaign_id=?
		  AND COALESCE(NULLIF(r.prompt_improvement_suggestion,''), NULLIF(r.insights,'')) IS NOT NULL
		  `+filterWhere+`
		GROUP BY s
		ORDER BY cnt DESC LIMIT 5`, append([]any{campaignID}, filterArgs...)...)
	if err == nil {
		defer iRows.Close()
		for iRows.Next() {
			var ic ImprovementCount
			if err := iRows.Scan(&ic.Suggestion, &ic.Count); err == nil {
				out.TopImprovements = append(out.TopImprovements, ic)
			}
		}
	}

	fRows, err := d.pool.Query(`
		SELECT r.failure_reason, COUNT(*) AS cnt
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		`+filterJoin+`
		WHERE t.campaign_id=? AND r.failure_reason IS NOT NULL AND r.failure_reason<>''
		  `+filterWhere+`
		GROUP BY r.failure_reason
		ORDER BY cnt DESC LIMIT 5`, append([]any{campaignID}, filterArgs...)...)
	if err == nil {
		defer fRows.Close()
		for fRows.Next() {
			var fr FailureReason
			if err := fRows.Scan(&fr.Reason, &fr.Count); err == nil {
				out.TopFailureReasons = append(out.TopFailureReasons, fr)
			}
		}
	}

	return out, nil
}

// GetCallReviewByTranscript fetches a single review for a transcript. Returns nil when not found.
func (d *DB) GetCallReviewByTranscript(transcriptID int64) (*CallReview, error) {
	row := d.pool.QueryRow(`
		SELECT id, transcript_id, COALESCE(org_id,0), COALESCE(quality_score,0),
		COALESCE(sentiment,'neutral'), COALESCE(appointment_booked,0),
		COALESCE(failure_reason,''), COALESCE(what_went_well,''), COALESCE(what_went_wrong,''),
		COALESCE(summary,''), COALESCE(insights,''),
		COALESCE(prompt_improvement_suggestion,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_reviews WHERE transcript_id=?`, transcriptID)
	r := &CallReview{}
	var apptBooked int
	err := row.Scan(&r.ID, &r.TranscriptID, &r.OrgID, &r.QualityScore, &r.Sentiment,
		&apptBooked, &r.FailureReason, &r.WhatWentWell, &r.WhatWentWrong,
		&r.Summary, &r.Insights, &r.PromptImprovementSuggestion, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.AppointmentBooked = apptBooked == 1
	return r, nil
}

// SaveCallReview upserts a call review record.
func (d *DB) SaveCallReview(r *CallReview) error {
	apptBooked := 0
	if r.AppointmentBooked {
		apptBooked = 1
	}
	_, err := d.pool.Exec(`
		INSERT INTO call_reviews
		(transcript_id, org_id, lead_id, quality_score, sentiment, appointment_booked,
		 failure_reason, what_went_well, what_went_wrong, summary, insights,
		 prompt_improvement_suggestion)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
		quality_score=VALUES(quality_score), sentiment=VALUES(sentiment),
		appointment_booked=VALUES(appointment_booked), failure_reason=VALUES(failure_reason),
		what_went_well=VALUES(what_went_well), what_went_wrong=VALUES(what_went_wrong),
		summary=VALUES(summary), insights=VALUES(insights),
		prompt_improvement_suggestion=VALUES(prompt_improvement_suggestion),
		lead_id=IF(VALUES(lead_id) > 0, VALUES(lead_id), lead_id)`,
		r.TranscriptID, r.OrgID, r.LeadID, r.QualityScore, r.Sentiment, apptBooked,
		nullString(r.FailureReason), nullString(r.WhatWentWell), nullString(r.WhatWentWrong),
		nullString(r.Summary), nullString(r.Insights), nullString(r.PromptImprovementSuggestion))
	return err
}

func scanReviews(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]CallReview, error) {
	var list []CallReview
	for rows.Next() {
		var r CallReview
		var apptBooked int
		if err := rows.Scan(&r.ID, &r.TranscriptID, &r.OrgID, &r.QualityScore, &r.Sentiment,
			&apptBooked, &r.FailureReason, &r.Summary, &r.Insights, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.AppointmentBooked = apptBooked == 1
		list = append(list, r)
	}
	return list, rows.Err()
}

// CallMemory is one past-call memory entry for a lead, injected into the
// voice agent's system prompt at the start of a new call so the agent
// knows the history with this customer (docs/call-memory-proposal.md).
type CallMemory struct {
	CreatedAt     string
	Summary       string
	FailureReason string
	Suggestion    string // prompt_improvement_suggestion
}

// GetLastCallMemory returns the most recent call reviews for a lead within
// the same campaign, newest first, capped at limit. Returns nil for invalid
// IDs or when the lead has no reviewed calls in this campaign — callers treat
// nil as "no memory", which must never block a call.
func (d *DB) GetLastCallMemory(leadID, campaignID int64, limit int) ([]CallMemory, error) {
	if leadID <= 0 || campaignID <= 0 || limit <= 0 {
		return nil, nil
	}
	// COALESCE(cr.lead_id, ct.lead_id): rows written before lead_id was
	// populated on call_reviews (and not covered by the backfill) still
	// resolve via their transcript.
	rows, err := d.pool.Query(`
		SELECT DATE_FORMAT(cr.created_at,'%Y-%m-%d'),
		       COALESCE(NULLIF(cr.summary,''), NULLIF(cr.what_went_well,''), ''),
		       COALESCE(cr.failure_reason,''),
		       COALESCE(NULLIF(cr.prompt_improvement_suggestion,''), NULLIF(cr.insights,''), '')
		FROM call_reviews cr
		JOIN call_transcripts ct ON cr.transcript_id = ct.id
		WHERE COALESCE(cr.lead_id, ct.lead_id) = ?
		  AND ct.campaign_id = ?
		ORDER BY cr.id DESC
		LIMIT ?`, leadID, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CallMemory
	for rows.Next() {
		var m CallMemory
		if err := rows.Scan(&m.CreatedAt, &m.Summary, &m.FailureReason, &m.Suggestion); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

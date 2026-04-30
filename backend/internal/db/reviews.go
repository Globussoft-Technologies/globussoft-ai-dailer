package db

import (
	"database/sql"
	"errors"
)

// CallReview mirrors the call_reviews table.
type CallReview struct {
	ID                int64   `json:"id"`
	TranscriptID      int64   `json:"transcript_id"`
	OrgID             int64   `json:"org_id"`
	QualityScore      float64 `json:"quality_score"`
	Sentiment         string  `json:"sentiment"`
	AppointmentBooked bool    `json:"appointment_booked"`
	FailureReason     string  `json:"failure_reason"`
	Summary           string  `json:"summary"`
	Insights          string  `json:"insights"`
	CreatedAt         string  `json:"created_at"`
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
func (d *DB) GetCallReviewsByCampaign(campaignID int64) ([]CallReviewWithLead, error) {
	rows, err := d.pool.Query(`
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
		WHERE t.campaign_id=?
		ORDER BY r.id DESC`, campaignID)
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
	TotalReviews       int64                `json:"total_reviews"`
	AvgQualityScore    float64              `json:"avg_quality_score"`
	AppointmentRate    float64              `json:"appointment_rate"`
	SentimentBreakdown map[string]int64     `json:"sentiment_breakdown"`
	TopImprovements    []ImprovementCount   `json:"top_improvements"`
	TopFailureReasons  []FailureReason      `json:"top_failure_reasons"`
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
func (d *DB) GetCampaignCallInsights(campaignID int64) (*CampaignCallInsights, error) {
	out := &CampaignCallInsights{
		SentimentBreakdown: map[string]int64{},
		TopImprovements:    []ImprovementCount{},
		TopFailureReasons:  []FailureReason{},
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
		WHERE t.campaign_id=?`, campaignID).
		Scan(&out.TotalReviews, &out.AvgQualityScore, &out.AppointmentRate)
	if err != nil {
		return nil, err
	}

	sRows, err := d.pool.Query(`
		SELECT COALESCE(NULLIF(r.sentiment,''), 'neutral') AS s,
		       COUNT(*)
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		WHERE t.campaign_id=?
		GROUP BY s`, campaignID)
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
		WHERE t.campaign_id=?
		  AND COALESCE(NULLIF(r.prompt_improvement_suggestion,''), NULLIF(r.insights,'')) IS NOT NULL
		GROUP BY s
		ORDER BY cnt DESC LIMIT 5`, campaignID)
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
		WHERE t.campaign_id=? AND r.failure_reason IS NOT NULL AND r.failure_reason<>''
		GROUP BY r.failure_reason
		ORDER BY cnt DESC LIMIT 5`, campaignID)
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
		COALESCE(failure_reason,''), COALESCE(summary,''), COALESCE(insights,''),
		DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_reviews WHERE transcript_id=?`, transcriptID)
	r := &CallReview{}
	var apptBooked int
	err := row.Scan(&r.ID, &r.TranscriptID, &r.OrgID, &r.QualityScore, &r.Sentiment,
		&apptBooked, &r.FailureReason, &r.Summary, &r.Insights, &r.CreatedAt)
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
		(transcript_id, org_id, quality_score, sentiment, appointment_booked, failure_reason, summary, insights)
		VALUES (?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
		quality_score=VALUES(quality_score), sentiment=VALUES(sentiment),
		appointment_booked=VALUES(appointment_booked), failure_reason=VALUES(failure_reason),
		summary=VALUES(summary), insights=VALUES(insights)`,
		r.TranscriptID, r.OrgID, r.QualityScore, r.Sentiment, apptBooked,
		nullString(r.FailureReason), nullString(r.Summary), nullString(r.Insights))
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

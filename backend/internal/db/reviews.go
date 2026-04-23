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

// GetCallReviewsByCampaign returns all reviews for transcripts belonging to a campaign.
func (d *DB) GetCallReviewsByCampaign(campaignID int64) ([]CallReview, error) {
	rows, err := d.pool.Query(`
		SELECT r.id, r.transcript_id, COALESCE(r.org_id,0), COALESCE(r.quality_score,0),
		COALESCE(r.sentiment,'neutral'), COALESCE(r.appointment_booked,0),
		COALESCE(r.failure_reason,''), COALESCE(r.summary,''), COALESCE(r.insights,''),
		DATE_FORMAT(r.created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_reviews r
		JOIN call_transcripts t ON r.transcript_id=t.id
		WHERE t.campaign_id=?
		ORDER BY r.id DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReviews(rows)
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

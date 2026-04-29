package db

// ── Dashboard types ──────────────────────────────────────────────────────────

// FullDashboardStats is the complete analytics payload the frontend expects.
type FullDashboardStats struct {
	TotalCalls          int64              `json:"total_calls"`
	CallsToday          int64              `json:"calls_today"`
	CallsThisWeek       int64              `json:"calls_this_week"`
	PickupRate          float64            `json:"pickup_rate"`
	AppointmentRate     float64            `json:"appointment_rate"`
	AvgCallDurationSec  float64            `json:"avg_call_duration_sec"`
	DailyCalls          []DailyCallCount   `json:"daily_calls"`
	SentimentBreakdown  SentimentBreakdown `json:"sentiment_breakdown"`
	CampaignPerformance []CampaignPerf     `json:"campaign_performance"`
	TopFailureReasons   []FailureReason    `json:"top_failure_reasons"`
}

type DailyCallCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type SentimentBreakdown struct {
	Positive int64 `json:"positive"`
	Neutral  int64 `json:"neutral"`
	Negative int64 `json:"negative"`
}

type CampaignPerf struct {
	CampaignID   int64   `json:"campaign_id"`
	Name         string  `json:"name"`
	Calls        int64   `json:"calls"`
	Appointments int64   `json:"appointments"`
	AvgScore     float64 `json:"avg_score"`
}

type FailureReason struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// LanguagePerf holds enriched per-language call metrics.
type LanguagePerf struct {
	Language       string  `json:"language"`
	TotalCalls     int64   `json:"total_calls"`
	Appointments   int64   `json:"appointments"`
	ConversionRate float64 `json:"conversion_rate"`
	AvgScore       float64 `json:"avg_score"`
	AvgDuration    float64 `json:"avg_duration"`
}

// GetFullDashboardStats returns the complete analytics payload for an org.
func (d *DB) GetFullDashboardStats(orgID int64) (*FullDashboardStats, error) {
	s := &FullDashboardStats{
		DailyCalls:          []DailyCallCount{},
		CampaignPerformance: []CampaignPerf{},
		TopFailureReasons:   []FailureReason{},
	}

	// ── 1. Aggregate counts ───────────────────────────────────────────────────
	// Read transcript-only metrics from call_transcripts alone — no LEFT JOIN
	// here. The previous version joined call_reviews and used COUNT(*) /
	// SUM(CASE…), which double-counted any transcript that has more than one
	// review (the review pipeline can write multiple rows per transcript when
	// it re-runs). That inflation is what made TotalCalls / CallsToday /
	// pickup-rate disagree with the campaign / sentiment / language tables on
	// the same dashboard (issue #45). Appointments are counted separately
	// below so the join-multiplied row count never bleeds into the tiles.
	var totalCalls, callsToday, callsThisWeek, connected int64
	var avgDur float64
	err := d.pool.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN DATE(created_at)=CURDATE() THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN created_at>=DATE_SUB(NOW(),INTERVAL 6 DAY) THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status NOT IN ('failed','no-answer','busy','initiated') THEN 1 ELSE 0 END),0),
			COALESCE(AVG(NULLIF(call_duration_s,0)),0)
		FROM call_transcripts
		WHERE org_id=?`, orgID).
		Scan(&totalCalls, &callsToday, &callsThisWeek, &connected, &avgDur)
	if err != nil {
		return s, err
	}

	// Appointments: distinct transcripts that have at least one review with
	// appointment_booked=1. COUNT(DISTINCT ct.id) protects against the
	// multi-review-per-transcript fan-out that broke the previous query.
	var appointments int64
	if err := d.pool.QueryRow(`
		SELECT COALESCE(COUNT(DISTINCT ct.id),0)
		FROM call_transcripts ct
		JOIN call_reviews cr ON cr.transcript_id=ct.id
		WHERE ct.org_id=? AND cr.appointment_booked=1`, orgID).
		Scan(&appointments); err != nil {
		// Non-fatal — leave at 0 if the review pipeline hasn't populated yet.
		appointments = 0
	}
	s.TotalCalls = totalCalls
	s.CallsToday = callsToday
	s.CallsThisWeek = callsThisWeek
	s.AvgCallDurationSec = avgDur
	if totalCalls > 0 {
		s.PickupRate = float64(connected) / float64(totalCalls)
		s.AppointmentRate = float64(appointments) / float64(totalCalls)
	}

	// ── 2. Daily calls (last 7 days) ──────────────────────────────────────────
	// Always return exactly 7 rows — one per trailing day, padded with 0 for
	// days that had no calls. Previously the query returned only days that
	// had data, so the bar chart's day-of-week sequence appeared to start on
	// a random weekday (issue #45). Recursive CTE generates the date series
	// (MySQL 8.0+); LEFT JOIN onto call_transcripts so empty days render as
	// 0 rather than being dropped.
	rows, err := d.pool.Query(`
		WITH RECURSIVE days AS (
			SELECT DATE_SUB(CURDATE(), INTERVAL 6 DAY) AS d
			UNION ALL
			SELECT DATE_ADD(d, INTERVAL 1 DAY) FROM days WHERE d < CURDATE()
		)
		SELECT DATE_FORMAT(days.d, '%Y-%m-%d') AS day,
		       COALESCE(COUNT(ct.id), 0) AS cnt
		FROM days
		LEFT JOIN call_transcripts ct
		  ON DATE(ct.created_at) = days.d AND ct.org_id = ?
		GROUP BY days.d
		ORDER BY days.d ASC`, orgID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var dc DailyCallCount
		if err := rows.Scan(&dc.Date, &dc.Count); err != nil {
			return s, err
		}
		s.DailyCalls = append(s.DailyCalls, dc)
	}
	if err := rows.Err(); err != nil {
		return s, err
	}

	// ── 3. Sentiment breakdown (from call_reviews) ────────────────────────────
	err = d.pool.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN cr.sentiment='positive' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN cr.sentiment='neutral' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN cr.sentiment='negative' THEN 1 ELSE 0 END),0)
		FROM call_reviews cr
		JOIN call_transcripts ct ON cr.transcript_id=ct.id
		WHERE ct.org_id=?`, orgID).
		Scan(&s.SentimentBreakdown.Positive, &s.SentimentBreakdown.Neutral, &s.SentimentBreakdown.Negative)
	if err != nil {
		// Non-fatal: sentiment data optional
		s.SentimentBreakdown = SentimentBreakdown{}
	}

	// ── 4. Campaign performance ───────────────────────────────────────────────
	// Same fix as block 1 — appointment count comes from call_reviews.
	cpRows, err := d.pool.Query(`
		SELECT ct.campaign_id, c.name,
			COUNT(*) AS calls,
			COALESCE(SUM(CASE WHEN cr.appointment_booked=1 THEN 1 ELSE 0 END),0) AS appts,
			COALESCE(AVG(cr.quality_score),0) AS avg_score
		FROM call_transcripts ct
		JOIN campaigns c ON ct.campaign_id=c.id
		LEFT JOIN call_reviews cr ON cr.transcript_id=ct.id
		WHERE ct.org_id=? AND ct.campaign_id IS NOT NULL
		GROUP BY ct.campaign_id, c.name
		ORDER BY calls DESC LIMIT 20`, orgID)
	if err == nil {
		defer cpRows.Close()
		for cpRows.Next() {
			var cp CampaignPerf
			if err := cpRows.Scan(&cp.CampaignID, &cp.Name, &cp.Calls, &cp.Appointments, &cp.AvgScore); err == nil {
				s.CampaignPerformance = append(s.CampaignPerformance, cp)
			}
		}
	}

	// ── 5. Top failure reasons ────────────────────────────────────────────────
	// Mirrors Backup_Callified's routes.py:477 — the reviewer/Gemini-tagged
	// failure_reason in call_reviews is what the user wants to see (prose like
	// "The AI failed to actively listen…"), not the 3-value dial status enum.
	// Limit matches Python (top 10) so the list looks the same in the UI.
	frRows, err := d.pool.Query(`
		SELECT cr.failure_reason, COUNT(*) AS cnt
		FROM call_reviews cr
		JOIN call_transcripts ct ON cr.transcript_id=ct.id
		WHERE ct.org_id=? AND cr.failure_reason IS NOT NULL AND cr.failure_reason<>''
		GROUP BY cr.failure_reason ORDER BY cnt DESC LIMIT 10`, orgID)
	if err == nil {
		defer frRows.Close()
		for frRows.Next() {
			var fr FailureReason
			if err := frRows.Scan(&fr.Reason, &fr.Count); err == nil {
				s.TopFailureReasons = append(s.TopFailureReasons, fr)
			}
		}
	}

	return s, nil
}

// GetLanguagePerformance returns per-language call metrics for an org.
func (d *DB) GetLanguagePerformance(orgID int64) ([]LanguagePerf, error) {
	rows, err := d.pool.Query(`
		SELECT
			COALESCE(ct.tts_language,'unknown') AS language,
			COUNT(*) AS total_calls,
			COALESCE(SUM(CASE WHEN ct.appointment_booked=1 THEN 1 ELSE 0 END),0) AS appointments,
			COALESCE(AVG(NULLIF(ct.call_duration_s,0)),0) AS avg_duration,
			COALESCE(AVG(cr.quality_score),0) AS avg_score
		FROM call_transcripts ct
		LEFT JOIN call_reviews cr ON cr.transcript_id=ct.id
		WHERE ct.org_id=?
		GROUP BY language ORDER BY total_calls DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []LanguagePerf
	for rows.Next() {
		var lp LanguagePerf
		if err := rows.Scan(&lp.Language, &lp.TotalCalls, &lp.Appointments, &lp.AvgDuration, &lp.AvgScore); err != nil {
			return nil, err
		}
		if lp.TotalCalls > 0 {
			lp.ConversionRate = float64(lp.Appointments) / float64(lp.TotalCalls) * 100
		}
		list = append(list, lp)
	}
	return list, rows.Err()
}

// CampaignExportRow holds per-lead call data for CSV/report export.
type CampaignExportRow struct {
	LeadName        string  `json:"lead_name"`
	Phone           string  `json:"phone"`
	Status          string  `json:"status"`
	CallDuration    int     `json:"call_duration_s"`
	SentimentScore  float64 `json:"sentiment_score"`
	AppointmentDate string  `json:"appointment_date"`
	FollowUpNote    string  `json:"follow_up_note"`
	CalledAt        string  `json:"called_at"`
}

// GetCampaignAnalyticsForExport returns one row per call transcript in a campaign.
func (d *DB) GetCampaignAnalyticsForExport(campaignID int64) ([]CampaignExportRow, error) {
	rows, err := d.pool.Query(`
		SELECT
			CONCAT(l.first_name,' ',COALESCE(l.last_name,'')) AS lead_name,
			l.phone,
			COALESCE(l.status,'new'),
			COALESCE(ct.call_duration_s,0),
			COALESCE(ct.sentiment_score,0),
			COALESCE(ct.appointment_date,''),
			COALESCE(l.follow_up_note,''),
			DATE_FORMAT(ct.created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_transcripts ct
		JOIN leads l ON ct.lead_id=l.id
		WHERE ct.campaign_id=?
		ORDER BY ct.id DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CampaignExportRow
	for rows.Next() {
		var r CampaignExportRow
		if err := rows.Scan(&r.LeadName, &r.Phone, &r.Status,
			&r.CallDuration, &r.SentimentScore, &r.AppointmentDate,
			&r.FollowUpNote, &r.CalledAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// ScoredLead is a lead with an AI quality score from call_reviews.
type ScoredLead struct {
	LeadID        int64   `json:"lead_id"`
	LeadName      string  `json:"lead_name"`
	Phone         string  `json:"phone"`
	Status        string  `json:"status"`
	QualityScore  float64 `json:"quality_score"`
	Sentiment     string  `json:"sentiment"`
	FailureReason string  `json:"failure_reason"`
	ReviewedAt    string  `json:"reviewed_at"`
}

// GetScoredLeads returns leads with AI quality scores for the given org/campaign.
// Pass campaignID=0 to get all scored leads for the org.
func (d *DB) GetScoredLeads(orgID, campaignID int64) ([]ScoredLead, error) {
	query := `
		SELECT l.id, CONCAT(l.first_name,' ',COALESCE(l.last_name,'')) AS name,
			l.phone, COALESCE(l.status,''),
			COALESCE(cr.quality_score,0), COALESCE(cr.sentiment,''),
			COALESCE(cr.failure_reason,''),
			DATE_FORMAT(cr.created_at,'%Y-%m-%d %H:%i:%s')
		FROM call_reviews cr
		JOIN call_transcripts ct ON cr.transcript_id=ct.id
		JOIN leads l ON ct.lead_id=l.id
		WHERE ct.org_id=?`
	args := []any{orgID}
	if campaignID > 0 {
		query += ` AND ct.campaign_id=?`
		args = append(args, campaignID)
	}
	query += ` ORDER BY cr.quality_score DESC, cr.id DESC LIMIT 500`

	rows, err := d.pool.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ScoredLead
	for rows.Next() {
		var s ScoredLead
		if err := rows.Scan(&s.LeadID, &s.LeadName, &s.Phone, &s.Status,
			&s.QualityScore, &s.Sentiment, &s.FailureReason, &s.ReviewedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

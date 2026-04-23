package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
)

// GET /api/analytics/dashboard
func (s *Server) analyticsDashboard(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	stats, err := s.db.GetFullDashboardStats(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("analyticsDashboard", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// GET /api/analytics/languages
func (s *Server) analyticsLanguages(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	langs, err := s.db.GetLanguagePerformance(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("analyticsLanguages", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(langs))
}

// GET /api/analytics/export?campaign_id=N
func (s *Server) analyticsExportCSV(w http.ResponseWriter, r *http.Request) {
	campaignIDStr := r.URL.Query().Get("campaign_id")
	campaignID, _ := strconv.ParseInt(campaignIDStr, 10, 64)

	rows, err := s.db.GetCampaignAnalyticsForExport(campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=campaign_%d_export.csv", campaignID))

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Lead Name", "Phone", "Status", "Call Duration (s)",
		"Sentiment Score", "Appointment Date", "Follow Up Note", "Called At"})
	for _, row := range rows {
		_ = cw.Write([]string{
			row.LeadName, row.Phone, row.Status,
			strconv.Itoa(row.CallDuration),
			fmt.Sprintf("%.2f", row.SentimentScore),
			row.AppointmentDate, row.FollowUpNote, row.CalledAt,
		})
	}
	cw.Flush()
}

// GET /api/analytics/report?campaign_id=N  — returns HTML report
func (s *Server) analyticsExportReport(w http.ResponseWriter, r *http.Request) {
	campaignIDStr := r.URL.Query().Get("campaign_id")
	campaignID, _ := strconv.ParseInt(campaignIDStr, 10, 64)

	rows, err := s.db.GetCampaignAnalyticsForExport(campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Campaign Report</title>
<style>body{font-family:sans-serif;margin:32px}table{border-collapse:collapse;width:100%%}
th,td{border:1px solid #ddd;padding:8px;font-size:13px}th{background:#f5f5f5}</style>
</head><body><h2>Campaign %d Report</h2>
<table><tr><th>Lead</th><th>Phone</th><th>Status</th><th>Duration (s)</th>
<th>Sentiment</th><th>Appointment</th><th>Called At</th></tr>`, campaignID)
	for _, row := range rows {
		fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%d</td>
<td>%.2f</td><td>%s</td><td>%s</td></tr>`,
			row.LeadName, row.Phone, row.Status, row.CallDuration,
			row.SentimentScore, row.AppointmentDate, row.CalledAt)
	}
	fmt.Fprint(w, "</table></body></html>")
}

// GET /api/analytics/scored-leads?campaign_id=N
func (s *Server) scoredLeads(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	campaignIDStr := r.URL.Query().Get("campaign_id")
	campaignID, _ := strconv.ParseInt(campaignIDStr, 10, 64)

	leads, err := s.db.GetScoredLeads(ac.OrgID, campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(leads))
}

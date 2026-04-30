package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// ── GET /api/scheduled-calls ──────────────────────────────────────────────────

func (s *Server) listScheduledCalls(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	calls, err := s.db.GetScheduledCallsByOrg(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listScheduledCalls", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(calls))
}

// ── POST /api/scheduled-calls ─────────────────────────────────────────────────

func (s *Server) createScheduledCall(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		LeadID      int64  `json:"lead_id"`
		CampaignID  int64  `json:"campaign_id"`
		ScheduledAt string `json:"scheduled_at"` // RFC3339 or "2006-01-02 15:04:05"
		Notes       string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LeadID == 0 || body.ScheduledAt == "" {
		writeError(w, http.StatusBadRequest, "lead_id and scheduled_at required")
		return
	}

	var scheduledAt time.Time
	var parseErr error
	scheduledAt, parseErr = time.Parse(time.RFC3339, body.ScheduledAt)
	if parseErr != nil {
		scheduledAt, parseErr = time.Parse("2006-01-02 15:04:05", body.ScheduledAt)
	}
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid scheduled_at — use RFC3339 or YYYY-MM-DD HH:MM:SS")
		return
	}

	// DND guard: refuse to schedule a call to a number on the org's DND list.
	// The dial-time worker also checks DND (dial/initiator.go), but blocking
	// here surfaces the reason to the user immediately instead of letting it
	// fail silently at dial time with a generic "failed" status.
	lead, leadErr := s.db.GetLeadByID(body.LeadID)
	if leadErr != nil || lead == nil {
		writeError(w, http.StatusBadRequest, "lead not found")
		return
	}
	if isDND, dndErr := s.db.IsDNDNumber(ac.OrgID, lead.Phone); dndErr == nil && isDND {
		writeError(w, http.StatusConflict,
			"This number is on the DND list. Remove it from DND before scheduling.")
		return
	}

	id, err := s.db.CreateScheduledCall(ac.OrgID, body.LeadID, body.CampaignID, scheduledAt, body.Notes)
	if err != nil {
		s.logger.Sugar().Errorw("createScheduledCall", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── DELETE /api/scheduled-calls/{id} ─────────────────────────────────────────

func (s *Server) cancelScheduledCall(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cancelled, err := s.db.CancelScheduledCall(ac.OrgID, id)
	if err != nil {
		s.logger.Sugar().Errorw("cancelScheduledCall", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !cancelled {
		writeError(w, http.StatusNotFound, "not found or already processed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

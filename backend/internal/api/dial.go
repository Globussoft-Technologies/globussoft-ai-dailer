package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/dial"
)

// dialLead initiates an immediate call to a specific lead.
// POST /api/dial/{lead_id}
func (s *Server) dialLead(w http.ResponseWriter, r *http.Request) {
	leadID, err := parseID(r, "lead_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid lead_id")
		return
	}

	lead, err := s.db.GetLeadByID(leadID)
	if err != nil || lead == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	var body struct {
		CampaignID int64 `json:"campaign_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	vs, _ := s.db.GetCampaignVoiceSettings(body.CampaignID)
	ac := getAuth(r)

	data := dial.CallData{
		LeadID:      lead.ID,
		LeadName:    lead.FirstName + " " + lead.LastName,
		LeadPhone:   lead.Phone,
		CampaignID:  body.CampaignID,
		OrgID:       ac.OrgID,
		Interest:    lead.Interest,
		TTSProvider: vs.TTSProvider,
		TTSVoiceID:  vs.TTSVoiceID,
		TTSLanguage: vs.TTSLanguage,
	}

	if err := s.initiator.Initiate(r.Context(), data); err != nil {
		s.logger.Warn("dialLead: initiate failed",
			zap.Int64("lead_id", leadID), zap.Error(err))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"dialed": true})
}

// campaignDialLead dials a specific lead within a campaign context.
// POST /api/campaigns/{id}/dial/{lead_id}
func (s *Server) campaignDialLead(w http.ResponseWriter, r *http.Request) {
	campaignID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid campaign id")
		return
	}
	leadID, err := parseID(r, "lead_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid lead_id")
		return
	}

	lead, err := s.db.GetLeadByID(leadID)
	if err != nil || lead == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)
	ac := getAuth(r)

	data := dial.CallData{
		LeadID:      lead.ID,
		LeadName:    lead.FirstName + " " + lead.LastName,
		LeadPhone:   lead.Phone,
		CampaignID:  campaignID,
		OrgID:       ac.OrgID,
		Interest:    lead.Interest,
		TTSProvider: vs.TTSProvider,
		TTSVoiceID:  vs.TTSVoiceID,
		TTSLanguage: vs.TTSLanguage,
	}

	if err := s.initiator.Initiate(r.Context(), data); err != nil {
		s.logger.Warn("campaignDialLead: initiate failed",
			zap.Int64("campaign_id", campaignID), zap.Int64("lead_id", leadID), zap.Error(err))
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"dialed": true})
}

// campaignDialAll dials all pending leads in a campaign (fire-and-forget goroutines).
// POST /api/campaigns/{id}/dial-all
func (s *Server) campaignDialAll(w http.ResponseWriter, r *http.Request) {
	campaignID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid campaign id")
		return
	}

	leads, err := s.db.GetCampaignLeads(campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list leads")
		return
	}

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)
	ac := getAuth(r)

	queued := 0
	for _, lead := range leads {
		// Only dial leads that haven't been called yet
		if lead.Status == "Calling" || lead.Status == "Completed" || lead.Status == "DND — do not call" {
			continue
		}
		ld := lead // capture loop var
		data := dial.CallData{
			LeadID:      ld.ID,
			LeadName:    ld.FirstName + " " + ld.LastName,
			LeadPhone:   ld.Phone,
			CampaignID:  campaignID,
			OrgID:       ac.OrgID,
			Interest:    ld.Interest,
			TTSProvider: vs.TTSProvider,
			TTSVoiceID:  vs.TTSVoiceID,
			TTSLanguage: vs.TTSLanguage,
		}
		go func(d dial.CallData) {
			if err := s.initiator.Initiate(r.Context(), d); err != nil {
				s.logger.Warn("campaignDialAll: lead failed",
					zap.Int64("lead_id", d.LeadID), zap.Error(err))
			}
		}(data)
		queued++
	}

	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}

// campaignRedialFailed re-dials all leads in the campaign that have a "Call Failed" status.
// POST /api/campaigns/{id}/redial-failed
func (s *Server) campaignRedialFailed(w http.ResponseWriter, r *http.Request) {
	campaignID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid campaign id")
		return
	}

	leads, err := s.db.GetFailedLeadsInCampaign(campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failed leads")
		return
	}

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)
	ac := getAuth(r)

	queued := 0
	for _, lead := range leads {
		ld := lead
		data := dial.CallData{
			LeadID:      ld.ID,
			LeadName:    ld.FirstName + " " + ld.LastName,
			LeadPhone:   ld.Phone,
			CampaignID:  campaignID,
			OrgID:       ac.OrgID,
			Interest:    ld.Interest,
			TTSProvider: vs.TTSProvider,
			TTSVoiceID:  vs.TTSVoiceID,
			TTSLanguage: vs.TTSLanguage,
		}
		go func(d dial.CallData) {
			if err := s.initiator.Initiate(r.Context(), d); err != nil {
				s.logger.Warn("campaignRedialFailed: lead failed",
					zap.Int64("lead_id", d.LeadID), zap.Error(err))
			}
		}(data)
		queued++
	}

	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}

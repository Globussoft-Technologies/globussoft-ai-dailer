package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

// ── GET /api/campaigns ───────────────────────────────────────────────────────

func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	campaigns, err := s.db.GetCampaignsByOrg(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listCampaigns", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(campaigns))
}

// ── POST /api/campaigns ──────────────────────────────────────────────────────

type campaignCreateRequest struct {
	Name       string `json:"name"`
	ProductID  int64  `json:"product_id"`
	LeadSource string `json:"lead_source"`
	Channel    string `json:"channel"`
}

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var req campaignCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.ProductID == 0 {
		writeError(w, http.StatusBadRequest, "name and product_id required")
		return
	}
	id, err := s.db.CreateCampaign(ac.OrgID, req.ProductID, req.Name, req.LeadSource, coalesceStr(req.Channel, "voice"))
	if err != nil {
		s.logger.Sugar().Errorw("createCampaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── GET /api/campaigns/{id} ──────────────────────────────────────────────────

func (s *Server) getCampaign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	c, err := s.db.GetCampaignByID(id)
	if err != nil {
		s.logger.Sugar().Errorw("getCampaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ── PUT /api/campaigns/{id} ──────────────────────────────────────────────────

type campaignUpdateRequest struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	LeadSource string `json:"lead_source"`
	ProductID  int64  `json:"product_id"`
	Channel    string `json:"channel"`
}

func (s *Server) updateCampaign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req campaignUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.UpdateCampaign(id, req.Name, req.Status, req.LeadSource, req.Channel, req.ProductID); err != nil {
		s.logger.Sugar().Errorw("updateCampaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── DELETE /api/campaigns/{id} ───────────────────────────────────────────────

func (s *Server) deleteCampaign(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := s.db.DeleteCampaign(id)
	if err != nil {
		s.logger.Sugar().Errorw("deleteCampaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── GET /api/campaigns/{id}/leads ────────────────────────────────────────────

func (s *Server) listCampaignLeads(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	leads, err := s.db.GetCampaignLeads(id)
	if err != nil {
		s.logger.Sugar().Errorw("listCampaignLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(leads))
}

// ── POST /api/campaigns/{id}/leads ───────────────────────────────────────────

func (s *Server) addCampaignLeads(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		LeadIDs []int64 `json:"lead_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.LeadIDs) == 0 {
		writeError(w, http.StatusBadRequest, "lead_ids required")
		return
	}
	added, err := s.db.AddLeadsToCampaign(id, body.LeadIDs)
	if err != nil {
		s.logger.Sugar().Errorw("addCampaignLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

// ── DELETE /api/campaigns/{id}/leads/{lead_id} ───────────────────────────────

func (s *Server) removeCampaignLead(w http.ResponseWriter, r *http.Request) {
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
	removed, err := s.db.RemoveLeadFromCampaign(campaignID, leadID)
	if err != nil {
		s.logger.Sugar().Errorw("removeCampaignLead", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "lead not in campaign")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// ── GET /api/campaigns/{id}/stats ────────────────────────────────────────────

func (s *Server) getCampaignStats(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	stats, err := s.db.GetCampaignStats(id)
	if err != nil {
		s.logger.Sugar().Errorw("getCampaignStats", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ── GET /api/campaigns/{id}/call-log ─────────────────────────────────────────

func (s *Server) getCampaignCallLog(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	log, err := s.db.GetCampaignCallLog(id)
	if err != nil {
		s.logger.Sugar().Errorw("getCampaignCallLog", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(log))
}

// ── GET /api/campaigns/{id}/voice-settings ───────────────────────────────────

func (s *Server) getCampaignVoiceSettings(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	vs, err := s.db.GetCampaignVoiceSettings(id)
	if err != nil {
		s.logger.Sugar().Errorw("getCampaignVoiceSettings", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

// ── PUT /api/campaigns/{id}/voice-settings ────────────────────────────────────

func (s *Server) saveCampaignVoiceSettings(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var vs db.VoiceSettings
	if err := json.NewDecoder(r.Body).Decode(&vs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.SaveCampaignVoiceSettings(id, vs); err != nil {
		s.logger.Sugar().Errorw("saveCampaignVoiceSettings", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── POST /api/campaigns/{id}/import-csv ──────────────────────────────────────
// Import CSV of leads and add them to the campaign in one step.

func (s *Server) importCampaignLeadsCSV(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	campaignID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil || len(records) < 2 {
		writeError(w, http.StatusBadRequest, "invalid CSV")
		return
	}

	header := records[0]
	idx := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}
	iFirst, iLast, iPhone, iSource := idx("first_name"), idx("last_name"), idx("phone"), idx("source")
	if iFirst < 0 || iPhone < 0 {
		writeError(w, http.StatusBadRequest, "CSV must have first_name and phone columns")
		return
	}

	get := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var rows []db.LeadImportRow
	for _, rec := range records[1:] {
		rows = append(rows, db.LeadImportRow{
			FirstName: get(rec, iFirst), LastName: get(rec, iLast),
			Phone: get(rec, iPhone), Source: get(rec, iSource),
		})
	}

	imported, errs := s.db.BulkCreateLeads(rows, ac.OrgID)

	// Fetch IDs of newly created leads to add to campaign — re-query by phone
	var addedIDs []int64
	for _, row := range rows {
		lead, err := s.db.SearchLeads(row.Phone, ac.OrgID)
		if err == nil && len(lead) > 0 {
			addedIDs = append(addedIDs, lead[0].ID)
		}
	}
	var addedToCampaign int
	if len(addedIDs) > 0 {
		addedToCampaign, _ = s.db.AddLeadsToCampaign(campaignID, addedIDs)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imported":          imported,
		"added_to_campaign": addedToCampaign,
		"errors":            errs,
	})
}

// ── GET /api/campaigns/{id}/call-reviews ──────────────────────────────────────

func (s *Server) getCampaignCallReviews(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	reviews, err := s.db.GetCallReviewsByCampaign(id)
	if err != nil {
		s.logger.Sugar().Errorw("getCampaignCallReviews", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(reviews))
}

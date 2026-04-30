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

// validateCampaignName mirrors frontend/src/utils/campaignName.js. Defense
// in depth — the React UI auto-escapes JSX text, but the same string can
// leak into less-defended surfaces (emails, CSV exports, plain-text logs)
// where `<` / `>` would matter, so we reject them at the API boundary too.
func validateCampaignName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "name is required"
	}
	if len(trimmed) > 100 {
		return "name must be 100 characters or fewer"
	}
	if strings.ContainsAny(trimmed, "<>") {
		return "name cannot contain < or > characters"
	}
	return ""
}

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var req campaignCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProductID == 0 {
		writeError(w, http.StatusBadRequest, "name and product_id required")
		return
	}
	if msg := validateCampaignName(req.Name); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, err := s.db.CreateCampaign(ac.OrgID, req.ProductID, strings.TrimSpace(req.Name), req.LeadSource, coalesceStr(req.Channel, "voice"))
	if err != nil {
		s.logger.Sugar().Errorw("createCampaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── GET /api/campaigns/{id} ──────────────────────────────────────────────────
//
// Attaches up-to-date stats (and voice settings) on the same response, so the
// Campaign Detail page renders live numbers on every open — not whatever stale
// snapshot the campaigns-list fetch had. Matches the Python endpoint shape
// (routes.py:1335-1341) exactly:
//
//     {...campaign_fields, "stats": {...}, "voice_settings": {...}}
//
// Before this change, the Total/Called/Qualified/Appointments KPI cards read
// from selectedCampaign.stats, which the list endpoint populates once — so any
// call or lead add that happened after the list was fetched left the cards
// frozen at 0 until a full page reload.

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
	// Attach fresh stats — best-effort; we don't fail the whole response if
	// the stats query breaks.
	if stats, err := s.db.GetCampaignStats(id); err == nil {
		c.Stats = &stats
	} else {
		s.logger.Sugar().Warnw("getCampaign: stats lookup failed", "id", id, "err", err)
	}
	// Attach voice settings in a merged map so we stay backwards-compatible
	// with clients that still read c.* directly.
	resp := map[string]any{
		"id":           c.ID,
		"org_id":       c.OrgID,
		"product_id":   c.ProductID,
		"name":         c.Name,
		"status":       c.Status,
		"tts_provider": c.TTSProvider,
		"tts_voice_id": c.TTSVoiceID,
		"tts_language": c.TTSLanguage,
		"lead_source":  c.LeadSource,
		"channel":      c.Channel,
		"product_name": c.ProductName,
		"created_at":   c.CreatedAt,
		"stats":        c.Stats,
	}
	if vs, err := s.db.GetCampaignVoiceSettings(id); err == nil {
		resp["voice_settings"] = map[string]string{
			"tts_provider": vs.TTSProvider,
			"tts_voice_id": vs.TTSVoiceID,
			"tts_language": vs.TTSLanguage,
		}
	}
	writeJSON(w, http.StatusOK, resp)
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
	// Only validate name when the caller is actually changing it. Empty Name
	// in this PATCH-style endpoint means "leave as-is" (UpdateCampaign
	// already skips empty fields).
	if req.Name != "" {
		if msg := validateCampaignName(req.Name); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
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

// ── GET /api/campaigns/{id}/call-insights ─────────────────────────────────────
// Aggregates call_reviews rows for a campaign into the summary cards +
// improvement/failure lists the Insights tab renders. Was missing entirely
// before — the tab fell back to the empty per-call list and showed the
// "no reviews yet" empty state forever. Issue #75.

func (s *Server) getCampaignCallInsights(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	insights, err := s.db.GetCampaignCallInsights(id)
	if err != nil {
		s.logger.Sugar().Errorw("getCampaignCallInsights", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, insights)
}

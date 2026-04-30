package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/llm"
)

// isValidPhone enforces exactly 10 digits, no other characters. Mirrors the
// frontend constraint in the Quick Add / Edit Lead inputs.
func isValidPhone(p string) bool {
	if len(p) != 10 {
		return false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ── GET /api/leads/sample-csv ─────────────────────────────────────────────────
// Returns a downloadable CSV template showing the expected import format.

func (s *Server) sampleCSV(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="sample_leads.csv"`)
	wr := csv.NewWriter(w)
	_ = wr.Write([]string{"first_name", "last_name", "phone", "source"})
	_ = wr.Write([]string{"Rahul", "Sharma", "9876543210", "Website"})
	_ = wr.Write([]string{"Priya", "Patel", "9123456789", "Referral"})
	_ = wr.Write([]string{"Amit", "Kumar", "9988776655", "Cold Call"})
	wr.Flush()
}

// ── GET /api/leads/export ─────────────────────────────────────────────────────
// Streams all org leads as a downloadable CSV file.

func (s *Server) exportLeads(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	leads, err := s.db.GetAllLeads(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("exportLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="leads_export.csv"`)
	wr := csv.NewWriter(w)
	_ = wr.Write([]string{
		"id", "first_name", "last_name", "phone", "source",
		"status", "interest", "follow_up_note", "external_id", "crm_provider", "created_at",
	})
	for _, l := range leads {
		_ = wr.Write([]string{
			strconv.FormatInt(l.ID, 10),
			l.FirstName, l.LastName, l.Phone, l.Source,
			l.Status, l.Interest, l.FollowUpNote, l.ExternalID, l.CRMProvider, l.CreatedAt,
		})
	}
	wr.Flush()
}

// ── GET /api/leads ────────────────────────────────────────────────────────────

func (s *Server) listLeads(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	leads, err := s.db.GetAllLeads(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(leads))
}

// ── GET /api/leads/search?q=... ───────────────────────────────────────────────

func (s *Server) searchLeads(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q query param required")
		return
	}
	leads, err := s.db.SearchLeads(q, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("searchLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(leads))
}

// ── POST /api/leads ───────────────────────────────────────────────────────────

type leadCreateRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Source    string `json:"source"`
	Interest  string `json:"interest"`
}

func (s *Server) createLead(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var req leadCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if fields := validateLeadFields(req.FirstName, req.Phone); len(fields) > 0 {
		writeFieldError(w, http.StatusBadRequest, "validation failed", fields)
		return
	}
	id, err := s.db.CreateLead(req.FirstName, req.LastName, req.Phone, req.Source, req.Interest, ac.OrgID)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "1062") {
			writeFieldError(w, http.StatusConflict, "phone number already exists",
				map[string]string{"phone": "Phone number already exists"})
			return
		}
		s.logger.Sugar().Errorw("createLead", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// validateLeadFields mirrors the Quick Add inline validation strings on the
// frontend so per-field server errors match what the form displays.
func validateLeadFields(firstName, phone string) map[string]string {
	fields := map[string]string{}
	name := strings.TrimSpace(firstName)
	if name == "" {
		fields["first_name"] = "Name is required"
	} else if !nameHasLettersOnly(name) {
		fields["first_name"] = "Name must contain only letters"
	}
	if strings.TrimSpace(phone) == "" {
		fields["phone"] = "Phone is required"
	} else if !isValidPhone(phone) {
		fields["phone"] = "Indian numbers must be exactly 10 digits"
	}
	return fields
}

// nameHasLettersOnly accepts names made of ASCII letters plus common
// punctuation (space, apostrophe, hyphen, dot). Rejects any digit and
// requires at least one letter — mirrors the frontend rule in CampaignDetail.
func nameHasLettersOnly(s string) bool {
	hasLetter := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			hasLetter = true
		case r == ' ', r == '\'', r == '-', r == '.':
			// allowed
		default:
			return false
		}
	}
	return hasLetter
}

// ── GET /api/leads/{id} ───────────────────────────────────────────────────────

func (s *Server) getLead(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	lead, err := s.db.GetLeadByID(id)
	if err != nil {
		s.logger.Sugar().Errorw("getLead", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if lead == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	writeJSON(w, http.StatusOK, lead)
}

// ── PUT /api/leads/{id} ───────────────────────────────────────────────────────

type leadUpdateRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Source    string `json:"source"`
	Interest  string `json:"interest"`
}

func (s *Server) updateLead(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req leadUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if fields := validateLeadFields(req.FirstName, req.Phone); len(fields) > 0 {
		writeFieldError(w, http.StatusBadRequest, "validation failed", fields)
		return
	}
	updated, err := s.db.UpdateLead(id, req.FirstName, req.LastName, req.Phone, req.Source, req.Interest, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("updateLead", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !updated {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── DELETE /api/leads/{id} ────────────────────────────────────────────────────

func (s *Server) deleteLead(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := s.db.DeleteLead(id, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("deleteLead", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── PUT /api/leads/{id}/status ────────────────────────────────────────────────

func (s *Server) updateLeadStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		writeError(w, http.StatusBadRequest, "status required")
		return
	}
	if err := s.db.UpdateLeadStatus(id, body.Status); err != nil {
		s.logger.Sugar().Errorw("updateLeadStatus", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── POST /api/leads/{id}/notes ────────────────────────────────────────────────

func (s *Server) updateLeadNote(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.UpdateLeadNote(id, body.Note); err != nil {
		s.logger.Sugar().Errorw("updateLeadNote", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── POST /api/leads/import-csv ────────────────────────────────────────────────
// Accepts multipart/form-data with a "file" field containing a CSV.
// CSV columns (header row): first_name,last_name,phone,source

func (s *Server) importLeadsCSV(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB limit
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid CSV")
		return
	}
	if len(records) < 2 {
		writeError(w, http.StatusBadRequest, "CSV must have header + at least one data row")
		return
	}

	// Map header columns to indices
	header := records[0]
	idx := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}
	iFirst := idx("first_name")
	iLast := idx("last_name")
	iPhone := idx("phone")
	iSource := idx("source")

	if iFirst < 0 || iPhone < 0 {
		writeError(w, http.StatusBadRequest, "CSV must have first_name and phone columns")
		return
	}

	var rows []db.LeadImportRow
	var skipped []string
	get := func(record []string, i int) string {
		if i < 0 || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}

	for rowIdx, rec := range records[1:] {
		phone := get(rec, iPhone)
		if !isValidPhone(phone) {
			skipped = append(skipped, fmt.Sprintf("row %d: phone %q not 10 digits", rowIdx+2, phone))
			continue
		}
		rows = append(rows, db.LeadImportRow{
			FirstName: get(rec, iFirst),
			LastName:  get(rec, iLast),
			Phone:     phone,
			Source:    get(rec, iSource),
		})
	}

	imported, errs := s.db.BulkCreateLeads(rows, ac.OrgID)
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": imported,
		"errors":   append(errs, skipped...),
	})
}

// ── GET /api/leads/{id}/documents ─────────────────────────────────────────────

func (s *Server) getLeadDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	docs, err := s.db.GetDocumentsByLead(id)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadDocuments", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(docs))
}

// ── POST /api/leads/{id}/documents ───────────────────────────────────────────

func (s *Server) uploadLeadDocument(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	// Save file to docs/ alongside the recordings directory
	docsDir := filepath.Join(s.cfg.RecordingsDir, "..", "docs")
	if mkErr := os.MkdirAll(docsDir, 0755); mkErr != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	dstPath := filepath.Join(docsDir, header.Filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "write error")
		return
	}
	fileURL := "/docs/" + header.Filename
	if err := s.db.CreateDocument(id, header.Filename, fileURL); err != nil {
		s.logger.Sugar().Errorw("uploadLeadDocument", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": fileURL})
}

// ── GET /api/transcripts/{id}/review ─────────────────────────────────────────

func (s *Server) getTranscriptReview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	review, err := s.db.GetCallReviewByTranscript(id)
	if err != nil {
		s.logger.Sugar().Errorw("getTranscriptReview", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if review == nil {
		writeError(w, http.StatusNotFound, "review not found")
		return
	}
	writeJSON(w, http.StatusOK, review)
}

// ── GET /api/leads/{id}/transcripts ───────────────────────────────────────────

func (s *Server) getLeadTranscripts(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	transcripts, err := s.db.GetTranscriptsByLead(id)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadTranscripts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(transcripts))
}

// ── GET /api/leads/by-phone/{phone}/calls ─────────────────────────────────────
//
// Returns all completed calls for the lead with the given phone — combining
// the audio recording URL and the interaction transcript turns into one row
// per call. Convenience for callers that have only the phone (e.g. external
// integrations, the wsprobe tool) and want everything about a lead's history
// in one fetch instead of search → get → transcripts → recording.
//
// Response shape:
//
//	[
//	  {
//	    "id":            <transcript id>,
//	    "lead_id":       <id>,
//	    "lead_name":     "Harsha",
//	    "phone":         "9177007429",
//	    "duration_s":    56.78,
//	    "tts_language":  "en",
//	    "created_at":    "2026-04-28 10:01:08",
//	    "recording_url": "/api/recordings/web_sim_..._.wav",
//	    "transcript":    [ {"role":"agent","text":"..."}, {"role":"user","text":"..."} ]
//	  },
//	  ...
//	]
//
// Org-scoped via GetLeadByPhoneOrg so an Agent in org A can't query org B's
// leads by guessing phone numbers. Returns an empty array (200 OK) when the
// phone matches no lead in the caller's org — same shape as a lead with no
// calls — so consumers don't need a 404 branch.
func (s *Server) getLeadCallsByPhone(w http.ResponseWriter, r *http.Request) {
	phone := strings.TrimSpace(r.PathValue("phone"))
	if phone == "" {
		writeError(w, http.StatusBadRequest, "phone required")
		return
	}

	ac := getAuth(r)
	lead, err := s.db.GetLeadByPhoneOrg(phone, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadCallsByPhone: lookup", "err", err, "phone", phone)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if lead == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	transcripts, err := s.db.GetTranscriptsByLead(lead.ID)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadCallsByPhone: transcripts", "err", err, "lead_id", lead.ID)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	leadName := strings.TrimSpace(lead.FirstName + " " + lead.LastName)
	out := make([]map[string]any, 0, len(transcripts))
	for _, t := range transcripts {
		// Decode the transcript JSON ([{role,text}, …]) into a structured
		// array so consumers don't have to re-parse a string-of-JSON. Falls
		// back to an empty array on malformed rows so a single corrupt
		// transcript can't blank out the whole response.
		var turns []map[string]any
		if len(t.Transcript) > 0 {
			if err := json.Unmarshal(t.Transcript, &turns); err != nil {
				turns = []map[string]any{}
			}
		}
		if turns == nil {
			turns = []map[string]any{}
		}
		out = append(out, map[string]any{
			"id":            t.ID,
			"lead_id":       t.LeadID,
			"lead_name":     leadName,
			"phone":         lead.Phone,
			"duration_s":    t.CallDurationS,
			"tts_language":  t.TTSLanguage,
			"created_at":    t.CreatedAt,
			"recording_url": t.RecordingURL,
			"transcript":    turns,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/leads/{id}/draft-email — Phase 4
// Asks Gemini to draft a personalised follow-up email for the lead.
func (s *Server) draftLeadEmail(w http.ResponseWriter, r *http.Request) {
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	lead, err := s.db.GetLeadByID(id)
	if err != nil || lead == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	// Gather last transcript for context (optional)
	transcriptContext := ""
	if transcripts, err := s.db.GetTranscriptsByLead(id); err == nil && len(transcripts) > 0 {
		transcriptContext = "\n\nLast call transcript (JSON): " + string(transcripts[0].Transcript)
	}

	name := strings.TrimSpace(lead.FirstName + " " + lead.LastName)
	prompt := fmt.Sprintf(`Draft a short, professional follow-up email to %s (phone: %s).
Interest: %s%s

The email should:
- Greet them by first name
- Reference the recent phone call
- Reinforce the value proposition
- Include a clear call-to-action
- Be concise (under 150 words)

Return ONLY the email body text, no subject line.`, name, lead.Phone, lead.Interest, transcriptContext)

	draft, err := s.llmProvider.GenerateResponse(r.Context(), prompt,
		[]llm.ChatMessage{{Role: "user", Text: "Write follow-up email"}}, 300)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email_draft": draft})
}

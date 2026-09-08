package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/llm"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/go-sql-driver/mysql"
)

// stripPhoneDigits removes all non-digit characters from a phone number.
func stripPhoneDigits(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizePhone converts an Indian phone number input (mobile or landline)
// into a canonical 10-digit form stored in the database:
//   - 10-digit mobile/landline: returned as-is
//   - 11-digit number starting with 0 (landline STD code): strips the leading 0
//   - 12-digit number starting with 91 or +91: strips the country code
//
// If the input is not a valid Indian phone number, it returns "".
func normalizePhone(p string) string {
	digits := stripPhoneDigits(p)

	// Domestic format with trunk prefix, e.g. 01112345678 or 09876543210.
	if strings.HasPrefix(digits, "0") {
		digits = strings.TrimPrefix(digits, "0")
	}

	// Country-code prefix, e.g. 919876543210 or 911112345678.
	if strings.HasPrefix(digits, "91") && len(digits) == 12 {
		digits = digits[2:]
	}

	if len(digits) == 10 {
		return digits
	}
	return ""
}

// isValidPhone accepts valid Indian mobile and landline numbers in common
// formats: 10 digits, 0-prefixed domestic, +91/91-prefixed, and with
// spaces/dashes/parentheses. It mirrors the relaxed frontend validation.
func isValidPhone(p string) bool {
	return normalizePhone(p) != ""
}

// ── GET /api/leads/sample-csv ─────────────────────────────────────────────────
// Returns a downloadable CSV template showing the expected import format.

// @Summary     Download sample CSV
// @Description Returns a downloadable CSV template for bulk lead import.
// @Tags        leads
// @Produce     text/csv
// @Security    BearerAuth
// @Success     200  {file}    binary
// @Failure     401  {object}  ErrorResponse
// @Router      /api/leads/sample-csv [get]
func (s *Server) sampleCSV(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="sample_leads.csv"`)
	wr := csv.NewWriter(w)
	// Only the header row — no sample data, so no default names leak into imports.
	_ = wr.Write([]string{"first_name", "last_name", "phone", "company", "source"})
	wr.Flush()
}

// ── GET /api/leads/export ─────────────────────────────────────────────────────
// Streams all org leads as a downloadable CSV file.

// @Summary     Export all leads
// @Description Streams all org leads as a downloadable CSV file.
// @Tags        leads
// @Produce     text/csv
// @Security    BearerAuth
// @Success     200  {file}    binary
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/export [get]
func (s *Server) exportLeads(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.export") {
		return
	}
	ac := getAuth(r)
	execIDs, apply, err := s.leadAccessExecIDs(ac)
	if err != nil {
		s.logger.Sugar().Errorw("exportLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	leads, err := s.db.GetAllLeads(ac.OrgID, execIDs, apply)
	if err != nil {
		s.logger.Sugar().Errorw("exportLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="leads_export.csv"`)
	wr := csv.NewWriter(w)
	_ = wr.Write([]string{
		"id", "first_name", "last_name", "phone", "company", "source",
		"status", "interest", "follow_up_note", "external_id", "crm_provider", "created_at",
	})
	for _, l := range leads {
		_ = wr.Write([]string{
			strconv.FormatInt(l.ID, 10),
			l.FirstName, l.LastName, l.Phone, l.Company, l.Source,
			l.Status, l.Interest, l.FollowUpNote, l.ExternalID, l.CRMProvider, l.CreatedAt,
		})
	}
	wr.Flush()
}

// ── GET /api/leads ────────────────────────────────────────────────────────────

// PaginatedLeadsResponse is the shape returned by listLeads.
type PaginatedLeadsResponse struct {
	Leads []db.Lead `json:"leads"`
	Total int64     `json:"total"`
	Page  int64     `json:"page"`
	Limit int64     `json:"limit"`
}

// @Summary     List leads
// @Description Returns a paginated list of leads for the authenticated user's org.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       page   query  int  false  "Page number (1-based)"  default(1)
// @Param       limit  query  int  false  "Page size"              default(100)
// @Success     200  {object}  PaginatedLeadsResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads [get]
func (s *Server) listLeads(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if !s.requirePermission(w, r, "crm.view") {
		return
	}
	page, limit := parsePagination(r, 100, 500)
	offset := (page - 1) * limit

	var excludeCampaignID int64
	if v := r.URL.Query().Get("exclude_campaign_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			excludeCampaignID = id
		}
	}

	execIDs, apply, err := s.leadAccessExecIDs(ac)
	if err != nil {
		s.logger.Sugar().Errorw("listLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	leads, err := s.db.GetLeadsPaginated(ac.OrgID, limit, offset, excludeCampaignID, execIDs, apply)
	if err != nil {
		s.logger.Sugar().Errorw("listLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	total, err := s.db.CountLeads(ac.OrgID, excludeCampaignID, execIDs, apply)
	if err != nil {
		s.logger.Sugar().Errorw("listLeads count", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, PaginatedLeadsResponse{
		Leads: emptyJSON(leads),
		Total: total,
		Page:  page,
		Limit: limit,
	})
}

// ── GET /api/leads/search?q=... ───────────────────────────────────────────────

// @Summary     Search leads
// @Description Full-text search across leads in the org by name, phone, company, or source.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       q  query  string  true  "Search query"
// @Success     200  {array}   db.Lead
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/search [get]
func (s *Server) searchLeads(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if !s.requirePermission(w, r, "crm.view") {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q query param required")
		return
	}
	var excludeCampaignID int64
	if v := r.URL.Query().Get("exclude_campaign_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			excludeCampaignID = id
		}
	}
	execIDs, apply, err := s.leadAccessExecIDs(ac)
	if err != nil {
		s.logger.Sugar().Errorw("searchLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	leads, err := s.db.SearchLeads(q, ac.OrgID, excludeCampaignID, execIDs, apply)
	if err != nil {
		s.logger.Sugar().Errorw("searchLeads", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(leads))
}

// ── GET /api/leads/search-campaigns?q=... ─────────────────────────────────────

// @Summary     Search leads with campaign info
// @Description Searches leads by name/phone within the org and returns one row
// @Description per campaign the lead belongs to. Accepts an optional comma-separated
// @Description status filter.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       q       query  string  true   "Search query"
// @Param       status  query  string  false  "Comma-separated statuses"
// @Success     200     {array}  db.LeadWithCampaign
// @Failure     400     {object} ErrorResponse
// @Failure     401     {object} ErrorResponse
// @Failure     500     {object} ErrorResponse
// @Router      /api/leads/search-campaigns [get]
func (s *Server) searchLeadsWithCampaigns(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if !s.requirePermission(w, r, "crm.view") {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q query param required")
		return
	}
	var statuses []string
	if s := r.URL.Query().Get("status"); s != "" {
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				statuses = append(statuses, p)
			}
		}
	}
	execIDs, apply, err := s.leadAccessExecIDs(ac)
	if err != nil {
		s.logger.Sugar().Errorw("searchLeadsWithCampaigns", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	leads, err := s.db.SearchLeadsWithCampaigns(q, ac.OrgID, statuses, execIDs, apply)
	if err != nil {
		s.logger.Sugar().Errorw("searchLeadsWithCampaigns", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(leads))
}

// ── POST /api/leads ───────────────────────────────────────────────────────────

type leadCreateRequest struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Phone       string `json:"phone"`
	Company     string `json:"company"`
	Source      string `json:"source"`
	Interest    string `json:"interest"`
	ExecutiveID int64  `json:"executive_id"`
}

// @Summary     Create lead
// @Description Creates a new CRM lead for the org.
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body  body      leadCreateRequest  true  "Lead data"
// @Success     201   {object}  IDResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     409   {object}  ErrorResponse  "phone already exists"
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads [post]
func (s *Server) createLead(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.create") {
		return
	}
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
	phone := normalizePhone(req.Phone)
	id, err := s.db.CreateLead(req.FirstName, req.LastName, phone, req.Source, req.Interest, req.Company, req.ExecutiveID, ac.OrgID)
	if err != nil {
		if isDuplicateEntryError(err) {
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
	} else if !nameHasAllowedChars(name) {
		fields["first_name"] = "Name must contain at least one letter"
	}
	if strings.TrimSpace(phone) == "" {
		fields["phone"] = "Phone is required"
	} else if !isValidPhone(phone) {
		fields["phone"] = "Enter a valid Indian phone number (e.g. 9876543210 or 01112345678)"
	}
	return fields
}

// nameHasAllowedChars accepts names made of ASCII letters, digits, and common
// punctuation (space, apostrophe, hyphen, dot). Requires at least one letter.
func nameHasAllowedChars(s string) bool {
	hasLetter := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= '0' && r <= '9':
			// allowed
		case r == ' ', r == '\'', r == '-', r == '.':
			// allowed
		default:
			return false
		}
	}
	return hasLetter
}

func isDuplicateEntryError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "constraint failed") ||
		strings.Contains(msg, "1062")
}

// ── GET /api/leads/{id} ───────────────────────────────────────────────────────

// @Summary     Get lead
// @Description Returns a single lead by ID.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Lead ID"
// @Success     200  {object}  db.Lead
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/{id} [get]
func (s *Server) getLead(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.view") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	lead := s.requireLeadAccess(w, r, id)
	if lead == nil {
		return
	}
	writeJSON(w, http.StatusOK, lead)
}

// ── PUT /api/leads/{id} ───────────────────────────────────────────────────────

type leadUpdateRequest struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Phone       string `json:"phone"`
	Company     string `json:"company"`
	Source      string `json:"source"`
	Interest    string `json:"interest"`
	ExecutiveID int64  `json:"executive_id"`
}

// @Summary     Update lead
// @Description Updates lead fields. All fields are replaced.
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path      int64              true  "Lead ID"
// @Param       body  body      leadUpdateRequest  true  "Updated lead data"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id} [put]
func (s *Server) updateLead(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.edit") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
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
	phone := normalizePhone(req.Phone)
	updated, err := s.db.UpdateLead(id, req.FirstName, req.LastName, phone, req.Source, req.Interest, req.Company, req.ExecutiveID, ac.OrgID)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "1062") {
			writeFieldError(w, http.StatusConflict, "phone number already exists",
				map[string]string{"phone": "Phone number already exists for another lead"})
			return
		}
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

// @Summary     Delete lead
// @Description Permanently deletes a lead from the org.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Lead ID"
// @Success     200  {object}  DeletedResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/{id} [delete]
func (s *Server) deleteLead(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.delete") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
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

// @Summary     Update lead status
// @Description Changes the CRM status of a lead (e.g. New, Interested, Converted).
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path      int64                       true  "Lead ID"
// @Param       body  body      object{status=string}       true  "New status"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id}/status [put]
func (s *Server) updateLeadStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.edit") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	lead := s.requireLeadAccess(w, r, id)
	if lead == nil {
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
	s.store.PublishDomainEvent(r.Context(), rstore.DomainEvent{
		Type:   rstore.EventLeadStatusChanged,
		OrgID:  ac.OrgID,
		LeadID: id,
		Status: body.Status,
	})
	// Log the status change for agent productivity reporting.
	if u, uErr := s.db.GetUserByEmail(ac.Email); uErr == nil && u != nil {
		_ = s.db.LogAgentActivity(u.ID, u.OrgID, 0, id, db.ActivityStatusUpdate, map[string]any{
			"new_status": body.Status,
			"old_status": lead.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── PUT /api/leads/{id}/executive ─────────────────────────────────────────────

// @Summary     Update lead executive
// @Description Assigns or unassigns an executive for a lead in one campaign without re-validating name/phone.
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path      int64                       true  "Lead ID"
// @Param       body  body      object{executive_id=int64,campaign_id=int64}  true  "Executive ID (0 to unassign) and campaign ID"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id}/executive [put]
func (s *Server) updateLeadExecutive(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.assign") {
		return
	}
	ac := getAuth(r)
	if !s.isSuperAdmin(ac.Email) {
		user, err := s.db.GetUserByEmail(ac.Email)
		if err != nil {
			s.logger.Sugar().Errorw("updateLeadExecutive: user lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if user != nil && user.Role == db.RoleExecutive {
			writeError(w, http.StatusForbidden, "executives cannot reassign leads")
			return
		}
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		ExecutiveID int64 `json:"executive_id"`
		CampaignID  int64 `json:"campaign_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.CampaignID <= 0 {
		body.CampaignID = campaignIDFromReferer(r)
		if body.CampaignID <= 0 {
			writeError(w, http.StatusBadRequest, "campaign_id is required")
			return
		}
	}
	campaign, err := s.db.GetCampaignByID(body.CampaignID)
	if err != nil {
		s.logger.Sugar().Errorw("updateLeadExecutive: campaign lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if campaign == nil || (campaign.OrgID != ac.OrgID && !s.isSuperAdmin(ac.Email)) {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	if !s.canViewCampaign(ac, body.CampaignID) {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	lead, err := s.db.GetLeadByID(id)
	if err != nil {
		s.logger.Sugar().Errorw("updateLeadExecutive: lead lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if lead == nil || (lead.OrgID != 0 && lead.OrgID != campaign.OrgID && !s.isSuperAdmin(ac.Email)) {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	executiveID := body.ExecutiveID
	if body.ExecutiveID > 0 {
		executiveID, err = s.db.ResolveExecutiveID(campaign.OrgID, body.ExecutiveID)
		if err != nil {
			s.logger.Sugar().Errorw("updateLeadExecutive: executive lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if executiveID == 0 {
			writeError(w, http.StatusBadRequest, "executive not found")
			return
		}
	}
	err = s.db.UpdateCampaignLeadExecutive(body.CampaignID, id, campaign.OrgID, executiveID)
	if err != nil {
		s.logger.Sugar().Errorw("updateLeadExecutive", "err", err)
		if err.Error() == "lead not found" {
			writeError(w, http.StatusNotFound, "lead not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	if err := s.db.UpdateLeadExecutive(id, campaign.OrgID, 0); err != nil {
		s.logger.Sugar().Warnw("updateLeadExecutive: clear legacy lead executive failed", "err", err, "lead_id", id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func campaignIDFromReferer(r *http.Request) int64 {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return 0
	}
	u, err := url.Parse(ref)
	if err != nil {
		return 0
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "campaigns" {
			continue
		}
		id, err := strconv.ParseInt(parts[i+1], 10, 64)
		if err == nil && id > 0 {
			return id
		}
	}
	return 0
}

// ── PUT /api/leads/{id}/source ────────────────────────────────────────────────

// @Summary     Update lead source
// @Description Updates the source of a lead without re-validating name/phone.
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path      int64                       true  "Lead ID"
// @Param       body  body      object{source=string}       true  "Source value"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id}/source [put]
func (s *Server) updateLeadSource(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.edit") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	updated, err := s.db.UpdateLeadSource(id, body.Source, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("updateLeadSource", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !updated {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── POST /api/leads/{id}/notes ────────────────────────────────────────────────

// @Summary     Add lead note
// @Description Saves a follow-up note against a lead.
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path      int64                   true  "Lead ID"
// @Param       body  body      object{note=string}     true  "Note text (max 5000 chars)"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id}/notes [post]
func (s *Server) updateLeadNote(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.edit") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Reject empty/whitespace-only notes. The Quick Note form submitted blank
	// notes silently before, which polluted the lead history with empty
	// rows. Issue #70.
	trimmed := strings.TrimSpace(body.Note)
	if trimmed == "" {
		writeError(w, http.StatusBadRequest, "note cannot be empty")
		return
	}
	if len(trimmed) > 5000 {
		writeError(w, http.StatusBadRequest, "note is too long (max 5000 characters)")
		return
	}
	if err := s.db.UpdateLeadNote(id, trimmed); err != nil {
		s.logger.Sugar().Errorw("updateLeadNote", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if u, uErr := s.db.GetUserByEmail(ac.Email); uErr == nil && u != nil {
		_ = s.db.LogAgentActivity(u.ID, u.OrgID, 0, id, db.ActivityNote, map[string]any{
			"note_preview": trimmed,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── PUT /api/leads/{id}/disposition ───────────────────────────────────────────

// leadDispositionRequest is the payload for saving a post-call disposition.
type leadDispositionRequest struct {
	Status     string `json:"status"`
	Note       string `json:"note"`
	FollowUpAt string `json:"follow_up_at"`
}

// @Summary     Save lead disposition
// @Description Atomically updates lead status, follow-up note and follow-up datetime after a call. Used by the browser auto-dial "Save & Next" flow.
// @Tags        leads
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id    path  int64                   true  "Lead ID"
// @Param       body  body  leadDispositionRequest  true  "Disposition data"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id}/disposition [put]
func (s *Server) updateLeadDisposition(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.edit") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
		return
	}
	var body leadDispositionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.Status) == "" {
		writeError(w, http.StatusBadRequest, "status required")
		return
	}
	if len(strings.TrimSpace(body.Note)) > 5000 {
		writeError(w, http.StatusBadRequest, "note is too long (max 5000 characters)")
		return
	}
	if err := s.db.UpdateLeadDisposition(id, strings.TrimSpace(body.Status), strings.TrimSpace(body.Note), strings.TrimSpace(body.FollowUpAt)); err != nil {
		s.logger.Sugar().Errorw("updateLeadDisposition", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.store.PublishDomainEvent(r.Context(), rstore.DomainEvent{
		Type:   rstore.EventLeadStatusChanged,
		OrgID:  ac.OrgID,
		LeadID: id,
		Status: strings.TrimSpace(body.Status),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── POST /api/leads/import-csv ────────────────────────────────────────────────
// Accepts multipart/form-data with a "file" field containing a CSV.
// CSV columns (header row): first_name,last_name,phone,company,source

// @Summary     Import leads from CSV
// @Description Accepts a multipart/form-data CSV upload with columns: first_name, last_name, phone, company, source.
// @Tags        leads
// @Accept      multipart/form-data
// @Produce     json
// @Security    BearerAuth
// @Param       file  formData  file  true  "CSV file"
// @Success     200   {object}  object{imported=int,errors=[]string}
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/import-csv [post]
func (s *Server) importLeadsCSV(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.import") {
		return
	}
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
	iCompany := idx("company")
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
		phone := normalizePhone(get(rec, iPhone))
		if phone == "" {
			skipped = append(skipped, fmt.Sprintf("row %d: phone %q is not a valid Indian number", rowIdx+2, get(rec, iPhone)))
			continue
		}
		rows = append(rows, db.LeadImportRow{
			Row:       rowIdx + 2,
			FirstName: get(rec, iFirst),
			LastName:  get(rec, iLast),
			Phone:     phone,
			Company:   get(rec, iCompany),
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

// @Summary     Get lead documents
// @Description Returns all uploaded documents attached to a lead.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Lead ID"
// @Success     200  {array}   db.Document
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/{id}/documents [get]
func (s *Server) getLeadDocuments(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.view") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
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

// @Summary     Upload lead document
// @Description Uploads a file and attaches it to a lead.
// @Tags        leads
// @Accept      multipart/form-data
// @Produce     json
// @Security    BearerAuth
// @Param       id    path      int64  true  "Lead ID"
// @Param       file  formData  file   true  "Document file"
// @Success     201   {object}  object{url=string}
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/leads/{id}/documents [post]
func (s *Server) uploadLeadDocument(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "crm.edit") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
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

// @Summary     Get transcript review
// @Description Returns the AI-generated call review for a specific transcript.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Transcript ID"
// @Success     200  {object}  object
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/transcripts/{id}/review [get]
func (s *Server) getTranscriptReview(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.transcripts") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireTranscriptAccess(w, r, id) == nil {
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
	// Hide legacy degenerate rows (score 0 AND no commentary text at all).
	// Those render as a misleading "0/5 neutral / no appointment" card with no
	// real analysis underneath; treat them as not-yet-analyzed so the UI hides
	// the card and the user can click 🔄 Regenerate to produce a real one.
	if review.QualityScore <= 0 &&
		review.WhatWentWell == "" && review.WhatWentWrong == "" &&
		review.FailureReason == "" && review.PromptImprovementSuggestion == "" &&
		review.Summary == "" && review.Insights == "" {
		writeError(w, http.StatusNotFound, "no usable review")
		return
	}
	writeJSON(w, http.StatusOK, review)
}


// ── POST /api/transcripts/{id}/conclusion ────────────────────────────────────
//
// (Re)generates the AI conclusion for a single transcript on demand and
// returns the full CallReview row. Unlike the post-call pipeline this path
// runs for EVERY interaction with at least one turn — no 10-second floor,
// no "skip one-sided calls" gate. The UI now wants a visible conclusion on
// ── GET /api/leads/{id}/transcripts ───────────────────────────────────────────

// @Summary     Get lead transcripts
// @Description Returns all call transcripts for a lead.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Lead ID"
// @Success     200  {array}   object
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/{id}/transcripts [get]
func (s *Server) getLeadTranscripts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.transcripts") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	campaignID, _ := strconv.ParseInt(r.URL.Query().Get("campaign_id"), 10, 64)
	if campaignID > 0 {
		ac := getAuth(r)
		campaign, err := s.db.GetCampaignByID(campaignID)
		if err != nil {
			s.logger.Sugar().Errorw("getLeadTranscripts: campaign", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if campaign == nil || campaign.OrgID != ac.OrgID || !s.canAccessCampaignLead(ac, campaignID, id) {
			writeError(w, http.StatusNotFound, "lead not found")
			return
		}
	} else if s.requireLeadAccess(w, r, id) == nil {
		return
	}
	transcripts, err := s.db.GetTranscriptsByLead(id)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadTranscripts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if campaignID > 0 {
		filtered := transcripts[:0]
		for _, t := range transcripts {
			if t.CampaignID == campaignID {
				filtered = append(filtered, t)
			}
		}
		transcripts = filtered
	}
	writeJSON(w, http.StatusOK, emptyJSON(transcripts))
}

// ── GET /api/leads/{id}/interactions ──────────────────────────────────────────

// @Summary     Get lead interaction timeline
// @Description Returns a unified timeline of all interactions for a lead: creation, notes, calls, scheduled calls, and WhatsApp messages.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Lead ID"
// @Success     200  {object}  db.InteractionTimeline
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/leads/{id}/interactions [get]
func (s *Server) getLeadInteractions(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.transcripts") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.requireLeadAccess(w, r, id) == nil {
		return
	}
	timeline, err := s.db.GetLeadInteractions(ac.OrgID, id)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadInteractions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if timeline == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}
	writeJSON(w, http.StatusOK, timeline)
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
// @Summary     Get lead calls by phone
// @Description Returns all calls for the lead matching the given phone number, combined with recording URL and transcript.
// @Tags        leads
// @Produce     json
// @Security    BearerAuth
// @Param       phone  path      string  true  "Indian phone number"
// @Success     200    {array}   object
// @Failure     400    {object}  ErrorResponse
// @Failure     401    {object}  ErrorResponse
// @Failure     500    {object}  ErrorResponse
// @Router      /api/leads/by-phone/{phone}/calls [get]
func (s *Server) getLeadCallsByPhone(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.transcripts") {
		return
	}
	phone := normalizePhone(strings.TrimSpace(r.PathValue("phone")))
	if phone == "" {
		writeError(w, http.StatusBadRequest, "phone required")
		return
	}

	ac := getAuth(r)
	execIDs, apply, err := s.leadAccessExecIDs(ac)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadCallsByPhone", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	lead, err := s.db.GetLeadByPhoneOrg(phone, ac.OrgID, execIDs, apply)
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

// ── GET /api/leads/by-name/{name}/calls ──────────────────────────────────────
//
// Like getLeadCallsByPhone but matches by first/last name. A name can match
// multiple leads (think two Harshas in the same org), so the response groups
// calls under each matched lead instead of flattening — otherwise a caller
// can't tell whose transcript is whose.
//
// Response shape:
//
//	[
//	  {
//	    "lead_id":   2,
//	    "lead_name": "Harsha",
//	    "phone":     "9177007429",
//	    "calls": [
//	      { "id": 259, "duration_s": 3.98, "tts_language": "te",
//	        "created_at": "...", "recording_url": "...",
//	        "transcript": [ {"role":"agent","text":"..."}, ... ] },
//	      ...
//	    ]
//	  },
//	  ...
//	]
//
// Org-scoped — only matches leads in the caller's org. Empty array (200 OK)
// when no name matches, same as a matched lead with no calls.
func (s *Server) getLeadCallsByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	ac := getAuth(r)
	leads, err := s.db.SearchLeads(name, ac.OrgID, 0, nil, false)
	if err != nil {
		s.logger.Sugar().Errorw("getLeadCallsByName: lookup", "err", err, "name", name)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(leads) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	out := make([]map[string]any, 0, len(leads))
	for _, lead := range leads {
		transcripts, err := s.db.GetTranscriptsByLead(lead.ID)
		if err != nil {
			s.logger.Sugar().Errorw("getLeadCallsByName: transcripts", "err", err, "lead_id", lead.ID)
			continue
		}
		leadName := strings.TrimSpace(lead.FirstName + " " + lead.LastName)
		calls := make([]map[string]any, 0, len(transcripts))
		for _, t := range transcripts {
			var turns []map[string]any
			if len(t.Transcript) > 0 {
				if err := json.Unmarshal(t.Transcript, &turns); err != nil {
					turns = []map[string]any{}
				}
			}
			if turns == nil {
				turns = []map[string]any{}
			}
			calls = append(calls, map[string]any{
				"id":            t.ID,
				"duration_s":    t.CallDurationS,
				"tts_language":  t.TTSLanguage,
				"created_at":    t.CreatedAt,
				"recording_url": t.RecordingURL,
				"transcript":    turns,
			})
		}
		out = append(out, map[string]any{
			"lead_id":   lead.ID,
			"lead_name": leadName,
			"phone":     lead.Phone,
			"calls":     calls,
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
	lead := s.requireLeadAccess(w, r, id)
	if lead == nil {
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

// POST /api/leads/{id}/generate-followup-note
// Generates an AI follow-up note for a manual call based on call time, duration, and transcript.
func (s *Server) generateFollowupNote(w http.ResponseWriter, r *http.Request) {
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	lead := s.requireLeadAccess(w, r, id)
	if lead == nil {
		return
	}

	// Build context from most recent call transcript
	callContext := ""
	transcriptRecordingURL := ""
	recordingFilename := ""
	if transcripts, err := s.db.GetTranscriptsByLead(id); err == nil && len(transcripts) > 0 {
		t := transcripts[0]
		callContext += fmt.Sprintf("Call time: %s\n", t.CreatedAt)
		if t.CallDurationS > 0 {
			mins := int(t.CallDurationS) / 60
			secs := int(t.CallDurationS) % 60
			if mins > 0 {
				callContext += fmt.Sprintf("Call duration: %dm %ds\n", mins, secs)
			} else {
				callContext += fmt.Sprintf("Call duration: %ds\n", secs)
			}
		}
		if t.RecordingURL != "" {
			transcriptRecordingURL = t.RecordingURL
			parts := strings.Split(t.RecordingURL, "/")
			recordingFilename = parts[len(parts)-1]
		}
		// Include transcript turns if it's an AI call (not a human-call stub)
		var turns []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		}
		if json.Unmarshal(t.Transcript, &turns) == nil {
			var sb strings.Builder
			for _, turn := range turns {
				if turn.Role == "system" {
					continue
				}
				sb.WriteString(turn.Role + ": " + turn.Text + "\n")
			}
			if sb.Len() > 0 {
				callContext += "Transcript:\n" + sb.String()
			}
		}
	}

	name := strings.TrimSpace(lead.FirstName + " " + lead.LastName)
	prompt := fmt.Sprintf(`You are a sales assistant. Generate a concise follow-up note (3-5 sentences) for a sales agent after a call with %s (phone: %s).
Interest: %s

%s
The note should summarise:
- When the call happened and how long it lasted (if known)
- Key points discussed or outcome
- Recommended next action

Return ONLY the note text, no labels or headers.`, name, lead.Phone, lead.Interest, callContext)

	note, err := s.llmProvider.GenerateResponse(r.Context(), prompt,
		[]llm.ChatMessage{{Role: "user", Text: "Generate follow-up note"}}, 250)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"note":               strings.TrimSpace(note),
		"recording_url":      transcriptRecordingURL,
		"recording_filename": recordingFilename,
	})
}

// ── POST /api/transcripts/{id}/conclusion ────────────────────────────────────
//
// (Re)generates the AI conclusion for a single transcript on demand and
// returns the full CallReview row. Idempotent: if a review already exists
// with prose it is returned as-is unless ?force=1 is passed.
// Returns 204 when the transcript has no turns to analyse.
func (s *Server) postTranscriptConclusion(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.transcripts") {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	force := r.URL.Query().Get("force") == "1"

	if s.recordingSvc == nil || s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "conclusion generation not available on this server")
		return
	}

	t, err := s.db.GetTranscriptByID(id)
	if err != nil {
		s.logger.Sugar().Errorw("postTranscriptConclusion: load transcript", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "transcript not found")
		return
	}
	ac := getAuth(r)
	if t.LeadID > 0 {
		if !s.canAccessLead(ac, t.LeadID) {
			writeError(w, http.StatusNotFound, "transcript not found")
			return
		}
	} else if t.OrgID != ac.OrgID {
		writeError(w, http.StatusNotFound, "transcript not found")
		return
	}

	// Return cached review unless force=1 — makes modal opens cheap.
	if !force {
		if existing, _ := s.db.GetCallReviewByTranscript(id); existing != nil &&
			(existing.Summary != "" || existing.WhatWentWell != "" || existing.WhatWentWrong != "" ||
				existing.FailureReason != "" || existing.Insights != "") {
			writeJSON(w, http.StatusOK, existing)
			return
		}
	}

	var turns []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(t.Transcript, &turns); err != nil {
		// Handle legacy capitalised keys: {Role, Text}
		var alt []struct {
			Role string `json:"Role"`
			Text string `json:"Text"`
		}
		if err2 := json.Unmarshal(t.Transcript, &alt); err2 != nil {
			writeError(w, http.StatusUnprocessableEntity, "transcript is not valid JSON turns")
			return
		}
		for _, a := range alt {
			turns = append(turns, struct {
				Role string `json:"role"`
				Text string `json:"text"`
			}{Role: a.Role, Text: a.Text})
		}
	}

	if len(turns) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	history := make([]llm.ChatMessage, 0, len(turns))
	for _, tn := range turns {
		role := "user"
		if strings.EqualFold(tn.Role, "AI") || strings.EqualFold(tn.Role, "model") || strings.EqualFold(tn.Role, "agent") {
			role = "model"
		}
		history = append(history, llm.ChatMessage{Role: role, Text: tn.Text})
	}

	a, err := s.recordingSvc.AnalyzeCall(r.Context(), history)
	if err != nil {
		s.logger.Sugar().Warnw("postTranscriptConclusion: LLM analysis failed", "id", id, "err", err)
		writeError(w, http.StatusBadGateway, "AI analysis failed: "+err.Error())
		return
	}

	review := &db.CallReview{
		TranscriptID:                id,
		OrgID:                       t.OrgID,
		QualityScore:                a.QualityScore,
		Sentiment:                   a.Sentiment,
		AppointmentBooked:           a.AppointmentBooked,
		FailureReason:               a.FailureReason,
		WhatWentWell:                a.WhatWentWell,
		WhatWentWrong:               a.WhatWentWrong,
		Summary:                     a.Summary,
		Insights:                    a.Insights,
		PromptImprovementSuggestion: a.PromptImprovementSuggestion,
	}
	if err := s.db.SaveCallReview(review); err != nil {
		s.logger.Sugar().Warnw("postTranscriptConclusion: save review failed", "id", id, "err", err)
	}
	if saved, _ := s.db.GetCallReviewByTranscript(id); saved != nil {
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeJSON(w, http.StatusOK, review)
}

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
	rstore "github.com/globussoft/callified-backend/internal/redis"
)

// dialErrorStatus maps a dial.Initiator error to the right HTTP status code
// so the frontend can distinguish "billing problem — show recharge prompt"
// from a generic provider failure. Sentinel errors live in package dial.
func dialErrorStatus(err error) int {
	switch {
	case errors.Is(err, dial.ErrInsufficientCredits):
		return http.StatusPaymentRequired // 402 — Razorpay top-up flow
	case errors.Is(err, dial.ErrDND), errors.Is(err, dial.ErrCallHours):
		return http.StatusConflict // 409 — request can't be served as-is
	default:
		return http.StatusBadGateway // 502 — upstream provider error
	}
}

// userIDForDial returns the authenticated user's ID when the caller is an
// Agent or TeamLeader so that the initiator can prefer their personal provider
// account. Admins/SuperAdmins return 0 so the campaign/org default is used.
func userIDForDial(ac AuthClaims) int64 {
	if ac.UserID == 0 {
		return 0
	}
	if db.IsAgentLikeRole(ac.Role) || ac.Role == db.RoleTeamLeader {
		return ac.UserID
	}
	return 0
}

// POST /api/dial/{lead_id}
// @Summary     Dial lead
// @Description Initiates an immediate outbound call to a specific lead.
// @Tags        dialing
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       lead_id  path      int64                       true   "Lead ID"
// @Param       body     body      object{campaign_id=int64}   false  "Optional campaign context"
// @Success     200   {object}  BoolResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     402   {object}  ErrorResponse  "insufficient credits"
// @Failure     404   {object}  ErrorResponse
// @Failure     409   {object}  ErrorResponse  "DND or outside call hours"
// @Failure     502   {object}  ErrorResponse  "provider error"
// @Router      /api/dial/{lead_id} [post]
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

	ac := getAuth(r)

	var body struct {
		CampaignID int64 `json:"campaign_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.CampaignID > 0 && !s.canViewCampaign(ac, body.CampaignID) {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	if body.CampaignID > 0 {
		if !s.canAccessCampaignLead(ac, body.CampaignID, lead.ID) {
			writeError(w, http.StatusNotFound, "lead not found")
			return
		}
	} else if !s.canAccessLead(ac, lead.ID) {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	vs, _ := s.db.GetCampaignVoiceSettings(body.CampaignID)

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
		UserEmail:   ac.Email,
		UserID:      userIDForDial(ac),
	}

	if _, err := s.initiator.Initiate(r.Context(), data); err != nil {
		s.logger.Warn("dialLead: initiate failed",
			zap.Int64("lead_id", leadID), zap.Error(err))
		writeError(w, dialErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"dialed": true})
}

// campaignDialLead dials a specific lead within a campaign context.
// POST /api/campaigns/{id}/dial/{lead_id}
// @Summary     Dial campaign lead
// @Description Initiates an immediate call to a specific lead using the campaign's voice settings. Requires Admin or Agent role.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id       path  int64  true  "Campaign ID"
// @Param       lead_id  path  int64  true  "Lead ID"
// @Success     200  {object}  BoolResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     402  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Failure     409  {object}  ErrorResponse
// @Failure     502  {object}  ErrorResponse
// @Router      /api/campaigns/{id}/dial/{lead_id} [post]
func (s *Server) campaignDialLead(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.make") {
		return
	}
	ac := getAuth(r)
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	campaignID := campaign.ID
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
	if !s.canAccessCampaignLead(ac, campaignID, lead.ID) {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	var body struct {
		ExotelAccountID int64 `json:"exotel_account_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)

	data := dial.CallData{
		LeadID:          lead.ID,
		LeadName:        lead.FirstName + " " + lead.LastName,
		LeadPhone:       lead.Phone,
		CampaignID:      campaignID,
		OrgID:           ac.OrgID,
		Interest:        lead.Interest,
		TTSProvider:     vs.TTSProvider,
		TTSVoiceID:      vs.TTSVoiceID,
		TTSLanguage:     vs.TTSLanguage,
		UserEmail:       ac.Email,
		UserID:          userIDForDial(ac),
		ExotelAccountID: body.ExotelAccountID,
	}

	if _, err := s.initiator.Initiate(r.Context(), data); err != nil {
		s.logger.Warn("campaignDialLead: initiate failed",
			zap.Int64("campaign_id", campaignID), zap.Int64("lead_id", leadID), zap.Error(err))
		writeError(w, dialErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"dialed": true})
}

// campaignDialAll queues a campaign's leads for sequential dialing with a
// 30s gap between calls. Ports Python's dial_routes.py:307-377 exactly.
//
//   - ?force=true  → dial EVERY lead (status-agnostic); used by the
//     "Dial All (N)" button to redial leads already in non-new states.
//   - no force     → dial only leads whose status is "new"/"New"; used by
//     the "Dial All New (N)" button. Matches Python's default behaviour.
//
// Previous Go impl hard-coded a status exclusion list (skipping Calling /
// Completed / DND) and ignored `force` entirely. That meant the "Dial All"
// button silently queued zero calls when every lead had already been dialed
// once — which is exactly the reported symptom.
//
// POST /api/campaigns/{id}/dial-all
// @Summary     Dial all campaign leads
// @Description Queues outbound calls for all (or new) leads in a campaign. Requires Admin role.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id     path   int64   true   "Campaign ID"
// @Param       force  query  boolean false  "Set true to dial all leads regardless of status"
// @Success     200  {object}  object{status=string,message=string,queued=int}
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/campaigns/{id}/dial-all [post]
func (s *Server) campaignDialAll(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.make") {
		return
	}
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	campaignID := campaign.ID
	force := r.URL.Query().Get("force") == "true"

	var body struct {
		ExotelAccountID int64 `json:"exotel_account_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	leads, err := s.db.GetCampaignLeads(campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list leads")
		return
	}

	// Python's filter: when not forced, only dial leads with status "new"
	// (case-insensitive). "force=true" bypasses the filter and dials every
	// lead regardless — matches the frontend contract.
	dialable := make([]db.CampaignLead, 0, len(leads))
	for _, l := range leads {
		if force {
			dialable = append(dialable, l)
			continue
		}
		st := strings.ToLower(strings.TrimSpace(l.Status))
		if st == "" || st == "new" {
			dialable = append(dialable, l)
		}
	}
	if len(dialable) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "error",
			"message": "No leads to dial",
			"queued":  0,
		})
		return
	}

	// Refuse to start a new queue while one is still running for this campaign.
	existing, err := s.store.GetDialState(r.Context(), campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read dial state")
		return
	}
	if existing.Running && !existing.Aborted {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "error",
			"message": "A dial queue is already running for this campaign",
			"queued":  existing.QueuedCount - existing.ProcessedCount,
		})
		return
	}

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)
	ac := getAuth(r)
	userID := userIDForDial(ac)
	if userID == 0 {
		userID = ac.UserID
	}

	jobs := make([]rstore.DialJob, 0, len(dialable))
	for _, l := range dialable {
		jobs = append(jobs, rstore.DialJob{
			LeadID:          l.ID,
			LeadName:        l.FirstName + " " + l.LastName,
			LeadPhone:       l.Phone,
			CampaignID:      campaignID,
			OrgID:           ac.OrgID,
			Interest:        l.Interest,
			TTSProvider:     vs.TTSProvider,
			TTSVoiceID:      vs.TTSVoiceID,
			TTSLanguage:     vs.TTSLanguage,
			UserEmail:       ac.Email,
			UserID:          userID,
			ExotelAccountID: body.ExotelAccountID,
			Attempt:         1,
			MaxAttempts:     3,
			EnqueuedAt:      time.Now(),
		})
	}

	state := rstore.DialState{
		CampaignID:     campaignID,
		Running:        true,
		Paused:         false,
		Aborted:        false,
		StartedAt:      time.Now(),
		QueuedCount:    len(jobs),
		ProcessedCount: 0,
		FailedCount:    0,
		RetryCount:     0,
	}

	if err := s.store.EnqueueDialJobs(r.Context(), state, jobs); err != nil {
		s.logger.Error("campaignDialAll: failed to enqueue", zap.Error(err), zap.Int64("campaign_id", campaignID))
		writeError(w, http.StatusInternalServerError, "failed to enqueue dial jobs")
		return
	}

	verb := "new leads"
	if force {
		verb = "leads"
	}
	s.store.EmitCampaignEvent(r.Context(), campaignID, "Campaign", "", "started", fmt.Sprintf("Queued %d %s for dialing", len(jobs), verb))
	s.store.PublishDomainEvent(r.Context(), rstore.DomainEvent{
		Type:       rstore.EventCampaignDialStarted,
		OrgID:      ac.OrgID,
		CampaignID: campaignID,
		Status:     "queued",
		Detail:     fmt.Sprintf("Queued %d %s for dialing", len(jobs), verb),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Queued %d %s for dialing (30s gap between calls)", len(jobs), verb),
		"queued":  len(jobs),
	})
}

// campaignRedialFailed re-dials all leads in the campaign that have a
// "Call Failed*" status. Matches Python's dial_routes.py:239-287 behaviour:
//   - sequential, not parallel (30s gap between calls — prevents carrier spam
//     flags and matches the confirm-dialog the frontend shows users)
//   - emits campaign-level "started" event + per-lead events to the Live
//     Campaign Activity feed
//   - skips DND numbers with a `dnd_skipped` event
//   - returns a user-friendly `message` that the frontend surfaces via alert()
//
// POST /api/campaigns/{id}/redial-failed
// @Summary     Redial failed campaign leads
// @Description Queues outbound calls for all leads in a campaign with "Call Failed" status. Requires Admin role.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id  path      int64  true  "Campaign ID"
// @Success     200  {object}  object{status=string,message=string,queued=int}
// @Failure     400  {object}  ErrorResponse
// @Failure     401  {object}  ErrorResponse
// @Failure     403  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Router      /api/campaigns/{id}/redial-failed [post]
func (s *Server) campaignRedialFailed(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.make") {
		return
	}
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	campaignID := campaign.ID

	leads, err := s.db.GetFailedLeadsInCampaign(campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failed leads")
		return
	}
	if len(leads) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "error",
			"message": "No failed leads to redial",
			"queued":  0,
		})
		return
	}

	existing, err := s.store.GetDialState(r.Context(), campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read dial state")
		return
	}
	if existing.Running && !existing.Aborted {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "error",
			"message": "A dial queue is already running for this campaign",
			"queued":  existing.QueuedCount - existing.ProcessedCount,
		})
		return
	}

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)
	ac := getAuth(r)
	userID := userIDForDial(ac)
	if userID == 0 {
		userID = ac.UserID
	}

	jobs := make([]rstore.DialJob, 0, len(leads))
	for _, lead := range leads {
		jobs = append(jobs, rstore.DialJob{
			LeadID:      lead.ID,
			LeadName:    lead.FirstName + " " + lead.LastName,
			LeadPhone:   lead.Phone,
			CampaignID:  campaignID,
			OrgID:       ac.OrgID,
			Interest:    lead.Interest,
			TTSProvider: vs.TTSProvider,
			TTSVoiceID:  vs.TTSVoiceID,
			TTSLanguage: vs.TTSLanguage,
			UserEmail:   ac.Email,
			UserID:      userID,
			Attempt:     1,
			MaxAttempts: 3,
			EnqueuedAt:  time.Now(),
		})
	}

	state := rstore.DialState{
		CampaignID:     campaignID,
		Running:        true,
		Paused:         false,
		Aborted:        false,
		StartedAt:      time.Now(),
		QueuedCount:    len(jobs),
		ProcessedCount: 0,
		FailedCount:    0,
		RetryCount:     0,
	}

	if err := s.store.EnqueueDialJobs(r.Context(), state, jobs); err != nil {
		s.logger.Error("campaignRedialFailed: failed to enqueue", zap.Error(err), zap.Int64("campaign_id", campaignID))
		writeError(w, http.StatusInternalServerError, "failed to enqueue redial jobs")
		return
	}

	s.store.EmitCampaignEvent(r.Context(), campaignID, "Campaign", "", "started", fmt.Sprintf("Queued %d failed leads for redial", len(jobs)))
	s.store.PublishDomainEvent(r.Context(), rstore.DomainEvent{
		Type:       rstore.EventCampaignDialStarted,
		OrgID:      ac.OrgID,
		CampaignID: campaignID,
		Status:     "queued",
		Detail:     fmt.Sprintf("Queued %d failed leads for redial", len(jobs)),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Queued %d failed leads for redial (30s gap between calls)", len(jobs)),
		"queued":  len(jobs),
	})
}

// campaignDialQueueStatus returns the current Redis-backed dial queue state for
// a campaign: counts, running/paused/aborted flags, and any last error.
//
// GET /api/campaigns/{id}/dial-queue/status
// @Summary     Dial queue status
// @Description Returns the current Redis-backed auto-dial state for a campaign.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id  path  int64  true  "Campaign ID"
// @Success     200  {object}  redis.DialState
// @Router      /api/campaigns/{id}/dial-queue/status [get]
func (s *Server) campaignDialQueueStatus(w http.ResponseWriter, r *http.Request) {
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	state, err := s.store.GetDialState(r.Context(), campaign.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read dial state")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// campaignDialQueuePause pauses an active dial queue.
//
// POST /api/campaigns/{id}/dial-queue/pause
// @Summary     Pause dial queue
// @Description Pauses the Redis-backed auto-dial queue for a campaign.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id  path  int64  true  "Campaign ID"
// @Success     200  {object}  BoolResponse
// @Router      /api/campaigns/{id}/dial-queue/pause [post]
func (s *Server) campaignDialQueuePause(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.make") {
		return
	}
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	if err := s.store.PauseDialQueue(r.Context(), campaign.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pause queue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"paused": true})
}

// campaignDialQueueResume resumes a paused dial queue.
//
// POST /api/campaigns/{id}/dial-queue/resume
// @Summary     Resume dial queue
// @Description Resumes a paused Redis-backed auto-dial queue.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id  path  int64  true  "Campaign ID"
// @Success     200  {object}  BoolResponse
// @Router      /api/campaigns/{id}/dial-queue/resume [post]
func (s *Server) campaignDialQueueResume(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.make") {
		return
	}
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	if err := s.store.ResumeDialQueue(r.Context(), campaign.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resume queue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"resumed": true})
}

// campaignDialQueueAbort stops and drains the dial queue for a campaign.
//
// POST /api/campaigns/{id}/dial-queue/abort
// @Summary     Abort dial queue
// @Description Aborts and drains the Redis-backed auto-dial queue for a campaign.
// @Tags        dialing
// @Produce     json
// @Security    BearerAuth
// @Param       id  path  int64  true  "Campaign ID"
// @Success     200  {object}  BoolResponse
// @Router      /api/campaigns/{id}/dial-queue/abort [post]
func (s *Server) campaignDialQueueAbort(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.make") {
		return
	}
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	if err := s.store.AbortDialQueue(r.Context(), campaign.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to abort queue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"aborted": true})
}

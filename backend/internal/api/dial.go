package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
)

// dialErrorStatus maps a dial.Initiator error to the right HTTP status code
// so the frontend can distinguish "minute balance completed — show popup"
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
// @Failure     402   {object}  ErrorResponse  "minute balance completed"
// @Failure     404   {object}  ErrorResponse
// @Failure     409   {object}  ErrorResponse  "DND or outside call hours"
// @Failure     502   {object}  ErrorResponse  "provider error"
// @Router      /api/dial/{lead_id} [post]
func (s *Server) dialLead(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "calls.dial") {
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
		LeadID:                 lead.ID,
		LeadName:               lead.FirstName + " " + lead.LastName,
		LeadPhone:              lead.Phone,
		CampaignID:             body.CampaignID,
		OrgID:                  ac.OrgID,
		Interest:               lead.Interest,
		TTSProvider:            vs.TTSProvider,
		TTSVoiceID:             vs.TTSVoiceID,
		TTSLanguage:            vs.TTSLanguage,
		MaxCallDurationSeconds: vs.MaxCallDurationSeconds,
		UserEmail:              ac.Email,
		UserID:                 userIDForDial(ac),
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
	if !s.requirePermission(w, r, "calls.dial") {
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
		LeadID:                 lead.ID,
		LeadName:               lead.FirstName + " " + lead.LastName,
		LeadPhone:              lead.Phone,
		CampaignID:             campaignID,
		OrgID:                  ac.OrgID,
		Interest:               lead.Interest,
		TTSProvider:            vs.TTSProvider,
		TTSVoiceID:             vs.TTSVoiceID,
		TTSLanguage:            vs.TTSLanguage,
		MaxCallDurationSeconds: vs.MaxCallDurationSeconds,
		UserEmail:              ac.Email,
		UserID:                 userIDForDial(ac),
		ExotelAccountID:        body.ExotelAccountID,
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
	if !s.requirePermission(w, r, "calls.dial_all") {
		return
	}
	ac := getAuth(r)
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

	executiveIDs, _, err := s.resolveCampaignExecutiveIDs(r, ac, campaignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve lead scope")
		return
	}
	leads, err := s.db.GetCampaignLeadsFiltered(campaignID, executiveIDs)
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

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)

	// Detach from the HTTP request's context — the queue runs for minutes
	// after the HTTP response returns. Using r.Context() would cancel every
	// pending dial the moment the response flushes.
	ctx := context.Background()
	queue := make([]dial.CallData, 0, len(dialable))
	for _, l := range dialable {
		queue = append(queue, dial.CallData{
			LeadID:                 l.ID,
			LeadName:               l.FirstName + " " + l.LastName,
			LeadPhone:              l.Phone,
			CampaignID:             campaignID,
			OrgID:                  ac.OrgID,
			Interest:               l.Interest,
			TTSProvider:            vs.TTSProvider,
			TTSVoiceID:             vs.TTSVoiceID,
			TTSLanguage:            vs.TTSLanguage,
			MaxCallDurationSeconds: vs.MaxCallDurationSeconds,
			UserEmail:              ac.Email,
			ExotelAccountID:        body.ExotelAccountID,
		})
	}

	go func() {
		verb := "new leads"
		if force {
			verb = "leads"
		}
		s.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "",
			"started", fmt.Sprintf("Dialing %d %s", len(queue), verb))
		for i, d := range queue {
			if i > 0 {
				time.Sleep(30 * time.Second)
			}
			if d.OrgID > 0 {
				if isDND, _ := s.db.IsDNDNumber(d.OrgID, d.LeadPhone); isDND {
					s.store.EmitCampaignEvent(ctx, campaignID, d.LeadName, d.LeadPhone,
						"dnd", "on DND list")
					continue
				}
			}
			if _, err := s.initiator.Initiate(ctx, d); err != nil {
				s.logger.Warn("campaignDialAll: lead failed",
					zap.Int64("lead_id", d.LeadID), zap.Error(err))
				// Initiator already emits `failed` on error — no duplicate.
				// Hard stop on completed minute balance — every remaining lead
				// would fail the same way, and we'd flood the activity feed
				// with N copies of the same prompt. Surface it
				// once and bail.
				if errors.Is(err, dial.ErrInsufficientCredits) {
					s.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "",
						"failed", dial.ErrInsufficientCredits.Error())
					return
				}
			}
		}
		s.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "",
			"finished", fmt.Sprintf("Dial queue complete (%d leads)", len(queue)))
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Dialing %d leads (30s gap between calls)", len(queue)),
		"queued":  len(queue),
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
	if !s.requirePermission(w, r, "calls.dial_all") {
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

	vs, _ := s.db.GetCampaignVoiceSettings(campaignID)
	ac := getAuth(r)

	// Copy slice into a simple, independently-owned value before handing it
	// to the background goroutine — r.Context() cancels when this handler
	// returns, but the redial queue runs for minutes. Use a detached ctx.
	ctx := context.Background()
	queue := make([]dial.CallData, 0, len(leads))
	for _, lead := range leads {
		queue = append(queue, dial.CallData{
			LeadID:                 lead.ID,
			LeadName:               lead.FirstName + " " + lead.LastName,
			LeadPhone:              lead.Phone,
			CampaignID:             campaignID,
			OrgID:                  ac.OrgID,
			Interest:               lead.Interest,
			TTSProvider:            vs.TTSProvider,
			TTSVoiceID:             vs.TTSVoiceID,
			TTSLanguage:            vs.TTSLanguage,
			MaxCallDurationSeconds: vs.MaxCallDurationSeconds,
			UserEmail:              ac.Email,
		})
	}

	go func() {
		s.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "",
			"started", fmt.Sprintf("Redialing %d failed leads", len(queue)))
		for i, d := range queue {
			if i > 0 {
				time.Sleep(30 * time.Second)
			}
			// DND check mirrors Python — skip and log to the feed so users
			// can see why the number was held back.
			if d.OrgID > 0 {
				if isDND, _ := s.db.IsDNDNumber(d.OrgID, d.LeadPhone); isDND {
					s.store.EmitCampaignEvent(ctx, campaignID, d.LeadName, d.LeadPhone,
						"dnd", "on DND list")
					continue
				}
			}
			if _, err := s.initiator.Initiate(ctx, d); err != nil {
				s.logger.Warn("campaignRedialFailed: lead failed",
					zap.Int64("lead_id", d.LeadID), zap.Error(err))
				// initiator.Initiate already emits a `failed` event on errors,
				// so no duplicate emit needed here.
				if errors.Is(err, dial.ErrInsufficientCredits) {
					s.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "",
						"failed", dial.ErrInsufficientCredits.Error())
					return
				}
			}
		}
		s.store.EmitCampaignEvent(ctx, campaignID, "Campaign", "",
			"finished", fmt.Sprintf("Redial queue complete (%d leads)", len(queue)))
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": fmt.Sprintf("Redialing %d failed leads (30s gap between calls)", len(queue)),
		"queued":  len(queue),
	})
}

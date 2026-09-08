package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/globussoft/callified-backend/internal/db"
)

// AdminSubscriptionRequest is the payload for creating/updating a subscription.
type AdminSubscriptionRequest struct {
	AdminEmail string    `json:"admin_email"`
	ExpiresAt  time.Time `json:"expires_at"`
	Plan       string    `json:"plan,omitempty"`
	IsActive   bool      `json:"is_active,omitempty"`
	Minutes    *int      `json:"minutes,omitempty"`
}

// AdminSubscriptionResponse is the payload returned for a subscription.
type AdminSubscriptionResponse struct {
	AdminEmail       string    `json:"admin_email"`
	ExpiresAt        time.Time `json:"expires_at"`
	Plan             string    `json:"plan"`
	IsActive         bool      `json:"is_active"`
	Status           string    `json:"status"`
	MinutesAvailable int       `json:"minutes_available"`
}

// isSuperAdmin checks whether the given email is the configured super-admin
// or has the SuperAdmin role in the DB.
func (s *Server) isSuperAdmin(email string) bool {
	if email == "" {
		return false
	}
	if s.cfg.SuperAdminEmail != "" && email == s.cfg.SuperAdminEmail {
		return true
	}
	if s.db != nil {
		if u, err := s.db.GetUserByEmail(email); err == nil && u != nil && u.Role == "SuperAdmin" {
			return true
		}
	}
	return false
}

// requireSuperAdmin gates an endpoint behind the configured super-admin email
// or a user whose DB role is "SuperAdmin". It revalidates the role from the DB
// so role changes take effect immediately.
func (s *Server) requireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		ac := getAuth(r)
		if s.isSuperAdmin(ac.Email) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "forbidden")
	})
}

// @Summary     List all subscriptions
// @Description Super-admin endpoint to fetch all admin subscriptions.
// @Tags        admin
// @Produce     json
// @Success     200   {array}   AdminSubscriptionResponse
// @Failure     403   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/admin/subscriptions [get]
func (s *Server) listAdminSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.db.ListAdminSubscriptions()
	if err != nil {
		s.logger.Sugar().Errorw("listAdminSubscriptions failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list subscriptions")
		return
	}

	now := time.Now().UTC()
	resp := make([]AdminSubscriptionResponse, 0, len(subs))
	for _, sub := range subs {
		statusText := "active"
		if !sub.IsActive {
			statusText = "inactive"
		} else if sub.ExpiresAt.Before(now) || sub.ExpiresAt.Equal(now) {
			statusText = "expired"
		}
		resp = append(resp, AdminSubscriptionResponse{
			AdminEmail:       sub.AdminEmail,
			ExpiresAt:        sub.ExpiresAt,
			Plan:             sub.Plan,
			IsActive:         sub.IsActive,
			Status:           statusText,
			MinutesAvailable: s.minutesAvailableForAdmin(sub.AdminEmail),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Create or update subscription
// @Description Super-admin endpoint to set or extend a subscription for an admin email.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       body  body      AdminSubscriptionRequest  true  "Subscription payload"
// @Success     200   {object}  AdminSubscriptionResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     403   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Router      /api/admin/subscriptions [post]
func (s *Server) createOrUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	var req AdminSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AdminEmail == "" {
		writeError(w, http.StatusBadRequest, "admin_email is required")
		return
	}
	if req.ExpiresAt.IsZero() {
		writeError(w, http.StatusBadRequest, "expires_at is required")
		return
	}
	if req.Plan == "" {
		req.Plan = "standard"
	}
	req.Plan = strings.ToLower(strings.TrimSpace(req.Plan))

	var adminOrgID int64
	if req.Minutes != nil {
		if *req.Minutes < 0 {
			writeError(w, http.StatusBadRequest, "minutes must be zero or greater")
			return
		}
		adminUser, err := s.db.GetUserByEmail(req.AdminEmail)
		if err != nil {
			s.logger.Sugar().Errorw("createOrUpdateSubscription: GetUserByEmail failed", "err", err, "email", req.AdminEmail)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if adminUser == nil || adminUser.OrgID <= 0 || adminUser.Role != db.RoleAdmin {
			writeError(w, http.StatusBadRequest, "admin user not found for minutes update")
			return
		}
		adminOrgID = adminUser.OrgID
	}

	if err := s.db.CreateOrUpdateAdminSubscription(req.AdminEmail, req.ExpiresAt.UTC(), req.Plan, req.IsActive); err != nil {
		s.logger.Sugar().Errorw("createOrUpdateSubscription failed", "err", err, "email", req.AdminEmail)
		writeError(w, http.StatusInternalServerError, "failed to save subscription")
		return
	}

	if req.Minutes != nil {
		if _, err := s.db.SetOrgCreditMinutes(adminOrgID, *req.Minutes, "superadmin-subscription", "Superadmin minutes update"); err != nil {
			s.logger.Sugar().Errorw("createOrUpdateSubscription: SetOrgCreditMinutes failed", "err", err, "email", req.AdminEmail, "org_id", adminOrgID)
			writeError(w, http.StatusInternalServerError, "failed to save minutes")
			return
		}
	}

	// Sync the AI-features flag with the chosen plan:
	//   manual  -> hide AI features (manual calls only)
	//   standard -> show AI features
	if err := s.db.SetUserFeatureFlag(req.AdminEmail, req.Plan == "manual"); err != nil {
		s.logger.Sugar().Errorw("setUserFeatureFlag after subscription save failed", "err", err, "email", req.AdminEmail)
		writeError(w, http.StatusInternalServerError, "failed to sync feature flag")
		return
	}

	status, err := s.db.ValidateAdminSubscription(req.AdminEmail)
	if err != nil {
		s.logger.Sugar().Errorw("ValidateAdminSubscription after save failed", "err", err, "email", req.AdminEmail)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	statusText := "active"
	if !status.Active {
		if status.Expired {
			statusText = "expired"
		} else {
			statusText = "inactive"
		}
	}

	writeJSON(w, http.StatusOK, AdminSubscriptionResponse{
		AdminEmail:       req.AdminEmail,
		ExpiresAt:        req.ExpiresAt.UTC(),
		Plan:             req.Plan,
		IsActive:         req.IsActive,
		Status:           statusText,
		MinutesAvailable: s.minutesAvailableForAdmin(req.AdminEmail),
	})
}

// @Summary     Get subscription
// @Description Super-admin endpoint to fetch a subscription by admin email.
// @Tags        admin
// @Produce     json
// @Param       email  path      string  true  "Admin email"
// @Success     200    {object}  AdminSubscriptionResponse
// @Failure     404    {object}  ErrorResponse
// @Failure     403    {object}  ErrorResponse
// @Router      /api/admin/subscriptions/{email} [get]
func (s *Server) getAdminSubscription(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	sub, err := s.db.GetAdminSubscriptionByEmail(email)
	if err != nil {
		s.logger.Sugar().Errorw("getSubscription failed", "err", err, "email", email)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sub == nil {
		writeError(w, http.StatusNotFound, "subscription not found")
		return
	}

	statusText := "active"
	now := time.Now().UTC()
	if !sub.IsActive {
		statusText = "inactive"
	} else if sub.ExpiresAt.Before(now) || sub.ExpiresAt.Equal(now) {
		statusText = "expired"
	}

	writeJSON(w, http.StatusOK, AdminSubscriptionResponse{
		AdminEmail:       sub.AdminEmail,
		ExpiresAt:        sub.ExpiresAt,
		Plan:             sub.Plan,
		IsActive:         sub.IsActive,
		Status:           statusText,
		MinutesAvailable: s.minutesAvailableForAdmin(sub.AdminEmail),
	})
}

func (s *Server) minutesAvailableForAdmin(email string) int {
	adminUser, err := s.db.GetUserByEmail(email)
	if err != nil || adminUser == nil || adminUser.OrgID <= 0 {
		return 0
	}
	if adminUser.Role != db.RoleAdmin {
		return 0
	}
	credits, err := s.db.GetOrgCredit(adminUser.OrgID)
	if err != nil || credits == nil {
		return 0
	}
	return credits.MinutesAvailable
}

// subscriptionError is a structured error for subscription failures.
type subscriptionError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Plan         string `json:"plan,omitempty"`
	SupportEmail string `json:"support_email,omitempty"`
}

// checkSubscription validates the admin's subscription during login.
func (s *Server) checkSubscription(email string) (*subscriptionError, error) {
	status, err := s.db.ValidateAdminSubscription(email)
	if err != nil {
		return nil, err
	}
	return s.subscriptionErrorFromStatus(status), nil
}

func (s *Server) checkOrgSubscription(orgID int64) (*subscriptionError, error) {
	status, err := s.db.ValidateOrgAdminSubscription(orgID)
	if err != nil {
		return nil, err
	}
	return s.subscriptionErrorFromStatus(status), nil
}

func (s *Server) subscriptionErrorFromStatus(status *db.AdminSubscriptionStatus) *subscriptionError {
	support := s.cfg.SupportEmail
	if support == "" {
		support = "support@callified.ai"
	}
	if !status.Found {
		return &subscriptionError{
			Code:         "SUBSCRIPTION_NOT_FOUND",
			Message:      "No active subscription found for this account. Please contact support to activate your subscription.",
			SupportEmail: support,
		}
	}
	if status.Expired {
		return &subscriptionError{
			Code:         "SUBSCRIPTION_EXPIRED",
			Message:      "Your subscription expired on " + status.ExpiresAt.UTC().Format("2006-01-02") + ". Please renew to continue.",
			ExpiresAt:    status.ExpiresAt.UTC().Format(time.RFC3339),
			Plan:         status.Plan,
			SupportEmail: support,
		}
	}
	if !status.Active {
		return &subscriptionError{
			Code:         "SUBSCRIPTION_INACTIVE",
			Message:      "Your subscription is currently inactive. Please contact support.",
			Plan:         status.Plan,
			SupportEmail: support,
		}
	}
	return nil
}

// writeSubscriptionError sends a 403 response with subscription error details.
func (s *Server) writeSubscriptionError(w http.ResponseWriter, err *subscriptionError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":         err.Message,
		"code":          err.Code,
		"expires_at":    err.ExpiresAt,
		"plan":          err.Plan,
		"support_email": err.SupportEmail,
	})
}

// Ensure db package usage is referenced.
var _ = db.AdminSubscription{}

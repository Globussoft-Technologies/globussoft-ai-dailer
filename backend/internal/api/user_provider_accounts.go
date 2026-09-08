package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

// providerAccountRequest mirrors the fields needed to create/update a
// user-owned provider account. It reuses the validation already in place for
// org-level accounts.
type providerAccountRequest struct {
	Provider   string `json:"provider"`
	Name       string `json:"name"`
	APIKey     string `json:"api_key"`
	APIToken   string `json:"api_token"`
	APISecret  string `json:"api_secret"`
	AccountSID string `json:"account_sid"`
	CallerID   string `json:"caller_id"`
	AppID      string `json:"app_id"`
	AppType    string `json:"app_type"`
	Direction  string `json:"direction"`
	Region     string `json:"region"`
	Subdomain  string `json:"subdomain"`
}

func normalizeProviderAccountRequest(req *providerAccountRequest) {
	if req.Provider == "" {
		req.Provider = "exotel"
	}
	if req.AppType == "" {
		req.AppType = "exoml"
	}
	req.Direction = normalizeProviderDirection(req.Direction)
}

// canManageUserProviderAccounts decides whether the caller may read or mutate
// provider accounts for targetUserID within the caller's org.
//   - Admin: any user in the org.
//   - TeamLeader: themselves or any Agent/Executive they manage.
//   - Agent/Executive: only themselves.
func canManageUserProviderAccounts(caller, target *db.User) bool {
	if caller == nil || target == nil {
		return false
	}
	if caller.OrgID != target.OrgID {
		return false
	}
	switch caller.Role {
	case db.RoleAdmin:
		return true
	case db.RoleTeamLeader:
		if caller.ID == target.ID {
			return true
		}
		return db.IsAgentLikeRole(target.Role) && target.ManagerID != nil && *target.ManagerID == caller.ID
	case db.RoleAgent, db.RoleExecutive:
		return caller.ID == target.ID
	default:
		return false
	}
}

// loadCallerAndTarget resolves the authenticated caller and the target user for
// /api/users/{id}/provider-accounts endpoints. It returns the target user when
// the caller is allowed to manage it; otherwise it writes a 404/403 response and
// returns nil.
func (s *Server) loadCallerAndTarget(w http.ResponseWriter, r *http.Request) (*db.User, *db.User) {
	ac := getAuth(r)
	targetID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return nil, nil
	}

	caller, err := s.db.GetUserByEmail(ac.Email)
	if err != nil || caller == nil {
		s.logger.Sugar().Errorw("loadCallerAndTarget: caller lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "could not resolve caller")
		return nil, nil
	}

	target, err := s.db.GetUserByIDInOrgWithRole(targetID, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("loadCallerAndTarget: target lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, nil
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return nil, nil
	}
	if !canManageUserProviderAccounts(caller, target) {
		writeError(w, http.StatusForbidden, "forbidden")
		return nil, nil
	}
	return caller, target
}

// ── GET /api/users/{id}/provider-accounts ─────────────────────────────────────
// List provider accounts owned by the target user. Admins and TeamLeaders can
// also view their reports' accounts; users can always view their own.
func (s *Server) listUserProviderAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.own") {
		return
	}
	_, target := s.loadCallerAndTarget(w, r)
	if target == nil {
		return
	}
	accounts, err := s.db.GetUserExotelAccounts(target.ID, target.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listUserProviderAccounts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(accounts))
}

// ── POST /api/users/{id}/provider-accounts ─────────────────────────────────────
// Create a provider account owned by the target user.
func (s *Server) createUserProviderAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.own") {
		return
	}
	_, target := s.loadCallerAndTarget(w, r)
	if target == nil {
		return
	}
	var req providerAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	normalizeProviderAccountRequest(&req)
	if errMsg := validateProviderAccount(req.Provider, req.Direction, req.Name, req.APIKey, req.APIToken, req.APISecret, req.AccountSID, req.CallerID, req.AppID); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if req.Provider == "exotel" && req.AppType != "exoml" && req.AppType != "voicebot" {
		writeError(w, http.StatusBadRequest, "app_type must be 'exoml' or 'voicebot'")
		return
	}

	id, err := s.db.CreateUserExotelAccount(target.ID, target.OrgID, req.Provider,
		strings.TrimSpace(req.Name), req.APIKey, req.APIToken, req.APISecret,
		req.AccountSID, req.CallerID, req.AppID, req.AppType, req.Direction, req.Region, req.Subdomain)
	if err != nil {
		s.logger.Sugar().Errorw("createUserProviderAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── PUT /api/users/{id}/provider-accounts/{account_id} ─────────────────────────
// Update a provider account owned by the target user.
func (s *Server) updateUserProviderAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.own") {
		return
	}
	_, target := s.loadCallerAndTarget(w, r)
	if target == nil {
		return
	}
	accountID, err := parseID(r, "account_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account_id")
		return
	}
	var req providerAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	normalizeProviderAccountRequest(&req)
	if errMsg := validateProviderAccount(req.Provider, req.Direction, req.Name, req.APIKey, req.APIToken, req.APISecret, req.AccountSID, req.CallerID, req.AppID); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if req.Provider == "exotel" && req.AppType != "exoml" && req.AppType != "voicebot" {
		writeError(w, http.StatusBadRequest, "app_type must be 'exoml' or 'voicebot'")
		return
	}

	if err := s.db.UpdateUserExotelAccount(accountID, target.ID, target.OrgID, req.Provider,
		strings.TrimSpace(req.Name), req.APIKey, req.APIToken, req.APISecret,
		req.AccountSID, req.CallerID, req.AppID, req.AppType, req.Direction, req.Region, req.Subdomain); err != nil {
		s.logger.Sugar().Errorw("updateUserProviderAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── DELETE /api/users/{id}/provider-accounts/{account_id} ──────────────────────
// Remove a provider account owned by the target user.
func (s *Server) deleteUserProviderAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.own") {
		return
	}
	_, target := s.loadCallerAndTarget(w, r)
	if target == nil {
		return
	}
	accountID, err := parseID(r, "account_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account_id")
		return
	}
	if err := s.db.DeleteUserExotelAccount(accountID, target.ID, target.OrgID); err != nil {
		s.logger.Sugar().Errorw("deleteUserProviderAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── GET /api/users/me/provider-accounts ────────────────────────────────────────
// Convenience endpoint for the authenticated user to list their own accounts
// without needing to know their user ID.
func (s *Server) listMyProviderAccounts(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if !s.requirePermission(w, r, "provider_accounts.own") {
		return
	}
	if ac.UserID == 0 {
		writeError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	accounts, err := s.db.GetUserExotelAccounts(ac.UserID, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listMyProviderAccounts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(accounts))
}

// ── POST /api/users/me/provider-accounts ───────────────────────────────────────
// Convenience endpoint for the authenticated user to create their own account.
func (s *Server) createMyProviderAccount(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if !s.requirePermission(w, r, "provider_accounts.own") {
		return
	}
	if ac.UserID == 0 {
		writeError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	var req providerAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	normalizeProviderAccountRequest(&req)
	if errMsg := validateProviderAccount(req.Provider, req.Direction, req.Name, req.APIKey, req.APIToken, req.APISecret, req.AccountSID, req.CallerID, req.AppID); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if req.Provider == "exotel" && req.AppType != "exoml" && req.AppType != "voicebot" {
		writeError(w, http.StatusBadRequest, "app_type must be 'exoml' or 'voicebot'")
		return
	}
	id, err := s.db.CreateUserExotelAccount(ac.UserID, ac.OrgID, req.Provider,
		strings.TrimSpace(req.Name), req.APIKey, req.APIToken, req.APISecret,
		req.AccountSID, req.CallerID, req.AppID, req.AppType, req.Direction, req.Region, req.Subdomain)
	if err != nil {
		s.logger.Sugar().Errorw("createMyProviderAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

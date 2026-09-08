package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

// userCreateRequest is the payload for Admin/Team Leader direct user creation.
type userCreateRequest struct {
	Email           string                  `json:"email"`
	Password        string                  `json:"password"`
	FullName        string                  `json:"full_name"`
	Role            string                  `json:"role"`
	ManagerID       *int64                  `json:"manager_id"`
	ProviderAccount *providerAccountRequest `json:"provider_account,omitempty"`
}

// userUpdateRequest is the payload for editing a user.
type userUpdateRequest struct {
	FullName  string `json:"full_name"`
	Role      string `json:"role"`
	ManagerID *int64 `json:"manager_id"`
	IsActive  *bool  `json:"is_active"`
}

// normalizeRole validates a dashboard role string.
func normalizeRole(role string) string {
	switch strings.ToLower(role) {
	case "admin":
		return db.RoleAdmin
	case "teamleader", "team_leader", "team leader":
		return db.RoleTeamLeader
	case "agent":
		return db.RoleAgent
	case "executive":
		return db.RoleExecutive
	default:
		return ""
	}
}

// ── GET /api/users ───────────────────────────────────────────────────────────
// Admin-only. Returns all dashboard users in the org with RBAC fields.
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	users, err := s.db.GetUsersByOrg(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listUsers", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(users))
}

// ── POST /api/users ──────────────────────────────────────────────────────────
// Admin-only. Creates a Team Leader or Agent directly with a set password.
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var req userCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.FullName = strings.TrimSpace(req.FullName)
	if req.Email == "" {
		writeFieldError(w, http.StatusBadRequest, "Email is required.", map[string]string{"email": "Email is required"})
		return
	}
	if req.Password == "" {
		writeFieldError(w, http.StatusBadRequest, "Password is required.", map[string]string{"password": "Password is required"})
		return
	}
	role := normalizeRole(req.Role)
	if role == "" {
		writeFieldError(w, http.StatusBadRequest, "Invalid role.", map[string]string{"role": "Role must be Admin, TeamLeader, Agent, or Executive"})
		return
	}
	if role == db.RoleAdmin && req.ManagerID != nil && *req.ManagerID != 0 {
		writeError(w, http.StatusBadRequest, "Admins cannot have a manager")
		return
	}

	existing, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Sugar().Errorw("createUser: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "A user with this email already exists.")
		return
	}

	hash, err := db.HashPassword(req.Password)
	if err != nil {
		s.logger.Sugar().Errorw("createUser: hash", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var managerID *int64
	if req.ManagerID != nil && *req.ManagerID > 0 {
		managerID = req.ManagerID
	}

	id, err := s.db.CreateManagedUser(req.Email, hash, req.FullName, role, ac.OrgID, managerID)
	if err != nil {
		s.logger.Sugar().Errorw("createUser", "err", err)
		writeError(w, http.StatusInternalServerError, "Could not create user. Please try again.")
		return
	}
	s.maybeCreateUserProviderAccount(id, ac.OrgID, req.ProviderAccount)
	if role == db.RoleExecutive {
		_ = s.db.EnsureExecutiveForUser(ac.OrgID, req.FullName, req.Email)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "email": req.Email})
}

// maybeCreateUserProviderAccount creates a personal provider account for a newly
// created user when the request includes one. Validation failures are ignored so
// a bad calling config does not roll back the user creation.
func (s *Server) maybeCreateUserProviderAccount(userID, orgID int64, pa *providerAccountRequest) {
	if pa == nil {
		return
	}
	normalizeProviderAccountRequest(pa)
	if errMsg := validateProviderAccount(pa.Provider, pa.Direction, pa.Name, pa.APIKey, pa.APIToken, pa.APISecret, pa.AccountSID, pa.CallerID, pa.AppID); errMsg != "" {
		return
	}
	if pa.Provider == "exotel" && pa.AppType != "exoml" && pa.AppType != "voicebot" {
		return
	}
	_, _ = s.db.CreateUserExotelAccount(userID, orgID, pa.Provider,
		strings.TrimSpace(pa.Name), pa.APIKey, pa.APIToken, pa.APISecret,
		pa.AccountSID, pa.CallerID, pa.AppID, pa.AppType, pa.Direction, pa.Region, pa.Subdomain)
}

// ── PUT /api/users/{id} ──────────────────────────────────────────────────────
// Admin-only. Edits any user in the org.
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req userUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	target, err := s.db.GetUserByIDInOrgWithRole(id, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("updateUser: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	role := target.Role
	if req.Role != "" {
		role = normalizeRole(req.Role)
		if role == "" {
			writeFieldError(w, http.StatusBadRequest, "Invalid role.", map[string]string{"role": "Role must be Admin, TeamLeader, Agent, or Executive"})
			return
		}
	}

	fullName := target.FullName
	if req.FullName != "" {
		fullName = strings.TrimSpace(req.FullName)
	}

	managerID := target.ManagerID
	if req.ManagerID != nil {
		if *req.ManagerID > 0 {
			managerID = req.ManagerID
		} else {
			managerID = nil
		}
	}
	if role == db.RoleAdmin && managerID != nil {
		writeError(w, http.StatusBadRequest, "Admins cannot have a manager")
		return
	}

	isActive := target.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	if err := s.db.UpdateUser(id, ac.OrgID, fullName, role, managerID, isActive); err != nil {
		s.logger.Sugar().Errorw("updateUser", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if role == db.RoleExecutive {
		_ = s.db.EnsureExecutiveForUser(ac.OrgID, fullName, target.Email)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── DELETE /api/users/{id} ───────────────────────────────────────────────────
// Admin-only. Deletes a user in the org, guarding the last admin.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	caller, err := s.db.GetUserByEmail(ac.Email)
	if err != nil || caller == nil {
		writeError(w, http.StatusInternalServerError, "could not resolve caller")
		return
	}
	target, err := s.db.GetUserByIDInOrgWithRole(id, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("deleteUser: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if target.ID == caller.ID {
		writeError(w, http.StatusForbidden, "you cannot remove your own account")
		return
	}
	if target.Role == db.RoleAdmin {
		count, err := s.db.CountAdminsInOrg(ac.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if count <= 1 {
			writeError(w, http.StatusForbidden, "cannot remove the last remaining admin")
			return
		}
	}
	if err := s.db.DeleteUser(id, ac.OrgID); err != nil {
		s.logger.Sugar().Errorw("deleteUser", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if exec, err := s.db.GetExecutiveByEmail(ac.OrgID, target.Email); err == nil && exec != nil {
		if err := s.db.DeleteExecutive(exec.ID, ac.OrgID); err != nil {
			s.logger.Sugar().Warnw("deleteUser: linked executive delete failed", "err", err)
		}
		if err := s.db.UnassignExecutiveFromLeads(exec.ID, ac.OrgID); err != nil {
			s.logger.Sugar().Warnw("deleteUser: linked executive unassign failed", "err", err)
		}
	} else if err != nil {
		s.logger.Sugar().Warnw("deleteUser: linked executive lookup failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── POST /api/users/{id}/toggle-active ───────────────────────────────────────
// Admin-only. Enables or disables a user account.
func (s *Server) toggleUserActive(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		IsActive bool `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	target, err := s.db.GetUserByIDInOrgWithRole(id, ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("toggleUserActive: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !body.IsActive && target.Role == db.RoleAdmin {
		count, err := s.db.CountAdminsInOrg(ac.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if count <= 1 {
			writeError(w, http.StatusForbidden, "cannot disable the last remaining admin")
			return
		}
	}
	if err := s.db.UpdateUserActive(id, body.IsActive); err != nil {
		s.logger.Sugar().Errorw("toggleUserActive", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── POST /api/users/agent ────────────────────────────────────────────────────
// Team Leader only. Creates an Agent assigned to the calling Team Leader.
func (s *Server) createAgentUnderManager(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	caller, err := s.db.GetUserByEmail(ac.Email)
	if err != nil || caller == nil {
		writeError(w, http.StatusInternalServerError, "could not resolve caller")
		return
	}

	var req struct {
		Email           string                  `json:"email"`
		Password        string                  `json:"password"`
		FullName        string                  `json:"full_name"`
		ProviderAccount *providerAccountRequest `json:"provider_account,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.FullName = strings.TrimSpace(req.FullName)
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}

	existing, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Sugar().Errorw("createAgentUnderManager: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "A user with this email already exists.")
		return
	}

	hash, err := db.HashPassword(req.Password)
	if err != nil {
		s.logger.Sugar().Errorw("createAgentUnderManager: hash", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	managerID := caller.ID
	id, err := s.db.CreateManagedUser(req.Email, hash, req.FullName, db.RoleAgent, ac.OrgID, &managerID)
	if err != nil {
		s.logger.Sugar().Errorw("createAgentUnderManager", "err", err)
		writeError(w, http.StatusInternalServerError, "Could not create user. Please try again.")
		return
	}
	s.maybeCreateUserProviderAccount(id, ac.OrgID, req.ProviderAccount)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "email": req.Email})
}

// ── GET /api/users/my-agents ─────────────────────────────────────────────────
// Team Leader only. Lists Agents managed by the caller.
func (s *Server) listMyAgents(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	caller, err := s.db.GetUserByEmail(ac.Email)
	if err != nil || caller == nil {
		writeError(w, http.StatusInternalServerError, "could not resolve caller")
		return
	}
	agents, err := s.db.GetAgentsByManager(caller.ID)
	if err != nil {
		s.logger.Sugar().Errorw("listMyAgents", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(agents))
}

// ── PUT /api/users/{id}/agent ────────────────────────────────────────────────
// Team Leader only. Updates an Agent they manage (name / active).
func (s *Server) updateManagedAgent(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	caller, err := s.db.GetUserByEmail(ac.Email)
	if err != nil || caller == nil {
		writeError(w, http.StatusInternalServerError, "could not resolve caller")
		return
	}
	managed, err := s.db.IsUserManagedBy(id, caller.ID)
	if err != nil {
		s.logger.Sugar().Errorw("updateManagedAgent: check", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !managed {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	var req struct {
		FullName string `json:"full_name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	target, err := s.db.GetUserByIDInOrgWithRole(id, ac.OrgID)
	if err != nil || target == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	fullName := target.FullName
	if req.FullName != "" {
		fullName = strings.TrimSpace(req.FullName)
	}
	isActive := target.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	if err := s.db.UpdateUser(id, ac.OrgID, fullName, db.RoleAgent, target.ManagerID, isActive); err != nil {
		s.logger.Sugar().Errorw("updateManagedAgent", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// userResponseWithRole builds the user-profile object including RBAC fields.
func userResponseWithRole(u *db.User, orgName string) map[string]any {
	return map[string]any{
		"id":         u.ID,
		"email":      u.Email,
		"full_name":  u.FullName,
		"role":       u.Role,
		"manager_id": u.ManagerID,
		"is_active":  u.IsActive,
		"org_id":     u.OrgID,
		"org_name":   orgName,
		"created_at": u.CreatedAt,
	}
}

// ensureNotLastAdmin is a shared guard for operations that would disable or
// delete the final admin of an org.
func (s *Server) ensureNotLastAdmin(orgID int64, targetRole string, targetID int64, action string) error {
	if targetRole != db.RoleAdmin {
		return nil
	}
	count, err := s.db.CountAdminsInOrg(orgID)
	if err != nil {
		return errors.New("internal error")
	}
	_ = targetID
	_ = action
	if count <= 1 {
		return errors.New("cannot remove or disable the last remaining admin")
	}
	return nil
}

package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

// ── GET /api/dashboard/summary ────────────────────────────────────────────────
// Open to any authenticated role (Admin / Agent / Viewer) so the CRM
// dashboard cards render real numbers even though full /api/campaigns is
// admin-gated. Returns just the 5 aggregate counts — no campaign objects.

func (s *Server) dashboardSummary(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	summary, err := s.db.GetOrgDashboardSummary(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("dashboardSummary", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// ── GET /api/team ─────────────────────────────────────────────────────────────

func (s *Server) listTeam(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	members, err := s.db.GetTeamMembers(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listTeam", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(members))
}

// ── POST /api/team/invite ─────────────────────────────────────────────────────

func (s *Server) inviteTeamMember(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		Email    string `json:"email"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	body.FullName = strings.TrimSpace(body.FullName)
	if body.Email == "" {
		writeError(w, http.StatusBadRequest, "Email is required.")
		return
	}
	if msg := validatePassword(body.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if body.Role == "" {
		body.Role = "Member"
	}
	hash, err := db.HashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id, err := s.db.CreateUserWithRole(body.Email, hash, body.FullName, body.Role, ac.OrgID)
	if err != nil {
		// Surface the specific reason the insert failed instead of a generic
		// "could not create user". The most common case is the unique-email
		// constraint (MySQL 1062); telling the inviter that explicitly avoids
		// the silent "Failed to invite user" that issue #56 reported.
		errMsg := err.Error()
		if strings.Contains(errMsg, "1062") || strings.Contains(errMsg, "Duplicate") {
			writeError(w, http.StatusConflict,
				"A user with this email already exists.")
			return
		}
		s.logger.Sugar().Errorw("inviteTeamMember", "err", err)
		writeError(w, http.StatusInternalServerError, "Could not create user. Please try again.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// validatePassword enforces the org-wide password policy. Returns "" when the
// password is acceptable, or a user-facing reason it isn't.
//
// Rules (issue #56):
//   - At least 8 characters (was 6 — too low for a 2026 baseline)
//   - At most 128 characters (bcrypt truncates at 72 bytes, but we let the
//     user type a passphrase up to 128 and bcrypt's silent truncation is
//     fine for practical purposes — we just guard against absurdly long
//     inputs that could DoS the bcrypt cost)
//   - Not in the small in-memory blocklist of trivially-common passwords
//
// We deliberately do NOT require character classes (NIST 800-63B explicitly
// recommends against the "must have one uppercase, one digit, one symbol"
// nonsense — it pushes users toward predictable patterns like "Password1!").
// Length + breach awareness is the right baseline.
func validatePassword(p string) string {
	if len(p) < 8 {
		return "Password must be at least 8 characters."
	}
	if len(p) > 128 {
		return "Password is too long (max 128 characters)."
	}
	lower := strings.ToLower(p)
	if _, bad := commonPasswords[lower]; bad {
		return "This password is too common. Please choose a stronger one."
	}
	return ""
}

// commonPasswords is a tiny, hard-coded blocklist of the top trivial
// passwords. Keeping it in-process avoids a dependency on an external
// breach-list service for a basic gate; the real defense is bcrypt + the
// 8-char minimum above. Update list in lockstep with whatever the auth
// signup endpoint enforces (so the policy is consistent across surfaces).
var commonPasswords = map[string]struct{}{
	"password": {}, "password1": {}, "password123": {}, "passw0rd": {},
	"12345678": {}, "123456789": {}, "1234567890": {},
	"qwerty": {}, "qwerty123": {}, "qwertyuiop": {},
	"abc12345": {}, "iloveyou": {}, "admin123": {}, "welcome1": {},
	"letmein1": {}, "monkey123": {}, "football": {},
}

// ── PUT /api/team/{id}/role ───────────────────────────────────────────────────

func (s *Server) updateTeamRole(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Role == "" {
		writeError(w, http.StatusBadRequest, "role required")
		return
	}
	if err := s.db.UpdateUserRole(id, body.Role); err != nil {
		s.logger.Sugar().Errorw("updateTeamRole", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// ── DELETE /api/team/{id} ─────────────────────────────────────────────────────

func (s *Server) deleteTeamMember(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// Resolve caller's user row so we can compare IDs (the JWT carries email,
	// not user id) and check the target's role for the last-admin guard. Both
	// must be in the same org. Issue #54.
	caller, err := s.db.GetUserByEmail(ac.Email)
	if err != nil || caller == nil {
		writeError(w, http.StatusInternalServerError, "could not resolve caller")
		return
	}
	target, err := s.db.GetUserByIDInOrg(id, ac.OrgID)
	if err != nil {
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
	if target.Role == "Admin" {
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
		s.logger.Sugar().Errorw("deleteTeamMember", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

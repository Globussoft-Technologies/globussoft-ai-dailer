package api

import (
	"encoding/json"
	"net/http"

	"github.com/globussoft/callified-backend/internal/db"
)

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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
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
		s.logger.Sugar().Errorw("inviteTeamMember", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
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
	if err := s.db.DeleteUser(id, ac.OrgID); err != nil {
		s.logger.Sugar().Errorw("deleteTeamMember", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

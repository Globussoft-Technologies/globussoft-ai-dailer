package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/globussoft/callified-backend/internal/db"
)

// ── POST /api/auth/signup ─────────────────────────────────────────────────────

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	OrgName  string `json:"org_name"`
	Role     string `json:"role"`
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}

	// Check duplicate email
	existing, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Sugar().Errorw("signup: GetUserByEmail", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	// Create org if org_name provided
	var orgID int64
	if req.OrgName != "" {
		orgID, err = s.db.CreateOrganization(req.OrgName)
		if err != nil {
			s.logger.Sugar().Errorw("signup: CreateOrganization", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	hash, err := db.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	role := req.Role
	if role == "" {
		role = "Admin"
	}

	userID, err := s.db.CreateUser(req.Email, hash, req.FullName, role, orgID)
	if err != nil {
		s.logger.Sugar().Errorw("signup: CreateUser", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	token, err := s.mintToken(req.Email, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Match Python auth.py:202 — return a nested `user` object with
	// org_name, so AuthContext.signup's `setCurrentUser(data.user)` gets
	// the full profile and the TopHeader renders "Name (Org)".
	writeJSON(w, http.StatusCreated, map[string]any{
		"access_token": token,
		"token_type":   "bearer",
		"user": map[string]any{
			"id":        userID,
			"email":     req.Email,
			"full_name": req.FullName,
			"role":      role,
			"org_id":    orgID,
			"org_name":  req.OrgName,
		},
	})
}

// ── POST /api/auth/login ──────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password required")
		return
	}

	user, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Sugar().Errorw("login: GetUserByEmail", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil || !db.CheckPassword(req.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := s.mintToken(user.Email, user.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Response shape matches Python auth.py:220 — `user` is nested so the
	// frontend's AuthContext.login() line `setCurrentUser(data.user)` picks
	// up the full profile (including org_name). A flat response leaves
	// `data.user` undefined and currentUser loses the org suffix.
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "bearer",
		"user":         userResponse(s, user),
	})
}

// ── GET /api/auth/me ──────────────────────────────────────────────────────────
//
// Response shape mirrors Python's auth.py:223-235 — in particular, `org_name`
// is looked up and attached so the TopHeader can render "Name (Org)". Without
// it the frontend's `{currentUser.org_name ? ` (${org_name})` : ''}` branch
// silently evaluates to empty and the org suffix disappears.

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	user, err := s.db.GetUserByEmail(ac.Email)
	if err != nil {
		s.logger.Sugar().Errorw("me: GetUserByEmail", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, userResponse(s, user))
}

// userResponse builds the user-profile object returned by /auth/me, login,
// and signup. One canonical shape keeps the frontend (AuthContext.jsx,
// TopHeader.jsx) consistent across all three endpoints.
func userResponse(s *Server, user *db.User) map[string]any {
	orgName := ""
	if user.OrgID > 0 {
		if org, err := s.db.GetOrganizationByID(user.OrgID); err == nil && org != nil {
			orgName = org.Name
		}
	}
	return map[string]any{
		"id":        user.ID,
		"email":     user.Email,
		"full_name": user.FullName,
		"role":      user.Role,
		"org_id":    user.OrgID,
		"org_name":  orgName,
	}
}

// ── JWT helpers ───────────────────────────────────────────────────────────────

const tokenTTL = 30 * 24 * time.Hour // 30 days — matches Python ACCESS_TOKEN_EXPIRE_MINUTES

func (s *Server) mintToken(email string, orgID int64) (string, error) {
	claims := &jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		},
		OrgID: orgID,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

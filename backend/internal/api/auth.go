package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "Email is required.")
		return
	}
	if msg := validatePassword(req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
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

	token, err := s.mintToken(req.Email, orgID, role)
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

	token, err := s.mintToken(user.Email, user.OrgID, user.Role)
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
const sseTicketTTL = 60 * time.Second // short window — minted just before EventSource connect

func (s *Server) mintToken(email string, orgID int64, role string) (string, error) {
	claims := &jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		},
		OrgID: orgID,
		Role:  role,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

// mintSSETicket signs a 60-second-TTL JWT carrying kind="sse". Used to
// authenticate EventSource connections without putting the long-lived auth
// JWT in the URL. (issue #80)
func (s *Server) mintSSETicket(email string, orgID int64, role string) (string, error) {
	claims := &jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   email,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(sseTicketTTL)),
		},
		OrgID: orgID,
		Role:  role,
		Kind:  "sse",
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

// GET /api/sse/ticket — issues a short-lived ticket the frontend appends as
// ?ticket=… to /api/campaign-events and /api/sse/live-logs. Requires the
// regular Bearer auth header.
func (s *Server) sseTicket(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	tok, err := s.mintSSETicket(ac.Email, ac.OrgID, ac.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not mint ticket")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": tok, "expires_in": int(sseTicketTTL.Seconds())})
}

// ── POST /api/auth/forgot-password ────────────────────────────────────────────
// Mirrors Python auth.py forgot_password: generates a 32-byte url-safe token,
// stores it with a 1-hour expiry, emails a reset link, returns the SAME generic
// message regardless of whether the email exists (so attackers can't enumerate
// users). Errors that prevent sending the email are logged but the response
// still claims success — same intentional opacity as the Python version.

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}

	const genericMsg = "If an account with that email exists, a reset link has been sent."

	user, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		s.logger.Sugar().Errorw("forgotPassword: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil {
		s.logger.Sugar().Infow("[AUTH] password reset for unknown email", "email", req.Email)
		writeJSON(w, http.StatusOK, map[string]string{"message": genericMsg})
		return
	}

	// 32 random bytes → ~43 url-safe base64 chars (no padding).
	rb := make([]byte, 32)
	if _, err := rand.Read(rb); err != nil {
		s.logger.Sugar().Errorw("forgotPassword: random", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(rb)
	expiresAt := time.Now().Add(time.Hour)

	if _, err := s.db.CreateResetToken(user.ID, token, expiresAt); err != nil {
		s.logger.Sugar().Errorw("forgotPassword: create token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if s.emailSvc != nil {
		resetLink := fmt.Sprintf("%s/reset-password?token=%s", s.cfg.AppURL, token)
		html := fmt.Sprintf(`<h2 style="color:#a5b4fc;margin-top:0;">Reset Your Password</h2>
<p>We received a request to reset your password. Click the button below to set a new password.</p>
<p>
  <a href="%s" style="display:inline-block;background:#6366f1;color:#f8fafc;padding:12px 28px;border-radius:6px;text-decoration:none;font-weight:bold;">Reset Password</a>
</p>
<p style="color:#94a3b8;margin-top:24px;font-size:13px;">
  This link expires in 1 hour. If you didn't request this, you can safely ignore this email.
</p>`, resetLink)
		if err := s.emailSvc.Send(user.Email, "Reset Your Password - Callified AI", html); err != nil {
			s.logger.Sugar().Warnw("forgotPassword: email send failed", "err", err, "email", req.Email)
		}
	} else {
		s.logger.Sugar().Warnw("forgotPassword: email service unavailable — token created but not delivered",
			"email", req.Email)
	}
	s.logger.Sugar().Infow("[AUTH] password reset link sent", "email", req.Email)
	writeJSON(w, http.StatusOK, map[string]string{"message": genericMsg})
}

// ── POST /api/auth/reset-password ─────────────────────────────────────────────
// Validates the token, replaces the user's bcrypt hash, marks the token used.
// Mirrors Python auth.py reset_password.

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "token and new_password required")
		return
	}
	if msg := validatePassword(req.NewPassword); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	tokenRow, err := s.db.GetValidResetToken(req.Token)
	if err != nil {
		s.logger.Sugar().Errorw("resetPassword: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tokenRow == nil {
		writeError(w, http.StatusBadRequest, "Invalid or expired reset token")
		return
	}
	hash, err := db.HashPassword(req.NewPassword)
	if err != nil {
		s.logger.Sugar().Errorw("resetPassword: hash", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.db.UpdateUserPassword(tokenRow.UserID, hash); err != nil {
		s.logger.Sugar().Errorw("resetPassword: update", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.db.MarkResetTokenUsed(tokenRow.ID)
	s.logger.Sugar().Infow("[AUTH] password reset completed", "user_id", tokenRow.UserID)
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password has been reset successfully. You can now log in.",
	})
}

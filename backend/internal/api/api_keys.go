package api

import (
	"encoding/json"
	"net/http"

	"github.com/globussoft/callified-backend/internal/db"
)

// ── GET /api/api-keys ─────────────────────────────────────────────────────────

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	keys, err := s.db.GetAPIKeysByOrg(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listAPIKeys", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(keys))
}

// ── POST /api/api-keys ────────────────────────────────────────────────────────

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	raw, hashed, err := db.GenerateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	prefix := raw
	if len(raw) > 10 {
		prefix = raw[:10]
	}
	id, err := s.db.CreateAPIKey(ac.OrgID, body.Name, hashed, prefix)
	if err != nil {
		s.logger.Sugar().Errorw("createAPIKey", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Return the raw key once — it is never stored in plaintext
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":  id,
		"key": raw,
	})
}

// ── DELETE /api/api-keys/{id} ─────────────────────────────────────────────────

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := s.db.DeleteAPIKey(ac.OrgID, id)
	if err != nil {
		s.logger.Sugar().Errorw("deleteAPIKey", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

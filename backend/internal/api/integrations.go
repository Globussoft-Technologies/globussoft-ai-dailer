package api

import (
	"encoding/json"
	"net/http"
)

// GET /api/integrations
func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	// Return only integrations for this org
	all, err := s.db.GetActiveCRMIntegrations()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var result []any
	for _, integ := range all {
		if integ.OrgID == ac.OrgID {
			result = append(result, integ)
		}
	}
	if result == nil {
		result = []any{}
	}
	writeJSON(w, http.StatusOK, result)
}

// POST /api/integrations
func (s *Server) createIntegration(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		Provider    string            `json:"provider"`
		Credentials map[string]string `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider required")
		return
	}
	id, err := s.db.SaveCRMIntegration(ac.OrgID, body.Provider, body.Credentials)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// DELETE /api/integrations/{id}
func (s *Server) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.DeleteCRMIntegration(ac.OrgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

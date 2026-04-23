package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
)

// ── GET /api/dnd ──────────────────────────────────────────────────────────────

func (s *Server) listDND(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	numbers, err := s.db.GetDNDNumbers(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listDND", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(numbers))
}

// ── POST /api/dnd ─────────────────────────────────────────────────────────────

func (s *Server) addDND(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		Phone  string `json:"phone"`
		Source string `json:"source"`
		Reason string `json:"reason"` // legacy alias
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Phone == "" {
		writeError(w, http.StatusBadRequest, "phone required")
		return
	}
	src := body.Source
	if src == "" {
		src = body.Reason
	}
	if err := s.db.AddDNDNumber(ac.OrgID, body.Phone, src); err != nil {
		s.logger.Sugar().Errorw("addDND", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"added": true})
}

// ── POST /api/dnd/import-csv ──────────────────────────────────────────────────

func (s *Server) importDNDCSV(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid CSV")
		return
	}

	var phones []string
	for i, rec := range records {
		if i == 0 {
			continue // skip header row
		}
		if len(rec) > 0 && strings.TrimSpace(rec[0]) != "" {
			phones = append(phones, strings.TrimSpace(rec[0]))
		}
	}

	if err := s.db.AddDNDNumbersBulk(ac.OrgID, phones, "manual"); err != nil {
		s.logger.Sugar().Errorw("importDNDCSV", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(phones)})
}

// ── DELETE /api/dnd/{id} ──────────────────────────────────────────────────────

func (s *Server) removeDND(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	deleted, err := s.db.RemoveDNDNumber(ac.OrgID, id)
	if err != nil {
		s.logger.Sugar().Errorw("removeDND", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── GET /api/dnd/check ────────────────────────────────────────────────────────

func (s *Server) checkDND(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		writeError(w, http.StatusBadRequest, "phone query param required")
		return
	}
	isDND, err := s.db.IsDNDNumber(ac.OrgID, phone)
	if err != nil {
		s.logger.Sugar().Errorw("checkDND", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"is_dnd": isDND})
}

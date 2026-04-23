package api

import (
	"encoding/json"
	"net/http"
)

// GET /api/wa/channels
func (s *Server) listWAChannels(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	configs, err := s.db.GetWAChannelConfigsByOrg(ac.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(configs))
}

// POST /api/wa/channels
func (s *Server) createWAChannel(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		Provider    string `json:"provider"`
		PhoneNumber string `json:"phone_number"`
		APIKey      string `json:"api_key"`
		AppID       string `json:"app_id"`
		WebhookURL  string `json:"webhook_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" || body.PhoneNumber == "" {
		writeError(w, http.StatusBadRequest, "provider and phone_number required")
		return
	}
	id, err := s.db.CreateWAChannelConfig(ac.OrgID, body.Provider, body.PhoneNumber, body.APIKey, body.AppID, body.WebhookURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// PUT /api/wa/channels/{id}
func (s *Server) updateWAChannel(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		APIKey     string `json:"api_key"`
		AppID      string `json:"app_id"`
		WebhookURL string `json:"webhook_url"`
		AIEnabled  bool   `json:"ai_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.db.UpdateWAChannelConfig(id, ac.OrgID, body.APIKey, body.AppID, body.WebhookURL, body.AIEnabled); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

// DELETE /api/wa/channels/{id}
func (s *Server) deleteWAChannel(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.DeleteWAChannelConfig(id, ac.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// PUT /api/wa/channels/{id}/toggle-ai
func (s *Server) toggleWAAI(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.db.ToggleWAAI(id, ac.OrgID, body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ai_enabled": body.Enabled})
}

// GET /api/wa/conversations
func (s *Server) listWAConversations(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	convs, err := s.db.GetWAConversationsList(ac.OrgID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(convs))
}

// GET /api/wa/conversations/{id}/history
func (s *Server) getWAHistory(w http.ResponseWriter, r *http.Request) {
	convID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	history, err := s.db.GetWAChatHistory(convID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(history))
}

// POST /api/wa/send
func (s *Server) sendWAMessage(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		ChannelID int64  `json:"channel_id"`
		ToPhone   string `json:"to_phone"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ToPhone == "" || body.Message == "" {
		writeError(w, http.StatusBadRequest, "to_phone and message required")
		return
	}

	settings, err := s.db.GetWAChannelConfigsByOrg(ac.OrgID)
	if err != nil || len(settings) == 0 {
		writeError(w, http.StatusBadRequest, "no WA channel configured")
		return
	}

	cfg := settings[0]
	if body.ChannelID > 0 {
		for _, c := range settings {
			if c.ID == body.ChannelID {
				cfg = c
				break
			}
		}
	}

	channelCfg := s.waChannelConfig(cfg.Provider, cfg.PhoneNumber, cfg.APIKey, cfg.AppID)
	if err := s.waSender.SendText(r.Context(), channelCfg, body.ToPhone, body.Message); err != nil {
		writeError(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

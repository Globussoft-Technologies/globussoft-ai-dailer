package api

import (
	"encoding/json"
	"net/http"
	"strings"
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

// ── /api/wa/config ──────────────────────────────────────────────────────────
//
// Single-config-per-org compatibility shim for the frontend modal
// (WhatsAppTab.jsx). Python exposes /api/wa/config returning a shape
// like `{provider, credentials{}, default_product_id, auto_reply}` and the
// Go native endpoints work with flat columns under /api/wa/channels.
// These two handlers translate between the shapes so the existing UI works
// without a rewrite.

// GET /api/wa/config — returns the org's first active WA channel config in
// Python's response shape, or a default empty object when none exists.
func (s *Server) getWAConfig(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	configs, err := s.db.GetWAChannelConfigsByOrg(ac.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(configs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider":           "gupshup",
			"credentials":        map[string]string{},
			"default_product_id": nil,
			"auto_reply":         true,
		})
		return
	}
	cfg := configs[0]
	// Merge the JSON credentials column with the legacy flat columns so any
	// provider's full field set surfaces. Flat columns win on conflict for
	// backwards-compatibility with rows written before the JSON column was
	// wired through (those rows have flat values but `credentials='{}'`).
	creds := map[string]string{}
	for k, v := range cfg.Credentials {
		if v != "" {
			creds[k] = v
		}
	}
	if cfg.APIKey != "" {
		creds["api_key"] = cfg.APIKey
	}
	if cfg.AppID != "" {
		creds["app_id"] = cfg.AppID
	}
	if cfg.PhoneNumber != "" {
		creds["phone_number"] = cfg.PhoneNumber
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                 cfg.ID,
		"provider":           cfg.Provider,
		"credentials":        creds,
		"default_product_id": nil, // column exists but not wired through Go struct yet
		"auto_reply":         cfg.AIEnabled,
	})
}

// validWAProviders is the closed set of WA channel providers the backend
// supports. Bonus side-effect: rejecting unknown providers at save time means
// we never try to render a webhook URL or persist a config for a typo'd
// provider name that no provider-specific code path would ever read.
var validWAProviders = map[string]bool{
	"gupshup": true, "wati": true, "aisensei": true, "interakt": true, "meta": true,
}

// POST /api/wa/config — upsert the org's single WA channel config. The
// frontend posts `{provider, credentials{}, default_product_id, auto_reply}`;
// we fan it out onto the flat columns. UNIQUE(org_id,provider) makes this
// an upsert.
func (s *Server) saveWAConfig(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	var body struct {
		Provider         string            `json:"provider"`
		Credentials      map[string]string `json:"credentials"`
		DefaultProductID *int64            `json:"default_product_id"`
		AutoReply        *bool             `json:"auto_reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider required")
		return
	}
	body.Provider = strings.TrimSpace(body.Provider)
	if !validWAProviders[body.Provider] {
		writeError(w, http.StatusBadRequest, "unknown provider")
		return
	}
	// Require at least one non-empty credential. The previous handler
	// happily upserted a row with all-empty {api_key, app_id, phone_number,
	// webhook_url} columns when the modal Save fired with blank inputs —
	// resulting in a "configured" channel that silently fails on the first
	// outbound send. Per-provider key validation is deferred (see #46
	// follow-up: backend reads `app_id`/`phone_number` while the gupshup UI
	// posts `app_name`/`source_phone`, so a strict check here would need
	// the key-name reconciliation first).
	hasAnyCred := false
	for _, v := range body.Credentials {
		if strings.TrimSpace(v) != "" {
			hasAnyCred = true
			break
		}
	}
	if !hasAnyCred {
		writeError(w, http.StatusBadRequest, "at least one credential is required")
		return
	}
	apiKey := body.Credentials["api_key"]
	appID := body.Credentials["app_id"]
	phone := body.Credentials["phone_number"]
	webhookURL := body.Credentials["webhook_url"]

	// UNIQUE(org_id, provider) turns this INSERT into an upsert. Avoids the
	// need for a separate update path + lookup.
	if _, err := s.db.UpsertWAChannelConfig(ac.OrgID, body.Provider, phone, apiKey, appID, webhookURL, body.Credentials, body.AutoReply); err != nil {
		s.logger.Sugar().Errorw("saveWAConfig", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// GET /api/wa/conversations/{phone}/messages — per-phone message history.
// The frontend looks up conversations by phone number, not the internal
// conversation ID, so we resolve phone → conversation for this org first.
func (s *Server) getWAMessagesByPhone(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	phone := r.PathValue("phone")
	if phone == "" {
		writeError(w, http.StatusBadRequest, "phone required")
		return
	}
	convID, err := s.db.GetWAConversationIDByPhone(ac.OrgID, phone)
	if err != nil || convID == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	history, err := s.db.GetWAChatHistory(convID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(history))
}

// POST /api/wa/toggle-ai/{phone} — flip ai_enabled on one conversation row.
// Body: `{enabled: bool}`. Stored per-conversation (so one runaway contact
// can be muted without disabling AI for the whole channel).
func (s *Server) toggleWAAIByPhone(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	phone := r.PathValue("phone")
	if phone == "" {
		writeError(w, http.StatusBadRequest, "phone required")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.db.ToggleWAConversationAI(ac.OrgID, phone, body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ai_enabled": body.Enabled})
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

package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/globussoft/callified-backend/internal/db"
)

type exotelAccountOption struct {
	ID         int64  `json:"id"`
	Provider   string `json:"provider"`
	Name       string `json:"name"`
	AccountSID string `json:"account_sid"`
	CallerID   string `json:"caller_id"`
	AppType    string `json:"app_type"`
	Direction  string `json:"direction"`
	Region     string `json:"region"`
	Subdomain  string `json:"subdomain"`
}

// ── GET /api/exotel-accounts ─────────────────────────────────────────────────

func (s *Server) listExotelAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.global") {
		return
	}
	ac := getAuth(r)
	accounts, err := s.db.GetOrgExotelAccounts(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listExotelAccounts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, emptyJSON(accounts))
}

// ── GET /api/exotel-accounts/options ─────────────────────────────────────────

func (s *Server) listExotelAccountOptions(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)
	accounts, err := s.db.GetOrgExotelAccounts(ac.OrgID)
	if err != nil {
		s.logger.Sugar().Errorw("listExotelAccountOptions", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Non-Admins only see org-level accounts explicitly allowed for them.
	// Admins bypass the filter so they can manage campaigns for any account.
	if ac.Role != db.RoleAdmin {
		allowedIDs, err := s.db.GetUserAllowedExotelAccountIDs(ac.UserID)
		if err != nil {
			s.logger.Sugar().Errorw("listExotelAccountOptions: allowed ids", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		allowedSet := make(map[int64]bool, len(allowedIDs))
		for _, id := range allowedIDs {
			allowedSet[id] = true
		}
		filtered := make([]db.OrgExotelAccount, 0, len(allowedIDs))
		for _, a := range accounts {
			if allowedSet[a.ID] {
				filtered = append(filtered, a)
			}
		}
		accounts = filtered
	}
	var userAccounts []db.OrgExotelAccount
	if ac.UserID > 0 {
		userAccounts, _ = s.db.GetUserExotelAccounts(ac.UserID, ac.OrgID)
	}
	options := make([]exotelAccountOption, 0, len(accounts)+len(userAccounts))
	for _, a := range accounts {
		options = append(options, exotelAccountOption{
			ID:         a.ID,
			Provider:   a.Provider,
			Name:       a.Name,
			AccountSID: a.AccountSID,
			CallerID:   a.CallerID,
			AppType:    a.AppType,
			Direction:  a.Direction,
			Region:     a.Region,
			Subdomain:  a.Subdomain,
		})
	}
	for _, a := range userAccounts {
		options = append(options, exotelAccountOption{
			ID:         a.ID,
			Provider:   a.Provider,
			Name:       a.Name + " (personal)",
			AccountSID: a.AccountSID,
			CallerID:   a.CallerID,
			AppType:    a.AppType,
			Direction:  a.Direction,
			Region:     a.Region,
			Subdomain:  a.Subdomain,
		})
	}
	writeJSON(w, http.StatusOK, emptyJSON(options))
}

// ── POST /api/exotel-accounts ────────────────────────────────────────────────

func (s *Server) createExotelAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.global") {
		return
	}
	ac := getAuth(r)
	var req struct {
		Provider   string `json:"provider"`
		Name       string `json:"name"`
		APIKey     string `json:"api_key"`
		APIToken   string `json:"api_token"`
		APISecret  string `json:"api_secret"`
		AccountSID string `json:"account_sid"`
		CallerID   string `json:"caller_id"`
		AppID      string `json:"app_id"`
		AppType    string `json:"app_type"`
		Direction  string `json:"direction"`
		Region     string `json:"region"`
		Subdomain  string `json:"subdomain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Provider == "" {
		req.Provider = "exotel"
	}
	if req.AppType == "" {
		req.AppType = "exoml"
	}
	req.Direction = normalizeProviderDirection(req.Direction)
	if err := validateProviderAccount(req.Provider, req.Direction, req.Name, req.APIKey, req.APIToken, req.APISecret, req.AccountSID, req.CallerID, req.AppID); err != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Provider == "exotel" && req.AppType != "exoml" && req.AppType != "voicebot" {
		writeError(w, http.StatusBadRequest, "app_type must be 'exoml' or 'voicebot'")
		return
	}
	id, err := s.db.CreateOrgExotelAccount(ac.OrgID, req.Provider,
		strings.TrimSpace(req.Name), req.APIKey, req.APIToken, req.APISecret,
		req.AccountSID, req.CallerID, req.AppID, req.AppType, req.Direction, req.Region, req.Subdomain)
	if err != nil {
		s.logger.Sugar().Errorw("createExotelAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// ── PUT /api/exotel-accounts/{id} ────────────────────────────────────────────

func (s *Server) updateExotelAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.global") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Provider   string `json:"provider"`
		Name       string `json:"name"`
		APIKey     string `json:"api_key"`
		APIToken   string `json:"api_token"`
		APISecret  string `json:"api_secret"`
		AccountSID string `json:"account_sid"`
		CallerID   string `json:"caller_id"`
		AppID      string `json:"app_id"`
		AppType    string `json:"app_type"`
		Direction  string `json:"direction"`
		Region     string `json:"region"`
		Subdomain  string `json:"subdomain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Provider == "" {
		req.Provider = "exotel"
	}
	if req.AppType == "" {
		req.AppType = "exoml"
	}
	req.Direction = normalizeProviderDirection(req.Direction)
	if errMsg := validateProviderAccount(req.Provider, req.Direction, req.Name, req.APIKey, req.APIToken, req.APISecret, req.AccountSID, req.CallerID, req.AppID); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if req.Provider == "exotel" && req.AppType != "exoml" && req.AppType != "voicebot" {
		writeError(w, http.StatusBadRequest, "app_type must be 'exoml' or 'voicebot'")
		return
	}
	if err := s.db.UpdateOrgExotelAccount(id, ac.OrgID, req.Provider,
		strings.TrimSpace(req.Name), req.APIKey, req.APIToken, req.APISecret,
		req.AccountSID, req.CallerID, req.AppID, req.AppType, req.Direction, req.Region, req.Subdomain); err != nil {
		s.logger.Sugar().Errorw("updateExotelAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// ── DELETE /api/exotel-accounts/{id} ─────────────────────────────────────────

func (s *Server) deleteExotelAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "provider_accounts.global") {
		return
	}
	ac := getAuth(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.db.DeleteOrgExotelAccount(id, ac.OrgID); err != nil {
		s.logger.Sugar().Errorw("deleteExotelAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── GET /api/campaigns/{id}/exotel-account ───────────────────────────────────

func (s *Server) getCampaignExotelAccount(w http.ResponseWriter, r *http.Request) {
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	accountID, _ := s.db.GetCampaignExotelAccountID(campaign.ID)
	writeJSON(w, http.StatusOK, map[string]int64{"exotel_account_id": accountID})
}

// ── PUT /api/campaigns/{id}/exotel-account ───────────────────────────────────

func (s *Server) setCampaignExotelAccount(w http.ResponseWriter, r *http.Request) {
	campaign := s.requireCampaignView(w, r)
	if campaign == nil {
		return
	}
	var req struct {
		ExotelAccountID int64 `json:"exotel_account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.db.SetCampaignExotelAccount(campaign.ID, req.ExotelAccountID); err != nil {
		s.logger.Sugar().Errorw("setCampaignExotelAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// validateProviderAccount checks required fields per provider.
func normalizeProviderDirection(direction string) string {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "inbound":
		return "inbound"
	default:
		return "outbound"
	}
}

func validateProviderAccount(provider, direction, name, apiKey, apiToken, apiSecret, accountSID, callerID, appID string) string {
	if strings.TrimSpace(name) == "" {
		return "account name is required"
	}
	switch provider {
	case "tata", "smartflo", "tata_tele":
		if direction == "inbound" {
			if apiKey == "" || callerID == "" {
				return "api_key (Tata API token) and caller_id (Tata DID) are required for inbound Tata Tele"
			}
			return ""
		}
		if apiKey == "" || callerID == "" || appID == "" {
			return "api_key (Tata API token), app_id (agent number) and caller_id (Tata number) are required for Tata Tele"
		}
	case "twilio":
		if accountSID == "" || apiKey == "" || apiToken == "" || apiSecret == "" || callerID == "" {
			return "account_sid, api_key (auth token), api_token (API key SID), api_secret and caller_id (phone number) are required for Twilio"
		}
	default: // exotel
		if apiKey == "" || apiToken == "" || accountSID == "" || callerID == "" {
			return "api_key, api_token, account_sid and caller_id are required for Exotel"
		}
	}
	return ""
}

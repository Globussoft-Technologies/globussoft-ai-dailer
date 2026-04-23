package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/db"
)

// strictTemplateProviders require a pre-approved template for first outbound contact.
var strictTemplateProviders = map[string]bool{
	"meta": true,
	"wati": true,
}

// ── POST /api/wa/campaign-blast/{campaign_id} ─────────────────────────────────

func (s *Server) campaignBlast(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)

	campaignID, err := parseID(r, "campaign_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid campaign_id")
		return
	}

	// Load campaign and verify ownership + channel
	campaign, err := s.db.GetCampaignByID(campaignID)
	if err != nil || campaign == nil {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	if campaign.OrgID != ac.OrgID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if campaign.Channel != "whatsapp" {
		writeError(w, http.StatusBadRequest, "campaign channel is not whatsapp")
		return
	}

	// Load active WA config
	cfg, err := s.db.GetActiveWAConfig(ac.OrgID)
	if err != nil {
		s.logger.Error("campaignBlast: GetActiveWAConfig", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cfg == nil {
		writeError(w, http.StatusNotFound, "no active WhatsApp config found — configure it in WhatsApp Comms → Settings")
		return
	}

	// Guard: strict providers need a welcome_template
	if cfg.WelcomeTemplate == "" && strictTemplateProviders[cfg.Provider] {
		writeError(w, http.StatusBadRequest,
			"no welcome_template configured. WhatsApp ("+cfg.Provider+") requires a pre-approved template "+
				"for first outbound contact. Set it in WhatsApp Comms → Settings.")
		return
	}

	// Guard: duplicate blast prevention
	running, err := s.db.HasRunningBlastJob(campaignID)
	if err != nil {
		s.logger.Error("campaignBlast: HasRunningBlastJob", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if running {
		writeError(w, http.StatusConflict, "a blast is already in progress for this campaign")
		return
	}

	// Fetch new leads
	leads, err := s.db.GetCampaignNewLeads(campaignID)
	if err != nil {
		s.logger.Error("campaignBlast: GetCampaignNewLeads", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(leads) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"sent": 0, "failed": 0, "message": "no new leads to contact",
		})
		return
	}

	// Resolve product name for greeting text
	productName, _ := s.db.GetProductName(campaign.ProductID)

	// Derive country code from the business WA number
	cc := db.CountryCodeFromPhone(cfg.PhoneNumber)

	// Create blast job
	jobID, err := s.db.CreateBlastJob(campaignID, ac.OrgID, len(leads))
	if err != nil {
		s.logger.Error("campaignBlast: CreateBlastJob", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Respond immediately — blast runs in background
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id": jobID,
		"total":  len(leads),
	})

	// Background blast: bounded goroutine pool (max 20 concurrent sends)
	go func() {
		sem := make(chan struct{}, 20)
		var wg sync.WaitGroup

		for _, lead := range leads {
			lead := lead // capture
			wg.Add(1)
			sem <- struct{}{}

			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				s.blastOneLead(r.Context(), jobID, cfg, lead, productName, cc)
			}()
		}

		wg.Wait()
		if err := s.db.FinishBlastJob(jobID); err != nil {
			s.logger.Error("campaignBlast: FinishBlastJob", zap.Int64("job_id", jobID), zap.Error(err))
		}
		s.logger.Info("campaignBlast: done", zap.Int64("job_id", jobID), zap.Int("leads", len(leads)))
	}()
}

// blastOneLead sends a WA message to a single lead within the blast goroutine pool.
func (s *Server) blastOneLead(
	ctx context.Context,
	jobID int64,
	cfg *db.WAChannelConfig,
	lead db.Lead,
	productName, countryCode string,
) {
	phone, ok := db.NormalizePhone(lead.Phone, countryCode)
	if !ok {
		_ = s.db.IncrBlastJobFailed(jobID, fmt.Sprintf("invalid phone for lead %d: %q", lead.ID, lead.Phone))
		return
	}

	// 24-hour dedup — skip if already sent today
	recent, err := s.db.HasRecentOutbound(cfg.OrgID, phone)
	if err != nil {
		s.logger.Warn("blastOneLead: HasRecentOutbound", zap.Error(err))
	}
	if recent {
		// Not counted as failure — just skipped
		s.logger.Debug("blastOneLead: skipping (recent outbound)", zap.String("phone", phone))
		_ = s.db.IncrBlastJobSent(jobID) // count as sent (already contacted)
		return
	}

	// Build message text
	leadName := strings.TrimSpace(lead.FirstName + " " + lead.LastName)
	if leadName == "" {
		leadName = "there"
	}
	msgText := db.BuildGreeting(leadName, productName)

	// Send via provider (10s timeout)
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var sendErr error
	if cfg.WelcomeTemplate != "" {
		sendErr = sendWATemplate(sendCtx, cfg, phone, cfg.WelcomeTemplate, lead.FirstName)
	} else {
		sendErr = sendWAText(sendCtx, cfg, phone, msgText)
	}

	if sendErr != nil {
		s.logger.Warn("blastOneLead: send failed",
			zap.String("phone", phone), zap.Int64("lead", lead.ID), zap.Error(sendErr))
		_ = s.db.IncrBlastJobFailed(jobID, fmt.Sprintf("lead %d (%s): %s", lead.ID, phone, sendErr.Error()))
		return
	}

	// Save outbound message
	_, err = s.db.SaveWAConversationMessage(cfg.OrgID, cfg.ID, lead.ID, phone, leadName, msgText)
	if err != nil {
		s.logger.Warn("blastOneLead: SaveWAConversationMessage", zap.Error(err))
		// Non-fatal: message was sent; just couldn't save record
	}

	// Update lead status → 'contacted'
	if err := s.db.UpdateLeadStatus(lead.ID, "contacted"); err != nil {
		s.logger.Warn("blastOneLead: UpdateLeadStatus", zap.Error(err))
	}

	_ = s.db.IncrBlastJobSent(jobID)
}

// ── GET /api/wa/campaign-blast/status/{job_id} ───────────────────────────────

func (s *Server) blastStatus(w http.ResponseWriter, r *http.Request) {
	ac := getAuth(r)

	jobID, err := parseID(r, "job_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job_id")
		return
	}

	job, err := s.db.GetBlastJob(jobID)
	if err != nil {
		s.logger.Error("blastStatus: GetBlastJob", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if job == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if job.OrgID != ac.OrgID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// ── WhatsApp provider HTTP dispatch ──────────────────────────────────────────

// sendWAText sends a free-form text message via the provider's REST API.
func sendWAText(ctx context.Context, cfg *db.WAChannelConfig, toPhone, text string) error {
	switch cfg.Provider {
	case "meta":
		return sendMetaText(ctx, cfg, toPhone, text)
	case "wati":
		return sendWatiText(ctx, cfg, toPhone, text)
	case "gupshup":
		return sendGupshupText(ctx, cfg, toPhone, text)
	case "interakt":
		return sendInteraktText(ctx, cfg, toPhone, text)
	case "aisensei":
		return sendAiSenseiText(ctx, cfg, toPhone, text)
	default:
		return fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

// sendWATemplate sends a pre-approved template message.
func sendWATemplate(ctx context.Context, cfg *db.WAChannelConfig, toPhone, templateName, firstName string) error {
	switch cfg.Provider {
	case "meta":
		return sendMetaTemplate(ctx, cfg, toPhone, templateName, firstName)
	case "wati":
		return sendWatiTemplate(ctx, cfg, toPhone, templateName, firstName)
	default:
		// Providers that don't enforce templates — fall back to text
		return sendWAText(ctx, cfg, toPhone, db.BuildGreeting(firstName, ""))
	}
}

// ── Meta Cloud API ────────────────────────────────────────────────────────────

func sendMetaText(ctx context.Context, cfg *db.WAChannelConfig, toPhone, text string) error {
	phoneNumberID := cfg.Credentials["phone_number_id"]
	token := cfg.Credentials["access_token"]
	if phoneNumberID == "" || token == "" {
		return fmt.Errorf("meta: missing phone_number_id or access_token")
	}
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                toPhone,
		"type":              "text",
		"text":              map[string]string{"body": text},
	})
	apiURL := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/messages", phoneNumberID)
	return doJSONPost(ctx, apiURL, "Bearer "+token, body)
}

func sendMetaTemplate(ctx context.Context, cfg *db.WAChannelConfig, toPhone, templateName, firstName string) error {
	phoneNumberID := cfg.Credentials["phone_number_id"]
	token := cfg.Credentials["access_token"]
	if phoneNumberID == "" || token == "" {
		return fmt.Errorf("meta: missing phone_number_id or access_token")
	}
	body, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                toPhone,
		"type":              "template",
		"template": map[string]any{
			"name": templateName,
			"language": map[string]string{
				"code": "en",
			},
			"components": []map[string]any{
				{
					"type": "body",
					"parameters": []map[string]any{
						{"type": "text", "text": firstName},
					},
				},
			},
		},
	})
	apiURL := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/messages", phoneNumberID)
	return doJSONPost(ctx, apiURL, "Bearer "+token, body)
}

// ── Wati ─────────────────────────────────────────────────────────────────────

func sendWatiText(ctx context.Context, cfg *db.WAChannelConfig, toPhone, text string) error {
	tenantURL := strings.TrimRight(cfg.Credentials["tenant_url"], "/")
	token := cfg.Credentials["bearer_token"]
	if tenantURL == "" || token == "" {
		return fmt.Errorf("wati: missing tenant_url or bearer_token")
	}
	// Wati expects phone without leading +
	phone := strings.TrimPrefix(toPhone, "+")
	body, _ := json.Marshal(map[string]any{
		"template_name": "",
		"broadcast_name": "blast",
		"parameters":    []any{},
	})
	_ = body
	// Wati free-form send
	apiURL := fmt.Sprintf("%s/api/v1/sendSessionMessage/%s?messageText=%s",
		tenantURL, phone, url.QueryEscape(text))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(""))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func sendWatiTemplate(ctx context.Context, cfg *db.WAChannelConfig, toPhone, templateName, firstName string) error {
	tenantURL := strings.TrimRight(cfg.Credentials["tenant_url"], "/")
	token := cfg.Credentials["bearer_token"]
	if tenantURL == "" || token == "" {
		return fmt.Errorf("wati: missing tenant_url or bearer_token")
	}
	phone := strings.TrimPrefix(toPhone, "+")
	apiURL := fmt.Sprintf("%s/api/v1/sendTemplateMessage/%s", tenantURL, phone)
	bodyBytes, _ := json.Marshal(map[string]any{
		"template_name": templateName,
		"broadcast_name": "blast",
		"parameters": []map[string]string{
			{"name": "1", "value": firstName},
		},
	})
	return doJSONPost(ctx, apiURL, "Bearer "+token, bodyBytes)
}

// ── Gupshup (form POST) ───────────────────────────────────────────────────────

func sendGupshupText(ctx context.Context, cfg *db.WAChannelConfig, toPhone, text string) error {
	apiKey := cfg.Credentials["api_key"]
	appName := cfg.Credentials["app_name"]
	sourcePhone := cfg.Credentials["source_phone"]
	if apiKey == "" || appName == "" {
		return fmt.Errorf("gupshup: missing api_key or app_name")
	}
	form := url.Values{}
	form.Set("channel", "whatsapp")
	form.Set("source", sourcePhone)
	form.Set("destination", toPhone)
	form.Set("message", fmt.Sprintf(`{"type":"text","text":"%s"}`, escapeJSON(text)))
	form.Set("src.name", appName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.gupshup.io/sm/api/v1/msg",
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doRequest(req)
}

// ── Interakt ──────────────────────────────────────────────────────────────────

func sendInteraktText(ctx context.Context, cfg *db.WAChannelConfig, toPhone, text string) error {
	apiKey := cfg.Credentials["api_key"]
	if apiKey == "" {
		return fmt.Errorf("interakt: missing api_key")
	}
	body, _ := json.Marshal(map[string]any{
		"countryCode": "+91",
		"phoneNumber": strings.TrimPrefix(toPhone, "+91"),
		"type":        "Text",
		"data":        map[string]string{"message": text},
	})
	return doJSONPost(ctx, "https://api.interakt.ai/v1/public/message/", "Basic "+apiKey, body)
}

// ── AiSensei ─────────────────────────────────────────────────────────────────

func sendAiSenseiText(ctx context.Context, cfg *db.WAChannelConfig, toPhone, text string) error {
	apiKey := cfg.Credentials["api_key"]
	baseURL := strings.TrimRight(cfg.Credentials["base_url"], "/")
	if apiKey == "" || baseURL == "" {
		return fmt.Errorf("aisensei: missing api_key or base_url")
	}
	body, _ := json.Marshal(map[string]any{
		"to":      toPhone,
		"type":    "text",
		"message": map[string]string{"text": text},
	})
	return doJSONPost(ctx, baseURL+"/messages", "Bearer "+apiKey, body)
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func doJSONPost(ctx context.Context, apiURL, authHeader string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	return doRequest(req)
}

func doRequest(req *http.Request) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// escapeJSON escapes a string for inline JSON embedding.
func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	// json.Marshal wraps in quotes; strip them
	return string(b[1 : len(b)-1])
}

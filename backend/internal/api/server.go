// Package api provides the REST API layer that mirrors Python's routes.py.
// Only stateless/high-traffic endpoints are served here; CRM-heavy routes
// remain in the Python FastAPI service.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/billing"
	"github.com/globussoft/callified-backend/internal/config"
	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
	"github.com/globussoft/callified-backend/internal/email"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/rag"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/globussoft/callified-backend/internal/wa"
	"github.com/globussoft/callified-backend/internal/webhook"
)

// Server holds shared dependencies for all REST handlers.
type Server struct {
	db          *db.DB
	cfg         *config.Config
	logger      *zap.Logger
	dispatcher  *webhook.Dispatcher
	store       *rstore.Store
	initiator   *dial.Initiator
	billingSvc  *billing.Service
	emailSvc    *email.Service
	ragClient   *rag.Client
	waAgent     *wa.Agent
	waSender    waSenderIface
	llmProvider *llm.Provider // Phase 4: Gemini-powered generation endpoints
}

// waSenderIface allows the WA sender to be nil-safe.
type waSenderIface interface {
	SendText(ctx context.Context, cfg wa.ChannelConfig, toPhone, text string) error
}

// waSend is the concrete implementation wrapping the wa package.
type waSend struct{}

func (waSend) SendText(ctx context.Context, cfg wa.ChannelConfig, toPhone, text string) error {
	return wa.SendText(ctx, cfg, toPhone, text)
}

// waChannelConfig converts DB config to wa.ChannelConfig.
func (s *Server) waChannelConfig(provider, phone, apiKey, appID string) wa.ChannelConfig {
	return wa.ChannelConfig{Provider: provider, PhoneNumber: phone, APIKey: apiKey, AppID: appID}
}

// New creates a new API server.
func New(d *db.DB, cfg *config.Config, store *rstore.Store, initiator *dial.Initiator, llmProvider *llm.Provider, logger *zap.Logger) *Server {
	emailSvc := email.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPFromName, cfg.AppURL, logger)
	billingSvc := billing.New(d, cfg.RazorpayKeyID, cfg.RazorpayKeySecret, emailSvc, logger)
	ragCli := rag.New(cfg.RAGServiceURL, logger)

	return &Server{
		db:          d,
		cfg:         cfg,
		logger:      logger,
		dispatcher:  webhook.New(d, logger),
		store:       store,
		initiator:   initiator,
		billingSvc:  billingSvc,
		emailSvc:    emailSvc,
		ragClient:   ragCli,
		waSender:    waSend{},
		llmProvider: llmProvider,
		// waAgent is wired in main.go after LLM provider is created (Phase 3C)
	}
}

// SetWAAgent wires the WhatsApp AI agent after construction.
func (s *Server) SetWAAgent(agent *wa.Agent) {
	s.waAgent = agent
}

// RegisterRoutes mounts all REST handlers onto the given mux.
// Path patterns use Go 1.22 method+path routing (METHOD /path/{param}).
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	auth := s.requireAuth

	// ── Auth ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/auth/signup", s.signup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/auth/me", auth(s.me))

	// ── Leads ─────────────────────────────────────────────────────────────────
	// Literal paths must be registered before the {id} wildcard so the mux
	// resolves /export and /search as exact matches, not lead IDs.
	mux.HandleFunc("GET /api/leads/export", auth(s.exportLeads))
	mux.HandleFunc("GET /api/leads/sample-csv", auth(s.sampleCSV))
	mux.HandleFunc("GET /api/leads/search", auth(s.searchLeads))
	mux.HandleFunc("POST /api/leads/import-csv", auth(s.importLeadsCSV))
	mux.HandleFunc("GET /api/leads", auth(s.listLeads))
	mux.HandleFunc("POST /api/leads", auth(s.createLead))
	mux.HandleFunc("GET /api/leads/{id}", auth(s.getLead))
	mux.HandleFunc("PUT /api/leads/{id}", auth(s.updateLead))
	mux.HandleFunc("DELETE /api/leads/{id}", auth(s.deleteLead))
	mux.HandleFunc("PUT /api/leads/{id}/status", auth(s.updateLeadStatus))
	mux.HandleFunc("POST /api/leads/{id}/notes", auth(s.updateLeadNote))
	mux.HandleFunc("POST /api/leads/{id}/documents", auth(s.uploadLeadDocument))
	mux.HandleFunc("GET /api/leads/{id}/documents", auth(s.getLeadDocuments))
	mux.HandleFunc("GET /api/leads/{id}/transcripts", auth(s.getLeadTranscripts))

	// ── Campaigns ─────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/campaigns", auth(s.listCampaigns))
	mux.HandleFunc("POST /api/campaigns", auth(s.createCampaign))
	mux.HandleFunc("GET /api/campaigns/{id}", auth(s.getCampaign))
	mux.HandleFunc("PUT /api/campaigns/{id}", auth(s.updateCampaign))
	mux.HandleFunc("DELETE /api/campaigns/{id}", auth(s.deleteCampaign))
	mux.HandleFunc("GET /api/campaigns/{id}/leads", auth(s.listCampaignLeads))
	mux.HandleFunc("POST /api/campaigns/{id}/leads", auth(s.addCampaignLeads))
	mux.HandleFunc("DELETE /api/campaigns/{id}/leads/{lead_id}", auth(s.removeCampaignLead))
	mux.HandleFunc("GET /api/campaigns/{id}/stats", auth(s.getCampaignStats))
	mux.HandleFunc("GET /api/campaigns/{id}/call-log", auth(s.getCampaignCallLog))
	mux.HandleFunc("GET /api/campaigns/{id}/voice-settings", auth(s.getCampaignVoiceSettings))
	mux.HandleFunc("PUT /api/campaigns/{id}/voice-settings", auth(s.saveCampaignVoiceSettings))
	mux.HandleFunc("POST /api/campaigns/{id}/import-csv", auth(s.importCampaignLeadsCSV))

	// ── Organizations ─────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/organizations", auth(s.listOrgs))
	mux.HandleFunc("POST /api/organizations", auth(s.createOrg))
	mux.HandleFunc("DELETE /api/organizations/{id}", auth(s.deleteOrg))
	mux.HandleFunc("GET /api/organizations/{id}/voice-settings", auth(s.getOrgVoiceSettings))
	mux.HandleFunc("PUT /api/organizations/{id}/voice-settings", auth(s.saveOrgVoiceSettings))
	mux.HandleFunc("PUT /api/organizations/{id}/timezone", auth(s.updateOrgTimezone))

	// ── Products ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/organizations/{id}/products", auth(s.listProducts))
	mux.HandleFunc("POST /api/organizations/{id}/products", auth(s.createProduct))
	mux.HandleFunc("PUT /api/products/{id}", auth(s.updateProduct))
	mux.HandleFunc("DELETE /api/products/{id}", auth(s.deleteProduct))
	mux.HandleFunc("GET /api/products/{id}/prompt", auth(s.getProductPrompt))
	mux.HandleFunc("PUT /api/products/{id}/prompt", auth(s.updateProductPrompt))

	// ── Recordings ────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/recordings/{filename}", auth(s.serveRecording))
	// Browser-side MediaRecorder upload (Opus/webm at native sample rate).
	// Handler exists in misc.go; the route was missing, so the browser POST
	// was 404'ing and the high-quality recording was being lost — only the
	// 8kHz server-side WAV survived. That was the "recording not clear"
	// symptom reported after Quick-Add + Sim Web Call.
	mux.HandleFunc("POST /api/upload-recording", auth(s.uploadRecording))

	// ── WhatsApp Campaign Blast ────────────────────────────────────────────────
	mux.HandleFunc("POST /api/wa/campaign-blast/{campaign_id}", auth(s.campaignBlast))
	mux.HandleFunc("GET /api/wa/campaign-blast/status/{job_id}", auth(s.blastStatus))

	// ── Organizations: system prompt ──────────────────────────────────────────
	mux.HandleFunc("GET /api/organizations/{id}/system-prompt", auth(s.getOrgSystemPrompt))
	mux.HandleFunc("PUT /api/organizations/{id}/system-prompt", auth(s.saveOrgSystemPrompt))

	// ── Campaign reviews ──────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/campaigns/{id}/call-reviews", auth(s.getCampaignCallReviews))

	// ── Transcript review ─────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/transcripts/{id}/review", auth(s.getTranscriptReview))

	// ── DND ───────────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/dnd/check", auth(s.checkDND))
	// Path-param flavour the frontend Check button uses
	// (GET /api/dnd/check/{phone}).
	mux.HandleFunc("GET /api/dnd/check/{phone}", auth(s.checkDNDByPhone))
	mux.HandleFunc("GET /api/dnd", auth(s.listDND))
	mux.HandleFunc("POST /api/dnd", auth(s.addDND))
	mux.HandleFunc("POST /api/dnd/import-csv", auth(s.importDNDCSV))
	// Alias for the frontend upload CSV input which posts to /dnd/import.
	mux.HandleFunc("POST /api/dnd/import", auth(s.importDNDCSV))
	mux.HandleFunc("DELETE /api/dnd/{id}", auth(s.removeDND))

	// ── Webhooks ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/webhooks", auth(s.listWebhooks))
	mux.HandleFunc("POST /api/webhooks", auth(s.createWebhook))
	mux.HandleFunc("DELETE /api/webhooks/{id}", auth(s.deleteWebhook))
	mux.HandleFunc("GET /api/webhooks/{id}/logs", auth(s.getWebhookLogs))

	// ── Scheduled calls ───────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/scheduled-calls", auth(s.listScheduledCalls))
	mux.HandleFunc("POST /api/scheduled-calls", auth(s.createScheduledCall))
	mux.HandleFunc("DELETE /api/scheduled-calls/{id}", auth(s.cancelScheduledCall))

	// ── Team ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/team", auth(s.listTeam))
	mux.HandleFunc("POST /api/team/invite", auth(s.inviteTeamMember))
	mux.HandleFunc("PUT /api/team/{id}/role", auth(s.updateTeamRole))
	mux.HandleFunc("DELETE /api/team/{id}", auth(s.deleteTeamMember))

	// ── API keys ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/api-keys", auth(s.listAPIKeys))
	mux.HandleFunc("POST /api/api-keys", auth(s.createAPIKey))
	mux.HandleFunc("DELETE /api/api-keys/{id}", auth(s.deleteAPIKey))

	// ── Onboarding ────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/onboarding", auth(s.getOnboarding))
	mux.HandleFunc("GET /api/onboarding/status", auth(s.onboardingStatus))
	mux.HandleFunc("POST /api/onboarding/complete", auth(s.completeOnboarding))

	// ── Calling status (TRAI guard) ───────────────────────────────────────────
	mux.HandleFunc("GET /api/calling-status", auth(s.callingStatus))

	// ── Demo requests ─────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/demo-requests", auth(s.listDemoRequests))
	mux.HandleFunc("POST /api/demo-requests", s.createDemoRequest) // no auth — public form

	// ── WhatsApp legacy logs ──────────────────────────────────────────────────
	mux.HandleFunc("GET /api/whatsapp", auth(s.listWhatsappLogs))

	// ── Debug / Health ────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/debug/health", s.debugHealth)
	mux.HandleFunc("GET /api/debug/logs", auth(s.debugLogs))
	mux.HandleFunc("GET /api/debug/last-dial", auth(s.debugLastDial))
	mux.HandleFunc("GET /api/debug/call-timeline", auth(s.debugCallTimeline))
	mux.HandleFunc("GET /ping", s.ping)

	// ── Mobile API (same lead handlers, different prefix) ─────────────────────
	mux.HandleFunc("GET /mobile/leads/search", auth(s.searchLeads))
	mux.HandleFunc("GET /mobile/leads/export", auth(s.exportLeads))
	mux.HandleFunc("GET /mobile/leads", auth(s.listLeads))
	mux.HandleFunc("POST /mobile/leads", auth(s.createLead))
	mux.HandleFunc("GET /mobile/leads/{id}", auth(s.getLead))
	mux.HandleFunc("PUT /mobile/leads/{id}", auth(s.updateLead))
	mux.HandleFunc("DELETE /mobile/leads/{id}", auth(s.deleteLead))
	mux.HandleFunc("PUT /mobile/leads/{id}/status", auth(s.updateLeadStatus))
	mux.HandleFunc("POST /mobile/leads/{id}/notes", auth(s.updateLeadNote))
	mux.HandleFunc("GET /mobile/leads/{id}/transcripts", auth(s.getLeadTranscripts))

	// ── Dial ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/dial/{lead_id}", auth(s.dialLead))
	mux.HandleFunc("POST /api/campaigns/{id}/dial/{lead_id}", auth(s.campaignDialLead))
	mux.HandleFunc("POST /api/campaigns/{id}/dial-all", auth(s.campaignDialAll))
	mux.HandleFunc("POST /api/campaigns/{id}/redial-failed", auth(s.campaignRedialFailed))
	mux.HandleFunc("POST /api/manual-call", auth(s.manualCall))

	// ── Telephony webhooks (no auth — provider-initiated) ──────────────────────
	mux.HandleFunc("GET /webhook/twilio", s.twilioTwiML)
	mux.HandleFunc("POST /webhook/twilio/status", s.twilioStatus)
	mux.HandleFunc("GET /webhook/exotel", s.exotelXML)
	mux.HandleFunc("POST /webhook/exotel", s.exotelXML)
	mux.HandleFunc("POST /webhook/exotel/status", s.exotelStatus)
	mux.HandleFunc("GET /exotel/recording-ready", s.exotelRecordingReady)
	mux.HandleFunc("POST /exotel/recording-ready", s.exotelRecordingReady)
	mux.HandleFunc("GET /crm-webhook", s.crmWebhook)
	mux.HandleFunc("POST /crm-webhook", s.crmWebhook)

	// ── Analytics (Phase 3A) ──────────────────────────────────────────────────
	mux.HandleFunc("GET /api/analytics/dashboard", auth(s.analyticsDashboard))
	mux.HandleFunc("GET /api/analytics/languages", auth(s.analyticsLanguages))
	mux.HandleFunc("GET /api/analytics/export", auth(s.analyticsExportCSV))
	mux.HandleFunc("GET /api/analytics/report", auth(s.analyticsExportReport))
	mux.HandleFunc("GET /api/analytics/scored-leads", auth(s.scoredLeads))

	// ── Billing (Phase 3B) ────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/billing/plans", s.listBillingPlans) // public
	mux.HandleFunc("GET /api/billing/subscription", auth(s.getSubscription))
	mux.HandleFunc("POST /api/billing/subscription", auth(s.createSubscription))
	mux.HandleFunc("DELETE /api/billing/subscription", auth(s.cancelSubscription))
	mux.HandleFunc("GET /api/billing/usage", auth(s.getBillingUsage))
	mux.HandleFunc("POST /api/billing/subscribe", auth(s.billingSubscribe))
	mux.HandleFunc("POST /api/billing/cancel", auth(s.cancelBillingPost))
	mux.HandleFunc("POST /api/billing/create-order", auth(s.createOrder))
	mux.HandleFunc("POST /api/billing/verify-payment", auth(s.verifyPayment))
	mux.HandleFunc("GET /api/billing/payments", auth(s.listPayments))
	mux.HandleFunc("GET /api/billing/invoices", auth(s.listInvoices))
	mux.HandleFunc("GET /api/billing/invoices/{number}/download", auth(s.downloadInvoice))
	mux.HandleFunc("POST /api/billing/webhook", s.razorpayWebhook) // public, HMAC-verified

	// ── WhatsApp Channels & Conversations (Phase 3C) ──────────────────────────
	mux.HandleFunc("GET /api/wa/channels", auth(s.listWAChannels))
	mux.HandleFunc("POST /api/wa/channels", auth(s.createWAChannel))
	mux.HandleFunc("PUT /api/wa/channels/{id}", auth(s.updateWAChannel))
	mux.HandleFunc("DELETE /api/wa/channels/{id}", auth(s.deleteWAChannel))
	mux.HandleFunc("PUT /api/wa/channels/{id}/toggle-ai", auth(s.toggleWAAI))
	mux.HandleFunc("GET /api/wa/conversations", auth(s.listWAConversations))
	mux.HandleFunc("GET /api/wa/conversations/{id}/history", auth(s.getWAHistory))
	// Frontend-shape routes (match WhatsAppTab.jsx). Python exposed a
	// single-config-per-org /api/wa/config and phone-scoped messages/toggle
	// routes; Go's native /api/wa/channels/* is ID-based. Add these aliases
	// so the existing UI works without a rewrite.
	mux.HandleFunc("GET /api/wa/config", auth(s.getWAConfig))
	mux.HandleFunc("POST /api/wa/config", auth(s.saveWAConfig))
	mux.HandleFunc("GET /api/wa/conversations/{phone}/messages", auth(s.getWAMessagesByPhone))
	mux.HandleFunc("POST /api/wa/toggle-ai/{phone}", auth(s.toggleWAAIByPhone))
	mux.HandleFunc("POST /api/wa/send", auth(s.sendWAMessage))

	// ── WhatsApp Provider Webhooks (Phase 3C) ─────────────────────────────────
	mux.HandleFunc("POST /wa/webhook/gupshup", s.waWebhookGupshup)
	mux.HandleFunc("POST /wa/webhook/wati", s.waWebhookWati)
	mux.HandleFunc("POST /wa/webhook/aisensei", s.waWebhookAiSensei)
	mux.HandleFunc("POST /wa/webhook/interakt", s.waWebhookInterakt)
	mux.HandleFunc("GET /wa/webhook/meta", s.waWebhookMeta)
	mux.HandleFunc("POST /wa/webhook/meta", s.waWebhookMeta)

	// ── CRM Integrations (Phase 3C) ───────────────────────────────────────────
	mux.HandleFunc("GET /api/integrations", auth(s.listIntegrations))
	mux.HandleFunc("POST /api/integrations", auth(s.createIntegration))
	mux.HandleFunc("DELETE /api/integrations/{id}", auth(s.deleteIntegration))

	// ── Knowledge Base (Phase 3C) ─────────────────────────────────────────────
	mux.HandleFunc("GET /api/knowledge", auth(s.listKnowledge))
	// POST path matches Python (routes.py:1255) — the RAG tab hits
	// /api/knowledge/upload. Without /upload the fetch 404s and the
	// "Upload & Embed PDF" button appears to silently do nothing.
	mux.HandleFunc("POST /api/knowledge/upload", auth(s.uploadKnowledge))
	mux.HandleFunc("GET /api/knowledge/{id}/download", auth(s.downloadKnowledge))
	mux.HandleFunc("DELETE /api/knowledge/{id}", auth(s.deleteKnowledge))

	// ── SSE (Phase 3C) ────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/sse/live-logs", auth(s.liveLogs))
	mux.HandleFunc("GET /api/live-logs", auth(s.liveLogs))
	mux.HandleFunc("GET /api/sse/campaign/{id}/events", auth(s.campaignEvents))
	mux.HandleFunc("GET /api/campaign-events", auth(s.campaignEventsQuery))

	// ── Test Email (Phase 3B) ─────────────────────────────────────────────────
	mux.HandleFunc("POST /api/test-email", auth(s.testEmail))

	// ── Misc ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/tasks", auth(s.listTasks))
	mux.HandleFunc("PUT /api/tasks/{id}/complete", auth(s.completeTask))
	mux.HandleFunc("GET /api/reports", auth(s.getReports))
	mux.HandleFunc("GET /api/pronunciation", auth(s.listPronunciations))
	mux.HandleFunc("POST /api/pronunciation", auth(s.addPronunciation))
	mux.HandleFunc("DELETE /api/pronunciation/{id}", auth(s.deletePronunciation))

	// ── Phase 4: LLM generation endpoints ────────────────────────────────────
	mux.HandleFunc("POST /api/products/{id}/scrape", auth(s.scrapeProduct))
	mux.HandleFunc("POST /api/products/{id}/generate-prompt", auth(s.generateProductPrompt))
	mux.HandleFunc("POST /api/products/{id}/generate-persona", auth(s.generateProductPersona))
	mux.HandleFunc("POST /api/organizations/{id}/generate-prompt", auth(s.generateOrgPrompt))
	mux.HandleFunc("GET /api/leads/{id}/draft-email", auth(s.draftLeadEmail))
}

// ── Response helpers ──────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent; nothing we can do
		_ = err
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// parseID reads a path parameter as int64.
func parseID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// emptyJSON returns [] for nil slices so the API never returns null.
func emptyJSON[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// coalesceStr returns fallback if s is empty.
func coalesceStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

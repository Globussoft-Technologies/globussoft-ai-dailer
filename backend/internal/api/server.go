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
	// adminAuth gates an endpoint behind a verified Admin role. Apply to any
	// route that exposes org-wide config, billing, PII firehoses, team
	// management, or write operations on shared resources. The frontend hides
	// these tabs for non-Admins, but without a server-side check a low-privileged
	// user could call the API directly (OWASP A01: broken access control).
	adminAuth := s.requireRole("Admin")

	// ── Auth ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/auth/signup", s.signup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/auth/me", auth(s.me))
	mux.HandleFunc("POST /api/auth/forgot-password", s.forgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", s.resetPassword)
	// Public SSO entry point — verified by signature on the inbound JWT,
	// not by our own middleware. See internal/api/sso.go for the flow.
	mux.HandleFunc("GET /api/auth/sso/jwt", s.ssoJWT)

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
	// Convenience lookup by phone — returns audio + interaction transcripts
	// in one fetch. Org-scoped at the DB layer so cross-tenant leakage is
	// impossible. Useful for external integrations that only have the phone.
	mux.HandleFunc("GET /api/leads/by-phone/{phone}/calls", auth(s.getLeadCallsByPhone))

	// ── Campaigns ─────────────────────────────────────────────────────────────
	// Admin-only across the board. The React route guard already redirects
	// non-Admins away from /campaigns, /logs and /analytics, but the API was
	// previously open to any authenticated user — Agent JWTs could read
	// /api/campaigns, per-campaign /leads, /stats, and /call-log directly.
	// Closing the read side here makes the page-level guard real (issue #51).
	// App.jsx fetches /api/campaigns at startup for everyone; non-Admin will
	// now get 403 there and the existing graceful "expected array" handler
	// falls back to []. LogsTab also fetches it for the campaign filter,
	// which only Admins can reach today anyway.
	mux.HandleFunc("GET /api/campaigns", adminAuth(s.listCampaigns))
	mux.HandleFunc("POST /api/campaigns", adminAuth(s.createCampaign))
	mux.HandleFunc("GET /api/campaigns/{id}", adminAuth(s.getCampaign))
	mux.HandleFunc("PUT /api/campaigns/{id}", adminAuth(s.updateCampaign))
	mux.HandleFunc("DELETE /api/campaigns/{id}", adminAuth(s.deleteCampaign))
	mux.HandleFunc("GET /api/campaigns/{id}/leads", adminAuth(s.listCampaignLeads))
	mux.HandleFunc("POST /api/campaigns/{id}/leads", adminAuth(s.addCampaignLeads))
	mux.HandleFunc("DELETE /api/campaigns/{id}/leads/{lead_id}", adminAuth(s.removeCampaignLead))
	mux.HandleFunc("GET /api/campaigns/{id}/stats", adminAuth(s.getCampaignStats))
	mux.HandleFunc("GET /api/campaigns/{id}/call-log", adminAuth(s.getCampaignCallLog))
	mux.HandleFunc("GET /api/campaigns/{id}/voice-settings", adminAuth(s.getCampaignVoiceSettings))
	mux.HandleFunc("PUT /api/campaigns/{id}/voice-settings", adminAuth(s.saveCampaignVoiceSettings))
	mux.HandleFunc("POST /api/campaigns/{id}/import-csv", adminAuth(s.importCampaignLeadsCSV))

	// ── Organizations ─────────────────────────────────────────────────────────
	// Org-level config (voice, timezone, system prompt) is Admin-only; reads
	// stay open so a CRM agent can see which voice/timezone is in effect.
	mux.HandleFunc("GET /api/organizations", auth(s.listOrgs))
	mux.HandleFunc("POST /api/organizations", adminAuth(s.createOrg))
	mux.HandleFunc("DELETE /api/organizations/{id}", adminAuth(s.deleteOrg))
	mux.HandleFunc("GET /api/organizations/{id}/voice-settings", auth(s.getOrgVoiceSettings))
	mux.HandleFunc("PUT /api/organizations/{id}/voice-settings", adminAuth(s.saveOrgVoiceSettings))
	mux.HandleFunc("PUT /api/organizations/{id}/timezone", adminAuth(s.updateOrgTimezone))

	// ── Products ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/organizations/{id}/products", auth(s.listProducts))
	mux.HandleFunc("POST /api/organizations/{id}/products", adminAuth(s.createProduct))
	mux.HandleFunc("PUT /api/products/{id}", adminAuth(s.updateProduct))
	mux.HandleFunc("DELETE /api/products/{id}", adminAuth(s.deleteProduct))
	mux.HandleFunc("GET /api/products/{id}/prompt", auth(s.getProductPrompt))
	mux.HandleFunc("PUT /api/products/{id}/prompt", adminAuth(s.updateProductPrompt))

	// ── Recordings ────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/recordings/{filename}", auth(s.serveRecording))
	// Browser-side MediaRecorder upload (Opus/webm at native sample rate).
	// Handler exists in misc.go; the route was missing, so the browser POST
	// was 404'ing and the high-quality recording was being lost — only the
	// 8kHz server-side WAV survived. That was the "recording not clear"
	// symptom reported after Quick-Add + Sim Web Call.
	mux.HandleFunc("POST /api/upload-recording", auth(s.uploadRecording))

	// ── WhatsApp Campaign Blast ────────────────────────────────────────────────
	mux.HandleFunc("POST /api/wa/campaign-blast/{campaign_id}", adminAuth(s.campaignBlast))
	mux.HandleFunc("GET /api/wa/campaign-blast/status/{job_id}", adminAuth(s.blastStatus))

	// ── Organizations: system prompt ──────────────────────────────────────────
	mux.HandleFunc("GET /api/organizations/{id}/system-prompt", auth(s.getOrgSystemPrompt))
	mux.HandleFunc("PUT /api/organizations/{id}/system-prompt", adminAuth(s.saveOrgSystemPrompt))

	// ── Campaign reviews ──────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/campaigns/{id}/call-reviews", auth(s.getCampaignCallReviews))

	// ── Transcript review ─────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/transcripts/{id}/review", auth(s.getTranscriptReview))

	// ── DND ───────────────────────────────────────────────────────────────────
	// /check is a read-only lookup any agent might need before placing a call;
	// list/add/import/remove modify org-level config so they're Admin-only.
	mux.HandleFunc("GET /api/dnd/check", auth(s.checkDND))
	mux.HandleFunc("GET /api/dnd/check/{phone}", auth(s.checkDNDByPhone))
	mux.HandleFunc("GET /api/dnd", adminAuth(s.listDND))
	mux.HandleFunc("POST /api/dnd", adminAuth(s.addDND))
	mux.HandleFunc("POST /api/dnd/import-csv", adminAuth(s.importDNDCSV))
	mux.HandleFunc("POST /api/dnd/import", adminAuth(s.importDNDCSV))
	mux.HandleFunc("DELETE /api/dnd/{id}", adminAuth(s.removeDND))

	// ── Webhooks ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/webhooks", adminAuth(s.listWebhooks))
	mux.HandleFunc("POST /api/webhooks", adminAuth(s.createWebhook))
	mux.HandleFunc("DELETE /api/webhooks/{id}", adminAuth(s.deleteWebhook))
	mux.HandleFunc("GET /api/webhooks/{id}/logs", adminAuth(s.getWebhookLogs))

	// ── Scheduled calls ───────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/scheduled-calls", adminAuth(s.listScheduledCalls))
	mux.HandleFunc("POST /api/scheduled-calls", adminAuth(s.createScheduledCall))
	mux.HandleFunc("DELETE /api/scheduled-calls/{id}", adminAuth(s.cancelScheduledCall))

	// ── Dashboard summary ────────────────────────────────────────────────────
	// 5 aggregate numbers for the CRM landing dashboard. Open to any
	// authenticated role so Viewers / Agents see real totals even though
	// /api/campaigns is admin-gated.
	mux.HandleFunc("GET /api/dashboard/summary", auth(s.dashboardSummary))

	// ── Team ──────────────────────────────────────────────────────────────────
	// Team management (invite, role change, delete) is strictly Admin.
	mux.HandleFunc("GET /api/team", adminAuth(s.listTeam))
	mux.HandleFunc("POST /api/team/invite", adminAuth(s.inviteTeamMember))
	mux.HandleFunc("PUT /api/team/{id}/role", adminAuth(s.updateTeamRole))
	mux.HandleFunc("DELETE /api/team/{id}", adminAuth(s.deleteTeamMember))

	// ── API keys ──────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/api-keys", adminAuth(s.listAPIKeys))
	mux.HandleFunc("POST /api/api-keys", adminAuth(s.createAPIKey))
	mux.HandleFunc("DELETE /api/api-keys/{id}", adminAuth(s.deleteAPIKey))

	// ── Onboarding ────────────────────────────────────────────────────────────
	// Reads are open (the App needs status to decide whether to render the
	// wizard); completing onboarding mutates org-wide config and is Admin-only.
	mux.HandleFunc("GET /api/onboarding", auth(s.getOnboarding))
	mux.HandleFunc("GET /api/onboarding/status", auth(s.onboardingStatus))
	mux.HandleFunc("POST /api/onboarding/complete", adminAuth(s.completeOnboarding))

	// ── Calling status (TRAI guard) ───────────────────────────────────────────
	mux.HandleFunc("GET /api/calling-status", auth(s.callingStatus))

	// ── Demo requests ─────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/demo-requests", auth(s.listDemoRequests))
	mux.HandleFunc("POST /api/demo-requests", s.createDemoRequest) // no auth — public form

	// ── WhatsApp legacy logs ──────────────────────────────────────────────────
	mux.HandleFunc("GET /api/whatsapp", adminAuth(s.listWhatsappLogs))

	// ── Debug / Health ────────────────────────────────────────────────────────
	// Debug endpoints expose internal state (recent dials, call timelines,
	// raw log lines) — keep them Admin-only.
	mux.HandleFunc("GET /api/debug/health", s.debugHealth)
	mux.HandleFunc("GET /api/debug/logs", adminAuth(s.debugLogs))
	mux.HandleFunc("GET /api/debug/last-dial", adminAuth(s.debugLastDial))
	mux.HandleFunc("GET /api/debug/call-timeline", adminAuth(s.debugCallTimeline))
	mux.HandleFunc("GET /api/debug/recording-config", adminAuth(s.debugRecordingConfig))
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
	// Single-lead dial stays open so a CRM agent can place calls to their
	// own leads. Bulk dial (dial-all, redial-failed) and the unrestricted
	// manual-call endpoint are Admin-only — they can fan out calls to many
	// numbers and have direct billing/reputation impact.
	mux.HandleFunc("POST /api/dial/{lead_id}", auth(s.dialLead))
	mux.HandleFunc("POST /api/campaigns/{id}/dial/{lead_id}", auth(s.campaignDialLead))
	mux.HandleFunc("POST /api/campaigns/{id}/dial-all", adminAuth(s.campaignDialAll))
	mux.HandleFunc("POST /api/campaigns/{id}/redial-failed", adminAuth(s.campaignRedialFailed))
	mux.HandleFunc("POST /api/manual-call", adminAuth(s.manualCall))

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
	// Org-wide analytics surfaces aggregate KPIs and PII counts; Admin-only.
	mux.HandleFunc("GET /api/analytics/dashboard", adminAuth(s.analyticsDashboard))
	mux.HandleFunc("GET /api/analytics/languages", adminAuth(s.analyticsLanguages))
	mux.HandleFunc("GET /api/analytics/export", adminAuth(s.analyticsExportCSV))
	mux.HandleFunc("GET /api/analytics/report", adminAuth(s.analyticsExportReport))
	mux.HandleFunc("GET /api/analytics/scored-leads", adminAuth(s.scoredLeads))

	// ── Billing (Phase 3B) ────────────────────────────────────────────────────
	// Subscribe/cancel/create-order/verify-payment all carry financial impact
	// and must be Admin. Read endpoints (subscription, usage, invoices) are
	// also Admin since they expose the org's billing posture.
	mux.HandleFunc("GET /api/billing/plans", s.listBillingPlans) // public
	mux.HandleFunc("GET /api/billing/subscription", adminAuth(s.getSubscription))
	mux.HandleFunc("POST /api/billing/subscription", adminAuth(s.createSubscription))
	mux.HandleFunc("DELETE /api/billing/subscription", adminAuth(s.cancelSubscription))
	mux.HandleFunc("GET /api/billing/usage", adminAuth(s.getBillingUsage))
	mux.HandleFunc("POST /api/billing/subscribe", adminAuth(s.billingSubscribe))
	mux.HandleFunc("POST /api/billing/cancel", adminAuth(s.cancelBillingPost))
	mux.HandleFunc("POST /api/billing/create-order", adminAuth(s.createOrder))
	mux.HandleFunc("POST /api/billing/verify-payment", adminAuth(s.verifyPayment))
	mux.HandleFunc("GET /api/billing/payments", adminAuth(s.listPayments))
	mux.HandleFunc("GET /api/billing/invoices", adminAuth(s.listInvoices))
	mux.HandleFunc("GET /api/billing/invoices/{number}/download", adminAuth(s.downloadInvoice))
	mux.HandleFunc("POST /api/billing/webhook", s.razorpayWebhook) // public, HMAC-verified

	// ── WhatsApp Channels & Conversations (Phase 3C) ──────────────────────────
	// WhatsApp tab is Admin-only in the nav; all of these manage org-wide
	// channels, credentials, and outbound message sending.
	mux.HandleFunc("GET /api/wa/channels", adminAuth(s.listWAChannels))
	mux.HandleFunc("POST /api/wa/channels", adminAuth(s.createWAChannel))
	mux.HandleFunc("PUT /api/wa/channels/{id}", adminAuth(s.updateWAChannel))
	mux.HandleFunc("DELETE /api/wa/channels/{id}", adminAuth(s.deleteWAChannel))
	mux.HandleFunc("PUT /api/wa/channels/{id}/toggle-ai", adminAuth(s.toggleWAAI))
	mux.HandleFunc("GET /api/wa/conversations", adminAuth(s.listWAConversations))
	mux.HandleFunc("GET /api/wa/conversations/{id}/history", adminAuth(s.getWAHistory))
	mux.HandleFunc("GET /api/wa/config", adminAuth(s.getWAConfig))
	mux.HandleFunc("POST /api/wa/config", adminAuth(s.saveWAConfig))
	mux.HandleFunc("GET /api/wa/conversations/{phone}/messages", adminAuth(s.getWAMessagesByPhone))
	mux.HandleFunc("POST /api/wa/toggle-ai/{phone}", adminAuth(s.toggleWAAIByPhone))
	mux.HandleFunc("POST /api/wa/send", adminAuth(s.sendWAMessage))

	// ── WhatsApp Provider Webhooks (Phase 3C) ─────────────────────────────────
	mux.HandleFunc("POST /wa/webhook/gupshup", s.waWebhookGupshup)
	mux.HandleFunc("POST /wa/webhook/wati", s.waWebhookWati)
	mux.HandleFunc("POST /wa/webhook/aisensei", s.waWebhookAiSensei)
	mux.HandleFunc("POST /wa/webhook/interakt", s.waWebhookInterakt)
	mux.HandleFunc("GET /wa/webhook/meta", s.waWebhookMeta)
	mux.HandleFunc("POST /wa/webhook/meta", s.waWebhookMeta)

	// ── CRM Integrations (Phase 3C) ───────────────────────────────────────────
	// External CRM tokens (HubSpot/Salesforce) — credential management is
	// strictly Admin to prevent data-exfiltration vectors via attacker-owned tokens.
	mux.HandleFunc("GET /api/integrations", adminAuth(s.listIntegrations))
	mux.HandleFunc("POST /api/integrations", adminAuth(s.createIntegration))
	mux.HandleFunc("DELETE /api/integrations/{id}", adminAuth(s.deleteIntegration))

	// ── Knowledge Base (Phase 3C) ─────────────────────────────────────────────
	// RAG knowledge tab is Admin-only in the nav.
	mux.HandleFunc("GET /api/knowledge", adminAuth(s.listKnowledge))
	mux.HandleFunc("POST /api/knowledge/upload", adminAuth(s.uploadKnowledge))
	mux.HandleFunc("GET /api/knowledge/{id}/download", adminAuth(s.downloadKnowledge))
	mux.HandleFunc("DELETE /api/knowledge/{id}", adminAuth(s.deleteKnowledge))

	// ── SSE (Phase 3C) ────────────────────────────────────────────────────────
	// Live log + campaign-event streams contain real lead PII (names + phone
	// numbers) for the entire org. SSE endpoints authenticate via a
	// short-lived ?ticket=… (kind="sse") minted by /api/sse/ticket — the
	// long-lived auth JWT must never appear in URLs. (issue #80)
	mux.HandleFunc("GET /api/sse/ticket", adminAuth(s.sseTicket))
	mux.HandleFunc("GET /api/sse/live-logs", s.requireSSETicket(s.liveLogs))
	mux.HandleFunc("GET /api/live-logs", s.requireSSETicket(s.liveLogs))
	mux.HandleFunc("GET /api/sse/campaign/{id}/events", s.requireSSETicket(s.campaignEvents))
	mux.HandleFunc("GET /api/campaign-events", s.requireSSETicket(s.campaignEventsQuery))

	// ── Test Email (Phase 3B) ─────────────────────────────────────────────────
	mux.HandleFunc("POST /api/test-email", adminAuth(s.testEmail))

	// ── Misc ──────────────────────────────────────────────────────────────────
	// /tasks and draft-email are agent-facing; reports and pronunciation
	// belong to Admin (they expose org config / aggregate report data).
	mux.HandleFunc("GET /api/tasks", auth(s.listTasks))
	mux.HandleFunc("PUT /api/tasks/{id}/complete", auth(s.completeTask))
	mux.HandleFunc("GET /api/reports", adminAuth(s.getReports))
	mux.HandleFunc("GET /api/pronunciation", adminAuth(s.listPronunciations))
	mux.HandleFunc("POST /api/pronunciation", adminAuth(s.addPronunciation))
	mux.HandleFunc("DELETE /api/pronunciation/{id}", adminAuth(s.deletePronunciation))

	// ── Phase 4: LLM generation endpoints ────────────────────────────────────
	// Generation endpoints touch Gemini/Groq (cost) and rewrite org/product
	// prompts (config) — Admin-only. draft-email runs against a single lead
	// and is fine for any agent.
	mux.HandleFunc("POST /api/products/{id}/scrape", adminAuth(s.scrapeProduct))
	mux.HandleFunc("POST /api/products/{id}/generate-prompt", adminAuth(s.generateProductPrompt))
	mux.HandleFunc("POST /api/products/{id}/generate-persona", adminAuth(s.generateProductPersona))
	mux.HandleFunc("POST /api/organizations/{id}/generate-prompt", adminAuth(s.generateOrgPrompt))
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

package api

import (
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/wa"
)

// POST /wa/webhook/gupshup
func (s *Server) waWebhookGupshup(w http.ResponseWriter, r *http.Request) {
	s.handleWAWebhook(w, r, "gupshup", wa.ParseGupshup)
}

// POST /wa/webhook/wati
func (s *Server) waWebhookWati(w http.ResponseWriter, r *http.Request) {
	s.handleWAWebhook(w, r, "wati", wa.ParseWati)
}

// POST /wa/webhook/aisensei
func (s *Server) waWebhookAiSensei(w http.ResponseWriter, r *http.Request) {
	s.handleWAWebhook(w, r, "aisensei", wa.ParseAiSensei)
}

// POST /wa/webhook/interakt
func (s *Server) waWebhookInterakt(w http.ResponseWriter, r *http.Request) {
	s.handleWAWebhook(w, r, "interakt", wa.ParseInterakt)
}

// POST /wa/webhook/meta — inbound messages
// GET  /wa/webhook/meta — Meta hub.challenge verification
func (s *Server) waWebhookMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Hub challenge verification
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")
		if mode == "subscribe" && token == s.cfg.MetaVerifyToken && challenge != "" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(challenge))
			return
		}
		writeError(w, http.StatusForbidden, "verification failed")
		return
	}
	s.handleWAWebhook(w, r, "meta", wa.ParseMeta)
}

// handleWAWebhook is the shared handler for all inbound WA provider webhooks.
func (s *Server) handleWAWebhook(w http.ResponseWriter, r *http.Request, provider string,
	parser func([]byte) (*wa.IncomingMessage, error)) {

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	msg, err := parser(body)
	if err != nil || msg == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Look up the channel config by provider + destination phone
	cfg, _ := s.db.GetWAChannelConfigByPhone(provider, msg.ToPhone)
	if cfg == nil || !cfg.AIEnabled {
		// No AI — just log the message
		if convID, err := s.db.GetOrCreateWAConversation(0, msg.FromPhone, provider); err == nil {
			_, _ = s.db.SaveWAMessage(convID, "inbound", msg.Text, msg.MessageType, msg.ProviderMsgID)
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// Process with AI agent (async so we return 200 quickly)
	go func() {
		if s.waAgent == nil {
			return
		}
		channelCfg := s.waChannelConfig(cfg.Provider, cfg.PhoneNumber, cfg.APIKey, cfg.AppID)
		reply, err := s.waAgent.ProcessIncoming(r.Context(), channelCfg, msg)
		if err != nil {
			s.logger.Warn("waWebhook: agent failed",
				zap.String("provider", provider), zap.Error(err))
			return
		}
		if reply == "" {
			return
		}
		if err := s.waSender.SendText(r.Context(), channelCfg, msg.FromPhone, reply); err != nil {
			s.logger.Warn("waWebhook: send reply failed",
				zap.String("provider", provider), zap.Error(err))
		}
	}()

	w.WriteHeader(http.StatusOK)
}

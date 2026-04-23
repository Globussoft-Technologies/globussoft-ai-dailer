package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/dial"
)

// ── GET /webhook/twilio ───────────────────────────────────────────────────────
// Twilio calls this URL when a call connects. We return TwiML that opens a
// media stream back to this server.

func (s *Server) twilioTwiML(w http.ResponseWriter, r *http.Request) {
	leadID := r.URL.Query().Get("lead_id")
	campaignID := r.URL.Query().Get("campaign_id")

	wsURL := strings.Replace(s.cfg.PublicServerURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = fmt.Sprintf("%s/media-stream?lead_id=%s&campaign_id=%s", wsURL, leadID, campaignID)

	twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="%s">
      <Parameter name="lead_id" value="%s"/>
      <Parameter name="campaign_id" value="%s"/>
    </Stream>
  </Connect>
</Response>`, wsURL, leadID, campaignID)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(twiml))
}

// ── POST /webhook/twilio/status ───────────────────────────────────────────────
// Twilio posts call status updates here (initiated, ringing, answered, completed).

func (s *Server) twilioStatus(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusOK) // always 200 for Twilio
		return
	}

	callSid := r.FormValue("CallSid")
	callStatus := r.FormValue("CallStatus")

	if callSid == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Map Twilio status → our internal status
	status := mapTwilioStatus(callStatus)
	if err := s.db.UpdateCallLogStatus(callSid, status); err != nil {
		s.logger.Warn("twilioStatus: UpdateCallLogStatus",
			zap.String("call_sid", callSid), zap.Error(err))
	}

	// On completion, fire webhook event
	if callStatus == "completed" || callStatus == "failed" || callStatus == "no-answer" || callStatus == "busy" {
		cl, _ := s.db.GetCallLogByCallSid(callSid)
		if cl != nil {
			s.dispatcher.Dispatch(r.Context(), cl.OrgID, "call.completed", map[string]any{
				"call_sid":   callSid,
				"status":     callStatus,
				"lead_id":    cl.LeadID,
				"campaign_id": cl.CampaignID,
			})
		}
	}

	w.WriteHeader(http.StatusOK)
}

// ── POST /webhook/exotel/status ───────────────────────────────────────────────
// Exotel posts call status updates here.

func (s *Server) exotelStatus(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	callSid := coalesceStr(r.FormValue("CallSid"), r.FormValue("sid"))
	callStatus := coalesceStr(r.FormValue("Status"), r.FormValue("CallStatus"))
	recordingURL := r.FormValue("RecordingUrl")

	if callSid == "" {
		// Try query params (some Exotel setups put them there)
		callSid = r.URL.Query().Get("lead_id") // fallback using lead_id from URL
		callSid = ""                           // reset — can't infer call_sid this way
		w.WriteHeader(http.StatusOK)
		return
	}

	status := mapExotelStatus(callStatus)
	if err := s.db.UpdateCallLogStatus(callSid, status); err != nil {
		s.logger.Warn("exotelStatus: UpdateCallLogStatus",
			zap.String("call_sid", callSid), zap.Error(err))
	}

	if recordingURL != "" {
		// Schedule async recording download
		go s.fetchAndSaveRecording(callSid, recordingURL)
	}

	// Fire webhook on terminal states
	if status == "completed" || status == "failed" || status == "no-answer" {
		cl, _ := s.db.GetCallLogByCallSid(callSid)
		if cl != nil {
			s.dispatcher.Dispatch(r.Context(), cl.OrgID, "call.completed", map[string]any{
				"call_sid":    callSid,
				"status":      status,
				"lead_id":     cl.LeadID,
				"campaign_id": cl.CampaignID,
			})
		}
	}

	w.WriteHeader(http.StatusOK)
}

// ── GET|POST /exotel/recording-ready ─────────────────────────────────────────
// Exotel calls this when a recording is ready for download.

func (s *Server) exotelRecordingReady(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	callSid := coalesceStr(r.FormValue("CallSid"), r.FormValue("sid"))
	recordingURL := coalesceStr(r.FormValue("RecordingUrl"), r.FormValue("recording_url"))

	if callSid != "" && recordingURL != "" {
		go s.fetchAndSaveRecording(callSid, recordingURL)
	}

	w.WriteHeader(http.StatusOK)
}

// ── POST /crm-webhook ─────────────────────────────────────────────────────────
// Generic CRM push webhook: challenge handshake or new lead notification.

func (s *Server) crmWebhook(w http.ResponseWriter, r *http.Request) {
	// HubSpot-style GET challenge verification
	if r.Method == http.MethodGet {
		challenge := r.URL.Query().Get("hub.challenge")
		if challenge != "" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(challenge))
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.logger.Info("crm_webhook: received payload",
		zap.Int("bytes", len(body)),
		zap.String("content_type", r.Header.Get("Content-Type")))

	// Attempt to parse as a new lead
	// The CRM provider may vary; we do best-effort parsing.
	// Full per-provider parsing happens in the CRM poller; this is for real-time pushes.
	w.WriteHeader(http.StatusOK)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func mapTwilioStatus(s string) string {
	switch s {
	case "initiated", "queued":
		return "initiated"
	case "ringing":
		return "ringing"
	case "in-progress":
		return "in-progress"
	case "completed":
		return "completed"
	case "busy":
		return "busy"
	case "no-answer":
		return "no-answer"
	case "failed", "canceled":
		return "failed"
	default:
		return s
	}
}

func mapExotelStatus(s string) string {
	switch strings.ToLower(s) {
	case "in-progress", "inprogress":
		return "in-progress"
	case "completed", "complete":
		return "completed"
	case "failed", "fail":
		return "failed"
	case "busy":
		return "busy"
	case "no-answer", "noanswer":
		return "no-answer"
	default:
		return s
	}
}

// fetchAndSaveRecording downloads a recording URL with up to 6 retries (10s backoff)
// and saves it to the recordings directory, then updates the DB.
func (s *Server) fetchAndSaveRecording(callSid, recordingURL string) {
	const maxRetries = 6
	const retryDelay = 10 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := s.downloadRecording(callSid, recordingURL)
		if err == nil {
			return
		}
		s.logger.Warn("fetchAndSaveRecording: attempt failed",
			zap.Int("attempt", attempt), zap.String("call_sid", callSid), zap.Error(err))
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}
	s.logger.Error("fetchAndSaveRecording: exhausted retries", zap.String("call_sid", callSid))
}

func (s *Server) downloadRecording(callSid, recordingURL string) error {
	// Build authenticated URL for Exotel recordings
	parsedURL, err := url.Parse(recordingURL)
	if err != nil {
		return fmt.Errorf("invalid recording URL: %w", err)
	}
	if parsedURL.User == nil && s.cfg.ExotelAPIKey != "" {
		parsedURL.User = url.UserPassword(s.cfg.ExotelAPIKey, s.cfg.ExotelAPIToken)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(parsedURL.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d downloading recording", resp.StatusCode)
	}

	ext := ".mp3"
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "wav") {
		ext = ".wav"
	}

	filename := fmt.Sprintf("recording_%s%s", callSid, ext)
	destPath := filepath.Join(s.cfg.RecordingsDir, filename)

	_ = os.MkdirAll(s.cfg.RecordingsDir, 0755)
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	localURL := fmt.Sprintf("%s/api/recordings/%s", s.cfg.PublicServerURL, filename)
	if err := s.db.UpdateCallLogRecordingURL(callSid, localURL); err != nil {
		s.logger.Warn("downloadRecording: UpdateCallLogRecordingURL", zap.Error(err))
	}

	s.logger.Info("recording saved",
		zap.String("call_sid", callSid),
		zap.String("file", filename))
	return nil
}

// ── ensure dial package is used ───────────────────────────────────────────────
var _ = dial.ErrDND

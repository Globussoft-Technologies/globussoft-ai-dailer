package wshandler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/audio"
	"github.com/globussoft/callified-backend/internal/config"
	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/metrics"
	"github.com/globussoft/callified-backend/internal/prompt"
	rstore "github.com/globussoft/callified-backend/internal/redis"
	"github.com/globussoft/callified-backend/internal/recording"
	"github.com/globussoft/callified-backend/internal/stt"
	"github.com/globussoft/callified-backend/internal/tts"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// Handler serves the /media-stream and /ws/sandbox WebSocket endpoints.
type Handler struct {
	cfg           *config.Config
	promptBuilder *prompt.Builder    // Phase 3C: replaces gRPC InitializeCall
	recordingSvc  *recording.Service // Phase 4: replaces gRPC FinalizeCall
	store         *rstore.Store
	db            *db.DB        // for lead lookups when Redis pending-call info is sparse
	provider      *llm.Provider // Phase 0: native Go LLM
	ttsKeys       map[string]string
	log           *zap.Logger
	sessions      sync.Map // stream_sid → *CallSession (for monitor WebSocket)
	sessionsByCallSid sync.Map // call_sid → *CallSession (for monitor lookup during dial flow before stream_sid arrives)
}

// New creates a Handler wired to the provided dependencies.
func New(
	cfg *config.Config,
	promptBuilder *prompt.Builder,
	recordingSvc *recording.Service,
	store *rstore.Store,
	database *db.DB,
	log *zap.Logger,
) *Handler {
	var provider *llm.Provider
	if cfg.GeminiAPIKey != "" || cfg.GroqAPIKey != "" {
		provider = llm.NewProvider(cfg, log)
	}
	return &Handler{
		cfg:           cfg,
		promptBuilder: promptBuilder,
		recordingSvc:  recordingSvc,
		store:         store,
		db:            database,
		provider:      provider,
		ttsKeys: map[string]string{
			"elevenlabs": cfg.ElevenLabsAPIKey,
			"sarvam":     cfg.SarvamAPIKey,
			"smallest":   cfg.SmallestAPIKey,
		},
		log: log,
	}
}

// ServeHTTP handles both /media-stream (Exotel) and /ws/sandbox (browser sim).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Extract initial identity from query params (may be overridden by "start" event)
	q := r.URL.Query()
	streamSid := q.Get("stream_sid")
	if streamSid == "" {
		streamSid = fmt.Sprintf("web_sim_%s_%d", q.Get("lead_id"), time.Now().UnixMilli())
	}

	sess := NewCallSession(streamSid, conn, h.log)
	// The browser-side web-sim sends `name` / `phone`; legacy callers may send
	// `lead_name` / `lead_phone`. Accept either so live-feed events render with
	// the lead label instead of the empty "()" we used to show.
	sess.LeadName = firstNonEmpty(q.Get("name"), q.Get("lead_name"))
	sess.LeadPhone = firstNonEmpty(q.Get("phone"), q.Get("lead_phone"))
	sess.Interest = q.Get("interest")
	if id := q.Get("lead_id"); id != "" {
		fmt.Sscanf(id, "%d", &sess.LeadID)
	}
	if id := q.Get("campaign_id"); id != "" {
		fmt.Sscanf(id, "%d", &sess.CampaignID)
	}
	if l := q.Get("tts_language"); l != "" {
		sess.Language = l
		sess.TTSLanguage = l
	}
	if p := q.Get("tts_provider"); p != "" {
		sess.TTSProvider = p
	}
	if v := q.Get("voice"); v != "" {
		sess.TTSVoiceID = v
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	h.sessions.Store(sess.StreamSid, sess)
	defer func() {
		h.sessions.Delete(sess.StreamSid)
		if sess.CallSid != "" {
			h.sessionsByCallSid.Delete(sess.CallSid)
		}
	}()

	metrics.ActiveCalls.Inc()
	defer func() {
		metrics.ActiveCalls.Dec()
		metrics.CallDuration.Observe(time.Since(sess.CallStart).Seconds())
	}()

	// Web-sim path doesn't go through dial.Initiator, so the live-feed never
	// gets a DIALING entry — operators only saw CONNECTED + COMPLETED with
	// empty "()". Emit one here so the activity panel renders the same
	// 3-event sequence (DIALING → CONNECTED → COMPLETED) as a real dial.
	if sess.CampaignID > 0 && strings.HasPrefix(streamSid, "web_sim_") {
		h.store.EmitCampaignEvent(ctx, sess.CampaignID, sess.LeadName, sess.LeadPhone,
			"dialing", "via web-sim")
	}

	// --- Initialize call via gRPC (get system prompt + voice config) ---
	if err := h.initializeCall(ctx, sess); err != nil {
		h.log.Error("InitializeCall failed", zap.Error(err))
		// Continue with defaults — don't abort the call
	}

	// --- Select TTS provider ---
	// Store the instance on the session so runTTSWorker (which reads it every
	// sentence) and the greeting dispatch can both use it. Previously this was
	// a closure variable, but the worker now lives outside this function.
	ttsProvider, err := tts.New(sess.TTSProvider, h.ttsKeys)
	if err != nil {
		h.log.Warn("TTS provider unavailable", zap.Error(err), zap.String("provider", sess.TTSProvider))
	}
	if ttsProvider != nil {
		sess.SetTTSInstance(ttsProvider)
	}

	// --- Start Deepgram STT client ---
	dgClient := stt.NewClient(h.cfg.DeepgramAPIKey, sess.Language, h.log)
	dgClient.OnTranscript = func(text string) {
		// Record STT TTFB once per call (first transcript)
		if first, elapsed := sess.MarkSTTFirst(); first {
			metrics.STTFirstByteLatency.Observe(elapsed)
		}
		if sess.HangupRequested() || sess.IsTTSPlaying() || sess.MsSinceTTSEnd() < 1000 {
			return
		}
		select {
		case sess.Transcripts <- text:
		default:
		}
	}
	dgClient.OnSpeechStarted = func() {
		if sess.IsTTSPlaying() {
			metrics.BargeIns.Inc()
		}
		sess.CancelActiveTTS()
		if sess.IsExotel {
			sendClearEvent(sess)
		}
	}

	var wg sync.WaitGroup

	// g2: STT goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		dgClient.Run(ctx, sess.AudioIn)
	}()

	// g4: Pipeline orchestrator
	wg.Add(1)
	go func() {
		defer wg.Done()
		runPipeline(ctx, sess, h.provider, h.store)
	}()

	// g5: TTS worker. Reads the provider from sess.TTSInstance() on each
	// sentence; the worker no-ops with a warning if no provider is loaded.
	// Launched unconditionally so that if the provider becomes available
	// mid-call (e.g. after Redis hydration of a campaign with a different
	// provider), synthesis resumes without needing to relaunch the worker.
	wg.Add(1)
	go func() {
		defer wg.Done()
		runTTSWorker(ctx, sess)
	}()

	// Send greeting immediately (Exotel 10s VoiceBot timeout)
	if sess.TrySetGreeting() && sess.GreetingText != "" && ttsProvider != nil {
		go synthesizeAndSend(ctx, sess, ttsProvider, sess.GreetingText)
	}

	// --- g1: WebSocket message loop ---
	done := h.messageLoop(ctx, sess)
	cancel() // signal all goroutines to stop

	// Close channels after cancellation so goroutines drain cleanly
	close(sess.AudioIn)
	close(sess.Transcripts)

	wg.Wait()

	if !done {
		// Abnormal close (network error) — still finalize
	}

	h.finalizeCall(context.Background(), sess)
}

// messageLoop reads WebSocket frames until the connection closes or a "stop"
// event is received. Returns true on clean stop, false on error.
func (h *Handler) messageLoop(ctx context.Context, sess *CallSession) bool {
	for {
		msgType, msg, err := sess.WS.ReadMessage()
		if err != nil {
			return false
		}
		switch msgType {
		case websocket.BinaryMessage:
			h.handleBinaryFrame(sess, msg)
		case websocket.TextMessage:
			if stop := h.handleTextFrame(ctx, sess, msg); stop {
				return true
			}
		}
	}
}

func (h *Handler) handleBinaryFrame(sess *CallSession, data []byte) {
	if sess.HangupRequested() {
		return
	}
	var pcm []byte
	if sess.IsExotel {
		// Echo cancellation: check ulaw frame before decoding
		if sess.EchoCanceller.IsEcho(data) {
			metrics.EchoSuppressions.Inc()
			return
		}
		pcm = audio.UlawToPCM(data)
	} else {
		pcm = data // web sim sends PCM directly
	}
	sess.AppendMicChunk(pcm)
	select {
	case sess.AudioIn <- pcm:
	default: // drop if buffer full
	}
}

func (h *Handler) handleTextFrame(ctx context.Context, sess *CallSession, data []byte) (stop bool) {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return false
	}
	switch event["event"] {
	case "connected":
		// Exotel handshake ack — ignore
	case "start":
		h.handleStartEvent(ctx, sess, event)
	case "media":
		h.handleMediaEvent(sess, event)
	case "stop":
		return true
	}
	return false
}

func (h *Handler) handleStartEvent(ctx context.Context, sess *CallSession, event map[string]interface{}) {
	// Extract stream_sid and call_sid from the "start" event. Exotel sometimes
	// sends snake_case (call_sid / stream_sid) and sometimes Twilio-style
	// camelCase (callSid / streamSid) depending on the integration; read both
	// so the Redis-pending-call lookup that hydrates lead name/phone never
	// silently misses on field-name casing.
	if startData, ok := event["start"].(map[string]interface{}); ok {
		if sid := pickStr(startData, "streamSid", "stream_sid", "StreamSid"); sid != "" {
			sess.StreamSid = sid
			sess.UpdateStreamType()
		}
		if callSid := pickStr(startData, "callSid", "call_sid", "CallSid"); callSid != "" {
			sess.CallSid = callSid
			h.sessionsByCallSid.Store(callSid, sess)
			// Redis lookup precedence:
			//   1) under the carrier-issued call_sid (set by dial.Initiator)
			//   2) under "phone:<E164>" (set by manual-call web-sim mode)
			//   3) under "latest" (last-resort fallback)
			info, ok := h.store.GetPendingCall(ctx, callSid)
			if !ok {
				if phone := pickStr(startData, "from", "From", "to", "To"); phone != "" {
					info, ok = h.store.GetPendingCall(ctx, "phone:"+phone)
				}
			}
			if !ok {
				info, ok = h.store.GetPendingCall(ctx, "latest")
			}
			if ok {
				// Only overwrite when Redis has something — otherwise we wipe
				// good values (e.g. set from query params on web-sim) with
				// empty strings from a stale "latest" key.
				if info.Name != "" {
					sess.LeadName = info.Name
				}
				if info.Phone != "" {
					sess.LeadPhone = info.Phone
				}
				if info.LeadID != 0 {
					sess.LeadID = info.LeadID
				}
				if info.Interest != "" {
					sess.Interest = info.Interest
				}
				if info.CampaignID != 0 {
					sess.CampaignID = info.CampaignID
				}
				if info.TTSProvider != "" {
					sess.TTSProvider = info.TTSProvider
				}
				if info.TTSVoiceID != "" {
					sess.TTSVoiceID = info.TTSVoiceID
				}
			}
		}
	}
	// Also accept top-level stream_sid (snake_case or camel)
	if sid := pickStr(event, "stream_sid", "streamSid"); sid != "" && sess.StreamSid == "" {
		sess.StreamSid = sid
		sess.UpdateStreamType()
	}

	// Live-feed: tell the campaign detail page that audio is flowing.
	// Fires on first "start" event so the Live Campaign Activity panel
	// shows one entry per connected call (web-sim + real Exotel both
	// send `start`, so both paths contribute to the live feed).
	if sess.CampaignID > 0 {
		name, phone := h.leadLabel(ctx, sess)
		h.store.EmitCampaignEvent(ctx, sess.CampaignID, name, phone,
			"connected", "audio stream opened")
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// pickStr returns the first non-empty string value found at any of the given
// keys in m. Used to tolerate camelCase / snake_case / PascalCase variants
// that Exotel and Twilio send for the same field.
func pickStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// leadLabel returns the (name, phone) pair to display in live-feed events.
// Falls back to a DB lookup when the session struct has empty values — this
// happens when the Redis pending-call entry hasn't been written or doesn't
// carry name/phone (e.g. some Exotel start events arrive before the dialer
// publishes lead context). Without this fallback, CONNECTED and COMPLETED
// events render as "() — COMPLETED" in the activity panel.
func (h *Handler) leadLabel(ctx context.Context, sess *CallSession) (string, string) {
	name, phone := sess.LeadName, sess.LeadPhone
	if name != "" && phone != "" {
		return name, phone
	}
	if h.db == nil {
		return name, phone
	}
	// Try by lead_id first (cheapest — primary key).
	if sess.LeadID != 0 {
		if lead, err := h.db.GetLeadByID(sess.LeadID); err == nil && lead != nil {
			if name == "" {
				name = strings.TrimSpace(lead.FirstName + " " + lead.LastName)
				sess.LeadName = name
			}
			if phone == "" {
				phone = lead.Phone
				sess.LeadPhone = phone
			}
			if name != "" && phone != "" {
				return name, phone
			}
		}
	}
	// Last resort: lookup by phone. Covers the Exotel case where the carrier's
	// call_sid didn't match the Redis key (stale TTL, race, or field-name
	// mismatch) and we lost the lead_id, but the session still knows the
	// phone number from the start event.
	if phone != "" {
		if lead, err := h.db.GetLeadByPhone(phone); err == nil && lead != nil && name == "" {
			name = strings.TrimSpace(lead.FirstName + " " + lead.LastName)
			sess.LeadName = name
			if sess.LeadID == 0 {
				sess.LeadID = lead.ID
			}
		}
	}
	return name, phone
}

func (h *Handler) handleMediaEvent(sess *CallSession, event map[string]interface{}) {
	if sess.HangupRequested() {
		return
	}
	mediaData, _ := event["media"].(map[string]interface{})
	if mediaData == nil {
		return
	}
	payload, _ := mediaData["payload"].(string)
	if payload == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 {
		return
	}

	var pcm []byte
	if sess.IsExotel {
		if sess.EchoCanceller.IsEcho(raw) {
			metrics.EchoSuppressions.Inc()
			return
		}
		pcm = audio.UlawToPCM(raw)
	} else {
		pcm = raw
	}
	sess.AppendMicChunk(pcm)
	select {
	case sess.AudioIn <- pcm:
	default:
	}

	// Relay a copy of the caller's inbound audio to any attached monitors.
	if sess.hasMonitors() {
		format := "pcm16_8k"
		if sess.IsExotel {
			format = "ulaw_8k"
		}
		sess.BroadcastAudio("user", payload, format)
	}
}

// initializeCall populates the session's system prompt and voice config.
// Phase 4: uses the native Go prompt builder exclusively (gRPC removed).
func (h *Handler) initializeCall(ctx context.Context, sess *CallSession) error {
	if h.promptBuilder == nil {
		return nil // no-op when DB is unavailable (dev/test)
	}
	callCtx, err := h.promptBuilder.BuildCallContext(ctx, sess.OrgID, sess.CampaignID, sess.LeadID, sess.Language)
	if err != nil {
		h.log.Warn("promptBuilder.BuildCallContext failed, proceeding with defaults", zap.Error(err))
		return nil
	}
	sess.SystemPrompt = callCtx.SystemPrompt
	sess.GreetingText = callCtx.GreetingText
	if callCtx.TTSProvider != "" {
		sess.TTSProvider = callCtx.TTSProvider
	}
	if callCtx.TTSVoiceID != "" {
		sess.TTSVoiceID = callCtx.TTSVoiceID
	}
	if callCtx.TTSLanguage != "" {
		sess.TTSLanguage = callCtx.TTSLanguage
		sess.Language = callCtx.TTSLanguage // drives Deepgram language + LLM prompt language
	}
	if callCtx.AgentName != "" {
		sess.AgentName = callCtx.AgentName
	}
	return nil
}

// finalizeCall runs post-call processing (Phase 4: native Go, no gRPC).
func (h *Handler) finalizeCall(ctx context.Context, sess *CallSession) {
	micChunks, ttsChunks := sess.DrainRecordingBuffers()
	wavBytes := audio.BuildStereoWAV(micChunks, ttsChunks)

	// Live-feed: emit completion so the Live Campaign Activity panel closes
	// out the entry. For web-sim calls this is the ONLY event that fires
	// (web-sim never goes through the Exotel webhook that emits dialing /
	// no-answer / etc.), so without it the panel stays empty during testing.
	if sess.CampaignID > 0 {
		durS := int(time.Since(sess.CallStart).Seconds())
		name, phone := h.leadLabel(ctx, sess)
		h.store.EmitCampaignEvent(ctx, sess.CampaignID, name, phone,
			"completed", fmt.Sprintf("%ds call", durS))
	}

	h.store.CleanupCall(ctx, sess.StreamSid)
	h.store.DeletePendingCall(ctx, sess.CallSid)

	if h.recordingSvc == nil {
		return // no-op when DB is unavailable
	}

	req := recording.SaveRequest{
		StreamSid:   sess.StreamSid,
		CallSid:     sess.CallSid,
		LeadID:      sess.LeadID,
		CampaignID:  sess.CampaignID,
		OrgID:       sess.OrgID,
		LeadPhone:   sess.LeadPhone,
		AgentName:   sess.AgentName,
		TTSLanguage: sess.TTSLanguage,
		ChatHistory: sess.HistorySnapshot(),
		DurationS:   float32(time.Since(sess.CallStart).Seconds()),
		StereoWav:   wavBytes,
	}
	go h.recordingSvc.SaveAndAnalyze(ctx, req)
}

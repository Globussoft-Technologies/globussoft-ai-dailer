package wshandler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/audio"
	"github.com/globussoft/callified-backend/internal/config"
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
	provider      *llm.Provider // Phase 0: native Go LLM
	ttsKeys       map[string]string
	log           *zap.Logger
	sessions      sync.Map // stream_sid → *CallSession (for monitor WebSocket)
}

// New creates a Handler wired to the provided dependencies.
func New(
	cfg *config.Config,
	promptBuilder *prompt.Builder,
	recordingSvc *recording.Service,
	store *rstore.Store,
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
	sess.LeadName = q.Get("lead_name")
	sess.LeadPhone = q.Get("lead_phone")
	sess.Interest = q.Get("interest")
	if id := q.Get("lead_id"); id != "" {
		fmt.Sscanf(id, "%d", &sess.LeadID)
	}
	if id := q.Get("campaign_id"); id != "" {
		fmt.Sscanf(id, "%d", &sess.CampaignID)
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	h.sessions.Store(sess.StreamSid, sess)
	defer h.sessions.Delete(sess.StreamSid)

	metrics.ActiveCalls.Inc()
	defer func() {
		metrics.ActiveCalls.Dec()
		metrics.CallDuration.Observe(time.Since(sess.CallStart).Seconds())
	}()

	// --- Initialize call via gRPC (get system prompt + voice config) ---
	if err := h.initializeCall(ctx, sess); err != nil {
		h.log.Error("InitializeCall failed", zap.Error(err))
		// Continue with defaults — don't abort the call
	}

	// --- Select TTS provider ---
	ttsProvider, err := tts.New(sess.TTSProvider, h.ttsKeys)
	if err != nil {
		h.log.Warn("TTS provider unavailable", zap.Error(err), zap.String("provider", sess.TTSProvider))
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

	// g5: TTS worker (only if provider is available)
	if ttsProvider != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runTTSWorker(ctx, sess, ttsProvider)
		}()
	}

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
	// Extract stream_sid and call_sid from the "start" event
	if startData, ok := event["start"].(map[string]interface{}); ok {
		if sid, _ := startData["streamSid"].(string); sid != "" {
			sess.StreamSid = sid
			sess.UpdateStreamType()
		}
		if callSid, _ := startData["callSid"].(string); callSid != "" {
			sess.CallSid = callSid
			// Redis lookup: override lead info with what Python dialer stored
			info, ok := h.store.GetPendingCall(ctx, callSid)
			if !ok {
				// fallback: try "latest"
				info, ok = h.store.GetPendingCall(ctx, "latest")
			}
			if ok {
				sess.LeadName = info.Name
				sess.LeadPhone = info.Phone
				sess.LeadID = info.LeadID
				sess.Interest = info.Interest
				sess.CampaignID = info.CampaignID
				if info.TTSProvider != "" {
					sess.TTSProvider = info.TTSProvider
				}
				if info.TTSVoiceID != "" {
					sess.TTSVoiceID = info.TTSVoiceID
				}
			}
		}
	}
	// Also accept top-level stream_sid
	if sid, _ := event["stream_sid"].(string); sid != "" && sess.StreamSid == "" {
		sess.StreamSid = sid
		sess.UpdateStreamType()
	}
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
		ChatHistory: sess.HistorySnapshot(),
		DurationS:   float32(time.Since(sess.CallStart).Seconds()),
		StereoWav:   wavBytes,
	}
	go h.recordingSvc.SaveAndAnalyze(ctx, req)
}

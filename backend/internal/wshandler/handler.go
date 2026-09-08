package wshandler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/audio"
	"github.com/globussoft/callified-backend/internal/config"
	"github.com/globussoft/callified-backend/internal/db"
	"github.com/globussoft/callified-backend/internal/dial"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/metrics"
	"github.com/globussoft/callified-backend/internal/prompt"
	"github.com/globussoft/callified-backend/internal/recording"
	rstore "github.com/globussoft/callified-backend/internal/redis"
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
	cfg               *config.Config
	promptBuilder     *prompt.Builder    // Phase 3C: replaces gRPC InitializeCall
	recordingSvc      *recording.Service // Phase 4: replaces gRPC FinalizeCall
	store             *rstore.Store
	db                *db.DB          // for lead lookups when Redis pending-call info is sparse
	provider          *llm.Provider   // Phase 0: native Go LLM
	initiator         *dial.Initiator // optional: used to hang up bridge calls from browser
	ttsKeys           map[string]string
	log               *zap.Logger
	sessions          sync.Map // stream_sid → *CallSession (for monitor WebSocket)
	sessionsByCallSid sync.Map // call_sid → *CallSession (for monitor lookup during dial flow before stream_sid arrives)
}

// SetInitiator wires the dial initiator after main has constructed it.
func (h *Handler) SetInitiator(i *dial.Initiator) {
	h.initiator = i
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
	// Validate query params BEFORE upgrading the WS so garbage from the
	// browser sandbox surfaces as a clean HTTP 400 (which the JS WebSocket API
	// turns into onerror/onclose-before-onopen) instead of opening a session
	// that then silently fails downstream — e.g. an unknown tts_provider would
	// previously upgrade the WS, fail tts.New() with a buried log warning, and
	// the sandbox would record audio with no TTS coming back.
	q := r.URL.Query()
	if msg := validateMediaStreamParams(q); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Extract initial identity from query params (may be overridden by "start" event)
	streamSid := q.Get("stream_sid")
	if streamSid == "" {
		streamSid = fmt.Sprintf("web_sim_%s_%d", q.Get("lead_id"), time.Now().UnixMilli())
	}
	isTataStream := strings.Contains(r.URL.Path, "/tata") || strings.EqualFold(q.Get("provider"), "tata")
	if isTataStream && strings.HasPrefix(streamSid, "web_sim_") {
		streamSid = fmt.Sprintf("tata_%s_%d", firstNonEmpty(q.Get("lead_id"), "call"), time.Now().UnixMilli())
	}

	sess := NewCallSession(streamSid, conn, h.log)
	if isTataStream {
		sess.Provider = "tata"
		sess.IsExotel = false
		sess.UseUlaw = strings.EqualFold(q.Get("codec"), "ulaw") || strings.EqualFold(q.Get("codec"), "mulaw")
	}
	sess.IsInbound = q.Get("mode") == "inbound-sim" || q.Get("direction") == "inbound"
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
	if id := q.Get("org_id"); id != "" {
		fmt.Sscanf(id, "%d", &sess.OrgID)
	}
	// Snapshot whether the URL explicitly carried a language BEFORE
	// initializeCall has a chance to populate sess.Language from a platform-
	// default fallback (GetOrganizationVoiceSettings(0) returns "en" when no
	// org row is matched). The immediate STT+greeting fire path below keys
	// off this snapshot, not sess.Language, so that Voicebot real-Dial (URL
	// is empty until the start frame lands) correctly defers to
	// handleStartEvent's Redis-hydration path instead of firing a greeting
	// in the platform-default English/Aditya combo.
	deferTataInboundUntilStart := isTataStream && sess.IsInbound
	langFromQuery := !deferTataInboundUntilStart && (q.Get("tts_language") != "" || (isTataStream && (q.Get("lead_id") != "" || q.Get("campaign_id") != "" || q.Get("org_id") != "")))
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
	if d := q.Get("max_call_duration_seconds"); d != "" {
		fmt.Sscanf(d, "%d", &sess.MaxCallDurationSeconds)
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	h.sessions.Store(sess.StreamSid, sess)
	defer func() {
		// Accumulate talk time and log the call activity for the agent who initiated it.
		if (sess.UserID > 0 || sess.UserEmail != "") && !sess.CallStart.IsZero() {
			userID := sess.UserID
			orgID := sess.OrgID
			if userID == 0 {
				if u, err := h.db.GetUserByEmail(sess.UserEmail); err == nil && u != nil {
					userID = u.ID
					orgID = u.OrgID
				}
			}
			if userID > 0 {
				dur := int64(time.Since(sess.CallStart).Seconds())
				if dur > 0 {
					_ = h.db.AddAgentTalkTime(userID, dur)
				}
				outcome := "no_answer"
				if dur > 30 {
					outcome = "completed"
				} else if dur > 5 {
					outcome = "connected"
				}
				_ = h.db.LogAgentActivity(userID, orgID, sess.CampaignID, sess.LeadID, db.ActivityCall, map[string]any{
					"duration_s": dur,
					"outcome":    outcome,
					"call_sid":   sess.CallSid,
				})
			}
		}
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
	if sess.IsInbound {
		h.applyInboundReceptionistPrompt(sess)
	}

	// --- Voice consistency cache (lead_voice:{id}, 90-day TTL) ---
	// Same lead reliably hears the same agent voice across calls (ported from
	// main-branch ws_handler.py 4aa3fa3). Best-effort: errors are swallowed.
	//
	// Skip the cache for web-sim streams: Sim Web Call is a testing tool. When
	// the operator changes a campaign's voice and hits Sim, they expect to
	// hear the freshly-saved voice — having a stale 90-day per-lead cache
	// silently override it is exactly the trap we want to avoid here.
	isSim := strings.HasPrefix(streamSid, "web_sim_")
	if !isSim && h.store != nil && sess.LeadID != 0 && sess.TTSVoiceID != "" {
		voice, fromCache := h.store.ResolveLeadVoice(ctx, sess.LeadID, sess.TTSVoiceID)
		if fromCache && voice != sess.TTSVoiceID {
			h.log.Info("voice cache: using cached voice",
				zap.Int64("lead_id", sess.LeadID),
				zap.String("from", sess.TTSVoiceID),
				zap.String("to", voice))
			sess.TTSVoiceID = voice
		}
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
	// Build the transcript callback once and share it between single and dual
	// clients. Dual mode runs primary (multi/lang) + secondary (hi) in parallel
	// and merges by confidence within 300ms — recovers Hindi misclassified by
	// Deepgram's "multi" mode. Mirrors main-branch ws_handler.py 4aa3fa3.
	onTranscript := func(text string) {
		if sess.IsMaxDurationClosing() {
			return
		}
		if first, elapsed := sess.MarkSTTFirst(); first {
			metrics.STTFirstByteLatency.Observe(elapsed)
		}
		// NOTE: we intentionally do NOT drop transcripts just because a hangup
		// has been requested. The customer may interrupt the AI's goodbye and
		// cancel the hangup via barge-in. processTranscript is the gate that
		// decides whether to act on a post-hangup transcript.
		// Explicit language switch request ("can you speak in kannada" etc.)
		// must be handled even during TTS cooldown — Sarvam detects these as
		// English but the customer clearly wants a different language.
		if targetLang, ok := isExplicitLangSwitch(text); ok {
			sess.Log.Info("lang: explicit switch request",
				zap.String("text", text), zap.String("target", targetLang))
			sess.SwitchLanguage(targetLang)
		}
		// Drop background filler sounds (hu, ha, hmm, ah, uh...) — Sarvam
		// picks these up as speech but they are not real customer replies.
		// Agent keeps waiting for a meaningful response.
		if isFillerSound(text) {
			// If a barge-in is pending, a filler sound means the user did not
			// actually intend to interrupt — cancel the barge-in so TTS can resume.
			if sess.CancelBargeIn() {
				sess.Log.Info("barge-in: cancelled by filler sound", zap.String("text", text))
			} else {
				sess.SetBargeIn(false)
				sess.Log.Debug("transcript dropped: filler sound", zap.String("text", text))
			}
			return
		}
		// A real transcript confirms any pending barge-in before we apply the
		// normal TTS cooldown filter.
		if sess.IsBargeInPending() {
			sess.ConfirmBargeIn()
		}
		// Suppress transcripts while TTS is playing or within 1s of it ending
		// to prevent the agent's own voice from looping back as customer input.
		// Skip this check when a barge-in was just confirmed — the user intentionally
		// interrupted the agent.
		if !sess.IsBargeInActive() && (sess.IsTTSPlaying() || sess.MsSinceTTSEnd() < 1000) {
			sess.Log.Debug("transcript dropped: TTS cooldown",
				zap.Bool("tts_playing", sess.IsTTSPlaying()),
				zap.Int64("ms_since_tts_end", sess.MsSinceTTSEnd()))
			return
		}
		// Guard against send on closed channel if session tore down mid-STT.
		select {
		case sess.Transcripts <- text:
		case <-ctx.Done():
		}
	}
	onSpeechStarted := func() {
		// Sarvam ASR detected possible human speech. Keep this tentative until
		// the final transcript arrives, because short fillers/noise such as
		// "hmm" should not cut off the agent mid-sentence.
		if sess.IsBargeInPending() {
			sess.Log.Debug("barge-in: speech_start while pending")
		} else {
			sess.TryBargeIn("SpeechStarted")
		}
	}

	var wg sync.WaitGroup

	// STT and greeting must be started *after* sess.Language is final. For
	// web-sim that's already true (URL params populated everything). For real
	// Exotel calls the WS connects with empty params and the campaign's
	// language only arrives via Redis on the "start" event — starting STT or
	// sending the greeting before then locks them to the wrong language for
	// the duration of the call (Deepgram doesn't accept mid-stream language
	// switches, and the greeting is one-shot).
	//
	// Solution: make both deferrable via closures that handleStartEvent can
	// trigger after Redis hydration completes, and only fire them now when
	// the URL params already gave us enough.
	var sttStarted atomic.Bool
	startSTT := func() {
		// Idempotent: web-sim invokes this directly at startup; handleStartEvent
		// invokes it again after Redis hydration if it wasn't fired yet.
		// Without the atomic gate a stray Exotel "start" event mid-call would
		// spawn a second Deepgram connection on the same audio channel.
		if sttStarted.Swap(true) {
			return
		}
		// g2: STT goroutine.
		// Sarvam realtime STT is used for Indian-language calls — it streams
		// audio over a WebSocket and emits partial transcripts, which let us
		// confirm a barge-in within the first few hundred milliseconds instead
		// of waiting for a full utterance to be POSTed to the batch API.
		// Deepgram is used as fallback when no Sarvam key is configured.
		wg.Add(1)
		onLangDetected := func(transcript string, detectedLang string) {
			// Auto language switching is disabled. The customer must explicitly
			// ask for a language switch (handled in onTranscript via
			// isExplicitLangSwitch). We still log detections for debugging.
			sess.Log.Debug("lang: detected but not auto-switching",
				zap.String("detected", detectedLang),
				zap.String("text", transcript))
		}
		onPartialTranscript := func(text string) {
			// Partial transcripts are not sent to the LLM pipeline; they only
			// confirm that the user's interruption was real speech. Use a
			// stricter filter than isFillerSound so short partial words like
			// "he" / "my" / "no" (often the beginning of a real interruption)
			// still confirm the barge-in.
			if isKnownFiller(text) {
				if sess.CancelBargeIn() {
					sess.Log.Info("barge-in: cancelled by filler partial", zap.String("text", text))
				}
				return
			}
			if sess.IsBargeInPending() {
				sess.ConfirmBargeIn()
			}
		}
		if h.cfg.SarvamAPIKey != "" && stt.SarvamLangSupported(sess.Language) {
			sarvamClient := stt.NewSarvamRealtimeClient(h.cfg.SarvamAPIKey, h.log)
			sarvamClient.OnTranscript = onTranscript
			sarvamClient.OnSpeechStarted = onSpeechStarted
			sarvamClient.OnPartialTranscript = onPartialTranscript
			sarvamClient.OnTranscriptWithLang = onLangDetected
			go func() {
				defer wg.Done()
				sarvamClient.Run(ctx, sess.AudioIn)
			}()
		} else {
			// Fallback: Deepgram.
			useDualSTT := sess.Language != "hi" && sess.Language != "en" && sess.Language != ""
			if useDualSTT {
				dual := stt.NewDualClient(h.cfg.DeepgramAPIKey, sess.Language, "hi", h.log)
				dual.OnTranscript = onTranscript
				// BARGE-IN DISABLED: OnSpeechStarted left nil.
				go func() {
					defer wg.Done()
					dual.Run(ctx, sess.AudioIn)
				}()
			} else {
				dgClient := stt.NewClient(h.cfg.DeepgramAPIKey, sess.Language, h.log)
				dgClient.OnTranscript = onTranscript
				// BARGE-IN DISABLED: OnSpeechStarted left nil.
				go func() {
					defer wg.Done()
					dgClient.Run(ctx, sess.AudioIn)
				}()
			}
		}
	}
	sess.StartSTT = startSTT

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
		runTTSWorker(ctx, sess, h.initiator)
	}()

	// Greeting closure — dispatched here for web-sim (we already have the
	// language from URL params), or from handleStartEvent for Exotel after
	// Redis hydration finalises the language. Reads sess.TTSInstance() so
	// it picks up whatever provider was actually configured.
	sendGreeting := func() {
		if !sess.TrySetGreeting() || sess.GreetingText == "" {
			return
		}
		prov := sess.TTSInstance()
		if prov == nil {
			return
		}
		go synthesizeAndSend(ctx, sess, prov, sess.GreetingText)
		// Also broadcast the greeting to monitors / Sandbox panel and store it
		// in chat history so the AI's opening line shows up alongside the
		// user's reply (issue #33). Without this, the Live Transcripts panel
		// only ever showed turns starting from the user's first utterance.
		sess.BroadcastTranscript("agent", sess.GreetingText)
		sess.AppendHistory("model", sess.GreetingText)
	}
	sess.SendGreeting = sendGreeting

	// For web-sim and direct API calls the URL carries the language, voice,
	// and lead context — start STT and send the greeting now. For Exotel
	// Voicebot the URL is empty until the "start" event lands so we defer
	// here and let handleStartEvent fire these after Redis hydration.
	//
	// The gate is `langFromQuery`, NOT `sess.Language != ""` — `initializeCall`
	// may have populated sess.Language from the platform-default fallback
	// when orgID/campaignID/leadID were all zero (the case for Voicebot at
	// connect time). Without this snapshot we'd fire a greeting in the
	// fallback English/Aditya combo, then handleStartEvent's deferred
	// sendGreeting would no-op because TrySetGreeting was already consumed
	// — exactly the "Hello. I'm Aditya..." regression seen in transcript #334.
	if langFromQuery {
		startSTT()
		sendGreeting()
	}
	h.startMaxCallDurationTimer(ctx, sess)

	// --- g1: WebSocket message loop ---
	done := h.messageLoop(ctx, sess)
	cancel() // signal all goroutines to stop

	// Close AudioIn so the STT send goroutine exits its range loop.
	// Do NOT close sess.Transcripts — the Deepgram receive goroutine may still
	// deliver a final transcript after cancel(), and sending to a closed channel
	// panics. runPipeline exits via ctx.Done() instead.
	close(sess.AudioIn)
	// Close BridgeCh to signal the agent browser relay goroutine to exit.
	if sess.IsBridge {
		close(sess.BridgeCh)
	}

	wg.Wait()

	if !done {
		// Abnormal close (network error) — still finalize
	}

	h.finalizeCall(context.Background(), sess)
}

// messageLoop reads WebSocket frames until the connection closes or a "stop"
// event is received. Returns true on clean stop, false on error.
func (h *Handler) messageLoop(ctx context.Context, sess *CallSession) bool {
	// One-shot frame-shape diagnostic — captures the first 5 inbound frames
	// per session so we can see exactly what protocol the WS opener is
	// speaking (Exotel Voicebot direct-WSS vs Stream/Passthru applet vs
	// browser web-sim).
	framesLogged := 0
	for {
		msgType, msg, err := sess.WS.ReadMessage()
		if err != nil {
			return false
		}

		if framesLogged < 5 {
			framesLogged++
			switch msgType {
			case websocket.TextMessage:
				preview := string(msg)
				if len(preview) > 200 {
					preview = preview[:200]
				}
				var probe map[string]interface{}
				_ = json.Unmarshal(msg, &probe)
				h.log.Info("ws frame probe",
					zap.String("stream_sid", sess.StreamSid),
					zap.Int("seq", framesLogged),
					zap.String("type", "text"),
					zap.Int("bytes", len(msg)),
					zap.Any("event_field", probe["event"]),
					zap.Strings("top_keys", topKeys(probe)),
					zap.String("preview", preview),
				)
			case websocket.BinaryMessage:
				n := len(msg)
				if n > 16 {
					n = 16
				}
				h.log.Info("ws frame probe",
					zap.String("stream_sid", sess.StreamSid),
					zap.Int("seq", framesLogged),
					zap.String("type", "binary"),
					zap.Int("bytes", len(msg)),
					zap.String("first16_hex", fmt.Sprintf("%x", msg[:n])),
				)
			default:
				h.log.Info("ws frame probe",
					zap.String("stream_sid", sess.StreamSid),
					zap.Int("seq", framesLogged),
					zap.Int("ws_msg_type", msgType),
					zap.Int("bytes", len(msg)),
				)
			}
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

// topKeys returns the top-level keys of a parsed JSON object — handy in the
// frame-shape diagnostic so we can see whether a "start"-like envelope uses
// our expected shape without logging the full payload.
func topKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (h *Handler) handleBinaryFrame(sess *CallSession, data []byte) {
	// Keep processing audio even if a hangup has been requested. A customer
	// interruption during the AI's goodbye should still trigger barge-in and
	// cancel the hangup.
	var pcm []byte
	if sess.UseUlaw {
		if sess.EchoCanceller.IsEcho(data) {
			metrics.EchoSuppressions.Inc()
			return
		}
		pcm = audio.UlawToPCM(data)
	} else {
		// Echo canceller stores μ-law TTS history; convert incoming PCM to μ-law
		// for echo detection, then continue processing the original PCM.
		if sess.EchoCanceller.IsEcho(audio.PCMToUlaw(data)) {
			metrics.EchoSuppressions.Inc()
			return
		}
		pcm = data // PCM-16 LE — Voicebot applet, browser web-sim
	}
	if sess.IsBridge {
		// Record customer audio for the server-side stereo WAV, then relay
		// to the agent browser via BridgeCh.
		sess.AppendMicChunk(pcm)
		bridgeSendRealtime(sess.BridgeCh, append([]byte(nil), pcm...))
		return
	}
	sess.AppendMicChunk(pcm)
	// Run VAD on every frame so the adaptive noise floor stays current.
	// Arm barge-in while TTS is playing, within 500ms of synthesis ending, or
	// within 1500ms of the last audio frame being sent (covers carrier/phone
	// buffering so the customer can interrupt even the end of a long sentence).
	vadSpeech := sess.VAD.ProcessPCM(pcm)
	if vadSpeech {
		sess.TryBargeIn("VAD")
	}
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
	if sess.Provider == "tata" {
		event = normalizeTataFrame(sess, event)
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

func normalizeTataFrame(sess *CallSession, event map[string]interface{}) map[string]interface{} {
	rawEvent := strings.ToLower(firstNonEmpty(
		pickStr(event, "event", "Event"),
		pickStr(event, "type", "Type"),
		pickStr(event, "message", "Message"),
	))

	callSid := pickStr(event, "ref_id", "refId", "call_sid", "callSid", "CallSid", "call_id", "callId", "CallID", "uuid")
	streamSid := pickStr(event, "stream_sid", "streamSid", "stream_id", "streamId", "StreamID", "streamSid")
	if streamSid == "" {
		streamSid = sess.StreamSid
	}

	switch rawEvent {
	case "connected", "connection_established", "websocket_connected":
		event["event"] = "connected"
	case "start", "started", "call_started", "stream_start", "stream_started", "call_connected", "answered":
		event["event"] = "start"
	case "media", "audio", "voice", "chunk", "audio_chunk":
		event["event"] = "media"
	case "stop", "stopped", "stream_stop", "stream_stopped", "call_ended", "completed", "hangup":
		event["event"] = "stop"
	default:
		if _, ok := event["media"]; ok {
			event["event"] = "media"
		}
	}

	if event["event"] == "start" {
		start, _ := event["start"].(map[string]interface{})
		if start == nil {
			start = map[string]interface{}{}
			event["start"] = start
		}
		if callSid != "" {
			start["call_sid"] = callSid
		}
		if streamSid != "" {
			start["stream_sid"] = streamSid
		}
		for _, key := range []string{
			"from", "From", "caller", "Caller", "caller_id", "callerId", "caller_id_number", "callerIdNumber",
			"customer_number", "customerNumber", "customer_no", "customerNo", "call_from", "CallFrom", "call_from_number", "from_number", "ani",
			"to", "To", "call_to_number", "callToNumber", "called_number", "calledNumber", "did", "DID",
			"destination", "destination_number", "destinationNumber",
			"caller_name", "callerName", "customer_name", "customerName", "name", "Name",
		} {
			if v := pickStr(event, key); v != "" {
				start[key] = v
			}
		}
		if codec := strings.ToLower(pickStr(event, "codec", "audio_codec", "encoding")); codec != "" {
			if codec == "ulaw" || codec == "mulaw" || codec == "pcmu" {
				sess.UseUlaw = true
			} else if strings.Contains(codec, "pcm") {
				sess.UseUlaw = false
			}
		}
	}

	if event["event"] == "media" {
		media, _ := event["media"].(map[string]interface{})
		if media == nil {
			media = map[string]interface{}{}
			event["media"] = media
		}
		if payload := firstNonEmpty(
			pickStr(media, "payload", "audio", "audio_data", "data", "chunk"),
			pickStr(event, "payload", "audio", "audio_data", "data", "chunk"),
		); payload != "" {
			media["payload"] = payload
		}
		if streamSid != "" {
			event["stream_sid"] = streamSid
		}
	}

	return event
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

		// Codec / envelope-shape detection from the start envelope's key
		// casing. Voicebot applet and browser web-sim both use snake_case
		// `stream_sid` and PCM-16 LE. Twilio and the older Exotel Stream/
		// Passthru applet use camelCase `streamSid` and μ-law. Without this
		// per-call detection, Voicebot calls get μ-law decode/encode applied
		// to PCM-16 bytes → garbled noise in both directions.
		_, hasSnake := startData["stream_sid"]
		_, hasCamel := startData["streamSid"]
		switch {
		case hasSnake && !hasCamel:
			sess.UseUlaw = false
		case hasCamel && !hasSnake:
			sess.UseUlaw = true
		}
		if sess.Provider == "tata" {
			// Tata Voice Streaming frames arrive in Twilio-style camelCase and
			// the media payload size matches 8 kHz μ-law chunks. Keep Tata on
			// μ-law unless Tata explicitly adds a PCM codec marker above.
			sess.UseUlaw = true
		}
		h.log.Info("ws codec detected",
			zap.String("stream_sid", sess.StreamSid),
			zap.Bool("is_exotel", sess.IsExotel),
			zap.Bool("use_ulaw", sess.UseUlaw),
			zap.Bool("start_has_snake", hasSnake),
			zap.Bool("start_has_camel", hasCamel),
		)
		// Browser web-sim passes the authenticated user's email in the start
		// event because WebSocket connections cannot send Authorization headers.
		if email := pickStr(startData, "user_email", "userEmail"); email != "" {
			sess.UserEmail = email
			if u, err := h.db.GetUserByEmail(email); err == nil && u != nil {
				sess.UserID = u.ID
			}
		}
		if phone := pickStr(startData,
			"from", "From", "caller", "Caller", "caller_id", "callerId", "caller_id_number", "callerIdNumber",
			"customer_number", "customerNumber", "customer_no", "customerNo", "call_from", "CallFrom", "call_from_number", "from_number", "ani",
		); phone != "" && sess.LeadPhone == "" {
			sess.LeadPhone = phone
		}
		if name := pickStr(startData, "caller_name", "callerName", "customer_name", "customerName", "name", "Name"); name != "" && sess.LeadName == "" {
			sess.LeadName = name
		}
		if sess.Provider == "tata" && sess.IsInbound && sess.OrgID == 0 && h.db != nil {
			if did := pickStr(startData,
				"to", "To", "call_to_number", "callToNumber", "called_number", "calledNumber",
				"did", "DID", "destination", "destination_number", "destinationNumber",
			); did != "" {
				if account, err := h.db.GetInboundTataAccountByDID(did); err == nil && account != nil {
					sess.OrgID = account.OrgID
					if sess.Interest == "" {
						sess.Interest = "inbound enquiry"
					}
					h.log.Info("tata inbound account matched",
						zap.String("did", did),
						zap.Int64("org_id", account.OrgID),
						zap.Int64("account_id", account.ID))
				} else if err != nil {
					h.log.Warn("tata inbound account lookup failed", zap.String("did", did), zap.Error(err))
				}
			}
		}
		if sess.IsInbound && sess.LeadName == "" && sess.LeadPhone != "" && sess.OrgID > 0 && h.db != nil {
			if lead, err := h.db.GetLeadByPhoneOrg(sess.LeadPhone, sess.OrgID, nil, false); err == nil && lead != nil {
				sess.LeadID = lead.ID
				sess.LeadName = strings.TrimSpace(lead.FirstName + " " + lead.LastName)
				if sess.Interest == "" {
					sess.Interest = lead.Interest
				}
			}
		}

		callSidKeys := []string{"callSid", "call_sid", "CallSid"}
		if sess.Provider == "tata" {
			callSidKeys = []string{"ref_id", "refId", "call_sid", "callSid", "CallSid"}
		}
		if callSid := pickStr(startData, callSidKeys...); callSid != "" {
			sess.CallSid = callSid
			h.sessionsByCallSid.Store(callSid, sess)
			// Redis lookup precedence:
			//   1) under the carrier-issued call_sid (set by dial.Initiator)
			//   2) under "phone:<E164>" (set by manual-call web-sim mode)
			//   3) under "latest" (last-resort fallback)
			hitKey := ""
			info, ok := h.store.GetPendingCall(ctx, callSid)
			if ok {
				hitKey = "call_sid"
			}
			if !ok {
				if phone := pickStr(startData, "from", "From", "to", "To"); phone != "" {
					info, ok = h.store.GetPendingCall(ctx, "phone:"+phone)
					if ok {
						hitKey = "phone"
					}
				}
			}
			if !ok {
				info, ok = h.store.GetPendingCall(ctx, "latest")
				if ok {
					hitKey = "latest"
				}
			}
			h.log.Info("ws redis hydration lookup",
				zap.String("call_sid", callSid),
				zap.String("hit_key", hitKey),
				zap.Bool("ok", ok),
			)
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
				if info.OrgID != 0 {
					sess.OrgID = info.OrgID
				}
				if info.TTSProvider != "" {
					sess.TTSProvider = info.TTSProvider
				}
				if info.TTSVoiceID != "" {
					sess.TTSVoiceID = info.TTSVoiceID
				}
				if info.TTSLanguage != "" {
					sess.TTSLanguage = info.TTSLanguage
					sess.Language = info.TTSLanguage
				}
				if info.MaxCallDurationSeconds > 0 {
					sess.MaxCallDurationSeconds = info.MaxCallDurationSeconds
				}
				// Carry credit-bypass flag from the dial initiator so post-call
				// deduction can be skipped for unlimited manual calls.
				sess.SkipCredits = info.SkipCredits
				sess.UserEmail = info.UserEmail
				sess.UserID = info.UserID
				// Rebuild SystemPrompt and GreetingText now that we know the
				// real campaign/org/lead. The initial initializeCall ran
				// before the start event with all-zero IDs (Exotel's Passthru
				// applet doesn't forward our query params), so it produced a
				// generic prompt with no language directive — Sarvam's Indian
				// voices then default to Hindi, and the LLM follows suit even
				// when the campaign is set to English.
				if h.promptBuilder != nil {
					_ = h.initializeCall(ctx, sess)
					if sess.IsInbound {
						h.applyInboundReceptionistPrompt(sess)
					}
				}
				// Re-create the TTS provider in case the original startup picked
				// the wrong one (Exotel calls hit tts.New("") which falls back
				// to ElevenLabs — wrong if the campaign uses sarvam/smallest).
				// The TTS worker reads sess.TTSInstance() on every sentence, so
				// swapping it here makes subsequent synthesis use the correct
				// provider without restarting the goroutine.
				if sess.TTSProvider != "" {
					if newProv, err := tts.New(sess.TTSProvider, h.ttsKeys); err == nil && newProv != nil {
						sess.SetTTSInstance(newProv)
					}
				}
				// Bridge mode: skip AI pipeline entirely (no STT, no greeting).
				// Audio from Exotel is relayed to the agent browser via BridgeCh.
				if info.IsBridge {
					sess.IsBridge = true
					sess.StartSTT = nil
					sess.SendGreeting = nil
					h.log.Info("bridge mode: AI pipeline skipped", zap.String("call_sid", callSid))
				} else {
					// Fire the deferred STT + greeting now that the language is
					// final. ServeHTTP wired these closures and skipped the
					// immediate startup path because URL params didn't carry a
					// language. StartSTT is a no-op the second time (web-sim
					// already invoked it directly); SendGreeting is gated by
					// TrySetGreeting so it's also single-shot.
					if sess.StartSTT != nil && sess.Language != "" {
						sess.StartSTT()
						sess.StartSTT = nil // prevent double-start on a second start event
					}
					if sess.SendGreeting != nil {
						sess.SendGreeting()
					}
					h.startMaxCallDurationTimer(ctx, sess)
				}
			}
			if sess.IsInbound && !sess.IsBridge {
				if h.promptBuilder != nil {
					_ = h.initializeCall(ctx, sess)
					h.applyInboundReceptionistPrompt(sess)
				}
				if sess.TTSProvider != "" {
					if newProv, err := tts.New(sess.TTSProvider, h.ttsKeys); err == nil && newProv != nil {
						sess.SetTTSInstance(newProv)
					}
				}
				if sess.StartSTT != nil && sess.Language != "" {
					sess.StartSTT()
					sess.StartSTT = nil
				}
				if sess.SendGreeting != nil {
					sess.SendGreeting()
				}
				h.startMaxCallDurationTimer(ctx, sess)
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
	// Keep processing media even if a hangup has been requested so a customer
	// can still barge-in during the AI's goodbye and cancel the hangup.
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
	if sess.UseUlaw {
		if sess.EchoCanceller.IsEcho(raw) {
			metrics.EchoSuppressions.Inc()
			return
		}
		pcm = audio.UlawToPCM(raw)
	} else {
		// Echo canceller stores μ-law TTS history; convert incoming PCM to μ-law
		// for echo detection, then continue processing the original PCM.
		if sess.EchoCanceller.IsEcho(audio.PCMToUlaw(raw)) {
			metrics.EchoSuppressions.Inc()
			return
		}
		pcm = raw // PCM-16 LE — Voicebot applet, browser web-sim
	}
	if sess.IsBridge {
		// Record customer audio for the server-side stereo WAV, then relay
		// to the agent browser via BridgeCh.
		sess.AppendMicChunk(pcm)
		bridgeSendRealtime(sess.BridgeCh, append([]byte(nil), pcm...))
		return
	}
	sess.AppendMicChunk(pcm)
	// Run VAD on every frame so the adaptive noise floor stays current.
	// Arm barge-in while TTS is playing, within 500ms of synthesis ending, or
	// within 1500ms of the last audio frame being sent (covers carrier/phone
	// buffering so the customer can interrupt even the end of a long sentence).
	vadSpeech := sess.VAD.ProcessPCM(pcm)
	if vadSpeech {
		sess.TryBargeIn("VAD")
	}
	select {
	case sess.AudioIn <- pcm:
	default:
	}

	// Relay a copy of the caller's inbound audio to any attached monitors.
	if sess.hasMonitors() {
		format := "pcm16_8k"
		if sess.UseUlaw {
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
	if callCtx.CallMemoryCount > 0 {
		sess.Log.Info("call memory injected",
			zap.Int("entries", callCtx.CallMemoryCount),
			zap.Int64("lead_id", sess.LeadID))
	}
	// Only fill in TTS fields the caller didn't already set via query params.
	// The Sandbox / web-sim flow passes ?tts_provider=&voice=&tts_language=
	// to override the org default for one session — without this guard, the
	// org default clobbers the explicit selection and the user always hears
	// the same default voice regardless of what they pick. (issue: Sandbox
	// "voice picker doesn't change the voice")
	if sess.TTSProvider == "" && callCtx.TTSProvider != "" {
		sess.TTSProvider = callCtx.TTSProvider
	}
	if sess.TTSVoiceID == "" && callCtx.TTSVoiceID != "" {
		sess.TTSVoiceID = callCtx.TTSVoiceID
	}
	// Only adopt callCtx.TTSLanguage when we had real context (an org or a
	// campaign). With orgID=0 && campaignID=0 — the Voicebot-at-connect case
	// before handleStartEvent has hydrated from Redis — BuildCallContext
	// falls through to GetOrganizationVoiceSettings(0) which returns the
	// platform-default English. Promoting that to sess.Language would make
	// the immediate-fire gate in ServeHTTP think "we know the language" and
	// fire a greeting in English/Aditya before Redis hydration runs. Keep
	// sess.Language empty so handleStartEvent's deferred path is the only
	// one that gets to set it.
	hasRealContext := sess.OrgID != 0 || sess.CampaignID != 0
	if hasRealContext && sess.TTSLanguage == "" && callCtx.TTSLanguage != "" {
		sess.TTSLanguage = callCtx.TTSLanguage
		sess.Language = callCtx.TTSLanguage // drives Deepgram language + LLM prompt language
	}
	if hasRealContext && sess.MaxCallDurationSeconds == 0 && callCtx.MaxCallDurationSeconds > 0 {
		sess.MaxCallDurationSeconds = callCtx.MaxCallDurationSeconds
	}
	if callCtx.AgentName != "" {
		sess.AgentName = callCtx.AgentName
	}
	// Swap the persona name in the greeting when the session's voice differs
	// from whatever the prompt builder used. Two cases:
	//   1. Org has a default voice (e.g. "aditya") and the Sandbox picked a
	//      different one (e.g. "mithali"): swap "Aditya" → "Mithali".
	//   2. Org has NO default voice configured: the prompt builder rendered
	//      the greeting with the empty-voice fallback ("Arjun"). The Sandbox
	//      almost always hits this path, so without the swap every voice
	//      ends up greeted as "Arjun".
	if sess.TTSVoiceID != "" && sess.TTSVoiceID != callCtx.TTSVoiceID {
		oldName := prompt.AgentPersonaName(callCtx.TTSVoiceID, sess.Language)
		newName := prompt.AgentPersonaName(sess.TTSVoiceID, sess.Language)
		if oldName != "" && newName != "" && oldName != newName {
			sess.GreetingText = strings.ReplaceAll(sess.GreetingText, oldName, newName)
		}
	}
	return nil
}

func (h *Handler) startMaxCallDurationTimer(ctx context.Context, sess *CallSession) {
	if sess.IsBridge || sess.MaxCallDurationSeconds <= 0 || !sess.TryStartMaxDurationTimer() {
		return
	}
	limit := time.Duration(sess.MaxCallDurationSeconds) * time.Second
	wait := limit - time.Since(sess.CallStart)
	if wait < 0 {
		wait = 0
	}
	softLead := maxDurationSoftLead(limit)
	softWait := wait - softLead
	sess.Log.Info("max call duration: timer started",
		zap.Duration("limit", limit),
		zap.Duration("wait", wait),
		zap.Duration("soft_lead", softLead))

	go func() {
		hardWait := wait
		if softWait > 0 {
			softTimer := time.NewTimer(softWait)
			select {
			case <-ctx.Done():
				softTimer.Stop()
				return
			case <-softTimer.C:
				sess.RequestMaxDurationSoftClose()
				sess.Log.Info("max call duration: wrap-up mode started",
					zap.Int("max_call_duration_seconds", sess.MaxCallDurationSeconds))
			}
			hardWait = softLead
		} else {
			sess.RequestMaxDurationSoftClose()
		}
		timer := time.NewTimer(hardWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if sess.HangupRequested() {
			return
		}
		sess.RequestMaxDurationWaitReply()
		sess.Log.Info("max call duration: waiting for final customer reply",
			zap.Int("max_call_duration_seconds", sess.MaxCallDurationSeconds))
		go h.forceMaxDurationCloseIfNoReply(ctx, sess)
	}()
}

func maxDurationSoftLead(limit time.Duration) time.Duration {
	if limit <= 45*time.Second {
		return 8 * time.Second
	}
	lead := limit / 5
	if lead < 10*time.Second {
		return 10 * time.Second
	}
	if lead > 30*time.Second {
		return 30 * time.Second
	}
	return lead
}

func maxDurationClosingLine(language string) string {
	switch language {
	case "hi":
		return "ठीक है, धन्यवाद. मैं अभी यहीं रोकता हूँ. हमारी टीम आगे की जानकारी के साथ follow up करेगी."
	case "mr":
		return "ठीक आहे, धन्यवाद. मी आत्ता इथेच थांबतो. आमची टीम पुढील माहिती घेऊन follow up करेल."
	case "te":
		return "సరే, ధన్యవాదాలు. నేను ఇప్పటికి ఇక్కడే ఆపుతాను. మా టీమ్ మరిన్ని వివరాలతో follow up చేస్తుంది."
	case "ta":
		return "சரி, நன்றி. நான் இப்போது இங்கே நிறுத்துகிறேன். எங்கள் team மேலும் விவரங்களுடன் follow up செய்யும்."
	case "kn":
		return "ಸರಿ, ಧನ್ಯವಾದಗಳು. ನಾನು ಈಗ ಇಲ್ಲಿಯೇ ನಿಲ್ಲಿಸುತ್ತೇನೆ. ನಮ್ಮ team ಹೆಚ್ಚಿನ ವಿವರಗಳೊಂದಿಗೆ follow up ಮಾಡುತ್ತದೆ."
	case "bn":
		return "ঠিক আছে, ধন্যবাদ. আমি এখন এখানেই থামছি. আমাদের team আরও details নিয়ে follow up করবে."
	case "gu":
		return "બરાબર, આભાર. હું અત્યારે અહીં જ અટકું છું. અમારી team વધુ માહિતી સાથે follow up કરશે."
	case "pa":
		return "ਠੀਕ ਹੈ, ਧੰਨਵਾਦ. ਮੈਂ ਹੁਣ ਇੱਥੇ ਹੀ ਰੁਕਦਾ ਹਾਂ. ਸਾਡੀ team ਹੋਰ ਜਾਣਕਾਰੀ ਨਾਲ follow up ਕਰੇਗੀ."
	case "ml":
		return "ശരി, നന്ദി. ഞാൻ ഇപ്പോൾ ഇവിടെ നിർത്തുന്നു. കൂടുതൽ വിവരങ്ങളുമായി ഞങ്ങളുടെ team follow up ചെയ്യും."
	default:
		return "Alright, thank you. Our senior employee will contact you with more details. Have a great day."
	}
}

func maxDurationClosingLineForReply(language, reply string) string {
	if language != "" && language != "en" {
		return maxDurationClosingLine(language)
	}
	reply = strings.ToLower(strings.TrimSpace(reply))
	switch {
	case strings.Contains(reply, "can you provide") || strings.Contains(reply, "do you provide") || strings.Contains(reply, "can you help"):
		return "Yes, we can help with that. Our senior employee will contact you with more details. Have a great day."
	case isCustomerQuestion(reply):
		return "Good question. It usually depends on employee count, locations, access points, and attendance process. Our senior employee will contact you with more details. Have a great day."
	case reply != "":
		return "Got it, thank you for sharing. Our senior employee will contact you with more details. Have a great day."
	default:
		return maxDurationClosingLine(language)
	}
}

func isCustomerQuestion(text string) bool {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return false
	}
	if strings.Contains(text, "?") {
		return true
	}
	for _, prefix := range []string{
		"what ", "why ", "when ", "where ", "who ", "which ", "how ",
		"can ", "could ", "do ", "does ", "did ", "is ", "are ", "will ", "would ", "should ",
		"tell me", "explain", "clarify",
	} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func (h *Handler) forceMaxDurationCloseIfNoReply(ctx context.Context, sess *CallSession) {
	delay := sess.PlaybackTracker.RemainingDuration() + 25*time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if sess.HangupRequested() || sess.IsMaxDurationClosing() || !sess.ConsumeMaxDurationWaitReply() {
		return
	}
	closeLine := maxDurationClosingLine(sess.Language)
	sess.Log.Info("max call duration: closing without final reply",
		zap.Int("max_call_duration_seconds", sess.MaxCallDurationSeconds))
	sess.RequestMaxDurationClose()
	sess.BroadcastTranscript("agent", closeLine)
	sess.AppendHistory("model", closeLine)
	select {
	case sess.TTSSentences <- closeLine:
	default:
	}
	select {
	case sess.TTSSentences <- "":
	default:
	}
	go h.forceMaxDurationHangup(sess, closeLine)
}

func (h *Handler) forceMaxDurationHangup(sess *CallSession, spokenLine string) {
	delay := sess.PlaybackTracker.RemainingDuration() + estimateSpeechDuration(spokenLine) + 4*time.Second
	time.Sleep(delay)
	if !sess.IsMaxDurationClosing() {
		return
	}
	if sess.WS != nil {
		_ = sess.WS.Close()
	}
	if h.initiator != nil && sess.CallSid != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.initiator.Hangup(ctx, sess.CallSid, sess.CampaignID); err != nil {
			sess.Log.Warn("max call duration: carrier hangup failed",
				zap.String("call_sid", sess.CallSid),
				zap.Error(err))
		}
	}
}

func estimateSpeechDuration(text string) time.Duration {
	words := len(strings.Fields(text))
	if words == 0 {
		words = len([]rune(text)) / 8
	}
	d := time.Duration(words) * 350 * time.Millisecond
	if d < 2*time.Second {
		return 2 * time.Second
	}
	if d > 10*time.Second {
		return 10 * time.Second
	}
	return d
}

// finalizeCall runs post-call processing (Phase 4: native Go, no gRPC).
func (h *Handler) finalizeCall(ctx context.Context, sess *CallSession) {
	h.log.Info("finalizeCall: started",
		zap.String("stream_sid", sess.StreamSid),
		zap.Int64("lead_id", sess.LeadID),
		zap.Int("chat_history_len", len(sess.ChatHistory)))

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
		SkipCredits: sess.SkipCredits,
		UserEmail:   sess.UserEmail,
		IsInbound:   sess.IsInbound,
	}
	go h.recordingSvc.SaveAndAnalyze(ctx, req)
}

func (h *Handler) applyInboundReceptionistPrompt(sess *CallSession) {
	baseKnowledge := h.inboundProductKnowledge(sess.OrgID)
	if baseKnowledge == "" {
		baseKnowledge = strings.TrimSpace(sess.SystemPrompt)
	}
	lang := sess.Language
	if lang == "" {
		lang = sess.TTSLanguage
	}
	if lang == "" {
		lang = "en"
	}
	label := inboundLanguageLabel(lang)
	company := "our team"
	if sess.AgentName != "" {
		company = sess.AgentName
	}
	if sess.Interest == "" {
		sess.Interest = "inbound enquiry"
	}
	sess.SystemPrompt = fmt.Sprintf(`You are a warm inbound AI receptionist for %s.

The customer called us first. Do not behave like an outbound sales caller and do not say you are calling them.

Goal:
Greet the caller, identify which product/service they are asking about, collect the caller's name, help with their requirement, then collect phone number before ending.

Rules:
1. Respond only in %s unless the customer explicitly asks for another language.
2. Do not switch languages just because the caller speaks, mixes, or is transcribed in another language. Do not mirror the caller's language automatically.
3. If the caller explicitly asks to continue in another language, switch to that language and stay there until they ask to switch again.
4. Ask one question at a time.
5. Keep every reply short and natural for voice. Use one complete sentence when possible, two at most, and always end with punctuation.
6. If the caller asks about a known product and their name is not known yet, acknowledge the product briefly and ask for their name before answering details. Example: "Sure, I can help with EmpMonitor. May I know who I am speaking with?"
7. If the caller gives their name, remember it and use it naturally.
8. After name is known, answer product/service questions from the matching product's knowledge only.
9. If the product is unclear, ask which product they mean instead of assuming.
10. Phone number is compulsory before ending. Ask for it near the end if it is missing, even if the caller only wanted basic information.
11. Do not ask for email address, mail ID, or any written-contact detail. If the caller wants details sent, say the team can call them and ask for their phone number.
12. When the enquiry is complete and phone number is collected, summarize the next step and end with [HANGUP].
13. Never reveal you are an AI.

Call context: %s

Available company/product knowledge:
%s`, company, label, sess.Interest, baseKnowledge)

	sess.GreetingText = inboundGreeting(lang, company)
}

func (h *Handler) inboundProductKnowledge(orgID int64) string {
	if h.db == nil || orgID == 0 {
		return ""
	}
	products, err := h.db.GetProductsByOrg(orgID)
	if err != nil || len(products) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Use this catalog for inbound callers. The company has multiple products, so do not assume the first product. Match by product name or ask a clarifying question.\n")
	for i, p := range products {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Product: %s\n", p.Name)
		if s := strings.TrimSpace(p.ScrapedInfo); s != "" {
			fmt.Fprintf(&b, "Details: %s\n", s)
		}
		if s := strings.TrimSpace(p.ManualNotes); s != "" {
			fmt.Fprintf(&b, "Notes: %s\n", s)
		}
		if s := strings.TrimSpace(p.AgentPersona); s != "" {
			fmt.Fprintf(&b, "Persona: %s\n", s)
		}
		if s := strings.TrimSpace(p.CallFlowInstructions); s != "" {
			fmt.Fprintf(&b, "Call flow: %s\n", s)
		}
	}
	return strings.TrimSpace(b.String())
}

func inboundLanguageLabel(lang string) string {
	switch lang {
	case "hi":
		return "Hindi"
	case "mr":
		return "Marathi"
	case "bn":
		return "Bengali"
	case "gu":
		return "Gujarati"
	case "pa":
		return "Punjabi"
	case "ta":
		return "Tamil"
	case "te":
		return "Telugu"
	case "kn":
		return "Kannada"
	case "ml":
		return "Malayalam"
	default:
		return "English"
	}
}

func inboundGreeting(lang, company string) string {
	switch lang {
	case "hi":
		return fmt.Sprintf("Namaste, %s mein aapka swagat hai. Main aapki kaise madad kar sakta hoon?", company)
	default:
		return fmt.Sprintf("Hi, thanks for calling %s. How can I help you today?", company)
	}
}

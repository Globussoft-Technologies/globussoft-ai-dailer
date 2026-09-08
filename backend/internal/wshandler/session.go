// Package wshandler manages per-call WebSocket state and orchestration.
package wshandler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/globussoft/callified-backend/internal/audio"
	"github.com/globussoft/callified-backend/internal/llm"
	"github.com/globussoft/callified-backend/internal/metrics"
	"github.com/globussoft/callified-backend/internal/tts"
)

// CallSession holds all per-call state for one WebSocket connection.
// Hot-path fields (audio path) use atomics or channels — no locks.
type CallSession struct {
	// Identity (set on connect / start event)
	StreamSid string
	CallSid   string
	Provider  string
	IsExotel  bool
	IsWebSim  bool
	IsInbound bool
	// IsBridge=true: browser-to-phone mode. AI pipeline is skipped; audio is
	// relayed between Exotel and the agent's browser WebSocket via BridgeCh.
	IsBridge bool
	// SkipCredits=true: this call should not be charged against the org's
	// prepaid balance (unlimited manual calls for AI-hidden users).
	SkipCredits bool
	// UserEmail is the agent/admin who initiated the call.
	UserEmail string
	// UserID is the resolved DB user id of the agent/admin who initiated the call.
	UserID int64
	// UseUlaw decides whether inbound/outbound audio is μ-law or PCM-16 LE,
	// and whether outbound JSON envelopes use camelCase ("streamSid") vs
	// snake_case ("stream_sid").
	//
	//   ulaw + camelCase  → Twilio / old Exotel Stream/Passthru applet
	//   PCM-16 + snake_case → Exotel Voicebot applet + browser web-sim
	//
	// Default at session creation = IsExotel (preserves legacy behaviour
	// when no start frame has arrived yet). Re-evaluated in handleStartEvent
	// from the casing of the start envelope's stream-id key.
	UseUlaw    bool
	LeadName   string
	LeadPhone  string
	Interest   string
	LeadID     int64
	CampaignID int64
	OrgID      int64
	// BridgeCh carries decoded PCM chunks from the Exotel phone to the agent
	// browser WebSocket when IsBridge=true. Closed when the Exotel call ends.
	BridgeCh chan []byte
	// agentConnected is true while exactly one /ws/agent WebSocket is relaying
	// audio for this bridge session. A second connection is rejected to prevent
	// two goroutines writing to the same Exotel WS (cross-voice contamination).
	agentConnected atomic.Bool

	// Atomic flags — safe to read/write without locks
	greetingSent         atomic.Bool
	ttsPlaying           atomic.Bool
	hangupReq            atomic.Bool
	dgAlive              atomic.Bool
	maxDurationStarted   atomic.Bool
	maxDurationSoftClose atomic.Bool
	maxDurationWaitReply atomic.Bool
	maxDurationClosing   atomic.Bool
	finalCloseReq        atomic.Bool
	bargeInActive        atomic.Bool  // set by VAD-detected speech during TTS; cleared when new LLM response starts
	bargeInPending       atomic.Bool  // true while waiting for STT confirmation of a barge-in
	bargeInDeadline      atomic.Int64 // UnixNano; STT must confirm by this time
	confirmedBargeInNano atomic.Int64 // UnixNano of last STT-confirmed barge-in
	lastBargeInNano      atomic.Int64 // UnixNano of last barge-in trigger — prevents re-triggering
	lastTTSEndNano       atomic.Int64 // UnixNano
	lastAudioSentNano    atomic.Int64 // UnixNano of last outbound audio frame sent
	lastTranscript       atomic.Int64 // UnixNano — debounce timestamp
	outboundSeq          atomic.Uint64

	// Serialization
	llmMu sync.Mutex // one LLM turn at a time per session
	wsMu  sync.Mutex // serialize concurrent WebSocket writes

	// Channels (created in NewCallSession)
	AudioIn      chan []byte // ulaw→PCM frames from WS → STT goroutine
	Transcripts  chan string // STT results → pipeline orchestrator
	TTSSentences chan string // sentences from pipeline → TTS worker

	// TTS barge-in cancellation
	cancelTTS context.CancelFunc
	cancelMu  sync.Mutex

	// Audio processing helpers
	PlaybackTracker *audio.PlaybackTracker
	EchoCanceller   *audio.EchoCanceller
	VAD             *audio.VAD

	// Monitor (manager dashboard) WebSocket connections
	monitorMu    sync.RWMutex
	monitorConns map[*websocket.Conn]struct{}

	// TTFB measurement: track first STT transcript time for metrics
	sttFirstAt atomic.Pointer[time.Time]

	// Server-side stereo recording buffers
	recMu           sync.Mutex
	micChunks       []audio.TimedChunk
	ttsChunks       []audio.TimedChunk
	micRecordCursor time.Time // virtual playback cursor for mic recording
	ttsRecordCursor time.Time // virtual playback cursor for TTS recording
	ttsNewUtterance bool      // signals AppendTTSChunk to reset cursor on next chunk

	// Chat history — populated by AppendHistory, read by pipeline
	historyMu   sync.Mutex
	ChatHistory []llm.ChatMessage

	// Recent customer utterances used to keep repeated questions from looping.
	repeatMu        sync.Mutex
	recentQuestions []repeatQuestion

	// Voice config — populated after InitializeCall gRPC returns
	SystemPrompt           string
	GreetingText           string
	TTSProvider            string
	TTSVoiceID             string
	TTSLanguage            string
	MaxCallDurationSeconds int
	AgentName              string
	Language               string

	// Deferred-init hooks. Real Exotel calls connect with empty URL params
	// (the campaign context arrives later via the Redis "start" event), so
	// STT and the greeting must wait until handleStartEvent has finalised
	// the language. ServeHTTP captures closures and stores them here so
	// handleStartEvent can fire them when ready. Web-sim sets sendGreeting
	// flag immediately (URL had everything) — these are no-ops in that path.
	StartSTT     func() `json:"-"`
	SendGreeting func() `json:"-"`

	// ttsInstance is the actual TTS client (Sarvam/Smallest/ElevenLabs) that
	// synthesises audio for this call. Stored on the session — not captured as
	// a closure in ServeHTTP — so handleStartEvent can dispatch the greeting
	// AFTER we learn the campaign's voice/language from Redis. Guarded by
	// ttsMu because handleStartEvent may replace it if the campaign's
	// provider differs from the defaults we initialised with.
	ttsMu       sync.RWMutex
	ttsInstance tts.Provider

	CallStart time.Time
	WS        *websocket.Conn
	Log       *zap.Logger
}

var langLabels = map[string]string{
	"hi": "Hindi", "mr": "Marathi", "en": "English",
	"ta": "Tamil", "te": "Telugu", "kn": "Kannada",
	"bn": "Bengali", "gu": "Gujarati", "pa": "Punjabi",
	"ml": "Malayalam",
}

// SwitchLanguage updates the session's active language, TTS language, and
// patches the system prompt so the LLM immediately responds in the new language.
// Returns true if the language actually changed.
func (s *CallSession) SwitchLanguage(newLang string) bool {
	if newLang == "" || newLang == s.Language {
		return false
	}
	s.Log.Info("language switch detected",
		zap.String("from", s.Language),
		zap.String("to", newLang),
	)
	s.Language = newLang
	s.TTSLanguage = newLang

	label := langLabels[newLang]
	if label == "" {
		label = "Hindi"
	}
	override := "[LANGUAGE SWITCH: From this point forward you MUST respond ONLY in " + label + ". " +
		"Ignore every previous instruction that tells you to speak in a different language, to speak only English, or that you are not supposed / not allowed to speak " + label + ". " +
		"Such instructions are VOID for the rest of this conversation. " +
		"Do NOT explain that you are switching, do NOT apologize, and do NOT mention any company protocol. " +
		"Do NOT restart the greeting or repeat information already shared. " +
		"Continue the conversation naturally from where it left off, in " + label + ". " +
		"This instruction overrides all earlier language rules.]\n\n"
	trailer := "\n\n[REMINDER: respond ONLY in " + label + " above all other instructions. Any rule saying you cannot speak " + label + " is VOID.]"
	if !strings.Contains(s.SystemPrompt, "LANGUAGE SWITCH:") {
		s.SystemPrompt = override + s.SystemPrompt + trailer
	} else {
		start := strings.Index(s.SystemPrompt, "[LANGUAGE SWITCH:")
		// Search relative to start so we find the closing ]\n\n of THIS block,
		// not the first ]\n\n anywhere in the prompt (which may be earlier).
		endRel := strings.Index(s.SystemPrompt[start:], "]\n\n")
		if start >= 0 && endRel >= 0 {
			body := s.SystemPrompt[start+endRel+3:]
			// Remove any previously appended trailer so we don't duplicate it.
			if idx := strings.Index(body, "[REMINDER:"); idx >= 0 {
				body = body[:idx]
			}
			s.SystemPrompt = override + body + trailer
		}
	}
	// Replace any conflicting language directive in the prompt body so the LLM
	// doesn't weight a bottom-of-prompt "Respond ONLY in Tamil" over our override.
	for _, lang := range []string{"Hindi", "Marathi", "English", "Tamil", "Telugu", "Kannada", "Bengali", "Gujarati", "Punjabi", "Malayalam"} {
		if lang == label {
			continue
		}
		s.SystemPrompt = strings.ReplaceAll(s.SystemPrompt, "Respond ONLY in "+lang, "Respond ONLY in "+label)
		s.SystemPrompt = strings.ReplaceAll(s.SystemPrompt, "Respond only in "+lang, "Respond ONLY in "+label)
		s.SystemPrompt = strings.ReplaceAll(s.SystemPrompt, "Respond in "+lang, "Respond ONLY in "+label)
		s.SystemPrompt = strings.ReplaceAll(s.SystemPrompt, "[LANG:"+strings.ToLower(lang[:2])+"]", "[LANG:"+newLang+"]")
	}
	return true
}

// SetTTSInstance stores the TTS client for this call.
func (s *CallSession) SetTTSInstance(p tts.Provider) {
	s.ttsMu.Lock()
	s.ttsInstance = p
	s.ttsMu.Unlock()
}

// TTSInstance returns the current TTS client (may be nil if no provider was
// available — e.g., missing API key).
func (s *CallSession) TTSInstance() tts.Provider {
	s.ttsMu.RLock()
	defer s.ttsMu.RUnlock()
	return s.ttsInstance
}

// NewCallSession allocates a CallSession. streamSid may be empty at this point
// (it is filled in later from the "start" event for Exotel calls).
func NewCallSession(streamSid string, ws *websocket.Conn, log *zap.Logger) *CallSession {
	isWebSim := strings.HasPrefix(streamSid, "web_sim_")
	isExotel := !isWebSim && !strings.HasPrefix(streamSid, "SM")
	s := &CallSession{
		StreamSid:       streamSid,
		IsExotel:        isExotel,
		IsWebSim:        isWebSim,
		UseUlaw:         isExotel, // legacy default; overridden by start-frame casing detection
		WS:              ws,
		Log:             log,
		AudioIn:         make(chan []byte, 512),
		Transcripts:     make(chan string, 32),
		TTSSentences:    make(chan string, 64),
		BridgeCh:        make(chan []byte, 10), // 10 frames × 20 ms = 200 ms max latency
		CallStart:       time.Now(),
		PlaybackTracker: audio.NewPlaybackTracker(isExotel),
		EchoCanceller:   audio.NewEchoCanceller(),
		VAD:             audio.NewVAD(),
		monitorConns:    make(map[*websocket.Conn]struct{}),
	}
	s.dgAlive.Store(true)
	return s
}

// --- Monitor connection management ---

// AddMonitor registers a manager WebSocket connection that will receive
// live transcripts from this call session.
func (s *CallSession) AddMonitor(conn *websocket.Conn) {
	s.monitorMu.Lock()
	s.monitorConns[conn] = struct{}{}
	s.monitorMu.Unlock()
}

// RemoveMonitor deregisters a manager WebSocket connection.
func (s *CallSession) RemoveMonitor(conn *websocket.Conn) {
	s.monitorMu.Lock()
	delete(s.monitorConns, conn)
	s.monitorMu.Unlock()
}

// hasMonitors returns true if at least one monitor WS is attached. Used as a
// fast-path guard on hot audio broadcasts so we don't marshal JSON for nothing.
func (s *CallSession) hasMonitors() bool {
	s.monitorMu.RLock()
	defer s.monitorMu.RUnlock()
	return len(s.monitorConns) > 0
}

// BroadcastAudio relays a single audio chunk to all attached monitor clients.
// role is "user" (inbound from phone/mic) or "agent" (outbound TTS).
// payloadB64 is the already base64-encoded audio. Format is "ulaw_8k" for
// Exotel calls (both directions) and "pcm16_8k" for web-sim calls.
//
// This is on the hot audio path — callers MUST guard with hasMonitors() to
// avoid JSON marshalling when nobody's listening.
func (s *CallSession) BroadcastAudio(role, payloadB64, format string) {
	msg, err := json.Marshal(map[string]string{
		"type":    "audio",
		"role":    role,
		"format":  format,
		"payload": payloadB64,
	})
	if err != nil {
		return
	}
	s.monitorMu.RLock()
	defer s.monitorMu.RUnlock()
	for conn := range s.monitorConns {
		conn.WriteMessage(websocket.TextMessage, msg) //nolint:errcheck
	}
}

// BroadcastTranscript sends a real-time transcript event to all connected
// monitor clients. role is "user" or "agent". Matches Python:
//
//	await monitor.send_json({"type":"transcript","role":"user","text":"..."})
//
// For web-sim sessions (the AI Training Sandbox / browser-driven calls) we
// also write the same payload back to the caller WS so the sandbox's "Live
// Transcripts" panel updates as the user speaks and the agent replies.
// (issue #33) Real Exotel/Twilio carrier WS sockets are skipped — they only
// expect their own protocol frames; sending stray JSON to them risks the
// carrier closing the session.
func (s *CallSession) BroadcastTranscript(role, text string) {
	msg, err := json.Marshal(map[string]string{
		"type": "transcript",
		"role": role,
		"text": text,
	})
	if err != nil {
		return
	}
	s.monitorMu.RLock()
	for conn := range s.monitorConns {
		conn.WriteMessage(websocket.TextMessage, msg) //nolint:errcheck
	}
	s.monitorMu.RUnlock()
	if s.IsWebSim && s.WS != nil {
		s.WS.WriteMessage(websocket.TextMessage, msg) //nolint:errcheck
	}
}

// MarkSTTFirst records the time of the first STT transcript (once) and returns
// whether this was the first call. Used to emit STT TTFB metrics.
func (s *CallSession) MarkSTTFirst() (first bool, elapsed float64) {
	now := time.Now()
	if s.sttFirstAt.CompareAndSwap(nil, &now) {
		return true, now.Sub(s.CallStart).Seconds()
	}
	return false, 0
}

// --- Stream type ---

// UpdateStreamType re-evaluates IsExotel/IsWebSim after StreamSid is updated
// from a "start" event.
func (s *CallSession) UpdateStreamType() {
	s.IsWebSim = strings.HasPrefix(s.StreamSid, "web_sim_")
	s.IsExotel = !s.IsWebSim && !strings.HasPrefix(s.StreamSid, "SM")
	s.PlaybackTracker = audio.NewPlaybackTracker(s.IsExotel)
}

// TrySetGreeting atomically marks greeting as sent. Returns true only the first call.
func (s *CallSession) TrySetGreeting() bool { return s.greetingSent.CompareAndSwap(false, true) }

func (s *CallSession) SetTTSPlaying(v bool)  { s.ttsPlaying.Store(v) }
func (s *CallSession) IsTTSPlaying() bool    { return s.ttsPlaying.Load() }
func (s *CallSession) RequestHangup()        { s.hangupReq.Store(true) }
func (s *CallSession) HangupRequested() bool { return s.hangupReq.Load() }
func (s *CallSession) TryStartMaxDurationTimer() bool {
	return s.maxDurationStarted.CompareAndSwap(false, true)
}
func (s *CallSession) RequestMaxDurationSoftClose() { s.maxDurationSoftClose.Store(true) }
func (s *CallSession) IsMaxDurationSoftClosing() bool {
	return s.maxDurationSoftClose.Load()
}
func (s *CallSession) RequestMaxDurationWaitReply() {
	s.maxDurationSoftClose.Store(true)
	s.maxDurationWaitReply.Store(true)
}
func (s *CallSession) ConsumeMaxDurationWaitReply() bool {
	return s.maxDurationWaitReply.CompareAndSwap(true, false)
}
func (s *CallSession) RequestMaxDurationClose() {
	s.maxDurationSoftClose.Store(true)
	s.maxDurationWaitReply.Store(false)
	s.maxDurationClosing.Store(true)
	s.RequestHangup()
}
func (s *CallSession) IsMaxDurationClosing() bool { return s.maxDurationClosing.Load() }
func (s *CallSession) RequestFinalClose() {
	s.finalCloseReq.Store(true)
	s.maxDurationWaitReply.Store(false)
	s.SetBargeInPending(false)
	s.SetBargeIn(false)
	s.RequestHangup()
}
func (s *CallSession) IsFinalClosing() bool {
	return s.finalCloseReq.Load() || s.IsMaxDurationClosing()
}
func (s *CallSession) StopDG()               { s.dgAlive.Store(false) }
func (s *CallSession) DGAlive() bool         { return s.dgAlive.Load() }
func (s *CallSession) SetBargeIn(v bool)     { s.bargeInActive.Store(v) }
func (s *CallSession) IsBargeInActive() bool { return s.bargeInActive.Load() }

// TriggerBargeIn is called by energy VAD when mic energy exceeds the threshold
// while TTS is playing. Returns false if barge-in was triggered too recently
// (500 ms cooldown) or is already active.
func (s *CallSession) TriggerBargeIn() bool {
	now := time.Now().UnixNano()
	last := s.lastBargeInNano.Load()
	if now-last < 500*int64(time.Millisecond) {
		return false // cooldown: don't flood barge-in events
	}
	if !s.lastBargeInNano.CompareAndSwap(last, now) {
		return false // another goroutine won the race
	}
	s.interruptActiveTTS()
	return true
}

func (s *CallSession) interruptActiveTTS() {
	s.SetBargeIn(true)
	metrics.BargeIns.Inc()
	s.DrainTTSSentences()
	go func() {
		time.Sleep(3 * time.Second)
		s.SetBargeIn(false)
	}()
	s.CancelActiveTTS()
	// Send clear event to flush browser/phone audio queue
	var frame []byte
	if s.IsWebSim {
		frame, _ = json.Marshal(map[string]string{"type": "clear"})
	} else if s.IsExotel {
		frame, _ = json.Marshal(map[string]string{"event": "clear", "streamSid": s.StreamSid})
	}
	if frame != nil && s.WS != nil {
		_ = s.SendText(frame)
	}
}

// TryBargeIn attempts to trigger a tentative barge-in when customer speech is
// detected. Returns true if barge-in was actually triggered. It requires TTS to
// be playing or recently finished (within 800ms of TTS end or 2000ms of last
// audio sent), and it respects the cooldown/active/pending guards.
func (s *CallSession) TryBargeIn(source string) bool {
	if s.IsFinalClosing() {
		return false
	}
	recentTTS := s.IsTTSPlaying() || s.MsSinceTTSEnd() < 800 || s.MsSinceAudioSent() < 2000
	if !recentTTS || s.IsBargeInActive() || s.IsBargeInPending() {
		return false
	}
	if !s.TentativeTriggerBargeIn() {
		return false
	}
	s.Log.Info("barge-in: triggered",
		zap.String("source", source),
		zap.Float64("noise_floor", s.VAD.NoiseFloor()))
	return true
}

// SetBargeInPending marks whether a barge-in is waiting for STT confirmation.
func (s *CallSession) SetBargeInPending(v bool) { s.bargeInPending.Store(v) }

// IsBargeInPending returns true when a barge-in has fired but STT has not yet
// confirmed it with a real transcript.
func (s *CallSession) IsBargeInPending() bool { return s.bargeInPending.Load() }

// BargeInDeadline returns the UnixNano deadline by which STT must confirm a
// pending barge-in. Zero means no deadline is active.
func (s *CallSession) BargeInDeadline() int64 { return s.bargeInDeadline.Load() }

// TentativeTriggerBargeIn triggers barge-in but keeps it in a pending state.
// STT must confirm the interruption with a real transcript within 4s;
// otherwise the barge-in is automatically cancelled so the AI can continue.
func (s *CallSession) TentativeTriggerBargeIn() bool {
	now := time.Now().UnixNano()
	last := s.lastBargeInNano.Load()
	if now-last < 500*int64(time.Millisecond) {
		return false
	}
	if !s.lastBargeInNano.CompareAndSwap(last, now) {
		return false
	}
	s.SetBargeInPending(true)
	deadline := time.Now().Add(4000 * time.Millisecond)
	s.bargeInDeadline.Store(deadline.UnixNano())
	go func() {
		time.Sleep(time.Until(deadline))
		// If still pending, no meaningful transcript arrived — cancel barge-in.
		if s.bargeInPending.CompareAndSwap(true, false) {
			s.SetBargeIn(false)
			s.Log.Info("barge-in: cancelled (no STT confirmation)")
		}
	}()
	return true
}

// ConfirmBargeIn marks a pending barge-in as confirmed by real STT input.
// It also clears any pending hangup request — an interruption during the AI's
// goodbye means the customer wants to keep talking.
// Returns true if there was a pending barge-in to confirm.
func (s *CallSession) ConfirmBargeIn() bool {
	if !s.bargeInPending.CompareAndSwap(true, false) {
		return false
	}
	if s.IsFinalClosing() {
		s.SetBargeIn(false)
		s.Log.Info("barge-in: ignored during final close")
		return true
	}
	s.interruptActiveTTS()
	s.confirmedBargeInNano.Store(time.Now().UnixNano())
	if s.HangupRequested() {
		s.hangupReq.Store(false)
		s.Log.Info("barge-in: confirmed by STT; hangup cancelled")
	} else {
		s.Log.Info("barge-in: confirmed by STT")
	}
	return true
}

// ConsumeRecentConfirmedBargeIn returns true once for the transcript that
// follows an STT-confirmed barge-in. Stale confirmations are ignored so a
// speech_start event without a final transcript cannot tag a later normal turn.
func (s *CallSession) ConsumeRecentConfirmedBargeIn(maxAge time.Duration) bool {
	nano := s.confirmedBargeInNano.Load()
	if nano == 0 {
		return false
	}
	if time.Since(time.Unix(0, nano)) > maxAge {
		s.confirmedBargeInNano.CompareAndSwap(nano, 0)
		return false
	}
	return s.confirmedBargeInNano.CompareAndSwap(nano, 0)
}

// CancelBargeIn cancels a pending barge-in and resets the active flag.
// Returns true if a pending barge-in was actually cancelled.
func (s *CallSession) CancelBargeIn() bool {
	if !s.bargeInPending.CompareAndSwap(true, false) {
		return false
	}
	s.SetBargeIn(false)
	return true
}

// DrainTTSSentences discards all sentences currently buffered in TTSSentences.
// Called on barge-in so stale agent sentences don't play after the customer interrupts.
func (s *CallSession) DrainTTSSentences() int {
	drained := 0
	for {
		select {
		case <-s.TTSSentences:
			drained++
		default:
			return drained
		}
	}
}

func (s *CallSession) MarkTTSEnd() { s.lastTTSEndNano.Store(time.Now().UnixNano()) }
func (s *CallSession) MsSinceTTSEnd() int64 {
	end := s.lastTTSEndNano.Load()
	if end == 0 {
		return 9999
	}
	return (time.Now().UnixNano() - end) / int64(time.Millisecond)
}

// MarkAudioSent records the current time as when the last outbound audio frame
// was sent. Used to extend the barge-in window past TTS synthesis end so the
// customer can interrupt while audio is still playing on the phone.
func (s *CallSession) MarkAudioSent() { s.lastAudioSentNano.Store(time.Now().UnixNano()) }

// MsSinceAudioSent returns milliseconds since the last outbound audio frame.
// Returns 9999 if no audio has been sent yet.
func (s *CallSession) MsSinceAudioSent() int64 {
	last := s.lastAudioSentNano.Load()
	if last == 0 {
		return 9999
	}
	return (time.Now().UnixNano() - last) / int64(time.Millisecond)
}

// StampTranscript records the current time as the latest transcript timestamp
// and returns it. Used for debouncing: if the value changes before the debounce
// sleep completes, the current processing run is stale and should be aborted.
func (s *CallSession) StampTranscript() int64 {
	ts := time.Now().UnixNano()
	s.lastTranscript.Store(ts)
	return ts
}
func (s *CallSession) LastTranscript() int64 { return s.lastTranscript.Load() }

// SetCancelTTS stores a context.CancelFunc for the active TTS goroutine.
func (s *CallSession) SetCancelTTS(cancel context.CancelFunc) {
	s.cancelMu.Lock()
	s.cancelTTS = cancel
	s.cancelMu.Unlock()
}

// CancelActiveTTS cancels any ongoing TTS synthesis (barge-in).
func (s *CallSession) CancelActiveTTS() {
	s.cancelMu.Lock()
	if s.cancelTTS != nil {
		s.cancelTTS()
		s.cancelTTS = nil
	}
	s.cancelMu.Unlock()
}

// SendText sends a text WebSocket frame thread-safely.
func (s *CallSession) SendText(data []byte) error {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	return s.WS.WriteMessage(websocket.TextMessage, data)
}

// AppendMicChunk records a user PCM chunk for server-side stereo recording.
// Uses a virtual cursor (same pattern as AppendTTSChunk) so network jitter in
// the Exotel WebSocket stream does not cause chunks to land on overlapping
// offsets in the WAV buffer — which previously created audible crackle/clicks.
func (s *CallSession) AppendMicChunk(pcm []byte) {
	const micHz = 8000 * 2 // bytes per second at 8kHz 16-bit mono
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.micRecordCursor.IsZero() {
		s.micRecordCursor = time.Now()
	}
	s.micChunks = append(s.micChunks, audio.TimedChunk{Ts: s.micRecordCursor, Data: append([]byte(nil), pcm...)})
	s.micRecordCursor = s.micRecordCursor.Add(time.Duration(len(pcm)) * time.Second / micHz)
}

// MarkTTSNewUtterance signals that the next TTS chunk starts a new utterance.
// Call this before each TTS synthesis so AppendTTSChunk resets its virtual
// playback cursor to wall-clock time, preserving silence gaps between turns.
func (s *CallSession) MarkTTSNewUtterance() {
	s.recMu.Lock()
	s.ttsNewUtterance = true
	s.recMu.Unlock()
}

// AppendTTSChunk records an AI PCM chunk for server-side stereo recording.
// Uses a virtual playback cursor (advancing by chunk duration at 8kHz) so
// batch TTS providers that deliver audio faster than real-time don't cluster
// all chunks at the same timestamp and overwrite each other in the WAV buffer.
func (s *CallSession) AppendTTSChunk(pcm []byte) {
	const ttsHz = 8000 * 2 // bytes per second at 8kHz 16-bit mono
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.ttsRecordCursor.IsZero() {
		s.ttsRecordCursor = time.Now()
		s.ttsNewUtterance = false
	} else if s.ttsNewUtterance {
		// Sarvam TTS delivers audio faster than real-time. If synthesis of the
		// next sentence completes before the previous sentence has "played out",
		// time.Now() is still behind ttsRecordCursor. Resetting to time.Now()
		// would make this sentence overlap the previous one in the WAV buffer
		// → audible distortion. Use the later of the two so we never go back.
		if now := time.Now(); now.After(s.ttsRecordCursor) {
			s.ttsRecordCursor = now
		}
		s.ttsNewUtterance = false
	}
	ts := s.ttsRecordCursor
	s.ttsChunks = append(s.ttsChunks, audio.TimedChunk{Ts: ts, Data: append([]byte(nil), pcm...)})
	s.ttsRecordCursor = s.ttsRecordCursor.Add(time.Duration(len(pcm)) * time.Second / ttsHz)
}

// DrainRecordingBuffers returns copies of both recording buffers and clears them.
func (s *CallSession) DrainRecordingBuffers() (mic, tts []audio.TimedChunk) {
	s.recMu.Lock()
	mic = append([]audio.TimedChunk(nil), s.micChunks...)
	tts = append([]audio.TimedChunk(nil), s.ttsChunks...)
	s.micChunks = nil
	s.ttsChunks = nil
	s.recMu.Unlock()
	return
}

// AppendHistory adds a turn to the conversation history.
func (s *CallSession) AppendHistory(role, text string) {
	s.historyMu.Lock()
	s.ChatHistory = append(s.ChatHistory, llm.ChatMessage{Role: role, Text: text})
	s.historyMu.Unlock()
}

// HistorySnapshot returns a copy of the current conversation history.
func (s *CallSession) HistorySnapshot() []llm.ChatMessage {
	s.historyMu.Lock()
	snap := make([]llm.ChatMessage, len(s.ChatHistory))
	copy(snap, s.ChatHistory)
	s.historyMu.Unlock()
	return snap
}

// MaxTokens returns a token budget based on transcript length and language,
// clamped to a sane range. Indian languages are tokenized less efficiently by
// LLM subword tokenizers, so they get a higher multiplier and a higher cap.
func (s *CallSession) MaxTokens(transcript string) int32 {
	isEnglish := s.Language == "en"

	if s.IsInbound {
		perWord, minTok, maxTok := int32(34), int32(500), int32(900)
		if !isEnglish {
			perWord = 50
			maxTok = 1600
		}
		words := len(strings.Fields(transcript))
		tokens := int32(words) * perWord
		if tokens < minTok {
			return minTok
		}
		if tokens > maxTok {
			return maxTok
		}
		return tokens
	}

	perWord, minTok, maxTok := int32(20), int32(250), int32(450)
	if !isEnglish {
		perWord = 24
		minTok = 320
		maxTok = 550
	}
	words := len(strings.Fields(transcript))
	tokens := int32(words) * perWord
	if tokens < minTok {
		return minTok
	}
	if tokens > maxTok {
		return maxTok
	}
	return tokens
}

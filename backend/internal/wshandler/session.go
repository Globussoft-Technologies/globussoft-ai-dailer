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
	"github.com/globussoft/callified-backend/internal/tts"
)

// CallSession holds all per-call state for one WebSocket connection.
// Hot-path fields (audio path) use atomics or channels — no locks.
type CallSession struct {
	// Identity (set on connect / start event)
	StreamSid  string
	CallSid    string
	IsExotel   bool
	IsWebSim   bool
	LeadName   string
	LeadPhone  string
	Interest   string
	LeadID     int64
	CampaignID int64
	OrgID      int64

	// Atomic flags — safe to read/write without locks
	greetingSent   atomic.Bool
	ttsPlaying     atomic.Bool
	hangupReq      atomic.Bool
	dgAlive        atomic.Bool
	lastTTSEndNano atomic.Int64 // UnixNano
	lastTranscript atomic.Int64 // UnixNano — debounce timestamp

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

	// Monitor (manager dashboard) WebSocket connections
	monitorMu    sync.RWMutex
	monitorConns map[*websocket.Conn]struct{}

	// TTFB measurement: track first STT transcript time for metrics
	sttFirstAt atomic.Pointer[time.Time]

	// Server-side stereo recording buffers
	recMu     sync.Mutex
	micChunks []audio.TimedChunk
	ttsChunks []audio.TimedChunk

	// Chat history — populated by AppendHistory, read by pipeline
	historyMu   sync.Mutex
	ChatHistory []llm.ChatMessage

	// Voice config — populated after InitializeCall gRPC returns
	SystemPrompt string
	GreetingText string
	TTSProvider  string
	TTSVoiceID   string
	TTSLanguage  string
	AgentName    string
	Language     string

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
		WS:              ws,
		Log:             log,
		AudioIn:         make(chan []byte, 512),
		Transcripts:     make(chan string, 32),
		TTSSentences:    make(chan string, 64),
		CallStart:       time.Now(),
		PlaybackTracker: audio.NewPlaybackTracker(isExotel),
		EchoCanceller:   audio.NewEchoCanceller(),
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

// BroadcastTranscript sends a real-time transcript event to all connected monitor clients.
// role is "user" or "agent". Matches Python:
//
//	await monitor.send_json({"type":"transcript","role":"user","text":"..."})
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
	defer s.monitorMu.RUnlock()
	for conn := range s.monitorConns {
		conn.WriteMessage(websocket.TextMessage, msg) //nolint:errcheck
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

func (s *CallSession) SetTTSPlaying(v bool) { s.ttsPlaying.Store(v) }
func (s *CallSession) IsTTSPlaying() bool    { return s.ttsPlaying.Load() }
func (s *CallSession) RequestHangup()         { s.hangupReq.Store(true) }
func (s *CallSession) HangupRequested() bool  { return s.hangupReq.Load() }
func (s *CallSession) StopDG()                { s.dgAlive.Store(false) }
func (s *CallSession) DGAlive() bool          { return s.dgAlive.Load() }

func (s *CallSession) MarkTTSEnd() { s.lastTTSEndNano.Store(time.Now().UnixNano()) }
func (s *CallSession) MsSinceTTSEnd() int64 {
	end := s.lastTTSEndNano.Load()
	if end == 0 {
		return 9999
	}
	return (time.Now().UnixNano() - end) / int64(time.Millisecond)
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
func (s *CallSession) AppendMicChunk(pcm []byte) {
	s.recMu.Lock()
	s.micChunks = append(s.micChunks, audio.TimedChunk{Ts: time.Now(), Data: append([]byte(nil), pcm...)})
	s.recMu.Unlock()
}

// AppendTTSChunk records an AI PCM chunk for server-side stereo recording.
func (s *CallSession) AppendTTSChunk(pcm []byte) {
	s.recMu.Lock()
	s.ttsChunks = append(s.ttsChunks, audio.TimedChunk{Ts: time.Now(), Data: append([]byte(nil), pcm...)})
	s.recMu.Unlock()
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

// MaxTokens returns the configured max_tokens for the session's language.
func (s *CallSession) MaxTokens() int32 {
	if s.Language == "mr" {
		return 400
	}
	return 250
}

package wshandler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// maxMonitorKeyLen caps stream_sid / call_sid length. Real Twilio/Exotel SIDs
// are ~34 chars; our internal web_sim SIDs are ~40. 128 leaves generous margin
// while rejecting obvious garbage and abuse.
const maxMonitorKeyLen = 128

// validateMonitorKey enforces the same shape rules the frontend does and a few
// the frontend can't (length cap, character set). Returns "" when valid, else
// the user-facing error string.
func validateMonitorKey(key string) string {
	if key == "" {
		return "stream_sid or call_sid required"
	}
	if len(key) > maxMonitorKeyLen {
		return "stream_sid or call_sid too long"
	}
	// Real SIDs are alphanumeric plus '_' and '-'. Anything else (slashes, dots,
	// query chars) is either a path-traversal attempt or a malformed copy/paste.
	for _, r := range key {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if !ok {
			return "stream_sid or call_sid contains invalid characters"
		}
	}
	return ""
}

// Allowed values for /media-stream query params. Anything outside these sets
// is either a typo, a stale client, or an attempted abuse — better to reject
// at the door than to silently swallow it and have the call act weird.
var (
	validTTSProviders = map[string]bool{"elevenlabs": true, "sarvam": true, "smallest": true}
	validTTSLanguages = map[string]bool{
		"hi": true, "mr": true, "en": true, "ta": true, "te": true,
		"kn": true, "bn": true, "gu": true, "pa": true, "ml": true,
		"multi": true, // sandbox uses "multi" for Deepgram multi-language mode
	}
)

// Caps for free-form params. Real lead names / interests sit well under these
// limits; the cap exists to bound memory and reject obviously malformed input.
const (
	maxFreeFormParamLen = 256
	maxStreamSidLen     = 128
	maxVoiceIDLen       = 128
)

// validateMediaStreamParams checks the /media-stream and /ws/sandbox query
// string. Returns "" when valid, else a user-facing error string. Empty values
// are always allowed — those mean "use the org / campaign default", which is
// how the Exotel webhook flow works.
func validateMediaStreamParams(q url.Values) string {
	if v := q.Get("tts_provider"); v != "" && !validTTSProviders[v] {
		return "tts_provider must be one of: elevenlabs, sarvam, smallest"
	}
	if v := q.Get("tts_language"); v != "" && !validTTSLanguages[v] {
		return "tts_language is not a supported language code"
	}
	if v := q.Get("voice"); v != "" {
		if len(v) > maxVoiceIDLen {
			return "voice is too long"
		}
		for _, r := range v {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '_' || r == '-'
			if !ok {
				return "voice contains invalid characters"
			}
		}
	}
	if v := q.Get("stream_sid"); v != "" {
		if len(v) > maxStreamSidLen {
			return "stream_sid is too long"
		}
		for _, r := range v {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '_' || r == '-'
			if !ok {
				return "stream_sid contains invalid characters"
			}
		}
	}
	// Numeric IDs must parse cleanly. Previous code used fmt.Sscanf which
	// silently accepts "123abc" as 123 — a real footgun for off-by-one bugs.
	for _, key := range []string{"lead_id", "campaign_id", "org_id"} {
		if v := q.Get(key); v != "" {
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return key + " must be a number"
			}
		}
	}
	// Free-form fields: bound length only, allow the full Unicode range so
	// names like "Akhil" or interests like "2BHK in Andheri" pass through.
	for _, key := range []string{"name", "lead_name", "phone", "lead_phone", "interest"} {
		if v := q.Get(key); len(v) > maxFreeFormParamLen {
			return key + " is too long"
		}
	}
	return ""
}

// lookupSession resolves a monitor-WS key to an active CallSession. The key
// may be either a stream_sid or a call_sid. The call_sid index is populated
// only after the carrier sends the "start" event for an outbound dial, so we
// poll for up to maxWait to cover the ringing gap between /api/manual-call
// returning and the media stream opening.
func (h *Handler) lookupSession(key string, maxWait time.Duration) (*CallSession, bool) {
	deadline := time.Now().Add(maxWait)
	for {
		if raw, ok := h.sessions.Load(key); ok {
			return raw.(*CallSession), true
		}
		if raw, ok := h.sessionsByCallSid.Load(key); ok {
			return raw.(*CallSession), true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ServeMonitor handles /ws/monitor/{key} where key is a stream_sid OR call_sid.
// External consumers connect here to:
//   - Receive live transcripts: {"type":"transcript","role":"user|agent","text":"..."}
//   - Receive live audio chunks: {"type":"audio","role":"user|agent","format":"...","payload":"<base64>"}
//   - Inject whispers:           {"action":"whisper","text":"hint for AI"}
//   - Trigger takeover:          {"action":"takeover"}
//   - Send audio during takeover:{"action":"audio_chunk","payload":"<base64>"}
//
// Accepting call_sid lets /api/manual-call return a URL the client can open
// immediately, before the carrier has dialled through to /media-stream.
func (h *Handler) ServeMonitor(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/ws/monitor/"))
	if msg := validateMonitorKey(key); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("monitor ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Try an immediate lookup first; if the caller is monitoring by call_sid
	// on an outbound dial we may need to wait for the media stream to open.
	sess, ok := h.lookupSession(key, 30*time.Second)
	if !ok {
		h.log.Warn("monitor: session not found", zap.String("key", key))
		conn.WriteMessage( //nolint:errcheck
			websocket.TextMessage,
			[]byte(`{"error":"session not found"}`),
		)
		return
	}

	// Use the session's actual stream_sid for Redis-backed ops (whispers,
	// takeover) since those keys always live under stream_sid.
	streamSid := sess.StreamSid

	sess.AddMonitor(conn)
	defer sess.RemoveMonitor(conn)

	h.log.Info("monitor connected",
		zap.String("key", key),
		zap.String("stream_sid", streamSid),
	)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}

		switch data["action"] {
		case "whisper":
			text, _ := data["text"].(string)
			if text != "" {
				h.store.PushWhisper(r.Context(), streamSid, text)
				h.log.Info("monitor whisper injected",
					zap.String("stream_sid", streamSid),
					zap.String("text", text),
				)
			}

		case "takeover":
			// Set Redis takeover flag and cancel any active TTS immediately.
			// After this, processTranscript will skip the LLM for this session.
			h.store.SetTakeover(r.Context(), streamSid, true)
			sess.CancelActiveTTS()
			if sess.IsExotel {
				sendClearEvent(sess)
			}
			h.log.Info("monitor takeover activated", zap.String("stream_sid", streamSid))

		case "audio_chunk":
			// Manager sends base64 audio directly to the phone (takeover mode).
			// Only forwarded if takeover is active.
			if !h.store.GetTakeover(r.Context(), streamSid) {
				continue
			}
			payload, _ := data["payload"].(string)
			if payload == "" {
				continue
			}
			// Validate it's valid base64 before forwarding
			if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
				continue
			}
			frame, _ := json.Marshal(map[string]interface{}{
				"event":     "media",
				"streamSid": streamSid,
				"media":     map[string]string{"payload": payload},
			})
			sess.SendText(frame) //nolint:errcheck
		}
	}

	h.log.Info("monitor disconnected", zap.String("stream_sid", streamSid))
}

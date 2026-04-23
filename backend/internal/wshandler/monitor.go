package wshandler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ServeMonitor handles /ws/monitor/{stream_sid}.
// Managers connect here to:
//   - Receive real-time transcripts: {"type":"transcript","role":"user|agent","text":"..."}
//   - Inject whispers:               {"action":"whisper","text":"hint for AI"}
//   - Trigger takeover:              {"action":"takeover"}
//   - Send audio during takeover:    {"action":"audio_chunk","payload":"<base64>"}
//
// Mirrors Python ws_handler.py monitor_call().
func (h *Handler) ServeMonitor(w http.ResponseWriter, r *http.Request) {
	// Parse stream_sid from path: /ws/monitor/{stream_sid}
	streamSid := strings.TrimPrefix(r.URL.Path, "/ws/monitor/")
	if streamSid == "" {
		http.Error(w, "stream_sid required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("monitor ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Look up the active call session
	raw, ok := h.sessions.Load(streamSid)
	if !ok {
		// Session not found — accept anyway, but nothing to monitor
		h.log.Warn("monitor: session not found", zap.String("stream_sid", streamSid))
		conn.WriteMessage( //nolint:errcheck
			websocket.TextMessage,
			[]byte(`{"error":"session not found"}`),
		)
		return
	}
	sess := raw.(*CallSession)

	sess.AddMonitor(conn)
	defer sess.RemoveMonitor(conn)

	h.log.Info("monitor connected", zap.String("stream_sid", streamSid))

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
